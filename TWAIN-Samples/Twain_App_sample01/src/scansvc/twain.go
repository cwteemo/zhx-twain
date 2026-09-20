package main

/*
// 链接哪个 DLL 由 link_windows_amd64.go / link_windows_386.go 决定：
// 32 位和 64 位的产物名不一样，这里不能写死。

#include <stdlib.h>

typedef int (*ScanCallback)(char *filename);

void  zhx_Init(void);
char* zhx_GetDevicesList(void);
int   zhx_OpenDevice(char *device);
int   zhx_Scan(char *path, ScanCallback cb, int count);
void  zhx_EndScan(void);
void  zhx_CloseDevice(void);
void  zhx_Exit(void);

int         zhx_GetState(void);
const char* zhx_GetCurrentDevice(void);
int         zhx_ShowSettingUI(void);
int         zhx_GetScanDiagnosis(int *enableFailed, int *conditionCode, int *feederLoaded, int *deviceOnline);

int   zhx_SetResolution(int dpi);
int   zhx_GetCurrentResolution(void);
char* zhx_GetSupportedResolutions(void);
int   zhx_SetCapability_STR(char *nCap, char *value);
char* zhx_GetCapability_STR(char *capOrDevice);

// 由 callback.go 用 //export 导出，这里只做声明
extern int goScanCallback(char *filename);

// 包一层，把 Go 导出的回调转成 DLL 需要的函数指针类型
static int scanWithGoCallback(char *path, int count) {
    return zhx_Scan(path, (ScanCallback)goScanCallback, count);
}
*/
import "C"

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"unsafe"
)

// TWAIN 状态机的几个档位，判断"能做什么"全看它。
const (
	StateNoEnv     = 0 // TWAIN 环境没初始化
	StateDSMLoaded = 2 // DSM 已加载未打开
	StateDSMOpen   = 3 // DSM 已打开，可以枚举设备
	StateDSOpen    = 4 // 已打开某台扫描仪
	StateDSEnabled = 5 // 数据源已启用，准备传输
)

// TWAIN 是严格的单线程 + 消息泵模型：
//   - DLL 内部的 EnableDS() 会在调用线程上跑 GetMessage 循环等待数据源事件
//   - 数据源把事件投递到"打开它的那条线程"的消息队列
//
// 所以进程内所有 TWAIN 调用必须固定在同一条 OS 线程上串行执行。
// 这里用一条专属 worker goroutine（LockOSThread 后永不解锁）+ 任务 channel 实现。
var twainTasks = make(chan func())

// 状态镜像。
//
// 为什么要在 Go 这边留一份：扫描期间 TWAIN 线程被占满，任何走 inTwain 的调用
// 都得排队等扫描结束。而 /api/status 恰恰是扫描时最需要能立刻回答的接口。
// 所以每次 TWAIN 操作结束时把真实状态刷进这份镜像，查询只读镜像、不进队列。
var (
	stateMu     sync.RWMutex
	mirrorState int
	mirrorDev   string
	mirrorBusy  bool
	// TODO(TODO-settings.md #7): 换设备 / 断开 / 重连都不清空。
	lastApplied = map[string]string{} // 本服务设置成功过的能力，供 GET /api/config 回显
)

// startTwainThread 启动 TWAIN 专属线程并完成初始化，阻塞到初始化结束。
func startTwainThread() {
	ready := make(chan struct{})
	go func() {
		// 绑定当前 goroutine 到独占的 OS 线程，且永不 UnlockOSThread。
		// 该 goroutine 不退出，这条线程就一直属于 TWAIN。
		runtime.LockOSThread()
		C.zhx_Init()
		syncStateFromDLL()
		close(ready)
		for fn := range twainTasks {
			fn()
		}
	}()
	<-ready
}

// inTwain 把 fn 投递到 TWAIN 线程执行并等待其完成。
// 所有请求天然串行化，扫描期间的其他请求会排队。
func inTwain(fn func()) {
	done := make(chan struct{})
	twainTasks <- func() {
		defer close(done)
		fn()
		syncStateFromDLL()
	}
	<-done
}

// syncStateFromDLL 把 DLL 里的真实状态刷进镜像，**只能在 TWAIN 线程上调用**。
func syncStateFromDLL() {
	state := int(C.zhx_GetState())
	device := C.GoString(C.zhx_GetCurrentDevice())

	stateMu.Lock()
	mirrorState = state
	mirrorDev = device
	stateMu.Unlock()
}

func setBusy(busy bool) {
	stateMu.Lock()
	mirrorBusy = busy
	stateMu.Unlock()
}

// isBusy 报告当前是不是正在扫描。TwainScan 一进门就置位，所以它涵盖了
// "还排在 TWAIN 线程队列里等着"的那段时间，不只是真正在走纸的时候。
func isBusy() bool {
	stateMu.Lock()
	defer stateMu.Unlock()
	return mirrorBusy
}

// ---- 状态 ----

// ScannerStatus 是 /api/status 的响应体。
type ScannerStatus struct {
	State     int    `json:"state"`
	StateText string `json:"stateText"`
	Ready     bool   `json:"ready"`     // TWAIN 环境可用（DSM 已连接），能枚举设备
	Connected bool   `json:"connected"` // 已打开某台扫描仪
	Device    string `json:"device"`
	Scanning  bool   `json:"scanning"`
}

func stateText(state int) string {
	switch {
	case state == StateNoEnv:
		return "TWAIN 环境未初始化"
	case state == StateDSMLoaded:
		return "DSM 已加载，未打开"
	case state == StateDSMOpen:
		return "DSM 已打开，未连接扫描仪"
	case state == StateDSOpen:
		return "已连接扫描仪"
	case state >= StateDSEnabled:
		return "扫描进行中"
	default:
		return fmt.Sprintf("未知状态 %d", state)
	}
}

// TwainStatus 读状态镜像，不进 TWAIN 队列，扫描期间也能立刻返回。
func TwainStatus() ScannerStatus {
	stateMu.RLock()
	state, device, busy := mirrorState, mirrorDev, mirrorBusy
	stateMu.RUnlock()

	return ScannerStatus{
		State:     state,
		StateText: stateText(state),
		Ready:     state >= StateDSMOpen,
		Connected: state >= StateDSOpen,
		Device:    device,
		Scanning:  busy,
	}
}

// ---- 扫描回调收集 ----

var (
	scanMu      sync.Mutex
	scanResults []string
	// 扫描进度钩子。扫描全程独占 TWAIN 线程，同一时刻只可能有一个扫描在跑，
	// 所以一个全局钩子就够；由 TwainScan 在扫描前设、扫描后清。
	scanProgress func(page int, path string)
)

// onScannedFile 由 goScanCallback 调用（运行在 TWAIN 线程上，zhx_Scan 内部同步回调）。
// 回调里只做登记和一次非阻塞推送，不做重活，更不能在这里写 HTTP 响应——
// 这条线程正被 DLL 的扫描流程占着，卡在这里就是卡住扫描本身。
func onScannedFile(path string) {
	scanMu.Lock()
	scanResults = append(scanResults, path)
	page := len(scanResults)
	notify := scanProgress
	scanMu.Unlock()

	if notify != nil {
		notify(page, path)
	}
}

// ---- 设备枚举 ----

// TwainDevices 返回可用扫描仪名称列表。
func TwainDevices() []string {
	var out []string
	inTwain(func() { out = devicesOnTwainThread() })
	return out
}

// devicesOnTwainThread 直接调 DLL 枚举设备，**必须已经在 TWAIN 线程上**执行
// （从 inTwain 的 fn 里调）。在别处调会破坏 TWAIN 的单线程约束，
// 从 TwainDevices 之外的地方套 inTwain 还会自己把自己锁死。
func devicesOnTwainThread() []string {
	var raw string
	// 注意：DLL 用 _strdup 分配，且 DLL 静态链接了自己的 CRT，
	// 在 Go 这侧 C.free 会跨堆释放导致崩溃，所以这里不释放（每次调用泄漏一小段）。
	// 后续应在 C 侧补一个 zhx_FreeString 导出再改成显式释放。
	if p := C.zhx_GetDevicesList(); p != nil {
		raw = C.GoString(p)
	}

	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" {
		return []string{}
	}
	out := make([]string, 0, 4)
	for _, name := range strings.Split(raw, ";") {
		if name = strings.TrimSpace(name); name != "" {
			out = append(out, name)
		}
	}
	return out
}

// ---- 连接 / 断开 / 重连 ----

// TwainConnect 打开指定扫描仪并**保持连接**，直到显式断开。
// 已经连着别的设备时先断开——TWAIN 一次只允许打开一个数据源。
func TwainConnect(device string) error {
	if strings.TrimSpace(device) == "" {
		return errors.New("device 不能为空")
	}

	var err error
	inTwain(func() { err = connectOnTwainThread(device) })
	return err
}

func connectOnTwainThread(device string) error {
	current := C.GoString(C.zhx_GetCurrentDevice())
	if int(C.zhx_GetState()) >= StateDSOpen {
		if current == device {
			return nil // 已经连着这台了，幂等
		}
		// 切换设备必须先关掉当前这台，TWAIN 不允许同时打开两个数据源。
		C.zhx_CloseDevice()
	}

	cDevice := C.CString(device)
	defer C.free(unsafe.Pointer(cDevice))

	if C.zhx_OpenDevice(cDevice) != 0 {
		return nil
	}

	// 打开失败分两类，处理方式正好相反：
	//   1) DSM 掉线（扫描仪拔插、旧版 DLL 的 zhx_CloseDevice 顺带断了 DSM）
	//      —— 环境已经废了，必须 zhx_Init 重连
	//   2) 环境好好的，就是这台设备打不开（被别的程序占用、驱动报错）
	//      —— 这时 zhx_Init 是帮倒忙：它 delete/new 整个 TwainApp，换一个新的
	//         app identity 再去敲同一台设备，而 DS 侧上一条连接未必已经释放，
	//         结果就是一路"扫描仪繁忙"，还把日志搅乱。
	// 用"还枚举得到设备吗"来区分这两类。
	if len(devicesOnTwainThread()) > 0 {
		return fmt.Errorf("打开扫描仪失败: %s（按这个顺序查：1. 设备是否被其他程序占用——"+
			"厂商扫描工具、上一个没退干净的进程，实测占用时报的也是 TWCC_CHECKDEVICEONLINE；"+
			"2. 选的型号是否就是当前接着的那台，设备列表来自已安装的驱动，不代表设备在线；"+
			"3. 设备是否开机、连线。condition code 见 twain.log）", device)
	}

	C.zhx_Init()
	if C.zhx_OpenDevice(cDevice) != 0 {
		return nil
	}
	return fmt.Errorf("打开扫描仪失败: %s（TWAIN 环境已重连仍打不开，"+
		"确认设备已连接、驱动已安装；详见 twain.log）", device)
}

// TwainDisconnect 关闭当前扫描仪，回到 state 3（DSM 仍连着，还能枚举、还能开别的设备）。
func TwainDisconnect() {
	inTwain(func() {
		if int(C.zhx_GetState()) >= StateDSOpen {
			C.zhx_CloseDevice()
		}
	})
}

// TwainReconnect 重连当前设备：先关掉再打开。
// deep = true 时连整个 TWAIN 环境一起重建（zhx_Exit + zhx_Init），
// 用于设备拔插、驱动崩了这种 DSM 层面也不干净的情况。
func TwainReconnect(deep bool) (string, error) {
	var device string
	var err error

	inTwain(func() {
		device = C.GoString(C.zhx_GetCurrentDevice())
		if device == "" {
			err = errors.New("当前没有连接任何扫描仪，无法重连（先调 /api/connect）")
			return
		}

		if int(C.zhx_GetState()) >= StateDSOpen {
			C.zhx_CloseDevice()
		}
		if deep {
			C.zhx_Exit()
			C.zhx_Init()
		}
		err = connectOnTwainThread(device)
	})

	return device, err
}

// ---- 扫描参数 ----

// ScanConfig 是一批可选的扫描参数，字段用指针以便区分"没传"和"传了零值"。
type ScanConfig struct {
	Resolution *int  `json:"resolution,omitempty"` // DPI
	PixelType  *int  `json:"pixelType,omitempty"`  // 0=黑白 1=灰度 2=彩色
	Feeder     *bool `json:"feeder,omitempty"`     // 用送纸器(ADF)还是平板
	AutoFeed   *bool `json:"autoFeed,omitempty"`   // 自动进纸，连续扫描要开
	Duplex     *bool `json:"duplex,omitempty"`     // 双面
	PaperSize  *int  `json:"paperSize,omitempty"`  // ICAP_SUPPORTEDSIZES 的取值
	Brightness *int  `json:"brightness,omitempty"`
	Contrast   *int  `json:"contrast,omitempty"`
}

// ConfigResult 是单项参数的设置结果。逐项返回而不是一失败就整体报错——
// 扫描仪支持哪些能力千差万别，一项不支持不该拖累其它项。
type ConfigResult struct {
	Field string `json:"field"`
	Cap   string `json:"cap"`
	Value string `json:"value"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// capErrorText 把 zhx_SetCapability_STR 的返回码翻译成人话。
func capErrorText(code int) string {
	switch code {
	case 0:
		return ""
	case -1:
		return "TWAIN 环境未初始化"
	case -2:
		return "未连接扫描仪"
	case -3:
		return "DLL 不认识这个能力名"
	case -4:
		return "扫描仪不支持该能力（读当前值失败）"
	case -5, -6:
		return "DSM 内存分配失败"
	case -7:
		return "FRAME 类型不支持用字符串设置"
	case -8:
		return "字符串类型的能力不支持这样设置"
	case -9:
		return "该能力的数据类型不支持"
	case -10:
		return "扫描仪拒绝了这个取值"
	default:
		return fmt.Sprintf("未知错误码 %d", code)
	}
}

// setCapOnTwainThread 设一项能力，**必须在 TWAIN 线程上**调用。
func setCapOnTwainThread(field, cap, value string) ConfigResult {
	res := ConfigResult{Field: field, Cap: cap, Value: value}

	cCap := C.CString(cap)
	defer C.free(unsafe.Pointer(cCap))
	cVal := C.CString(value)
	defer C.free(unsafe.Pointer(cVal))

	code := int(C.zhx_SetCapability_STR(cCap, cVal))
	if code == 0 {
		res.OK = true
		stateMu.Lock()
		lastApplied[field] = value
		stateMu.Unlock()
		return res
	}
	res.Error = capErrorText(code)
	return res
}

func boolCapValue(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// TwainApplyConfig 逐项下发扫描参数，返回每一项的结果。
// 必须先连上扫描仪：TWAIN 的能力协商只在 state 4 有效。
func TwainApplyConfig(cfg ScanConfig) ([]ConfigResult, error) {
	var results []ConfigResult
	var err error

	inTwain(func() {
		if int(C.zhx_GetState()) < StateDSOpen {
			err = errors.New("尚未连接扫描仪，先调 /api/connect")
			return
		}

		// 分辨率走 DLL 里的专用函数：它会同时设 X/Y 并读回校验，
		// 比裸设 ICAP_XRESOLUTION 可靠（不少设备只认成对设置）。
		if cfg.Resolution != nil {
			dpi := *cfg.Resolution
			res := ConfigResult{Field: "resolution", Cap: "ICAP_XRESOLUTION+ICAP_YRESOLUTION",
				Value: fmt.Sprintf("%d", dpi)}
			if C.zhx_SetResolution(C.int(dpi)) == 1 {
				res.OK = true
				stateMu.Lock()
				lastApplied["resolution"] = res.Value
				stateMu.Unlock()
			} else {
				// DLL 里设完会读回校验，不等值就算失败。多数扫描仪只支持固定几档 DPI，
				// 支持哪些看 /api/capability?name=ICAP_XRESOLUTION。
				// TODO(TODO-settings.md #11): 文案里的 ?name=ICAP_XRESOLUTION 实际读不出来，应为 0x1118。
				res.Error = "扫描仪未接受这个 DPI（多数设备只支持固定档位，可用 /api/capability?name=ICAP_XRESOLUTION 查）"
			}
			results = append(results, res)
		}

		if cfg.PixelType != nil {
			results = append(results, setCapOnTwainThread("pixelType", "ICAP_PIXELTYPE",
				fmt.Sprintf("%d", *cfg.PixelType)))
		}
		if cfg.Feeder != nil {
			results = append(results, setCapOnTwainThread("feeder", "CAP_FEEDERENABLED",
				boolCapValue(*cfg.Feeder)))
		}
		if cfg.AutoFeed != nil {
			results = append(results, setCapOnTwainThread("autoFeed", "CAP_AUTOFEED",
				boolCapValue(*cfg.AutoFeed)))
		}
		if cfg.Duplex != nil {
			results = append(results, setCapOnTwainThread("duplex", "CAP_DUPLEXENABLED",
				boolCapValue(*cfg.Duplex)))
		}
		if cfg.PaperSize != nil {
			results = append(results, setCapOnTwainThread("paperSize", "ICAP_SUPPORTEDSIZES",
				fmt.Sprintf("%d", *cfg.PaperSize)))
		}
		if cfg.Brightness != nil {
			results = append(results, setCapOnTwainThread("brightness", "ICAP_BRIGHTNESS",
				fmt.Sprintf("%d", *cfg.Brightness)))
		}
		if cfg.Contrast != nil {
			results = append(results, setCapOnTwainThread("contrast", "ICAP_CONTRAST",
				fmt.Sprintf("%d", *cfg.Contrast)))
		}
	})

	return results, err
}

// TwainCurrentConfig 返回当前分辨率和本服务设置过的参数。
//
// 只实时读分辨率一项，其余靠回显：DLL 的 zhx_GetCapability_STR 为了读能力会
// 临时把数据源 enable 到 state 5，有些设备会因此空走一次纸或者亮灯，
// 不适合放在一个随手可调的 GET 上。要读原始能力用 /api/capability，那里有明确说明。
func TwainCurrentConfig() (map[string]any, error) {
	out := map[string]any{}
	var err error

	inTwain(func() {
		if int(C.zhx_GetState()) < StateDSOpen {
			err = errors.New("尚未连接扫描仪，先调 /api/connect")
			return
		}
		if dpi := int(C.zhx_GetCurrentResolution()); dpi > 0 {
			out["resolution"] = dpi
		}
	})
	if err != nil {
		return nil, err
	}

	stateMu.RLock()
	applied := make(map[string]string, len(lastApplied))
	for k, v := range lastApplied {
		applied[k] = v
	}
	stateMu.RUnlock()

	out["applied"] = applied
	return out, nil
}

// TwainCapability 读一项能力的原始信息（容器类型、当前值、可选值列表）。
func TwainCapability(name string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", errors.New("name 不能为空")
	}

	var raw string
	var err error
	inTwain(func() {
		if int(C.zhx_GetState()) < StateDSOpen {
			err = errors.New("尚未连接扫描仪，先调 /api/connect")
			return
		}
		cName := C.CString(name)
		defer C.free(unsafe.Pointer(cName))
		// 返回的是 DLL 内部静态缓冲区，只读、不释放。
		raw = C.GoString(C.zhx_GetCapability_STR(cName))
	})
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(raw) == "" {
		raw = "[]"
	}
	return raw, nil
}

// TODO(TODO-settings.md #1 #3): DLL 读取侧的名字表、缓冲区越界问题。
// TwainReadCapabilities 在一次 TWAIN 任务里连续读多项能力，返回 编号 -> DLL 原始 JSON。
// 读不到的项是 "[]"。编号按十进制传给 DLL：它读取侧的名字表残缺且有错，只有编号靠得住。
func TwainReadCapabilities(codes []uint16) (map[uint16]string, error) {
	var out map[uint16]string
	var err error
	inTwain(func() {
		if int(C.zhx_GetState()) < StateDSOpen {
			err = errors.New("尚未连接扫描仪，先调 /api/connect")
			return
		}
		out = readCapsOnTwainThread(codes)
	})
	return out, err
}

// readCapsOnTwainThread 逐项读能力，**必须在 TWAIN 线程上**调用。
func readCapsOnTwainThread(codes []uint16) map[uint16]string {
	out := make(map[uint16]string, len(codes))
	for _, code := range codes {
		cName := C.CString(fmt.Sprintf("%d", code))
		// 返回的是 DLL 内部静态缓冲区，下一次调用就会被覆盖，所以立刻拷成 Go 字符串。
		out[code] = C.GoString(C.zhx_GetCapability_STR(cName))
		C.free(unsafe.Pointer(cName))
	}
	return out
}

// currentDeviceOnTwainThread 返回已打开的设备名，没打开返回空串。**必须在 TWAIN 线程上**调用。
func currentDeviceOnTwainThread() string {
	if int(C.zhx_GetState()) < StateDSOpen {
		return ""
	}
	return C.GoString(C.zhx_GetCurrentDevice())
}

// TwainSetCapability 直接设一项 TWAIN 能力，给 ScanConfig 没覆盖到的场景用。
// name 可以是能力名（ICAP_PIXELTYPE）也可以是编号（0x0101 或 257）。
func TwainSetCapability(name, value string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("name 不能为空")
	}

	var res ConfigResult
	var err error
	inTwain(func() {
		if int(C.zhx_GetState()) < StateDSOpen {
			err = errors.New("尚未连接扫描仪，先调 /api/connect")
			return
		}
		res = setCapOnTwainThread(name, name, value)
	})
	if err != nil {
		return err
	}
	if !res.OK {
		return fmt.Errorf("设置 %s = %s 失败: %s", name, value, res.Error)
	}
	return nil
}

// ---- 驱动自带的设置面板 ----

// TwainShowSettingUI 打开扫描仪驱动自带的设置面板，阻塞到用户关闭它。
//
// 走的是 MSG_ENABLEDSUIONLY：只显示界面让数据源自己保存参数，不传输图像。
// 面板开着的这段时间 TWAIN 线程被完全占住，其它请求都会排队——和扫描时一样，
// 只有 /api/status 不受影响（读状态镜像，不进队列）。
// 用户不关面板就会一直等下去，这里不设超时：强行关掉正在调参数的面板更糟。
func TwainShowSettingUI() error {
	var err error

	setBusy(true)
	defer setBusy(false)

	inTwain(func() {
		if int(C.zhx_GetState()) < StateDSOpen {
			err = errors.New("尚未连接扫描仪，先调 /api/connect")
			return
		}
		if C.zhx_ShowSettingUI() == 0 {
			err = errors.New("打开扫描仪设置面板失败（可能是驱动不提供设置界面，详见 twain.log）")
		}
	})

	return err
}

// ---- 扫描 ----

// TwainScan 扫描 count 页并返回落盘的文件绝对路径。
// count = 0 表示走送纸器一直扫到没纸。
// device 非空时会先确保连上这台设备；为空则用当前已连接的设备。
//
// 扫描结束后**保持连接**，可以接着扫下一批——这正是 zhx_EndScan 不能在这里调的原因，
// 它内部会 unloadDS() 把设备关掉，回到 state 3。
// progress 可以为 nil；不为 nil 时每扫出一张图调一次，调用发生在 TWAIN 线程上，
// 实现必须是非阻塞的（比如往 channel 里塞一条），否则会拖慢扫描。
func TwainScan(device, dir string, count int, progress func(page int, path string)) ([]string, error) {
	var files []string
	var opErr error

	setBusy(true)
	defer setBusy(false)

	inTwain(func() {
		if device != "" {
			if opErr = connectOnTwainThread(device); opErr != nil {
				return
			}
		}
		if int(C.zhx_GetState()) < StateDSOpen {
			opErr = errors.New("尚未连接扫描仪，先调 /api/connect 或在请求里带上 device")
			return
		}

		scanMu.Lock()
		scanResults = nil
		scanProgress = progress
		scanMu.Unlock()

		defer func() {
			scanMu.Lock()
			scanProgress = nil
			scanMu.Unlock()
		}()

		cDir := C.CString(dir)
		defer C.free(unsafe.Pointer(cDir))

		pages := int(C.scanWithGoCallback(cDir, C.int(count)))

		scanMu.Lock()
		files = append([]string(nil), scanResults...)
		scanResults = nil
		scanMu.Unlock()

		if pages == 0 && len(files) == 0 {
			opErr = scanFailureOnTwainThread()
		}
	})

	return files, opErr
}

// scanFailureOnTwainThread 在扫出 0 页之后，按 DLL 记下的诊断信息说清楚原因。
// **必须在 TWAIN 线程上**、紧跟着 zhx_Scan 调用。
//
// 判断顺序有讲究：设备离线 > 启用失败的 condition code > 送纸器没纸。
// 设备都不在线时 CAP_FEEDERLOADED 读出来的"没纸"是没有意义的。
func scanFailureOnTwainThread() error {
	var enableFailed, cc, feederLoaded, deviceOnline C.int
	C.zhx_GetScanDiagnosis(&enableFailed, &cc, &feederLoaded, &deviceOnline)

	return errors.New(scanFailureText(int(enableFailed) != 0, int(cc), int(feederLoaded), int(deviceOnline)))
}

// scanFailureText 是 scanFailureOnTwainThread 的纯函数部分，参数含义见 DLL 的 zhx_GetScanDiagnosis：
// cc 为 -1 表示没有 condition code；feederLoaded / deviceOnline 为 1 / 0，-1 表示设备不支持该能力。
func scanFailureText(enableFailed bool, cc, feederLoaded, deviceOnline int) string {
	if deviceOnline == 0 || cc == twccCheckDeviceOnline {
		return "扫描失败：扫描仪未连接或未开机（请检查电源、USB 线，或是否被其他扫描程序占用）"
	}

	if enableFailed {
		switch cc {
		case twccNoMedia:
			return "扫描失败：送纸器里没有纸，请放纸后重试"
		case twccPaperJam:
			return "扫描失败：卡纸，请清除卡纸后重试"
		case twccPaperDoubleFeed:
			return "扫描失败：检测到重张进纸，请整理纸张后重试"
		case twccInterlock:
			return "扫描失败：扫描仪盖板或送纸器未合上"
		case twccSeqError:
			// 数据源停在了错误的状态（常见于上一次启用没有正常关闭），重连能恢复。
			return "扫描失败：扫描仪状态异常（数据源未正常复位），请断开重连扫描仪后重试"
		case twccDenied, twccMaxConnections:
			return "扫描失败：扫描仪被其他程序占用，请关闭厂商扫描工具等程序后重试"
		}
		// 驱动在启用阶段就拒绝了，但没给出能识别的原因。没纸也可能走到这里，一并提示。
		if feederLoaded == 0 {
			return "扫描失败：送纸器里没有纸，请放纸后重试"
		}
		if cc >= 0 {
			return fmt.Sprintf("扫描失败：扫描仪启动失败（TWAIN condition code %d），详见 twain.log", cc)
		}
		return "扫描失败：扫描仪启动失败，驱动未返回原因，详见 twain.log"
	}

	// 启用成功但没有图：多数是没纸，驱动直接结束了本次传输。
	if feederLoaded == 0 {
		return "扫描失败：送纸器里没有纸，请放纸后重试"
	}
	return "扫描未产出任何图片（检查是否放纸、盖板是否合上，详见 twain.log）"
}

// TWAIN condition code，取值见 twain.h 的 TWCC_*。
const (
	twccMaxConnections    = 4
	twccSeqError          = 11
	twccDenied            = 16
	twccPaperJam          = 20
	twccPaperDoubleFeed   = 21
	twccCheckDeviceOnline = 23
	twccInterlock         = 24
	twccNoMedia           = 29
)

// TwainExit 释放 TWAIN 环境，进程退出前调用。
func TwainExit() {
	inTwain(func() { C.zhx_Exit() })
}

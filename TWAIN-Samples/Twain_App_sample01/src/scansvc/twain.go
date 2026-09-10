package main

/*
#cgo LDFLAGS: -L${SRCDIR} -lTWAIN_APP_CMD64 -lstdc++

#include <stdlib.h>

typedef int (*ScanCallback)(char *filename);

void  zhx_Init(void);
char* zhx_GetDevicesList(void);
int   zhx_OpenDevice(char *device);
int   zhx_Scan(char *path, ScanCallback cb, int count);
void  zhx_EndScan(void);
void  zhx_CloseDevice(void);
void  zhx_Exit(void);

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

// TWAIN 是严格的单线程 + 消息泵模型：
//   - DLL 内部的 EnableDS() 会在调用线程上跑 GetMessage 循环等待数据源事件
//   - 数据源把事件投递到"打开它的那条线程"的消息队列
// 所以进程内所有 TWAIN 调用必须固定在同一条 OS 线程上串行执行。
// 这里用一条专属 worker goroutine（LockOSThread 后永不解锁）+ 任务 channel 实现。
var twainTasks = make(chan func())

// startTwainThread 启动 TWAIN 专属线程并完成初始化，阻塞到初始化结束。
func startTwainThread() {
	ready := make(chan struct{})
	go func() {
		// 绑定当前 goroutine 到独占的 OS 线程，且永不 UnlockOSThread。
		// 该 goroutine 不退出，这条线程就一直属于 TWAIN。
		runtime.LockOSThread()
		C.zhx_Init()
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
	}
	<-done
}

// ---- 扫描回调收集 ----

var (
	scanMu      sync.Mutex
	scanResults []string
)

// onScannedFile 由 goScanCallback 调用（运行在 TWAIN 线程上，zhx_Scan 内部同步回调）。
// 回调里只做登记，不做重活，更不能在这里写 HTTP 响应。
func onScannedFile(path string) {
	scanMu.Lock()
	defer scanMu.Unlock()
	scanResults = append(scanResults, path)
}

// ---- 对外的三个操作 ----

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

// TwainScan 打开指定设备扫描 count 页，返回落盘的文件绝对路径。
// count = 0 表示不限页数（走 ADF 一直扫到没纸）。
func TwainScan(device, dir string, count int) ([]string, error) {
	if strings.TrimSpace(device) == "" {
		return nil, errors.New("device 不能为空")
	}

	var files []string
	var opErr error

	inTwain(func() {
		scanMu.Lock()
		scanResults = nil
		scanMu.Unlock()

		cDevice := C.CString(device)
		defer C.free(unsafe.Pointer(cDevice))

		if C.zhx_OpenDevice(cDevice) == 0 {
			// 打开失败分两类，处理方式正好相反：
			//   1) DSM 掉线（扫描仪拔插、旧版 DLL 的 zhx_CloseDevice 顺带断了 DSM）
			//      —— 环境已经废了，必须 zhx_Init 重连
			//   2) 环境好好的，就是这台设备打不开（被别的程序占用、驱动报错）
			//      —— 这时 zhx_Init 是帮倒忙：它 delete/new 整个 TwainApp，换一个新的
			//         app identity 再去敲同一台设备，而 DS 侧上一条连接未必已经释放，
			//         结果就是一路"扫描仪繁忙"，还把日志搅乱。
			// 用"还枚举得到设备吗"来区分这两类。
			if len(devicesOnTwainThread()) > 0 {
				opErr = fmt.Errorf("打开扫描仪失败: %s（设备可能被其他程序占用、离线或驱动异常；"+
					"具体原因看 twain.log 里的 condition code）", device)
				return
			}

			C.zhx_Init()
			if C.zhx_OpenDevice(cDevice) == 0 {
				opErr = fmt.Errorf("打开扫描仪失败: %s（TWAIN 环境已重连仍打不开，"+
					"确认设备已连接、驱动已安装；详见 twain.log）", device)
				return
			}
		}
		defer C.zhx_CloseDevice()

		cDir := C.CString(dir)
		defer C.free(unsafe.Pointer(cDir))

		pages := int(C.scanWithGoCallback(cDir, C.int(count)))
		C.zhx_EndScan()

		scanMu.Lock()
		files = append([]string(nil), scanResults...)
		scanResults = nil
		scanMu.Unlock()

		if pages == 0 && len(files) == 0 {
			opErr = errors.New("扫描未产出任何图片（检查是否放纸、盖板是否合上、日志 twain.log）")
		}
	})

	return files, opErr
}

// TwainExit 释放 TWAIN 环境，进程退出前调用。
func TwainExit() {
	inTwain(func() { C.zhx_Exit() })
}

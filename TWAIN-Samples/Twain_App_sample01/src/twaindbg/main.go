// twaindbg —— TWAIN DLL 交互式调试工具
//
// 思路承自 CMD-twain 分支的 gotwain/main.go：先用命令行把 DLL 逐个接口打通，
// 再谈上层服务。区别是这里把 20 个导出全接上了，每步都打印返回值和耗时，
// 出问题能定位到具体是哪个 TWAIN 调用挂的。
//
// 用法：
//	twaindbg                              进入交互式命令行
//	twaindbg -run "init;list;open 0;scan . 1;close;exit"   一次性执行
//	twaindbg -auto                        一键跑通完整流程
package main

/*
#cgo LDFLAGS: -L${SRCDIR} -lTWAIN_APP_CMD64 -lstdc++

#include <stdlib.h>

typedef int (*ScanCallback)(char *filename);

void  zhx_twain_test(void);
void  zhx_twain(void);
void  zhx_Init(void);
char* zhx_GetDevicesList(void);
int   zhx_OpenDevice(char *device);
int   zhx_Scan(char *path, ScanCallback cb, int count);
void  zhx_EndScan(void);
void  zhx_CloseDevice(void);
void  zhx_Exit(void);
int   zhx_SetTransferMechanism(int mechanism);
int   zhx_SetImageFileFormat(int format);
int   zhx_GetCurrentFileFormat(void);
char* zhx_GetSupportedFileFormats(void);
int   zhx_SetResolution(int dpi);
char* zhx_GetSupportedResolutions(void);
int   zhx_GetCurrentResolution(void);
char* zhx_GetDevCapability_JSON(char *device);
char* zhx_GetDevCapability_STR(char *device);
char* zhx_GetCapability_STR(char *capOrDevice);
int   zhx_SetCapability_STR(char *nCap, char *value);

extern int goDbgScanCallback(char *filename);

static int dbgScan(char *path, int count) {
    return zhx_Scan(path, (ScanCallback)goDbgScanCallback, count);
}
*/
import "C"

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unsafe"
)

var (
	runScript = flag.String("run", "", "一次性执行的命令，用分号分隔")
	autoRun   = flag.Bool("auto", false, "一键跑通 init→list→open 0→scan→close→exit")
	scanDir   = flag.String("dir", ".", "-auto 模式下的图片保存目录")
)

// 会话状态，方便命令之间接力
var (
	devices []string // 最近一次 list 的结果
	current string   // 当前打开的设备名
	scanned []string // 回调收集到的文件，由 callback.go 追加
)

func logf(format string, a ...any) {
	fmt.Printf(format+"\n", a...)
}

// cstr 把 Go 字符串转成 C 字符串，调用方负责 free。
func cstr(s string) (*C.char, func()) {
	p := C.CString(s)
	return p, func() { C.free(unsafe.Pointer(p)) }
}

// gostr 读取 DLL 返回的 C 字符串。
//
// 注意：DLL 用 _strdup / 静态 buffer 返回，且自己静态链接了 CRT，
// 在 Go 这侧 C.free 会跨堆释放导致崩溃，所以这里只读不释放。
// 每次调用会泄漏一小段内存，调试工具可以接受；正式服务应在 C 侧补 zhx_FreeString。
func gostr(p *C.char) string {
	if p == nil {
		return ""
	}
	return C.GoString(p)
}

// timed 执行 fn 并打印耗时，扫描这类慢操作能一眼看出卡在哪。
func timed(name string, fn func()) {
	start := time.Now()
	fn()
	logf("  [%s] 耗时 %v", name, time.Since(start).Round(time.Millisecond))
}

func main() {
	// TWAIN 是严格的单线程模型：DLL 内部 EnableDS() 会在调用线程上跑 GetMessage
	// 消息泵，数据源也只往"打开它的那条线程"投递事件。
	// 这里把 main goroutine 钉死在主线程上，所有调用顺序执行。
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	flag.Parse()

	logf("TWAIN DLL 调试工具  (输入 help 查看命令)")
	logf("======================================")

	switch {
	case *autoRun:
		runCommands([]string{"test", "init", "list", "open 0", "scan " + *scanDir + " 1", "close", "exit"})
	case *runScript != "":
		runCommands(strings.Split(*runScript, ";"))
	default:
		repl()
	}
}

func repl() {
	sc := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("twain> ")
		if !sc.Scan() {
			return
		}
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if line == "q" || line == "quit" {
			logf("(未调用 zhx_Exit 直接退出，如需正常释放请先执行 exit)")
			return
		}
		if !dispatch(line) {
			return
		}
	}
}

func runCommands(cmds []string) {
	for _, c := range cmds {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		logf("\n$ %s", c)
		if !dispatch(c) {
			return
		}
	}
}

// dispatch 执行一条命令，返回 false 表示应当结束会话。
func dispatch(line string) bool {
	fields := strings.Fields(line)
	cmd := fields[0]
	args := fields[1:]

	switch cmd {
	case "help", "?":
		printHelp()

	case "test":
		logf("→ zhx_twain_test()  (DLL 里会往标准输出打印一句话)")
		C.zhx_twain_test()

	case "oneshot":
		logf("→ zhx_twain()  一把梭流程，设备号写死在 C++ 里，仅用于对照老版本")
		timed("zhx_twain", func() { C.zhx_twain() })

	case "init":
		logf("→ zhx_Init()")
		timed("zhx_Init", func() { C.zhx_Init() })
		// zhx_Init 是 void 返回，拿不到成败，只能靠"能不能枚举出设备"反推
		if probe := parseDevices(gostr(C.zhx_GetDevicesList())); len(probe) == 0 {
			logf("  ⚠ 初始化后枚举不到设备。按可能性排序：")
			logf("     1) 找不到 TWAINDSM.dll —— 必须在 exe 同目录/系统目录/PATH 里")
			logf("        (装在 C:\\Windows\\twain_64\\ 下不在默认搜索路径，需拷到 exe 旁边)")
			logf("     2) 扫描仪驱动未装，或设备未连接/未开机")
			logf("     3) 设备只有 32 位数据源，本工具是 64 位，加载不了")
		} else {
			devices = probe
			logf("  就绪，发现 %d 台设备（已缓存，可直接 open <序号>）:", len(probe))
			for i, d := range probe {
				logf("    [%d] %s", i, d)
			}
		}

	case "list":
		logf("→ zhx_GetDevicesList()")
		var raw string
		timed("zhx_GetDevicesList", func() { raw = gostr(C.zhx_GetDevicesList()) })
		logf("  原始返回: %q", raw)
		devices = parseDevices(raw)
		if len(devices) == 0 {
			logf("  未找到设备。排查顺序：驱动装了吗 / 设备开机了吗 / 是不是 32 位 DS（本工具是 64 位）")
			return true
		}
		for i, d := range devices {
			logf("  [%d] %s", i, d)
		}

	case "open":
		name, ok := resolveDevice(args)
		if !ok {
			return true
		}
		logf("→ zhx_OpenDevice(%q)", name)
		var rc C.int
		p, free := cstr(name)
		timed("zhx_OpenDevice", func() { rc = C.zhx_OpenDevice(p) })
		free()
		if rc == 0 {
			logf("  失败(返回 0)。常见原因：设备被其它程序占用 / 名称不对 / DSM 状态不足")
			return true
		}
		current = name
		logf("  成功，当前设备: %s", current)

	case "close":
		logf("→ zhx_CloseDevice()")
		timed("zhx_CloseDevice", func() { C.zhx_CloseDevice() })
		current = ""

	case "exit":
		logf("→ zhx_Exit()")
		timed("zhx_Exit", func() { C.zhx_Exit() })
		logf("会话结束")
		return false

	case "scan":
		dir := "."
		count := 1
		if len(args) >= 1 {
			dir = args[0]
		}
		if len(args) >= 2 {
			n, err := strconv.Atoi(args[1])
			if err != nil {
				logf("  页数不是数字: %s", args[1])
				return true
			}
			count = n
		}
		if current == "" {
			logf("  还没有打开设备，先执行 open")
			return true
		}
		abs, _ := os.Getwd()
		logf("→ zhx_Scan(path=%q, count=%d)   (工作目录 %s)", dir, count, abs)
		if count == 0 {
			logf("  count=0 表示走送纸器一直扫到没纸")
		}
		scanned = nil
		var pages C.int
		p, free := cstr(dir)
		timed("zhx_Scan", func() { pages = C.dbgScan(p, C.int(count)) })
		free()
		C.zhx_EndScan()
		logf("  返回页数: %d，回调收到 %d 个文件", int(pages), len(scanned))
		for _, f := range scanned {
			logf("    %s", f)
		}
		if pages == 0 && len(scanned) == 0 {
			logf("  没出图。排查：放纸了吗 / 盖板合上了吗 / twain.log 里 EnableDS 之后卡在哪")
		}

	case "endscan":
		logf("→ zhx_EndScan()")
		C.zhx_EndScan()

	case "res":
		if len(args) == 0 {
			logf("→ zhx_GetCurrentResolution() / zhx_GetSupportedResolutions()")
			logf("  当前: %d dpi", int(C.zhx_GetCurrentResolution()))
			logf("  支持: %s", gostr(C.zhx_GetSupportedResolutions()))
			return true
		}
		dpi, err := strconv.Atoi(args[0])
		if err != nil {
			logf("  dpi 不是数字: %s", args[0])
			return true
		}
		logf("→ zhx_SetResolution(%d)", dpi)
		logf("  返回: %d", int(C.zhx_SetResolution(C.int(dpi))))

	case "fmt":
		if len(args) == 0 {
			logf("→ zhx_GetCurrentFileFormat() / zhx_GetSupportedFileFormats()")
			logf("  当前: %d", int(C.zhx_GetCurrentFileFormat()))
			logf("  支持: %s", gostr(C.zhx_GetSupportedFileFormats()))
			return true
		}
		f, err := strconv.Atoi(args[0])
		if err != nil {
			logf("  格式号不是数字: %s", args[0])
			return true
		}
		logf("→ zhx_SetImageFileFormat(%d)", f)
		logf("  返回: %d", int(C.zhx_SetImageFileFormat(C.int(f))))

	case "mech":
		if len(args) == 0 {
			logf("  用法: mech <传输方式号>")
			return true
		}
		m, err := strconv.Atoi(args[0])
		if err != nil {
			logf("  传输方式号不是数字: %s", args[0])
			return true
		}
		logf("→ zhx_SetTransferMechanism(%d)", m)
		logf("  返回: %d", int(C.zhx_SetTransferMechanism(C.int(m))))

	case "caps":
		name, ok := resolveDevice(args)
		if !ok {
			return true
		}
		logf("→ zhx_GetDevCapability_JSON(%q)", name)
		p, free := cstr(name)
		var out string
		timed("zhx_GetDevCapability_JSON", func() { out = gostr(C.zhx_GetDevCapability_JSON(p)) })
		free()
		logf("%s", out)

	case "capstr":
		name, ok := resolveDevice(args)
		if !ok {
			return true
		}
		logf("→ zhx_GetDevCapability_STR(%q)", name)
		p, free := cstr(name)
		out := gostr(C.zhx_GetDevCapability_STR(p))
		free()
		logf("%s", out)

	case "get":
		if len(args) == 0 {
			logf("  用法: get <能力名>   例: get ICAP_XRESOLUTION")
			return true
		}
		logf("→ zhx_GetCapability_STR(%q)", args[0])
		p, free := cstr(args[0])
		out := gostr(C.zhx_GetCapability_STR(p))
		free()
		logf("  %s", out)

	case "set":
		if len(args) < 2 {
			logf("  用法: set <能力名> <值>   例: set ICAP_XRESOLUTION 300")
			return true
		}
		logf("→ zhx_SetCapability_STR(%q, %q)", args[0], args[1])
		pc, freeC := cstr(args[0])
		pv, freeV := cstr(args[1])
		rc := int(C.zhx_SetCapability_STR(pc, pv))
		freeC()
		freeV()
		logf("  返回: %d", rc)

	case "state":
		logf("  当前设备: %q", current)
		logf("  设备列表: %v", devices)

	default:
		// 列完设备后直接敲序号是最符合直觉的写法，这里等价于 open <序号>
		if _, err := strconv.Atoi(cmd); err == nil && len(fields) == 1 {
			return dispatch("open " + cmd)
		}
		logf("  未知命令 %q，输入 help 查看", cmd)
	}
	return true
}

// resolveDevice 把参数解析成设备名：支持直接给名称，也支持给 list 里的序号。
// 不给参数时用当前已打开的设备。
func resolveDevice(args []string) (string, bool) {
	if len(args) == 0 {
		if current != "" {
			return current, true
		}
		logf("  需要指定设备（名称或 list 里的序号），或先 open")
		return "", false
	}
	arg := strings.Join(args, " ")
	if idx, err := strconv.Atoi(arg); err == nil {
		if idx < 0 || idx >= len(devices) {
			logf("  序号 %d 超范围，先执行 list（当前 %d 台）", idx, len(devices))
			return "", false
		}
		return devices[idx], true
	}
	return arg, true
}

// parseDevices 解析 zhx_GetDevicesList 的返回值。
// 正常返回是分号分隔的名称串 "扫描仪A;扫描仪B"，出错时 DLL 返回字面量 "[]"。
func parseDevices(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" {
		return nil
	}
	var out []string
	for _, n := range strings.Split(raw, ";") {
		if n = strings.TrimSpace(n); n != "" {
			out = append(out, n)
		}
	}
	return out
}

func printHelp() {
	fmt.Print(`
可用命令（按典型调试顺序排列）：

  test              zhx_twain_test        验证 DLL 能不能加载
  init              zhx_Init              初始化 TWAIN 环境、连接 DSM
  list              zhx_GetDevicesList    枚举扫描仪，结果带序号
  open <序号|名称>   zhx_OpenDevice        打开设备
  caps [序号|名称]   zhx_GetDevCapability_JSON   设备能力(JSON)
  capstr [序号|名称] zhx_GetDevCapability_STR    设备能力(文本)
  get <能力名>       zhx_GetCapability_STR       读单个能力，例 get ICAP_XRESOLUTION
  set <能力名> <值>  zhx_SetCapability_STR       写单个能力，例 set ICAP_XRESOLUTION 300
  res [dpi]         分辨率：不带参数读当前+支持列表，带参数设置
  fmt [格式号]       文件格式：同上
  mech <方式号>      zhx_SetTransferMechanism    传输方式
  scan <目录> [页数] zhx_Scan              扫描，页数 0 = 扫到没纸
  endscan           zhx_EndScan
  close             zhx_CloseDevice
  exit              zhx_Exit 并结束会话
  state             打印当前会话状态
  oneshot           zhx_twain             老版本的一把梭流程，用于对照
  q                 直接退出（不调 zhx_Exit）

`)
}

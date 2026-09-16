//go:build windows

package main

// 托盘图标（Windows）。
//
// 双击 scansvc.exe 就缩到右下角托盘、后台运行，右键菜单可以启动 / 停止 / 重启服务、
// 打开演示页和日志、退出。
//
// 想完全没有黑窗口，编译时要加 -ldflags "-H=windowsgui"（见 README）：
// 那样进程没有控制台，日志全靠 scansvc.log（logfile.go）。
// 不加也能用，只是会多一个控制台窗口，命令行调试时反而方便。
//
// 直接用 Win32 API，不引第三方托盘库：这个程序本来就只能在 Windows 上跑，
// 而且多一个依赖就多一份要跟着 Go 版本升级的东西。
//
// 线程：托盘窗口的消息循环必须固定在一条线程上（和 TWAIN 一样的要求，但不是同一条），
// 所以 runTray 里 LockOSThread 后跑 GetMessage 循环，一直到用户点"退出"。

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procRegisterClassEx     = user32.NewProc("RegisterClassExW")
	procCreateWindowEx      = user32.NewProc("CreateWindowExW")
	procDefWindowProc       = user32.NewProc("DefWindowProcW")
	procDestroyWindow       = user32.NewProc("DestroyWindow")
	procGetMessage          = user32.NewProc("GetMessageW")
	procTranslateMessage    = user32.NewProc("TranslateMessage")
	procDispatchMessage     = user32.NewProc("DispatchMessageW")
	procPostQuitMessage     = user32.NewProc("PostQuitMessage")
	procPostMessage         = user32.NewProc("PostMessageW")
	procLoadIcon            = user32.NewProc("LoadIconW")
	procLoadCursor          = user32.NewProc("LoadCursorW")
	procCreatePopupMenu     = user32.NewProc("CreatePopupMenu")
	procAppendMenu          = user32.NewProc("AppendMenuW")
	procTrackPopupMenu      = user32.NewProc("TrackPopupMenu")
	procDestroyMenu         = user32.NewProc("DestroyMenu")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procGetCursorPos        = user32.NewProc("GetCursorPos")
	procRegisterWindowMsg   = user32.NewProc("RegisterWindowMessageW")

	procShellNotifyIcon = shell32.NewProc("Shell_NotifyIconW")
	procShellExecute    = shell32.NewProc("ShellExecuteW")
	procExtractIcon     = shell32.NewProc("ExtractIconW")

	procGetModuleHandle       = kernel32.NewProc("GetModuleHandleW")
	procGetConsoleWindow      = kernel32.NewProc("GetConsoleWindow")
	procGetConsoleProcessList = kernel32.NewProc("GetConsoleProcessList")
	procShowWindow            = user32.NewProc("ShowWindow")
)

const (
	wmDestroy       = 0x0002
	wmClose         = 0x0010
	wmCommand       = 0x0111
	wmLButtonDblClk = 0x0203
	wmRButtonUp     = 0x0205
	wmTrayCallback  = 0x0400 + 1 // WM_USER+1，托盘图标的事件回调

	nimAdd    = 0x0000
	nimModify = 0x0001
	nimDelete = 0x0002

	nifMessage = 0x0001
	nifIcon    = 0x0002
	nifTip     = 0x0004
	nifInfo    = 0x0010

	mfString    = 0x0000
	mfSeparator = 0x0800
	mfGrayed    = 0x0001
	mfDisabled  = 0x0002

	tpmLeftAlign  = 0x0000
	tpmRightAlign = 0x0008
	tpmRightBtn   = 0x0002
	tpmReturnCmd  = 0x0100

	idiApplication = 32512
	idcArrow       = 32512
	swShowNormal   = 1
	swHide         = 0

	csHRedraw = 0x0002
	csVRedraw = 0x0001
)

// 菜单项 id。TrackPopupMenu 用 TPM_RETURNCMD 直接把点中的 id 返回来，
// 不用再走 WM_COMMAND，省一层。
const (
	menuOpenPage = iota + 1
	menuOpenLog
	menuOpenScans
	menuStart
	menuStop
	menuRestart
	menuExit
)

type wndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     syscall.Handle
	hIcon         syscall.Handle
	hCursor       syscall.Handle
	hbrBackground syscall.Handle
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       syscall.Handle
}

type point struct{ X, Y int32 }

type winMsg struct {
	hwnd     syscall.Handle
	message  uint32
	wParam   uintptr
	lParam   uintptr
	time     uint32
	pt       point
	lPrivate uint32
}

// notifyIconData 对应 NOTIFYICONDATAW，字段顺序和类型必须和 Windows 那边一致，
// cbSize 直接取 Go 结构体大小（64 位下是 976，和系统期望的一致）。
type notifyIconData struct {
	cbSize           uint32
	hWnd             syscall.Handle
	uID              uint32
	uFlags           uint32
	uCallbackMessage uint32
	hIcon            syscall.Handle
	szTip            [128]uint16
	dwState          uint32
	dwStateMask      uint32
	szInfo           [256]uint16
	uVersion         uint32
	szInfoTitle      [64]uint16
	dwInfoFlags      uint32
	guidItem         [16]byte
	hBalloonIcon     syscall.Handle
}

var (
	trayHwnd syscall.Handle
	trayIcon syscall.Handle
	// msgTaskbarCreated 是资源管理器重启后广播的消息，收到要把图标加回去，
	// 否则资源管理器崩过一次之后托盘图标就没了，服务还在跑但没人能操作它。
	msgTaskbarCreated uint32
)

func utf16(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		p, _ = syscall.UTF16PtrFromString("")
	}
	return p
}

func copyUTF16(dst []uint16, s string) {
	src, err := syscall.UTF16FromString(s)
	if err != nil {
		return
	}
	if len(src) > len(dst) {
		src = src[:len(dst)]
		src[len(src)-1] = 0
	}
	copy(dst, src)
}

// runTray 建托盘图标并跑消息循环，用户点"退出"后才返回。
func runTray() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hInstance, _, _ := procGetModuleHandle.Call(0)
	className := utf16("ScansvcTrayWindow")

	wc := wndClassEx{
		style:         csHRedraw | csVRedraw,
		lpfnWndProc:   syscall.NewCallback(trayWndProc),
		hInstance:     syscall.Handle(hInstance),
		lpszClassName: className,
	}
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	hIcon, _, _ := procLoadIcon.Call(0, idiApplication)
	wc.hIcon = syscall.Handle(hIcon)
	hCursor, _, _ := procLoadCursor.Call(0, idcArrow)
	wc.hCursor = syscall.Handle(hCursor)

	if ret, _, err := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc))); ret == 0 {
		log.Printf("⚠ 托盘窗口注册失败: %v（服务照常运行，但没有托盘图标）", err)
		waitWithoutTray()
		return
	}

	hwnd, _, err := procCreateWindowEx.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(utf16("scansvc"))),
		0, 0, 0, 0, 0,
		0, 0, hInstance, 0,
	)
	if hwnd == 0 {
		log.Printf("⚠ 托盘窗口创建失败: %v（服务照常运行，但没有托盘图标）", err)
		waitWithoutTray()
		return
	}
	trayHwnd = syscall.Handle(hwnd)
	trayIcon = loadAppIcon(syscall.Handle(hInstance))
	msgTaskbarCreated = registerWindowMessage("TaskbarCreated")

	if !addTrayIcon() {
		log.Printf("⚠ 托盘图标添加失败（服务照常运行）")
	} else {
		log.Printf("已缩到右下角托盘，右键图标可以启动 / 停止 / 重启 / 退出")
		// 图标出来了，才把双击带出来的控制台窗口藏起来——万一托盘没建起来，
		// 至少还有个窗口能看见服务在跑。
		hideOwnedConsole()
	}

	// 监听意外挂掉时提示一下，不然后台运行时没人知道。
	go func() {
		for err := range serverFailures() {
			log.Printf("⚠ 服务异常退出: %v", err)
			trayNotify("scansvc 服务异常", fmt.Sprintf("监听已断开：%v\n可以在托盘右键里重新启动。", err))
		}
	}()

	var m winMsg
	for {
		ret, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) <= 0 { // 0 = WM_QUIT，-1 = 出错
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
}

// hideOwnedConsole 把双击 exe 时弹出来的那个控制台窗口藏起来。
//
// 没用 -ldflags "-H=windowsgui" 编译时，双击 exe 会附带一个控制台窗口。它有两个问题：
// 一是难看（用户以为是"弹框"），二是**点窗口的关闭按钮会把服务一起关掉**——
// Windows 会给控制台里的进程发 CTRL_CLOSE_EVENT，几秒后强杀，托盘也就没了。
//
// 所以托盘起来之后就把它藏掉：进程照常跑，日志照样写文件（见 logfile.go），
// 要退出从托盘右键点"退出"。
//
// 只藏"自己的"控制台：从 cmd / PowerShell 里启动时，控制台是那个 shell 的，
// 藏了会把用户正在用的窗口弄没。用 GetConsoleProcessList 判断——附在这个控制台上的
// 进程只有我们自己（返回 1），才说明是双击启动的。
func hideOwnedConsole() {
	hwnd, _, _ := procGetConsoleWindow.Call()
	if hwnd == 0 {
		return // 用 -H=windowsgui 编的，本来就没有控制台
	}
	var pids [4]uint32
	n, _, _ := procGetConsoleProcessList.Call(uintptr(unsafe.Pointer(&pids[0])), uintptr(len(pids)))
	if n != 1 {
		return // 控制台是别人的（从命令行启动），留着让用户看日志
	}
	procShowWindow.Call(hwnd, swHide)
	log.Printf("已隐藏双击启动时弹出的控制台窗口；日志见 %s，退出请用托盘右键", logFilePath)
}

// waitWithoutTray 在托盘建不起来时退回"等到监听挂掉"的老行为，别让进程直接退出。
func waitWithoutTray() {
	if err := <-srv.Failed(); err != nil {
		log.Printf("服务异常退出: %v", err)
	}
}

func serverFailures() <-chan error { return srv.Failed() }

func registerWindowMessage(name string) uint32 {
	ret, _, _ := procRegisterWindowMsg.Call(uintptr(unsafe.Pointer(utf16(name))))
	return uint32(ret)
}

// loadAppIcon 优先用 exe 自己的图标，没有就用系统默认程序图标。
func loadAppIcon(hInstance syscall.Handle) syscall.Handle {
	if exe, err := os.Executable(); err == nil {
		ret, _, _ := procExtractIcon.Call(uintptr(hInstance), uintptr(unsafe.Pointer(utf16(exe))), 0)
		// 返回 0 表示没图标，1 表示文件不是可执行文件
		if ret > 1 {
			return syscall.Handle(ret)
		}
	}
	ret, _, _ := procLoadIcon.Call(0, idiApplication)
	return syscall.Handle(ret)
}

func newNotifyIconData() notifyIconData {
	nid := notifyIconData{
		hWnd:             trayHwnd,
		uID:              1,
		uCallbackMessage: wmTrayCallback,
		hIcon:            trayIcon,
	}
	nid.cbSize = uint32(unsafe.Sizeof(nid))
	return nid
}

func addTrayIcon() bool {
	nid := newNotifyIconData()
	nid.uFlags = nifMessage | nifIcon | nifTip
	copyUTF16(nid.szTip[:], trayTip())
	ret, _, _ := procShellNotifyIcon.Call(nimAdd, uintptr(unsafe.Pointer(&nid)))
	return ret != 0
}

func removeTrayIcon() {
	nid := newNotifyIconData()
	procShellNotifyIcon.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
}

// updateTrayTip 让鼠标停在图标上时能看到当前状态。
func updateTrayTip() {
	nid := newNotifyIconData()
	nid.uFlags = nifTip
	copyUTF16(nid.szTip[:], trayTip())
	procShellNotifyIcon.Call(nimModify, uintptr(unsafe.Pointer(&nid)))
}

// trayNotify 弹一个气泡提示。托盘提示被系统关掉时不会显示，所以重要信息同时写日志。
func trayNotify(title, text string) {
	nid := newNotifyIconData()
	nid.uFlags = nifInfo
	copyUTF16(nid.szInfoTitle[:], title)
	copyUTF16(nid.szInfo[:], text)
	procShellNotifyIcon.Call(nimModify, uintptr(unsafe.Pointer(&nid)))
}

func trayTip() string {
	if srv.Running() {
		return "scansvc 扫描服务（运行中）"
	}
	return "scansvc 扫描服务（已停止）"
}

func trayWndProc(hwnd syscall.Handle, message uint32, wParam, lParam uintptr) uintptr {
	switch {
	case message == wmTrayCallback:
		switch uint32(lParam) {
		case wmRButtonUp:
			showTrayMenu()
		case wmLButtonDblClk:
			openHomePage()
		}
		return 0

	case message == wmClose, message == wmDestroy:
		removeTrayIcon()
		procPostQuitMessage.Call(0)
		return 0

	case msgTaskbarCreated != 0 && message == msgTaskbarCreated:
		// 资源管理器重启了，把图标加回去
		addTrayIcon()
		return 0
	}

	ret, _, _ := procDefWindowProc.Call(uintptr(hwnd), uintptr(message), wParam, lParam)
	return ret
}

func showTrayMenu() {
	hmenu, _, _ := procCreatePopupMenu.Call()
	if hmenu == 0 {
		return
	}
	defer procDestroyMenu.Call(hmenu)

	running := srv.Running()

	// 第一行是状态，灰掉不让点
	status := "已停止"
	if addrs := srv.Addrs(); len(addrs) > 0 {
		status = "运行中: " + joinAddrs(addrs)
	}
	appendMenu(hmenu, mfString|mfGrayed|mfDisabled, 0, status)
	appendMenu(hmenu, mfSeparator, 0, "")

	pageFlags := uintptr(mfString)
	if !running {
		pageFlags = mfString | mfGrayed | mfDisabled
	}
	appendMenu(hmenu, pageFlags, menuOpenPage, "打开测试页面")
	appendMenu(hmenu, mfString, menuOpenLog, "打开日志")
	appendMenu(hmenu, mfString, menuOpenScans, "打开图片目录")
	appendMenu(hmenu, mfSeparator, 0, "")

	startFlags, stopFlags := uintptr(mfString), uintptr(mfString)
	if running {
		startFlags = mfString | mfGrayed | mfDisabled
	} else {
		stopFlags = mfString | mfGrayed | mfDisabled
	}
	appendMenu(hmenu, startFlags, menuStart, "启动服务")
	appendMenu(hmenu, stopFlags, menuStop, "停止服务")
	appendMenu(hmenu, mfString, menuRestart, "重启服务")
	appendMenu(hmenu, mfSeparator, 0, "")
	appendMenu(hmenu, mfString, menuExit, "退出")

	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	// 不先把窗口提到前台的话，菜单会点不掉（Win32 的老问题）
	procSetForegroundWindow.Call(uintptr(trayHwnd))
	cmd, _, _ := procTrackPopupMenu.Call(
		hmenu,
		tpmLeftAlign|tpmRightBtn|tpmReturnCmd,
		uintptr(pt.X), uintptr(pt.Y),
		0, uintptr(trayHwnd), 0,
	)
	procPostMessage.Call(uintptr(trayHwnd), 0, 0, 0)

	handleTrayCommand(int(cmd))
}

func appendMenu(hmenu uintptr, flags, id uintptr, text string) {
	var textPtr uintptr
	if text != "" {
		textPtr = uintptr(unsafe.Pointer(utf16(text)))
	}
	procAppendMenu.Call(hmenu, flags, id, textPtr)
}

func handleTrayCommand(cmd int) {
	switch cmd {
	case menuOpenPage:
		openHomePage()
	case menuOpenLog:
		if logFilePath == "" {
			trayNotify("没有日志文件", "配置里 log 是空的，日志只打在控制台。")
			return
		}
		shellOpen(logFilePath)
	case menuOpenScans:
		shellOpen(scanRoot)
	case menuStart:
		startFromTray()
	case menuStop:
		srv.Stop()
		updateTrayTip()
	case menuRestart:
		srv.Stop()
		startFromTray()
	case menuExit:
		log.Printf("从托盘退出")
		removeTrayIcon()
		procDestroyWindow.Call(uintptr(trayHwnd))
	}
}

func startFromTray() {
	if n := srv.Start(); n == 0 {
		trayNotify("启动失败", fmt.Sprintf("端口 %d / %d 都没监听成功，多半被别的程序占着，详情见日志。",
			cfg.WSPort, cfg.HTTPPort))
	}
	updateTrayTip()
}

func openHomePage() {
	addrs := srv.Addrs()
	if len(addrs) == 0 {
		trayNotify("服务已停止", "先从托盘右键里启动服务。")
		return
	}
	port := defaultHTTPPort
	if httpPortActual > 0 {
		port = httpPortActual
	}
	shellOpen(fmt.Sprintf("http://127.0.0.1:%d/", port))
}

// shellOpen 用系统默认方式打开：网址给浏览器，目录给资源管理器，文件给记事本之类。
// 不用 cmd /c start，那样会闪一个黑窗口。
func shellOpen(target string) {
	if target == "" {
		return
	}
	if abs, err := filepath.Abs(target); err == nil && fileExist(abs) {
		target = abs
	}
	procShellExecute.Call(0,
		uintptr(unsafe.Pointer(utf16("open"))),
		uintptr(unsafe.Pointer(utf16(target))),
		0, 0, swShowNormal)
}

func joinAddrs(addrs []string) string {
	out := ""
	for i, a := range addrs {
		if i > 0 {
			out += "  "
		}
		out += a
	}
	return out
}

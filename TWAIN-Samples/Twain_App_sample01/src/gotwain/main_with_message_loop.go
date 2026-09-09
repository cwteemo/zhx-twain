package main

import (
	"fmt"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

/*
#cgo LDFLAGS: -L../../visual_studio/Debug -lTWAIN_APP_CMD64 -lstdc++
#include <windows.h>

// DLL函数声明
void zhx_twain_test();
void zhx_twain();
int zhx_Init(HWND hWnd);
int zhx_LoadDS(int deviceId);
int zhx_Cleanup();
int zhx_ProcessEvent(MSG* msg);
int zhx_GetDSMessage();
int zhx_EnableDS(HWND hWnd);
int zhx_HandleScanReady();

// 辅助函数：从Go调用PeekMessage
static int peekMessageWrapper(MSG* msg, HWND hWnd, UINT wMsgFilterMin, UINT wMsgFilterMax, UINT wRemoveMsg) {
    return PeekMessageA(msg, hWnd, wMsgFilterMin, wMsgFilterMax, wRemoveMsg);
}
*/
import "C"

// Windows消息常量
const (
	WM_QUIT       = 0x0012
	PM_REMOVE     = 0x0001
	PM_NOREMOVE   = 0x0000
)

// TWAIN消息常量（从twain.h）
const (
	MSG_NULL      = 0x0000
	MSG_XFERREADY = 0x0001
	MSG_CLOSEDSREQ = 0x0002
	MSG_CLOSEDSOK  = 0x0003
)

// Windows API函数（用于获取窗口句柄）
var (
	user32               = syscall.NewLazyDLL("user32.dll")
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleWindow = kernel32.NewProc("GetConsoleWindow")
	procGetDesktopWindow = user32.NewProc("GetDesktopWindow")
)

func main() {
	// TWAIN / Windows GUI 要求固定线程，否则可能崩溃
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	fmt.Println("TWAIN DLL测试程序（带消息循环）")
	fmt.Println("==================================")

	// 获取窗口句柄（优先使用控制台窗口）
	hWnd := getConsoleWindow()
	if hWnd == 0 {
		fmt.Println("警告: 无法获取控制台窗口句柄，尝试使用桌面窗口")
		hWnd = getDesktopWindow()
	}

	if hWnd == 0 {
		fmt.Println("错误: 无法获取有效的窗口句柄")
		return
	}

	fmt.Printf("使用窗口句柄: 0x%x\n", hWnd)

	// 初始化TWAIN（这会创建gpTwainApplicationCMD并连接DSM）
	fmt.Println("\n初始化TWAIN...")
	result := C.zhx_Init((C.HWND)(unsafe.Pointer(hWnd)))
	if result == 0 {
		fmt.Println("错误: TWAIN初始化失败")
		return
	}
	fmt.Println("TWAIN初始化成功")

	// 加载数据源（设备ID从1开始）
	fmt.Println("\n加载数据源（设备ID: 1）...")
	result = C.zhx_LoadDS(1)
	if result == 0 {
		fmt.Println("错误: 加载数据源失败")
		C.zhx_Cleanup()
		return
	}
	fmt.Println("数据源加载成功")

	// 启用DS（不进入消息循环）
	fmt.Println("\n启用数据源...")
	result = C.zhx_EnableDS((C.HWND)(unsafe.Pointer(hWnd)))
	if result == 0 {
		fmt.Println("错误: 启用数据源失败")
		C.zhx_Cleanup()
		return
	}

	fmt.Println("数据源已启用，开始消息循环...")

	// 运行消息循环（这会处理Windows消息和TWAIN事件）
	runMessageLoop()

	// 检查是否有DS消息
	dsMessage := C.zhx_GetDSMessage()
	fmt.Printf("\n消息循环结束，DS消息状态: %d\n", dsMessage)

	if dsMessage != 0 {
		// 处理扫描就绪
		if dsMessage == MSG_XFERREADY {
			fmt.Println("处理扫描就绪...")
			result := C.zhx_HandleScanReady()
			if result != 0 {
				fmt.Println("扫描完成")
			} else {
				fmt.Println("扫描处理失败")
			}
		} else if dsMessage == MSG_CLOSEDSREQ || dsMessage == MSG_CLOSEDSOK {
			fmt.Printf("收到关闭请求消息: %d\n", dsMessage)
		} else {
			fmt.Printf("收到未知DS消息: %d\n", dsMessage)
		}
	} else {
		fmt.Println("未收到DS消息")
	}

	// 清理TWAIN环境
	fmt.Println("\n清理TWAIN环境...")
	C.zhx_Cleanup()

	fmt.Println("\n测试完成")
	fmt.Println("按Enter键退出...")
	fmt.Scanln()
}

// 运行Windows消息循环
// 这个函数实现了与原始C++项目相同的消息循环逻辑
func runMessageLoop() {
	const timeout = 30 * time.Second
	startTime := time.Now()
	maxLoops := 10000
	loopCount := 0

	fmt.Println("进入消息循环...")

	for {
		loopCount++
		
		// 超时检查
		if time.Since(startTime) > timeout {
			fmt.Printf("消息循环超时（%v）\n", timeout)
			break
		}

		// 检查循环次数（防止无限循环）
		if loopCount > maxLoops {
			fmt.Printf("消息循环达到最大次数（%d）\n", maxLoops)
			break
		}

		// 首先检查是否有DS消息（避免不必要的消息处理）
		dsMessage := C.zhx_GetDSMessage()
		if dsMessage != 0 {
			fmt.Printf("检测到DS消息: %d，退出消息循环\n", dsMessage)
			break
		}

		// 使用PeekMessage获取消息（非阻塞）
		// 直接使用C的MSG结构体，确保内存布局正确
		var cMsg C.MSG
		hasMessage := C.peekMessageWrapper(&cMsg, nil, 0, 0, C.UINT(PM_REMOVE))
		
		if hasMessage != 0 {
			// 处理WM_QUIT消息
			if cMsg.message == C.UINT(WM_QUIT) {
				fmt.Println("收到WM_QUIT消息，退出消息循环")
				break
			}

			// 将消息传递给DLL处理
			// DLL会调用MSG_PROCESSEVENT来处理TWAIN事件
			shouldContinue := C.zhx_ProcessEvent(&cMsg)
			
			if shouldContinue == 0 {
				// DLL指示退出循环（可能收到了TWAIN消息）
				// 再次检查DS消息
				dsMessage = C.zhx_GetDSMessage()
				if dsMessage != 0 {
					fmt.Printf("DLL处理消息后检测到DS消息: %d\n", dsMessage)
				}
				break
			}
		} else {
			// 没有消息，短暂休眠以避免CPU占用过高
			// 这与原始项目的Sleep(10)一致
			time.Sleep(10 * time.Millisecond)
		}

		// 每1000次循环输出一次进度（可选，用于调试）
		if loopCount%1000 == 0 {
			elapsed := time.Since(startTime)
			fmt.Printf("消息循环进行中... (循环: %d, 耗时: %v)\n", loopCount, elapsed)
		}
	}

	fmt.Printf("消息循环结束，共处理 %d 次循环，耗时 %v\n", loopCount, time.Since(startTime))
}

// 获取控制台窗口句柄
func getConsoleWindow() uintptr {
	ret, _, _ := procGetConsoleWindow.Call()
	return ret
}

// 获取桌面窗口句柄（作为后备）
func getDesktopWindow() uintptr {
	ret, _, _ := procGetDesktopWindow.Call()
	return ret
}


package main

import (
	"fmt"
)

// 定义DLL文件路径 - 根据实际路径修改

/*
#cgo LDFLAGS: -L../../visual_studio/Debug -lTWAIN_APP_CMD64 -lstdc++
//#cgo CFLAGS : -I${SRCDIR}/include

//typedef int (*ScanCallback)(char *filename);
//void goFuncForScanCallBack(char *);
//#include "scanner.h";

void zhx_twain_test();
void zhx_twain_test1();
// void zhx_Init();
// int zhx_ApproveLicenseA(char *license);
// char *zhx_GetDevicesList();
// char *zhx_GetDevCapability_JSON(char *device);
// int zhx_SetCapability_STR(char *nCap, char *value);
// int zhx_OpenDevice(char *device);
// int zhx_Scan(char *path, ScanCallback cb, int count);
// void zhx_EndScan();
// void zhx_CloseDevice();
// void zhx_Exit();

*/
import "C" // 切勿换行再写这个

var dllPath = "L:/code/twain/zhx-twain/TWAIN-Samples/Twain_App_sample01/visual_studio/Debug/TWAIN_APP_CMD64.dll"

// // 调用zhx_twain_test函数
// func zhxTwainTest(message string) string {
// 	if procZhxTwainTest == nil {
// 		return "函数未找到"
// 	}

// 	// 将Go字符串转换为C字符串
// 	msgPtr, _ := syscall.BytePtrFromString(message)

// 	// 调用DLL函数
// 	ret, _, _ := procZhxTwainTest.Call(uintptr(unsafe.Pointer(msgPtr)))

// 	// 将返回值转换为Go字符串
// 	if ret != 0 {
// 		// 将uintptr转换为字符串指针，然后读取字符串
// 		// 使用unsafe.Pointer和字节数组手动转换C字符串
// 		ptr := (*byte)(unsafe.Pointer(ret))
// 		if ptr == nil {
// 			return ""
// 		}

// 		// 读取C字符串（以NULL结尾）
// 		var bytes []byte
// 		for i := 0; ; i++ {
// 			b := *(*byte)(unsafe.Pointer(uintptr(unsafe.Pointer(ptr)) + uintptr(i)))
// 			if b == 0 {
// 				break
// 			}
// 			bytes = append(bytes, b)
// 		}

// 		return string(bytes)
// 	}

// 	return "调用失败"
// }

// // 初始化TWAIN环境
// func zhxTwainInit() bool {
// 	if procZhxTwainInit == nil {
// 		return false
// 	}

// 	// 传递nil作为窗口句柄
// 	ret, _, _ := procZhxTwainInit.Call(0)

// 	return ret != 0
// }

// // 退出TWAIN环境
// func zhxTwainExit() {
// 	if procZhxTwainExit == nil {
// 		return
// 	}

// 	procZhxTwainExit.Call()
// }

// // 获取可用扫描仪列表
// func zhxTwainGetScanners() []string {
// 	if procZhxTwainGetScanners == nil {
// 		return nil
// 	}

// 	// 创建字符串指针数组，最多存储10个扫描仪
// 	const maxScanners = 10
// 	scannerPtrs := make([]uintptr, maxScanners)
// 	scannerBuffers := make([][]byte, maxScanners)

// 	// 为每个扫描仪名称分配内存缓冲区
// 	for i := 0; i < maxScanners; i++ {
// 		// 每个扫描仪名称最多256个字符
// 		buffer := make([]byte, 256)
// 		scannerBuffers[i] = buffer
// 		scannerPtrs[i] = uintptr(unsafe.Pointer(&buffer[0]))
// 	}

// 	// 调用DLL函数
// 	ret, _, _ := procZhxTwainGetScanners.Call(
// 		uintptr(unsafe.Pointer(&scannerPtrs[0])),
// 		uintptr(maxScanners),
// 	)

// 	// 将返回值转换为扫描仪数量
// 	count := int(ret)
// 	if count <= 0 {
// 		return nil
// 	}

// 	// 提取扫描仪名称
// 	scanners := make([]string, count)
// 	for i := 0; i < count; i++ {
// 		scanners[i] = string(scannerBuffers[i])
// 	}

// 	return scanners
// }

// // 执行扫描
// func zhxTwainScan(scannerName, savePath, format string, showUI bool) bool {
// 	if procZhxTwainScan == nil {
// 		return false
// 	}

// 	// 将Go字符串转换为C字符串
// 	var scannerPtr, savePathPtr, formatPtr *byte
// 	var scannerPtrVal, savePathPtrVal, formatPtrVal uintptr

// 	if scannerName != "" {
// 		tmp, _ := syscall.BytePtrFromString(scannerName)
// 		scannerPtr = tmp
// 		scannerPtrVal = uintptr(unsafe.Pointer(scannerPtr))
// 	}

// 	if savePath != "" {
// 		tmp, _ := syscall.BytePtrFromString(savePath)
// 		savePathPtr = tmp
// 		savePathPtrVal = uintptr(unsafe.Pointer(savePathPtr))
// 	}

// 	if format != "" {
// 		tmp, _ := syscall.BytePtrFromString(format)
// 		formatPtr = tmp
// 		formatPtrVal = uintptr(unsafe.Pointer(formatPtr))
// 	}

// 	// 将bool转换为整数
// 	showUIInt := 0
// 	if showUI {
// 		showUIInt = 1
// 	}

// 	// 调用DLL函数
// 	ret, _, _ := procZhxTwainScan.Call(
// 		scannerPtrVal,
// 		savePathPtrVal,
// 		formatPtrVal,
// 		uintptr(showUIInt),
// 	)

// 	return ret != 0
// }

func main() {
	fmt.Println("TWAIN DLL测试程序")
	fmt.Println("==================")

	// 测试zhx_twain_test函数
	fmt.Println("\n测试1: 调用zhx_twain_test函数")
	C.zhx_twain_test()
	// 测试初始化TWAIN环境
	fmt.Println("\n测试2: 初始化TWAIN环境")

	fmt.Println("\n测试完成")
	fmt.Println("按Enter键退出...")
	fmt.Scanln()
}

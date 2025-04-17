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
void zhx_twain();
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
func main() {
	fmt.Println("TWAIN DLL测试程序")
	fmt.Println("==================")

	// 测试zhx_twain_test函数
	fmt.Println("\n测试1: 调用zhx_twain_test函数")
	C.zhx_twain_test()
	C.zhx_twain()
	// 测试初始化TWAIN环境
	fmt.Println("\n测试2: 初始化TWAIN环境")

	fmt.Println("\n测试完成")
	fmt.Println("按Enter键退出...")
	fmt.Scanln()
}

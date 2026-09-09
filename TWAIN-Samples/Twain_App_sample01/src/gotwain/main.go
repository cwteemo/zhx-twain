package main

import (
	"fmt"
	"runtime"
)

// 定义DLL文件路径 - 根据实际路径修改

/*
#cgo LDFLAGS: -L../../visual_studio/Debug -lTWAIN_APP_CMD64 -lstdc++
//#cgo CFLAGS : -I${SRCDIR}/include
// 标准库 free() 声明
#include <stdlib.h>
// 如无需回调，可仅定义回调类型而不实现函数
typedef int (*ScanCallback)(char *filename);

void zhx_twain_test();
void zhx_twain();

*/
import "C" // 切勿换行再写这个

// TwainSimpleScan 使用已封装好的 C 接口完成一次简单扫描示例。
// 说明：
//   - 这里直接调用 DLL 中已经封装好的 zhx_twain()
//   - 后续如果在 DLL 里新增 zhx_Init/zhx_Scan 等细粒度接口，再在这里拆分也可以
func TwainSimpleScan(outputDir string, count int) int {
	_ = outputDir
	_ = count
	// 当前 DLL 暴露的入口是 zhx_twain，内部已完成连接 DSM、打开数据源、扫描等流程
	C.zhx_twain()
	return 0
}

func main() {
    // TWAIN / Windows GUI 要求固定线程，否则可能 0xc0000005 崩溃
	runtime.LockOSThread()
    defer runtime.UnlockOSThread()

    fmt.Println("TWAIN DLL测试程序")
    fmt.Println("==================")

    fmt.Println("\n测试1: 调用zhx_twain_test函数")
    C.zhx_twain_test()

    fmt.Println("\n测试2: 调用封装接口进行简单扫描")
    ret := TwainSimpleScan("./temp1", 1)
    fmt.Println("TwainSimpleScan 返回值:", ret)

    fmt.Println("\n测试完成")
    fmt.Println("按Enter键退出...")
    fmt.Scanln()
}

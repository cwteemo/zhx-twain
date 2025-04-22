package main

// 定义DLL文件路径 - 根据实际路径修改

/*
#cgo LDFLAGS: -L../../visual_studio/Debug -lTWAIN_APP_CMD64 -lstdc++
//#cgo CFLAGS : -I${SRCDIR}/include

typedef int (*ScanCallback)(char *filename);
void goFuncForScanCallBack(char *);
//#include "scanner.h";

void zhx_twain_test();
void zhx_twain();
void zhx_Init();
int zhx_SetTransferMechanism(int mechanism);
int zhx_SetImageFileFormat(int format);
// int zhx_ApproveLicenseA(char *license);
char *zhx_GetDevicesList();
// char *zhx_GetDevCapability_JSON(char *device);
// int zhx_SetCapability_STR(char *nCap, char *value);
int zhx_OpenDevice(char *device);
int zhx_Scan(char *path, ScanCallback cb, int count);
void zhx_EndScan();
void zhx_CloseDevice();
void zhx_Exit();

*/
import "C" // 切勿换行再写这个
import (
	"fmt"
	"net/http"
	"runtime"
	"strings"
)

func indexHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "hello world")
}

type GoScanCallBack func(filename string)

// 导出为C函数
//
//export goFuncForScanCallBack
func goFuncForScanCallBack(filename *C.char) {
	fmt.Println("get file name: ", C.GoString(filename))
}

// 添加一个新的处理函数来处理 /zhx_twain 路径的请求
func zhxTwainHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	// 使用 recover 捕获可能的 panic
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(w, "scan error: %v", r)
			fmt.Println("error:", r)
		}
	}()

	runtime.LockOSThread()
	zhx_twain()
	runtime.UnlockOSThread()

	fmt.Fprintf(w, "scan  done")
}

func main() {
	// 注册路由处理函数
	zhx_twain()
	// http.HandleFunc("/", indexHandler)
	// http.HandleFunc("/zhx_twain", zhxTwainHandler)

	// // 打印服务启动信息
	// fmt.Println("服务器已启动，访问 http://localhost:8000/zhx_twain 来测试TWAIN")

	// // 启动HTTP服务器
	// http.ListenAndServe(":8000", nil)
}
func zhx_twain() {
	//fmt.Println("TWAIN DLL测试程序")
	//fmt.Println("==================")

	// 测试zhx_twain_test函数
	//fmt.Println("\n测试1: 调用zhx_twain_test函数")
	//C.zhx_twain_test()
	// 首先初始化TWAIN环境
	C.zhx_Init()

	// 获取设备列表
	devicesList := C.zhx_GetDevicesList()
	devicesString := C.GoString(devicesList)

	// 打印字符串内容
	fmt.Printf("\n扫描仪列表: %s\n", devicesString)
	// 解析分号分隔的扫描仪列表
	scanners := strings.Split(devicesString, ";")
	fmt.Printf("找到%d台扫描仪:\n", len(scanners))
	for i, scanner := range scanners {
		if scanner != "" { // 忽略空项
			fmt.Printf("%d. %s\n", i+1, scanner)
		}
	}

	// 选择第二个扫描仪（如果存在）
	if len(scanners) >= 2 && scanners[1] != "" {
		// 将Go字符串转换为C字符串
		cScannerName := C.CString(scanners[1])

		// 打开第二个扫描仪
		result := C.zhx_OpenDevice(cScannerName)
		fmt.Printf("打开扫描仪 '%s' 的结果: %v\n", scanners[1], result)

		// 后续操作...
	} else {
		fmt.Println("没有找到第二个扫描仪")
	}
	C.zhx_SetTransferMechanism(C.int(1))
	C.zhx_SetImageFileFormat(0)
	//C.goFuncForScanCallBack(C.CString("temp"))
	code := int(C.zhx_Scan(C.CString("L:/code/twain/zhx-twain/TWAIN-Samples/Twain_App_sample01/src/gotwain/temp1"), C.ScanCallback(C.goFuncForScanCallBack), C.int(6)))
	fmt.Printf("\n扫描完成，错误码%d\n", code)

	code1 := int(C.zhx_Scan(C.CString("L:/code/twain/zhx-twain/TWAIN-Samples/Twain_App_sample01/src/gotwain/temp1"), C.ScanCallback(C.goFuncForScanCallBack), C.int(6)))
	fmt.Printf("\n扫描完成，错误码%d\n", code1)

	code2 := int(C.zhx_Scan(C.CString("L:/code/twain/zhx-twain/TWAIN-Samples/Twain_App_sample01/src/gotwain/temp2"), C.ScanCallback(C.goFuncForScanCallBack), C.int(6)))
	fmt.Printf("\n扫描完成，错误码%d\n", code2)
	C.zhx_EndScan()
	C.zhx_CloseDevice()
	C.zhx_Exit()
	//C.zhx_twain()
	// 测试初始化TWAIN环境
	//fmt.Println("\n测试2: 初始化TWAIN环境")

	//fmt.Println("\n测试完成")
	//fmt.Println("按Enter键退出...")
	//fmt.Scanln()
}

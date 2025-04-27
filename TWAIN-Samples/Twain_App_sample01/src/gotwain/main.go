package main

// 定义DLL文件路径 - 根据实际路径修改

/*
#cgo LDFLAGS: -L. -lTWAIN_APP_CMD64 -lstdc++
//#cgo CFLAGS : -I${SRCDIR}/include
#include <stdlib.h>  // 添加这一行以使用 free 函数
typedef int (*ScanCallback)(char *filename);
void goFuncForScanCallBack(char *);
//#include "scanner.h";

void zhx_twain_test();
void zhx_twain();
void zhx_Init();
int zhx_SetTransferMechanism(int mechanism);
int zhx_SetImageFileFormat(int format);
int zhx_GetCurrentFileFormat();
char *zhx_GetSupportedFileFormats();
char* zhx_GetSupportedResolutions();
int zhx_SetResolution(int dpi);
int zhx_GetCurrentResolution();
// int zhx_ApproveLicenseA(char *license);
char *zhx_GetDevicesList();
char *zhx_GetDevCapability_JSON(char *device);
char *zhx_GetDevCapability_STR(char *device);
char *zhx_GetCapability_STR(char* capOrDevice);
int zhx_SetCapability_STR(char *nCap, char *value);
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
	"unsafe"

	"gotwain/twainConfig" // 导入本地包
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
	//zhx_twain()
	//return
	// 注册路由处理函数
	//http.HandleFunc("/", indexHandler)
	//http.HandleFunc("/zhx_twain", zhxTwainHandler)
	http.HandleFunc("/config", twainConfig.ScannerConfigHandler) // 直接使用twainConfig包中的函数

	fmt.Println("Server started, visit http://localhost:8000/ to test the API")
	http.ListenAndServe(":8000", nil)
}

func zhx_twain() {
	// 初始化TWAIN环境
	C.zhx_Init()

	// 获取设备列表
	devicesList := C.zhx_GetDevicesList()
	devicesString := C.GoString(devicesList)

	// 打印字符串内容
	fmt.Printf("\n===== 可用扫描仪列表 =====\n")
	// 解析分号分隔的扫描仪列表
	scanners := strings.Split(devicesString, ";")

	// 创建有效扫描仪的列表（过滤空项）
	var validScanners []string
	for _, scanner := range scanners {
		if scanner != "" { // 忽略空项
			validScanners = append(validScanners, scanner)
		}
	}

	// 检查是否找到扫描仪
	if len(validScanners) == 0 {
		fmt.Println("未检测到扫描仪设备！")
		fmt.Println("按Enter键退出...")
		fmt.Scanln()
		return
	}

	// 输出扫描仪列表供用户选择
	fmt.Printf("找到 %d 台扫描仪:\n", len(validScanners))
	for i, scanner := range validScanners {
		fmt.Printf("%d. %s\n", i+1, scanner)
	}

	// 提示用户选择扫描仪
	var selectedIndex int
	for {
		fmt.Printf("\n请输入要使用的扫描仪序号 (1-%d): ", len(validScanners))
		_, err := fmt.Scanln(&selectedIndex)

		if err != nil {
			// 清除输入缓冲区
			fmt.Scanln() // 消耗可能剩余的换行符
			fmt.Println("输入无效，请重新输入数字。")
			continue
		}

		// 检查输入的有效性
		if selectedIndex < 1 || selectedIndex > len(validScanners) {
			fmt.Printf("序号必须在 1 到 %d 之间，请重新输入。\n", len(validScanners))
			continue
		}

		break // 输入有效，退出循环
	}

	// 调整索引（用户输入1-n，但索引从0开始）
	selectedIndex--

	// 将Go字符串转换为C字符串
	cScannerName := C.CString(validScanners[selectedIndex])

	// 打开选定的扫描仪
	result := C.zhx_OpenDevice(cScannerName)
	fmt.Printf("打开扫描仪 '%s' 的结果: %v\n", validScanners[selectedIndex], result)

	if result == 0 {
		fmt.Println("打开扫描仪失败！")
		fmt.Println("按Enter键退出...")
		fmt.Scanln()
		return
	}

	//获取支持的capabilities
	supportedCapabilities := C.zhx_GetDevCapability_STR(cScannerName)
	supportedCapabilitiesStr := C.GoString(supportedCapabilities)
	fmt.Printf("支持的capabilities: %s\n", supportedCapabilitiesStr)

	capabilitiesStr := "CAP_DEVICEONLINE:4111;CAP_INDICATORS:4107;CAP_ENABLEDSUIONLY:4116;CAP_PAPERDETECTABLE:4109;CAP_FEEDERENABLED:4098;CAP_FEEDERLOADED:4099;CAP_DUPLEX:4114;CAP_DUPLEXENABLED:4115;CAP_AUTOFEED:4103;CAP_SUPPORTEDCAPS:4101;Unknown CAP 0x:103E:4158;CAP_UICONTROLLABLE:4110;CAP_XFERCOUNT:1;ICAP_BITDEPTH:4395;ICAP_BITORDER:4380;ICAP_COMPRESSION:256;ICAP_FRAMES:4372;ICAP_MAXFRAMES:4378;ICAP_IMAGEFILEFORMAT:4364;ICAP_PHYSICALHEIGHT:4370;ICAP_PHYSICALWIDTH:4369;ICAP_PIXELFLAVOR:4383;ICAP_PIXELTYPE:257;ICAP_PLANARCHUNKY:4384;ICAP_SUPPORTEDSIZES:4386;ICAP_ORIENTATION:4368;ICAP_UNITS:258;ICAP_XFERMECH:259;ICAP_XNATIVERESOLUTION:4374;ICAP_XRESOLUTION:4376;ICAP_YNATIVERESOLUTION:4375;ICAP_YRESOLUTION:4377;ICAP_THRESHOLD:4387;ICAP_CONTRAST:4355;ICAP_BRIGHTNESS:4353;ICAP_GAMMA:4360;CAP_CUSTOMINTERFACEGUID:4156;Custom CAP 0x:8001:32769;Custom CAP 0x:8002:32770;CAP_CUSTOMDSDATA:4117"

	// 解析字符串
	capabilities := strings.Split(capabilitiesStr, ";")

	// 遍历每个能力项并获取其值
	for _, capability := range capabilities {
		// 以冒号分割
		parts := strings.SplitN(capability, ":", 2)
		if len(parts) < 2 {
			continue // 跳过格式不正确的项
		}

		capName := parts[0]
		capValue := parts[1]

		// 设置为当前线程锁定
		runtime.LockOSThread()

		// 调用C函数获取能力值
		cCapName := C.CString(capValue)
		defer C.free(unsafe.Pointer(cCapName))
		capResult := C.zhx_GetCapability_STR(cCapName)

		// 转换结果为Go字符串
		resultStr := C.GoString(capResult)

		// 解锁线程
		runtime.UnlockOSThread()
		// 打印结果
		fmt.Printf("能力项: %s (值: %s)\n结果: %s\n\n", capName, capValue, resultStr)
	}

	cStr := C.CString("CAP_XFERCOUNT")
	defer C.free(unsafe.Pointer(cStr))
	paperSizes := C.zhx_GetCapability_STR(cStr)
	paperSizesStr := C.GoString(paperSizes)
	fmt.Printf("传输计数: %s\n", paperSizesStr)
	return

	// 获取支持的capabilities
	// supportedCapabilities := C.zhx_GetDevCapability_JSON(cScannerName)
	// supportedCapabilitiesStr := C.GoString(supportedCapabilities)
	// fmt.Printf("支持的capabilities: %s\n", supportedCapabilitiesStr)

	// // 获取支持的capabilities
	// supportedCapabilitiesStrA := C.zhx_GetDevCapability_STR(cScannerName)
	// supportedCapabilitiesStrB := C.GoString(supportedCapabilitiesStrA)
	// fmt.Printf("支持的capabilities: %s\n", supportedCapabilitiesStrB)

	// // =========== 手动设置扫描参数 ===========
	// fmt.Println("\n===== 设置扫描参数 =====")
	// // 获取扫描仪支持的纸张尺寸
	// paperSizes := C.zhx_GetCapability_STR(C.CString("ICAP_SUPPORTEDSIZES"))
	// paperSizesStr := C.GoString(paperSizes)
	// fmt.Printf("扫描仪支持的纸张尺寸: %s\n", paperSizesStr)
	// // 设置分辨率为 300 DPI
	// fmt.Println("设置分辨率为 300 DPI...")
	// resXResult := C.zhx_SetCapability_STR(C.CString("ICAP_XRESOLUTION"), C.CString("300"))
	// resYResult := C.zhx_SetCapability_STR(C.CString("ICAP_YRESOLUTION"), C.CString("300"))
	// fmt.Printf("设置 X 分辨率结果: %d, Y 分辨率结果: %d\n", resXResult, resYResult)

	// // 设置文件格式为 TIFF (TWFF_TIFF = 0)
	// fmt.Println("设置文件格式为 TIFF...")
	// formatResult := C.zhx_SetCapability_STR(C.CString("ICAP_IMAGEFILEFORMAT"), C.CString("0"))
	// fmt.Printf("设置文件格式结果: %d\n", formatResult)

	// // 设置纸张尺寸为 A4 (TWSS_A4 = 1)
	// fmt.Println("设置纸张尺寸为 A4...")
	// // 确保单位为英寸 (TWUN_INCHES = 0)
	// // 设置纸张尺寸为 US Letter (值为 3)
	// fmt.Println("设置纸张尺寸为 US Letter...")
	// paperResult := C.zhx_SetCapability_STR(C.CString("ICAP_SUPPORTEDSIZES"), C.CString("3"))
	// fmt.Printf("纸张尺寸设置结果: %d\n", paperResult)

	// // 设置传输机制
	// C.zhx_SetTransferMechanism(C.int(1))

	// // =========== 设置文件格式 ===========
	// fmt.Println("\n===== 文件格式设置 =====")

	// // 获取当前文件格式
	// currentFormat := C.zhx_GetCurrentFileFormat()
	// fmt.Printf("当前文件格式: %d\n", currentFormat)

	// // 获取支持的文件格式
	// supportedFormatsC := C.zhx_GetSupportedFileFormats()
	// supportedFormatsStr := C.GoString(supportedFormatsC)

	// // 解析JSON格式的支持格式
	// var formatsList []map[string]interface{}
	// if err := json.Unmarshal([]byte(supportedFormatsStr), &formatsList); err != nil {
	// 	fmt.Printf("解析文件格式数据出错: %v\n", err)
	// 	fmt.Println("将使用默认文件格式继续...")
	// } else {
	// 	// 显示可用的文件格式选项
	// 	fmt.Println("\n可用的文件格式:")
	// 	for i, format := range formatsList {
	// 		fmt.Printf("%d. %s\n", i+1, format["label"])
	// 	}

	// 	// 提示用户选择文件格式
	// 	var formatIndex int
	// 	for {
	// 		fmt.Printf("\n请输入要使用的文件格式序号 (1-%d): ", len(formatsList))
	// 		_, err := fmt.Scanln(&formatIndex)

	// 		if err != nil {
	// 			fmt.Scanln() // 清除输入
	// 			fmt.Println("输入无效，请重新输入数字。")
	// 			continue
	// 		}

	// 		if formatIndex < 1 || formatIndex > len(formatsList) {
	// 			fmt.Printf("序号必须在 1 到 %d 之间，请重新输入。\n", len(formatsList))
	// 			continue
	// 		}

	// 		break
	// 	}

	// 	// 设置用户选择的文件格式
	// 	selectedFormat := int(formatsList[formatIndex-1]["value"].(float64))
	// 	formatResult := C.zhx_SetImageFileFormat(C.int(selectedFormat))
	// 	fmt.Printf("设置文件格式的结果: %v\n", formatResult)

	// 	// 获取设置后的当前格式
	// 	currentFormat = C.zhx_GetCurrentFileFormat()
	// 	var selectedFormatName string
	// 	for _, format := range formatsList {
	// 		if int(format["value"].(float64)) == int(currentFormat) {
	// 			selectedFormatName = format["label"].(string)
	// 			break
	// 		}
	// 	}
	// 	fmt.Printf("当前设置的文件格式: %s (%d)\n", selectedFormatName, currentFormat)
	// }

	// // =========== 设置分辨率 ===========
	// fmt.Println("\n===== 分辨率设置 =====")

	// // 获取支持的分辨率
	// resolutions := C.zhx_GetSupportedResolutions()
	// resStr := C.GoString(resolutions)

	// // 解析分辨率JSON
	// var resList []map[string]interface{}
	// if err := json.Unmarshal([]byte(resStr), &resList); err != nil {
	// 	fmt.Printf("解析分辨率数据出错: %v\n", err)
	// 	// 使用默认值300 DPI
	// 	C.zhx_SetResolution(C.int(300))
	// 	fmt.Println("将使用默认分辨率 300 DPI 继续...")
	// } else {
	// 	// 显示可用分辨率选项
	// 	fmt.Println("\n可用的扫描分辨率:")
	// 	for i, res := range resList {
	// 		fmt.Printf("%d. %s\n", i+1, res["label"])
	// 	}

	// 	// 提示用户选择分辨率
	// 	var resIndex int
	// 	for {
	// 		fmt.Printf("\n请输入要使用的分辨率序号 (1-%d): ", len(resList))
	// 		_, err := fmt.Scanln(&resIndex)

	// 		if err != nil {
	// 			fmt.Scanln() // 清除输入
	// 			fmt.Println("输入无效，请重新输入数字。")
	// 			continue
	// 		}

	// 		if resIndex < 1 || resIndex > len(resList) {
	// 			fmt.Printf("序号必须在 1 到 %d 之间，请重新输入。\n", len(resList))
	// 			continue
	// 		}

	// 		break
	// 	}

	// 	// 设置用户选择的分辨率
	// 	selectedRes := int(resList[resIndex-1]["value"].(float64))
	// 	result := C.zhx_SetResolution(C.int(selectedRes))
	// 	fmt.Printf("设置分辨率为 %d DPI 的结果: %v\n", selectedRes, result)
	// }

	// // 再次获取当前分辨率确认
	// currentDPI := C.zhx_GetCurrentResolution()
	// fmt.Printf("当前扫描分辨率: %d DPI\n", currentDPI)

	// // =========== 设置扫描目录 ===========
	// fmt.Println("\n===== 扫描设置 =====")

	// // 使用当前工作目录，而不是可执行文件的目录
	// workDir, err := os.Getwd()
	// if err != nil {
	// 	fmt.Printf("获取当前工作目录出错: %v\n", err)
	// 	return
	// }

	// // 创建temp目录路径
	// tempDir1 := filepath.Join(workDir, "temp1")
	// tempDir2 := filepath.Join(workDir, "temp2")

	// fmt.Printf("\n扫描文件将保存到:\n1. %s\n2. %s\n", tempDir1, tempDir2)

	// // 确保目录存在
	// os.MkdirAll(tempDir1, 0755)
	// os.MkdirAll(tempDir2, 0755)

	// // =========== 开始扫描 ===========
	// fmt.Println("\n===== 开始扫描 =====")

	// // 询问用户是否开始扫描
	// fmt.Print("\n准备开始扫描，按Enter键继续...")
	// fmt.Scanln()

	// // 进行扫描
	// fmt.Println("\n开始第一次扫描...")
	// code := int(C.zhx_Scan(C.CString(tempDir1), C.ScanCallback(C.goFuncForScanCallBack), C.int(2)))
	// fmt.Printf("第一次扫描完成，状态码: %d\n", code)

	// // 询问是否继续
	// var continueScan string
	// fmt.Print("\n是否继续进行下一次扫描? (y/n): ")
	// fmt.Scanln(&continueScan)

	// if strings.ToLower(continueScan) == "y" || strings.ToLower(continueScan) == "yes" {
	// 	fmt.Println("\n开始第二次扫描...")
	// 	code1 := int(C.zhx_Scan(C.CString(tempDir1), C.ScanCallback(C.goFuncForScanCallBack), C.int(2)))
	// 	fmt.Printf("第二次扫描完成，状态码: %d\n", code1)

	// 	fmt.Print("\n是否继续进行第三次扫描? (y/n): ")
	// 	fmt.Scanln(&continueScan)

	// 	if strings.ToLower(continueScan) == "y" || strings.ToLower(continueScan) == "yes" {
	// 		fmt.Println("\n开始第三次扫描...")
	// 		code2 := int(C.zhx_Scan(C.CString(tempDir2), C.ScanCallback(C.goFuncForScanCallBack), C.int(2)))
	// 		fmt.Printf("第三次扫描完成，状态码: %d\n", code2)
	// 	}
	// }

	// // =========== 清理资源 ===========
	// fmt.Println("\n===== 清理资源 =====")
	// C.zhx_EndScan()
	// C.zhx_CloseDevice()
	// C.zhx_Exit()

	// // 显示扫描结果路径
	// fmt.Printf("\n扫描结果保存在:\n1和2: %s\n3: %s\n", tempDir1, tempDir2)

	// fmt.Println("\n所有操作已完成")
	// fmt.Println("按Enter键退出...")
	// fmt.Scanln()
}

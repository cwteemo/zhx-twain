package twainConfig

/*
#cgo LDFLAGS: -L.. -lTWAIN_APP_CMD64 -lstdc++
#include <stdlib.h>  // 添加这一行以使用 free 函数

void zhx_Init();
void zhx_Exit();
char *zhx_GetDevicesList();
char *zhx_GetDevCapability_STR(char *device);
char *zhx_GetCapability_STR(char* capOrDevice);
int zhx_OpenDevice(char *device);
void zhx_CloseDevice();
*/
import "C"
import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"unsafe"
)

// TWAIN 能力ID常量定义
const (
	// 基础控制能力
	CAP_DEVICEONLINE        = 4111
	CAP_INDICATORS          = 4107
	CAP_ENABLEDSUIONLY      = 4116
	CAP_UICONTROLLABLE      = 4110
	CAP_SUPPORTEDCAPS       = 4101
	CAP_CUSTOMINTERFACEGUID = 4156
	CAP_CUSTOMDSDATA        = 4117

	// 送纸器相关
	CAP_PAPERDETECTABLE = 4109
	CAP_FEEDERENABLED   = 4098
	CAP_FEEDERLOADED    = 4099
	CAP_DUPLEX          = 4114
	CAP_DUPLEXENABLED   = 4115
	CAP_AUTOFEED        = 4103

	// 图像属性和传输
	CAP_XFERCOUNT        = 1
	ICAP_BITDEPTH        = 4395
	ICAP_BITORDER        = 4380
	ICAP_COMPRESSION     = 256
	ICAP_IMAGEFILEFORMAT = 4364
	ICAP_PIXELFLAVOR     = 4383
	ICAP_PIXELTYPE       = 257
	ICAP_PLANARCHUNKY    = 4384
	ICAP_XFERMECH        = 259

	// 页面和图像尺寸
	ICAP_FRAMES         = 4372
	ICAP_MAXFRAMES      = 4378
	ICAP_PHYSICALHEIGHT = 4370
	ICAP_PHYSICALWIDTH  = 4369
	ICAP_SUPPORTEDSIZES = 4386
	ICAP_ORIENTATION    = 4368
	ICAP_UNITS          = 258

	// 分辨率
	ICAP_XNATIVERESOLUTION = 4374
	ICAP_YNATIVERESOLUTION = 4375
	ICAP_XRESOLUTION       = 4376
	ICAP_YRESOLUTION       = 4377

	// 图像增强
	ICAP_THRESHOLD  = 4387
	ICAP_CONTRAST   = 4355
	ICAP_BRIGHTNESS = 4353
	ICAP_GAMMA      = 4360

	// 自定义能力范围
	CAP_CUSTOMBASE  = 4096
	ICAP_AUTOBRIGHT = 4352
	ICAP_ZOOMFACTOR = 4391
)

// TWAIN 值标记常量
const (
	// ICAP_SUPPORTEDSIZES values
	TWSS_NONE        = 0
	TWSS_A4          = 1
	TWSS_JISB5       = 2
	TWSS_USLETTER    = 3 // US Letter
	TWSS_USLEGAL     = 4
	TWSS_A5          = 5
	TWSS_ISOB4       = 6
	TWSS_ISOB6       = 7
	TWSS_USEXECUTIVE = 9
	TWSS_A3          = 10
	TWSS_ISOB3       = 11
	TWSS_A6          = 12
	TWSS_C4          = 13
	TWSS_C5          = 14
	TWSS_C6          = 15
	TWSS_4A0         = 16
	TWSS_2A0         = 17
	TWSS_A0          = 18
	TWSS_A1          = 19
	TWSS_A2          = 20
	TWSS_A7          = 21
	TWSS_A8          = 22
	TWSS_A9          = 23
	TWSS_A10         = 24
	TWSS_ISOB0       = 25
	TWSS_ISOB1       = 26
	TWSS_ISOB2       = 27
	TWSS_ISOB5       = 28
	TWSS_ISOB7       = 29
	TWSS_ISOB8       = 30
	TWSS_ISOB9       = 31
	TWSS_ISOB10      = 32
	TWSS_JISB0       = 33
	TWSS_JISB1       = 34
	TWSS_JISB2       = 35
	TWSS_JISB3       = 36
	TWSS_JISB4       = 37
	TWSS_JISB6       = 38
	TWSS_JISB7       = 39
	TWSS_JISB8       = 40
	TWSS_JISB9       = 41
	TWSS_JISB10      = 42

	// ICAP_PIXELTYPE values
	TWPT_BW      = 0 // 黑白
	TWPT_GRAY    = 1 // 灰度
	TWPT_RGB     = 2 // RGB
	TWPT_PALETTE = 3 // 调色板
	TWPT_CMY     = 4 // CMY
	TWPT_CMYK    = 5 // CMYK
	TWPT_YUV     = 6 // YUV
	TWPT_YUVK    = 7 // YUVK
	TWPT_CIEXYZ  = 8 // CIEXYZ

	// ICAP_UNITS values
	TWUN_INCHES      = 0 // 英寸
	TWUN_CENTIMETERS = 1 // 厘米
	TWUN_PICAS       = 2 // 派卡
	TWUN_POINTS      = 3 // 点
	TWUN_TWIPS       = 4 // 缇
	TWUN_PIXELS      = 5 // 像素
	TWUN_MILLIMETERS = 6 // 毫米

	// ICAP_XFERMECH values
	TWSX_NATIVE  = 0 // 原生
	TWSX_FILE    = 1 // 文件
	TWSX_MEMORY  = 2 // 内存
	TWSX_MEMFILE = 4 // 内存文件

	// ICAP_IMAGEFILEFORMAT values
	TWFF_TIFF      = 0  // TIFF
	TWFF_PICT      = 1  // PICT
	TWFF_BMP       = 2  // BMP
	TWFF_XBM       = 3  // XBM
	TWFF_JFIF      = 4  // JPEG
	TWFF_FPX       = 5  // FlashPix
	TWFF_TIFFMULTI = 6  // Multi-page TIFF
	TWFF_PNG       = 7  // PNG
	TWFF_SPIFF     = 8  // SPIFF
	TWFF_EXIF      = 9  // EXIF
	TWFF_PDF       = 10 // PDF
	TWFF_JP2       = 11 // JPEG 2000
	TWFF_JPX       = 12 // JPEG 2000 Extended
	TWFF_DEJAVU    = 13 // DEJAVU
	TWFF_PDFA      = 14 // PDF/A
	TWFF_PDFA2     = 15 // PDF/A-2
)

// ConfigItem represents a scanner configuration item
type ConfigItem struct {
	Option      int         `json:"option"`
	Name        string      `json:"name"`  // 修改为仅包含名称部分
	Value       interface{} `json:"value"` // 修改为包含值部分
	Type        string      `json:"type"`
	Max         float64     `json:"max,omitempty"`
	Min         float64     `json:"min,omitempty"`
	Step        float64     `json:"step,omitempty"`
	Description string      `json:"description,omitempty"` // 添加一个描述字段
	List        interface{} `json:"list,omitempty"`        // 选项列表，改名为list
}

// Device represents a TWAIN scanner device
type Device struct {
	Devices []string `json:"devices"`
}

// ScannerConfigHandler processes requests for scanner configuration in JSON format
func ScannerConfigHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get query parameters
	deviceName := r.URL.Query().Get("device")

	// If no device specified, return all available devices
	if deviceName == "" {
		devices := Device{}
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		C.zhx_Init()
		defer C.zhx_Exit()
		result := C.zhx_GetDevicesList()
		deviceStr := C.GoString(result)
		deviceArr := strings.Split(deviceStr, ";")

		var validDevices []string
		for _, device := range deviceArr {
			if device != "" {
				validDevices = append(validDevices, device)
			}
		}

		devices.Devices = validDevices
		jsonData, err := json.Marshal(devices)
		if err != nil {
			http.Error(w, "Failed to marshal JSON", http.StatusInternalServerError)
			return
		}

		w.Write(jsonData)
		return
	}

	// Get configuration for the specified device
	jsonConfig, success := GetScannerConfigurationsWithDevice(deviceName)
	if !success {
		http.Error(w, "Failed to get scanner configuration", http.StatusInternalServerError)
		return
	}

	// Write the JSON directly as it's already properly formatted
	w.Write([]byte(jsonConfig))
}

// GetDeviceList 获取所有可用扫描仪列表
func GetDeviceList() []string {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	C.zhx_Init()
	defer C.zhx_Exit()

	result := C.zhx_GetDevicesList()
	deviceStr := C.GoString(result)
	deviceArr := strings.Split(deviceStr, ";")

	var validDevices []string
	for _, device := range deviceArr {
		if device != "" {
			validDevices = append(validDevices, device)
		}
	}

	return validDevices
}

// GetScannerConfigurations retrieves all configuration options from a scanner and saves them to a JSON file
// Returns true if successful, false otherwise
func GetScannerConfigurations(outputFile string) bool {
	// Lock the OS thread for CGO calls
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Initialize TWAIN environment
	C.zhx_Init()
	defer C.zhx_Exit()

	// Get available devices
	devicesList := C.zhx_GetDevicesList()
	devicesString := C.GoString(devicesList)

	// Parse semicolon-separated device list
	scanners := strings.Split(devicesString, ";")

	// Filter out empty entries
	var validScanners []string
	for _, scanner := range scanners {
		if scanner != "" {
			validScanners = append(validScanners, scanner)
		}
	}

	// Check if scanners were found
	if len(validScanners) == 0 {
		fmt.Println("No scanner devices detected!")
		return false
	}

	// Display available scanners
	fmt.Printf("Found %d scanner(s):\n", len(validScanners))
	for i, scanner := range validScanners {
		fmt.Printf("%d. %s\n", i+1, scanner)
	}

	// Get scanner selection from user
	var selectedIndex int
	for {
		fmt.Printf("\nPlease enter the scanner number to use (1-%d): ", len(validScanners))
		_, err := fmt.Scanln(&selectedIndex)

		if err != nil {
			fmt.Scanln() // Consume any remaining newline character
			fmt.Println("Invalid input, please enter a number.")
			continue
		}

		if selectedIndex < 1 || selectedIndex > len(validScanners) {
			fmt.Printf("Number must be between 1 and %d, please try again.\n", len(validScanners))
			continue
		}

		break
	}

	// Adjust index (user input is 1-n, but array index is 0-based)
	selectedIndex--

	// Get the selected scanner name
	selectedScanner := validScanners[selectedIndex]
	fmt.Printf("Selected scanner: %s\n", selectedScanner)

	jsonData, ok := fetchScannerConfig(selectedScanner)
	if !ok {
		return false
	}

	// Write to file
	err := os.WriteFile(outputFile, []byte(jsonData), 0644)
	if err != nil {
		fmt.Printf("Error writing to file: %v\n", err)
		return false
	}

	fmt.Printf("Scanner configuration has been saved to %s\n", outputFile)
	return true
}

// parseCapNameValue 从 "CAP_NAME:1234" 格式的字符串中解析名称和值
func parseCapNameValue(fullName string) (name string, value string) {
	parts := strings.Split(fullName, ":")
	if len(parts) > 1 {
		return parts[0], parts[1]
	}
	return fullName, "" // 如果无法分割，返回原始字符串和空值
}

// fetchScannerConfig 从指定的扫描仪获取配置信息
func fetchScannerConfig(deviceName string) (string, bool) {
	// Lock the OS thread for CGO calls
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Initialize TWAIN environment
	C.zhx_Init()
	defer C.zhx_Exit()

	// Open the scanner
	cScannerName := C.CString(deviceName)
	defer C.free(unsafe.Pointer(cScannerName))
	result := C.zhx_OpenDevice(cScannerName)
	if result == 0 {
		fmt.Println("Failed to open scanner!")
		return "", false
	}
	defer C.zhx_CloseDevice()

	// Get all device capabilities
	fmt.Println("Retrieving scanner capabilities...")
	capabilitiesStr := C.zhx_GetDevCapability_STR(cScannerName)
	capabilities := C.GoString(capabilitiesStr)
	fmt.Printf("Retrieved capabilities: %s\n", capabilities)

	// Split capabilities by semicolon
	capList := strings.Split(capabilities, ";")
	var validCaps []string
	for _, cap := range capList {
		if cap != "" {
			validCaps = append(validCaps, cap)
		}
	}

	// Process each capability to get its details
	var configItems []ConfigItem
	for i, capFullName := range validCaps {
		fmt.Printf("Processing capability %d/%d: %s\n", i+1, len(validCaps), capFullName)

		// 从完整名称中解析出名称和值部分
		capName, capValueStr := parseCapNameValue(capFullName)
		fmt.Printf("Parsed: name=%s, value=%s\n", capName, capValueStr)

		// 解析出能力ID值
		var capValue int
		var capDescription string

		if capValueStr != "" {
			// 尝试将字符串转换为整数
			var err error
			capValue, err = strconv.Atoi(capValueStr)
			if err == nil {
				// 成功转换为整数，获取能力的描述
				capDescription = GetCapabilityLabel(capValue)
			} else {
				// 转换失败，使用原始名称作为描述
				capDescription = capName
			}
		} else {
			// 没有值部分，使用原始名称作为描述
			capDescription = capName
		}

		// 获取该功能的选项值
		ccapValueStr := C.CString(capValueStr)
		defer C.free(unsafe.Pointer(ccapValueStr))
		optionsC := C.zhx_GetCapability_STR(ccapValueStr)
		options := C.GoString(optionsC)
		fmt.Printf("Options for %s: %s\n", capName, options)

		// 创建配置项
		configItem := ConfigItem{
			Option:      i + 1,
			Name:        capName,
			Description: capDescription,
		}

		// 如果有值部分，设置为Value
		if capValueStr != "" {
			// 尝试解析为数字
			if val, err := strconv.Atoi(capValueStr); err == nil {
				configItem.Value = val
			} else {
				configItem.Value = capValueStr
			}
		}

		// 处理选项
		if options != "" {
			// 看看是否是JSON格式
			// 在处理选项的部分添加以下代码
			if strings.HasPrefix(options, "[") {
				// 尝试解析JSON数组
				var optionItems []map[string]interface{}
				err := json.Unmarshal([]byte(options), &optionItems)
				if err == nil {
					// 成功解析JSON
					for i := range optionItems {
						// 检查是否有value字段，如果有，尝试获取其对应的标签
						if val, ok := optionItems[i]["value"]; ok {
							// 尝试将值转换为整数
							if floatVal, ok := val.(float64); ok {
								intVal := int(floatVal)
								// 获取该值对应的标签
								label := GetCapabilityValueLabel(capValue, intVal)
								// 更新label字段（如果已经存在则忽略）
								if _, hasLabel := optionItems[i]["label"]; !hasLabel || optionItems[i]["label"] == fmt.Sprintf("Value %d", intVal) {
									optionItems[i]["label"] = label
								}
							}
						}
					}
					// 重新序列化为JSON
					if updatedOptions, err := json.Marshal(optionItems); err == nil {
						options = string(updatedOptions)
					}
				}

				configItem.Type = "str"
				configItem.List = options
			}
		} else {
			configItem.Type = "str"
			configItem.List = "[]"
		}

		configItems = append(configItems, configItem)
	}

	// Convert to JSON
	jsonData, err := json.MarshalIndent(configItems, "", "  ")
	if err != nil {
		fmt.Printf("Error creating JSON: %v\n", err)
		return "", false
	}

	return string(jsonData), true
}

// GetScannerConfigurationsWithDevice retrieves all configuration options from a specified scanner and returns them as a JSON string
func GetScannerConfigurationsWithDevice(deviceName string) (string, bool) {
	return fetchScannerConfig(deviceName)
}

// GetCapabilityLabel 获取能力ID对应的说明
func GetCapabilityLabel(capValue int) string {
	switch capValue {
	// 基本控制能力
	case CAP_DEVICEONLINE:
		return "设备在线状态"
	case CAP_INDICATORS:
		return "进度指示器"
	case CAP_ENABLEDSUIONLY:
		return "仅启用DS UI"
	case CAP_UICONTROLLABLE:
		return "UI可控"
	case CAP_SUPPORTEDCAPS:
		return "支持的能力"
	case CAP_CUSTOMINTERFACEGUID:
		return "自定义接口GUID"
	case CAP_CUSTOMDSDATA:
		return "自定义DS数据"

	// 送纸器相关
	case CAP_PAPERDETECTABLE:
		return "纸张检测"
	case CAP_FEEDERENABLED:
		return "送纸器已启用"
	case CAP_FEEDERLOADED:
		return "送纸器已加载"
	case CAP_DUPLEX:
		return "双面扫描支持"
	case CAP_DUPLEXENABLED:
		return "双面扫描已启用"
	case CAP_AUTOFEED:
		return "自动送纸"

	// 图像属性和传输
	case CAP_XFERCOUNT:
		return "传输计数"
	case ICAP_BITDEPTH:
		return "位深度"
	case ICAP_BITORDER:
		return "位顺序"
	case ICAP_COMPRESSION:
		return "压缩"
	case ICAP_IMAGEFILEFORMAT:
		return "图像文件格式"
	case ICAP_PIXELFLAVOR:
		return "像素类型"
	case ICAP_PIXELTYPE:
		return "像素类型"
	case ICAP_PLANARCHUNKY:
		return "平面/块状"
	case ICAP_XFERMECH:
		return "传输机制"

	// 页面和图像尺寸
	case ICAP_FRAMES:
		return "图像帧"
	case ICAP_MAXFRAMES:
		return "最大帧数"
	case ICAP_PHYSICALHEIGHT:
		return "物理高度"
	case ICAP_PHYSICALWIDTH:
		return "物理宽度"
	case ICAP_SUPPORTEDSIZES:
		return "支持的纸张尺寸"
	case ICAP_ORIENTATION:
		return "图像方向"
	case ICAP_UNITS:
		return "测量单位"

	// 分辨率
	case ICAP_XNATIVERESOLUTION:
		return "原生X轴分辨率"
	case ICAP_YNATIVERESOLUTION:
		return "原生Y轴分辨率"
	case ICAP_XRESOLUTION:
		return "X轴分辨率"
	case ICAP_YRESOLUTION:
		return "Y轴分辨率"

	// 图像增强
	case ICAP_THRESHOLD:
		return "阈值"
	case ICAP_CONTRAST:
		return "对比度"
	case ICAP_BRIGHTNESS:
		return "亮度"
	case ICAP_GAMMA:
		return "伽玛"

	// 自定义能力
	case 4158:
		return "厂商特定功能"
	case 32769:
		return "自定义功能 1"
	case 32770:
		return "自定义功能 2"

	default:
		if capValue >= CAP_CUSTOMBASE {
			return "自定义功能"
		} else if capValue >= ICAP_AUTOBRIGHT && capValue <= ICAP_ZOOMFACTOR {
			return "图像控制功能"
		} else {
			return "基本控制功能"
		}
	}
}

// GetCapabilityValueLabel 获取能力值的标签
func GetCapabilityValueLabel(capValue int, itemValue int) string {
	// 根据能力类型返回对应值的标签
	switch capValue {
	case ICAP_SUPPORTEDSIZES:
		switch itemValue {
		case TWSS_NONE:
			return "无 (None)"
		case TWSS_A4:
			return "A4"
		case TWSS_JISB5:
			return "JIS B5"
		case TWSS_USLETTER:
			return "US Letter"
		case TWSS_USLEGAL:
			return "US Legal"
		case TWSS_A5:
			return "A5"
		case TWSS_ISOB4:
			return "ISO B4"
		case TWSS_ISOB6:
			return "ISO B6"
		case TWSS_USEXECUTIVE:
			return "US Executive"
		case TWSS_A3:
			return "A3"
		case TWSS_ISOB3:
			return "ISO B3"
		case TWSS_A6:
			return "A6"
		case TWSS_C4:
			return "C4"
		case TWSS_C5:
			return "C5"
		case TWSS_C6:
			return "C6"
		case TWSS_4A0:
			return "4A0"
		case TWSS_2A0:
			return "2A0"
		case TWSS_A0:
			return "A0"
		case TWSS_A1:
			return "A1"
		case TWSS_A2:
			return "A2"
		case TWSS_A7:
			return "A7"
		case TWSS_A8:
			return "A8"
		case TWSS_A9:
			return "A9"
		case TWSS_A10:
			return "A10"
		case TWSS_ISOB0:
			return "ISO B0"
		case TWSS_ISOB1:
			return "ISO B1"
		case TWSS_ISOB2:
			return "ISO B2"
		case TWSS_ISOB5:
			return "ISO B5"
		case TWSS_ISOB7:
			return "ISO B7"
		case TWSS_ISOB8:
			return "ISO B8"
		case TWSS_ISOB9:
			return "ISO B9"
		case TWSS_ISOB10:
			return "ISO B10"
		case TWSS_JISB0:
			return "JIS B0"
		case TWSS_JISB1:
			return "JIS B1"
		case TWSS_JISB2:
			return "JIS B2"
		case TWSS_JISB3:
			return "JIS B3"
		case TWSS_JISB4:
			return "JIS B4"
		case TWSS_JISB6:
			return "JIS B6"
		case TWSS_JISB7:
			return "JIS B7"
		case TWSS_JISB8:
			return "JIS B8"
		case TWSS_JISB9:
			return "JIS B9"
		case TWSS_JISB10:
			return "JIS B10"
		default:
			return "未知纸张尺寸 (Unknown Paper Size)"
		}

	case ICAP_PIXELTYPE:
		switch itemValue {
		case TWPT_BW:
			return "黑白 (Black & White)"
		case TWPT_GRAY:
			return "灰度 (Grayscale)"
		case TWPT_RGB:
			return "RGB"
		case TWPT_PALETTE:
			return "调色板 (Palette)"
		case TWPT_CMY:
			return "CMY"
		case TWPT_CMYK:
			return "CMYK"
		case TWPT_YUV:
			return "YUV"
		case TWPT_YUVK:
			return "YUVK"
		case TWPT_CIEXYZ:
			return "CIEXYZ"
		default:
			return "未知像素类型 (Unknown Pixel Type)"
		}

	case ICAP_UNITS:
		switch itemValue {
		case TWUN_INCHES:
			return "英寸 (Inches)"
		case TWUN_CENTIMETERS:
			return "厘米 (Centimeters)"
		case TWUN_PICAS:
			return "派卡 (Picas)"
		case TWUN_POINTS:
			return "点 (Points)"
		case TWUN_TWIPS:
			return "缇 (Twips)"
		case TWUN_PIXELS:
			return "像素 (Pixels)"
		case TWUN_MILLIMETERS:
			return "毫米 (Millimeters)"
		default:
			return "未知单位 (Unknown Unit)"
		}

	case ICAP_XFERMECH:
		switch itemValue {
		case TWSX_NATIVE:
			return "原生 (Native)"
		case TWSX_FILE:
			return "文件 (File)"
		case TWSX_MEMORY:
			return "内存 (Memory)"
		case TWSX_MEMFILE:
			return "内存文件 (Memory File)"
		default:
			return "未知传输机制 (Unknown Transfer Mechanism)"
		}

	case ICAP_IMAGEFILEFORMAT:
		switch itemValue {
		case TWFF_TIFF:
			return "TIFF"
		case TWFF_PICT:
			return "PICT"
		case TWFF_BMP:
			return "BMP"
		case TWFF_XBM:
			return "XBM"
		case TWFF_JFIF:
			return "JPEG"
		case TWFF_FPX:
			return "FlashPix"
		case TWFF_TIFFMULTI:
			return "多页TIFF (Multi-page TIFF)"
		case TWFF_PNG:
			return "PNG"
		case TWFF_SPIFF:
			return "SPIFF"
		case TWFF_EXIF:
			return "EXIF"
		case TWFF_PDF:
			return "PDF"
		case TWFF_JP2:
			return "JPEG 2000"
		case TWFF_JPX:
			return "JPEG 2000 Extended"
		case TWFF_DEJAVU:
			return "DEJAVU"
		case TWFF_PDFA:
			return "PDF/A"
		case TWFF_PDFA2:
			return "PDF/A-2"
		default:
			return "未知文件格式 (Unknown File Format)"
		}

	default:
		// 对于其他能力值，直接返回数值的字符串表示
		return fmt.Sprintf("值 %d (Value %d)", itemValue, itemValue)
	}
}

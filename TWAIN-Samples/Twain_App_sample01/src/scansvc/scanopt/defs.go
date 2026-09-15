package scanopt

// 选项定义表。**新增 / 调整一项选项只需要改这个文件。**
//
// 每条定义：
//   Name        前端的 name。能和前端已有命名对上的就沿用（前端的中文标签表按 name 查），
//               对不上的新起一个短横线命名。
//   Caps        要读的 TWAIN 能力编号。Build 收到的参数和它一一对应、顺序相同，
//               设备不支持的那一位是 nil。
//   VirtualOnly 只对虚拟扫描仪读。0x8000 以上的自定义能力含义各厂商自己定，
//               别的设备上同一个编号可能是另一回事，必须打这个标记。
//   Build       拼出选项；返回 nil 表示略过这一项。Option / Name 由 Build() 统一填，这里不用管。
//               常用的现成写法：
//                 listOf(labels)  枚举值 → 下拉框，编号按 labels 翻成文字
//                 number          数值，形态跟着设备报的容器走（区间→滑块，枚举→下拉，单值→数字框）
//                 boolean         勾选框
//
// 例：加一项"自动进纸"勾选框
//
//	{Name: "auto-feed", Caps: []uint16{CapAutoFeed}, Build: boolean},
//
// option 编号 = 在表里的序号 + 1。**往末尾追加**不会影响已有项的编号；
// 插到中间会让后面的项改号——前端按 name 取值不受影响，但尽量别这么做。
//
// 列哪些项：目前照着虚拟扫描仪（Twain_DS_sample01）自带设置面板上的项来。

// TWAIN 能力编号，见 pub/external/include/twain.h。
const (
	CapPixelType     uint16 = 0x0101 // ICAP_PIXELTYPE
	CapUnits         uint16 = 0x0102 // ICAP_UNITS
	CapFeederEnabled uint16 = 0x1002 // CAP_FEEDERENABLED
	CapAutoFeed      uint16 = 0x1007 // CAP_AUTOFEED
	CapDuplex        uint16 = 0x1012 // CAP_DUPLEX，设备双面能力，只读
	CapDuplexEnabled uint16 = 0x1013 // CAP_DUPLEXENABLED
	CapBrightness    uint16 = 0x1101 // ICAP_BRIGHTNESS
	CapContrast      uint16 = 0x1103 // ICAP_CONTRAST
	CapGamma         uint16 = 0x1108 // ICAP_GAMMA
	CapOrientation   uint16 = 0x1110 // ICAP_ORIENTATION
	CapXResolution   uint16 = 0x1118 // ICAP_XRESOLUTION
	CapSupportedSize uint16 = 0x1122 // ICAP_SUPPORTEDSIZES
	CapThreshold     uint16 = 0x1123 // ICAP_THRESHOLD

	// 虚拟扫描仪的自定义能力（Twain_DS_sample01/src/CTWAINDS_FreeImage.h）。
	CapLongDocument uint16 = 0x8001 // CUSTCAP_LONGDOCUMENT
	CapDocsInADF    uint16 = 0x8002 // CUSTCAP_DOCS_IN_ADF
)

// Def 是一条选项定义，字段说明见文件头。
type Def struct {
	Name        string
	Caps        []uint16
	VirtualOnly bool
	Build       func(caps []*Cap) *Option
}

// Defs 是全部选项定义，顺序即返回顺序。
var Defs = []Def{
	{Name: "source", Caps: []uint16{CapFeederEnabled, CapDuplex, CapDuplexEnabled}, Build: buildSource},
	{Name: "mode", Caps: []uint16{CapPixelType}, Build: listOf(pixelTypeLabels)},
	{Name: "resolution", Caps: []uint16{CapXResolution}, Build: number},
	{Name: "paper-size", Caps: []uint16{CapSupportedSize}, Build: listOf(paperSizeLabels)},
	{Name: "rotate", Caps: []uint16{CapOrientation}, Build: listOf(orientationLabels)},
	{Name: "units", Caps: []uint16{CapUnits}, Build: listOf(unitLabels)},
	{Name: "brightness", Caps: []uint16{CapBrightness}, Build: number},
	{Name: "contrast", Caps: []uint16{CapContrast}, Build: number},
	{Name: "threshold", Caps: []uint16{CapThreshold}, Build: number},
	{Name: "gamma", Caps: []uint16{CapGamma}, Build: number},
	{Name: "long-paper-scan", Caps: []uint16{CapLongDocument}, VirtualOnly: true, Build: boolean},
	{Name: "documents-in-adf", Caps: []uint16{CapDocsInADF}, VirtualOnly: true, Build: number},
}

// ---- 取值文字 ----
//
// 能用前端 temporaryOptions 里已有的英文词就用，那边按这些词查中文标签。
// 不在表里的编号会原样输出成数字字符串，不会丢项。

var pixelTypeLabels = map[int]string{ // TWPT_*
	0: "Lineart", 1: "Gray", 2: "Color", 3: "Palette", 4: "CMY", 5: "CMYK",
}

// TODO(TODO-settings.md #17): 90 / 270 的方向没核对。
var orientationLabels = map[int]string{ // TWOR_*
	0: "None", 1: "90", 2: "180", 3: "270",
}

var unitLabels = map[int]string{ // TWUN_*
	0: "Inches", 1: "Centimeters", 2: "Picas", 3: "Points", 4: "Twips", 5: "Pixels", 6: "Millimeters",
}

var paperSizeLabels = map[int]string{ // TWSS_*，只列常见的
	0: "None", 1: "A4", 2: "JIS B5", 3: "US Letter", 4: "US Legal", 5: "A5",
	6: "ISO B4", 7: "ISO B6", 9: "US Ledger", 10: "US Executive", 11: "A3", 13: "A6",
	29: "ISO B5", 52: "US Statement", 53: "Business Card",
}

// ---- 需要组合多项能力的选项 ----

// buildSource 把"送纸器开关 + 双面开关"合成前端的 source 一项，
// 取值沿用前端已有的 Flatbed / ADF Front / ADF Duplex。
// 参数顺序同 Defs 里的 Caps：feederEnabled, duplex, duplexEnabled。
// 设备报 CAP_DUPLEX 为 0（不支持双面）时不给 ADF Duplex。
func buildSource(caps []*Cap) *Option {
	feeder, duplex, duplexEnabled := caps[0], caps[1], caps[2]

	feederOn, ok := feeder.Current()
	if !ok {
		return nil
	}

	list := []string{"Flatbed", "ADF Front"}
	duplexCapable := false
	if v, ok := duplex.Current(); ok && v != 0 {
		duplexCapable = true
		list = append(list, "ADF Duplex")
	}

	value := "Flatbed"
	if feederOn != 0 {
		value = "ADF Front"
		if v, ok := duplexEnabled.Current(); duplexCapable && ok && v != 0 {
			value = "ADF Duplex"
		}
	}
	return &Option{Type: TypeStrList, Value: value, List: list}
}

// Package scanopt 把 TWAIN 能力翻译成既有前端的"扫描仪选项"模型。
//
// 前端模型（court-document-processing 加工/通用 分支，ScanCom/Scan.js 里的开发假数据）：
//
//	{"option":2,"name":"mode","type":"str-list","value":"Color","list":["Lineart","Gray","Color"]}
//	{"option":3,"name":"resolution","type":"int-range","value":300,"min":75,"max":1200,"step":1}
//	{"option":4,"name":"preview","type":"bool","value":0}
//
// 这是 SANE 的选项体系，不是 TWAIN 的 CAP。type 只认前端 SetScanBase 组件支持的
// str-list / int-range / bool / int / str / button——没有 int-list。
//
// 本包是纯 Go，不碰 cgo / TWAIN：输入是 DLL zhx_GetCapability_STR 吐出的原始 JSON，
// 输出是选项列表。所以不接扫描仪、在 Linux 上也能 go test ./scanopt。
//
// 用法（见上层 options.go）：
//
//	codes := scanopt.Codes(device)          // 这台设备要读哪些能力
//	raw   := 在 TWAIN 线程上逐项读 codes     // 编号 -> 原始 JSON
//	opts  := scanopt.Build(raw, device)     // 拼成前端要的列表
//
// 新增一项选项：只改 defs.go 里的 Defs 表，见那里的说明。
package scanopt

import (
	"strings"
)

// Option 是前端选项模型里的一项。
type Option struct {
	Option int      `json:"option"`
	Name   string   `json:"name"`
	Type   string   `json:"type"`
	Value  any      `json:"value,omitempty"`
	List   []string `json:"list,omitempty"`
	Min    *float64 `json:"min,omitempty"`
	Max    *float64 `json:"max,omitempty"`
	Step   *float64 `json:"step,omitempty"`
}

// 前端 SetScanBase 组件认的 type。
const (
	TypeStrList  = "str-list"
	TypeIntRange = "int-range"
	TypeBool     = "bool"
	TypeInt      = "int"
)

// virtualScannerName 是虚拟扫描仪（Twain_DS_sample01）ProductName 里的固定片段。
const virtualScannerName = "Software Scanner"

// IsVirtualScanner 判断设备是不是虚拟扫描仪。
func IsVirtualScanner(device string) bool {
	return strings.Contains(device, virtualScannerName)
}

// applies 判断某条定义对这台设备是否适用。
func (d Def) applies(device string) bool {
	return !d.VirtualOnly || IsVirtualScanner(device)
}

// Codes 返回这台设备要读的全部能力编号，去重、保持 Defs 里的先后顺序。
func Codes(device string) []uint16 {
	seen := map[uint16]bool{}
	var out []uint16
	for _, d := range Defs {
		if !d.applies(device) {
			continue
		}
		for _, c := range d.Caps {
			if !seen[c] {
				seen[c] = true
				out = append(out, c)
			}
		}
	}
	return out
}

// Build 把原始能力 JSON（编号 -> DLL 输出）拼成选项列表。
// raw 里缺的、读不到的、解析失败的能力都按"设备不支持"处理，对应的选项直接略过。
// 返回值永远不是 nil：前端判的是 `if (!options)`，null 会弹"请关闭其他扫描程序"。
//
// option 编号 = 在 Defs 里的序号 + 1，某项被略过不会让后面的项改号。
func Build(raw map[uint16]string, device string) []Option {
	caps := make(map[uint16]*Cap, len(raw))
	for code, s := range raw {
		if c := ParseCap(code, s); c != nil {
			caps[code] = c
		}
	}

	options := make([]Option, 0, len(Defs))
	for i, d := range Defs {
		if !d.applies(device) {
			continue
		}
		args := make([]*Cap, len(d.Caps))
		for j, code := range d.Caps {
			args[j] = caps[code]
		}
		if opt := d.Build(args); opt != nil {
			opt.Option = i + 1
			opt.Name = d.Name
			options = append(options, *opt)
		}
	}
	return options
}

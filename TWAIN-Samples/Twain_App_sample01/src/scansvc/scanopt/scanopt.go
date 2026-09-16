// Package scanopt 是扫描仪设置的中间层：把各家扫描仪五花八门的 TWAIN 能力，
// 翻译成一套固定的"设置项"协议给前端；前端改了设置，再翻译回 TWAIN 能力下发。
//
// 前端只认协议（见 ../SCANNER_OPTIONS_API.md），不知道 TWAIN 的存在：
//
//	{"key":"colorMode","label":"颜色模式","control":"radio","value":"color",
//	 "choices":[{"value":"bw","label":"黑白"},{"value":"gray","label":"灰度"},{"value":"color","label":"彩色"}]}
//
// **有哪些设置项、每项对应哪个能力、下拉框有哪些选项，全部写在声明式的配置里**：
// default_options.jsonc，编译进 exe，是唯一来源（现场的 scanner-options.jsonc 只作应急覆盖，见 ../options.go）。
// 写法见 ../SCANNER_OPTIONS_CONFIG.md。增删设置项、增删选项都只改配置，不写 Go；
// 只有要一种现有四种类型（enum / number / switch / combo）都表达不了的新类型时才改代码。
// 改完跑 go test ./scanopt：真实设备的回归用例（testdata/devices）会检查已适配的机型有没有被改坏。
//
// 本包是纯 Go，不碰 cgo：输入是 DLL zhx_GetCapability_STR 吐出的原始 JSON，
// 输出是设置项 / 要下发的能力值。所以不接扫描仪、在 Linux 上也能 go test ./scanopt。
//
// 用法（见上层 options.go）：
//
//	配置：reg, err := scanopt.Compile(文件内容); scanopt.Use(reg)
//	读：  codes := scanopt.Codes(device)            // 这台设备要读哪些能力
//	      raw   := 在 TWAIN 线程上逐项读 codes       // 编号 -> 原始 JSON
//	      opts  := scanopt.Build(raw, device)       // 拼成设置项列表
//	写：  for _, d := range scanopt.DefsFor(device) // 按配置顺序逐项处理
//	          raw := 读 d.Caps                        // 每项现读，前一项可能改变后一项的可选值
//	          writes, err := d.Plan(value, d.ParseCaps(raw))
//	          逐条下发 writes
package scanopt

import (
	_ "embed"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
)

// ProtocolVersion 是设置项协议（给前端的结构）的版本号，结构有不兼容的改动时加一。
const ProtocolVersion = 1

// control 的取值，前端按它决定画什么控件。只增不改。
const (
	ControlSelect = "select" // 下拉框，配 choices
	ControlRadio  = "radio"  // 单选按钮组，配 choices（选项少时用）
	ControlSwitch = "switch" // 开关，value 是 true / false
	ControlSlider = "slider" // 滑块，配 min / max / step
	ControlNumber = "number" // 数字输入框，min / max / step 可能没有
)

// group 的取值。前端可以按它分区显示，也可以忽略。
const (
	GroupBasic    = "basic"    // 基础设置
	GroupAdvanced = "advanced" // 高级设置
)

// DefaultConfig 是内置配置原文，设置项定义的唯一来源。
//
//go:embed default_options.jsonc
var DefaultConfig []byte

// Choice 是下拉框 / 单选组里的一个选项。
type Choice struct {
	Value any    `json:"value"`
	Label string `json:"label"`
}

// Option 是一个设置项，字段含义见 SCANNER_OPTIONS_API.md。
type Option struct {
	Key     string   `json:"key"`
	Label   string   `json:"label"`
	Group   string   `json:"group"`
	Control string   `json:"control"`
	Value   any      `json:"value"`
	Choices []Choice `json:"choices,omitempty"`
	Min     *float64 `json:"min,omitempty"`
	Max     *float64 `json:"max,omitempty"`
	Step    *float64 `json:"step,omitempty"`
	Unit    string   `json:"unit,omitempty"`
}

// CapWrite 是一条要下发的能力值。Value 是 DLL zhx_SetCapability_STR 认的字符串：
// 整数 / 小数写数字，布尔写 "1" / "0"。
type CapWrite struct {
	Code  uint16
	Value string
}

// Def 是编译好的一个设置项：怎么从能力读出来、怎么写回去。由配置文件生成。
type Def struct {
	Key   string
	Label string
	Group string
	// Caps 是要读的能力编号。Read / Plan 收到的 caps 和它一一对应、顺序相同，
	// 设备不支持的那一位是 nil。
	Caps []uint16
	// Read 从能力拼出设置项（只填 Control / Value / Choices / Min / Max / Step / Unit），
	// 返回 nil 表示设备不支持，这一项不输出。Key / Label / Group 由 Build 统一填。
	Read func(caps []*Cap) *Option
	// Plan 校验前端传来的取值，算出要下发的能力值。取值不合法返回 error（给用户看的中文）。
	Plan func(value any, caps []*Cap) ([]CapWrite, error)
}

// ParseCaps 把一组原始 JSON 按 d.Caps 的顺序解析好，给 Read / Plan 用。
func (d Def) ParseCaps(raw map[uint16]string) []*Cap {
	out := make([]*Cap, len(d.Caps))
	for i, code := range d.Caps {
		if s, ok := raw[code]; ok {
			out[i] = ParseCap(code, s)
		}
	}
	return out
}

// Build 用这份原始数据拼出这一项；设备不支持返回 nil。
func (d Def) Build(raw map[uint16]string) *Option {
	opt := d.Read(d.ParseCaps(raw))
	if opt == nil {
		return nil
	}
	opt.Key, opt.Label, opt.Group = d.Key, d.Label, d.Group
	return opt
}

// Profile 是针对某类设备的特殊处理，由配置文件的 profiles 生成。
type Profile struct {
	Name  string
	Match []string // 设备名包含其中任意一个（不区分大小写）就算命中
	Hide  []string // 这类设备上不输出的 key
	Defs  []Def    // 按 Key 替换通用定义；Key 不存在时追加到末尾
}

func (p Profile) matches(device string) bool {
	d := strings.ToLower(device)
	for _, m := range p.Match {
		if strings.Contains(d, strings.ToLower(m)) {
			return true
		}
	}
	return false
}

// Registry 是一份编译好的配置。
type Registry struct {
	Defs     []Def
	Profiles []Profile
}

// DefsFor 返回这台设备生效的定义：通用 Defs 叠加命中的 Profiles。
func (r *Registry) DefsFor(device string) []Def {
	defs := append([]Def(nil), r.Defs...)
	for _, p := range r.Profiles {
		if !p.matches(device) {
			continue
		}
		for _, pd := range p.Defs {
			replaced := false
			for i := range defs {
				if defs[i].Key == pd.Key {
					defs[i] = pd
					replaced = true
					break
				}
			}
			if !replaced {
				defs = append(defs, pd)
			}
		}
		if len(p.Hide) > 0 {
			kept := defs[:0]
			for _, d := range defs {
				if !containsString(p.Hide, d.Key) {
					kept = append(kept, d)
				}
			}
			defs = kept
		}
	}
	return defs
}

// ---- 当前生效的配置 ----

var (
	registryMu sync.RWMutex
	registry   *Registry
	builtin    *Registry
)

func init() {
	r, err := Compile(DefaultConfig)
	if err != nil {
		// 内置配置写错是开发期的问题，go test 会先炸在这里。
		panic("scanopt: 内置默认配置有误: " + err.Error())
	}
	registry, builtin = r, r
}

// Builtin 返回编译好的内置配置。
func Builtin() *Registry {
	return builtin
}

// Use 切换当前生效的配置。
func Use(r *Registry) {
	registryMu.Lock()
	registry = r
	registryMu.Unlock()
}

// Current 返回当前生效的配置。
func Current() *Registry {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return registry
}

// DefsFor 用当前配置取这台设备生效的定义。
func DefsFor(device string) []Def {
	return Current().DefsFor(device)
}

// Codes 返回这台设备要读的全部能力编号，去重、保持定义里的先后顺序。
func Codes(device string) []uint16 {
	seen := map[uint16]bool{}
	var out []uint16
	for _, d := range DefsFor(device) {
		for _, c := range d.Caps {
			if !seen[c] {
				seen[c] = true
				out = append(out, c)
			}
		}
	}
	return out
}

// Build 把原始能力 JSON（编号 -> DLL 输出）拼成设置项列表，用当前生效的配置。
// 读不到、解析失败的能力按"设备不支持"处理，对应的项直接不输出。
// 返回值永远不是 nil，序列化出来是 []。
func Build(raw map[uint16]string, device string) []Option {
	return Current().build(raw, device)
}

func (r *Registry) build(raw map[uint16]string, device string) []Option {
	defs := r.DefsFor(device)
	options := make([]Option, 0, len(defs))
	for _, d := range defs {
		if opt := d.Build(raw); opt != nil {
			options = append(options, *opt)
		}
	}
	return options
}

// ---- 取值规整 ----
//
// 前端传来的值经过 JSON 解码，数字是 float64、布尔是 bool；但前端表单里拿到的
// 经常是字符串（"300"、"true"），这里都认，免得前端到处转类型。

func toNumber(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f, err == nil
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

func toBool(v any) (bool, bool) {
	switch x := v.(type) {
	case bool:
		return x, true
	case float64:
		return x != 0, true
	case string:
		switch strings.ToLower(strings.TrimSpace(x)) {
		case "true", "1", "on", "yes":
			return true, true
		case "false", "0", "off", "no":
			return false, true
		}
	}
	return false, false
}

func toStr(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case float64:
		return formatNumber(x), true
	case int:
		return strconv.Itoa(x), true
	}
	return "", false
}

// sameValue 比较配置里的取值和前端传来的取值：都能当数字的按数字比（300 和 "300" 相等），
// 否则按字符串比。
func sameValue(a, b any) bool {
	if x, ok := a.(float64); ok {
		if y, ok := toNumber(b); ok {
			return sameNumber(x, y)
		}
		return false
	}
	sa, okA := toStr(a)
	sb, okB := toStr(b)
	return okA && okB && sa == sb
}

// sameNumber 比较两个数值，容忍 FIX32 带来的小数误差（1/65536 量级）。
func sameNumber(a, b float64) bool {
	return math.Abs(a-b) < 0.001
}

func formatNumber(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// describe 把取值格式化进错误信息。
func describe(v any) string {
	if s, ok := v.(string); ok {
		return strconv.Quote(s)
	}
	return fmt.Sprint(v)
}

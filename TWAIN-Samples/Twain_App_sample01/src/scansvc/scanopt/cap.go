package scanopt

// 解析 DLL zhx_GetCapability_STR 的输出，以及几种通用的选项拼法。

import (
	"encoding/json"
	"log"
	"strconv"
)

// Cap 对应 zhx_GetCapability_STR 输出数组里的那一个对象。
// 三种容器的字段混在一起，按 Container 取用。输出格式见 src/main.cpp。
type Cap struct {
	Container    string          `json:"container"` // ONEVALUE / ENUMERATION / RANGE / ARRAY
	ItemType     int             `json:"itemType"`
	Value        json.RawMessage `json:"value"`        // ONEVALUE
	CurrentIndex int             `json:"currentIndex"` // ENUMERATION
	Items        []struct {
		Value     json.RawMessage `json:"value"`
		IsCurrent bool            `json:"isCurrent"`
	} `json:"items"`
	MinValue     float64 `json:"minValue"` // RANGE
	MaxValue     float64 `json:"maxValue"`
	StepSize     float64 `json:"stepSize"`
	CurrentValue float64 `json:"currentValue"`
}

// ParseCap 解析一项能力。读不到（空数组）或解析失败返回 nil，调用方按"设备不支持"处理。
func ParseCap(code uint16, s string) *Cap {
	var arr []Cap
	if err := json.Unmarshal([]byte(s), &arr); err != nil {
		// DLL 是 sprintf 手拼的 JSON，字符串类能力里带引号、或者超出 4096 字节缓冲区都会拼坏。
		log.Printf("能力 0x%04x 的返回解析失败: %v（原文: %.200s）", code, err, s)
		return nil
	}
	if len(arr) == 0 {
		return nil
	}
	return &arr[0]
}

// Current 取当前值。c 为 nil（设备不支持）时返回 false，调用方不用先判空。
func (c *Cap) Current() (float64, bool) {
	if c == nil {
		return 0, false
	}
	switch c.Container {
	case "ONEVALUE":
		return rawNumber(c.Value)
	case "ENUMERATION":
		for _, it := range c.Items {
			if it.IsCurrent {
				return rawNumber(it.Value)
			}
		}
		if c.CurrentIndex >= 0 && c.CurrentIndex < len(c.Items) {
			return rawNumber(c.Items[c.CurrentIndex].Value)
		}
	case "RANGE":
		return c.CurrentValue, true
	}
	return 0, false
}

// Choices 取可选值列表（ARRAY 取全部项）。RANGE 没有列表，返回 nil。
func (c *Cap) Choices() []float64 {
	if c == nil {
		return nil
	}
	switch c.Container {
	case "ENUMERATION", "ARRAY":
		out := make([]float64, 0, len(c.Items))
		for _, it := range c.Items {
			if v, ok := rawNumber(it.Value); ok {
				out = append(out, v)
			}
		}
		return out
	case "ONEVALUE":
		if v, ok := rawNumber(c.Value); ok {
			return []float64{v}
		}
	}
	return nil
}

// ---- 通用拼法，供 Defs 引用 ----

// listOf 返回一个"枚举值 → 下拉框"的拼法，编号按 labels 翻成文字，不认识的原样出数字。
func listOf(labels map[int]string) func([]*Cap) *Option {
	label := func(v float64) string {
		if s, ok := labels[int(v)]; ok {
			return s
		}
		return formatNumber(v)
	}
	return func(caps []*Cap) *Option {
		c := caps[0]
		cur, ok := c.Current()
		if !ok {
			return nil
		}
		choices := c.Choices()
		list := make([]string, 0, len(choices))
		for _, v := range choices {
			list = append(list, label(v))
		}
		return &Option{Type: TypeStrList, Value: label(cur), List: list}
	}
}

// number 生成数值选项，形态跟着设备报的容器走：
// RANGE → int-range（滑块）；ENUMERATION → str-list（前端没有 int-list）；ONEVALUE → int。
func number(caps []*Cap) *Option {
	c := caps[0]
	cur, ok := c.Current()
	if !ok {
		return nil
	}
	switch c.Container {
	case "RANGE":
		min, max, step := c.MinValue, c.MaxValue, c.StepSize
		return &Option{Type: TypeIntRange, Value: cur, Min: &min, Max: &max, Step: &step}
	case "ENUMERATION":
		choices := c.Choices()
		list := make([]string, 0, len(choices))
		for _, v := range choices {
			list = append(list, formatNumber(v))
		}
		return &Option{Type: TypeStrList, Value: formatNumber(cur), List: list}
	default:
		return &Option{Type: TypeInt, Value: cur}
	}
}

// boolean 生成勾选项。前端的 checkbox 用 1 / 0 表示真假，不是 true / false。
func boolean(caps []*Cap) *Option {
	cur, ok := caps[0].Current()
	if !ok {
		return nil
	}
	value := 0
	if cur != 0 {
		value = 1
	}
	return &Option{Type: TypeBool, Value: value}
}

// rawNumber 把 DLL 输出的值转成数字。BOOL 类型输出的是 true / false，按 1 / 0 算；
// FRAME、字符串这类不是数字的返回 false。
func rawNumber(raw json.RawMessage) (float64, bool) {
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		return f, true
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil {
		if b {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

// formatNumber 去掉 FIX32 带出来的多余小数：200.0000 → "200"，12.5 → "12.5"。
func formatNumber(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

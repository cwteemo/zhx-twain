package scanopt

// 解析 DLL zhx_GetCapability_STR 的输出。

import (
	"encoding/json"
	"log"
)

// Cap 对应 zhx_GetCapability_STR 输出数组里的那一个对象。
// 几种容器的字段混在一起，按 Container 取用。输出格式见 src/main.cpp。
type Cap struct {
	Code         uint16          `json:"-"`         // 能力编号，ParseCap 填
	Container    string          `json:"container"` // ONEVALUE / ENUMERATION / RANGE / ARRAY
	ItemType     int             `json:"itemType"`
	Value        json.RawMessage `json:"value"`        // ONEVALUE
	CurrentIndex int             `json:"currentIndex"` // ENUMERATION
	Items        []struct {
		Value     json.RawMessage `json:"value"`
		IsCurrent bool            `json:"isCurrent"`
	} `json:"items"`
	DefaultIndex int     `json:"defaultIndex"` // ENUMERATION
	MinValue     float64 `json:"minValue"`     // RANGE
	MaxValue     float64 `json:"maxValue"`
	StepSize     float64 `json:"stepSize"`
	CurrentValue float64 `json:"currentValue"`
}

// ParseCap 解析一项能力。读不到（空数组）或解析失败返回 nil，调用方按"设备不支持"处理。
func ParseCap(code uint16, s string) *Cap {
	var arr []Cap
	if err := json.Unmarshal([]byte(s), &arr); err != nil {
		// DLL 手拼的 JSON；旧版 DLL 字符串不转义、结果超过 4096 字节会拼坏。
		log.Printf("能力 0x%04x 的返回解析失败: %v（原文: %.200s）", code, err, s)
		return nil
	}
	if len(arr) == 0 {
		return nil
	}
	arr[0].Code = code
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

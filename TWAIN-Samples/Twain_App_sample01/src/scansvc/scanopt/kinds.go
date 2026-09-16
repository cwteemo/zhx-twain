package scanopt

// 四种设置项类型（配置文件里的 kind）的读写实现。
//
//	enum    一个能力的枚举值 ↔ 下拉框 / 单选组。例：颜色模式、纸张尺寸
//	number  一个数值能力（可以同时写多个）↔ 滑块 / 下拉 / 数字框。例：亮度、分辨率
//	switch  一个布尔能力 ↔ 开关。例：自动纠偏
//	combo   每个选项对应"同时设几个能力"↔ 下拉框 / 单选组。例：扫描方式（送纸器 + 双面）
//
// 各类型在配置里怎么写见 SCANNER_OPTIONS_CONFIG.md。配置解析在 config.go。

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// ---- enum ----

type enumChoice struct {
	Code  float64
	Value any
	Label string
}

type enumKind struct {
	control      string
	choices      []enumChoice // 配置里的顺序就是下拉框里的顺序
	showUnknown  bool
	unknownLabel string // 含 {code} 占位符
}

func (k enumKind) byCode(code float64) (enumChoice, bool) {
	for _, c := range k.choices {
		if sameNumber(c.Code, code) {
			return c, true
		}
	}
	return enumChoice{}, false
}

// itemFor 返回编号对应的选项；配置里没有的给一个 code<编号> 的兜底。
func (k enumKind) itemFor(code float64) enumChoice {
	if c, ok := k.byCode(code); ok {
		return c
	}
	n := formatNumber(code)
	return enumChoice{Code: code, Value: "code" + n, Label: strings.ReplaceAll(k.unknownLabel, "{code}", n)}
}

func (k enumKind) read(caps []*Cap) *Option {
	c := caps[0]
	cur, ok := c.Current()
	if !ok {
		return nil
	}
	supported := c.Choices()

	choices := []Choice{}
	seen := map[string]bool{}
	add := func(it enumChoice) {
		key := fmt.Sprint(it.Value)
		if !seen[key] {
			seen[key] = true
			choices = append(choices, Choice{Value: it.Value, Label: it.Label})
		}
	}
	// 先按配置顺序列设备支持的，再补设备报了、配置里没写的（showUnknown 时）。
	for _, it := range k.choices {
		if containsNumber(supported, it.Code) {
			add(it)
		}
	}
	if k.showUnknown {
		for _, code := range supported {
			if _, known := k.byCode(code); !known {
				add(k.itemFor(code))
			}
		}
	}
	// 当前值必须在选项里，否则前端的下拉框对不上。
	current := k.itemFor(cur)
	add(current)
	return &Option{Control: k.control, Value: current.Value, Choices: choices}
}

func (k enumKind) plan(v any, caps []*Cap) ([]CapWrite, error) {
	c := caps[0]
	if c == nil {
		return nil, fmt.Errorf("该扫描仪不支持此项")
	}
	code, label, found := 0.0, "", false
	for _, it := range k.choices {
		if sameValue(it.Value, v) {
			code, label, found = it.Code, it.Label, true
			break
		}
	}
	if !found {
		var n float64
		if s, ok := v.(string); ok {
			if _, err := fmt.Sscanf(s, "code%g", &n); err == nil {
				code, label, found = n, s, true
			}
		}
	}
	if !found {
		return nil, fmt.Errorf("不认识的取值 %s", describe(v))
	}
	if !capAllows(c, code) {
		return nil, fmt.Errorf("该扫描仪不支持「%s」", label)
	}
	return []CapWrite{{Code: c.Code, Value: formatNumber(code)}}, nil
}

// ---- number ----

type numberKind struct {
	codes       []uint16 // 读第一个，写全部（设备不支持的跳过，第一个除外）
	control     string   // auto / slider / select / number
	presets     []float64
	choiceLabel string // 含 {value} 占位符；空则直接显示数字
	unit        string
}

func (k numberKind) label(v float64) string {
	if k.choiceLabel == "" {
		return formatNumber(v)
	}
	return strings.ReplaceAll(k.choiceLabel, "{value}", formatNumber(v))
}

func (k numberKind) selectOf(values []float64, cur float64) *Option {
	if !containsNumber(values, cur) {
		values = append(values, cur)
	}
	sort.Float64s(values)
	choices := make([]Choice, 0, len(values))
	for _, v := range values {
		choices = append(choices, Choice{Value: v, Label: k.label(v)})
	}
	return &Option{Control: ControlSelect, Value: cur, Choices: choices}
}

func (k numberKind) read(caps []*Cap) *Option {
	c := caps[0]
	cur, ok := c.Current()
	if !ok {
		return nil
	}
	isRange := c.Container == "RANGE"
	slider := func() *Option {
		min, max, step := c.MinValue, c.MaxValue, c.StepSize
		if step <= 0 {
			step = 1
		}
		return &Option{Control: ControlSlider, Value: cur, Min: &min, Max: &max, Step: &step}
	}
	rangePresets := func() []float64 {
		var list []float64
		for _, p := range k.presets {
			if rangeAllows(c, p) {
				list = append(list, p)
			}
		}
		return list
	}

	var opt *Option
	switch k.control {
	case ControlNumber:
		opt = &Option{Control: ControlNumber, Value: cur}
		if isRange {
			opt = slider()
			opt.Control = ControlNumber
		}
	case ControlSlider:
		if isRange {
			opt = slider()
		}
	case ControlSelect:
		if isRange {
			if list := rangePresets(); len(list) > 0 {
				opt = k.selectOf(list, cur)
			} else {
				opt = slider()
			}
		} else {
			opt = k.selectOf(c.Choices(), cur)
		}
	}
	if opt == nil { // auto，或者指定的控件和设备报的形态对不上
		switch {
		case isRange && len(k.presets) > 0 && len(rangePresets()) > 0:
			opt = k.selectOf(rangePresets(), cur)
		case isRange:
			opt = slider()
		case c.Container == "ENUMERATION":
			opt = k.selectOf(c.Choices(), cur)
		default:
			opt = &Option{Control: ControlNumber, Value: cur}
		}
	}
	opt.Unit = k.unit
	return opt
}

func (k numberKind) plan(v any, caps []*Cap) ([]CapWrite, error) {
	c := caps[0]
	if c == nil {
		return nil, fmt.Errorf("该扫描仪不支持此项")
	}
	n, ok := toNumber(v)
	if !ok {
		return nil, fmt.Errorf("必须是数字，收到 %s", describe(v))
	}
	switch c.Container {
	case "RANGE":
		if n < c.MinValue-0.001 || n > c.MaxValue+0.001 {
			return nil, fmt.Errorf("%s 超出范围 %s~%s", k.label(n), k.label(c.MinValue), k.label(c.MaxValue))
		}
		if !rangeAllows(c, n) {
			return nil, fmt.Errorf("%s 不符合步长 %s", k.label(n), formatNumber(c.StepSize))
		}
	case "ENUMERATION":
		if !containsNumber(c.Choices(), n) {
			return nil, fmt.Errorf("该扫描仪不支持 %s", k.label(n))
		}
	}
	writes := []CapWrite{}
	for i, code := range k.codes {
		if i == 0 || caps[i] != nil {
			writes = append(writes, CapWrite{Code: code, Value: formatNumber(n)})
		}
	}
	return writes, nil
}

// ---- switch ----

type switchKind struct {
	on, off float64
}

func (k switchKind) read(caps []*Cap) *Option {
	cur, ok := caps[0].Current()
	if !ok {
		return nil
	}
	return &Option{Control: ControlSwitch, Value: sameNumber(cur, k.on)}
}

func (k switchKind) plan(v any, caps []*Cap) ([]CapWrite, error) {
	c := caps[0]
	if c == nil {
		return nil, fmt.Errorf("该扫描仪不支持此项")
	}
	b, ok := toBool(v)
	if !ok {
		return nil, fmt.Errorf("必须是 true 或 false，收到 %s", describe(v))
	}
	target := k.off
	if b {
		target = k.on
	}
	if !capAllows(c, target) {
		return nil, fmt.Errorf("该扫描仪不支持修改此项")
	}
	return []CapWrite{{Code: c.Code, Value: formatNumber(target)}}, nil
}

// ---- combo ----

type comboSet struct {
	idx      int // 在 Def.Caps 里的位置
	code     uint16
	value    float64
	optional bool
}

type comboCond struct {
	idx   int
	op    string
	value float64
}

type comboChoice struct {
	value    any
	label    string
	set      []comboSet
	requires []comboCond
}

type comboKind struct {
	control string
	choices []comboChoice
}

// available 判断这台设备能不能选这一项：非可选的能力都支持且取值合法，requires 都满足。
func (k comboKind) available(ch comboChoice, caps []*Cap) bool {
	for _, s := range ch.set {
		c := caps[s.idx]
		if c == nil || !capAllows(c, s.value) {
			if s.optional {
				continue
			}
			return false
		}
	}
	for _, r := range ch.requires {
		if !r.eval(caps[r.idx]) {
			return false
		}
	}
	return true
}

// matches 判断设备当前状态是不是这一项。strict 时可选能力也要对得上（设备支持的话）。
func (k comboKind) matches(ch comboChoice, caps []*Cap, strict bool) bool {
	for _, s := range ch.set {
		c := caps[s.idx]
		if c == nil || (s.optional && !strict) {
			continue
		}
		cur, ok := c.Current()
		if !ok || !sameNumber(cur, s.value) {
			return false
		}
	}
	return true
}

func (k comboKind) read(caps []*Cap) *Option {
	var avail []comboChoice
	for _, ch := range k.choices {
		if k.available(ch, caps) {
			avail = append(avail, ch)
		}
	}
	if len(avail) == 0 {
		return nil
	}
	choices := make([]Choice, 0, len(avail))
	for _, ch := range avail {
		choices = append(choices, Choice{Value: ch.value, Label: ch.label})
	}

	// 当前值：先要求可选能力也对得上，找不到再放宽；都对不上给 null。
	var value any
	for _, strict := range []bool{true, false} {
		for _, ch := range avail {
			if k.matches(ch, caps, strict) {
				value = ch.value
				break
			}
		}
		if value != nil {
			break
		}
	}
	return &Option{Control: k.control, Value: value, Choices: choices}
}

func (k comboKind) plan(v any, caps []*Cap) ([]CapWrite, error) {
	for _, ch := range k.choices {
		if !sameValue(ch.value, v) {
			continue
		}
		if !k.available(ch, caps) {
			return nil, fmt.Errorf("该扫描仪不支持「%s」", ch.label)
		}
		writes := []CapWrite{}
		for _, s := range ch.set {
			c := caps[s.idx]
			if c == nil || (s.optional && !capAllows(c, s.value)) {
				continue
			}
			writes = append(writes, CapWrite{Code: s.code, Value: formatNumber(s.value)})
		}
		return writes, nil
	}
	return nil, fmt.Errorf("不认识的取值 %s", describe(v))
}

var comboOps = []string{"==", "!=", ">", ">=", "<", "<=", "supported", "unsupported"}

func (r comboCond) eval(c *Cap) bool {
	switch r.op {
	case "supported":
		return c != nil
	case "unsupported":
		return c == nil
	}
	cur, ok := c.Current()
	if !ok {
		return false
	}
	switch r.op {
	case "==":
		return sameNumber(cur, r.value)
	case "!=":
		return !sameNumber(cur, r.value)
	case ">":
		return cur > r.value
	case ">=":
		return cur >= r.value-0.001
	case "<":
		return cur < r.value
	case "<=":
		return cur <= r.value+0.001
	}
	return false
}

// ---- 小工具 ----

// capAllows 看设备能不能把这个能力设成 v。
// 报 ENUMERATION 的按列表、报 RANGE 的按区间和步长判断；报 ONEVALUE 的看不出来
// （多数驱动的布尔能力、不少整数能力都这么报），按允许处理，真设不上时设备会拒绝，
// 错误会带回给前端。
func capAllows(c *Cap, v float64) bool {
	if c == nil {
		return false
	}
	switch c.Container {
	case "ENUMERATION":
		return containsNumber(c.Choices(), v)
	case "RANGE":
		return rangeAllows(c, v)
	}
	return true
}

func rangeAllows(c *Cap, v float64) bool {
	if v < c.MinValue-0.001 || v > c.MaxValue+0.001 {
		return false
	}
	if c.StepSize <= 0 {
		return true
	}
	k := (v - c.MinValue) / c.StepSize
	return math.Abs(k-math.Round(k)) < 0.001
}

func containsNumber(list []float64, v float64) bool {
	for _, x := range list {
		if sameNumber(x, v) {
			return true
		}
	}
	return false
}

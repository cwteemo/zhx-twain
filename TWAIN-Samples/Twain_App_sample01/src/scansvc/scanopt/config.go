package scanopt

// 设置项配置文件的解析与校验。写法见 SCANNER_OPTIONS_CONFIG.md。
//
// 文件是 JSON，另外允许 // 和 /* */ 注释、以及对象 / 数组最后多一个逗号（手改配置时最常犯的错），
// 所以扩展名用 .jsonc。解析出错时报的位置是"options[2].choices[1].code"这种路径，照着找就行。

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ConfigVersion 是配置文件格式的版本号，和给前端的 ProtocolVersion 无关。
const ConfigVersion = 1

type fileConfig struct {
	Version  int             `json:"version"`
	Options  []optionConfig  `json:"options"`
	Profiles []profileConfig `json:"profiles"`
}

type optionConfig struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Group    string `json:"group"`
	Kind     string `json:"kind"`
	Control  string `json:"control"`
	Disabled bool   `json:"disabled"`

	Cap  string   `json:"cap"`  // enum / number / switch
	Caps []string `json:"caps"` // number：读第一个、写全部

	Choices      []choiceConfig `json:"choices"`      // enum / combo
	ShowUnknown  bool           `json:"showUnknown"`  // enum
	UnknownLabel string         `json:"unknownLabel"` // enum

	Presets     []float64 `json:"presets"`     // number
	ChoiceLabel string    `json:"choiceLabel"` // number
	Unit        string    `json:"unit"`        // number

	On  *float64 `json:"on"`  // switch
	Off *float64 `json:"off"` // switch
}

type choiceConfig struct {
	Value any    `json:"value"`
	Label string `json:"label"`

	Code *float64 `json:"code"` // enum

	Set      []capValueConfig  `json:"set"`      // combo
	Requires []conditionConfig `json:"requires"` // combo
}

type capValueConfig struct {
	Cap      string   `json:"cap"`
	Value    *float64 `json:"value"`
	Optional bool     `json:"optional"`
}

type conditionConfig struct {
	Cap   string   `json:"cap"`
	Op    string   `json:"op"`
	Value *float64 `json:"value"`
}

type profileConfig struct {
	Name    string         `json:"name"`
	Match   []string       `json:"match"`
	Hide    []string       `json:"hide"`
	Options []optionConfig `json:"options"`
}

// Compile 解析并校验配置文件内容，返回可用的 Registry。任何一处不对都整体报错，
// 不会"部分生效"——半份配置比报错更难排查。
func Compile(data []byte) (*Registry, error) {
	clean, err := stripJSONC(data)
	if err != nil {
		return nil, err
	}

	var cfg fileConfig
	dec := json.NewDecoder(bytes.NewReader(clean))
	dec.DisallowUnknownFields() // 字段名拼错（比如 lable）直接报出来，别静默忽略
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("配置不是合法的 JSON: %w", jsonErrorWithLine(clean, err))
	}
	if cfg.Version != ConfigVersion {
		return nil, fmt.Errorf("version 应为 %d，实际是 %d", ConfigVersion, cfg.Version)
	}

	reg := &Registry{}
	reg.Defs, err = compileOptions(cfg.Options, "options")
	if err != nil {
		return nil, err
	}

	for i, p := range cfg.Profiles {
		path := fmt.Sprintf("profiles[%d]", i)
		if strings.TrimSpace(p.Name) == "" {
			return nil, fmt.Errorf("%s: 缺少 name", path)
		}
		if len(p.Match) == 0 {
			return nil, fmt.Errorf("%s（%s）: match 不能为空，写设备名里的关键字，如 [\"Q400\"]", path, p.Name)
		}
		defs, err := compileOptions(p.Options, path+".options")
		if err != nil {
			return nil, err
		}
		reg.Profiles = append(reg.Profiles, Profile{Name: p.Name, Match: p.Match, Hide: p.Hide, Defs: defs})
	}
	return reg, nil
}

func compileOptions(list []optionConfig, base string) ([]Def, error) {
	defs := []Def{}
	seen := map[string]bool{}
	for i, oc := range list {
		path := fmt.Sprintf("%s[%d]", base, i)
		if oc.Key != "" {
			path += "（" + oc.Key + "）"
		}
		if seen[oc.Key] {
			return nil, fmt.Errorf("%s: key 重复", path)
		}
		// disabled 的也照样校验：示例项平时关着，打开时才发现写错就晚了。
		def, err := compileOption(oc)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		seen[oc.Key] = true
		if !oc.Disabled {
			defs = append(defs, def)
		}
	}
	return defs, nil
}

func compileOption(oc optionConfig) (Def, error) {
	if strings.TrimSpace(oc.Key) == "" {
		return Def{}, errors.New("缺少 key")
	}
	if strings.TrimSpace(oc.Label) == "" {
		return Def{}, errors.New("缺少 label")
	}
	def := Def{Key: oc.Key, Label: oc.Label, Group: oc.Group}
	switch def.Group {
	case "":
		def.Group = GroupBasic
	case GroupBasic, GroupAdvanced:
	default:
		return Def{}, fmt.Errorf("group 只能是 %s 或 %s", GroupBasic, GroupAdvanced)
	}

	switch oc.Kind {
	case "enum":
		return compileEnum(def, oc)
	case "number":
		return compileNumber(def, oc)
	case "switch":
		return compileSwitch(def, oc)
	case "combo":
		return compileCombo(def, oc)
	case "":
		return Def{}, errors.New("缺少 kind（enum / number / switch / combo）")
	default:
		return Def{}, fmt.Errorf("不认识的 kind %q，只能是 enum / number / switch / combo", oc.Kind)
	}
}

func compileEnum(def Def, oc optionConfig) (Def, error) {
	code, err := singleCap(oc)
	if err != nil {
		return Def{}, err
	}
	control, err := pickControl(oc.Control, ControlSelect, ControlSelect, ControlRadio)
	if err != nil {
		return Def{}, err
	}
	if len(oc.Choices) == 0 {
		return Def{}, errors.New("enum 至少要有一个 choices")
	}

	k := enumKind{control: control, showUnknown: oc.ShowUnknown, unknownLabel: oc.UnknownLabel}
	if k.unknownLabel == "" {
		k.unknownLabel = "其他（{code}）"
	}
	codes := map[float64]bool{}
	values := map[string]bool{}
	for j, ch := range oc.Choices {
		p := fmt.Sprintf("choices[%d]", j)
		if ch.Code == nil {
			return Def{}, fmt.Errorf("%s: 缺少 code（TWAIN 里的编号）", p)
		}
		if err := checkChoiceValue(ch, p); err != nil {
			return Def{}, err
		}
		if len(ch.Set) > 0 || len(ch.Requires) > 0 {
			return Def{}, fmt.Errorf("%s: set / requires 是 combo 才有的，enum 用 code", p)
		}
		if codes[*ch.Code] {
			return Def{}, fmt.Errorf("%s: code %s 重复", p, formatNumber(*ch.Code))
		}
		vk := fmt.Sprint(ch.Value)
		if values[vk] {
			return Def{}, fmt.Errorf("%s: value %s 重复", p, describe(ch.Value))
		}
		codes[*ch.Code], values[vk] = true, true
		k.choices = append(k.choices, enumChoice{Code: *ch.Code, Value: ch.Value, Label: ch.Label})
	}

	def.Caps = []uint16{code}
	def.Read, def.Plan = k.read, k.plan
	return def, nil
}

func compileNumber(def Def, oc optionConfig) (Def, error) {
	var codes []uint16
	switch {
	case oc.Cap != "" && len(oc.Caps) > 0:
		return Def{}, errors.New("cap 和 caps 只写一个")
	case oc.Cap != "":
		c, err := ResolveCap(oc.Cap)
		if err != nil {
			return Def{}, fmt.Errorf("cap: %w", err)
		}
		codes = []uint16{c}
	case len(oc.Caps) > 0:
		for j, name := range oc.Caps {
			c, err := ResolveCap(name)
			if err != nil {
				return Def{}, fmt.Errorf("caps[%d]: %w", j, err)
			}
			codes = append(codes, c)
		}
	default:
		return Def{}, errors.New("缺少 cap")
	}
	if len(oc.Choices) > 0 {
		return Def{}, errors.New("number 没有 choices；想要固定几档用 presets，想要文字选项用 enum")
	}
	control, err := pickControl(oc.Control, "auto", "auto", ControlSlider, ControlSelect, ControlNumber)
	if err != nil {
		return Def{}, err
	}

	k := numberKind{codes: codes, control: control, presets: oc.Presets, choiceLabel: oc.ChoiceLabel, unit: oc.Unit}
	def.Caps = codes
	def.Read, def.Plan = k.read, k.plan
	return def, nil
}

func compileSwitch(def Def, oc optionConfig) (Def, error) {
	code, err := singleCap(oc)
	if err != nil {
		return Def{}, err
	}
	if oc.Control != "" && oc.Control != ControlSwitch {
		return Def{}, errors.New("switch 的 control 只能是 switch（可以不写）")
	}
	k := switchKind{on: 1, off: 0}
	if oc.On != nil {
		k.on = *oc.On
	}
	if oc.Off != nil {
		k.off = *oc.Off
	}
	if sameNumber(k.on, k.off) {
		return Def{}, errors.New("on 和 off 不能相同")
	}
	def.Caps = []uint16{code}
	def.Read, def.Plan = k.read, k.plan
	return def, nil
}

func compileCombo(def Def, oc optionConfig) (Def, error) {
	if oc.Cap != "" || len(oc.Caps) > 0 {
		return Def{}, errors.New("combo 不写 cap，能力写在每个选项的 set / requires 里")
	}
	control, err := pickControl(oc.Control, ControlSelect, ControlSelect, ControlRadio)
	if err != nil {
		return Def{}, err
	}
	if len(oc.Choices) == 0 {
		return Def{}, errors.New("combo 至少要有一个 choices")
	}

	// 所有选项用到的能力合在一起就是这项要读的能力，按出现顺序。
	index := map[uint16]int{}
	capIndex := func(name, p string) (uint16, int, error) {
		c, err := ResolveCap(name)
		if err != nil {
			return 0, 0, fmt.Errorf("%s: %w", p, err)
		}
		if i, ok := index[c]; ok {
			return c, i, nil
		}
		index[c] = len(def.Caps)
		def.Caps = append(def.Caps, c)
		return c, index[c], nil
	}

	k := comboKind{control: control}
	values := map[string]bool{}
	for j, ch := range oc.Choices {
		p := fmt.Sprintf("choices[%d]", j)
		if err := checkChoiceValue(ch, p); err != nil {
			return Def{}, err
		}
		if ch.Code != nil {
			return Def{}, fmt.Errorf("%s: code 是 enum 才有的，combo 用 set", p)
		}
		if len(ch.Set) == 0 {
			return Def{}, fmt.Errorf("%s: set 不能为空，写选中这一项时要设哪些能力", p)
		}
		vk := fmt.Sprint(ch.Value)
		if values[vk] {
			return Def{}, fmt.Errorf("%s: value %s 重复", p, describe(ch.Value))
		}
		values[vk] = true

		cc := comboChoice{value: ch.Value, label: ch.Label}
		for m, s := range ch.Set {
			sp := fmt.Sprintf("%s.set[%d]", p, m)
			if s.Value == nil {
				return Def{}, fmt.Errorf("%s: 缺少 value", sp)
			}
			code, idx, err := capIndex(s.Cap, sp+".cap")
			if err != nil {
				return Def{}, err
			}
			cc.set = append(cc.set, comboSet{idx: idx, code: code, value: *s.Value, optional: s.Optional})
		}
		for m, r := range ch.Requires {
			rp := fmt.Sprintf("%s.requires[%d]", p, m)
			if !containsString(comboOps, r.Op) {
				return Def{}, fmt.Errorf("%s: op 只能是 %s", rp, strings.Join(comboOps, " "))
			}
			if r.Value == nil && r.Op != "supported" && r.Op != "unsupported" {
				return Def{}, fmt.Errorf("%s: op %s 需要 value", rp, r.Op)
			}
			_, idx, err := capIndex(r.Cap, rp+".cap")
			if err != nil {
				return Def{}, err
			}
			cond := comboCond{idx: idx, op: r.Op}
			if r.Value != nil {
				cond.value = *r.Value
			}
			cc.requires = append(cc.requires, cond)
		}
		k.choices = append(k.choices, cc)
	}

	def.Read, def.Plan = k.read, k.plan
	return def, nil
}

func singleCap(oc optionConfig) (uint16, error) {
	if len(oc.Caps) > 0 {
		return 0, fmt.Errorf("%s 只对应一个能力，用 cap 不用 caps", oc.Kind)
	}
	if oc.Cap == "" {
		return 0, errors.New("缺少 cap")
	}
	c, err := ResolveCap(oc.Cap)
	if err != nil {
		return 0, fmt.Errorf("cap: %w", err)
	}
	return c, nil
}

func pickControl(got, def string, allowed ...string) (string, error) {
	if got == "" {
		return def, nil
	}
	if containsString(allowed, got) {
		return got, nil
	}
	return "", fmt.Errorf("control 只能是 %s", strings.Join(allowed, " / "))
}

func checkChoiceValue(ch choiceConfig, p string) error {
	switch v := ch.Value.(type) {
	case string:
		if v == "" {
			return fmt.Errorf("%s: value 不能是空字符串", p)
		}
	case float64:
	case nil:
		return fmt.Errorf("%s: 缺少 value（发给前端、前端再发回来的取值）", p)
	default:
		return fmt.Errorf("%s: value 只能是字符串或数字", p)
	}
	if strings.TrimSpace(ch.Label) == "" {
		return fmt.Errorf("%s: 缺少 label", p)
	}
	return nil
}

// ResolveCap 把配置里写的能力翻成编号：宏名（ICAP_PIXELTYPE）、十六进制（0x0101）、十进制（257）都认。
// 厂商自定义能力（0x8000 以上）没有宏名，写编号。
func ResolveCap(s string) (uint16, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("能力不能为空")
	}
	if code, ok := capCodesByName[strings.ToUpper(s)]; ok {
		return code, nil
	}
	base := 10
	num := s
	if strings.HasPrefix(strings.ToLower(s), "0x") {
		base, num = 16, s[2:]
	}
	n, err := strconv.ParseUint(num, base, 16)
	if err != nil || n == 0 {
		return 0, fmt.Errorf("不认识的能力 %q（写 twain.h 里的宏名如 ICAP_PIXELTYPE，或编号如 0x0101）", s)
	}
	return uint16(n), nil
}

var capCodesByName = func() map[string]uint16 {
	m := make(map[string]uint16, len(CapNames))
	for code, name := range CapNames {
		m[name] = code
	}
	return m
}()

// stripJSONC 去掉 // 和 /* */ 注释、以及 } ] 前面多余的逗号，字符串里的内容不动。
// 去注释时保留换行，JSON 解析报错的行号仍然和原文件对得上。
func stripJSONC(src []byte) ([]byte, error) {
	src = bytes.TrimPrefix(src, []byte{0xEF, 0xBB, 0xBF}) // 记事本存的 UTF-8 BOM
	out := make([]byte, 0, len(src))
	inString := false
	for i := 0; i < len(src); i++ {
		c := src[i]
		if inString {
			out = append(out, c)
			if c == '\\' && i+1 < len(src) {
				i++
				out = append(out, src[i])
			} else if c == '"' {
				inString = false
			}
			continue
		}
		switch {
		case c == '"':
			inString = true
			out = append(out, c)
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' {
				i++
			}
			if i < len(src) {
				out = append(out, '\n')
			}
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			end := bytes.Index(src[i+2:], []byte("*/"))
			if end < 0 {
				return nil, errors.New("有一段 /* 注释没有结束的 */")
			}
			for _, b := range src[i : i+2+end+2] {
				if b == '\n' {
					out = append(out, '\n')
				}
			}
			i += 2 + end + 1
		default:
			out = append(out, c)
		}
	}
	return removeTrailingCommas(out), nil
}

func removeTrailingCommas(src []byte) []byte {
	out := make([]byte, 0, len(src))
	inString := false
	for i := 0; i < len(src); i++ {
		c := src[i]
		if inString {
			out = append(out, c)
			if c == '\\' && i+1 < len(src) {
				i++
				out = append(out, src[i])
			} else if c == '"' {
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
		}
		if c == ',' {
			j := i + 1
			for j < len(src) && (src[j] == ' ' || src[j] == '\t' || src[j] == '\r' || src[j] == '\n') {
				j++
			}
			if j < len(src) && (src[j] == '}' || src[j] == ']') {
				out = append(out, ' ') // 占位，保持列号不乱
				continue
			}
		}
		out = append(out, c)
	}
	return out
}

// jsonErrorWithLine 给 JSON 解析错误补上行号。
func jsonErrorWithLine(src []byte, err error) error {
	var offset int64 = -1
	var syn *json.SyntaxError
	var typ *json.UnmarshalTypeError
	switch {
	case errors.As(err, &syn):
		offset = syn.Offset
	case errors.As(err, &typ):
		offset = typ.Offset
		return fmt.Errorf("第 %d 行: 字段 %s 的类型不对，应为 %s", lineOf(src, offset), typ.Field, typ.Type)
	}
	if offset < 0 {
		return err
	}
	return fmt.Errorf("第 %d 行: %v", lineOf(src, offset), err)
}

func lineOf(src []byte, offset int64) int {
	if offset > int64(len(src)) {
		offset = int64(len(src))
	}
	return bytes.Count(src[:offset], []byte("\n")) + 1
}

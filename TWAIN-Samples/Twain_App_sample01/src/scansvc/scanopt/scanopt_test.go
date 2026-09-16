package scanopt

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const virtualDevice = "TWAIN2 Software Scanner"

// 测试里用到的能力编号，见 twain.h。
const (
	CapPixelType     uint16 = 0x0101
	CapFeederEnabled uint16 = 0x1002
	CapAutoFeed      uint16 = 0x1007
	CapDuplex        uint16 = 0x1012
	CapDuplexEnabled uint16 = 0x1013
	CapBrightness    uint16 = 0x1101
	CapContrast      uint16 = 0x1103
	CapXResolution   uint16 = 0x1118
	CapYResolution   uint16 = 0x1119
	CapSupportedSize uint16 = 0x1122
)

// virtualScannerDump 是虚拟扫描仪的能力导出。
// TODO(TODO-settings.md #13): 这份是按 DS 源码默认值推出来的，不是真实抓取。
const virtualScannerDump = "testdata/devices/TWAIN2_Software_Scanner.json"

// loadRaw 读一份 capdump 文件（/api/capabilities/dump 导出的原样），
// 返回设备名和 能力编号 -> DLL 原始输出。
func loadRaw(t *testing.T, path string) map[uint16]string {
	t.Helper()
	_, raw := loadDump(t, path)
	return raw
}

func loadDump(t *testing.T, path string) (string, map[uint16]string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var dump struct {
		Device string `json:"device"`
		Caps   []struct {
			Code string          `json:"code"`
			Raw  json.RawMessage `json:"raw"`
		} `json:"caps"`
	}
	if err := json.Unmarshal(b, &dump); err != nil {
		t.Fatalf("%s 不是 capdump 格式: %v", path, err)
	}
	raw := make(map[uint16]string, len(dump.Caps))
	for _, c := range dump.Caps {
		code, err := ResolveCap(c.Code)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		// DLL 输出不是合法 JSON 时 capdump 存的是字符串，还原成原文。
		var s string
		if json.Unmarshal(c.Raw, &s) == nil {
			raw[code] = s
		} else {
			raw[code] = string(c.Raw)
		}
	}
	return dump.Device, raw
}

func byKey(opts []Option) map[string]Option {
	m := make(map[string]Option, len(opts))
	for _, o := range opts {
		m[o.Key] = o
	}
	return m
}

func choiceValues(o Option) []any {
	out := []any{}
	for _, c := range o.Choices {
		out = append(out, c.Value)
	}
	return out
}

func defByKey(t *testing.T, key string) Def {
	t.Helper()
	for _, d := range DefsFor("") {
		if d.Key == key {
			return d
		}
	}
	t.Fatalf("没有 key=%s 的定义", key)
	return Def{}
}

// plan 用给定的原始数据跑一次某项的 Plan。
func plan(t *testing.T, raw map[uint16]string, key string, v any) ([]CapWrite, error) {
	t.Helper()
	d := defByKey(t, key)
	return d.Plan(v, d.ParseCaps(raw))
}

func TestBuildVirtualScanner(t *testing.T) {
	opts := Build(loadRaw(t, virtualScannerDump), virtualDevice)
	m := byKey(opts)

	// 顺序就是定义表的顺序。
	var keys []string
	for _, o := range opts {
		keys = append(keys, o.Key)
	}
	if want := []string{"source", "colorMode", "dpi", "paperSize", "brightness", "contrast"}; !reflect.DeepEqual(keys, want) {
		t.Errorf("项目顺序 %v，期望 %v", keys, want)
	}

	cases := []struct {
		key, control string
		value        any
		choices      []any
	}{
		{"source", ControlSelect, "adf", []any{"flatbed", "adf"}},
		{"colorMode", ControlRadio, "color", []any{"bw", "gray", "color"}},
		{"dpi", ControlSelect, 200.0, []any{50.0, 100.0, 150.0, 200.0, 300.0, 400.0, 500.0, 600.0}},
		{"paperSize", ControlSelect, "letter", []any{"none", "letter", "legal"}},
		{"brightness", ControlSlider, 0.0, []any{}},
		{"contrast", ControlSlider, 0.0, []any{}},
	}
	for _, c := range cases {
		o, ok := m[c.key]
		if !ok {
			t.Errorf("%s: 缺项", c.key)
			continue
		}
		if o.Control != c.control || o.Value != c.value {
			t.Errorf("%s: 得到 control=%s value=%#v，期望 control=%s value=%#v", c.key, o.Control, o.Value, c.control, c.value)
		}
		if got := choiceValues(o); !reflect.DeepEqual(got, c.choices) {
			t.Errorf("%s: 得到 choices=%v，期望 %v", c.key, got, c.choices)
		}
		if o.Label == "" || o.Group != GroupBasic {
			t.Errorf("%s: label / group 没填: %+v", c.key, o)
		}
	}

	if b := m["brightness"]; b.Min == nil || *b.Min != -1000 || *b.Max != 1000 || *b.Step != 1 {
		t.Errorf("brightness 区间不对: %+v", b)
	}
	if m["dpi"].Unit != "dpi" || m["dpi"].Choices[3].Label != "200 dpi" {
		t.Errorf("dpi 单位 / 文字不对: %+v", m["dpi"])
	}
}

// 读不到、坏 JSON 都按不支持略过；一项都没有时返回空数组而不是 nil。
func TestBuildTolerance(t *testing.T) {
	opts := Build(map[uint16]string{
		CapPixelType:  `[]`,
		CapBrightness: `not json`,
	}, virtualDevice)
	if opts == nil || len(opts) != 0 {
		t.Errorf("期望空数组，得到 %#v", opts)
	}
	b, _ := json.Marshal(Build(nil, ""))
	if string(b) != "[]" {
		t.Errorf("期望序列化成 []，得到 %s", b)
	}
}

// 支持双面的设备才给 adfDuplex，且双面开着时当前值是 adfDuplex。
func TestSourceDuplex(t *testing.T) {
	raw := map[uint16]string{
		CapFeederEnabled: `[{"container":"ONEVALUE","itemType":6,"value":true}]`,
		CapDuplex:        `[{"container":"ONEVALUE","itemType":4,"value":1}]`,
		CapDuplexEnabled: `[{"container":"ONEVALUE","itemType":6,"value":true}]`,
	}
	s := byKey(Build(raw, "Uniscan Q400"))["source"]
	if s.Value != "adfDuplex" || !reflect.DeepEqual(choiceValues(s), []any{"flatbed", "adf", "adfDuplex"}) {
		t.Errorf("source 不对: %+v", s)
	}

	w, err := plan(t, raw, "source", "adf")
	if err != nil || !reflect.DeepEqual(w, []CapWrite{{CapFeederEnabled, "1"}, {CapDuplexEnabled, "0"}}) {
		t.Errorf("adf 下发不对: %v %v", w, err)
	}
	w, err = plan(t, raw, "source", "adfDuplex")
	if err != nil || !reflect.DeepEqual(w, []CapWrite{{CapFeederEnabled, "1"}, {CapDuplexEnabled, "1"}}) {
		t.Errorf("adfDuplex 下发不对: %v %v", w, err)
	}
}

func TestPlanVirtualScanner(t *testing.T) {
	raw := loadRaw(t, virtualScannerDump)

	ok := []struct {
		key   string
		value any
		want  []CapWrite
	}{
		{"colorMode", "gray", []CapWrite{{CapPixelType, "1"}}},
		{"dpi", 300.0, []CapWrite{{CapXResolution, "300"}}}, // 测试数据里没有 Y 分辨率，只写 X
		{"dpi", "300", []CapWrite{{CapXResolution, "300"}}}, // 前端传字符串也认
		{"paperSize", "legal", []CapWrite{{CapSupportedSize, "4"}}},
		{"brightness", "-20", []CapWrite{{CapBrightness, "-20"}}},
		{"contrast", 500.0, []CapWrite{{CapContrast, "500"}}},
		{"source", "flatbed", []CapWrite{{CapFeederEnabled, "0"}}},
	}
	for _, c := range ok {
		got, err := plan(t, raw, c.key, c.value)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s=%v: 得到 %v, %v；期望 %v", c.key, c.value, got, err, c.want)
		}
	}

	bad := []struct {
		key     string
		value   any
		errPart string
	}{
		{"colorMode", "cmyk", "不认识"},
		{"dpi", 250.0, "不支持 250 dpi"},
		{"dpi", "abc", "必须是数字"},
		{"paperSize", "a3", "不支持「A3」"},
		{"brightness", 1001.0, "超出范围"},
		{"source", "adfDuplex", "不支持「进纸器双面」"},
		{"source", "scanner", "不认识"},
	}
	for _, c := range bad {
		_, err := plan(t, raw, c.key, c.value)
		if err == nil || !strings.Contains(err.Error(), c.errPart) {
			t.Errorf("%s=%v: 期望报错含 %q，得到 %v", c.key, c.value, c.errPart, err)
		}
	}
}

// 设备报区间的分辨率：下拉框给区间内对得上步长的常用档位，写入时区间内对得上步长的都行。
func TestDPIRange(t *testing.T) {
	raw := map[uint16]string{
		CapXResolution: `[{"container":"RANGE","itemType":7,"minValue":100,"maxValue":600,"stepSize":50,"currentValue":250}]`,
		CapYResolution: `[{"container":"RANGE","itemType":7,"minValue":100,"maxValue":600,"stepSize":50,"currentValue":250}]`,
	}
	o := byKey(Build(raw, "x"))["dpi"]
	if want := []any{100.0, 150.0, 200.0, 250.0, 300.0, 400.0, 600.0}; !reflect.DeepEqual(choiceValues(o), want) {
		t.Errorf("区间分辨率的档位 %v，期望 %v", choiceValues(o), want)
	}
	if w, err := plan(t, raw, "dpi", 350.0); err != nil || len(w) != 2 {
		t.Errorf("350 在区间内应当可写 X/Y 两项: %v %v", w, err)
	}
	if _, err := plan(t, raw, "dpi", 240.0); err == nil {
		t.Error("240 对不上步长 50，应当报错")
	}
}

func TestSwitch(t *testing.T) {
	reg := mustCompile(t, `{"version":1,"options":[
		{"key":"autoFeed","label":"自动进纸","kind":"switch","cap":"CAP_AUTOFEED"},
		{"key":"blank","label":"去空白","kind":"switch","cap":"0x1134","on":-1,"off":-2}
	]}`)
	raw := map[uint16]string{
		CapAutoFeed: `[{"container":"ONEVALUE","itemType":6,"value":false}]`,
		0x1134:      `[{"container":"ONEVALUE","itemType":4,"value":-1}]`,
	}
	d := reg.Defs[0]
	o := d.Build(raw)
	if o == nil || o.Control != ControlSwitch || o.Value != false {
		t.Fatalf("开关读取不对: %+v", o)
	}
	w, err := d.Plan("true", d.ParseCaps(raw))
	if err != nil || !reflect.DeepEqual(w, []CapWrite{{CapAutoFeed, "1"}}) {
		t.Errorf("开关下发不对: %v %v", w, err)
	}

	d = reg.Defs[1]
	if o := d.Build(raw); o == nil || o.Value != true {
		t.Errorf("on=-1 时当前值 -1 应为开: %+v", o)
	}
	w, err = d.Plan(false, d.ParseCaps(raw))
	if err != nil || !reflect.DeepEqual(w, []CapWrite{{0x1134, "-2"}}) {
		t.Errorf("off=-2 下发不对: %v %v", w, err)
	}
}

func TestProfiles(t *testing.T) {
	reg := mustCompile(t, `{"version":1,
	  "options":[
	    {"key":"brightness","label":"亮度","kind":"number","cap":"ICAP_BRIGHTNESS"},
	    {"key":"contrast","label":"对比度","kind":"number","cap":"ICAP_CONTRAST"},
	  ],
	  "profiles":[{
	    "name":"测试机", "match":["q400"], "hide":["contrast"],
	    "options":[
	      {"key":"brightness","label":"亮度（改）","kind":"number","cap":"ICAP_BRIGHTNESS","control":"number"},
	      {"key":"autoFeed","label":"自动进纸","kind":"switch","cap":"CAP_AUTOFEED"},
	    ]
	  }]
	}`)

	keys := func(device string) []string {
		var out []string
		for _, d := range reg.DefsFor(device) {
			out = append(out, d.Key+":"+d.Label)
		}
		return out
	}
	if got, want := keys("Uniscan Q400"), []string{"brightness:亮度（改）", "autoFeed:自动进纸"}; !reflect.DeepEqual(got, want) {
		t.Errorf("命中 Profile 后 %v，期望 %v", got, want)
	}
	if got, want := keys("KODAK S2000"), []string{"brightness:亮度", "contrast:对比度"}; !reflect.DeepEqual(got, want) {
		t.Errorf("没命中的设备 %v，期望 %v", got, want)
	}
	if len(reg.Defs) != 2 {
		t.Errorf("DefsFor 不应改动通用 Defs，现在有 %d 项", len(reg.Defs))
	}
}

// 下拉框的选项增减、顺序、文字都只看配置。
func TestEnumFromConfig(t *testing.T) {
	reg := mustCompile(t, `{"version":1,"options":[
		// 故意把彩色写在前面、去掉灰度
		{"key":"colorMode","label":"颜色","kind":"enum","cap":"ICAP_PIXELTYPE","control":"radio",
		 "choices":[{"code":2,"value":"color","label":"彩色"},{"code":0,"value":"bw","label":"黑白"}]},
	]}`)
	raw := map[uint16]string{CapPixelType: loadRaw(t, virtualScannerDump)[CapPixelType]} // 设备支持 0 1 2，当前 2
	o := reg.Defs[0].Build(raw)
	if o == nil || o.Control != ControlRadio || o.Value != "color" ||
		!reflect.DeepEqual(choiceValues(*o), []any{"color", "bw"}) {
		t.Errorf("按配置的选项和顺序输出不对: %+v", o)
	}
	if _, err := reg.Defs[0].Plan("gray", reg.Defs[0].ParseCaps(raw)); err == nil {
		t.Error("配置里去掉的 gray 不应能设置")
	}
}

// 纸张：showUnknown 时设备报了配置里没有的编号也给出来，并且能写回去。
func TestEnumUnknown(t *testing.T) {
	reg := mustCompile(t, `{"version":1,"options":[
		{"key":"paper","label":"纸张","kind":"enum","cap":"ICAP_SUPPORTEDSIZES","showUnknown":true,"unknownLabel":"规格{code}",
		 "choices":[{"code":1,"value":"a4","label":"A4"}]}
	]}`)
	raw := map[uint16]string{CapSupportedSize: `[{"container":"ENUMERATION","itemType":4,"currentIndex":1,
		"items":[{"value":1,"isCurrent":false},{"value":60,"isCurrent":true}]}]`}
	d := reg.Defs[0]
	o := d.Build(raw)
	if o == nil || o.Value != "code60" || o.Choices[1].Label != "规格60" {
		t.Fatalf("未知编号输出不对: %+v", o)
	}
	w, err := d.Plan("code60", d.ParseCaps(raw))
	if err != nil || !reflect.DeepEqual(w, []CapWrite{{CapSupportedSize, "60"}}) {
		t.Errorf("未知编号写回不对: %v %v", w, err)
	}
}

// number 的 control 指定与退回。
func TestNumberControl(t *testing.T) {
	reg := mustCompile(t, `{"version":1,"options":[
		{"key":"a","label":"A","kind":"number","cap":"ICAP_BRIGHTNESS","control":"number"},
		{"key":"b","label":"B","kind":"number","cap":"ICAP_CONTRAST","control":"select"},
		{"key":"c","label":"C","kind":"number","cap":"ICAP_BRIGHTNESS","control":"slider","unit":"%"}
	]}`)
	rng := `[{"container":"RANGE","itemType":7,"minValue":-100,"maxValue":100,"stepSize":10,"currentValue":0}]`
	one := `[{"container":"ONEVALUE","itemType":7,"value":5}]`

	a := reg.Defs[0].Build(map[uint16]string{CapBrightness: rng})
	if a.Control != ControlNumber || *a.Min != -100 || *a.Step != 10 {
		t.Errorf("number 控件应带区间: %+v", a)
	}
	// select 但设备报区间、又没配 presets：退回滑块
	b := reg.Defs[1].Build(map[uint16]string{CapContrast: rng})
	if b.Control != ControlSlider {
		t.Errorf("没有 presets 的区间应退回滑块: %+v", b)
	}
	// slider 但设备只报单值：退回数字框
	c := reg.Defs[2].Build(map[uint16]string{CapBrightness: one})
	if c.Control != ControlNumber || c.Unit != "%" {
		t.Errorf("单值应退回数字框: %+v", c)
	}
}

// 配置写错时报的错要能看出是哪一项、哪个字段。
func TestConfigErrors(t *testing.T) {
	cases := []struct {
		name, cfg, errPart string
	}{
		{"版本", `{"version":2,"options":[]}`, "version"},
		{"字段拼错", `{"version":1,"options":[{"key":"a","lable":"A"}]}`, "lable"},
		{"缺 kind", `{"version":1,"options":[{"key":"a","label":"A","cap":"ICAP_BRIGHTNESS"}]}`, "options[0]（a）: 缺少 kind"},
		{"kind 错", `{"version":1,"options":[{"key":"a","label":"A","kind":"list"}]}`, "不认识的 kind"},
		{"能力名错", `{"version":1,"options":[{"key":"a","label":"A","kind":"number","cap":"ICAP_BRIGHT"}]}`, "不认识的能力"},
		{"key 重复", `{"version":1,"options":[
			{"key":"a","label":"A","kind":"number","cap":"ICAP_BRIGHTNESS"},
			{"key":"a","label":"A","kind":"number","cap":"ICAP_CONTRAST"}]}`, "options[1]（a）: key 重复"},
		{"enum 缺 code", `{"version":1,"options":[{"key":"a","label":"A","kind":"enum","cap":"ICAP_PIXELTYPE",
			"choices":[{"value":"bw","label":"黑白"}]}]}`, "choices[0]: 缺少 code"},
		{"enum value 重复", `{"version":1,"options":[{"key":"a","label":"A","kind":"enum","cap":"ICAP_PIXELTYPE",
			"choices":[{"code":0,"value":"x","label":"1"},{"code":1,"value":"x","label":"2"}]}]}`, "value \"x\" 重复"},
		{"combo 缺 set", `{"version":1,"options":[{"key":"a","label":"A","kind":"combo",
			"choices":[{"value":"x","label":"X"}]}]}`, "set 不能为空"},
		{"combo op 错", `{"version":1,"options":[{"key":"a","label":"A","kind":"combo",
			"choices":[{"value":"x","label":"X","set":[{"cap":"CAP_FEEDERENABLED","value":1}],
			"requires":[{"cap":"CAP_DUPLEX","op":"<>","value":0}]}]}]}`, "op 只能是"},
		{"control 错", `{"version":1,"options":[{"key":"a","label":"A","kind":"enum","cap":"ICAP_PIXELTYPE","control":"slider",
			"choices":[{"code":0,"value":"bw","label":"黑白"}]}]}`, "control 只能是"},
		{"disabled 也校验", `{"version":1,"options":[{"key":"a","label":"A","kind":"switch","cap":"NOPE","disabled":true}]}`, "不认识的能力"},
		{"profile 缺 match", `{"version":1,"options":[],"profiles":[{"name":"x","options":[]}]}`, "match 不能为空"},
		{"语法错带行号", "{\"version\":1,\n\"options\":[\n{\"key\" \"a\"}]}", "第 3 行"},
		{"注释没结束", `{"version":1 /* 忘了关`, "没有结束"},
	}
	for _, c := range cases {
		_, err := Compile([]byte(c.cfg))
		if err == nil || !strings.Contains(err.Error(), c.errPart) {
			t.Errorf("%s: 期望报错含 %q，得到 %v", c.name, c.errPart, err)
		}
	}
}

// 注释、多余逗号、字符串里的 // 都要处理对。
func TestJSONC(t *testing.T) {
	reg := mustCompile(t, `
	// 行注释
	{
	  "version": 1, /* 块
	  注释 */
	  "options": [
	    {"key": "a", "label": "http://x/*不是注释*/", "kind": "number", "cap": "0x1101",},
	  ],
	}`)
	if len(reg.Defs) != 1 || reg.Defs[0].Label != "http://x/*不是注释*/" || reg.Defs[0].Caps[0] != CapBrightness {
		t.Errorf("JSONC 解析不对: %+v", reg.Defs)
	}
}

// 内置默认配置：能编译、key 不重复、示例项默认不生效但写法正确。
func TestDefaultConfig(t *testing.T) {
	reg := mustCompile(t, string(DefaultConfig))
	var keys []string
	for _, d := range reg.Defs {
		keys = append(keys, d.Key)
		if d.Read == nil || d.Plan == nil || len(d.Caps) == 0 {
			t.Errorf("%s: 编译结果不完整", d.Key)
		}
	}
	if want := []string{"source", "colorMode", "dpi", "paperSize", "brightness", "contrast"}; !reflect.DeepEqual(keys, want) {
		t.Errorf("默认生效的项 %v，期望 %v", keys, want)
	}
}

func mustCompile(t *testing.T, cfg string) *Registry {
	t.Helper()
	reg, err := Compile([]byte(cfg))
	if err != nil {
		t.Fatalf("配置编译失败: %v", err)
	}
	return reg
}

var update = flag.Bool("update", false, "按当前内置配置重新生成 testdata/devices 下的 .expected.json")

// 真实设备回归：testdata/devices 下每个 capdump 文件，用内置配置拼出设置项，
// 和同名的 .expected.json 比对。改了内置配置导致某台已适配的设备输出变了，这里会报出来。
//
// 新适配一台设备：把 capdump 文件拷进 testdata/devices，跑 go test ./scanopt -update 生成 .expected.json，
// 逐项核对无误后一起提交。之后改配置时若有意改变了输出，同样 -update，并在提交里检查 diff。
func TestDeviceRegression(t *testing.T) {
	files, _ := filepath.Glob("testdata/devices/*.json")
	n := 0
	for _, f := range files {
		if strings.HasSuffix(f, ".expected.json") {
			continue
		}
		n++
		t.Run(filepath.Base(f), func(t *testing.T) {
			device, raw := loadDump(t, f)
			got, err := json.MarshalIndent(Builtin().build(raw, device), "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, '\n')

			golden := strings.TrimSuffix(f, ".json") + ".expected.json"
			if *update {
				if err := os.WriteFile(golden, got, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("缺少 %s：新加的设备先跑 go test ./scanopt -update 生成，核对后提交", golden)
			}
			if !bytes.Equal(bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n")), got) {
				t.Errorf("设备 %s 的设置项和 %s 不一致。\n"+
					"如果是有意改的配置，跑 go test ./scanopt -update 更新，并检查 .expected.json 的 diff；否则是改坏了。\n"+
					"当前输出:\n%s", device, golden, got)
			}
		})
	}
	if n == 0 {
		t.Skip("testdata/devices 下还没有设备的能力导出")
	}
}

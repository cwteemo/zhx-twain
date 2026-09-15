package scanopt

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"
)

const virtualDevice = "TWAIN2 Software Scanner"

// loadRaw 读 testdata 里模拟的 DLL 输出。文件格式：{"能力编号(十进制)": "DLL 原文"}。
// TODO(TODO-settings.md #13): virtual_scanner.json 是按 DS 源码默认值推出来的，不是真实抓取。
// 想对着一台真实设备回归，把 twain.log / 接口返回的原文照这个格式存一份即可。
func loadRaw(t *testing.T, name string) map[uint16]string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]string
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	out := make(map[uint16]string, len(m))
	for k, v := range m {
		n, err := strconv.ParseUint(k, 10, 16)
		if err != nil {
			t.Fatalf("testdata 键 %q 不是十进制编号", k)
		}
		out[uint16(n)] = v
	}
	return out
}

func byName(opts []Option) map[string]Option {
	m := make(map[string]Option, len(opts))
	for _, o := range opts {
		m[o.Name] = o
	}
	return m
}

func TestBuildVirtualScanner(t *testing.T) {
	opts := byName(Build(loadRaw(t, "virtual_scanner.json"), virtualDevice))

	cases := []struct {
		name, typ string
		value     any
		list      []string
	}{
		{"source", TypeStrList, "ADF Front", []string{"Flatbed", "ADF Front"}},
		{"mode", TypeStrList, "Color", []string{"Lineart", "Gray", "Color"}},
		{"resolution", TypeStrList, "200", []string{"50", "100", "150", "200", "300", "400", "500", "600"}},
		{"paper-size", TypeStrList, "US Letter", []string{"None", "US Letter", "US Legal"}},
		{"units", TypeStrList, "Inches", []string{"Inches", "Pixels", "Centimeters", "Picas", "Points", "Twips"}},
		{"brightness", TypeIntRange, 0.0, nil},
		{"threshold", TypeIntRange, 128.0, nil},
		{"gamma", TypeInt, 1.0, nil},
		{"long-paper-scan", TypeBool, 0, nil},
		{"documents-in-adf", TypeInt, 20.0, nil},
	}
	for _, c := range cases {
		o, ok := opts[c.name]
		if !ok {
			t.Errorf("%s: 缺项", c.name)
			continue
		}
		if o.Type != c.typ || o.Value != c.value {
			t.Errorf("%s: 得到 type=%s value=%#v，期望 type=%s value=%#v", c.name, o.Type, o.Value, c.typ, c.value)
		}
		if c.list != nil && !equal(o.List, c.list) {
			t.Errorf("%s: 得到 list=%v，期望 %v", c.name, o.List, c.list)
		}
	}

	if b := opts["brightness"]; b.Min == nil || *b.Min != -1000 || *b.Max != 1000 || *b.Step != 1 {
		t.Errorf("brightness 区间不对: %+v", b)
	}
	if opts["mode"].Option != 2 {
		t.Errorf("mode 的 option 编号应为 2，得到 %d", opts["mode"].Option)
	}
}

// 真实设备：不读自定义能力，也不输出只属于虚拟扫描仪的项。
func TestCodesSkipsVirtualOnlyForRealDevice(t *testing.T) {
	for _, c := range Codes("Uniscan Q400") {
		if c >= 0x8000 {
			t.Errorf("真实设备不应读自定义能力 0x%04x", c)
		}
	}
	opts := byName(Build(loadRaw(t, "virtual_scanner.json"), "Uniscan Q400"))
	if _, ok := opts["long-paper-scan"]; ok {
		t.Error("真实设备不应输出 long-paper-scan")
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

// 支持双面的设备才给 ADF Duplex，且双面开着时当前值是 ADF Duplex。
func TestSourceDuplex(t *testing.T) {
	opts := byName(Build(map[uint16]string{
		CapFeederEnabled: `[{"container":"ONEVALUE","itemType":6,"itemTypeName":"BOOL","value":true}]`,
		CapDuplex:        `[{"container":"ONEVALUE","itemType":4,"itemTypeName":"UINT16","value":1}]`,
		CapDuplexEnabled: `[{"container":"ONEVALUE","itemType":6,"itemTypeName":"BOOL","value":true}]`,
	}, "Uniscan Q400"))
	s := opts["source"]
	if s.Value != "ADF Duplex" || !equal(s.List, []string{"Flatbed", "ADF Front", "ADF Duplex"}) {
		t.Errorf("source 不对: %+v", s)
	}
}

// 定义表自检：name 不重复、每条都有 Build 和至少一个能力、自定义能力必须标 VirtualOnly。
func TestDefsSanity(t *testing.T) {
	seen := map[string]bool{}
	for _, d := range Defs {
		if seen[d.Name] {
			t.Errorf("name 重复: %s", d.Name)
		}
		seen[d.Name] = true
		if d.Build == nil || len(d.Caps) == 0 {
			t.Errorf("%s: 缺 Build 或 Caps", d.Name)
		}
		for _, c := range d.Caps {
			if c >= 0x8000 && !d.VirtualOnly {
				t.Errorf("%s: 用了自定义能力 0x%04x 却没标 VirtualOnly", d.Name, c)
			}
		}
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

package twaindiag

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 64 位服务 + 只装了 32 位驱动：这是柯达那几款老机型枚举不出来的典型现场，
// 结论必须点名是哪几个驱动、并告诉现场换 32 位版。
func TestConcludeOnlyOtherBitness(t *testing.T) {
	d := Diagnosis{
		ProcessArch: "amd64", ProcessBits: 64,
		WindowsDir: `C:\Windows`,
		DSMPath:    `C:\Windows\twain_64\TWAINDSM.dll`, DSMFound: true,
		Enumerated: []string{"Uniscan Q400"},
		Dirs: []DSDir{
			{Path: `C:\Windows\twain_32`, Exists: true, Bits: 32, Usable: false,
				Sources: []DSEntry{{Name: "KODAKS.ds"}, {Name: "KDSi2000.ds"}}},
			{Path: `C:\Windows\twain_64`, Exists: true, Bits: 64, Usable: true,
				Sources: []DSEntry{{Name: "uniscan.ds"}}},
		},
	}

	got := strings.Join(Conclude(d), "\n")
	for _, want := range []string{"KODAKS.ds", "KDSi2000.ds", "32 位", "枚举不出来"} {
		if !strings.Contains(got, want) {
			t.Errorf("结论里少了 %q:\n%s", want, got)
		}
	}
	// 同名驱动两边都装了的不该被点名
	if strings.Contains(got, "uniscan.ds") {
		t.Errorf("64 位能用的驱动不该出现在结论里:\n%s", got)
	}
}

// 两边位数一致、驱动数和枚举数对得上：不该无端报问题。
func TestConcludeHealthy(t *testing.T) {
	d := Diagnosis{
		ProcessArch: "386", ProcessBits: 32, WindowsDir: `C:\Windows`,
		DSMPath: `C:\Windows\twain_32\TWAINDSM.dll`, DSMFound: true,
		Enumerated: []string{"A", "B"},
		Dirs: []DSDir{
			{Path: `C:\Windows\twain_32`, Exists: true, Bits: 32, Usable: true,
				Sources: []DSEntry{{Name: "a.ds"}, {Name: "b.ds"}}},
			{Path: `C:\Windows\twain_64`, Exists: true, Bits: 64, Usable: false, Sources: []DSEntry{}},
		},
	}

	got := strings.Join(Conclude(d), "\n")
	if !strings.Contains(got, "没发现明显问题") {
		t.Errorf("健康的环境不该报问题:\n%s", got)
	}
}

// 少了 DSM 是"一台都枚举不到"的另一个常见原因，要单独说。
func TestConcludeMissingDSM(t *testing.T) {
	d := Diagnosis{
		ProcessBits: 64, WindowsDir: `C:\Windows`,
		DSMNextToExe: `D:\scansvc\TWAINDSM.dll`, DSMNextToExeFound: false,
		DSMPath: `C:\Windows\twain_64\TWAINDSM.dll`, DSMFound: false,
		Enumerated: []string{},
		Dirs: []DSDir{
			{Path: `C:\Windows\twain_32`, Bits: 32, Sources: []DSEntry{}},
			{Path: `C:\Windows\twain_64`, Bits: 64, Usable: true, Sources: []DSEntry{}},
		},
	}

	got := strings.Join(Conclude(d), "\n")
	if !strings.Contains(got, "TWAINDSM.dll") {
		t.Errorf("没提 TWAINDSM.dll:\n%s", got)
	}
}

// DSM 装在系统目录但没拷到 exe 旁边：DSM 是用裸文件名加载的，那里找不到，
// 结论要说清"拷到 exe 旁边"，不能只说"没装"。
func TestConcludeDSMOnlyInSystemDir(t *testing.T) {
	d := Diagnosis{
		ProcessBits: 64, WindowsDir: `C:\Windows`,
		DSMNextToExe: `D:\scansvc\TWAINDSM.dll`, DSMNextToExeFound: false,
		DSMPath: `C:\Windows\twain_64\TWAINDSM.dll`, DSMFound: true,
		Enumerated: []string{},
		Dirs: []DSDir{
			{Path: `C:\Windows\twain_32`, Bits: 32, Sources: []DSEntry{}},
			{Path: `C:\Windows\twain_64`, Bits: 64, Usable: true, Sources: []DSEntry{{Name: "a.ds"}}},
		},
	}

	got := strings.Join(Conclude(d), "\n")
	if !strings.Contains(got, "exe 旁边") {
		t.Errorf("应提示把 DSM 拷到 exe 旁边:\n%s", got)
	}
}

// 已经枚举到设备，说明 DSM 明明加载成功了，不能因为路径判断失败就报它缺失
// （服务可能是从别的目录、或者按 PATH 找到的）。
func TestConcludeNoDSMComplaintWhenDevicesFound(t *testing.T) {
	d := Diagnosis{
		ProcessBits: 64, WindowsDir: `C:\Windows`,
		DSMNextToExe: `D:\scansvc\TWAINDSM.dll`, DSMNextToExeFound: false,
		DSMPath: `C:\Windows\twain_64\TWAINDSM.dll`, DSMFound: false,
		Enumerated: []string{"A"},
		Dirs: []DSDir{
			{Path: `C:\Windows\twain_32`, Bits: 32, Sources: []DSEntry{}},
			{Path: `C:\Windows\twain_64`, Bits: 64, Usable: true, Sources: []DSEntry{{Name: "a.ds"}}},
		},
	}

	got := strings.Join(Conclude(d), "\n")
	if strings.Contains(got, "TWAINDSM.dll") {
		t.Errorf("枚举到设备时不该抱怨 DSM:\n%s", got)
	}
}

// 非 Windows 上只给一句话，不要跑出一堆空目录的结论。
func TestConcludeNotWindows(t *testing.T) {
	got := Conclude(Diagnosis{})
	if len(got) != 1 || !strings.Contains(got[0], "Windows") {
		t.Errorf("非 Windows 的结论不对: %v", got)
	}
}

// 驱动可以放在 twain_xx 的一级子目录里，DSM 也是这么找的，不能漏。
func TestListDataSourcesIncludesSubdir(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "top.ds"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notadriver.dll"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "vendor")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	// 大写扩展名也算：Windows 上大小写不敏感，驱动装出来两种写法都见过
	if err := os.WriteFile(filepath.Join(sub, "Nested.DS"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ListDataSources(root)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range got {
		names = append(names, e.Name)
	}
	if len(names) != 2 {
		t.Fatalf("应找到 top.ds 和 Nested.DS，实际 %v", names)
	}
	if strings.Join(names, ",") != "Nested.DS,top.ds" {
		t.Errorf("结果应按名字排序，实际 %v", names)
	}
}

// 目录不存在时报错而不是 panic。
func TestListDataSourcesMissingDir(t *testing.T) {
	if _, err := ListDataSources(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("目录不存在应该返回错误")
	}
}

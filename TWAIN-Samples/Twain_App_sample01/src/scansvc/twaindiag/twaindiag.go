package twaindiag

// TWAIN 环境体检：回答"驱动明明装了，为什么这台扫描仪枚举不出来"。
//
// 最常见的原因是位数不匹配。TWAIN 的数据源（.ds 文件）本质上是 DLL，由 DSM
// 直接加载进调用方进程，所以 32 位进程只能用 C:\Windows\twain_32 下的驱动，
// 64 位进程只能用 C:\Windows\twain_64 下的——跨位数没有任何办法，
// 不是配置问题。柯达这类老机型不少只发 32 位驱动，装在 64 位 Windows 上
// 也只会落到 twain_32，64 位的 scansvc 就怎么都看不见它。
//
// 所以这里把两个目录里实际装了什么列出来，和 DSM 枚举到的清单摆在一起，
// 再直接给一句结论，现场不用猜也不用翻日志。
//
// 结论只是最可能的解释，不是断言：一个 .ds 可以对应多台设备，也有驱动
// 把真正的数据源装在别处、twain_xx 下只放一个壳。拿不准就看 twain.log，
// 里面有每台设备的 identity（厂商、TWAIN 协议版本、SupportedGroups）。

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// DSEntry 是一个已安装的 TWAIN 数据源文件。
type DSEntry struct {
	Name string `json:"name"` // 文件名，如 KODAKS.ds
	Path string `json:"path"` // 完整路径
	Size int64  `json:"size"`
}

// DSDir 是 twain_32 / twain_64 中的一个。
type DSDir struct {
	Path    string    `json:"path"`
	Exists  bool      `json:"exists"`
	Bits    int       `json:"bits"`    // 这个目录放的是几位的驱动
	Usable  bool      `json:"usable"`  // 本进程能不能用（位数是否匹配）
	Sources []DSEntry `json:"sources"` // 目录里的 .ds
	ReadErr string    `json:"readErr,omitempty"`
}

// Diagnosis 是 /api/diagnose 的响应体。
type Diagnosis struct {
	ProcessArch string `json:"processArch"` // amd64 / 386
	ProcessBits int    `json:"processBits"` // 64 / 32
	WindowsDir  string `json:"windowsDir"`
	// DSM 是用裸文件名 LoadLibrary 加载的（DSMInterface.cpp），所以真正生效的是
	// exe 旁边那一份；系统目录下的 twain_xx\TWAINDSM.dll **不在** 搜索路径里。
	// 两处都要报，只说一处会把现场引到错的方向去。
	DSMNextToExe      string   `json:"dsmNextToExe"`      // exe 旁边的 TWAINDSM.dll（真正会被加载的）
	DSMNextToExeFound bool     `json:"dsmNextToExeFound"` //
	DSMPath           string   `json:"dsmPath"`           // 系统目录下本位数对应的那份（安装包装进去的）
	DSMFound          bool     `json:"dsmFound"`          //
	Enumerated        []string `json:"enumerated"`
	Dirs              []DSDir  `json:"dirs"`
	Conclusion        []string `json:"conclusion"` // 人话结论，逐条
}

// processBits 是本进程的位数。cgo 链接的 DLL 和它一致，DSM、数据源也必须一致。
func ProcessBits() int {
	switch runtime.GOARCH {
	case "386", "arm":
		return 32
	default:
		return 64
	}
}

// windowsDir 返回 Windows 目录（C:\Windows）。非 Windows 上返回空串。
func WindowsDir() string {
	if runtime.GOOS != "windows" {
		return ""
	}
	if d := os.Getenv("SystemRoot"); d != "" {
		return d
	}
	if d := os.Getenv("windir"); d != "" {
		return d
	}
	return `C:\Windows`
}

// listDataSources 列出一个 twain_xx 目录下的数据源。
//
// 连一级子目录一起找：TWAIN 规范允许驱动把 .ds 放进 twain_xx\<厂商>\ 子目录，
// DSM 也是这么枚举的，只看顶层会漏掉一部分驱动。
func ListDataSources(dir string) ([]DSEntry, error) {
	var out []DSEntry

	collect := func(d string) error {
		items, err := os.ReadDir(d)
		if err != nil {
			return err
		}
		for _, it := range items {
			if it.IsDir() {
				continue
			}
			if !strings.EqualFold(filepath.Ext(it.Name()), ".ds") {
				continue
			}
			e := DSEntry{Name: it.Name(), Path: filepath.Join(d, it.Name())}
			if info, err := it.Info(); err == nil {
				e.Size = info.Size()
			}
			out = append(out, e)
		}
		return nil
	}

	if err := collect(dir); err != nil {
		return nil, err
	}

	// 一级子目录
	if items, err := os.ReadDir(dir); err == nil {
		for _, it := range items {
			if it.IsDir() {
				// 子目录读不动就跳过，不影响整体结果
				_ = collect(filepath.Join(dir, it.Name()))
			}
		}
	}

	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

// diagnoseTwain 做一次体检。会调 DSM 枚举设备，所以要占用一次 TWAIN 线程。
func Collect(enumerated []string) Diagnosis {
	bits := ProcessBits()
	win := WindowsDir()

	d := Diagnosis{
		ProcessArch: runtime.GOARCH,
		ProcessBits: bits,
		WindowsDir:  win,
		Enumerated:  enumerated,
	}
	if d.Enumerated == nil {
		d.Enumerated = []string{}
	}

	// exe 旁边那一份才是真正会被加载的
	if exe, err := os.Executable(); err == nil {
		d.DSMNextToExe = filepath.Join(filepath.Dir(exe), "TWAINDSM.dll")
		if _, err := os.Stat(d.DSMNextToExe); err == nil {
			d.DSMNextToExeFound = true
		}
	}

	if win != "" {
		// DSM 也分位数，各自躺在对应目录里。
		dsmDir := "twain_64"
		if bits == 32 {
			dsmDir = "twain_32"
		}
		d.DSMPath = filepath.Join(win, dsmDir, "TWAINDSM.dll")
		if _, err := os.Stat(d.DSMPath); err == nil {
			d.DSMFound = true
		}

		for _, cand := range []struct {
			dir  string
			bits int
		}{{"twain_32", 32}, {"twain_64", 64}} {
			entry := DSDir{
				Path:   filepath.Join(win, cand.dir),
				Bits:   cand.bits,
				Usable: cand.bits == bits,
			}
			if st, err := os.Stat(entry.Path); err == nil && st.IsDir() {
				entry.Exists = true
				sources, err := ListDataSources(entry.Path)
				if err != nil {
					entry.ReadErr = err.Error()
				}
				entry.Sources = sources
			}
			if entry.Sources == nil {
				entry.Sources = []DSEntry{}
			}
			d.Dirs = append(d.Dirs, entry)
		}
	}

	d.Conclusion = Conclude(d)
	return d
}

// conclude 把体检数据翻成人话。纯函数，方便测。
func Conclude(d Diagnosis) []string {
	var out []string

	if d.WindowsDir == "" {
		return []string{"当前不是 Windows，TWAIN 体检只在 Windows 上有意义。"}
	}

	var usable, unusable *DSDir
	for i := range d.Dirs {
		if d.Dirs[i].Usable {
			usable = &d.Dirs[i]
		} else {
			unusable = &d.Dirs[i]
		}
	}

	// 已经枚举到设备就说明 DSM 加载成功了，别再拿路径判断去打扰现场。
	if len(d.Enumerated) == 0 && !d.DSMNextToExeFound {
		if d.DSMFound {
			out = append(out, fmt.Sprintf(
				"TWAINDSM.dll 不在 exe 旁边（%s）。它是用裸文件名加载的，系统目录下的 %s 不在搜索路径里，"+
					"把那一份拷到 exe 旁边即可——少了它一台扫描仪都枚举不到。",
				d.DSMNextToExe, d.DSMPath))
		} else {
			out = append(out, fmt.Sprintf(
				"两处都没有 TWAINDSM.dll（exe 旁边的 %s、系统目录的 %s）——少了它一台扫描仪都枚举不到。"+
					"装 releases 里对应位数的 twainapp 安装包，再把它拷到 exe 旁边。",
				d.DSMNextToExe, d.DSMPath))
		}
	}

	if usable == nil || len(usable.Sources) == 0 {
		where := fmt.Sprintf("%s\\twain_%d", d.WindowsDir, d.ProcessBits)
		out = append(out, fmt.Sprintf(
			"%s 里没有任何 .ds 驱动文件，所以本进程（%d 位）一台扫描仪也枚举不到。",
			where, d.ProcessBits))
	}

	// 核心那一条：另一个位数的目录里装了驱动，本进程用不了。
	if unusable != nil && len(unusable.Sources) > 0 {
		var onlyOther []string
		for _, s := range unusable.Sources {
			found := false
			if usable != nil {
				for _, u := range usable.Sources {
					if strings.EqualFold(u.Name, s.Name) {
						found = true
						break
					}
				}
			}
			if !found {
				onlyOther = append(onlyOther, s.Name)
			}
		}
		if len(onlyOther) > 0 {
			out = append(out, fmt.Sprintf(
				"%s 里有 %d 个驱动只装了 %d 位版：%s。本进程是 %d 位的，TWAIN 的数据源要加载进本进程，"+
					"位数不同没法用——这些扫描仪在这个服务里就是枚举不出来。"+
					"要用它们，请换 %d 位版的 scansvc（build.bat -x86 / -x64 各出一套，同一份代码），"+
					"或者装这些设备的 %d 位驱动。",
				unusable.Path, len(onlyOther), unusable.Bits, strings.Join(onlyOther, "、"),
				d.ProcessBits, unusable.Bits, d.ProcessBits))
		}
	}

	if usable != nil && len(usable.Sources) > len(d.Enumerated) {
		out = append(out, fmt.Sprintf(
			"%s 里有 %d 个驱动文件，但只枚举到 %d 台设备。一个驱动对应多台设备、或者驱动装了一半都会这样；"+
				"具体是哪台没出来，看 twain.log 里 \"Additional source found\" 和 \"MSG_GETNEXT failed\" 那几行。",
			usable.Path, len(usable.Sources), len(d.Enumerated)))
	}

	if len(out) == 0 {
		out = append(out, fmt.Sprintf("没发现明显问题：%d 位进程，枚举到 %d 台设备。",
			d.ProcessBits, len(d.Enumerated)))
	}
	return out
}

package main

// 导出扫描仪能力的"真实结构"。
//
// 适配一台新扫描仪之前，先把它支持的全部 TWAIN 能力原样读出来存成一个 JSON 文件，
// 再照着真实数据去写 scanopt 里的映射规则——不同厂商同一项能力报的容器类型、取值范围
// 经常不一样（分辨率有的报 RANGE 有的报 ENUMERATION），靠猜写出来的规则一上真机就错。
//
// 触发方式（任选一个，都会先连上设备）：
//
//	scansvc-tools.bat dump "<设备名>"                     现场用这个，双击也有菜单，不依赖 curl
//	GET  /api/capabilities/dump?device=<设备名>          HTTP
//	{"cmd":"dumpCapabilities","params":{"device":"..."}}  WebSocket（cmd 协议）
//	{"handle":"dumpCapabilities","scanner":"..."}         WebSocket（既有前端的 handle 协议）
//
// 结果既在响应里返回，也存到 exe 旁边的 capdump\<设备名>_<时间>.json。
// DLL 那边同时会把每一项写一行 "@CAP ..." 到 twain.log，两处内容一致。

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"scansvc/scanopt"
)

// capSupportedCaps 是 CAP_SUPPORTEDCAPS：设备自己报的"我支持哪些能力"，ARRAY 容器。
const capSupportedCaps uint16 = 0x1005

const capDumpDirName = "capdump"

type capDumpEntry struct {
	Code string `json:"code"` // 十六进制编号，如 0x0101
	Name string `json:"name"` // twain.h 里的宏名；厂商自定义能力（0x8000 以上）为空
	// Raw 是 DLL zhx_GetCapability_STR 的原样输出（一个只含一项的数组）。
	// 万一 DLL 拼出来的不是合法 JSON，这里存成字符串，方便看坏在哪。
	Raw json.RawMessage `json:"raw"`
}

type capDump struct {
	Device  string `json:"device"`
	Time    string `json:"time"`
	Service string `json:"service"`
	// SupportedCapsReported 为 false 表示设备没报 CAP_SUPPORTEDCAPS，
	// 这时是拿 twain.h 里全部标准能力逐项试读的，厂商自定义能力读不到。
	SupportedCapsReported bool           `json:"supportedCapsReported"`
	Caps                  []capDumpEntry `json:"caps"`
	// Unreadable 是设备在 CAP_SUPPORTEDCAPS 里列了、MSG_GET 却读不出来的能力。
	Unreadable []string `json:"unreadable"`
	// Options 是按现有 scanopt 规则拼出来的 getScannerOptions 结果，和 Caps 对照着看。
	Options []scanopt.Option `json:"options"`
	File    string           `json:"file,omitempty"`
}

// TwainDumpCapabilities 读出设备支持的全部能力并存盘。
// device 非空时先连上这台；为空则用当前已连接的设备。
func TwainDumpCapabilities(device string) (*capDump, error) {
	reloadOptionsConfig()
	if strings.TrimSpace(device) != "" {
		if err := TwainConnect(device); err != nil {
			return nil, err
		}
	}
	st := TwainStatus()
	if !st.Connected {
		return nil, fmt.Errorf("尚未连接扫描仪，请带上设备名")
	}
	device = st.Device

	// 先问设备支持哪些，报了就只读这些；没报就把标准能力全试一遍。
	supported, err := TwainReadCapabilities([]uint16{capSupportedCaps})
	if err != nil {
		return nil, err
	}
	listed := supportedCapCodes(supported[capSupportedCaps])

	codeSet := map[uint16]bool{capSupportedCaps: true}
	for _, c := range listed {
		codeSet[c] = true
	}
	if len(listed) == 0 {
		for c := range scanopt.CapNames {
			codeSet[c] = true
		}
	}
	// scanopt 要用的也读上，保证 Options 能拼出来。
	for _, c := range scanopt.Codes(device) {
		codeSet[c] = true
	}
	codes := make([]uint16, 0, len(codeSet))
	for c := range codeSet {
		codes = append(codes, c)
	}
	sort.Slice(codes, func(i, j int) bool { return codes[i] < codes[j] })

	raw, err := TwainReadCapabilities(codes)
	if err != nil {
		return nil, err
	}

	listedSet := map[uint16]bool{}
	for _, c := range listed {
		listedSet[c] = true
	}

	dump := &capDump{
		Device:                device,
		Time:                  time.Now().Format("2006-01-02 15:04:05"),
		Service:               serviceVersion,
		SupportedCapsReported: len(listed) > 0,
		Caps:                  []capDumpEntry{},
		Unreadable:            []string{},
	}
	for _, c := range codes {
		s := strings.TrimSpace(raw[c])
		if s == "" || s == "[]" {
			// 没列在 SUPPORTEDCAPS 里的读不到是正常的（试读模式下大部分都这样），不记。
			if listedSet[c] {
				dump.Unreadable = append(dump.Unreadable, capLabel(c))
			}
			continue
		}
		entry := capDumpEntry{Code: fmt.Sprintf("0x%04x", c), Name: scanopt.CapName(c)}
		if json.Valid([]byte(s)) {
			entry.Raw = json.RawMessage(s)
		} else {
			entry.Raw, _ = json.Marshal(s)
		}
		dump.Caps = append(dump.Caps, entry)
	}
	dump.Options = scanopt.Build(raw, device)

	if path, err := saveCapDump(dump); err != nil {
		// 存不下来不影响响应里带回结果。
		log.Printf("能力导出写文件失败: %v", err)
	} else {
		dump.File = path
		log.Printf("已导出 %s 的 %d 项能力到 %s", device, len(dump.Caps), path)
	}
	return dump, nil
}

// supportedCapCodes 从 CAP_SUPPORTEDCAPS 的原始输出里取出能力编号。
func supportedCapCodes(s string) []uint16 {
	c := scanopt.ParseCap(capSupportedCaps, s)
	var out []uint16
	for _, v := range c.Choices() {
		if v > 0 && v <= 0xFFFF {
			out = append(out, uint16(v))
		}
	}
	return out
}

func capLabel(code uint16) string {
	if name := scanopt.CapName(code); name != "" {
		return fmt.Sprintf("0x%04x %s", code, name)
	}
	return fmt.Sprintf("0x%04x", code)
}

// saveCapDump 存到 exe 旁边的 capdump 目录（和 scansvc.conf 同一个地方，好找）。
func saveCapDump(d *capDump) (string, error) {
	dir := capDumpDirName
	if exe, err := os.Executable(); err == nil {
		dir = filepath.Join(filepath.Dir(exe), capDumpDirName)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	name := fmt.Sprintf("%s_%s.json", safeFileName(d.Device), time.Now().Format("20060102-150405"))
	path := filepath.Join(dir, name)

	body, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, body, 0o644)
}

// safeFileName 把设备名里 Windows 文件名不允许的字符换成下划线。
func safeFileName(s string) string {
	s = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`\/:*?"<>| `, r) || r < 0x20 {
			return '_'
		}
		return r
	}, strings.TrimSpace(s))
	if s == "" {
		return "unknown"
	}
	return s
}

func handleCapabilityDump(w http.ResponseWriter, r *http.Request) {
	dump, err := TwainDumpCapabilities(r.URL.Query().Get("device"))
	if err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(dump)
}

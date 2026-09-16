package main

// 扫描仪设置项（getScannerOptions / setScannerOptions）的服务侧：
// 加载设置项配置文件、在 TWAIN 线程上读写能力、缓存读到的结果。翻译规则全在 scanopt 包里。
// 协议文档（给前端）见 SCANNER_OPTIONS_API.md，配置文件写法见 SCANNER_OPTIONS_CONFIG.md。

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"scansvc/scanopt"
)

// scannerOptionsData 是 getScannerOptions 返回的 data。
type scannerOptionsData struct {
	Version   int    `json:"version"`
	Scanner   string `json:"scanner"`
	Connected bool   `json:"connected"` // 设备当前是否连着
	// Cached 为 true 表示设备没连着，options 是上次连着时读到的；
	// 为 false 且 options 为空，说明这台设备还从来没连过（扫描一次之后就有了）。
	Cached    bool             `json:"cached"`
	UpdatedAt string           `json:"updatedAt,omitempty"` // options 是什么时候从设备读的
	Options   []scanopt.Option `json:"options"`
}

// optionResult 是一项设置的下发结果。
type optionResult struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
	OK    bool   `json:"ok"`
	// Skipped 为 true 表示这台扫描仪没有这一项（或者 key 不认识），已忽略，不算失败。
	// 前端的配置方案可能是在别的型号上存的，带着这台没有的项很正常。
	Skipped bool   `json:"skipped,omitempty"`
	Error   string `json:"error,omitempty"`
}

// ---- 读 ----

// ScannerOptionsFor 返回 getScannerOptions 的结果，**绝不打开设备**。
// 设备正连着（且就是要问的这台）时现读；否则给缓存。
func ScannerOptionsFor(device string) scannerOptionsData {
	reloadOptionsConfig()
	st := TwainStatus()
	if device == "" {
		device = st.Device
	}
	if st.Connected && device == st.Device {
		if data, err := TwainScannerOptions(); err == nil {
			return data
		}
		// 刚好在这期间断开了，退回缓存。
	}

	if cached, ok := cachedCaps(device); ok {
		// 缓存的是设备的原始能力，按当前配置现拼：改了配置不用重新连设备也能看到效果。
		return scannerOptionsData{
			Version:   scanopt.ProtocolVersion,
			Scanner:   device,
			Cached:    true,
			UpdatedAt: cached.UpdatedAt,
			Options:   scanopt.Build(cached.Caps, device),
		}
	}
	return scannerOptionsData{
		Version: scanopt.ProtocolVersion,
		Scanner: device,
		Options: []scanopt.Option{},
	}
}

// TwainScannerOptions 从当前已连接的设备现读设置项，并更新缓存。
func TwainScannerOptions() (scannerOptionsData, error) {
	reloadOptionsConfig()
	var data scannerOptionsData
	var err error
	inTwain(func() {
		data, err = scannerOptionsOnTwainThread()
	})
	return data, err
}

func scannerOptionsOnTwainThread() (scannerOptionsData, error) {
	device := currentDeviceOnTwainThread()
	if device == "" {
		return scannerOptionsData{}, fmt.Errorf("尚未连接扫描仪")
	}
	raw := readCapsOnTwainThread(scanopt.Codes(device))
	data := scannerOptionsData{
		Version:   scanopt.ProtocolVersion,
		Scanner:   device,
		Connected: true,
		UpdatedAt: time.Now().Format("2006-01-02 15:04:05"),
		Options:   scanopt.Build(raw, device),
	}
	storeCaps(capsCache{Scanner: device, UpdatedAt: data.UpdatedAt, Caps: raw})
	return data, nil
}

// ---- 写 ----

// TwainApplyScannerOptions 把前端传来的设置项（key -> 取值）下发到当前已连接的设备，
// 返回每一项的结果，以及下发之后现读的最新设置项。
//
// 按定义表的顺序逐项处理，每项下发前现读它的能力：前一项（比如扫描方式）
// 可能改变后一项（比如分辨率）的可选值，拿旧状态校验会误判。
// 某项失败不影响其它项，是否因此放弃扫描由调用方决定。
func TwainApplyScannerOptions(values map[string]any) ([]optionResult, scannerOptionsData, error) {
	reloadOptionsConfig()
	var results []optionResult
	var data scannerOptionsData
	var err error

	inTwain(func() {
		device := currentDeviceOnTwainThread()
		if device == "" {
			err = fmt.Errorf("尚未连接扫描仪")
			return
		}

		defs := scanopt.DefsFor(device)
		known := map[string]bool{}
		for _, d := range defs {
			known[d.Key] = true
			v, present := values[d.Key]
			if !present {
				continue
			}
			results = append(results, applyOptionOnTwainThread(d, v))
		}

		// 不认识的 key 排在最后，按字母序，结果稳定好对照。
		var unknown []string
		for k := range values {
			if !known[k] {
				unknown = append(unknown, k)
			}
		}
		sort.Strings(unknown)
		for _, k := range unknown {
			results = append(results, optionResult{Key: k, Value: values[k], Skipped: true, Error: "不认识的设置项"})
		}

		data, _ = scannerOptionsOnTwainThread()
	})

	return results, data, err
}

func applyOptionOnTwainThread(d scanopt.Def, v any) optionResult {
	res := optionResult{Key: d.Key, Value: v}

	caps := d.ParseCaps(readCapsOnTwainThread(d.Caps))
	if d.Read(caps) == nil {
		res.Skipped = true
		res.Error = "该扫描仪没有这一项"
		return res
	}

	writes, err := d.Plan(v, caps)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	for _, w := range writes {
		r := setCapOnTwainThread(d.Key, fmt.Sprintf("%d", w.Code), w.Value)
		if !r.OK {
			res.Error = fmt.Sprintf("扫描仪拒绝了这个设置（%s = %s：%s）", scanopt.CapName(w.Code), w.Value, r.Error)
			log.Printf("设置项 %s = %v 下发失败: 0x%04x = %s，%s", d.Key, v, w.Code, w.Value, r.Error)
			return res
		}
	}
	res.OK = true
	return res
}

// failedOptions 挑出真正失败的项（不含 skipped），拼成一句给用户看的话；都成功返回空串。
func failedOptions(results []optionResult, labelOf map[string]string) string {
	var parts []string
	for _, r := range results {
		if r.OK || r.Skipped {
			continue
		}
		name := labelOf[r.Key]
		if name == "" {
			name = r.Key
		}
		parts = append(parts, fmt.Sprintf("%s：%s", name, r.Error))
	}
	return strings.Join(parts, "；")
}

// labelsOf 取设置项的中文名，拼错误信息用。
func labelsOf(device string) map[string]string {
	m := map[string]string{}
	for _, d := range scanopt.DefsFor(device) {
		m[d.Key] = d.Label
	}
	return m
}

// optionValues 把消息里的 scannerOptions 字段转成 key -> 取值。
// 没带或者不是对象时返回 nil（不是错误：扫描时不带设置很正常）。
func optionValues(v any) (map[string]any, error) {
	if v == nil {
		return nil, nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("scannerOptions 必须是对象，形如 {\"dpi\":300}")
	}
	return m, nil
}

// ---- 缓存 ----
//
// 前端一进页面就要设置项，那时设备多半还没连（getScannerOptions 不能去打开设备，
// 原因见 legacy.go 的 handleLegacyScannerOptions）。所以每次从设备读到就存一份，
// 没连着时拿它按当前配置现拼。内存里一份，exe 旁边 cache\ 目录下一份，服务重启后也有。
//
// 存的是**原始能力**而不是拼好的设置项：改了配置文件，没连设备也能马上生效。
// 配置里新加的项用到了缓存里没有的能力时，那一项要等下次连上设备才出现。

const optionsCacheDirName = "cache"

type capsCache struct {
	Scanner   string            `json:"scanner"`
	UpdatedAt string            `json:"updatedAt"`
	Caps      map[uint16]string `json:"caps"` // 能力编号 -> DLL 原始输出
}

var (
	capsCacheMu sync.Mutex
	capsCacheM  = map[string]capsCache{}
)

func storeCaps(c capsCache) {
	capsCacheMu.Lock()
	capsCacheM[c.Scanner] = c
	capsCacheMu.Unlock()

	body, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return
	}
	path := capsCachePath(c.Scanner)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Printf("设置项缓存目录创建失败: %v", err)
		return
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		log.Printf("设置项缓存写入失败: %v", err)
	}
}

func cachedCaps(device string) (capsCache, bool) {
	if device == "" {
		return capsCache{}, false
	}
	capsCacheMu.Lock()
	c, ok := capsCacheM[device]
	capsCacheMu.Unlock()
	if ok {
		return c, true
	}

	body, err := os.ReadFile(capsCachePath(device))
	if err != nil {
		return capsCache{}, false
	}
	if err := json.Unmarshal(body, &c); err != nil || c.Scanner != device || len(c.Caps) == 0 {
		return capsCache{}, false
	}
	capsCacheMu.Lock()
	capsCacheM[device] = c
	capsCacheMu.Unlock()
	return c, true
}

func hasCachedOptions(device string) bool {
	_, ok := cachedCaps(device)
	return ok
}

func capsCachePath(device string) string {
	dir := optionsCacheDirName
	if exe, err := os.Executable(); err == nil {
		dir = filepath.Join(filepath.Dir(exe), optionsCacheDirName)
	}
	return filepath.Join(dir, "caps_"+safeFileName(device)+".json")
}

// ---- 设置项配置：内置为主，现场覆盖只作应急 ----
//
// 设置项定义的**唯一来源是编译进 exe 的内置配置**（scanopt/default_options.jsonc），
// 跟代码一起提交、过单测、发版。正常情况下现场不需要、也不应该有配置文件。
//
// 应急时（现场某台扫描仪急需调整、来不及发版），在 exe 旁边放一个 scanner-options.jsonc，
// 它会整份替换内置配置。服务不会自动生成这个文件，用了会在控制台和
// /api/scanner-options/config 里明确提示；改好的内容要同步回源码，下个版本发出去后把现场文件删掉。
//
// 覆盖文件每次用到设置项前检查一次（修改时间 + 大小），改了自动重新加载，删了自动恢复内置配置。
// 写错了打日志、继续用上一份正确的配置——不能因为手滑把设置面板整个弄没了。

const optionsOverrideFileName = "scanner-options.jsonc"

const (
	optionsSourceBuiltin  = "builtin"
	optionsSourceOverride = "override"
)

var optionsConfig struct {
	sync.Mutex
	path     string    // 覆盖文件应该在的位置
	present  bool      // 上次检查时覆盖文件是否存在
	modTime  time.Time // 上次加载的覆盖文件的修改时间 / 大小，用来判断改没改
	size     int64
	source   string // 当前生效的是哪份：builtin / override
	loadedAt string // 当前这份的生效时间
	err      string // 覆盖文件最近一次加载失败的原因；成功或删除后清空
}

// initOptionsConfig 在启动时调用：定位覆盖文件，有就加载。
func initOptionsConfig() {
	path := optionsOverrideFileName
	if exe, err := os.Executable(); err == nil {
		path = filepath.Join(filepath.Dir(exe), optionsOverrideFileName)
	}
	optionsConfig.Lock()
	optionsConfig.path = path
	optionsConfig.source = optionsSourceBuiltin
	optionsConfig.loadedAt = time.Now().Format("2006-01-02 15:04:05")
	optionsConfig.Unlock()

	reloadOptionsConfig()

	optionsConfig.Lock()
	defer optionsConfig.Unlock()
	if optionsConfig.source == optionsSourceBuiltin && !optionsConfig.present {
		reg := scanopt.Builtin()
		log.Printf("设置项使用内置配置（%d 项，%d 条设备特例）", len(reg.Defs), len(reg.Profiles))
	}
}

func reloadOptionsConfig() {
	optionsConfig.Lock()
	defer optionsConfig.Unlock()
	if optionsConfig.path == "" {
		return
	}

	st, err := os.Stat(optionsConfig.path)
	if err != nil {
		if optionsConfig.present {
			// 覆盖文件被删了：回到内置配置。
			optionsConfig.present = false
			optionsConfig.modTime, optionsConfig.size = time.Time{}, 0
			optionsConfig.err = ""
			if optionsConfig.source != optionsSourceBuiltin {
				scanopt.Use(scanopt.Builtin())
				optionsConfig.source = optionsSourceBuiltin
				optionsConfig.loadedAt = time.Now().Format("2006-01-02 15:04:05")
				log.Printf("现场覆盖配置 %s 已删除，恢复使用内置配置", optionsConfig.path)
			}
		}
		return
	}
	optionsConfig.present = true
	if st.ModTime().Equal(optionsConfig.modTime) && st.Size() == optionsConfig.size {
		return
	}
	optionsConfig.modTime, optionsConfig.size = st.ModTime(), st.Size()

	data, err := os.ReadFile(optionsConfig.path)
	if err == nil {
		var reg *scanopt.Registry
		if reg, err = scanopt.Compile(data); err == nil {
			scanopt.Use(reg)
			optionsConfig.source = optionsSourceOverride
			optionsConfig.err = ""
			optionsConfig.loadedAt = time.Now().Format("2006-01-02 15:04:05")
			log.Printf("⚠ 设置项正在使用现场覆盖配置 %s（%d 项，%d 条设备特例），内置配置不生效。"+
				"这是应急手段：改好的内容请同步回源码 scanopt/default_options.jsonc，发版后删掉这个文件",
				optionsConfig.path, len(reg.Defs), len(reg.Profiles))
			return
		}
	}
	optionsConfig.err = err.Error()
	current := "内置配置"
	if optionsConfig.source == optionsSourceOverride {
		current = "上一份正确的覆盖配置"
	}
	log.Printf("⚠ 现场覆盖配置 %s 有误，继续使用%s：%v", optionsConfig.path, current, err)
}

// handleOptionsConfigStatus 查看设置项配置的状态：用的是哪份、覆盖文件有没有写错。
func handleOptionsConfigStatus(w http.ResponseWriter, r *http.Request) {
	reloadOptionsConfig()
	optionsConfig.Lock()
	defer optionsConfig.Unlock()

	keys := []string{}
	for _, d := range scanopt.Current().Defs {
		keys = append(keys, d.Key)
	}
	profiles := []string{}
	for _, p := range scanopt.Current().Profiles {
		profiles = append(profiles, p.Name)
	}
	hint := "使用内置配置，正常状态"
	switch {
	case optionsConfig.err != "":
		hint = "现场覆盖文件有误，未生效，见 error"
	case optionsConfig.source == optionsSourceOverride:
		hint = "正在使用现场覆盖配置（应急），改好的内容请同步回源码，发版后删除覆盖文件"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"source":          optionsConfig.source,
		"hint":            hint,
		"overrideFile":    optionsConfig.path,
		"overridePresent": optionsConfig.present,
		"ok":              optionsConfig.err == "",
		"error":           optionsConfig.err,
		"loadedAt":        optionsConfig.loadedAt,
		"options":         keys,
		"profiles":        profiles,
	})
}

// handleOptionsConfigBuiltin 下载内置配置原文，现场要应急覆盖时拿它当起点：
//
//	curl.exe http://127.0.0.1:18080/api/scanner-options/config/builtin -o scanner-options.jsonc
func handleOptionsConfigBuiltin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(strings.ReplaceAll(string(scanopt.DefaultConfig), "\n", "\r\n")))
}

// ---- HTTP ----
//
// 和 WebSocket 的 getScannerOptions / setScannerOptions 一样，方便不开前端直接用 curl 调试。

func handleScannerOptions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, ScannerOptionsFor(r.URL.Query().Get("device")))

	case http.MethodPost:
		var req struct {
			Device         string         `json:"device"`
			ScannerOptions map[string]any `json:"scannerOptions"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeErr(w, http.StatusBadRequest, "请求体不是合法 JSON: "+err.Error())
			return
		}
		if strings.TrimSpace(req.Device) != "" {
			if err := TwainConnect(req.Device); err != nil {
				writeErr(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		results, data, err := TwainApplyScannerOptions(req.ScannerOptions)
		if err != nil {
			writeErr(w, http.StatusConflict, err.Error())
			return
		}
		if results == nil {
			results = []optionResult{}
		}
		failed := failedOptions(results, labelsOf(data.Scanner))
		writeJSON(w, http.StatusOK, map[string]any{
			"success": failed == "",
			"error":   failed,
			"results": results,
			"options": data,
		})

	default:
		writeErr(w, http.StatusMethodNotAllowed, "只支持 GET / POST")
	}
}

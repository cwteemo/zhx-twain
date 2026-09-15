// scansvc —— 扫描仪本地中间服务（最小闭环版）
//
// 职责：把 TWAIN 扫描仪能力通过 HTTP 暴露给浏览器里的业务系统。
//
//	GET  /                 内置演示页面
//	GET  /api/devices      枚举扫描仪（返回的是已装驱动，不代表设备在线）
//	GET  /api/status       当前连接状态，扫描期间也能立刻返回
//	POST /api/connect      连接扫描仪并保持（JSON: {"device":"..."}）
//	POST /api/disconnect   断开当前扫描仪
//	POST /api/reconnect    重连（JSON: {"deep":true} 连 TWAIN 环境一起重建）
//	GET  /api/config       读当前扫描参数
//	POST /api/config       设扫描参数（分辨率/色彩/ADF/双面…）
//	GET  /api/capability   读一项 TWAIN 能力原始信息（?name=ICAP_PIXELTYPE）
//	POST /api/capability   设一项 TWAIN 能力（JSON: {"name":"...","value":"..."}）
//	POST /api/setting-ui   打开驱动自带的设置面板，阻塞到用户关闭
//	POST /api/scan         扫描（JSON: {"device":"...","count":1}，count=0 扫到没纸）
//	GET  /api/image?id=xx  取回扫描出的图片
//	WS   /ws  和  WS  /    WebSocket。/ 上同时提供演示页和 WebSocket，按握手头分流，
//	                       因为既有前端把 ws://127.0.0.1:5000/ 写死了（见 legacy.go）
//
// 另有一组文件/目录管理接口（/version、/file/*、/dir/*），是从既有的 Go 转发服务
// 搬过来的，收表单、回 {"errorCode":...}，见 files.go。
//
// 服务监听两个端口，和被替换掉的那个转发服务保持一致：
//
//	5000    WebSocket 端口   前端写死 ws://127.0.0.1:5000/
//	18080   HTTP 端口        前端写死 http://127.0.0.1:18080/dir/*、/file/*
//
// 两个端口供的是**同一套路由**，谁也不比谁特殊——分开只是因为前端把两个地址都
// 编译进打包好的 js 里了，改不动。两个端口都能自定义：
//
//	scansvc.exe -ws-port 5001            改 WebSocket 端口
//	scansvc.exe -http-port 18081         改 HTTP 端口
//	scansvc.exe -host 127.0.0.1          只允许本机访问（默认监听所有网卡）
//	scansvc.exe -auto-port               端口被占用时自动向后顺延
//	scansvc.exe -http-port 0             不监听 HTTP 端口
//	set SCANSVC_WS_PORT=5001             环境变量方式
//	set SCANSVC_HTTP_PORT=18081
//
// 编译运行见同目录 README.md。
package main

import (
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

//go:embed index.html
var indexHTML []byte

const (
	// serviceVersion 由 GET /version 返回。被替换掉的那个转发服务报的是 v2.1，
	// 前端没有据此分支的逻辑，这里带上自己的名字方便一眼看出连的是哪个服务。
	serviceVersion = "v2.1-scansvc"

	// defaultScanDir 是扫描图片的默认保存目录，相对启动时的工作目录。
	defaultScanDir = "scans"
)

var (
	scanDir = flag.String("dir", defaultScanDir, "扫描图片保存根目录")
	// 原转发服务有个每小时清一次 temp 的定时任务，但在源码里是注释掉的——
	// 它清的是"当前工作目录"，而工作目录会被 /dir/verify 切到用户的档案目录去，
	// 真跑起来就是在删用户的档案。这里保留这个能力但只清扫描根目录，且默认关闭。
	cleanHours = flag.Int("clean-hours", 0, "自动清理扫描目录下超过多少小时的图片；0 表示不清理")
)

// 扫描产物登记表：id -> 绝对路径。
// 只有登记过的 id 才允许通过 /api/image 读取，避免路径穿越。
var (
	imageMu  sync.RWMutex
	images   = map[string]string{}
	imageSeq uint64
)

// scanRoot 是图片保存根目录的绝对路径，main 里算好后 WebSocket 那边也要用。
var scanRoot string

type scanRequest struct {
	// Device 可以不传：不传就用当前已连接的设备（先调 /api/connect）。
	// 传了则先确保连上这台，方便"一把梭"的调用方。
	Device string      `json:"device"`
	Count  int         `json:"count"`
	Config *ScanConfig `json:"config,omitempty"` // 可选：扫之前顺手把参数设了
}

type connectRequest struct {
	Device string `json:"device"`
}

type reconnectRequest struct {
	// Deep = true 时连 TWAIN 环境一起重建，用于设备拔插、驱动崩溃这类
	// 光重开数据源救不回来的情况。
	Deep bool `json:"deep"`
}

type capabilityRequest struct {
	Name  string `json:"name"`
	Value string `json:"value"` // TODO(TODO-settings.md #10): 传数字会 400
}

type imageInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Size int64  `json:"size"`
	URL  string `json:"url"`
}

func main() {
	flag.Parse()
	// 命令行 > 环境变量 > 配置文件 > 默认值，解析细节见 config.go。
	// 配置文件不存在时会在 exe 旁边生成一份带注释的模板。
	cfg = loadSettings()

	root, err := filepath.Abs(cfg.ScanDir)
	if err != nil {
		log.Fatalf("解析保存目录失败: %v", err)
	}
	scanRoot = root
	if err := os.MkdirAll(root, 0o755); err != nil {
		log.Fatalf("创建保存目录失败: %v", err)
	}
	// 工作目录（对应原转发服务的 temp path）先指向扫描目录，
	// 前端调 /dir/verify 选中档案目录后会被换掉。
	setWorkDir(root)

	if cfg.CleanHours > 0 {
		go startCleaner(root, time.Duration(cfg.CleanHours)*time.Hour)
	}

	log.Println("正在初始化 TWAIN 环境...")
	startTwainThread()
	defer TwainExit()

	// zhx_Init 是 void 返回，拿不到成败，只能靠"能不能枚举出设备"反推。
	// 不这么做的话，DSM 加载失败时服务照样打印"就绪"，排查起来很误导。
	if devs := TwainDevices(); len(devs) == 0 {
		log.Println("⚠ TWAIN 初始化后枚举不到任何扫描仪，服务仍会启动，但扫描一定失败。常见原因：")
		log.Println("   1) 找不到 TWAINDSM.dll —— 它必须在 exe 同目录、系统目录或 PATH 里")
		log.Println("      （注意：装在 C:\\Windows\\twain_64\\ 下不在默认搜索路径，需拷到 exe 旁边）")
		log.Println("   2) 扫描仪驱动未安装，或设备未连接/未开机")
		log.Println("   3) 设备只有 32 位 TWAIN 数据源，本服务是 64 位，加载不了")
	} else {
		log.Printf("TWAIN 环境就绪，发现 %d 台设备: %v", len(devs), devs)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleIndex)
	mux.HandleFunc("/api/devices", handleDevices)
	mux.HandleFunc("/api/status", handleStatus)
	mux.HandleFunc("/api/connect", handleConnect)
	mux.HandleFunc("/api/disconnect", handleDisconnect)
	mux.HandleFunc("/api/reconnect", handleReconnect)
	mux.HandleFunc("/api/config", handleConfig)
	mux.HandleFunc("/api/capability", handleCapability)
	mux.HandleFunc("/api/setting-ui", handleSettingUI)
	mux.HandleFunc("/api/scan", handleScan(root))
	mux.HandleFunc("/api/image", handleImage)
	mux.HandleFunc("/ws", handleWS)
	// 文件 / 目录管理那一组（含 /file/，图片的静态出口也在里面）。
	// 既有前端是从 URL 结尾取扩展名的（xxx.png），/api/image?id=xxx 那种
	// 带查询串的形式它认不了，所以图片始终有 /file/ 这条以扩展名结尾的出口。
	registerFileRoutes(mux)

	// 两个端口（WebSocket 5000 / HTTP 18080）供同一套路由，见 serve.go。
	serveErr, listening, closeAll := startServers(withCORS(mux))
	defer closeAll()
	if listening == 0 {
		log.Printf("没有任何端口监听成功，服务退出")
		return
	}

	log.Printf("扫描服务就绪，图片保存于 %s", root)

	// 任一监听挂掉就整体退出：前端两个端口都要用，剩一个也是半残状态，
	// 与其让它带病运行不如退干净，让人一眼看出要重启。
	if err := <-serveErr; err != nil {
		log.Printf("服务异常退出: %v", err)
	}
}

// handleIndex 是根路径：WebSocket 握手走 WebSocket，普通请求给内置演示页。
//
// 既有前端连的是 ws://127.0.0.1:5000/ ——根路径，没有子路径，所以只能按握手头分流。
// 日常用的前端（court-document-processing）部署在 80 端口，页面不从这里取，
// 这个演示页是撇开前端单独试扫描时用的。
func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if websocket.IsWebSocketUpgrade(r) {
		handleWS(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(indexHTML)
}

func handleDevices(w http.ResponseWriter, r *http.Request) {
	devices := TwainDevices()
	writeJSON(w, http.StatusOK, map[string]any{
		"devices": devices,
		"count":   len(devices),
	})
}

// decodeJSON 读请求体。允许空体：连接/重连这类接口用默认值也说得通。
func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil && err != io.EOF {
		return err
	}
	return nil
}

func requirePOST(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "只支持 POST")
		return false
	}
	return true
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, TwainStatus())
}

func handleConnect(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}

	var req connectRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体不是合法 JSON: "+err.Error())
		return
	}
	if strings.TrimSpace(req.Device) == "" {
		writeErr(w, http.StatusBadRequest, "device 不能为空")
		return
	}
	if err := TwainConnect(req.Device); err != nil {
		// 打不开设备是设备侧的事，不是请求写错了，所以给 500 而不是 4xx。
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"status":  TwainStatus(),
	})
}

func handleDisconnect(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}
	TwainDisconnect()
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"status":  TwainStatus(),
	})
}

func handleReconnect(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}

	var req reconnectRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体不是合法 JSON: "+err.Error())
		return
	}

	device, err := TwainReconnect(req.Deep)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"device":  device,
		"deep":    req.Deep,
		"status":  TwainStatus(),
	})
}

// handleSettingUI 打开驱动自带的设置面板。
// 请求会一直挂着直到用户关掉面板——这不是超时，是设计如此，面板本身就是模态的。
// 调用方要么把超时放宽，要么改用 WebSocket 的 showSetting 指令。
func handleSettingUI(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}
	if err := TwainShowSettingUI(); err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg, err := TwainCurrentConfig()
		if err != nil {
			writeErr(w, http.StatusConflict, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, cfg)

	case http.MethodPost:
		var cfg ScanConfig
		if err := decodeJSON(r, &cfg); err != nil {
			writeErr(w, http.StatusBadRequest, "请求体不是合法 JSON: "+err.Error())
			return
		}

		results, err := TwainApplyConfig(cfg)
		if err != nil {
			writeErr(w, http.StatusConflict, err.Error())
			return
		}

		// 逐项返回，整体 success 只表示"每一项都设上了"。
		// 一项失败不影响其它项，调用方按 results 自己判断要不要继续。
		allOK := true
		for _, res := range results {
			if !res.OK {
				allOK = false
				break
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"success": allOK,
			"results": results,
		})

	default:
		writeErr(w, http.StatusMethodNotAllowed, "只支持 GET / POST")
	}
}

func handleCapability(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		name := strings.TrimSpace(r.URL.Query().Get("name"))
		if name == "" {
			writeErr(w, http.StatusBadRequest, "缺少参数 name，例如 /api/capability?name=ICAP_PIXELTYPE")
			return
		}
		raw, err := TwainCapability(name)
		if err != nil {
			writeErr(w, http.StatusConflict, err.Error())
			return
		}
		// raw 是 DLL 直接吐出来的 JSON 片段，原样塞进 capability 字段，
		// 不在 Go 这边二次解析——容器类型有 ONEVALUE/ENUMERATION/RANGE 三种，
		// 解析了反而丢信息。
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		fmt.Fprintf(w, `{"name":%q,"capability":%s}`, name, raw)

	case http.MethodPost:
		var req capabilityRequest
		if err := decodeJSON(r, &req); err != nil {
			writeErr(w, http.StatusBadRequest, "请求体不是合法 JSON: "+err.Error())
			return
		}
		if strings.TrimSpace(req.Name) == "" {
			writeErr(w, http.StatusBadRequest, "name 不能为空")
			return
		}
		// TODO(TODO-settings.md #6): 未连接时这里给 500，其它设置接口给 409。
		if err := TwainSetCapability(req.Name, req.Value); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true})

	default:
		writeErr(w, http.StatusMethodNotAllowed, "只支持 GET / POST")
	}
}

func handleScan(root string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "只支持 POST")
			return
		}

		var req scanRequest
		if err := decodeJSON(r, &req); err != nil {
			writeErr(w, http.StatusBadRequest, "请求体不是合法 JSON: "+err.Error())
			return
		}
		if req.Count < 0 {
			req.Count = 1
		}

		// 先连上再设参数：TWAIN 的能力协商只在数据源打开(state 4)之后有效。
		if req.Device != "" {
			if err := TwainConnect(req.Device); err != nil {
				writeErr(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		if req.Config != nil {
			// TODO(TODO-settings.md #9): 单项失败被忽略，照常开扫。
			if _, err := TwainApplyConfig(*req.Config); err != nil {
				writeErr(w, http.StatusConflict, err.Error())
				return
			}
		}

		dir, err := newScanDir(root)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}

		// HTTP 是同步的，没有边扫边报的余地，进度传 nil。要进度用 WebSocket。
		files, err := TwainScan(req.Device, dir, req.Count, nil)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}

		infos := make([]imageInfo, 0, len(files))
		for _, f := range files {
			infos = append(infos, registerImage(f))
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"count":   len(infos),
			"images":  infos,
		})
	}
}

// newScanDir 为一次扫描开一个独立目录。
// DLL 靠"扫描前后目录里新增的文件"来识别产物，目录隔离能避免多次扫描互相干扰。
func newScanDir(root string) (string, error) {
	// 目录名不带点：既有前端是用 lastIndexOf('.') 从图片 URL 取扩展名的，
	// 路径里多一个点就多一分取错的风险。
	dir := filepath.Join(root, time.Now().Format("20060102-150405-000"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("创建扫描目录失败: %w", err)
	}
	return dir, nil
}

// registerImage 把一个扫描产物登记进内存表，返回给调用方用的元信息。
// HTTP 和 WebSocket 两条路都走它，图片本身始终通过 GET /api/image 取。
func registerImage(path string) imageInfo {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}

	imageMu.Lock()
	imageSeq++
	id := fmt.Sprintf("%d-%d", time.Now().UnixNano(), imageSeq)
	images[id] = abs
	imageMu.Unlock()

	var size int64
	if st, statErr := os.Stat(abs); statErr == nil {
		size = st.Size()
	}
	return imageInfo{
		ID:   id,
		Name: filepath.Base(abs),
		Size: size,
		URL:  "/api/image?id=" + id,
	}
}

func handleImage(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")

	imageMu.RLock()
	path, ok := images[id]
	imageMu.RUnlock()
	if !ok {
		writeErr(w, http.StatusNotFound, "图片不存在或已过期")
		return
	}

	f, err := os.Open(path)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取图片失败: "+err.Error())
		return
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取图片信息失败: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", contentTypeOf(path))
	http.ServeContent(w, r, filepath.Base(path), st.ModTime(), f)
}

func contentTypeOf(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".bmp":
		return "image/bmp"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".tif", ".tiff":
		return "image/tiff"
	default:
		return "application/octet-stream"
	}
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(body)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]any{"success": false, "error": msg})
}

// scansvc —— 扫描仪本地中间服务（最小闭环版）
//
// 职责：把 TWAIN 扫描仪能力通过 HTTP 暴露给浏览器里的业务系统。
//
//	GET  /                 内置演示页面
//	GET  /api/devices      枚举扫描仪
//	POST /api/scan         扫描（JSON: {"device":"...","count":1}）
//	GET  /api/image?id=xx  取回扫描出的图片
//
// 监听端口可自定义（优先级：命令行 > 环境变量 > 默认 :8000）：
//
//	scansvc.exe -port 8010            指定端口
//	scansvc.exe -addr 127.0.0.1:8010  指定地址+端口（只允许本机访问）
//	scansvc.exe -auto-port            端口被占用时自动向后顺延
//	set SCANSVC_PORT=8010             环境变量方式
//
// 编译运行见同目录 README.md。
package main

import (
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed index.html
var indexHTML []byte

// 默认监听地址；端口被 -auto-port 顺延时最多向后试这么多个。
const (
	defaultAddr   = ":8000"
	autoPortTries = 20
)

var (
	addr     = flag.String("addr", "", "监听地址，如 :8000 或 127.0.0.1:8000（留空则按 -port / 环境变量 / 默认值决定）")
	port     = flag.Int("port", 0, "监听端口，等价于 -addr :<port>；0 表示不指定")
	autoPort = flag.Bool("auto-port", false, "端口被占用时自动向后顺延寻找可用端口")
	scanDir  = flag.String("dir", "scans", "扫描图片保存根目录")
)

// 扫描产物登记表：id -> 绝对路径。
// 只有登记过的 id 才允许通过 /api/image 读取，避免路径穿越。
var (
	imageMu sync.RWMutex
	images  = map[string]string{}
)

type scanRequest struct {
	Device string `json:"device"`
	Count  int    `json:"count"`
}

type imageInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Size int64  `json:"size"`
	URL  string `json:"url"`
}

func main() {
	flag.Parse()

	root, err := filepath.Abs(*scanDir)
	if err != nil {
		log.Fatalf("解析保存目录失败: %v", err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		log.Fatalf("创建保存目录失败: %v", err)
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
	mux.HandleFunc("/api/scan", handleScan(root))
	mux.HandleFunc("/api/image", handleImage)

	listenAddr := resolveAddr()
	ln, err := listenWithFallback(listenAddr, *autoPort)
	if err != nil {
		// 这里不用 log.Fatalf：它会跳过 defer TwainExit()，让 DSM/数据源没关干净。
		log.Printf("监听 %s 失败: %v", listenAddr, err)
		log.Printf("端口多半已被别的程序占用，可以：")
		log.Printf("  1) 换个端口启动：scansvc.exe -port 8010")
		log.Printf("  2) 让它自动顺延：scansvc.exe -auto-port")
		log.Printf("  3) 查是谁占着：netstat -ano | findstr :%s", portOf(listenAddr))
		return
	}
	defer ln.Close()

	log.Printf("扫描服务已启动: http://localhost:%s  （图片保存于 %s）", portOf(ln.Addr().String()), root)
	if err := http.Serve(ln, withCORS(mux)); err != nil {
		log.Printf("服务异常退出: %v", err)
	}
}

// resolveAddr 按 命令行 flag > 环境变量 > 默认值 的优先级决定监听地址。
func resolveAddr() string {
	if v := strings.TrimSpace(*addr); v != "" {
		return normalizeAddr(v)
	}
	if *port > 0 {
		return ":" + strconv.Itoa(*port)
	}
	if v := strings.TrimSpace(os.Getenv("SCANSVC_ADDR")); v != "" {
		return normalizeAddr(v)
	}
	if v := strings.TrimSpace(os.Getenv("SCANSVC_PORT")); v != "" {
		return normalizeAddr(v)
	}
	return defaultAddr
}

// normalizeAddr 允许只写端口号这种简写（"8010" → ":8010"）。
func normalizeAddr(s string) string {
	if !strings.Contains(s, ":") {
		return ":" + s
	}
	return s
}

// portOf 从监听地址里取出端口号，取不到就原样返回，只用于日志和提示。
func portOf(addr string) string {
	if _, p, err := net.SplitHostPort(addr); err == nil {
		return p
	}
	return addr
}

// listenWithFallback 在指定地址上监听；开了 auto 就在端口被占用时向后顺延，
// 直到找到一个能用的（最多试 autoPortTries 个）。
func listenWithFallback(addr string, auto bool) (net.Listener, error) {
	ln, err := net.Listen("tcp", addr)
	if err == nil || !auto {
		return ln, err
	}

	host, portStr, splitErr := net.SplitHostPort(addr)
	if splitErr != nil {
		return nil, err
	}
	base, convErr := strconv.Atoi(portStr)
	if convErr != nil {
		return nil, err
	}

	firstErr := err
	for i := 1; i <= autoPortTries; i++ {
		next := base + i
		if next > 65535 {
			break
		}
		cand := net.JoinHostPort(host, strconv.Itoa(next))
		if ln, err := net.Listen("tcp", cand); err == nil {
			log.Printf("端口 %d 不可用（%v），已自动顺延到 %d", base, firstErr, next)
			return ln, nil
		}
	}
	return nil, firstErr
}

// withCORS 允许业务系统从其它域名调用本机服务。
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
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

func handleScan(root string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "只支持 POST")
			return
		}

		var req scanRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "请求体不是合法 JSON: "+err.Error())
			return
		}
		if req.Count < 0 {
			req.Count = 1
		}

		// 每次扫描单独一个目录，DLL 靠"扫描前后目录里新增的文件"来识别产物，
		// 目录隔离能避免多次扫描互相干扰。
		dir := filepath.Join(root, time.Now().Format("20060102-150405.000"))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			writeErr(w, http.StatusInternalServerError, "创建扫描目录失败: "+err.Error())
			return
		}

		files, err := TwainScan(req.Device, dir, req.Count)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}

		infos := make([]imageInfo, 0, len(files))
		for _, f := range files {
			abs, absErr := filepath.Abs(f)
			if absErr != nil {
				abs = f
			}
			id := fmt.Sprintf("%d-%d", time.Now().UnixNano(), len(infos))

			imageMu.Lock()
			images[id] = abs
			imageMu.Unlock()

			var size int64
			if st, statErr := os.Stat(abs); statErr == nil {
				size = st.Size()
			}
			infos = append(infos, imageInfo{
				ID:   id,
				Name: filepath.Base(abs),
				Size: size,
				URL:  "/api/image?id=" + id,
			})
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"count":   len(infos),
			"images":  infos,
		})
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

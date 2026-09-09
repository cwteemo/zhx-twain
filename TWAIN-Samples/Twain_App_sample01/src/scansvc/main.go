// scansvc —— 扫描仪本地中间服务（最小闭环版）
//
// 职责：把 TWAIN 扫描仪能力通过 HTTP 暴露给浏览器里的业务系统。
//
//	GET  /                 内置演示页面
//	GET  /api/devices      枚举扫描仪
//	POST /api/scan         扫描（JSON: {"device":"...","count":1}）
//	GET  /api/image?id=xx  取回扫描出的图片
//
// 编译运行见同目录 README.md。
package main

import (
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

//go:embed index.html
var indexHTML []byte

var (
	addr    = flag.String("addr", ":8000", "监听地址")
	scanDir = flag.String("dir", "scans", "扫描图片保存根目录")
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
	log.Println("TWAIN 环境就绪")

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleIndex)
	mux.HandleFunc("/api/devices", handleDevices)
	mux.HandleFunc("/api/scan", handleScan(root))
	mux.HandleFunc("/api/image", handleImage)

	log.Printf("扫描服务已启动: http://localhost%s  （图片保存于 %s）", *addr, root)
	if err := http.ListenAndServe(*addr, withCORS(mux)); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
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

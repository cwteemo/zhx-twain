package main

// 文件 / 目录管理接口。
//
// 这一组接口是从既有的 Go 转发服务（filemanager 仓库 "扫描工具" 分支）搬过来的，
// 路径、表单字段、响应结构都按原样对齐，目的是让 scansvc 一个进程就能顶掉那个
// 服务，前端不用改：
//
//	GET  /version                 版本 + 当前工作目录
//	GET  /file/<相对路径>          读工作目录下的文件，?thumbnail=1 优先取同名 .jpeg
//	POST /file/restore            从 <工作目录>_备份 把原图还原回来
//	POST /file/upload             把工作目录下的文件转发上传到 server 指定的业务系统
//	POST /file/temp/path/update   切换工作目录
//	POST /dir/verify              备份整个目录并递归列出文件（前端进入加工流程的第一步）
//	POST /dir/children            列出一层子目录和文件
//	POST /dir/open                用资源管理器打开目录
//	POST /dir/upload              接收上传的文件，落到工作目录下的指定相对路径
//
// 两点和 scansvc 原有接口的区别，别混：
//   - 这组接口收的是表单（multipart / urlencoded），不是 JSON；/api/* 那组收 JSON。
//   - 这组的响应是 {"errorCode":0,"msg":"","data":...}，错误码沿用原服务的编号；
//     /api/* 那组是 {"success":true,...}。两套都保留，各自的调用方都不用动。
//
// 原服务里那套 fsnotify 监听目录、等扫描驱动把图片写进来再推给前端的机制没有搬：
// scansvc 是直接通过 TWAIN 拿到图的（见 legacy.go / protocol.go），不需要靠猜。

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// 原服务的错误码，前端按数字判，不能改。
const (
	errCodeOK           = 0
	errCodeFail         = 1 // 通用失败（还原、转发上传）
	errCodeBackupFail   = 2 // 备份目录失败
	errCodeUploadFail   = 3 // 保存上传文件失败
	errCodePathInvalid  = 4 // 路径不存在
	errCodeNotDirectory = 5 // 选中的是文件而不是目录
)

// 备份目录的后缀，和原服务一致（前端不知道这个目录的存在，纯服务端约定）。
const backupSuffix = "_备份"

// workDir 是"当前工作目录"，对应原服务里那个全局可变的 temp path。
// 前端调 /dir/verify 选中档案目录、或直接调 /file/temp/path/update 时会被换掉；
// 没被换过时就是扫描图片的保存目录，这样扫出来的图不用额外设置就能通过 /file/ 读到。
var (
	workMu  sync.RWMutex
	workDir string
)

func getWorkDir() string {
	workMu.RLock()
	defer workMu.RUnlock()
	return workDir
}

func setWorkDir(p string) {
	workMu.Lock()
	workDir = trimTrailingSep(p)
	workMu.Unlock()
	log.Printf("工作目录已切换到: %s", trimTrailingSep(p))
}

// apiRes 是这组接口统一的响应体，字段名对齐原服务的 structs.Res。
type apiRes struct {
	ErrorCode int    `json:"errorCode"`
	Msg       string `json:"msg"`
	Data      any    `json:"data"`
}

type dirUploadData struct {
	Path string `json:"path"`
}

type dirChildListData struct {
	Folders []string `json:"folders"`
	Files   []string `json:"files"`
	Dir     string   `json:"dir"`
}

func writeAPI(w http.ResponseWriter, res apiRes) {
	// 这组接口一律 HTTP 200，成败看 errorCode——原服务就是这么干的，
	// 前端只读 body，给非 200 反而会被它的请求库当成网络错误吞掉。
	writeJSON(w, http.StatusOK, res)
}

func writeAPIErr(w http.ResponseWriter, code int, format string, args ...any) {
	writeAPI(w, apiRes{ErrorCode: code, Msg: fmt.Sprintf(format, args...)})
}

// registerFileRoutes 把这组接口挂到 mux 上。
// /file/restore 这类具体路径是精确匹配，优先级高于 /file/ 前缀，不会互相盖掉。
func registerFileRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/version", handleVersion)
	mux.HandleFunc("/file/", handleFileGet)
	mux.HandleFunc("/file/restore", handleFileRestore)
	mux.HandleFunc("/file/upload", handleFileUpload)
	mux.HandleFunc("/file/temp/path/update", handleTempPathUpdate)
	mux.HandleFunc("/dir/verify", handleDirVerify)
	mux.HandleFunc("/dir/children", handleDirChildren)
	mux.HandleFunc("/dir/open", handleDirOpen)
	mux.HandleFunc("/dir/upload", handleDirUpload)
}

// formDir 取表单里的 dir 字段。
// 前端有"把文件夹拖进来"的入口，拿到的路径会带 file:// 前缀，原服务在这里剥掉，照做。
func formDir(r *http.Request) string {
	dir := r.FormValue("dir")
	return strings.TrimPrefix(dir, "file://")
}

// checkDir 是原服务 middlewares/DirVerify 那层校验。
// 原版把中间件注释掉了（挂在全局上会和 multipart 的解析时机打架），
// 这里改成在用到 dir 的接口里就地调用，错误码沿用原来的 4 / 5。
func checkDir(w http.ResponseWriter, dir string) bool {
	if dir == "" {
		writeAPIErr(w, errCodePathInvalid, "缺少参数 dir")
		return false
	}
	if !fileExist(dir) {
		writeAPIErr(w, errCodePathInvalid, "文件路径无效【%s】", dir)
		return false
	}
	if !isDir(dir) {
		writeAPIErr(w, errCodeNotDirectory, "请选择文件夹，暂时不支持文件")
		return false
	}
	return true
}

func handleVersion(w http.ResponseWriter, r *http.Request) {
	cwd, _ := os.Getwd()
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "scansvc %s 工作目录: %s 进程目录: %s", serviceVersion, getWorkDir(), cwd)
}

// handleFileGet 读工作目录下的文件。
//
// 找不到时还会去扫描根目录下再找一遍：工作目录被 /dir/verify 换到别的档案目录之后，
// 之前扫出来的图（URL 是 /file/<时间戳目录>/<页号>.png）仍然要能打开。
func handleFileGet(w http.ResponseWriter, r *http.Request) {
	rel := strings.TrimPrefix(r.URL.Path, "/file/")
	if rel == "" {
		http.NotFound(w, r)
		return
	}

	thumbnail := r.URL.Query().Get("thumbnail") == "1"

	for _, root := range candidateRoots() {
		full, ok := safeJoin(root, rel)
		if !ok {
			continue
		}
		// 缩略图：同名的 .jpeg 存在就给它，没有就退回原图——和原服务一致，
		// 缩略图是加工流程另外生成的，这里只负责挑。
		if thumbnail {
			thumb := strings.TrimSuffix(full, filepath.Ext(full)) + ".jpeg"
			if fileExist(thumb) && !isDir(thumb) {
				serveFile(w, r, thumb)
				return
			}
		}
		if fileExist(full) && !isDir(full) {
			serveFile(w, r, full)
			return
		}
	}
	http.NotFound(w, r)
}

// candidateRoots 是 /file/ 的查找顺序：先当前工作目录，再扫描根目录。
func candidateRoots() []string {
	roots := []string{}
	if wd := getWorkDir(); wd != "" {
		roots = append(roots, wd)
	}
	if scanRoot != "" && scanRoot != getWorkDir() {
		roots = append(roots, scanRoot)
	}
	return roots
}

func serveFile(w http.ResponseWriter, r *http.Request, path string) {
	f, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// 两小时缓存，照搬原服务：加工界面会反复拉同一批图，不缓存翻页会很卡。
	w.Header().Set("Cache-Control", "max-age=7200")
	w.Header().Set("Content-Type", contentTypeOf(path))
	http.ServeContent(w, r, filepath.Base(path), st.ModTime(), f)
}

// handleFileRestore 把 <工作目录>_备份 里的原图复制回工作目录，覆盖加工过的那份。
func handleFileRestore(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}

	name := r.FormValue("name")
	root := getWorkDir()

	src, srcOK := safeJoin(root+backupSuffix, name)
	dst, dstOK := safeJoin(root, name)
	if name == "" || !srcOK || !dstOK {
		writeAPIErr(w, errCodeFail, "文件名无效【%s】", name)
		return
	}

	res := apiRes{}
	if !fileExist(src) {
		// 注意：原服务在这里只填 msg、不改 errorCode，前端会当成还原成功。
		// 这是原来的行为，前端的判断逻辑就建在它上面，照搬，没有改。
		res.Msg = "找不到原始文件【" + src + "】"
	} else if err := copyPath(src, dst); err != nil {
		res.ErrorCode = errCodeFail
		res.Msg = err.Error()
	}
	writeAPI(w, res)
}

// handleFileUpload 是这个服务"转发"二字的由来：
// 浏览器受同源和本机文件访问限制，传不了本地磁盘上的图，于是让本服务代劳——
// 前端只给文件名和目标地址，文件内容由本服务从工作目录读出来，以 multipart
// 发到业务系统，再把业务系统的响应原样转回去。
func handleFileUpload(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}

	token := r.Header.Get("token")
	if err := r.ParseMultipartForm(32 << 20); err != nil && err != http.ErrNotMultipart {
		writeAPIErr(w, errCodeFail, "解析表单失败: %s", err.Error())
		return
	}

	name := r.FormValue("filename")
	server := r.FormValue("server")
	if name == "" || server == "" {
		writeAPIErr(w, errCodeFail, "filename 和 server 都不能为空")
		return
	}

	full, ok := safeJoin(getWorkDir(), name)
	if !ok {
		writeAPIErr(w, errCodeFail, "文件名无效【%s】", name)
		return
	}

	body, contentType, err := buildUploadBody(full, name, r.Form)
	if err != nil {
		writeAPIErr(w, errCodeFail, "读取待上传文件失败: %s", err.Error())
		return
	}

	req, err := http.NewRequest(http.MethodPost, server, body)
	if err != nil {
		writeAPIErr(w, errCodeFail, "上传地址无效【%s】: %s", server, err.Error())
		return
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("token", token)

	// 原服务对网络错误一律忽略，resp 为 nil 时后面直接 panic 掉整个进程。
	// 这里把错误如实转成 errorCode，本机服务崩了比传不上去更难查。
	resp, err := uploadClient.Do(req)
	if err != nil {
		writeAPIErr(w, errCodeFail, "转发上传失败: %s", err.Error())
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		writeAPIErr(w, errCodeFail, "读取上传响应失败: %s", err.Error())
		return
	}

	// 业务系统返回的就是这套 {errorCode,msg,data} 结构，原样转回前端。
	// 解不出 JSON 说明对面返回的是错误页之类，把原文带上，别让前端对着空响应猜。
	//
	// 这里是**字节透传**，不是原服务那样 Unmarshal 成 map 再重新序列化：
	// 那样走一圈，档案 id 这种 19 位整数会被当成 float64，回到前端时变成
	// 1.2345678901234568e+18，对不上任何一条记录。
	if !json.Valid(respBody) {
		writeAPIErr(w, errCodeFail, "上传接口返回的不是 JSON（HTTP %d）: %s",
			resp.StatusCode, strings.TrimSpace(string(respBody)))
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Write(respBody)
}

// uploadClient 单独一个：转发上传可能要传几十兆的图，超时给得比默认宽。
var uploadClient = &http.Client{Timeout: 10 * time.Minute}

// buildUploadBody 拼出转发用的 multipart 请求体。
// 文件字段名是 image，除 filename / server 之外的表单字段原样带过去——
// 业务系统靠它们（档案 id、页号之类）判断这张图归到哪儿。
func buildUploadBody(path, name string, form map[string][]string) (io.Reader, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer file.Close()

	buf := &bytes.Buffer{}
	writer := multipart.NewWriter(buf)

	fw, err := writer.CreateFormFile("image", name)
	if err != nil {
		return nil, "", err
	}
	if _, err := io.Copy(fw, file); err != nil {
		return nil, "", err
	}

	for k, v := range form {
		if k == "filename" || k == "server" || len(v) == 0 {
			continue
		}
		if err := writer.WriteField(k, v[0]); err != nil {
			return nil, "", err
		}
	}

	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return buf, writer.FormDataContentType(), nil
}

// handleDirVerify 是前端进入加工流程的第一步：选定目录 → 服务端整目录备份一份 →
// 递归列出所有文件给前端展示。备份是后面 /file/restore 能还原的前提，
// 只在备份目录还不存在时做，已经有了就不碰——否则第二次进来会把加工结果覆盖掉原图。
func handleDirVerify(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}

	dir := formDir(r)
	if !checkDir(w, dir) {
		return
	}
	ignorePattern := r.FormValue("filenameIgnorePattern")
	onlyPattern := r.FormValue("filenameOnlyPattern")

	root := trimTrailingSep(dir)
	backupDir := root + backupSuffix
	if !fileExist(backupDir) {
		if err := copyPath(root, backupDir); err != nil {
			writeAPIErr(w, errCodeBackupFail, "%s", err.Error())
			return
		}
		log.Printf("已备份目录: %s -> %s", root, backupDir)
	}

	files, err := getAllFiles(root, "", ignorePattern, onlyPattern)
	if err != nil {
		writeAPIErr(w, errCodePathInvalid, "读取目录失败: %s", err.Error())
		return
	}

	setWorkDir(root)
	writeAPI(w, apiRes{Data: files})
}

func handleDirChildren(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}

	dir := formDir(r)
	if !checkDir(w, dir) {
		return
	}

	root := trimTrailingSep(dir)
	folders, files, err := getChildFolder(root, "", r.FormValue("filenameIgnorePattern"))
	if err != nil {
		writeAPIErr(w, errCodePathInvalid, "读取目录失败: %s", err.Error())
		return
	}

	// 这里不动工作目录：前端是拿它做目录树浏览的，点开一个子目录看看不代表要切过去。
	writeAPI(w, apiRes{Data: dirChildListData{Folders: folders, Files: files, Dir: root}})
}

func handleDirOpen(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}

	dir := formDir(r)
	if !checkDir(w, dir) {
		return
	}
	if err := openWithSystem(dir); err != nil {
		writeAPIErr(w, errCodeFail, "打开目录失败: %s", err.Error())
		return
	}
	writeAPI(w, apiRes{Data: dirUploadData{}})
}

// handleDirUpload 收前端传上来的文件，落到 <dir>/<filepath>。
// 没带文件也算成功：前端对没动过的图不会重复上传，只把目标路径回一遍就行。
func handleDirUpload(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}

	if err := r.ParseMultipartForm(32 << 20); err != nil && err != http.ErrNotMultipart {
		writeAPIErr(w, errCodeUploadFail, "解析表单失败: %s", err.Error())
		return
	}

	dir := formDir(r)
	if !checkDir(w, dir) {
		return
	}
	root := trimTrailingSep(dir)

	rel := r.FormValue("filepath")
	full, ok := safeJoin(root, rel)
	if rel == "" || !ok {
		writeAPIErr(w, errCodeUploadFail, "filepath 无效【%s】", rel)
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		writeAPI(w, apiRes{Data: dirUploadData{Path: full}})
		return
	}
	defer file.Close()

	if err := mkDir(filepath.Dir(full)); err != nil {
		writeAPIErr(w, errCodeUploadFail, "创建目录失败: %s", err.Error())
		return
	}

	dst, err := os.Create(full)
	if err != nil {
		writeAPIErr(w, errCodeUploadFail, "%s", err.Error())
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		writeAPIErr(w, errCodeUploadFail, "%s", err.Error())
		return
	}
	writeAPI(w, apiRes{Data: dirUploadData{Path: full}})
}

// handleTempPathUpdate 直接切工作目录，不做备份也不列文件——
// 前端在已经确认过目录的前提下换扫描落盘位置时走这条。
func handleTempPathUpdate(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}

	p := strings.TrimPrefix(r.FormValue("path"), "file://")
	if p == "" {
		writeAPIErr(w, errCodePathInvalid, "缺少参数 path")
		return
	}
	// 原服务不校验就直接设，设成一个不存在的目录后，之后每次读文件都会莫名其妙地 404。
	// 这里先确认目录在。
	if !fileExist(p) {
		writeAPIErr(w, errCodePathInvalid, "文件路径无效【%s】", p)
		return
	}
	if !isDir(p) {
		writeAPIErr(w, errCodeNotDirectory, "请选择文件夹，暂时不支持文件")
		return
	}

	setWorkDir(p)
	writeAPI(w, apiRes{Data: dirUploadData{Path: trimTrailingSep(p)}})
}

// startCleaner 定期清掉扫描目录下的旧图。
//
// 对应原转发服务 modules/crontab.go 里那个每小时跑一次的任务（它在源码里是
// 注释掉的，原因见 main.go 里 -clean-hours 的说明）。这里只清扫描根目录，
// 不碰工作目录——工作目录可能是用户的档案目录，误删了找不回来。
func startCleaner(root string, maxAge time.Duration) {
	log.Printf("已启用自动清理：每小时清一次 %s 下超过 %s 的图片", root, maxAge)
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	cleanOldFiles(root, maxAge)
	for range ticker.C {
		cleanOldFiles(root, maxAge)
	}
}

func cleanOldFiles(root string, maxAge time.Duration) {
	deadline := time.Now().Add(-maxAge)

	// 先删过期文件，再回头删空掉的时间戳目录——一次扫描一个目录，
	// 图清完了空壳留着只会越堆越多。
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || path == root {
			return nil
		}
		if info.ModTime().Before(deadline) {
			if rmErr := os.Remove(path); rmErr != nil {
				log.Printf("清理 %s 失败: %v", path, rmErr)
			}
		}
		return nil
	})

	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sub := filepath.Join(root, e.Name())
		// Remove 只在目录已经空了的时候成功，非空会报错，正好当判断用。
		os.Remove(sub)
	}
}

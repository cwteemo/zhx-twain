package main

// HTTP 扫描任务（异步 + 轮询），给不方便用 WebSocket 的客户端。
//
//	POST /api/scan/start    立刻返回 jobId，扫描在后台跑
//	GET  /api/scan/status   轮询这个 job 的状态和已扫出的页
//
// 产出和 WebSocket 那条路**完全一样**：按 extension 转成 jpeg/png、平铺到扫描根目录、
// 给出 /file/<文件名> 地址，所以拿到的图能直接显示，也能直接走 /file/upload 转发上传。
//
// 为什么是异步：驱动是整叠纸传完才回调的，一次送纸器作业挂几分钟很正常，
// 同步请求会撞上浏览器或中间代理的超时。异步之后客户端随时可以查进度，也不怕超时。
//
// 只保留最近几次任务的结果，够客户端取完就行，不做持久化。

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// 任务状态。
const (
	scanStateScanning = "scanning"
	scanStateDone     = "done"
	scanStateFailed   = "failed"
)

// keptScanJobs 是内存里保留的历史任务数，超出就丢最早的。
const keptScanJobs = 20

// scanPage 是扫出来的一页。
type scanPage struct {
	Page int    `json:"page"`
	File string `json:"file"` // 文件名，平铺在扫描根目录下
	URL  string `json:"url"`  // 可直接 <img src> 的地址
}

// scanJob 是一次扫描任务。
type scanJob struct {
	ID        string     `json:"id"`
	Device    string     `json:"device"`
	State     string     `json:"state"`           // scanning / done / failed
	Error     string     `json:"error,omitempty"` // 失败原因，中文，可直接显示
	StartedAt string     `json:"startedAt"`
	EndedAt   string     `json:"endedAt,omitempty"`
	Pages     []scanPage `json:"pages"`

	host string // 发起请求时的 Host，用来拼图片地址
}

var (
	scanJobMu   sync.Mutex
	scanJobs    = map[string]*scanJob{}
	scanJobList []string // 按创建顺序，用来淘汰最早的
	scanJobSeq  int
)

// snapshot 拷一份出去给 HTTP 响应用，避免边写边序列化。调用方须持有 scanJobMu。
func (j *scanJob) snapshot() scanJob {
	cp := *j
	cp.Pages = append([]scanPage(nil), j.Pages...)
	return cp
}

func newScanJob(device, host string) *scanJob {
	scanJobMu.Lock()
	defer scanJobMu.Unlock()

	scanJobSeq++
	job := &scanJob{
		ID:        fmt.Sprintf("%d-%d", time.Now().UnixNano(), scanJobSeq),
		Device:    device,
		State:     scanStateScanning,
		StartedAt: time.Now().Format("2006-01-02 15:04:05"),
		Pages:     []scanPage{},
		host:      host,
	}
	scanJobs[job.ID] = job
	scanJobList = append(scanJobList, job.ID)
	for len(scanJobList) > keptScanJobs {
		delete(scanJobs, scanJobList[0])
		scanJobList = scanJobList[1:]
	}
	return job
}

// scanJobRunning 判断有没有任务在跑。TWAIN 是串行的，同时发两次扫描只会排队，
// 排队期间客户端拿到的状态会很迷惑，所以直接不让开第二次。
func scanJobRunning() bool {
	scanJobMu.Lock()
	defer scanJobMu.Unlock()
	for _, id := range scanJobList {
		if scanJobs[id].State == scanStateScanning {
			return true
		}
	}
	return false
}

// preparePage 把 DLL 刚写出来的原始文件整理成可以交给客户端的一页：
// 转成 jpeg/png（转不动就留原始 BMP）、挪到扫描根目录、拼出访问地址。
// WebSocket 和 HTTP 两条路都走它，保证两边产出一致。
func preparePage(path, ext, host string, page int) (scanPage, error) {
	converted, convErr := convertScan(path, ext)
	if convErr != nil {
		// 转换失败会退回原始 BMP，扫描本身不受影响，记一笔就够。
		log.Printf("第 %d 页格式转换失败: %v", page, convErr)
	}

	flat, err := moveToScanRoot(converted)
	if err != nil {
		return scanPage{}, fmt.Errorf("移到扫描根目录失败: %w", err)
	}

	url, err := fileURL(host, flat)
	if err != nil {
		return scanPage{}, fmt.Errorf("生成访问地址失败: %w", err)
	}
	return scanPage{Page: page, File: filepath.Base(flat), URL: url}, nil
}

// normalizeExt 规整客户端给的图片格式：去掉点、转小写，没给就用默认（png）。
func normalizeExt(s string) string {
	e := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(s), "."))
	if e == "" {
		return legacyDefaultExt
	}
	return e
}

// ---- HTTP ----

type scanStartRequest struct {
	Device string `json:"device"`
	// Extension 是图片格式：jpeg / jpg / png，默认 png。和 WebSocket 的 extension 一致。
	Extension string `json:"extension"`
	// Count 是扫几页；显式传 0 表示走送纸器一直扫到没纸。
	// 用指针是为了区分"没传"和"传了 0"：没传按 1 处理。要是把没传也当 0，
	// 客户端只想扫一页却会把整叠纸都走完，这个坑很贵。
	Count          *int           `json:"count"`
	ScannerOptions map[string]any `json:"scannerOptions"`
}

func handleScanStart(w http.ResponseWriter, r *http.Request) {
	if !requirePOST(w, r) {
		return
	}

	var req scanStartRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体不是合法 JSON: "+err.Error())
		return
	}
	count := 1
	if req.Count != nil {
		count = *req.Count
		if count < 0 {
			count = 1
		}
	}
	ext := normalizeExt(req.Extension)

	if scanJobRunning() {
		// 409：不是请求写错了，是时机不对，客户端等上一次扫完再来。
		writeErr(w, http.StatusConflict, "上一次扫描还没结束，等它结束再发起（GET /api/scan/status 查进度）")
		return
	}

	// 连接设备、下发设置都在开任务之前做：失败就直接报错，不留下一个"失败的任务"让客户端去查。
	device := req.Device
	if device != "" {
		if err := TwainConnect(device); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	} else {
		device = TwainStatus().Device
		if device == "" {
			writeErr(w, http.StatusBadRequest, "没有连接扫描仪，请在 device 里指定设备名")
			return
		}
	}

	if len(req.ScannerOptions) > 0 {
		results, _, err := TwainApplyScannerOptions(req.ScannerOptions)
		if err != nil {
			writeErr(w, http.StatusConflict, err.Error())
			return
		}
		// 和 WebSocket 一致：有一项设不上就不扫，省得扫出一叠参数不对的图。
		if failed := failedOptions(results, labelsOf(device)); failed != "" {
			writeJSON(w, http.StatusConflict, map[string]any{
				"success": false,
				"error":   "扫描设置未生效，已取消扫描。" + failed,
				"results": results,
			})
			return
		}
	}

	job := newScanJob(device, r.Host)
	go runScanJob(job, ext, count)

	scanJobMu.Lock()
	snap := job.snapshot()
	scanJobMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "job": snap})
}

func runScanJob(job *scanJob, ext string, count int) {
	dir, err := newScanDir(scanRoot)
	if err != nil {
		finishScanJob(job, err)
		return
	}

	// 回调跑在 TWAIN 线程上，只做格式转换和登记，不能在这里做重活。
	progress := func(page int, path string) {
		p, prepErr := preparePage(path, ext, job.host, page)
		if prepErr != nil {
			log.Printf("扫描任务 %s 第 %d 页处理失败: %v", job.ID, page, prepErr)
			return
		}
		scanJobMu.Lock()
		job.Pages = append(job.Pages, p)
		scanJobMu.Unlock()
	}

	_, scanErr := TwainScan("", dir, count, progress)
	// 图都挪到扫描根目录了，临时目录空了就删掉；没空（有页处理失败）就留着备查。
	os.Remove(dir)
	finishScanJob(job, scanErr)
}

func finishScanJob(job *scanJob, err error) {
	scanJobMu.Lock()
	defer scanJobMu.Unlock()
	job.EndedAt = time.Now().Format("2006-01-02 15:04:05")
	if err != nil {
		job.State, job.Error = scanStateFailed, err.Error()
		log.Printf("扫描任务 %s 失败: %v", job.ID, err)
		return
	}
	job.State = scanStateDone
	log.Printf("扫描任务 %s 完成，共 %d 页", job.ID, len(job.Pages))
}

// handleScanStatus 查任务状态。不带 job 参数时给最近一次任务。
func handleScanStatus(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("job")

	scanJobMu.Lock()
	var job *scanJob
	if id == "" {
		if len(scanJobList) > 0 {
			job = scanJobs[scanJobList[len(scanJobList)-1]]
		}
	} else {
		job = scanJobs[id]
	}
	var snap scanJob
	if job != nil {
		snap = job.snapshot()
	}
	scanJobMu.Unlock()

	if job == nil {
		if id == "" {
			writeErr(w, http.StatusNotFound, "还没有发起过扫描")
			return
		}
		// 任务被淘汰了也走这里：只保留最近 20 次，客户端扫完要及时把结果取走。
		writeErr(w, http.StatusNotFound, "没有这个扫描任务（可能已过期）")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "job": snap})
}

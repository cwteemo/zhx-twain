package main

// 日志落盘。
//
// 双击启动（托盘模式）时没有控制台窗口，日志只打在屏幕上等于没有。所以默认把日志
// 同时写进 exe 旁边的 scansvc.log，出问题时直接看这个文件（托盘右键"打开日志"）。
//
// 只做按大小滚动一份备份（scansvc.log.1）：这个服务的日志量很小，几百 KB 顶天，
// 真正的 TWAIN 细节在 DLL 写的 twain.log 里，不需要按天分文件那套。

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
)

const (
	defaultLogFileName = "scansvc.log"
	// 超过这个大小就把当前日志改名成 .1，重新开一个。一份最多这么大，加备份共两份。
	maxLogBytes = 2 << 20 // 2MB
)

// logFilePath 是实际写入的日志文件路径，空表示没开文件日志。托盘菜单要用它。
var logFilePath string

// rotatingFile 是带大小滚动的日志文件。log 包会并发调 Write，要加锁。
type rotatingFile struct {
	mu   sync.Mutex
	path string
	f    *os.File
	size int64
}

func (w *rotatingFile) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return len(p), nil
	}
	if w.size+int64(len(p)) > maxLogBytes {
		w.rotate()
	}
	n, err := w.f.Write(p)
	w.size += int64(n)
	return n, err
}

// rotate 关掉当前文件、改名成 .1（覆盖上一份备份）、重新开一个。调用方须持有锁。
func (w *rotatingFile) rotate() {
	w.f.Close()
	os.Remove(w.path + ".1")
	os.Rename(w.path, w.path+".1")
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		w.f = nil
		return
	}
	w.f, w.size = f, 0
}

// teeWriter 往控制台和文件各写一份，控制台写失败不影响文件。
type teeWriter struct {
	console io.Writer
	file    io.Writer
}

func (t *teeWriter) Write(p []byte) (int, error) {
	if t.console != nil {
		t.console.Write(p) // 没有控制台时必然失败，忽略
	}
	return t.file.Write(p)
}

// setupLogFile 把日志接到文件上。path 为空表示关掉文件日志（只打控制台）。
//
// 控制台那一路保留：命令行跑的时候还是能直接看到日志，两边内容一样。
func setupLogFile(path string) {
	if path == "" {
		return
	}
	if !filepath.IsAbs(path) {
		dir := "."
		if exe, err := os.Executable(); err == nil {
			dir = filepath.Dir(exe)
		}
		path = filepath.Join(dir, path)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		log.Printf("⚠ 打开日志文件 %s 失败: %v（日志只打控制台）", path, err)
		return
	}
	var size int64
	if st, statErr := f.Stat(); statErr == nil {
		size = st.Size()
	}

	logFilePath = path
	w := &rotatingFile{path: path, f: f, size: size}
	// 同时写控制台和文件：命令行运行时照常能看，双击运行时文件里也有。
	// 不能用 io.MultiWriter：用 -H=windowsgui 编译时进程没有控制台，
	// 往 os.Stderr 写会报错，而 MultiWriter 一遇错就返回，文件那一路就被跳过了。
	log.SetOutput(&teeWriter{console: os.Stderr, file: w})
	fmt.Fprintf(w, "\n==== scansvc %s 启动 ====\n", serviceVersion)
}

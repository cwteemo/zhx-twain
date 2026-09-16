//go:build !windows

package main

// 非 Windows 下没有托盘（本服务本来也只能在 Windows 上跑，这个文件是为了
// 在 Linux 上也能 go vet / go build 整个包，方便改代码时先自查一遍）。

import "log"

func runTray() {
	log.Printf("当前系统没有托盘支持，按普通前台程序运行")
	if err := <-srv.Failed(); err != nil {
		log.Printf("服务异常退出: %v", err)
	}
}

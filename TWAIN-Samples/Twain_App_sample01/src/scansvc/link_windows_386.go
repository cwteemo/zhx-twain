package main

// 32 位构建链接 TWAIN_APP_CMD32.dll。说明见 link_windows_amd64.go。
//
// 32 位版存在的理由：TWAIN 的数据源（.ds）要加载进调用方进程，位数必须一致。
// 只发 32 位驱动的机型（柯达那几款老扫描仪就是）在 64 位服务里根本枚举不出来，
// 只能用 32 位版的 scansvc。用 /api/diagnose 可以确认是不是这个原因。

/*
#cgo LDFLAGS: -L${SRCDIR} -lTWAIN_APP_CMD32 -lstdc++
*/
import "C"

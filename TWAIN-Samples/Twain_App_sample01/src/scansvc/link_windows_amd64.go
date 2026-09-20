package main

// 64 位构建链接 TWAIN_APP_CMD64.dll。
//
// DLL 工程（visual_studio\TWAIN_APP_VS2017.vcxproj）的 TargetName 是
// $(ProjectName)32 / $(ProjectName)64，所以两种位数的产物名不同。
// 代码里只有"链接哪个 .lib"这一处需要分位数，用文件名后缀让 go 工具链
// 自己挑，就不必在源码里写条件编译，也不用维护两份 twain.go。
//
// 32 位那份见 link_windows_386.go。

/*
#cgo LDFLAGS: -L${SRCDIR} -lTWAIN_APP_CMD64 -lstdc++
*/
import "C"

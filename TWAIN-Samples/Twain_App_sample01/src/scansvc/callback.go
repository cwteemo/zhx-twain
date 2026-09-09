package main

// 这里 cgo 注释块刻意留空：使用 //export 的文件，preamble 里只允许出现声明，
// 不能出现任何定义（包括可能带 static inline 的头文件）。
// 需要用到的包装函数 scanWithGoCallback 放在 twain.go。

import "C"

// goScanCallback 是传给 DLL 的扫描完成回调，每扫出一张图 DLL 调一次，
// 参数是图片的完整路径。返回值 <= 0 会被 DLL 视为"用户取消扫描"。
//
//export goScanCallback
func goScanCallback(filename *C.char) C.int {
	onScannedFile(C.GoString(filename))
	return 1 // 继续扫描
}

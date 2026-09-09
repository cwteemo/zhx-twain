package main

// 使用 //export 的文件，cgo 注释块里只能有声明不能有定义，这里干脆留空。
// 包装函数 dbgScan 放在 main.go。

import "C"

//export goDbgScanCallback
func goDbgScanCallback(filename *C.char) C.int {
	name := C.GoString(filename)
	scanned = append(scanned, name)
	logf("  <回调> 扫出文件: %s", name)
	return 1 // 返回 <=0 会被 DLL 当成"用户取消扫描"
}

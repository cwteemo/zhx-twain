package main

// /api/diagnose 的接入层。真正的体检逻辑在 twaindiag 子包里（纯 Go，可以 go test），
// 这里只负责把设备清单喂给它、把结果写成 JSON。
//
// 这个接口是为"驱动装了却枚举不出来"准备的：最常见的原因是 TWAIN 驱动的位数
// 和本服务不一致，详见 twaindiag 包的说明。

import (
	"net/http"

	"scansvc/twaindiag"
)

func handleDiagnose(w http.ResponseWriter, r *http.Request) {
	// 设备清单从 DLL 拿（要占一次 TWAIN 线程），目录扫描和结论都在 twaindiag 里做。
	writeJSON(w, http.StatusOK, twaindiag.Collect(TwainDevices()))
}

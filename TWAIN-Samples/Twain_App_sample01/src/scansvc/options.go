package main

// getScannerOptions 的 TWAIN 侧：决定读哪些能力、在 TWAIN 线程上读出来，
// 翻译成前端选项模型的活全在 scanopt 包里（纯 Go，可单测）。

import "scansvc/scanopt"

// TwainScannerOptions 读当前已连接设备的选项。
// 注意：DLL 每读一项都会把数据源临时 enable 再 disable，这里要读十来项。
func TwainScannerOptions() ([]scanopt.Option, error) {
	device := TwainStatus().Device

	raw, err := TwainReadCapabilities(scanopt.Codes(device))
	if err != nil {
		return nil, err
	}
	return scanopt.Build(raw, device), nil
}

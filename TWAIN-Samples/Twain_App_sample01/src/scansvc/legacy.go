package main

// 兼容协议：对接既有前端（court-document-processing 的"加工/通用"分支）。
//
// 前端连的是 ws://127.0.0.1:5000/，消息用 handle 字段区分指令。这套协议最要命的
// 一个特点是**回显**：服务端把收到的整个 JSON 原样带回，再补上 code / data / message。
// scan 请求里的 sort、id 就是靠这个原样回去的，前端拿它们把图对应到具体档案页——
// 丢字段就等于图片不知道往哪儿放，所以这里一律在原对象上改，不另起结构体。
//
// 指令（对齐旧的 C# 服务端 zhxserver/Socket/MySocket.cs）：
//   scannerList        → data: ["设备名", ...]
//   scan               → 每扫出一张推一条，base64 字段装图片地址
//   getScannerOptions  → data: 选项数组
//   export / import    → 前端只用来关 loading
//
// 图片不内联：前端 uploadFile 里是 `base64.slice(base64.lastIndexOf('.') + 1)`
// 这样取扩展名的，所以给的必须是**以扩展名结尾的 URL**（.../xxx.png），
// 不能是 /api/image?id=123 这种查询串。图片由本服务的 /file/ 静态路由提供。

import (
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strings"
)

// legacy 协议的返回码，跟旧服务端保持一致。
const (
	legacyCodeOK     = 0
	legacyCodeError  = -1
	legacyCodeStop   = 2 // 前端收到会调 stopScan() 停止连续扫描
	legacyDefaultExt = "png"
)

// isLegacyMessage 判断一条消息该不该走兼容协议。
// 老前端发 {"handle":"..."}，本服务自带的协议发 {"cmd":"..."}，靠这个分流，
// 两套可以在同一个端点上共存。
func isLegacyMessage(raw map[string]any) bool {
	_, ok := raw["handle"]
	return ok
}

// legacyMsg 是解出来的原始对象，回显时直接在它上面加字段。
type legacyMsg map[string]any

func (m legacyMsg) str(key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func (m legacyMsg) boolean(key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}

// send 把当前对象连同 code 一起发回去。
func (m legacyMsg) send(c *wsClient, code int) {
	m["code"] = code
	payload, err := json.Marshal(m)
	if err != nil {
		log.Printf("兼容协议响应序列化失败: %v", err)
		return
	}
	c.Push(payload)
}

func (m legacyMsg) fail(c *wsClient, format string, args ...any) {
	// 前端读的是 data['msg'] || data['message']，两个都填上，省得版本差异漏提示。
	msg := fmt.Sprintf(format, args...)
	m["message"] = msg
	m["msg"] = msg
	m.send(c, legacyCodeError)
}

// handleLegacyMessage 处理一条 handle 协议消息。
func handleLegacyMessage(c *wsClient, raw map[string]any) {
	msg := legacyMsg(raw)

	switch msg.str("handle") {
	case "scannerList":
		st := TwainStatus()
		if !st.Ready {
			msg.fail(c, "TWAIN 环境未就绪，请确认 TWAINDSM.dll 与扫描仪驱动已安装")
			return
		}
		// data 给字符串数组。前端两种都认（字符串，或带 scanName 的对象），
		// 字符串是旧服务端的形式，保持一致。
		msg["data"] = TwainDevices()
		msg.send(c, legacyCodeOK)

	case "scan":
		handleLegacyScan(c, msg)

	case "getScannerOptions":
		handleLegacyScannerOptions(c, msg)

	case "export", "import":
		// 前端只拿它关 loading，本服务不负责导入导出。
		msg.send(c, legacyCodeOK)

	case "rfidRead", "codePrintList", "codePrint":
		// 被替换的那个 C# 服务端把 RFID 读卡（串口读卡器）和斑马打印机（ZPL）
		// 也挂在同一条 WebSocket 上，但那两块和 TWAIN 没有任何关系。
		// 本服务只做扫描，遇到它们给一句说得清的话，别让人对着"未实现"猜。
		msg.fail(c, "本服务只提供扫描功能，%s 属于 RFID / 条码打印，"+
			"这部分仍需原来的服务端（zhxserver）", msg.str("handle"))

	default:
		msg.fail(c, "本服务未实现的指令: %s", msg.str("handle"))
	}
}

func handleLegacyScan(c *wsClient, msg legacyMsg) {
	device := msg.str("scanner")
	if device == "" {
		msg.fail(c, "未指定扫描仪")
		return
	}

	// 顺序照搬旧服务端：先打开设备，再看是不是只要弹设置面板。
	// 设置面板要在数据源已打开(state 4)的前提下才能开。
	if err := TwainConnect(device); err != nil {
		msg.fail(c, "%s", err.Error())
		return
	}

	if msg.boolean("show_setting") {
		if err := TwainShowSettingUI(); err != nil {
			msg.fail(c, "%s", err.Error())
		}
		// 成功时**不回响应**，和旧服务端一致（那边是 deShowSettingUi() 后直接 return）。
		// 不能回 code:0——前端会把它当成一条扫描结果去读 data['base64']，
		// 那是 undefined，紧接着的 .slice() 会直接抛异常。
		// 前端本来就靠 updateScanStatus 的定时器复位 loading，不需要这条响应。
		return
	}

	ext := strings.ToLower(strings.TrimPrefix(msg.str("extension"), "."))
	if ext == "" {
		ext = legacyDefaultExt
	}

	dir, err := newScanDir(scanRoot)
	if err != nil {
		msg.fail(c, "%s", err.Error())
		return
	}

	// 每扫出一张就推一条，形式和旧服务端一致：整个请求对象回显 + base64 装地址。
	// 这个回调跑在 TWAIN 线程上，只做格式转换和一次非阻塞 Push。
	progress := func(page int, path string) {
		converted, convErr := convertScan(path, ext)
		if convErr != nil {
			// 转换失败会退回原始 BMP，扫描本身不受影响，记一笔就够。
			log.Printf("第 %d 页格式转换失败: %v", page, convErr)
		}

		url, urlErr := fileURL(c.host, converted)
		if urlErr != nil {
			log.Printf("第 %d 页生成访问地址失败: %v", page, urlErr)
			return
		}

		// 复制一份再改：原 msg 还要留着给后面的页用，直接改会互相覆盖。
		one := legacyMsg{}
		for k, v := range msg {
			one[k] = v
		}
		one["base64"] = url
		one.send(c, legacyCodeOK)
	}

	// 前端自己控制连续扫描（定时器循环发 scan），所以这里一次只扫一页，
	// 和旧服务端的 deRunScan 行为对齐。
	if _, err := TwainScan("", dir, 1, progress); err != nil {
		msg.fail(c, "%s", err.Error())
		return
	}
}

// handleLegacyScannerOptions 返回扫描仪选项，模型见 options.go。
//
// 前端在切换扫描仪时就会自动发这条（ScanImageHeader.vue 监听 curScanner），
// 进页面时默认选中列表第一台，所以这里**绝不能去打开设备**：设备列表来自已安装的
// 驱动，不代表设备接着——只装了驱动没接实体机，一进页面就会报"打开扫描仪失败"，
// 打开一台不在线的设备还要卡住 TWAIN 队列好几秒（实测 S8660 MSG_OPENDS 4.2 秒）。
// 设备只在用户点扫描时才连接（handleLegacyScan）。
//
// 所以只有请求的正好是**已经连着**的那台时才读真实选项，否则回空数组。
// 空数组而不是 null：前端判的是 `if (!options)`，null 会弹"请关闭其他扫描程序"，
// 空数组只是面板空着。
//
// TODO(TODO-settings.md #14): 只读不写，scan 里还没接收 scannerOptions。
func handleLegacyScannerOptions(c *wsClient, msg legacyMsg) {
	st := TwainStatus()
	device := msg.str("scanner")
	if !st.Connected || (device != "" && device != st.Device) {
		msg["data"] = []any{}
		msg.send(c, legacyCodeOK)
		return
	}

	options, err := TwainScannerOptions()
	if err != nil {
		msg.fail(c, "%s", err.Error())
		return
	}
	msg["data"] = options
	msg.send(c, legacyCodeOK)
}

// fileURL 把落盘路径转成前端能取的 URL。
// 必须以扩展名结尾——前端就是从最后一个点之后取扩展名的。
// host 是 WebSocket 握手请求里的 Host，用来定主机名；端口见 fileHost()。
func fileURL(host, path string) (string, error) {
	rel, err := filepath.Rel(scanRoot, path)
	if err != nil {
		return "", err
	}
	rel = filepath.ToSlash(rel)
	if strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("文件 %s 不在扫描目录内", path)
	}
	// 指向 HTTP 端口，和原服务一致（它推的是写死的 http://127.0.0.1:18080/file/...）。
	return "http://" + fileHost(host) + "/file/" + rel, nil
}

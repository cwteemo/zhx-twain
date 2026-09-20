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
//   getScannerOptions  → data: 设置项（协议见 SCANNER_OPTIONS_API.md）
//   setScannerOptions  → 下发设置项，data: 每项结果 + 最新设置项（本服务新增）
//   export / import    → 前端只用来关 loading
//
// 图片不内联：前端 uploadFile 里是 `base64.slice(base64.lastIndexOf('.') + 1)`
// 这样取扩展名的，所以给的必须是**以扩展名结尾的 URL**（.../xxx.png），
// 不能是 /api/image?id=123 这种查询串。图片由本服务的 /file/ 静态路由提供。
// 而且 URL 里 /file/ 后面只能有文件名、不能带子目录，前端上传时只取最后一段，
// 见 moveToScanRoot。

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
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

	case "setScannerOptions":
		handleLegacySetScannerOptions(c, msg)

	case "dumpCapabilities":
		// 不是旧服务端的指令，是给适配新扫描仪抓真实能力结构用的，见 capdump.go。
		dump, err := TwainDumpCapabilities(msg.str("scanner"))
		if err != nil {
			msg.fail(c, "%s", err.Error())
			return
		}
		msg["data"] = dump
		msg.send(c, legacyCodeOK)

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

	// 扫描中又来一条 scan：丢掉，什么都不回。
	//
	// 旧前端靠定时器循环发 scan 来实现"连续扫描"，而现在一次请求就扫到没纸，
	// 那些定时请求全是多余的。不丢的话它们会排在 TWAIN 线程队列里，
	// 等这一叠扫完再一个个执行，每个都扫不到纸，前端就会连弹好几条"没纸"。
	// 不回响应是安全的：前端本来就靠 updateScanStatus 的定时器复位 loading，
	// 它要的图这会儿正由进行中的那次扫描逐页推给它。
	// show_setting 是用户点"设置"，不能丢，所以只挡真正的扫描请求。
	if !msg.boolean("show_setting") && isBusy() {
		log.Printf("扫描进行中，忽略重复的 scan 请求（scanner=%s）", device)
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

	values, err := optionValues(msg["scannerOptions"])
	if err != nil {
		msg.fail(c, "%s", err.Error())
		return
	}

	// 扫描前先下发设置。有一项设不上就不扫：参数不对扫出来的图多半要重扫，
	// 不如直接告诉用户哪一项不行。这台扫描仪没有的项（skipped）忽略。
	if len(values) > 0 {
		results, _, applyErr := TwainApplyScannerOptions(values)
		if applyErr != nil {
			msg.fail(c, "%s", applyErr.Error())
			return
		}
		if failed := failedOptions(results, labelsOf(device)); failed != "" {
			msg.fail(c, "扫描设置未生效，已取消扫描。%s", failed)
			return
		}
	}

	ext := normalizeExt(msg.str("extension"))

	dir, err := newScanDir(scanRoot)
	if err != nil {
		msg.fail(c, "%s", err.Error())
		return
	}

	// 每扫出一张就推一条，形式和旧服务端一致：整个请求对象回显 + base64 装地址。
	// 这个回调跑在 TWAIN 线程上，只做格式转换和一次非阻塞 Push。
	// 格式转换、挪到扫描根目录、拼地址和 HTTP 那条路共用，见 scanjob.go 的 preparePage。
	progress := func(page int, path string) {
		p, err := preparePage(path, ext, c.host, page)
		if err != nil {
			log.Printf("第 %d 页处理失败: %v", page, err)
			return
		}

		// 复制一份再改：原 msg 还要留着给后面的页用，直接改会互相覆盖。
		one := legacyMsg{}
		for k, v := range msg {
			one[k] = v
		}
		one["base64"] = p.URL
		one.send(c, legacyCodeOK)
	}

	// 一次请求就把送纸器里的纸全部扫完（count=0）。
	// 每扫出一张仍然照原样推一条，所以前端不用改：它那个"定时器循环发 scan"的老逻辑
	// 在这次扫描期间发来的请求会被上面的 isBusy 挡掉，等纸走完之后再发的那一次
	// 才真的开一轮扫描、扫不到纸回"没纸"——和旧服务端一样，正好是前端停止循环的信号。
	_, err = TwainScan("", dir, 0, progress)
	// 图都已经挪到扫描根目录了，临时目录空了就删掉；没空（有页处理失败）就留着备查。
	os.Remove(dir)
	if err != nil {
		msg.fail(c, "%s", err.Error())
		return
	}

	// 这台设备第一次连上：读一份设置项存进缓存，并主动推给前端。
	// 前端进页面时设备还没连，那时 getScannerOptions 只能给空的，设置面板就一直是空的。
	if !hasCachedOptions(device) {
		if data, optErr := TwainScannerOptions(); optErr == nil {
			push := legacyMsg{"handle": "getScannerOptions", "scanner": device, "data": data}
			push.send(c, legacyCodeOK)
		}
	}
}

// moveToScanRoot 把扫描出的图从本次扫描的临时目录挪到扫描根目录，返回新路径。
//
// 为什么要平铺：前端上传时是 `url.substring(url.lastIndexOf('/') + 1)` 只取 URL
// 最后一段当 filename 传给 /file/upload 的（court-document-processing 加工/通用分支
// ScanCom/Updata.js）。原服务的图就平铺在 temp 根目录下，最后一段就是完整相对路径；
// 这里要是留在时间戳子目录里，子目录就被前端丢掉了，上传时找不到文件。
//
// 临时目录本身不能省：DLL 靠"扫描前后目录里新增的 .bmp"识别产物，要一个干净目录。
//
// 重名时加 _1、_2 后缀，绝不覆盖：DLL 的文件名是 进程内固定的批次号 + 序号，
// 而原 BMP 转换后会被删掉，DLL 在目录里找不到旧文件，序号会从 1 重新数，
// 同一次运行里前后两次扫描产出同名文件是常态。覆盖掉的话，前端已显示、
// 还没上传完的那页就被换成了新扫的图。扫描在 TWAIN 线程上串行，查了再挪不会有竞争。
func moveToScanRoot(path string) (string, error) {
	if filepath.Dir(path) == scanRoot {
		return path, nil
	}

	ext := filepath.Ext(path)
	stem := strings.TrimSuffix(filepath.Base(path), ext)
	dst := filepath.Join(scanRoot, stem+ext)
	for i := 1; fileExist(dst); i++ {
		dst = filepath.Join(scanRoot, fmt.Sprintf("%s_%d%s", stem, i, ext))
	}

	if err := os.Rename(path, dst); err != nil {
		return "", err
	}
	return dst, nil
}

// handleLegacyScannerOptions 返回扫描仪设置项，协议见 SCANNER_OPTIONS_API.md。
//
// 前端在切换扫描仪时就会自动发这条（ScanImageHeader.vue 监听 curScanner），
// 进页面时默认选中列表第一台，所以这里**绝不能去打开设备**：设备列表来自已安装的
// 驱动，不代表设备接着——只装了驱动没接实体机，一进页面就会报"打开扫描仪失败"，
// 打开一台不在线的设备还要卡住 TWAIN 队列好几秒（实测 S8660 MSG_OPENDS 4.2 秒）。
// 设备只在用户点扫描时才连接（handleLegacyScan）。
//
// 所以只有请求的正好是**已经连着**的那台时才现读；否则给上次读到的缓存（connected=false,
// cached=true），从来没连过的给空 options。data 永远是对象不是 null：前端判的是
// `if (!options)`，null 会弹"请关闭其他扫描程序"。
func handleLegacyScannerOptions(c *wsClient, msg legacyMsg) {
	msg["data"] = ScannerOptionsFor(msg.str("scanner"))
	msg.send(c, legacyCodeOK)
}

// handleLegacySetScannerOptions 下发设置项。这是用户在设置面板点"应用"，
// 属于明确的操作，所以**会打开设备**（和 getScannerOptions 不同）。
// data 带回每一项的结果和下发后现读的设置项；有失败项时 code=-1、msg 说明哪几项。
func handleLegacySetScannerOptions(c *wsClient, msg legacyMsg) {
	device := msg.str("scanner")
	if device == "" {
		msg.fail(c, "未指定扫描仪")
		return
	}
	values, err := optionValues(msg["scannerOptions"])
	if err != nil {
		msg.fail(c, "%s", err.Error())
		return
	}
	if err := TwainConnect(device); err != nil {
		msg.fail(c, "%s", err.Error())
		return
	}

	results, data, err := TwainApplyScannerOptions(values)
	if err != nil {
		msg.fail(c, "%s", err.Error())
		return
	}
	if results == nil {
		results = []optionResult{}
	}
	msg["data"] = map[string]any{"results": results, "options": data}
	if failed := failedOptions(results, labelsOf(device)); failed != "" {
		msg.fail(c, "部分设置未生效。%s", failed)
		return
	}
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

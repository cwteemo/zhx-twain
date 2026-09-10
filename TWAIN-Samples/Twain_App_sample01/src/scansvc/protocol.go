package main

// WebSocket 消息协议。
//
// 这是**默认协议**，和 HTTP 接口一一对应。要对接一个已有的前端，
// 改这个文件就够了——ws.go 里的连接管理与协议无关，不用动。
//
// 请求：{"id":"任意串","cmd":"scan","params":{...}}
//   id 原样带回，用来把响应和请求配对；不关心配对可以不传。
// 响应：{"id":"...","cmd":"scan","success":true,"data":{...}}
//       {"id":"...","cmd":"scan","success":false,"error":"打开扫描仪失败: ..."}
// 推送：{"cmd":"scan.progress","data":{"page":1,...}}   没有 id，服务端主动发
//
// 图片不走 WebSocket，仍旧由 GET /api/image?id=xxx 取——WebSocket 和 HTTP
// 在同一个端口上，前端拿到 url 直接塞进 <img src> 即可。单张 BMP 十几 MB，
// 塞进 JSON 要先 base64 膨胀三分之一，还会把出站队列堵死。

import (
	"encoding/json"
	"fmt"
	"log"
)

type wsRequest struct {
	ID     string          `json:"id"`
	Cmd    string          `json:"cmd"`
	Params json.RawMessage `json:"params"`
}

type wsResponse struct {
	ID      string `json:"id,omitempty"`
	Cmd     string `json:"cmd"`
	Success bool   `json:"success"`
	Data    any    `json:"data,omitempty"`
	Error   string `json:"error,omitempty"`
}

func (c *wsClient) reply(req wsRequest, data any) {
	c.sendJSON(wsResponse{ID: req.ID, Cmd: req.Cmd, Success: true, Data: data})
}

func (c *wsClient) replyErr(req wsRequest, err error) {
	c.sendJSON(wsResponse{ID: req.ID, Cmd: req.Cmd, Success: false, Error: err.Error()})
}

func (c *wsClient) sendJSON(v any) {
	payload, err := json.Marshal(v)
	if err != nil {
		log.Printf("WebSocket 响应序列化失败: %v", err)
		return
	}
	c.Push(payload)
}

// handleWSMessage 处理一条入站消息。由 ws.go 在独立 goroutine 里调用，
// 真正的串行化由 TWAIN 线程的任务队列保证。
func handleWSMessage(c *wsClient, payload []byte) {
	// 先当成通用对象解一次，用来分流：老前端发 {"handle":...}，本协议发 {"cmd":...}。
	// 两套协议共存在同一个端点上，换前端不用换端口。
	var raw map[string]any
	if err := json.Unmarshal(payload, &raw); err != nil {
		c.sendJSON(wsResponse{Cmd: "error", Success: false,
			Error: "消息不是合法 JSON: " + err.Error()})
		return
	}
	if isLegacyMessage(raw) {
		handleLegacyMessage(c, raw)
		return
	}

	var req wsRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		c.sendJSON(wsResponse{Cmd: "error", Success: false,
			Error: "消息格式不对: " + err.Error()})
		return
	}

	switch req.Cmd {
	case "status":
		c.reply(req, TwainStatus())

	case "devices":
		devices := TwainDevices()
		c.reply(req, map[string]any{"devices": devices, "count": len(devices)})

	case "connect":
		var p struct {
			Device string `json:"device"`
		}
		if !c.bindParams(req, &p) {
			return
		}
		if err := TwainConnect(p.Device); err != nil {
			c.replyErr(req, err)
			return
		}
		c.reply(req, TwainStatus())

	case "disconnect":
		TwainDisconnect()
		c.reply(req, TwainStatus())

	case "reconnect":
		var p struct {
			Deep bool `json:"deep"`
		}
		if !c.bindParams(req, &p) {
			return
		}
		device, err := TwainReconnect(p.Deep)
		if err != nil {
			c.replyErr(req, err)
			return
		}
		c.reply(req, map[string]any{"device": device, "status": TwainStatus()})

	case "getConfig":
		cfg, err := TwainCurrentConfig()
		if err != nil {
			c.replyErr(req, err)
			return
		}
		c.reply(req, cfg)

	case "config":
		var cfg ScanConfig
		if !c.bindParams(req, &cfg) {
			return
		}
		results, err := TwainApplyConfig(cfg)
		if err != nil {
			c.replyErr(req, err)
			return
		}
		c.reply(req, map[string]any{"results": results})

	case "capability":
		var p struct {
			Name string `json:"name"`
		}
		if !c.bindParams(req, &p) {
			return
		}
		raw, err := TwainCapability(p.Name)
		if err != nil {
			c.replyErr(req, err)
			return
		}
		// raw 是 DLL 吐出的 JSON 片段，原样透传，不二次解析——
		// 容器有 ONEVALUE/ENUMERATION/RANGE 三种，解析了反而丢信息。
		c.reply(req, map[string]any{"name": p.Name, "capability": json.RawMessage(raw)})

	case "setCapability":
		var p struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		}
		if !c.bindParams(req, &p) {
			return
		}
		if err := TwainSetCapability(p.Name, p.Value); err != nil {
			c.replyErr(req, err)
			return
		}
		c.reply(req, nil)

	case "scan":
		c.handleScanCmd(req)

	default:
		c.replyErr(req, fmt.Errorf("未知指令 %q（支持：status devices connect disconnect "+
			"reconnect config getConfig capability setCapability scan）", req.Cmd))
	}
}

// bindParams 解析 params。params 缺省时按零值处理——像 disconnect、
// reconnect 这种不带参数也说得通的指令不该因为少个字段就报错。
func (c *wsClient) bindParams(req wsRequest, dst any) bool {
	if len(req.Params) == 0 {
		return true
	}
	if err := json.Unmarshal(req.Params, dst); err != nil {
		c.replyErr(req, fmt.Errorf("params 解析失败: %w", err))
		return false
	}
	return true
}

func (c *wsClient) handleScanCmd(req wsRequest) {
	var p struct {
		Device string      `json:"device"`
		Count  int         `json:"count"`
		Config *ScanConfig `json:"config"`
	}
	if !c.bindParams(req, &p) {
		return
	}
	if p.Count < 0 {
		p.Count = 1
	}

	// 先连上再设参数：TWAIN 的能力协商只在数据源打开(state 4)之后有效。
	if p.Device != "" {
		if err := TwainConnect(p.Device); err != nil {
			c.replyErr(req, err)
			return
		}
	}
	if p.Config != nil {
		if _, err := TwainApplyConfig(*p.Config); err != nil {
			c.replyErr(req, err)
			return
		}
	}

	dir, err := newScanDir(scanRoot)
	if err != nil {
		c.replyErr(req, err)
		return
	}

	// 边扫边推。这个回调跑在 TWAIN 线程上，Push 是非阻塞的（队列满就断开连接），
	// 绝不能在这里等对端——那等于让一条慢连接卡住扫描本身。
	var infos []imageInfo
	progress := func(page int, path string) {
		info := registerImage(path)
		infos = append(infos, info)
		c.sendJSON(wsResponse{Cmd: "scan.progress", Success: true, Data: map[string]any{
			"page":  page,
			"image": info,
		}})
	}

	files, err := TwainScan("", dir, p.Count, progress)
	if err != nil {
		c.replyErr(req, err)
		return
	}

	// infos 是在 TWAIN 线程上填的，TwainScan 返回时那条线程已经跑完这次任务，
	// 这里读它是安全的。正常情况下它和 files 一一对应；万一回调漏了谁，
	// 以 files 为准补齐，别让前端少收图。
	if len(infos) != len(files) {
		infos = infos[:0]
		for _, f := range files {
			infos = append(infos, registerImage(f))
		}
	}

	c.reply(req, map[string]any{"count": len(infos), "images": infos})
}

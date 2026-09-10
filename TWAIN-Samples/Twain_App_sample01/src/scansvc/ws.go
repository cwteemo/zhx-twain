package main

// WebSocket 接入层。
//
// 存在的理由：扫描是长活儿——多页 ADF 作业几分钟很正常，HTTP 同步等着必然超时，
// 而且没法边扫边报进度。WebSocket 长连接可以每扫出一张就推一条。
//
// 这一层只负责"连接怎么管"，具体收发什么消息由 protocol.go 决定，
// 这样换协议（对接既有前端 / 定义新协议）不用动连接管理。

import (
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// 写超时：对端卡住时别让写循环永久阻塞。
	wsWriteWait = 30 * time.Second
	// 读一条消息的上限。图片走不了这个方向，请求消息不会大。
	wsMaxMessageSize = 1 << 20
	// Pong 等待与 Ping 间隔：Ping 间隔必须小于 Pong 等待，否则连接会被自己判死。
	wsPongWait   = 90 * time.Second
	wsPingPeriod = 60 * time.Second
	// 出站缓冲。扫描进度可能连着推，留一点余量；满了说明对端消费不动，直接断开。
	wsSendBuffer = 64
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	// 本机服务，调用方是哪个业务系统的页面都有可能，和 HTTP 那边的 CORS 保持一致：不拦。
	CheckOrigin: func(r *http.Request) bool { return true },
}

// wsClient 是一条已建立的连接。
//
// gorilla/websocket 允许一个读 goroutine 和一个写 goroutine 并发，但**不允许**
// 多个 goroutine 同时写。所以所有出站消息一律塞进 send，由唯一的写循环发出去——
// 扫描回调在 TWAIN 线程上推进度，读循环在自己的 goroutine 里回响应，
// 两边都往 send 里塞，谁都不直接碰 conn。
type wsClient struct {
	conn *websocket.Conn
	send chan []byte
	// host 是握手请求里的 Host（如 127.0.0.1:5000），用来拼图片的访问地址。
	host     string
	closeOne sync.Once
}

// 旧的 C# 服务端只保留一条连接：新连接进来就把之前的全部关掉。前端很可能依赖
// 这个行为（页面刷新后重连，旧连接不会残留），所以照做。
var (
	clientMu      sync.Mutex
	currentClient *wsClient
)

// Push 把一条消息排进出站队列。可以从任意 goroutine 调用，包括 TWAIN 线程。
// 队列满（对端消费不动）时丢弃并断开，不阻塞调用方——绝不能让一条卡死的
// WebSocket 把 TWAIN 线程拖住。
func (c *wsClient) Push(payload []byte) {
	select {
	case c.send <- payload:
	default:
		log.Printf("WebSocket 出站队列已满，断开该连接（对端消费不过来）")
		c.close()
	}
}

func (c *wsClient) close() {
	c.closeOne.Do(func() { close(c.send) })
}

// handleWS 是 WebSocket 端点。
func handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade 内部已经写过 HTTP 错误响应了，这里只记一笔。
		log.Printf("WebSocket 握手失败: %v", err)
		return
	}

	client := &wsClient{
		conn: conn,
		send: make(chan []byte, wsSendBuffer),
		host: r.Host,
	}

	clientMu.Lock()
	if currentClient != nil {
		log.Printf("有新连接接入，关闭旧连接 %s", currentClient.conn.RemoteAddr())
		currentClient.close()
	}
	currentClient = client
	clientMu.Unlock()

	log.Printf("WebSocket 已连接: %s", conn.RemoteAddr())

	go client.writeLoop()
	client.readLoop()

	clientMu.Lock()
	if currentClient == client {
		currentClient = nil
	}
	clientMu.Unlock()
}

// readLoop 收消息、交给协议层处理，返回后连接即关闭。
func (c *wsClient) readLoop() {
	defer func() {
		c.close()
		c.conn.Close()
		log.Printf("WebSocket 已断开: %s", c.conn.RemoteAddr())
	}()

	c.conn.SetReadLimit(wsMaxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(wsPongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(wsPongWait))
	})

	for {
		msgType, payload, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				log.Printf("WebSocket 读取出错: %v", err)
			}
			return
		}
		if msgType != websocket.TextMessage && msgType != websocket.BinaryMessage {
			continue
		}

		// 协议层可能要做扫描这种长活儿，不能占着读循环——占着的话
		// 扫描期间连"取消"这类消息都收不到。所以起独立 goroutine 处理；
		// 真正的串行化由 TWAIN 线程的任务队列保证，这里不需要额外加锁。
		go handleWSMessage(c, payload)
	}
}

// writeLoop 是这条连接唯一的写出口。
func (c *wsClient) writeLoop() {
	ticker := time.NewTicker(wsPingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case payload, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
			if !ok {
				// send 被关掉，说明该收工了，尽量礼貌地告别。
				c.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(
					websocket.CloseNormalClosure, ""))
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, payload); err != nil {
				log.Printf("WebSocket 写入失败: %v", err)
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

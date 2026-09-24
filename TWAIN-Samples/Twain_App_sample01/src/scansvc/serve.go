package main

// 监听与端口。
//
// 服务监听两个端口，和被替换掉的那个转发服务保持一致——它也是一个进程开两个：
//
//	5000    WebSocket 端口   前端写死 ws://127.0.0.1:5000/
//	18080   HTTP 端口        前端写死 http://127.0.0.1:18080/dir/*、/file/*
//
// 两个端口供的是**同一套路由**，谁也不比谁特殊：分开只是因为前端把两个地址都
// 编译进打包好的 js 里了，改不动。所以从哪个端口调什么都通，不用记。
//
// 这个文件不碰 TWAIN，端口逻辑可以脱离扫描仪单独验证。

import (
	"flag"
	"log"
	"net"
	"net/http"
	"strconv"
	"sync"
)

// 默认端口；端口被 -auto-port 顺延时最多向后试 autoPortTries 个。
const (
	// 两个默认值都不是随便定的：既有前端把 ws://127.0.0.1:5000/ 和
	// http://127.0.0.1:18080 写死在打包好的代码里，本服务是去替换那个中间服务的，
	// 端口对不上前端连都连不上。
	defaultWSPort   = 5000
	defaultHTTPPort = 18080
	autoPortTries   = 20
)

// 这些 flag 只是**命令行入口**，真正生效的值一律读 cfg（见 config.go）——
// 配置文件和环境变量也能给同一个值，绕过 cfg 直接读 flag 会把它们吞掉。
var (
	host = flag.String("host", "", "监听地址，留空为所有网卡；只允许本机访问填 127.0.0.1")
	// 两个端口供同一套路由，分开只是为了对齐既有前端写死的那两个地址。
	wsPort   = flag.Int("ws-port", defaultWSPort, "WebSocket 端口（既有前端写死 ws://127.0.0.1:5000/）；0 表示不监听")
	httpPort = flag.Int("http-port", defaultHTTPPort, "HTTP 端口（既有前端写死 http://127.0.0.1:18080）；0 表示不监听")
	autoPort = flag.Bool("auto-port", false, "端口被占用时自动向后顺延寻找可用端口")
)

// httpPortActual 是 HTTP 端口实际监听到的端口号（-auto-port 可能让它顺延过），
// 没监听成功就是 0。legacy.go 生成图片 URL 时要用它，见 fileHost()。
var httpPortActual int

// servers 管着两个监听，可以随时停掉再起来——托盘菜单里的"停止服务 / 启动服务"就是它。
//
// 停止只关监听（不再接受新连接），不动 TWAIN：扫描仪连着的状态、设置项缓存都还在，
// 重新启动不用再等一次设备打开。
type servers struct {
	mu        sync.Mutex
	handler   http.Handler
	listeners []net.Listener
	// fail 用来把"没人动它却挂了"的监听错误送出去。主动停止时不往里发。
	fail chan error
}

var srv = &servers{fail: make(chan error, 2)}

// Start 把两个端口都起起来，返回实际起来的监听数。
// 一个端口起不来不影响另一个——扫描和文件管理是两条相对独立的链路，
// 能通一条是一条，比整个服务起不来强。
func (s *servers) Start() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.listeners) > 0 {
		return len(s.listeners) // 已经在跑了
	}

	bindHost, wp, hp := cfg.Host, cfg.WSPort, cfg.HTTPPort
	handler := s.handler

	if wp > 0 {
		if ln, err := listenWithFallback(hostPort(bindHost, wp), cfg.AutoPort); err != nil {
			reportListenFail("WebSocket", wp, err)
		} else {
			s.listeners = append(s.listeners, ln)
			log.Printf("WebSocket 端口已启动: ws://localhost:%s/  （既有前端写死的地址）", portOf(ln.Addr().String()))
			s.serve(ln, handler)
		}
	}

	if hp > 0 && hp != wp {
		if ln, err := listenWithFallback(hostPort(bindHost, hp), cfg.AutoPort); err != nil {
			reportListenFail("HTTP", hp, err)
		} else {
			s.listeners = append(s.listeners, ln)
			actual := portOf(ln.Addr().String())
			// 记下实际端口：WebSocket 推图片地址时要指到这个端口上来。
			if n, convErr := strconv.Atoi(actual); convErr == nil {
				httpPortActual = n
			}
			log.Printf("HTTP 端口已启动: http://localhost:%s  （既有前端写死的地址）", actual)
			s.serve(ln, handler)
		}
	} else if hp > 0 && hp == wp {
		httpPortActual = wp
		log.Printf("WebSocket 和 HTTP 指定了同一个端口 %d，只监听一次（两边本来就是同一套路由）", wp)
	}

	return len(s.listeners)
}

func (s *servers) serve(ln net.Listener, handler http.Handler) {
	go func() {
		err := http.Serve(ln, handler)
		// 还在当前监听列表里才算"意外挂掉"；Stop 会先把它摘掉再关。
		// 原来用一个全局 stopping 标记判断：托盘"重启"是 Stop 紧接着 Start，
		// Start 把标记复位时旧监听的 goroutine 往往还没走到这里，
		// 结果每次重启都误报两条"服务异常退出: use of closed network connection"。
		s.mu.Lock()
		current := false
		for _, l := range s.listeners {
			if l == ln {
				current = true
				break
			}
		}
		s.mu.Unlock()
		if !current {
			return // 是我们自己关的，正常
		}
		select {
		case s.fail <- err:
		default:
		}
	}()
}

// Stop 关掉监听。已经停了再调也没事。
func (s *servers) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.listeners) == 0 {
		return
	}
	for _, ln := range s.listeners {
		ln.Close()
	}
	s.listeners = nil
	httpPortActual = 0
	log.Printf("监听已停止（扫描仪连接和缓存保留，重新启动即可继续用）")
}

// Running 表示当前有没有在监听。
func (s *servers) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.listeners) > 0
}

// Addrs 返回当前监听的地址，给托盘菜单显示用。
func (s *servers) Addrs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.listeners))
	for _, ln := range s.listeners {
		out = append(out, ln.Addr().String())
	}
	return out
}

// Failed 是监听意外挂掉时的通知（主动停止不会发）。
func (s *servers) Failed() <-chan error { return s.fail }

// reportListenFail 把监听失败说清楚。端口被占是最常见的情况，直接给排查命令。
func reportListenFail(name string, port int, err error) {
	log.Printf("⚠ %s 端口 %d 监听失败: %v", name, port, err)
	log.Printf("  端口多半被别的程序占着，可以：")
	log.Printf("    1) 查是谁占着：netstat -ano | findstr :%d", port)
	log.Printf("    2) 换个端口：scansvc.exe -ws-port 5001 -http-port 18081")
	log.Printf("    3) 让它自动顺延：scansvc.exe -auto-port")
	if port == defaultHTTPPort {
		log.Printf("  注意：既有前端把文件管理接口写死成 http://127.0.0.1:%d，"+
			"这个端口起不来它就调不通。多半是旧的转发服务还开着，关掉它再启动本服务。", port)
	}
}

// hostPort 拼监听地址。host 为空时得到 ":5000" 这种形式，即所有网卡。
func hostPort(bindHost string, port int) string {
	return net.JoinHostPort(bindHost, strconv.Itoa(port))
}

// fileHost 决定推给前端的图片 URL 用哪个 host:port。
//
// 被替换掉的那个服务推的是写死的 http://127.0.0.1:18080/file/...，也就是它的
// HTTP 端口。这里照做：主机名取 WebSocket 握手时前端用的那个（前端连 127.0.0.1
// 就回 127.0.0.1，回一个它够不着的地址等于图片打不开），端口换成 HTTP 端口实际
// 监听到的那个。HTTP 端口没起来就退回 WebSocket 那个——那上面也有 /file/。
func fileHost(wsHost string) string {
	if wsHost == "" {
		wsHost = hostPort("127.0.0.1", defaultWSPort)
	}
	if httpPortActual <= 0 {
		return wsHost
	}
	h, _, err := net.SplitHostPort(wsHost)
	if err != nil {
		// Host 头没带端口（走 80 端口时会这样），整个就是主机名
		h = wsHost
	}
	return hostPort(h, httpPortActual)
}

// portOf 从监听地址里取出端口号，取不到就原样返回，只用于日志和提示。
func portOf(addr string) string {
	if _, p, err := net.SplitHostPort(addr); err == nil {
		return p
	}
	return addr
}

// listenWithFallback 在指定地址上监听；开了 auto 就在端口被占用时向后顺延，
// 直到找到一个能用的（最多试 autoPortTries 个）。
func listenWithFallback(addr string, auto bool) (net.Listener, error) {
	ln, err := net.Listen("tcp", addr)
	if err == nil || !auto {
		return ln, err
	}

	host, portStr, splitErr := net.SplitHostPort(addr)
	if splitErr != nil {
		return nil, err
	}
	base, convErr := strconv.Atoi(portStr)
	if convErr != nil {
		return nil, err
	}

	firstErr := err
	for i := 1; i <= autoPortTries; i++ {
		next := base + i
		if next > 65535 {
			break
		}
		cand := net.JoinHostPort(host, strconv.Itoa(next))
		if ln, err := net.Listen("tcp", cand); err == nil {
			log.Printf("端口 %d 不可用（%v），已自动顺延到 %d", base, firstErr, next)
			return ln, nil
		}
	}
	return nil, firstErr
}

// withCORS 允许业务系统从其它域名调用本机服务。
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		// token / DIR / session 是既有前端调文件管理接口时会带的自定义头，
		// 不在这里放行，浏览器的预检就过不去，请求根本到不了服务端。
		w.Header().Set("Access-Control-Allow-Headers",
			"Authorization, Content-Type, Content-Length, X-CSRF-Token, Token, token, DIR, session")
		w.Header().Set("Access-Control-Expose-Headers",
			"Content-Length, Access-Control-Allow-Origin, Access-Control-Allow-Headers")
		w.Header().Set("Access-Control-Max-Age", "86400")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

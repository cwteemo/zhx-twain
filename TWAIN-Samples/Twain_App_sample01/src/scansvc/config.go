package main

// 配置文件。
//
// 所有能自定义的项都可以写进配置文件，**键名和命令行参数一模一样**（去掉前面的 `-`），
// 不用记两套。文件放在 exe 旁边，第一次启动时自动生成一份带注释的模板，
// 改完重启生效。
//
// 优先级：命令行 > 环境变量 > 配置文件 > 内置默认值。
//
// 命令行判的是"有没有**显式给过**"，不是拿值和默认值比——`-http-port 0`（显式关掉）
// 和"没给"是两回事，拿值去比会被配置文件覆盖回去，等于关不掉。

import (
	"bytes"
	"flag"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

// configFileName 是默认的配置文件名，放在 exe 所在目录。
const configFileName = "scansvc.conf"

var configFile = flag.String("config", "", "配置文件路径；留空则用 exe 所在目录下的 "+configFileName)

// settings 是最终生效的配置。
// 各处一律读它，**不要**再去读 flag 变量——那样会绕过配置文件和环境变量。
type settings struct {
	Host       string
	WSPort     int
	HTTPPort   int
	AutoPort   bool
	ScanDir    string
	CleanHours int
	LogFile    string
	Tray       bool
}

var cfg settings

// knownKeys 用来提醒键名拼错。手工维护配置最容易栽在这上面：
// 写成 ws_port 或者 wsport，服务照常按默认值跑，人还以为改生效了。
var knownKeys = map[string]bool{
	"host": true, "ws-port": true, "http-port": true,
	"auto-port": true, "dir": true, "clean-hours": true,
	"log": true, "tray": true,
}

// configTemplate 是自动生成的默认配置。
// 写出去时换行会转成 CRLF——老版本记事本遇到 LF 会把整个文件挤成一行。
const configTemplate = `# scansvc 配置文件
# ------------------------------------------------------------------
# 改完保存，重启 scansvc.exe 生效。
#
# 每行一个 <键> = <值>，键名和命令行参数一样（去掉前面的 -）。
# 整行以 # 或 ; 开头的是注释；行尾不支持注释——路径里就可能带 # 号。
# 命令行参数和环境变量都优先于这个文件。
#
# 文件要存成 UTF-8 编码，否则中文路径会乱码（记事本「另存为」时可以选编码）。
# ------------------------------------------------------------------

# WebSocket 端口。前端把 ws://127.0.0.1:5000/ 写死在代码里了，
# 除非端口冲突否则别改——改了前端那边也得跟着改才连得上。
# 填 0 表示不监听。
ws-port = 5000

# HTTP 端口。前端把 http://127.0.0.1:18080 写死在代码里了，同上。
# 填 0 表示不监听。
http-port = 18080

# 监听地址。留空监听所有网卡（同一局域网的机器也能连）；
# 只允许本机访问就填 127.0.0.1。
host =

# 端口被占用时自动向后顺延，找一个能用的（最多试 20 个）。
# 注意：顺延之后前端就连不上了（它认死 5000 / 18080），
# 所以这个适合排查问题时临时开，日常建议关着。
auto-port = false

# 扫描图片保存根目录。相对路径是相对启动时的工作目录算的，
# 建议直接写绝对路径，比如  dir = D:\scansvc\scans
dir = scans

# 自动清掉扫描目录下超过 N 小时的图片，0 表示不清理。
# 只清扫描目录，不碰 /dir/verify 选中的那个档案目录。
clean-hours = 0

# 日志文件。相对路径是相对 exe 所在目录；留空表示不写文件（只打控制台）。
# 双击启动时没有控制台窗口，日志全靠这个文件，别关掉。
# 超过 2MB 会改名成 scansvc.log.1 重新开一个，最多占两份。
log = scansvc.log

# 托盘图标。true 时双击启动就缩到右下角托盘，右键有启动 / 停止 / 重启 / 退出。
# false 时按普通控制台程序跑（命令行调试、或者用别的方式托管时用）。
tray = true
`

// loadSettings 解析出最终生效的配置。必须在 flag.Parse() 之后调用。
func loadSettings() settings {
	// 先记下命令行显式给过哪些——下面读配置时要靠它判断谁说了算。
	explicit := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { explicit[f.Name] = true })

	path := configPath()
	kv, err := readConfigKV(path)
	switch {
	case err == nil:
		log.Printf("已加载配置文件: %s", path)
		for k := range kv {
			if !knownKeys[k] {
				log.Printf("⚠ 配置文件里有不认识的键 %q，已忽略（键名和命令行参数一样，"+
					"去掉前面的 -；可用的有：host、ws-port、http-port、auto-port、dir、clean-hours、log、tray）", k)
			}
		}
	case os.IsNotExist(err):
		writeDefaultConfig(path)
		kv = map[string]string{}
	default:
		log.Printf("⚠ 读配置文件 %s 失败: %v（按默认值运行）", path, err)
		kv = map[string]string{}
	}

	r := resolver{explicit: explicit, kv: kv}
	s := settings{
		Host:       r.str("host", *host, "SCANSVC_HOST", ""),
		WSPort:     r.num("ws-port", *wsPort, "SCANSVC_WS_PORT", defaultWSPort),
		HTTPPort:   r.num("http-port", *httpPort, "SCANSVC_HTTP_PORT", defaultHTTPPort),
		AutoPort:   r.yes("auto-port", *autoPort, "SCANSVC_AUTO_PORT", false),
		ScanDir:    r.str("dir", *scanDir, "SCANSVC_DIR", defaultScanDir),
		CleanHours: r.num("clean-hours", *cleanHours, "SCANSVC_CLEAN_HOURS", 0),
		LogFile:    r.str("log", *logFile, "SCANSVC_LOG", defaultLogFileName),
		Tray:       r.yes("tray", *tray, "SCANSVC_TRAY", true),
	}

	s.WSPort = clampPort("ws-port", s.WSPort, defaultWSPort)
	s.HTTPPort = clampPort("http-port", s.HTTPPort, defaultHTTPPort)
	if s.CleanHours < 0 {
		log.Printf("⚠ clean-hours 不能是负数（当前 %d），按 0（不清理）处理", s.CleanHours)
		s.CleanHours = 0
	}
	if strings.TrimSpace(s.ScanDir) == "" {
		s.ScanDir = defaultScanDir
	}
	return s
}

func clampPort(name string, port, def int) int {
	if port < 0 || port > 65535 {
		log.Printf("⚠ %s = %d 不是合法端口号（0-65535），按默认值 %d 处理", name, port, def)
		return def
	}
	return port
}

// configPath 决定读哪个配置文件：-config 指定的，或者 exe 旁边的 scansvc.conf。
//
// 用 exe 所在目录而不是工作目录：从别的地方（快捷方式、计划任务、别的程序）
// 拉起 scansvc.exe 时，工作目录是什么全看调用方，配置得跟着 exe 走才稳。
func configPath() string {
	if v := strings.TrimSpace(*configFile); v != "" {
		return v
	}
	exe, err := os.Executable()
	if err != nil {
		return configFileName
	}
	return filepath.Join(filepath.Dir(exe), configFileName)
}

// readConfigKV 把配置文件读成键值对。
func readConfigKV(path string) (map[string]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// 记事本存 UTF-8 时会在开头加 BOM，不去掉的话第一个键名会变成 "\ufeffws-port"。
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	if !utf8.Valid(raw) {
		log.Printf("⚠ 配置文件 %s 不是 UTF-8 编码，中文路径会乱码。"+
			"用记事本打开后「另存为」，把编码选成 UTF-8 即可。", path)
	}

	kv := map[string]string{}
	for i, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}

		eq := strings.Index(line, "=")
		if eq < 0 {
			log.Printf("⚠ 配置文件第 %d 行不是 <键> = <值> 的形式，已跳过: %s", i+1, line)
			continue
		}
		key := strings.TrimSpace(line[:eq])
		if key == "" {
			log.Printf("⚠ 配置文件第 %d 行没有键名，已跳过: %s", i+1, line)
			continue
		}
		kv[key] = strings.TrimSpace(line[eq+1:])
	}
	return kv, nil
}

// writeDefaultConfig 在配置文件不存在时生成一份带注释的模板。
// 生成失败不算错——目录只读时服务照样能按默认值跑，没必要拦着不让启动。
func writeDefaultConfig(path string) {
	content := strings.ReplaceAll(configTemplate, "\n", "\r\n")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		log.Printf("⚠ 生成默认配置文件 %s 失败: %v（不影响运行，按默认值跑）", path, err)
		return
	}
	log.Printf("已生成默认配置文件: %s（可以直接用记事本编辑，改完重启生效）", path)
}

// resolver 按 命令行 > 环境变量 > 配置文件 > 默认值 的顺序取值。
type resolver struct {
	explicit map[string]bool
	kv       map[string]string
}

func (r resolver) str(name, flagVal, envKey, def string) string {
	if r.explicit[name] {
		return flagVal
	}
	if v := strings.TrimSpace(os.Getenv(envKey)); v != "" {
		return v
	}
	// 配置文件里写了空值（如 host =）也是一种表态，按它来，不退回默认
	if v, ok := r.kv[name]; ok {
		return v
	}
	return def
}

func (r resolver) num(name string, flagVal int, envKey string, def int) int {
	if r.explicit[name] {
		return flagVal
	}
	if v := strings.TrimSpace(os.Getenv(envKey)); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
		log.Printf("⚠ 环境变量 %s=%q 不是整数，已忽略", envKey, v)
	}
	if v, ok := r.kv[name]; ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
		log.Printf("⚠ 配置项 %s = %q 不是整数，按默认值 %d 处理", name, v, def)
	}
	return def
}

func (r resolver) yes(name string, flagVal bool, envKey string, def bool) bool {
	if r.explicit[name] {
		return flagVal
	}
	if v := strings.TrimSpace(os.Getenv(envKey)); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
		log.Printf("⚠ 环境变量 %s=%q 不是 true/false，已忽略", envKey, v)
	}
	if v, ok := r.kv[name]; ok && v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
		log.Printf("⚠ 配置项 %s = %q 不是 true/false，按默认值 %v 处理", name, v, def)
	}
	return def
}

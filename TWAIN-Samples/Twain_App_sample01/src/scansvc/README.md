# scansvc —— 扫描仪本地中间服务（最小闭环）

浏览器 → HTTP → Go 中间服务 → cgo → `TWAIN_APP_CMD64.dll` → TWAIN DSM → 扫描仪。

当前范围：**枚举设备 → 打开设备 → 配置参数 → 扫描 → 图片回传网页显示**，
HTTP 和 WebSocket 两条路都通。另外带一组文件 / 目录管理接口（见第 5 章），
是从既有的那个 Go 转发服务搬来的，好让这一个进程把前端要的都供上。
不含 RFID 读卡、条码打印（那两块只有 zhxserver 有，见
[TODO-rfid-zebra.md](TODO-rfid-zebra.md)）和 32 位设备支持。

**文档分工**：本文是给开发和运维看的（编译、部署、全部接口、内部设计）。
**发给客户端（业务系统前端）的是这两份**：

- [CLIENT_API.md](CLIENT_API.md) —— 对接说明：WebSocket 扫描协议、取图、转发上传、错误提示、对接约定
- [SCANNER_OPTIONS_API.md](SCANNER_OPTIONS_API.md) —— 扫描仪设置项协议

维护用的还有 [SCANNER_OPTIONS_CONFIG.md](SCANNER_OPTIONS_CONFIG.md)（设置项配置怎么写）、
[SETTINGS_API.md](SETTINGS_API.md)（直接读写 TWAIN 能力的底层接口）。

## 0. 一条命令编完（推荐）

仓库根目录的 `build.bat`：编 DLL + 编服务 + 把交付要的文件拷到 `dist\`。
**完整用法、依赖、部署和常见问题见 [BUILD.md](../../../../BUILD.md)**，下面只列常用命令。

```powershell
build.bat                  rem DLL(Release|x86) + scansvc.exe(无控制台窗口) -> dist\x86\（默认 32 位）
build.bat -x64             rem 改编 64 位，产物进 dist\x64\（同一份代码）
build.bat dll              rem 只编 DLL
build.bat go               rem 只编 scansvc.exe
build.bat -debug           rem DLL 用 Debug 配置
build.bat -console         rem exe 带控制台窗口（调试用）
build.bat -notest          rem 跳过 Go 单元测试
build.bat -out D:\deploy    rem 换个输出目录（不再追加架构子目录）
```

**什么时候要 32 位版**：TWAIN 驱动（`.ds`）要加载进本进程，位数必须一致。
只发 32 位驱动的机型（几款老柯达就是这样）在 64 位服务里根本枚举不出来。
拿不准就在目标机器上跑 `GET /api/diagnose`，`conclusion` 会直接说是不是这个原因。
两套 `dist` 不要混装：`FreeImage.dll` / `TWAINDSM.dll` 两边同名但位数不同。

它会做这些事，任一步失败就停下并说清楚原因：

1. 用 `vswhere` 找 MSBuild（找不到就提示装 VS 的 C++ 工作负载，或从"VS 开发人员命令提示"里跑）
2. 编 DLL，把 `TWAIN_APP_CMD64.dll`（`-x86` 时是 `…CMD32.dll`）/ `.lib` 拷到
   `src\scansvc\`（cgo 链接要用；链哪个由 `link_windows_amd64.go` / `link_windows_386.go` 决定）
3. 检查 Go 和 **位数匹配的** gcc（`gcc -dumpmachine` 对不上直接报错——位数装错是个老坑。
   编 32 位时优先用 PATH 里的 `i686-w64-mingw32-gcc.exe`）
4. 跑 `go test ./imgfmt ./scanopt ./twaindiag`（纯 Go 那几个包，主包要 DLL 没法在这一步测）
5. 编 `scansvc.exe`
6. 拷贝到 `dist\<架构>\`：`scansvc.exe`、`TWAIN_APP_CMD<32|64>.dll`、`FreeImage.dll`、
   `scansvc-tools.bat` / `.ps1`，并尝试从 `C:\Windows\twain_64`（32 位时 `twain_32`）
   找 `TWAINDSM.dll`；找不到会明确提示（少了它枚举不到任何扫描仪）

`dist\` 整个目录拷到操作员机器上，双击 `scansvc.exe` 即可。目标机器上已有的
`scansvc.conf`、`scans\`、`cache\` 不要删。

下面两章是手工编译的步骤，排查编译问题时看。

## 1. 先编出 DLL

用 VS 打开 `TWAIN-Samples/Twain_App_sample01/visual_studio/TWAIN_APP_VS2017.sln`：

- 配置选 **Debug | x64** 或 **Release | x64**（本分支该工程已是 `DynamicLibrary`，模块定义文件
  `..\src\exports.def` 也已配好）。
  两个配置都能编：上游代码在 `/W4` 下有不少警告，原来 Release 开着"警告视为错误"(`/WX`)，
  一编就是一串 `C2220 以下警告被视为错误`，现已关掉（Debug|x64 本来就是关的）。
  警告仍然会显示，只是不再中断编译。
- **32 位配置（Win32）现在也能用了**：补齐了 `exports.def`、`ZHX_TWAIN_EXPORTS` 宏，
  中间目录也和 x64 分开（`Debug_Win32\` / `Release_Win32\`，否则两种位数的 `.obj`
  在同一个目录里打架）。`exports.def` 里的 `LIBRARY` 语句已删掉——留着会把导入库里记的
  DLL 名钉成 `TWAIN_APP_CMD64.dll`，32 位产物（`…CMD32.dll`）运行时照那个名字去加载，只能失败。
  产物是 `TWAIN_APP_CMD32.dll`，配 `pub\external\lib\win32` 下的 FreeImage。
- 生成后把产物拷到本目录：

```
TWAIN_APP_CMD64.dll
TWAIN_APP_CMD64.lib
FreeImage.dll
TWAINDSM.dll        ← TWAIN 数据源管理器，缺了它枚举不到任何设备
```

`TWAINDSM.dll` 由 `releases/Twain_App_sample01_*/twainapp.win64.installer.msi` 安装。装完如果它在
`C:\Windows\twain_64\`，**必须拷到 exe 旁边**——代码是裸 `LoadLibraryA("TWAINDSM.dll")`，
只搜 exe 目录/系统目录/PATH，`twain_64` 不在其中。服务启动时会自检并在枚举不到设备时给出提示。

`.lib` 是链接期需要的导入库，`.dll` 和 `FreeImage.dll` 是运行期需要的。

## 2. 编译运行服务

需要 Go 1.19+ 和 **位数匹配的 MinGW-w64 gcc**（cgo 依赖）：

```powershell
cd TWAIN-Samples\Twain_App_sample01\src\scansvc
set CGO_ENABLED=1
set GOARCH=amd64
rem 32 位改成：set GOARCH=386  再加 set CC=i686-w64-mingw32-gcc

rem 交付用：没有控制台窗口，双击就缩到托盘后台运行
go build -ldflags "-H=windowsgui" -o scansvc.exe .

rem 调试用：带控制台窗口，日志直接打在屏幕上
go build -o scansvc.exe .

.\scansvc.exe
```

**`-H=windowsgui` 只影响有没有控制台窗口**，功能完全一样。加了它就没有黑窗口，日志只能看
`scansvc.log`（见 2.4 的 `log`）；不加则多一个控制台窗口，调试时看日志方便。

### 2.1 双击运行 / 托盘

双击 `scansvc.exe` 就在后台跑起来，图标缩在右下角托盘（新图标可能被折叠进「显示隐藏的图标」
里，可以拖到任务栏上常驻）。

- **右键图标**：
  - 第一行显示当前状态（`运行中: 0.0.0.0:5000  0.0.0.0:18080` 或 `已停止`）
  - 打开测试页面 / 打开日志 / 打开图片目录
  - **启动服务 / 停止服务 / 重启服务**：只开关两个端口的监听，不动扫描仪连接和缓存，
    重启后不用再等一次设备打开
  - 退出：关掉服务和 TWAIN 环境，进程结束
- **双击图标**：打开测试页面
- 端口被占用导致一个都没起来时**进程不会退出**，托盘仍在：关掉占用端口的程序后，
  右键点「启动服务」即可，不用重新双击 exe
- 监听意外断开时会弹气泡提示，并写进 `scansvc.log`
- 资源管理器崩溃重启后，托盘图标会自动加回来

不想要托盘（比如用别的方式托管进程）：`scansvc.exe -tray=false`，就是普通控制台程序。
**注意**：用 `-H=windowsgui` 编译的 exe 再加 `-tray=false`，既没窗口也没图标，只能到任务管理器里结束。

**开机自启**：用维护工具（下一节）的「开机自启设置」，它在当前用户的「启动」文件夹里放一个快捷方式。
不建议注册成 Windows 服务——服务跑在 session 0 没有桌面会话，而 TWAIN 驱动基本都要求有桌面，
多数扫描仪在服务里直接打不开。

### 2.2 维护工具（不用记命令，也不需要 curl）

`scansvc-tools.bat` + `scansvc-tools.ps1` 两个文件拷到 `scansvc.exe` 同目录，双击 bat 就有菜单：
看服务状态、列扫描仪、**导出扫描仪能力（适配新扫描仪时发给开发的那个文件）**、看设置项、
看设置项配置状态、下载内置配置。

也可以带参数直接跑：

```powershell
scansvc-tools.bat status
scansvc-tools.bat devices
scansvc-tools.bat diagnose                 # TWAIN 环境体检：某台扫描仪枚举不出来时先跑这个
scansvc-tools.bat dump "Uniscan Q400"      # 导出能力，存到 capdump\ 下
scansvc-tools.bat options "Uniscan Q400"   # 看这台设备的设置项
scansvc-tools.bat setting-ui 4             # 打开驱动自带的设置界面（按编号或设备名；关掉界面才返回）
scansvc-tools.bat config                   # 设置项配置用的是内置还是现场覆盖
scansvc-tools.bat builtin-config           # 下载内置配置，存成 scanner-options.jsonc
scansvc-tools.bat -Port 18081 status       # 服务改过 HTTP 端口
scansvc-tools.bat start                    # 启动 scansvc.exe
scansvc-tools.bat stop                     # 停止 scansvc.exe
scansvc-tools.bat autostart                # 设置 / 取消开机自启
```

活儿都在 `.ps1` 里做，走 Windows 自带的 PowerShell（3.0+），**机器上没有 curl.exe 也能用**；
端口不指定时会去读同目录的 `scansvc.conf`。下文各处的 `curl.exe ...` 命令都能换成对应的 bat 命令。

### 2.3 两个端口

服务监听两个端口，和被替换掉的那个转发服务保持一致——它本来就是一个进程开两个
（`StartWebsocket` 占 5000、`StartHttpServer` 占 18080）：

| | 默认端口 | 前端写死的地址 |
|---|---|---|
| WebSocket | **5000** | `new WebSocket("ws://127.0.0.1:5000/")` |
| HTTP | **18080** | `POST "http://127.0.0.1:18080/dir/verify"` 等 |

两个地址都编译进打包好的 js 里，改不动：

```js
new WebSocket("ws://127.0.0.1:5000/")                    // 扫描：scannerList / scan
POST "http://127.0.0.1:18080/dir/verify"                 // 文件 / 目录管理全是 HTTP
formatLocalUrl: "http://127.0.0.1:18080/file/" + encodeURIComponent(名字) + "?t=" + Date
```

所以两个默认值都不能随便改。但**两个端口供的是同一套路由**，谁也不比谁特殊——
分开只是为了对齐前端写死的那两个地址，从哪个端口调什么都通，不用记。

启动日志会把实际端口打出来：

```
WebSocket 端口已启动: ws://localhost:5000/  （既有前端写死的地址）
HTTP 端口已启动: http://localhost:18080  （既有前端写死的地址）
扫描服务就绪，图片保存于 D:\scansvc\scans
```

### 2.4 配置文件（推荐）

第一次启动时会在 **exe 旁边**自动生成 `scansvc.conf`，所有能自定义的项都在里面，
带注释，改完重启生效：

```ini
# WebSocket 端口。前端把 ws://127.0.0.1:5000/ 写死在代码里了，
# 除非端口冲突否则别改——改了前端那边也得跟着改才连得上。
# 填 0 表示不监听。
ws-port = 5000

# HTTP 端口。前端把 http://127.0.0.1:18080 写死在代码里了，同上。
http-port = 18080

# 监听地址。留空监听所有网卡；只允许本机访问就填 127.0.0.1。
host =

# 端口被占用时自动向后顺延（最多试 20 个）。
auto-port = false

# 扫描图片保存根目录。建议写绝对路径。
dir = scans

# 自动清掉扫描目录下超过 N 小时的图片，0 表示不清理。
clean-hours = 0

# 日志文件（相对 exe 目录），超过 2MB 滚动成 scansvc.log.1。留空则不写文件。
log = scansvc.log

# 托盘图标。false 时按普通控制台程序跑。
tray = true
```

几条规矩：

- **键名和命令行参数一模一样**（去掉前面的 `-`），不用记两套
- 整行以 `#` 或 `;` 开头的是注释；**行尾不支持注释**——路径里就可能带 `#` 号
- 文件存成 **UTF-8**。记事本「另存为」时可以选编码；存成 ANSI(GBK) 的话中文路径会乱码，
  服务启动时会警告。记事本加的 BOM 会自动去掉，CRLF / LF 都认
- 键名拼错（比如写成 `ws_port`）会在启动日志里点名警告，不会静悄悄地按默认值跑
- 值不合法（端口越界、该填数字填了字母）也会警告并退回默认值，不会因此起不来
- 想把配置放别处：`scansvc.exe -config D:\conf\scansvc.conf`

配置文件放 **exe 所在目录**而不是当前工作目录：从快捷方式、计划任务、别的程序拉起
`scansvc.exe` 时，工作目录是什么全看调用方，配置得跟着 exe 走才稳。

### 2.5 命令行参数与环境变量

配置文件之外，同样的东西也能用命令行或环境变量给，适合临时试一下、或者打包成服务：

| 配置项 | 命令行 | 环境变量 | 默认 |
|---|---|---|---|
| WebSocket 端口 | `-ws-port 5000` | `SCANSVC_WS_PORT` | 5000 |
| HTTP 端口 | `-http-port 18080` | `SCANSVC_HTTP_PORT` | 18080 |
| 监听地址 | `-host 127.0.0.1` | `SCANSVC_HOST` | 空（所有网卡） |
| 端口顺延 | `-auto-port` | `SCANSVC_AUTO_PORT` | false |
| 图片保存目录 | `-dir scans` | `SCANSVC_DIR` | scans |
| 自动清理 | `-clean-hours 24` | `SCANSVC_CLEAN_HOURS` | 0（不清理） |
| 日志文件 | `-log scansvc.log` | `SCANSVC_LOG` | exe 旁边的 `scansvc.log` |
| 托盘图标 | `-tray=false` | `SCANSVC_TRAY` | true |
| 配置文件路径 | `-config <路径>` | — | exe 旁边的 `scansvc.conf` |

优先级：**命令行 > 环境变量 > 配置文件 > 内置默认值**。

判的是命令行有没有**显式给过**这个参数，不是拿值和默认值比——`-http-port 0`
（显式关掉）和"没给"是两回事，拿值去比会被配置文件覆盖回去，等于关不掉。

两个端口设成同一个值时只监听一次（本来就是同一套路由，不会冲突）。

### 2.6 端口被占用

一个端口起不来不影响另一个，日志会说清楚是哪个、怎么查：

```
WebSocket 端口已启动: ws://localhost:5000/  （既有前端写死的地址）
⚠ HTTP 端口 18080 监听失败: listen tcp :18080: bind: address already in use
  端口多半被别的程序占着，可以：
    1) 查是谁占着：netstat -ano | findstr :18080
    2) 换个端口：scansvc.exe -ws-port 5001 -http-port 18081
    3) 让它自动顺延：scansvc.exe -auto-port
  注意：既有前端把文件管理接口写死成 http://127.0.0.1:18080，这个端口起不来它就调不通。
  多半是旧的转发服务还开着，关掉它再启动本服务。
```

拿 `netstat` 查出的 PID 再 `tasklist | findstr <PID>` 就能看到是哪个进程。

**18080 被占最常见的原因就是旧的转发服务还开着**。那种情况最难查：前端的文件管理接口
照样打得通（打到旧服务上了），但扫描不通——半通不通的状态。启动前先确认一下。

注意 MFC 版应用（`TWAIN_App_mfc64.exe`）自带的 HTTP 服务器占的是 8080，跟这里不冲突。

启动后浏览器打开 <http://localhost:18080> 或 <http://localhost:5000> 都能看到**内置接口测试台**
（`index.html`，随 exe 编译进去）：服务状态 → 设备列表 → 设置项（按返回值通用渲染）→
扫描（`/api/scan/start` + 轮询）→ 转发上传，底部带请求日志。
客户端对接前可以先用它把整条链路走通，它同时也是一份不依赖任何库的参考实现。
页面用的是相对路径请求，换端口不用改页面。

## 3. HTTP 接口

会话式：**连接一次，之后配置和扫描都复用这条连接**，直到显式断开。
不像早期版本那样每扫一次就开关一遍设备（开设备本身就要几秒到几十秒）。

### 设备

```
GET  /api/devices
  → {"devices":["Scanner A","Scanner B"],"count":2}
  注意：返回的是这台机器上**已安装的 TWAIN 驱动**，不是"当前连着的扫描仪"。
  装了 8 个厂商的驱动就会返回 8 条，哪怕一台设备都没插。
  TWAIN 没有便宜的在线探测：CAP_DEVICEONLINE 要先把数据源打开（state 4）才能查，
  而打不开正是这里要判断的事情。

GET  /api/diagnose
  → {"processBits":64,"dsmNextToExe":"D:\\scansvc\\TWAINDSM.dll","dsmNextToExeFound":true,
     "dsmPath":"C:\\Windows\\twain_64\\TWAINDSM.dll","dsmFound":true,
     "enumerated":["Scanner A"],"dirs":[{"path":"C:\\Windows\\twain_32","bits":32,"usable":false,
     "sources":[{"name":"KODAKS.ds",...}]},...],"conclusion":["..."]}
  "驱动装了却枚举不到"就发这个。conclusion 是可以直接给人看的结论。
  最常见的原因是位数不匹配：TWAIN 驱动（.ds）要加载进本进程，32 位进程只能用
  C:\Windows\twain_32 下的，64 位只能用 twain_64 下的，跨位数没有任何办法。
  只发 32 位驱动的机型（几款老柯达就是）在 64 位服务里一定枚举不出来，只能换 32 位版。
  实现见 twaindiag/ 子包（纯 Go，有测试）。

GET  /api/status
  → {"state":4,"stateText":"已连接扫描仪","ready":true,
     "connected":true,"device":"Scanner A","scanning":false}
  state 就是 TWAIN 状态机：0=环境没起来 3=DSM 已连(能枚举) 4=设备已打开 5+=扫描中。
  这个接口读的是状态镜像，不进 TWAIN 队列，**扫描期间也能立刻返回**。

POST /api/connect       {"device":"Scanner A"}
  → {"success":true,"status":{...}}
  打开设备并保持。已经连着别的设备会先自动断开——TWAIN 一次只允许打开一个数据源。
  重复连同一台是幂等的。

POST /api/disconnect
  → {"success":true,"status":{...}}
  关掉当前设备，回到 state 3（DSM 仍连着，还能枚举、还能开别的设备）。

POST /api/reconnect     {"deep":false}
  → {"success":true,"device":"Scanner A","deep":false,"status":{...}}
  重连当前设备。deep=true 时连整个 TWAIN 环境一起重建（zhx_Exit + zhx_Init），
  用于设备拔插、驱动崩了这种光重开数据源救不回来的情况。
```

### 参数

完整的设置 / 获取说明（字段、返回结构、错误码、取值表、已知问题）见 [SETTINGS_API.md](SETTINGS_API.md)。

```
GET  /api/config
  → {"resolution":300,"resolutionFallback":false,"applied":{"feeder":"1","pixelType":"2"}}
  只实时读分辨率，其余是本服务设置过的值的回显。原因见下面的说明。
  分辨率读不到或异常时按 300 兜底，此时 resolutionFallback 为 true。

POST /api/config        {"resolution":300,"pixelType":2,"feeder":true,
                         "autoFeed":true,"duplex":false}
  → {"success":true,"results":[
       {"field":"resolution","cap":"ICAP_XRESOLUTION+ICAP_YRESOLUTION","value":"300","ok":true},
       {"field":"duplex","cap":"CAP_DUPLEXENABLED","value":"0","ok":false,
        "error":"扫描仪拒绝了这个取值"}]}
  逐项下发、逐项返回结果。扫描仪支持哪些能力千差万别，一项不支持不该拖累其它项，
  所以顶层 success 只表示"每一项都设上了"，具体看 results。
  字段全是可选的，只传想改的那些。必须先 /api/connect —— TWAIN 的能力协商
  只在数据源打开(state 4)之后有效，没连设备返回 409。

  可选字段：resolution(DPI) pixelType(0黑白/1灰度/2彩色) feeder(用ADF)
            autoFeed(自动进纸) duplex(双面) paperSize brightness contrast

GET  /api/capability?name=0x1118
  → {"name":"0x1118","capability":[{"container":"ENUMERATION",...,
     "items":[{"index":0,"value":200.0000,...,"isCurrent":false},...]}]}
  读一项能力的原始信息，用来查这台设备到底支持哪些取值。
  name 请用编号（0x1118 即 ICAP_XRESOLUTION）：DLL 读取侧的名字表残缺且有错，
  用名字读大多拿到空数组 []，且仍是 200。详见 SETTINGS_API.md 第 7 节。

POST /api/capability    {"name":"ICAP_PIXELTYPE","value":"2"}
  → {"success":true}
  设一项 TWAIN 能力，给 /api/config 没覆盖到的场景用。
  name 可以是能力名(ICAP_PIXELTYPE)，也可以是编号(0x0101 或 257)。

POST /api/setting-ui
  → {"success":true}
  打开扫描仪驱动自带的设置面板（TWAIN 的 MSG_ENABLEDSUIONLY：只显示界面、
  由数据源自己保存参数，不传输图像）。项比 /api/config 全得多，但内容由驱动决定。
  ⚠ 请求会一直挂着直到用户关掉面板，这不是超时是设计如此——面板本身就是模态的。
  调用方要么把超时放宽，要么改用 WebSocket 的 showSetting 指令。
```

### 扫描（HTTP 异步任务，和 WebSocket 等价）

```
POST /api/scan/start   {"device":"...","extension":"jpg","count":0,"scannerOptions":{...}}   # jpg / tiff / png
  → {"success":true,"job":{"id":"...","state":"scanning","pages":[]}}
  立刻返回，扫描在后台跑。同一时刻只允许一个任务，重复发起给 409。
  count 不传或 <=0 = 扫到送纸器没纸；只扫一页要显式传 1。
  产出和 WebSocket 一致：转成 jpeg/png、平铺到扫描根目录、给 /file/<文件名> 地址。

GET  /api/scan/status[?job=<id>]
  → {"success":true,"job":{"state":"scanning|done|failed","pages":[{"page":1,"file":"...","url":"..."}],"error":""}}
  不带 job 给最近一次；只保留最近 20 次任务。实现见 scanjob.go。
```

### 扫描（同步，调试用）

```
POST /api/scan          {"count":0}
  → {"success":true,"count":1,
     "images":[{"id":"...","name":"xxx.bmp","size":11220054,"url":"/api/image?id=..."}]}
  用当前已连接的设备扫描。count 不传或 <=0 表示走送纸器一直扫到没纸，只扫一页传 1。
  可选 "device"：传了就先确保连上这台（省掉一次 /api/connect）。
  可选 "config"：扫之前顺手把参数设了，内容同 POST /api/config。
  扫完**保持连接**，可以接着扫下一批。

GET  /api/image?id=xxx  → 图片字节流
```

跨域已开（`Access-Control-Allow-Origin: *`），业务系统可以从别的域名直接调。

### 连续扫描怎么做的

**所有扫描入口默认都是扫到没纸**（旧 WebSocket 协议的 `scan`、`/api/scan`、
`/api/scan/start`）。只扫一页要显式传 `count: 1`。

一叠纸是在**一次 `MSG_ENABLEDS` 会话里**传完的，不是反复开关设备凑页数：

1. 开扫前 DLL 先谈两个能力（`prepareContinuousScan()`，main.cpp）：
   - `CAP_XFERCOUNT = -1`（不限张数）。**不设的话按驱动默认值来，有的机型默认就是 1，
     一次会话只给一张图**——这正是以前"每次只扫一张"的根源。
     指定了页数时把页数本身设进去，让驱动自己扫够了停。
   - `CAP_AUTOFEED`：读出来是 FALSE 才打开（关掉的话 ADF 扫完第一张就不再进纸）。
   - `CAP_FEEDERENABLED` **故意不动**：那是"走平板还是走送纸器"，归设置项管
     （scanopt 的 `source`），在这里覆盖会把用户选的平板扫描改掉。
2. 传输循环在 `initiateTransfer_File()` 里，按 `DAT_PENDINGXFERS` 的 `Count`
   一张接一张传，传到 0 为止。
3. 外层还有一圈兜底：驱动不认 `CAP_XFERCOUNT`、一次会话只给一张时，
   靠 `CAP_FEEDERLOADED` 判断"还有纸"再开一轮。设备不支持这个能力
   （平板扫描仪基本都不支持）就当没纸，不再开新的一轮——`checkIfMorePagesAvailable()`
   原来写死 `return true`，无限模式永远等不到结束条件会一直空转。
   无限模式另有 1000 页的兜底上限。

每页一落盘就报给上层（DLL 里的 `gPageDoneHook`，详见第 6 章"扫描回调"），所以
前端是**边扫边出图**，不是等整叠扫完才一次性收到一堆。

旧 WebSocket 协议那边还有一条：**扫描进行中又来的 `scan` 会被直接忽略**（legacy.go）。
老前端靠定时器循环发 `scan` 实现连续扫描，那些请求现在是多余的；不忽略的话它们会排在
TWAIN 线程队列里，等这叠扫完再一个个执行，每个都报"没纸"，前端会连弹好几条。

## 4. WebSocket

两套协议共存在同一个端点上，按消息里的字段自动分流——发 `handle` 的走兼容协议，
发 `cmd` 的走本服务自己的协议。端点有两个入口：`/ws`，以及根路径 `/`
（`/` 上按握手头分流：带 `Upgrade: websocket` 的走 WebSocket，其余返回演示页），
因为既有前端连的就是 `ws://127.0.0.1:5000/`。

同一时刻只保留一条连接，新连接进来会把旧的关掉——和被替换的那个 C# 服务端行为一致。

### 4.1 兼容既有前端（`legacy.go`）

对接 `court-document-processing` 的 `加工/通用` 分支。协议最要命的特点是**回显**：
服务端把收到的整个 JSON 原样带回，再补上 `code` / `data` / `message`。
`scan` 请求里的 `sort`、`id` 就是靠这个原样回去的，前端拿它们把图对应到具体档案页。

```
→ {"handle":"scannerList"}
← {"handle":"scannerList","code":0,"data":["Uniscan Q400","S8660"]}

→ {"handle":"scan","sort":3,"id":88,"scanner":"Uniscan Q400","extension":"png","show_setting":false}
← {"handle":"scan","sort":3,"id":88,"scanner":"Uniscan Q400","extension":"png",
   "show_setting":false,"code":0,
   "base64":"http://127.0.0.1:5000/file/20260910-114359-703/page1.png"}

出错：{"...原字段...","code":-1,"message":"...","msg":"..."}
```

**`base64` 字段装的是 URL，不是 base64**（字段名是历史遗留）。前端用
`base64.slice(base64.lastIndexOf('.') + 1)` 从中取扩展名，所以给的必须是
**以扩展名结尾的地址**，`/api/image?id=xxx` 那种带查询串的形式它认不了。
图片由 `/file/` 静态路由提供。

一条 `scan` 会**一直扫到送纸器没纸**，每扫出一张推一条（原来是一次只扫一页、
靠前端定时器循环发起）。扫描还没结束时又发来的 `scan` 会被忽略、不回任何消息，
所以老前端那个定时器循环不用改：纸走完之后它再发的那一次才真的开一轮扫描，
扫不到纸回 `code:-1`，正好是它停止循环的信号。

已实现：`scannerList`、`scan`、`show_setting`、`export`/`import`（前端只拿来关 loading）。

**没有实现、也不打算实现的**：`rfidRead`、`codePrintList`、`codePrint`。被替换的那个
C# 服务端把 RFID 读卡（串口读卡器）和斑马打印机（ZPL）也挂在同一条 WebSocket 上，
但那两块和 TWAIN 没有任何关系。前端的 `PrintTags/`、`searchRfid/`、`boxManager/`、
`warehouseManager/` 用得到它们——**要是那些功能还在用，就不能直接拿本服务顶替
整个 zhxserver**（两者端口都是 5000，没法并存）。收到这三个指令会回一句明确的说明。

`show_setting:true` 是前端"控制面板"按钮，打开扫描仪驱动自带的设置界面。
顺序照搬旧服务端：先打开设备，再弹面板。**成功时不回任何响应**——回了的话前端会把它
当成一条扫描结果去读 `data['base64']`，那是 undefined，紧接着的 `.slice()` 直接抛异常；
前端本来就靠自己的定时器复位 loading。失败才回 `code:-1`。

`getScannerOptions` / `setScannerOptions` / `scan` 的 `scannerOptions` 是一套固定的**设置项协议**：
服务端把扫描仪能力描述成 `{key,label,control,value,choices}`，前端照着画控件、把 `{key: 取值}` 发回来，
TWAIN 细节全在服务端。**有哪些设置项、下拉框有哪些选项都写在声明式配置 `scanopt/default_options.jsonc` 里**
（编译进 exe，增删改不写 Go 代码；`scanopt/testdata/devices` 下是已适配设备的回归用例），
写法见 [SCANNER_OPTIONS_CONFIG.md](SCANNER_OPTIONS_CONFIG.md)。现场应急可以放 `scanner-options.jsonc` 覆盖。
给前端的文档：[SCANNER_OPTIONS_API.md](SCANNER_OPTIONS_API.md)。
`getScannerOptions` **不会打开设备**：连着的现读，没连着的给上次读到的缓存（exe 旁边 `cache\`）。

`scan` 扫出 0 页时，`message` 会说明原因：扫描仪未连接/未开机、送纸器没纸、卡纸、重张、
盖板未合上、被其他程序占用、数据源状态异常（需重连）。依据是 DLL 的 `zhx_GetScanDiagnosis`
（`MSG_ENABLEDS` 失败时的 condition code + `CAP_FEEDERLOADED` + `CAP_DEVICEONLINE`），
`twain.log` 里对应一行 `Scan produced no pages. diagnosis: ...`。

### 4.2 本服务自己的协议（`protocol.go`）

和 HTTP 接口一一对应，多了边扫边推：

```
→ {"id":"7","cmd":"scan","params":{"count":0}}
← {"cmd":"scan.progress","data":{"page":1,"image":{...}}}
← {"cmd":"scan.progress","data":{"page":2,"image":{...}}}
← {"id":"7","cmd":"scan","success":true,"data":{"count":2,"images":[...]}}
```

指令：`status` `devices` `connect` `disconnect` `reconnect` `config` `getConfig`
`capability` `setCapability` `showSetting` `scan`。

### 4.3 图片格式

DLL 只会吐 BMP——`zhx_Scan` 的兜底路径靠 `*.bmp` 通配符比对扫描前后的目录来认产物，
换格式那条路就认不出来了（每页回调的钩子给的是完整路径，不受这个限制，但兜底还得能用）。
所以转换放在 Go 侧：**转成什么只看请求里的 `extension`，源文件是什么只看内容**
（`imgfmt.Sniff` 认 BMP / JPEG / PNG / TIFF 的文件头），不看源文件的后缀——后缀谁都能改，内容才是事实。
转完删掉原图（一张十几 MB，连扫几百页很快吃满盘）。

几种情况：

| 源文件 | `extension` | 结果 |
|---|---|---|
| BMP（正常情况） | jpg / tiff / png | 解码后按目标格式重新编码 |
| 已经是目标格式（驱动直接出了 JPEG） | 同格式 | **只改后缀，不重新编码**（JPEG 重编会掉画质） |
| 不是目标格式又解不开 | 任意 | 保留原文件并记日志，不让格式转换挡住扫描 |
| `extension` 不认识（空、bmp…） | — | 原样保留。扩展名必须和实际内容一致，前端要拿它报给后端 |

支持三种：

| 格式 | 实现 |
|---|---|
| `jpg` | 标准库，质量 85，另外补一段 JFIF APP0 把 DPI 写进去（Go 的编码器根本不写这段） |
| `tiff` | **自己写的 TIFF + LZW**，见下 |
| `png` | 标准库，早期默认值，留着兼容。同样补一块 pHYs 写 DPI（Go 的编码器也不写） |

**DPI 从哪来、坏了怎么办**：服务默认走原生传输，DLL 拿到驱动给的 DIB 后，先用这一页
`DAT_IMAGEINFO` 的分辨率回填 BMP 头里的 `biX/YPelsPerMeter` 再落盘——驱动自己往 DIB 头填的值
不可靠（有填 0 的、填 96/72 默认值的、按页不稳定的），不能直接用。取值顺序是
**IMAGEINFO → DIB 头 → 300**；Go 侧转格式时再按 `imgfmt.ResolveDPI` 过一遍，
可信范围 50~9600，只有一边可信时另一边跟它一样，都不可信写 300。
用了兜底会在 `twain.log`（`@WARN Page N: IMAGEINFO resolution unusable…`）和
`scansvc.log`（`警告: … DPI 缺失或异常…`）各留一笔。

转 TIFF 时会**一并存一张同名 JPEG** 当预览（浏览器显示不了 TIFF），前端取预览加 `?thumbnail=1`。

**为什么 TIFF 要自己写**：标准库没有 TIFF 编码器；`golang.org/x/image/tiff` 只支持
"不压缩"和 Deflate，**没有 LZW**；扫描仪那边也指望不上（Q400 的 `ICAP_COMPRESSION`
只有 无压缩 / JPEG / G4）。而档案数字化要的就是 TIFF + LZW。

代码在 `imgfmt/` 包（纯 Go，不碰 cgo，所以 `go test ./imgfmt` 在 Linux 上也能跑）：
`bmp.go` 解码（按原始位深还原：1 位黑白 / 8 位灰度 / 24 位彩色，不再一律转 RGBA，
否则黑白图存成 TIFF 会从几十 KB 变几十 MB）、`lzw.go` TIFF 版 LZW、`tiff.go` 容器。

⚠️ TIFF 的 LZW 和标准库的 `compress/lzw` **不是一回事**：码长要提前一位加宽
（x/image 的注释直接把它叫 "off by one"）。差这一位，别人的解码器读出来就是
`lzw: invalid code`。改这块务必跑测试。

BMP 解码是自己写的，没用 `golang.org/x/image/bmp`：那个包的新版本要求 Go 1.23+，
引进来会把 `go.mod` 的版本要求抬上去。支持 1/4/8/24/32 位未压缩 BMP。

## 5. 文件 / 目录管理接口（从既有转发服务搬来）

这一组接口是把既有的那个 Go 转发服务（`filemanager` 仓库的 `扫描工具` 分支）照搬过来的，
路径、表单字段、错误码全部对齐，目的是让 scansvc 一个进程顶掉它，前端不用改一行。
实现在 `files.go` + `fileutil.go`，两个文件都不碰 TWAIN。

和 `/api/*` 那组的区别，别混：

| | `/api/*` | 本组（`/version`、`/file/*`、`/dir/*`） |
|---|---|---|
| 请求体 | JSON | 表单（multipart 或 urlencoded 都认） |
| 响应体 | `{"success":true,...}` | `{"errorCode":0,"msg":"","data":...}` |
| HTTP 状态码 | 按语义给 4xx/5xx | **永远 200**，成败看 `errorCode` |

错误码沿用原服务的编号：`1` 通用失败 / `2` 备份目录失败 / `3` 保存上传文件失败 /
`4` 路径不存在 / `5` 选中的是文件而不是目录。

### 5.1 前端连的是哪个端口

| 前端调用 | 地址 | 谁来应答 |
|---|---|---|
| `new WebSocket(...)` | `ws://127.0.0.1:5000/` | `legacy.go`，指令 `scannerList` / `scan` |
| `dirVerify` / `dirChildren` / `dirOpen` / `dirUpload` | `http://127.0.0.1:18080/dir/*` | `files.go` |
| `restore` / `upload` | `http://127.0.0.1:18080/file/*` | `files.go` |
| `formatLocalUrl`（显示图片） | `http://127.0.0.1:18080/file/<编码过的相对路径>?t=` | `files.go` |

两个端口是同一套路由，所以上表的地址换成任意一个端口都通。

**注意有两个不同的前端，别混**：

- `court-document-processing`（部署在 80 端口，日常用的那个）只调两样东西：
  WebSocket 的 `scannerList` / `scan`，和 `http://127.0.0.1:18080/file/upload`
  （`src/api/base.js:128`，`localUpload=true` 时走本机转发上传）。**它不从 18080 取页面。**
- filemanager 内嵌的那个"数字化加工系统"页面才是 `/dir/verify`、`/dir/children`、
  `/file/restore` 这些接口的调用方。那个页面没有搬进来（见 5.4）。

也就是说第 5 章这一整套接口里，日常那个前端只用得到 `/file/upload` 一个。
其余的是为了顶替 filemanager 而搬的，用不上也不碍事。

`formatLocalUrl` 用的是 `encodeURIComponent`，会把路径分隔符也编进去
（`/` → `%2F`、Windows 相对路径的 `\` → `%5C`）。两种都认：`%5C` 直接命中，
`%2F` 会被 Go 的 ServeMux 规范化成一次 301，浏览器自动跟随，对前端透明。

### 5.2 工作目录

原服务里有个全局可变的 "temp path"，一切文件操作都以它为根。本服务照搬这个概念，
叫**工作目录**：启动时等于扫描保存目录（`-dir`），前端调 `/dir/verify` 选中档案目录后切过去。

`GET /file/<相对路径>` 找不到文件时，会**回头再到扫描目录下找一遍**。因为工作目录被切走以后，
之前扫出来的图（URL 形如 `/file/SCAN_20260911_153000_123_000001N.png`）还得能打开，
`POST /file/upload` 也按同样的顺序找。

扫描图**平铺在扫描目录根下，不建子目录**。前端上传时只取图片 URL 的最后一段当 `filename`
（`url.substring(url.lastIndexOf('/') + 1)`），原服务的图本来就平铺在 temp 根目录，所以没问题；
放进子目录的话，子目录那一段会被前端丢掉，上传报"找不到文件"。DLL 仍然先写进一个临时的
时间戳子目录（它靠目录里新增的 .bmp 识别产物），每页转换完就挪到根目录，重名时加 `_1`、`_2` 后缀。

### 5.3 接口清单

```
GET  /version
  → 文本：scansvc v2.1-scansvc 工作目录: D:\档案 进程目录: D:\scansvc

GET  /file/<相对路径>[?thumbnail=1]
  → 文件内容，带 Cache-Control: max-age=7200
  thumbnail=1 时先找同名的 .jpeg（加工流程另外生成的缩略图），没有才回原图。

POST /file/restore          name=<相对路径>
  从 <工作目录>_备份 把原图复制回来，覆盖加工过的那份。

POST /file/upload           filename=<相对路径>&server=<业务系统地址>&<其余字段原样透传>
     header: token
  → 业务系统的响应原样转回
  浏览器传不了本地磁盘上的图，所以由本服务读出文件、以 multipart（文件字段名 image）
  发给业务系统。这就是"转发"二字的由来。

POST /file/temp/path/update path=<目录>
  直接切工作目录，不备份也不列文件。

POST /dir/verify            dir=<目录>&filenameIgnorePattern=<正则>&filenameOnlyPattern=<正则>
  → data: ["001.png", "第一卷\002.png", ...]
  前端进加工流程的第一步：整目录备份到 <目录>_备份（已存在就不动，否则第二次进来会拿
  加工结果盖掉原图），递归列出文件，并把工作目录切到这里。
  ignore 命中就跳过；ignore 为空时才看 only，后者反过来——没命中的才跳过。

POST /dir/children          dir=<目录>&filenameIgnorePattern=<正则>
  → data: {"folders":[...], "files":[...], "dir":"..."}
  只看一层，给目录树用。不动工作目录。

POST /dir/open              dir=<目录>
  用资源管理器打开它。

POST /dir/upload            dir=<目录>&filepath=<相对路径>&file=<文件>
  → data: {"path":"落盘的绝对路径"}
  不带 file 也算成功，只把目标路径回一遍——前端对没动过的图不会重复上传。
```

### 5.4 没有搬过来的部分

- **fsnotify 监听目录**。原服务自己不驱动扫描仪，是让厂商驱动把图写进 temp 目录，
  再靠文件系统事件发现新图、推给前端。scansvc 直接通过 TWAIN 拿图（`legacy.go`），
  不需要靠猜，那套监听连同"请设置驱动输出为 png/jpeg"的提示一起没搬。
- **激活码 / 机器码校验**（`middlewares/TokenVerify.go`、`utils/Keychain.go`）。
  scansvc 是本机扫描服务，不上锁。
- **filemanager 内嵌的那个前端 SPA**（"数字化加工系统"页面，4.5 MB）。
  日常用的前端是 `court-document-processing`，部署在 80 端口，页面不从本服务取，
  搬过来纯属多余。`/` 上是 scansvc 自己的演示页，撇开前端单独试扫描时用。
- **每小时清 temp 的定时任务**（`modules/crontab.go`）。它在原服务里本来就是注释掉的：
  它清的是"当前工作目录"，而工作目录会被 `/dir/verify` 切到用户的档案目录去，
  真跑起来就是在删用户的档案。这里保留能力但只清扫描目录，且默认关闭，见 `-clean-hours`。

### 5.5 和原服务刻意不一致的地方

- **路径穿越**。原服务对 `name` / `filepath` 这类前端传来的相对路径完全不校验，
  一个 `../..` 就能读写到目录外面。这里统一过 `safeJoin()` 收敛回根目录内。
- **`/dir/children` 不传忽略规则时返回空**。原服务对空正则没判空，而空正则匹配任何字符串，
  结果是目录和文件全被过滤掉。这里补了判空。
- **`/dir/open` 在 Windows 上根本打不开**。原服务是 `exec.Command("cmd /c start", uri)`，
  把 `"cmd /c start"` 整个当成可执行文件名。这里拆成了正确的参数形式。
- **转发上传的错误处理**。原服务对网络错误一律忽略，`resp` 为 nil 时直接 panic 掉整个进程；
  而且它把业务系统的响应 `Unmarshal` 成 map 再重新序列化，19 位的档案 id 会被当成 float64，
  回到前端变成 `1.2345678901234568e+18`。这里改成错误如实回报、响应体**字节透传**。
- **`/file/temp/path/update` 加了目录存在校验**。原服务不校验就直接设，
  设成一个不存在的目录后，之后每次读文件都会莫名其妙地 404。

有一处**明知有问题也照搬了**：`/file/restore` 在备份里找不到原图时，原服务只填 `msg`、
`errorCode` 仍是 0，前端会当成还原成功。前端的判断逻辑就建在这个行为上，改了反而出事。

## 6. 设计要点

**TWAIN 单线程模型**是整个服务的地基。DLL 里的 `EnableDS()` 会在调用线程上跑 `GetMessage` 消息泵等待数据源事件，而数据源把事件投递到"打开它的那条线程"的消息队列。所以：

- `twain.go` 启动时开一条**专属 goroutine**，`runtime.LockOSThread()` 后**永不解锁**，独占一条 OS 线程
- 所有 TWAIN 调用通过 `inTwain()` 投递到这条线程**串行执行**
- HTTP handler 里**绝不能**直接调 `C.zhx_*`

扫描期间该线程被占满，其它请求会排队等待——这是 TWAIN 的固有限制，不是 bug。

**扫描回调**（`callback.go` 的 `goScanCallback`）由 DLL 在扫描线程上同步调用，只做文件路径登记和
一次非阻塞推送，不做重活。

它是**每传完一页就回调一次**的：DLL 侧在 `TwainApp` 的传输循环里装了个钩子
（`gPageDoneHook`，见 TwainApp.h），每页落盘就报一次。原来只能等
`initiateTransfer_*` 把整叠纸传完返回之后，再比对目录里多出来的文件才知道扫了几页——
一叠 50 页要等全部扫完才一次性收到 50 条，前端只能盯着空白界面等。
目录比对没删，退化成兜底（多页 TIFF 一个文件，只能等传输结束）；
钩子报过的文件名 DLL 记着，比对时跳过，不会重复报。

**代价**：回调是在传输循环里同步执行的，格式转换那几百毫秒会让驱动等着。
换来的是边扫边出图。真嫌慢就把转换挪出 TWAIN 线程，但要自己保证页序和原图不被提前删掉。

**状态镜像**。扫描期间 TWAIN 线程被占满，任何走 `inTwain()` 的调用都得排队等扫描结束，
而 `/api/status` 恰恰是扫描时最需要能立刻回答的接口。所以每次 TWAIN 操作结束时
把 `zhx_GetState()` / `zhx_GetCurrentDevice()` 的结果刷进一份镜像（`twain.go` 的 `mirrorState`），
`/api/status` 只读镜像。**不要**为了"更准"把它改成走 `inTwain()`——那样扫描期间就查不了状态了。

**设置面板必须自己泵消息**。`zhx_ShowSettingUI()` 发完 `MSG_ENABLEDSUIONLY` 之后要跑
`GetMessage`/`DispatchMessage` 循环，直到数据源发来 `MSG_CLOSEDSREQ`(取消) 或
`MSG_CLOSEDSOK`(确定)。原因是那个对话框是数据源在**调用线程**上创建的，本进程是控制台程序、
没有天然的消息循环，不泵它就是死的——点不动也关不掉，一直耗到驱动内部超时。
被替换的那个 C# 服务端不需要操心这个，它跑在 WinForms 的 UI 线程上，消息泵是现成的。

**扫完不要调 `zhx_EndScan()`**。名字像是"结束本次扫描"，实际上它内部会 `unloadDS()`
把设备关掉、退回 state 3。会话模型下每次扫描后调它，等于每扫一批就断一次连接，
下一批又要重新打开设备（几秒到几十秒）。真正要断开时用 `zhx_CloseDevice()`。

## 7. 枚举不到 / 打不开设备时怎么查

### 7.0 设备列表里根本没有这台扫描仪

先跑体检：`scansvc-tools.bat diagnose`（或 `GET /api/diagnose`），`conclusion` 直接给结论。
按出现频率：

1. **驱动只有另一个位数**——TWAIN 驱动（`.ds`）要加载进本服务进程，位数必须一致：
   32 位服务只认 `C:\Windows\twain_32`，64 位只认 `twain_64`。厂商只发 32 位驱动的机型
   （几款老柯达就是这样）在 64 位服务里**一定**枚举不出来，换 32 位版服务（`build.bat`，默认就是 32 位）。
   体检会点名是哪几个驱动文件。
2. **`TWAINDSM.dll` 不在 exe 旁边**——少了它一台都枚举不到，体检会报缺失。
3. **驱动装了但枚举中途报错**——`twain.log` 里找 `MSG_GETNEXT failed`。
   一台报错不再中断整张清单（`TwainApp::getSources()`，以前一报错就 `return`，
   排在它后面的扫描仪会全部"消失"），跳过它继续问下一台，连续失败 3 次才放弃。
   日志里每台设备都记了厂商、TWAIN 协议版本、`SupportedGroups`，
   带 `(no DG_IMAGE!)` 的说明那个数据源根本不提供图像能力。

### 7.1 设备列在列表里，但打不开

症状：`/api/scan` 卡二三十秒，然后报打开失败，`twain.log` 里是

```
Error: Failed to open data source, condition code = 23 (TWCC_CHECKDEVICEONLINE)
```

**`TWCC_CHECKDEVICEONLINE` 字面是"设备不在线"，但设备被别的程序占用时报的也是它**
（2026-09-10 实测：设备接着、开着机，被另一个程序占用，就是这个码，MSG_OPENDS 卡 24 秒）。
所以别一上来就去查线和电源，按这个顺序：

1. **被占用**——厂商自带的扫描工具、扫描仪托盘程序、上一个没退干净的 scansvc
   （`tasklist | findstr /i scansvc`，8000/8010 端口被占用就是它还活着的信号）
2. **型号选错**——设备列表是驱动列表，选了一台没插的设备，驱动会去找它直到超时
3. **设备本身**——开机、连线、休眠、面板是否就绪

`MSG_OPENDS` 的耗时和条件码都在 `twain.log` 里，卡二三十秒基本都是驱动在等一台够不着的设备。

另一种是**设备回"忙"**，`twain.log` 里是

```
DSM_Entry return code: 10 TWRC_BUSY (MSG_OPENDS took 3.2 s)
Error: Failed to open data source, TWRC_BUSY (device busy / locked, ...)
```

`TWRC_BUSY`（10）/ `TWRC_SCANNERLOCKED`（11）是 TWAIN 2.4 新增的返回码，不是 `TWRC_FAILURE`，
**不带 condition code**——旧版日志在这里会写 `condition code = 0 (TWCC_SUCCESS)`，别被它误导。
2026-09-23 实测 Kodak S2000w 每次都是 3.2 秒后回 `TWRC_BUSY`：驱动联系上了设备，设备说"忙"。
服务会隔 2 秒重试 2 次（DLL 导出 `zhx_GetLastOpenResult` 取返回码），仍然忙就报"扫描仪忙，打不开"，
不会去重建 TWAIN 环境。排查顺序：

1. **被别的主机或程序连着**——网络款同一时间只接受一台主机，看设备面板上显示的主机名
2. **面板上有待处理的提示**——卡纸、盖板、错误码，或正在唤醒
3. **上一次会话没释放**——比如扫描卡住后强退了服务（见 7.2），把扫描仪断电重启

### 7.2 能打开，但一扫就卡住

症状：设备打开、读能力都正常，`scan` 发出去就没有下文。`twain.log` 停在

```
Data source enabled successfully (TWRC_SUCCESS)
@INFO Waiting for MSG_XFERREADY from the data source (callbacks: yes)
@INFO Still waiting for MSG_XFERREADY, 30 s so far
```

启用成功之后，驱动要回调一个 `MSG_XFERREADY` 才开始传图。2026-09-23 实测 Kodak S2000w 会从
**它自己的工作线程**回调，原来的等待循环阻塞在 `GetMessage` 上，线程消息队列里没有东西就永远
醒不过来——而 TWAIN 线程被占住后，设备列表、设置、再次扫描全部在队列里排到服务重启。现在：

- 回调里会给等待线程投一条 `WM_NULL` 叫醒它，日志里是
  `DS callback: MSG 0x0101 on thread A (waiting thread B)`（A≠B 就是这种驱动）；
- 等待改成每秒醒一次，**2 分钟**还没等到就放弃这次扫描、关掉数据源回到 state 4，
  日志 `@ERROR No MSG_XFERREADY after 120 s`，客户端收到"扫描未产出任何图片"，服务继续可用。

2026-09-23 加了 `@DIAG` 日志后再测 S2000w：启用成功、`CAP_FEEDERLOADED=1`、`CAP_DEVICEONLINE=1`，
2 分钟里**回调 0 次、本线程消息 0 条**。当时 `MSG_OPENDSM` / `MSG_ENABLEDS` 的父窗口都是
`GetDesktopWindow()`，它属于别的进程（日志里 `hParent=... owned by ... pid` 能看出来），
TWAINDSM 或驱动往父窗口投的消息本线程根本收不到。现在：

- 父窗口改成 TWAIN 线程上自建的隐藏窗口（`getTwainParentWindow`），日志 `Created hidden TWAIN parent window`；
- 等待期间每秒拿一条合成的 `WM_NULL` 主动调一次 `MSG_PROCESSEVENT`，DSM 手里有待领的消息就能取到，
  日志 `poll PROCESSEVENT -> TWRC_DSEVENT`；数据源消息不管从哪条路来都会记
  `Data source message 0x0101 received via ...`；
- 超时单独诊断（`zhx_GetScanDiagnosis` 的 `enableFailed=2`），送纸器状态取等待期间读到的值——
  S2000w 停用后再读会报没纸，以前因此误报"送纸器里没有纸"。

还是超时的话，说明驱动根本没发通知：看纸是否放到位、驱动有没有弹出等人点的窗口（界面扫描关着，
弹窗会没人管）、面板上有没有要按的键。

## 8. 已知限制 / 下一步

- **扫描仪设置相关的待办**（DLL 读能力的 bug、接口不一致、getScannerOptions 的遗留）汇总在
  [TODO-settings.md](TODO-settings.md)，代码里标了 `TODO(TODO-settings.md #编号)`。

- **RFID 读卡和条码打印没有搬**，前端 `searchRfid/`、`boxManager/`、`warehouseManager/`、
  `list/` 这几处在用它们，目前只能靠 zhxserver。而 zhxserver 也监听 5000
  （`zhxserver/Socket/MySocket.cs:39`），和本服务抢端口，两者只能二选一——
  所以"用 scansvc 顶掉 zhxserver"要等这两块搬完。摸底见 [TODO-rfid-zebra.md](TODO-rfid-zebra.md)。

- `zhx_GetDevicesList()` 返回的是 DLL 用 `_strdup` 分配的内存，而 DLL 静态链接了自己的 CRT，Go 侧 `C.free` 会跨堆释放导致崩溃，所以当前**不释放**（每次枚举泄漏一小段）。修法是在 C 侧补一个 `zhx_FreeString` 导出。
- 扫描是同步阻塞的，多页扫描时 HTTP 可能超时。下一步改成"提交任务返回 taskId + WebSocket 推进度"。
- `images` 登记表只在内存里，服务重启后旧图片取不回来。
- `zhx_Init()` 里父窗口用的是 `GetDesktopWindow()`，`zhx_Scan` 的消息泵也用它。
  TWAIN 要求应用传自己的窗口句柄，数据源拿它当模态框的 owner 并向该窗口所属线程投消息。
  控制台程序压根没有窗口，DS 弹的框就成了没人处理的孤儿窗口。目前没发现它导致具体故障，
  但这是个真隐患，正解是在 TWAIN 线程上建一个 `HWND_MESSAGE` 隐藏窗口顶上去。

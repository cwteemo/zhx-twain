# scansvc —— 扫描仪本地中间服务（最小闭环）

浏览器 → HTTP → Go 中间服务 → cgo → `TWAIN_APP_CMD64.dll` → TWAIN DSM → 扫描仪。

当前范围：**枚举设备 → 打开设备 → 扫描 → 图片回传网页显示**。
不含能力配置（分辨率/纸张/色彩）、WebSocket 推送、32 位设备支持——留到下一阶段。

## 1. 先编出 DLL

用 VS 打开 `TWAIN-Samples/Twain_App_sample01/visual_studio/TWAIN_APP_VS2017.sln`：

- 配置选 **Debug | x64**（本分支该工程已是 `DynamicLibrary`，模块定义文件 `..\src\exports.def` 也已配好）
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

需要 Go 1.19+ 和 **64 位 MinGW-w64 gcc**（cgo 依赖）：

```powershell
cd TWAIN-Samples\Twain_App_sample01\src\scansvc
set CGO_ENABLED=1
set GOARCH=amd64
go build -o scansvc.exe .
.\scansvc.exe
```

参数：

- `-port 5000`            监听端口（默认 5000，等价于 `-addr :5000`）
- `-addr 127.0.0.1:8010`  监听地址，想限制只允许本机访问时用；只写端口号（`-addr 8010`）也认
- `-auto-port`            端口被占用时自动向后顺延（最多试 20 个），实际端口看启动日志
- `-dir scans`            图片保存根目录（每次扫描一个时间戳子目录）

端口也可以用环境变量给，方便打包成服务/快捷方式：

```powershell
set SCANSVC_PORT=8010
rem 或者 set SCANSVC_ADDR=127.0.0.1:8010
.\scansvc.exe
```

优先级：`-addr` > `-port` > `SCANSVC_ADDR` > `SCANSVC_PORT` > 默认 `:5000`。

**默认端口是 5000**，因为既有前端把 `ws://127.0.0.1:5000/` 写死在代码里，
本服务是去替换那个中间服务的，端口对不上前端连都连不上。

**8000 端口被占用**时不再直接崩，日志会打出占用提示和排查命令：

```
监听 :8000 失败: listen tcp :8000: bind: address already in use
端口多半已被别的程序占用，可以：
  1) 换个端口启动：scansvc.exe -port 8010
  2) 让它自动顺延：scansvc.exe -auto-port
  3) 查是谁占着：netstat -ano | findstr :8000
```

拿 `netstat` 查出的 PID 再 `tasklist | findstr <PID>` 就能看到是哪个进程。注意 MFC 版应用
（`TWAIN_App_mfc64.exe`）自带的 HTTP 服务器占的是 8080，跟这里不冲突。

启动后浏览器按日志里打印的实际端口打开（默认 <http://localhost:8000>）即可点按钮跑通闭环。
内置演示页用的是相对路径请求，换端口不用改页面。

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

```
GET  /api/config
  → {"resolution":300,"applied":{"feeder":"1","pixelType":"2"}}
  只实时读分辨率，其余是本服务设置过的值的回显。原因见下面的说明。

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

GET  /api/capability?name=ICAP_XRESOLUTION
  → {"name":"ICAP_XRESOLUTION","capability":{"container":"ENUMERATION",...,"items":[100,200,300]}}
  读一项能力的原始信息，用来查这台设备到底支持哪些取值。
  ⚠ DLL 为了读能力会把数据源临时 enable 到 state 5，个别设备会因此空走一次纸或者亮灯。
  正因如此 GET /api/config 没有顺带调它。

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

### 扫描

```
POST /api/scan          {"count":1}
  → {"success":true,"count":1,
     "images":[{"id":"...","name":"xxx.bmp","size":11220054,"url":"/api/image?id=..."}]}
  用当前已连接的设备扫描。count=0 表示走送纸器一直扫到没纸。
  可选 "device"：传了就先确保连上这台（省掉一次 /api/connect）。
  可选 "config"：扫之前顺手把参数设了，内容同 POST /api/config。
  扫完**保持连接**，可以接着扫下一批。

GET  /api/image?id=xxx  → 图片字节流
```

跨域已开（`Access-Control-Allow-Origin: *`），业务系统可以从别的域名直接调。

### 连续扫描怎么开

1. `POST /api/config` 带 `{"feeder":true,"autoFeed":true}`——先让设备走送纸器
2. `POST /api/scan` 带 `{"count":0}`——一直扫到 `CAP_FEEDERLOADED` 报没纸为止

设备不支持 `CAP_FEEDERLOADED`（平板扫描仪基本都不支持）时，`count=0` 只会扫一页就停。
这是有意的：DLL 里 `checkIfMorePagesAvailable()` 原来写死 `return true`，
无限模式永远等不到结束条件，会一直空转。现在查不到送纸器状态就当作没纸了。
无限模式另有 1000 页的兜底上限。

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

一次 `scan` 只扫一页——连续扫描由前端自己的定时器循环发起，和被替换的服务端一致。

已实现：`scannerList`、`scan`、`show_setting`、`export`/`import`（前端只拿来关 loading）。

`show_setting:true` 是前端"控制面板"按钮，打开扫描仪驱动自带的设置界面。
顺序照搬旧服务端：先打开设备，再弹面板。**成功时不回任何响应**——回了的话前端会把它
当成一条扫描结果去读 `data['base64']`，那是 undefined，紧接着的 `.slice()` 直接抛异常；
前端本来就靠自己的定时器复位 loading。失败才回 `code:-1`。

`getScannerOptions` 目前返回空数组：它的选项模型（`{"option":136,"name":"flip-side-rotation",
"type":"str-list","list":[...]}`）是 `option` 编号 + 短横线命名的另一套体系，
不是 TWAIN 的 CAP，参照实现不在手上。返回空数组前端只是参数面板空着，不报错。

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

DLL 只会吐 BMP——`zhx_Scan` 靠 `*.bmp` 通配符比对扫描前后的目录来认产物，换格式就认不出来。
所以转换放在 Go 侧（`imageconv.go`）：按请求里的 `extension` 转成 png/jpg，转完删掉原 BMP
（一张十几 MB，连扫几百页很快吃满盘）。不认识的格式原样保留 BMP——扩展名必须和实际内容一致，
前端要拿它报给后端。

BMP 解码是自己写的，没用 `golang.org/x/image/bmp`：那个包的新版本要求 Go 1.23+，
引进来会把 `go.mod` 的版本要求抬上去。支持 1/4/8/24/32 位未压缩 BMP。

## 5. 设计要点

**TWAIN 单线程模型**是整个服务的地基。DLL 里的 `EnableDS()` 会在调用线程上跑 `GetMessage` 消息泵等待数据源事件，而数据源把事件投递到"打开它的那条线程"的消息队列。所以：

- `twain.go` 启动时开一条**专属 goroutine**，`runtime.LockOSThread()` 后**永不解锁**，独占一条 OS 线程
- 所有 TWAIN 调用通过 `inTwain()` 投递到这条线程**串行执行**
- HTTP handler 里**绝不能**直接调 `C.zhx_*`

扫描期间该线程被占满，其它请求会排队等待——这是 TWAIN 的固有限制，不是 bug。

**扫描回调**（`callback.go` 的 `goScanCallback`）由 DLL 在扫描线程上同步调用，只做文件路径登记，不做重活。

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

## 6. 打不开设备时怎么查

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

## 7. 已知限制 / 下一步

- `zhx_GetDevicesList()` 返回的是 DLL 用 `_strdup` 分配的内存，而 DLL 静态链接了自己的 CRT，Go 侧 `C.free` 会跨堆释放导致崩溃，所以当前**不释放**（每次枚举泄漏一小段）。修法是在 C 侧补一个 `zhx_FreeString` 导出。
- 图片按 BMP 原样回传，单张约 11 MB。后续应在服务端转 JPEG/PNG 再传。
- 扫描是同步阻塞的，多页扫描时 HTTP 可能超时。下一步改成"提交任务返回 taskId + WebSocket 推进度"。
- `images` 登记表只在内存里，服务重启后旧图片取不回来。
- `GET /api/capability` 会让 DLL 把数据源临时 enable 到 state 5 才能读能力，个别设备
  会因此空走一次纸。想干净地读能力，得在 C 侧补一条 state 4 就能查的实现。
- `zhx_Init()` 里父窗口用的是 `GetDesktopWindow()`，`zhx_Scan` 的消息泵也用它。
  TWAIN 要求应用传自己的窗口句柄，数据源拿它当模态框的 owner 并向该窗口所属线程投消息。
  控制台程序压根没有窗口，DS 弹的框就成了没人处理的孤儿窗口。目前没发现它导致具体故障，
  但这是个真隐患，正解是在 TWAIN 线程上建一个 `HWND_MESSAGE` 隐藏窗口顶上去。

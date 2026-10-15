# scansvc 接口文档（协议契约）

本文是 scansvc 对外暴露的**全部接口**的权威参考：端口、路由、WebSocket 指令、请求 / 响应字段、
状态码、错误码、图片地址规则。以代码为准整理（`main.go` / `serve.go` / `ws.go` / `protocol.go` /
`legacy.go` / `scanjob.go` / `options.go` / `files.go` / `capdump.go` / `diagnose.go` / `twain.go`）。

**本文同时是 TWAIN 版（Windows，本仓库）和 SANE 版（Linux，`/code/scansvc-sane`）共同遵守的契约**，
两边入参出参一致，前端不需要知道连的是哪个平台。仅有的差异集中列在[第 8 章](#8-跨平台twain-版与-sane-版)。

其它文档的分工：

| 文档 | 读者 | 内容 |
|---|---|---|
| **API.md（本文）** | 前后端开发、测试 | 全部接口的完整参考 |
| [CLIENT_API.md](CLIENT_API.md) | 业务系统前端 | 对接说明、最小示例、自检清单（是本文的子集 + 对接建议） |
| [SCANNER_OPTIONS_API.md](SCANNER_OPTIONS_API.md) | 业务系统前端 | 设置项协议详解与渲染参考 |
| [SETTINGS_API.md](SETTINGS_API.md) | 开发 / 排查 | TWAIN 能力底层读写（`/api/config`、`/api/capability`）的细节（仅 TWAIN 版仓库有） |
| [SCANNER_OPTIONS_CONFIG.md](SCANNER_OPTIONS_CONFIG.md) | 服务端维护 | 设置项配置文件怎么写 |
| [README.md](README.md) | 开发 / 运维 | 编译、部署、内部设计、排障 |

> 仓库里另有两个**早期**的 C++ HTTP 服务器（DLL 里的 `src/http_server.cpp`，文档
> `HTTP_API_DOCUMENTATION.md`；MFC 应用里的 `visual_studio_mfc/http_server.cpp`），都监听 8080，
> 已不再使用，和本文无关。

## 目录

- [0. 概览](#0-概览)
- [1. WebSocket 兼容协议（handle）](#1-websocket-兼容协议handle)
- [2. WebSocket 本服务协议（cmd）](#2-websocket-本服务协议cmd)
- [3. HTTP /api/*](#3-http-api)
- [4. 设置项协议](#4-设置项协议)
- [5. 文件 / 目录接口](#5-文件--目录接口)
- [6. 图片格式与地址规则](#6-图片格式与地址规则)
- [7. 错误提示对照](#7-错误提示对照)
- [8. 跨平台：TWAIN 版与 SANE 版](#8-跨平台twain-版与-sane-版)
- [9. 已知不一致 / 遗留](#9-已知不一致--遗留)

---

## 0. 概览

### 0.1 端口

| | 默认端口 | 前端写死的地址 | 配置项 |
|---|---|---|---|
| WebSocket | **5000** | `ws://127.0.0.1:5000/` | `ws-port` / `SCANSVC_WS_PORT` |
| HTTP | **18080** | `http://127.0.0.1:18080` | `http-port` / `SCANSVC_HTTP_PORT` |

- 两个端口供的是**同一套路由**，任何接口在哪个端口上都能调；分成两个只是为了对齐既有前端写死的地址。
- 两个端口配成同一个值时只监听一次。端口填 `0` 表示不监听。
- WebSocket 端点有两个入口：`/ws`，以及根路径 `/`（按握手头分流：带 `Upgrade: websocket` 的走
  WebSocket，其余返回内置接口测试台页面）。

### 0.2 跨域

所有 HTTP 响应带：

```
Access-Control-Allow-Origin: *
Access-Control-Allow-Methods: GET, POST, PUT, DELETE, OPTIONS
Access-Control-Allow-Headers: Authorization, Content-Type, Content-Length, X-CSRF-Token, Token, token, DIR, session
Access-Control-Max-Age: 86400
```

`OPTIONS` 预检直接回 `204`。WebSocket 不校验 Origin。

### 0.3 三种响应风格（别混）

| 接口组 | 请求体 | 响应体 | HTTP 状态码 |
|---|---|---|---|
| `/api/*` | JSON | 成功 `{"success":true,...}`（个别接口直接是数据对象），失败 `{"success":false,"error":"中文原因"}` | 按语义：400 / 404 / 405 / 409 / 500 |
| `/version`、`/file/*`、`/dir/*` | 表单（multipart 或 urlencoded） | `{"errorCode":0,"msg":"","data":...}` | **永远 200**（405 除外），成败看 `errorCode` |
| WebSocket `handle` 协议 | JSON | **请求原样回显** + `code`（0 成功 / -1 失败）+ `data` / `msg` / `message` | — |
| WebSocket `cmd` 协议 | JSON | `{"id","cmd","success","data"}` / `{"id","cmd","success":false,"error"}` | — |

### 0.4 状态

`state` 是扫描仪会话状态，所有平台语义相同：

| state | 含义 | ready | connected |
|---|---|---|---|
| 0 | 扫描环境没起来 | false | false |
| 2 | DSM 已加载未打开（仅 TWAIN 版可能出现，过渡态） | false | false |
| 3 | 环境就绪，可以枚举设备，未连接扫描仪 | true | false |
| 4 | 已连接（打开）某台扫描仪 | true | true |
| ≥5 | 扫描进行中 | true | true |

`scanning` 是服务侧的"有扫描或设置面板在占用"标记，比 `state>=5` 覆盖更宽（含排队等待的时间）。

### 0.5 会话模型与串行

- **连接一次，之后配置和扫描都复用这条连接**，直到显式断开或切换设备。切换设备时自动断开旧的。
- 所有扫描仪操作在一条专属线程上**串行执行**；扫描期间其它操作排队。`/api/status`（及 WS `status`）
  读的是状态镜像，扫描期间也立即返回。
- WebSocket **同一时刻只保留一条连接**，新连接接入会关掉旧的。

---

## 1. WebSocket 兼容协议（handle）

对接既有前端（`court-document-processing` 加工/通用分支）。消息带 `handle` 字段即走本协议。

### 1.1 通用规则

- 服务端把**整个请求对象原样带回**，再补字段：`code`（`0` 成功 / `-1` 失败）、`data`、失败时的
  `msg` 与 `message`（内容相同，中文，可直接显示）。请求里的自定义字段（`sort`、`id`…）原样回来。
- 消息不是合法 JSON 时回 `{"cmd":"error","success":false,"error":"消息不是合法 JSON: ..."}`（无 `handle`、无 `code`）。
- 服务端每 60 秒发 ping；90 秒没有 pong 断开。单条入站消息上限 1 MB。

### 1.2 scannerList —— 扫描仪列表

```json
→ {"handle":"scannerList"}
← {"handle":"scannerList","code":0,"data":["Uniscan Q400","KODAK Scanner: S2000"]}
```

- `data` 是字符串数组，没有设备时是 `[]`。
- 列表是**已安装的驱动**，不代表设备在线。
- 扫描环境没就绪（`ready=false`）时 `code:-1`，`msg` 说明原因。

### 1.3 scan —— 扫描

```json
→ {"handle":"scan",
   "scanner":"Uniscan Q400",       // 必填
   "extension":"jpg",              // jpg|jpeg / tiff|tif / png；不传默认 png；其它值保留原始格式
   "show_setting":false,           // true：只弹驱动自带设置面板，不扫描（见 1.4）
   "scannerOptions":{"dpi":300},   // 可选，设置项 key -> 取值，见第 4 章
   "sort":3, "id":88                // 任意自定义字段，原样回显
  }
```

**每扫出一张推一条**，图片地址在 `base64` 字段（字段名是历史遗留，装的是 URL）：

```json
← {"handle":"scan","scanner":"Uniscan Q400","extension":"jpg","show_setting":false,
   "scannerOptions":{"dpi":300},"sort":3,"id":88,
   "code":0,
   "base64":"http://127.0.0.1:18080/file/SCAN_20260916_095614_861_000001N.jpg"}
```

行为要点：

| 情况 | 行为 |
|---|---|
| 正常 | 先连接设备 → 下发 `scannerOptions` → **一直扫到送纸器没纸**（忽略 count，固定扫到空），每页推一条 `code:0` |
| 平板（非送纸器） | 扫一页结束 |
| 扫描进行中又收到 `scan`（且不是 `show_setting:true`） | **直接忽略，不回任何消息** |
| 缺 `scanner` | `code:-1`，`未指定扫描仪` |
| `scannerOptions` 不是对象 | `code:-1`，`scannerOptions 必须是对象，形如 {"dpi":300}` |
| `scannerOptions` 有一项设不上（不含 skipped） | `code:-1`，`扫描设置未生效，已取消扫描。分辨率：该扫描仪不支持 250 dpi`，**不扫描** |
| 打开设备失败 / 0 页 | 一条 `code:-1`，`msg` 见第 7 章 |
| 中途某页格式处理失败 | 该页不推，只记日志，其余页照常 |
| 这台设备第一次扫描完成（此前没有设置项缓存） | 扫完后额外**主动推一条** `getScannerOptions`（见 1.6） |

`base64` 地址的规则见第 6 章：一定以扩展名结尾、`/file/` 后只有文件名、主机名取 WS 握手时的 Host、端口是 HTTP 端口。

### 1.4 scan + show_setting:true —— 驱动自带设置面板

```json
→ {"handle":"scan","scanner":"Uniscan Q400","show_setting":true}
```

- 先连接设备，再打开驱动自带的设置界面，阻塞到用户关闭。
- **成功不回任何消息**（回 `code:0` 会被前端当成扫描结果去读 `base64` 而抛异常）。失败回 `code:-1`。
- 不处理 `scannerOptions`；扫描进行中也不会被忽略。
- SANE 版没有驱动界面，固定回 `code:-1`，见第 8 章。

### 1.5 getScannerOptions —— 取设置项

```json
→ {"handle":"getScannerOptions","scanner":"Uniscan Q400"}
← {"handle":"getScannerOptions","scanner":"Uniscan Q400","code":0,"data":{
     "version":1,"scanner":"Uniscan Q400","connected":false,"cached":true,
     "updatedAt":"2026-09-15 14:30:12","options":[ ...见第 4 章... ]}}
```

- **绝不打开设备**。请求的正是当前连着的设备 → 现读（`connected:true`）；否则给上次读到的缓存
  （`cached:true`）；从没连过 → `options: []`，`connected/cached` 都是 false。
- `scanner` 不传时用当前连着的设备。
- 永远 `code:0`，`data` 永远是对象。

### 1.6 服务端主动推送的 getScannerOptions

某台设备第一次扫描完成后推一条（`scanner` 可能不是前端当前选中的那台）：

```json
← {"handle":"getScannerOptions","scanner":"Uniscan Q400","code":0,"data":{ ...同 1.5... }}
```

### 1.7 setScannerOptions —— 下发设置项

```json
→ {"handle":"setScannerOptions","scanner":"Uniscan Q400",
   "scannerOptions":{"colorMode":"gray","dpi":300}}
← {"handle":"setScannerOptions", ...原字段..., "code":0,"data":{
     "results":[{"key":"colorMode","value":"gray","ok":true},{"key":"dpi","value":300,"ok":true}],
     "options":{ ...同 1.5 的 data，下发后现读... }}}
```

- **会打开设备**。缺 `scanner` → `未指定扫描仪`；连接失败 → `code:-1`。
- 有项失败：`code:-1`，`msg` 形如 `部分设置未生效。分辨率：该扫描仪不支持 250 dpi`，`data` 照样带回。
- `results` 结构见 4.4。

### 1.8 其它指令

| 指令 | 行为 |
|---|---|
| `{"handle":"dumpCapabilities","scanner":"设备名"}` | 连接设备并导出全部能力，`data` 同 `GET /api/capabilities/dump`。排查用 |
| `export` / `import` | 直接回 `code:0`（老前端拿它关 loading） |
| `rfidRead` / `codePrintList` / `codePrint` | `code:-1`，`本服务只提供扫描功能，<指令> 属于 RFID / 条码打印，这部分仍需原来的服务端（zhxserver）` |
| 其它 | `code:-1`，`本服务未实现的指令: <指令>` |

---

## 2. WebSocket 本服务协议（cmd）

和 HTTP 接口一一对应，多了扫描边扫边推。消息**不带** `handle` 即走本协议。

```
请求：{"id":"任意串","cmd":"指令","params":{...}}       id 原样带回，可不传；params 可省略
成功：{"id":"...","cmd":"指令","success":true,"data":{...}}   data 为空时省略
失败：{"id":"...","cmd":"指令","success":false,"error":"中文原因"}
推送：{"cmd":"scan.progress","success":true,"data":{...}}     无 id
```

`params` 解析失败回 `params 解析失败: ...`；未知指令回 `未知指令 "x"（支持：...）`。

| cmd | params | 成功 data | 对应 HTTP |
|---|---|---|---|
| `status` | — | ScannerStatus（见 3.2） | `GET /api/status` |
| `devices` | — | `{"devices":[...],"count":n}` | `GET /api/devices` |
| `connect` | `{"device":"..."}` | ScannerStatus | `POST /api/connect` |
| `disconnect` | — | ScannerStatus | `POST /api/disconnect` |
| `reconnect` | `{"deep":false}` | `{"device":"...","status":{...}}` | `POST /api/reconnect` |
| `dumpCapabilities` | `{"device":"..."}` | 能力导出（见 3.7） | `GET /api/capabilities/dump` |
| `getConfig` | — | `{"resolution":300,"resolutionFallback":false,"applied":{...}}` | `GET /api/config` |
| `config` | ScanConfig（见 3.4） | `{"results":[...]}`（**没有顶层 success 判断，看 results[].ok**） | `POST /api/config` |
| `capability` | `{"name":"0x1118"}` | `{"name":"...","capability":[...]}` | `GET /api/capability` |
| `setCapability` | `{"name":"...","value":"字符串"}` | 无 data | `POST /api/capability` |
| `showSetting` | — | 无 data，用户关掉面板才回 | `POST /api/setting-ui` |
| `scan` | `{"device":"","count":0,"config":{...}}` | `{"count":n,"images":[ImageInfo...]}` | `POST /api/scan` |

`scan` 的推送与结果：

```json
→ {"id":"7","cmd":"scan","params":{"count":0}}
← {"cmd":"scan.progress","success":true,"data":{"page":1,"image":{"id":"...","name":"xxx.bmp","size":11220054,"url":"/api/image?id=..."}}}
← {"id":"7","cmd":"scan","success":true,"data":{"count":1,"images":[{...}]}}
```

> `cmd` 协议的 `scan` 和 `POST /api/scan` 一样，产出是**驱动原始格式（TWAIN 版为 BMP）**、
> 地址是 `/api/image?id=`，不支持 `extension` / `scannerOptions`。业务前端请用 handle 协议的 `scan`
> 或 HTTP 的 `/api/scan/start`。`count` 不传或 ≤0 = 扫到没纸。`config` 单项失败会被忽略照常开扫。

---

## 3. HTTP /api/*

通用：请求体 JSON，允许空体；体不是合法 JSON → `400 请求体不是合法 JSON: ...`；
方法不对 → `405 只支持 POST`（或 `只支持 GET / POST`）。未列方法限制的接口任何方法都行。

### 3.1 路由一览

| 方法 | 路径 | 用途 |
|---|---|---|
| GET | `/` | 内置接口测试台（带 Upgrade 头时是 WebSocket） |
| GET | `/favicon.ico` | 图标 |
| GET | `/api/devices` | 设备列表 |
| GET | `/api/diagnose` | 扫描环境体检 |
| GET | `/api/status` | 当前状态（不排队） |
| POST | `/api/connect` | 连接设备并保持 |
| POST | `/api/disconnect` | 断开 |
| POST | `/api/reconnect` | 重连 |
| GET / POST | `/api/config` | 读 / 设常用扫描参数（底层接口） |
| GET / POST | `/api/capability` | 读 / 设单项原始能力（底层接口） |
| GET | `/api/capabilities/dump` | 导出设备全部能力 |
| GET / POST | `/api/scanner-options` | 读 / 下发设置项 |
| GET | `/api/scanner-options/config` | 设置项配置状态 |
| GET | `/api/scanner-options/config/builtin` | 下载内置设置项配置原文 |
| POST | `/api/setting-ui` | 打开驱动自带设置面板 |
| POST | `/api/scan/start` | 发起异步扫描任务（**推荐**） |
| GET | `/api/scan/status` | 查扫描任务 |
| POST | `/api/scan` | 同步扫描（调试用） |
| GET | `/api/image` | 取 `/api/scan` 的产物 |
| GET | `/ws` | WebSocket |

### 3.2 设备与连接

**GET /api/devices**

```json
← 200 {"devices":["Uniscan Q400","S8660"],"count":2}
```

**GET /api/status**

```json
← 200 {"state":4,"stateText":"已连接扫描仪","ready":true,"connected":true,"device":"Uniscan Q400","scanning":false}
```

ScannerStatus 字段：`state`(int) `stateText`(string，平台相关文案) `ready`(state≥3) `connected`(state≥4)
`device`(当前设备名，没有为空串) `scanning`(bool)。

**POST /api/connect** `{"device":"Uniscan Q400"}`

```json
← 200 {"success":true,"status":{ScannerStatus}}
```

| 状态码 | 情况 |
|---|---|
| 400 | `device 不能为空` / JSON 错 |
| 500 | 打开失败（设备忙、被占用、不在线…，`error` 带排查提示） |

已连着同一台是幂等的；连着别的会先断开。

**POST /api/disconnect** → `200 {"success":true,"status":{...}}`，回到 state 3。

**POST /api/reconnect** `{"deep":false}`

```json
← 200 {"success":true,"device":"Uniscan Q400","deep":false,"status":{...}}
```

`deep:true` 连扫描环境一起重建。当前没连设备 → `500 当前没有连接任何扫描仪，无法重连（先调 /api/connect）`；重开失败 → 500。

**GET /api/diagnose** → `200`，JSON 对象，**必有 `conclusion`（字符串数组，可直接给人看）**，其余字段平台相关（见第 8 章）。
TWAIN 版字段：`processArch` `processBits` `windowsDir` `dsmNextToExe` `dsmNextToExeFound` `dsmPath` `dsmFound`
`enumerated` `dirs[{path,exists,bits,usable,sources[{name,path,size}],readErr}]` `conclusion`。

### 3.3 扫描（推荐：异步任务）

**POST /api/scan/start**

```json
→ {"device":"Uniscan Q400",        // 可选；不传用当前已连接的设备
   "extension":"jpg",              // jpg|jpeg / tiff|tif / png，默认 png
   "count":0,                      // 不传或 <=0 = 扫到没纸；只扫一页传 1
   "scannerOptions":{"dpi":300}}   // 可选，同第 4 章
← 200 {"success":true,"job":{"id":"1758...-1","device":"Uniscan Q400","state":"scanning",
                             "startedAt":"2026-09-16 10:20:31","pages":[]}}
```

| 状态码 | 情况 |
|---|---|
| 400 | JSON 错；没传 `device` 且当前没连设备（`没有连接扫描仪，请在 device 里指定设备名`） |
| 409 | 上一个 HTTP 任务还在跑；或 `scannerOptions` 有项设不上——此时响应带 `results`，**不开扫**；或下发设置时未连接 |
| 500 | 打开设备失败 |

**GET /api/scan/status[?job=<id>]**（不带 job 给最近一次）

```json
← 200 {"success":true,"job":{
     "id":"1758...-1","device":"Uniscan Q400",
     "state":"done",                    // scanning / done / failed
     "error":"",                        // failed 时为中文原因（omitempty）
     "startedAt":"2026-09-16 10:20:31","endedAt":"2026-09-16 10:20:48",
     "pages":[{"page":1,"file":"SCAN_..._000001N.jpg","url":"http://127.0.0.1:18080/file/SCAN_..._000001N.jpg"}]}}
```

- 404：`还没有发起过扫描` / `没有这个扫描任务（可能已过期）`（只保留最近 20 个任务）。
- `pages` 边扫边增长；`file` 就是 `/file/upload` 的 `filename`。

**POST /api/scan**（同步，调试用，产出原始格式）

```json
→ {"device":"","count":0,"config":{ScanConfig}}
← 200 {"success":true,"count":1,"images":[{"id":"...","name":"xxx.bmp","size":11220054,"url":"/api/image?id=..."}]}
```

400 JSON 错 / 409 `config` 时未连接 / 500 打开失败或扫描失败。

**GET /api/image?id=** → 图片字节流；id 不存在 `404 图片不存在或已过期`（登记表只在内存，重启即失效）。

### 3.4 常用参数（底层）

**GET /api/config**

```json
← 200 {"resolution":300,"resolutionFallback":false,"applied":{"pixelType":"2","feeder":"1"}}
```

只实时读分辨率（读不到或不在 50~9600 时按 300 并置 `resolutionFallback:true`），`applied` 是本服务设置成功过的值的回显。未连接 → 409。

**POST /api/config** —— ScanConfig，字段全可选：

| 字段 | 类型 | 含义 | TWAIN 能力 |
|---|---|---|---|
| `resolution` | int | DPI | ICAP_XRESOLUTION+ICAP_YRESOLUTION |
| `pixelType` | int | 0 黑白 / 1 灰度 / 2 彩色 | ICAP_PIXELTYPE |
| `feeder` | bool | 用送纸器 | CAP_FEEDERENABLED |
| `autoFeed` | bool | 自动进纸 | CAP_AUTOFEED |
| `duplex` | bool | 双面 | CAP_DUPLEXENABLED |
| `paperSize` | int | 纸张编号（TWSS_*） | ICAP_SUPPORTEDSIZES |
| `brightness` | int | 亮度 | ICAP_BRIGHTNESS |
| `contrast` | int | 对比度 | ICAP_CONTRAST |

```json
← 200 {"success":false,"results":[
     {"field":"resolution","cap":"ICAP_XRESOLUTION+ICAP_YRESOLUTION","value":"300","ok":true},
     {"field":"duplex","cap":"CAP_DUPLEXENABLED","value":"0","ok":false,"error":"扫描仪拒绝了这个取值"}]}
```

顶层 `success` = 每一项都 ok。未连接 → 409。`results[].error` 文案见 [SETTINGS_API.md 第 5 节](SETTINGS_API.md)（TWAIN 版仓库）。

### 3.5 单项原始能力（底层）

**GET /api/capability?name=0x1118**

```json
← 200 {"name":"0x1118","capability":[{"container":"ENUMERATION","itemType":7,"currentIndex":2,"defaultIndex":2,
        "items":[{"index":0,"value":200.0000,"isCurrent":false}, ...]}]}
```

- `capability` 是数组，读不到时为 `[]`（仍是 200）。元素按 `container` 取字段：
  `ONEVALUE`（`value`）/ `ENUMERATION`（`items[{index,value,isCurrent,isDefault}]`、`numItems`、`currentIndex`、`defaultIndex`）/
  `RANGE`（`minValue` `maxValue` `stepSize` `currentValue` `defaultValue`）/ `ARRAY`（`items`）。
- `name` 用编号最可靠（`0x1118` 或十进制）。缺 name → 400；未连接 → 409。

**POST /api/capability** `{"name":"ICAP_PIXELTYPE","value":"2"}` → `200 {"success":true}`。
`value` 必须是字符串；缺 name → 400；未连接或设备拒绝 → **500**。

### 3.6 驱动设置面板

**POST /api/setting-ui** → `200 {"success":true}`（用户关掉面板后才返回，请放宽超时）。
未连接 / 驱动不提供界面 → 409。SANE 版固定 409，见第 8 章。

### 3.7 能力导出（排查用）

**GET /api/capabilities/dump?device=<设备名>**（device 可省，用当前设备）

```json
← 200 {"device":"...","time":"...","service":"v2.1-scansvc","supportedCapsReported":true,
       "caps":[{"code":"0x0101","name":"ICAP_PIXELTYPE","raw":[{...}]}],
       "unreadable":["0x1234"],"options":[...设置项...],"file":"<存盘路径>"}
```

同时存到程序旁的 `capdump/<设备名>_<时间>.json`。连接失败或未连接 → 409。结构平台相关，仅供排查。

### 3.8 设置项

**GET /api/scanner-options?device=<设备名>** → `200`，data 同 1.5（不打开设备）。

**POST /api/scanner-options**

```json
→ {"device":"Uniscan Q400","scannerOptions":{"dpi":300,"colorMode":"gray"}}
← 200 {"success":true,"error":"","results":[...],"options":{...同 1.5 的 data...}}
```

- `device` 可省（用当前设备）；传了会先连接，失败 → 500。没连设备 → `409 尚未连接扫描仪`。
- 有项失败时仍是 **200**，`success:false`、`error` 是拼好的中文（`分辨率：该扫描仪不支持 250 dpi`）。

**GET /api/scanner-options/config**

```json
← 200 {"source":"builtin","hint":"使用内置配置，正常状态","overrideFile":"...\\scanner-options.jsonc",
       "overridePresent":false,"ok":true,"error":"","loadedAt":"...","options":["source","colorMode",...],"profiles":[]}
```

**GET /api/scanner-options/config/builtin** → `text/plain`，内置配置原文（CRLF）。

---

## 4. 设置项协议

详解与前端渲染参考见 [SCANNER_OPTIONS_API.md](SCANNER_OPTIONS_API.md)，这里列全字段。

### 4.1 data（getScannerOptions / GET /api/scanner-options）

| 字段 | 类型 | 说明 |
|---|---|---|
| `version` | int | 协议版本，当前 `1` |
| `scanner` | string | 设备名 |
| `connected` | bool | 设备当前连着、本次是现读 |
| `cached` | bool | 设备没连，给的是缓存 |
| `updatedAt` | string | options 从设备读取的时间（`2006-01-02 15:04:05`），没有时省略 |
| `options` | Option[] | 永不为 null |

### 4.2 Option

| 字段 | 类型 | 说明 |
|---|---|---|
| `key` | string | 标识，发布后不改名 |
| `label` | string | 中文名 |
| `group` | string | `basic` / `advanced` |
| `control` | string | `select` / `radio` / `switch` / `slider` / `number`；不认识的跳过 |
| `value` | any | 当前值；对不上任何选项时为 `null` |
| `choices` | `[{value,label}]` | select / radio 才有 |
| `min` `max` `step` | number | slider 必有，number 可能有 |
| `unit` | string | 可选，如 `dpi` |

### 4.3 当前 key（version 1）

| key | label | group | 常见 control | 取值 |
|---|---|---|---|---|
| `source` | 扫描方式 | basic | select | `flatbed` / `adf` / `adfDuplex` |
| `colorMode` | 颜色模式 | basic | radio | `bw` / `gray` / `color` |
| `dpi` | 分辨率 | basic | select | 数字（75~1200 中设备支持的档位） |
| `paperSize` | 纸张尺寸 | basic | select | `none` `maxSize` `a3` `a4` `a5` `a6` `jisB4` `jisB5` `isoB4` `isoB5` `letter` `legal` `businessCard` …；未收录的为 `code<编号>` |
| `brightness` | 亮度 | basic | slider | 数字，范围由设备给 |
| `contrast` | 对比度 | basic | slider | 数字 |
| `autoCrop` | 自动裁切 | advanced | switch | bool |
| `autoDeskew` | 自动纠偏 | advanced | switch | bool |
| `autoRotate` | 自动旋转 | advanced | switch | bool |
| `autoColor` | 自动识别彩色 | advanced | switch | bool |
| `blankPage` | 空白页 | advanced | radio | `keep` / `discard` |
| `rotate` | 旋转 | advanced | select | `0` `90` `180` `270` |
| `threshold` | 黑白阈值 | advanced | slider | 数字 |

设备不支持的项、不支持的选项不输出。数值类 control 随设备而变（区间→slider，固定档→select，单值→number）。

### 4.4 下发：scannerOptions 与 results

请求：`"scannerOptions": {"key": 取值, ...}`，只传要改的；数字 / 布尔也接受字符串形式（`"300"`、`"true"`）。
按配置顺序逐项下发，每项下发前现读。

`results[]`：

| 字段 | 说明 |
|---|---|
| `key` / `value` | 前端发来的 |
| `ok` | 是否成功 |
| `skipped` | true = 这台设备没有这项 / key 不认识，已忽略，**不算失败** |
| `error` | 失败或忽略原因：`该扫描仪不支持 250 dpi`、`300 超出范围 50~200`、`不认识的取值 "x"`、`必须是数字，收到 "x"`、`该扫描仪没有这一项`、`不认识的设置项`、`扫描仪拒绝了这个设置（ICAP_… = …：…）` 等 |

---

## 5. 文件 / 目录接口

从既有 Go 转发服务（filemanager「扫描工具」分支）照搬，路径、字段、错误码对齐。
响应统一 `{"errorCode":0,"msg":"","data":...}`，**HTTP 永远 200**（方法不对时 405 + `/api` 风格错误体）。

| errorCode | 含义 |
|---|---|
| 0 | 成功 |
| 1 | 通用失败（还原、转发上传、打开目录） |
| 2 | 备份目录失败 |
| 3 | 保存上传文件失败 |
| 4 | 路径不存在 / 缺参数 |
| 5 | 选中的是文件而不是目录 |

**工作目录**：启动时等于扫描保存目录，`/dir/verify` 或 `/file/temp/path/update` 会切换它。
`/file/<相对路径>` 和 `/file/upload` 先在工作目录找，找不到再去扫描目录找。所有相对路径都经过防穿越校验。

| 方法 | 路径 | 入参 | 成功 data / 返回 |
|---|---|---|---|
| GET | `/version` | — | 纯文本：`scansvc v2.1-scansvc 工作目录: <dir> 进程目录: <cwd>`（SANE 版是 `v2.1-scansvc-sane`） |
| GET | `/file/<相对路径>[?thumbnail=1]` | — | 文件内容，`Cache-Control: max-age=7200`；`thumbnail=1` 优先同名 `.jpeg`；找不到 404 |
| POST | `/file/restore` | `name` | `null`。从 `<工作目录>_备份` 复制回来。**备份里找不到时 errorCode 仍为 0，只填 msg**（照搬原服务） |
| POST | `/file/upload` | `filename`、`server`、其余字段透传；header `token` | 业务系统响应**字节透传**；失败 errorCode 1 |
| POST | `/file/temp/path/update` | `path` | `{"path":"..."}`；不存在 4，是文件 5 |
| POST | `/dir/verify` | `dir`、`filenameIgnorePattern`、`filenameOnlyPattern` | `["001.png","第一卷\\002.png",...]`；首次会备份到 `<dir>_备份`（失败 2），并切换工作目录 |
| POST | `/dir/children` | `dir`、`filenameIgnorePattern` | `{"folders":[...],"files":[...],"dir":"..."}`；不切工作目录 |
| POST | `/dir/open` | `dir` | `{"path":""}`；用系统文件管理器打开（失败 1） |
| POST | `/dir/upload` | `dir`、`filepath`、`file`（可省） | `{"path":"<落盘绝对路径>"}`；不带 file 也成功；filepath 无效 3 |

`dir` / `path` 会剥掉 `file://` 前缀。`dir` 缺失或不存在 → 4，是文件 → 5。

**转发上传**：服务从磁盘读出 `filename` 对应的文件，以 multipart 发到 `server`，**文件字段名 `image`**，
除 `filename`/`server` 外的表单字段原样带上，`token` 头带上，超时 10 分钟。业务系统返回非 JSON →
errorCode 1，`上传接口返回的不是 JSON（HTTP 状态）: 原文`。

---

## 6. 图片格式与地址规则

### 6.1 extension

| 取值 | 结果 |
|---|---|
| `jpg` / `jpeg` | JPEG，质量 85，写 JFIF 密度（DPI）；扩展名统一为 `.jpg` |
| `tiff` / `tif` | TIFF + LZW，黑白 1 位 / 灰度 8 位 / 彩色 24 位，写 X/YResolution；**另存同名 `.jpeg` 预览** |
| `png` | PNG，写 pHYs（DPI） |
| 不传 | 按 `png`（handle 协议、`/api/scan/start` 都是） |
| 其它（`bmp`…） | 不转换，保留驱动原始输出（TWAIN 版为 BMP） |

- 源文件格式按内容识别；已是目标格式只改后缀不重编码；解码失败保留原文件，不挡扫描。
- **DPI**：取这一页设备报告的分辨率，缺失或不在 50~9600 时按 **300** 兜底并在日志警告。

### 6.2 地址

- handle `scan` 的 `base64`、`/api/scan/start` 的 `pages[].url`：
  `http://<主机>:<HTTP端口>/file/<文件名>`
  - 主机名取客户端请求时用的 Host（连 `127.0.0.1` 就回 `127.0.0.1`），端口是 HTTP 端口实际监听值（没起来时用 WS 端口）。
  - **必定以扩展名结尾**，`/file/` 后**只有文件名**，没有子目录（扫描图平铺在扫描目录根下，重名加 `_1`、`_2`）。
  - 文件名形如 `SCAN_20260916_095614_861_000001N.jpg`，前端不要依赖其格式。
- TIFF 预览：`<地址>?thumbnail=1`。
- `cmd` 协议 / `POST /api/scan` 的 `url` 是相对地址 `/api/image?id=...`。

---

## 7. 错误提示对照

所有 `msg` / `error` 都是可直接显示的中文，**前端原样显示，不要按关键词替换**。常见的：

| 场景 | 文案（开头） |
|---|---|
| 设备离线 | `扫描失败：扫描仪未连接或未开机（请检查电源、USB 线，或是否被其他扫描程序占用）` |
| 没纸 | `扫描失败：送纸器里没有纸，请放纸后重试` |
| 卡纸 | `扫描失败：卡纸，请清除卡纸后重试` |
| 重张 | `扫描失败：检测到重张进纸，请整理纸张后重试` |
| 盖板 | `扫描失败：扫描仪盖板或送纸器未合上` |
| 状态异常 | `扫描失败：扫描仪状态异常（数据源未正常复位），请断开重连扫描仪后重试` |
| 被占用 | `扫描失败：扫描仪被其他程序占用，请关闭厂商扫描工具等程序后重试` |
| 2 分钟未开始送纸 | `扫描失败：扫描仪启动后 2 分钟一直没有开始送纸…` |
| 启动失败 | `扫描失败：扫描仪启动失败（… condition code N）…` |
| 0 页其它 | `扫描未产出任何图片（检查是否放纸、盖板是否合上，详见 twain.log）` |
| 打开失败 | `打开扫描仪失败: <设备>（按这个顺序查：…）` |
| 设备忙 | `扫描仪忙，打不开: <设备>（驱动返回 TWRC_BUSY…）` |
| 设置不生效（扫描） | `扫描设置未生效，已取消扫描。<label>：<原因>；…` |
| 设置不生效（setScannerOptions） | `部分设置未生效。<label>：<原因>；…` |
| 未连接 | `尚未连接扫描仪，先调 /api/connect` / `尚未连接扫描仪` |
| 并发扫描 | `上一次扫描还没结束，等它结束再发起（GET /api/scan/status 查进度）` |

---

## 8. 跨平台：TWAIN 版与 SANE 版

| | TWAIN 版 | SANE 版 |
|---|---|---|
| 平台 | Windows | Linux |
| 代码 | 本仓库 `src/scansvc`（cgo → `TWAIN_APP_CMD*.dll` → TWAINDSM） | `/code/scansvc-sane`（Go → libsane） |

**本文档是两个实现共同遵守的契约。** 以下部分两边**完全一致**：

- 端口（5000 / 18080）、监听与 CORS 规则、配置项（`scansvc.conf` / 命令行 / 环境变量）；
- 全部 HTTP 路由、方法、请求字段、响应字段、HTTP 状态码；
- 全部 WebSocket 指令（handle 与 cmd 两套）、回显规则、推送时机、忽略重复 scan 等边界行为；
- 设置项协议：`version`、全部 `key`、取值（`bw/gray/color`、`flatbed/adf/adfDuplex`、`a4`…）、`results` 结构；
- 图片格式、文件命名与平铺、`/file/` 地址规则、DPI 兜底；
- `/file/*`、`/dir/*` 的字段与 `errorCode`；扫描失败等中文提示的措辞。

**仅有的差异**：

- **`stateText` 文案**不同（TWAIN 版"TWAIN 环境未初始化"，SANE 版"SANE 环境未初始化"等），`state` 数值语义相同：
  0 未初始化 / 3 可枚举 / 4 已连接 / 5 扫描中。前端只应判断 `state` / `ready` / `connected` / `scanning`。
- **驱动自带设置界面**（`POST /api/setting-ui`、WS `showSetting`、legacy `show_setting:true`）：SANE 没有驱动 UI，
  SANE 版固定返回失败（HTTP 409 / `success:false` / `code:-1`），msg 提示改用设置项面板。
- **`GET /api/diagnose`**：两边都返回 JSON 且都有 `conclusion` 字符串数组；其余字段平台相关（TWAIN 版是
  DSM / 位数 / twain_32|64 目录，SANE 版是 SANE 版本、后端、设备权限等），前端只应展示 `conclusion`。
- **`GET/POST /api/capability`、`/api/capabilities/dump`、`dumpCapabilities`**：`name` 在两边都接受 TWAIN 能力名 / 编号
  （SANE 版内部做了映射，返回结构一样：数组，元素含 `container` / `itemType` / `value` 或 `items` 等）；
  SANE 版另外接受 SANE 选项名（如 `resolution`、`mode`、`source`）。dump 的详细结构平台相关，仅供排查。
- **设备名**：TWAIN 版是驱动的 ProductName；SANE 版是"厂商 型号"（重名时追加 SANE 设备名），同时也接受
  SANE 原始设备名（如 `fujitsu:fi-7160:1234`）。前端都只应原样回传服务给的名字。
- **日志文件**：TWAIN 版 `scansvc.log` + `twain.log`；SANE 版只有 `scansvc.log`（SANE 细节也写在里面）。
  错误提示里出现的"详见 twain.log"在 SANE 版是"详见 scansvc.log"。
- **错误提示括号里的排查细节**：第 7 章各条的开头（`扫描失败：卡纸`、`打开扫描仪失败: <设备>`、`扫描仪忙，打不开` …）两边一致，
  括号里的排查步骤和底层返回码是平台相关的（TWAIN 版写 `TWRC_BUSY` / condition code，SANE 版写 `SANE_STATUS_*`、
  用户组权限等）。前端原样显示即可，不要解析括号里的内容。SANE 版另有一条 `没有权限访问扫描仪`（服务用户不在 scanner / lp 组）。
- **`/version`** 的版本串：TWAIN 版 `v2.1-scansvc`，SANE 版 `v2.1-scansvc-sane`，可以用来区分连的是哪一版（前端一般不需要）。
- **SANE 版 `/api/diagnose` 字段**：`backend`（固定 `"sane"`）`processBits` `user` `groups` `lib` `version` `initError`
  `configDir` `enabledBackends` `enumerated` `devices[{name,vendor,model,type}]` `usbScanners` `conclusion`。
- **开机自启 / 托盘**：TWAIN 版托盘 + 启动文件夹快捷方式；SANE 版 systemd 服务，无托盘。

改契约（新增字段、路由、指令、设置项 key）时**两个仓库要同步改**，并更新本文。

---

## 9. 已知不一致 / 遗留

以代码为准，下列是现有文档或注释与代码不符、或接口自身不统一的地方：

1. **WS `config` 顶层不判断每项**：`cmd:"config"` 永远 `success:true`，HTTP `POST /api/config` 才按每项算（TODO-settings #8）。
2. **未连接时状态码不统一**：`POST /api/capability` 给 500，其它设置类接口给 409（TODO-settings #6）。
   `/api/capabilities/dump` 连接失败也给 409，而 `/api/connect`、`/api/scan/start`、`POST /api/scanner-options` 连接失败给 500。
3. **`setCapability` / `POST /api/capability` 的 `value` 必须是字符串**，传数字 400 / 解析失败（TODO-settings #10）。
4. **`/api/scan` 与 WS `cmd:"scan"` 的 `config` 单项失败被忽略照常开扫**，且不返回结果（TODO-settings #9）；
   而 handle `scan` / `/api/scan/start` 的 `scannerOptions` 单项失败会取消扫描。
5. **`cmd:"scan"` 不支持 `extension` / `scannerOptions`**，产出原始 BMP、地址 `/api/image?id=`；和 handle `scan` 产出不同。
   `protocol.go` 头注释"图片仍旧由 /api/image 取"只对 cmd 协议成立。
6. **handle `scan` 忽略 `count`**，固定扫到没纸；`/api/scan/start` 才支持 `count`。
7. **`scanjob.go` 的 `Extension` 注释写"jpeg / jpg / png，默认 png"**，实际也支持 `tiff`。
8. **`protocol.go` 的未知指令提示**列出的支持列表漏了 `dumpCapabilities`。
9. **分辨率设置失败的文案**建议用 `/api/capability?name=ICAP_XRESOLUTION` 查，按名字实际读不出，应为 `name=0x1118`（TODO-settings #11）。
10. **`/file/restore` 备份里找不到原图时 `errorCode` 仍为 0**，只填 `msg`（照搬原服务，前端依赖此行为）。
11. **`/dir/open` 成功的 `data` 是 `{"path":""}`**（复用了 dirUploadData），不是 null。
12. **`applied` 回显不随换设备 / 断开清空**（TODO-settings #7）。
13. **WS 与 HTTP 混用扫描不互斥**：`/api/scan/start` 只挡上一个 HTTP 任务；handle `scan` 只按 `scanning` 标记忽略；
    两条路同时发会在扫描线程上排队。
14. **`/api/image` 登记表只在内存**，服务重启后 `/api/image?id=` 全部失效（`/file/` 地址不受影响）。
15. **CLIENT_API.md / SCANNER_OPTIONS_API.md 的示例地址以 `.jpeg` 结尾**，实际 `extension` 为 `jpg` 或 `jpeg` 时文件后缀都是 `.jpg`（`imgfmt.NormalizeExt`）；只有 TIFF 的预览图是 `.jpeg`。
16. README 3 章 `/api/devices` 等示例未写 `resolutionFallback` 之外的新字段（如 `/api/scan/status` 的 `device`、`startedAt`），以本文为准。

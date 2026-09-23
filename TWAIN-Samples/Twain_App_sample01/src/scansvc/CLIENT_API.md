# scansvc 客户端对接说明

本机扫描服务，把扫描仪能力通过 WebSocket / HTTP 暴露给浏览器里的业务系统。
浏览器不能直接驱动扫描仪，也读不了本地磁盘上的图，这两件事都由本服务代劳。

扫描仪设置项（颜色模式、分辨率、纸张…）的协议单独一份：[SCANNER_OPTIONS_API.md](SCANNER_OPTIONS_API.md)。

---

## 1. 部署形态

服务装在**操作员自己的电脑上**（和扫描仪同一台机器），业务系统的页面从浏览器连它。

| | 地址 | 用途 |
|---|---|---|
| WebSocket | `ws://127.0.0.1:5000/` | 扫描：取设备列表、扫描、设置项（推荐，能边扫边推） |
| HTTP | `http://127.0.0.1:18080` | 取图片、转发上传、查状态；也能扫描（异步任务，见第 7 章） |

- 两个端口供的是**同一套路由**，功能上不分主次；分成两个只是为了兼容既有前端写死的地址。
- WebSocket 连根路径 `/` 即可；`ws://127.0.0.1:5000/ws` 也是同一个端点。
- 已开 CORS（`Access-Control-Allow-Origin: *`），业务系统部署在哪个域名都能调。
- 服务没启动时，WebSocket 连不上、HTTP 请求直接失败，前端要给出"请启动扫描服务"之类的提示。

### 1.1 对接前先自查

服务自带一个**接口测试台**，浏览器直接打开 <http://127.0.0.1:18080/>，按本文的流程逐步走：

```
0. 服务状态    → GET /version、GET /api/status
1. 扫描仪列表  → GET /api/devices（列表里少了某台设备？看 GET /api/diagnose 的 conclusion）
2. 设置项      → GET/POST /api/scanner-options（控件是按返回值通用渲染的）
3. 扫描        → POST /api/scan/start + 轮询 /api/scan/status，扫出来的图直接显示
4. 转发上传    → POST /file/upload，填上你的业务系统地址就能试
```

页面底部有请求日志（方法、路径、状态码、耗时），排查时很好用。

**两个用途**：一是对接前先在上面扫出一张图、传成功一次，确认服务、驱动、扫描仪、业务系统接口都没问题，
接下来出的问题就在客户端这一侧；二是它本身就是一份**参考实现**——没有用任何第三方库，
在浏览器里"查看源代码"就能看到每个接口怎么调、设置项怎么通用渲染。

---

## 2. 最小闭环

```
1. new WebSocket("ws://127.0.0.1:5000/")
2. 发 {"handle":"scannerList"}                     → 拿到扫描仪名称列表，让用户选一台
3. 发 {"handle":"getScannerOptions","scanner":名称} → 画设置面板（可选，见设置项文档）
4. 发 {"handle":"scan","scanner":名称,...}          → 每扫出一张推一条，base64 字段是图片地址
5. <img src="那个地址">                             → 显示
6. POST http://127.0.0.1:18080/file/upload         → 让本服务把图转发上传到业务系统
```

一条 `scan` 就会**一直扫到送纸器没纸**，每扫出一张推一条。扫描期间重复发的 `scan` 会被忽略。

---

## 3. WebSocket 协议

### 3.1 通用规则

- 每条消息是一个 JSON 对象，用 **`handle` 字段**区分指令。
- **服务端把请求原样带回**，再补上 `code` / `data` / `msg`。所以请求里带的自定义字段
  （比如 `sort`、`id`）会原样回来，用来把响应对应回是哪一次请求、哪一页。
- `code`：`0` 成功，`-1` 失败。失败原因在 `msg` 和 `message` 里（两个字段内容一样，取哪个都行），
  是中文，可以直接显示给用户。
- 同一时刻**只保留一条连接**：新连接接入时旧连接会被关掉（页面刷新后不会残留）。
- 发过去的内容**不是合法 JSON** 时，回的是 `{"cmd":"error","success":false,"error":"消息不是合法 JSON: ..."}`，
  这条没有 `handle` 也没有 `code`。按 `data.code !== 0` 判断仍然安全，但日志里看到它就是消息拼错了。
- 服务端会定期发 ping，客户端用标准 WebSocket API 即可，不用额外处理。

### 3.2 scannerList —— 扫描仪列表

```json
→ {"handle":"scannerList"}
← {"handle":"scannerList","code":0,"data":["Uniscan Q400","KODAK Scanner: S2000"]}
```

**列表来自已安装的驱动，不代表设备接着或开机**。设备是在第一次扫描时才真正打开的。

失败（`code:-1`）通常是 TWAIN 环境没起来，`msg` 会说明。

### 3.3 scan —— 扫描

```json
→ {"handle":"scan",
   "scanner":"Uniscan Q400",      // 必填，设备名
   "extension":"jpg",             // 图片格式：jpg / tiff / png，默认 png。见 4.1
   "sort":3,                      // 任意自定义字段，会原样回来（这里是页序号）
   "id":null,                     //   同上（这里是要替换的页 id）
   "show_setting":false,          // true 时弹扫描仪驱动自带的设置面板，不扫描
   "scannerOptions":{"dpi":300}   // 可选，见 SCANNER_OPTIONS_API.md
  }
```

**每扫出一张图推一条**，请求内容原样回显，图片地址放在 `base64` 字段里
（字段名是历史原因，里面是 URL 不是 base64）：

```json
← {"handle":"scan","scanner":"Uniscan Q400","extension":"jpeg","sort":3,"id":null,
   "code":0,
   "base64":"http://127.0.0.1:18080/file/SCAN_20260916_095614_861_000001N.jpeg"}
```

- **一条 `scan` 会一直扫到送纸器没纸**，每扫出一张推一条，有几张推几条。
  只想扫一张就一张一张地放纸，或者走 HTTP 接口传 `count: 1`（见 7.2）。
- 扫描期间界面要保持"扫描中"，直到不再收到新消息。
- 扫描还没结束时又发来的 `scan` **会被忽略，不回任何消息**。
  老前端靠定时器循环发 `scan` 来实现连续扫描，那些请求现在都是多余的，
  直接忽略掉——否则它们会排在后面，等纸扫完再一个个执行、每个都报"没纸"。
  这样老前端不用改：纸走完之后它再发的那一次才真的开一轮扫描，扫不到纸回
  `code:-1`（"送纸器里没有纸"），正好是停止循环的信号。
- 失败时只回一条 `code:-1`，`msg` 说明原因（见 8.1 的对照表）。
- `show_setting:true` 时**成功不回任何消息**（面板是模态的，用户关掉就结束），失败才回 `code:-1`。
  所以这个分支要靠客户端自己的超时/定时器复位界面状态。

### 3.4 设置项

`getScannerOptions` / `setScannerOptions`，以及 `scan` 里的 `scannerOptions` 字段，
见 [SCANNER_OPTIONS_API.md](SCANNER_OPTIONS_API.md)。要点：

- 服务端把"这台扫描仪能设什么"描述成一组设置项，客户端照着画控件，不用关心 TWAIN。
- 设置项**按扫描仪而异**，数量、顺序、可选值都不要写死。
- 推荐每次 `scan` 都带上当前设置：扫描仪重连后会回到驱动默认值。

### 3.5 其它指令

| 指令 | 行为 |
|---|---|
| `{"handle":"export"}` / `{"handle":"import"}` | 直接回 `code:0`，本服务不负责导入导出（老前端拿它关 loading） |
| `{"handle":"dumpCapabilities","scanner":"设备名"}` | 导出这台扫描仪支持的全部 TWAIN 能力并存盘，回 `code:0`。**排查问题时服务方会让你发这个**，客户端日常不用 |
| `{"handle":"rfidRead"}`、`codePrintList`、`codePrint` | 回 `code:-1` 并说明：RFID 读卡和条码打印不在本服务范围内 |
| 其它没实现的 | 回 `code:-1`，`msg` 里写明指令名 |

---

## 4. 图片怎么取

### 4.1 支持的格式

扫描请求里的 `extension` 决定存成什么格式，目前支持三种（写 `jpeg`、`tif` 也认）：

| 取值 | 说明 |
|---|---|
| `jpg` | 有损压缩，质量 85。体积小，适合给人看、往业务系统传 |
| `tiff` | **LZW 无损压缩**，档案数字化归档要的就是这个。黑白扫描存 1 位、灰度存 8 位、彩色存 24 位，不会因为格式转换把黑白图撑成彩色 |
| `png` | 无损，本服务早期的默认值，留着兼容 |

三种格式都会把**扫描分辨率（DPI）写进文件**（TIFF 写 XResolution/YResolution，JPEG 写 JFIF 密度，
PNG 写 pHYs）。档案验收要查图片里记录的 DPI，别丢。

- DPI 取的是扫描仪对**这一页**报告的分辨率（TWAIN 的 `DAT_IMAGEINFO`），不是请求里填的值。
- 扫描仪没报、或报的值不在 50~9600 之间时，**按 300 兜底**写入，服务日志里会有一条警告。
  客户端不用做任何处理；验收发现某页 DPI 对不上时，拿 `scansvc.log` / `twain.log` 查是不是走了兜底。

> **TIFF 浏览器显示不了**。所以转 TIFF 时服务会**一并生成同名的 JPEG 预览**，
> 预览取 `<图片地址>?thumbnail=1`，上传和归档仍然用原来的 `.tiff` 地址。

### 4.2 取图片

扫出来的图由服务的 HTTP 端口提供：

```
http://127.0.0.1:18080/file/<文件名>
http://127.0.0.1:18080/file/<文件名>?thumbnail=1     # 有同名 .jpeg 缩略图就给缩略图，没有回原图
```

- `scan` 推回来的 `base64` 字段**就是完整可用的地址**，直接塞 `<img src>` 即可，不要自己拼。
- 文件名形如 `SCAN_20260916_095614_861_000001N.jpeg`，**平铺在扫描目录下，地址里不会有子目录**。
- 响应带 `Cache-Control: max-age=7200`。同一张图反复显示不会重复下载；
  如果某张图会被改写（加工后覆盖），显示时自己在 URL 后面加个 `?t=时间戳`。
- 图片会一直留在操作员机器上，除非服务开了自动清理（默认关闭）。
- `?thumbnail=1` 对 TIFF 尤其有用：拿到的是同名 JPEG 预览（见 4.1）。

---

## 5. 上传：让服务把图转发给业务系统

浏览器读不到本地磁盘上的图，所以**不要**试图自己上传扫描结果。让服务代劳：

```
POST http://127.0.0.1:18080/file/upload
Content-Type: multipart/form-data
Header: token: <业务系统要的令牌，可选>

filename = SCAN_20260916_095614_861_000001N.jpeg   # 图片地址的最后一段
server   = https://业务系统/api/file/upload         # 目标地址，完整 URL
<其余任意字段>                                       # 原样透传给业务系统，比如 archive_id、sort
```

服务把这张图从磁盘读出来，以 `multipart/form-data` 发给 `server`，
**文件字段名固定是 `image`**，其余表单字段原样带上，`token` 头也带上；
业务系统的响应**字节透传**回来（不重新序列化，19 位的 id 不会被转成科学计数法）。

响应形如：

```json
{"errorCode":0,"msg":"","data":{...}}          // errorCode 0 成功，其余见下
```

| errorCode | 含义 |
|---|---|
| 0 | 成功。`data` 是业务系统返回的内容 |
| 1 | 转发失败：文件找不到、`server` 地址不对、网络不通、业务系统没返回 JSON，`msg` 里有具体原因 |

> 注意：`filename` 只写文件名，不要带路径。图片地址的最后一段就是它。

---

## 6. 可能用到的其它 HTTP 接口

| 接口 | 用途 |
|---|---|
| `GET /version` | 一行文本：服务版本 + 工作目录 + 进程目录。用来探测服务在不在 |
| `GET /api/devices` | `{"devices":[...],"count":2}`，和 `scannerList` 等价 |
| `GET /api/diagnose` | TWAIN 环境体检：装了哪些驱动、本服务是 32 位还是 64 位、某台设备为什么枚举不出来。`conclusion` 是可以直接给人看的结论。**"驱动装了却枚举不到"时先发这个** |
| `GET /api/status` | 当前状态：`{"state":4,"stateText":"已连接扫描仪","ready":true,"connected":true,"device":"...","scanning":false}`，扫描期间也能立刻返回 |
| `GET /api/scanner-options?device=<设备名>` | 设置项，和 `getScannerOptions` 等价 |
| `POST /api/scanner-options` | 下发设置项，和 `setScannerOptions` 等价，body `{"device":"...","scannerOptions":{...}}` |
| `POST /file/restore` | 从备份目录还原原图（`name=<相对路径>`） |
| `POST /file/temp/path/update` | 切换服务的工作目录（`path=<目录>`），影响 `/file/` 的查找起点 |
| `POST /dir/verify`、`/dir/children`、`/dir/open`、`/dir/upload` | 目录浏览 / 备份 / 打开，给"数字化加工"那类页面用 |

扫描本身也能走 HTTP，见下一节。（`POST /api/scan` 是早期留下的调试接口，产出是原始 BMP、
地址形式也不一样，**客户端不要用**。）

---

## 7. 走 HTTP 扫描（不方便用 WebSocket 时）

推荐还是用 WebSocket：驱动是整叠纸传完才回调的，WebSocket 能边扫边推，HTTP 只能轮询。
但如果客户端环境不方便用 WebSocket，这一套等价接口能完成同样的事，**产出完全一致**
（同样转成 jpeg/png、同样平铺、同样是 `/file/<文件名>` 地址，一样能走 `/file/upload` 上传）。

扫描是**异步任务**：发起后立刻返回 `jobId`，扫描在后台跑，客户端轮询进度。
这样不受浏览器和中间代理的超时限制——走送纸器扫一叠纸挂几分钟很正常。

### 7.1 取扫描仪列表

纯 HTTP 对接时，设备列表和设置项都有对应的接口，不需要 WebSocket：

```
GET http://127.0.0.1:18080/api/devices
← {"devices":["Uniscan Q400","KODAK Scanner: S2000"],"count":2}

GET http://127.0.0.1:18080/api/scanner-options?device=Uniscan%20Q400     # 取设置项
POST http://127.0.0.1:18080/api/scanner-options                          # 下发设置项
     {"device":"Uniscan Q400","scannerOptions":{"dpi":300}}
```

和 WebSocket 的 `scannerList` / `getScannerOptions` / `setScannerOptions` 完全等价，
返回结构也一样（设置项的结构见 [SCANNER_OPTIONS_API.md](SCANNER_OPTIONS_API.md)）。
所以走 HTTP 的完整流程是：

```
GET  /api/devices                 → 让用户选一台
GET  /api/scanner-options?device= → 画设置面板（可选）
POST /api/scan/start              → 拿 jobId
GET  /api/scan/status?job=        → 轮询，拿到每页的 url
POST /file/upload                 → 转发上传
```

### 7.2 发起扫描

```
POST http://127.0.0.1:18080/api/scan/start
Content-Type: application/json

{
  "device": "Uniscan Q400",       // 设备名；不传则用当前已连接的那台
  "extension": "jpg",             // 图片格式：jpg / tiff / png，默认 png。见 4.1
  "count": 0,                     // 扫几页；不传或 <=0 都表示走送纸器一直扫到没纸，只扫一页传 1
  "scannerOptions": {"dpi": 300}  // 可选，同 SCANNER_OPTIONS_API.md
}
```

```json
← 200 {"success":true,"job":{"id":"1758...-1","device":"Uniscan Q400","state":"scanning",
       "startedAt":"2026-09-16 10:20:31","pages":[]}}
```

失败时 HTTP 状态码非 200，`error` 是可以直接显示的中文：

| 状态码 | 情况 |
|---|---|
| 400 | 请求体不是合法 JSON；或没传 `device` 且当前没有连接扫描仪 |
| 409 | 上一次扫描还没结束；或 `scannerOptions` 有项设不上（这时 `results` 里是逐项结果，**不会开始扫描**） |
| 500 | 打开扫描仪失败（设备没接、被占用、选错型号…） |

### 7.3 查进度

```
GET http://127.0.0.1:18080/api/scan/status?job=<id>     # 不带 job 参数时给最近一次任务
```

```json
← {"success":true,"job":{
     "id":"1758...-1","device":"Uniscan Q400",
     "state":"done",                       // scanning 扫描中 / done 完成 / failed 失败
     "startedAt":"2026-09-16 10:20:31","endedAt":"2026-09-16 10:20:48",
     "pages":[
       {"page":1,"file":"SCAN_20260916_102031_861_000001N.jpeg",
        "url":"http://127.0.0.1:18080/file/SCAN_20260916_102031_861_000001N.jpeg"}
     ]}}
```

- `state` 是 `scanning` 时继续轮询，**建议间隔 1 秒**；`pages` 里已经有的页可以先显示出来。
- `state` 是 `failed` 时，`error` 是失败原因（和 8.1 的对照表一样）。
- `url` 直接用；`file` 就是 `/file/upload` 要的 `filename`。
- 查不到任务返回 404：只保留最近 20 次任务的结果，扫完及时取走。

### 7.4 一次连续扫描怎么写

```
POST /api/scan/start {"device":"...","extension":"jpeg"}   // count 不传 = 扫到没纸
循环: GET /api/scan/status?job=<id>  每秒一次
       state == "scanning" → 把新出现的 pages 显示出来，继续
       state == "done"     → 全部页都在 pages 里，结束
       state == "failed"   → 显示 error
```

要一张一张地扫（前端自己控制节奏）就把 `count` 设成 1，扫完再发一次 `start`。

---

## 8. 出错时客户端该怎么提示

### 8.1 扫描失败

`scan` 回 `code:-1` 时，`msg` 已经是可以直接显示的中文。下表列的是**关键词**，
实际字符串通常带"扫描失败："前缀，"打开扫描仪失败: ..."后面还跟着一段排查提示——
**直接把 `msg` 原样显示即可，不要按关键词做匹配替换**：

| 提示 | 客户端建议动作 |
|---|---|
| 扫描仪未连接或未开机（请检查电源、USB 线，或是否被其他扫描程序占用） | 提示用户检查设备，允许重试 |
| 送纸器里没有纸，请放纸后重试 | 提示放纸，允许重试 |
| 卡纸 / 检测到重张进纸 / 盖板或送纸器未合上 | 原样提示，允许重试 |
| 扫描仪被其他程序占用 | 提示关闭厂商扫描工具 |
| 扫描仪状态异常（数据源未正常复位），请断开重连扫描仪后重试 | 提示重新插拔或重启服务 |
| 打开扫描仪失败: ... | 多半是选错型号或设备没接，提示重新选择 |
| 扫描设置未生效，已取消扫描。分辨率：该扫描仪不支持 250 dpi | 指向设置面板，让用户改这一项 |
| 扫描未产出任何图片（检查是否放纸、盖板是否合上，详见 twain.log） | 提示检查纸张后重试。扫描仪启动后 **2 分钟**内没开始传图，服务会主动放弃这一次并返回这条，服务本身不受影响，可以直接重试 |

### 8.2 连不上服务

| 现象 | 原因 | 提示 |
|---|---|---|
| WebSocket 连不上 5000 | 服务没启动，或被旧版服务占着端口 | "请启动扫描服务" |
| HTTP 请求失败 | 同上 | 同上 |
| 能连上但 `scannerList` 为空 | 驱动没装 / 设备没接 / 32 位驱动 | "未检测到扫描仪，请确认驱动已安装、设备已连接" |

服务端的详细日志在操作员机器上：`scansvc.log`（服务旁边），
扫描仪层面的细节在 `twain.log`。让用户提供这两个文件就能定位问题。

---

## 9. 对接约定

- **端口固定** `5000` / `18080`，不要做成可配置项去猜——服务端改端口的场景极少，真改了会通知。
- **图片地址原样使用**，不要自己拼路径、不要改文件名。
- **设置项不要写死**：数量、顺序、可选值都由服务端给，按控件类型通用渲染，
  遇到不认识的控件类型跳过即可（详见设置项文档）。
- **连续扫描由客户端控制节奏**，服务端一次 `scan`（或一次 `/api/scan/start`）只做一轮。
- **不要并发扫描**：一台扫描仪同一时刻只能扫一件事。HTTP 方式发起时，如果**上一次 HTTP 任务**
  还没结束会返回 409；但 WebSocket 和 HTTP 混用时服务端**不会**互相拦截，
  请求会排队等前一次扫完（表现为卡住很久），所以两条路不要同时用。
- **上传一律走 `/file/upload` 转发**，不要尝试在浏览器里读本地文件。
- 请求里的自定义字段会原样回显，用它把异步返回的图片对应回业务数据（页序号、档案 id 等）。

---

## 附录 A. 最小示例代码

去掉了界面部分，只留协议交互，可以直接照着改。

### A.1 WebSocket（推荐）

```js
const WS_URL   = 'ws://127.0.0.1:5000/'
const HTTP_URL = 'http://127.0.0.1:18080'

let scanners = []          // 扫描仪列表
let options  = {}          // 用户选的设置项 {key: 取值}，见设置项文档
let current  = null        // 当前选中的扫描仪

const ws = new WebSocket(WS_URL)

ws.onopen  = () => ws.send(JSON.stringify({ handle: 'scannerList' }))
ws.onerror = () => alert('连不上扫描服务，请确认已启动')

ws.onmessage = (e) => {
  const data = JSON.parse(e.data)

  // 失败：msg 是可以直接显示的中文
  if (data.code !== 0) {
    alert(data.msg || data.message || '扫描失败')
    return
  }

  switch (data.handle) {
    case 'scannerList':
      scanners = data.data            // ["Uniscan Q400", ...]
      current = scanners[0]
      ws.send(JSON.stringify({ handle: 'getScannerOptions', scanner: current }))
      break

    case 'getScannerOptions':
      renderOptionPanel(data.data)    // 见 SCANNER_OPTIONS_API.md
      break

    case 'scan':
      onPage(data)                    // 每扫出一张来一条
      break
  }
}

// 开始扫一轮。sort 是自定义字段，会原样回来
function startScan(sort) {
  ws.send(JSON.stringify({
    handle: 'scan',
    scanner: current,
    extension: 'jpeg',
    sort: sort,
    show_setting: false,
    scannerOptions: options,
  }))
}

function onPage(msg) {
  const url = msg.base64              // 图片地址，直接可用
  showImage(url, msg.sort)            // <img src={url}>
  uploadPage(url, msg.sort)
}

// 让服务把图转发上传到业务系统
async function uploadPage(url, sort) {
  const fd = new FormData()
  fd.append('filename', url.substring(url.lastIndexOf('/') + 1))  // 只要文件名
  fd.append('server', 'https://业务系统/api/file/upload')          // 目标地址
  fd.append('archive_id', currentArchiveId)                       // 其余字段原样透传
  fd.append('sort', sort)

  const res = await fetch(HTTP_URL + '/file/upload', {
    method: 'POST',
    headers: { token: myToken },     // 业务系统要的令牌，不需要就去掉
    body: fd,
  })
  const out = await res.json()
  if (out.errorCode !== 0) {
    alert('上传失败：' + out.msg)
  }
  return out.data                    // 业务系统返回的内容
}
```

### A.2 HTTP（不方便用 WebSocket 时）

```js
// 设备列表：const { devices } = await fetch(HTTP_URL + '/api/devices').then(r => r.json())

async function scanOnce(device, sort) {
  const started = await fetch(HTTP_URL + '/api/scan/start', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    // count: 1 = 只扫一页。想一次扫到没纸就把 count 去掉
    body: JSON.stringify({ device, extension: 'jpeg', count: 1, scannerOptions: options }),
  }).then(r => r.json())

  if (!started.success) throw new Error(started.error)   // 409 / 500 时 error 是中文原因

  const jobId = started.job.id
  let shown = 0

  for (;;) {
    await new Promise(r => setTimeout(r, 1000))          // 每秒查一次
    const { job } = await fetch(HTTP_URL + '/api/scan/status?job=' + jobId).then(r => r.json())

    job.pages.slice(shown).forEach(p => {                // 新出现的页先显示
      showImage(p.url, sort)
      uploadPage(p.url, sort)
    })
    shown = job.pages.length

    if (job.state === 'done')   return job.pages
    if (job.state === 'failed') throw new Error(job.error)
  }
}
```

---

## 附录 B. 对接自检清单

- [ ] 操作员机器上装了 scansvc 并已启动（建议设置开机自启，服务方提供的工具里有这一项）
- [ ] 先用内置接口测试台 <http://127.0.0.1:18080/> 扫出过图、传成功过一次
- [ ] 服务没启动 / 扫描仪没接时，界面有明确提示，不是转圈卡死
- [ ] 扫描仪列表让用户选，并记住上次选的那台（按用户 + 机器记）
- [ ] 设置面板按 `control` 通用渲染，没有把某台扫描仪的取值写死
- [ ] 每次扫描都带上 `scannerOptions`
- [ ] 图片地址原样使用，没有自己拼路径；会被改写的图加了 `?t=` 时间戳
- [ ] 上传走 `/file/upload`，业务字段随表单透传
- [ ] 失败时直接显示服务返回的 `msg` / `error`，没有替换成"操作失败"这种笼统文案
- [ ] 连续扫描的节奏由客户端控制，上一轮结束才发下一轮
- [ ] 同一时刻没有并发发起两次扫描（同一条路的 HTTP 任务会被 409 挡住；WS 和 HTTP 混用不会拦，要自己保证）

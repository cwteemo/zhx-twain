# 扫描仪设置接口文档（设置 / 获取）

本文只讲 scansvc 里**扫描参数的读和写**，连接、扫描、文件管理见 [README.md](README.md)。
内容以代码为准（`main.go` / `protocol.go` / `legacy.go` / `twain.go`，以及 DLL 侧
`src/main.cpp` 的 `zhx_SetCapability_STR` / `zhx_GetCapability_STR`），
和 README 第 3 章有出入的地方以本文为准，见第 7 节。

## 0. 总览

| 用途 | HTTP | WebSocket（`cmd` 协议） | 前提 |
|---|---|---|---|
| 批量设常用参数 | `POST /api/config` | `config` | 已连接 |
| 读当前参数 | `GET /api/config` | `getConfig` | 已连接 |
| 设任意一项 TWAIN 能力 | `POST /api/capability` | `setCapability` | 已连接 |
| 读任意一项能力（当前值 + 可选值） | `GET /api/capability?name=` | `capability` | 已连接 |
| 打开驱动自带设置面板 | `POST /api/setting-ui` | `showSetting` | 已连接 |
| 扫描时顺带设参数 | `POST /api/scan` 的 `config` 字段 | `scan` 的 `params.config` | 已连接或带 `device` |

**所有设置/获取接口都要求先连上设备**（`POST /api/connect`，TWAIN state ≥ 4）。
TWAIN 的能力协商只在数据源打开之后有效，没连时返回错误（HTTP 409 或 500，见各接口）。

两个端口（WebSocket 5000 / HTTP 18080）是同一套路由，下文路径在哪个端口上都能调。

### 通用约定

- 请求体、响应体都是 JSON，`Content-Type: application/json; charset=utf-8`
- 失败统一形如 `{"success":false,"error":"错误说明"}`，HTTP 状态码按语义给
- 请求体里多余的字段会被忽略；**字段类型不对（比如该传数字传了字符串）直接 400**
- 已开 CORS（`Access-Control-Allow-Origin: *`）
- 所有 TWAIN 操作在同一条线程上串行执行：扫描中、设置面板开着时，这些请求会**排队**等

### 推荐调用顺序

```
POST /api/connect      {"device":"Uniscan Q400"}
GET  /api/capability?name=0x1118            # 可选：查这台设备支持哪些 DPI
POST /api/config       {"resolution":300,"pixelType":2,"feeder":true,"duplex":true}
                                             # 看 results 里每一项的 ok
POST /api/scan         {"count":0}
```

---

## 1. 批量设置常用参数

```
POST /api/config
```

### 请求

所有字段都可选，只传要改的：

| 字段 | 类型 | 对应 TWAIN 能力 | 取值 |
|---|---|---|---|
| `resolution` | int | `ICAP_XRESOLUTION` + `ICAP_YRESOLUTION` | DPI，如 `200` `300` `600` |
| `pixelType` | int | `ICAP_PIXELTYPE` | `0` 黑白 / `1` 灰度 / `2` 彩色 |
| `feeder` | bool | `CAP_FEEDERENABLED` | `true` 走送纸器(ADF) / `false` 走平板 |
| `autoFeed` | bool | `CAP_AUTOFEED` | 自动进纸，连续扫描要开 |
| `duplex` | bool | `CAP_DUPLEXENABLED` | 双面 |
| `paperSize` | int | `ICAP_SUPPORTEDSIZES` | 见 [6.2 纸张尺寸](#62-纸张尺寸-icap_supportedsizes) |
| `brightness` | int | `ICAP_BRIGHTNESS` | 一般 `-1000` ~ `1000`，以设备为准 |
| `contrast` | int | `ICAP_CONTRAST` | 一般 `-1000` ~ `1000`，以设备为准 |

```json
{"resolution":300,"pixelType":2,"feeder":true,"autoFeed":true,"duplex":false}
```

下发顺序固定为上表顺序，与 JSON 里的字段顺序无关。

### 响应

**逐项下发、逐项返回**。扫描仪支持哪些能力差别很大，一项失败不影响其它项：

```json
{
  "success": false,
  "results": [
    {"field":"resolution","cap":"ICAP_XRESOLUTION+ICAP_YRESOLUTION","value":"300","ok":true},
    {"field":"pixelType","cap":"ICAP_PIXELTYPE","value":"2","ok":true},
    {"field":"feeder","cap":"CAP_FEEDERENABLED","value":"1","ok":true},
    {"field":"autoFeed","cap":"CAP_AUTOFEED","value":"1","ok":true},
    {"field":"duplex","cap":"CAP_DUPLEXENABLED","value":"0","ok":false,"error":"扫描仪拒绝了这个取值"}
  ]
}
```

| 字段 | 说明 |
|---|---|
| `success` | 只有**每一项都 ok** 才是 `true`；调用方应看 `results` 自己判断 |
| `results[].field` | 请求里的字段名 |
| `results[].cap` | 实际下发的 TWAIN 能力 |
| `results[].value` | 下发的值，统一成字符串（bool 转成 `"1"` / `"0"`） |
| `results[].ok` | 这一项是否设上 |
| `results[].error` | 失败原因，见 [5. 错误码](#5-错误码) |

注意：

- 请求体为空或 `{}` 时返回 `{"success":true,"results":null}`——什么都没设
- `resolution` 走 DLL 的专用函数：同时设 X/Y，再**读回比对**，读回值不等于请求值就算失败
  （多数设备只支持固定几档，传 `250` 会被驱动就近改成 `200`/`300`，此时 `ok:false`，
  但设备上的 DPI **可能已经变了**）。要知道支持哪几档，先查 `0x1118`
- 其它字段设完**不读回**，`ok:true` 只表示驱动接受了 `MSG_SET`

### HTTP 状态码

| 状态码 | 场景 |
|---|---|
| 200 | 已下发（不论每项成败） |
| 400 | 请求体不是合法 JSON，或字段类型不对 |
| 405 | 不是 GET / POST |
| 409 | 未连接扫描仪 |

---

## 2. 读取当前参数

```
GET /api/config
```

### 响应

```json
{"resolution":300,"applied":{"resolution":"300","pixelType":"2","feeder":"1","duplex":"0"}}
```

| 字段 | 说明 |
|---|---|
| `resolution` | **实时**从设备读的当前 X 分辨率。读失败时这个字段不出现 |
| `applied` | 本服务**成功设置过**的值的回显，不是从设备读的。键是 `/api/config` 的字段名，或 `/api/capability` 设置时传的 `name` 原样 |

只实时读分辨率是历史原因：DLL 以前读能力时会把数据源临时 enable 到 state 5（已去掉，见 TODO-settings.md #2）。

`applied` 的局限，用之前心里要有数：

- 通过驱动设置面板（第 4 节）改的参数**不会**反映在这里
- 设置失败的项不会记进来
- **切换设备、断开、重连都不会清空它**，换了台设备看到的可能还是上一台设过的值
- 服务重启后清空

要确切知道设备当前值，用第 3.2 节逐项读。

### HTTP 状态码

| 状态码 | 场景 |
|---|---|
| 200 | 成功 |
| 409 | 未连接扫描仪 |

---

## 3. 单项能力（原始 TWAIN 能力）

给 `/api/config` 没覆盖到的能力用，或者用来查设备支持哪些取值。

### 3.1 设置

```
POST /api/capability
```

```json
{"name":"ICAP_ORIENTATION","value":"1"}
```

| 字段 | 类型 | 说明 |
|---|---|---|
| `name` | string，必填 | 能力名，或能力编号：十六进制 `"0x1110"`、十进制 `"4368"` |
| `value` | **string**，必填 | 值一律传字符串。**传数字 `1` 会 400** |

`name` 可以直接写名字的只有下面这些（DLL 里写死的），其余能力用编号：

```
ICAP_BITDEPTH  ICAP_PIXELTYPE  ICAP_UNITS  ICAP_XFERMECH  ICAP_COMPRESSION
ICAP_IMAGEFILEFORMAT  ICAP_XRESOLUTION  ICAP_YRESOLUTION  CAP_FEEDERENABLED
CAP_DUPLEXENABLED  CAP_AUTOFEED  ICAP_SUPPORTEDSIZES  ICAP_ORIENTATION
ICAP_CONTRAST  ICAP_BRIGHTNESS  ICAP_GAMMA  ICAP_THRESHOLD
```

`value` 如何解释取决于设备报的数据类型（DLL 先 `MSG_GET` 探出类型，再以 `ONEVALUE` 容器 `MSG_SET`）：

| 设备报的类型 | `value` 写法 | 备注 |
|---|---|---|
| INT8/16/32、UINT8/16/32 | `"2"` | 按整数解析（`atoi`） |
| BOOL | `"1"` / `"true"` 为真 | **其它任何写法都当假**，包括 `"True"`、`"yes"` |
| FIX32 | `"300"`、`"-12.5"` | 按浮点解析 |
| FRAME、STR32~STR255 | — | 不支持，返回错误 |

成功：

```json
{"success":true}
```

失败：

```json
{"success":false,"error":"设置 ICAP_ORIENTATION = 1 失败: 扫描仪拒绝了这个取值"}
```

| 状态码 | 场景 |
|---|---|
| 200 | 成功 |
| 400 | JSON 不合法、`name` 为空、`value` 不是字符串 |
| 500 | 未连接扫描仪，或设备/DLL 拒绝（原因见 `error`，对照[错误码](#5-错误码)） |

> 未连接时这个接口给的是 500，`/api/config` 给的是 409，不一致，调用方别只靠状态码判断。

### 3.2 读取

```
GET /api/capability?name=0x1118
```

> ⚠ **读取请用十六进制/十进制编号，不要用名字。** DLL 读取侧的名字表和设置侧不是一张，
> 而且有几项的编号写错了，用名字读大多拿到空数组，见 [7. 已知问题](#7-已知问题)。
> 常用能力编号见 [6.1](#61-常用能力编号)。

读取在 state 4 直接 `MSG_GET`，不会启用数据源。仍建议连上设备后查一次、前端缓存起来，别在轮询里调。

响应外层固定为：

```json
{"name":"0x1118","capability":[ ... ]}
```

`capability` 是**数组**：成功时里面恰好一个对象，失败（未知能力、设备不支持）时是空数组 `[]`
——**失败也是 HTTP 200**，调用方必须判断数组是否为空。

对象按容器类型分三种：

#### ONEVALUE —— 只有当前值

```json
{"name":"0x1013","capability":[
  {"container":"ONEVALUE","itemType":6,"itemTypeName":"BOOL","value":false}
]}
```

FIX32 类型额外带定点数原始分量：

```json
{"container":"ONEVALUE","itemType":7,"itemTypeName":"FIX32","value":300.0000,"whole":300,"frac":0}
```

#### ENUMERATION —— 离散可选值（DPI 档位、色彩模式、纸张尺寸多是这种）

```json
{"name":"0x1118","capability":[
  {"container":"ENUMERATION","itemType":7,"itemTypeName":"FIX32",
   "numItems":3,"currentIndex":1,"defaultIndex":1,
   "items":[
     {"index":0,"value":200.0000,"whole":200,"frac":0,"isCurrent":false,"isDefault":false},
     {"index":1,"value":300.0000,"whole":300,"frac":0,"isCurrent":true,"isDefault":true},
     {"index":2,"value":600.0000,"whole":600,"frac":0,"isCurrent":false,"isDefault":false}
   ]}
]}
```

- 可选值：`items[].value`
- 当前值：`isCurrent` 为 `true` 的那一项（或 `items[currentIndex]`）
- 默认值：`isDefault` 为 `true` 的那一项

#### RANGE —— 连续区间（亮度、对比度、部分设备的 DPI）

整数类型：

```json
{"container":"RANGE","itemType":4,"itemTypeName":"UINT16",
 "minValue":0,"maxValue":255,"stepSize":1,"defaultValue":128,"currentValue":128}
```

FIX32 类型（每个值都额外带 `xxxWhole` / `xxxFrac`）：

```json
{"container":"RANGE","itemType":7,"itemTypeName":"FIX32",
 "minValue":-1000.0000,"minWhole":-1000,"minFrac":0,
 "maxValue":1000.0000,"maxWhole":1000,"maxFrac":0,
 "stepSize":1.0000,"stepWhole":1,"stepFrac":0,
 "defaultValue":0.0000,"defaultWhole":0,"defaultFrac":0,
 "currentValue":0.0000,"currentWhole":0,"currentFrac":0}
```

`itemType` 取值：`0` INT8 / `1` INT16 / `2` INT32 / `3` UINT8 / `4` UINT16 / `5` UINT32 /
`6` BOOL / `7` FIX32 / `8` FRAME / `9`~`12` STR32~STR255；`itemTypeName` 是对应的名字（`"BOOL"`、`"FIX32"` 等）。

| 状态码 | 场景 |
|---|---|
| 200 | 已执行（**含读取失败，此时 `capability` 为 `[]`**） |
| 400 | 缺少 `name` |
| 409 | 未连接扫描仪 |

### 3.3 导出设备全部能力（适配新扫描仪用）

```
GET /api/capabilities/dump?device=<设备名>
WebSocket: {"cmd":"dumpCapabilities","params":{"device":"..."}}
           {"handle":"dumpCapabilities","scanner":"..."}
```

会先连上设备，读 `CAP_SUPPORTEDCAPS`，把设备报的每一项都读出来（设备不报就把 `twain.h` 里的
标准能力逐项试读），结果在响应里返回，同时存到 exe 旁边的 `capdump\<设备名>_<时间>.json`：

```json
{
  "device": "Uniscan Q400",
  "supportedCapsReported": true,
  "caps": [
    {"code": "0x0101", "name": "ICAP_PIXELTYPE", "raw": [{"container":"ENUMERATION", ...}]}
  ],
  "unreadable": ["0x1234 CAP_XXX"],
  "options": [ /* 按现有 scanopt 规则拼出的 getScannerOptions 结果，对照用 */ ]
}
```

DLL 同时会给每一项在 `twain.log` 里写一行 `@CAP <编号> (0x....) = <原始 JSON>`。
实现见 `capdump.go`。

---

## 4. 驱动自带设置面板

```
POST /api/setting-ui
```

无请求体。在**运行 scansvc 的那台机器上**弹出扫描仪厂商驱动的设置界面
（TWAIN `MSG_ENABLEDSUIONLY`：只显示界面、由驱动自己保存参数，不传输图像）。
项比 `/api/config` 全得多，但有哪些项完全由驱动决定。

```json
{"success":true}
```

- **请求会一直挂着，直到用户关掉面板**（确定或取消都算），这是设计如此，不设超时。
  调用方要把 HTTP 超时放宽，或改用 WebSocket 的 `showSetting`
- 面板开着期间，其它设置/扫描请求都会排队；只有 `GET /api/status` 能立刻返回，
  此时 `scanning` 为 `true`
- 在面板里改的参数，`GET /api/config` 的 `applied` 看不到，要用 3.2 逐项读

| 状态码 | 场景 |
|---|---|
| 200 | 用户已关闭面板 |
| 405 | 不是 POST |
| 409 | 未连接扫描仪，或驱动不提供设置界面（详见 `twain.log`） |

---

## 5. 错误码

`/api/config` 的 `results[].error` 和 `/api/capability` 的 `error` 文案来自 DLL 返回码：

| DLL 返回码 | 文案 | 常见原因 |
|---|---|---|
| -1 | TWAIN 环境未初始化 | TWAINDSM.dll 没加载上 |
| -2 | 未连接扫描仪 | 没调 `/api/connect` |
| -3 | DLL 不认识这个能力名 | 名字不在 3.1 的列表里，改用编号 |
| -4 | 扫描仪不支持该能力（读当前值失败） | 设备没实现这个能力 |
| -5 / -6 | DSM 内存分配失败 | 极少见 |
| -7 | FRAME 类型不支持用字符串设置 | 如 `ICAP_FRAMES` |
| -8 | 字符串类型的能力不支持这样设置 | STR32~STR255 类能力 |
| -9 | 该能力的数据类型不支持 | 设备报了非标准类型 |
| -10 | 扫描仪拒绝了这个取值 | 值不在设备支持的范围/枚举里 |

分辨率单独一条：`扫描仪未接受这个 DPI（多数设备只支持固定档位，可用 /api/capability?name=ICAP_XRESOLUTION 查）`
——文案里的查法用名字查不出来，实际请用 `name=0x1118`。

具体的 TWAIN 返回码、条件码在 `twain.log` 里。

---

## 6. 取值参考

### 6.1 常用能力编号

读取（3.2）请用编号；设置（3.1）用名字或编号都行。

| 能力 | 编号 | 十进制 | 说明 | 常见取值 |
|---|---|---|---|---|
| `ICAP_XRESOLUTION` | `0x1118` | 4376 | 水平 DPI | 150 / 200 / 300 / 600 |
| `ICAP_YRESOLUTION` | `0x1119` | 4377 | 垂直 DPI | 同上 |
| `ICAP_PIXELTYPE` | `0x0101` | 257 | 色彩模式 | 0 黑白 / 1 灰度 / 2 彩色 |
| `ICAP_BITDEPTH` | `0x112b` | 4395 | 位深 | 1 / 8 / 24 |
| `CAP_FEEDERENABLED` | `0x1002` | 4098 | 用送纸器 | 0 / 1 |
| `CAP_FEEDERLOADED` | `0x1003` | 4099 | 送纸器里有没有纸（只读） | — |
| `CAP_AUTOFEED` | `0x1007` | 4103 | 自动进纸 | 0 / 1 |
| `CAP_DUPLEX` | `0x1012` | 4114 | 设备双面能力（只读） | 0 无 / 1 单通道 / 2 双通道 |
| `CAP_DUPLEXENABLED` | `0x1013` | 4115 | 启用双面 | 0 / 1 |
| `ICAP_SUPPORTEDSIZES` | `0x1122` | 4386 | 纸张尺寸 | 见 6.2 |
| `ICAP_ORIENTATION` | `0x1110` | 4368 | 旋转 | 0 不转 / 1 90° / 2 180° / 3 270° |
| `ICAP_BRIGHTNESS` | `0x1101` | 4353 | 亮度 | 以设备 RANGE 为准 |
| `ICAP_CONTRAST` | `0x1103` | 4355 | 对比度 | 以设备 RANGE 为准 |
| `ICAP_THRESHOLD` | `0x1123` | 4387 | 黑白阈值 | 以设备 RANGE 为准 |
| `ICAP_GAMMA` | `0x1108` | 4360 | 伽马 | 以设备为准 |
| `ICAP_UNITS` | `0x0102` | 258 | 单位 | 0 英寸 / 1 厘米 |
| `CAP_DEVICEONLINE` | `0x100f` | 4111 | 设备在线（只读） | — |
| `CAP_SUPPORTEDCAPS` | `0x1005` | 4101 | 设备支持的全部能力编号列表（只读） | — |

`ICAP_XFERMECH`（传输方式）、`ICAP_IMAGEFILEFORMAT`、`ICAP_COMPRESSION` **不要改**：
DLL 靠固定的传输方式和 `*.bmp` 产物来识别扫描结果，改了扫描可能认不出图。

### 6.2 纸张尺寸（ICAP_SUPPORTEDSIZES）

| 值 | 尺寸 |
|---|---|
| 0 | 不指定（按设备默认/自动） |
| 1 | A4 |
| 2 | JIS B5 |
| 3 | US Letter |
| 4 | US Legal |
| 5 | A5 |
| 11 | A3 |
| 29 | ISO B5 |

完整列表见 `pub/external/include/twain.h` 的 `TWSS_*`。设备实际支持哪些，读 `0x1122` 的 `items`。

---

## 7. 已知问题

> 全部待办（含修法建议、优先级、代码位置）汇总在 [TODO-settings.md](TODO-settings.md)，下面只列和接口行为直接相关的。

读和写走的是 DLL 里两套独立实现，行为不对称：

1. **`GET /api/capability` 用名字读基本读不出来。** 读取侧的名字表（`main.cpp` 的
   `zhx_GetCapability_STR`）只认 `CAP_SUPPORTEDCAPS`、`CAP_XFERCOUNT`、`ICAP_PIXELTYPE`、
   `ICAP_UNITS`、`ICAP_XFERMECH`、`CAP_DEVICEONLINE`、`CAP_FEEDERENABLED`、`CAP_AUTOFEED`、
   `CAP_DUPLEX`、`ICAP_RESOLUTION` 这几个，其中后四个编号写错了（分别写成 2、7、16、283，
   正确值是 `0x1002`、`0x1007`、`0x1012`，`ICAP_RESOLUTION` 在 TWAIN 里不存在），
   像 `ICAP_XRESOLUTION`、`CAP_DUPLEXENABLED`、`ICAP_BRIGHTNESS` 这些根本不在表里。
   不认识就返回 `[]`，外面还是 200。**README 第 3 章里 `?name=ICAP_XRESOLUTION` 的示例实际拿到的是空数组。**
   按设备动态补全名字表的 `updateCapabilityMapFromDevice` 调用处被注释掉了。
   在修 DLL 之前，读取一律用编号。
2. ~~读取结果缓冲区只有 4096 字节且不做越界检查。~~ 已改成 `std::string` 拼接，长度不限（需要用新 DLL）；
   同时支持了 ARRAY 容器（`CAP_SUPPORTEDCAPS` 就是这种），字符串值会做 JSON 转义。
3. **读取失败和“读到空”无法区分**，都是 `capability: []`，具体原因只能看 `twain.log`。
4. **未连接时状态码不一致**：`/api/config`、`GET /api/capability`、`/api/setting-ui` 给 409，
   `POST /api/capability` 给 500。
5. **`applied` 不随设备切换清空**，见第 2 节。
6. 读取会让数据源临时进入 state 5，个别设备有物理动作，见 3.2。

---

## 8. WebSocket 对照

### 8.1 本服务协议（`cmd`）

连接 `ws://127.0.0.1:5000/`（或 `/ws`）。请求带 `id` 时响应原样带回，用来配对。
`data` 的内容与对应 HTTP 接口一致，失败时 `success:false` + `error`。

```
→ {"id":"1","cmd":"config","params":{"resolution":300,"duplex":true}}
← {"id":"1","cmd":"config","success":true,"data":{"results":[...]}}
```

注意：WS 的 `config` 只要下发了就是 `success:true`，**没有 HTTP 那个“每项都 ok”的顶层判断**，
一定要看 `data.results[].ok`。

```
→ {"id":"2","cmd":"getConfig"}
← {"id":"2","cmd":"getConfig","success":true,"data":{"resolution":300,"applied":{...}}}

→ {"id":"3","cmd":"capability","params":{"name":"0x1118"}}
← {"id":"3","cmd":"capability","success":true,"data":{"name":"0x1118","capability":[{...}]}}

→ {"id":"4","cmd":"setCapability","params":{"name":"ICAP_ORIENTATION","value":"1"}}
← {"id":"4","cmd":"setCapability","success":true}

→ {"id":"5","cmd":"showSetting"}
← {"id":"5","cmd":"showSetting","success":true}          ← 用户关掉面板后才回
```

扫描时顺带设参数：

```
→ {"id":"6","cmd":"scan","params":{"count":0,"config":{"feeder":true,"autoFeed":true}}}
```

> `scan` / `POST /api/scan` 里的 `config`：只有“未连接”这种整体错误会中止扫描，
> **单项设置失败会被静默忽略、照常开扫**，且结果不返回。在意参数是否生效的话，先单独调 `config`。

### 8.2 兼容既有前端（`handle`）

| 指令 | 行为 |
|---|---|
| `{"handle":"scan","scanner":"设备名","show_setting":true}` | 先连接该设备，再弹驱动设置面板。**成功不回任何消息**（回了前端会当成扫描结果去读 `base64` 而抛异常），失败回 `code:-1` |
| `{"handle":"getScannerOptions","scanner":"设备名"}` | **不连接设备**。只有该设备已经连着（扫描过一次之后）才现读能力，否则回空数组 `data:[]`，见 8.3 |

兼容协议里**没有**直接设参数的指令，前端能改参数的途径只有驱动设置面板。

### 8.3 getScannerOptions 返回结构

前端（`加工/通用` 分支 `ScanCom/Scan.js`）用的是 SANE 风格的选项模型，不是 TWAIN 的 CAP。
实现在 `scanopt/`（纯 Go，`go test ./scanopt` 不接扫描仪也能跑）：列哪些项照着**虚拟扫描仪**自带设置面板来，可选值和当前值都现读设备，
设备不支持的项直接略过（真实扫描仪上项会少一些）。

```json
{"handle":"getScannerOptions","scanner":"TWAIN2 Software Scanner","code":0,"data":[
  {"option":1,"name":"source","type":"str-list","value":"ADF Front","list":["Flatbed","ADF Front"]},
  {"option":2,"name":"mode","type":"str-list","value":"Color","list":["Lineart","Gray","Color"]},
  {"option":3,"name":"resolution","type":"str-list","value":"200","list":["50","100","150","200","300","400","500","600"]},
  {"option":4,"name":"paper-size","type":"str-list","value":"US Letter","list":["None","US Letter","US Legal"]},
  {"option":5,"name":"rotate","type":"str-list","value":"None","list":["None"]},
  {"option":6,"name":"units","type":"str-list","value":"Inches","list":["Inches","Pixels","Centimeters","Picas","Points","Twips"]},
  {"option":7,"name":"brightness","type":"int-range","value":0,"min":-1000,"max":1000,"step":1},
  {"option":8,"name":"contrast","type":"int-range","value":0,"min":-1000,"max":1000,"step":1},
  {"option":9,"name":"threshold","type":"int-range","value":128,"min":0,"max":255,"step":1},
  {"option":10,"name":"gamma","type":"int","value":1},
  {"option":11,"name":"long-paper-scan","type":"bool","value":0},
  {"option":12,"name":"documents-in-adf","type":"int","value":20}
]}
```

上面是按虚拟扫描仪源码里的默认值推出来的示意，实际以设备返回为准。

| name | TWAIN 能力 | type 怎么定 |
|---|---|---|
| `source` | `CAP_FEEDERENABLED` + `CAP_DUPLEXENABLED` | str-list：`Flatbed` / `ADF Front` / `ADF Duplex`（`CAP_DUPLEX` 为 0 时不给双面） |
| `mode` | `ICAP_PIXELTYPE` | str-list：`Lineart`(黑白) / `Gray` / `Color` |
| `resolution` | `ICAP_XRESOLUTION` | 设备报枚举 → str-list（前端没有 int-list）；报区间 → int-range |
| `paper-size` | `ICAP_SUPPORTEDSIZES` | str-list：`A4`、`US Letter` 等，不认识的编号原样出数字 |
| `rotate` | `ICAP_ORIENTATION` | str-list：`None` / `90` / `180` / `270` |
| `units` | `ICAP_UNITS` | str-list |
| `brightness` / `contrast` / `threshold` | `ICAP_BRIGHTNESS` / `ICAP_CONTRAST` / `ICAP_THRESHOLD` | 区间 → int-range；枚举 → str-list；单值 → int |
| `gamma` | `ICAP_GAMMA` | 同上 |
| `long-paper-scan` | `CUSTCAP_LONGDOCUMENT` (0x8001) | bool，值为 1 / 0。**仅虚拟扫描仪** |
| `documents-in-adf` | `CUSTCAP_DOCS_IN_ADF` (0x8002) | int。**仅虚拟扫描仪** |

- `option` 编号按上表顺序固定，某项被略过不会让后面的项改号
- 自定义能力（0x8000 以上）的含义各厂商自己定，所以只在设备名含 `Software Scanner` 时才读
- 前端在切换扫描仪时（包括进页面默认选中第一台）就会自动发这条，所以**这里不打开设备**：
  设备列表来自已安装的驱动，不代表设备接着。只有请求的正好是已连接的那台才读，否则回空数组；
  设备在用户点扫描时才连接
- 目前**只读不写**：前端 `scan` 请求里带选项的那行是注释掉的，改选项还没有入口

**新增一项选项**：只改 `scanopt/defs.go` 里的 `Defs` 表，文件头有字段说明和示例；
改完跑 `go test ./scanopt`（有定义表自检）。

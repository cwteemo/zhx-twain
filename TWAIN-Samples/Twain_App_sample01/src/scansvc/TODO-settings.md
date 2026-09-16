# 扫描仪设置相关的待办 / 已知问题

整理于 2026-09-14，来源是梳理设置接口文档（[SETTINGS_API.md](SETTINGS_API.md)）和实现
`getScannerOptions` 时对代码的核对。**都没有修，只是记下来。**

代码里对应位置标了 `TODO(TODO-settings.md #编号)`，`grep -rn "TODO-settings" .` 能全部找出来。
DLL 侧（`src/main.cpp`）没有加标记，按函数名找。

优先级：**高** = 会让调用方拿到错误结果；**中** = 行为别扭但有绕法；**低** = 体验/整洁。

---

## 一、DLL（`src/main.cpp`，改完要重新编译 DLL）

### #1 【高】按名字读能力基本读不出来

- **位置**：`zhx_GetCapability_STR` 开头那段 `g_capabilityMap[...] = ...`
- **现象**：读取侧的名字表和设置侧（`zhx_SetCapability_STR` 里的 `strcmp` 链）不是一张。
  读取侧只认十个名字，其中 4 个编号写错：
  `CAP_FEEDERENABLED`=2（应为 0x1002）、`CAP_AUTOFEED`=7（应为 0x1007）、
  `CAP_DUPLEX`=16（应为 0x1012）、`ICAP_RESOLUTION`=283（TWAIN 里没有这个能力）。
  `ICAP_XRESOLUTION`、`CAP_DUPLEXENABLED`、`ICAP_BRIGHTNESS` 等根本不在表里。
  按设备动态补表的 `updateCapabilityMapFromDevice` 调用处被注释掉了。
- **影响**：`GET /api/capability?name=ICAP_XRESOLUTION` 返回 `[]`，且 HTTP 200。
- **现在的绕法**：一律按编号读（`?name=0x1118`）；scansvc 内部（`TwainReadCapabilities`）已经这么做了。
- **建议修法**：抽一个 `名字 → 编号` 的公共函数，读和写都用它，名字直接用 `twain.h` 的宏。

### #2 【中】读能力时每次都把数据源 enable 到 state 5 —— ✅ 已修（2026-09-15）

> 比原先估计的严重：成功判据多判了 `m_DSMState >= 5`（直接调 DSM_Entry 不会更新它），启用成功也被当成失败、
> 不发 `MSG_DISABLEDS`，数据源卡在 state 5，之后扫描的 `MSG_ENABLEDS` 全部失败。实测 Uniscan Q400 读完
> `getScannerOptions` 必然扫不了。已整段去掉临时 enable。

- **位置**：`zhx_GetCapability_STR` 里 `MSG_ENABLEDS` / `MSG_DISABLEDS` 那两段
- **现象**：TWAIN 规范里 `MSG_GET` 在 state 4 就合法，这里却先 enable 再 disable。
- **影响**：个别设备会亮灯、空走纸。`getScannerOptions` 一次读十来项，就是十来次 enable/disable。
- **建议修法**：去掉临时 enable；如果某台设备确实只在 state 5 才肯答，再针对它加回来。

### #3 【中】读能力的结果缓冲区可能写穿 —— ✅ 已修（2026-09-15）

> 改成 `std::string` 拼接，顺带支持 ARRAY 容器、字符串 JSON 转义。

- **位置**：`zhx_GetCapability_STR` 的 `static char result[4096]` + 一路 `sprintf`
- **现象**：没有越界检查。FIX32 枚举每项约 90 字节，超过 40 来项就溢出。
- **影响**：DPI、纸张一般到不了；`CAP_SUPPORTEDCAPS` 这类长列表有风险，溢出是内存破坏不是报错。
- **建议修法**：改 `snprintf` 带剩余长度，或者用 `std::string` 拼。

### #4 【中】读失败和"读到空"区分不开

- **位置**：`zhx_GetCapability_STR`，失败时 `strcpy(result, "[]")`
- **影响**：调用方只能拿到 `[]`，原因要翻 `twain.log`。
- **建议修法**：失败时输出 `{"error":"...","rc":..,"cc":..}`，Go 侧据此给明确错误。

### #5 【低】设 BOOL 能力只认 `"1"` / `"true"`

- **位置**：`zhx_SetCapability_STR` 的 `case TWTY_BOOL`
- **影响**：传 `"True"`、`"yes"` 会被静默当成假。
- **建议修法**：大小写不敏感，或者不认识的值直接报错。

---

## 二、Go 服务（scansvc）

### #6 【中】未连接设备时状态码不统一

- **位置**：`main.go` 的 `handleCapability`（POST 分支）
- **现象**：`POST /api/capability` 未连接给 500，其余设置/获取接口给 409。
- **建议修法**：`TwainSetCapability` 返回可区分的"未连接"错误，handler 映射成 409。

### #7 【中】`applied` 回显不随设备切换清空

- **位置**：`twain.go` 的 `lastApplied`
- **影响**：`GET /api/config` 在换了设备、断开、重连后，`applied` 里还是上一台设过的值。
- **建议修法**：在 `connectOnTwainThread` 真正打开新设备、`TwainDisconnect`、`TwainReconnect` 时清空。

### #8 【中】WebSocket 的 `config` 顶层永远 `success:true`

- **位置**：`protocol.go` 的 `case "config"`
- **现象**：HTTP 版只有每项都 ok 才 `success:true`，WS 版只要下发了就是 true。
- **建议修法**：照 `handleConfig` 算一遍 allOK。

### #9 【中】扫描时带的 `config` 单项失败被静默忽略

- **位置**：`main.go` 的 `handleScan`、`protocol.go` 的 `handleScanCmd`，`_, err := TwainApplyConfig(...)`
- **影响**：参数没设上照样开扫，调用方不知道。
- **建议修法**：至少把 results 带进扫描响应；或者加个 `strict` 开关，有失败就不扫。

### #10 【低】`setCapability` 的 `value` 必须是字符串

- **位置**：`main.go` 的 `capabilityRequest`、`protocol.go` 的 `setCapability`
- **影响**：传 `"value":2` 直接 400，容易踩。
- **建议修法**：字段改成 `json.RawMessage`，数字/布尔转成字符串再下发。

### #11 【低】分辨率设置失败的提示文案给了一个不能用的查法

- **位置**：`twain.go` 的 `TwainApplyConfig`，`res.Error = "扫描仪未接受这个 DPI…?name=ICAP_XRESOLUTION 查"`
- **建议修法**：改成 `?name=0x1118`；修了 #1 之后两种都行。

---

## 三、getScannerOptions（`scanopt/` + `legacy.go`）

### #12 【高】选中扫描仪就会打开设备 —— ✅ 已修（2026-09-15）

> 采用"只在已连接同一台设备时读，否则回空数组"；设备在 `scan` 时才连接。

- **位置**：`legacy.go` 的 `handleLegacyScannerOptions`
- **现象**：前端 `ScanImageHeader.vue` 监听 `curScanner`，一变就发 `getScannerOptions`，
  页面加载拿到扫描仪列表时也会触发。读能力必须先打开设备。
- **影响**：以前秒回空数组；现在要打开设备（几秒），选到没接的设备会占住 TWAIN 队列二十来秒。
- **建议修法（任选）**：按设备名缓存上一次的选项，先回缓存；或者只在已连接同一台设备时读，
  没连就回空数组，等第一次扫描后再读。

### #13 【高】还没在 Windows 上实测

- **现状**：`scanopt` 只在 Linux 上 `go test` 过。`scanopt/testdata/devices/TWAIN2_Software_Scanner.json`
  是按虚拟扫描仪源码（`Twain_DS_sample01/src/CTWAINDS_FreeImage.cpp`）的默认值和 DLL 输出格式**推出来的**，不是真实抓取。
- **要做**：Windows 上对虚拟扫描仪和每台真实扫描仪各导出一份 capdump，放进 `scanopt/testdata/devices/`，
  `go test ./scanopt -update` 生成期望结果、核对后提交（步骤见 SCANNER_OPTIONS_CONFIG.md 第 6 节）；
  虚拟扫描仪那份真实数据替换掉模拟数据。

### #14 【中】只读不写 —— ✅ 已做（2026-09-15）

> 改成新的设置项协议（SCANNER_OPTIONS_API.md），新增 `setScannerOptions`，`scan` 接收 `scannerOptions`。

- **现状**：前端 `Scan.js` 的 `scan_data` 里 `scannerOptions` 那行是注释掉的，设置面板也被注释了，
  所以目前没有入口把选项发回来。
- **建议做法**：在 `scanopt/defs.go` 的 `Def` 上加一个 `Apply` 字段（选项值 → 若干 `能力编号=值`），
  取值表是 `map[int]string`，反查即可；`legacy.go` 的 `scan` 里读 `scannerOptions`，
  扫描前逐项 `setCapOnTwainThread`。`source` 这种合成项要拆回两个能力。

### #15 【中】前端设置面板缺项会崩（前端仓库）—— 作废

> 协议已换，前端设置面板要按 SCANNER_OPTIONS_API.md 重做，旧的 `formatConfigs` 不再适用。

- **位置**：court-document-processing `加工/通用` 分支，`ScanCom/_components/setScanTools.vue` 的 `formatConfigs`
- **现象**：对白名单里每个 name 直接取 `fieldMap[name].value`，本服务没返回的项 `fieldMap[name]` 是 undefined。
- **影响**：面板现在是注释掉的，暂时不会触发；**重新启用面板前必须先改**，否则一打开就报错。
- **建议修法**：`val[item] && val[item].value`，或者白名单只保留服务端返回了的 name。

### #16 【低】部分选项在前端没有中文标签 / 形态不一致 —— 作废

> 新协议由服务端给中文 `label`，前端不再维护标签表。

- **现象**：
  - `paper-size`、`units`、`threshold`、`gamma`、`documents-in-adf` 不在前端 `temporaryData` 里，标签是空的；
    `Flatbed`、`Inches`、`US Letter` 等取值不在 `temporaryOptions` 里，下拉框显示英文原文。
  - 前端假数据里 `resolution` 是 `int-range`，TWAIN 设备多报枚举，这里给的是 `str-list`（前端没有 int-list）。
- **建议修法**：前端补标签；或者前端加一个 `int-list` 类型。

### #17 【低】`rotate` 的方向没核对 —— 暂缓

> 新协议第一版没有 `rotate`，以后加回来时再核对。

- **位置**：设置项配置 `scanopt/default_options.jsonc` 里的 `rotate` 示例（默认 disabled）
- **现象**：`TWOR_ROT90`/`TWOR_ROT270` 在 TWAIN 里是顺时针还是逆时针没查证，前端把 `"90"` 标成"顺时针90度"。
- **要做**：拿虚拟扫描仪或真实设备扫一张确认，不对就对调 1 和 3 的文字。

---

## 四、其它已记录的问题

README 第 8 节"已知限制 / 下一步"里还有几条和设置无关的（设备列表内存泄漏、`GetDesktopWindow` 当父窗口等），
RFID / 条码打印见 [TODO-rfid-zebra.md](TODO-rfid-zebra.md)，这里不重复。

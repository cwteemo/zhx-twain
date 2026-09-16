# 扫描仪设置项协议（给前端）

版本：`version: 1`　　适用服务：scansvc（WebSocket `ws://127.0.0.1:5000/`，既有的 `handle` 协议）

扫描、取图、上传等其余对接内容见 [CLIENT_API.md](CLIENT_API.md)。

## 0. 一句话

服务端把"这台扫描仪能设什么、当前是什么、可以选哪些"描述成一组**设置项**发给前端，
前端**照着描述画控件**，用户改完把 `{key: 取值}` 发回来，服务端负责翻译成扫描仪的参数。

前端不需要知道 TWAIN、不需要知道扫描仪型号，也不需要维护中文标签表。
以后适配新扫描仪、增加设置项，**只改服务端**；前端只要按本文实现了几种控件，就不用跟着改。

---

## 1. 交互流程

```
进页面 / 切换扫描仪
  → {"handle":"getScannerOptions","scanner":"设备名"}
  ← data: {connected, cached, options:[...]}          按 options 画设置面板

用户点"应用"（可选，想立即看到设置结果时用）
  → {"handle":"setScannerOptions","scanner":"设备名","scannerOptions":{"dpi":300,...}}
  ← data: {results:[...], options:{...}}               逐项结果 + 最新设置项

扫描（推荐：每次扫描都带上用户选的设置）
  → {"handle":"scan","scanner":"设备名","scannerOptions":{"dpi":300,...}, ...原有字段}
  ← 原有的扫描结果消息；设置有一项不生效时 code=-1，不会扫描

某台设备第一次扫描完成后，服务端会**主动推一条** getScannerOptions（见 2.4）
```

---

## 2. 消息

所有消息沿用 `handle` 协议的老规矩：**服务端把请求原样带回**，再加上 `code` / `data` / `msg`。
`code`：`0` 成功，`-1` 失败（原因在 `msg` 和 `message` 里，两个字段内容一样）。

### 2.1 getScannerOptions —— 取设置项

```json
→ {"handle":"getScannerOptions","scanner":"Uniscan Q400"}
```

```json
← {"handle":"getScannerOptions","scanner":"Uniscan Q400","code":0,"data":{
    "version": 1,
    "scanner": "Uniscan Q400",
    "connected": false,
    "cached": true,
    "updatedAt": "2026-09-15 14:30:12",
    "options": [
      {"key":"source","label":"扫描方式","group":"basic","control":"select","value":"adfDuplex",
       "choices":[{"value":"adf","label":"进纸器单面"},{"value":"adfDuplex","label":"进纸器双面"}]},
      {"key":"colorMode","label":"颜色模式","group":"basic","control":"radio","value":"color",
       "choices":[{"value":"bw","label":"黑白"},{"value":"gray","label":"灰度"},{"value":"color","label":"彩色"}]},
      {"key":"dpi","label":"分辨率","group":"basic","control":"select","value":300,"unit":"dpi",
       "choices":[{"value":200,"label":"200 dpi"},{"value":300,"label":"300 dpi"},{"value":600,"label":"600 dpi"}]},
      {"key":"paperSize","label":"纸张尺寸","group":"basic","control":"select","value":"a4",
       "choices":[{"value":"none","label":"不指定"},{"value":"a4","label":"A4"},{"value":"a3","label":"A3"}]},
      {"key":"brightness","label":"亮度","group":"basic","control":"slider","value":0,"min":-1000,"max":1000,"step":1},
      {"key":"contrast","label":"对比度","group":"basic","control":"slider","value":0,"min":-1000,"max":1000,"step":1}
    ]
  }}
```

**这条不会打开扫描仪**（进页面就发，打开一台没接的设备会卡好几秒）。所以：

| `connected` | `cached` | `options` | 含义 / 前端怎么做 |
|---|---|---|---|
| `true` | `false` | 有 | 设备连着，现读的，最准 |
| `false` | `true` | 有 | 设备没连，给的是上次连着时读到的（`updatedAt` 是读的时间）。照常显示即可 |
| `false` | `false` | `[]` | 这台设备从来没连过，不知道它能设什么。提示"扫描一次后可设置"，或者不显示设置面板 |

`data` 永远是对象，不会是 `null`。

### 2.2 setScannerOptions —— 下发设置

```json
→ {"handle":"setScannerOptions","scanner":"Uniscan Q400",
   "scannerOptions":{"colorMode":"gray","dpi":300,"brightness":100}}
```

```json
← {"handle":"setScannerOptions", ..., "code":0,"data":{
    "results":[
      {"key":"colorMode","value":"gray","ok":true},
      {"key":"dpi","value":300,"ok":true},
      {"key":"brightness","value":100,"ok":true}
    ],
    "options":{ /* 同 2.1 的 data，下发之后现读的最新状态，直接替换前端手里的那份 */ }
  }}
```

- 这是用户明确的操作，**会打开扫描仪**（没接会失败，`code:-1`）。
- 只传要改的项；没传的保持不变。
- 有任何一项失败：`code:-1`，`msg` 形如 `部分设置未生效。分辨率：该扫描仪不支持 250 dpi`，
  `data` 照样带回，前端可以按 `results` 标出是哪一项。
- 设置之间可能互相影响（比如换了颜色模式，可选分辨率变了），所以**以返回的 `options` 为准**刷新面板。

### 2.3 scan —— 扫描时带上设置

在原有的 `scan` 消息里加一个 `scannerOptions` 字段，其它字段不变：

```json
→ {"handle":"scan","scanner":"Uniscan Q400","sort":3,"id":null,"extension":"jpeg","show_setting":false,
   "scannerOptions":{"source":"adfDuplex","colorMode":"color","dpi":300}}
```

- 服务端先下发设置、再扫描。**有一项设不上就不扫**，回 `code:-1`，
  `msg` 形如 `扫描设置未生效，已取消扫描。分辨率：该扫描仪不支持 250 dpi`。
- 不带 `scannerOptions`（或带 `{}`）就用扫描仪当前的设置，和以前行为一样。
- 推荐**每次扫描都带上**：扫描仪断开重连后会恢复成驱动默认值，靠前端每次带上才能保证一致。
- `show_setting:true`（弹驱动自带面板）时不处理 `scannerOptions`。

### 2.4 服务端主动推送

某台扫描仪**第一次扫描完成**后（此前没有它的缓存），服务端会主动发一条：

```json
← {"handle":"getScannerOptions","scanner":"Uniscan Q400","code":0,"data":{ /* 同 2.1 */ }}
```

前端处理 `getScannerOptions` 回复的地方本来就会更新设置项，不需要额外处理；
只是要注意**可能收到自己没请求过的这条**（`scanner` 可能不是当前选中的那台，按 `scanner` 判断要不要用）。

---

## 3. 设置项结构

### 3.1 字段

| 字段 | 类型 | 说明 |
|---|---|---|
| `key` | string | 设置项标识。发回服务端、存配置方案都用它。**发布后永不改名** |
| `label` | string | 中文名，直接显示 |
| `group` | string | 分组：`basic` 基础 / `advanced` 高级。可以分区显示，也可以忽略 |
| `control` | string | 画什么控件，见 3.2 |
| `value` | string / number / boolean / null | 扫描仪**当前**的值。极少数情况下设备当前状态对不上任何选项，会是 `null`，下拉框显示为空即可 |
| `choices` | `[{value, label}]` | `select` / `radio` 才有。`value` 发回服务端，`label` 显示 |
| `min` / `max` / `step` | number | `slider` 一定有；`number` 可能有 |
| `unit` | string | 单位，可选，比如 `dpi`。只用于显示 |

没有值的字段不会出现（比如 `select` 没有 `min`），前端按 `control` 取需要的字段即可。

### 3.2 control —— 前端要实现的控件

| control | 控件（Element UI） | 用到的字段 | value 类型 |
|---|---|---|---|
| `select` | `el-select` + `el-option` | `choices` | 和 `choices[].value` 相同（字符串或数字） |
| `radio` | `el-radio-group` + `el-radio` | `choices` | 同上 |
| `switch` | `el-switch` | — | `true` / `false` |
| `slider` | `el-slider`（可配 `show-input`） | `min` `max` `step` | number |
| `number` | `el-input-number` | `min` `max` `step`（都可能没有） | number |

**兼容规则（很重要）**：
- 遇到**不认识的 `control`**：跳过这一项不画，别报错。以后服务端可能加新控件类型。
- 遇到**不认识的 `key`**：照样按 `control` 画。新增设置项就是这样做到前端不改的。
- `choices[].value` 的类型要原样发回（数字发数字），不要统一转成字符串——虽然服务端两种都认。
- 设置项的**数量、顺序、可选值都因扫描仪而异**，不要写死；设备不支持的项服务端直接不给。

### 3.3 当前版本的 key

下表是 `version: 1` 的全部设置项，**仅供了解和测试**，前端逻辑不应依赖它（依赖 3.2 就够了）。
不同扫描仪只会给其中的一部分，`choices` 也只列设备支持的。

| key | label | control | 取值 |
|---|---|---|---|
| `source` | 扫描方式 | select | `flatbed` 平板 / `adf` 进纸器单面 / `adfDuplex` 进纸器双面 |
| `colorMode` | 颜色模式 | radio | `bw` 黑白 / `gray` 灰度 / `color` 彩色 |
| `dpi` | 分辨率 | select | 数字，如 `200` `300` `600` |
| `paperSize` | 纸张尺寸 | select | `none` 不指定 / `maxSize` 最大尺寸 / `a3` `a4` `a5` `a6` / `jisB4` `jisB5` `isoB4` `isoB5` / `letter` `legal` … / `businessCard` 名片；扫描仪报了表里没有的规格时为 `code<编号>`，如 `code60` |
| `brightness` | 亮度 | slider（设备报枚举时是 select，报单值时是 number） | 数字，常见 -1000 ~ 1000 |
| `contrast` | 对比度 | 同上 | 数字 |
| `autoCrop` | 自动裁切 | switch | `true` / `false` |
| `autoDeskew` | 自动纠偏 | switch | `true` / `false` |
| `autoRotate` | 自动旋转 | switch | `true` / `false` |
| `autoColor` | 自动识别彩色 | switch | `true` / `false` |
| `blankPage` | 空白页 | radio | `keep` 保留 / `discard` 自动去除 |
| `rotate` | 旋转 | select | 数字，`0` `90` `180` `270` |
| `threshold` | 黑白阈值 | slider | 数字，常见 0 ~ 255 |

前 6 项是 `basic`（基础），后 7 项是 `advanced`（高级），前端可以分区显示。

设置项由服务端配置文件决定，后续会随着适配的扫描仪增加（比如去空白页、自动纠偏、自动裁切）。
前端按 3.2 的规则通用渲染即可，服务端加项时前端不用改。

---

## 4. results —— 下发结果

`setScannerOptions` 的 `data.results` 里每项：

| 字段 | 说明 |
|---|---|
| `key` / `value` | 前端发来的 |
| `ok` | 是否设置成功 |
| `skipped` | `true` 表示**这台扫描仪没有这一项**（或 key 不认识），已忽略，**不算失败** |
| `error` | 失败或忽略的原因，中文，可以直接显示 |

`skipped` 的意义：配置方案可能是在另一台扫描仪上存的，带着这台没有的项很正常，
不要因此提示失败。整条消息的 `code` 只看真正失败的项（`ok:false` 且没有 `skipped`）。

---

## 5. 前端实现参考

### 5.1 渲染

```vue
<el-form label-width="90px" size="mini">
  <el-form-item v-for="opt in visibleOptions" :key="opt.key" :label="opt.label">
    <el-select v-if="opt.control === 'select'" v-model="form[opt.key]">
      <el-option v-for="c in opt.choices" :key="String(c.value)" :label="c.label" :value="c.value" />
    </el-select>

    <el-radio-group v-else-if="opt.control === 'radio'" v-model="form[opt.key]">
      <el-radio v-for="c in opt.choices" :key="String(c.value)" :label="c.value">{{ c.label }}</el-radio>
    </el-radio-group>

    <el-switch v-else-if="opt.control === 'switch'" v-model="form[opt.key]" />

    <el-slider v-else-if="opt.control === 'slider'" v-model="form[opt.key]"
               :min="opt.min" :max="opt.max" :step="opt.step" show-input />

    <el-input-number v-else-if="opt.control === 'number'" v-model="form[opt.key]"
                     :min="opt.min" :max="opt.max" :step="opt.step" />
    <span v-if="opt.unit" style="margin-left: 6px">{{ opt.unit }}</span>
  </el-form-item>
</el-form>
```

```js
const CONTROLS = ['select', 'radio', 'switch', 'slider', 'number']

computed: {
  // 不认识的 control 跳过
  visibleOptions () {
    return (this.scannerOptions.options || []).filter(o => CONTROLS.includes(o.control))
  }
},
methods: {
  // 收到 getScannerOptions / setScannerOptions 的 options 后：
  // 用户存过的配置方案优先，没存过的项用扫描仪当前值；
  // 方案里的值如果已经不在 choices 里（换了扫描仪），退回当前值
  fillForm (data, saved = {}) {
    const form = {}
    for (const o of data.options) {
      const v = saved[o.key]
      const valid = v !== undefined && (!o.choices || o.choices.some(c => c.value === v))
      form[o.key] = valid ? v : o.value
    }
    this.form = form
  }
}
```

### 5.2 配置方案

前端现有的"配置方案"（`scannerOptionsValue:${扫描仪}:${用户}`）直接存 `form`，也就是 `{key: 取值}`，
扫描时把当前方案原样放进 `scan` 消息的 `scannerOptions`。

### 5.3 什么时候发什么

| 时机 | 发 |
|---|---|
| 进页面、切换扫描仪 | `getScannerOptions` |
| 打开设置面板 | 可以再发一次 `getScannerOptions`，拿最新的（设备连着时是现读的） |
| 设置面板点"应用" | `setScannerOptions`，用返回的 `options` 刷新面板，`code:-1` 时提示 `msg` |
| 每次扫描 | `scan` 带 `scannerOptions` |

---

## 6. 调试

不开前端，用 HTTP 也能调（和 WebSocket 同一套逻辑）。
服务端机器上没有 curl.exe 时，用 exe 旁边的 `scansvc-tools.bat options "设备名"` 看设置项。

```powershell
# 取设置项（不打开设备）
curl.exe "http://127.0.0.1:18080/api/scanner-options?device=Uniscan%20Q400"

# 下发设置（会打开设备）
curl.exe -X POST http://127.0.0.1:18080/api/scanner-options -H "Content-Type: application/json" `
  -d '{\"device\":\"Uniscan Q400\",\"scannerOptions\":{\"dpi\":300,\"colorMode\":\"gray\"}}'
```

服务端实现：设置项由内置配置 `scanopt/default_options.jsonc` 定义（见 [SCANNER_OPTIONS_CONFIG.md](SCANNER_OPTIONS_CONFIG.md)），
`scanopt/`（配置解析与四种类型的读写）、`options.go`（读写、缓存、现场覆盖配置）、
`legacy.go`（WebSocket 消息）。

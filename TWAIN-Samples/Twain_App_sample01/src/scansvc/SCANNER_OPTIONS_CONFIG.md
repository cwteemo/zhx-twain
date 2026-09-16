# 扫描仪设置项配置说明（给维护人员）

前端设置面板里**有哪些设置项、每项对应扫描仪的哪个 TWAIN 能力、下拉框里有哪些选项**，
全部由一份声明式配置决定。增删设置项、增删下拉选项、改文字、改顺序，**都只改配置，不写 Go 代码**。

只有一种情况要改代码：要的效果现有的四种类型（`enum` / `number` / `switch` / `combo`）都表达不了。

给前端的协议文档是另一份：[SCANNER_OPTIONS_API.md](SCANNER_OPTIONS_API.md)。

---

## 1. 配置在哪、怎么生效

**唯一来源是源码里的 `scanopt/default_options.jsonc`**，编译进 exe。它跟代码一起提交、一起过测试、一起发版，
所有现场用的都是同一份，不会出现"每个现场一份配置"的情况。

```
改 scanopt/default_options.jsonc
  → go test ./scanopt          检查配置写法 + 已适配设备的回归（见第 6 节）
  → 编译 scansvc.exe、发版
```

### 1.1 现场应急覆盖

现场某台扫描仪急需调整、来不及发版时，可以在 `scansvc.exe` 同目录放一个 `scanner-options.jsonc`，
**它会整份替换内置配置**。这是应急手段，不是常规做法：

| | |
|---|---|
| 怎么开始 | 先下载内置配置当起点：`scansvc-tools.bat builtin-config`（或 `curl.exe http://127.0.0.1:18080/api/scanner-options/config/builtin -o scanner-options.jsonc`），再改 |
| 生效 | 保存即可，不用重启。控制台打印 `⚠ 设置项正在使用现场覆盖配置 ...` |
| 写错了 | 控制台打印 `⚠ 现场覆盖配置 ... 有误，继续使用<内置配置 / 上一份正确的覆盖配置>：<原因>`，服务照常运行 |
| 撤销 | 删掉这个文件，自动恢复内置配置 |
| 查看状态 | `scansvc-tools.bat config`：`source` 是 `builtin`（内置）还是 `override`（覆盖），写错了会显示原因 |
| 事后 | **改好的内容同步回 `scanopt/default_options.jsonc`**，补上回归用例（第 6 节），发版后删掉现场文件 |

服务**不会自动生成**这个文件。升级 scansvc 时现场文件也不会被动，所以覆盖文件一直留着的话，
新版本内置配置里的改进在这个现场就不生效——这正是要尽快同步回源码、删掉现场文件的原因。

### 1.2 看效果

```powershell
scansvc-tools.bat options "设备名"
```

**不需要设备连着**：服务缓存的是扫描仪的原始能力，配置变了会按新配置重新拼。
只有配置里新加的项用到了之前没读过的能力时，那一项要等下次连上设备（扫描一次）才出现。

---

## 2. 文件格式

标准 JSON，额外允许：

- `//` 行注释、`/* */` 块注释
- 数组 / 对象最后一项后面多一个逗号

**字段名写错会报错**（比如把 `label` 写成 `lable`），不会被悄悄忽略。

```jsonc
{
  "version": 1,          // 配置格式版本，固定写 1
  "options": [ ... ],    // 设置项，顺序 = 前端显示顺序 = 下发顺序
  "profiles": [ ... ]    // 个别扫描仪的特殊处理，可以没有
}
```

---

## 3. 每个设置项的公共字段

| 字段 | 必填 | 说明 |
|---|---|---|
| `key` | ✔ | 标识。前端存配置方案、发回服务端都用它。**发布后不要改名**，否则用户存的方案对不上 |
| `label` | ✔ | 显示给用户的名字 |
| `kind` | ✔ | 类型：`enum` / `number` / `switch` / `combo`，见第 4 节 |
| `group` | | `basic`（默认）或 `advanced`。前端可以据此分"基础 / 高级"区 |
| `control` | | 前端控件，每种 kind 能选的不一样，见第 4 节 |
| `disabled` | | `true` 时不给前端，但**照样检查写法**。临时下线某项、或者放示例用 |

**能力（cap）的写法**：`twain.h` 里的宏名，如 `"ICAP_PIXELTYPE"`；或者编号，如 `"0x0101"`、`"257"`。
厂商自定义能力（`0x8000` 以上）没有宏名，写编号。不知道设备支持哪些能力、报的是什么值，先导出看看：

```powershell
scansvc-tools.bat dump "设备名"
```

**不用按型号删项**：扫描仪不支持的能力、不支持的选项，服务自动不给前端。
配置里写全集就行，每台扫描仪只会看到自己支持的那部分。

---

## 4. 四种类型

### 4.1 `enum` —— 一个能力的几个编号 → 下拉框 / 单选组

适合：能力的取值是几个固定编号，每个编号有明确含义。例：颜色模式、纸张尺寸、旋转。

| 字段 | 必填 | 说明 |
|---|---|---|
| `cap` | ✔ | 对应的能力 |
| `control` | | `select`（默认）或 `radio` |
| `choices` | ✔ | 选项列表，**顺序就是下拉框里的顺序**。每项：`code` TWAIN 编号、`value` 发给前端的取值（字符串或数字）、`label` 显示文字 |
| `showUnknown` | | `true` 时设备报了 `choices` 里没写的编号也列出来，取值是 `code<编号>`（如 `code60`） |
| `unknownLabel` | | 上面那种选项的显示文字，`{code}` 会换成编号。默认 `其他（{code}）` |

```jsonc
// 示例：颜色模式，单选按钮
{
  "key": "colorMode",
  "label": "颜色模式",
  "kind": "enum",
  "cap": "ICAP_PIXELTYPE",
  "control": "radio",
  "choices": [
    { "code": 0, "value": "bw",    "label": "黑白" },
    { "code": 1, "value": "gray",  "label": "灰度" },
    { "code": 2, "value": "color", "label": "彩色" }
  ]
}
```

```jsonc
// 示例：旋转，下拉框，放在高级设置
{
  "key": "rotate",
  "label": "旋转",
  "group": "advanced",
  "kind": "enum",
  "cap": "ICAP_ORIENTATION",
  "choices": [
    { "code": 0, "value": "none",   "label": "不旋转" },
    { "code": 1, "value": "rot90",  "label": "旋转 90°" },
    { "code": 2, "value": "rot180", "label": "旋转 180°" },
    { "code": 3, "value": "rot270", "label": "旋转 270°" }
  ]
}
```

**常见改动：**
- **加一个下拉选项**：在 `choices` 里加一行 `{ "code": 编号, "value": "取值", "label": "文字" }`
- **去掉一个下拉选项**：删掉那一行（设备支持也不会再给前端，前端发来也会被拒绝）
- **调整顺序**：调 `choices` 里的行序
- **改显示文字**：改 `label`；`value` 别动

规则：同一项里 `code` 不能重复、`value` 不能重复。

### 4.2 `number` —— 一个数值能力 → 滑块 / 下拉档位 / 数字框

适合：能力是连续的数值。例：亮度、对比度、阈值、分辨率。

| 字段 | 必填 | 说明 |
|---|---|---|
| `cap` 或 `caps` | ✔ | 二选一。`caps` 写多个能力时：**读第一个，写的时候一起写**（分辨率 X/Y 要成对设） |
| `control` | | 见下表，默认 `auto` |
| `presets` | | 常用档位。设备报一个大区间时，下拉框只列落在区间里的这些值 |
| `choiceLabel` | | 下拉选项的显示文字，`{value}` 换成数值，如 `"{value} dpi"`。默认只显示数字 |
| `unit` | | 单位，前端显示在控件旁边 |

扫描仪报数值能力有三种形态：一个**区间**（如 -1000~1000，步长 1）、**固定几档**（如 200/300/600）、**只有一个值**。
`control` 决定每种形态画成什么：

| control | 设备报区间 | 设备报固定几档 | 设备只报一个值 |
|---|---|---|---|
| `auto`（默认） | 有 `presets` → 下拉；没有 → 滑块 | 下拉 | 数字框 |
| `slider` | 滑块 | 同 auto | 同 auto |
| `select` | 下拉（`presets` 里在区间内的）；没有可用档位 → 滑块 | 下拉 | 下拉（只有一项） |
| `number` | 数字框（带范围） | 数字框 | 数字框 |

指定的控件和设备形态对不上时自动退回 `auto` 的做法，所以不会出现"滑块没有范围"这种情况。

```jsonc
// 示例：亮度，全部用默认
{ "key": "brightness", "label": "亮度", "kind": "number", "cap": "ICAP_BRIGHTNESS" }
```

```jsonc
// 示例：分辨率，X/Y 一起写，下拉档位
{
  "key": "dpi",
  "label": "分辨率",
  "kind": "number",
  "caps": ["ICAP_XRESOLUTION", "ICAP_YRESOLUTION"],
  "control": "select",
  "presets": [150, 200, 300, 600],
  "choiceLabel": "{value} dpi",
  "unit": "dpi"
}
```

```jsonc
// 示例：黑白阈值，强制滑块，高级设置
{
  "key": "threshold",
  "label": "黑白阈值",
  "group": "advanced",
  "kind": "number",
  "cap": "ICAP_THRESHOLD",
  "control": "slider"
}
```

**常见改动：**
- **分辨率下拉多加一档 / 少一档**：改 `presets`（设备报固定档位时以设备为准，`presets` 不起作用）
- **滑块改成数字框**：`"control": "number"`

取值校验：区间要在范围内且对得上步长；固定档位要在档位里。不合法会返回类似 `分辨率 250 dpi 不符合步长 50` 的提示。

### 4.3 `switch` —— 开关

适合：能力只有开 / 关两种状态。例：自动纠偏、自动裁切。

| 字段 | 必填 | 说明 |
|---|---|---|
| `cap` | ✔ | 对应的能力 |
| `on` | | "开"对应的能力值，默认 `1` |
| `off` | | "关"对应的能力值，默认 `0` |
| `control` | | 只能是 `switch`，可以不写 |

前端拿到的 `value` 是 `true` / `false`。

```jsonc
// 示例：自动纠偏（布尔能力）
{
  "key": "autoDeskew",
  "label": "自动纠偏",
  "group": "advanced",
  "kind": "switch",
  "cap": "ICAP_AUTOMATICDESKEW"
}
```

```jsonc
// 示例：能力本身是数字，开 = -1、关 = -2
{
  "key": "discardBlank",
  "label": "去空白页",
  "kind": "switch",
  "cap": "ICAP_AUTODISCARDBLANKPAGES",
  "on": -1,
  "off": -2
}
```

### 4.4 `combo` —— 每个选项同时设几个能力 → 下拉框 / 单选组

适合：用户眼里的一个选择，在扫描仪那边要**同时设好几个能力**，或者某个选项**只有特定设备才有**。
例：扫描方式 = 送纸器开关 + 双面开关。

| 字段 | 必填 | 说明 |
|---|---|---|
| `control` | | `select`（默认）或 `radio` |
| `choices` | ✔ | 选项列表，顺序即显示顺序。每项见下 |

每个选项：

| 字段 | 必填 | 说明 |
|---|---|---|
| `value` / `label` | ✔ | 同 enum |
| `set` | ✔ | 选中这一项时要设的能力，**按顺序下发**。每条 `{ "cap": 能力, "value": 数值, "optional": 可选 }` |
| `requires` | | 设备满足这些条件才给这个选项。每条 `{ "cap": 能力, "op": 比较, "value": 数值 }` |

- `set` 里 `optional: true` 的：设备没有这个能力（或不接受这个值）就跳过，不影响这个选项。
  其余的：设备没有这个能力，这个选项就不给前端。
- `op` 可以是 `==` `!=` `>` `>=` `<` `<=`，以及 `supported`（设备有这个能力）、`unsupported`（没有），后两个不用写 `value`。
- **当前选中哪一项**：服务把每个选项的 `set` 和设备当前值逐个比，第一个完全对上的就是。都对不上时 `value` 是 `null`。
  所以几个选项的 `set` 要能互相区分（比如单面和双面都写上 `CAP_DUPLEXENABLED`）。

```jsonc
// 示例：扫描方式
{
  "key": "source",
  "label": "扫描方式",
  "kind": "combo",
  "control": "select",
  "choices": [
    {
      "value": "flatbed",
      "label": "平板",
      "set": [{ "cap": "CAP_FEEDERENABLED", "value": 0 }]
    },
    {
      "value": "adf",
      "label": "进纸器单面",
      "set": [
        { "cap": "CAP_FEEDERENABLED", "value": 1 },
        { "cap": "CAP_DUPLEXENABLED", "value": 0, "optional": true }
      ]
    },
    {
      "value": "adfDuplex",
      "label": "进纸器双面",
      "set": [
        { "cap": "CAP_FEEDERENABLED", "value": 1 },
        { "cap": "CAP_DUPLEXENABLED", "value": 1 }
      ],
      "requires": [{ "cap": "CAP_DUPLEX", "op": "!=", "value": 0 }]
    }
  ]
}
```

```jsonc
// 示例：去空白页做成三选一（单个能力的几个特殊值也可以用 combo）
{
  "key": "blankPage",
  "label": "空白页",
  "group": "advanced",
  "kind": "combo",
  "control": "radio",
  "choices": [
    { "value": "keep",    "label": "保留",     "set": [{ "cap": "ICAP_AUTODISCARDBLANKPAGES", "value": -2 }] },
    { "value": "auto",    "label": "自动去除", "set": [{ "cap": "ICAP_AUTODISCARDBLANKPAGES", "value": -1 }] },
    { "value": "small",   "label": "小于 2KB 视为空白", "set": [{ "cap": "ICAP_AUTODISCARDBLANKPAGES", "value": 2048 }] }
  ]
}
```

**enum 和 combo 怎么选**：一个能力、编号和选项一一对应 → `enum`（写起来短，还支持 `showUnknown`）；
一个选项要动多个能力、或者要按条件显示 → `combo`。

---

## 5. profiles —— 个别扫描仪单独处理

通用配置对某台扫描仪不合适时用。**能用通用配置解决的别加 profile。**

| 字段 | 必填 | 说明 |
|---|---|---|
| `name` | ✔ | 说明用，出现在日志和 `/api/scanner-options/config` 里 |
| `match` | ✔ | 设备名包含其中任意一个关键字（不区分大小写）就命中 |
| `hide` | | 这类设备上不给前端的 key |
| `options` | | 写法和第 4 节完全一样。**key 相同的替换**通用定义（位置不变），**key 不同的追加**到末尾 |

多个 profile 都命中时按顺序叠加。

```jsonc
"profiles": [
  // 示例：这个型号驱动报 50~1200 的区间，实际只有 200 / 300 能用；且不显示对比度
  {
    "name": "Uniscan Q400",
    "match": ["Q400"],
    "hide": ["contrast"],
    "options": [
      {
        "key": "dpi",
        "label": "分辨率",
        "kind": "number",
        "caps": ["ICAP_XRESOLUTION", "ICAP_YRESOLUTION"],
        "control": "select",
        "presets": [200, 300],
        "choiceLabel": "{value} dpi",
        "unit": "dpi"
      }
    ]
  },
  // 示例：柯达的机器额外给一个厂商自定义能力做的开关
  {
    "name": "KODAK 系列",
    "match": ["KODAK", "Alaris"],
    "options": [
      { "key": "kodakColorDrop", "label": "滤色", "group": "advanced", "kind": "switch", "cap": "0x8123" }
    ]
  }
]
```

---

## 6. 适配一台新扫描仪的步骤

1. **导出能力**：接上扫描仪，关掉厂商扫描工具，双击 `scansvc-tools.bat` 选"导出扫描仪能力"
   （或 `scansvc-tools.bat dump "设备名"`），exe 旁边 `capdump\` 下会生成 `<设备名>_<时间>.json`。
2. **看现状**：文件末尾的 `options` 是按当前内置配置拼出来的结果，缺什么、哪个不对一目了然。
   对照 `caps` 里对应能力的 `raw`（`container` 是区间 / 枚举 / 单值，`items` 是可选值）。
3. **加回归用例**：把 capdump 文件拷到源码 `scanopt/testdata/devices/`（文件名随意，建议 `<设备名>.json`）。
4. **改配置**：改 `scanopt/default_options.jsonc`。通用规则的问题改 `options`，只有这台机器的问题加 `profiles`。
5. **生成期望结果并核对**：
   ```powershell
   cd TWAIN-Samples\Twain_App_sample01\src\scansvc
   go test ./scanopt -update     # 为每台设备生成 / 更新 <文件名>.expected.json
   go test ./scanopt             # 确认全部通过
   ```
   **逐项看一遍新设备的 `.expected.json`**：选项是不是这台机器真有的、文字对不对、当前值对不对。
   同时看 `git diff`：**其它设备的 `.expected.json` 如果也变了，要确认是有意的**，否则就是这次改动把已适配的机型改坏了。
6. **真机确认**：编译运行，前端（或 `setScannerOptions`）实际设一遍、扫一张，确认扫描仪接受。
7. 配置、capdump、`.expected.json` 一起提交。

以后任何人改内置配置，`go test ./scanopt` 都会拿 `testdata/devices` 下所有设备跑一遍，
哪台机器的设置项变了会直接报出来，并打印新的输出。

> 现场应急覆盖（1.1）改过的内容，也按上面第 3~7 步回到源码。

## 7. 常见报错

| 报错 | 原因 |
|---|---|
| `配置不是合法的 JSON: 第 N 行: ...` | 语法错：少了逗号 / 引号 / 括号 |
| `json: unknown field "xxx"` | 字段名拼错，或者把某种 kind 专用的字段写到了别处 |
| `options[3]（dpi）: cap: 不认识的能力 "ICAP_XRES"` | 能力名写错，对照 `twain.h` 或 capdump 里的 `name` |
| `options[1]（a）: key 重复` | 两项 key 一样 |
| `choices[2]: 缺少 code（TWAIN 里的编号）` | enum 的选项没写 `code` |
| `set / requires 是 combo 才有的，enum 用 code` | kind 和写法对不上 |
| `control 只能是 select / radio` | 这种 kind 不支持这个控件 |
| `go test` 报 `设备 xxx 的设置项和 xxx.expected.json 不一致` | 配置改动影响了这台已适配设备的输出。有意的就 `-update` 并检查 diff，否则是改坏了 |
| 前端没出现某项，配置加载也 `ok` | 设备不支持这个能力，或者所有选项都被设备排除了；看 capdump 里有没有这个能力 |
| 扫描时报 `扫描仪拒绝了这个设置（ICAP_XXX = 1：扫描仪拒绝了这个取值）` | 设备报的是单值，服务没法预先判断，实际设不上。按 capdump 调整选项或加 profile |

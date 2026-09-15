# TODO：RFID 读卡 与 条码打印

scansvc 目前只做扫描，这两块还留在 C# 的 zhxserver 里。因为 **zhxserver 和 scansvc
都监听 5000**（`zhxscannew/zhxserver/Socket/MySocket.cs:39  this.wsport = 5000`），
两者没法并存——想让 scansvc 顶掉整个 zhxserver，就得把这两块也搬过来。

本文件是搬之前的摸底，不是设计文档。**动手前先跑通现有的扫描链路**。

## 1. 谁在用

前端 `court-document-processing` 这几处发这三个指令：

| 指令 | 前端文件 |
|---|---|
| `codePrintList` / `codePrint` | `src/controller/list/index/prints.js`<br>`src/controller/searchRfid/index/prints.js`<br>`src/controller/boxManager/Minxins/boxTagMx.js`<br>`src/controller/warehouseManager/Minxins/print_one.js` |
| `rfidRead` | `src/controller/searchRfid/`、`boxManager/`、`warehouseManager/` 等 |

scansvc 现在收到它们会回一句明确的说明（`legacy.go` 的 `rfidRead` / `codePrintList`
/ `codePrint` 分支），不是静默失败——前端会看到"本服务只提供扫描功能……"。

## 2. 协议（照抄 zhxserver，不能改）

三条都走同一条 WebSocket，回显整个请求对象再补 `code` / `data` / `message`，
和 `scan` 一样的套路。

### rfidRead

```
请求  {"handle":"rfidRead"}
成功  {"handle":"rfidRead","code":0,"data":{"epc":"...","user":"..."}}
失败  {"handle":"rfidRead","code":-1,"message":"..."}
```

失败文案要一字不改地照搬，前端可能在判它：

- `电子标签读取数据错误。请确认是否正确连接发卡器`
- `未读取到电子标签，请检查电子标签是否在读卡器识别范围内，或电子标签是否损坏`

### codePrintList

```
请求  {"handle":"codePrintList"}
成功  {"handle":"codePrintList","code":0,"data":["打印机名1","打印机名2"]}
```

前端拿 `data[0]` 当默认值填进下拉框（`prints.js` 的 `wsOnMsg`）。

### codePrint

```
请求  {"handle":"codePrint","select":"打印机名","img":"<base64 图片>"}
响应  无
```

**`codePrint` 不回任何响应**（`MySocket.cs:294` 那个 case 里 `break` 前没有
`SendMessage`），前端也不等——发完就关对话框、自己复位 loading。照做，别多回一条，
和 `show_setting` 同理：多回的那条会被前端当成别的东西去解析。

## 3. RFID 这块要移植什么

zhxserver 侧：`zhxserver/RFIDReader/`，共约 835 行有效代码。

| 文件 | 行数 | 作用 |
|---|---|---|
| `RFIDReader.cs` | 548 | 开串口、盘点循环、重试/复位 |
| `CCommondMethod.cs` | 287 | 协议帧的拼装与解析 |
| `InventoryBuffer.cs` | 123 | 盘点结果缓冲 |
| `ReaderSetting.cs` | 64 | 读卡器参数 |
| `Rdb.cs` / `OperateTagBuffer.cs` / `RfidData.cs` | 76 | 数据结构 |

要点：

- **是裸串口，不是厂商 DLL**。`reader.OpenCom(port, 115200, out ex)`，波特率 115200；
  先试 `COM3`，不行就把 `SerialPort.GetPortNames()` 拿到的口挨个试一遍。
  这意味着协议得自己移植（`CCommondMethod.cs` 那 287 行），**没有 DLL 可以直接 cgo 调**。
- Go 侧要引串口库（`go.bug.st/serial` 或 `tarm/serial`）——这是 scansvc 的**第一个
  非 gorilla/websocket 依赖**，要 vendor 进来。
- 读一次的流程是 `ReadRFIDLoop()` → `getRfidData()`，失败时 `reset()` + 重开串口重来一次。
  这套重试逻辑是有原因的（读卡器容易卡），别简化掉。
- 拿不到实物读卡器就没法验证。**动手前先确认手上有设备能测**，否则移植完也只是"看着像对的"。

估：这块是大头。协议移植 + 串口调试，乐观 2~3 天，读卡器行为不稳的话更久。

## 4. 条码打印这块要移植什么

zhxserver 侧：`zhxserver/Zebra/`，共约 425 行。

| 文件 | 行数 | 作用 |
|---|---|---|
| `ZPLII.cs` | 210 | ZPL II 指令生成 |
| `RawPrinterHelper.cs` | 127 | 往打印机直发原始字节 |
| `ZebraPrint.cs` | 66 | 枚举打印机、打印入口 |
| `ZplConvert.cs` | 22 | `BitmapToZPLII(Bitmap, posX, posY)`，位图转 `^GF` 图形域 |

要点：

- **枚举打印机**：C# 用 `PrinterSettings.InstalledPrinters`（`ZebraPrint.cs:25`）。
  Go 侧对应 `winspool.drv` 的 `EnumPrinters`，得自己写 syscall 封装。
- **直发原始数据**：`RawPrinterHelper.cs` 就是一串 `winspool.Drv` 的 P/Invoke——
  `OpenPrinter` → `StartDocPrinter` → `StartPagePrinter` → `WritePrinter` →
  收尾三个 End/Close。Go 侧照着翻成 `syscall.NewLazyDLL("winspool.drv")` 即可，
  是纯机械工作，没有坑。
- **图片转 ZPL**：前端传的是 base64 图片，要解码成位图、转成 ZPL 的 `^GF` 图形域。
  `ZplConvert.cs` 只有 22 行，逻辑简单（逐行扫黑白像素拼十六进制），
  但 Go 这边要自己解码 png/jpeg（标准库有）再走同样的转换。
- 这块**不需要新依赖**，也不需要实物打印机就能先验证一半：生成的 ZPL 文本可以直接比对，
  或者用 Labelary 之类的在线 ZPL 预览确认版面。

估：1~2 天，比 RFID 稳妥得多。

## 5. 建议的顺序

1. 先把扫描这条主链路在生产上跑稳几天
2. 再做**条码打印**——工作量小、能离线验证、不加依赖
3. 最后做 **RFID**——依赖实物设备，风险集中在串口协议上

两块都做完，zhxserver 才能真正下线。在那之前，要用 RFID / 条码打印就只能跑 zhxserver，
而它和 scansvc 抢 5000，两者只能二选一。

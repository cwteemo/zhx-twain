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

- `-addr :8000`  监听地址
- `-dir scans`   图片保存根目录（每次扫描一个时间戳子目录）

启动后浏览器打开 <http://localhost:8000> 即可点按钮跑通闭环。

## 3. HTTP 接口

```
GET  /api/devices
  → {"devices":["Scanner A","Scanner B"],"count":2}

POST /api/scan          {"device":"Scanner A","count":1}
  → {"success":true,"count":1,
     "images":[{"id":"...","name":"xxx.bmp","size":11220054,"url":"/api/image?id=..."}]}
  count = 0 表示走送纸器一直扫到没纸

GET  /api/image?id=xxx  → 图片字节流
```

跨域已开（`Access-Control-Allow-Origin: *`），业务系统可以从别的域名直接调。

## 4. 设计要点

**TWAIN 单线程模型**是整个服务的地基。DLL 里的 `EnableDS()` 会在调用线程上跑 `GetMessage` 消息泵等待数据源事件，而数据源把事件投递到"打开它的那条线程"的消息队列。所以：

- `twain.go` 启动时开一条**专属 goroutine**，`runtime.LockOSThread()` 后**永不解锁**，独占一条 OS 线程
- 所有 TWAIN 调用通过 `inTwain()` 投递到这条线程**串行执行**
- HTTP handler 里**绝不能**直接调 `C.zhx_*`

扫描期间该线程被占满，其它请求会排队等待——这是 TWAIN 的固有限制，不是 bug。

**扫描回调**（`callback.go` 的 `goScanCallback`）由 DLL 在扫描线程上同步调用，只做文件路径登记，不做重活。

## 5. 已知限制 / 下一步

- `zhx_GetDevicesList()` 返回的是 DLL 用 `_strdup` 分配的内存，而 DLL 静态链接了自己的 CRT，Go 侧 `C.free` 会跨堆释放导致崩溃，所以当前**不释放**（每次枚举泄漏一小段）。修法是在 C 侧补一个 `zhx_FreeString` 导出。
- 图片按 BMP 原样回传，单张约 11 MB。后续应在服务端转 JPEG/PNG 再传。
- 扫描是同步阻塞的，多页扫描时 HTTP 可能超时。下一步改成"提交任务返回 taskId + WebSocket 推进度"。
- `images` 登记表只在内存里，服务重启后旧图片取不回来。

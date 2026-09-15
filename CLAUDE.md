# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

本仓库 fork 自 [twain/twain-samples](https://github.com/twain/twain-samples)（TWAIN 2.4 官方示例：一个 TWAIN 应用 + 一个纯软件虚拟扫描仪数据源），在其之上做二次开发：**给 TWAIN 应用加一层 HTTP 控制接口**，让任意语言（Go / Python / JS…）通过 HTTP 或 DLL 导出函数驱动扫描仪。

上游代码为 Windows/MSVC + MFC/Qt，**只能在 Windows 上编译运行**；在 Linux 环境下只能做阅读与修改。

## 目录结构与三条产物线

```
TWAIN-Samples/
├── common/                    # 三个工程共享：Common.h、CommonTWAIN、CTiffWriter、TwainString
├── pub/external/              # 第三方：twain.h、FreeImage.h + lib/win32|win64/FreeImage.lib
├── Twain_App_sample01/        # ★ 二次开发主战场：TWAIN 应用端
│   ├── src/                   # 与 UI 无关的 TWAIN 核心 + DLL 导出层 + 纯 Win32 HTTP 服务器
│   ├── visual_studio/         # 控制台工程 TWAIN_APP_CMD（TargetName: TWAIN_APP_CMD32/64）
│   ├── visual_studio_mfc/     # ★ MFC 工程 TWAIN_App_mfc（当前活跃开发目标）
│   ├── TWAIN_App_QT/          # Qt 版界面工程
│   └── HTTP_API_DOCUMENTATION.md  # DLL 内 HTTP 服务器（/api/*）的接口文档
└── Twain_DS_sample01/         # 虚拟扫描仪 DS（TWAINDS_FreeImage），基本未改动
releases/                      # 上游发布的 .msi 安装包
```

`src/` 与 `common/` 的源文件被 MFC 工程和 CMD 工程**同时以相对路径引用**（`..\src\TwainApp.cpp` 等），改动 `src/` 会同时影响两个工程。

## 构建

无 CMake、无自动化测试。全部通过 Visual Studio 解决方案或 qmake 构建：

```powershell
# MFC 应用（当前主要目标，产出 TWAIN_App_mfc64.exe）
msbuild TWAIN-Samples\Twain_App_sample01\visual_studio_mfc\TWAIN_APP_VS2017_mfc.sln /p:Configuration=Debug /p:Platform=x64

# 控制台/DLL 导出工程
msbuild TWAIN-Samples\Twain_App_sample01\visual_studio\TWAIN_APP_VS2017.sln /p:Configuration=Debug /p:Platform=x64

# 虚拟扫描仪数据源
msbuild TWAIN-Samples\Twain_DS_sample01\visual_studio\TWAINDS_VS2017.sln /p:Configuration=Release /p:Platform=x64
```

- 平台工具集：`v141` / `v141_xp` / `v143`；字符集 **MultiByte**（非 Unicode），但代码里仍有大量 `#ifdef _UNICODE` 分支。
- Qt 版本按上游 README：Qt 5.9.9，`QTDIR=C:\Qt\Qt5.9.9\5.9.9\msvc2017_64`；`.pro` + qmake 生成 `Makefile`（仓库里已提交的 `Makefile*` 是 qmake 产物，不要手改）。
- 运行需要系统上有 FreeImage（`src/gotwain/FreeImage.dll` 有一份）。

Go 测试程序（CGO 调用 DLL 导出函数）：

```powershell
cd TWAIN-Samples\Twain_App_sample01\src\gotwain
go run main.go        # LDFLAGS 指向 ..\..\visual_studio\Debug 下的 TWAIN_APP_CMD64
```

验证扫描功能只能靠手工：启动 MFC 应用（自带 HTTP 服务器），再用 `curl` 打接口，配合日志文件核对。

## 核心架构

### 1. TWAIN 状态机（上游代码，改动前必读）

`src/TwainApp.{h,cpp}` 是所有上层的基础，`m_DSMState` 表示 TWAIN 状态 2~7：

- `connectDSM()` → state 3（DSM 已加载，此时才能枚举设备）
- `getDataSource(index)` → 遍历设备，返回 `pTW_IDENTITY`（`ProductName` 是设备名）
- `loadDS(index)` → state 4（已打开某台扫描仪），`unloadDS()` 回到 3
- `enableDS()` / 传输流程 → state 5~7

判断"能否取设备列表"用 `m_DSMState >= 3`，"是否已连上扫描仪"用 `m_DSMState >= 4`。切换设备必须先 `unloadDS()` 再 `loadDS()`。

### 2. 两套彼此独立的 HTTP 服务器（重要）

仓库里有**两个同名 `HttpServer` 类**，实现和协议完全不同，改代码前先确认在哪一层：

| | `src/http_server.{h,cpp}` | `visual_studio_mfc/http_server.{h,cpp}` |
|---|---|---|
| 依赖 | 纯 Win32 + winsock，不依赖 MFC | MFC（`CWinThread`/`CString`/`AfxBeginThread`） |
| 调用方式 | 直接调用 `zhx_*` C 接口 | `PostMessage` 给 MFC 主对话框，由 UI 线程执行 TWAIN |
| 路由 | REST 风格 `/api/devices`、`/api/scan`、`/api/scan/batch`、`/api/status` | `/scanners` 或 `?handle=scanners` / `?handle=getScannerOptions&scanner=<名称>` |
| 文档 | `HTTP_API_DOCUMENTATION.md` | 无，看 `ParseHttpRequest()` 里的 `requestType` 判定 |
| 响应体 | `{"devices":[...]}` 等 | 统一 `{"errorCode":0,"msg":"...","data":{...}}` |

两者都监听 8080，都带 `Access-Control-Allow-Origin: *`，每个连接开一个线程处理。

### 3. DLL 导出层（`src/main.cpp` + `main.h` + `exports.def`）

给非 C++ 语言用的 C 接口，全部 `zhx_` 前缀：`zhx_Init` / `zhx_LoadDS` / `zhx_GetDevicesList` / `zhx_ScanComplete` / `zhx_Cleanup`、事件循环相关的 `zhx_ProcessEvent` / `zhx_GetDSMessage` / `zhx_EnableDS` / `zhx_HandleScanReady`，以及 `zhx_HttpServer_Start/Stop/IsRunning/GetPort`（实现在 `src/http_server.cpp`）。

调用顺序固定：`zhx_Init()` → `zhx_HttpServer_Start(port)` → … → `zhx_HttpServer_Stop()` → `zhx_Cleanup()`。TWAIN/Win32 GUI 要求固定线程，Go 侧必须 `runtime.LockOSThread()`。

### 4. MFC 侧：HTTP 线程 ↔ UI 线程的窗口消息协议

TWAIN 调用必须回到 UI 线程，因此 HTTP 工作线程只做解析，然后 `PostMessage` 给 `CmfcDlgMain`：

- 请求参数被 `buildJSON()` 拼成 JSON，`GlobalAlloc(GMEM_MOVEABLE)` 分配后把 `HGLOBAL` 放进 `WPARAM`。
- `CmfcDlgMain::OnHttpRequest` 解析 JSON（`ExtractJsonValue` / `ExtractJsonParam` 是手写的极简解析），取设备列表后写回两处：`m_httpServer->SetLastResponse()`（HTTP 线程下次读缓存）+ `AfxGetApp()->WriteProfileString(_T("Settings"), _T("LastScannerList"), ...)`（注册表备份）。
- `CmfcDlgMain::OnConnectScanner` 收到扫描仪名称（UTF-8 字符串，`WPARAM` 传 `HGLOBAL`），按 `ProductName` 匹配后 `loadDS()`。
- HTTP 响应是**异步凑出来的**：`GetScannerListResponse()` 返回的是上一次 UI 线程缓存的列表（`lastResponse` 全局变量 → 注册表备份 → 空数组），并非本次请求的实时结果。首次请求通常拿到空列表。

设备列表在 UI 线程侧的中间格式是 `SCANNERS:[0]名称;[1]名称;`，由 HTTP 线程再转成 JSON。

### 5. 日志

`src/Logger.{h,cpp}`：`Logger::Log(fmt, ...)` 写进程工作目录下的 `twain.log`（追加、带时间戳、mutex 保护）。这是跨线程调试唯一手段，新增流程请沿用它，不要用 `printf`。

## 已知不一致（动这些文件时注意）

- **自定义消息号冲突**：`http_server.h` 定义 `WM_HTTP_REQUEST = WM_USER+101`，而 `http_server.cpp` 顶部又重定义为 `WM_USER+100`（= `WM_RECEIVE_DATA`）。`mfcDlgMain.cpp` 里 `ON_MESSAGE` 注册的是头文件的 101，服务器 `PostMessage` 发的是 100，消息实际落到 `OnReceiveData`，而后者把 `HGLOBAL` 当 `CString*` 解引用。`WM_CONNECT_SCANNER = WM_USER+102` 两边一致，所以连接扫描仪的链路是通的。改消息号时要同步 `http_server.h`、`http_server.cpp`、`mfcDlgMain.h`、`AfxMainChannel.h` 四处。
- **8080 端口被启动两次**：`Cmfc32App::InitInstance`（`mfc.cpp`）用成员 `m_httpServer` 启一次，`CmfcDlgMain::OnInitDialog` 又 `new HttpServer()` 启一次，未设 `SO_REUSEADDR`，第二次 bind 会失败。
- **CMD 工程输出类型与代码意图不符**：`visual_studio/TWAIN_APP_VS2017.vcxproj` 四个配置都是 `<ConfigurationType>Application</ConfigurationType>`（控制台 exe），也没引用 `exports.def`；但 `main.h` 全是 `__declspec(dllexport)`，`src/main.cpp` 的 `_tmain()` 第一句就 `return`，`src/gotwain/` 下还放着编译好的 `TWAIN_APP_CMD64.dll`/`.lib` —— 说明实际是按 DLL 在用，改成 DLL 的配置没有提交。已编译的那份 DLL 只导出 9 个函数，缺 `zhx_ScanComplete` / `zhx_GetDevicesList` / `zhx_HttpServer_*`，是旧版本。
- **游离文件**：`AfxMainChannel.{h,cpp}`（引用了不存在的 `IDD_YOUR_DIALOG`）和 `src/http_server.cpp` 都没有被加入对应的 `.vcxproj`；新增源文件必须手工加进 vcxproj 的 `ClCompile`/`ClInclude`。

## 代码约定

- 上游文件是 ISO-8859/CRLF，二次开发新增的文件（`http_server`、`Logger`、`mfcDlgMain` 等）是 UTF-8 + 中文注释；`grep` 上游文件时可能需要 `-a`，编辑时保持各文件原有编码和 CRLF。
- 新代码注释用中文，与现有二次开发部分保持一致；上游文件里的英文注释与 BSD 版权头不要动。
- MFC 侧大量使用手动 `m_mutex.lock()/unlock()` 而非 `lock_guard`（源码注释说明是为规避死锁），沿用即可。
- 提交信息沿用现有风格：`modify <中文描述>`。

# 编译说明（build.bat）

一条命令编完 DLL + 扫描服务，并把要交付的文件打包到 `dist\`。

```powershell
cd D:\code\twain\zhx-twain
build.bat
```

跑完看到 `done -> ...\dist` 就成功了，把 `dist\` 整个目录拷到操作员机器上即可。

---

## 1. 编译机上要装什么

| 依赖 | 说明 | 怎么确认 |
|---|---|---|
| Visual Studio 2017 或更新 | 要勾选 **"使用 C++ 的桌面开发"** 工作负载。脚本用 `vswhere` 自动找 MSBuild，不用开"开发人员命令提示" | `build.bat dll` 能跑起来 |
| Go 1.19+ | 编扫描服务 | `go version` |
| **64 位** MinGW-w64 gcc | cgo 要用。装成 32 位的编译不报错、链接才炸，脚本会提前拦住 | `gcc -dumpmachine` → `x86_64-w64-mingw32` |

`TWAINDSM.dll` 不在仓库里，由 `releases\Twain_App_sample01_*\twainapp.win64.installer.msi` 安装。
装完它一般在 `C:\Windows\twain_64\`，脚本会自动从那里取；取不到会提示（**少了它一台扫描仪都枚举不到**）。

---

## 2. 命令

```powershell
build.bat                  DLL(Release|x64) + scansvc.exe（无控制台窗口）-> dist\
build.bat dll              只编 DLL
build.bat go               只编 scansvc.exe（要求 DLL 已经编过）
build.bat -debug           DLL 用 Debug 配置
build.bat -console         exe 带控制台窗口，调试时看日志方便
build.bat -notest          跳过 Go 单元测试
build.bat -out D:\deploy   换个输出目录
```

参数可以组合，例如调试用一版：

```powershell
build.bat -debug -console -out D:\test
```

---

## 3. 每一步做了什么

```
[1/3] 编 DLL          MSBuild 编 TWAIN_APP_VS2017.sln（x64），
                      产物 TWAIN_APP_CMD64.dll / .lib 拷到 src\scansvc\
[2/3] 跑测试          go test ./imgfmt ./scanopt
[3/3] 编 scansvc.exe  go build（默认 -ldflags "-H=windowsgui"）
      打包            拷到 dist\
```

- **DLL 的 .lib 为什么要拷到 `src\scansvc\`**：cgo 链接时要用它（`twain.go` 里
  `#cgo LDFLAGS: -L${SRCDIR} -lTWAIN_APP_CMD64`）。它只在编译时用，**不需要随 exe 交付**。
- **测试只跑 `imgfmt` 和 `scanopt` 两个包**：它们是纯 Go 的。主包要连 DLL，编译机上没接扫描仪，测不了。
  这两个包恰好是最容易出静默错误的地方（TIFF 的 LZW 码流、设置项配置解析），所以默认跑。
- **默认不带控制台窗口**：双击就缩到托盘。日志写在 exe 旁边的 `scansvc.log`。
  调试想直接看日志就加 `-console`。

任何一步失败都会停下并打印原因，不会带着半成品继续往下走。

---

## 4. dist\ 里有什么

| 文件 | 说明 |
|---|---|
| `scansvc.exe` | 服务本体。设置项配置和测试页都编译在里面 |
| `TWAIN_APP_CMD64.dll` | TWAIN 封装层，运行时要 |
| `FreeImage.dll` | DLL 存图要用 |
| `TWAINDSM.dll` | TWAIN 数据源管理器，**没有它枚举不到扫描仪** |
| `scansvc-tools.bat` / `.ps1` | 现场维护工具，双击有菜单 |

**不在 dist 里的**：`TWAIN_APP_CMD64.lib`（只在编译时用）、`scansvc.conf`（首次运行自动生成）、
`scanner-options.jsonc`（设置项配置已编进 exe，只在应急覆盖时才需要）。

`dist\` 已经加进 `.gitignore`，不会进仓库。

---

## 5. 部署到操作员机器

第一次：把 `dist\` 整个目录拷过去（比如 `D:\data\scansvc\`），双击 `scansvc.exe`。

升级：**只覆盖 `dist\` 里的那几个文件**，目标机器上这些要留着：

- `scansvc.conf` —— 端口、图片目录等本机配置
- `scans\` —— 扫出来的图
- `cache\` —— 设置项缓存（删了也行，扫一次会重建）
- `scanner-options.jsonc` —— 如果现场做过应急覆盖

开机自启：`scansvc-tools.bat` → 菜单里的"开机自启设置"。

---

## 6. 换图标

图标是 `src\scansvc\favicon.ico`，一个文件管三处：exe 图标、托盘图标、测试页站点图标。

换了图标之后要重新生成资源文件（`build.bat` 会在 `.syso` 不存在时自动生成，
但图标改了不会自动察觉，得手工跑一次）：

```powershell
cd TWAIN-Samples\Twain_App_sample01\src\scansvc
go run ./tools/mkicon favicon.ico rsrc_windows_amd64.syso
```

生成的 `rsrc_windows_amd64.syso` 要一起提交。`go build` 会自动带上同目录下的 `.syso`，
不需要任何额外工具（这也是自己写 `tools/mkicon` 而不用 `rsrc` 之类工具的原因：现场机器常常没网）。

---

## 7. 常见问题

| 现象 | 原因 / 处理 |
|---|---|
| `[ERROR] MSBuild not found` | VS 没装 C++ 工作负载，或装的是不带 vswhere 的老版本。也可以从"VS 开发人员命令提示"里跑这个脚本 |
| `C2220 以下警告被视为错误` | 旧版仓库里 Release 开着 `/WX`，已经关掉，拉最新代码即可 |
| `[ERROR] gcc is "...", not 64-bit` | 装的是 32 位 MinGW。装 MinGW-w64 x86_64，并把它的 `bin` 放在 PATH 前面 |
| `[ERROR] ...TWAIN_APP_CMD64.lib is missing` | 先 `build.bat dll` |
| `MISSING TWAINDSM.dll` | 装 `releases\` 下的 msi，或从别的机器上拷一份放进 `dist\` |
| 编出来的 exe 双击没反应 | 正常：默认没有窗口，图标在右下角托盘（可能被折叠进"显示隐藏的图标"）。日志看 `scansvc.log` |
| 服务起来了但扫描仪列表是空的 | `TWAINDSM.dll` 不在 exe 旁边，或驱动没装、设备没接、只有 32 位驱动 |

# twaindbg —— TWAIN DLL 交互式调试工具

思路承自 `CMD-twain` 分支的 `gotwain/main.go`：**先用命令行把 DLL 逐个接口打通，再谈上层服务**。
区别是这里把 `exports.def` 里的 20 个导出全接上了，每步打印返回值和耗时，出错能定位到具体是哪个 TWAIN 调用挂的。

## 准备

把三个文件拷到本目录（`.gitignore` 已排除，不进仓库）：

```
TWAIN_APP_CMD64.dll     运行期
TWAIN_APP_CMD64.lib     链接期（导入库）
FreeImage.dll           运行期
TWAINDSM.dll            运行期（TWAIN 数据源管理器）
```

`TWAINDSM.dll` 由 `releases/Twain_App_sample01_*/twainapp.win64.installer.msi` 安装（该 MSI 合入了
`pub/external/bin/win64/TWAINDSM64.msm`）。装完如果它落在 `C:\Windows\twain_64\`，**必须拷到 exe 旁边**——
代码里是裸 `LoadLibraryA("TWAINDSM.dll")`，只搜 exe 目录/系统目录/PATH，`twain_64` 不在其中。

DLL 从 `visual_studio/TWAIN_APP_VS2017.sln`（Debug|x64）生成。

## 运行

```powershell
set CGO_ENABLED=1
go build -o twaindbg.exe .

.\twaindbg.exe                                  # 交互式
.\twaindbg.exe -auto                            # 一键跑通完整流程
.\twaindbg.exe -run "init;list;open 0;scan . 1;close;exit"   # 一次性执行
```

## 典型调试会话

```
twain> test                     ← DLL 能不能加载
twain> init                     ← TWAIN 环境 / DSM 连接
twain> list                     ← 枚举设备，带序号
  [0] Brother DS-620
  [1] TWAIN2 FreeImage Software Scanner
twain> open 0                   ← 按序号打开，不用手打名称
twain> caps                     ← 看这台设备支持哪些能力
twain> res                      ← 当前分辨率 + 支持列表
twain> res 300                  ← 设成 300 dpi
twain> scan ./out 1             ← 扫一张，回调实时打印文件路径
twain> close
twain> exit
```

每条命令都会打印实际调用的 C 函数名和参数，日志形如：

```
→ zhx_OpenDevice("Brother DS-620")
  [zhx_OpenDevice] 耗时 1.203s
  成功，当前设备: Brother DS-620
```

## 排错提示

工具在关键失败点会直接给排查方向：

| 现象 | 提示 |
|---|---|
| `list` 返回空 | 驱动装了吗 / 设备开机了吗 / **是不是 32 位 DS**（本工具是 64 位，加载不了 32 位数据源） |
| `open` 返回 0 | 设备被其它程序占用 / 名称不对 / DSM 状态不足 |
| `scan` 返回 0 页 | 放纸了吗 / 盖板合上了吗 / 看 `twain.log` 里 `EnableDS` 之后卡在哪 |

DLL 自己会往工作目录写 `twain.log`（`@INFO` / `@WARN` / `@ERROR` 前缀），和本工具的输出对照着看。

## 设计说明

- `runtime.LockOSThread()` 钉在主线程：DLL 里 `EnableDS()` 跑的是 `GetMessage` 阻塞消息泵，数据源只往"打开它的那条线程"投递事件，换线程就收不到
- 回调 `goDbgScanCallback` 返回 `<=0` 会被 DLL 当成"用户取消扫描"，这里固定返回 1
- DLL 返回的 `char*` 一律**只读不释放**：DLL 静态链接了自己的 CRT，Go 侧 `C.free` 跨堆释放会崩。调试工具可以接受这点泄漏，正式服务应在 C 侧补 `zhx_FreeString`

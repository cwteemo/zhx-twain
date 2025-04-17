# Go TWAIN DLL 测试程序

这个Go程序用于测试TWAIN DLL的接口调用，包括基本的测试、扫描仪列表获取和扫描操作。

## 前提条件

1. Go编程语言环境（推荐1.16+）
2. Windows操作系统（因为TWAIN是Windows API）
3. 已编译的TWAIN DLL文件

## 配置

在运行前，请确保修改程序中的以下设置：

1. `dllPath`变量 - 设置为实际的DLL文件路径:
   ```go
   const dllPath = "L:/code/twain/zhx-twain/TWAIN-Samples/Twain_App_sample01/visual_studio_mfc/Debug/TWAIN_App_mfc32.dll"
   ```

2. 扫描保存路径 - 如果需要测试扫描功能，请确保以下路径可写：
   ```go
   savePath := "L:/code/twain/test_scan.tiff"
   ```

## 运行方法

1. 确保TWAIN DLL已成功编译
2. 打开命令提示符或PowerShell，导航到项目目录
3. 运行以下命令：
   ```
   go run main.go
   ```

## 测试功能

程序会按顺序测试以下功能：

1. **基本测试** - 调用`zhx_twain_test`函数，测试DLL是否正常响应
2. **TWAIN环境初始化** - 调用`zhx_twain_init`函数
3. **获取扫描仪列表** - 调用`zhx_twain_get_scanners`函数
4. **扫描测试** (可选) - 如果找到扫描仪，会询问是否执行扫描测试

## 错误排查

如果遇到以下问题：

- **无法加载DLL** - 确认DLL路径正确，并且DLL文件存在
- **函数未找到** - 确认DLL导出了所需的函数
- **扫描失败** - 检查扫描仪连接状态和TWAIN日志

## 注意事项

- DLL测试需要在有管理员权限的情况下运行
- 扫描操作可能会打开扫描仪UI，请遵循屏幕上的指示
- 程序使用了CGO和unsafe包进行C语言交互，确保了解其中的风险 
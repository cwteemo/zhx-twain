<#
scansvc 维护小工具：导出扫描仪能力、查看设置项、查看配置状态等。

不要直接双击这个文件，运行同目录的 scansvc-tools.bat。
只用 Windows 自带的 PowerShell 发 HTTP 请求，机器上没有 curl.exe 也能用。

用法（bat 会把参数原样转过来）：
  scansvc-tools.bat                     打开菜单
  scansvc-tools.bat status              服务状态
  scansvc-tools.bat devices             扫描仪列表
  scansvc-tools.bat dump [设备名]        导出扫描仪能力（不带设备名会让你选）
  scansvc-tools.bat options [设备名]     查看设置项
  scansvc-tools.bat config              设置项配置状态（内置 / 现场覆盖）
  scansvc-tools.bat builtin-config      下载内置设置项配置，存成 scanner-options.jsonc
  scansvc-tools.bat -Port 18081 status  服务改过 HTTP 端口时指定

端口的取值顺序：-Port 参数 > 环境变量 SCANSVC_HTTP_PORT > 同目录 scansvc.conf 里的 http-port > 18080
#>

[CmdletBinding()]
param(
    [string] $Command = '',
    [string] $Device = '',
    [int] $Port = 0
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

# 只连本机，明确不走代理。机器上配了代理或开着"自动检测设置"(WPAD) 时，
# PowerShell 默认会拿它去连 127.0.0.1，断网的机器上会卡住几十秒甚至直接失败。
# Windows PowerShell 5.1 看 DefaultWebProxy；PowerShell 6+ 用的是另一套客户端，要靠 -NoProxy。
[System.Net.WebRequest]::DefaultWebProxy = $null
$script:HttpArgs = @{}
if ($PSVersionTable.PSVersion.Major -ge 6) { $script:HttpArgs['NoProxy'] = $true }

if ($PSVersionTable.PSVersion.Major -lt 3) {
    Write-Host '需要 PowerShell 3.0 或更高版本（Windows 8 / Server 2012 以上自带；Win7 装 WMF 3.0 以上）。' -ForegroundColor Red
    exit 1
}

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Definition

# ---- 端口 ----

function Resolve-Port {
    if ($Port -gt 0) { return $Port }
    if ($env:SCANSVC_HTTP_PORT -and ($env:SCANSVC_HTTP_PORT -as [int])) { return [int] $env:SCANSVC_HTTP_PORT }

    # 服务的配置文件就在 exe 旁边，端口改过的话这里能读到
    $conf = Join-Path $ScriptDir 'scansvc.conf'
    if (Test-Path $conf) {
        foreach ($line in Get-Content -LiteralPath $conf -Encoding UTF8) {
            if ($line -match '^\s*http-port\s*=\s*(\d+)') { return [int] $Matches[1] }
        }
    }
    return 18080
}

$script:BaseUrl = 'http://127.0.0.1:{0}' -f (Resolve-Port)

# ---- HTTP ----

# Get-ErrorDetail 从失败响应体里取服务端给的中文说明，取不到返回空串。
function Get-ErrorDetail {
    param($ErrorRecord)
    try {
        $response = $ErrorRecord.Exception.Response
        if (-not $response) { return '' }
        $reader = New-Object System.IO.StreamReader($response.GetResponseStream())
        $body = $reader.ReadToEnd()
        $reader.Close()
        if (-not $body) { return '' }
        try {
            $json = $body | ConvertFrom-Json
            if ($json.error) { return [string] $json.error }
            if ($json.msg) { return [string] $json.msg }
        } catch { }
        return $body.Trim()
    } catch {
        return ''
    }
}

function Invoke-Api {
    param(
        [string] $Path,
        [int] $TimeoutSec = 30,
        [switch] $Raw           # 返回原文而不是解析后的对象
    )
    $url = $script:BaseUrl + $Path
    try {
        if ($Raw) {
            return (Invoke-WebRequest -Uri $url -TimeoutSec $TimeoutSec -UseBasicParsing @script:HttpArgs).Content
        }
        return Invoke-RestMethod -Uri $url -TimeoutSec $TimeoutSec @script:HttpArgs
    } catch {
        Write-Host ''
        Write-Host ('请求失败: ' + $url) -ForegroundColor Red
        # 服务端返回 4xx / 5xx 时，真正有用的原因在响应体里（{"error":"..."}）
        $detail = Get-ErrorDetail $_
        if ($detail) {
            Write-Host ('  服务返回: ' + $detail) -ForegroundColor Red
        } else {
            Write-Host ('  ' + $_.Exception.Message) -ForegroundColor Red
            Write-Host ('  先确认 scansvc.exe 已经启动，且 HTTP 端口是 ' + $script:BaseUrl + '（改过端口用 -Port 指定）') -ForegroundColor Yellow
        }
        return $null
    }
}

# ---- 各项功能 ----

function Show-Status {
    $version = Invoke-Api -Path '/version' -Raw
    if ($null -eq $version) { return }
    Write-Host ''
    Write-Host $version

    $status = Invoke-Api -Path '/api/status'
    if ($null -eq $status) { return }
    Write-Host ''
    Write-Host ('  状态   : ' + $status.stateText)
    Write-Host ('  扫描仪 : ' + $(if ($status.device) { $status.device } else { '未连接' }))
    Write-Host ('  扫描中 : ' + $(if ($status.scanning) { '是' } else { '否' }))
}

function Get-Devices {
    $res = Invoke-Api -Path '/api/devices'
    if ($null -eq $res) { return @() }
    return @($res.devices)
}

function Show-Devices {
    $devices = Get-Devices
    Write-Host ''
    if ($devices.Count -eq 0) {
        Write-Host '没有发现扫描仪。常见原因：驱动没装、TWAINDSM.dll 不在 exe 旁边、设备是 32 位数据源。' -ForegroundColor Yellow
        return
    }
    Write-Host ('发现 ' + $devices.Count + ' 台扫描仪（来自已安装的驱动，不代表设备接着）:')
    for ($i = 0; $i -lt $devices.Count; $i++) {
        Write-Host ('  [' + ($i + 1) + '] ' + $devices[$i])
    }
}

# Select-Device 让用户从列表里挑一台；已经给了设备名就直接用。
function Select-Device {
    param([string] $Name)
    if ($Name) { return $Name }

    $devices = Get-Devices
    if ($devices.Count -eq 0) {
        Write-Host '没有发现扫描仪，无法继续。' -ForegroundColor Yellow
        return $null
    }
    Write-Host ''
    for ($i = 0; $i -lt $devices.Count; $i++) {
        Write-Host ('  [' + ($i + 1) + '] ' + $devices[$i])
    }
    $answer = Read-Host '选择扫描仪编号（直接回车取消）'
    if (-not $answer) { return $null }
    $index = $answer -as [int]
    if ($null -eq $index -or $index -lt 1 -or $index -gt $devices.Count) {
        Write-Host '编号不对。' -ForegroundColor Yellow
        return $null
    }
    return $devices[$index - 1]
}

function Invoke-Dump {
    param([string] $Name)
    $device = Select-Device -Name $Name
    if (-not $device) { return }

    Write-Host ''
    Write-Host ('正在连接 ' + $device + ' 并读取它支持的全部能力，可能要几十秒，请稍候...')
    $res = Invoke-Api -Path ('/api/capabilities/dump?device=' + [uri]::EscapeDataString($device)) -TimeoutSec 300
    if ($null -eq $res) { return }

    Write-Host ''
    Write-Host ('导出成功：' + $res.caps.Count + ' 项能力') -ForegroundColor Green
    if (-not $res.supportedCapsReported) {
        Write-Host '  注意：这台设备没有报 CAP_SUPPORTEDCAPS，是逐项试读出来的，厂商自定义能力可能读不到。' -ForegroundColor Yellow
    }
    if ($res.unreadable.Count -gt 0) {
        Write-Host ('  设备列了但读不出来的: ' + ($res.unreadable -join ', '))
    }
    Write-Host ('  按当前配置能给前端 ' + $res.options.Count + ' 个设置项: ' + (($res.options | ForEach-Object { $_.key }) -join ', '))
    Write-Host ''
    Write-Host ('文件: ' + $res.file) -ForegroundColor Green
    Write-Host '把这个文件发给开发，用来适配这台扫描仪的设置项。'

    if ($res.file -and (Test-Path -LiteralPath $res.file)) {
        $open = Read-Host '打开文件所在目录？(y/N)'
        if ($open -eq 'y' -or $open -eq 'Y') {
            Start-Process explorer.exe ('/select,"' + $res.file + '"')
        }
    }
}

function Show-Options {
    param([string] $Name)
    $device = Select-Device -Name $Name
    if (-not $device) { return }

    $res = Invoke-Api -Path ('/api/scanner-options?device=' + [uri]::EscapeDataString($device)) -TimeoutSec 60
    if ($null -eq $res) { return }

    Write-Host ''
    Write-Host ('扫描仪: ' + $res.scanner)
    if ($res.connected) {
        Write-Host '数据来源: 设备当前连着，实时读取'
    } elseif ($res.cached) {
        Write-Host ('数据来源: 缓存（设备没连着），读取时间 ' + $res.updatedAt)
    } else {
        Write-Host '这台设备还没连过，没有设置项。扫描一次之后再看。' -ForegroundColor Yellow
        return
    }

    Write-Host ''
    foreach ($opt in $res.options) {
        $line = '  {0,-12} {1,-8} {2,-8} 当前: {3}' -f $opt.key, $opt.label, $opt.control, $opt.value
        Write-Host $line
        if ($opt.choices) {
            Write-Host ('               可选: ' + (($opt.choices | ForEach-Object { '' + $_.value + '(' + $_.label + ')' }) -join '  '))
        }
        if ($null -ne $opt.min) {
            Write-Host ('               范围: ' + $opt.min + ' ~ ' + $opt.max + '  步长 ' + $opt.step)
        }
    }
}

function Show-Config {
    $res = Invoke-Api -Path '/api/scanner-options/config'
    if ($null -eq $res) { return }

    Write-Host ''
    if ($res.source -eq 'builtin') {
        Write-Host '设置项配置: 内置（正常状态）' -ForegroundColor Green
    } else {
        Write-Host '设置项配置: 现场覆盖文件（应急状态）' -ForegroundColor Yellow
        Write-Host '  改好的内容要同步回源码 scanopt/default_options.jsonc，发版后删掉覆盖文件。'
    }
    Write-Host ('  覆盖文件: ' + $res.overrideFile + $(if ($res.overridePresent) { '（存在）' } else { '（不存在）' }))
    if (-not $res.ok) {
        Write-Host ('  覆盖文件有误，未生效: ' + $res.error) -ForegroundColor Red
    }
    Write-Host ('  生效时间: ' + $res.loadedAt)
    Write-Host ('  设置项  : ' + ($res.options -join ', '))
    if ($res.profiles.Count -gt 0) {
        Write-Host ('  设备特例: ' + ($res.profiles -join ', '))
    }
}

function Save-BuiltinConfig {
    $target = Join-Path $ScriptDir 'scanner-options.jsonc'
    Write-Host ''
    Write-Host '这是应急手段：放下这个文件后，它会整份替换内置配置。' -ForegroundColor Yellow
    Write-Host '改好的内容要同步回源码，发版后删掉这个文件。'
    if (Test-Path -LiteralPath $target) {
        $answer = Read-Host ($target + ' 已存在，覆盖？(y/N)')
        if ($answer -ne 'y' -and $answer -ne 'Y') { Write-Host '已取消。'; return }
    }

    $content = Invoke-Api -Path '/api/scanner-options/config/builtin' -Raw
    if ($null -eq $content) { return }
    # 存成 UTF-8 无 BOM：服务端读的是 UTF-8，有没有 BOM 都认，但记事本看着干净些
    [System.IO.File]::WriteAllText($target, $content, (New-Object System.Text.UTF8Encoding($false)))
    Write-Host ''
    Write-Host ('已保存: ' + $target) -ForegroundColor Green
    Write-Host '用记事本改完保存即可生效（不用重启服务），改坏了服务会在控制台提示并继续用内置配置。'
    Write-Host '撤销：删掉这个文件。'
}

# ---- 菜单 ----

function Show-Menu {
    while ($true) {
        Write-Host ''
        Write-Host '==============================================='
        Write-Host ' scansvc 维护工具    服务地址: ' -NoNewline
        Write-Host $script:BaseUrl
        Write-Host '==============================================='
        Write-Host '  1) 服务状态'
        Write-Host '  2) 扫描仪列表'
        Write-Host '  3) 导出扫描仪能力（适配新扫描仪时发给开发）'
        Write-Host '  4) 查看某台扫描仪的设置项'
        Write-Host '  5) 设置项配置状态'
        Write-Host '  6) 下载内置设置项配置（应急覆盖用）'
        Write-Host '  0) 退出'
        Write-Host ''
        $choice = Read-Host '请选择'
        switch ($choice) {
            '1' { Show-Status }
            '2' { Show-Devices }
            '3' { Invoke-Dump -Name '' }
            '4' { Show-Options -Name '' }
            '5' { Show-Config }
            '6' { Save-BuiltinConfig }
            '0' { return }
            default { Write-Host '没有这个选项。' -ForegroundColor Yellow }
        }
    }
}

# ---- 入口 ----

switch ($Command.ToLower()) {
    ''                { Show-Menu }
    'menu'            { Show-Menu }
    'status'          { Show-Status }
    'devices'         { Show-Devices }
    'dump'            { Invoke-Dump -Name $Device }
    'options'         { Show-Options -Name $Device }
    'config'          { Show-Config }
    'builtin-config'  { Save-BuiltinConfig }
    default {
        Write-Host ('不认识的命令: ' + $Command) -ForegroundColor Yellow
        Write-Host '可用: status | devices | dump [设备名] | options [设备名] | config | builtin-config'
        exit 1
    }
}

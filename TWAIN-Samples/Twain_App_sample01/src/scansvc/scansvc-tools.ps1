<#
scansvc 维护小工具：导出扫描仪能力、查看设置项、查看配置状态等。

不要直接双击这个文件，运行同目录的 scansvc-tools.bat。
只用 Windows 自带的 PowerShell 发 HTTP 请求，机器上没有 curl.exe 也能用。

用法（bat 会把参数原样转过来）：
  scansvc-tools.bat                     打开菜单
  scansvc-tools.bat status              服务状态
  scansvc-tools.bat devices             扫描仪列表
  scansvc-tools.bat diagnose            TWAIN 环境体检（某台扫描仪枚举不出来时先跑这个）
  scansvc-tools.bat dump [设备名]        导出扫描仪能力（不带设备名会让你选）
  scansvc-tools.bat options [设备名]     查看设置项
  scansvc-tools.bat set [设备名] -Set "dpi=300,colorMode=gray"     修改设置项（不带 -Set 会让你挑）
  scansvc-tools.bat scan [设备名] -Count 1 -Extension jpeg -Set "dpi=300"   扫描测试（HTTP 异步接口）
  scansvc-tools.bat upload <文件名> -Server <业务系统地址> -Token <令牌> -Set "archive_id=123"
                                        转发上传测试（文件名就是扫描返回地址的最后一段）
  scansvc-tools.bat config              设置项配置状态（内置 / 现场覆盖）
  scansvc-tools.bat builtin-config      下载内置设置项配置，存成 scanner-options.jsonc
  scansvc-tools.bat start / stop        启动 / 停止 scansvc.exe
  scansvc-tools.bat autostart           设置或取消开机自启（当前用户「启动」文件夹）
  scansvc-tools.bat -Port 18081 status  服务改过 HTTP 端口时指定

端口的取值顺序：-Port 参数 > 环境变量 SCANSVC_HTTP_PORT > 同目录 scansvc.conf 里的 http-port > 18080
#>

[CmdletBinding()]
param(
    [string] $Command = '',
    [string] $Device = '',
    [int] $Port = 0,
    # set / scan 用：设置项，形如 "dpi=300,colorMode=gray"
    [string] $Set = '',
    # scan 用：扫几页（0 = 走送纸器扫到没纸，默认）、图片格式
    [int] $Count = 0,
    [string] $Extension = 'jpeg',
    # upload 用：业务系统的接收地址、令牌
    [string] $Server = '',
    [string] $Token = ''
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

# Invoke-ApiPost 发 JSON 请求。失败时同样把服务端给的中文原因带出来。
function Invoke-ApiPost {
    param(
        [string] $Path,
        $Body,
        [int] $TimeoutSec = 60
    )
    $url = $script:BaseUrl + $Path
    try {
        $json = $Body | ConvertTo-Json -Depth 6 -Compress
        return Invoke-RestMethod -Uri $url -Method Post -Body $json -ContentType 'application/json; charset=utf-8' -TimeoutSec $TimeoutSec @script:HttpArgs
    } catch {
        Write-Host ''
        Write-Host ('请求失败: ' + $url) -ForegroundColor Red
        $detail = Get-ErrorDetail $_
        if ($detail) {
            Write-Host ('  服务返回: ' + $detail) -ForegroundColor Red
        } else {
            Write-Host ('  ' + $_.Exception.Message) -ForegroundColor Red
        }
        return $null
    }
}

# ParseOptionPairs 把 "dpi=300,colorMode=gray" 解析成哈希表。
# 纯数字按数字发，true/false 按布尔发——服务端两种都认，但发对类型更贴近前端的真实用法。
function ParseOptionPairs {
    param([string] $Text)
    $out = @{}
    if (-not $Text) { return $out }
    foreach ($pair in $Text.Split(',')) {
        $kv = $pair.Split('=', 2)
        if ($kv.Count -ne 2) {
            Write-Host ('忽略写法不对的设置项: ' + $pair + '（应为 key=value）') -ForegroundColor Yellow
            continue
        }
        $key = $kv[0].Trim()
        $val = $kv[1].Trim()
        if ($val -match '^-?\d+$')            { $out[$key] = [int] $val }
        elseif ($val -match '^-?\d+\.\d+$')  { $out[$key] = [double] $val }
        elseif ($val -eq 'true')             { $out[$key] = $true }
        elseif ($val -eq 'false')            { $out[$key] = $false }
        else                                 { $out[$key] = $val }
    }
    return $out
}

# ---- 各项功能 ----

function Show-Status {
    Show-Process
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
        Write-Host '没有发现扫描仪。常见原因：驱动没装、TWAINDSM.dll 不在 exe 旁边、驱动位数和本服务不符。' -ForegroundColor Yellow
        Write-Host '跑一下 “scansvc-tools.bat diagnose” 会直接给出结论。' -ForegroundColor Yellow
        return
    }
    Write-Host ('发现 ' + $devices.Count + ' 台扫描仪（来自已安装的驱动，不代表设备接着）:')
    for ($i = 0; $i -lt $devices.Count; $i++) {
        Write-Host ('  [' + ($i + 1) + '] ' + $devices[$i])
    }
}

# Show-Diagnosis 把 /api/diagnose 的结果排版出来。
# 这是"驱动装了却枚举不到"的第一手段：绝大多数是驱动只装了 32 位、服务却是 64 位的，
# 这种情况在服务里无解，只能换对应位数的 scansvc。
function Show-Diagnosis {
    $d = Invoke-Api -Path '/api/diagnose' -TimeoutSec 30
    if ($null -eq $d) { return }

    Write-Host ''
    Write-Host ('本服务  : ' + $d.processBits + ' 位 (' + $d.processArch + ')')
    # DSM 是用裸文件名 LoadLibrary 加载的，真正生效的是 exe 旁边那一份，
    # 系统目录下的 twain_xx\TWAINDSM.dll 不在搜索路径里，所以两处都要报。
    if ($d.dsmNextToExeFound) {
        Write-Host ('DSM     : ' + $d.dsmNextToExe + '   (exe 旁边, 生效的就是这份)')
    } else {
        Write-Host ('DSM     : ' + $d.dsmNextToExe + '   ** exe 旁边没有 **') -ForegroundColor Yellow
    }
    if ($d.dsmFound) {
        Write-Host ('          ' + $d.dsmPath + '   (系统目录, 安装包装的那份)')
    } else {
        Write-Host ('          ' + $d.dsmPath + '   (系统目录也没有)')
    }

    Write-Host ''
    Write-Host '已安装的 TWAIN 驱动:'
    foreach ($dir in @($d.dirs)) {
        $tag = if ($dir.usable) { '本服务可用' } else { '本服务用不了' }
        Write-Host ('  ' + $dir.path + '  [' + $dir.bits + ' 位, ' + $tag + ']')
        $names = @($dir.sources | ForEach-Object { $_.name })
        if ($names.Count -eq 0) {
            Write-Host '      (空)'
        } else {
            foreach ($n in $names) { Write-Host ('      ' + $n) }
        }
    }

    $found = @($d.enumerated)
    Write-Host ''
    Write-Host ('枚举到 ' + $found.Count + ' 台: ' + $(if ($found.Count) { $found -join '、' } else { '(无)' }))

    Write-Host ''
    Write-Host '结论:'
    foreach ($c in @($d.conclusion)) {
        Write-Host ('  · ' + $c) -ForegroundColor Cyan
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

# Set-Options 下发设置项。不带 -Set 时列出当前设置项，让用户挑一项改。
function Set-Options {
    param([string] $Name, [string] $Pairs)

    $device = Select-Device -Name $Name
    if (-not $device) { return }

    $values = ParseOptionPairs $Pairs
    if ($values.Count -eq 0) {
        $res = Invoke-Api -Path ('/api/scanner-options?device=' + [uri]::EscapeDataString($device)) -TimeoutSec 60
        if ($null -eq $res) { return }
        if ($res.options.Count -eq 0) {
            Write-Host '这台设备还没有设置项（先扫一次，或用菜单里的"查看设置项"确认）。' -ForegroundColor Yellow
            return
        }

        Write-Host ''
        for ($i = 0; $i -lt $res.options.Count; $i++) {
            $o = $res.options[$i]
            Write-Host ('  [' + ($i + 1) + '] ' + $o.label + ' (' + $o.key + ')  当前: ' + $o.value)
        }
        $pick = Read-Host '选要修改的项（直接回车取消）'
        if (-not $pick) { return }
        $idx = $pick -as [int]
        if ($null -eq $idx -or $idx -lt 1 -or $idx -gt $res.options.Count) {
            Write-Host '编号不对。' -ForegroundColor Yellow
            return
        }

        $opt = $res.options[$idx - 1]
        Write-Host ''
        if ($opt.choices) {
            for ($i = 0; $i -lt $opt.choices.Count; $i++) {
                Write-Host ('  [' + ($i + 1) + '] ' + $opt.choices[$i].label + '  (' + $opt.choices[$i].value + ')')
            }
            $vp = Read-Host '选取值编号'
            $vi = $vp -as [int]
            if ($null -eq $vi -or $vi -lt 1 -or $vi -gt $opt.choices.Count) {
                Write-Host '编号不对。' -ForegroundColor Yellow
                return
            }
            $values[$opt.key] = $opt.choices[$vi - 1].value
        } elseif ($opt.control -eq 'switch') {
            $vp = Read-Host '开(1) 还是关(0)'
            $values[$opt.key] = ($vp -eq '1')
        } else {
            $hint = '输入数值'
            if ($null -ne $opt.min) { $hint += ('（' + $opt.min + ' ~ ' + $opt.max + '，步长 ' + $opt.step + '）') }
            $vp = Read-Host $hint
            $num = $vp -as [double]
            if ($null -eq $num) { Write-Host '不是数字。' -ForegroundColor Yellow; return }
            $values[$opt.key] = $num
        }
    }

    Write-Host ''
    Write-Host ('下发: ' + (($values.Keys | ForEach-Object { $_ + '=' + $values[$_] }) -join '  '))
    Write-Host '（会连接扫描仪）'
    $res = Invoke-ApiPost -Path '/api/scanner-options' -Body @{ device = $device; scannerOptions = $values }
    if ($null -eq $res) { return }

    Write-Host ''
    foreach ($r in $res.results) {
        if ($r.ok) {
            Write-Host ('  ' + $r.key + ' = ' + $r.value + '  成功') -ForegroundColor Green
        } elseif ($r.skipped) {
            Write-Host ('  ' + $r.key + '  已忽略：' + $r.error) -ForegroundColor Yellow
        } else {
            Write-Host ('  ' + $r.key + ' = ' + $r.value + '  失败：' + $r.error) -ForegroundColor Red
        }
    }
    if ($res.success) {
        Write-Host ''
        Write-Host '全部生效。下面是下发后现读的设置项：' -ForegroundColor Green
        foreach ($o in $res.options.options) {
            Write-Host ('  ' + $o.key.PadRight(12) + ' 当前: ' + $o.value)
        }
    }
}

# Invoke-HttpScan 走 HTTP 异步扫描：发起任务，然后每秒轮询，把扫出来的页打印出来。
# 和客户端文档第 7 章描述的流程完全一致，可以拿它验证服务端这条链路。
function Invoke-HttpScan {
    param([string] $Name, [string] $Pairs, [int] $Pages, [string] $Ext)

    $device = Select-Device -Name $Name
    if (-not $device) { return }

    $body = @{ device = $device; extension = $Ext; count = $Pages }
    $values = ParseOptionPairs $Pairs
    if ($values.Count -gt 0) { $body['scannerOptions'] = $values }

    Write-Host ''
    Write-Host ('发起扫描: ' + $device + '  格式 ' + $Ext + '  页数 ' + $(if ($Pages -eq 0) { '扫到没纸' } else { $Pages }))
    $started = Invoke-ApiPost -Path '/api/scan/start' -Body $body -TimeoutSec 120
    if ($null -eq $started) { return }

    $jobId = $started.job.id
    Write-Host ('任务 ' + $jobId + ' 已发起，轮询中（Ctrl+C 可以停止轮询，扫描不受影响）...')

    $shown = 0
    while ($true) {
        Start-Sleep -Seconds 1
        $res = Invoke-Api -Path ('/api/scan/status?job=' + [uri]::EscapeDataString($jobId)) -TimeoutSec 30
        if ($null -eq $res) { return }
        $job = $res.job

        while ($shown -lt $job.pages.Count) {
            $p = $job.pages[$shown]
            Write-Host ('  第 ' + $p.page + ' 页: ' + $p.url) -ForegroundColor Green
            $shown++
        }

        if ($job.state -eq 'done') {
            Write-Host ''
            Write-Host ('扫描完成，共 ' + $job.pages.Count + ' 页（' + $job.startedAt + ' ~ ' + $job.endedAt + '）') -ForegroundColor Green
            if ($job.pages.Count -gt 0) {
                $open = Read-Host '在浏览器里打开第一张？(y/N)'
                if ($open -eq 'y' -or $open -eq 'Y') { Start-Process $job.pages[0].url }
            }
            return
        }
        if ($job.state -eq 'failed') {
            Write-Host ''
            Write-Host ('扫描失败: ' + $job.error) -ForegroundColor Red
            return
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

# Invoke-UploadTest 测 /file/upload：让服务把某张扫描图转发到业务系统。
# 这条链路只有真连业务系统才能验证，所以 -Server 必填。
# 表单字段用 x-www-form-urlencoded 发（服务端两种都认），PowerShell 5.1 不用自己拼 multipart。
function Invoke-UploadTest {
    param([string] $FileName, [string] $ServerUrl, [string] $TokenValue, [string] $Extra)

    if (-not $ServerUrl) {
        Write-Host ''
        Write-Host '要测转发上传，得先有业务系统的接收地址。' -ForegroundColor Yellow
        $ServerUrl = Read-Host '业务系统接收地址（完整 URL，直接回车取消）'
        if (-not $ServerUrl) { return }
    }
    if (-not $FileName) {
        Write-Host ''
        Write-Host '要传哪张图？填扫描返回的文件名（图片地址的最后一段），例如 SCAN_20260916_095614_861_000001N.jpeg'
        $FileName = Read-Host '文件名（直接回车取消）'
        if (-not $FileName) { return }
    }

    $form = @{ filename = $FileName; server = $ServerUrl }
    foreach ($kv in (ParseOptionPairs $Extra).GetEnumerator()) { $form[$kv.Key] = $kv.Value }

    $headers = @{}
    if ($TokenValue) { $headers['token'] = $TokenValue }

    Write-Host ''
    Write-Host ('转发 ' + $FileName + ' 到 ' + $ServerUrl)
    if ($Extra) { Write-Host ('附加字段: ' + $Extra) }

    try {
        $res = Invoke-RestMethod -Uri ($script:BaseUrl + '/file/upload') -Method Post -Body $form `
            -ContentType 'application/x-www-form-urlencoded' -Headers $headers -TimeoutSec 600 @script:HttpArgs
    } catch {
        Write-Host ''
        Write-Host ('请求失败: ' + $_.Exception.Message) -ForegroundColor Red
        $detail = Get-ErrorDetail $_
        if ($detail) { Write-Host ('  服务返回: ' + $detail) -ForegroundColor Red }
        return
    }

    Write-Host ''
    if ($res.errorCode -eq 0) {
        Write-Host '转发成功。业务系统返回：' -ForegroundColor Green
    } else {
        Write-Host ('转发失败（errorCode ' + $res.errorCode + '）：' + $res.msg) -ForegroundColor Red
        Write-Host '常见原因：文件名写错、业务系统地址不通、对方没返回 JSON。'
    }
    $res | ConvertTo-Json -Depth 6
}

# ---- 进程 / 开机自启 ----

function Get-ScansvcProcess {
    return @(Get-Process -Name 'scansvc' -ErrorAction SilentlyContinue)
}

function Start-Scansvc {
    $exe = Join-Path $ScriptDir 'scansvc.exe'
    if (-not (Test-Path -LiteralPath $exe)) {
        Write-Host ('找不到 ' + $exe + '，把这两个脚本放到 scansvc.exe 同目录。') -ForegroundColor Red
        return
    }
    if ((Get-ScansvcProcess).Count -gt 0) {
        Write-Host 'scansvc 已经在运行。' -ForegroundColor Yellow
        return
    }
    Start-Process -FilePath $exe -WorkingDirectory $ScriptDir
    Start-Sleep -Seconds 2
    if ((Get-ScansvcProcess).Count -gt 0) {
        Write-Host '已启动。托盘模式下图标在右下角（可能要点「显示隐藏的图标」）。' -ForegroundColor Green
    } else {
        Write-Host '启动后进程又退出了，看 scansvc.log。' -ForegroundColor Red
    }
}

function Stop-Scansvc {
    $procs = Get-ScansvcProcess
    if ($procs.Count -eq 0) {
        Write-Host 'scansvc 没有在运行。' -ForegroundColor Yellow
        return
    }
    $procs | Stop-Process -Force
    Write-Host ('已停止 ' + $procs.Count + ' 个 scansvc 进程。') -ForegroundColor Green
}

function Get-AutostartPath {
    $startup = [Environment]::GetFolderPath('Startup')
    if (-not $startup) { return '' }   # 取不到（非 Windows）时按"没有开机自启"处理
    return (Join-Path $startup 'scansvc.lnk')
}

# 开机自启用「启动」文件夹里的快捷方式，不是 Windows 服务：
# TWAIN 驱动要有桌面会话，注册成服务（session 0）多数扫描仪打不开。
function Set-Autostart {
    $link = Get-AutostartPath
    $exe = Join-Path $ScriptDir 'scansvc.exe'
    Write-Host ''
    if (-not $link) {
        Write-Host '取不到「启动」文件夹，这个功能只能在 Windows 上用。' -ForegroundColor Red
        return
    }
    if (Test-Path -LiteralPath $link) {
        Write-Host ('当前：已设置开机自启 -> ' + $link)
        $answer = Read-Host '取消开机自启？(y/N)'
        if ($answer -eq 'y' -or $answer -eq 'Y') {
            Remove-Item -LiteralPath $link -Force
            Write-Host '已取消。' -ForegroundColor Green
        }
        return
    }
    Write-Host '当前：未设置开机自启'
    if (-not (Test-Path -LiteralPath $exe)) {
        Write-Host ('找不到 ' + $exe + '，把这两个脚本放到 scansvc.exe 同目录。') -ForegroundColor Red
        return
    }
    $answer = Read-Host '设置为当前用户登录后自动启动？(y/N)'
    if ($answer -ne 'y' -and $answer -ne 'Y') { return }
    try {
        $shell = New-Object -ComObject WScript.Shell
        $shortcut = $shell.CreateShortcut($link)
        $shortcut.TargetPath = $exe
        $shortcut.WorkingDirectory = $ScriptDir
        $shortcut.Description = 'scansvc 扫描服务'
        $shortcut.Save()
        Write-Host ('已设置: ' + $link) -ForegroundColor Green
        Write-Host '下次登录 Windows 时自动启动（登录后才起，锁屏 / 注销状态下不运行）。'
    } catch {
        Write-Host ('设置失败: ' + $_.Exception.Message) -ForegroundColor Red
    }
}

function Show-Process {
    $procs = Get-ScansvcProcess
    Write-Host ''
    if ($procs.Count -eq 0) {
        Write-Host 'scansvc 进程: 未运行' -ForegroundColor Yellow
    } else {
        foreach ($p in $procs) {
            Write-Host ('scansvc 进程: 运行中 (PID ' + $p.Id + '，启动于 ' + $p.StartTime + ')') -ForegroundColor Green
        }
    }
    $link = Get-AutostartPath
    Write-Host ('开机自启: ' + $(if ($link -and (Test-Path -LiteralPath $link)) { '已设置' } else { '未设置' }))
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
        Write-Host '  2d) TWAIN 环境体检（扫描仪枚举不出来时用）'
        Write-Host '  3) 导出扫描仪能力（适配新扫描仪时发给开发）'
        Write-Host '  4) 查看某台扫描仪的设置项'
        Write-Host '  5) 修改设置项（会连接扫描仪）'
        Write-Host '  6) 扫描测试（走 HTTP 异步接口，客户端文档第 7 章那套）'
        Write-Host '  7) 转发上传测试（需要业务系统地址）'
        Write-Host '  ---'
        Write-Host '  8) 设置项配置状态'
        Write-Host '  9) 下载内置设置项配置（应急覆盖用）'
        Write-Host '  ---'
        Write-Host ' 10) 启动 scansvc'
        Write-Host ' 11) 停止 scansvc'
        Write-Host ' 12) 开机自启设置'
        Write-Host '  0) 退出'
        Write-Host ''
        $choice = Read-Host '请选择'
        switch ($choice) {
            '1' { Show-Status }
            '2' { Show-Devices }
            '2d' { Show-Diagnosis }
            '3' { Invoke-Dump -Name '' }
            '4' { Show-Options -Name '' }
            '5' { Set-Options -Name '' -Pairs '' }
            '6' { Invoke-HttpScan -Name '' -Pairs '' -Pages 1 -Ext 'jpeg' }
            '7' { Invoke-UploadTest -FileName '' -ServerUrl $Server -TokenValue $Token -Extra '' }
            '8' { Show-Config }
            '9' { Save-BuiltinConfig }
            '10' { Start-Scansvc }
            '11' { Stop-Scansvc }
            '12' { Set-Autostart }
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
    'diagnose'        { Show-Diagnosis }
    'dump'            { Invoke-Dump -Name $Device }
    'options'         { Show-Options -Name $Device }
    'set'             { Set-Options -Name $Device -Pairs $Set }
    'scan'            { Invoke-HttpScan -Name $Device -Pairs $Set -Pages $Count -Ext $Extension }
    'upload'          { Invoke-UploadTest -FileName $Device -ServerUrl $Server -TokenValue $Token -Extra $Set }
    'config'          { Show-Config }
    'builtin-config'  { Save-BuiltinConfig }
    'start'           { Start-Scansvc }
    'stop'            { Stop-Scansvc }
    'autostart'       { Set-Autostart }
    default {
        Write-Host ('不认识的命令: ' + $Command) -ForegroundColor Yellow
        Write-Host '可用: status | devices | diagnose | dump [设备名] | options [设备名] | set [设备名] | scan [设备名] | upload [文件名] | config | builtin-config | start | stop | autostart'
        exit 1
    }
}

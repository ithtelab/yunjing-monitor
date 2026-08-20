#requires -Version 5
<#
.SYNOPSIS
  本地开发一键重启：同步前端产物 → 重新编译 Go 服务 → 后台重启 :3000。

.DESCRIPTION
  1. web/dist → internal/server/web/dist（go:embed 在编译期读取该目录）
  2. go build 出 .local-run/bin/vps-server-dev.exe
  3. 结束占用 :3000 的旧进程
  4. 通过 WMI (Win32_Process) 启动完全脱离当前终端进程树的后台服务，
     终端/IDE 退出或清理子进程都不会误杀服务，也不会再出现 ChildProcess.kill 报错。

  仅本地开发使用：管理员密码固定为 local-admin-dev-2026，AUTH_SECRET 为开发值。
  生产部署请用 scripts/build-release.ps1 并自行配置环境变量。
#>
$ErrorActionPreference = 'Stop'
$root = Resolve-Path "$PSScriptRoot\.."

# 1. 同步前端产物到 embed 目录
$webDist = Join-Path $root 'web\dist'
$embedDist = Join-Path $root 'internal\server\web\dist'
if (Test-Path $webDist) {
  if (Test-Path $embedDist) { Remove-Item $embedDist -Recurse -Force }
  Copy-Item $webDist $embedDist -Recurse -Force
  Write-Host '[1/4] web/dist 已同步到 internal/server/web/dist'
} else {
  Write-Warning 'web/dist 不存在，跳过同步（请先在 web 目录执行 npm run build）'
}

# 2. 编译服务二进制（embed 在编译期生效，必须重新 build）
$bin = Join-Path $root '.local-run\bin\vps-server-dev.exe'
New-Item -ItemType Directory -Force -Path (Split-Path $bin -Parent) | Out-Null
Push-Location $root
try {
  go build -o $bin ./cmd/vps-server
  if ($LASTEXITCODE -ne 0) { throw "go build 失败，退出码 $LASTEXITCODE" }
} finally {
  Pop-Location
}
Write-Host '[2/4] 已编译 .local-run/bin/vps-server-dev.exe'

# 3. 结束 :3000 上的旧进程
$conn = Get-NetTCPConnection -LocalPort 3000 -State Listen -ErrorAction SilentlyContinue | Select-Object -First 1
if ($conn) {
  Stop-Process -Id $conn.OwningProcess -Force
  Start-Sleep -Milliseconds 800
  Write-Host "[3/4] 已结束旧进程 (PID $($conn.OwningProcess))"
} else {
  Write-Host '[3/4] 3000 端口空闲'
}

# 4. WMI 启动后台服务（进程挂在 WMI 服务下，与当前终端无关）
$logDir = Join-Path $root '.local-run\logs'
New-Item -ItemType Directory -Force -Path $logDir | Out-Null
$cmd = 'cmd.exe /c "set ADDR=:3000&& set AUTH_SECRET=local-dev-secret-9f8e7d6c5b4a&& set ADMIN_PASS=local-admin-dev-2026&& set DATA_PATH=data/server.json&& .local-run\bin\vps-server-dev.exe >> .local-run\logs\server.out.log 2>&1"'
$result = Invoke-CimMethod -ClassName Win32_Process -MethodName Create -Arguments @{
  CommandLine     = $cmd
  CurrentDirectory = "$root"
}
if ($result.ReturnValue -ne 0) { throw "服务进程启动失败，ReturnValue=$($result.ReturnValue)" }

Start-Sleep -Seconds 2
$up = Test-NetConnection -ComputerName localhost -Port 3000 -InformationLevel Quiet -WarningAction SilentlyContinue
if ($up) {
  Write-Host "[4/4] 服务已启动 (PID $($result.ProcessId)) → http://127.0.0.1:3000 （管理员密码 local-admin-dev-2026）"
} else {
  Write-Warning "进程已创建 (PID $($result.ProcessId)) 但 3000 端口未监听，日志见 .local-run\logs\server.out.log"
}

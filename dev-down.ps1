<#
.SYNOPSIS
  TestPilot 开发环境一键关停脚本。

.DESCRIPTION
  - 停止并移除 app + mysql + redis 三容器（默认保留 mysql_data 数据卷）
  - 停止本机 Executor（监听 8100）
  - 停止本机前端 dev server（监听 5173）

.PARAMETER PurgeData
  同时删除 mysql_data 数据卷（所有数据库数据会丢失，谨慎使用）。

.EXAMPLE
  .\dev-down.ps1
  .\dev-down.ps1 -PurgeData
#>
[CmdletBinding()]
param(
  [switch]$PurgeData
)

$ErrorActionPreference = "Continue"
$ROOT = $PSScriptRoot

function Write-Step($msg) { Write-Host "[dev-down] $msg" -ForegroundColor Cyan }
function Write-Ok($msg)   { Write-Host "[dev-down] OK  $msg" -ForegroundColor Green }
function Write-Warn2($msg){ Write-Host "[dev-down] !   $msg" -ForegroundColor Yellow }

# 1. 关停本机进程（Executor 8100 + 前端 5173）
foreach ($port in 8100, 5173) {
  $conns = Get-NetTCPConnection -LocalPort $port -State Listen -ErrorAction SilentlyContinue
  if (-not $conns) {
    Write-Warn2 "端口 $port 未被占用，跳过"
    continue
  }
  foreach ($c in $conns) {
    try {
      Stop-Process -Id $c.OwningProcess -Force -ErrorAction Stop
      Write-Ok "已停止端口 $port 上的进程 pid=$($c.OwningProcess)"
    } catch {
      Write-Warn2 "无法停止 pid=$($c.OwningProcess)：$($_.Exception.Message)"
    }
  }
}

# 2. 关停 docker compose
Write-Step "关停 app/mysql/redis 容器..."
$compose = @("compose", "-f", (Join-Path $ROOT "docker-compose.yml"), "--env-file", (Join-Path $ROOT ".env"), "down")
if ($PurgeData) {
  $compose += "-v"
  Write-Warn2 "PurgeData 启用：将删除 mysql_data 数据卷"
}
& docker @compose
if ($LASTEXITCODE -eq 0) { Write-Ok "容器已停止" }

Write-Host ""
Write-Host "=== TestPilot Dev Environment Stopped ===" -ForegroundColor Green
if (-not $PurgeData) {
  Write-Host "MySQL 数据卷保留，下次启动自动复用。" -ForegroundColor Gray
}

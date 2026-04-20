<#
.SYNOPSIS
  TestPilot 开发环境一键启动脚本（跨电脑可复用）。

.DESCRIPTION
  执行以下流程，全部支持"已就绪则跳过"的幂等行为：
    1) 检查 docker / node / npm / py|python 依赖
    2) 首次生成 TestPilot/.env（从 .env.example 拷贝并随机化 JWT_SECRET）
    3) 首次生成 executor/.env（提示补 OPENAI_API_KEY）
    4) docker compose 启动 app + mysql + redis 三容器（必要时重建）
    5) executor 首次自动建 venv + 装依赖 + playwright chromium
    6) 独立窗口启动 Executor
    7) 前端首次自动 npm install
    8) 独立窗口启动前端 dev server
    9) 轮询 /health 等待服务可用，打印访问地址

  假设目录结构：
    <root>/
      TestPilot/       <- 当前脚本所在位置
      TestFront/       <- 兄弟目录

.PARAMETER Rebuild
  强制 docker build --no-cache 重建 app 镜像。

.PARAMETER SkipExecutor
  不启动 Python Executor（8100）。

.PARAMETER SkipFrontend
  不启动前端 dev server（5173）。

.EXAMPLE
  .\dev-up.ps1
  .\dev-up.ps1 -Rebuild
  .\dev-up.ps1 -SkipFrontend
#>
[CmdletBinding()]
param(
  [switch]$Rebuild,
  [switch]$SkipExecutor,
  [switch]$SkipFrontend
)

$ErrorActionPreference = "Stop"
$ROOT = $PSScriptRoot                         # TestPilot 目录
$WORKSPACE = Split-Path -Parent $ROOT         # 兄弟仓库所在目录
$FRONT = Join-Path $WORKSPACE "TestFront"
$EXECUTOR = Join-Path $ROOT "executor"

function Write-Step($msg)    { Write-Host "[dev-up] $msg" -ForegroundColor Cyan }
function Write-Ok($msg)      { Write-Host "[dev-up] OK  $msg" -ForegroundColor Green }
function Write-Warn2($msg)   { Write-Host "[dev-up] !   $msg" -ForegroundColor Yellow }
function Write-Err2($msg)    { Write-Host "[dev-up] ERR $msg" -ForegroundColor Red }

function Ensure-Command($name, $altNames = @()) {
  if (Get-Command $name -ErrorAction SilentlyContinue) { return $name }
  foreach ($alt in $altNames) {
    if (Get-Command $alt -ErrorAction SilentlyContinue) { return $alt }
  }
  Write-Err2 "未找到 $name（可选替代：$($altNames -join ', ')）。请先安装后再运行。"
  exit 1
}

function Test-PortInUse($port) {
  $conn = Get-NetTCPConnection -LocalPort $port -State Listen -ErrorAction SilentlyContinue
  return $null -ne $conn
}

# ---------- 1. 依赖检查 ----------
Write-Step "[1/8] 检查依赖..."
$dockerCmd = Ensure-Command "docker"
$nodeCmd   = Ensure-Command "node"
$npmCmd    = Ensure-Command "npm"
$pyCmd     = Ensure-Command "py" @("python")
Write-Ok  "docker / node / npm / $pyCmd 均可用"

# 确保 Docker Desktop 已启动
try {
  & $dockerCmd info --format '{{.ServerVersion}}' | Out-Null
} catch {
  Write-Err2 "docker daemon 未就绪，请先启动 Docker Desktop"
  exit 1
}

# ---------- 2. TestPilot/.env ----------
Write-Step "[2/8] 检查 TestPilot/.env ..."
$envFile = Join-Path $ROOT ".env"
$envSample = Join-Path $ROOT ".env.example"
if (-not (Test-Path $envFile)) {
  if (-not (Test-Path $envSample)) {
    Write-Err2 ".env.example 不存在，无法自动生成 .env"
    exit 1
  }
  Copy-Item $envSample $envFile
  # 随机化 JWT_SECRET
  $bytes = New-Object byte[] 32
  (New-Object System.Security.Cryptography.RNGCryptoServiceProvider).GetBytes($bytes)
  $jwt = [Convert]::ToBase64String($bytes)
  (Get-Content $envFile) -replace 'JWT_SECRET=.*', "JWT_SECRET=$jwt" | Set-Content $envFile -Encoding UTF8
  Write-Ok  "已生成 .env 并随机化 JWT_SECRET"
} else {
  Write-Ok  ".env 已存在，跳过"
}

# ---------- 3. executor/.env ----------
Write-Step "[3/8] 检查 executor/.env ..."
$execEnv = Join-Path $EXECUTOR ".env"
if (-not (Test-Path $execEnv)) {
  @"
# Executor 环境变量（首次由 dev-up.ps1 生成，请补齐 OPENAI_API_KEY）
OPENAI_API_KEY=
OPENAI_BASE_URL=https://api.openai.com/v1
OPENAI_MODEL=gpt-4.1
BROWSER_HEADLESS=true
EXECUTOR_PORT=8100
EXECUTOR_API_KEY=tp-executor-secret-key-change-in-prod
"@ | Set-Content $execEnv -Encoding UTF8
  Write-Warn2 "已生成 executor/.env 模板，请填写 OPENAI_API_KEY 后再使用 AI 相关功能"
} else {
  Write-Ok  "executor/.env 已存在，跳过"
}

# ---------- 4. 三容器 ----------
Write-Step "[4/8] 启动 app + mysql + redis 容器 ..."
$composeArgs = @("compose", "-f", (Join-Path $ROOT "docker-compose.yml"), "--env-file", $envFile, "up", "-d")
if ($Rebuild) { $composeArgs += @("--build", "--force-recreate") } else { $composeArgs += "--build" }
$composeArgs += @("app", "mysql", "redis")
& $dockerCmd @composeArgs
if ($LASTEXITCODE -ne 0) { Write-Err2 "docker compose up 失败"; exit 1 }
Write-Ok "三容器已 up"

# ---------- 5 & 6. Executor ----------
if (-not $SkipExecutor) {
  Write-Step "[5/8] 准备 Executor venv & 依赖 ..."
  $venvPy = Join-Path $EXECUTOR ".venv\Scripts\python.exe"
  $venvPip = Join-Path $EXECUTOR ".venv\Scripts\pip.exe"
  if (-not (Test-Path $venvPy)) {
    & $pyCmd -m venv (Join-Path $EXECUTOR ".venv")
    & $venvPip install --upgrade pip
    & $venvPip install -r (Join-Path $EXECUTOR "requirements.txt")
    Push-Location $EXECUTOR
    try { npx --yes playwright install chromium } finally { Pop-Location }
    Write-Ok "Executor 依赖已安装"
  } else {
    Write-Ok "Executor venv 已存在，跳过安装"
  }

  Write-Step "[6/8] 启动 Executor (:8100) ..."
  if (Test-PortInUse 8100) {
    Write-Ok "端口 8100 已被占用，视为 Executor 已运行，跳过启动"
  } else {
    Start-Process -FilePath $venvPy -ArgumentList "main.py" -WorkingDirectory $EXECUTOR -WindowStyle Normal | Out-Null
    Write-Ok "Executor 已在新窗口启动"
  }
} else {
  Write-Warn2 "跳过 Executor（-SkipExecutor）"
}

# ---------- 7 & 8. 前端 ----------
if (-not $SkipFrontend) {
  if (-not (Test-Path $FRONT)) {
    Write-Warn2 "未找到 $FRONT，跳过前端（期望与 TestPilot 同级）"
  } else {
    Write-Step "[7/8] 准备前端依赖 ..."
    if (-not (Test-Path (Join-Path $FRONT "node_modules"))) {
      Push-Location $FRONT
      try { & $npmCmd install } finally { Pop-Location }
      Write-Ok "前端依赖已安装"
    } else {
      Write-Ok "前端 node_modules 已存在，跳过安装"
    }

    Write-Step "[8/8] 启动前端 dev server (:5173) ..."
    if (Test-PortInUse 5173) {
      Write-Ok "端口 5173 已被占用，视为前端已运行，跳过启动"
    } else {
      Start-Process -FilePath "powershell.exe" -ArgumentList @("-NoExit", "-Command", "Set-Location '$FRONT'; npm run dev") -WorkingDirectory $FRONT -WindowStyle Normal | Out-Null
      Write-Ok "前端已在新窗口启动"
    }
  }
} else {
  Write-Warn2 "跳过前端（-SkipFrontend）"
}

# ---------- 9. 健康检查 ----------
Write-Step "等待服务就绪..."
$targets = @(
  @{ Name = "app";       Url = "http://localhost:8080/health" }
)
if (-not $SkipExecutor) { $targets += @{ Name = "executor"; Url = "http://localhost:8100/health" } }
if (-not $SkipFrontend) { $targets += @{ Name = "front";    Url = "http://localhost:5173/" } }

foreach ($t in $targets) {
  $ok = $false
  for ($i = 0; $i -lt 30; $i++) {
    try {
      $resp = Invoke-WebRequest -Uri $t.Url -TimeoutSec 3 -UseBasicParsing -ErrorAction Stop
      if ($resp.StatusCode -eq 200) { $ok = $true; break }
    } catch { Start-Sleep -Seconds 2 }
  }
  if ($ok) { Write-Ok  "$($t.Name) ready  $($t.Url)" }
  else     { Write-Warn2 "$($t.Name) 未就绪 $($t.Url)，请稍后手动检查" }
}

Write-Host ""
Write-Host "=== TestPilot Dev Environment Ready ===" -ForegroundColor Green
Write-Host "  Backend API : http://localhost:8080"
Write-Host "  Executor    : http://localhost:8100"
Write-Host "  Frontend    : http://localhost:5173"
Write-Host ""
Write-Host "默认账号（密码均为 TestPilot@2026）:"
Write-Host "  admin@testpilot.local / manager@testpilot.local / tester@testpilot.local"
Write-Host ""
Write-Host "关停：.\\dev-down.ps1"

param(
  [string]$ProjectRoot = "D:\hsxa\ai_project\测试管理平台\TestPilot",
  [string]$ApiBase = "http://localhost:8080"
)

$ErrorActionPreference = "Stop"

Write-Host "[1/6] 切换目录: $ProjectRoot"
Set-Location $ProjectRoot

Write-Host "[2/6] 拉起后端容器（重建 app）..."
docker compose up -d --build app | Out-Host

Write-Host "[3/6] 等待健康检查..."
$healthOk = $false
for ($i=0; $i -lt 30; $i++) {
  Start-Sleep -Seconds 2
  try {
    $resp = Invoke-RestMethod -Uri "$ApiBase/health" -Method GET -TimeoutSec 3
    if ($resp.status -eq "ok") { $healthOk = $true; break }
  } catch {}
}
if (-not $healthOk) {
  Write-Host "❌ 健康检查失败，输出最近日志："
  docker compose logs --tail=120 app | Out-Host
  exit 1
}
Write-Host "✅ 健康检查通过"

Write-Host "[4/6] 验证 IAM 核心接口可用..."
$headers = @{ "X-User-ID" = "1" }
$users = Invoke-RestMethod -Uri "$ApiBase/api/v1/users?page=1&pageSize=5" -Headers $headers -Method GET -TimeoutSec 10
$roles = Invoke-RestMethod -Uri "$ApiBase/api/v1/roles" -Headers $headers -Method GET -TimeoutSec 10
if (-not $users.items) { throw "users 接口返回异常" }
if (-not $roles.roles) { throw "roles 接口返回异常" }
Write-Host "✅ users/roles 接口正常"

Write-Host "[5/6] 验证‘系统预置角色不可删除’..."
$presetNames = @("admin","manager","tester","reviewer","readonly")
foreach ($r in $roles.roles) {
  if ($presetNames -contains $r.name) {
    try {
      Invoke-RestMethod -Uri "$ApiBase/api/v1/roles/$($r.id)" -Headers $headers -Method DELETE -TimeoutSec 10 | Out-Null
      throw "预置角色 $($r.name) 被删除了（不符合预期）"
    } catch {
      $msg = $_.Exception.Message
      if ($msg -notmatch "409") {
        throw "删除预置角色 $($r.name) 返回非409：$msg"
      }
    }
  }
}
Write-Host "✅ 预置角色删除保护正常"

Write-Host "[6/6] 当前状态"
docker compose ps | Out-Host
Write-Host "\n🎉 系统可用性检查通过。"

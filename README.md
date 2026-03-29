# Aisight 测试管理平台 — 后端

> Go + Python 微服务架构，提供测试管理全流程 API

## 核心能力

- JWT 认证 + 项目管理 + 成员 ACL
- 用例管理（CRUD + 批量 + 模块树 + 附件 + Excel 导入导出）
- 需求/脚本管理 + 执行触发 + 缺陷管理
- **测试智编**（AI 脚本生成 + 录制增强 + Playwright 验证）
- 用户/角色 + 审计日志 + 增量 SQL 迁移

---

## 快速开始

### 环境要求

| 依赖 | 版本 | 用途 |
|------|------|------|
| Go | 1.25+ | 后端 |
| Python | 3.11+ | Executor 执行服务 |
| Node.js | 20+ | 前端 + Playwright |
| Docker | - | MySQL / Redis |

### 启动三个服务

```powershell
# 1. Go 后端 (端口 8080)
$env:EXECUTOR_URL="http://127.0.0.1:8100"
go run ./cmd/server

# 2. Python Executor (端口 8100)
cd executor
python -m venv .venv
.venv\Scripts\activate
pip install -r requirements.txt
npx playwright install chromium
python main.py

# 3. Vue 前端 (端口 5173) — 见 TestFront 仓库
```

| 服务 | 地址 |
|------|------|
| 前端 | http://localhost:5173 |
| 后端 API | http://localhost:8080 |
| Executor | http://localhost:8100 |

### 默认账号

| 邮箱 | 密码 | 角色 |
|------|------|------|
| admin@testpilot.local | TestPilot@2026 | 管理员 |
| manager@testpilot.local | TestPilot@2026 | 经理 |
| tester@testpilot.local | TestPilot@2026 | 测试员 |

---

## 环境变量

### Go 后端

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `APP_PORT` | 服务端口 | `8080` |
| `DB_HOST` / `DB_PORT` / `DB_USER` / `DB_PASSWORD` / `DB_NAME` | MySQL | `127.0.0.1:3306` / `testpilot` |
| `REDIS_ADDR` | Redis | `127.0.0.1:6379` |
| `JWT_SECRET` | JWT 密钥 | dev 默认值 |
| `EXECUTOR_URL` | Executor 地址 | `http://127.0.0.1:8100` |
| `EXECUTOR_API_KEY` | Executor 鉴权密钥 | dev 默认值 |

### Executor（`executor/.env`）

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `SERVICE_PORT` | 端口 | `8100` |
| `OPENAI_API_KEY` | LLM API Key | 必填 |
| `OPENAI_API_BASE` | LLM 地址 | `https://api.openai.com/v1` |
| `OPENAI_MODEL` | 模型 | `gpt-4o` |
| `EXECUTOR_API_KEY` | 鉴权密钥 | 与 Go 后端一致 |
| `CODEGEN_SESSION_TIMEOUT_SEC` | 录制超时 | `600` |

---

## 项目结构

```
TestPilot/
├── cmd/server/main.go              # 入口
├── internal/
│   ├── api/                        # Handler 层
│   ├── service/                    # 业务逻辑
│   ├── model/                      # 数据模型
│   ├── repository/                 # 数据访问
│   ├── config/                     # 配置管理
│   ├── migration/                  # SQL 迁移
│   └── seed/                       # 种子数据
├── executor/                       # Python 执行服务
│   ├── main.py                     #   FastAPI + API Key 中间件
│   ├── browser_runner.py           #   browser-use 自动探索
│   ├── script_generator.py         #   LLM 脚本生成/重构
│   ├── validation_runner.py        #   Playwright 回放验证
│   └── requirements.txt            #   依赖清单
├── docs/                           # 需求/架构文档
└── go.mod
```

---

## 开源依赖

| 层 | 依赖 | 用途 |
|----|------|------|
| Go | Gin, GORM, go-redis, golang-jwt | Web + ORM + 缓存 + 认证 |
| Python | FastAPI, browser-use, Playwright, OpenAI SDK | AI 自动化 + 脚本生成 |
| 前端 | Vue 3, Vite, Pinia, vue-codemirror, Element Plus | SPA + 代码编辑器 |

# Aisight 测试管理平台

## 核心能力

- JWT 认证 + bcrypt 密码（access_token + refresh_token）
- 项目管理（CRUD + 成员 ACL）
- 用例管理（CRUD + 批量操作 + 模块目录树 + 附件 + Excel 导入导出 + 历史记录 + 关联关系）
- 需求、脚本实体管理 + 需求-用例、用例-脚本关联
- 执行触发（全量、单脚本、批量）+ 运行结果创建缺陷
- **测试智编**（AI 脚本生成 + 录制增强 + Playwright 验证）
- 用户/角色管理 + 审计日志
- 增量 SQL 迁移引擎（幂等、自动执行）

---

## 1. 快速开始

### 环境要求

| 依赖 | 版本 | 用途 |
|------|------|------|
| Go | 1.25+ | 后端编译运行 |
| Node.js | 20+ | 前端开发 + Playwright |
| Python | 3.11+ | Executor 执行服务 |
| Docker + Docker Compose | - | MySQL / Redis |
| Playwright | latest | 浏览器自动化（`npx playwright install`） |

### 一键启动（推荐）

```powershell
# 开发环境（本地 Go + Docker 基础设施）
.\deploy.ps1
# 自动完成: Docker Desktop → MySQL/Redis → 编译后端 → 数据库迁移 → Seed → 前端

# Docker 全容器部署
.\deploy-docker.ps1
# 自动完成: Docker Desktop → docker compose up --build → 健康检查
```

### 手动启动（三个服务）

```powershell
# 1. Go 后端 (端口 8080)
cd TestPilot
$env:EXECUTOR_URL="http://127.0.0.1:8100"
go run ./cmd/server

# 2. Python Executor 执行服务 (端口 8100)
cd TestPilot/executor
python -m venv .venv
.venv\Scripts\activate
pip install -r requirements.txt
npx playwright install chromium     # 首次需要安装浏览器
python main.py

# 3. Vue 前端 (端口 5173)
cd TestFront
npm install
npm run dev
```

启动后访问：

| 服务 | 地址 | 说明 |
|------|------|------|
| 前端 | http://localhost:5173 | Vue 3 SPA |
| 后端 API | http://localhost:8080 | Go REST API |
| Executor | http://localhost:8100 | Python 执行服务（browser-use + Playwright） |
| MySQL | localhost:3306 | 数据库 |
| Redis | localhost:6379 | 缓存 |

---

## 2. 默认账号

| 角色 | 邮箱 | 密码 |
|------|------|------|
| 管理员 | admin@testpilot.local | TestPilot@2026 |
| 经理 | manager@testpilot.local | TestPilot@2026 |
| 测试员 | tester@testpilot.local | TestPilot@2026 |
| 测试员 | 18518325564@163.com | TestPilot@2026 |

---

## 3. 常用命令

```bash
go run ./cmd/server               # 启动后端（自动执行迁移 + Seed）
go run ./cmd/server migrate       # 仅执行 GORM AutoMigrate（建表/新增列）
go run ./cmd/server migrate-sql   # 仅执行增量 SQL 迁移（修改列类型等）
go run ./cmd/server seed          # 仅插入种子数据
```

---

## 4. 环境变量

### Go 后端

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `APP_PORT` | 服务端口 | `8080` |
| `DB_HOST` / `DB_PORT` / `DB_USER` / `DB_PASSWORD` / `DB_NAME` | MySQL 连接 | `127.0.0.1:3306` / `testpilot` |
| `REDIS_ADDR` / `REDIS_PASSWORD` | Redis 连接 | `127.0.0.1:6379` |
| `JWT_SECRET` | JWT 签名密钥 | `testpilot-dev-secret-...` |
| `EXECUTOR_URL` | Executor 服务地址 | `http://127.0.0.1:8100` |
| `EXECUTOR_API_KEY` | Executor 鉴权密钥 | `tp-executor-secret-key-...` |

### Executor（Python）

配置文件：`executor/.env`

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `SERVICE_PORT` | 服务端口 | `8100` |
| `OPENAI_API_KEY` | LLM API Key | - |
| `OPENAI_API_BASE` | LLM API 地址 | `https://api.openai.com/v1` |
| `OPENAI_MODEL` | LLM 模型名 | `gpt-4o` |
| `EXECUTOR_API_KEY` | API 鉴权密钥（与 Go 后端一致） | `tp-executor-secret-key-...` |
| `CODEGEN_SESSION_TIMEOUT_SEC` | Codegen 录制超时（秒） | `600` |

---

## 5. 增量 SQL 迁移

GORM AutoMigrate 只能创建新列，无法修改已有列类型。项目内置了轻量级 SQL 迁移引擎：

- 迁移文件位于 `internal/migration/sql/`，按文件名排序依次执行
- 已执行的版本记录在 `schema_migrations` 表中，幂等安全
- **服务启动时自动执行**（AutoMigrate → SQL 迁移 → Seed）

**新增迁移**：在 `internal/migration/sql/` 下添加文件，命名格式 `002_描述.sql`

---

## 6. 项目结构

```
TestPilot/
├── cmd/server/main.go              # 入口 + 子命令
├── internal/
│   ├── api/                        # API 层
│   │   ├── handler_ai_script.go    #   测试智编 Handler
│   │   ├── router.go               #   路由注册
│   │   └── ...
│   ├── service/                    # Service 层
│   │   ├── ai_script_service.go    #   测试智编核心业务
│   │   └── ...
│   ├── model/                      # 数据模型
│   │   ├── ai_script.go            #   测试智编模型
│   │   └── ...
│   ├── repository/                 # Repository 层
│   │   ├── ai_script_repo.go       #   测试智编数据访问
│   │   └── ...
│   ├── config/config.go            # 配置管理
│   ├── migration/                  # 增量 SQL 迁移
│   └── seed/                       # 种子数据
├── executor/                       # Python 执行服务
│   ├── main.py                     #   FastAPI 入口 + API Key 中间件
│   ├── config.py                   #   配置加载
│   ├── browser_runner.py           #   browser-use 浏览器自动探索
│   ├── script_generator.py         #   LLM 脚本生成 + 重构
│   ├── validation_runner.py        #   Playwright 回放验证
│   ├── requirements.txt            #   Python 依赖清单
│   └── .env                        #   环境变量（不提交）
├── docs/                           # 文档
├── .gitignore
├── Dockerfile
├── docker-compose.yml
└── go.mod

## 7. 开源依赖

### Go 后端
- **Gin** — Web 框架
- **GORM** — ORM
- **go-redis** — Redis 客户端
- **golang-jwt** — JWT 认证

### Executor（Python）
- **FastAPI + Uvicorn** — Web 服务框架
- **browser-use** — AI 驱动的浏览器自动化
- **Playwright** — 浏览器自动化（录制 + 回放）
- **OpenAI SDK** — LLM API 调用（脚本生成/重构）
- **python-dotenv** — 环境变量管理

### 前端（TestFront）
- **Vue 3 + Vite + TypeScript** — 前端框架
- **Pinia** — 状态管理
- **vue-codemirror** — 代码编辑器（脚本预览/编辑）
- **Element Plus** — UI 组件库
- **Axios** — HTTP 客户端
```


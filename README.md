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

当前 README 推荐的开发部署方式是：

- `MySQL`、`Redis` 通过 Docker Compose 启动。
- Go 后端、Python Executor、Vue 前端在本机启动，便于调试与热更新。
- 如需验证容器化后端，也可以额外执行 `docker compose --env-file .env up --build -d app`，但 Executor 仍建议本机独立启动。

补充说明：

- 关闭启动 Go 后端、Executor、Vite 的 PowerShell 窗口，会直接结束这些本机前台进程；只有 Docker 容器里的服务会继续运行。
- 默认推荐的调试形态是“`mysql/redis/app` 可容器化，`executor/front` 本机运行”；这样录制窗口、Playwright 验证和前端热更新更稳定，排障也更直接。

### 环境要求

| 依赖 | 版本 | 用途 |
|------|------|------|
| Go | 1.25+ | 后端 API |
| Python | 3.11+ | Executor 执行服务 |
| Node.js | 20+ | 前端 + Playwright |
| Docker | 24+ | MySQL 8.4 / Redis 7.4 |

### Step 0 — 启动基础设施（MySQL + Redis）

```powershell
# 在项目根目录执行，使用 .env 中的配置启动容器
docker compose --env-file .env up -d mysql redis

# 等待健康检查通过（约 10-15 秒）
docker compose ps   # STATUS 列应显示 healthy
```

> MySQL 默认账号 `testpilot / testpilot`，数据库 `testpilot`，端口 `3306`
> Redis 无密码，端口 `6379`

### Step 1 — 启动 Go 后端（端口 8080）

```powershell
# 在项目根目录执行
$env:EXECUTOR_URL = "http://127.0.0.1:8100"
go run ./cmd/server

# 或直接运行预编译二进制
.\testpilot.exe
```

首次启动会自动执行 SQL 迁移和种子数据初始化（`AUTO_SEED=true`）。

### Step 2 — 启动 Python Executor（端口 8100）

```powershell
cd executor

# 首次：创建虚拟环境并安装依赖
py -m venv .venv
.venv\Scripts\pip.exe install -r requirements.txt

# 安装 Playwright 浏览器
npx playwright install chromium

# 如 executor/.env 不存在，请先创建
# 下面仅为示例，按实际网关和模型修改
@"
OPENAI_API_KEY=your-api-key
OPENAI_BASE_URL=https://api.openai.com/v1
OPENAI_MODEL=gpt-5.4
BROWSER_HEADLESS=true
EXECUTOR_PORT=8100
EXECUTOR_API_KEY=tp-executor-secret-key-change-in-prod
"@ | Set-Content -Encoding UTF8 .env

# 可选：预装 AST 合并器依赖，避免首次 V1 增量合并时再自动安装
cd ast_merger
npm install
cd ..

# 启动服务
.venv\Scripts\python.exe main.py
```

> **注意**：Windows 上系统未注册 `python` 命令时，请使用 `py` launcher 代替。
> Executor 至少需要在 `executor/.env` 中配置 `OPENAI_API_KEY`，建议同时显式配置 `OPENAI_BASE_URL`、`OPENAI_MODEL`、`EXECUTOR_PORT`。
> 当前代码中的 `OPENAI_MODEL` 默认值仍是 `gpt-4.1`，因此建议在 `executor/.env` 中显式写入你要使用的模型，不要依赖默认值。
> `executor/ast_merger/` 会在首次执行 V1 页面增量合并时自动 `npm install`，上面的预装步骤只是为了减少第一次等待时间。

### Step 2.1 — 当前测试智编工作区说明

当前实现已经切换为“多项目 Playwright 工作区模式”，生成与验证不再只依赖单文件脚本：

- 每个项目会在 `executor/pw_projects/projects/<project_key>/` 下拥有独立工作区。
- 工作区会按需自动创建 `pages/`、`pages/shared/`、`tests/`、`fixtures/`、`registry/`、`auth_states/`。
- `fixtures/base.fixture.ts` 由程序根据 `registry/page-registry.json` 自动重建，不建议手工维护。
- `playwright.config.ts`、项目级 `.env`、共享模板页会在项目初始化时自动下发。
- 首次在某个项目工作区执行验证时，如缺少 `node_modules`，系统会自动执行 `npm install`。

### Step 3 — 启动 Vue 前端（端口 5173）

```powershell
# 在 TestFront 目录执行
cd ..\TestFront
npm install          # 首次需要安装依赖
npm run dev          # 启动 Vite 开发服务器
```

### 服务总览

| 服务 | 地址 | 默认启动方式 | 说明 |
|------|------|--------------|------|
| 前端 | http://localhost:5173 | 本机前台进程 | Vue 3 + Vite HMR |
| 后端 API | http://localhost:8080 | 本机前台进程或 `app` 容器 | Go Gin RESTful API |
| Executor | http://localhost:8100 | 本机前台进程 | Python FastAPI + AI 引擎 |
| MySQL | localhost:3306 | Docker 容器 | 基础数据库 |
| Redis | localhost:6379 | Docker 容器 | 缓存与队列依赖 |

### 验证服务是否正常

```powershell
# 检查所有端口是否已监听
netstat -ano | findstr "LISTENING" | findstr "5173 8080 8100 3306 6379"

# 测试后端健康
curl http://localhost:8080/health

# 测试 Executor 健康
curl http://localhost:8100/health
```

### 常见问题

| 问题 | 解决方案 |
|------|----------|
| `python` 命令未找到 | Windows 使用 `py` launcher 或完整路径 `.venv\Scripts\python.exe` |
| `.venv` 目录不存在 | 执行 `py -m venv .venv` 创建虚拟环境 |
| Executor 启动报 `OPENAI_API_KEY` 缺失 | 在 `executor/.env` 中配置有效的 API Key |
| Executor 启动后模型地址不生效 | 检查变量名是否为 `OPENAI_BASE_URL`，不是 `OPENAI_API_BASE` |
| MySQL 连接失败 | 确认 Docker 容器已启动：`docker compose ps` |
| 后端找不到 Executor | 确保设置了 `$env:EXECUTOR_URL = "http://127.0.0.1:8100"` |
| 首次 V1 重构或验证等待较久 | 这是项目工作区或 `ast_merger` 首次自动安装依赖，可提前执行 `npm install` 预热 |

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
| `EXECUTOR_PORT` | 服务端口 | `8100` |
| `OPENAI_API_KEY` | LLM API Key | 必填 |
| `OPENAI_BASE_URL` | LLM 网关地址 | 建议显式配置，如 `https://api.openai.com/v1` |
| `OPENAI_MODEL` | 模型 | `gpt-4.1`（代码默认值，建议显式配置） |
| `BROWSER_HEADLESS` | 浏览器是否无头运行 | `true` |
| `EXECUTOR_API_KEY` | 鉴权密钥 | 与 Go 后端一致 |
| `CODEGEN_SESSION_TIMEOUT_SEC` | 录制超时 | `600` |
| `AUTH_STATE_MAX_AGE_HOURS` | 登录态缓存有效期（小时） | `24` |
| `OCR_SERVICE_URL` | 验证码识别服务地址 | 内网默认值 |
| `DEFAULT_LOGIN_URL` / `DEFAULT_LOGIN_USERNAME` / `DEFAULT_LOGIN_PASSWORD` | 默认登录配置 | 空 |

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
│   ├── v1_generation_pipeline.py   #   多项目工程化重构管线
│   ├── validation_runner.py        #   Playwright 回放验证
│   ├── project_workspace.py        #   项目工作区初始化与补齐
│   ├── page_registry.py            #   Page Registry 管理
│   ├── fixture_builder.py          #   base.fixture.ts 自动生成
│   ├── raw_locator_guard.py        #   原始定位器/URL 语义守卫
│   ├── ast_merger/                 #   ts-morph AST 增量合并器
│   ├── pw_projects/                #   多项目 Playwright 模板与项目工作区
│   │   ├── templates/              #     项目模板（config/shared/fixture/prompt）
│   │   └── projects/               #     项目级脚本工程（按 project_key 隔离）
│   ├── pw_workspace/               #   V0 单文件验证兼容目录
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

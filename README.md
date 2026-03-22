# Aisight 测试管理平台

## 核心能力

- JWT 认证 + bcrypt 密码（access_token + refresh_token）
- 项目管理（CRUD + 成员 ACL）
- 用例管理（CRUD + 批量操作 + 模块目录树 + 附件 + Excel 导入导出 + 历史记录 + 关联关系）
- 需求、脚本实体管理 + 需求-用例、用例-脚本关联
- 执行触发（全量、单脚本、批量）+ 运行结果创建缺陷
- 用户/角色管理 + 审计日志

---

## 1. 快速开始

### 环境要求

- Docker + Docker Compose
- Go 1.25（开发环境）
- Node.js（前端开发）

### 一键启动（推荐）

```powershell
# 开发环境（本地 Go + Docker 基础设施）
.\deploy.ps1
# 自动完成: Docker Desktop → MySQL/Redis → 编译后端 → 数据库迁移 → Seed → 前端

# Docker 全容器部署
.\deploy-docker.ps1
# 自动完成: Docker Desktop → docker compose up --build → 健康检查
```

启动后访问：

| 服务 | 地址 |
|------|------|
| 前端 | http://localhost:5173 |
| 后端 API | http://localhost:8080 |
| MySQL | localhost:3306 |
| Redis | localhost:6379 |

---

## 2. 默认账号

| 角色 | 邮箱 | 密码 |
|------|------|------|
| 管理员 | admin@testpilot.local | TestPilot@2026 |
| 经理 | manager@testpilot.local | TestPilot@2026 |
| 测试员 | tester@testpilot.local | TestPilot@2026 |

---

## 3. 常用命令

```bash
go run ./cmd/server               # 启动后端（自动执行迁移 + Seed）
go run ./cmd/server migrate       # 仅执行 GORM AutoMigrate（建表/新增列）
go run ./cmd/server migrate-sql   # 仅执行增量 SQL 迁移（修改列类型等）
go run ./cmd/server seed          # 仅插入种子数据
```

---

## 4. 增量 SQL 迁移

GORM AutoMigrate 只能创建新列，无法修改已有列类型。项目内置了轻量级 SQL 迁移引擎：

- 迁移文件位于 `internal/migration/sql/`，按文件名排序依次执行
- 已执行的版本记录在 `schema_migrations` 表中，幂等安全
- **服务启动时自动执行**（AutoMigrate → SQL 迁移 → Seed）

**新增迁移**：在 `internal/migration/sql/` 下添加文件，命名格式 `002_描述.sql`

---

## 5. 项目结构

```
测试管理平台/
├── deploy.ps1                        # 一键启动（开发环境）
├── deploy-docker.ps1                 # 一键启动（Docker 全容器）
├── TestPilot/                        # 后端
│   ├── cmd/server/main.go            #   入口 + 子命令
│   ├── internal/
│   │   ├── migration/                #   增量 SQL 迁移
│   │   │   ├── migrate.go
│   │   │   └── sql/
│   │   ├── model/                    #   数据模型
│   │   ├── api/                      #   API 层
│   │   ├── service/                  #   Service 层
│   │   ├── repository/               #   Repository 层
│   │   ├── store/                    #   数据库连接
│   │   └── seed/                     #   种子数据
│   ├── Dockerfile
│   ├── docker-compose.yml
│   └── go.mod
└── TestFront/                        # 前端
```

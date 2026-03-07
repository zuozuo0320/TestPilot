# TestPilot Demo (Gin + MySQL + Redis)

TestPilot 是一个可运行的演示后端，覆盖以下能力：

- 项目管理（创建/列表）
- 需求、用例、脚本实体管理
- 需求-用例、用例-脚本关联
- 执行触发模式（全量、单脚本、批量）
- 基于运行结果创建缺陷
- 轻量用户/权限模型（全局角色 + 项目成员 ACL）
- Mock GitLab 集成入口（内部模拟）

## 1. Quick Start

### 1.1 准备环境

- Docker + Docker Compose
- Go 1.25（本地直接运行时）

### 1.2 使用 Docker Compose 运行（推荐）

```bash
cp .env.example .env
make docker-up
```

服务启动后：

- API: `http://localhost:8080`
- Health: `GET /health`

停止并清理：

```bash
make docker-down
```

### 1.3 本地直接运行

先确保本地 MySQL 和 Redis 可用，然后：

```bash
cp .env.example .env
go mod tidy
make run
```

手动补种子：

```bash
make seed
```

## 2. 常用 Makefile 命令

```bash
make run         # 本地启动服务
make test        # 运行 go test ./...
make fmt         # 执行 gofmt
make docker-up   # 启动 docker compose
make docker-down # 停止并清理 compose
make seed        # 执行 seed 数据写入
```

## 3. Demo 账号（seed 后）

使用 `X-User-ID` 作为简化鉴权头：

- `1` -> admin (`admin@testpilot.local`)
- `2` -> manager (`manager@testpilot.local`)
- `3` -> tester (`tester@testpilot.local`)

## 4. 核心接口前缀

- `/api/v1/users`
- `/api/v1/projects`
- `/api/v1/projects/:projectID/requirements`
- `/api/v1/projects/:projectID/testcases`
- `/api/v1/projects/:projectID/scripts`
- `/api/v1/projects/:projectID/runs`
- `/api/v1/projects/:projectID/defects`
- `/api/v1/projects/:projectID/demo-overview`
- `/api/v1/integrations/mock-gitlab/webhook`

完整清单和 curl 示例见：

- `D:/hsxa/ai_project/测试管理平台/架构及需求梳理/TestPilot_Demo_API清单与示例.md`

## 5. 文档交付

- `D:/hsxa/ai_project/测试管理平台/架构及需求梳理/TestPilot_Demo_开发总结.md`
- `D:/hsxa/ai_project/测试管理平台/架构及需求梳理/TestPilot_Demo_部署与运行手册.md`
- `D:/hsxa/ai_project/测试管理平台/架构及需求梳理/TestPilot_Demo_API清单与示例.md`
- `D:/hsxa/ai_project/测试管理平台/架构及需求梳理/TestPilot_Demo_测试报告.md`
- `D:/hsxa/ai_project/测试管理平台/架构及需求梳理/TestPilot_Demo_已知限制与后续计划.md`

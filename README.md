# Aisight Backend (Gin + MySQL + Redis)

Aisight 是工业级测试管理平台后端，采用四层架构（Middleware → Handler → Service → Repository）。

## 核心能力

- JWT 认证 + bcrypt 密码（access_token + refresh_token）
- 项目管理（CRUD + 成员 ACL）
- 用例管理（CRUD + 批量操作 + 模块目录树 + 附件 + Excel 导入导出 + 历史记录 + 关联关系）
- 需求、脚本实体管理 + 需求-用例、用例-脚本关联
- 执行触发（全量、单脚本、批量）+ 运行结果创建缺陷
- 用户/角色管理 + 全局角色 + 项目成员权限
- 个人中心（查看/编辑资料 + 头像上传）
- 审计日志
- 统一响应格式 + Request ID 追踪

---

## 1. Quick Start

### 1.1 准备环境

- Docker + Docker Compose
- Go 1.25（本地直接运行时）

### 1.2 使用 Docker Compose 运行（推荐）

```bash
cp .env.example .env
docker compose up -d
```

服务启动后：

| 服务 | 地址 | 说明 |
|------|------|------|
| API | http://localhost:8080 | 后端接口 |
| MySQL | localhost:3306 | 用户名/密码见 .env |
| Redis | localhost:6379 | 默认无密码 |
| Health | GET /health | 健康检查 |

CORS 默认允许：`http://localhost:5173`、`http://127.0.0.1:5173`、`http://localhost:3000`

### 1.3 本地直接运行

```bash
cp .env.example .env
go mod tidy
make run
```

---

## 2. 常用命令

```bash
make run         # 本地启动
make test        # 运行 go test ./...
make fmt         # 执行 gofmt
make docker-up   # 启动 docker compose
make docker-down # 停止并清理
make seed        # 执行 seed 数据
```

---

## 3. 默认账号（seed 后）

| 角色 | 邮箱 | 密码 |
|------|------|------|
| 管理员 | admin@testpilot.local | TestPilot@2026 |
| 经理 | manager@testpilot.local | TestPilot@2026 |
| 测试员 | tester@testpilot.local | TestPilot@2026 |

认证方式：

```
POST /api/v1/auth/login      → { access_token, refresh_token, expires_at, user }
POST /api/v1/auth/refresh    → { access_token, refresh_token, expires_at, user }

需认证接口: Authorization: Bearer <access_token>
```

---

## 4. API 接口清单

| 模块 | 路由前缀 | 操作 |
|------|---------|------|
| 认证 | `/api/v1/auth` | login, refresh |
| 用户 | `/api/v1/users` | list, create, update, delete, assignRoles, assignProjects |
| 个人 | `/api/v1/users/me` | getProfile, updateProfile, uploadAvatar |
| 角色 | `/api/v1/roles` | list, create, update, delete |
| 项目 | `/api/v1/projects` | list, create, addMember, listMembers |
| 用例 | `/api/v1/projects/:id/testcases` | list, create, update, delete, clone |
| 用例批量 | `/api/v1/projects/:id/testcases` | batch-delete, batch-update-level, batch-move |
| 用例导入导出 | `/api/v1/projects/:id/testcases` | export (xlsx), import (xlsx) |
| 用例历史 | `/api/v1/projects/:id/testcases/:id` | history |
| 用例关联 | `/api/v1/projects/:id/testcases/:id` | relations (list, create, delete) |
| 附件 | `/api/v1/projects/:id/testcases/:id/attachments` | upload, list, delete, download |
| 模块 | `/api/v1/projects/:id/modules` | list, create, rename, move, delete |
| 需求 | `/api/v1/projects/:id/requirements` | create, list, linkTestCase |
| 脚本 | `/api/v1/projects/:id/scripts` | create, list, linkTestCase |
| 执行 | `/api/v1/projects/:id/runs` | createRun, listResults |
| 缺陷 | `/api/v1/projects/:id/defects` | create, list |
| 概览 | `/api/v1/projects/:id/overview` | projectDemoOverview |
| 审计 | `/api/v1/audit-logs` | list |

### 用例查询参数

`GET /api/v1/projects/:projectID/testcases`

| 参数 | 说明 | 默认值 |
|------|------|--------|
| `page` | 页码 | 1 |
| `pageSize` | 每页条数 | 10（最大 100） |
| `keyword` | 模糊搜索（title/steps/tags/module_path） | - |
| `level` | P0/P1/P2/P3 | - |
| `review_result` | 未评审/通过/驳回 | - |
| `exec_result` | 未执行/通过/失败/阻塞 | - |
| `sortBy` | id/created_at/updated_at | - |
| `sortOrder` | asc/desc | desc |

---

## 5. 项目结构

```
TestPilot/
├── cmd/server/main.go                # 入口 + 依赖注入 + 优雅关停
├── internal/
│   ├── config/config.go              # 环境变量 + JWTSecret
│   ├── model/models.go               # 数据模型
│   ├── api/                          # API 层（20 个文件）
│   │   ├── router.go                 #   路由注册 + Dependencies
│   │   ├── middleware.go             #   CORS / JWT Auth / RequestID / Recovery / Logging
│   │   ├── helpers.go                #   bindJSON / parseUintParam / currentUser
│   │   ├── request.go                #   请求结构体 + binding 校验标签
│   │   ├── handler_auth.go           #   登录 / 刷新 Token
│   │   ├── handler_user.go           #   用户管理
│   │   ├── handler_profile.go        #   个人中心（get/update/avatar）
│   │   ├── handler_role.go           #   角色管理
│   │   ├── handler_project.go        #   项目管理
│   │   ├── handler_testcase.go       #   用例管理（含 batch/clone/history/relations）
│   │   ├── handler_module.go         #   模块目录树
│   │   ├── handler_attachment.go     #   附件管理
│   │   ├── handler_xlsx.go           #   Excel 导入导出
│   │   ├── handler_requirement.go    #   需求管理
│   │   ├── handler_script.go         #   脚本 + 关联
│   │   ├── handler_run.go            #   执行管理
│   │   ├── handler_defect.go         #   缺陷管理
│   │   ├── handler_audit.go          #   审计日志
│   │   ├── handler_overview.go       #   概览 + WebHook
│   │   └── router_test.go            #   集成测试
│   ├── service/                      # Service 层（17 个文件 + 7 个测试文件）
│   │   ├── errors.go                 #   BizError + 预定义错误
│   │   ├── validators.go             #   输入校验工具
│   │   ├── helpers.go                #   公共辅助函数
│   │   ├── auth_service.go           #   登录 + JWT + bcrypt
│   │   ├── user_service.go           #   用户 CRUD + 业务规则
│   │   ├── role_service.go           #   角色 CRUD + 预置保护
│   │   ├── project_service.go        #   项目 + 权限
│   │   ├── testcase_service.go       #   用例 CRUD + 分页
│   │   ├── module_service.go         #   模块目录树 CRUD
│   │   ├── attachment_service.go     #   附件上传/下载
│   │   ├── xlsx_service.go           #   Excel 导入/导出
│   │   ├── requirement_service.go    #   需求管理
│   │   ├── script_service.go         #   脚本管理
│   │   ├── execution_service.go      #   执行引擎
│   │   ├── defect_service.go         #   缺陷管理
│   │   ├── profile_service.go        #   个人资料
│   │   ├── audit_service.go          #   审计日志
│   │   ├── overview_service.go       #   概览统计
│   │   └── *_test.go                 #   单元测试
│   ├── repository/                   # Repository 层（14 个文件）
│   │   ├── user_repo.go
│   │   ├── role_repo.go
│   │   ├── project_repo.go
│   │   ├── testcase_repo.go
│   │   ├── module_repo.go
│   │   ├── attachment_repo.go
│   │   ├── case_history_repo.go
│   │   ├── case_relation_repo.go
│   │   ├── requirement_repo.go
│   │   ├── script_repo.go
│   │   ├── execution_repo.go
│   │   ├── defect_repo.go
│   │   ├── audit_repo.go
│   │   └── transaction.go
│   ├── dto/response/response.go      # 统一响应包装
│   ├── pkg/auth/                     # JWT + bcrypt
│   ├── execution/                    # Mock 执行器
│   ├── logging/                      # 日志
│   ├── store/                        # 数据库连接
│   └── seed/seed.go                  # 种子数据
├── Dockerfile
├── docker-compose.yml
├── Makefile
└── go.mod
```

---

## 6. 统一响应格式

```json
{
  "code": 200,
  "message": "ok",
  "data": { ... },
  "request_id": "a1b2c3..."
}
```

分页：

```json
{
  "code": 200,
  "message": "ok",
  "data": { "items": [...], "total": 100, "page": 1, "page_size": 20 },
  "request_id": "a1b2c3..."
}
```

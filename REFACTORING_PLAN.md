# TestPilot 后端重构方案

> 日期：2026-03-16 ~ 2026-03-18 | 版本：v2.2 → v3.0
> 状态：**P1-P7 全部完成** ✅ | 测试：44/44 PASS
> 目标：从 Demo 单文件架构重构为工业级四层架构（Middleware → Handler → Service → Repository）

---

## 一、重构原则

1. **渐进式**：每个阶段完成后系统可编译、可测试、可运行
2. **不改接口**：前端 API 调用保持兼容，响应格式逐步统一
3. **先拆后改**：先做文件拆分（纯搬代码），再做逻辑重构

---

## 二、阶段总览

| 阶段 | 内容 | 状态 |
|------|------|------|
| **P1** | 拆分 `router.go` 为 15 个文件 | ✅ 完成 |
| **P2** | 提取 Repository 层（10 个 Repo 文件） | ✅ 完成 |
| **P3** | 提取 Service 层（13 个 Service 文件） | ✅ 完成 |
| **P4** | 统一错误体系 + 响应格式 + Request ID + 优雅关停 | ✅ 完成 |
| **P5** | JWT 认证 + bcrypt 密码 | ✅ 完成 |
| **P6** | Service 层单元测试（38 个用例） | ✅ 完成 |
| **P7** | 输入校验 validator 化（binding tags） | ✅ 完成 |

---

## 三、各阶段详细记录

### P1：拆分 router.go ✅

**成果**：2309 行的 `router.go` 被拆分为 15 个职责清晰的文件。

```
internal/api/
├── router.go              # 路由注册 + Dependencies 依赖注入（~140 行）
├── middleware.go           # CORS / Auth / RequestID / Recovery / Logging（~150 行）
├── handler_auth.go        # login / refreshToken
├── handler_user.go        # listUsers / createUser / updateUser / deleteUser / assignRoles / assignProjects
├── handler_profile.go     # updateProfile / uploadMyAvatar / getProfile
├── handler_role.go        # listRoles / createRole / updateRole / deleteRole
├── handler_project.go     # createProject / listProjects / addProjectMember / listProjectMembers
├── handler_testcase.go    # createTestCase / listTestCases / updateTestCase / deleteTestCase
├── handler_requirement.go # createRequirement / listRequirements
├── handler_script.go      # createScript / listScripts / linkRequirementAndTestCase / linkTestCaseAndScript
├── handler_run.go         # createRun / listRunResults
├── handler_defect.go      # createDefect / listDefects
├── handler_overview.go    # projectDemoOverview / mockGitLabWebhook
├── request.go             # 所有 request 结构体 + binding 校验标签
└── helpers.go             # bindJSON / parseUintParam / currentUser / requireRole / CORS 工具
```

---

### P2：提取 Repository 层 ✅

**成果**：将 Handler 中所有 `db.xxx` 调用提取为 Repository 接口 + 实现。

```
internal/repository/
├── user_repo.go           # UserRepository 接口 + 实现
├── role_repo.go           # RoleRepository 接口 + 实现
├── project_repo.go        # ProjectRepository 接口 + 实现
├── testcase_repo.go       # TestCaseRepository 接口 + TestCaseFilter / TestCaseListItem
├── requirement_repo.go    # RequirementRepository 接口 + 实现
├── script_repo.go         # ScriptRepository 接口 + 实现
├── execution_repo.go      # RunRepository 接口 + 实现
├── defect_repo.go         # DefectRepository 接口 + 实现
├── audit_repo.go          # AuditRepository 接口 + 实现
└── tx_manager.go          # TxManager 事务管理器
```

**关键设计**：
- 每个 Repo 文件顶部定义接口，底部实现
- 事务方法使用 `xxxTx(tx *gorm.DB, ...)` 后缀，支持跨 Repo 事务
- `TxManager.WithTx()` 封装事务开始/提交/回滚

---

### P3：提取 Service 层 ✅

**成果**：Handler 变为"瘦包装" — 仅做参数解析 + 调 Service + 返回响应。

```
internal/service/
├── errors.go              # BizError 类型 + 预定义错误（ErrBadRequest / ErrConflict / ErrForbidden 等）
├── validators.go          # isValidEmail / isValidPhone / isValidPersonName 等校验工具
├── auth_service.go        # Login / RefreshToken / FindUserForAuth
├── user_service.go        # List / Create / Update / Delete / AssignRoles / AssignProjects
├── role_service.go        # List / Create / Update / Delete（含预置角色保护）
├── project_service.go     # List / Create / AddMember / ListMembers / RequireAccess
├── testcase_service.go    # Create / ListPaged / Update / Delete（含默认值填充）
├── requirement_service.go # Create / List / LinkTestCase
├── script_service.go      # Create / List / LinkTestCase
├── execution_service.go   # CreateRun / ListResults
├── defect_service.go      # Create / List
├── profile_service.go     # Update / UpdateAvatar（含邮箱禁改）
├── audit_service.go       # ListByUser / ListRecent
└── overview_service.go    # GetOverview（聚合统计）
```

**Handler 改造效果**：
| Handler | 改前行数 | 改后行数 | 压缩比 |
|---------|---------|---------|--------|
| `createUser` | ~145 行 | ~15 行 | 90% |
| `updateUser` | ~180 行 | ~20 行 | 89% |
| `createTestCase` | ~80 行 | ~15 行 | 81% |

---

### P4：统一错误 + 响应格式 + 运维增强 ✅

**新增文件**：

- `internal/dto/response/response.go` — 统一响应包装
- `internal/service/errors.go` — `BizError` 结构化错误

**统一响应格式**：

```json
{
  "code": 200,
  "message": "ok",
  "data": { ... },
  "request_id": "a1b2c3..."
}
```

分页响应：

```json
{
  "code": 200,
  "message": "ok",
  "data": {
    "items": [...],
    "total": 100,
    "page": 1,
    "page_size": 20
  },
  "request_id": "a1b2c3..."
}
```

**运维增强**：

| 特性 | 实现 |
|------|------|
| Request ID | UUID 贯穿日志 + 响应头 `X-Request-ID` |
| Panic 恢复 | recovery middleware → 结构化 500 |
| 配置校验 | 启动时检查 `DB_HOST`、`APP_PORT` 等必填字段 |
| 优雅关停 | SIGINT/SIGTERM → 10s 请求排空 |

---

### P5：JWT 认证 + bcrypt 密码 ✅

**新增文件**：

```
internal/pkg/auth/
├── jwt.go                 # GenerateTokenPair / ParseToken（HMAC-SHA256）
└── password.go            # HashPassword / CheckPassword（bcrypt）
```

**改造内容**：

| 组件 | 改前 | 改后 |
|------|------|------|
| `model.User` | 无密码字段 | 新增 `PasswordHash`（`json:"-"` 不暴露） |
| `config.go` | 无 JWT 配置 | 新增 `JWTSecret` |
| `seed.go` | 明文密码 | bcrypt 哈希 |
| `AuthService.Login` | 硬编码比对 | bcrypt 校验 + JWT 签发 |
| `authMiddleware` | 仅 `X-User-ID` | `Authorization: Bearer <jwt>` + 兼容旧 `X-User-ID` |

**认证流程**：

```
POST /api/v1/auth/login    → { access_token, refresh_token, expires_at, user }
POST /api/v1/auth/refresh  → { access_token, refresh_token, expires_at, user }

需认证接口:
  Authorization: Bearer <access_token>   ← 推荐
  X-User-ID: <id>                        ← 兼容过渡
```

---

### P6：Service 层单元测试 ✅

**测试覆盖**：38 个用例，全部通过。

```
internal/service/
├── test_helpers_test.go       # 公共辅助：testDB / seedAdmin / seedTester / seedRoles / seedProject
├── auth_service_test.go       # 8 个用例
├── user_service_test.go       # 9 个用例
├── role_service_test.go       # 7 个用例
├── project_service_test.go    # 5 个用例
├── testcase_service_test.go   # 6 个用例
└── profile_service_test.go    # 3 个用例
```

| Service | 用例数 | 覆盖场景 |
|---------|--------|----------|
| `AuthService` | 8 | 登录成功/错密码/邮箱不存在/冻结用户/空字段/刷新Token/无效Token/FindUserForAuth |
| `UserService` | 9 | 创建成功/邮箱重复/admin禁配/缺字段/admin删除保护/删除成功/列表/角色分配/项目分配 |
| `RoleService` | 7 | 创建/重复/预置删除保护/在用删除保护/删除成功/列表/更新 |
| `ProjectService` | 5 | 创建/列表(admin)/admin权限绕过/非成员拒绝/添加成员 |
| `TestCaseService` | 6 | 创建(含默认值)/分页/更新/删除成功/删除不存在 |
| `ProfileService` | 3 | 更新名字+手机/邮箱禁改/更新头像 |

**测试技术**：
- SQLite 内存数据库（每个测试独立 DSN，避免数据污染）
- testify/assert + testify/require 断言

---

### P7：输入校验 validator 化 ✅

**改造内容**：

将所有 request struct 添加声明式 `binding` 校验标签：

```go
// 改前
type createUserRequest struct {
    Name  string `json:"name"`
    Email string `json:"email"`
}

// 改后
type createUserRequest struct {
    Name  string `json:"name"  binding:"required,min=2,max=80"`
    Email string `json:"email" binding:"required,email,max=120"`
}
```

新增 `bindJSON()` 统一校验错误格式化：

```go
// 返回人可读消息
// "name is required; email must be a valid email"
// "role must be one of [admin manager tester]"
```

所有 11 个 handler 文件使用 `bindJSON(c, &req)` 替代原 `c.ShouldBindJSON(&req)`。

---

## 四、重构后目录结构

```
TestPilot/
├── cmd/server/main.go                # 入口 + 依赖注入 + 优雅关停
├── internal/
│   ├── config/config.go              # 环境变量 + JWTSecret
│   ├── model/models.go               # 数据模型（含 PasswordHash）
│   ├── api/                          # API 层（15 个文件）
│   │   ├── router.go                 #   路由注册 + Dependencies
│   │   ├── middleware.go             #   CORS / JWT Auth / RequestID / Recovery / Logging
│   │   ├── helpers.go                #   bindJSON / parseUintParam / currentUser / requireRole
│   │   ├── request.go                #   请求结构体 + binding 校验标签
│   │   ├── handler_auth.go           #   登录 / 刷新 Token
│   │   ├── handler_user.go           #   用户管理
│   │   ├── handler_profile.go        #   个人中心
│   │   ├── handler_role.go           #   角色管理
│   │   ├── handler_project.go        #   项目管理
│   │   ├── handler_testcase.go       #   用例管理
│   │   ├── handler_requirement.go    #   需求管理
│   │   ├── handler_script.go         #   脚本 + 关联
│   │   ├── handler_run.go            #   执行管理
│   │   ├── handler_defect.go         #   缺陷管理
│   │   ├── handler_overview.go       #   概览 + WebHook
│   │   └── router_test.go            #   集成测试（6 个）
│   ├── service/                      # Service 层（14 个文件 + 7 个测试文件）
│   │   ├── errors.go                 #   BizError + 预定义错误
│   │   ├── validators.go             #   输入校验工具
│   │   ├── auth_service.go           #   登录 + JWT + bcrypt
│   │   ├── user_service.go           #   用户 CRUD + 业务规则
│   │   ├── role_service.go           #   角色 CRUD + 预置保护
│   │   ├── project_service.go        #   项目 + 权限
│   │   ├── testcase_service.go       #   用例 CRUD + 分页
│   │   ├── requirement_service.go    #   需求管理
│   │   ├── script_service.go         #   脚本管理
│   │   ├── execution_service.go      #   执行引擎
│   │   ├── defect_service.go         #   缺陷管理
│   │   ├── profile_service.go        #   个人资料
│   │   ├── audit_service.go          #   审计日志
│   │   ├── overview_service.go       #   概览统计
│   │   └── *_test.go                 #   单元测试（38 个）
│   ├── repository/                   # Repository 层（10 个文件）
│   │   ├── user_repo.go
│   │   ├── role_repo.go
│   │   ├── project_repo.go
│   │   ├── testcase_repo.go
│   │   ├── requirement_repo.go
│   │   ├── script_repo.go
│   │   ├── execution_repo.go
│   │   ├── defect_repo.go
│   │   ├── audit_repo.go
│   │   └── tx_manager.go
│   ├── dto/response/response.go      # 统一响应包装
│   ├── pkg/auth/                     # 认证工具
│   │   ├── jwt.go                    #   JWT 签发 / 验证
│   │   └── password.go               #   bcrypt 哈希 / 校验
│   ├── execution/                    # Mock 执行器
│   ├── store/                        # 数据库连接
│   └── seed/seed.go                  # 种子数据
├── Dockerfile
├── docker-compose.yml
└── go.mod
```

---

## 五、测试验证结果

```
$ go test ./... -count=1

ok  testpilot/internal/api      2.463s   (6/6 PASS)
ok  testpilot/internal/service  3.599s   (38/38 PASS)

总计：44 个测试用例，全部通过
```

---

## 六、工业级规范达标对照

| 维度 | 要求 | 本项目状态 |
|------|------|-----------|
| 分层架构 | Middleware → Handler → Service → Repository | ✅ 严格四层 |
| 依赖注入 | main.go 构建依赖链，无全局变量 | ✅ |
| 统一响应 | `Result{code, message, data, request_id}` | ✅ |
| 结构化错误 | `BizError` 携带 HTTP 码 + 业务码 | ✅ |
| 认证机制 | JWT (access + refresh) + bcrypt | ✅ |
| 请求追踪 | UUID Request ID 贯穿日志 + 响应 | ✅ |
| 优雅关停 | SIGINT/SIGTERM → 10s drain | ✅ |
| Panic 恢复 | recovery middleware → 结构化 500 | ✅ |
| 配置校验 | 启动时检查必填字段 | ✅ |
| 事务管理 | TxManager + Tx 版 Repo 方法 | ✅ |
| 输入校验 | go-playground/validator binding tags | ✅ |
| 单元测试 | Service 层 38 个用例 + API 集成 6 个 | ✅ |

# 测试管理平台 — 新机器部署指南

> 本文档面向需要在新机器上搭建开发环境的团队成员。

---

## 1. 环境要求

| 软件 | 版本 | 用途 | 安装地址 |
|------|------|------|----------|
| **Docker Desktop** | ≥ 24.x | 运行 MySQL + Redis + 后端 | https://www.docker.com/products/docker-desktop |
| **Node.js** | ≥ 18.x | 前端开发 | https://nodejs.org |
| **Git** | ≥ 2.x | 拉取代码 | https://git-scm.com |
| Go *(可选)* | ≥ 1.25 | 本地运行测试 | https://go.dev |

---

## 2. 拉取代码

```bash
# 后端
git clone https://github.com/zuozuo0320/TestPilot.git

# 前端
git clone https://github.com/zuozuo0320/TestFront.git
```

---

## 3. 启动后端

```bash
cd TestPilot

# 复制环境变量模板
cp .env.example .env

# （可选）编辑 .env，修改 JWT_SECRET 等配置
# 开发阶段可直接使用默认值

# 启动（MySQL 8.4 + Redis 7.4 + Go API 服务）
docker compose up -d
```

首次启动需要拉取镜像，大约 2-5 分钟。启动完成后：

| 服务 | 地址 | 说明 |
|------|------|------|
| API 服务 | http://localhost:8080 | 后端接口 |
| MySQL | localhost:3306 | 用户名/密码见 .env |
| Redis | localhost:6379 | 默认无密码 |

验证后端是否正常：

```bash
curl http://localhost:8080/api/v1/health
# 应返回 {"code":200,"message":"ok","data":{"status":"ok"},...}
```

### 常用 Docker 命令

```bash
docker compose up -d       # 后台启动
docker compose down        # 停止并移除容器
docker compose logs -f app # 查看后端日志
docker compose restart app # 重启后端
```

---

## 4. 启动前端

```bash
cd TestFront

# 安装依赖
npm install

# 启动开发服务器
npm run dev
```

启动后访问 http://localhost:5173

---

## 5. 默认账号

首次启动后端（`AUTO_SEED=true`），系统会自动创建以下测试账号：

| 角色 | 邮箱 | 密码 |
|------|------|------|
| 管理员 | admin@testpilot.local | TestPilot@2026 |
| 测试员 | tester@testpilot.local | TestPilot@2026 |

---

## 6. 项目结构

```
测试管理平台/
├── TestPilot/           # 后端（Go + Gin）
│   ├── cmd/server/      #   入口
│   ├── internal/        #   四层架构（api → service → repository → model）
│   ├── docker-compose.yml
│   ├── Dockerfile
│   ├── .env.example     #   环境变量模板
│   └── go.mod
│
└── TestFront/           # 前端（Vue 3 + Vite）
    ├── src/
    └── package.json
```

---

## 7. 常见问题

### Q: `docker compose up` 报端口占用？

```bash
# 修改 .env 中的端口映射
APP_PORT=8081        # 改后端端口
MYSQL_PORT=3307      # 改 MySQL 端口
REDIS_PORT=6380      # 改 Redis 端口
```

### Q: 数据库连接失败？

MySQL 容器启动需要 10-20 秒初始化，后端会自动重试连接。如仍失败：

```bash
docker compose logs mysql   # 检查 MySQL 日志
docker compose restart app  # 重启后端
```

### Q: 如何清空数据库重新初始化？

```bash
docker compose down -v       # -v 删除数据卷
docker compose up -d         # 重新启动，自动建库 + 种子数据
```

### Q: 前端请求后端跨域报错？

确保 `.env` 中 `CORS_ALLOW_ORIGINS` 包含前端地址（默认已配置 `localhost:5173`）。

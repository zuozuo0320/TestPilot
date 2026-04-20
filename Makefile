.PHONY: test fmt up down rebuild logs ps

test:
	go test ./...

fmt:
	gofmt -w cmd internal

# 启动 app + mysql + redis 三个容器（Executor 与前端在宿主机另行启动）
up:
	docker compose -f docker-compose.yml --env-file .env up -d app mysql redis

# 停止并移除容器（保留 mysql_data volume，不丢数据）
down:
	docker compose -f docker-compose.yml --env-file .env down

# 基于最新代码重建 app 镜像并重启三个容器
rebuild:
	docker compose -f docker-compose.yml --env-file .env up -d --build app mysql redis

logs:
	docker compose -f docker-compose.yml --env-file .env logs -f --tail=200 app

ps:
	docker compose -f docker-compose.yml --env-file .env ps

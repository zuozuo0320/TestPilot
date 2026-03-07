.PHONY: run test fmt docker-up docker-down seed

run:
	go run ./cmd/server

test:
	go test ./...

fmt:
	gofmt -w cmd internal

docker-up:
	docker compose --env-file .env up --build -d

docker-down:
	docker compose --env-file .env down -v

seed:
	go run ./cmd/server seed

# syntax=docker/dockerfile:1

FROM golang:1.25 AS builder
WORKDIR /app

ENV GOPROXY=https://goproxy.cn,direct
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/testpilot ./cmd/server

FROM alpine:3.21
RUN adduser -D -g '' appuser
WORKDIR /app

COPY --from=builder /out/testpilot /app/testpilot
RUN mkdir -p /app/uploads/avatars /app/uploads/projects && chown -R appuser:appuser /app/uploads
USER appuser
EXPOSE 8080

ENTRYPOINT ["/app/testpilot"]

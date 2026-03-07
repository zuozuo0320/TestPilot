# syntax=docker/dockerfile:1

FROM golang:1.25 AS builder
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/testpilot ./cmd/server

FROM alpine:3.21
RUN adduser -D -g '' appuser
USER appuser
WORKDIR /app

COPY --from=builder /out/testpilot /app/testpilot
EXPOSE 8080

ENTRYPOINT ["/app/testpilot"]

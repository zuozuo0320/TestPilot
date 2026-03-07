package main

import (
	"errors"
	"net/http"
	"os"
	"time"

	"testpilot/internal/api"
	"testpilot/internal/config"
	"testpilot/internal/execution"
	"testpilot/internal/logging"
	"testpilot/internal/model"
	"testpilot/internal/seed"
	"testpilot/internal/store"
)

func main() {
	cfg := config.Load()
	logger := logging.New(cfg.LogLevel)

	db, err := store.NewMySQL(cfg, logger)
	if err != nil {
		logger.Error("mysql init failed", "error", err)
		os.Exit(1)
	}

	if err := model.AutoMigrate(db); err != nil {
		logger.Error("migration failed", "error", err)
		os.Exit(1)
	}

	command := "serve"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}

	switch command {
	case "migrate":
		logger.Info("migration done")
		return
	case "seed":
		if err := seed.Seed(db, logger); err != nil {
			logger.Error("seed failed", "error", err)
			os.Exit(1)
		}
		logger.Info("seed done")
		return
	case "serve":
	default:
		logger.Error("unknown command", "command", command)
		os.Exit(1)
	}

	if cfg.AutoSeed {
		if err := seed.Seed(db, logger); err != nil {
			logger.Error("auto seed failed", "error", err)
			os.Exit(1)
		}
	}

	redisClient, err := store.NewRedis(cfg, logger)
	if err != nil {
		logger.Warn("redis unavailable, continue without cache", "error", err)
	}

	router := api.NewRouter(api.Dependencies{
		DB:       db,
		Redis:    redisClient,
		Logger:   logger,
		Executor: execution.NewMockExecutor(logger, cfg.RunFailRate),
		AllowedOrigins: cfg.CORSAllowOrigins,
	})

	server := &http.Server{
		Addr:              cfg.HTTPAddr(),
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	logger.Info("server starting", "addr", cfg.HTTPAddr(), "auto_seed", cfg.AutoSeed)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

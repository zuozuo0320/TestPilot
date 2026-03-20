package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"testpilot/internal/api"
	"testpilot/internal/config"
	"testpilot/internal/execution"
	"testpilot/internal/model"
	pkgauth "testpilot/internal/pkg/auth"
	"testpilot/internal/repository"
	"testpilot/internal/seed"
	"testpilot/internal/service"
	"testpilot/internal/store"
)

func main() {
	cfg := config.Load()

	logLevel := slog.LevelInfo
	if cfg.LogLevel == "debug" {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}))

	// 校验配置
	if err := cfg.Validate(); err != nil {
		logger.Error("config validation failed", "error", err)
		os.Exit(1)
	}

	// 子命令
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "migrate":
			db, err := store.NewMySQL(cfg, logger)
			if err != nil {
				logger.Error("db connect failed", "error", err)
				os.Exit(1)
			}
			if err := model.AutoMigrate(db); err != nil {
				logger.Error("migrate failed", "error", err)
				os.Exit(1)
			}
			logger.Info("migration done")
			return
		case "seed":
			db, err := store.NewMySQL(cfg, logger)
			if err != nil {
				logger.Error("db connect failed", "error", err)
				os.Exit(1)
			}
			if err := seed.Seed(db, logger); err != nil {
				logger.Error("seed failed", "error", err)
				os.Exit(1)
			}
			logger.Info("seed done")
			return
		}
	}

	// 连接数据库
	db, err := store.NewMySQL(cfg, logger)
	if err != nil {
		logger.Error("db connect failed", "error", err)
		os.Exit(1)
	}
	if err := model.AutoMigrate(db); err != nil {
		logger.Error("auto migrate failed", "error", err)
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

	// ========== 三层架构构建 ==========

	// 1. Repository 层
	txMgr := repository.NewTxManager(db)
	userRepo := repository.NewUserRepo(db)
	roleRepo := repository.NewRoleRepo(db)
	projectRepo := repository.NewProjectRepo(db)
	testCaseRepo := repository.NewTestCaseRepo(db)
	auditRepo := repository.NewAuditRepo(db)
	executionRepo := repository.NewExecutionRepo(db)
	defectRepo := repository.NewDefectRepo(db)
	requirementRepo := repository.NewRequirementRepo(db)
	scriptRepo := repository.NewScriptRepo(db)
	moduleRepo := repository.NewModuleRepo(db)
	attachmentRepo := repository.NewAttachmentRepo(db)
	caseHistoryRepo := repository.NewCaseHistoryRepo(db)
	caseRelationRepo := repository.NewCaseRelationRepo(db)

	// 2. Service 层
	mockExecutor := execution.NewMockExecutor(logger, cfg.RunFailRate)
	jwtCfg := pkgauth.DefaultConfig(cfg.JWTSecret)
	authSvc := service.NewAuthService(userRepo, jwtCfg)
	userSvc := service.NewUserService(userRepo, roleRepo, projectRepo, auditRepo, txMgr)
	roleSvc := service.NewRoleService(roleRepo, auditRepo, txMgr)
	projectSvc := service.NewProjectService(projectRepo, userRepo)
	testCaseSvc := service.NewTestCaseService(testCaseRepo, caseHistoryRepo)
	profileSvc := service.NewProfileService(userRepo, auditRepo, txMgr)
	executionSvc := service.NewExecutionService(executionRepo, txMgr, mockExecutor, redisClient, logger)
	defectSvc := service.NewDefectService(defectRepo, executionRepo)
	requirementSvc := service.NewRequirementService(requirementRepo, testCaseRepo)
	scriptSvc := service.NewScriptService(scriptRepo, testCaseRepo)
	overviewSvc := service.NewOverviewService(projectRepo, requirementRepo, testCaseRepo, scriptRepo, executionRepo, defectRepo)
	auditSvc := service.NewAuditService(auditRepo)
	moduleSvc := service.NewModuleService(moduleRepo)
	attachmentSvc := service.NewAttachmentService(attachmentRepo, "./uploads")
	xlsxSvc := service.NewXlsxService(testCaseRepo)

	// 3. API 层
	router := api.NewRouter(api.Dependencies{
		Logger:             logger,
		AuthService:        authSvc,
		UserService:        userSvc,
		RoleService:        roleSvc,
		ProjectService:     projectSvc,
		TestCaseService:    testCaseSvc,
		ProfileService:     profileSvc,
		ExecutionService:   executionSvc,
		DefectService:      defectSvc,
		RequirementService: requirementSvc,
		ScriptService:      scriptSvc,
		OverviewService:    overviewSvc,
		AuditService:       auditSvc,
		ModuleService:      moduleSvc,
		AttachmentService:  attachmentSvc,
		CaseHistoryRepo:    caseHistoryRepo,
		CaseRelationRepo:   caseRelationRepo,
		XlsxService:        xlsxSvc,
	}, cfg.CORSAllowOrigins)

	server := &http.Server{
		Addr:              cfg.HTTPAddr(),
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// ========== 优雅关停 ==========
	go func() {
		logger.Info("server starting", "addr", cfg.HTTPAddr())
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	logger.Info("shutting down", "signal", sig.String())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}
	logger.Info("server stopped")
}

package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gorm.io/gorm"
	"testpilot/internal/api"
	"testpilot/internal/config"
	"testpilot/internal/execution"
	"testpilot/internal/migration"
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

	// 数据库连接逻辑助手 (严格遵循 MySQL)
	connectDB := func() (*gorm.DB, error) {
		return store.NewMySQL(cfg, logger)
	}

	// 子命令
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "cleanup-demo":
			db, err := connectDB()
			if err != nil {
				logger.Error("db connect failed", "error", err)
				os.Exit(1)
			}
			if err := seed.CleanupDemoData(db, logger); err != nil {
				logger.Error("cleanup demo failed", "error", err)
				os.Exit(1)
			}
			logger.Info("cleanup demo done")
			return
		case "seed-bootstrap":
			db, err := connectDB()
			if err != nil {
				logger.Error("db connect failed", "error", err)
				os.Exit(1)
			}
			if err := seed.SeedBootstrap(db, logger); err != nil {
				logger.Error("bootstrap seed failed", "error", err)
				os.Exit(1)
			}
			logger.Info("bootstrap seed done")
			return
		case "seed-demo":
			db, err := connectDB()
			if err != nil {
				logger.Error("db connect failed", "error", err)
				os.Exit(1)
			}
			if err := seed.SeedDemo(db, logger); err != nil {
				logger.Error("demo seed failed", "error", err)
				os.Exit(1)
			}
			logger.Info("demo seed done")
			return
		case "migrate":
			db, err := connectDB()
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
		case "migrate-sql":
			db, err := connectDB()
			if err != nil {
				logger.Error("db connect failed", "error", err)
				os.Exit(1)
			}
			if err := migration.Run(db, logger); err != nil {
				logger.Error("sql migration failed", "error", err)
				os.Exit(1)
			}
			logger.Info("sql migration done")
			return
		case "seed":
			db, err := connectDB()
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

	// 连接主数据库
	db, err := connectDB()
	if err != nil {
		logger.Error("db connect failed", "error", err)
		os.Exit(1)
	}
	if err := model.AutoMigrate(db); err != nil {
		logger.Error("auto migrate failed", "error", err)
		os.Exit(1)
	}
	// 执行增量 SQL 迁移（处理 AutoMigrate 无法完成的变更）
	if err := migration.Run(db, logger); err != nil {
		logger.Error("sql migration failed", "error", err)
		os.Exit(1)
	}
	if cfg.AutoSeed {
		if err := seed.SeedBootstrap(db, logger); err != nil {
			logger.Error("auto bootstrap seed failed", "error", err)
			os.Exit(1)
		}
	}
	if cfg.AutoSeedDemo {
		if err := seed.SeedDemo(db, logger); err != nil {
			logger.Error("auto demo seed failed", "error", err)
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
	aiScriptRepo := repository.NewAIScriptRepo(db)
	caseReviewRepo := repository.NewCaseReviewRepo(db)
	caseReviewRecordRepo := repository.NewCaseReviewRecordRepo(db)
	caseReviewAttachmentRepo := repository.NewCaseReviewAttachmentRepo(db)
	tagRepo := repository.NewTagRepo(db)

	// 2. Service 层
	mockExecutor := execution.NewMockExecutor(logger, cfg.RunFailRate)
	jwtCfg := pkgauth.DefaultConfig(cfg.JWTSecret)
	authSvc := service.NewAuthService(userRepo, jwtCfg)
	userSvc := service.NewUserService(userRepo, roleRepo, projectRepo, auditRepo, txMgr)
	roleSvc := service.NewRoleService(roleRepo, auditRepo, txMgr)
	projectSvc := service.NewProjectService(logger, projectRepo, userRepo, auditRepo, txMgr)
	testCaseSvc := service.NewTestCaseService(testCaseRepo, caseHistoryRepo, auditRepo, tagRepo)
	profileSvc := service.NewProfileService(userRepo, auditRepo, txMgr)
	executionSvc := service.NewExecutionService(executionRepo, txMgr, mockExecutor, redisClient, logger)
	defectSvc := service.NewDefectService(defectRepo, executionRepo)
	requirementSvc := service.NewRequirementService(requirementRepo, testCaseRepo)
	scriptSvc := service.NewScriptService(scriptRepo, testCaseRepo)
	overviewSvc := service.NewOverviewService(projectRepo, requirementRepo, testCaseRepo, scriptRepo, executionRepo, defectRepo)
	auditSvc := service.NewAuditService(auditRepo)
	moduleSvc := service.NewModuleService(moduleRepo, testCaseRepo)
	attachmentSvc := service.NewAttachmentService(attachmentRepo, "./uploads")
	xlsxSvc := service.NewXlsxService(testCaseRepo, tagRepo)
	aiScriptSvc := service.NewAIScriptService(aiScriptRepo, projectRepo, userRepo, txMgr, cfg.ExecutorURL, cfg.ExecutorPublicURL, cfg.ExecutorAPIKey, logger)
	caseReviewSvc := service.NewCaseReviewService(caseReviewRepo, caseReviewRecordRepo, testCaseRepo, userRepo, caseReviewAttachmentRepo, txMgr, logger)
	caseReviewSubmitSvc := service.NewCaseReviewSubmitService(caseReviewRepo, caseReviewRecordRepo, testCaseRepo, txMgr, logger)
	caseReviewAttachmentSvc := service.NewCaseReviewAttachmentService(caseReviewAttachmentRepo, caseReviewRepo, "./uploads")
	tagSvc := service.NewTagService(tagRepo, auditRepo, txMgr, logger)

	// 3. API 层
	router := api.NewRouter(api.Dependencies{
		Logger:                      logger,
		AuthService:                 authSvc,
		UserService:                 userSvc,
		RoleService:                 roleSvc,
		ProjectService:              projectSvc,
		TestCaseService:             testCaseSvc,
		ProfileService:              profileSvc,
		ExecutionService:            executionSvc,
		DefectService:               defectSvc,
		RequirementService:          requirementSvc,
		ScriptService:               scriptSvc,
		OverviewService:             overviewSvc,
		AuditService:                auditSvc,
		ModuleService:               moduleSvc,
		AttachmentService:           attachmentSvc,
		CaseHistoryRepo:             caseHistoryRepo,
		CaseRelationRepo:            caseRelationRepo,
		XlsxService:                 xlsxSvc,
		AIScriptService:             aiScriptSvc,
		CaseReviewService:           caseReviewSvc,
		CaseReviewSubmitService:     caseReviewSubmitSvc,
		CaseReviewAttachmentService: caseReviewAttachmentSvc,
		TagService:                  tagSvc,
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

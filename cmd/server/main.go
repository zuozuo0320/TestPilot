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
	"testpilot/internal/queue"
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

	// ========== 异步任务队列（Asynq）==========
	// 复用 Redis 实例（独立 DB index 隔离），构建生成任务入队器与 worker 池。
	// Redis 不可用时 genQueueClient/genQueueServer 为 nil，Service 自动降级为本地执行。
	var genQueueClient *queue.Client
	var genQueueServer *queue.Server
	genTimeout := time.Duration(cfg.ExecutorGenTimeoutSec) * time.Second
	if redisClient != nil {
		redisCfg := queue.RedisConfig{Addr: cfg.RedisAddr, Password: cfg.RedisPassword, DB: cfg.AsynqRedisDB}
		genQueueClient = queue.NewClient(redisCfg, queue.ClientOptions{
			MaxRetry:  cfg.GenMaxRetry,
			Timeout:   genTimeout + 60*time.Second, // 任务超时略大于 HTTP 超时，确保 HTTP 先超时返回
			Retention: 24 * time.Hour,
		}, logger)
		genQueueServer = queue.NewServer(redisCfg, cfg.GenWorkerConcurrency, logger)
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
	aiFlowAssetRepo := repository.NewAIFlowAssetRepo(db)
	aiAssertionAssetRepo := repository.NewAIAssertionAssetRepo(db)
	aiScenarioCompositionRepo := repository.NewAIScenarioCompositionRepo(db)
	aiAssetReferenceRepo := repository.NewAIAssetReferenceRepo(db)
	caseReviewRepo := repository.NewCaseReviewRepo(db)
	caseReviewRecordRepo := repository.NewCaseReviewRecordRepo(db)
	caseReviewAttachmentRepo := repository.NewCaseReviewAttachmentRepo(db)
	caseReviewDefectRepo := repository.NewCaseReviewDefectRepo(db)
	tagRepo := repository.NewTagRepo(db)
	aiModelConfigRepo := repository.NewAIModelConfigRepo(db)
	reqDocRepo := repository.NewRequirementDocRepo(db)
	reqDocSourceRepo := repository.NewRequirementDocSourceRepo(db)
	projectIntegrationRepo := repository.NewProjectIntegrationRepo(db)
	reqGenTaskRepo := repository.NewRequirementGenTaskRepo(db)
	reqGenResultRepo := repository.NewRequirementGenResultRepo(db)
	aiSkillRepo := repository.NewAISkillRepo(db)

	// 2. Service 层
	mockExecutor := execution.NewMockExecutor(logger, cfg.RunFailRate)
	jwtCfg := pkgauth.DefaultConfig(cfg.JWTSecret)
	authSvc := service.NewAuthService(userRepo, jwtCfg)
	userSvc := service.NewUserService(userRepo, roleRepo, projectRepo, auditRepo, txMgr)
	roleSvc := service.NewRoleService(roleRepo, auditRepo, txMgr)
	projectSvc := service.NewProjectService(logger, projectRepo, userRepo, auditRepo, txMgr)
	testCaseSvc := service.NewTestCaseService(testCaseRepo, caseHistoryRepo, auditRepo, tagRepo, caseReviewRepo, txMgr)
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
	aiModelConfigSvc := service.NewAIModelConfigService(aiModelConfigRepo, txMgr, cfg.ExecutorURL, cfg.ExecutorAPIKey, logger)
	aiScriptSvc := service.NewAIScriptService(aiScriptRepo, projectRepo, userRepo, txMgr, aiModelConfigSvc, cfg.ExecutorURL, cfg.ExecutorPublicURL, cfg.ExecutorAPIKey, logger)
	aiFlowAssetSvc := service.NewAIFlowAssetService(logger, aiFlowAssetRepo, aiAssetReferenceRepo, aiScriptRepo, projectRepo, userRepo, txMgr)
	aiAssertionAssetSvc := service.NewAIAssertionAssetService(logger, aiAssertionAssetRepo, aiAssetReferenceRepo, projectRepo, userRepo, txMgr)
	aiScenarioCompositionSvc := service.NewAIScenarioCompositionService(logger, aiScenarioCompositionRepo, aiFlowAssetRepo, aiAssertionAssetRepo, aiAssetReferenceRepo, aiScriptRepo, projectRepo, userRepo, txMgr, aiModelConfigSvc, cfg.ExecutorURL, cfg.ExecutorPublicURL, cfg.ExecutorAPIKey)
	caseReviewSvc := service.NewCaseReviewService(caseReviewRepo, caseReviewRecordRepo, testCaseRepo, userRepo, projectRepo, caseReviewAttachmentRepo, txMgr, logger)
	caseReviewSubmitSvc := service.NewCaseReviewSubmitService(caseReviewRepo, caseReviewRecordRepo, testCaseRepo, txMgr, logger)
	caseReviewAttachmentSvc := service.NewCaseReviewAttachmentService(caseReviewAttachmentRepo, caseReviewRepo, "./uploads")
	caseReviewDefectSvc := service.NewCaseReviewDefectService(caseReviewDefectRepo, caseReviewRepo, testCaseRepo, txMgr, logger)
	caseReviewRuleSvc := service.NewCaseReviewRuleService(caseReviewRepo, testCaseRepo, caseReviewDefectRepo, caseReviewDefectSvc, caseReviewSubmitSvc, txMgr, logger)
	tagSvc := service.NewTagService(tagRepo, auditRepo, txMgr, logger)
	reqDocSvc := service.NewRequirementDocService(logger, reqDocRepo, reqDocSourceRepo, txMgr, cfg.ExecutorURL, cfg.ExecutorAPIKey)
	gitLabIntegrationSvc := service.NewGitLabIntegrationService(logger, projectIntegrationRepo, reqDocSourceRepo, reqDocRepo, auditRepo, txMgr, cfg.JWTSecret, cfg.ExecutorURL, cfg.ExecutorAPIKey)
	// 入队器注入：Redis 不可用时传真正的 nil 接口（避免 typed-nil 陷阱），Service 降级本地执行
	var genEnqueuer service.GenTaskEnqueuer
	if genQueueClient != nil {
		genEnqueuer = genQueueClient
	}
	reqGenTaskSvc := service.NewRequirementGenTaskService(logger, reqGenTaskRepo, reqGenResultRepo, reqDocRepo, aiSkillRepo, tagRepo, projectRepo, aiModelConfigSvc, txMgr, cfg.ExecutorURL, cfg.ExecutorAPIKey, genEnqueuer, genTimeout)
	aiSkillSvc := service.NewAISkillService(logger, aiSkillRepo, txMgr)
	aiRegressionRepo := repository.NewAIRegressionRepo(db)
	aiRegressionSvc := service.NewAIRegressionService(logger, aiRegressionRepo, aiScenarioCompositionRepo, aiScriptRepo, userRepo, aiScenarioCompositionSvc, aiModelConfigSvc, cfg.ExecutorURL, cfg.ExecutorAPIKey)
	// setter 注入计划指标记录钩子，避免两个服务构造函数循环依赖
	aiScenarioCompositionSvc.SetPlanRecorder(aiRegressionSvc)

	// 3. API 层
	router := api.NewRouter(api.Dependencies{
		Logger:                       logger,
		AuthService:                  authSvc,
		UserService:                  userSvc,
		RoleService:                  roleSvc,
		ProjectService:               projectSvc,
		TestCaseService:              testCaseSvc,
		ProfileService:               profileSvc,
		ExecutionService:             executionSvc,
		DefectService:                defectSvc,
		RequirementService:           requirementSvc,
		ScriptService:                scriptSvc,
		OverviewService:              overviewSvc,
		AuditService:                 auditSvc,
		ModuleService:                moduleSvc,
		AttachmentService:            attachmentSvc,
		CaseHistoryRepo:              caseHistoryRepo,
		CaseRelationRepo:             caseRelationRepo,
		XlsxService:                  xlsxSvc,
		AIScriptService:              aiScriptSvc,
		AIFlowAssetService:           aiFlowAssetSvc,
		AIAssertionAssetService:      aiAssertionAssetSvc,
		AIScenarioCompositionService: aiScenarioCompositionSvc,
		CaseReviewService:            caseReviewSvc,
		CaseReviewSubmitService:      caseReviewSubmitSvc,
		CaseReviewAttachmentService:  caseReviewAttachmentSvc,
		CaseReviewRuleService:        caseReviewRuleSvc,
		CaseReviewDefectService:      caseReviewDefectSvc,
		TagService:                   tagSvc,
		AIModelConfigService:         aiModelConfigSvc,
		ReqDocService:                reqDocSvc,
		ReqGenTaskService:            reqGenTaskSvc,
		GitLabIntegrationService:     gitLabIntegrationSvc,
		AISkillService:               aiSkillSvc,
		AIRegressionService:          aiRegressionSvc,
		ExecutorURL:                  cfg.ExecutorURL,
		ExecutorAPIKey:               cfg.ExecutorAPIKey,
	}, cfg.CORSAllowOrigins)

	server := &http.Server{
		Addr:              cfg.HTTPAddr(),
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// 启动需求智生 worker 池（消费队列任务，并发由 GenWorkerConcurrency 控制）
	if genQueueServer != nil {
		if startErr := genQueueServer.Start(func(ctx context.Context, taskID uint) error {
			return reqGenTaskSvc.RunGenerate(ctx, taskID)
		}); startErr != nil {
			logger.Error("启动需求智生 worker 失败", "error", startErr)
		} else {
			logger.Info("需求智生 worker 已启动", "concurrency", cfg.GenWorkerConcurrency)
		}
	}

	if _, syncErr := aiModelConfigSvc.SyncActiveToExecutor(context.Background()); syncErr != nil {
		logger.Warn("sync active AI model to executor skipped", "error", syncErr)
	}

	// 启动定时回归调度器，关停时通过 context 退出
	schedulerCtx, schedulerCancel := context.WithCancel(context.Background())
	defer schedulerCancel()
	aiRegressionSvc.StartScheduler(schedulerCtx)
	logger.Info("AI 回归调度器已启动")

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

	schedulerCancel()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}

	// 关停 worker：停止拉取新任务，等待在途任务完成（超时后未完成任务退回队列）
	if genQueueServer != nil {
		genQueueServer.Shutdown()
	}
	if genQueueClient != nil {
		_ = genQueueClient.Close()
	}
	logger.Info("server stopped")
}

// execution_service.go — 执行管理业务逻辑
package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"testpilot/internal/execution"
	"testpilot/internal/model"
	"testpilot/internal/repository"
)

// RunOutput 创建执行任务输出
type RunOutput struct {
	Run     model.Run         `json:"run"`
	Results []model.RunResult `json:"results"`
}

// ExecutionService 执行管理服务
type ExecutionService struct {
	executionRepo repository.ExecutionRepository
	txMgr         *repository.TxManager
	executor      *execution.MockExecutor
	redis         *redis.Client
	logger        *slog.Logger
}

// NewExecutionService 创建执行服务
func NewExecutionService(
	repo repository.ExecutionRepository,
	txMgr *repository.TxManager,
	executor *execution.MockExecutor,
	redisClient *redis.Client,
	logger *slog.Logger,
) *ExecutionService {
	return &ExecutionService{
		executionRepo: repo, txMgr: txMgr, executor: executor,
		redis: redisClient, logger: logger,
	}
}

// CreateRun 创建执行任务
func (s *ExecutionService) CreateRun(ctx context.Context, projectID, userID uint, mode string, scriptID uint, scriptIDs []uint) (*RunOutput, error) {
	scripts, err := s.executionRepo.ResolveScripts(ctx, projectID, mode, scriptID, uniqueUint(scriptIDs))
	if err != nil {
		return nil, ErrBadRequest(CodeParamsError, err.Error())
	}

	run := model.Run{ProjectID: projectID, TriggeredBy: userID, Mode: mode, Status: "running"}
	results := make([]model.RunResult, 0, len(scripts))
	overallStatus := "passed"

	err = s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		if err := s.executionRepo.CreateRunTx(tx, &run); err != nil {
			return err
		}
		for _, script := range scripts {
			execResult := s.executor.RunScript(script)
			if execResult.Status == "failed" {
				overallStatus = "failed"
			}
			result := model.RunResult{
				RunID: run.ID, ProjectID: projectID, ScriptID: script.ID,
				Status: execResult.Status, Output: execResult.Output, DurationMS: execResult.DurationMS,
			}
			if err := s.executionRepo.CreateResultTx(tx, &result); err != nil {
				return err
			}
			results = append(results, result)
		}
		run.Status = overallStatus
		return s.executionRepo.SaveRunTx(tx, &run)
	})
	if err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}

	s.cacheRunStatus(ctx, run.ID, overallStatus)
	return &RunOutput{Run: run, Results: results}, nil
}

// ListResults 获取执行结果列表
func (s *ExecutionService) ListResults(ctx context.Context, runID, projectID uint) (*model.Run, []model.RunResult, error) {
	run, err := s.executionRepo.FindRun(ctx, runID, projectID)
	if err != nil {
		return nil, nil, ErrNotFound(CodeNotFound, "run not found")
	}
	results, err := s.executionRepo.ListResults(ctx, runID, projectID)
	if err != nil {
		return nil, nil, ErrInternal(CodeInternal, err)
	}
	return run, results, nil
}

// cacheRunStatus 缓存执行状态到 Redis
func (s *ExecutionService) cacheRunStatus(ctx context.Context, runID uint, status string) {
	if s.redis == nil {
		return
	}
	cacheCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := s.redis.Set(cacheCtx, fmt.Sprintf("run:%d:status", runID), status, 30*time.Minute).Err(); err != nil {
		s.logger.Warn("cache run status failed", "run_id", runID, "error", err)
	}
}

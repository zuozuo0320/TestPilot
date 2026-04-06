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
	linkedCaseIDsByScriptID, err := s.executionRepo.ListLinkedTestCaseIDsByScriptIDs(ctx, projectID, collectScriptIDs(scripts))
	if err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}

	run := model.Run{ProjectID: projectID, TriggeredBy: userID, Mode: mode, Status: "running"}
	results := make([]model.RunResult, 0, len(scripts))
	overallStatus := "passed"
	testCaseExecResults := make(map[uint]string)

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
			for _, testCaseID := range linkedCaseIDsByScriptID[script.ID] {
				testCaseExecResults[testCaseID] = mergeExecResult(testCaseExecResults[testCaseID], mapRunStatusToCaseExecResult(execResult.Status))
			}
		}
		if err := s.persistTestCaseExecResults(tx, projectID, testCaseExecResults); err != nil {
			return err
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

func collectScriptIDs(scripts []model.Script) []uint {
	ids := make([]uint, 0, len(scripts))
	for _, script := range scripts {
		ids = append(ids, script.ID)
	}
	return ids
}

func mapRunStatusToCaseExecResult(status string) string {
	switch status {
	case "passed":
		return "成功"
	case "blocked":
		return "阻塞"
	default:
		return "失败"
	}
}

func mergeExecResult(current string, next string) string {
	if current == "" {
		return next
	}
	priority := map[string]int{
		"失败": 2,
		"阻塞": 1,
		"成功": 0,
	}
	if priority[next] > priority[current] {
		return next
	}
	return current
}

func (s *ExecutionService) persistTestCaseExecResults(tx *gorm.DB, projectID uint, caseResults map[uint]string) error {
	if len(caseResults) == 0 {
		return nil
	}
	groupedIDs := make(map[string][]uint)
	for testCaseID, execResult := range caseResults {
		groupedIDs[execResult] = append(groupedIDs[execResult], testCaseID)
	}
	for execResult, ids := range groupedIDs {
		if err := s.executionRepo.UpdateTestCaseExecResultsTx(tx, projectID, ids, execResult); err != nil {
			return err
		}
	}
	return nil
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

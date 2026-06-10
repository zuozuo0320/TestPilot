// ai_regression_service.go — 阶段三（18.3）：已发布编排定时回归、AI 修复 Diff 建议与编排指标统计
package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"

	"testpilot/internal/model"
	"testpilot/internal/repository"
)

const (
	defaultRegressionTickInterval = 30 * time.Second
	minRegressionIntervalMinutes  = 5
	maxRegressionIntervalMinutes  = 7 * 24 * 60
	regressionClaimBatchSize      = 10
	repairFailureLogLimit         = 4000
)

// AIRegressionService 回归与 AI 修复闭环服务。
type AIRegressionService struct {
	logger         *slog.Logger
	regressionRepo *repository.AIRegressionRepo
	scenarioRepo   *repository.AIScenarioCompositionRepo
	aiScriptRepo   *repository.AIScriptRepo
	userRepo       repository.UserRepository
	compositionSvc *AIScenarioCompositionService
	aiModelSvc     *AIModelConfigService
	executorURL    string
	executorAPIKey string
	httpClient     *http.Client
	tickInterval   time.Duration
	nowFn          func() time.Time
}

// NewAIRegressionService 创建回归与 AI 修复闭环服务。
func NewAIRegressionService(
	logger *slog.Logger,
	regressionRepo *repository.AIRegressionRepo,
	scenarioRepo *repository.AIScenarioCompositionRepo,
	aiScriptRepo *repository.AIScriptRepo,
	userRepo repository.UserRepository,
	compositionSvc *AIScenarioCompositionService,
	aiModelSvc *AIModelConfigService,
	executorURL string,
	executorAPIKey string,
) *AIRegressionService {
	return &AIRegressionService{
		logger:         logger.With("module", "ai_regression"),
		regressionRepo: regressionRepo,
		scenarioRepo:   scenarioRepo,
		aiScriptRepo:   aiScriptRepo,
		userRepo:       userRepo,
		compositionSvc: compositionSvc,
		aiModelSvc:     aiModelSvc,
		executorURL:    strings.TrimRight(executorURL, "/"),
		executorAPIKey: executorAPIKey,
		httpClient:     &http.Client{Timeout: 300 * time.Second},
		tickInterval:   defaultRegressionTickInterval,
		nowFn:          time.Now,
	}
}

// ── 回归计划管理 ──

// RegressionPlanInput 创建/更新回归计划输入。
type RegressionPlanInput struct {
	ProjectID       uint
	CompositionID   uint
	Name            string
	IntervalMinutes int
	Enabled         *bool
}

func normalizeRegressionInterval(minutes int) (int, error) {
	if minutes == 0 {
		return 60, nil
	}
	if minutes < minRegressionIntervalMinutes || minutes > maxRegressionIntervalMinutes {
		return 0, ErrBadRequest(CodeParamsError, fmt.Sprintf("回归间隔需在 %d 到 %d 分钟之间", minRegressionIntervalMinutes, maxRegressionIntervalMinutes))
	}
	return minutes, nil
}

// CreatePlan 创建回归计划：仅允许已发布编排纳入定时回归。
func (s *AIRegressionService) CreatePlan(ctx context.Context, userID uint, input RegressionPlanInput) (*model.AIRegressionPlan, error) {
	if input.ProjectID == 0 || input.CompositionID == 0 {
		return nil, ErrBadRequest(CodeParamsError, "project_id 和 composition_id 不能为空")
	}
	interval, err := normalizeRegressionInterval(input.IntervalMinutes)
	if err != nil {
		return nil, err
	}
	composition, err := s.compositionSvc.Get(ctx, input.ProjectID, input.CompositionID)
	if err != nil {
		return nil, err
	}
	if composition.Status != model.AIScenarioStatusPublished {
		return nil, ErrConflict(CodeAIRegressionNotPublished, "仅已发布的编排可纳入定时回归")
	}
	if _, err := s.regressionRepo.GetPlanByComposition(ctx, input.ProjectID, input.CompositionID); err == nil {
		return nil, ErrConflict(CodeAIRegressionPlanExists, "该编排已存在回归计划")
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrInternal(CodeInternal, err)
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = composition.ScenarioName + " 回归"
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	now := s.nowFn()
	nextRunAt := now.Add(time.Duration(interval) * time.Minute)
	plan := &model.AIRegressionPlan{
		ProjectID:       input.ProjectID,
		CompositionID:   input.CompositionID,
		Name:            name,
		IntervalMinutes: interval,
		Enabled:         enabled,
		NextRunAt:       &nextRunAt,
		CreatedBy:       userID,
		UpdatedBy:       userID,
	}
	if err := s.regressionRepo.CreatePlan(ctx, plan); err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}
	plan.CompositionName = composition.ScenarioName
	plan.CompositionStatus = composition.Status
	return plan, nil
}

// UpdatePlan 更新回归计划（间隔/启用状态/名称）。
func (s *AIRegressionService) UpdatePlan(ctx context.Context, userID uint, planID uint, input RegressionPlanInput) (*model.AIRegressionPlan, error) {
	plan, err := s.getProjectPlan(ctx, input.ProjectID, planID)
	if err != nil {
		return nil, err
	}
	fields := map[string]interface{}{"updated_by": userID}
	if input.IntervalMinutes > 0 {
		interval, err := normalizeRegressionInterval(input.IntervalMinutes)
		if err != nil {
			return nil, err
		}
		fields["interval_minutes"] = interval
		nextRunAt := s.nowFn().Add(time.Duration(interval) * time.Minute)
		fields["next_run_at"] = nextRunAt
	}
	if name := strings.TrimSpace(input.Name); name != "" {
		fields["name"] = name
	}
	if input.Enabled != nil {
		fields["enabled"] = *input.Enabled
		if *input.Enabled && plan.NextRunAt == nil {
			interval := plan.IntervalMinutes
			if v, ok := fields["interval_minutes"].(int); ok {
				interval = v
			}
			fields["next_run_at"] = s.nowFn().Add(time.Duration(interval) * time.Minute)
		}
	}
	if err := s.regressionRepo.UpdatePlanFields(ctx, plan.ID, fields); err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}
	updated, err := s.regressionRepo.GetPlanByID(ctx, plan.ID)
	if err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}
	s.fillPlanCompositionInfo(ctx, []model.AIRegressionPlan{*updated})
	return updated, nil
}

// DeletePlan 删除回归计划。
func (s *AIRegressionService) DeletePlan(ctx context.Context, projectID, planID uint) error {
	plan, err := s.getProjectPlan(ctx, projectID, planID)
	if err != nil {
		return err
	}
	if err := s.regressionRepo.DeletePlan(ctx, plan.ID); err != nil {
		return ErrInternal(CodeInternal, err)
	}
	return nil
}

// ListPlans 查询项目回归计划列表。
func (s *AIRegressionService) ListPlans(ctx context.Context, projectID uint) ([]model.AIRegressionPlan, error) {
	if projectID == 0 {
		return nil, ErrBadRequest(CodeParamsError, "project_id 不能为空")
	}
	plans, err := s.regressionRepo.ListPlans(ctx, projectID)
	if err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}
	s.fillPlanCompositionInfo(ctx, plans)
	return plans, nil
}

func (s *AIRegressionService) getProjectPlan(ctx context.Context, projectID, planID uint) (*model.AIRegressionPlan, error) {
	if projectID == 0 {
		return nil, ErrBadRequest(CodeParamsError, "project_id 不能为空")
	}
	plan, err := s.regressionRepo.GetPlanByID(ctx, planID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound(CodeNotFound, "回归计划不存在")
		}
		return nil, ErrInternal(CodeInternal, err)
	}
	if plan.ProjectID != projectID {
		return nil, ErrForbidden(CodeForbidden, "回归计划不属于当前项目")
	}
	return plan, nil
}

func (s *AIRegressionService) fillPlanCompositionInfo(ctx context.Context, plans []model.AIRegressionPlan) {
	for i := range plans {
		composition, err := s.scenarioRepo.GetByID(ctx, plans[i].CompositionID)
		if err != nil {
			continue
		}
		plans[i].CompositionName = composition.ScenarioName
		plans[i].CompositionStatus = composition.Status
	}
}

// ── 回归执行 ──

// TriggerNow 手动触发一次回归执行。
func (s *AIRegressionService) TriggerNow(ctx context.Context, userID, projectID, planID uint) (*model.AIRegressionExecution, error) {
	plan, err := s.getProjectPlan(ctx, projectID, planID)
	if err != nil {
		return nil, err
	}
	execution := s.runRegression(ctx, plan, model.AIRegressionTriggerManual, userID)
	return execution, nil
}

// ListExecutions 分页查询回归执行记录。
func (s *AIRegressionService) ListExecutions(ctx context.Context, filter repository.AIRegressionExecutionFilter) ([]model.AIRegressionExecution, int64, error) {
	if filter.ProjectID == 0 {
		return nil, 0, ErrBadRequest(CodeParamsError, "project_id 不能为空")
	}
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 || filter.PageSize > 100 {
		filter.PageSize = 20
	}
	executions, total, err := s.regressionRepo.ListExecutions(ctx, filter)
	if err != nil {
		return nil, 0, ErrInternal(CodeInternal, err)
	}
	for i := range executions {
		if composition, err := s.scenarioRepo.GetByID(ctx, executions[i].CompositionID); err == nil {
			executions[i].CompositionName = composition.ScenarioName
		}
	}
	return executions, total, nil
}

// StartScheduler 启动定时回归调度循环，ctx 取消后退出。
func (s *AIRegressionService) StartScheduler(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(s.tickInterval)
		defer ticker.Stop()
		s.logger.Info("regression scheduler started", "tick", s.tickInterval.String())
		for {
			select {
			case <-ctx.Done():
				s.logger.Info("regression scheduler stopped")
				return
			case <-ticker.C:
				s.runDuePlans(ctx)
			}
		}
	}()
}

func (s *AIRegressionService) runDuePlans(ctx context.Context) {
	now := s.nowFn()
	plans, err := s.regressionRepo.FindDuePlans(ctx, now, regressionClaimBatchSize)
	if err != nil {
		s.logger.Error("find due regression plans failed", "error", err)
		return
	}
	for i := range plans {
		plan := plans[i]
		nextRunAt := now.Add(time.Duration(plan.IntervalMinutes) * time.Minute)
		claimed, err := s.regressionRepo.ClaimDuePlan(ctx, &plan, nextRunAt)
		if err != nil {
			s.logger.Error("claim regression plan failed", "plan_id", plan.ID, "error", err)
			continue
		}
		if !claimed {
			continue
		}
		s.runRegression(ctx, &plan, model.AIRegressionTriggerScheduled, plan.CreatedBy)
	}
}

// runRegression 执行一次回归：验证已发布编排，失败时生成 AI 修复 Diff 建议。
func (s *AIRegressionService) runRegression(ctx context.Context, plan *model.AIRegressionPlan, triggerType string, operatorID uint) *model.AIRegressionExecution {
	startedAt := s.nowFn()
	execution := &model.AIRegressionExecution{
		PlanID:        plan.ID,
		ProjectID:     plan.ProjectID,
		CompositionID: plan.CompositionID,
		TriggerType:   triggerType,
		StartedAt:     &startedAt,
	}

	composition, err := s.compositionSvc.Get(ctx, plan.ProjectID, plan.CompositionID)
	switch {
	case err != nil:
		execution.Status = model.AIRegressionExecutionStatusError
		execution.FailureSummary = truncateRunes("编排查询失败: "+err.Error(), 1000)
	case composition.Status != model.AIScenarioStatusPublished:
		execution.Status = model.AIRegressionExecutionStatusSkipped
		execution.FailureSummary = "编排当前状态不是已发布，已跳过回归"
	default:
		validation, validateErr := s.compositionSvc.Validate(ctx, operatorID, plan.CompositionID, ValidateCompositionInput{
			ProjectID: plan.ProjectID,
		})
		if validateErr != nil {
			execution.Status = model.AIRegressionExecutionStatusError
			execution.FailureSummary = truncateRunes("回归验证调用失败: "+validateErr.Error(), 1000)
		} else {
			execution.ValidationID = &validation.ID
			if validation.Status == model.AICompositionValidationStatusPassed {
				execution.Status = model.AIRegressionExecutionStatusPassed
			} else {
				execution.Status = model.AIRegressionExecutionStatusFailed
				execution.FailureSummary = truncateRunes(buildRegressionFailureSummary(validation), 1000)
			}
		}
	}

	finishedAt := s.nowFn()
	execution.FinishedAt = &finishedAt
	execution.DurationMs = finishedAt.Sub(startedAt).Milliseconds()
	if err := s.regressionRepo.CreateExecution(ctx, execution); err != nil {
		s.logger.Error("create regression execution failed", "plan_id", plan.ID, "error", err)
		return execution
	}
	if err := s.regressionRepo.UpdatePlanFields(ctx, plan.ID, map[string]interface{}{
		"last_run_at": finishedAt,
		"last_status": execution.Status,
	}); err != nil {
		s.logger.Warn("update regression plan last run failed", "plan_id", plan.ID, "error", err)
	}

	if execution.Status == model.AIRegressionExecutionStatusFailed && composition != nil {
		suggestion := s.generateRepairSuggestion(ctx, execution, composition, operatorID)
		if suggestion != nil {
			if err := s.regressionRepo.UpdateExecutionFields(ctx, execution.ID, map[string]interface{}{
				"repair_suggestion_id": suggestion.ID,
			}); err != nil {
				s.logger.Warn("link repair suggestion to execution failed", "execution_id", execution.ID, "error", err)
			} else {
				execution.RepairSuggestionID = &suggestion.ID
			}
		}
	}
	return execution
}

func buildRegressionFailureSummary(validation *model.AICompositionValidation) string {
	parts := []string{}
	for _, result := range validation.AssertionResults {
		if result.Status != model.AICompositionValidationStatusPassed && result.FailureMessage != "" {
			parts = append(parts, result.FailureMessage)
		}
	}
	if len(parts) == 0 {
		parts = append(parts, "回归验证未通过（状态 "+validation.Status+"）")
	}
	return strings.Join(parts, "；")
}

// ── AI 修复 Diff 建议 ──

// repairExecutorRequest 是发送给执行服务 /execute/repair 的请求体。
type repairExecutorRequest struct {
	CompositionID  uint   `json:"composition_id"`
	ScenarioName   string `json:"scenario_name"`
	ScriptContent  string `json:"script_content"`
	FailureSummary string `json:"failure_summary"`
	FailureLogs    string `json:"failure_logs"`
}

// repairExecutorResponse 是执行服务 /execute/repair 的响应体。
type repairExecutorResponse struct {
	Success      bool         `json:"success"`
	Summary      string       `json:"summary"`
	Diff         string       `json:"diff"`
	PatchedCode  string       `json:"patched_code"`
	Model        string       `json:"model"`
	Usage        plannerUsage `json:"usage"`
	ErrorMessage string       `json:"error_message"`
}

// generateRepairSuggestion 调用 LLM 生成修复 Diff 建议；按 14.3 约束建议仅入库为 PENDING，必须人工确认后才能应用。
func (s *AIRegressionService) generateRepairSuggestion(ctx context.Context, execution *model.AIRegressionExecution, composition *model.AIScenarioComposition, operatorID uint) *model.AIRepairSuggestion {
	suggestion := &model.AIRepairSuggestion{
		ProjectID:     execution.ProjectID,
		CompositionID: execution.CompositionID,
		ExecutionID:   execution.ID,
		Status:        model.AIRepairSuggestionStatusPending,
	}
	if s.aiModelSvc == nil {
		suggestion.Status = model.AIRepairSuggestionStatusFailed
		suggestion.ErrorMessage = fmt.Sprintf("[%d] AI 模型配置服务未接入，无法生成修复建议", CodeAIRepairModelNotConfigured)
	} else if modelCfg, err := s.aiModelSvc.SyncActiveToExecutor(ctx); err != nil {
		suggestion.Status = model.AIRepairSuggestionStatusFailed
		suggestion.ErrorMessage = fmt.Sprintf("[%d] 激活模型未配置或同步失败，无法生成修复建议", CodeAIRepairModelNotConfigured)
	} else {
		suggestion.ModelConfigID = modelCfg.ID
		suggestion.Model = modelCfg.ModelID
		resp, err := s.callRepairExecutor(ctx, repairExecutorRequest{
			CompositionID:  composition.ID,
			ScenarioName:   composition.ScenarioName,
			ScriptContent:  composition.GeneratedCode,
			FailureSummary: execution.FailureSummary,
			FailureLogs:    s.loadValidationLogs(ctx, execution),
		})
		switch {
		case err != nil:
			suggestion.Status = model.AIRepairSuggestionStatusFailed
			suggestion.ErrorMessage = truncateRunes("调用执行服务生成修复建议失败: "+err.Error(), 1000)
		case !resp.Success:
			suggestion.Status = model.AIRepairSuggestionStatusFailed
			suggestion.ErrorMessage = truncateRunes(fmt.Sprintf("[%d] %s", CodeAIRepairOutputInvalid, firstNonEmpty(resp.ErrorMessage, "AI 修复生成失败")), 1000)
		case strings.TrimSpace(resp.Diff) == "" || strings.TrimSpace(resp.PatchedCode) == "":
			suggestion.Status = model.AIRepairSuggestionStatusFailed
			suggestion.ErrorMessage = fmt.Sprintf("[%d] AI 修复输出缺少 Diff 或补丁代码", CodeAIRepairOutputInvalid)
		default:
			suggestion.Summary = truncateRunes(resp.Summary, 1000)
			suggestion.DiffContent = resp.Diff
			suggestion.PatchedCode = resp.PatchedCode
			if resp.Model != "" {
				suggestion.Model = resp.Model
			}
			suggestion.PromptTokens = resp.Usage.PromptTokens
			suggestion.CompletionTokens = resp.Usage.CompletionTokens
			suggestion.TotalTokens = resp.Usage.TotalTokens
		}
	}
	if err := s.regressionRepo.CreateSuggestion(ctx, suggestion); err != nil {
		s.logger.Error("create repair suggestion failed", "execution_id", execution.ID, "error", err)
		return nil
	}
	s.recordRepairOperationLog(ctx, model.AIScriptOperationAIRepair, execution.CompositionID, operatorID, plannerRunMeta{
		Model:            suggestion.Model,
		ModelConfigID:    suggestion.ModelConfigID,
		PromptTokens:     suggestion.PromptTokens,
		CompletionTokens: suggestion.CompletionTokens,
		TotalTokens:      suggestion.TotalTokens,
	}, suggestion)
	return suggestion
}

func (s *AIRegressionService) loadValidationLogs(ctx context.Context, execution *model.AIRegressionExecution) string {
	if execution.ValidationID == nil {
		return ""
	}
	validation, err := s.regressionRepo.GetValidationByID(ctx, *execution.ValidationID)
	if err != nil || validation == nil {
		return ""
	}
	return truncateRunes(string(validation.LogsJSON), repairFailureLogLimit)
}

func (s *AIRegressionService) callRepairExecutor(ctx context.Context, reqBody repairExecutorRequest) (*repairExecutorResponse, error) {
	if s.executorURL == "" {
		return nil, errors.New("执行服务地址未配置")
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("序列化修复请求失败: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.executorURL+"/execute/repair", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("创建修复请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if s.executorAPIKey != "" {
		req.Header.Set("X-API-Key", s.executorAPIKey)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("执行服务调用失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("执行服务返回 HTTP %d: %s", resp.StatusCode, string(errBody))
	}
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, executorBodyLimit))
	if err != nil {
		return nil, fmt.Errorf("读取执行服务响应失败: %w", err)
	}
	var result repairExecutorResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("解析执行服务响应失败: %w", err)
	}
	return &result, nil
}

func (s *AIRegressionService) recordRepairOperationLog(ctx context.Context, operationType string, compositionID uint, operatorID uint, meta plannerRunMeta, suggestion *model.AIRepairSuggestion) {
	descPayload := map[string]interface{}{
		"composition_id":    compositionID,
		"suggestion_id":     suggestion.ID,
		"suggestion_status": suggestion.Status,
		"model":             meta.Model,
		"model_config_id":   meta.ModelConfigID,
		"prompt_tokens":     meta.PromptTokens,
		"completion_tokens": meta.CompletionTokens,
		"total_tokens":      meta.TotalTokens,
	}
	desc := ""
	if data, err := json.Marshal(descPayload); err == nil {
		desc = truncateRunes(string(data), 500)
	}
	logEntry := &model.AIScriptOperationLog{
		OperationType: operationType,
		OperatorID:    operatorID,
		OperationDesc: desc,
	}
	if err := s.aiScriptRepo.CreateOperationLog(ctx, logEntry); err != nil {
		s.logger.Warn("record repair operation log failed", "suggestion_id", suggestion.ID, "error", err)
	}
}

// ── 修复建议人工确认 ──

// ListSuggestions 分页查询修复建议。
func (s *AIRegressionService) ListSuggestions(ctx context.Context, filter repository.AIRepairSuggestionFilter) ([]model.AIRepairSuggestion, int64, error) {
	if filter.ProjectID == 0 {
		return nil, 0, ErrBadRequest(CodeParamsError, "project_id 不能为空")
	}
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 || filter.PageSize > 100 {
		filter.PageSize = 20
	}
	suggestions, total, err := s.regressionRepo.ListSuggestions(ctx, filter)
	if err != nil {
		return nil, 0, ErrInternal(CodeInternal, err)
	}
	for i := range suggestions {
		if composition, err := s.scenarioRepo.GetByID(ctx, suggestions[i].CompositionID); err == nil {
			suggestions[i].CompositionName = composition.ScenarioName
		}
	}
	return suggestions, total, nil
}

// GetSuggestion 查询修复建议详情。
func (s *AIRegressionService) GetSuggestion(ctx context.Context, projectID, suggestionID uint) (*model.AIRepairSuggestion, error) {
	if projectID == 0 {
		return nil, ErrBadRequest(CodeParamsError, "project_id 不能为空")
	}
	suggestion, err := s.regressionRepo.GetSuggestionByID(ctx, suggestionID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound(CodeNotFound, "修复建议不存在")
		}
		return nil, ErrInternal(CodeInternal, err)
	}
	if suggestion.ProjectID != projectID {
		return nil, ErrForbidden(CodeForbidden, "修复建议不属于当前项目")
	}
	if composition, err := s.scenarioRepo.GetByID(ctx, suggestion.CompositionID); err == nil {
		suggestion.CompositionName = composition.ScenarioName
	}
	return suggestion, nil
}

// ApplySuggestion 人工确认后应用修复建议：通过手工补丁通道写入补丁代码（14.3 约束）。
func (s *AIRegressionService) ApplySuggestion(ctx context.Context, userID, projectID, suggestionID uint) (*model.AIScenarioComposition, error) {
	suggestion, err := s.GetSuggestion(ctx, projectID, suggestionID)
	if err != nil {
		return nil, err
	}
	if suggestion.Status != model.AIRepairSuggestionStatusPending {
		return nil, ErrConflict(CodeAIRepairSuggestionNotPending, "修复建议不处于待确认状态，不可应用")
	}
	if strings.TrimSpace(suggestion.PatchedCode) == "" {
		return nil, ErrConflict(CodeAIRepairOutputInvalid, "修复建议缺少补丁代码，无法应用")
	}
	composition, err := s.compositionSvc.ManualUpdateCode(ctx, userID, suggestion.CompositionID, ManualUpdateCompositionCodeInput{
		ProjectID:     projectID,
		GeneratedCode: suggestion.PatchedCode,
		ChangeSummary: truncateRunes(fmt.Sprintf("应用 AI 修复建议 #%d：%s", suggestion.ID, suggestion.Summary), 500),
	})
	if err != nil {
		return nil, err
	}
	now := s.nowFn()
	updated, err := s.regressionRepo.UpdateSuggestionStatusCAS(ctx, suggestion.ID, model.AIRepairSuggestionStatusPending, map[string]interface{}{
		"status":       model.AIRepairSuggestionStatusAdopted,
		"confirmed_by": userID,
		"confirmed_at": now,
	})
	if err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}
	if !updated {
		return nil, ErrConflict(CodeAIRepairSuggestionNotPending, "修复建议状态已变更，请刷新后重试")
	}
	suggestion.Status = model.AIRepairSuggestionStatusAdopted
	suggestion.ConfirmedBy = &userID
	suggestion.ConfirmedAt = &now
	s.recordRepairOperationLog(ctx, model.AIScriptOperationAIRepairApply, suggestion.CompositionID, userID, plannerRunMeta{
		Model:         suggestion.Model,
		ModelConfigID: suggestion.ModelConfigID,
	}, suggestion)
	return composition, nil
}

// RejectSuggestion 人工拒绝修复建议。
func (s *AIRegressionService) RejectSuggestion(ctx context.Context, userID, projectID, suggestionID uint) (*model.AIRepairSuggestion, error) {
	suggestion, err := s.GetSuggestion(ctx, projectID, suggestionID)
	if err != nil {
		return nil, err
	}
	if suggestion.Status != model.AIRepairSuggestionStatusPending {
		return nil, ErrConflict(CodeAIRepairSuggestionNotPending, "修复建议不处于待确认状态，不可拒绝")
	}
	now := s.nowFn()
	updated, err := s.regressionRepo.UpdateSuggestionStatusCAS(ctx, suggestion.ID, model.AIRepairSuggestionStatusPending, map[string]interface{}{
		"status":       model.AIRepairSuggestionStatusRejected,
		"confirmed_by": userID,
		"confirmed_at": now,
	})
	if err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}
	if !updated {
		return nil, ErrConflict(CodeAIRepairSuggestionNotPending, "修复建议状态已变更，请刷新后重试")
	}
	suggestion.Status = model.AIRepairSuggestionStatusRejected
	suggestion.ConfirmedBy = &userID
	suggestion.ConfirmedAt = &now
	return suggestion, nil
}

// ── 18.3 指标统计 ──

// PlanAdoptionInput 计划采纳上报输入。
type PlanAdoptionInput struct {
	ProjectID          uint
	PlanID             string
	CompositionID      uint
	AdoptedSteps       int
	ModifiedSteps      int
	ManualConfirmSteps int
}

// RecordPlanAdoption 记录 AI 计划被采纳为编排草稿的统计信息。
func (s *AIRegressionService) RecordPlanAdoption(ctx context.Context, input PlanAdoptionInput) (*model.AIPlanRecord, error) {
	if input.ProjectID == 0 || strings.TrimSpace(input.PlanID) == "" || input.CompositionID == 0 {
		return nil, ErrBadRequest(CodeParamsError, "project_id、plan_id 和 composition_id 不能为空")
	}
	if input.AdoptedSteps < 0 || input.ModifiedSteps < 0 || input.ManualConfirmSteps < 0 {
		return nil, ErrBadRequest(CodeParamsError, "采纳统计数值不能为负数")
	}
	record, err := s.regressionRepo.GetPlanRecordByPlanID(ctx, input.ProjectID, strings.TrimSpace(input.PlanID))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound(CodeNotFound, "计划记录不存在")
		}
		return nil, ErrInternal(CodeInternal, err)
	}
	if composition, err := s.compositionSvc.Get(ctx, input.ProjectID, input.CompositionID); err != nil {
		return nil, err
	} else if composition.ProjectID != input.ProjectID {
		return nil, ErrForbidden(CodeForbidden, "编排不属于当前项目")
	}
	if err := s.regressionRepo.UpdatePlanRecordFields(ctx, record.ID, map[string]interface{}{
		"composition_id":       input.CompositionID,
		"adopted_steps":        input.AdoptedSteps,
		"modified_steps":       input.ModifiedSteps,
		"manual_confirm_steps": input.ManualConfirmSteps,
	}); err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}
	return s.regressionRepo.GetPlanRecordByPlanID(ctx, input.ProjectID, record.PlanID)
}

// AIOrchestrationMetrics 18.3 指标看板数据。
type AIOrchestrationMetrics struct {
	ReuseHitRate          *float64 `json:"reuse_hit_rate"`
	ReuseHitTarget        float64  `json:"reuse_hit_target"`
	AdoptionRate          *float64 `json:"adoption_rate"`
	AdoptionTarget        float64  `json:"adoption_target"`
	FirstPassRate         *float64 `json:"first_pass_rate"`
	FirstPassTarget       float64  `json:"first_pass_target"`
	AvgManualConfirmSteps *float64 `json:"avg_manual_confirm_steps"`

	PlanCount          int64 `json:"plan_count"`
	TotalSteps         int64 `json:"total_steps"`
	FlowCallSteps      int64 `json:"flow_call_steps"`
	AdoptedPlanCount   int64 `json:"adopted_plan_count"`
	AdoptedSteps       int64 `json:"adopted_steps"`
	ModifiedSteps      int64 `json:"modified_steps"`
	FirstValidated     int64 `json:"first_validated"`
	FirstPassed        int64 `json:"first_passed"`
	RegressionTotal    int64 `json:"regression_total"`
	RegressionPassed   int64 `json:"regression_passed"`
	RegressionFailed   int64 `json:"regression_failed"`
	PendingSuggestions int64 `json:"pending_suggestions"`
	AdoptedSuggestions int64 `json:"adopted_suggestions"`
}

// Metrics 计算项目 18.3 指标：复用命中率、计划采纳率、一次验证通过率与人工干预步骤数。
func (s *AIRegressionService) Metrics(ctx context.Context, projectID uint, days int) (*AIOrchestrationMetrics, error) {
	if projectID == 0 {
		return nil, ErrBadRequest(CodeParamsError, "project_id 不能为空")
	}
	var since *time.Time
	if days > 0 {
		t := s.nowFn().AddDate(0, 0, -days)
		since = &t
	}
	agg, err := s.regressionRepo.AggregatePlanRecords(ctx, projectID, since)
	if err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}
	regressionTotal, regressionPassed, regressionFailed, err := s.regressionRepo.CountExecutionsByStatus(ctx, projectID, since)
	if err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}
	pendingSuggestions, err := s.regressionRepo.CountSuggestionsByStatus(ctx, projectID, model.AIRepairSuggestionStatusPending)
	if err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}
	adoptedSuggestions, err := s.regressionRepo.CountSuggestionsByStatus(ctx, projectID, model.AIRepairSuggestionStatusAdopted)
	if err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}

	metrics := &AIOrchestrationMetrics{
		ReuseHitTarget:     0.7,
		AdoptionTarget:     0.8,
		FirstPassTarget:    0.85,
		PlanCount:          agg.PlanCount,
		TotalSteps:         agg.TotalSteps,
		FlowCallSteps:      agg.FlowCallSteps,
		AdoptedPlanCount:   agg.AdoptedPlanCount,
		AdoptedSteps:       agg.AdoptedSteps,
		ModifiedSteps:      agg.ModifiedSteps,
		FirstValidated:     agg.FirstValidated,
		FirstPassed:        agg.FirstPassed,
		RegressionTotal:    regressionTotal,
		RegressionPassed:   regressionPassed,
		RegressionFailed:   regressionFailed,
		PendingSuggestions: pendingSuggestions,
		AdoptedSuggestions: adoptedSuggestions,
	}
	metrics.ReuseHitRate = ratioOrNil(agg.FlowCallSteps, agg.TotalSteps)
	metrics.AdoptionRate = ratioOrNil(agg.AdoptedSteps, agg.AdoptedSteps+agg.ModifiedSteps)
	metrics.FirstPassRate = ratioOrNil(agg.FirstPassed, agg.FirstValidated)
	if agg.AdoptedPlanCount > 0 {
		avg := float64(agg.ManualConfirmSteps) / float64(agg.AdoptedPlanCount)
		metrics.AvgManualConfirmSteps = &avg
	}
	return metrics, nil
}

func ratioOrNil(numerator, denominator int64) *float64 {
	if denominator <= 0 {
		return nil
	}
	v := float64(numerator) / float64(denominator)
	return &v
}

func truncateRunes(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit])
}

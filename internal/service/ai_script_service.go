// ai_script_service.go — 测试智编模块核心业务逻辑
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

// executorBodyLimit 限制从执行服务读取的最大响应体大小 (10 MB)
const executorBodyLimit = 10 << 20

// AIScriptService 测试智编业务服务
type AIScriptService struct {
	repo              *repository.AIScriptRepo
	projectRepo       repository.ProjectRepository
	userRepo          repository.UserRepository
	txMgr             *repository.TxManager
	executorURL       string // Python 执行服务地址（后端内部调用）
	executorPublicURL string // Python 执行服务地址（前端浏览器可访问）
	executorAPIKey    string // Python 执行服务 API Key
	httpClient        *http.Client
	logger            *slog.Logger
}

// NewAIScriptService 创建测试智编服务
func NewAIScriptService(
	repo *repository.AIScriptRepo,
	projectRepo repository.ProjectRepository,
	userRepo repository.UserRepository,
	txMgr *repository.TxManager,
	executorURL string,
	executorPublicURL string,
	executorAPIKey string,
	logger *slog.Logger,
) *AIScriptService {
	return &AIScriptService{
		repo:              repo,
		projectRepo:       projectRepo,
		userRepo:          userRepo,
		txMgr:             txMgr,
		executorURL:       strings.TrimRight(executorURL, "/"),
		executorPublicURL: strings.TrimRight(executorPublicURL, "/"),
		executorAPIKey:    executorAPIKey,
		httpClient:        &http.Client{Timeout: 300 * time.Second},
		logger:            logger,
	}
}

// executorPost 带鉴权的 Executor HTTP POST 请求
func (s *AIScriptService) executorPost(url string, bodyBytes []byte) (*http.Response, error) {
	req, err := http.NewRequest("POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.executorAPIKey != "" {
		req.Header.Set("X-API-Key", s.executorAPIKey)
	}
	return s.httpClient.Do(req)
}

// ── 请求/响应结构 ──

// CreateTaskInput 创建任务输入
type CreateTaskInput struct {
	ProjectID      uint   `json:"project_id"`
	TaskName       string `json:"task_name"`
	GenerationMode string `json:"generation_mode"`
	ScenarioDesc   string `json:"scenario_desc"`
	StartURL       string `json:"start_url"`
	AccountRef     string `json:"account_ref"`
	CaseIDs        []uint `json:"case_ids"`
	FrameworkType  string `json:"framework_type"`
}

// ExecutorGenerateRequest 发送给 Python 执行服务的生成请求
type ExecutorGenerateRequest struct {
	TaskID       uint   `json:"task_id"`
	ScenarioDesc string `json:"scenario_desc"`
	StartURL     string `json:"start_url"`
	AccountRef   string `json:"account_ref"`
	CallbackURL  string `json:"callback_url"`
}

// ExecutorGenerateResponse Python 执行服务生成响应
type ExecutorGenerateResponse struct {
	Success       bool                     `json:"success"`
	ScriptContent string                   `json:"script_content"`
	Traces        []ExecutorTraceItem      `json:"traces"`
	Screenshots   []ExecutorScreenshotItem `json:"screenshots"`
	ErrorMessage  string                   `json:"error_message"`
}

// ExecutorTraceItem Python 返回的轨迹条目
type ExecutorTraceItem struct {
	TraceNo          int    `json:"trace_no"`
	ActionType       string `json:"action_type"`
	PageURL          string `json:"page_url"`
	TargetSummary    string `json:"target_summary"`
	LocatorUsed      string `json:"locator_used"`
	InputValueMasked string `json:"input_value_masked"`
	ActionResult     string `json:"action_result"`
	ErrorMessage     string `json:"error_message"`
	ScreenshotURL    string `json:"screenshot_url"`
	OccurredAt       string `json:"occurred_at"`
}

// ExecutorScreenshotItem Python 返回的截图条目
type ExecutorScreenshotItem struct {
	FileName string `json:"file_name"`
	URL      string `json:"url"`
	TraceNo  *int   `json:"trace_no"`
	Caption  string `json:"caption"`
}

// ExecutorValidateRequest 发送给 Python 执行服务的验证请求
type ExecutorValidateRequest struct {
	TaskID          uint   `json:"task_id"`
	ScriptVersionID uint   `json:"script_version_id"`
	ScriptContent   string `json:"script_content"`
	StartURL        string `json:"start_url"`
	CallbackURL     string `json:"callback_url"`
}

// ExecutorValidateResponse Python 执行服务验证响应
type ExecutorValidateResponse struct {
	Success          bool                     `json:"success"`
	TotalStepCount   int                      `json:"total_step_count"`
	PassedStepCount  int                      `json:"passed_step_count"`
	FailedStepNo     *int                     `json:"failed_step_no"`
	FailReason       string                   `json:"fail_reason"`
	AssertionSummary json.RawMessage          `json:"assertion_summary"`
	DurationMs       int64                    `json:"duration_ms"`
	Logs             json.RawMessage          `json:"logs"`
	Screenshots      []ExecutorScreenshotItem `json:"screenshots"`
	ErrorMessage     string                   `json:"error_message"`
}

// EditScriptInput 脚本编辑输入
type EditScriptInput struct {
	ScriptContent string `json:"script_content"`
	CommentText   string `json:"comment_text"`
}

// ── 业务方法 ──

// CreateTask 创建生成任务（事务保证一致性）
func (s *AIScriptService) CreateTask(ctx context.Context, userID uint, input CreateTaskInput) (*model.AIScriptTask, error) {
	if strings.TrimSpace(input.TaskName) == "" {
		return nil, ErrBadRequest("MISSING_TASK_NAME", "任务名称不能为空")
	}
	if strings.TrimSpace(input.ScenarioDesc) == "" {
		return nil, ErrBadRequest("MISSING_SCENARIO_DESC", "场景描述不能为空")
	}
	if strings.TrimSpace(input.StartURL) == "" {
		return nil, ErrBadRequest("MISSING_START_URL", "起始地址不能为空")
	}
	if len(input.CaseIDs) == 0 {
		return nil, ErrBadRequest("MISSING_CASE_IDS", "至少需要关联一条测试用例")
	}

	// 去重 CaseIDs
	uniqueCaseIDs := deduplicateUints(input.CaseIDs)

	framework := strings.TrimSpace(input.FrameworkType)
	if framework == "" {
		framework = "Playwright"
	}

	genMode := strings.TrimSpace(input.GenerationMode)
	if genMode == "" {
		genMode = model.AIGenerationModeRecordingEnhanced
	}
	if genMode != model.AIGenerationModeRecordingEnhanced && genMode != model.AIGenerationModeAIDirect {
		return nil, ErrBadRequest("INVALID_GENERATION_MODE", "生成模式无效，仅支持 RECORDING_ENHANCED 或 AI_DIRECT")
	}

	task := &model.AIScriptTask{
		ProjectID:      input.ProjectID,
		TaskName:       strings.TrimSpace(input.TaskName),
		GenerationMode: genMode,
		ScenarioDesc:   strings.TrimSpace(input.ScenarioDesc),
		StartURL:       strings.TrimSpace(input.StartURL),
		AccountRef:     strings.TrimSpace(input.AccountRef),
		TaskStatus:     model.AITaskStatusPendingExecute,
		FrameworkType:  framework,
		CreatedBy:      userID,
		UpdatedBy:      userID,
	}

	// 事务：确保 task 和 case_rel 要么同时成功，要么同时回滚
	err := s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		if err := tx.Create(task).Error; err != nil {
			return fmt.Errorf("create task: %w", err)
		}

		rels := make([]model.AIScriptTaskCaseRel, len(uniqueCaseIDs))
		for i, caseID := range uniqueCaseIDs {
			rels[i] = model.AIScriptTaskCaseRel{
				TaskID:    task.ID,
				CaseID:    caseID,
				CreatedBy: userID,
			}
		}
		if err := tx.Create(&rels).Error; err != nil {
			return fmt.Errorf("create task-case relations: %w", err)
		}
		return nil
	})
	if err != nil {
		s.logger.Error("CreateTask transaction failed", "error", err, "user_id", userID)
		return nil, ErrInternal("AI_TASK_CREATE_FAILED", err)
	}

	task.CaseCount = int64(len(uniqueCaseIDs))
	return task, nil
}

// GetTask 获取任务详情（含虚拟字段填充）
func (s *AIScriptService) GetTask(ctx context.Context, taskID uint) (*model.AIScriptTask, error) {
	task, err := s.repo.GetTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound("AI_TASK_NOT_FOUND", "任务不存在")
		}
		return nil, ErrInternal("AI_TASK_QUERY_FAILED", err)
	}
	s.fillTaskVirtualFields(ctx, task)
	return task, nil
}

// ListTasks 分页查询任务列表
func (s *AIScriptService) ListTasks(ctx context.Context, projectID uint, keyword, taskStatus string, page, pageSize int) ([]model.AIScriptTask, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	tasks, total, err := s.repo.ListTasks(ctx, projectID, keyword, taskStatus, page, pageSize)
	if err != nil {
		return nil, 0, ErrInternal("AI_TASK_LIST_FAILED", err)
	}

	// 批量填充虚拟字段，减少 N+1 查询影响
	s.batchFillTaskVirtualFields(ctx, tasks)
	return tasks, total, nil
}

// ExecuteTask 触发生成任务执行（调用 Python 执行服务）
func (s *AIScriptService) ExecuteTask(ctx context.Context, userID, taskID uint) error {
	task, err := s.repo.GetTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound("AI_TASK_NOT_FOUND", "任务不存在")
		}
		return ErrInternal("AI_TASK_QUERY_FAILED", err)
	}

	// 仅 AI_DIRECT 模式可通过此接口触发，RECORDING_ENHANCED 走录制接口
	if task.GenerationMode != model.AIGenerationModeAIDirect {
		return ErrConflict("AI_TASK_MODE_MISMATCH",
			"当前任务为录制增强模式，请通过录制接口操作，此接口仅用于 AI 直生模式")
	}

	// 仅 PENDING_EXECUTE / GENERATE_FAILED 状态可触发执行
	if task.TaskStatus != model.AITaskStatusPendingExecute && task.TaskStatus != model.AITaskStatusGenerateFailed {
		return ErrConflict("AI_TASK_STATUS_INVALID",
			fmt.Sprintf("当前状态 %s 不允许执行生成，仅 DRAFT 或 GENERATE_FAILED 可触发", task.TaskStatus))
	}

	// 更新任务状态为执行中
	now := time.Now()
	if err := s.repo.UpdateTaskFields(ctx, taskID, map[string]interface{}{
		"task_status":       model.AITaskStatusRunning,
		"latest_execute_at": &now,
	}); err != nil {
		return ErrInternal("AI_TASK_UPDATE_FAILED", err)
	}

	// 异步调用 Python 执行服务
	go s.callExecutorGenerate(taskID, task.ScenarioDesc, task.StartURL, task.AccountRef, userID)

	return nil
}

// callExecutorGenerate 调用 Python 执行服务生成脚本（在 goroutine 中运行）
func (s *AIScriptService) callExecutorGenerate(taskID uint, scenarioDesc, startURL, accountRef string, userID uint) {
	ctx := context.Background()
	log := s.logger.With("task_id", taskID, "action", "generate")

	reqBody := ExecutorGenerateRequest{
		TaskID:       taskID,
		ScenarioDesc: scenarioDesc,
		StartURL:     startURL,
		AccountRef:   accountRef,
	}

	result, err := s.callExecutorHTTP(ctx, "/execute/generate", reqBody, log)
	if err != nil {
		log.Error("executor HTTP call failed", "error", err)
		_ = s.repo.UpdateTaskStatus(ctx, taskID, model.AITaskStatusGenerateFailed)
		return
	}

	var genResult ExecutorGenerateResponse
	if err := json.Unmarshal(result, &genResult); err != nil {
		log.Error("parse executor response failed", "error", err)
		_ = s.repo.UpdateTaskStatus(ctx, taskID, model.AITaskStatusGenerateFailed)
		return
	}

	if !genResult.Success {
		log.Warn("executor generate returned failure", "error_message", genResult.ErrorMessage)
		_ = s.repo.UpdateTaskStatus(ctx, taskID, model.AITaskStatusGenerateFailed)
		return
	}

	// 写入结果
	if err := s.handleGenerateResult(ctx, taskID, userID, &genResult); err != nil {
		log.Error("handleGenerateResult failed", "error", err)
		_ = s.repo.UpdateTaskStatus(ctx, taskID, model.AITaskStatusGenerateFailed)
	}
}

// handleGenerateResult 处理执行服务的生成结果回写（事务保证一致性）
func (s *AIScriptService) handleGenerateResult(ctx context.Context, taskID, userID uint, result *ExecutorGenerateResponse) error {
	return s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		// 1. 清除旧的 current flag
		if err := tx.Model(&model.AIScriptVersion{}).
			Where("task_id = ? AND is_current_flag = ?", taskID, true).
			Update("is_current_flag", false).Error; err != nil {
			return fmt.Errorf("clear current flag: %w", err)
		}

		// 2. 获取最大版本号
		var maxNo *int
		if err := tx.Model(&model.AIScriptVersion{}).
			Where("task_id = ?", taskID).
			Select("MAX(version_no)").Scan(&maxNo).Error; err != nil {
			return fmt.Errorf("get max version: %w", err)
		}
		nextNo := 1
		if maxNo != nil {
			nextNo = *maxNo + 1
		}

		// 3. 创建脚本版本
		version := &model.AIScriptVersion{
			TaskID:           taskID,
			VersionNo:        nextNo,
			FrameworkType:    "Playwright",
			ScriptName:       fmt.Sprintf("auto_gen_v%d", nextNo),
			ScriptStatus:     model.AIScriptStatusDraft,
			ValidationStatus: model.AIValidationStatusNotValidated,
			SourceType:       model.AISourceTypeAIGenerated,
			ScriptContent:    result.ScriptContent,
			IsCurrentFlag:    true,
			CreatedBy:        userID,
		}
		if err := tx.Create(version).Error; err != nil {
			return fmt.Errorf("create script version: %w", err)
		}

		// 4. 写入轨迹
		if len(result.Traces) > 0 {
			traces := make([]model.AIScriptTrace, len(result.Traces))
			for i, t := range result.Traces {
				traces[i] = model.AIScriptTrace{
					TaskID:           taskID,
					TraceNo:          t.TraceNo,
					ActionType:       t.ActionType,
					PageURL:          t.PageURL,
					TargetSummary:    t.TargetSummary,
					LocatorUsed:      t.LocatorUsed,
					InputValueMasked: t.InputValueMasked,
					ActionResult:     t.ActionResult,
					ErrorMessage:     t.ErrorMessage,
					ScreenshotURL:    t.ScreenshotURL,
					OccurredAt:       t.OccurredAt,
				}
			}
			if err := tx.Create(&traces).Error; err != nil {
				return fmt.Errorf("create traces: %w", err)
			}
		}

		// 5. 写入截图证据
		if len(result.Screenshots) > 0 {
			evidences := make([]model.AIScriptEvidence, len(result.Screenshots))
			for i, sc := range result.Screenshots {
				evidences[i] = model.AIScriptEvidence{
					TaskID:       taskID,
					EvidenceType: "SCREENSHOT",
					FileName:     sc.FileName,
					FileURL:      sc.URL,
					TraceNo:      sc.TraceNo,
					Caption:      sc.Caption,
				}
			}
			if err := tx.Create(&evidences).Error; err != nil {
				return fmt.Errorf("create evidences: %w", err)
			}
		}

		// 6. 更新任务状态
		if err := tx.Model(&model.AIScriptTask{}).Where("id = ?", taskID).Updates(map[string]interface{}{
			"task_status":               model.AITaskStatusGenerateSuccess,
			"current_script_version_id": version.ID,
		}).Error; err != nil {
			return fmt.Errorf("update task status: %w", err)
		}

		return nil
	})
}

// EditScript 编辑脚本（生成新版本，事务保证一致性）
func (s *AIScriptService) EditScript(ctx context.Context, userID, taskID uint, input EditScriptInput) (*model.AIScriptVersion, error) {
	if strings.TrimSpace(input.ScriptContent) == "" {
		return nil, ErrBadRequest("MISSING_SCRIPT_CONTENT", "脚本内容不能为空")
	}

	task, err := s.repo.GetTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound("AI_TASK_NOT_FOUND", "任务不存在")
		}
		return nil, ErrInternal("AI_TASK_QUERY_FAILED", err)
	}

	// 仅特定状态可编辑
	allowed := map[string]bool{
		model.AITaskStatusGenerateSuccess:   true,
		model.AITaskStatusPendingConfirm:    true,
		model.AITaskStatusPendingRevalidate: true,
	}
	if !allowed[task.TaskStatus] {
		return nil, ErrConflict("AI_TASK_STATUS_INVALID",
			fmt.Sprintf("当前状态 %s 不允许编辑脚本", task.TaskStatus))
	}

	// 获取当前版本作为 basedOn
	var basedOnID *uint
	currentVersion, _ := s.repo.GetCurrentScriptVersion(ctx, taskID)
	if currentVersion != nil {
		basedOnID = &currentVersion.ID
	}

	var version *model.AIScriptVersion

	// 事务：清除旧 current + 创建新版本 + 更新任务
	err = s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		// 清除旧 current flag
		if err := tx.Model(&model.AIScriptVersion{}).
			Where("task_id = ? AND is_current_flag = ?", taskID, true).
			Update("is_current_flag", false).Error; err != nil {
			return fmt.Errorf("clear current flag: %w", err)
		}

		// 获取新版本号
		var maxNo *int
		if err := tx.Model(&model.AIScriptVersion{}).
			Where("task_id = ?", taskID).
			Select("MAX(version_no)").Scan(&maxNo).Error; err != nil {
			return fmt.Errorf("get max version: %w", err)
		}
		nextNo := 1
		if maxNo != nil {
			nextNo = *maxNo + 1
		}

		version = &model.AIScriptVersion{
			TaskID:           taskID,
			VersionNo:        nextNo,
			FrameworkType:    task.FrameworkType,
			ScriptName:       fmt.Sprintf("manual_edit_v%d", nextNo),
			ScriptStatus:     model.AIScriptStatusDraft,
			ValidationStatus: model.AIValidationStatusNotValidated,
			SourceType:       model.AISourceTypeHumanEdited,
			ScriptContent:    strings.TrimSpace(input.ScriptContent),
			CommentText:      strings.TrimSpace(input.CommentText),
			BasedOnVersionID: basedOnID,
			IsCurrentFlag:    true,
			CreatedBy:        userID,
		}

		if err := tx.Create(version).Error; err != nil {
			return fmt.Errorf("create script version: %w", err)
		}

		// 更新任务状态：编辑后需要重新验证
		if err := tx.Model(&model.AIScriptTask{}).Where("id = ?", taskID).Updates(map[string]interface{}{
			"task_status":               model.AITaskStatusPendingRevalidate,
			"current_script_version_id": version.ID,
			"latest_validation_status":  model.AIValidationStatusNotValidated,
		}).Error; err != nil {
			return fmt.Errorf("update task status: %w", err)
		}

		return nil
	})
	if err != nil {
		s.logger.Error("EditScript transaction failed", "error", err, "task_id", taskID)
		return nil, ErrInternal("AI_SCRIPT_CREATE_FAILED", err)
	}

	return version, nil
}

// GetScriptVersions 获取任务的脚本版本列表
func (s *AIScriptService) GetScriptVersions(ctx context.Context, taskID uint) ([]model.AIScriptVersion, error) {
	versions, err := s.repo.ListScriptVersions(ctx, taskID)
	if err != nil {
		return nil, ErrInternal("AI_SCRIPT_LIST_FAILED", err)
	}

	// 批量查询创建人，避免 N+1
	userIDs := make([]uint, 0, len(versions))
	for _, v := range versions {
		userIDs = append(userIDs, v.CreatedBy)
	}
	userMap := s.batchGetUserNames(ctx, userIDs)
	for i := range versions {
		versions[i].CreatedName = userMap[versions[i].CreatedBy]
	}
	return versions, nil
}

// GetCurrentScript 获取任务的当前脚本版本
func (s *AIScriptService) GetCurrentScript(ctx context.Context, taskID uint) (*model.AIScriptVersion, error) {
	version, err := s.repo.GetCurrentScriptVersion(ctx, taskID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound("AI_SCRIPT_NOT_FOUND", "当前无脚本版本")
		}
		return nil, ErrInternal("AI_SCRIPT_QUERY_FAILED", err)
	}
	user, _ := s.userRepo.FindByID(ctx, version.CreatedBy)
	if user != nil {
		version.CreatedName = user.Name
	}
	return version, nil
}

// TriggerValidation 手动触发回放验证
func (s *AIScriptService) TriggerValidation(ctx context.Context, userID, taskID, scriptVersionID uint) (*model.AIScriptValidation, error) {
	// 防重复验证
	hasActive, err := s.repo.HasActiveValidation(ctx, scriptVersionID)
	if err != nil {
		return nil, ErrInternal("AI_VALIDATION_CHECK_FAILED", err)
	}
	if hasActive {
		return nil, ErrConflict("AI_VALIDATION_IN_PROGRESS", "该版本正在验证中，请稍后再试")
	}

	version, err := s.repo.GetScriptVersion(ctx, scriptVersionID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound("AI_SCRIPT_NOT_FOUND", "脚本版本不存在")
		}
		return nil, ErrInternal("AI_SCRIPT_QUERY_FAILED", err)
	}

	task, err := s.repo.GetTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound("AI_TASK_NOT_FOUND", "任务不存在")
		}
		return nil, ErrInternal("AI_TASK_QUERY_FAILED", err)
	}

	// 校验 scriptVersion 归属于此 task
	if version.TaskID != taskID {
		return nil, ErrBadRequest("AI_SCRIPT_TASK_MISMATCH", "脚本版本不属于当前任务")
	}

	// 创建验证记录
	now := time.Now()
	validation := &model.AIScriptValidation{
		ScriptVersionID:  scriptVersionID,
		TaskID:           taskID,
		TriggerType:      "MANUAL",
		ValidationStatus: model.AIValidationStatusValidating,
		StartedAt:        now,
		TriggeredBy:      userID,
	}

	if err := s.repo.CreateValidation(ctx, validation); err != nil {
		return nil, ErrInternal("AI_VALIDATION_CREATE_FAILED", err)
	}

	// 更新脚本版本的验证状态
	if err := s.repo.UpdateScriptVersionFields(ctx, scriptVersionID, map[string]interface{}{
		"validation_status":    model.AIValidationStatusValidating,
		"latest_validation_id": validation.ID,
	}); err != nil {
		s.logger.Error("update script version validation status failed", "error", err)
	}

	// 更新任务的验证状态
	if err := s.repo.UpdateTaskFields(ctx, taskID, map[string]interface{}{
		"latest_validation_id":     validation.ID,
		"latest_validation_status": model.AIValidationStatusValidating,
	}); err != nil {
		s.logger.Error("update task validation status failed", "error", err)
	}

	// 异步调用 Python 执行服务
	go s.callExecutorValidate(taskID, scriptVersionID, validation.ID, version.ScriptContent, task.StartURL)

	return validation, nil
}

// callExecutorValidate 调用 Python 执行服务回放验证（在 goroutine 中运行）
func (s *AIScriptService) callExecutorValidate(taskID, scriptVersionID, validationID uint, scriptContent, startURL string) {
	ctx := context.Background()
	log := s.logger.With("task_id", taskID, "script_version_id", scriptVersionID, "validation_id", validationID, "action", "validate")

	reqBody := ExecutorValidateRequest{
		TaskID:          taskID,
		ScriptVersionID: scriptVersionID,
		ScriptContent:   scriptContent,
		StartURL:        startURL,
	}

	rawResult, err := s.callExecutorHTTP(ctx, "/execute/validate", reqBody, log)
	if err != nil {
		log.Error("executor HTTP call failed", "error", err)
		s.failValidation(ctx, validationID, scriptVersionID, taskID, "调用执行服务失败: "+err.Error())
		return
	}

	var result ExecutorValidateResponse
	if err := json.Unmarshal(rawResult, &result); err != nil {
		log.Error("parse validate response failed", "error", err)
		s.failValidation(ctx, validationID, scriptVersionID, taskID, "解析执行服务响应失败")
		return
	}

	// 处理验证结果
	s.handleValidateResult(ctx, validationID, scriptVersionID, taskID, &result, log)
}

// handleValidateResult 处理验证结果回写
func (s *AIScriptService) handleValidateResult(ctx context.Context, validationID, scriptVersionID, taskID uint, result *ExecutorValidateResponse, log *slog.Logger) {
	now := time.Now()

	finalStatus := model.AIValidationStatusPassed
	if !result.Success {
		if result.ErrorMessage != "" {
			finalStatus = model.AIValidationStatusError
		} else {
			finalStatus = model.AIValidationStatusFailed
		}
	}

	updateFields := map[string]interface{}{
		"validation_status": finalStatus,
		"total_step_count":  result.TotalStepCount,
		"passed_step_count": result.PassedStepCount,
		"failed_step_no":    result.FailedStepNo,
		"fail_reason":       result.FailReason,
		"duration_ms":       result.DurationMs,
		"finished_at":       &now,
	}
	if result.AssertionSummary != nil {
		updateFields["assertion_summary_json"] = model.RawJSON(result.AssertionSummary)
	}
	if result.Logs != nil {
		updateFields["execution_logs_json"] = model.RawJSON(result.Logs)
	}

	if err := s.repo.UpdateValidationFields(ctx, validationID, updateFields); err != nil {
		log.Error("update validation fields failed", "error", err)
	}

	// 更新脚本版本
	if err := s.repo.UpdateScriptVersionFields(ctx, scriptVersionID, map[string]interface{}{
		"validation_status": finalStatus,
	}); err != nil {
		log.Error("update script version status failed", "error", err)
	}

	// 更新任务
	taskStatus := model.AITaskStatusGenerateSuccess
	if finalStatus == model.AIValidationStatusPassed {
		taskStatus = model.AITaskStatusPendingConfirm
	}
	if err := s.repo.UpdateTaskFields(ctx, taskID, map[string]interface{}{
		"latest_validation_status": finalStatus,
		"task_status":              taskStatus,
	}); err != nil {
		log.Error("update task status failed", "error", err)
	}

	// 写入截图证据
	if len(result.Screenshots) > 0 {
		evidences := make([]model.AIScriptEvidence, len(result.Screenshots))
		for i, sc := range result.Screenshots {
			validationIDCopy := validationID
			// 将相对路径拼接为完整 URL（如: /screenshots/xxx.png → http://127.0.0.1:8100/screenshots/xxx.png）
			fileURL := sc.URL
			if len(fileURL) > 0 && fileURL[0] == '/' {
				fileURL = s.executorPublicURL + fileURL
			}
			evidences[i] = model.AIScriptEvidence{
				TaskID:          taskID,
				ScriptVersionID: &scriptVersionID,
				ValidationID:    &validationIDCopy,
				EvidenceType:    "SCREENSHOT",
				FileName:        sc.FileName,
				FileURL:         fileURL,
				TraceNo:         sc.TraceNo,
				Caption:         sc.Caption,
			}
		}
		if err := s.repo.BatchCreateEvidences(ctx, evidences); err != nil {
			log.Error("create validation evidences failed", "error", err)
		}
	}
}

// failValidation 验证失败时的统一处理
func (s *AIScriptService) failValidation(ctx context.Context, validationID, scriptVersionID, taskID uint, reason string) {
	now := time.Now()
	if err := s.repo.UpdateValidationFields(ctx, validationID, map[string]interface{}{
		"validation_status": model.AIValidationStatusError,
		"fail_reason":       reason,
		"finished_at":       &now,
	}); err != nil {
		s.logger.Error("failValidation: update validation failed", "error", err)
	}
	if err := s.repo.UpdateScriptVersionFields(ctx, scriptVersionID, map[string]interface{}{
		"validation_status": model.AIValidationStatusError,
	}); err != nil {
		s.logger.Error("failValidation: update script version failed", "error", err)
	}
	if err := s.repo.UpdateTaskFields(ctx, taskID, map[string]interface{}{
		"latest_validation_status": model.AIValidationStatusError,
	}); err != nil {
		s.logger.Error("failValidation: update task failed", "error", err)
	}
}

// GetTraces 获取操作轨迹
func (s *AIScriptService) GetTraces(ctx context.Context, taskID uint) ([]model.AIScriptTrace, error) {
	traces, err := s.repo.ListTraces(ctx, taskID)
	if err != nil {
		return nil, ErrInternal("AI_TRACE_QUERY_FAILED", err)
	}
	return traces, nil
}

// GetLatestValidation 获取脚本版本的最近验证结果
func (s *AIScriptService) GetLatestValidation(ctx context.Context, scriptVersionID uint) (*model.AIScriptValidation, error) {
	v, err := s.repo.GetLatestValidation(ctx, scriptVersionID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound("AI_VALIDATION_NOT_FOUND", "暂无验证记录")
		}
		return nil, ErrInternal("AI_VALIDATION_QUERY_FAILED", err)
	}
	// 填充触发人名称
	user, _ := s.userRepo.FindByID(ctx, v.TriggeredBy)
	if user != nil {
		v.TriggeredName = user.Name
	}
	// 填充日志（从 DB JSON 列复制到 API 虚拟字段）
	if v.ExecutionLogsJSON != nil {
		v.Logs = json.RawMessage(v.ExecutionLogsJSON)
	} else {
		v.Logs = json.RawMessage("[]")
	}
	// 填充截图证据
	evidences, _ := s.repo.ListEvidencesByValidation(ctx, v.ID)
	if len(evidences) > 0 {
		v.Screenshots = evidences
	}
	return v, nil
}

// GetEvidences 获取任务的证据列表
func (s *AIScriptService) GetEvidences(ctx context.Context, taskID uint) ([]model.AIScriptEvidence, error) {
	evidences, err := s.repo.ListEvidences(ctx, taskID)
	if err != nil {
		return nil, ErrInternal("AI_EVIDENCE_QUERY_FAILED", err)
	}
	return evidences, nil
}

// ── 内部辅助方法 ──

// callExecutorHTTP 统一的执行服务 HTTP 调用（含响应码校验 + body 大小限制）
func (s *AIScriptService) callExecutorHTTP(ctx context.Context, path string, reqBody interface{}, log *slog.Logger) (json.RawMessage, error) {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.executorURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if s.executorAPIKey != "" {
		req.Header.Set("X-API-Key", s.executorAPIKey)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http call: %w", err)
	}
	defer resp.Body.Close()

	// 校验 HTTP 响应码
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// 读有限的 body 用于错误诊断
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("executor returned HTTP %d: %s", resp.StatusCode, string(errBody))
	}

	// 限制响应体大小，防止 OOM
	limitedReader := io.LimitReader(resp.Body, executorBodyLimit)
	respBody, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	return respBody, nil
}

// fillTaskVirtualFields 填充单个任务的虚拟字段
func (s *AIScriptService) fillTaskVirtualFields(ctx context.Context, task *model.AIScriptTask) {
	project, _ := s.projectRepo.FindByID(ctx, task.ProjectID)
	if project != nil {
		task.ProjectName = project.Name
	}

	user, _ := s.userRepo.FindByID(ctx, task.CreatedBy)
	if user != nil {
		task.CreatedName = user.Name
	}

	count, _ := s.repo.CountTaskCases(ctx, task.ID)
	task.CaseCount = count
}

// batchFillTaskVirtualFields 批量填充任务列表的虚拟字段（减少 N+1 查询）
func (s *AIScriptService) batchFillTaskVirtualFields(ctx context.Context, tasks []model.AIScriptTask) {
	if len(tasks) == 0 {
		return
	}

	// 收集所有需要查询的 userID 和 projectID（去重）
	userIDs := make([]uint, 0, len(tasks))
	projectIDs := make([]uint, 0, len(tasks))
	for _, t := range tasks {
		userIDs = append(userIDs, t.CreatedBy)
		projectIDs = append(projectIDs, t.ProjectID)
	}

	userMap := s.batchGetUserNames(ctx, deduplicateUints(userIDs))
	projectMap := s.batchGetProjectNames(ctx, deduplicateUints(projectIDs))

	for i := range tasks {
		tasks[i].CreatedName = userMap[tasks[i].CreatedBy]
		tasks[i].ProjectName = projectMap[tasks[i].ProjectID]

		count, _ := s.repo.CountTaskCases(ctx, tasks[i].ID)
		tasks[i].CaseCount = count
	}
}

// batchGetUserNames 批量查询用户名
func (s *AIScriptService) batchGetUserNames(ctx context.Context, ids []uint) map[uint]string {
	result := make(map[uint]string, len(ids))
	for _, id := range ids {
		if _, exists := result[id]; exists {
			continue
		}
		user, _ := s.userRepo.FindByID(ctx, id)
		if user != nil {
			result[id] = user.Name
		}
	}
	return result
}

// batchGetProjectNames 批量查询项目名
func (s *AIScriptService) batchGetProjectNames(ctx context.Context, ids []uint) map[uint]string {
	result := make(map[uint]string, len(ids))
	for _, id := range ids {
		if _, exists := result[id]; exists {
			continue
		}
		project, _ := s.projectRepo.FindByID(ctx, id)
		if project != nil {
			result[id] = project.Name
		}
	}
	return result
}

// deduplicateUints 去重 uint 切片
// deduplicateUints 去重 uint 切片
func deduplicateUints(input []uint) []uint {
	seen := make(map[uint]struct{}, len(input))
	result := make([]uint, 0, len(input))
	for _, v := range input {
		if _, exists := seen[v]; !exists {
			seen[v] = struct{}{}
			result = append(result, v)
		}
	}
	return result
}

// ── 新增业务方法（阶段一） ──

// ConfirmScript 确认脚本
func (s *AIScriptService) ConfirmScript(ctx context.Context, userID, scriptID uint) error {
	version, err := s.repo.GetScriptVersion(ctx, scriptID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound("AI_SCRIPT_4004", "脚本版本不存在")
		}
		return ErrInternal("AI_SCRIPT_QUERY_FAILED", err)
	}
	if version.ScriptStatus == model.AIScriptStatusDiscarded {
		return ErrConflict("AI_SCRIPT_4010", "已废弃版本不允许操作")
	}
	if version.ValidationStatus != model.AIValidationStatusPassed {
		return ErrConflict("AI_SCRIPT_4007", "脚本尚未验证通过，不允许确认")
	}
	if !version.IsCurrentFlag {
		return ErrConflict("AI_SCRIPT_4008", "当前版本不是任务主版本")
	}

	now := time.Now()
	if err := s.repo.UpdateScriptVersionFields(ctx, scriptID, map[string]interface{}{
		"script_status": model.AIScriptStatusConfirmed,
		"confirmed_by":  &userID,
		"confirmed_at":  &now,
	}); err != nil {
		return ErrInternal("AI_SCRIPT_CONFIRM_FAILED", err)
	}

	// 同步任务状态
	if err := s.repo.UpdateTaskFields(ctx, version.TaskID, map[string]interface{}{
		"task_status":          model.AITaskStatusConfirmed,
		"latest_confirmed_at":  &now,
		"latest_confirmed_by":  &userID,
	}); err != nil {
		s.logger.Error("ConfirmScript: update task status failed", "error", err)
	}
	return nil
}

// DiscardScript 废弃脚本版本
func (s *AIScriptService) DiscardScript(ctx context.Context, userID, scriptID uint, reason string) error {
	version, err := s.repo.GetScriptVersion(ctx, scriptID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound("AI_SCRIPT_4004", "脚本版本不存在")
		}
		return ErrInternal("AI_SCRIPT_QUERY_FAILED", err)
	}
	if version.ScriptStatus == model.AIScriptStatusDiscarded {
		return ErrConflict("AI_SCRIPT_4010", "已废弃版本不允许操作")
	}

	if err := s.repo.UpdateScriptVersionFields(ctx, scriptID, map[string]interface{}{
		"script_status": model.AIScriptStatusDiscarded,
		"comment_text":  reason,
	}); err != nil {
		return ErrInternal("AI_SCRIPT_DISCARD_FAILED", err)
	}
	return nil
}

// DiscardTask 废弃任务
func (s *AIScriptService) DiscardTask(ctx context.Context, userID, taskID uint, reason string) error {
	task, err := s.repo.GetTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound("AI_SCRIPT_4003", "任务不存在")
		}
		return ErrInternal("AI_TASK_QUERY_FAILED", err)
	}
	if task.TaskStatus == model.AITaskStatusDiscarded {
		return ErrConflict("AI_SCRIPT_4005", "任务已废弃，不可重复操作")
	}

	if err := s.repo.UpdateTaskFields(ctx, taskID, map[string]interface{}{
		"task_status":    model.AITaskStatusDiscarded,
		"discard_reason": reason,
	}); err != nil {
		return ErrInternal("AI_TASK_DISCARD_FAILED", err)
	}
	return nil
}

// DeleteTask 删除已废弃任务（物理删除 + 级联清理）
func (s *AIScriptService) DeleteTask(ctx context.Context, userID, taskID uint) error {
	task, err := s.repo.GetTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound("AI_SCRIPT_4003", "任务不存在")
		}
		return ErrInternal("AI_TASK_QUERY_FAILED", err)
	}
	if task.TaskStatus != model.AITaskStatusDiscarded {
		return ErrConflict("AI_SCRIPT_4011", "仅允许删除已废弃任务")
	}

	if err := s.repo.DeleteTask(ctx, taskID); err != nil {
		return ErrInternal("AI_TASK_DELETE_FAILED", err)
	}
	return nil
}

// CloneTask 复制任务配置生成新任务
func (s *AIScriptService) CloneTask(ctx context.Context, userID, taskID uint, newTaskName string) (*model.AIScriptTask, error) {
	task, err := s.repo.GetTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound("AI_SCRIPT_4003", "任务不存在")
		}
		return nil, ErrInternal("AI_TASK_QUERY_FAILED", err)
	}

	// 获取原任务关联的用例
	caseIDs, err := s.repo.GetTaskCaseIDs(ctx, taskID)
	if err != nil {
		return nil, ErrInternal("AI_TASK_CASE_QUERY_FAILED", err)
	}

	return s.CreateTask(ctx, userID, CreateTaskInput{
		ProjectID:      task.ProjectID,
		TaskName:       newTaskName,
		GenerationMode: task.GenerationMode,
		ScenarioDesc:   task.ScenarioDesc,
		StartURL:       task.StartURL,
		AccountRef:     task.AccountRef,
		CaseIDs:        caseIDs,
		FrameworkType:  task.FrameworkType,
	})
}

// StartRecording 启动录制会话
func (s *AIScriptService) StartRecording(ctx context.Context, userID, taskID uint) (*model.AIScriptRecordingSession, error) {
	task, err := s.repo.GetTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound("AI_SCRIPT_4003", "任务不存在")
		}
		return nil, ErrInternal("AI_TASK_QUERY_FAILED", err)
	}
	if task.GenerationMode != model.AIGenerationModeRecordingEnhanced {
		return nil, ErrConflict("AI_TASK_MODE_MISMATCH", "仅录制增强模式支持录制")
	}
	if task.TaskStatus != model.AITaskStatusPendingExecute && task.TaskStatus != model.AITaskStatusGenerateFailed {
		return nil, ErrConflict("AI_SCRIPT_4005", fmt.Sprintf("当前状态 %s 不允许开始录制", task.TaskStatus))
	}

	// #4 并发录制互斥检查：同一任务只允许一个活跃录制
	existingRecording, _ := s.repo.FindLatestRecordingByTaskID(ctx, taskID)
	if existingRecording != nil && existingRecording.RecordingStatus == model.AIRecordingStatusRecording {
		return nil, ErrConflict("AI_RECORDING_IN_PROGRESS", "该任务已有进行中的录制会话，请等待完成或取消后重试")
	}

	session := &model.AIScriptRecordingSession{
		TaskID:          taskID,
		RecorderType:    "PLAYWRIGHT_CODEGEN",
		RecordingStatus: model.AIRecordingStatusRecording,
		CreatedBy:       userID,
	}
	if err := s.repo.CreateRecordingSession(ctx, session); err != nil {
		return nil, ErrInternal("AI_RECORDING_CREATE_FAILED", err)
	}

	// 更新任务状态
	if err := s.repo.UpdateTaskFields(ctx, taskID, map[string]interface{}{
		"task_status":          model.AITaskStatusRunning,
		"latest_recording_id":  &session.ID,
		"latest_execute_at":    time.Now(),
	}); err != nil {
		s.logger.Error("StartRecording: update task status failed", "error", err)
	}

	return session, nil
}

// FinishRecording 结束录制
func (s *AIScriptService) FinishRecording(ctx context.Context, userID, taskID uint, recordingID uint, rawScript string, triggerAIRefactor bool) error {
	now := time.Now()
	updates := map[string]interface{}{
		"recording_status":  model.AIRecordingStatusFinished,
		"raw_script_content": rawScript,
		"finished_at":        &now,
	}
	if err := s.repo.UpdateRecordingSessionFields(ctx, recordingID, updates); err != nil {
		return ErrInternal("AI_RECORDING_UPDATE_FAILED", err)
	}

	// 如果需要触发 AI 重构，可在此异步调用 executor/refactor
	if triggerAIRefactor {
		go s.callExecutorRefactor(taskID, recordingID, rawScript, userID)
	}
	return nil
}

// GetLatestRecording 获取最近一次录制结果
func (s *AIScriptService) GetLatestRecording(ctx context.Context, taskID uint) (*model.AIScriptRecordingSession, error) {
	session, err := s.repo.FindLatestRecordingByTaskID(ctx, taskID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound("AI_RECORDING_NOT_FOUND", "未找到录制记录")
		}
		return nil, ErrInternal("AI_RECORDING_QUERY_FAILED", err)
	}
	return session, nil
}

// callExecutorRefactor 异步调用执行服务进行 AI 重构（录制增强模式）
func (s *AIScriptService) callExecutorRefactor(taskID, recordingID uint, rawScript string, userID uint) {
	ctx := context.Background()
	s.logger.Info("Calling executor refactor", "task_id", taskID, "recording_id", recordingID)

	// 获取任务信息，传入场景描述和起始地址
	task, _ := s.repo.GetTask(ctx, taskID)
	scenarioDesc := ""
	startURL := ""
	if task != nil {
		scenarioDesc = task.ScenarioDesc
		startURL = task.StartURL
	}

	reqBody := map[string]interface{}{
		"task_id":       taskID,
		"recording_id":  recordingID,
		"raw_script":    rawScript,
		"scenario_desc": scenarioDesc,
		"start_url":     startURL,
	}
	bodyBytes, _ := json.Marshal(reqBody)

	url := fmt.Sprintf("%s/execute/refactor", s.executorURL)
	resp, err := s.executorPost(url, bodyBytes)
	if err != nil {
		s.logger.Error("callExecutorRefactor: HTTP call failed", "error", err, "task_id", taskID)
		_ = s.repo.UpdateTaskFields(ctx, taskID, map[string]interface{}{
			"task_status": model.AITaskStatusGenerateFailed,
		})
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, executorBodyLimit))

	// 使用 map 解析以提取 step_model_json
	var rawResult map[string]interface{}
	if err := json.Unmarshal(respBody, &rawResult); err != nil {
		s.logger.Error("callExecutorRefactor: parse response failed", "error", err, "task_id", taskID)
		_ = s.repo.UpdateTaskFields(ctx, taskID, map[string]interface{}{
			"task_status": model.AITaskStatusGenerateFailed,
		})
		return
	}

	// 保存 step_model_json 到录制会话
	if stepModel, ok := rawResult["step_model_json"]; ok && stepModel != nil {
		stepModelBytes, _ := json.Marshal(stepModel)
		_ = s.repo.UpdateRecordingSessionFields(ctx, recordingID, map[string]interface{}{
			"step_model_json": string(stepModelBytes),
		})
		s.logger.Info("Saved step_model_json to recording", "recording_id", recordingID)
	}

	// 转换为标准响应
	var result ExecutorGenerateResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		s.logger.Error("callExecutorRefactor: parse result failed", "error", err, "task_id", taskID)
		_ = s.repo.UpdateTaskFields(ctx, taskID, map[string]interface{}{
			"task_status": model.AITaskStatusGenerateFailed,
		})
		return
	}

	if !result.Success {
		s.logger.Warn("callExecutorRefactor: refactor failed", "task_id", taskID, "error", result.ErrorMessage)
		_ = s.repo.UpdateTaskFields(ctx, taskID, map[string]interface{}{
			"task_status": model.AITaskStatusGenerateFailed,
		})
		return
	}

	// 保存脚本版本
	s.saveGeneratedScript(ctx, taskID, result, userID, model.AISourceTypeAIEnhancedFromRecording, &recordingID)
}

// saveGeneratedScript 通用保存生成脚本逻辑
func (s *AIScriptService) saveGeneratedScript(ctx context.Context, taskID uint, result ExecutorGenerateResponse, userID uint, sourceType string, recordingID *uint) {
	// 获取最大版本号
	maxVersion, _ := s.repo.MaxVersionNo(ctx, taskID)

	version := &model.AIScriptVersion{
		TaskID:            taskID,
		VersionNo:         maxVersion + 1,
		FrameworkType:     "Playwright",
		ScriptStatus:      model.AIScriptStatusDraft,
		ValidationStatus:  model.AIValidationStatusNotValidated,
		SourceType:        sourceType,
		SourceRecordingID: recordingID,
		ScriptContent:     result.ScriptContent,
		IsCurrentFlag:     true,
		CreatedBy:         userID,
		UpdatedBy:         userID,
	}
	if err := s.repo.ClearCurrentFlag(ctx, taskID); err != nil {
		s.logger.Error("saveGeneratedScript: clear current flag failed", "error", err)
	}
	if err := s.repo.CreateScriptVersion(ctx, version); err != nil {
		s.logger.Error("saveGeneratedScript: create script version failed", "error", err, "task_id", taskID)
		return
	}

	// 保存轨迹和截图
	if len(result.Traces) > 0 {
		traces := make([]model.AIScriptTrace, len(result.Traces))
		for i, t := range result.Traces {
			traces[i] = model.AIScriptTrace{
				TaskID:           taskID,
				TraceNo:          t.TraceNo,
				ActionType:       t.ActionType,
				PageURL:          t.PageURL,
				TargetSummary:    t.TargetSummary,
				LocatorUsed:      t.LocatorUsed,
				InputValueMasked: t.InputValueMasked,
				ActionResult:     t.ActionResult,
				ErrorMessage:     t.ErrorMessage,
				ScreenshotURL:    t.ScreenshotURL,
				OccurredAt:       t.OccurredAt,
			}
		}
		if err := s.repo.BatchCreateTraces(ctx, traces); err != nil {
			s.logger.Error("saveGeneratedScript: create traces failed", "error", err)
		}
	}
	if len(result.Screenshots) > 0 {
		evidences := make([]model.AIScriptEvidence, len(result.Screenshots))
		for i, sc := range result.Screenshots {
			evidences[i] = model.AIScriptEvidence{
				TaskID:          taskID,
				ScriptVersionID: &version.ID,
				EvidenceType:    "SCREENSHOT",
				FileName:        sc.FileName,
				FileURL:         sc.URL,
				TraceNo:         sc.TraceNo,
				Caption:         sc.Caption,
			}
		}
		if err := s.repo.BatchCreateEvidences(ctx, evidences); err != nil {
			s.logger.Error("saveGeneratedScript: create evidences failed", "error", err)
		}
	}

	// 更新任务状态
	_ = s.repo.UpdateTaskFields(ctx, taskID, map[string]interface{}{
		"task_status":               model.AITaskStatusGenerateSuccess,
		"current_script_version_id": &version.ID,
	})
}

// ComputePermissions 根据用户角色和任务状态计算操作权限
func (s *AIScriptService) ComputePermissions(ctx context.Context, userID uint, task *model.AIScriptTask) *model.ActionPermissions {
	// 获取用户角色
	roleName := s.getUserRole(ctx, userID, task.ProjectID)

	perms := &model.ActionPermissions{}

	isAdmin := roleName == "admin"
	isManager := roleName == "manager"
	isTester := roleName == "tester"
	isReviewer := roleName == "reviewer"

	canWrite := isAdmin || isManager || isTester

	// can_execute: 录制增强显示开始录制, AI直生显示执行生成
	perms.CanExecute = canWrite && (task.TaskStatus == model.AITaskStatusPendingExecute || task.TaskStatus == model.AITaskStatusGenerateFailed)

	// can_validate: 存在脚本版本且非验证中
	hasScript := task.CurrentScriptVersionID != nil
	perms.CanValidate = canWrite && hasScript && task.LatestValidationStatus != model.AIValidationStatusValidating

	// can_edit: 脚本存在且未废弃
	perms.CanEdit = canWrite && hasScript && task.TaskStatus != model.AITaskStatusDiscarded

	// can_confirm: admin/manager/reviewer 且验证通过
	perms.CanConfirm = (isAdmin || isManager || isReviewer) && task.LatestValidationStatus == model.AIValidationStatusPassed

	// can_export: admin/manager/tester/reviewer
	perms.CanExport = (isAdmin || isManager || isTester || isReviewer) && hasScript

	// can_discard: admin/manager
	perms.CanDiscard = (isAdmin || isManager) && task.TaskStatus != model.AITaskStatusDiscarded

	// can_delete: admin/manager 且已废弃
	perms.CanDelete = (isAdmin || isManager) && task.TaskStatus == model.AITaskStatusDiscarded

	return perms
}

// getUserRole 获取用户在项目中的角色
func (s *AIScriptService) getUserRole(ctx context.Context, userID, projectID uint) string {
	user, err := s.userRepo.FindByID(ctx, userID)
	if err != nil || user == nil {
		return ""
	}
	// User 模型中 Role 字段直接存储主角色名称
	if user.Role == "" {
		return "readonly"
	}
	return user.Role
}

// GetValidationHistory 获取验证历史
func (s *AIScriptService) GetValidationHistory(ctx context.Context, scriptVersionID uint) ([]model.AIScriptValidation, error) {
	return s.repo.ListValidationsByScriptID(ctx, scriptVersionID)
}

// UpdateTaskCases 更新任务关联用例
func (s *AIScriptService) UpdateTaskCases(ctx context.Context, userID, taskID uint, caseIDs []uint) error {
	if len(caseIDs) == 0 {
		return ErrBadRequest("AI_SCRIPT_4002", "至少需要关联一条测试用例")
	}

	_, err := s.repo.GetTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound("AI_SCRIPT_4003", "任务不存在")
		}
		return ErrInternal("AI_TASK_QUERY_FAILED", err)
	}

	uniqueIDs := deduplicateUints(caseIDs)

	return s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		// 删除旧关系
		if err := tx.Where("task_id = ?", taskID).Delete(&model.AIScriptTaskCaseRel{}).Error; err != nil {
			return err
		}
		// 创建新关系
		rels := make([]model.AIScriptTaskCaseRel, len(uniqueIDs))
		for i, cid := range uniqueIDs {
			rels[i] = model.AIScriptTaskCaseRel{
				TaskID:    taskID,
				CaseID:    cid,
				CreatedBy: userID,
			}
		}
		return tx.Create(&rels).Error
	})
}

// ExportScript 导出脚本文件信息
func (s *AIScriptService) ExportScript(ctx context.Context, scriptID uint) (string, string, error) {
	version, err := s.repo.GetScriptVersion(ctx, scriptID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", "", ErrNotFound("AI_SCRIPT_4004", "脚本版本不存在")
		}
		return "", "", ErrInternal("AI_SCRIPT_QUERY_FAILED", err)
	}
	fileName := fmt.Sprintf("task-%d-v%d.spec.ts", version.TaskID, version.VersionNo)
	return version.ScriptContent, fileName, nil
}


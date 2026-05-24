// Package service — 需求智生-生成任务业务逻辑层
//
// RequirementGenTaskService 管理生成任务的全生命周期：
//   - 创建任务（校验配额 + 文档状态 + Skill 有效性）
//   - 查询任务列表 / 详情
//   - 状态推进（CAS 保护）
//   - 回调处理（Executor 完成回调）
//   - 产物采纳 / 丢弃
//   - 任务关闭
package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"

	"testpilot/internal/model"
	"testpilot/internal/repository"
)

// ========== 配置常量 ==========

const (
	defaultProjectConcurrentLimit = 3   // 项目并发任务上限
	defaultGlobalConcurrentLimit  = 20  // 全局并发任务上限
	defaultMaxCases               = 30  // 默认最大生成条数
	maxAllowedCases               = 100 // 用户可配置最大值
)

// RequirementGenTaskService 需求智生-生成任务业务逻辑层
type RequirementGenTaskService struct {
	logger         *slog.Logger
	taskRepo       repository.RequirementGenTaskRepository
	resultRepo     repository.RequirementGenResultRepository
	docRepo        repository.RequirementDocRepository
	skillRepo      repository.AISkillRepository
	txMgr          *repository.TxManager
	executorURL    string
	executorAPIKey string
	httpClient     *http.Client
}

// NewRequirementGenTaskService 创建生成任务 Service
func NewRequirementGenTaskService(
	logger *slog.Logger,
	taskRepo repository.RequirementGenTaskRepository,
	resultRepo repository.RequirementGenResultRepository,
	docRepo repository.RequirementDocRepository,
	skillRepo repository.AISkillRepository,
	txMgr *repository.TxManager,
	executorURL string,
	executorAPIKey string,
) *RequirementGenTaskService {
	return &RequirementGenTaskService{
		logger:         logger.With("module", "requirement_gen_task"),
		taskRepo:       taskRepo,
		resultRepo:     resultRepo,
		docRepo:        docRepo,
		skillRepo:      skillRepo,
		txMgr:          txMgr,
		executorURL:    strings.TrimRight(executorURL, "/"),
		executorAPIKey: executorAPIKey,
		httpClient:     &http.Client{Timeout: 30 * time.Second},
	}
}

// ========== 输入结构体 ==========

// CreateGenTaskInput 创建生成任务的参数
type CreateGenTaskInput struct {
	ProjectID        uint
	RequirementDocID uint
	SkillID          uint
	AIModelConfigID  uint
	AIModelSnapshot  string
	TaskName         string
	TargetModuleID   uint
	DefaultLevel     string
	MaxCases         int
	ExtraPrompt      string
	CreatedBy        uint
}

// CallbackSuccessInput Executor 成功回调参数
type CallbackSuccessInput struct {
	TaskID           uint
	GeneratedCount   int
	PromptTokens     int
	CompletionTokens int
	DurationMs       int64
	Results          []CallbackResultItem
}

// CallbackResultItem 单条 AI 产物
type CallbackResultItem struct {
	SeqNo         int
	Title         string
	Level         string
	Precondition  string
	Steps         string // JSON 数组
	Postcondition string
	Remark        string
	TagsSuggested string
	AIConfidence  float64
	RawJSON       string
}

// CallbackFailInput Executor 失败回调参数
type CallbackFailInput struct {
	TaskID     uint
	FailReason string
}

// AdoptResultInput 采纳产物参数
type AdoptResultInput struct {
	ResultID      uint
	ProjectID     uint
	AdoptedBy     uint
	AdoptedCaseID uint   // 采纳后关联的测试用例 ID
	ModuleID      uint   // 目标目录
	TagIDs        []uint // 采纳时关联的标签
}

// ========== 业务方法 ==========

// Create 创建生成任务。
// 校验链路：文档存在且已解析 → Skill 有效 → 项目配额未超限 → 创建 PENDING 任务。
func (s *RequirementGenTaskService) Create(ctx context.Context, input CreateGenTaskInput) (*model.RequirementGenTask, error) {
	// 1. 校验需求文档
	doc, err := s.docRepo.FindByID(ctx, input.RequirementDocID)
	if err != nil {
		return nil, ErrNotFound(CodeReqDocNotFound, "需求文档不存在")
	}
	if doc.ProjectID != input.ProjectID {
		return nil, ErrNotFound(CodeReqDocNotFound, "需求文档不存在")
	}
	if doc.ParseStatus != model.DocParseStatusParsed {
		return nil, ErrBadRequest(CodeReqDocParsing, "需求文档尚未解析完成，请稍候")
	}

	// 2. 校验 Skill
	skill, err := s.skillRepo.FindByID(ctx, input.SkillID)
	if err != nil {
		return nil, ErrNotFound(CodeReqSkillNotFound, "Skill 不存在")
	}
	if !skill.IsActive {
		return nil, ErrBadRequest(CodeReqSkillNotFound, "Skill 已禁用")
	}
	// 系统 Skill 全局可用；项目 Skill 需要归属校验
	if skill.ProjectID != 0 && skill.ProjectID != input.ProjectID {
		return nil, ErrNotFound(CodeReqSkillNotFound, "Skill 不存在")
	}

	// 3. 校验项目并发配额
	activeCount, err := s.taskRepo.CountActiveByProject(ctx, input.ProjectID)
	if err != nil {
		s.logger.Error("查询项目活跃任务数失败", "error", err, "project_id", input.ProjectID)
		return nil, ErrInternal(CodeInternal, err)
	}
	if activeCount >= int64(defaultProjectConcurrentLimit) {
		return nil, ErrTooManyRequests(CodeReqGenProjectQuotaExceed,
			fmt.Sprintf("项目并发任务已达上限(%d)，请等待现有任务完成后再创建", defaultProjectConcurrentLimit))
	}

	// 4. 校验全局并发配额
	globalCount, err := s.taskRepo.CountActiveGlobal(ctx)
	if err != nil {
		s.logger.Error("查询全局活跃任务数失败", "error", err)
		return nil, ErrInternal(CodeInternal, err)
	}
	if globalCount >= int64(defaultGlobalConcurrentLimit) {
		return nil, ErrTooManyRequests(CodeReqGenGlobalQuotaExceed, "系统繁忙，请稍后重试")
	}

	// 5. 规范化参数
	maxCases := input.MaxCases
	if maxCases <= 0 {
		maxCases = defaultMaxCases
	}
	if maxCases > maxAllowedCases {
		maxCases = maxAllowedCases
	}
	defaultLevel := input.DefaultLevel
	if defaultLevel == "" {
		defaultLevel = "P2"
	}

	// 6. 创建任务
	task := &model.RequirementGenTask{
		ProjectID:        input.ProjectID,
		RequirementDocID: input.RequirementDocID,
		SkillID:          input.SkillID,
		AIModelConfigID:  input.AIModelConfigID,
		AIModelSnapshot:  input.AIModelSnapshot,
		TaskName:         input.TaskName,
		TargetModuleID:   input.TargetModuleID,
		DefaultLevel:     defaultLevel,
		MaxCases:         maxCases,
		ExtraPrompt:      input.ExtraPrompt,
		Status:           model.GenTaskStatusPending,
		CreatedBy:        input.CreatedBy,
	}

	if err := s.taskRepo.Create(ctx, task); err != nil {
		s.logger.Error("创建生成任务失败", "error", err, "project_id", input.ProjectID)
		return nil, ErrInternal(CodeInternal, err)
	}

	s.logger.Info("生成任务创建成功",
		"task_id", task.ID, "project_id", input.ProjectID,
		"doc_id", input.RequirementDocID, "skill_id", input.SkillID,
		"max_cases", maxCases,
	)

	// 异步派发到 Executor（非阻塞，失败不影响创建结果）
	go func() {
		if err := s.dispatchGenerate(task, doc, skill); err != nil {
			s.logger.Error("派发生成任务到 Executor 失败", "error", err, "task_id", task.ID)
		}
	}()

	return task, nil
}

// GetByID 查询任务详情
func (s *RequirementGenTaskService) GetByID(ctx context.Context, projectID, taskID uint) (*model.RequirementGenTask, error) {
	task, err := s.taskRepo.FindByID(ctx, taskID)
	if err != nil {
		return nil, ErrNotFound(CodeReqGenTaskNotFound, "生成任务不存在")
	}
	if task.ProjectID != projectID {
		return nil, ErrNotFound(CodeReqGenTaskNotFound, "生成任务不存在")
	}
	return task, nil
}

// ListPaged 分页查询生成任务列表
func (s *RequirementGenTaskService) ListPaged(ctx context.Context, projectID uint, f repository.RequirementGenTaskFilter) ([]model.RequirementGenTask, int64, error) {
	return s.taskRepo.ListPaged(ctx, projectID, f)
}

// ========== 状态推进 ==========

// MarkRunning 标记任务开始执行（PENDING → RUNNING）
func (s *RequirementGenTaskService) MarkRunning(ctx context.Context, taskID uint, executorNodeID, requestID string) error {
	task, err := s.taskRepo.FindByID(ctx, taskID)
	if err != nil {
		return ErrNotFound(CodeReqGenTaskNotFound, "生成任务不存在")
	}

	now := time.Now()
	affected, err := s.taskRepo.CASStatus(ctx, taskID,
		[]string{model.GenTaskStatusPending},
		task.LockVersion,
		model.GenTaskStatusRunning,
		map[string]interface{}{
			"started_at":        now,
			"last_heartbeat_at": now,
			"executor_node_id":  executorNodeID,
			"request_id":        requestID,
		},
	)
	if err != nil {
		return ErrInternal(CodeInternal, err)
	}
	if affected == 0 {
		return ErrConflict(CodeReqGenTaskStatusInvalid, "任务当前状态不允许开始执行")
	}

	s.logger.Info("任务开始执行", "task_id", taskID, "executor_node", executorNodeID)
	return nil
}

// Heartbeat 更新任务心跳
func (s *RequirementGenTaskService) Heartbeat(ctx context.Context, taskID uint) error {
	return s.taskRepo.UpdateHeartbeat(ctx, taskID)
}

// CallbackSuccess Executor 成功回调：写入产物 + 推进状态到 SUCCESS
func (s *RequirementGenTaskService) CallbackSuccess(ctx context.Context, input CallbackSuccessInput) error {
	task, err := s.taskRepo.FindByID(ctx, input.TaskID)
	if err != nil {
		return ErrNotFound(CodeReqGenTaskNotFound, "生成任务不存在")
	}
	// 幂等：已为终态则忽略
	if task.IsTerminal() {
		s.logger.Warn("回调忽略：任务已为终态", "task_id", input.TaskID, "status", task.Status)
		return nil
	}

	// 事务内：写入产物 + 更新任务状态
	err = s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		// 写入产物
		results := make([]model.RequirementGenResult, 0, len(input.Results))
		for _, item := range input.Results {
			results = append(results, model.RequirementGenResult{
				TaskID:        input.TaskID,
				ProjectID:     task.ProjectID,
				SeqNo:         item.SeqNo,
				Title:         item.Title,
				Level:         item.Level,
				Precondition:  item.Precondition,
				Steps:         item.Steps,
				Postcondition: item.Postcondition,
				Remark:        item.Remark,
				TagsSuggested: item.TagsSuggested,
				AIConfidence:  item.AIConfidence,
				RawJSON:       item.RawJSON,
			})
		}
		if err := s.resultRepo.BatchCreate(ctx, tx, results); err != nil {
			return err
		}

		// 推进状态
		now := time.Now()
		return s.taskRepo.UpdateFields(ctx, tx, input.TaskID, map[string]interface{}{
			"status":            model.GenTaskStatusSuccess,
			"generated_count":   input.GeneratedCount,
			"prompt_tokens":     input.PromptTokens,
			"completion_tokens": input.CompletionTokens,
			"duration_ms":       input.DurationMs,
			"finished_at":       now,
			"lock_version":      gorm.Expr("lock_version + 1"),
		})
	})

	if err != nil {
		s.logger.Error("成功回调处理失败", "error", err, "task_id", input.TaskID)
		return ErrInternal(CodeInternal, err)
	}

	s.logger.Info("任务执行成功",
		"task_id", input.TaskID, "generated_count", input.GeneratedCount,
		"prompt_tokens", input.PromptTokens, "completion_tokens", input.CompletionTokens,
	)
	return nil
}

// CallbackFail Executor 失败回调：推进状态到 FAILED
func (s *RequirementGenTaskService) CallbackFail(ctx context.Context, input CallbackFailInput) error {
	task, err := s.taskRepo.FindByID(ctx, input.TaskID)
	if err != nil {
		return ErrNotFound(CodeReqGenTaskNotFound, "生成任务不存在")
	}
	// 幂等：已为终态则忽略
	if task.IsTerminal() {
		s.logger.Warn("回调忽略：任务已为终态", "task_id", input.TaskID, "status", task.Status)
		return nil
	}

	now := time.Now()
	affected, err := s.taskRepo.CASStatus(ctx, input.TaskID,
		[]string{model.GenTaskStatusPending, model.GenTaskStatusRunning},
		task.LockVersion,
		model.GenTaskStatusFailed,
		map[string]interface{}{
			"fail_reason": input.FailReason,
			"finished_at": now,
		},
	)
	if err != nil {
		return ErrInternal(CodeInternal, err)
	}
	if affected == 0 {
		// 并发冲突或已为终态，忽略
		s.logger.Warn("失败回调 CAS 竞争，忽略", "task_id", input.TaskID)
		return nil
	}

	s.logger.Warn("任务执行失败", "task_id", input.TaskID, "reason", input.FailReason)
	return nil
}

// ========== 产物操作 ==========

// GetResultByID 获取单条产物详情（含项目归属校验）
func (s *RequirementGenTaskService) GetResultByID(ctx context.Context, projectID, resultID uint) (*model.RequirementGenResult, error) {
	result, err := s.resultRepo.FindByID(ctx, resultID)
	if err != nil {
		return nil, ErrNotFound(CodeReqResultNotFound, "产物不存在")
	}
	if result.ProjectID != projectID {
		return nil, ErrNotFound(CodeReqResultNotFound, "产物不存在")
	}
	return result, nil
}

// ListResults 查询任务下所有产物
func (s *RequirementGenTaskService) ListResults(ctx context.Context, projectID, taskID uint) ([]model.RequirementGenResult, error) {
	// 校验任务归属
	task, err := s.taskRepo.FindByID(ctx, taskID)
	if err != nil {
		return nil, ErrNotFound(CodeReqGenTaskNotFound, "生成任务不存在")
	}
	if task.ProjectID != projectID {
		return nil, ErrNotFound(CodeReqGenTaskNotFound, "生成任务不存在")
	}

	return s.resultRepo.ListByTaskID(ctx, taskID)
}

// AdoptResult 采纳单条产物：CAS 标记 + 递增任务已采纳数。
// 注意：实际用例入库逻辑由上层 Handler/编排Service 组合调用 TestCaseService.Create 完成，
// 此处仅更新产物状态和任务计数。
func (s *RequirementGenTaskService) AdoptResult(ctx context.Context, input AdoptResultInput) error {
	result, err := s.resultRepo.FindByID(ctx, input.ResultID)
	if err != nil {
		return ErrNotFound(CodeReqResultNotFound, "产物不存在")
	}
	if result.Adopted {
		return ErrConflict(CodeReqResultAlreadyAdopted, "产物已被采纳")
	}
	if result.Discarded {
		return ErrConflict(CodeReqResultDiscarded, "产物已被丢弃，不可采纳")
	}

	// 校验项目归属
	task, err := s.taskRepo.FindByID(ctx, result.TaskID)
	if err != nil {
		return ErrNotFound(CodeReqGenTaskNotFound, "关联任务不存在")
	}
	if task.ProjectID != input.ProjectID {
		return ErrNotFound(CodeReqResultNotFound, "产物不存在")
	}

	// 事务内：CAS 标记采纳 + 递增任务计数
	err = s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		affected, casErr := s.resultRepo.CASAdopt(ctx, tx, input.ResultID, result.LockVersion, input.AdoptedBy, input.AdoptedCaseID)
		if casErr != nil {
			return casErr
		}
		if affected == 0 {
			return ErrConflict(CodeReqResultVersionConflict, "产物已变更，请刷新后重试")
		}
		return s.taskRepo.IncrAdoptedCount(ctx, tx, result.TaskID, 1)
	})

	if err != nil {
		if bizErr, ok := err.(*BizError); ok {
			return bizErr
		}
		s.logger.Error("采纳产物失败", "error", err, "result_id", input.ResultID)
		return ErrInternal(CodeInternal, err)
	}

	s.logger.Info("产物采纳成功", "result_id", input.ResultID, "task_id", result.TaskID, "adopted_by", input.AdoptedBy)
	return nil
}

// DiscardResult 丢弃单条产物
func (s *RequirementGenTaskService) DiscardResult(ctx context.Context, projectID, resultID, userID uint) error {
	result, err := s.resultRepo.FindByID(ctx, resultID)
	if err != nil {
		return ErrNotFound(CodeReqResultNotFound, "产物不存在")
	}
	if result.Adopted {
		return ErrConflict(CodeReqResultAlreadyAdopted, "产物已被采纳，不可丢弃")
	}
	if result.Discarded {
		return nil // 幂等
	}

	// 校验项目归属
	task, err := s.taskRepo.FindByID(ctx, result.TaskID)
	if err != nil {
		return ErrNotFound(CodeReqGenTaskNotFound, "关联任务不存在")
	}
	if task.ProjectID != projectID {
		return ErrNotFound(CodeReqResultNotFound, "产物不存在")
	}

	// 事务内：CAS 丢弃 + 递增任务丢弃计数
	err = s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		affected, casErr := s.resultRepo.CASDiscard(ctx, tx, resultID, result.LockVersion, userID)
		if casErr != nil {
			return casErr
		}
		if affected == 0 {
			return ErrConflict(CodeReqResultVersionConflict, "产物已变更，请刷新后重试")
		}
		return s.taskRepo.IncrDiscardedCount(ctx, tx, result.TaskID, 1)
	})

	if err != nil {
		if bizErr, ok := err.(*BizError); ok {
			return bizErr
		}
		s.logger.Error("丢弃产物失败", "error", err, "result_id", resultID)
		return ErrInternal(CodeInternal, err)
	}

	s.logger.Info("产物已丢弃", "result_id", resultID, "task_id", result.TaskID, "user_id", userID)
	return nil
}

// CloseTask 关闭任务：批量丢弃所有 pending 产物，推进状态到 FULLY_CLOSED
func (s *RequirementGenTaskService) CloseTask(ctx context.Context, projectID, taskID, userID uint) error {
	task, err := s.taskRepo.FindByID(ctx, taskID)
	if err != nil {
		return ErrNotFound(CodeReqGenTaskNotFound, "生成任务不存在")
	}
	if task.ProjectID != projectID {
		return ErrNotFound(CodeReqGenTaskNotFound, "生成任务不存在")
	}

	// 仅 SUCCESS 或 PARTIAL_ADOPTED 状态可关闭
	if task.Status != model.GenTaskStatusSuccess && task.Status != model.GenTaskStatusPartialAdopted {
		return ErrBadRequest(CodeReqGenTaskStatusInvalid, "当前状态不允许关闭任务")
	}

	err = s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		// 批量丢弃 pending 产物
		discardedCount, batchErr := s.resultRepo.BatchDiscardByTaskID(ctx, tx, taskID, userID)
		if batchErr != nil {
			return batchErr
		}

		// 更新任务状态
		return s.taskRepo.UpdateFields(ctx, tx, taskID, map[string]interface{}{
			"status":          model.GenTaskStatusFullyClosed,
			"discarded_count": gorm.Expr("discarded_count + ?", discardedCount),
			"lock_version":    gorm.Expr("lock_version + 1"),
		})
	})

	if err != nil {
		s.logger.Error("关闭任务失败", "error", err, "task_id", taskID)
		return ErrInternal(CodeInternal, err)
	}

	s.logger.Info("任务已关闭", "task_id", taskID, "project_id", projectID)
	return nil
}

// ========== 智能生成 ==========

// SmartGenerateInput 智能生成的参数
type SmartGenerateInput struct {
	ProjectID        uint
	RequirementDocID uint
	AIModelConfigID  uint
	AIModelSnapshot  string
	TaskNamePrefix   string // 任务名前缀，自动拼接 Skill 名称
	TargetModuleID   uint
	DefaultLevel     string
	MaxCasesPerSkill int
	ExtraPrompt      string
	CreatedBy        uint
}

// SmartGenerateResult 智能生成的返回结果
type SmartGenerateResult struct {
	RecommendedSkills []SkillRecommendation      `json:"recommended_skills"`
	CreatedTasks      []*model.RequirementGenTask `json:"created_tasks"`
}

// SkillRecommendation Skill 路由推荐项
type SkillRecommendation struct {
	SkillID  uint   `json:"skill_id"`
	SkillKey string `json:"skill_key"`
	Reason   string `json:"reason"`
}

// skillRouterResponse Executor skill-router 响应
type skillRouterResponse struct {
	Status            string `json:"status"`
	Message           string `json:"message"`
	RecommendedSkills []struct {
		SkillID  int    `json:"skill_id"`
		SkillKey string `json:"skill_key"`
		Reason   string `json:"reason"`
	} `json:"recommended_skills"`
}

// SmartGenerate 智能生成：自动分析需求 → 匹配 Skill → 批量创建任务。
// 流程：校验文档 → 获取 Skill 列表 → 调用 Executor skill-router → 批量创建 + 派发。
func (s *RequirementGenTaskService) SmartGenerate(ctx context.Context, input SmartGenerateInput) (*SmartGenerateResult, error) {
	// 1. 校验需求文档
	doc, err := s.docRepo.FindByID(ctx, input.RequirementDocID)
	if err != nil {
		return nil, ErrNotFound(CodeReqDocNotFound, "需求文档不存在")
	}
	if doc.ProjectID != input.ProjectID {
		return nil, ErrNotFound(CodeReqDocNotFound, "需求文档不存在")
	}
	if doc.ParseStatus != model.DocParseStatusParsed {
		return nil, ErrBadRequest(CodeReqDocParsing, "需求文档尚未解析完成，请稍候")
	}

	requirementText := ""
	if doc.RawContent != nil {
		requirementText = *doc.RawContent
	}
	if requirementText == "" {
		return nil, ErrBadRequest(CodeReqDocParseFailed, "需求文档内容为空")
	}

	// 2. 获取项目可用的活跃 Skill 列表
	allSkills, err := s.skillRepo.ListProjectSkills(ctx, input.ProjectID)
	if err != nil {
		s.logger.Error("查询 Skill 列表失败", "error", err, "project_id", input.ProjectID)
		return nil, ErrInternal(CodeInternal, err)
	}

	// 过滤活跃 Skill 并构建候选列表
	type skillCandidate struct {
		SkillID     int    `json:"skill_id"`
		SkillKey    string `json:"skill_key"`
		Name        string `json:"name"`
		Scope       string `json:"scope"`
		Description string `json:"description"`
	}
	var candidates []skillCandidate
	skillMap := make(map[uint]*model.AISkill)
	for i := range allSkills {
		sk := &allSkills[i]
		if !sk.IsActive {
			continue
		}
		candidates = append(candidates, skillCandidate{
			SkillID:     int(sk.ID),
			SkillKey:    sk.SkillKey,
			Name:        sk.Name,
			Scope:       sk.Scope,
			Description: sk.Description,
		})
		skillMap[sk.ID] = sk
	}

	if len(candidates) == 0 {
		return nil, ErrBadRequest(CodeReqSkillNotFound, "没有可用的 Skill")
	}

	// 3. 调用 Executor skill-router
	recommended, err := s.callSkillRouter(requirementText, candidates)
	if err != nil {
		s.logger.Error("Skill 路由调用失败", "error", err, "project_id", input.ProjectID)
		return nil, ErrServiceUnavailable(CodeReqGenSkillRouterFailed, "AI 分析需求特征失败，请重试")
	}
	if len(recommended) == 0 {
		return nil, ErrBadRequest(CodeReqGenNoSkillRecommended, "AI 未匹配到适用的 Skill，请检查需求文档内容")
	}

	s.logger.Info("Skill 路由完成",
		"project_id", input.ProjectID, "doc_id", input.RequirementDocID,
		"recommended_count", len(recommended),
	)

	// 4. 规范化参数
	maxCases := input.MaxCasesPerSkill
	if maxCases <= 0 {
		maxCases = defaultMaxCases
	}
	if maxCases > maxAllowedCases {
		maxCases = maxAllowedCases
	}
	defaultLevel := input.DefaultLevel
	if defaultLevel == "" {
		defaultLevel = "P2"
	}

	// 5. 为每个推荐的 Skill 创建生成任务
	result := &SmartGenerateResult{
		RecommendedSkills: make([]SkillRecommendation, 0, len(recommended)),
		CreatedTasks:      make([]*model.RequirementGenTask, 0, len(recommended)),
	}

	for _, rec := range recommended {
		skill, ok := skillMap[uint(rec.SkillID)]
		if !ok {
			s.logger.Warn("推荐的 Skill 不在候选列表中", "skill_id", rec.SkillID)
			continue
		}

		result.RecommendedSkills = append(result.RecommendedSkills, SkillRecommendation{
			SkillID:  uint(rec.SkillID),
			SkillKey: rec.SkillKey,
			Reason:   rec.Reason,
		})

		// 组合任务名称
		taskName := input.TaskNamePrefix
		if taskName == "" {
			taskName = doc.Title
		}
		taskName = taskName + " - " + skill.Name

		task := &model.RequirementGenTask{
			ProjectID:        input.ProjectID,
			RequirementDocID: input.RequirementDocID,
			SkillID:          skill.ID,
			AIModelConfigID:  input.AIModelConfigID,
			AIModelSnapshot:  input.AIModelSnapshot,
			TaskName:         taskName,
			TargetModuleID:   input.TargetModuleID,
			DefaultLevel:     defaultLevel,
			MaxCases:         maxCases,
			ExtraPrompt:      input.ExtraPrompt,
			Status:           model.GenTaskStatusPending,
			CreatedBy:        input.CreatedBy,
		}

		if err := s.taskRepo.Create(ctx, task); err != nil {
			s.logger.Error("批量创建任务失败", "error", err, "skill_id", skill.ID)
			continue
		}

		result.CreatedTasks = append(result.CreatedTasks, task)

		// 异步派发到 Executor
		go func(t *model.RequirementGenTask, d *model.RequirementDoc, sk *model.AISkill) {
			if dispatchErr := s.dispatchGenerate(t, d, sk); dispatchErr != nil {
				s.logger.Error("派发生成任务到 Executor 失败", "error", dispatchErr, "task_id", t.ID)
			}
		}(task, doc, skill)
	}

	s.logger.Info("智能生成任务批量创建完成",
		"project_id", input.ProjectID, "doc_id", input.RequirementDocID,
		"recommended", len(result.RecommendedSkills), "created", len(result.CreatedTasks),
	)

	return result, nil
}

// callSkillRouter 调用 Executor 的 /requirement-gen/skill-router 接口
func (s *RequirementGenTaskService) callSkillRouter(requirementText string, candidates interface{}) ([]struct {
	SkillID  int    `json:"skill_id"`
	SkillKey string `json:"skill_key"`
	Reason   string `json:"reason"`
}, error) {
	if s.executorURL == "" {
		return nil, fmt.Errorf("executor URL 未配置")
	}

	payload := map[string]interface{}{
		"requirement_text": requirementText,
		"skills":           candidates,
		"max_skills":       6,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequest(http.MethodPost, s.executorURL+"/requirement-gen/skill-router", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create skill router request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", s.executorAPIKey)

	// 使用较长超时（LLM 调用可能需要 15-30 秒）
	routerClient := &http.Client{Timeout: 60 * time.Second}
	resp, err := routerClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call skill router: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("skill router returned status %d", resp.StatusCode)
	}

	var routerResp skillRouterResponse
	if err := json.NewDecoder(resp.Body).Decode(&routerResp); err != nil {
		return nil, fmt.Errorf("decode skill router response: %w", err)
	}

	if routerResp.Status != "ok" {
		return nil, fmt.Errorf("skill router error: %s", routerResp.Message)
	}

	return routerResp.RecommendedSkills, nil
}

// ========== 内部方法 ==========

// dispatchGenerate 将生成任务派发给 Executor 异步执行
func (s *RequirementGenTaskService) dispatchGenerate(task *model.RequirementGenTask, doc *model.RequirementDoc, skill *model.AISkill) error {
	if s.executorURL == "" {
		s.logger.Warn("executor URL 未配置，跳过生成任务派发", "task_id", task.ID)
		return nil
	}

	// CAS: pending → running
	_ = s.MarkRunning(context.Background(), task.ID, "executor-local", fmt.Sprintf("dispatch-%d", task.ID))

	// 获取文档内容
	requirementText := ""
	if doc.RawContent != nil {
		requirementText = *doc.RawContent
	}

	payload := map[string]interface{}{
		"task_id":          task.ID,
		"project_id":       task.ProjectID,
		"requirement_text": requirementText,
		"skill_name":       skill.Name,
		"prompt_template":  skill.PromptTemplate,
		"output_schema":    skill.OutputSchema,
		"max_cases":        task.MaxCases,
		"default_level":    task.DefaultLevel,
		"extra_prompt":     task.ExtraPrompt,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequest(http.MethodPost, s.executorURL+"/requirement-gen/generate", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create dispatch request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", s.executorAPIKey)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		s.logger.Error("派发生成任务失败", "error", err, "task_id", task.ID)
		return fmt.Errorf("dispatch generate: %w", err)
	}
	defer resp.Body.Close()

	s.logger.Info("生成任务已派发", "task_id", task.ID, "status", resp.StatusCode)
	return nil
}

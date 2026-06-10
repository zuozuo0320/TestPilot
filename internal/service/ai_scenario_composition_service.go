// ai_scenario_composition_service.go — 测试智编场景编排业务逻辑
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
	"regexp"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"testpilot/internal/model"
	"testpilot/internal/repository"
)

const lowConfidenceConfirmThreshold = 0.8

var dslReferencePattern = regexp.MustCompile(`\$\{([^}]+)\}`)

var allowedCompositionEnvKeys = map[string]struct{}{
	"ADMIN_USER":     {},
	"ADMIN_PASSWORD": {},
	"BASE_URL":       {},
}

var sensitiveDSLKeyParts = []string{"password", "token", "api_key", "apikey", "secret", "access_key", "authorization"}

// AIScenarioCompositionService 管理复杂测试场景编排、DSL、代码生成、验证和发布。
type AIScenarioCompositionService struct {
	logger            *slog.Logger
	scenarioRepo      *repository.AIScenarioCompositionRepo
	flowRepo          *repository.AIFlowAssetRepo
	assertionRepo     *repository.AIAssertionAssetRepo
	refRepo           *repository.AIAssetReferenceRepo
	aiScriptRepo      *repository.AIScriptRepo
	projectRepo       repository.ProjectRepository
	userRepo          repository.UserRepository
	txMgr             *repository.TxManager
	executorURL       string
	executorPublicURL string
	executorAPIKey    string
	httpClient        *http.Client
}

// NewAIScenarioCompositionService 创建场景编排服务。
func NewAIScenarioCompositionService(
	logger *slog.Logger,
	scenarioRepo *repository.AIScenarioCompositionRepo,
	flowRepo *repository.AIFlowAssetRepo,
	assertionRepo *repository.AIAssertionAssetRepo,
	refRepo *repository.AIAssetReferenceRepo,
	aiScriptRepo *repository.AIScriptRepo,
	projectRepo repository.ProjectRepository,
	userRepo repository.UserRepository,
	txMgr *repository.TxManager,
	executorURL string,
	executorPublicURL string,
	executorAPIKey string,
) *AIScenarioCompositionService {
	return &AIScenarioCompositionService{
		logger:            logger.With("module", "ai_scenario_composition"),
		scenarioRepo:      scenarioRepo,
		flowRepo:          flowRepo,
		assertionRepo:     assertionRepo,
		refRepo:           refRepo,
		aiScriptRepo:      aiScriptRepo,
		projectRepo:       projectRepo,
		userRepo:          userRepo,
		txMgr:             txMgr,
		executorURL:       strings.TrimRight(executorURL, "/"),
		executorPublicURL: strings.TrimRight(executorPublicURL, "/"),
		executorAPIKey:    executorAPIKey,
		httpClient:        &http.Client{Timeout: 300 * time.Second},
	}
}

// ScenarioCompositionListInput 表示场景编排列表查询输入。
type ScenarioCompositionListInput struct {
	ProjectID        uint
	Keyword          string
	Status           string
	ValidationStatus string
	Page             int
	PageSize         int
}

// ScenarioCompositionCreateInput 表示创建场景编排的输入。
type ScenarioCompositionCreateInput struct {
	ProjectID    uint
	ScenarioKey  string
	ScenarioName string
	Description  string
}

// ScenarioCompositionUpdateInput 表示更新场景编排基础信息和 DSL 的输入。
type ScenarioCompositionUpdateInput struct {
	ProjectID         uint
	ScenarioName      string
	Description       string
	DSL               json.RawMessage
	ExpectedRevision  int
	SkipRevisionCheck bool
}

// ScenarioStepSaveInput 表示新增或更新编排步骤的输入。
type ScenarioStepSaveInput struct {
	ProjectID        uint
	StepType         string
	StepName         string
	RefFlowID        *uint
	RefFlowVersionID *uint
	RefAssertionID   *uint
	ParamMapping     json.RawMessage
	OutputMapping    json.RawMessage
	AtomicAction     string
	CodeBlock        string
	ManualReviewed   bool
	AIConfidence     float64
	Enabled          bool
	EnabledSpecified bool
}

// GenerateCompositionCodeInput 表示生成编排代码的输入。
type GenerateCompositionCodeInput struct {
	ProjectID uint
	Force     bool
	Target    string
	// ConfirmPartial 显式确认引用 compile_health=PARTIAL 的存量资产，允许跳过不可编译步骤继续生成。
	ConfirmPartial bool
}

// ManualUpdateCompositionCodeInput 表示人工编辑生成代码的输入。
type ManualUpdateCompositionCodeInput struct {
	ProjectID        uint
	GeneratedCode    string
	ChangeSummary    string
	Locked           bool
	ExpectedRevision int
}

// LockCompositionCodeInput 表示锁定或解锁生成代码的输入。
type LockCompositionCodeInput struct {
	ProjectID     uint
	Locked        bool
	ChangeSummary string
}

// ScenarioVersionDiffInput 表示编排版本 Diff 的输入。
type ScenarioVersionDiffInput struct {
	ProjectID       uint
	BaseVersionID   uint
	TargetVersionID uint
}

// ScenarioVersionRollbackInput 表示回滚到指定编排版本的输入。
type ScenarioVersionRollbackInput struct {
	ProjectID          uint
	VersionID          uint
	OverrideLockedCode bool
	ChangeSummary      string
}

// GeneratedFileSummary 表示代码生成产物摘要。
type GeneratedFileSummary struct {
	Path      string `json:"path"`
	Operation string `json:"operation"`
}

// GenerateCompositionCodeResult 表示代码生成结果。
type GenerateCompositionCodeResult struct {
	CompositionID uint                   `json:"composition_id"`
	Status        string                 `json:"status"`
	Files         []GeneratedFileSummary `json:"files"`
	Warnings      []string               `json:"warnings"`
	GeneratedCode string                 `json:"generated_code"`
}

// ScenarioVersionDiffLine 表示版本差异中的单行变化。
type ScenarioVersionDiffLine struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// ScenarioVersionDiffStats 表示版本差异统计。
type ScenarioVersionDiffStats struct {
	BaseLineCount   int                       `json:"base_line_count"`
	TargetLineCount int                       `json:"target_line_count"`
	AddedLines      int                       `json:"added_lines"`
	RemovedLines    int                       `json:"removed_lines"`
	UnchangedLines  int                       `json:"unchanged_lines"`
	Preview         []ScenarioVersionDiffLine `json:"preview"`
	Truncated       bool                      `json:"truncated"`
}

// ScenarioVersionDiffResult 表示两个编排版本之间的 DSL 和代码差异。
type ScenarioVersionDiffResult struct {
	CompositionID uint                               `json:"composition_id"`
	BaseVersion   model.AIScenarioCompositionVersion `json:"base_version"`
	TargetVersion model.AIScenarioCompositionVersion `json:"target_version"`
	DSLChanged    bool                               `json:"dsl_changed"`
	CodeChanged   bool                               `json:"code_changed"`
	DSLStats      ScenarioVersionDiffStats           `json:"dsl_stats"`
	CodeStats     ScenarioVersionDiffStats           `json:"code_stats"`
	Summary       []string                           `json:"summary"`
}

// ValidateCompositionInput 表示触发编排验证的输入。
type ValidateCompositionInput struct {
	ProjectID      uint
	Environment    string
	Variables      json.RawMessage
	IdempotencyKey string
}

// PublishCompositionInput 表示发布编排的输入。
type PublishCompositionInput struct {
	ProjectID     uint
	ChangeSummary string
}

// AIPlanFromTaskInput 表示从录制任务生成 AI 编排建议的输入。
type AIPlanFromTaskInput struct {
	ProjectID       uint
	TaskID          uint
	SourceVersionID uint
	MaxSteps        int
}

// AIPlanStep 表示单个 AI 编排建议步骤。
type AIPlanStep struct {
	Type          string                 `json:"type"`
	FlowID        uint                   `json:"flow_id,omitempty"`
	FlowVersionID uint                   `json:"flow_version_id,omitempty"`
	FlowKey       string                 `json:"flow_key,omitempty"`
	AssertionID   uint                   `json:"assertion_id,omitempty"`
	AssertionKey  string                 `json:"assertion_key,omitempty"`
	Confidence    float64                `json:"confidence"`
	Reason        string                 `json:"reason"`
	Inputs        map[string]interface{} `json:"inputs,omitempty"`
}

// AIPlanResult 表示 AI 编排建议结果。
type AIPlanResult struct {
	PlanID     string       `json:"plan_id"`
	Confidence float64      `json:"confidence"`
	Summary    string       `json:"summary"`
	Steps      []AIPlanStep `json:"steps"`
	Warnings   []string     `json:"warnings"`
}

// List 分页查询场景编排列表。
func (s *AIScenarioCompositionService) List(ctx context.Context, input ScenarioCompositionListInput) ([]model.AIScenarioComposition, int64, error) {
	if input.ProjectID == 0 {
		return nil, 0, ErrBadRequest(CodeParamsError, "project_id 不能为空")
	}
	if input.Page < 1 {
		input.Page = 1
	}
	if input.PageSize < 1 || input.PageSize > 100 {
		input.PageSize = 20
	}
	compositions, total, err := s.scenarioRepo.List(ctx, repository.AIScenarioCompositionFilter{
		ProjectID:        input.ProjectID,
		Keyword:          strings.TrimSpace(input.Keyword),
		Status:           strings.TrimSpace(input.Status),
		ValidationStatus: strings.TrimSpace(input.ValidationStatus),
		Page:             input.Page,
		PageSize:         input.PageSize,
	})
	if err != nil {
		return nil, 0, ErrInternal(CodeInternal, err)
	}
	s.fillCompositionVirtualFields(ctx, compositions)
	return compositions, total, nil
}

// Get 获取场景编排详情，包含步骤列表。
func (s *AIScenarioCompositionService) Get(ctx context.Context, projectID, compositionID uint) (*model.AIScenarioComposition, error) {
	composition, err := s.scenarioRepo.GetByID(ctx, compositionID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound(CodeNotFound, "场景编排不存在")
		}
		return nil, ErrInternal(CodeInternal, err)
	}
	if composition.ProjectID != projectID {
		return nil, ErrForbidden(CodeForbidden, "场景编排不属于当前项目")
	}
	steps, err := s.scenarioRepo.ListSteps(ctx, composition.ID)
	if err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}
	s.fillStepNames(ctx, steps)
	composition.Steps = steps
	s.fillCompositionVirtualField(ctx, composition)
	return composition, nil
}

// Create 创建场景编排草稿。
func (s *AIScenarioCompositionService) Create(ctx context.Context, userID uint, input ScenarioCompositionCreateInput) (*model.AIScenarioComposition, error) {
	normalized, err := normalizeCompositionCreateInput(input)
	if err != nil {
		return nil, err
	}
	if _, err := s.scenarioRepo.GetByProjectAndKey(ctx, normalized.ProjectID, normalized.ScenarioKey); err == nil {
		return nil, ErrConflict(CodeConflict, "scenario_key 已存在，请换一个稳定标识")
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrInternal(CodeInternal, err)
	}

	composition := &model.AIScenarioComposition{
		ProjectID:              normalized.ProjectID,
		ScenarioKey:            normalized.ScenarioKey,
		ScenarioName:           normalized.ScenarioName,
		Description:            normalized.Description,
		Status:                 model.AIScenarioStatusDraft,
		CodeEditStatus:         model.AIScenarioCodeEditStatusAutoGenerated,
		LatestValidationStatus: model.AIValidationStatusNotValidated,
		CreatedBy:              userID,
		UpdatedBy:              userID,
		Revision:               1,
	}
	composition.DSLJSON = buildCompositionDSLRaw(composition, nil)
	if err := s.scenarioRepo.Create(ctx, nil, composition); err != nil {
		s.logger.Error("create scenario composition failed", "error", err, "project_id", normalized.ProjectID)
		return nil, ErrInternal(CodeInternal, err)
	}
	return composition, nil
}

// Update 更新场景编排基础信息或直接保存 DSL。
func (s *AIScenarioCompositionService) Update(ctx context.Context, userID, compositionID uint, input ScenarioCompositionUpdateInput) (*model.AIScenarioComposition, error) {
	composition, err := s.Get(ctx, input.ProjectID, compositionID)
	if err != nil {
		return nil, err
	}
	if composition.Status == model.AIScenarioStatusArchived {
		return nil, ErrConflict(CodeConflict, "已归档编排不可编辑")
	}
	if !input.SkipRevisionCheck && input.ExpectedRevision > 0 && input.ExpectedRevision != composition.Revision {
		return nil, ErrConflict(CodeConflict, "编排已被他人修改，请刷新后再编辑")
	}
	input.ScenarioName = strings.TrimSpace(input.ScenarioName)
	input.Description = strings.TrimSpace(input.Description)
	if input.ScenarioName == "" {
		return nil, ErrBadRequest(CodeParamsError, "场景名称不能为空")
	}
	if len(input.ScenarioName) > 128 {
		return nil, ErrBadRequest(CodeParamsError, "场景名称不能超过 128 个字符")
	}
	fields := map[string]interface{}{
		"scenario_name": input.ScenarioName,
		"description":   input.Description,
		"updated_by":    userID,
		"revision":      composition.Revision + 1,
	}
	if len(input.DSL) > 0 {
		if !json.Valid(input.DSL) {
			return nil, ErrBadRequest(CodeParamsError, "DSL 不是合法 JSON")
		}
		if failures := validateCompositionDSLSemantics(composition, model.RawJSON(input.DSL), false); len(failures) > 0 {
			return nil, ErrBadRequest(CodeParamsError, "DSL 校验失败: "+strings.Join(failures, "；"))
		}
		fields["dsl_json"] = string(input.DSL)
		fields["status"] = model.AIScenarioStatusDraft
		fields["latest_validation_status"] = model.AIValidationStatusNotValidated
	}
	if err := s.scenarioRepo.UpdateFields(ctx, nil, compositionID, fields); err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}
	return s.Get(ctx, input.ProjectID, compositionID)
}

// AddStep 新增编排步骤。
func (s *AIScenarioCompositionService) AddStep(ctx context.Context, userID, compositionID uint, input ScenarioStepSaveInput) (*model.AIScenarioStep, error) {
	composition, err := s.Get(ctx, input.ProjectID, compositionID)
	if err != nil {
		return nil, err
	}
	if composition.Status == model.AIScenarioStatusArchived {
		return nil, ErrConflict(CodeConflict, "已归档编排不可编辑")
	}
	normalized, err := s.normalizeStepInput(ctx, composition.ProjectID, input)
	if err != nil {
		return nil, err
	}
	count, err := s.scenarioRepo.CountSteps(ctx, compositionID)
	if err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}
	step := &model.AIScenarioStep{
		ScenarioID:        compositionID,
		StepNo:            int(count) + 1,
		StepType:          normalized.StepType,
		StepName:          normalized.StepName,
		RefFlowID:         normalized.RefFlowID,
		RefFlowVersionID:  normalized.RefFlowVersionID,
		RefAssertionID:    normalized.RefAssertionID,
		ParamMappingJSON:  model.RawJSON(normalized.ParamMapping),
		OutputMappingJSON: model.RawJSON(normalized.OutputMapping),
		AtomicAction:      normalized.AtomicAction,
		CodeBlock:         normalized.CodeBlock,
		ManualReviewed:    normalized.ManualReviewed,
		AIConfidence:      normalized.AIConfidence,
		Enabled:           normalized.Enabled,
	}
	err = s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		if err := s.scenarioRepo.CreateStep(ctx, tx, step); err != nil {
			return err
		}
		if step.DSLStepID == "" {
			step.DSLStepID = scenarioStepStableID(*step)
			if err := s.scenarioRepo.UpdateStepFields(ctx, tx, step.ID, map[string]interface{}{
				"dsl_step_id": step.DSLStepID,
			}); err != nil {
				return err
			}
		}
		return s.rebuildScenarioDerivedState(ctx, tx, composition, userID)
	})
	if err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}
	s.fillStepNames(ctx, []model.AIScenarioStep{*step})
	return step, nil
}

// UpdateStep 更新编排步骤。
func (s *AIScenarioCompositionService) UpdateStep(ctx context.Context, userID, compositionID, stepID uint, input ScenarioStepSaveInput) (*model.AIScenarioStep, error) {
	composition, err := s.Get(ctx, input.ProjectID, compositionID)
	if err != nil {
		return nil, err
	}
	if composition.Status == model.AIScenarioStatusArchived {
		return nil, ErrConflict(CodeConflict, "已归档编排不可编辑")
	}
	step, err := s.scenarioRepo.GetStepByID(ctx, stepID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound(CodeNotFound, "编排步骤不存在")
		}
		return nil, ErrInternal(CodeInternal, err)
	}
	if step.ScenarioID != compositionID {
		return nil, ErrForbidden(CodeForbidden, "步骤不属于当前编排")
	}
	normalized, err := s.normalizeStepInput(ctx, composition.ProjectID, input)
	if err != nil {
		return nil, err
	}
	err = s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		if err := s.scenarioRepo.UpdateStepFields(ctx, tx, stepID, map[string]interface{}{
			"step_type":           normalized.StepType,
			"step_name":           normalized.StepName,
			"ref_flow_id":         normalized.RefFlowID,
			"ref_flow_version_id": normalized.RefFlowVersionID,
			"ref_assertion_id":    normalized.RefAssertionID,
			"param_mapping_json":  string(normalized.ParamMapping),
			"output_mapping_json": string(normalized.OutputMapping),
			"atomic_action":       normalized.AtomicAction,
			"code_block":          normalized.CodeBlock,
			"manual_reviewed":     normalized.ManualReviewed,
			"ai_confidence":       normalized.AIConfidence,
			"enabled":             normalized.Enabled,
		}); err != nil {
			return err
		}
		return s.rebuildScenarioDerivedState(ctx, tx, composition, userID)
	})
	if err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}
	return s.scenarioRepo.GetStepByID(ctx, stepID)
}

// DeleteStep 删除编排步骤并重排序号。
func (s *AIScenarioCompositionService) DeleteStep(ctx context.Context, userID, projectID, compositionID, stepID uint) error {
	composition, err := s.Get(ctx, projectID, compositionID)
	if err != nil {
		return err
	}
	if composition.Status == model.AIScenarioStatusArchived {
		return ErrConflict(CodeConflict, "已归档编排不可编辑")
	}
	step, err := s.scenarioRepo.GetStepByID(ctx, stepID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound(CodeNotFound, "编排步骤不存在")
		}
		return ErrInternal(CodeInternal, err)
	}
	if step.ScenarioID != compositionID {
		return ErrForbidden(CodeForbidden, "步骤不属于当前编排")
	}
	return s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		if err := s.scenarioRepo.DeleteStep(ctx, tx, stepID); err != nil {
			return err
		}
		steps, err := s.scenarioRepo.ListStepsTx(ctx, tx, compositionID)
		if err != nil {
			return err
		}
		for i := range steps {
			if err := s.scenarioRepo.UpdateStepNo(ctx, tx, steps[i].ID, i+1); err != nil {
				return err
			}
		}
		return s.rebuildScenarioDerivedState(ctx, tx, composition, userID)
	})
}

// ReorderSteps 调整步骤顺序。
func (s *AIScenarioCompositionService) ReorderSteps(ctx context.Context, userID, projectID, compositionID uint, stepIDs []uint) error {
	composition, err := s.Get(ctx, projectID, compositionID)
	if err != nil {
		return err
	}
	if composition.Status == model.AIScenarioStatusArchived {
		return ErrConflict(CodeConflict, "已归档编排不可编辑")
	}
	existingSteps, err := s.scenarioRepo.ListSteps(ctx, compositionID)
	if err != nil {
		return ErrInternal(CodeInternal, err)
	}
	if len(stepIDs) != len(existingSteps) {
		return ErrBadRequest(CodeParamsError, "步骤数量不匹配，请刷新后再排序")
	}
	existing := make(map[uint]struct{}, len(existingSteps))
	for _, step := range existingSteps {
		existing[step.ID] = struct{}{}
	}
	for _, stepID := range stepIDs {
		if _, ok := existing[stepID]; !ok {
			return ErrBadRequest(CodeParamsError, "存在不属于当前编排的步骤")
		}
	}
	return s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		for i, stepID := range stepIDs {
			if err := s.scenarioRepo.UpdateStepNo(ctx, tx, stepID, i+1); err != nil {
				return err
			}
		}
		return s.rebuildScenarioDerivedState(ctx, tx, composition, userID)
	})
}

// GenerateCode 根据 DSL 和步骤生成 Playwright 代码。
func (s *AIScenarioCompositionService) GenerateCode(ctx context.Context, userID, compositionID uint, input GenerateCompositionCodeInput) (*GenerateCompositionCodeResult, error) {
	composition, err := s.Get(ctx, input.ProjectID, compositionID)
	if err != nil {
		return nil, err
	}
	if composition.Status == model.AIScenarioStatusArchived {
		return nil, ErrConflict(CodeConflict, "已归档编排不可生成代码")
	}
	if composition.CodeEditStatus == model.AIScenarioCodeEditStatusLocked {
		return nil, ErrConflict(CodeConflict, "生成代码已被人工锁定，请先显式解除锁定后再重新生成")
	}
	if failures := s.validateCompositionStructure(ctx, composition); len(failures) > 0 {
		return nil, ErrConflict(CodeConflict, "编排校验未通过: "+strings.Join(failures, "；"))
	}
	code, warnings, confirmedPartialFlows, err := s.compilePlaywrightSpec(ctx, composition, input.ConfirmPartial)
	if err != nil {
		return nil, err
	}
	if len(confirmedPartialFlows) > 0 {
		s.recordConfirmPartialAudit(ctx, userID, composition, confirmedPartialFlows)
	}
	if err := s.scenarioRepo.UpdateFields(ctx, nil, composition.ID, map[string]interface{}{
		"generated_code":           code,
		"code_edit_status":         model.AIScenarioCodeEditStatusAutoGenerated,
		"code_change_summary":      "自动生成 Playwright 代码",
		"manual_patched_by":        nil,
		"manual_patched_at":        nil,
		"code_locked_by":           nil,
		"code_locked_at":           nil,
		"status":                   model.AIScenarioStatusGenerated,
		"latest_validation_status": model.AIValidationStatusNotValidated,
		"updated_by":               userID,
		"revision":                 composition.Revision + 1,
	}); err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}
	return &GenerateCompositionCodeResult{
		CompositionID: composition.ID,
		Status:        model.AIScenarioStatusGenerated,
		Files: []GeneratedFileSummary{{
			Path:      fmt.Sprintf("spec/%s.spec.ts", composition.ScenarioKey),
			Operation: "UPSERT",
		}},
		Warnings:      warnings,
		GeneratedCode: code,
	}, nil
}

// recordConfirmPartialAudit 记录显式确认引用 PARTIAL 固定场景的操作审计日志，便于事后回溯。
func (s *AIScenarioCompositionService) recordConfirmPartialAudit(ctx context.Context, userID uint, composition *model.AIScenarioComposition, flowKeys []string) {
	operatorName := ""
	if user, err := s.userRepo.FindByID(ctx, userID); err == nil && user != nil {
		operatorName = user.Name
	}
	desc := fmt.Sprintf("生成编排 %s(ID=%d) 代码时显式确认引用 PARTIAL 固定场景：%s", composition.ScenarioKey, composition.ID, strings.Join(flowKeys, ", "))
	if runes := []rune(desc); len(runes) > 500 {
		desc = string(runes[:500])
	}
	log := &model.AIScriptOperationLog{
		OperationType: model.AIScriptOperationConfirmPartial,
		OperatorID:    userID,
		OperatorName:  operatorName,
		OperationDesc: desc,
	}
	if err := s.aiScriptRepo.CreateOperationLog(ctx, log); err != nil {
		s.logger.Error("record confirm_partial audit failed", "error", err, "composition_id", composition.ID)
	}
}

// RefreshFlowRefsInput 升级编排内固定场景引用版本的输入。
type RefreshFlowRefsInput struct {
	ProjectID uint
	FlowIDs   []uint
}

// RefreshFlowRefs 将编排中 FLOW_CALL 步骤锁定的固定场景版本升级到最新发布版本（仅在用户显式确认后调用，不做自动升级）。
func (s *AIScenarioCompositionService) RefreshFlowRefs(ctx context.Context, userID, compositionID uint, input RefreshFlowRefsInput) (*model.AIScenarioComposition, error) {
	composition, err := s.Get(ctx, input.ProjectID, compositionID)
	if err != nil {
		return nil, err
	}
	if composition.Status == model.AIScenarioStatusArchived {
		return nil, ErrConflict(CodeConflict, "已归档编排不可升级引用版本")
	}
	targetFilter := map[uint]struct{}{}
	for _, flowID := range input.FlowIDs {
		targetFilter[flowID] = struct{}{}
	}
	upgrades := map[uint]uint{}
	for _, step := range composition.Steps {
		if step.StepType != model.AIScenarioStepTypeFlowCall || step.RefFlowID == nil || step.RefFlowVersionID == nil {
			continue
		}
		if len(targetFilter) > 0 {
			if _, ok := targetFilter[*step.RefFlowID]; !ok {
				continue
			}
		}
		latest, latestErr := s.flowRepo.GetLatestPublishedVersion(ctx, *step.RefFlowID)
		if latestErr != nil || latest.ID == *step.RefFlowVersionID {
			continue
		}
		upgrades[step.ID] = latest.ID
	}
	if len(upgrades) == 0 {
		return composition, nil
	}
	err = s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		for stepID, versionID := range upgrades {
			if err := s.scenarioRepo.UpdateStepFields(ctx, tx, stepID, map[string]interface{}{
				"ref_flow_version_id": versionID,
			}); err != nil {
				return err
			}
		}
		return s.rebuildScenarioDerivedState(ctx, tx, composition, userID)
	})
	if err != nil {
		s.logger.Error("refresh flow refs failed", "error", err, "composition_id", compositionID)
		return nil, ErrInternal(CodeInternal, err)
	}
	return s.Get(ctx, input.ProjectID, compositionID)
}

// ManualUpdateCode 保存人工编辑后的生成代码，并按需锁定自动覆盖。
func (s *AIScenarioCompositionService) ManualUpdateCode(ctx context.Context, userID, compositionID uint, input ManualUpdateCompositionCodeInput) (*model.AIScenarioComposition, error) {
	composition, err := s.Get(ctx, input.ProjectID, compositionID)
	if err != nil {
		return nil, err
	}
	if composition.Status == model.AIScenarioStatusArchived {
		return nil, ErrConflict(CodeConflict, "已归档编排不可编辑生成代码")
	}
	if input.ExpectedRevision > 0 && input.ExpectedRevision != composition.Revision {
		return nil, ErrConflict(CodeConflict, "编排已被他人修改，请刷新后再编辑")
	}
	code := strings.TrimSpace(input.GeneratedCode)
	if code == "" {
		return nil, ErrBadRequest(CodeParamsError, "生成代码不能为空")
	}
	changeSummary := strings.TrimSpace(input.ChangeSummary)
	if changeSummary == "" {
		changeSummary = "人工编辑生成代码"
	}
	now := time.Now()
	codeStatus := model.AIScenarioCodeEditStatusManualPatched
	var lockedBy interface{}
	var lockedAt interface{}
	if input.Locked {
		codeStatus = model.AIScenarioCodeEditStatusLocked
		lockedBy = userID
		lockedAt = now
	} else {
		lockedBy = nil
		lockedAt = nil
	}
	if err := s.scenarioRepo.UpdateFields(ctx, nil, composition.ID, map[string]interface{}{
		"generated_code":           code,
		"code_edit_status":         codeStatus,
		"code_change_summary":      changeSummary,
		"manual_patched_by":        userID,
		"manual_patched_at":        now,
		"code_locked_by":           lockedBy,
		"code_locked_at":           lockedAt,
		"status":                   model.AIScenarioStatusGenerated,
		"latest_validation_status": model.AIValidationStatusNotValidated,
		"latest_validation_id":     nil,
		"updated_by":               userID,
		"revision":                 composition.Revision + 1,
	}); err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}
	return s.Get(ctx, input.ProjectID, composition.ID)
}

// SetCodeLock 显式锁定或解除生成代码锁。
func (s *AIScenarioCompositionService) SetCodeLock(ctx context.Context, userID, compositionID uint, input LockCompositionCodeInput) (*model.AIScenarioComposition, error) {
	composition, err := s.Get(ctx, input.ProjectID, compositionID)
	if err != nil {
		return nil, err
	}
	if composition.Status == model.AIScenarioStatusArchived {
		return nil, ErrConflict(CodeConflict, "已归档编排不可变更代码锁")
	}
	if strings.TrimSpace(composition.GeneratedCode) == "" {
		return nil, ErrConflict(CodeConflict, "暂无生成代码，不能变更代码锁")
	}
	changeSummary := strings.TrimSpace(input.ChangeSummary)
	if changeSummary == "" {
		if input.Locked {
			changeSummary = "锁定人工修改代码"
		} else {
			changeSummary = "解除生成代码锁定"
		}
	}
	fields := map[string]interface{}{
		"code_change_summary": changeSummary,
		"updated_by":          userID,
		"revision":            composition.Revision + 1,
	}
	if input.Locked {
		now := time.Now()
		fields["code_edit_status"] = model.AIScenarioCodeEditStatusLocked
		fields["code_locked_by"] = userID
		fields["code_locked_at"] = now
		if composition.ManualPatchedBy == nil {
			fields["manual_patched_by"] = userID
			fields["manual_patched_at"] = now
		}
	} else {
		fields["code_edit_status"] = model.AIScenarioCodeEditStatusManualPatched
		fields["code_locked_by"] = nil
		fields["code_locked_at"] = nil
	}
	if err := s.scenarioRepo.UpdateFields(ctx, nil, composition.ID, fields); err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}
	return s.Get(ctx, input.ProjectID, composition.ID)
}

// Validate 同步执行编排结构验证，并在执行服务可用时触发 Playwright 回放。
func (s *AIScenarioCompositionService) Validate(ctx context.Context, userID, compositionID uint, input ValidateCompositionInput) (*model.AICompositionValidation, error) {
	composition, err := s.Get(ctx, input.ProjectID, compositionID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(composition.GeneratedCode) == "" {
		return nil, ErrConflict(CodeConflict, "请先生成 Playwright 代码后再验证")
	}
	idempotencyKey := strings.TrimSpace(input.IdempotencyKey)
	if len(idempotencyKey) > 128 {
		return nil, ErrBadRequest(CodeParamsError, "幂等键不能超过 128 个字符")
	}
	if idempotencyKey != "" {
		existing, idemErr := s.scenarioRepo.GetValidationByIdempotencyKey(ctx, input.ProjectID, compositionID, idempotencyKey)
		if idemErr == nil {
			s.fillValidationAssertionResults(ctx, existing)
			return existing, nil
		}
		if !errors.Is(idemErr, gorm.ErrRecordNotFound) {
			return nil, ErrInternal(CodeInternal, idemErr)
		}
	}

	startedAt := time.Now()
	failures := s.validateCompositionStructure(ctx, composition)

	var executorResult *ExecutorValidateResponse
	if len(failures) == 0 && s.executorURL != "" {
		log := s.logger.With(
			"composition_id", composition.ID,
			"project_id", composition.ProjectID,
			"action", "composition_validate",
		)
		if !s.acquireCompositionWorkspaceLock(ctx, composition.ProjectID, composition.ID, "validate_run") {
			failures = append(failures, "项目执行工作区正在被其他验证占用，请稍后重试")
		} else {
			executorResult, err = s.callCompositionExecutorValidate(ctx, composition, input, log)
			s.releaseCompositionWorkspaceLock(context.Background(), composition.ProjectID)
			if err != nil {
				log.Error("composition executor validate failed", "error", err)
				failures = append(failures, "调用执行服务失败: "+err.Error())
			}
		}
	}

	finishedAt := time.Now()
	status := model.AICompositionValidationStatusPassed
	durationMs := finishedAt.Sub(startedAt).Milliseconds()
	logsJSON := buildCompositionValidationLogs(startedAt, finishedAt, len(composition.Steps), failures)
	if executorResult != nil {
		durationMs = executorResult.DurationMs
		if durationMs <= 0 {
			durationMs = finishedAt.Sub(startedAt).Milliseconds()
		}
		logsJSON = normalizeExecutorLogs(executorResult.Logs, logsJSON)
		if !executorResult.Success {
			status = model.AICompositionValidationStatusFailed
			if executorResult.FailReason != "" {
				failures = append(failures, executorResult.FailReason)
			} else if executorResult.ErrorMessage != "" {
				failures = append(failures, executorResult.ErrorMessage)
			} else {
				failures = append(failures, "执行服务返回验证失败")
			}
		}
	}
	if len(failures) > 0 {
		status = model.AICompositionValidationStatusFailed
	}

	assertionResults := buildCompositionAssertionResults(composition, status, failures, executorResult)
	validation := &model.AICompositionValidation{
		CompositionID:  composition.ID,
		ProjectID:      composition.ProjectID,
		IdempotencyKey: optionalStringPtr(idempotencyKey),
		Status:         status,
		ExecutorJobID:  buildCompositionExecutorJobID(composition.ID, executorResult),
		WorkspaceID:    fmt.Sprintf("project_%d", composition.ProjectID),
		LogsJSON:       logsJSON,
		StartedAt:      &startedAt,
		FinishedAt:     &finishedAt,
		DurationMs:     durationMs,
		CreatedBy:      userID,
	}
	err = s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		if err := s.scenarioRepo.CreateValidation(ctx, tx, validation); err != nil {
			return err
		}
		for i := range assertionResults {
			assertionResults[i].ValidationID = validation.ID
		}
		if err := s.scenarioRepo.CreateAssertionResults(ctx, tx, assertionResults); err != nil {
			return err
		}
		return s.scenarioRepo.UpdateFields(ctx, tx, composition.ID, map[string]interface{}{
			"latest_validation_id":     validation.ID,
			"latest_validation_status": status,
			"status":                   scenarioStatusAfterValidation(status),
			"updated_by":               userID,
		})
	})
	if err != nil {
		if idempotencyKey != "" {
			existing, idemErr := s.scenarioRepo.GetValidationByIdempotencyKey(ctx, input.ProjectID, compositionID, idempotencyKey)
			if idemErr == nil {
				s.fillValidationAssertionResults(ctx, existing)
				return existing, nil
			}
		}
		return nil, ErrInternal(CodeInternal, err)
	}
	validation.AssertionResults = assertionResults
	return validation, nil
}

func (s *AIScenarioCompositionService) callCompositionExecutorValidate(
	ctx context.Context,
	composition *model.AIScenarioComposition,
	input ValidateCompositionInput,
	log *slog.Logger,
) (*ExecutorValidateResponse, error) {
	versionID := composition.ID
	if composition.CurrentVersionID != nil && *composition.CurrentVersionID > 0 {
		versionID = *composition.CurrentVersionID
	}
	reqBody := ExecutorValidateRequest{
		TaskID:          composition.ID,
		ScriptVersionID: versionID,
		ScriptContent:   composition.GeneratedCode,
		StartURL:        s.resolveCompositionStartURL(ctx, composition.ProjectID, input),
	}
	rawResult, err := s.callCompositionExecutorHTTP(ctx, "/execute/validate", reqBody, log)
	if err != nil {
		return nil, err
	}
	var result ExecutorValidateResponse
	if err := json.Unmarshal(rawResult, &result); err != nil {
		return nil, fmt.Errorf("解析执行服务响应失败: %w", err)
	}
	return &result, nil
}

func (s *AIScenarioCompositionService) callCompositionExecutorHTTP(ctx context.Context, path string, reqBody interface{}, log *slog.Logger) (json.RawMessage, error) {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("序列化执行请求失败: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.executorURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("创建执行请求失败: %w", err)
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
	limitedReader := io.LimitReader(resp.Body, executorBodyLimit)
	respBody, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("读取执行服务响应失败: %w", err)
	}
	log.Info("composition executor validate finished", "response_bytes", len(respBody))
	return respBody, nil
}

func (s *AIScenarioCompositionService) acquireCompositionWorkspaceLock(ctx context.Context, projectID, compositionID uint, lockType string) bool {
	lock := &model.AIScriptWorkspaceLock{
		ProjectID:      projectID,
		LockKey:        fmt.Sprintf("project_%d", projectID),
		LockType:       lockType,
		OwnerRequestID: fmt.Sprintf("composition_%d", compositionID),
		HeartbeatAt:    time.Now(),
		ExpiresAt:      time.Now().Add(10 * time.Minute),
		Status:         "active",
	}
	acquired, err := s.aiScriptRepo.AcquireWorkspaceLockAtomic(ctx, lock)
	if err != nil {
		s.logger.Error("acquire composition workspace lock failed", "project_id", projectID, "composition_id", compositionID, "error", err)
		return false
	}
	if !acquired {
		s.logger.Warn("composition workspace lock conflict", "project_id", projectID, "composition_id", compositionID, "lock_type", lockType)
	}
	return acquired
}

func (s *AIScenarioCompositionService) releaseCompositionWorkspaceLock(ctx context.Context, projectID uint) {
	if err := s.aiScriptRepo.ReleaseWorkspaceLock(ctx, projectID); err != nil {
		s.logger.Error("release composition workspace lock failed", "project_id", projectID, "error", err)
	}
}

func (s *AIScenarioCompositionService) resolveCompositionStartURL(ctx context.Context, projectID uint, input ValidateCompositionInput) string {
	if rawURL := extractStringFromRawJSON(input.Variables, "start_url", "startUrl", "BASE_URL", "baseUrl"); rawURL != "" {
		return rawURL
	}
	environment := strings.TrimSpace(input.Environment)
	if project, err := s.projectRepo.FindByID(ctx, projectID); err == nil && project != nil {
		settings := project.ParseSettings()
		for _, env := range settings.TestEnvironments {
			if environment != "" && (env.ID == environment || env.Name == environment) && strings.TrimSpace(env.BaseURL) != "" {
				return strings.TrimSpace(env.BaseURL)
			}
		}
		for _, env := range settings.TestEnvironments {
			if env.IsDefault && strings.TrimSpace(env.BaseURL) != "" {
				return strings.TrimSpace(env.BaseURL)
			}
		}
		if len(settings.TestEnvironments) > 0 && strings.TrimSpace(settings.TestEnvironments[0].BaseURL) != "" {
			return strings.TrimSpace(settings.TestEnvironments[0].BaseURL)
		}
	}
	return "http://localhost:5173"
}

func extractStringFromRawJSON(data json.RawMessage, keys ...string) string {
	if len(data) == 0 || !json.Valid(data) {
		return ""
	}
	var value interface{}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return ""
	}
	for _, key := range keys {
		if found, ok := findStringValueByKey(value, key); ok {
			return strings.TrimSpace(found)
		}
	}
	return ""
}

func findStringValueByKey(value interface{}, key string) (string, bool) {
	switch typed := value.(type) {
	case map[string]interface{}:
		for currentKey, currentValue := range typed {
			if strings.EqualFold(currentKey, key) {
				return stringifyJSONScalar(currentValue)
			}
		}
		for _, currentValue := range typed {
			if found, ok := findStringValueByKey(currentValue, key); ok {
				return found, true
			}
		}
	case []interface{}:
		for _, item := range typed {
			if found, ok := findStringValueByKey(item, key); ok {
				return found, true
			}
		}
	}
	return "", false
}

func stringifyJSONScalar(value interface{}) (string, bool) {
	switch typed := value.(type) {
	case string:
		return typed, typed != ""
	case json.Number:
		return typed.String(), true
	case bool:
		if typed {
			return "true", true
		}
		return "false", true
	default:
		return "", false
	}
}

func buildCompositionValidationLogs(startedAt, finishedAt time.Time, stepCount int, failures []string) model.RawJSON {
	level := "INFO"
	message := "编排验证通过"
	if len(failures) > 0 {
		level = "ERROR"
		message = "编排验证失败"
	}
	entries := []map[string]interface{}{
		{
			"level":      "INFO",
			"message":    "编排验证开始",
			"step_count": stepCount,
			"timestamp":  startedAt.Format(time.RFC3339),
		},
		{
			"duration_ms": finishedAt.Sub(startedAt).Milliseconds(),
			"failures":    failures,
			"level":       level,
			"message":     message,
			"timestamp":   finishedAt.Format(time.RFC3339),
		},
	}
	return mustRawJSON(entries)
}

func normalizeExecutorLogs(logs json.RawMessage, fallback model.RawJSON) model.RawJSON {
	if len(logs) == 0 || !json.Valid(logs) || string(logs) == "null" {
		return fallback
	}
	var value interface{}
	decoder := json.NewDecoder(bytes.NewReader(logs))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return fallback
	}
	switch typed := value.(type) {
	case []interface{}:
		return model.RawJSON(logs)
	case map[string]interface{}:
		return mustRawJSON([]interface{}{typed})
	case string:
		if strings.TrimSpace(typed) == "" {
			return fallback
		}
		return mustRawJSON([]map[string]interface{}{
			{"level": "INFO", "message": typed, "timestamp": time.Now().Format(time.RFC3339)},
		})
	default:
		return fallback
	}
}

func buildCompositionAssertionResults(
	composition *model.AIScenarioComposition,
	status string,
	failures []string,
	executorResult *ExecutorValidateResponse,
) []model.AICompositionAssertionResult {
	results := make([]model.AICompositionAssertionResult, 0)
	assertionSummaries := parseCompositionAssertionSummaries(executorResult)
	assertionIndex := 0
	for _, step := range composition.Steps {
		if !step.Enabled || step.StepType != model.AIScenarioStepTypeAssertion || step.RefAssertionID == nil {
			continue
		}
		resultStatus := model.AICompositionValidationStatusPassed
		failureMessage := ""
		actual := map[string]interface{}{"status": resultStatus}
		if status != model.AICompositionValidationStatusPassed {
			resultStatus = model.AICompositionValidationStatusFailed
			failureMessage = truncateString(strings.Join(failures, "；"), 1000)
			actual["status"] = resultStatus
			actual["failures"] = failures
		}
		if assertionIndex < len(assertionSummaries) {
			summary := assertionSummaries[assertionIndex]
			resultStatus = summary.Status
			failureMessage = truncateString(summary.FailureMessage, 1000)
			actual = summary.Actual
		}
		results = append(results, model.AICompositionAssertionResult{
			StepID:         scenarioStepStableID(step),
			AssertionID:    *step.RefAssertionID,
			Status:         resultStatus,
			ExpectedJSON:   mustRawJSON(map[string]interface{}{"step_name": step.StepName, "params": rawJSONToObject(step.ParamMappingJSON)}),
			ActualJSON:     mustRawJSON(actual),
			FailureMessage: failureMessage,
			EvidenceJSON:   buildCompositionAssertionEvidence(executorResult),
			DurationMs:     0,
		})
		assertionIndex++
	}
	return results
}

type compositionAssertionSummary struct {
	Status         string
	Actual         map[string]interface{}
	FailureMessage string
}

func parseCompositionAssertionSummaries(executorResult *ExecutorValidateResponse) []compositionAssertionSummary {
	if executorResult == nil || len(executorResult.AssertionSummary) == 0 || !json.Valid(executorResult.AssertionSummary) {
		return nil
	}
	var rawItems []map[string]interface{}
	if err := json.Unmarshal(executorResult.AssertionSummary, &rawItems); err != nil {
		return nil
	}
	summaries := make([]compositionAssertionSummary, 0, len(rawItems))
	for _, item := range rawItems {
		status := model.AICompositionValidationStatusPassed
		if skipped, _ := item["skipped"].(bool); skipped {
			status = model.AICompositionValidationStatusCanceled
		} else if passed, ok := item["passed"].(bool); ok && !passed {
			status = model.AICompositionValidationStatusFailed
		} else if rawStatus, _ := item["status"].(string); strings.EqualFold(rawStatus, "FAILED") {
			status = model.AICompositionValidationStatusFailed
		}
		failureMessage, _ := item["failure_message"].(string)
		if failureMessage == "" {
			failureMessage, _ = item["message"].(string)
		}
		summaries = append(summaries, compositionAssertionSummary{
			Status:         status,
			Actual:         item,
			FailureMessage: failureMessage,
		})
	}
	return summaries
}

func buildCompositionAssertionEvidence(executorResult *ExecutorValidateResponse) model.RawJSON {
	if executorResult == nil || len(executorResult.Screenshots) == 0 {
		return mustRawJSON(map[string]interface{}{})
	}
	return mustRawJSON(map[string]interface{}{"screenshots": executorResult.Screenshots})
}

func buildCompositionExecutorJobID(compositionID uint, executorResult *ExecutorValidateResponse) string {
	if executorResult == nil {
		return fmt.Sprintf("composition_%d_local", compositionID)
	}
	if jobID := extractStringFromRawJSON(executorResult.Logs, "executor_job_id", "job_id", "jobId"); jobID != "" {
		return jobID
	}
	return fmt.Sprintf("composition_%d_executor", compositionID)
}

func truncateString(value string, maxLength int) string {
	if len(value) <= maxLength {
		return value
	}
	return value[:maxLength]
}

// Publish 发布编排并保存不可变版本快照。
func (s *AIScenarioCompositionService) Publish(ctx context.Context, userID, compositionID uint, input PublishCompositionInput) (*model.AIScenarioCompositionVersion, error) {
	composition, err := s.Get(ctx, input.ProjectID, compositionID)
	if err != nil {
		return nil, err
	}
	if composition.Status == model.AIScenarioStatusArchived {
		return nil, ErrConflict(CodeConflict, "已归档编排不可发布")
	}
	if strings.TrimSpace(composition.GeneratedCode) == "" {
		return nil, ErrConflict(CodeConflict, "请先生成代码后再发布")
	}
	if failures := s.validateCompositionStructure(ctx, composition); len(failures) > 0 {
		return nil, ErrConflict(CodeConflict, "发布前编排校验未通过: "+strings.Join(failures, "；"))
	}
	if composition.LatestValidationStatus != model.AICompositionValidationStatusPassed &&
		composition.LatestValidationStatus != model.AIValidationStatusPassed {
		return nil, ErrConflict(CodeConflict, "最近一次验证未通过，不能发布")
	}
	for _, step := range composition.Steps {
		if step.StepType == model.AIScenarioStepTypeAIGenerated {
			return nil, ErrConflict(CodeConflict, "发布前请先采纳或移除 AI 临时步骤")
		}
		if step.StepType == model.AIScenarioStepTypeCodeBlock && !step.ManualReviewed {
			return nil, ErrConflict(CodeConflict, "自定义代码块发布前必须完成审核")
		}
	}
	changeSummary := strings.TrimSpace(input.ChangeSummary)
	if changeSummary == "" {
		changeSummary = "发布场景编排"
	}
	maxNo, err := s.scenarioRepo.MaxVersionNo(ctx, composition.ID)
	if err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}
	version := &model.AIScenarioCompositionVersion{
		CompositionID: composition.ID,
		VersionNo:     maxNo + 1,
		VersionStatus: model.AIScenarioStatusPublished,
		DSLJSON:       composition.DSLJSON,
		GeneratedCode: composition.GeneratedCode,
		ChangeSummary: changeSummary,
		CreatedBy:     userID,
	}
	err = s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		if err := s.scenarioRepo.CreateVersion(ctx, tx, version); err != nil {
			return err
		}
		return s.scenarioRepo.UpdateFields(ctx, tx, composition.ID, map[string]interface{}{
			"status":             model.AIScenarioStatusPublished,
			"current_version_id": version.ID,
			"updated_by":         userID,
		})
	})
	if err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}
	return version, nil
}

// Archive 归档场景编排。
func (s *AIScenarioCompositionService) Archive(ctx context.Context, userID, projectID, compositionID uint) (*model.AIScenarioComposition, error) {
	if _, err := s.Get(ctx, projectID, compositionID); err != nil {
		return nil, err
	}
	if err := s.scenarioRepo.UpdateFields(ctx, nil, compositionID, map[string]interface{}{
		"status":     model.AIScenarioStatusArchived,
		"updated_by": userID,
	}); err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}
	return s.Get(ctx, projectID, compositionID)
}

// Delete 删除未生成历史的场景编排草稿。
func (s *AIScenarioCompositionService) Delete(ctx context.Context, projectID, compositionID uint) error {
	composition, err := s.Get(ctx, projectID, compositionID)
	if err != nil {
		return err
	}
	if composition.Status != model.AIScenarioStatusDraft {
		return ErrConflict(CodeConflict, "仅草稿编排允许删除，已发布或已验证编排请使用归档")
	}
	versions, err := s.scenarioRepo.ListVersions(ctx, compositionID)
	if err != nil {
		return ErrInternal(CodeInternal, err)
	}
	if len(versions) > 0 {
		return ErrConflict(CodeConflict, "编排已生成发布版本，不能删除")
	}
	validationCount, err := s.scenarioRepo.CountValidations(ctx, compositionID)
	if err != nil {
		return ErrInternal(CodeInternal, err)
	}
	if validationCount > 0 {
		return ErrConflict(CodeConflict, "编排已有验证历史，不能删除")
	}
	if err := s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		if err := s.refRepo.ReplaceForSource(ctx, tx, model.AIAssetRefSourceScenario, compositionID, nil); err != nil {
			return err
		}
		if err := s.scenarioRepo.DeleteStepsByScenario(ctx, tx, compositionID); err != nil {
			return err
		}
		return s.scenarioRepo.Delete(ctx, tx, compositionID)
	}); err != nil {
		return ErrInternal(CodeInternal, err)
	}
	return nil
}

// ListVersions 查询编排版本列表。
func (s *AIScenarioCompositionService) ListVersions(ctx context.Context, projectID, compositionID uint) ([]model.AIScenarioCompositionVersion, error) {
	if _, err := s.Get(ctx, projectID, compositionID); err != nil {
		return nil, err
	}
	versions, err := s.scenarioRepo.ListVersions(ctx, compositionID)
	if err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}
	return versions, nil
}

// DiffVersion 比较两个编排版本的 DSL 和生成代码差异。
func (s *AIScenarioCompositionService) DiffVersion(ctx context.Context, compositionID uint, input ScenarioVersionDiffInput) (*ScenarioVersionDiffResult, error) {
	if _, err := s.Get(ctx, input.ProjectID, compositionID); err != nil {
		return nil, err
	}
	if input.BaseVersionID == 0 || input.TargetVersionID == 0 {
		return nil, ErrBadRequest(CodeParamsError, "base_version_id 和 target_version_id 不能为空")
	}
	baseVersion, err := s.scenarioRepo.GetVersionByID(ctx, input.BaseVersionID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound(CodeNotFound, "基准版本不存在")
		}
		return nil, ErrInternal(CodeInternal, err)
	}
	targetVersion, err := s.scenarioRepo.GetVersionByID(ctx, input.TargetVersionID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound(CodeNotFound, "目标版本不存在")
		}
		return nil, ErrInternal(CodeInternal, err)
	}
	if baseVersion.CompositionID != compositionID || targetVersion.CompositionID != compositionID {
		return nil, ErrForbidden(CodeForbidden, "版本不属于当前编排")
	}

	baseDSL := normalizeJSONForDiff(baseVersion.DSLJSON)
	targetDSL := normalizeJSONForDiff(targetVersion.DSLJSON)
	dslStats := buildTextDiffStats(baseDSL, targetDSL, 80)
	codeStats := buildTextDiffStats(baseVersion.GeneratedCode, targetVersion.GeneratedCode, 80)
	result := &ScenarioVersionDiffResult{
		CompositionID: compositionID,
		BaseVersion:   *baseVersion,
		TargetVersion: *targetVersion,
		DSLChanged:    baseDSL != targetDSL,
		CodeChanged:   baseVersion.GeneratedCode != targetVersion.GeneratedCode,
		DSLStats:      dslStats,
		CodeStats:     codeStats,
	}
	result.Summary = buildScenarioVersionDiffSummary(result)
	return result, nil
}

// RollbackVersion 回滚当前编排到指定版本快照。
func (s *AIScenarioCompositionService) RollbackVersion(ctx context.Context, userID, compositionID uint, input ScenarioVersionRollbackInput) (*model.AIScenarioComposition, error) {
	composition, err := s.Get(ctx, input.ProjectID, compositionID)
	if err != nil {
		return nil, err
	}
	if composition.Status == model.AIScenarioStatusArchived {
		return nil, ErrConflict(CodeConflict, "已归档编排不可回滚")
	}
	if composition.CodeEditStatus == model.AIScenarioCodeEditStatusLocked && !input.OverrideLockedCode {
		return nil, ErrConflict(CodeConflict, "生成代码已被人工锁定，请确认覆盖锁定代码后再回滚")
	}
	if input.VersionID == 0 {
		return nil, ErrBadRequest(CodeParamsError, "version_id 不能为空")
	}
	version, err := s.scenarioRepo.GetVersionByID(ctx, input.VersionID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound(CodeNotFound, "回滚版本不存在")
		}
		return nil, ErrInternal(CodeInternal, err)
	}
	if version.CompositionID != compositionID {
		return nil, ErrForbidden(CodeForbidden, "版本不属于当前编排")
	}
	if !json.Valid(version.DSLJSON) {
		return nil, ErrConflict(CodeConflict, "版本 DSL 快照损坏，无法回滚")
	}
	steps, err := buildScenarioStepsFromVersionDSL(composition.ID, version.DSLJSON)
	if err != nil {
		return nil, err
	}
	if failures := validateCompositionDSLSemantics(composition, version.DSLJSON, false); len(failures) > 0 {
		return nil, ErrConflict(CodeConflict, "版本 DSL 校验失败: "+strings.Join(failures, "；"))
	}
	changeSummary := strings.TrimSpace(input.ChangeSummary)
	if changeSummary == "" {
		changeSummary = fmt.Sprintf("回滚到 V%d", version.VersionNo)
	}
	err = s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		if err := s.scenarioRepo.DeleteStepsByScenario(ctx, tx, composition.ID); err != nil {
			return err
		}
		for i := range steps {
			if err := s.scenarioRepo.CreateStep(ctx, tx, &steps[i]); err != nil {
				return err
			}
		}
		if err := s.refRepo.ReplaceForSource(ctx, tx, model.AIAssetRefSourceScenario, composition.ID, buildScenarioReferences(steps)); err != nil {
			return err
		}
		return s.scenarioRepo.UpdateFields(ctx, tx, composition.ID, map[string]interface{}{
			"dsl_json":                 string(version.DSLJSON),
			"generated_code":           version.GeneratedCode,
			"code_edit_status":         model.AIScenarioCodeEditStatusAutoGenerated,
			"code_change_summary":      changeSummary,
			"manual_patched_by":        nil,
			"manual_patched_at":        nil,
			"code_locked_by":           nil,
			"code_locked_at":           nil,
			"current_version_id":       version.ID,
			"latest_validation_id":     nil,
			"latest_validation_status": model.AIValidationStatusNotValidated,
			"status":                   model.AIScenarioStatusGenerated,
			"updated_by":               userID,
			"revision":                 composition.Revision + 1,
		})
	})
	if err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}
	return s.Get(ctx, input.ProjectID, composition.ID)
}

// ListValidations 查询编排验证历史。
func (s *AIScenarioCompositionService) ListValidations(ctx context.Context, projectID, compositionID uint) ([]model.AICompositionValidation, error) {
	if _, err := s.Get(ctx, projectID, compositionID); err != nil {
		return nil, err
	}
	validations, err := s.scenarioRepo.ListValidations(ctx, compositionID)
	if err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}
	for i := range validations {
		results, resultErr := s.scenarioRepo.ListAssertionResults(ctx, validations[i].ID)
		if resultErr == nil {
			validations[i].AssertionResults = results
		}
	}
	return validations, nil
}

func (s *AIScenarioCompositionService) fillValidationAssertionResults(ctx context.Context, validation *model.AICompositionValidation) {
	if validation == nil {
		return
	}
	results, err := s.scenarioRepo.ListAssertionResults(ctx, validation.ID)
	if err == nil {
		validation.AssertionResults = results
	}
}

// References 查询编排引用的固定场景和断言资产。
func (s *AIScenarioCompositionService) References(ctx context.Context, projectID, compositionID uint) ([]model.AIAssetReference, error) {
	if _, err := s.Get(ctx, projectID, compositionID); err != nil {
		return nil, err
	}
	refs, err := s.refRepo.ListBySource(ctx, model.AIAssetRefSourceScenario, compositionID)
	if err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}
	return refs, nil
}

// AIPlanFromTask 基于当前资产库生成可解释的编排建议。
func (s *AIScenarioCompositionService) AIPlanFromTask(ctx context.Context, input AIPlanFromTaskInput) (*AIPlanResult, error) {
	if input.ProjectID == 0 || input.TaskID == 0 {
		return nil, ErrBadRequest(CodeParamsError, "project_id 和 task_id 不能为空")
	}
	task, err := s.aiScriptRepo.GetTask(ctx, input.TaskID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound(CodeNotFound, "测试智编任务不存在")
		}
		return nil, ErrInternal(CodeInternal, err)
	}
	if task.ProjectID != input.ProjectID {
		return nil, ErrForbidden(CodeForbidden, "任务不属于当前项目")
	}
	flows, err := s.flowRepo.ListAllByProject(ctx, input.ProjectID, true, true)
	if err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}
	assertions, err := s.assertionRepo.ListAllByProject(ctx, input.ProjectID, true, true)
	if err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}
	maxSteps := input.MaxSteps
	if maxSteps <= 0 || maxSteps > 20 {
		maxSteps = 20
	}
	sourceVersion, err := s.resolveAIPlanSourceVersion(ctx, task, input.SourceVersionID)
	if err != nil {
		return nil, err
	}
	profile := buildAIPlanSourceProfile(task, sourceVersion)

	candidates := make([]aiPlanCandidate, 0, len(flows)+len(assertions))
	for _, flow := range flows {
		if isValidationUnusableForAI(flow.LatestValidationStatus) {
			continue
		}
		version, versionErr := s.flowRepo.GetLatestPublishedVersion(ctx, flow.ID)
		if versionErr != nil {
			continue
		}
		score, matched := scoreFlowPlanCandidate(profile, flow, version)
		confidence := planConfidence(score, model.AIScenarioStepTypeFlowCall)
		candidates = append(candidates, aiPlanCandidate{
			Score: score,
			Step: AIPlanStep{
				Type:          model.AIScenarioStepTypeFlowCall,
				FlowID:        flow.ID,
				FlowVersionID: version.ID,
				FlowKey:       flow.FlowKey,
				Confidence:    confidence,
				Reason:        buildAIPlanReason("固定场景", flow.FlowName, matched, confidence),
				Inputs:        inferFlowPlanInputs(flow),
			},
		})
	}
	for _, assertion := range assertions {
		if isValidationUnusableForAI(assertion.LatestValidationStatus) {
			continue
		}
		score, matched := scoreAssertionPlanCandidate(profile, assertion)
		confidence := planConfidence(score, model.AIScenarioStepTypeAssertion)
		candidates = append(candidates, aiPlanCandidate{
			Score: score,
			Step: AIPlanStep{
				Type:         model.AIScenarioStepTypeAssertion,
				AssertionID:  assertion.ID,
				AssertionKey: assertion.AssertionKey,
				Confidence:   confidence,
				Reason:       buildAIPlanReason("断言", assertion.AssertionName, matched, confidence),
				Inputs:       inferAssertionPlanInputs(assertion),
			},
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Step.Confidence == candidates[j].Step.Confidence {
			if candidates[i].Step.Type == candidates[j].Step.Type {
				return candidates[i].Score > candidates[j].Score
			}
			return candidates[i].Step.Type == model.AIScenarioStepTypeFlowCall
		}
		return candidates[i].Step.Confidence > candidates[j].Step.Confidence
	})

	steps := make([]AIPlanStep, 0, maxSteps)
	for _, candidate := range candidates {
		if len(steps) >= maxSteps {
			break
		}
		steps = append(steps, candidate.Step)
	}
	confidence := averagePlanConfidence(steps)
	warnings := []string{}
	if len(steps) == 0 {
		confidence = 0.7
		warnings = append(warnings, "当前项目暂无可复用的已发布固定场景或断言")
	} else if confidence < 0.75 {
		warnings = append(warnings, "整体置信度低于 75%，建议逐条确认后再生成编排草稿")
	}
	return &AIPlanResult{
		PlanID:     fmt.Sprintf("plan_%d_%d", input.TaskID, time.Now().Unix()),
		Confidence: confidence,
		Summary:    fmt.Sprintf("基于录制任务、步骤模型和资产契约推荐 %d 个可复用编排步骤", len(steps)),
		Steps:      steps,
		Warnings:   warnings,
	}, nil
}

// AISuggestAssertions 为当前编排推荐断言资产。
func (s *AIScenarioCompositionService) AISuggestAssertions(ctx context.Context, projectID, compositionID uint) (*AIPlanResult, error) {
	if _, err := s.Get(ctx, projectID, compositionID); err != nil {
		return nil, err
	}
	assertions, err := s.assertionRepo.ListAllByProject(ctx, projectID, true, true)
	if err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}
	steps := make([]AIPlanStep, 0, len(assertions))
	for _, assertion := range assertions {
		if isValidationUnusableForAI(assertion.LatestValidationStatus) {
			continue
		}
		steps = append(steps, AIPlanStep{
			Type:         model.AIScenarioStepTypeAssertion,
			AssertionID:  assertion.ID,
			AssertionKey: assertion.AssertionKey,
			Confidence:   0.82,
			Reason:       "断言已发布且允许 AI 推荐",
			Inputs:       map[string]interface{}{},
		})
	}
	return &AIPlanResult{
		PlanID:     fmt.Sprintf("assertion_plan_%d_%d", compositionID, time.Now().Unix()),
		Confidence: 0.82,
		Summary:    fmt.Sprintf("推荐 %d 个可插入断言", len(steps)),
		Steps:      steps,
		Warnings:   []string{},
	}, nil
}

type aiPlanSourceProfile struct {
	Text        string
	Tokens      map[string]struct{}
	ActionTypes map[string]struct{}
}

type aiPlanCandidate struct {
	Score float64
	Step  AIPlanStep
}

func isValidationUnusableForAI(status string) bool {
	return status == model.AIValidationStatusFailed || status == model.AIValidationStatusError
}

func (s *AIScenarioCompositionService) resolveAIPlanSourceVersion(ctx context.Context, task *model.AIScriptTask, sourceVersionID uint) (*model.AIScriptVersion, error) {
	if sourceVersionID > 0 {
		version, err := s.aiScriptRepo.GetScriptVersion(ctx, sourceVersionID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, ErrNotFound(CodeNotFound, "来源脚本版本不存在")
			}
			return nil, ErrInternal(CodeInternal, err)
		}
		if version.TaskID != task.ID {
			return nil, ErrForbidden(CodeForbidden, "来源脚本版本不属于当前任务")
		}
		return version, nil
	}
	version, err := s.aiScriptRepo.GetCurrentScriptVersion(ctx, task.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrConflict(CodeConflict, "任务暂无当前脚本版本，不能生成编排建议")
		}
		return nil, ErrInternal(CodeInternal, err)
	}
	return version, nil
}

func buildAIPlanSourceProfile(task *model.AIScriptTask, version *model.AIScriptVersion) aiPlanSourceProfile {
	parts := []string{task.TaskName, task.ScenarioDesc, task.StartURL, version.ScriptName, version.ScriptContent}
	if version.StepModelJSON != nil {
		if data, err := json.Marshal(version.StepModelJSON); err == nil {
			parts = append(parts, string(data))
		}
	}
	text := strings.ToLower(strings.Join(parts, " "))
	profile := aiPlanSourceProfile{
		Text:        text,
		Tokens:      tokenizePlanText(text),
		ActionTypes: map[string]struct{}{},
	}
	collectPlanActionTypes(version.StepModelJSON, profile.ActionTypes)
	return profile
}

func collectPlanActionTypes(value interface{}, result map[string]struct{}) {
	switch typed := value.(type) {
	case map[string]interface{}:
		for key, item := range typed {
			if strings.EqualFold(key, "action_type") || strings.EqualFold(key, "actionType") || strings.EqualFold(key, "type") {
				if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
					result[strings.ToUpper(strings.TrimSpace(text))] = struct{}{}
				}
			}
			collectPlanActionTypes(item, result)
		}
	case []interface{}:
		for _, item := range typed {
			collectPlanActionTypes(item, result)
		}
	case model.JSONMap:
		collectPlanActionTypes(map[string]interface{}(typed), result)
	}
}

func tokenizePlanText(text string) map[string]struct{} {
	tokens := map[string]struct{}{}
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !isPlanTokenRune(r)
	})
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if len([]rune(field)) < 2 {
			continue
		}
		tokens[field] = struct{}{}
	}
	return tokens
}

func isPlanTokenRune(r rune) bool {
	return r >= 'a' && r <= 'z' ||
		r >= '0' && r <= '9' ||
		r == '_' ||
		r >= 0x4e00 && r <= 0x9fff
}

func scoreFlowPlanCandidate(profile aiPlanSourceProfile, flow model.AIFlowAsset, version *model.AIFlowAssetVersion) (float64, []string) {
	assetText := strings.ToLower(strings.Join([]string{
		flow.FlowKey,
		flow.FlowName,
		flow.Description,
		string(flow.TagsJSON),
		string(flow.PreconditionsJSON),
		string(flow.PostconditionsJSON),
		string(flow.InputSchemaJSON),
		string(flow.OutputSchemaJSON),
		string(flow.DSLJSON),
		string(version.DSLJSON),
	}, " "))
	score, matched := scorePlanText(profile, assetText)
	if strings.Contains(assetText, "login") || strings.Contains(assetText, "登录") {
		if _, ok := profile.ActionTypes["INPUT"]; ok {
			score += 0.08
			matched = append(matched, "录制包含输入动作，匹配登录/表单类固定场景")
		}
	}
	if strings.Contains(assetText, "create") || strings.Contains(assetText, "新建") || strings.Contains(assetText, "创建") {
		if _, ok := profile.ActionTypes["CLICK"]; ok {
			score += 0.06
			matched = append(matched, "录制包含点击动作，匹配创建类流程")
		}
	}
	if flow.LatestValidationStatus == model.AIValidationStatusPassed {
		score += 0.05
	}
	return score, matched
}

func scoreAssertionPlanCandidate(profile aiPlanSourceProfile, assertion model.AIAssertionAsset) (float64, []string) {
	assetText := strings.ToLower(strings.Join([]string{
		assertion.AssertionKey,
		assertion.AssertionName,
		assertion.AssertionType,
		assertion.Description,
		string(assertion.ParamSchemaJSON),
		string(assertion.ImplementationJSON),
	}, " "))
	score, matched := scorePlanText(profile, assetText)
	if strings.Contains(profile.Text, "验证") || strings.Contains(profile.Text, "检查") || strings.Contains(profile.Text, "assert") {
		score += 0.08
		matched = append(matched, "录制任务描述包含验证意图")
	}
	if assertion.LatestValidationStatus == model.AIValidationStatusPassed {
		score += 0.04
	}
	return score, matched
}

func scorePlanText(profile aiPlanSourceProfile, assetText string) (float64, []string) {
	score := 0.2
	matched := []string{}
	for token := range profile.Tokens {
		if strings.Contains(assetText, token) {
			score += 0.08
			if len(matched) < 4 {
				matched = append(matched, token)
			}
		}
	}
	for _, phrase := range []string{"登录", "新建", "创建", "任务", "项目", "列表", "状态", "可见", "断言", "login", "create", "task", "visible"} {
		if strings.Contains(profile.Text, phrase) && strings.Contains(assetText, phrase) {
			score += 0.06
			if len(matched) < 4 {
				matched = append(matched, phrase)
			}
		}
	}
	if score > 1 {
		score = 1
	}
	return score, matched
}

func planConfidence(score float64, stepType string) float64 {
	base := 0.62 + score*0.3
	if stepType == model.AIScenarioStepTypeFlowCall {
		base += 0.03
	}
	if base > 0.96 {
		base = 0.96
	}
	if base < 0.62 {
		base = 0.62
	}
	return roundConfidence(base)
}

func averagePlanConfidence(steps []AIPlanStep) float64 {
	if len(steps) == 0 {
		return 0
	}
	var total float64
	for _, step := range steps {
		total += step.Confidence
	}
	return roundConfidence(total / float64(len(steps)))
}

func roundConfidence(value float64) float64 {
	return float64(int(value*100+0.5)) / 100
}

func buildAIPlanReason(assetType, name string, matched []string, confidence float64) string {
	if len(matched) == 0 {
		return fmt.Sprintf("%s「%s」已发布且允许 AI 复用，当前匹配置信度 %.0f%%", assetType, name, confidence*100)
	}
	return fmt.Sprintf("%s「%s」与录制上下文匹配：%s，置信度 %.0f%%", assetType, name, strings.Join(deduplicateStrings(matched), "、"), confidence*100)
}

func inferFlowPlanInputs(flow model.AIFlowAsset) map[string]interface{} {
	return inferInputsFromSchema(flow.InputSchemaJSON)
}

func inferAssertionPlanInputs(assertion model.AIAssertionAsset) map[string]interface{} {
	inputs := inferInputsFromSchema(assertion.ParamSchemaJSON)
	switch assertion.AssertionType {
	case model.AIAssertionTypeURLContains:
		if _, ok := inputs["expected"]; !ok {
			inputs["expected"] = "${env.BASE_URL}"
		}
	case model.AIAssertionTypeTextContains:
		if _, ok := inputs["text"]; !ok {
			inputs["text"] = "${variables.expectedText}"
		}
	case model.AIAssertionTypeUIVisible, model.AIAssertionTypeUIHidden:
		if _, ok := inputs["selector"]; !ok {
			inputs["selector"] = "${variables.selector}"
		}
	}
	return inputs
}

func inferInputsFromSchema(schema model.RawJSON) map[string]interface{} {
	inputs := map[string]interface{}{}
	if len(schema) == 0 || !json.Valid(schema) {
		return inputs
	}
	var root map[string]interface{}
	if err := json.Unmarshal(schema, &root); err != nil {
		return inputs
	}
	properties := objectFromAny(root["properties"])
	required := map[string]struct{}{}
	for _, item := range dslStringSlice(root["required"]) {
		required[item] = struct{}{}
	}
	for key, value := range properties {
		prop := objectFromAny(value)
		defaultValue, hasDefault := prop["default"]
		switch {
		case hasDefault:
			inputs[key] = defaultValue
		case strings.Contains(strings.ToLower(key), "user"):
			inputs[key] = "${env.ADMIN_USER}"
		case strings.Contains(strings.ToLower(key), "password"):
			inputs[key] = "${env.ADMIN_PASSWORD}"
		case strings.Contains(strings.ToLower(key), "url"):
			inputs[key] = "${env.BASE_URL}"
		case strings.Contains(strings.ToLower(key), "name"):
			inputs[key] = "${variables." + key + "}"
		default:
			if _, ok := required[key]; ok {
				inputs[key] = "${variables." + key + "}"
			}
		}
	}
	return inputs
}

func deduplicateStrings(items []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}

func normalizeCompositionCreateInput(input ScenarioCompositionCreateInput) (ScenarioCompositionCreateInput, error) {
	input.ScenarioKey = strings.TrimSpace(input.ScenarioKey)
	input.ScenarioName = strings.TrimSpace(input.ScenarioName)
	input.Description = strings.TrimSpace(input.Description)
	if input.ProjectID == 0 {
		return input, ErrBadRequest(CodeParamsError, "project_id 不能为空")
	}
	if !flowKeyPattern.MatchString(input.ScenarioKey) {
		return input, ErrBadRequest(CodeParamsError, "scenario_key 仅支持小写字母、数字和下划线，且必须以小写字母开头")
	}
	if input.ScenarioName == "" {
		return input, ErrBadRequest(CodeParamsError, "场景名称不能为空")
	}
	if len(input.ScenarioName) > 128 {
		return input, ErrBadRequest(CodeParamsError, "场景名称不能超过 128 个字符")
	}
	return input, nil
}

func (s *AIScenarioCompositionService) normalizeStepInput(ctx context.Context, projectID uint, input ScenarioStepSaveInput) (ScenarioStepSaveInput, error) {
	input.StepType = strings.TrimSpace(input.StepType)
	input.StepName = strings.TrimSpace(input.StepName)
	input.AtomicAction = strings.TrimSpace(input.AtomicAction)
	input.CodeBlock = strings.TrimSpace(input.CodeBlock)
	if input.StepName == "" {
		input.StepName = defaultStepName(input.StepType)
	}
	if input.StepName == "" {
		return input, ErrBadRequest(CodeParamsError, "步骤名称不能为空")
	}
	if !input.EnabledSpecified {
		input.Enabled = true
	}
	if len(input.ParamMapping) == 0 {
		input.ParamMapping = json.RawMessage(`{}`)
	} else if !json.Valid(input.ParamMapping) {
		return input, ErrBadRequest(CodeParamsError, "参数映射不是合法 JSON")
	}
	if len(input.OutputMapping) == 0 {
		input.OutputMapping = json.RawMessage(`{}`)
	} else if !json.Valid(input.OutputMapping) {
		return input, ErrBadRequest(CodeParamsError, "输出映射不是合法 JSON")
	}

	switch input.StepType {
	case model.AIScenarioStepTypeFlowCall:
		if input.RefFlowID == nil || *input.RefFlowID == 0 {
			return input, ErrBadRequest(CodeParamsError, "固定场景步骤必须选择 ref_flow_id")
		}
		flow, err := s.flowRepo.GetByID(ctx, *input.RefFlowID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return input, ErrNotFound(CodeNotFound, "引用的固定场景不存在")
			}
			return input, ErrInternal(CodeInternal, err)
		}
		if flow.ProjectID != projectID {
			return input, ErrForbidden(CodeForbidden, "引用的固定场景不属于当前项目")
		}
		if flow.Status != model.AIFlowAssetStatusPublished {
			return input, ErrConflict(CodeConflict, "只能引用已发布固定场景")
		}
		if input.RefFlowVersionID == nil || *input.RefFlowVersionID == 0 {
			version, err := s.flowRepo.GetLatestPublishedVersion(ctx, flow.ID)
			if err != nil {
				return input, ErrConflict(CodeConflict, "固定场景暂无可引用的发布版本")
			}
			input.RefFlowVersionID = &version.ID
		} else {
			version, err := s.flowRepo.GetVersionByID(ctx, *input.RefFlowVersionID)
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return input, ErrNotFound(CodeNotFound, "引用的固定场景版本不存在")
				}
				return input, ErrInternal(CodeInternal, err)
			}
			if version.FlowID != flow.ID {
				return input, ErrForbidden(CodeForbidden, "固定场景版本不属于所选固定场景")
			}
			if version.VersionStatus != model.AIFlowAssetStatusPublished {
				return input, ErrConflict(CodeConflict, "只能引用已发布固定场景版本")
			}
		}
	case model.AIScenarioStepTypeAssertion:
		if input.RefAssertionID == nil || *input.RefAssertionID == 0 {
			return input, ErrBadRequest(CodeParamsError, "断言步骤必须选择 ref_assertion_id")
		}
		assertion, err := s.assertionRepo.GetByID(ctx, *input.RefAssertionID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return input, ErrNotFound(CodeNotFound, "引用的断言资产不存在")
			}
			return input, ErrInternal(CodeInternal, err)
		}
		if assertion.ProjectID != projectID {
			return input, ErrForbidden(CodeForbidden, "引用的断言资产不属于当前项目")
		}
		if assertion.Status != model.AIAssertionAssetStatusPublished {
			return input, ErrConflict(CodeConflict, "只能引用已发布断言资产")
		}
	case model.AIScenarioStepTypeAtomicAction:
		if input.AtomicAction == "" {
			return input, ErrBadRequest(CodeParamsError, "原子操作步骤必须填写 atomic_action")
		}
	case model.AIScenarioStepTypeCodeBlock:
		if input.CodeBlock == "" {
			return input, ErrBadRequest(CodeParamsError, "自定义代码块不能为空")
		}
	case model.AIScenarioStepTypeAIGenerated:
		if input.AIConfidence <= 0 {
			input.AIConfidence = 0.75
		}
	default:
		return input, ErrBadRequest(CodeParamsError, "步骤类型无效")
	}
	return input, nil
}

func (s *AIScenarioCompositionService) rebuildScenarioDerivedState(ctx context.Context, tx *gorm.DB, composition *model.AIScenarioComposition, userID uint) error {
	steps, err := s.scenarioRepo.ListStepsTx(ctx, tx, composition.ID)
	if err != nil {
		return err
	}
	dsl := buildCompositionDSLRaw(composition, steps)
	refs := buildScenarioReferences(steps)
	if err := s.refRepo.ReplaceForSource(ctx, tx, model.AIAssetRefSourceScenario, composition.ID, refs); err != nil {
		return err
	}
	status := composition.Status
	if status == model.AIScenarioStatusGenerated || status == model.AIScenarioStatusPassed || status == model.AIScenarioStatusPublished || status == model.AIScenarioStatusFailed {
		status = model.AIScenarioStatusDraft
	}
	fields := map[string]interface{}{
		"dsl_json":                 string(dsl),
		"status":                   status,
		"latest_validation_status": model.AIValidationStatusNotValidated,
		"updated_by":               userID,
		"revision":                 composition.Revision + 1,
	}
	if composition.CodeEditStatus == model.AIScenarioCodeEditStatusLocked {
		fields["code_change_summary"] = "编排步骤已变更，保留锁定人工代码"
	} else {
		fields["generated_code"] = ""
		fields["code_edit_status"] = model.AIScenarioCodeEditStatusAutoGenerated
		fields["code_change_summary"] = "编排步骤已变更，等待重新生成"
		fields["manual_patched_by"] = nil
		fields["manual_patched_at"] = nil
		fields["code_locked_by"] = nil
		fields["code_locked_at"] = nil
	}
	return s.scenarioRepo.UpdateFields(ctx, tx, composition.ID, fields)
}

func buildScenarioReferences(steps []model.AIScenarioStep) []model.AIAssetReference {
	refs := make([]model.AIAssetReference, 0, len(steps))
	for _, step := range steps {
		if step.StepType == model.AIScenarioStepTypeFlowCall && step.RefFlowID != nil {
			refs = append(refs, model.AIAssetReference{
				SourceType:      model.AIAssetRefSourceScenario,
				SourceID:        step.ScenarioID,
				TargetType:      model.AIAssetRefTargetFlow,
				TargetID:        *step.RefFlowID,
				TargetVersionID: step.RefFlowVersionID,
			})
		}
		if step.StepType == model.AIScenarioStepTypeAssertion && step.RefAssertionID != nil {
			refs = append(refs, model.AIAssetReference{
				SourceType: model.AIAssetRefSourceScenario,
				SourceID:   step.ScenarioID,
				TargetType: model.AIAssetRefTargetAssertion,
				TargetID:   *step.RefAssertionID,
			})
		}
	}
	return refs
}

func scenarioStepStableID(step model.AIScenarioStep) string {
	if trimmed := strings.TrimSpace(step.DSLStepID); trimmed != "" {
		return trimmed
	}
	if step.ID > 0 {
		return fmt.Sprintf("step_%d", step.ID)
	}
	if step.StepNo > 0 {
		return fmt.Sprintf("step_no_%d", step.StepNo)
	}
	return "step_unknown"
}

func normalizeJSONForDiff(data model.RawJSON) string {
	if len(data) == 0 {
		return ""
	}
	var value interface{}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return string(data)
	}
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return string(data)
	}
	return string(encoded)
}

func buildScenarioVersionDiffSummary(result *ScenarioVersionDiffResult) []string {
	summary := []string{}
	if result.DSLChanged {
		summary = append(summary, fmt.Sprintf("DSL 变更：新增 %d 行，删除 %d 行", result.DSLStats.AddedLines, result.DSLStats.RemovedLines))
	} else {
		summary = append(summary, "DSL 无差异")
	}
	if result.CodeChanged {
		summary = append(summary, fmt.Sprintf("生成代码变更：新增 %d 行，删除 %d 行", result.CodeStats.AddedLines, result.CodeStats.RemovedLines))
	} else {
		summary = append(summary, "生成代码无差异")
	}
	return summary
}

func buildTextDiffStats(baseText, targetText string, maxPreview int) ScenarioVersionDiffStats {
	baseLines := splitDiffLines(baseText)
	targetLines := splitDiffLines(targetText)
	stats := ScenarioVersionDiffStats{
		BaseLineCount:   len(baseLines),
		TargetLineCount: len(targetLines),
		Preview:         []ScenarioVersionDiffLine{},
	}
	baseIndex := 0
	targetIndex := 0
	for baseIndex < len(baseLines) || targetIndex < len(targetLines) {
		switch {
		case baseIndex < len(baseLines) && targetIndex < len(targetLines) && baseLines[baseIndex] == targetLines[targetIndex]:
			stats.UnchangedLines++
			appendDiffPreview(&stats, maxPreview, "context", baseLines[baseIndex])
			baseIndex++
			targetIndex++
		case targetIndex+1 < len(targetLines) && baseIndex < len(baseLines) && baseLines[baseIndex] == targetLines[targetIndex+1]:
			stats.AddedLines++
			appendDiffPreview(&stats, maxPreview, "added", targetLines[targetIndex])
			targetIndex++
		case baseIndex+1 < len(baseLines) && targetIndex < len(targetLines) && baseLines[baseIndex+1] == targetLines[targetIndex]:
			stats.RemovedLines++
			appendDiffPreview(&stats, maxPreview, "removed", baseLines[baseIndex])
			baseIndex++
		default:
			if baseIndex < len(baseLines) {
				stats.RemovedLines++
				appendDiffPreview(&stats, maxPreview, "removed", baseLines[baseIndex])
				baseIndex++
			}
			if targetIndex < len(targetLines) {
				stats.AddedLines++
				appendDiffPreview(&stats, maxPreview, "added", targetLines[targetIndex])
				targetIndex++
			}
		}
	}
	return stats
}

func appendDiffPreview(stats *ScenarioVersionDiffStats, maxPreview int, kind string, text string) {
	if maxPreview <= 0 {
		stats.Truncated = true
		return
	}
	if len(stats.Preview) >= maxPreview {
		stats.Truncated = true
		return
	}
	stats.Preview = append(stats.Preview, ScenarioVersionDiffLine{Kind: kind, Text: text})
}

func splitDiffLines(text string) []string {
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	if strings.TrimSpace(normalized) == "" {
		return []string{}
	}
	return strings.Split(normalized, "\n")
}

func buildScenarioStepsFromVersionDSL(compositionID uint, raw model.RawJSON) ([]model.AIScenarioStep, error) {
	var root map[string]interface{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&root); err != nil {
		return nil, ErrBadRequest(CodeParamsError, "版本 DSL 不是合法对象")
	}
	rawSteps, ok := root["steps"].([]interface{})
	if !ok {
		return nil, ErrBadRequest(CodeParamsError, "版本 DSL steps 必须是数组")
	}
	steps := make([]model.AIScenarioStep, 0, len(rawSteps))
	for index, item := range rawSteps {
		stepObject, ok := item.(map[string]interface{})
		if !ok {
			return nil, ErrBadRequest(CodeParamsError, fmt.Sprintf("版本 DSL steps[%d] 必须是对象", index))
		}
		stepType, _ := stepObject["type"].(string)
		if !isValidScenarioStepType(stepType) {
			return nil, ErrBadRequest(CodeParamsError, fmt.Sprintf("版本 DSL steps[%d] 类型无效", index))
		}
		dslStepID, _ := stepObject["id"].(string)
		dslStepID = strings.TrimSpace(dslStepID)
		if dslStepID == "" {
			dslStepID = fmt.Sprintf("step_snapshot_%03d", index+1)
		}
		stepName, _ := stepObject["name"].(string)
		stepName = strings.TrimSpace(stepName)
		if stepName == "" {
			stepName = defaultStepName(stepType)
		}
		enabled := true
		if rawEnabled, ok := stepObject["enabled"].(bool); ok {
			enabled = rawEnabled
		}
		step := model.AIScenarioStep{
			ScenarioID:        compositionID,
			DSLStepID:         dslStepID,
			StepNo:            index + 1,
			StepType:          stepType,
			StepName:          stepName,
			ParamMappingJSON:  rawJSONFromDSLValue(stepObject["inputs"]),
			OutputMappingJSON: rawJSONFromDSLValue(stepObject["outputs"]),
			Enabled:           enabled,
		}
		ref := objectFromAny(stepObject["ref"])
		if flowID, ok := uintFromDSLValue(ref["flow_id"]); ok && flowID > 0 {
			step.RefFlowID = &flowID
		}
		if flowVersionID, ok := uintFromDSLValue(ref["flow_version_id"]); ok && flowVersionID > 0 {
			step.RefFlowVersionID = &flowVersionID
		}
		if assertionID, ok := uintFromDSLValue(ref["assertion_id"]); ok && assertionID > 0 {
			step.RefAssertionID = &assertionID
		}
		if action := objectFromAny(stepObject["action"]); len(action) > 0 {
			step.AtomicAction, _ = action["type"].(string)
		}
		if codeBlock := objectFromAny(stepObject["code_block"]); len(codeBlock) > 0 {
			step.CodeBlock, _ = codeBlock["code"].(string)
			step.ManualReviewed, _ = codeBlock["manual_reviewed"].(bool)
		}
		if aiMeta := objectFromAny(stepObject["ai_meta"]); len(aiMeta) > 0 {
			if confidence, ok := floatFromDSLValue(aiMeta["confidence"]); ok {
				step.AIConfidence = confidence
			}
		}
		steps = append(steps, step)
	}
	return steps, nil
}

func rawJSONFromDSLValue(value interface{}) model.RawJSON {
	if value == nil {
		return mustRawJSON(map[string]interface{}{})
	}
	return mustRawJSON(value)
}

func buildCompositionDSLRaw(composition *model.AIScenarioComposition, steps []model.AIScenarioStep) model.RawJSON {
	stepPayload := make([]map[string]interface{}, 0, len(steps))
	for _, step := range steps {
		stepPayload = append(stepPayload, buildStepDSL(step))
	}
	payload := map[string]interface{}{
		"schema_version": "1.0",
		"scenario": map[string]interface{}{
			"scenario_key":  composition.ScenarioKey,
			"scenario_name": composition.ScenarioName,
			"project_id":    composition.ProjectID,
		},
		"variables": map[string]interface{}{},
		"env":       []string{"ADMIN_USER", "ADMIN_PASSWORD", "BASE_URL"},
		"steps":     stepPayload,
		"policies": map[string]interface{}{
			"failure_strategy": "STOP_ON_FAILURE",
			"retry":            map[string]interface{}{"enabled": false, "times": 0, "interval_ms": 0},
			"evidence":         map[string]interface{}{"screenshot": "ON_FAILURE", "trace": true, "dom_snapshot": false},
		},
		"ai_meta": map[string]interface{}{
			"generated_by_ai": false,
			"plan_id":         "",
			"confidence":      0,
			"explanation":     "",
		},
	}
	return mustRawJSON(payload)
}

func buildStepDSL(step model.AIScenarioStep) map[string]interface{} {
	payload := map[string]interface{}{
		"id":               scenarioStepStableID(step),
		"name":             step.StepName,
		"type":             step.StepType,
		"enabled":          step.Enabled,
		"inputs":           rawJSONToObject(step.ParamMappingJSON),
		"outputs":          rawJSONToObject(step.OutputMappingJSON),
		"depends_on":       []string{},
		"failure_strategy": "INHERIT",
		"retry":            map[string]interface{}{"enabled": false, "times": 0, "interval_ms": 0},
		"evidence":         map[string]interface{}{"screenshot": "INHERIT", "trace": true},
	}
	switch step.StepType {
	case model.AIScenarioStepTypeFlowCall:
		ref := map[string]interface{}{}
		if step.RefFlowID != nil {
			ref["flow_id"] = *step.RefFlowID
		}
		if step.RefFlowVersionID != nil {
			ref["flow_version_id"] = *step.RefFlowVersionID
		}
		payload["ref"] = ref
	case model.AIScenarioStepTypeAssertion:
		ref := map[string]interface{}{}
		if step.RefAssertionID != nil {
			ref["assertion_id"] = *step.RefAssertionID
		}
		payload["ref"] = ref
	case model.AIScenarioStepTypeAtomicAction:
		payload["action"] = map[string]interface{}{"type": step.AtomicAction}
	case model.AIScenarioStepTypeCodeBlock:
		payload["code_block"] = map[string]interface{}{"language": "typescript", "code": step.CodeBlock, "manual_reviewed": step.ManualReviewed}
	case model.AIScenarioStepTypeAIGenerated:
		payload["ai_meta"] = map[string]interface{}{"recommended": true, "confidence": step.AIConfidence, "reason": "AI 推荐的临时步骤"}
	}
	return payload
}

func rawJSONToObject(data model.RawJSON) interface{} {
	if len(data) == 0 {
		return map[string]interface{}{}
	}
	var value interface{}
	if err := json.Unmarshal(data, &value); err != nil {
		return map[string]interface{}{}
	}
	return value
}

// collectNestedFlowAssets 按 FLOW_CALL 引用广度优先收集嵌套固定场景（深度不超过 maxFlowReferenceDepth），
// 使被引用的固定场景同样内联进生成代码，FLOW_CALL 步骤可直接调用 flows.<flow_key>。
func (s *AIScenarioCompositionService) collectNestedFlowAssets(ctx context.Context, projectID uint, flowAssets map[uint]*model.AIFlowAsset, flowVersions map[uint]*model.AIFlowAssetVersion) error {
	queue := make([]uint, 0, len(flowAssets))
	depths := make(map[uint]int, len(flowAssets))
	for id := range flowAssets {
		queue = append(queue, id)
		depths[id] = 1
	}
	for len(queue) > 0 {
		currentID := queue[0]
		queue = queue[1:]
		if depths[currentID] >= maxFlowReferenceDepth {
			continue
		}
		flow := flowAssets[currentID]
		dsl := flow.DSLJSON
		if version := flowVersions[currentID]; version != nil && len(version.DSLJSON) > 0 {
			dsl = version.DSLJSON
		}
		rawRefs, err := extractFlowDSLReferences(dsl)
		if err != nil {
			continue
		}
		for _, rawRef := range rawRefs {
			var target *model.AIFlowAsset
			var lookupErr error
			switch {
			case rawRef.FlowID > 0:
				if _, ok := flowAssets[rawRef.FlowID]; ok {
					continue
				}
				target, lookupErr = s.flowRepo.GetByID(ctx, rawRef.FlowID)
			case rawRef.FlowKey != "":
				target, lookupErr = s.flowRepo.GetByProjectAndKey(ctx, projectID, rawRef.FlowKey)
			default:
				continue
			}
			if lookupErr != nil {
				return ErrConflict(CodeConflict, fmt.Sprintf("固定场景 %s 引用的嵌套固定场景不存在", flow.FlowKey))
			}
			if target.ProjectID != projectID {
				return ErrForbidden(CodeForbidden, "嵌套引用的固定场景不属于当前项目")
			}
			if _, ok := flowAssets[target.ID]; ok {
				continue
			}
			flowAssets[target.ID] = target
			if rawRef.FlowVersionID > 0 {
				if version, versionErr := s.flowRepo.GetVersionByID(ctx, rawRef.FlowVersionID); versionErr == nil && version.FlowID == target.ID {
					flowVersions[target.ID] = version
				}
			}
			depths[target.ID] = depths[currentID] + 1
			queue = append(queue, target.ID)
		}
	}
	return nil
}

func (s *AIScenarioCompositionService) compilePlaywrightSpec(ctx context.Context, composition *model.AIScenarioComposition, confirmPartial bool) (string, []string, []string, error) {
	if len(composition.Steps) == 0 {
		return "", nil, nil, ErrConflict(CodeConflict, "编排至少需要一个步骤")
	}
	warnings := []string{}
	confirmedPartialFlows := []string{}
	var b strings.Builder
	b.WriteString("import { test, expect, type Page } from '@playwright/test'\n\n")
	b.WriteString("type ScenarioContext = {\n")
	b.WriteString("  page: Page\n")
	b.WriteString("  outputs: Record<string, Record<string, unknown>>\n")
	b.WriteString("  env: Record<string, string>\n")
	b.WriteString("  variables: Record<string, unknown>\n")
	b.WriteString("}\n\n")
	b.WriteString("function escapeRegExp(value: string): string {\n")
	b.WriteString("  return value.replace(/[.*+?^${}()|[\\]\\\\]/g, '\\\\$&')\n")
	b.WriteString("}\n\n")
	b.WriteString("function getByPath(source: unknown, path: string): unknown {\n")
	b.WriteString("  if (!path) return source\n")
	b.WriteString("  const parts = path.replace(/^\\$\\.?/, '').split('.').filter(Boolean)\n")
	b.WriteString("  let current: unknown = source\n")
	b.WriteString("  for (const part of parts) {\n")
	b.WriteString("    if (current === null || current === undefined) return undefined\n")
	b.WriteString("    if (Array.isArray(current) && /^\\d+$/.test(part)) current = current[Number(part)]\n")
	b.WriteString("    else current = (current as Record<string, unknown>)[part]\n")
	b.WriteString("  }\n")
	b.WriteString("  return current\n")
	b.WriteString("}\n\n")
	b.WriteString("function resolveReference(ctx: ScenarioContext, ref: string): unknown {\n")
	b.WriteString("  if (ref.startsWith('env.')) return ctx.env[ref.slice(4)] || ''\n")
	b.WriteString("  if (ref.startsWith('variables.')) return getByPath(ctx.variables, ref.slice(10))\n")
	b.WriteString("  if (ref.startsWith('literal.')) return ref.slice(8)\n")
	b.WriteString("  if (ref.startsWith('steps.')) {\n")
	b.WriteString("    const match = ref.match(/^steps\\.([^.]+)\\.outputs\\.?(.*)$/)\n")
	b.WriteString("    if (!match) return ''\n")
	b.WriteString("    return getByPath(ctx.outputs[match[1]] || {}, match[2] || '')\n")
	b.WriteString("  }\n")
	b.WriteString("  return ''\n")
	b.WriteString("}\n\n")
	b.WriteString("function resolveScenarioValue(ctx: ScenarioContext, value: unknown): unknown {\n")
	b.WriteString("  if (Array.isArray(value)) return value.map((item) => resolveScenarioValue(ctx, item))\n")
	b.WriteString("  if (value && typeof value === 'object') {\n")
	b.WriteString("    return Object.fromEntries(Object.entries(value as Record<string, unknown>).map(([key, item]) => [key, resolveScenarioValue(ctx, item)]))\n")
	b.WriteString("  }\n")
	b.WriteString("  if (typeof value !== 'string') return value\n")
	b.WriteString("  const exact = value.match(/^\\$\\{([^}]+)\\}$/)\n")
	b.WriteString("  if (exact) return resolveReference(ctx, exact[1].trim())\n")
	b.WriteString("  return value.replace(/\\$\\{([^}]+)\\}/g, (_match, ref) => String(resolveReference(ctx, String(ref).trim()) ?? ''))\n")
	b.WriteString("}\n\n")
	b.WriteString("function mapStepOutputs(result: unknown, mapping: Record<string, unknown>, fallback: Record<string, unknown> = {}): Record<string, unknown> {\n")
	b.WriteString("  if (!mapping || Object.keys(mapping).length === 0) return typeof result === 'object' && result !== null ? result as Record<string, unknown> : fallback\n")
	b.WriteString("  return Object.fromEntries(Object.entries(mapping).map(([key, path]) => [key, typeof path === 'string' ? getByPath(result, path) : path]))\n")
	b.WriteString("}\n\n")
	b.WriteString("function cssLocator(page: Page, selector: unknown) {\n")
	b.WriteString("  const resolved = String(selector ?? '')\n")
	b.WriteString("  if (!resolved) throw new Error('缺少选择器参数')\n")
	b.WriteString("  return page.locator(resolved)\n")
	b.WriteString("}\n\n")
	b.WriteString("const flows: Record<string, (ctx: ScenarioContext, inputs: Record<string, unknown>) => Promise<Record<string, unknown>>> = {\n")
	flowAssets := make(map[uint]*model.AIFlowAsset)
	flowVersions := make(map[uint]*model.AIFlowAssetVersion)
	for _, step := range composition.Steps {
		if step.RefFlowID == nil {
			continue
		}
		flow, err := s.flowRepo.GetByID(ctx, *step.RefFlowID)
		if err != nil {
			return "", nil, nil, ErrConflict(CodeConflict, "引用的固定场景不存在")
		}
		flowAssets[flow.ID] = flow
		if step.RefFlowVersionID != nil {
			version, versionErr := s.flowRepo.GetVersionByID(ctx, *step.RefFlowVersionID)
			if versionErr != nil {
				return "", nil, nil, ErrConflict(CodeConflict, "引用的固定场景版本不存在")
			}
			flowVersions[flow.ID] = version
		}
	}
	if err := s.collectNestedFlowAssets(ctx, composition.ProjectID, flowAssets, flowVersions); err != nil {
		return "", nil, nil, err
	}
	flowKeys := make(map[uint]string, len(flowAssets))
	for id, flow := range flowAssets {
		flowKeys[id] = flow.FlowKey
	}
	for _, id := range sortedUintKeys(flowKeys) {
		flow := flowAssets[id]
		version := flowVersions[id]
		dsl := flow.DSLJSON
		if version != nil && len(version.DSLJSON) > 0 {
			dsl = version.DSLJSON
		}
		if failures := dryRunCompileFlowDSL(dsl, flowKeys); len(failures) > 0 {
			if !confirmPartial {
				return "", nil, nil, ErrConflictWithData(
					CodeAICompositionFlowCompileFailed,
					fmt.Sprintf("固定场景 %s dry-run 编译失败：%s；请修复该资产 DSL，或显式确认引用 PARTIAL 资产后重试", flow.FlowKey, summarizeFlowCompileFailures(failures)),
					map[string]interface{}{"flow_key": flow.FlowKey, "compile_failures": failures},
				)
			}
			confirmedPartialFlows = append(confirmedPartialFlows, flow.FlowKey)
			warnings = append(warnings, fmt.Sprintf("已显式确认引用 PARTIAL 固定场景 %s，跳过其中 %d 个不可编译步骤", flow.FlowKey, len(failures)))
		}
		fmt.Fprintf(&b, "  %s: async (parentCtx, inputs) => {\n", flow.FlowKey)
		b.WriteString("    const ctx: ScenarioContext = { ...parentCtx, variables: { ...parentCtx.variables, ...inputs } }\n")
		b.WriteString("    const page = ctx.page\n")
		b.WriteString("    const outputs: Record<string, unknown> = {}\n")
		flowLines, flowWarnings := compileFlowDSLStatements(dsl, "    ", flowKeys)
		warnings = append(warnings, flowWarnings...)
		if len(flowLines) == 0 {
			if strings.TrimSpace(flow.CodeSnapshot) != "" {
				warnings = append(warnings, fmt.Sprintf("固定场景 %s 暂无法从 DSL 编译，已保留代码快照摘要但不会内联执行", flow.FlowKey))
			}
			fmt.Fprintf(&b, "    throw new Error('固定场景 %s 缺少可编译 DSL 步骤')\n", flow.FlowKey)
		} else {
			for _, line := range flowLines {
				b.WriteString(line)
			}
		}
		b.WriteString("    outputs.currentUrl = page.url()\n")
		b.WriteString("    return outputs\n")
		b.WriteString("  },\n")
	}
	b.WriteString("}\n\n")
	b.WriteString("const assertions: Record<string, (ctx: ScenarioContext, inputs: Record<string, unknown>) => Promise<void>> = {\n")
	assertionAssets := make(map[uint]*model.AIAssertionAsset)
	for _, step := range composition.Steps {
		if step.RefAssertionID == nil {
			continue
		}
		assertion, err := s.assertionRepo.GetByID(ctx, *step.RefAssertionID)
		if err != nil {
			return "", nil, nil, ErrConflict(CodeConflict, "引用的断言资产不存在")
		}
		assertionAssets[assertion.ID] = assertion
	}
	assertionKeys := make(map[uint]string, len(assertionAssets))
	for id, assertion := range assertionAssets {
		assertionKeys[id] = assertion.AssertionKey
	}
	for _, id := range sortedUintKeys(assertionKeys) {
		assertion := assertionAssets[id]
		fmt.Fprintf(&b, "  %s: async (ctx, inputs) => {\n", assertion.AssertionKey)
		b.WriteString("    const page = ctx.page\n")
		assertionLines, assertionWarnings, err := compileAssertionAssetStatements(assertion, "    ")
		if err != nil {
			return "", nil, nil, err
		}
		warnings = append(warnings, assertionWarnings...)
		for _, line := range assertionLines {
			b.WriteString(line)
		}
		b.WriteString("  },\n")
	}
	b.WriteString("}\n\n")
	fmt.Fprintf(&b, "test(%s, async ({ page }) => {\n", tsString(composition.ScenarioName))
	b.WriteString("  const ctx: ScenarioContext = {\n")
	b.WriteString("    page,\n")
	b.WriteString("    outputs: {},\n")
	b.WriteString("    env: {\n")
	b.WriteString("      ADMIN_USER: process.env.ADMIN_USER || '',\n")
	b.WriteString("      ADMIN_PASSWORD: process.env.ADMIN_PASSWORD || '',\n")
	b.WriteString("      BASE_URL: process.env.BASE_URL || '',\n")
	b.WriteString("    },\n")
	b.WriteString("    variables: {},\n")
	b.WriteString("  }\n")
	for _, step := range composition.Steps {
		if !step.Enabled {
			fmt.Fprintf(&b, "  // 跳过已禁用步骤：%s\n", step.StepName)
			continue
		}
		inputLiteral := rawJSONToTSLiteral(step.ParamMappingJSON)
		outputLiteral := rawJSONToTSLiteral(step.OutputMappingJSON)
		stepID := scenarioStepStableID(step)
		switch step.StepType {
		case model.AIScenarioStepTypeFlowCall:
			if step.RefFlowID == nil {
				return "", nil, nil, ErrConflict(CodeConflict, "固定场景步骤缺少引用")
			}
			flowKey := flowKeys[*step.RefFlowID]
			if flowKey == "" {
				return "", nil, nil, ErrConflict(CodeConflict, "固定场景引用不可用")
			}
			b.WriteString("  {\n")
			fmt.Fprintf(&b, "    const inputs = resolveScenarioValue(ctx, %s) as Record<string, unknown>\n", inputLiteral)
			fmt.Fprintf(&b, "    const result = await flows.%s(ctx, inputs)\n", flowKey)
			fmt.Fprintf(&b, "    ctx.outputs[%s] = mapStepOutputs(result, %s as Record<string, unknown>, result)\n", tsString(stepID), outputLiteral)
			b.WriteString("  }\n")
		case model.AIScenarioStepTypeAssertion:
			if step.RefAssertionID == nil {
				return "", nil, nil, ErrConflict(CodeConflict, "断言步骤缺少引用")
			}
			assertionKey := assertionKeys[*step.RefAssertionID]
			if assertionKey == "" {
				return "", nil, nil, ErrConflict(CodeConflict, "断言引用不可用")
			}
			fmt.Fprintf(&b, "  await assertions.%s(ctx, resolveScenarioValue(ctx, %s) as Record<string, unknown>)\n", assertionKey, inputLiteral)
			fmt.Fprintf(&b, "  ctx.outputs[%s] = { status: 'PASSED' }\n", tsString(stepID))
		case model.AIScenarioStepTypeAtomicAction:
			line, err := compileAtomicAction(step, inputLiteral, outputLiteral, stepID)
			if err != nil {
				return "", nil, nil, err
			}
			b.WriteString(line)
		case model.AIScenarioStepTypeCodeBlock:
			if !step.ManualReviewed {
				return "", nil, nil, ErrConflict(CodeConflict, "自定义代码块生成前必须标记已审核")
			}
			b.WriteString("  // 已审核自定义代码块\n")
			b.WriteString(indentCodeBlock(step.CodeBlock, "  "))
			b.WriteString("\n")
		case model.AIScenarioStepTypeAIGenerated:
			return "", nil, nil, ErrConflict(CodeConflict, "AI 临时步骤不能直接生成正式代码，请先采纳为确定步骤类型")
		}
	}
	b.WriteString("})\n")
	if len(assertionKeys) == 0 {
		warnings = append(warnings, "当前编排未配置断言步骤")
	}
	return b.String(), warnings, confirmedPartialFlows, nil
}

func compileAtomicAction(step model.AIScenarioStep, inputLiteral string, outputLiteral string, stepID string) (string, error) {
	params := map[string]interface{}{}
	_ = json.Unmarshal(step.ParamMappingJSON, &params)
	selector, _ := params["selector"].(string)
	url, _ := params["url"].(string)
	switch step.AtomicAction {
	case "goto":
		if url == "" {
			return "", ErrBadRequest(CodeParamsError, "goto 原子操作需要 url 参数")
		}
		return "  {\n    const inputs = resolveScenarioValue(ctx, " + inputLiteral + ") as any\n    await page.goto(String(inputs.url || ''))\n    ctx.outputs[" + tsString(stepID) + "] = mapStepOutputs({ url: page.url() }, " + outputLiteral + " as Record<string, unknown>, { url: page.url() })\n  }\n", nil
	case "click":
		if selector == "" {
			return "", ErrBadRequest(CodeParamsError, "click 原子操作需要 selector 参数")
		}
		return "  {\n    const inputs = resolveScenarioValue(ctx, " + inputLiteral + ") as any\n    await cssLocator(page, inputs.selector).click()\n    ctx.outputs[" + tsString(stepID) + "] = mapStepOutputs({ clicked: true }, " + outputLiteral + " as Record<string, unknown>, { clicked: true })\n  }\n", nil
	case "fill":
		if selector == "" {
			return "", ErrBadRequest(CodeParamsError, "fill 原子操作需要 selector 参数")
		}
		return "  {\n    const inputs = resolveScenarioValue(ctx, " + inputLiteral + ") as any\n    await cssLocator(page, inputs.selector).fill(String(inputs.value ?? ''))\n    ctx.outputs[" + tsString(stepID) + "] = mapStepOutputs({ filled: true }, " + outputLiteral + " as Record<string, unknown>, { filled: true })\n  }\n", nil
	case "wait":
		return "  {\n    const inputs = resolveScenarioValue(ctx, " + inputLiteral + ") as any\n    await page.waitForTimeout(Number(inputs.timeout_ms || 1000))\n    ctx.outputs[" + tsString(stepID) + "] = mapStepOutputs({ waited: true }, " + outputLiteral + " as Record<string, unknown>, { waited: true })\n  }\n", nil
	case "select":
		if selector == "" {
			return "", ErrBadRequest(CodeParamsError, "select 原子操作需要 selector 参数")
		}
		return "  {\n    const inputs = resolveScenarioValue(ctx, " + inputLiteral + ") as any\n    await cssLocator(page, inputs.selector).selectOption(String(inputs.value ?? ''))\n    ctx.outputs[" + tsString(stepID) + "] = mapStepOutputs({ selected: true }, " + outputLiteral + " as Record<string, unknown>, { selected: true })\n  }\n", nil
	case "press":
		key, _ := params["key"].(string)
		if key == "" {
			key, _ = params["value"].(string)
		}
		if selector == "" {
			return "", ErrBadRequest(CodeParamsError, "press 原子操作需要 selector 参数")
		}
		if key == "" {
			return "", ErrBadRequest(CodeParamsError, "press 原子操作需要 key 参数")
		}
		return "  {\n    const inputs = resolveScenarioValue(ctx, " + inputLiteral + ") as any\n    await cssLocator(page, inputs.selector).press(String(inputs.key ?? inputs.value ?? ''))\n    ctx.outputs[" + tsString(stepID) + "] = mapStepOutputs({ pressed: true }, " + outputLiteral + " as Record<string, unknown>, { pressed: true })\n  }\n", nil
	case "wait_for":
		if selector == "" {
			return "", ErrBadRequest(CodeParamsError, "wait_for 原子操作需要 selector 参数")
		}
		return "  {\n    const inputs = resolveScenarioValue(ctx, " + inputLiteral + ") as any\n    await cssLocator(page, inputs.selector).waitFor({ state: 'visible', timeout: Number(inputs.timeout_ms || 5000) })\n    ctx.outputs[" + tsString(stepID) + "] = mapStepOutputs({ waited: true }, " + outputLiteral + " as Record<string, unknown>, { waited: true })\n  }\n", nil
	default:
		return "", ErrBadRequest(CodeParamsError, "原子操作类型无效，支持："+strings.Join(supportedAtomicActions, "、"))
	}
}

// supportedFlowDSLStepTypes 固定场景 DSL 支持的步骤类型清单，与 compileFlowStepStatement 能力保持同一清单。
// 新增类型必须同步更新编译器与本清单。
var supportedFlowDSLStepTypes = []string{
	"NAVIGATE", "GOTO", "CLICK", "INPUT", "FILL", "SELECT", "KEY_PRESS", "WAIT", "ASSERT", "ASSERT_CANDIDATE", "FLOW_CALL",
}

// supportedAtomicActions 编排原子操作支持的类型清单，与 compileAtomicAction 能力保持同一清单。
var supportedAtomicActions = []string{"goto", "click", "fill", "select", "press", "wait", "wait_for"}

func compileFlowDSLStatements(data model.RawJSON, indent string, flowKeys map[uint]string) ([]string, []string) {
	lines, failures := compileFlowDSL(data, indent, flowKeys)
	warnings := make([]string, 0, len(failures))
	for _, failure := range failures {
		warnings = append(warnings, flowCompileFailureWarning(failure))
	}
	return lines, warnings
}

// dryRunCompileFlowDSL 对固定场景 DSL 执行 dry-run 编译，返回结构化失败清单。
// 发布门禁、compile-check 接口、compile_health 标记与编排生成代码硬错误均复用本函数。
func dryRunCompileFlowDSL(data model.RawJSON, flowKeys map[uint]string) []model.FlowCompileFailure {
	_, failures := compileFlowDSL(data, "", flowKeys)
	return failures
}

func compileFlowDSL(data model.RawJSON, indent string, flowKeys map[uint]string) ([]string, []model.FlowCompileFailure) {
	root := map[string]interface{}{}
	if len(data) == 0 || !json.Valid(data) {
		return nil, []model.FlowCompileFailure{{Reason: "固定场景 DSL 为空或不是合法 JSON"}}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&root); err != nil {
		return nil, []model.FlowCompileFailure{{Reason: "固定场景 DSL 解析失败"}}
	}
	steps := dslObjectSlice(root["generation_steps"])
	if len(steps) == 0 {
		steps = dslObjectSlice(root["steps"])
	}
	lines := make([]string, 0, len(steps)*3)
	failures := []model.FlowCompileFailure{}
	for index, step := range steps {
		stepLines, failure := compileFlowStepStatement(step, index+1, indent, flowKeys)
		if failure != nil {
			failures = append(failures, *failure)
			continue
		}
		lines = append(lines, stepLines...)
	}
	return lines, failures
}

func flowCompileFailureWarning(failure model.FlowCompileFailure) string {
	if failure.StepNo == 0 {
		return failure.Reason
	}
	return fmt.Sprintf("固定场景步骤 %d %s，已跳过", failure.StepNo, failure.Reason)
}

func summarizeFlowCompileFailures(failures []model.FlowCompileFailure) string {
	parts := make([]string, 0, len(failures))
	for _, failure := range failures {
		if failure.StepNo == 0 {
			parts = append(parts, failure.Reason)
			continue
		}
		stepType := failure.StepType
		if stepType == "" {
			stepType = "未知类型"
		}
		parts = append(parts, fmt.Sprintf("步骤 %d（%s）%s", failure.StepNo, stepType, failure.Reason))
	}
	return strings.Join(parts, "；")
}

func flowStepFailure(stepNo int, stepType, reason string) *model.FlowCompileFailure {
	return &model.FlowCompileFailure{StepNo: stepNo, StepType: stepType, Reason: reason}
}

func compileFlowStepStatement(step map[string]interface{}, fallbackNo int, indent string, flowKeys map[uint]string) ([]string, *model.FlowCompileFailure) {
	actionType := strings.ToUpper(firstNonEmptyString(step, "type", "step_type", "action_type", "actionType"))
	if actionType == model.AIScenarioStepTypeAtomicAction {
		actionType = strings.ToUpper(firstNonEmptyString(step, "action.type", "action_type", "actionType"))
	}
	if actionType == "" {
		return nil, flowStepFailure(fallbackNo, "", "缺少动作类型")
	}
	inputs := objectFromAny(step["inputs"])
	selector := firstNonEmptyStringFromObjects([]map[string]interface{}{inputs, step}, "selector", "locator", "locator_used", "locatorUsed")
	url := firstNonEmptyStringFromObjects([]map[string]interface{}{inputs, step}, "url", "page_url", "pageUrl")
	value := firstNonEmptyStringFromObjects([]map[string]interface{}{inputs, step}, "value", "input_value", "inputValue")
	timeout := firstNonEmptyStringFromObjects([]map[string]interface{}{inputs, step}, "timeout_ms", "timeoutMs", "timeout")
	if timeout == "" {
		timeout = "1000"
	}

	switch actionType {
	case "NAVIGATE", "GOTO":
		if url == "" {
			return nil, flowStepFailure(fallbackNo, actionType, "缺少跳转 URL")
		}
		return []string{indent + "await page.goto(String(resolveScenarioValue(ctx, " + tsStringOrInput(url, "url") + ")))\n"}, nil
	case "CLICK":
		if selector == "" {
			return nil, flowStepFailure(fallbackNo, actionType, "缺少点击选择器")
		}
		return []string{indent + "await " + compileLocatorExpression(selector, "selector") + ".click()\n"}, nil
	case "INPUT", "FILL":
		if selector == "" {
			return nil, flowStepFailure(fallbackNo, actionType, "缺少输入选择器")
		}
		return []string{indent + "await " + compileLocatorExpression(selector, "selector") + ".fill(String(resolveScenarioValue(ctx, " + tsStringOrInput(value, "value") + ") ?? ''))\n"}, nil
	case "SELECT":
		if selector == "" {
			return nil, flowStepFailure(fallbackNo, actionType, "缺少选择器")
		}
		return []string{indent + "await " + compileLocatorExpression(selector, "selector") + ".selectOption(String(resolveScenarioValue(ctx, " + tsStringOrInput(value, "value") + ") ?? ''))\n"}, nil
	case "KEY_PRESS":
		if selector == "" {
			return nil, flowStepFailure(fallbackNo, actionType, "缺少按键选择器")
		}
		return []string{indent + "await " + compileLocatorExpression(selector, "selector") + ".press(String(resolveScenarioValue(ctx, " + tsStringOrInput(value, "value") + ") ?? ''))\n"}, nil
	case "WAIT":
		if strings.HasPrefix(strings.ToLower(value), "load") {
			return []string{indent + "await page.waitForLoadState('networkidle')\n"}, nil
		}
		return []string{indent + "await page.waitForTimeout(Number(resolveScenarioValue(ctx, " + tsStringOrInput(timeout, "timeout_ms") + ") || 1000))\n"}, nil
	case "ASSERT", "ASSERT_CANDIDATE":
		if selector != "" {
			return []string{indent + "await expect(" + compileLocatorExpression(selector, "selector") + ").toBeVisible()\n"}, nil
		}
		if value != "" {
			return []string{indent + "await expect(page.getByText(String(resolveScenarioValue(ctx, " + tsStringOrInput(value, "text") + ")))).toBeVisible()\n"}, nil
		}
		return nil, flowStepFailure(fallbackNo, actionType, "缺少断言目标")
	case model.AIScenarioStepTypeFlowCall:
		refObject := objectFromAny(step["ref"])
		flowKey := firstNonEmptyStringFromObjects([]map[string]interface{}{refObject, step}, "flow_key", "ref_flow_key", "flowKey", "refFlowKey")
		if flowKey == "" {
			flowID, _ := firstUintFromObjects([]map[string]interface{}{refObject, step}, "flow_id", "ref_flow_id", "flowId", "refFlowId")
			if flowID == 0 {
				return nil, flowStepFailure(fallbackNo, actionType, "缺少 flow_key 或 flow_id 引用")
			}
			flowKey = flowKeys[flowID]
		}
		if !flowKeyPattern.MatchString(flowKey) {
			return nil, flowStepFailure(fallbackNo, actionType, "引用的 flow_key 无法解析为可编译的固定场景")
		}
		inputsLiteral := "{}"
		if len(inputs) > 0 {
			if data, err := json.Marshal(inputs); err == nil {
				inputsLiteral = string(data)
			}
		}
		return []string{indent + "await flows." + flowKey + "(ctx, resolveScenarioValue(ctx, " + inputsLiteral + ") as Record<string, unknown>)\n"}, nil
	default:
		return nil, flowStepFailure(fallbackNo, actionType, fmt.Sprintf("动作类型 %s 暂不支持", actionType))
	}
}

func compileLocatorExpression(selector string, inputKey string) string {
	if expression, ok := safePlaywrightLocatorExpression(selector); ok {
		return expression
	}
	return "cssLocator(page, resolveScenarioValue(ctx, " + tsStringOrInput(selector, inputKey) + "))"
}

func safePlaywrightLocatorExpression(selector string) (string, bool) {
	trimmed := strings.TrimSpace(selector)
	if trimmed == "" || strings.ContainsAny(trimmed, "\r\n;") {
		return "", false
	}
	allowedPrefixes := []string{
		"locator(",
		"getByRole(",
		"getByText(",
		"getByLabel(",
		"getByPlaceholder(",
		"getByTestId(",
		"getByAltText(",
		"getByTitle(",
	}
	if strings.HasPrefix(trimmed, "page.") {
		withoutPage := strings.TrimPrefix(trimmed, "page.")
		for _, prefix := range allowedPrefixes {
			if strings.HasPrefix(withoutPage, prefix) {
				return trimmed, true
			}
		}
		return "", false
	}
	for _, prefix := range allowedPrefixes {
		if strings.HasPrefix(trimmed, prefix) {
			return "page." + trimmed, true
		}
	}
	return "", false
}

func compileAssertionAssetStatements(assertion *model.AIAssertionAsset, indent string) ([]string, []string, error) {
	implementation := map[string]interface{}{}
	if len(assertion.ImplementationJSON) > 0 && json.Valid(assertion.ImplementationJSON) {
		_ = json.Unmarshal(assertion.ImplementationJSON, &implementation)
	}
	if code := strings.TrimSpace(firstNonEmptyString(implementation, "code", "template", "typescript")); code != "" {
		return compileCustomAssertionStatements(assertion.ParamSchemaJSON, code, indent), nil, nil
	}

	switch assertion.AssertionType {
	case model.AIAssertionTypeUIVisible:
		return []string{indent + "await expect(cssLocator(page, inputs.selector)).toBeVisible({ timeout: Number(inputs.timeout_ms || 5000) })\n"}, nil, nil
	case model.AIAssertionTypeUIHidden:
		return []string{indent + "await expect(cssLocator(page, inputs.selector)).toBeHidden({ timeout: Number(inputs.timeout_ms || 5000) })\n"}, nil, nil
	case model.AIAssertionTypeTextContains:
		return []string{indent + "await expect(page.getByText(String(inputs.text || inputs.expected || ''))).toBeVisible({ timeout: Number(inputs.timeout_ms || 5000) })\n"}, nil, nil
	case model.AIAssertionTypeURLContains:
		return []string{indent + "await expect(page).toHaveURL(new RegExp(escapeRegExp(String(inputs.expected || inputs.url_part || ''))))\n"}, nil, nil
	case model.AIAssertionTypeTableRowExists:
		return []string{
			indent + "const table = cssLocator(page, inputs.table_selector)\n",
			indent + "await expect(table.getByText(String(inputs.match || inputs.expected || ''))).toBeVisible({ timeout: Number(inputs.timeout_ms || 5000) })\n",
		}, nil, nil
	case model.AIAssertionTypeTableCellEquals:
		return []string{
			indent + "const table = cssLocator(page, inputs.table_selector)\n",
			indent + "const row = table.getByText(String(inputs.row_match || '')).locator('xpath=ancestor::tr').first()\n",
			indent + "await expect(row.getByText(String(inputs.expected || ''))).toBeVisible({ timeout: Number(inputs.timeout_ms || 5000) })\n",
		}, nil, nil
	case model.AIAssertionTypeAPIFieldEquals:
		return []string{
			indent + "const actual = getByPath(ctx.outputs, String(inputs.response_ref || ''))\n",
			indent + "await expect(getByPath(actual, String(inputs.json_path || ''))).toEqual(inputs.expected)\n",
		}, nil, nil
	case model.AIAssertionTypeCustomCode:
		return nil, nil, ErrConflict(CodeConflict, "自定义代码断言必须提供 implementation.code 或 implementation.template")
	default:
		return nil, nil, ErrBadRequest(CodeParamsError, "断言类型无效")
	}
}

func compileCustomAssertionStatements(paramSchema model.RawJSON, code string, indent string) []string {
	lines := []string{
		indent + "// 断言资产自定义实现\n",
		indent + "const params = inputs\n",
	}
	for _, key := range schemaPropertyKeys(paramSchema) {
		if isSafeTSIdentifier(key) {
			lines = append(lines, fmt.Sprintf("%sconst %s = String(params[%s] ?? '')\n", indent, key, tsString(key)))
		}
	}
	lines = append(lines, indentCodeBlock(code, indent)+"\n")
	return lines
}

func schemaPropertyKeys(schema model.RawJSON) []string {
	if len(schema) == 0 || !json.Valid(schema) {
		return nil
	}
	root := map[string]interface{}{}
	if err := json.Unmarshal(schema, &root); err != nil {
		return nil
	}
	properties := objectFromAny(root["properties"])
	keys := make([]string, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func isSafeTSIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for index, r := range value {
		if index == 0 {
			if !isTSIdentifierStartRune(r) {
				return false
			}
			continue
		}
		if !isTSIdentifierPartRune(r) {
			return false
		}
	}
	return true
}

func isTSIdentifierStartRune(r rune) bool {
	return r == '_' || r == '$' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
}

func isTSIdentifierPartRune(r rune) bool {
	return isTSIdentifierStartRune(r) || r >= '0' && r <= '9'
}

func dslObjectSlice(value interface{}) []map[string]interface{} {
	items, ok := value.([]interface{})
	if !ok {
		return nil
	}
	result := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		if object, ok := item.(map[string]interface{}); ok {
			result = append(result, object)
		}
	}
	return result
}

func objectFromAny(value interface{}) map[string]interface{} {
	if object, ok := value.(map[string]interface{}); ok {
		return object
	}
	return map[string]interface{}{}
}

func firstNonEmptyString(values map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value, ok := lookupMapPath(values, key); ok {
			if text, ok := stringifyJSONScalar(value); ok && strings.TrimSpace(text) != "" {
				return strings.TrimSpace(text)
			}
		}
	}
	return ""
}

func firstNonEmptyStringFromObjects(objects []map[string]interface{}, keys ...string) string {
	for _, object := range objects {
		if value := firstNonEmptyString(object, keys...); value != "" {
			return value
		}
	}
	return ""
}

func lookupMapPath(values map[string]interface{}, path string) (interface{}, bool) {
	if values == nil {
		return nil, false
	}
	current := interface{}(values)
	for _, part := range strings.Split(path, ".") {
		object, ok := current.(map[string]interface{})
		if !ok {
			return nil, false
		}
		value, exists := object[part]
		if !exists {
			return nil, false
		}
		current = value
	}
	return current, true
}

func tsString(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func tsStringOrInput(value string, inputKey string) string {
	if strings.TrimSpace(value) != "" {
		return tsString(value)
	}
	return "inputs." + inputKey
}

func rawJSONToTSLiteral(data model.RawJSON) string {
	if len(data) == 0 {
		return "{}"
	}
	var value interface{}
	if err := json.Unmarshal(data, &value); err != nil {
		return "{}"
	}
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func (s *AIScenarioCompositionService) validateCompositionStructure(ctx context.Context, composition *model.AIScenarioComposition) []string {
	failures := []string{}
	if len(composition.Steps) == 0 {
		failures = append(failures, "编排至少需要一个步骤")
	}
	if !json.Valid(composition.DSLJSON) {
		failures = append(failures, "DSL 不是合法 JSON")
	}
	failures = append(failures, validateCompositionDSLSemantics(composition, composition.DSLJSON, true)...)
	for _, step := range composition.Steps {
		if !step.Enabled {
			continue
		}
		if step.AIConfidence > 0 && step.AIConfidence < lowConfidenceConfirmThreshold && !step.ManualReviewed {
			failures = append(failures, fmt.Sprintf("步骤 %s AI 置信度低于 80%%，需要人工确认", step.StepName))
		}
		switch step.StepType {
		case model.AIScenarioStepTypeFlowCall:
			if step.RefFlowID == nil || step.RefFlowVersionID == nil {
				failures = append(failures, fmt.Sprintf("步骤 %s 缺少固定场景或版本引用", step.StepName))
				continue
			}
			flow, err := s.flowRepo.GetByID(ctx, *step.RefFlowID)
			if err != nil {
				failures = append(failures, fmt.Sprintf("步骤 %s 引用的固定场景不存在", step.StepName))
				continue
			}
			if flow.ProjectID != composition.ProjectID {
				failures = append(failures, fmt.Sprintf("步骤 %s 引用的固定场景不属于当前项目", step.StepName))
			}
			if flow.Status != model.AIFlowAssetStatusPublished {
				failures = append(failures, fmt.Sprintf("步骤 %s 引用的固定场景未发布", step.StepName))
			}
			if flow.LatestValidationStatus == model.AIValidationStatusFailed || flow.LatestValidationStatus == model.AIValidationStatusError {
				failures = append(failures, fmt.Sprintf("步骤 %s 引用的固定场景最近验证失败", step.StepName))
			}
			version, err := s.flowRepo.GetVersionByID(ctx, *step.RefFlowVersionID)
			if err != nil {
				failures = append(failures, fmt.Sprintf("步骤 %s 引用的固定场景版本不存在", step.StepName))
			} else if version.FlowID != flow.ID || version.VersionStatus != model.AIFlowAssetStatusPublished {
				failures = append(failures, fmt.Sprintf("步骤 %s 引用的固定场景版本不可用", step.StepName))
			}
		case model.AIScenarioStepTypeAssertion:
			if step.RefAssertionID == nil {
				failures = append(failures, fmt.Sprintf("步骤 %s 缺少断言引用", step.StepName))
				continue
			}
			assertion, err := s.assertionRepo.GetByID(ctx, *step.RefAssertionID)
			if err != nil {
				failures = append(failures, fmt.Sprintf("步骤 %s 引用的断言资产不存在", step.StepName))
				continue
			}
			if assertion.ProjectID != composition.ProjectID {
				failures = append(failures, fmt.Sprintf("步骤 %s 引用的断言资产不属于当前项目", step.StepName))
			}
			if assertion.Status != model.AIAssertionAssetStatusPublished {
				failures = append(failures, fmt.Sprintf("步骤 %s 引用的断言资产未发布", step.StepName))
			}
			if assertion.LatestValidationStatus == model.AIValidationStatusFailed || assertion.LatestValidationStatus == model.AIValidationStatusError {
				failures = append(failures, fmt.Sprintf("步骤 %s 引用的断言资产最近验证失败", step.StepName))
			}
		case model.AIScenarioStepTypeAtomicAction:
			if step.AtomicAction == "" {
				failures = append(failures, fmt.Sprintf("步骤 %s 缺少原子操作类型", step.StepName))
			}
		case model.AIScenarioStepTypeCodeBlock:
			if !step.ManualReviewed {
				failures = append(failures, fmt.Sprintf("步骤 %s 自定义代码块未审核", step.StepName))
			}
		case model.AIScenarioStepTypeAIGenerated:
			failures = append(failures, fmt.Sprintf("步骤 %s 仍是 AI 临时步骤", step.StepName))
		}
	}
	return failures
}

func validateCompositionDSLSemantics(composition *model.AIScenarioComposition, raw model.RawJSON, requirePublished bool) []string {
	failures := []string{}
	if len(raw) == 0 {
		return append(failures, "DSL 不能为空")
	}
	var root map[string]interface{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&root); err != nil {
		return append(failures, "DSL 不是合法对象")
	}
	if schemaVersion, _ := root["schema_version"].(string); schemaVersion != "1.0" {
		failures = append(failures, "DSL schema_version 必须为 1.0")
	}
	scenario, ok := root["scenario"].(map[string]interface{})
	if !ok {
		failures = append(failures, "DSL scenario 缺失")
	} else {
		if projectID, ok := uintFromDSLValue(scenario["project_id"]); !ok || projectID != composition.ProjectID {
			failures = append(failures, "DSL scenario.project_id 与编排所属项目不一致")
		}
		if scenarioKey, _ := scenario["scenario_key"].(string); strings.TrimSpace(scenarioKey) != "" && scenarioKey != composition.ScenarioKey {
			failures = append(failures, "DSL scenario_key 与编排标识不一致")
		}
	}

	envSet, envOK := dslEnvSet(root["env"])
	if !envOK {
		failures = append(failures, "DSL env 必须声明为字符串数组")
	}
	for envKey := range envSet {
		if _, ok := allowedCompositionEnvKeys[envKey]; !ok {
			failures = append(failures, fmt.Sprintf("DSL env.%s 不在白名单内", envKey))
		}
	}

	stepsRaw, ok := root["steps"].([]interface{})
	if !ok {
		failures = append(failures, "DSL steps 必须是数组")
		return failures
	}
	stepOrder := make(map[string]int, len(stepsRaw))
	stepObjects := make([]map[string]interface{}, 0, len(stepsRaw))
	for index, rawStep := range stepsRaw {
		step, ok := rawStep.(map[string]interface{})
		if !ok {
			failures = append(failures, fmt.Sprintf("DSL steps[%d] 必须是对象", index))
			continue
		}
		stepObjects = append(stepObjects, step)
		stepID, _ := step["id"].(string)
		stepID = strings.TrimSpace(stepID)
		if stepID == "" {
			failures = append(failures, fmt.Sprintf("DSL steps[%d].id 不能为空", index))
		} else if _, exists := stepOrder[stepID]; exists {
			failures = append(failures, fmt.Sprintf("DSL step id %s 重复", stepID))
		} else {
			stepOrder[stepID] = index
		}
		stepType, _ := step["type"].(string)
		if !isValidScenarioStepType(stepType) {
			failures = append(failures, fmt.Sprintf("DSL step %s 类型无效", stepID))
		}
		if stepType == model.AIScenarioStepTypeCodeBlock && !dslCodeBlockReviewed(step) {
			failures = append(failures, fmt.Sprintf("DSL step %s 自定义代码块未审核", stepID))
		}
		if requirePublished && stepType == model.AIScenarioStepTypeAIGenerated {
			failures = append(failures, fmt.Sprintf("DSL step %s 仍是 AI 临时步骤", stepID))
		}
	}
	for index, step := range stepObjects {
		stepID, _ := step["id"].(string)
		for _, depID := range dslStringSlice(step["depends_on"]) {
			depIndex, exists := stepOrder[depID]
			if !exists {
				failures = append(failures, fmt.Sprintf("DSL step %s depends_on 引用不存在的步骤 %s", stepID, depID))
				continue
			}
			if depIndex >= index {
				failures = append(failures, fmt.Sprintf("DSL step %s depends_on 不能引用未来步骤 %s", stepID, depID))
			}
		}
		failures = append(failures, collectDSLReferenceFailures(step, index, stepOrder, envSet, fmt.Sprintf("steps[%d]", index))...)
		failures = append(failures, collectSensitivePlaintextFailures(step, fmt.Sprintf("steps[%d]", index))...)
	}
	failures = append(failures, collectSensitivePlaintextFailures(root["variables"], "variables")...)
	return failures
}

func dslEnvSet(value interface{}) (map[string]struct{}, bool) {
	items, ok := value.([]interface{})
	if !ok {
		return map[string]struct{}{}, false
	}
	result := make(map[string]struct{}, len(items))
	for _, item := range items {
		envKey, ok := item.(string)
		if !ok || strings.TrimSpace(envKey) == "" {
			return result, false
		}
		result[strings.TrimSpace(envKey)] = struct{}{}
	}
	return result, true
}

func dslStringSlice(value interface{}) []string {
	items, ok := value.([]interface{})
	if !ok {
		return nil
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
			result = append(result, strings.TrimSpace(text))
		}
	}
	return result
}

func dslCodeBlockReviewed(step map[string]interface{}) bool {
	if reviewed, _ := step["manual_reviewed"].(bool); reviewed {
		return true
	}
	codeBlock, ok := step["code_block"].(map[string]interface{})
	if !ok {
		return false
	}
	reviewed, _ := codeBlock["manual_reviewed"].(bool)
	return reviewed
}

func collectDSLReferenceFailures(value interface{}, currentIndex int, stepOrder map[string]int, envSet map[string]struct{}, path string) []string {
	failures := []string{}
	switch typed := value.(type) {
	case map[string]interface{}:
		for key, item := range typed {
			failures = append(failures, collectDSLReferenceFailures(item, currentIndex, stepOrder, envSet, path+"."+key)...)
		}
	case []interface{}:
		for index, item := range typed {
			failures = append(failures, collectDSLReferenceFailures(item, currentIndex, stepOrder, envSet, fmt.Sprintf("%s[%d]", path, index))...)
		}
	case string:
		for _, match := range dslReferencePattern.FindAllStringSubmatch(typed, -1) {
			if len(match) < 2 {
				continue
			}
			ref := strings.TrimSpace(match[1])
			switch {
			case strings.HasPrefix(ref, "env."):
				envKey := strings.TrimPrefix(ref, "env.")
				if _, ok := allowedCompositionEnvKeys[envKey]; !ok {
					failures = append(failures, fmt.Sprintf("%s 引用了非白名单环境变量 %s", path, envKey))
				}
				if _, ok := envSet[envKey]; !ok {
					failures = append(failures, fmt.Sprintf("%s 引用的环境变量 %s 未在 env 声明", path, envKey))
				}
			case strings.HasPrefix(ref, "steps."):
				stepID := strings.TrimPrefix(ref, "steps.")
				if dot := strings.Index(stepID, "."); dot >= 0 {
					stepID = stepID[:dot]
				}
				refIndex, ok := stepOrder[stepID]
				if !ok {
					failures = append(failures, fmt.Sprintf("%s 引用不存在的步骤输出 %s", path, stepID))
					continue
				}
				if refIndex >= currentIndex {
					failures = append(failures, fmt.Sprintf("%s 不能引用未来步骤输出 %s", path, stepID))
				}
			case strings.HasPrefix(ref, "variables."), strings.HasPrefix(ref, "literal."):
				continue
			default:
				failures = append(failures, fmt.Sprintf("%s 引用了不支持的参数表达式 %s", path, ref))
			}
		}
	}
	return failures
}

func collectSensitivePlaintextFailures(value interface{}, path string) []string {
	failures := []string{}
	switch typed := value.(type) {
	case map[string]interface{}:
		for key, item := range typed {
			currentPath := path + "." + key
			if containsSensitiveDSLKey(key) {
				if text, ok := item.(string); ok && isPlainSensitiveValue(text) {
					failures = append(failures, fmt.Sprintf("%s 不能保存敏感明文，请改用 env 或前置步骤输出", currentPath))
				}
			}
			failures = append(failures, collectSensitivePlaintextFailures(item, currentPath)...)
		}
	case []interface{}:
		for index, item := range typed {
			failures = append(failures, collectSensitivePlaintextFailures(item, fmt.Sprintf("%s[%d]", path, index))...)
		}
	}
	return failures
}

func containsSensitiveDSLKey(key string) bool {
	lowered := strings.ToLower(key)
	for _, part := range sensitiveDSLKeyParts {
		if strings.Contains(lowered, part) {
			return true
		}
	}
	return false
}

func isPlainSensitiveValue(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	return !strings.HasPrefix(trimmed, "${env.") && !strings.HasPrefix(trimmed, "${steps.")
}

func uintFromDSLValue(value interface{}) (uint, bool) {
	switch typed := value.(type) {
	case json.Number:
		number, err := typed.Int64()
		if err != nil || number < 0 {
			return 0, false
		}
		return uint(number), true
	case float64:
		if typed < 0 {
			return 0, false
		}
		return uint(typed), true
	default:
		return 0, false
	}
}

func floatFromDSLValue(value interface{}) (float64, bool) {
	switch typed := value.(type) {
	case json.Number:
		number, err := typed.Float64()
		if err != nil {
			return 0, false
		}
		return number, true
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	default:
		return 0, false
	}
}

func isValidScenarioStepType(stepType string) bool {
	switch stepType {
	case model.AIScenarioStepTypeFlowCall,
		model.AIScenarioStepTypeAssertion,
		model.AIScenarioStepTypeAtomicAction,
		model.AIScenarioStepTypeCodeBlock,
		model.AIScenarioStepTypeAIGenerated:
		return true
	default:
		return false
	}
}

func scenarioStatusAfterValidation(status string) string {
	if status == model.AICompositionValidationStatusPassed {
		return model.AIScenarioStatusPassed
	}
	return model.AIScenarioStatusFailed
}

func (s *AIScenarioCompositionService) fillCompositionVirtualFields(ctx context.Context, compositions []model.AIScenarioComposition) {
	if len(compositions) == 0 {
		return
	}
	projectIDs := make([]uint, 0, len(compositions))
	userIDs := make([]uint, 0, len(compositions))
	for _, composition := range compositions {
		projectIDs = append(projectIDs, composition.ProjectID)
		userIDs = append(userIDs, composition.CreatedBy)
	}
	projectMap := batchProjectNames(ctx, s.projectRepo, s.logger, deduplicateUints(projectIDs))
	userMap := batchUserNames(ctx, s.userRepo, s.logger, deduplicateUints(userIDs))
	for i := range compositions {
		compositions[i].ProjectName = projectMap[compositions[i].ProjectID]
		compositions[i].CreatedName = userMap[compositions[i].CreatedBy]
		if count, err := s.refRepo.CountSourceTargets(ctx, model.AIAssetRefSourceScenario, compositions[i].ID, model.AIAssetRefTargetFlow); err == nil {
			compositions[i].FlowRefCount = count
		}
		if count, err := s.refRepo.CountSourceTargets(ctx, model.AIAssetRefSourceScenario, compositions[i].ID, model.AIAssetRefTargetAssertion); err == nil {
			compositions[i].AssertionRefCount = count
		}
	}
}

func (s *AIScenarioCompositionService) fillCompositionVirtualField(ctx context.Context, composition *model.AIScenarioComposition) {
	if project, err := s.projectRepo.FindByID(ctx, composition.ProjectID); err == nil && project != nil {
		composition.ProjectName = project.Name
	}
	if user, err := s.userRepo.FindByID(ctx, composition.CreatedBy); err == nil && user != nil {
		composition.CreatedName = user.Name
	}
	if count, err := s.refRepo.CountSourceTargets(ctx, model.AIAssetRefSourceScenario, composition.ID, model.AIAssetRefTargetFlow); err == nil {
		composition.FlowRefCount = count
	}
	if count, err := s.refRepo.CountSourceTargets(ctx, model.AIAssetRefSourceScenario, composition.ID, model.AIAssetRefTargetAssertion); err == nil {
		composition.AssertionRefCount = count
	}
	composition.OutdatedFlowRefs = detectOutdatedFlowRefs(ctx, s.refRepo, s.flowRepo, model.AIAssetRefSourceScenario, composition.ID)
}

func (s *AIScenarioCompositionService) fillStepNames(ctx context.Context, steps []model.AIScenarioStep) {
	for i := range steps {
		if steps[i].RefFlowID != nil {
			if flow, err := s.flowRepo.GetByID(ctx, *steps[i].RefFlowID); err == nil && flow != nil {
				steps[i].FlowName = flow.FlowName
			}
		}
		if steps[i].RefAssertionID != nil {
			if assertion, err := s.assertionRepo.GetByID(ctx, *steps[i].RefAssertionID); err == nil && assertion != nil {
				steps[i].AssertionName = assertion.AssertionName
			}
		}
	}
}

func defaultStepName(stepType string) string {
	switch stepType {
	case model.AIScenarioStepTypeFlowCall:
		return "调用固定场景"
	case model.AIScenarioStepTypeAssertion:
		return "执行断言"
	case model.AIScenarioStepTypeAtomicAction:
		return "原子操作"
	case model.AIScenarioStepTypeCodeBlock:
		return "自定义代码"
	case model.AIScenarioStepTypeAIGenerated:
		return "AI 推荐步骤"
	default:
		return ""
	}
}

func sortedUintKeys(values map[uint]string) []uint {
	keys := make([]uint, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

func indentCodeBlock(code string, prefix string) string {
	lines := strings.Split(code, "\n")
	for i := range lines {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}

// ai_flow_asset_service.go — 测试智编固定场景资产业务逻辑
package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"gorm.io/gorm"

	"testpilot/internal/model"
	"testpilot/internal/repository"
)

var flowKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,127}$`)

const maxFlowReferenceDepth = 3

// AIFlowAssetService 管理测试智编固定场景资产的发布、查询和版本。
type AIFlowAssetService struct {
	logger       *slog.Logger
	flowRepo     *repository.AIFlowAssetRepo
	refRepo      *repository.AIAssetReferenceRepo
	aiScriptRepo *repository.AIScriptRepo
	projectRepo  repository.ProjectRepository
	userRepo     repository.UserRepository
	txMgr        *repository.TxManager
}

// NewAIFlowAssetService 创建固定场景资产服务。
func NewAIFlowAssetService(
	logger *slog.Logger,
	flowRepo *repository.AIFlowAssetRepo,
	refRepo *repository.AIAssetReferenceRepo,
	aiScriptRepo *repository.AIScriptRepo,
	projectRepo repository.ProjectRepository,
	userRepo repository.UserRepository,
	txMgr *repository.TxManager,
) *AIFlowAssetService {
	return &AIFlowAssetService{
		logger:       logger.With("module", "ai_flow_asset"),
		flowRepo:     flowRepo,
		refRepo:      refRepo,
		aiScriptRepo: aiScriptRepo,
		projectRepo:  projectRepo,
		userRepo:     userRepo,
		txMgr:        txMgr,
	}
}

// FlowAssetListInput 表示固定场景列表查询输入。
type FlowAssetListInput struct {
	ProjectID        uint
	Keyword          string
	Status           string
	ValidationStatus string
	Page             int
	PageSize         int
}

// PublishFlowAssetInput 表示从录制任务发布固定场景的输入。
type PublishFlowAssetInput struct {
	ProjectID      uint
	FlowKey        string
	FlowName       string
	Description    string
	Tags           []string
	InputSchema    json.RawMessage
	OutputSchema   json.RawMessage
	Preconditions  []string
	Postconditions []string
	AllowAIReuse   bool
	ChangeSummary  string
}

// SaveFlowAssetInput 表示手动创建或编辑固定场景资产的输入。
type SaveFlowAssetInput struct {
	ProjectID      uint
	FlowKey        string
	FlowName       string
	Description    string
	Tags           []string
	InputSchema    json.RawMessage
	OutputSchema   json.RawMessage
	Preconditions  []string
	Postconditions []string
	DSL            json.RawMessage
	CodeSnapshot   string
	AllowAIReuse   bool
	ChangeSummary  string
}

// PublishFlowAssetResult 表示固定场景发布结果。
type PublishFlowAssetResult struct {
	FlowID        uint   `json:"flow_id"`
	FlowVersionID uint   `json:"flow_version_id"`
	Status        string `json:"status"`
}

// FlowCompileCheckResult 表示固定场景 DSL dry-run 自检结果，与发布门禁返回同一结构。
type FlowCompileCheckResult struct {
	FlowID             uint                       `json:"flow_id"`
	CompileHealth      string                     `json:"compile_health"`
	SupportedStepTypes []string                   `json:"supported_step_types"`
	CompileFailures    []model.FlowCompileFailure `json:"compile_failures"`
}

// dryRunFlowCompile 对固定场景 DSL 执行 dry-run 编译，返回结构化失败清单。
func (s *AIFlowAssetService) dryRunFlowCompile(ctx context.Context, dsl model.RawJSON) []model.FlowCompileFailure {
	flowKeys := map[uint]string{}
	if rawRefs, err := extractFlowDSLReferences(dsl); err == nil {
		for _, rawRef := range rawRefs {
			if rawRef.FlowID == 0 {
				continue
			}
			if _, ok := flowKeys[rawRef.FlowID]; ok {
				continue
			}
			if flow, lookupErr := s.flowRepo.GetByID(ctx, rawRef.FlowID); lookupErr == nil {
				flowKeys[rawRef.FlowID] = flow.FlowKey
			}
		}
	}
	return dryRunCompileFlowDSL(dsl, flowKeys)
}

// applyCompileHealth 根据 dry-run 失败明细填充 compile_health 标记与落库字段。
func applyCompileHealth(flow *model.AIFlowAsset, failures []model.FlowCompileFailure) {
	flow.CompileFailures = failures
	flow.CompileFailuresJSON = encodeCompileFailures(failures)
	if len(failures) > 0 {
		flow.CompileHealth = model.AIFlowCompileHealthPartial
		return
	}
	flow.CompileHealth = model.AIFlowCompileHealthOK
}

func encodeCompileFailures(failures []model.FlowCompileFailure) model.RawJSON {
	if failures == nil {
		failures = []model.FlowCompileFailure{}
	}
	return mustRawJSON(failures)
}

func decodeCompileFailures(data model.RawJSON) []model.FlowCompileFailure {
	failures := []model.FlowCompileFailure{}
	if len(data) > 0 {
		_ = json.Unmarshal(data, &failures)
	}
	return failures
}

// refreshCompileHealth 重新计算 compile_health 并落库缓存，详情/列表读库即可，不再每次实时 dry-run。
func (s *AIFlowAssetService) refreshCompileHealth(ctx context.Context, flow *model.AIFlowAsset) {
	applyCompileHealth(flow, s.dryRunFlowCompile(ctx, flow.DSLJSON))
	if err := s.flowRepo.UpdateFields(ctx, nil, flow.ID, map[string]interface{}{
		"compile_health":        flow.CompileHealth,
		"compile_failures_json": string(flow.CompileFailuresJSON),
		"updated_at":            flow.UpdatedAt,
	}); err != nil {
		s.logger.Error("persist compile health failed", "error", err, "flow_id", flow.ID)
	}
}

// CompileCheck 草稿阶段手动触发 dry-run 编译自检，返回与发布门禁相同结构的结果。
func (s *AIFlowAssetService) CompileCheck(ctx context.Context, projectID, flowID uint) (*FlowCompileCheckResult, error) {
	flow, err := s.Get(ctx, projectID, flowID)
	if err != nil {
		return nil, err
	}
	s.refreshCompileHealth(ctx, flow)
	failures := flow.CompileFailures
	if failures == nil {
		failures = []model.FlowCompileFailure{}
	}
	return &FlowCompileCheckResult{
		FlowID:             flow.ID,
		CompileHealth:      flow.CompileHealth,
		SupportedStepTypes: supportedFlowDSLStepTypes,
		CompileFailures:    failures,
	}, nil
}

// List 分页查询固定场景资产。
func (s *AIFlowAssetService) List(ctx context.Context, input FlowAssetListInput) ([]model.AIFlowAsset, int64, error) {
	if input.ProjectID == 0 {
		return nil, 0, ErrBadRequest(CodeParamsError, "project_id 不能为空")
	}
	if input.Page < 1 {
		input.Page = 1
	}
	if input.PageSize < 1 || input.PageSize > 100 {
		input.PageSize = 20
	}

	flows, total, err := s.flowRepo.List(ctx, repository.AIFlowAssetFilter{
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
	s.fillFlowVirtualFields(ctx, flows)
	return flows, total, nil
}

// Get 获取固定场景详情，按 projectID 做归属校验。
func (s *AIFlowAssetService) Get(ctx context.Context, projectID, flowID uint) (*model.AIFlowAsset, error) {
	flow, err := s.flowRepo.GetByID(ctx, flowID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound(CodeNotFound, "固定场景不存在")
		}
		return nil, ErrInternal(CodeInternal, err)
	}
	if flow.ProjectID != projectID {
		return nil, ErrForbidden(CodeForbidden, "固定场景不属于当前项目")
	}
	if project, projectErr := s.projectRepo.FindByID(ctx, flow.ProjectID); projectErr == nil && project != nil {
		flow.ProjectName = project.Name
	}
	if user, userErr := s.userRepo.FindByID(ctx, flow.CreatedBy); userErr == nil && user != nil {
		flow.CreatedName = user.Name
	}
	if flow.SourceTaskID != nil {
		task, taskErr := s.aiScriptRepo.GetTask(ctx, *flow.SourceTaskID)
		if taskErr == nil && task != nil {
			flow.SourceTaskName = task.TaskName
		}
	}
	if flow.CompileHealth == "" {
		s.refreshCompileHealth(ctx, flow)
	} else {
		flow.CompileFailures = decodeCompileFailures(flow.CompileFailuresJSON)
	}
	flow.OutdatedFlowRefs = s.detectOutdatedFlowRefs(ctx, model.AIAssetRefSourceFlow, flow.ID)
	return flow, nil
}

// ListVersions 查询固定场景版本列表。
func (s *AIFlowAssetService) ListVersions(ctx context.Context, projectID, flowID uint) ([]model.AIFlowAssetVersion, error) {
	if _, err := s.Get(ctx, projectID, flowID); err != nil {
		return nil, err
	}
	versions, err := s.flowRepo.ListVersions(ctx, flowID)
	if err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}
	return versions, nil
}

// Create 手动创建固定场景草稿。
func (s *AIFlowAssetService) Create(ctx context.Context, userID uint, input SaveFlowAssetInput) (*model.AIFlowAsset, error) {
	normalized, err := s.normalizeSaveInput(input, true)
	if err != nil {
		return nil, err
	}
	if _, err := s.flowRepo.GetByProjectAndKey(ctx, normalized.ProjectID, normalized.FlowKey); err == nil {
		return nil, ErrConflict(CodeConflict, "flow_key 已存在，请换一个稳定标识")
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrInternal(CodeInternal, err)
	}
	refs, err := s.resolveFlowDSLReferences(ctx, normalized.ProjectID, 0, normalized.FlowKey, model.RawJSON(normalized.DSL))
	if err != nil {
		return nil, err
	}

	flow := &model.AIFlowAsset{
		ProjectID:              normalized.ProjectID,
		FlowKey:                normalized.FlowKey,
		FlowName:               normalized.FlowName,
		Description:            normalized.Description,
		Status:                 model.AIFlowAssetStatusDraft,
		InputSchemaJSON:        model.RawJSON(normalized.InputSchema),
		OutputSchemaJSON:       model.RawJSON(normalized.OutputSchema),
		PreconditionsJSON:      mustRawJSON(normalized.Preconditions),
		PostconditionsJSON:     mustRawJSON(normalized.Postconditions),
		DSLJSON:                model.RawJSON(normalized.DSL),
		CodeSnapshot:           normalized.CodeSnapshot,
		TagsJSON:               mustRawJSON(normalized.Tags),
		AllowAIReuse:           normalized.AllowAIReuse,
		LatestValidationStatus: model.AIValidationStatusNotValidated,
		CreatedBy:              userID,
		UpdatedBy:              userID,
	}
	applyCompileHealth(flow, s.dryRunFlowCompile(ctx, flow.DSLJSON))
	err = s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		if err := s.flowRepo.Create(ctx, tx, flow); err != nil {
			return err
		}
		for i := range refs {
			refs[i].SourceID = flow.ID
		}
		return s.refRepo.ReplaceForSource(ctx, tx, model.AIAssetRefSourceFlow, flow.ID, refs)
	})
	if err != nil {
		s.logger.Error("create flow asset failed", "error", err, "project_id", normalized.ProjectID)
		return nil, ErrInternal(CodeInternal, err)
	}
	return flow, nil
}

// Update 更新固定场景资产草稿或已发布资产的治理信息。
func (s *AIFlowAssetService) Update(ctx context.Context, userID, flowID uint, input SaveFlowAssetInput) (*model.AIFlowAsset, error) {
	flow, err := s.Get(ctx, input.ProjectID, flowID)
	if err != nil {
		return nil, err
	}
	if flow.Status == model.AIFlowAssetStatusArchived {
		return nil, ErrConflict(CodeConflict, "已归档固定场景不可编辑")
	}

	normalized, err := s.normalizeSaveInput(input, false)
	if err != nil {
		return nil, err
	}
	refs, err := s.resolveFlowDSLReferences(ctx, normalized.ProjectID, flow.ID, flow.FlowKey, model.RawJSON(normalized.DSL))
	if err != nil {
		return nil, err
	}
	failures := s.dryRunFlowCompile(ctx, model.RawJSON(normalized.DSL))
	health := model.AIFlowCompileHealthOK
	if len(failures) > 0 {
		health = model.AIFlowCompileHealthPartial
	}
	err = s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		if err := s.flowRepo.UpdateFields(ctx, tx, flowID, map[string]interface{}{
			"flow_name":                normalized.FlowName,
			"description":              normalized.Description,
			"input_schema_json":        string(normalized.InputSchema),
			"output_schema_json":       string(normalized.OutputSchema),
			"preconditions_json":       string(mustRawJSON(normalized.Preconditions)),
			"postconditions_json":      string(mustRawJSON(normalized.Postconditions)),
			"dsl_json":                 string(normalized.DSL),
			"code_snapshot":            normalized.CodeSnapshot,
			"tags_json":                string(mustRawJSON(normalized.Tags)),
			"allow_ai_reuse":           normalized.AllowAIReuse,
			"latest_validation_status": model.AIValidationStatusNotValidated,
			"compile_health":           health,
			"compile_failures_json":    string(encodeCompileFailures(failures)),
			"updated_by":               userID,
		}); err != nil {
			return err
		}
		return s.refRepo.ReplaceForSource(ctx, tx, model.AIAssetRefSourceFlow, flow.ID, refs)
	})
	if err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}
	return s.Get(ctx, normalized.ProjectID, flowID)
}

// Publish 发布固定场景资产并生成不可变版本。
func (s *AIFlowAssetService) Publish(ctx context.Context, userID, projectID, flowID uint, changeSummary string) (*PublishFlowAssetResult, error) {
	flow, err := s.Get(ctx, projectID, flowID)
	if err != nil {
		return nil, err
	}
	if flow.Status == model.AIFlowAssetStatusArchived {
		return nil, ErrConflict(CodeConflict, "已归档固定场景不可发布")
	}
	if len(flow.PreconditionsJSON) == 0 || len(flow.PostconditionsJSON) == 0 {
		return nil, ErrConflict(CodeConflict, "发布前必须确认前置条件和后置条件")
	}
	if len(flow.DSLJSON) == 0 {
		return nil, ErrConflict(CodeConflict, "发布前必须保存固定场景 DSL")
	}
	if !json.Valid(flow.DSLJSON) {
		return nil, ErrConflict(CodeConflict, "固定场景 DSL 不是合法 JSON")
	}
	if failures := s.dryRunFlowCompile(ctx, flow.DSLJSON); len(failures) > 0 {
		return nil, ErrConflictWithData(
			CodeAIFlowCompileFailed,
			fmt.Sprintf("DSL dry-run 编译失败，拒绝发布：%s", summarizeFlowCompileFailures(failures)),
			map[string]interface{}{"compile_failures": failures},
		)
	}
	refs, err := s.resolveFlowDSLReferences(ctx, projectID, flow.ID, flow.FlowKey, flow.DSLJSON)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(changeSummary) == "" {
		changeSummary = "发布固定场景"
	}

	nextNo, err := s.flowRepo.MaxVersionNo(ctx, flow.ID)
	if err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}
	var flowVersion model.AIFlowAssetVersion
	err = s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		if err := s.flowRepo.UpdateFields(ctx, tx, flow.ID, map[string]interface{}{
			"status":                model.AIFlowAssetStatusPublished,
			"compile_health":        model.AIFlowCompileHealthOK,
			"compile_failures_json": string(encodeCompileFailures(nil)),
			"updated_by":            userID,
		}); err != nil {
			return err
		}
		flowVersion = model.AIFlowAssetVersion{
			FlowID:           flow.ID,
			VersionNo:        nextNo + 1,
			VersionStatus:    model.AIFlowAssetStatusPublished,
			DSLJSON:          flow.DSLJSON,
			CodeSnapshot:     flow.CodeSnapshot,
			InputSchemaJSON:  flow.InputSchemaJSON,
			OutputSchemaJSON: flow.OutputSchemaJSON,
			ChangeSummary:    strings.TrimSpace(changeSummary),
			SourceTaskID:     flow.SourceTaskID,
			SourceVersionID:  flow.SourceVersionID,
			ValidationStatus: flow.LatestValidationStatus,
			CreatedBy:        userID,
		}
		if err := s.flowRepo.CreateVersion(ctx, tx, &flowVersion); err != nil {
			return err
		}
		return s.refRepo.ReplaceForSource(ctx, tx, model.AIAssetRefSourceFlow, flow.ID, refs)
	})
	if err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}
	return &PublishFlowAssetResult{FlowID: flow.ID, FlowVersionID: flowVersion.ID, Status: model.AIFlowAssetStatusPublished}, nil
}

// Archive 归档固定场景资产，禁止后续新增引用。
func (s *AIFlowAssetService) Archive(ctx context.Context, userID, projectID, flowID uint) (*model.AIFlowAsset, error) {
	if _, err := s.Get(ctx, projectID, flowID); err != nil {
		return nil, err
	}
	if err := s.flowRepo.UpdateFields(ctx, nil, flowID, map[string]interface{}{
		"status":     model.AIFlowAssetStatusArchived,
		"updated_by": userID,
	}); err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}
	return s.Get(ctx, projectID, flowID)
}

// Delete 删除未发布且未被引用的固定场景草稿。
func (s *AIFlowAssetService) Delete(ctx context.Context, projectID, flowID uint) error {
	flow, err := s.Get(ctx, projectID, flowID)
	if err != nil {
		return err
	}
	if flow.Status != model.AIFlowAssetStatusDraft {
		return ErrConflict(CodeConflict, "仅草稿固定场景允许删除，已发布资产请使用归档")
	}
	refCount, err := s.refRepo.CountByTarget(ctx, model.AIAssetRefTargetFlow, flowID)
	if err != nil {
		return ErrInternal(CodeInternal, err)
	}
	if refCount > 0 {
		return ErrConflict(CodeConflict, "固定场景已被引用，不能删除")
	}
	if err := s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		if err := s.refRepo.ReplaceForSource(ctx, tx, model.AIAssetRefSourceFlow, flowID, nil); err != nil {
			return err
		}
		return s.flowRepo.Delete(ctx, tx, flowID)
	}); err != nil {
		return ErrInternal(CodeInternal, err)
	}
	return nil
}

// References 查询引用该固定场景的编排或其他固定场景。
func (s *AIFlowAssetService) References(ctx context.Context, projectID, flowID uint) ([]model.AIAssetReference, error) {
	if _, err := s.Get(ctx, projectID, flowID); err != nil {
		return nil, err
	}
	refs, err := expandAssetImpactReferences(ctx, s.refRepo, model.AIAssetRefTargetFlow, flowID, 3)
	if err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}
	return refs, nil
}

// PublishFromTask 从已验证通过的测试智编任务发布固定场景。
func (s *AIFlowAssetService) PublishFromTask(ctx context.Context, userID, taskID uint, input PublishFlowAssetInput) (*PublishFlowAssetResult, error) {
	normalized, err := s.normalizePublishInput(input)
	if err != nil {
		return nil, err
	}

	task, err := s.aiScriptRepo.GetTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound(CodeNotFound, "测试智编任务不存在")
		}
		return nil, ErrInternal(CodeInternal, err)
	}
	if task.ProjectID != normalized.ProjectID {
		return nil, ErrForbidden(CodeForbidden, "任务不属于当前项目")
	}

	version, err := s.aiScriptRepo.GetCurrentScriptVersion(ctx, taskID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrConflict(CodeConflict, "任务暂无当前脚本版本，不能发布固定场景")
		}
		return nil, ErrInternal(CodeInternal, err)
	}
	if version.ValidationStatus != model.AIValidationStatusPassed {
		return nil, ErrConflict(CodeConflict, "当前脚本尚未验证通过，不能发布固定场景")
	}

	if _, err := s.flowRepo.GetByProjectAndKey(ctx, normalized.ProjectID, normalized.FlowKey); err == nil {
		return nil, ErrConflict(CodeConflict, "flow_key 已存在，请换一个稳定标识")
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrInternal(CodeInternal, err)
	}

	traces, err := s.aiScriptRepo.ListTraces(ctx, task.ID)
	if err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}

	sourceTaskID := task.ID
	sourceVersionID := version.ID
	dsl, err := buildFlowDSL(task, version, traces)
	if err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}
	if failures := s.dryRunFlowCompile(ctx, dsl); len(failures) > 0 {
		return nil, ErrConflictWithData(
			CodeAIFlowCompileFailed,
			fmt.Sprintf("DSL dry-run 编译失败，拒绝发布：%s", summarizeFlowCompileFailures(failures)),
			map[string]interface{}{"compile_failures": failures},
		)
	}

	flow := &model.AIFlowAsset{
		ProjectID:              normalized.ProjectID,
		FlowKey:                normalized.FlowKey,
		FlowName:               normalized.FlowName,
		Description:            normalized.Description,
		SourceTaskID:           &sourceTaskID,
		SourceVersionID:        &sourceVersionID,
		Status:                 model.AIFlowAssetStatusPublished,
		InputSchemaJSON:        model.RawJSON(normalized.InputSchema),
		OutputSchemaJSON:       model.RawJSON(normalized.OutputSchema),
		PreconditionsJSON:      mustRawJSON(normalized.Preconditions),
		PostconditionsJSON:     mustRawJSON(normalized.Postconditions),
		DSLJSON:                dsl,
		CodeSnapshot:           version.ScriptContent,
		TagsJSON:               mustRawJSON(normalized.Tags),
		AllowAIReuse:           normalized.AllowAIReuse,
		LatestValidationStatus: version.ValidationStatus,
		CreatedBy:              userID,
		UpdatedBy:              userID,
	}
	applyCompileHealth(flow, nil)
	var flowVersion model.AIFlowAssetVersion
	refs, err := s.resolveFlowDSLReferences(ctx, normalized.ProjectID, 0, normalized.FlowKey, dsl)
	if err != nil {
		return nil, err
	}

	err = s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		if err := s.flowRepo.Create(ctx, tx, flow); err != nil {
			return err
		}
		flowVersion = model.AIFlowAssetVersion{
			FlowID:           flow.ID,
			VersionNo:        1,
			VersionStatus:    model.AIFlowAssetStatusPublished,
			DSLJSON:          dsl,
			CodeSnapshot:     version.ScriptContent,
			InputSchemaJSON:  model.RawJSON(normalized.InputSchema),
			OutputSchemaJSON: model.RawJSON(normalized.OutputSchema),
			ChangeSummary:    normalized.ChangeSummary,
			SourceTaskID:     &sourceTaskID,
			SourceVersionID:  &sourceVersionID,
			ValidationStatus: version.ValidationStatus,
			CreatedBy:        userID,
		}
		if err := s.flowRepo.CreateVersion(ctx, tx, &flowVersion); err != nil {
			return err
		}
		for i := range refs {
			refs[i].SourceID = flow.ID
		}
		return s.refRepo.ReplaceForSource(ctx, tx, model.AIAssetRefSourceFlow, flow.ID, refs)
	})
	if err != nil {
		s.logger.Error("publish flow asset failed",
			"error", err,
			"project_id", normalized.ProjectID,
			"task_id", taskID,
			"flow_key", normalized.FlowKey,
		)
		return nil, ErrInternal(CodeInternal, err)
	}

	return &PublishFlowAssetResult{
		FlowID:        flow.ID,
		FlowVersionID: flowVersion.ID,
		Status:        flow.Status,
	}, nil
}

func (s *AIFlowAssetService) normalizeSaveInput(input SaveFlowAssetInput, requireKey bool) (SaveFlowAssetInput, error) {
	input.FlowKey = strings.TrimSpace(input.FlowKey)
	input.FlowName = strings.TrimSpace(input.FlowName)
	input.Description = strings.TrimSpace(input.Description)
	input.ChangeSummary = strings.TrimSpace(input.ChangeSummary)
	input.CodeSnapshot = strings.TrimSpace(input.CodeSnapshot)
	input.Tags = normalizeStringSlice(input.Tags)
	input.Preconditions = normalizeStringSlice(input.Preconditions)
	input.Postconditions = normalizeStringSlice(input.Postconditions)

	if input.ProjectID == 0 {
		return input, ErrBadRequest(CodeParamsError, "project_id 不能为空")
	}
	if requireKey && !flowKeyPattern.MatchString(input.FlowKey) {
		return input, ErrBadRequest(CodeParamsError, "flow_key 仅支持小写字母、数字和下划线，且必须以小写字母开头")
	}
	if input.FlowName == "" {
		return input, ErrBadRequest(CodeParamsError, "场景名称不能为空")
	}
	if len(input.FlowName) > 128 {
		return input, ErrBadRequest(CodeParamsError, "场景名称不能超过 128 个字符")
	}
	if len(input.Preconditions) == 0 {
		return input, ErrBadRequest(CodeParamsError, "前置条件至少填写一条")
	}
	if len(input.Postconditions) == 0 {
		return input, ErrBadRequest(CodeParamsError, "后置条件至少填写一条")
	}
	if len(input.InputSchema) == 0 {
		input.InputSchema = json.RawMessage(`{}`)
	} else if !json.Valid(input.InputSchema) {
		return input, ErrBadRequest(CodeParamsError, "入参 Schema 不是合法 JSON")
	}
	if len(input.OutputSchema) == 0 {
		input.OutputSchema = json.RawMessage(`{}`)
	} else if !json.Valid(input.OutputSchema) {
		return input, ErrBadRequest(CodeParamsError, "出参 Schema 不是合法 JSON")
	}
	if len(input.DSL) == 0 {
		input.DSL = json.RawMessage(`{"schema_version":"1.0","steps":[]}`)
	} else if !json.Valid(input.DSL) {
		return input, ErrBadRequest(CodeParamsError, "DSL 不是合法 JSON")
	}
	return input, nil
}

func (s *AIFlowAssetService) normalizePublishInput(input PublishFlowAssetInput) (PublishFlowAssetInput, error) {
	input.FlowKey = strings.TrimSpace(input.FlowKey)
	input.FlowName = strings.TrimSpace(input.FlowName)
	input.Description = strings.TrimSpace(input.Description)
	input.ChangeSummary = strings.TrimSpace(input.ChangeSummary)
	input.Tags = normalizeStringSlice(input.Tags)
	input.Preconditions = normalizeStringSlice(input.Preconditions)
	input.Postconditions = normalizeStringSlice(input.Postconditions)

	if input.ProjectID == 0 {
		return input, ErrBadRequest(CodeParamsError, "project_id 不能为空")
	}
	if !flowKeyPattern.MatchString(input.FlowKey) {
		return input, ErrBadRequest(CodeParamsError, "flow_key 仅支持小写字母、数字和下划线，且必须以小写字母开头")
	}
	if input.FlowName == "" {
		return input, ErrBadRequest(CodeParamsError, "场景名称不能为空")
	}
	if len(input.FlowName) > 128 {
		return input, ErrBadRequest(CodeParamsError, "场景名称不能超过 128 个字符")
	}
	if len(input.Preconditions) == 0 {
		return input, ErrBadRequest(CodeParamsError, "前置条件至少填写一条")
	}
	if len(input.Postconditions) == 0 {
		return input, ErrBadRequest(CodeParamsError, "后置条件至少填写一条")
	}
	if len(input.InputSchema) == 0 {
		input.InputSchema = json.RawMessage(`{}`)
	} else if !json.Valid(input.InputSchema) {
		return input, ErrBadRequest(CodeParamsError, "入参 Schema 不是合法 JSON")
	}
	if len(input.OutputSchema) == 0 {
		input.OutputSchema = json.RawMessage(`{}`)
	} else if !json.Valid(input.OutputSchema) {
		return input, ErrBadRequest(CodeParamsError, "出参 Schema 不是合法 JSON")
	}
	if input.ChangeSummary == "" {
		input.ChangeSummary = "首次发布"
	}
	return input, nil
}

type flowDSLReference struct {
	FlowID        uint
	FlowKey       string
	FlowVersionID uint
}

func (s *AIFlowAssetService) resolveFlowDSLReferences(ctx context.Context, projectID, sourceFlowID uint, sourceFlowKey string, dsl model.RawJSON) ([]model.AIAssetReference, error) {
	rawRefs, err := extractFlowDSLReferences(dsl)
	if err != nil {
		return nil, err
	}
	refs := make([]model.AIAssetReference, 0, len(rawRefs))
	targetIDs := make([]uint, 0, len(rawRefs))
	seenTargets := map[uint]struct{}{}
	for _, rawRef := range rawRefs {
		flow, err := s.resolveFlowReferenceTarget(ctx, projectID, sourceFlowID, sourceFlowKey, rawRef)
		if err != nil {
			return nil, err
		}
		versionID := rawRef.FlowVersionID
		if versionID == 0 {
			version, err := s.flowRepo.GetLatestPublishedVersion(ctx, flow.ID)
			if err != nil {
				return nil, ErrConflict(CodeConflict, fmt.Sprintf("固定场景 %s 暂无可引用的发布版本", flow.FlowKey))
			}
			versionID = version.ID
		} else {
			version, err := s.flowRepo.GetVersionByID(ctx, versionID)
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil, ErrNotFound(CodeNotFound, "引用的固定场景版本不存在")
				}
				return nil, ErrInternal(CodeInternal, err)
			}
			if version.FlowID != flow.ID {
				return nil, ErrForbidden(CodeForbidden, "固定场景 DSL 引用的版本不属于目标固定场景")
			}
			if version.VersionStatus != model.AIFlowAssetStatusPublished {
				return nil, ErrConflict(CodeConflict, "固定场景 DSL 只能引用已发布版本")
			}
		}
		if _, ok := seenTargets[flow.ID]; ok {
			continue
		}
		seenTargets[flow.ID] = struct{}{}
		targetIDs = append(targetIDs, flow.ID)
		refs = append(refs, model.AIAssetReference{
			SourceType:      model.AIAssetRefSourceFlow,
			SourceID:        sourceFlowID,
			TargetType:      model.AIAssetRefTargetFlow,
			TargetID:        flow.ID,
			TargetVersionID: &versionID,
		})
	}
	if err := s.validateFlowReferenceGraph(ctx, sourceFlowID, targetIDs); err != nil {
		return nil, err
	}
	return refs, nil
}

func (s *AIFlowAssetService) resolveFlowReferenceTarget(ctx context.Context, projectID, sourceFlowID uint, sourceFlowKey string, rawRef flowDSLReference) (*model.AIFlowAsset, error) {
	var flow *model.AIFlowAsset
	var err error
	switch {
	case rawRef.FlowID > 0:
		flow, err = s.flowRepo.GetByID(ctx, rawRef.FlowID)
	case rawRef.FlowKey != "":
		if rawRef.FlowKey == sourceFlowKey {
			return nil, ErrConflict(CodeConflict, "固定场景 DSL 不能引用自身")
		}
		flow, err = s.flowRepo.GetByProjectAndKey(ctx, projectID, rawRef.FlowKey)
	default:
		return nil, ErrBadRequest(CodeParamsError, "FLOW_CALL 步骤必须提供 flow_id 或 flow_key")
	}
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound(CodeNotFound, "引用的固定场景不存在")
		}
		return nil, ErrInternal(CodeInternal, err)
	}
	if sourceFlowID > 0 && flow.ID == sourceFlowID {
		return nil, ErrConflict(CodeConflict, "固定场景 DSL 不能引用自身")
	}
	if flow.ProjectID != projectID {
		return nil, ErrForbidden(CodeForbidden, "固定场景 DSL 引用的固定场景不属于当前项目")
	}
	if flow.Status != model.AIFlowAssetStatusPublished {
		return nil, ErrConflict(CodeConflict, "固定场景 DSL 只能引用已发布固定场景")
	}
	if isValidationUnusableForAI(flow.LatestValidationStatus) {
		return nil, ErrConflict(CodeConflict, "固定场景 DSL 不能引用最近验证失败的固定场景")
	}
	return flow, nil
}

func (s *AIFlowAssetService) validateFlowReferenceGraph(ctx context.Context, sourceFlowID uint, targetIDs []uint) error {
	for _, targetID := range targetIDs {
		if sourceFlowID > 0 && targetID == sourceFlowID {
			return ErrConflict(CodeConflict, "固定场景引用存在循环依赖")
		}
		if err := s.walkFlowReferenceGraph(ctx, sourceFlowID, targetID, 1, map[uint]struct{}{}); err != nil {
			return err
		}
	}
	return nil
}

func (s *AIFlowAssetService) walkFlowReferenceGraph(ctx context.Context, sourceFlowID, currentFlowID uint, depth int, visited map[uint]struct{}) error {
	if sourceFlowID > 0 && currentFlowID == sourceFlowID {
		return ErrConflict(CodeConflict, "固定场景引用存在循环依赖")
	}
	if depth > maxFlowReferenceDepth {
		return ErrConflict(CodeConflict, "固定场景引用深度不能超过 3 层")
	}
	if _, ok := visited[currentFlowID]; ok {
		return nil
	}
	visited[currentFlowID] = struct{}{}
	refs, err := s.refRepo.ListBySource(ctx, model.AIAssetRefSourceFlow, currentFlowID)
	if err != nil {
		return ErrInternal(CodeInternal, err)
	}
	for _, ref := range refs {
		if ref.TargetType != model.AIAssetRefTargetFlow {
			continue
		}
		if err := s.walkFlowReferenceGraph(ctx, sourceFlowID, ref.TargetID, depth+1, visited); err != nil {
			return err
		}
	}
	return nil
}

func extractFlowDSLReferences(data model.RawJSON) ([]flowDSLReference, error) {
	if len(data) == 0 {
		return nil, nil
	}
	if !json.Valid(data) {
		return nil, ErrBadRequest(CodeParamsError, "固定场景 DSL 不是合法 JSON")
	}
	root := map[string]interface{}{}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&root); err != nil {
		return nil, ErrBadRequest(CodeParamsError, "固定场景 DSL 不是合法对象")
	}
	steps := dslObjectSlice(root["steps"])
	if len(steps) == 0 {
		steps = dslObjectSlice(root["generation_steps"])
	}
	refs := make([]flowDSLReference, 0)
	for _, step := range steps {
		stepType := strings.ToUpper(firstNonEmptyString(step, "type", "step_type", "stepType"))
		if stepType != model.AIScenarioStepTypeFlowCall {
			continue
		}
		refObject := objectFromAny(step["ref"])
		flowID, _ := firstUintFromObjects([]map[string]interface{}{refObject, step}, "flow_id", "ref_flow_id", "flowId", "refFlowId")
		versionID, _ := firstUintFromObjects([]map[string]interface{}{refObject, step}, "flow_version_id", "ref_flow_version_id", "flowVersionId", "refFlowVersionId")
		flowKey := firstNonEmptyStringFromObjects([]map[string]interface{}{refObject, step}, "flow_key", "ref_flow_key", "flowKey", "refFlowKey")
		refs = append(refs, flowDSLReference{FlowID: flowID, FlowKey: flowKey, FlowVersionID: versionID})
	}
	return refs, nil
}

func firstUintFromObjects(objects []map[string]interface{}, keys ...string) (uint, bool) {
	for _, object := range objects {
		for _, key := range keys {
			if value, ok := lookupMapPath(object, key); ok {
				if id, ok := uintFromAny(value); ok && id > 0 {
					return id, true
				}
			}
		}
	}
	return 0, false
}

func uintFromAny(value interface{}) (uint, bool) {
	if id, ok := uintFromDSLValue(value); ok {
		return id, true
	}
	switch typed := value.(type) {
	case uint:
		return typed, true
	case int:
		if typed < 0 {
			return 0, false
		}
		return uint(typed), true
	case int64:
		if typed < 0 {
			return 0, false
		}
		return uint(typed), true
	default:
		return 0, false
	}
}

func (s *AIFlowAssetService) fillFlowVirtualFields(ctx context.Context, flows []model.AIFlowAsset) {
	if len(flows) == 0 {
		return
	}
	projectIDs := make([]uint, 0, len(flows))
	userIDs := make([]uint, 0, len(flows))
	for _, flow := range flows {
		projectIDs = append(projectIDs, flow.ProjectID)
		userIDs = append(userIDs, flow.CreatedBy)
	}
	projectMap := s.batchProjectNames(ctx, deduplicateUints(projectIDs))
	userMap := s.batchUserNames(ctx, deduplicateUints(userIDs))
	for i := range flows {
		flows[i].ProjectName = projectMap[flows[i].ProjectID]
		flows[i].CreatedName = userMap[flows[i].CreatedBy]
		flows[i].CompileFailures = decodeCompileFailures(flows[i].CompileFailuresJSON)
	}
}

// detectOutdatedFlowRefs 检查引用方锁定的固定场景版本是否落后于目标最新发布版本，返回已失效的引用明细。
func (s *AIFlowAssetService) detectOutdatedFlowRefs(ctx context.Context, sourceType string, sourceID uint) []model.AIAssetReference {
	return detectOutdatedFlowRefs(ctx, s.refRepo, s.flowRepo, sourceType, sourceID)
}

func detectOutdatedFlowRefs(
	ctx context.Context,
	refRepo *repository.AIAssetReferenceRepo,
	flowRepo *repository.AIFlowAssetRepo,
	sourceType string,
	sourceID uint,
) []model.AIAssetReference {
	refs, err := refRepo.ListBySource(ctx, sourceType, sourceID)
	if err != nil {
		return nil
	}
	outdated := []model.AIAssetReference{}
	for _, ref := range refs {
		if ref.TargetType != model.AIAssetRefTargetFlow || ref.TargetVersionID == nil {
			continue
		}
		latest, err := flowRepo.GetLatestPublishedVersion(ctx, ref.TargetID)
		if err != nil || latest.ID == *ref.TargetVersionID {
			continue
		}
		locked, err := flowRepo.GetVersionByID(ctx, *ref.TargetVersionID)
		if err != nil || latest.VersionNo <= locked.VersionNo {
			continue
		}
		ref.RefOutdated = true
		ref.LockedVersionNo = locked.VersionNo
		latestID := latest.ID
		ref.LatestVersionID = &latestID
		ref.LatestVersionNo = latest.VersionNo
		if target, targetErr := flowRepo.GetByID(ctx, ref.TargetID); targetErr == nil {
			ref.TargetName = target.FlowName
		}
		outdated = append(outdated, ref)
	}
	if len(outdated) == 0 {
		return nil
	}
	return outdated
}

func (s *AIFlowAssetService) batchProjectNames(ctx context.Context, ids []uint) map[uint]string {
	result := make(map[uint]string, len(ids))
	projects, err := s.projectRepo.FindByIDs(ctx, ids)
	if err != nil {
		s.logger.Error("batch project names failed", "error", err)
		return result
	}
	for _, project := range projects {
		result[project.ID] = project.Name
	}
	return result
}

func (s *AIFlowAssetService) batchUserNames(ctx context.Context, ids []uint) map[uint]string {
	result := make(map[uint]string, len(ids))
	users, err := s.userRepo.FindByIDs(ctx, ids)
	if err != nil {
		s.logger.Error("batch user names failed", "error", err)
		return result
	}
	for _, user := range users {
		result[user.ID] = user.Name
	}
	return result
}

func buildFlowDSL(task *model.AIScriptTask, version *model.AIScriptVersion, traces []model.AIScriptTrace) (model.RawJSON, error) {
	steps := buildFlowGenerationSteps(version.StepModelJSON, traces)
	payload := map[string]interface{}{
		"schema_version": "1.0",
		"flow": map[string]interface{}{
			"project_id": task.ProjectID,
			"task_id":    task.ID,
			"version_id": version.ID,
			"name":       task.TaskName,
		},
		"source": map[string]interface{}{
			"type":       "AI_SCRIPT_TASK",
			"task_id":    task.ID,
			"version_id": version.ID,
		},
		"generation_steps": steps,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return model.RawJSON(data), nil
}

func buildFlowGenerationSteps(stepModel model.JSONMap, traces []model.AIScriptTrace) []map[string]interface{} {
	if steps := generationStepsFromStepModel(stepModel); len(steps) > 0 {
		return steps
	}
	return generationStepsFromTraces(traces)
}

func generationStepsFromStepModel(stepModel model.JSONMap) []map[string]interface{} {
	if len(stepModel) == 0 {
		return nil
	}
	for _, key := range []string{"generation_steps", "steps"} {
		rawSteps, ok := stepModel[key]
		if !ok {
			continue
		}
		items, ok := rawSteps.([]interface{})
		if !ok {
			continue
		}
		steps := make([]map[string]interface{}, 0, len(items))
		for _, item := range items {
			step, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			normalized := normalizeFlowGenerationStep(step)
			if len(normalized) > 0 {
				steps = append(steps, normalized)
			}
		}
		if len(steps) > 0 {
			return steps
		}
	}
	return nil
}

func generationStepsFromTraces(traces []model.AIScriptTrace) []map[string]interface{} {
	steps := make([]map[string]interface{}, 0, len(traces))
	for _, trace := range traces {
		step := map[string]interface{}{
			"action_type": strings.ToUpper(strings.TrimSpace(trace.ActionType)),
		}
		if trace.PageURL != "" {
			step["page_url"] = trace.PageURL
		}
		if trace.LocatorUsed != "" {
			step["locator"] = trace.LocatorUsed
		}
		if trace.InputValueMasked != "" {
			step["input_value"] = trace.InputValueMasked
		}
		normalized := normalizeFlowGenerationStep(step)
		if len(normalized) > 0 {
			steps = append(steps, normalized)
		}
	}
	return steps
}

func normalizeFlowGenerationStep(step map[string]interface{}) map[string]interface{} {
	actionType := strings.ToUpper(firstNonEmptyStringFromObjects([]map[string]interface{}{step}, "action_type", "actionType", "type", "step_type"))
	if actionType == "" {
		return nil
	}
	normalized := map[string]interface{}{
		"action_type": actionType,
	}
	switch actionType {
	case "NAVIGATE", "GOTO":
		if url := firstNonEmptyStringFromObjects([]map[string]interface{}{step}, "page_url", "pageUrl", "url"); url != "" {
			normalized["page_url"] = url
		}
	case "CLICK":
		if locator := firstNonEmptyStringFromObjects([]map[string]interface{}{step}, "locator", "locator_used", "locatorUsed", "selector"); locator != "" {
			normalized["locator"] = locator
		}
	case "INPUT", "FILL":
		if locator := firstNonEmptyStringFromObjects([]map[string]interface{}{step}, "locator", "locator_used", "locatorUsed", "selector"); locator != "" {
			normalized["locator"] = locator
		}
		if value := firstNonEmptyStringFromObjects([]map[string]interface{}{step}, "input_value", "inputValue", "value"); value != "" {
			normalized["input_value"] = value
		}
	case "KEY_PRESS":
		if locator := firstNonEmptyStringFromObjects([]map[string]interface{}{step}, "locator", "locator_used", "locatorUsed", "selector"); locator != "" {
			normalized["locator"] = locator
		}
		if value := firstNonEmptyStringFromObjects([]map[string]interface{}{step}, "input_value", "inputValue", "value", "key"); value != "" {
			normalized["input_value"] = value
		}
	case "WAIT":
		if timeout := firstNonEmptyStringFromObjects([]map[string]interface{}{step}, "timeout_ms", "timeoutMs", "timeout"); timeout != "" {
			normalized["timeout_ms"] = timeout
		}
		if value := firstNonEmptyStringFromObjects([]map[string]interface{}{step}, "value", "state"); value != "" {
			normalized["value"] = value
		}
	default:
		if locator := firstNonEmptyStringFromObjects([]map[string]interface{}{step}, "locator", "locator_used", "locatorUsed", "selector"); locator != "" {
			normalized["locator"] = locator
		}
		if value := firstNonEmptyStringFromObjects([]map[string]interface{}{step}, "input_value", "inputValue", "value"); value != "" {
			normalized["input_value"] = value
		}
	}
	return normalized
}

func mustRawJSON(value interface{}) model.RawJSON {
	data, err := json.Marshal(value)
	if err != nil {
		return model.RawJSON(`null`)
	}
	return model.RawJSON(data)
}

func normalizeStringSlice(items []string) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		result = append(result, trimmed)
	}
	return result
}

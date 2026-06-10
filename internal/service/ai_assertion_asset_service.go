// ai_assertion_asset_service.go — 测试智编断言资产业务逻辑
package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"

	"gorm.io/gorm"

	"testpilot/internal/model"
	"testpilot/internal/repository"
)

// AIAssertionAssetService 管理测试智编断言资产的创建、发布、归档和查询。
type AIAssertionAssetService struct {
	logger        *slog.Logger
	assertionRepo *repository.AIAssertionAssetRepo
	refRepo       *repository.AIAssetReferenceRepo
	projectRepo   repository.ProjectRepository
	userRepo      repository.UserRepository
	txMgr         *repository.TxManager
}

// NewAIAssertionAssetService 创建断言资产服务。
func NewAIAssertionAssetService(
	logger *slog.Logger,
	assertionRepo *repository.AIAssertionAssetRepo,
	refRepo *repository.AIAssetReferenceRepo,
	projectRepo repository.ProjectRepository,
	userRepo repository.UserRepository,
	txMgr *repository.TxManager,
) *AIAssertionAssetService {
	return &AIAssertionAssetService{
		logger:        logger.With("module", "ai_assertion_asset"),
		assertionRepo: assertionRepo,
		refRepo:       refRepo,
		projectRepo:   projectRepo,
		userRepo:      userRepo,
		txMgr:         txMgr,
	}
}

// AssertionAssetListInput 表示断言资产列表查询输入。
type AssertionAssetListInput struct {
	ProjectID uint
	Keyword   string
	Status    string
	Type      string
	Page      int
	PageSize  int
}

// AssertionAssetSaveInput 表示创建或更新断言资产的输入。
type AssertionAssetSaveInput struct {
	ProjectID          uint
	AssertionKey       string
	AssertionName      string
	AssertionType      string
	Description        string
	ParamSchema        json.RawMessage
	Implementation     json.RawMessage
	FailureMessageTpl  string
	EvidenceConfig     json.RawMessage
	AllowAIReuse       bool
	ValidationRequired bool
}

// List 分页查询断言资产。
func (s *AIAssertionAssetService) List(ctx context.Context, input AssertionAssetListInput) ([]model.AIAssertionAsset, int64, error) {
	if input.ProjectID == 0 {
		return nil, 0, ErrBadRequest(CodeParamsError, "project_id 不能为空")
	}
	if input.Page < 1 {
		input.Page = 1
	}
	if input.PageSize < 1 || input.PageSize > 100 {
		input.PageSize = 20
	}

	assertions, total, err := s.assertionRepo.List(ctx, repository.AIAssertionAssetFilter{
		ProjectID: input.ProjectID,
		Keyword:   strings.TrimSpace(input.Keyword),
		Status:    strings.TrimSpace(input.Status),
		Type:      strings.TrimSpace(input.Type),
		Page:      input.Page,
		PageSize:  input.PageSize,
	})
	if err != nil {
		return nil, 0, ErrInternal(CodeInternal, err)
	}
	s.fillAssertionVirtualFields(ctx, assertions)
	return assertions, total, nil
}

// Get 获取断言资产详情，并校验项目归属。
func (s *AIAssertionAssetService) Get(ctx context.Context, projectID, assertionID uint) (*model.AIAssertionAsset, error) {
	assertion, err := s.assertionRepo.GetByID(ctx, assertionID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound(CodeNotFound, "断言资产不存在")
		}
		return nil, ErrInternal(CodeInternal, err)
	}
	if assertion.ProjectID != projectID {
		return nil, ErrForbidden(CodeForbidden, "断言资产不属于当前项目")
	}
	s.fillAssertionVirtualField(ctx, assertion)
	return assertion, nil
}

// Create 创建断言资产草稿。
func (s *AIAssertionAssetService) Create(ctx context.Context, userID uint, input AssertionAssetSaveInput) (*model.AIAssertionAsset, error) {
	normalized, err := s.normalizeAssertionInput(input, true)
	if err != nil {
		return nil, err
	}
	if _, err := s.assertionRepo.GetByProjectAndKey(ctx, normalized.ProjectID, normalized.AssertionKey); err == nil {
		return nil, ErrConflict(CodeConflict, "assertion_key 已存在，请换一个稳定标识")
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrInternal(CodeInternal, err)
	}

	assertion := &model.AIAssertionAsset{
		ProjectID:              normalized.ProjectID,
		AssertionKey:           normalized.AssertionKey,
		AssertionName:          normalized.AssertionName,
		AssertionType:          normalized.AssertionType,
		Description:            normalized.Description,
		ParamSchemaJSON:        model.RawJSON(normalized.ParamSchema),
		ImplementationJSON:     model.RawJSON(normalized.Implementation),
		FailureMessageTpl:      normalized.FailureMessageTpl,
		EvidenceConfigJSON:     model.RawJSON(normalized.EvidenceConfig),
		Status:                 model.AIAssertionAssetStatusDraft,
		AllowAIReuse:           normalized.AllowAIReuse,
		LatestValidationStatus: model.AIValidationStatusNotValidated,
		CreatedBy:              userID,
		UpdatedBy:              userID,
	}
	if err := s.assertionRepo.Create(ctx, nil, assertion); err != nil {
		s.logger.Error("create assertion asset failed", "error", err, "project_id", normalized.ProjectID)
		return nil, ErrInternal(CodeInternal, err)
	}
	return assertion, nil
}

// Update 更新断言资产基础信息。
func (s *AIAssertionAssetService) Update(ctx context.Context, userID, assertionID uint, input AssertionAssetSaveInput) (*model.AIAssertionAsset, error) {
	existing, err := s.Get(ctx, input.ProjectID, assertionID)
	if err != nil {
		return nil, err
	}
	if existing.Status == model.AIAssertionAssetStatusArchived {
		return nil, ErrConflict(CodeConflict, "已归档断言不可编辑")
	}

	normalized, err := s.normalizeAssertionInput(input, false)
	if err != nil {
		return nil, err
	}

	fields := map[string]interface{}{
		"assertion_name":       normalized.AssertionName,
		"assertion_type":       normalized.AssertionType,
		"description":          normalized.Description,
		"param_schema_json":    string(normalized.ParamSchema),
		"implementation_json":  string(normalized.Implementation),
		"failure_message_tpl":  normalized.FailureMessageTpl,
		"evidence_config_json": string(normalized.EvidenceConfig),
		"allow_ai_reuse":       normalized.AllowAIReuse,
		"updated_by":           userID,
	}
	if err := s.assertionRepo.UpdateFields(ctx, nil, assertionID, fields); err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}
	return s.Get(ctx, normalized.ProjectID, assertionID)
}

// Publish 发布断言资产，使其可被编排引用和 AI 推荐。
func (s *AIAssertionAssetService) Publish(ctx context.Context, userID, projectID, assertionID uint) (*model.AIAssertionAsset, error) {
	assertion, err := s.Get(ctx, projectID, assertionID)
	if err != nil {
		return nil, err
	}
	if assertion.Status == model.AIAssertionAssetStatusArchived {
		return nil, ErrConflict(CodeConflict, "已归档断言不可发布")
	}
	if len(assertion.ParamSchemaJSON) == 0 || len(assertion.ImplementationJSON) == 0 {
		return nil, ErrConflict(CodeConflict, "断言参数 Schema 和实现模板不能为空")
	}
	if err := s.assertionRepo.UpdateFields(ctx, nil, assertionID, map[string]interface{}{
		"status":                   model.AIAssertionAssetStatusPublished,
		"latest_validation_status": model.AIValidationStatusPassed,
		"updated_by":               userID,
	}); err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}
	return s.Get(ctx, projectID, assertionID)
}

// Archive 归档断言资产；历史编排仍保留锁定引用。
func (s *AIAssertionAssetService) Archive(ctx context.Context, userID, projectID, assertionID uint) (*model.AIAssertionAsset, error) {
	if _, err := s.Get(ctx, projectID, assertionID); err != nil {
		return nil, err
	}
	if err := s.assertionRepo.UpdateFields(ctx, nil, assertionID, map[string]interface{}{
		"status":     model.AIAssertionAssetStatusArchived,
		"updated_by": userID,
	}); err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}
	return s.Get(ctx, projectID, assertionID)
}

// Delete 删除未发布且未被引用的断言草稿。
func (s *AIAssertionAssetService) Delete(ctx context.Context, projectID, assertionID uint) error {
	assertion, err := s.Get(ctx, projectID, assertionID)
	if err != nil {
		return err
	}
	if assertion.Status != model.AIAssertionAssetStatusDraft {
		return ErrConflict(CodeConflict, "仅草稿断言允许删除，已发布资产请使用归档")
	}
	refCount, err := s.refRepo.CountByTarget(ctx, model.AIAssetRefTargetAssertion, assertionID)
	if err != nil {
		return ErrInternal(CodeInternal, err)
	}
	if refCount > 0 {
		return ErrConflict(CodeConflict, "断言资产已被引用，不能删除")
	}
	if err := s.assertionRepo.Delete(ctx, nil, assertionID); err != nil {
		return ErrInternal(CodeInternal, err)
	}
	return nil
}

// References 查询引用该断言资产的编排场景。
func (s *AIAssertionAssetService) References(ctx context.Context, projectID, assertionID uint) ([]model.AIAssetReference, error) {
	if _, err := s.Get(ctx, projectID, assertionID); err != nil {
		return nil, err
	}
	refs, err := expandAssetImpactReferences(ctx, s.refRepo, model.AIAssetRefTargetAssertion, assertionID, 3)
	if err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}
	return refs, nil
}

func (s *AIAssertionAssetService) normalizeAssertionInput(input AssertionAssetSaveInput, requireKey bool) (AssertionAssetSaveInput, error) {
	input.AssertionKey = strings.TrimSpace(input.AssertionKey)
	input.AssertionName = strings.TrimSpace(input.AssertionName)
	input.AssertionType = strings.TrimSpace(input.AssertionType)
	input.Description = strings.TrimSpace(input.Description)
	input.FailureMessageTpl = strings.TrimSpace(input.FailureMessageTpl)

	if input.ProjectID == 0 {
		return input, ErrBadRequest(CodeParamsError, "project_id 不能为空")
	}
	if requireKey && !flowKeyPattern.MatchString(input.AssertionKey) {
		return input, ErrBadRequest(CodeParamsError, "assertion_key 仅支持小写字母、数字和下划线，且必须以小写字母开头")
	}
	if input.AssertionName == "" {
		return input, ErrBadRequest(CodeParamsError, "断言名称不能为空")
	}
	if len(input.AssertionName) > 128 {
		return input, ErrBadRequest(CodeParamsError, "断言名称不能超过 128 个字符")
	}
	if !isValidAssertionType(input.AssertionType) {
		return input, ErrBadRequest(CodeParamsError, "断言类型无效")
	}
	if len(input.ParamSchema) == 0 {
		input.ParamSchema = json.RawMessage(`{}`)
	} else if !json.Valid(input.ParamSchema) {
		return input, ErrBadRequest(CodeParamsError, "参数 Schema 不是合法 JSON")
	}
	if len(input.Implementation) == 0 {
		input.Implementation = json.RawMessage(`{}`)
	} else if !json.Valid(input.Implementation) {
		return input, ErrBadRequest(CodeParamsError, "实现模板不是合法 JSON")
	}
	if len(input.EvidenceConfig) == 0 {
		input.EvidenceConfig = json.RawMessage(`{"screenshot":"ON_FAILURE","trace":true}`)
	} else if !json.Valid(input.EvidenceConfig) {
		return input, ErrBadRequest(CodeParamsError, "证据配置不是合法 JSON")
	}
	if input.FailureMessageTpl == "" {
		input.FailureMessageTpl = "断言未通过，请检查页面或接口返回"
	}
	return input, nil
}

func isValidAssertionType(assertionType string) bool {
	switch assertionType {
	case model.AIAssertionTypeUIVisible,
		model.AIAssertionTypeUIHidden,
		model.AIAssertionTypeTextContains,
		model.AIAssertionTypeURLContains,
		model.AIAssertionTypeTableRowExists,
		model.AIAssertionTypeTableCellEquals,
		model.AIAssertionTypeAPIFieldEquals,
		model.AIAssertionTypeCustomCode:
		return true
	default:
		return false
	}
}

func (s *AIAssertionAssetService) fillAssertionVirtualFields(ctx context.Context, assertions []model.AIAssertionAsset) {
	if len(assertions) == 0 {
		return
	}
	projectIDs := make([]uint, 0, len(assertions))
	userIDs := make([]uint, 0, len(assertions))
	for _, assertion := range assertions {
		projectIDs = append(projectIDs, assertion.ProjectID)
		userIDs = append(userIDs, assertion.CreatedBy)
	}
	projectMap := batchProjectNames(ctx, s.projectRepo, s.logger, deduplicateUints(projectIDs))
	userMap := batchUserNames(ctx, s.userRepo, s.logger, deduplicateUints(userIDs))
	for i := range assertions {
		assertions[i].ProjectName = projectMap[assertions[i].ProjectID]
		assertions[i].CreatedName = userMap[assertions[i].CreatedBy]
		refCount, err := s.refRepo.CountByTarget(ctx, model.AIAssetRefTargetAssertion, assertions[i].ID)
		if err == nil {
			assertions[i].RefCount = refCount
		}
	}
}

func (s *AIAssertionAssetService) fillAssertionVirtualField(ctx context.Context, assertion *model.AIAssertionAsset) {
	if project, err := s.projectRepo.FindByID(ctx, assertion.ProjectID); err == nil && project != nil {
		assertion.ProjectName = project.Name
	}
	if user, err := s.userRepo.FindByID(ctx, assertion.CreatedBy); err == nil && user != nil {
		assertion.CreatedName = user.Name
	}
	if count, err := s.refRepo.CountByTarget(ctx, model.AIAssetRefTargetAssertion, assertion.ID); err == nil {
		assertion.RefCount = count
	}
}

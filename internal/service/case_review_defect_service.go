// case_review_defect_service.go — 用例评审 Action Items 业务服务（v0.2）
package service

import (
	"context"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"testpilot/internal/model"
	"testpilot/internal/repository"
)

// CaseReviewDefectService 管理评审缺陷（Action Items）的业务规则
//
// 业务边界：
//   - 生成端（Create*）是内部 API，只由同包 Service 调用（主评人驳回、AI 门禁失败）
//   - 处理端（Resolve/Dispute/Reopen）是 Handler 直接触发的对外 API
//   - 任一变更都走 TxManager 事务，确保与 review_item 状态一致
type CaseReviewDefectService struct {
	defectRepo   repository.CaseReviewDefectRepository
	reviewRepo   repository.CaseReviewRepository
	testCaseRepo repository.TestCaseRepository
	txMgr        *repository.TxManager
	logger       *slog.Logger
}

// NewCaseReviewDefectService 构造 CaseReviewDefectService
func NewCaseReviewDefectService(
	defectRepo repository.CaseReviewDefectRepository,
	reviewRepo repository.CaseReviewRepository,
	testCaseRepo repository.TestCaseRepository,
	txMgr *repository.TxManager,
	logger *slog.Logger,
) *CaseReviewDefectService {
	return &CaseReviewDefectService{
		defectRepo:   defectRepo,
		reviewRepo:   reviewRepo,
		testCaseRepo: testCaseRepo,
		txMgr:        txMgr,
		logger:       logger.With("module", "case_review_defect"),
	}
}

// ─── 输入结构 ───

// CreatePrimaryReviewDefectInput 主评人驳回触发的 Action Item 输入
type CreatePrimaryReviewDefectInput struct {
	// ReviewID / ReviewItemID / ProjectID / RecordID 由上游评审提交流程填充
	ReviewID     uint
	ReviewItemID uint
	ProjectID    uint
	RecordID     uint
	// CreatedBy 主评人 ID
	CreatedBy uint
	// Severity critical / major / minor（必填）
	Severity string
	// Title 缺陷标题（取评审评论首行或给定摘要）
	Title string
}

// CreateAIGateDefectInput AI 门禁失败触发的 Action Item 输入
type CreateAIGateDefectInput struct {
	ReviewID     uint
	ReviewItemID uint
	ProjectID    uint
	Severity     string
	Title        string
	// CreatedBy 触发 AI 门禁的用户 ID（手动 rerun 的人，或 Phase 2 后台 worker 的系统用户 ID）
	CreatedBy uint
}

// ResolveDefectInput Author 处理缺陷
type ResolveDefectInput struct {
	Note string
}

// DisputeDefectInput Author 对缺陷提出异议
type DisputeDefectInput struct {
	Reason string
}

// ListDefectsFilter 列表筛选透传到 Repo
type ListDefectsFilter = repository.CaseReviewDefectFilter

// ─── 业务校验工具 ───

// validateSeverity 校验 severity 枚举
func validateSeverity(s string) error {
	switch s {
	case model.ReviewSeverityCritical, model.ReviewSeverityMajor, model.ReviewSeverityMinor:
		return nil
	default:
		return ErrBadRequest(CodeReviewSeverityInvalid, "严重度必须为 critical / major / minor")
	}
}

// ─── 生成端（同包 Service 调用） ───

// CreatePrimaryReviewDefect 主评人驳回/需修改时创建缺陷
// 调用方应当已经在更外层事务里，这里不再开新事务；当 tx == nil 时使用主连接。
func (s *CaseReviewDefectService) CreatePrimaryReviewDefect(ctx context.Context, tx *gorm.DB, input CreatePrimaryReviewDefectInput) (*model.CaseReviewDefect, error) {
	if err := validateSeverity(input.Severity); err != nil {
		return nil, err
	}
	title := input.Title
	if title == "" {
		title = "评审意见待处理"
	}
	defect := &model.CaseReviewDefect{
		ReviewID:     input.ReviewID,
		ReviewItemID: input.ReviewItemID,
		ProjectID:    input.ProjectID,
		RecordID:     input.RecordID,
		Source:       model.DefectSourcePrimaryReview,
		Title:        title,
		Severity:     input.Severity,
		Status:       model.DefectStatusOpen,
		CreatedBy:    input.CreatedBy,
	}
	if err := s.defectRepo.Create(ctx, tx, defect); err != nil {
		s.logger.Error("create primary review defect failed", "review_item_id", input.ReviewItemID, "error", err)
		return nil, err
	}
	s.logger.Info("primary review defect created",
		"defect_id", defect.ID,
		"review_item_id", input.ReviewItemID,
		"severity", input.Severity,
	)
	return defect, nil
}

// CreateAIGateDefect AI 门禁 failed 时创建缺陷（同包 Service 调用）
func (s *CaseReviewDefectService) CreateAIGateDefect(ctx context.Context, tx *gorm.DB, input CreateAIGateDefectInput) (*model.CaseReviewDefect, error) {
	if err := validateSeverity(input.Severity); err != nil {
		return nil, err
	}
	defect := &model.CaseReviewDefect{
		ReviewID:     input.ReviewID,
		ReviewItemID: input.ReviewItemID,
		ProjectID:    input.ProjectID,
		Source:       model.DefectSourceAIGate,
		Title:        input.Title,
		Severity:     input.Severity,
		Status:       model.DefectStatusOpen,
		CreatedBy:    input.CreatedBy,
	}
	if err := s.defectRepo.Create(ctx, tx, defect); err != nil {
		s.logger.Error("create ai gate defect failed", "review_item_id", input.ReviewItemID, "error", err)
		return nil, err
	}
	return defect, nil
}

// ─── 查询端 ───

// List 分页查询
func (s *CaseReviewDefectService) List(ctx context.Context, filter ListDefectsFilter) ([]model.CaseReviewDefect, int64, error) {
	return s.defectRepo.List(ctx, filter)
}

// ListByItemID 单评审项下全部缺陷（按创建时间升序）
func (s *CaseReviewDefectService) ListByItemID(ctx context.Context, reviewItemID uint) ([]model.CaseReviewDefect, error) {
	return s.defectRepo.ListByItemID(ctx, reviewItemID)
}

// Get 查询单条（带项目隔离）
func (s *CaseReviewDefectService) Get(ctx context.Context, projectID, defectID uint) (*model.CaseReviewDefect, error) {
	defect, err := s.defectRepo.GetByID(ctx, defectID, projectID)
	if err != nil {
		return nil, ErrNotFound(CodeReviewDefectNotFound, "Action Item 不存在")
	}
	return defect, nil
}

// ─── 处理端（Handler 调用） ───

// Resolve Author 将缺陷标记为已解决
// 仅允许从 open 状态流转到 resolved；其他状态返回 CodeReviewDefectStatusInvalid。
func (s *CaseReviewDefectService) Resolve(ctx context.Context, projectID, defectID, userID uint, input ResolveDefectInput) error {
	s.logger.Info("resolve defect request", "project_id", projectID, "defect_id", defectID, "user_id", userID)
	defect, err := s.defectRepo.GetByID(ctx, defectID, projectID)
	if err != nil {
		return ErrNotFound(CodeReviewDefectNotFound, "Action Item 不存在")
	}
	if defect.Status != model.DefectStatusOpen {
		return ErrBadRequest(CodeReviewDefectStatusInvalid, "仅 open 状态的 Action Item 可被解决")
	}
	now := time.Now()
	return s.defectRepo.Update(ctx, nil, defect, map[string]any{
		"status":          model.DefectStatusResolved,
		"resolved_by":     userID,
		"resolved_at":     now,
		"resolution_note": input.Note,
	})
}

// Dispute Author 对缺陷提异议
// 仅允许从 open → disputed；dispute_reason 必填。
func (s *CaseReviewDefectService) Dispute(ctx context.Context, projectID, defectID, userID uint, input DisputeDefectInput) error {
	s.logger.Info("dispute defect request", "project_id", projectID, "defect_id", defectID, "user_id", userID)
	if input.Reason == "" {
		return ErrBadRequest(CodeReviewDefectStatusInvalid, "异议原因不能为空")
	}
	defect, err := s.defectRepo.GetByID(ctx, defectID, projectID)
	if err != nil {
		return ErrNotFound(CodeReviewDefectNotFound, "Action Item 不存在")
	}
	if defect.Status != model.DefectStatusOpen {
		return ErrBadRequest(CodeReviewDefectStatusInvalid, "仅 open 状态的 Action Item 可被提异议")
	}
	return s.defectRepo.Update(ctx, nil, defect, map[string]any{
		"status":         model.DefectStatusDisputed,
		"dispute_reason": input.Reason,
	})
}

// Reopen Moderator 将缺陷重置为 open（Phase 1 不限角色，Phase 2 接入 RBAC 校验）
func (s *CaseReviewDefectService) Reopen(ctx context.Context, projectID, defectID, userID uint) error {
	s.logger.Info("reopen defect request", "project_id", projectID, "defect_id", defectID, "user_id", userID)
	defect, err := s.defectRepo.GetByID(ctx, defectID, projectID)
	if err != nil {
		return ErrNotFound(CodeReviewDefectNotFound, "Action Item 不存在")
	}
	if defect.Status == model.DefectStatusOpen {
		return ErrBadRequest(CodeReviewDefectStatusInvalid, "Action Item 已经是 open 状态")
	}
	return s.defectRepo.Update(ctx, nil, defect, map[string]any{
		"status":          model.DefectStatusOpen,
		"resolved_by":     0,
		"resolved_at":     nil,
		"resolution_note": "",
		"dispute_reason":  "",
	})
}

// ─── 重提守卫 ───

// EnsureNoOpenCritical 评审项重提前检查：不允许存在 open+critical 缺陷。
// 供 CaseReviewService.BatchResubmit 之类的流程在事务内调用。
func (s *CaseReviewDefectService) EnsureNoOpenCritical(ctx context.Context, tx *gorm.DB, reviewItemID uint) error {
	count, err := s.defectRepo.CountOpenCriticalByItem(ctx, tx, reviewItemID)
	if err != nil {
		return err
	}
	if count > 0 {
		return ErrBadRequest(CodeReviewCriticalOpen, "存在未解决的严重 Action Item，禁止重新提交评审项")
	}
	return nil
}

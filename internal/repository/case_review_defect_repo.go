// case_review_defect_repo.go — 评审缺陷 / Action Items 数据访问层（v0.2 新增）
package repository

import (
	"context"

	"gorm.io/gorm"

	"testpilot/internal/model"
)

// CaseReviewDefectFilter 评审缺陷列表筛选
type CaseReviewDefectFilter struct {
	// Source: primary_review / ai_gate（空串表示不筛选）
	Source string
	// Status: open / resolved / disputed（空串表示不筛选）
	Status string
	// Severity: critical / major / minor（空串表示不筛选）
	Severity string
	// ReviewItemID: 评审项 ID（0 表示不筛选）
	ReviewItemID uint
	// ReviewID: 评审计划 ID（0 表示不筛选）
	ReviewID uint
	// ProjectID: 项目 ID（必填，用于数据隔离）
	ProjectID uint
	Page      int
	PageSize  int
}

// CaseReviewDefectRepository 评审缺陷仓库接口
type CaseReviewDefectRepository interface {
	// Create 新建一条 Action Item
	Create(ctx context.Context, tx *gorm.DB, defect *model.CaseReviewDefect) error
	// GetByID 根据主键查询（带项目隔离）
	GetByID(ctx context.Context, id, projectID uint) (*model.CaseReviewDefect, error)
	// List 按条件分页列出；ProjectID 必填
	List(ctx context.Context, f CaseReviewDefectFilter) ([]model.CaseReviewDefect, int64, error)
	// ListByItemID 简单查询某评审项下全部缺陷（按创建时间升序）
	ListByItemID(ctx context.Context, reviewItemID uint) ([]model.CaseReviewDefect, error)
	// Update 更新字段
	Update(ctx context.Context, tx *gorm.DB, defect *model.CaseReviewDefect, fields map[string]any) error
	// CountOpenCriticalByItem 统计某评审项下"未 resolve 的 critical"缺陷数，用于重提守卫
	CountOpenCriticalByItem(ctx context.Context, tx *gorm.DB, reviewItemID uint) (int64, error)
	// DeleteByReviewID 级联清理
	DeleteByReviewID(ctx context.Context, tx *gorm.DB, reviewID uint) error
	// DeleteByItemIDs 级联清理（评审项解绑）
	DeleteByItemIDs(ctx context.Context, tx *gorm.DB, itemIDs []uint) error
	// DeleteAIGateOpenByItem 移除指定评审项下 source=ai_gate 且 status=open 的缺陷，
	// 用于规则引擎 rerun 时幂等重建（人工处理过的 disputed/resolved 保留）。
	DeleteAIGateOpenByItem(ctx context.Context, tx *gorm.DB, reviewItemID uint) error
}

type caseReviewDefectRepo struct {
	db *gorm.DB
}

// NewCaseReviewDefectRepo 构造 CaseReviewDefectRepository
func NewCaseReviewDefectRepo(db *gorm.DB) CaseReviewDefectRepository {
	return &caseReviewDefectRepo{db: db}
}

func (r *caseReviewDefectRepo) getDB(tx *gorm.DB) *gorm.DB {
	if tx != nil {
		return tx
	}
	return r.db
}

func (r *caseReviewDefectRepo) Create(ctx context.Context, tx *gorm.DB, defect *model.CaseReviewDefect) error {
	return r.getDB(tx).WithContext(ctx).Create(defect).Error
}

func (r *caseReviewDefectRepo) GetByID(ctx context.Context, id, projectID uint) (*model.CaseReviewDefect, error) {
	var defect model.CaseReviewDefect
	err := r.db.WithContext(ctx).
		Table("case_review_defects").
		Select("case_review_defects.*, COALESCE(u.name, '') AS resolved_by_name").
		Joins("LEFT JOIN users u ON u.id = case_review_defects.resolved_by").
		Where("case_review_defects.id = ? AND case_review_defects.project_id = ?", id, projectID).
		Limit(1).
		Scan(&defect).Error
	if err != nil {
		return nil, err
	}
	if defect.ID == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return &defect, nil
}

func (r *caseReviewDefectRepo) List(ctx context.Context, f CaseReviewDefectFilter) ([]model.CaseReviewDefect, int64, error) {
	countQuery := r.db.WithContext(ctx).Model(&model.CaseReviewDefect{}).Where("project_id = ?", f.ProjectID)
	if f.ReviewID > 0 {
		countQuery = countQuery.Where("review_id = ?", f.ReviewID)
	}
	if f.ReviewItemID > 0 {
		countQuery = countQuery.Where("review_item_id = ?", f.ReviewItemID)
	}
	if f.Source != "" {
		countQuery = countQuery.Where("source = ?", f.Source)
	}
	if f.Status != "" {
		countQuery = countQuery.Where("status = ?", f.Status)
	}
	if f.Severity != "" {
		countQuery = countQuery.Where("severity = ?", f.Severity)
	}

	var total int64
	if err := countQuery.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page, pageSize := f.Page, f.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	listQuery := r.db.WithContext(ctx).
		Table("case_review_defects").
		Select("case_review_defects.*, COALESCE(u.name, '') AS resolved_by_name").
		Joins("LEFT JOIN users u ON u.id = case_review_defects.resolved_by").
		Where("case_review_defects.project_id = ?", f.ProjectID)
	if f.ReviewID > 0 {
		listQuery = listQuery.Where("case_review_defects.review_id = ?", f.ReviewID)
	}
	if f.ReviewItemID > 0 {
		listQuery = listQuery.Where("case_review_defects.review_item_id = ?", f.ReviewItemID)
	}
	if f.Source != "" {
		listQuery = listQuery.Where("case_review_defects.source = ?", f.Source)
	}
	if f.Status != "" {
		listQuery = listQuery.Where("case_review_defects.status = ?", f.Status)
	}
	if f.Severity != "" {
		listQuery = listQuery.Where("case_review_defects.severity = ?", f.Severity)
	}

	var defects []model.CaseReviewDefect
	err := listQuery.
		Order("case_review_defects.created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Scan(&defects).Error
	return defects, total, err
}

func (r *caseReviewDefectRepo) ListByItemID(ctx context.Context, reviewItemID uint) ([]model.CaseReviewDefect, error) {
	var defects []model.CaseReviewDefect
	err := r.db.WithContext(ctx).
		Table("case_review_defects").
		Select("case_review_defects.*, COALESCE(u.name, '') AS resolved_by_name").
		Joins("LEFT JOIN users u ON u.id = case_review_defects.resolved_by").
		Where("case_review_defects.review_item_id = ?", reviewItemID).
		Order("case_review_defects.created_at ASC").
		Scan(&defects).Error
	return defects, err
}

func (r *caseReviewDefectRepo) Update(ctx context.Context, tx *gorm.DB, defect *model.CaseReviewDefect, fields map[string]any) error {
	return r.getDB(tx).WithContext(ctx).Model(defect).Updates(fields).Error
}

func (r *caseReviewDefectRepo) CountOpenCriticalByItem(ctx context.Context, tx *gorm.DB, reviewItemID uint) (int64, error) {
	var count int64
	err := r.getDB(tx).WithContext(ctx).
		Model(&model.CaseReviewDefect{}).
		Where("review_item_id = ? AND severity = ? AND status = ?", reviewItemID, model.ReviewSeverityCritical, model.DefectStatusOpen).
		Count(&count).Error
	return count, err
}

func (r *caseReviewDefectRepo) DeleteByReviewID(ctx context.Context, tx *gorm.DB, reviewID uint) error {
	return r.getDB(tx).WithContext(ctx).Where("review_id = ?", reviewID).Delete(&model.CaseReviewDefect{}).Error
}

func (r *caseReviewDefectRepo) DeleteByItemIDs(ctx context.Context, tx *gorm.DB, itemIDs []uint) error {
	if len(itemIDs) == 0 {
		return nil
	}
	return r.getDB(tx).WithContext(ctx).Where("review_item_id IN ?", itemIDs).Delete(&model.CaseReviewDefect{}).Error
}

func (r *caseReviewDefectRepo) DeleteAIGateOpenByItem(ctx context.Context, tx *gorm.DB, reviewItemID uint) error {
	return r.getDB(tx).WithContext(ctx).
		Where("review_item_id = ? AND source = ? AND status = ?",
			reviewItemID, model.DefectSourceAIGate, model.DefectStatusOpen).
		Delete(&model.CaseReviewDefect{}).Error
}

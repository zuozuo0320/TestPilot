// case_review_repo.go — 用例评审数据访问层（计划 + 评审项 + 评审人）
package repository

import (
	"context"
	"strconv"

	"gorm.io/gorm"

	"testpilot/internal/model"
)

// ─── Filter ───

// CaseReviewFilter 评审计划列表筛选
type CaseReviewFilter struct {
	View       string // all / assigned / created
	Keyword    string
	Status     string
	ReviewMode string
	ReviewerID *uint
	ModuleID   *uint
	CreatedBy  *uint
	Page       int
	PageSize   int
}

// CaseReviewItemFilter 评审项列表筛选
type CaseReviewItemFilter struct {
	Keyword      string
	ReviewStatus string
	FinalResult  string
	ReviewerID   *uint
	ModuleID     *uint
	Page         int
	PageSize     int
}

// ─── Interface ───

// CaseReviewRepository 评审计划仓库接口
type CaseReviewRepository interface {
	// ── 评审计划 CRUD ──
	CreateReview(ctx context.Context, tx *gorm.DB, r *model.CaseReview) error
	GetReviewByID(ctx context.Context, id, projectID uint) (*model.CaseReview, error)
	ListReviews(ctx context.Context, projectID, currentUserID uint, f CaseReviewFilter) ([]model.CaseReview, int64, error)
	UpdateReview(ctx context.Context, tx *gorm.DB, r *model.CaseReview, fields map[string]any) error
	DeleteReview(ctx context.Context, tx *gorm.DB, id, projectID uint) error

	// ── 评审项 ──
	CreateItems(ctx context.Context, tx *gorm.DB, items []model.CaseReviewItem) error
	CreateItem(ctx context.Context, tx *gorm.DB, item *model.CaseReviewItem) error
	GetItemByID(ctx context.Context, tx *gorm.DB, itemID uint) (*model.CaseReviewItem, error)
	ListItems(ctx context.Context, reviewID, projectID uint, f CaseReviewItemFilter) ([]model.CaseReviewItem, int64, error)
	UpdateItem(ctx context.Context, tx *gorm.DB, item *model.CaseReviewItem, fields map[string]any) error
	DeleteItems(ctx context.Context, tx *gorm.DB, reviewID uint, itemIDs []uint) error
	DeleteItemsByReviewID(ctx context.Context, tx *gorm.DB, reviewID uint) error
	HasActiveReviewForCase(ctx context.Context, projectID, testcaseID uint, excludeReviewID uint) (bool, error)
	FindNextPendingItem(ctx context.Context, reviewID, currentItemID uint) (*model.CaseReviewItem, error)
	CountItemsByOwnership(ctx context.Context, tx *gorm.DB, reviewID, projectID uint, itemIDs []uint) (int64, error)

	// ── 评审人分配 ──
	CreateReviewers(ctx context.Context, tx *gorm.DB, reviewers []model.CaseReviewItemReviewer) error
	GetReviewer(ctx context.Context, tx *gorm.DB, itemID, reviewerID uint) (*model.CaseReviewItemReviewer, error)
	UpdateReviewer(ctx context.Context, tx *gorm.DB, r *model.CaseReviewItemReviewer, fields map[string]any) error
	ListReviewersByItemID(ctx context.Context, tx *gorm.DB, itemID uint) ([]model.CaseReviewItemReviewer, error)
	ListReviewersByReviewID(ctx context.Context, reviewID uint) ([]model.CaseReviewItemReviewer, error)
	DeleteReviewersByItemIDs(ctx context.Context, tx *gorm.DB, itemIDs []uint) error
	DeleteReviewersByReviewID(ctx context.Context, tx *gorm.DB, reviewID uint) error
	ResetReviewersByItemID(ctx context.Context, tx *gorm.DB, itemID uint) error

	// ── 统计重算 ──
	RecalcReviewStats(ctx context.Context, tx *gorm.DB, reviewID uint) error
}

// ─── Implementation ───

type caseReviewRepo struct {
	db *gorm.DB
}

func NewCaseReviewRepo(db *gorm.DB) CaseReviewRepository {
	return &caseReviewRepo{db: db}
}

func (r *caseReviewRepo) getDB(tx *gorm.DB) *gorm.DB {
	if tx != nil {
		return tx
	}
	return r.db
}

// ── 评审计划 CRUD ──

func (r *caseReviewRepo) CreateReview(ctx context.Context, tx *gorm.DB, review *model.CaseReview) error {
	return r.getDB(tx).WithContext(ctx).Create(review).Error
}

func (r *caseReviewRepo) GetReviewByID(ctx context.Context, id, projectID uint) (*model.CaseReview, error) {
	var cr model.CaseReview
	err := r.db.WithContext(ctx).
		Table("case_reviews").
		Select("case_reviews.*, COALESCE(users.name, '') AS created_by_name, COALESCE(users.avatar, '') AS created_by_avatar").
		Joins("LEFT JOIN users ON users.id = case_reviews.created_by").
		Where("case_reviews.id = ? AND case_reviews.project_id = ?", id, projectID).
		Limit(1).
		Scan(&cr).Error
	if err != nil {
		return nil, err
	}
	if cr.ID == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return &cr, nil
}

func (r *caseReviewRepo) ListReviews(ctx context.Context, projectID, currentUserID uint, f CaseReviewFilter) ([]model.CaseReview, int64, error) {
	baseQuery := r.db.WithContext(ctx).Model(&model.CaseReview{}).Where("project_id = ?", projectID)

	// 视图过滤
	switch f.View {
	case "created":
		baseQuery = baseQuery.Where("created_by = ?", currentUserID)
	case "assigned":
		baseQuery = baseQuery.Where("id IN (?)",
			r.db.Model(&model.CaseReviewItemReviewer{}).
				Select("DISTINCT review_id").
				Where("project_id = ? AND reviewer_id = ?", projectID, currentUserID),
		)
	}

	if f.Keyword != "" {
		like := "%" + f.Keyword + "%"
		if idKey, err := strconv.Atoi(f.Keyword); err == nil && idKey > 0 {
			baseQuery = baseQuery.Where("id = ? OR name LIKE ?", idKey, like)
		} else {
			baseQuery = baseQuery.Where("name LIKE ?", like)
		}
	}
	if f.Status != "" {
		baseQuery = baseQuery.Where("status = ?", f.Status)
	}
	if f.ReviewMode != "" {
		baseQuery = baseQuery.Where("review_mode = ?", f.ReviewMode)
	}
	if f.ModuleID != nil {
		baseQuery = baseQuery.Where("module_id = ?", *f.ModuleID)
	}
	if f.CreatedBy != nil {
		baseQuery = baseQuery.Where("created_by = ?", *f.CreatedBy)
	}
	if f.ReviewerID != nil {
		baseQuery = baseQuery.Where("id IN (?)",
			r.db.Model(&model.CaseReviewItemReviewer{}).
				Select("DISTINCT review_id").
				Where("project_id = ? AND reviewer_id = ?", projectID, *f.ReviewerID),
		)
	}

	var total int64
	if err := baseQuery.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page, pageSize := f.Page, f.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	var reviews []model.CaseReview
	err := baseQuery.
		Table("case_reviews").
		Select("case_reviews.*, COALESCE(users.name, '') AS created_by_name, COALESCE(users.avatar, '') AS created_by_avatar").
		Joins("LEFT JOIN users ON users.id = case_reviews.created_by").
		Order("case_reviews.created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Scan(&reviews).Error
	return reviews, total, err
}

func (r *caseReviewRepo) UpdateReview(ctx context.Context, tx *gorm.DB, review *model.CaseReview, fields map[string]any) error {
	return r.getDB(tx).WithContext(ctx).Model(review).Updates(fields).Error
}

func (r *caseReviewRepo) DeleteReview(ctx context.Context, tx *gorm.DB, id, projectID uint) error {
	return r.getDB(tx).WithContext(ctx).Where("id = ? AND project_id = ?", id, projectID).Delete(&model.CaseReview{}).Error
}

// ── 评审项 ──

func (r *caseReviewRepo) CreateItems(ctx context.Context, tx *gorm.DB, items []model.CaseReviewItem) error {
	if len(items) == 0 {
		return nil
	}
	return r.getDB(tx).WithContext(ctx).Create(&items).Error
}

func (r *caseReviewRepo) CreateItem(ctx context.Context, tx *gorm.DB, item *model.CaseReviewItem) error {
	return r.getDB(tx).WithContext(ctx).Create(item).Error
}

func (r *caseReviewRepo) GetItemByID(ctx context.Context, tx *gorm.DB, itemID uint) (*model.CaseReviewItem, error) {
	var item model.CaseReviewItem
	err := r.getDB(tx).WithContext(ctx).Where("id = ?", itemID).First(&item).Error
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *caseReviewRepo) ListItems(ctx context.Context, reviewID, projectID uint, f CaseReviewItemFilter) ([]model.CaseReviewItem, int64, error) {
	q := r.db.WithContext(ctx).Model(&model.CaseReviewItem{}).Where("review_id = ? AND project_id = ?", reviewID, projectID)

	if f.Keyword != "" {
		like := "%" + f.Keyword + "%"
		if idKey, err := strconv.Atoi(f.Keyword); err == nil && idKey > 0 {
			q = q.Where("testcase_id = ? OR title_snapshot LIKE ?", idKey, like)
		} else {
			q = q.Where("title_snapshot LIKE ?", like)
		}
	}
	if f.ReviewStatus != "" {
		q = q.Where("review_status = ?", f.ReviewStatus)
	}
	if f.FinalResult != "" {
		q = q.Where("final_result = ?", f.FinalResult)
	}
	if f.ModuleID != nil {
		q = q.Where("module_id = ?", *f.ModuleID)
	}
	if f.ReviewerID != nil {
		q = q.Where("id IN (?)",
			r.db.Model(&model.CaseReviewItemReviewer{}).
				Select("review_item_id").
				Where("reviewer_id = ?", *f.ReviewerID),
		)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page, pageSize := f.Page, f.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	var items []model.CaseReviewItem
	err := q.Order("sort_order ASC, id ASC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error
	return items, total, err
}

func (r *caseReviewRepo) UpdateItem(ctx context.Context, tx *gorm.DB, item *model.CaseReviewItem, fields map[string]any) error {
	return r.getDB(tx).WithContext(ctx).Model(item).Updates(fields).Error
}

func (r *caseReviewRepo) DeleteItems(ctx context.Context, tx *gorm.DB, reviewID uint, itemIDs []uint) error {
	if len(itemIDs) == 0 {
		return nil
	}
	return r.getDB(tx).WithContext(ctx).Where("review_id = ? AND id IN ?", reviewID, itemIDs).Delete(&model.CaseReviewItem{}).Error
}

func (r *caseReviewRepo) DeleteItemsByReviewID(ctx context.Context, tx *gorm.DB, reviewID uint) error {
	return r.getDB(tx).WithContext(ctx).Where("review_id = ?", reviewID).Delete(&model.CaseReviewItem{}).Error
}

func (r *caseReviewRepo) HasActiveReviewForCase(ctx context.Context, projectID, testcaseID, excludeReviewID uint) (bool, error) {
	var count int64
	q := r.db.WithContext(ctx).
		Model(&model.CaseReviewItem{}).
		Joins("JOIN case_reviews ON case_reviews.id = case_review_items.review_id").
		Where("case_review_items.project_id = ? AND case_review_items.testcase_id = ?", projectID, testcaseID).
		Where("case_reviews.status IN ?", []string{model.ReviewPlanStatusNotStarted, model.ReviewPlanStatusInProgress})
	if excludeReviewID > 0 {
		q = q.Where("case_review_items.review_id != ?", excludeReviewID)
	}
	err := q.Count(&count).Error
	return count > 0, err
}

func (r *caseReviewRepo) FindNextPendingItem(ctx context.Context, reviewID, currentItemID uint) (*model.CaseReviewItem, error) {
	var item model.CaseReviewItem
	err := r.db.WithContext(ctx).
		Where("review_id = ? AND id > ? AND final_result = ?", reviewID, currentItemID, model.ReviewResultPending).
		Order("sort_order ASC, id ASC").
		First(&item).Error
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// CountItemsByOwnership 校验 item 归属：返回给定 itemIDs 中确实属于 reviewID+projectID 的记录数
func (r *caseReviewRepo) CountItemsByOwnership(ctx context.Context, tx *gorm.DB, reviewID, projectID uint, itemIDs []uint) (int64, error) {
	var count int64
	err := r.getDB(tx).WithContext(ctx).
		Model(&model.CaseReviewItem{}).
		Where("review_id = ? AND project_id = ? AND id IN ?", reviewID, projectID, itemIDs).
		Count(&count).Error
	return count, err
}

// ── 评审人分配 ──

func (r *caseReviewRepo) CreateReviewers(ctx context.Context, tx *gorm.DB, reviewers []model.CaseReviewItemReviewer) error {
	if len(reviewers) == 0 {
		return nil
	}
	return r.getDB(tx).WithContext(ctx).Create(&reviewers).Error
}

func (r *caseReviewRepo) GetReviewer(ctx context.Context, tx *gorm.DB, itemID, reviewerID uint) (*model.CaseReviewItemReviewer, error) {
	var rev model.CaseReviewItemReviewer
	err := r.getDB(tx).WithContext(ctx).Where("review_item_id = ? AND reviewer_id = ?", itemID, reviewerID).First(&rev).Error
	if err != nil {
		return nil, err
	}
	return &rev, nil
}

func (r *caseReviewRepo) UpdateReviewer(ctx context.Context, tx *gorm.DB, rev *model.CaseReviewItemReviewer, fields map[string]any) error {
	return r.getDB(tx).WithContext(ctx).Model(rev).Updates(fields).Error
}

func (r *caseReviewRepo) ListReviewersByItemID(ctx context.Context, tx *gorm.DB, itemID uint) ([]model.CaseReviewItemReviewer, error) {
	var reviewers []model.CaseReviewItemReviewer
	// [FIX #6] 按 reviewed_at DESC 排序，确保单人模式可正确取最新提交
	err := r.getDB(tx).WithContext(ctx).Where("review_item_id = ?", itemID).Order("reviewed_at DESC, id DESC").Find(&reviewers).Error
	return reviewers, err
}

func (r *caseReviewRepo) ListReviewersByReviewID(ctx context.Context, reviewID uint) ([]model.CaseReviewItemReviewer, error) {
	var reviewers []model.CaseReviewItemReviewer
	err := r.db.WithContext(ctx).Where("review_id = ?", reviewID).Find(&reviewers).Error
	return reviewers, err
}

func (r *caseReviewRepo) DeleteReviewersByItemIDs(ctx context.Context, tx *gorm.DB, itemIDs []uint) error {
	if len(itemIDs) == 0 {
		return nil
	}
	return r.getDB(tx).WithContext(ctx).Where("review_item_id IN ?", itemIDs).Delete(&model.CaseReviewItemReviewer{}).Error
}

func (r *caseReviewRepo) DeleteReviewersByReviewID(ctx context.Context, tx *gorm.DB, reviewID uint) error {
	return r.getDB(tx).WithContext(ctx).Where("review_id = ?", reviewID).Delete(&model.CaseReviewItemReviewer{}).Error
}

func (r *caseReviewRepo) ResetReviewersByItemID(ctx context.Context, tx *gorm.DB, itemID uint) error {
	return r.getDB(tx).WithContext(ctx).
		Model(&model.CaseReviewItemReviewer{}).
		Where("review_item_id = ?", itemID).
		Updates(map[string]any{
			"review_status":  model.ReviewerStatusPending,
			"latest_result":  nil,
			"latest_comment": nil,
			"reviewed_at":    nil,
		}).Error
}

// ── 统计重算 ──

func (r *caseReviewRepo) RecalcReviewStats(ctx context.Context, tx *gorm.DB, reviewID uint) error {
	d := r.getDB(tx).WithContext(ctx)

	type stats struct {
		Total       int
		Pending     int
		Approved    int
		Rejected    int
		NeedsUpdate int
	}

	var s stats
	err := d.Raw(`
		SELECT
			COUNT(*) AS total,
			SUM(CASE WHEN final_result = 'pending' THEN 1 ELSE 0 END) AS pending,
			SUM(CASE WHEN final_result = 'approved' THEN 1 ELSE 0 END) AS approved,
			SUM(CASE WHEN final_result = 'rejected' THEN 1 ELSE 0 END) AS rejected,
			SUM(CASE WHEN final_result = 'needs_update' THEN 1 ELSE 0 END) AS needs_update
		FROM case_review_items WHERE review_id = ?
	`, reviewID).Scan(&s).Error
	if err != nil {
		return err
	}

	passRate := 0.0
	if s.Total > 0 {
		passRate = float64(s.Approved) / float64(s.Total) * 100
	}

	// 自动判定计划状态
	planStatus := model.ReviewPlanStatusNotStarted
	if s.Total > 0 {
		if s.Pending == s.Total {
			planStatus = model.ReviewPlanStatusNotStarted
		} else if s.Approved+s.Rejected+s.NeedsUpdate == s.Total {
			planStatus = model.ReviewPlanStatusCompleted
		} else {
			planStatus = model.ReviewPlanStatusInProgress
		}
	}

	return d.Model(&model.CaseReview{}).Where("id = ?", reviewID).Updates(map[string]any{
		"case_total_count":   s.Total,
		"pending_count":      s.Pending,
		"approved_count":     s.Approved,
		"rejected_count":     s.Rejected,
		"needs_update_count": s.NeedsUpdate,
		"pass_rate":          passRate,
		"status":             planStatus,
	}).Error
}

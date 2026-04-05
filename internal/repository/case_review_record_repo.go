// case_review_record_repo.go — 评审记录数据访问层（append-only）
package repository

import (
	"context"

	"gorm.io/gorm"

	"testpilot/internal/model"
)

// CaseReviewRecordRepository 评审记录仓库接口
type CaseReviewRecordRepository interface {
	Create(ctx context.Context, tx *gorm.DB, record *model.CaseReviewRecord) error
	ListByItemID(ctx context.Context, itemID uint, roundNo *int, page, pageSize int) ([]model.CaseReviewRecord, int64, error)
	HasRecordsByReviewID(ctx context.Context, reviewID uint) (bool, error)
	HasRecordsByItemIDs(ctx context.Context, itemIDs []uint) (bool, error)
	DeleteByReviewID(ctx context.Context, tx *gorm.DB, reviewID uint) error
}

type caseReviewRecordRepo struct {
	db *gorm.DB
}

func NewCaseReviewRecordRepo(db *gorm.DB) CaseReviewRecordRepository {
	return &caseReviewRecordRepo{db: db}
}

func (r *caseReviewRecordRepo) getDB(tx *gorm.DB) *gorm.DB {
	if tx != nil {
		return tx
	}
	return r.db
}

func (r *caseReviewRecordRepo) Create(ctx context.Context, tx *gorm.DB, record *model.CaseReviewRecord) error {
	return r.getDB(tx).WithContext(ctx).Create(record).Error
}

func (r *caseReviewRecordRepo) ListByItemID(ctx context.Context, itemID uint, roundNo *int, page, pageSize int) ([]model.CaseReviewRecord, int64, error) {
	q := r.db.WithContext(ctx).Model(&model.CaseReviewRecord{}).Where("review_item_id = ?", itemID)

	if roundNo != nil {
		q = q.Where("round_no = ?", *roundNo)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	var records []model.CaseReviewRecord
	err := q.Order("created_at ASC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&records).Error
	return records, total, err
}

func (r *caseReviewRecordRepo) HasRecordsByReviewID(ctx context.Context, reviewID uint) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.CaseReviewRecord{}).Where("review_id = ?", reviewID).Limit(1).Count(&count).Error
	return count > 0, err
}

func (r *caseReviewRecordRepo) HasRecordsByItemIDs(ctx context.Context, itemIDs []uint) (bool, error) {
	if len(itemIDs) == 0 {
		return false, nil
	}
	var count int64
	err := r.db.WithContext(ctx).Model(&model.CaseReviewRecord{}).Where("review_item_id IN ?", itemIDs).Limit(1).Count(&count).Error
	return count > 0, err
}

func (r *caseReviewRecordRepo) DeleteByReviewID(ctx context.Context, tx *gorm.DB, reviewID uint) error {
	return r.getDB(tx).WithContext(ctx).Where("review_id = ?", reviewID).Delete(&model.CaseReviewRecord{}).Error
}

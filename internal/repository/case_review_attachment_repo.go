// case_review_attachment_repo.go — 评审项附件数据访问层
package repository

import (
	"context"

	"gorm.io/gorm"

	"testpilot/internal/model"
)

// CaseReviewAttachmentRepository 评审附件仓库接口
type CaseReviewAttachmentRepository interface {
	Create(ctx context.Context, tx *gorm.DB, att *model.CaseReviewAttachment) error
	GetByID(ctx context.Context, id uint) (*model.CaseReviewAttachment, error)
	ListByItemID(ctx context.Context, reviewItemID uint) ([]model.CaseReviewAttachment, error)
	ListByTestCaseID(ctx context.Context, projectID, testCaseID uint) ([]model.CaseReviewAttachment, error)
	Delete(ctx context.Context, tx *gorm.DB, id uint) error
	DeleteByReviewID(ctx context.Context, tx *gorm.DB, reviewID uint) error
	DeleteByItemIDs(ctx context.Context, tx *gorm.DB, itemIDs []uint) error
}

type caseReviewAttachmentRepo struct {
	db *gorm.DB
}

func NewCaseReviewAttachmentRepo(db *gorm.DB) CaseReviewAttachmentRepository {
	return &caseReviewAttachmentRepo{db: db}
}

func (r *caseReviewAttachmentRepo) getDB(tx *gorm.DB) *gorm.DB {
	if tx != nil {
		return tx
	}
	return r.db
}

func (r *caseReviewAttachmentRepo) Create(ctx context.Context, tx *gorm.DB, att *model.CaseReviewAttachment) error {
	return r.getDB(tx).WithContext(ctx).Create(att).Error
}

func (r *caseReviewAttachmentRepo) GetByID(ctx context.Context, id uint) (*model.CaseReviewAttachment, error) {
	var att model.CaseReviewAttachment
	err := r.db.WithContext(ctx).First(&att, id).Error
	if err != nil {
		return nil, err
	}
	return &att, nil
}

// ListByItemID 查询单个评审项的全部附件，带上传人姓名回填
func (r *caseReviewAttachmentRepo) ListByItemID(ctx context.Context, reviewItemID uint) ([]model.CaseReviewAttachment, error) {
	var list []model.CaseReviewAttachment
	err := r.db.WithContext(ctx).
		Table("case_review_attachments").
		Select("case_review_attachments.*, COALESCE(u.name, '') AS uploader_name").
		Joins("LEFT JOIN users u ON u.id = case_review_attachments.created_by").
		Where("case_review_attachments.review_item_id = ?", reviewItemID).
		Order("case_review_attachments.created_at ASC").
		Scan(&list).Error
	return list, err
}

// ListByTestCaseID 查询某个用例在所有评审计划下的历史附件（只读镜像），带评审计划名称 + 上传人
// 注意：GORM 根据 struct 字段 TestCaseID 自动生成列名 test_case_id（snake_case），SQL 必须用真实列名
func (r *caseReviewAttachmentRepo) ListByTestCaseID(ctx context.Context, projectID, testCaseID uint) ([]model.CaseReviewAttachment, error) {
	var list []model.CaseReviewAttachment
	err := r.db.WithContext(ctx).
		Table("case_review_attachments").
		Select("case_review_attachments.*, COALESCE(u.name, '') AS uploader_name, COALESCE(r.name, '') AS review_name").
		Joins("LEFT JOIN users u ON u.id = case_review_attachments.created_by").
		Joins("LEFT JOIN case_reviews r ON r.id = case_review_attachments.review_id").
		Where("case_review_attachments.project_id = ? AND case_review_attachments.test_case_id = ?", projectID, testCaseID).
		Order("case_review_attachments.created_at DESC").
		Scan(&list).Error
	return list, err
}

func (r *caseReviewAttachmentRepo) Delete(ctx context.Context, tx *gorm.DB, id uint) error {
	return r.getDB(tx).WithContext(ctx).Delete(&model.CaseReviewAttachment{}, id).Error
}

func (r *caseReviewAttachmentRepo) DeleteByReviewID(ctx context.Context, tx *gorm.DB, reviewID uint) error {
	return r.getDB(tx).WithContext(ctx).Where("review_id = ?", reviewID).Delete(&model.CaseReviewAttachment{}).Error
}

func (r *caseReviewAttachmentRepo) DeleteByItemIDs(ctx context.Context, tx *gorm.DB, itemIDs []uint) error {
	if len(itemIDs) == 0 {
		return nil
	}
	return r.getDB(tx).WithContext(ctx).Where("review_item_id IN ?", itemIDs).Delete(&model.CaseReviewAttachment{}).Error
}

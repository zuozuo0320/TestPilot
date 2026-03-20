package repository

import (
	"testpilot/internal/model"

	"gorm.io/gorm"
)

type AttachmentRepo struct {
	db *gorm.DB
}

func NewAttachmentRepo(db *gorm.DB) *AttachmentRepo {
	return &AttachmentRepo{db: db}
}

// Create inserts a new attachment.
func (r *AttachmentRepo) Create(a *model.CaseAttachment) error {
	return r.db.Create(a).Error
}

// ListByCaseID returns all attachments for a test case.
func (r *AttachmentRepo) ListByCaseID(testCaseID uint) ([]model.CaseAttachment, error) {
	var attachments []model.CaseAttachment
	err := r.db.Where("test_case_id = ?", testCaseID).Order("id ASC").Find(&attachments).Error
	return attachments, err
}

// GetByID fetches a single attachment.
func (r *AttachmentRepo) GetByID(id uint) (*model.CaseAttachment, error) {
	var a model.CaseAttachment
	err := r.db.First(&a, id).Error
	return &a, err
}

// Delete removes an attachment by ID.
func (r *AttachmentRepo) Delete(id uint) error {
	return r.db.Delete(&model.CaseAttachment{}, id).Error
}

// DeleteByCaseID removes all attachments for a test case.
func (r *AttachmentRepo) DeleteByCaseID(testCaseID uint) error {
	return r.db.Where("test_case_id = ?", testCaseID).Delete(&model.CaseAttachment{}).Error
}

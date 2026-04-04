package repository

import (
	"testpilot/internal/model"

	"gorm.io/gorm"
)

type CaseHistoryRepo struct {
	db *gorm.DB
}

func NewCaseHistoryRepo(db *gorm.DB) *CaseHistoryRepo {
	return &CaseHistoryRepo{db: db}
}

// Create inserts a history record.
func (r *CaseHistoryRepo) Create(db *gorm.DB, h *model.CaseHistory) error {
	if db == nil {
		db = r.db
	}
	return db.Create(h).Error
}

// CreateBatch inserts multiple history records.
func (r *CaseHistoryRepo) CreateBatch(records []model.CaseHistory) error {
	if len(records) == 0 {
		return nil
	}
	return r.db.Create(&records).Error
}

// ListByCaseID returns paginated history for a test case.
func (r *CaseHistoryRepo) ListByCaseID(testCaseID uint, page, pageSize int) ([]model.CaseHistory, int64, error) {
	var total int64
	r.db.Model(&model.CaseHistory{}).Where("testcase_id = ?", testCaseID).Count(&total)

	var items []model.CaseHistory
	err := r.db.Where("testcase_id = ?", testCaseID).
		Order("id DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&items).Error
	return items, total, err
}

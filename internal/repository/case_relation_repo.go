package repository

import (
	"testpilot/internal/model"

	"gorm.io/gorm"
)

type CaseRelationRepo struct {
	db *gorm.DB
}

func NewCaseRelationRepo(db *gorm.DB) *CaseRelationRepo {
	return &CaseRelationRepo{db: db}
}

// Create inserts a new relation.
func (r *CaseRelationRepo) Create(rel *model.CaseRelation) error {
	return r.db.Create(rel).Error
}

// ListByCaseID returns all relations where the case is source or target.
func (r *CaseRelationRepo) ListByCaseID(caseID uint) ([]model.CaseRelation, error) {
	var relations []model.CaseRelation
	err := r.db.Where("source_case_id = ? OR target_case_id = ?", caseID, caseID).
		Order("id ASC").
		Find(&relations).Error
	return relations, err
}

// Delete removes a relation by ID.
func (r *CaseRelationRepo) Delete(id uint) error {
	return r.db.Delete(&model.CaseRelation{}, id).Error
}

// DeleteByCaseID removes all relations for a case.
func (r *CaseRelationRepo) DeleteByCaseID(caseID uint) error {
	return r.db.Where("source_case_id = ? OR target_case_id = ?", caseID, caseID).
		Delete(&model.CaseRelation{}).Error
}

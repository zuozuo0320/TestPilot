// requirement_repo.go — 需求数据访问层
package repository

import (
	"context"

	"gorm.io/gorm"

	"testpilot/internal/model"
)

// RequirementRepository 需求数据访问接口
type RequirementRepository interface {
	// Create 创建需求
	Create(ctx context.Context, req *model.Requirement) error
	// List 获取项目需求列表
	List(ctx context.Context, projectID uint) ([]model.Requirement, error)
	// BelongsToProject 检查需求是否属于指定项目
	BelongsToProject(ctx context.Context, id, projectID uint) (bool, error)
	// Count 统计项目需求数量
	Count(ctx context.Context, projectID uint) (int64, error)
	// LinkTestCase 关联需求与用例
	LinkTestCase(ctx context.Context, requirementID, testCaseID uint) error
}

// requirementRepo RequirementRepository 的 GORM 实现
type requirementRepo struct {
	db *gorm.DB
}

// NewRequirementRepo 创建需求仓库
func NewRequirementRepo(db *gorm.DB) RequirementRepository {
	return &requirementRepo{db: db}
}

func (r *requirementRepo) Create(ctx context.Context, req *model.Requirement) error {
	return r.db.WithContext(ctx).Create(req).Error
}

func (r *requirementRepo) List(ctx context.Context, projectID uint) ([]model.Requirement, error) {
	var entities []model.Requirement
	err := r.db.WithContext(ctx).Where("project_id = ?", projectID).Order("id asc").Find(&entities).Error
	return entities, err
}

func (r *requirementRepo) BelongsToProject(ctx context.Context, id, projectID uint) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.Requirement{}).Where("id = ? AND project_id = ?", id, projectID).Count(&count).Error
	return count > 0, err
}

func (r *requirementRepo) Count(ctx context.Context, projectID uint) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.Requirement{}).Where("project_id = ?", projectID).Count(&count).Error
	return count, err
}

func (r *requirementRepo) LinkTestCase(ctx context.Context, requirementID, testCaseID uint) error {
	link := model.RequirementTestCase{RequirementID: requirementID, TestCaseID: testCaseID}
	return r.db.WithContext(ctx).Where(&link).FirstOrCreate(&link).Error
}

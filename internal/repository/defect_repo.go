// defect_repo.go — 缺陷数据访问层
package repository

import (
	"context"

	"gorm.io/gorm"

	"testpilot/internal/model"
)

// DefectRepository 缺陷数据访问接口
type DefectRepository interface {
	// Create 创建缺陷
	Create(ctx context.Context, defect *model.Defect) error
	// List 获取项目缺陷列表
	List(ctx context.Context, projectID uint) ([]model.Defect, error)
	// Count 统计项目缺陷数量
	Count(ctx context.Context, projectID uint) (int64, error)
}

// defectRepo DefectRepository 的 GORM 实现
type defectRepo struct {
	db *gorm.DB
}

// NewDefectRepo 创建缺陷仓库
func NewDefectRepo(db *gorm.DB) DefectRepository {
	return &defectRepo{db: db}
}

func (r *defectRepo) Create(ctx context.Context, defect *model.Defect) error {
	return r.db.WithContext(ctx).Create(defect).Error
}

func (r *defectRepo) List(ctx context.Context, projectID uint) ([]model.Defect, error) {
	var defects []model.Defect
	err := r.db.WithContext(ctx).Where("project_id = ?", projectID).Order("id asc").Find(&defects).Error
	return defects, err
}

func (r *defectRepo) Count(ctx context.Context, projectID uint) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.Defect{}).Where("project_id = ?", projectID).Count(&count).Error
	return count, err
}

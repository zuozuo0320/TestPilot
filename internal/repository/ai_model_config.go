// ai_model_config.go — AI 模型配置数据访问层
package repository

import (
	"context"

	"gorm.io/gorm"

	"testpilot/internal/model"
)

// AIModelConfigRepo AI 模型配置数据访问
type AIModelConfigRepo struct {
	db *gorm.DB
}

// NewAIModelConfigRepo 构造函数
func NewAIModelConfigRepo(db *gorm.DB) *AIModelConfigRepo {
	return &AIModelConfigRepo{db: db}
}

func (r *AIModelConfigRepo) getDB(tx *gorm.DB) *gorm.DB {
	if tx != nil {
		return tx
	}
	return r.db
}

// List 查询所有模型配置，按 sort_order 排序
func (r *AIModelConfigRepo) List(ctx context.Context) ([]model.AIModelConfig, error) {
	var configs []model.AIModelConfig
	err := r.db.WithContext(ctx).Order("sort_order ASC, id ASC").Find(&configs).Error
	return configs, err
}

// GetByID 根据 ID 查询
func (r *AIModelConfigRepo) GetByID(ctx context.Context, id uint) (*model.AIModelConfig, error) {
	var cfg model.AIModelConfig
	if err := r.db.WithContext(ctx).First(&cfg, id).Error; err != nil {
		return nil, err
	}
	return &cfg, nil
}

// GetActive 查询当前启用的模型
func (r *AIModelConfigRepo) GetActive(ctx context.Context) (*model.AIModelConfig, error) {
	var cfg model.AIModelConfig
	if err := r.db.WithContext(ctx).Where("is_active = ?", true).First(&cfg).Error; err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Create 创建模型配置
func (r *AIModelConfigRepo) Create(ctx context.Context, cfg *model.AIModelConfig, tx *gorm.DB) error {
	return r.getDB(tx).WithContext(ctx).Create(cfg).Error
}

// Update 更新模型配置
func (r *AIModelConfigRepo) Update(ctx context.Context, cfg *model.AIModelConfig, tx *gorm.DB) error {
	return r.getDB(tx).WithContext(ctx).Save(cfg).Error
}

// Delete 删除模型配置
func (r *AIModelConfigRepo) Delete(ctx context.Context, id uint, tx *gorm.DB) error {
	return r.getDB(tx).WithContext(ctx).Delete(&model.AIModelConfig{}, id).Error
}

// ClearActive 将所有模型设为非启用（事务内使用）
func (r *AIModelConfigRepo) ClearActive(ctx context.Context, tx *gorm.DB) error {
	return r.getDB(tx).WithContext(ctx).Model(&model.AIModelConfig{}).Where("is_active = ?", true).Update("is_active", false).Error
}

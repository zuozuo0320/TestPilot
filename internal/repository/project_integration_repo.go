// project_integration_repo.go — 项目外部集成配置数据访问层
package repository

import (
	"context"

	"gorm.io/gorm"

	"testpilot/internal/model"
)

// ProjectIntegrationRepository 项目外部集成配置仓库接口。
type ProjectIntegrationRepository interface {
	FindByProvider(ctx context.Context, projectID uint, provider string) (*model.ProjectIntegration, error)
	Upsert(ctx context.Context, tx *gorm.DB, integration *model.ProjectIntegration) error
}

// projectIntegrationRepo ProjectIntegrationRepository 的 GORM 实现。
type projectIntegrationRepo struct {
	db *gorm.DB
}

// NewProjectIntegrationRepo 创建项目外部集成配置仓库。
func NewProjectIntegrationRepo(db *gorm.DB) ProjectIntegrationRepository {
	return &projectIntegrationRepo{db: db}
}

// getDB 事务连接助手。
func (r *projectIntegrationRepo) getDB(tx *gorm.DB) *gorm.DB {
	if tx != nil {
		return tx
	}
	return r.db
}

// FindByProvider 按项目和外部系统查询集成配置。
func (r *projectIntegrationRepo) FindByProvider(ctx context.Context, projectID uint, provider string) (*model.ProjectIntegration, error) {
	var integration model.ProjectIntegration
	if err := r.db.WithContext(ctx).
		Where("project_id = ? AND provider = ?", projectID, provider).
		First(&integration).Error; err != nil {
		return nil, err
	}
	return &integration, nil
}

// Upsert 创建或更新项目外部集成配置。
func (r *projectIntegrationRepo) Upsert(ctx context.Context, tx *gorm.DB, integration *model.ProjectIntegration) error {
	db := r.getDB(tx).WithContext(ctx)
	var existing model.ProjectIntegration
	err := db.Where("project_id = ? AND provider = ?", integration.ProjectID, integration.Provider).
		First(&existing).Error
	if err == nil {
		integration.ID = existing.ID
		return db.Model(&model.ProjectIntegration{}).
			Where("id = ?", existing.ID).
			Updates(map[string]any{
				"base_url":        integration.BaseURL,
				"project_path":    integration.ProjectPath,
				"encrypted_token": integration.EncryptedToken,
				"token_mask":      integration.TokenMask,
				"enabled":         integration.Enabled,
				"updated_by":      integration.UpdatedBy,
			}).Error
	}
	if err != nil && err != gorm.ErrRecordNotFound {
		return err
	}
	return db.Create(integration).Error
}

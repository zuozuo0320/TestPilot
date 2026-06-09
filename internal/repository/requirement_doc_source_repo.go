// requirement_doc_source_repo.go — 需求文档外部来源数据访问层
package repository

import (
	"context"

	"gorm.io/gorm"

	"testpilot/internal/model"
)

// RequirementDocSourceRepository 需求文档外部来源仓库接口。
type RequirementDocSourceRepository interface {
	Create(ctx context.Context, tx *gorm.DB, source *model.RequirementDocSource) error
	FindLatestByExternalKey(ctx context.Context, projectID uint, sourceType, externalKey string) (*model.RequirementDocSource, error)
	ListByDocIDs(ctx context.Context, docIDs []uint) ([]model.RequirementDocSource, error)
}

// requirementDocSourceRepo RequirementDocSourceRepository 的 GORM 实现。
type requirementDocSourceRepo struct {
	db *gorm.DB
}

// NewRequirementDocSourceRepo 创建需求文档外部来源仓库。
func NewRequirementDocSourceRepo(db *gorm.DB) RequirementDocSourceRepository {
	return &requirementDocSourceRepo{db: db}
}

// getDB 事务连接助手。
func (r *requirementDocSourceRepo) getDB(tx *gorm.DB) *gorm.DB {
	if tx != nil {
		return tx
	}
	return r.db
}

// Create 在事务中创建需求文档外部来源记录。
func (r *requirementDocSourceRepo) Create(ctx context.Context, tx *gorm.DB, source *model.RequirementDocSource) error {
	return r.getDB(tx).WithContext(ctx).Create(source).Error
}

// FindLatestByExternalKey 查询同一外部来源的最新文档版本。
func (r *requirementDocSourceRepo) FindLatestByExternalKey(ctx context.Context, projectID uint, sourceType, externalKey string) (*model.RequirementDocSource, error) {
	var source model.RequirementDocSource
	if err := r.db.WithContext(ctx).
		Where("project_id = ? AND source_type = ? AND external_key = ?", projectID, sourceType, externalKey).
		Order("version_no DESC, id DESC").
		First(&source).Error; err != nil {
		return nil, err
	}
	return &source, nil
}

// ListByDocIDs 批量查询需求文档来源记录。
func (r *requirementDocSourceRepo) ListByDocIDs(ctx context.Context, docIDs []uint) ([]model.RequirementDocSource, error) {
	if len(docIDs) == 0 {
		return []model.RequirementDocSource{}, nil
	}
	var sources []model.RequirementDocSource
	if err := r.db.WithContext(ctx).
		Where("requirement_doc_id IN ?", docIDs).
		Find(&sources).Error; err != nil {
		return nil, err
	}
	return sources, nil
}

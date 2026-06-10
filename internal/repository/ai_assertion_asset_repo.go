// ai_assertion_asset_repo.go — 测试智编断言资产数据访问层
package repository

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"testpilot/internal/model"
)

// AIAssertionAssetFilter 断言资产列表筛选条件。
type AIAssertionAssetFilter struct {
	ProjectID uint
	Keyword   string
	Status    string
	Type      string
	Page      int
	PageSize  int
}

// AIAssertionAssetRepo 断言资产 Repository。
type AIAssertionAssetRepo struct {
	db *gorm.DB
}

// NewAIAssertionAssetRepo 创建断言资产 Repository。
func NewAIAssertionAssetRepo(db *gorm.DB) *AIAssertionAssetRepo {
	return &AIAssertionAssetRepo{db: db}
}

func (r *AIAssertionAssetRepo) getDB(tx *gorm.DB) *gorm.DB {
	if tx != nil {
		return tx
	}
	return r.db
}

func (r *AIAssertionAssetRepo) buildListQuery(ctx context.Context, filter AIAssertionAssetFilter) *gorm.DB {
	q := r.db.WithContext(ctx).Model(&model.AIAssertionAsset{})
	if filter.ProjectID > 0 {
		q = q.Where("project_id = ?", filter.ProjectID)
	}
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		like := "%" + keyword + "%"
		q = q.Where("assertion_name LIKE ? OR assertion_key LIKE ? OR description LIKE ?", like, like, like)
	}
	if status := strings.TrimSpace(filter.Status); status != "" {
		q = q.Where("status = ?", status)
	}
	if assertionType := strings.TrimSpace(filter.Type); assertionType != "" {
		q = q.Where("assertion_type = ?", assertionType)
	}
	return q
}

// List 分页查询断言资产。
func (r *AIAssertionAssetRepo) List(ctx context.Context, filter AIAssertionAssetFilter) ([]model.AIAssertionAsset, int64, error) {
	q := r.buildListQuery(ctx, filter)

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var assertions []model.AIAssertionAsset
	offset := (filter.Page - 1) * filter.PageSize
	err := q.Order("updated_at DESC, id DESC").
		Offset(offset).
		Limit(filter.PageSize).
		Find(&assertions).Error
	return assertions, total, err
}

// ListAllByProject 查询项目内全部断言资产，用于资产库和 AI 推荐。
func (r *AIAssertionAssetRepo) ListAllByProject(ctx context.Context, projectID uint, publishedOnly bool, allowAIReuseOnly bool) ([]model.AIAssertionAsset, error) {
	q := r.db.WithContext(ctx).Model(&model.AIAssertionAsset{}).
		Where("project_id = ?", projectID)
	if publishedOnly {
		q = q.Where("status = ?", model.AIAssertionAssetStatusPublished)
	}
	if allowAIReuseOnly {
		q = q.Where("allow_ai_reuse = ?", true)
	}
	var assertions []model.AIAssertionAsset
	err := q.Order("updated_at DESC, id DESC").Find(&assertions).Error
	return assertions, err
}

// GetByID 按 ID 查询断言资产。
func (r *AIAssertionAssetRepo) GetByID(ctx context.Context, id uint) (*model.AIAssertionAsset, error) {
	var assertion model.AIAssertionAsset
	if err := r.db.WithContext(ctx).First(&assertion, id).Error; err != nil {
		return nil, err
	}
	return &assertion, nil
}

// GetByProjectAndKey 按项目和稳定 Key 查询断言资产。
func (r *AIAssertionAssetRepo) GetByProjectAndKey(ctx context.Context, projectID uint, assertionKey string) (*model.AIAssertionAsset, error) {
	var assertion model.AIAssertionAsset
	err := r.db.WithContext(ctx).
		Where("project_id = ? AND assertion_key = ?", projectID, assertionKey).
		First(&assertion).Error
	if err != nil {
		return nil, err
	}
	return &assertion, nil
}

// Create 在事务中创建断言资产。
func (r *AIAssertionAssetRepo) Create(ctx context.Context, tx *gorm.DB, assertion *model.AIAssertionAsset) error {
	return r.getDB(tx).WithContext(ctx).Create(assertion).Error
}

// UpdateFields 在事务中更新断言资产字段。
func (r *AIAssertionAssetRepo) UpdateFields(ctx context.Context, tx *gorm.DB, id uint, fields map[string]interface{}) error {
	return r.getDB(tx).WithContext(ctx).
		Model(&model.AIAssertionAsset{}).
		Where("id = ?", id).
		Updates(fields).Error
}

// Delete 在事务中物理删除断言草稿。
func (r *AIAssertionAssetRepo) Delete(ctx context.Context, tx *gorm.DB, id uint) error {
	return r.getDB(tx).WithContext(ctx).Delete(&model.AIAssertionAsset{}, id).Error
}

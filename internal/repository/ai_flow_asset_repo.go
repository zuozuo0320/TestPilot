// ai_flow_asset_repo.go — 测试智编固定场景资产数据访问层
package repository

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"testpilot/internal/model"
)

// AIFlowAssetFilter 固定场景列表筛选条件。
type AIFlowAssetFilter struct {
	ProjectID        uint
	Keyword          string
	Status           string
	ValidationStatus string
	Page             int
	PageSize         int
}

// AIFlowAssetRepo 固定场景资产 Repository。
type AIFlowAssetRepo struct {
	db *gorm.DB
}

// NewAIFlowAssetRepo 创建固定场景资产 Repository。
func NewAIFlowAssetRepo(db *gorm.DB) *AIFlowAssetRepo {
	return &AIFlowAssetRepo{db: db}
}

func (r *AIFlowAssetRepo) getDB(tx *gorm.DB) *gorm.DB {
	if tx != nil {
		return tx
	}
	return r.db
}

func (r *AIFlowAssetRepo) buildListQuery(ctx context.Context, filter AIFlowAssetFilter) *gorm.DB {
	q := r.db.WithContext(ctx).Model(&model.AIFlowAsset{})
	if filter.ProjectID > 0 {
		q = q.Where("project_id = ?", filter.ProjectID)
	}
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		like := "%" + keyword + "%"
		q = q.Where("flow_name LIKE ? OR flow_key LIKE ? OR description LIKE ?", like, like, like)
	}
	if status := strings.TrimSpace(filter.Status); status != "" {
		q = q.Where("status = ?", status)
	}
	if validationStatus := strings.TrimSpace(filter.ValidationStatus); validationStatus != "" {
		q = q.Where("latest_validation_status = ?", validationStatus)
	}
	return q
}

// ListAllByProject 查询项目内全部固定场景，用于 AI 规划和编排资产库。
func (r *AIFlowAssetRepo) ListAllByProject(ctx context.Context, projectID uint, publishedOnly bool, allowAIReuseOnly bool) ([]model.AIFlowAsset, error) {
	q := r.db.WithContext(ctx).Model(&model.AIFlowAsset{}).
		Where("project_id = ?", projectID)
	if publishedOnly {
		q = q.Where("status = ?", model.AIFlowAssetStatusPublished)
	}
	if allowAIReuseOnly {
		q = q.Where("allow_ai_reuse = ?", true)
	}
	var flows []model.AIFlowAsset
	err := q.Order("updated_at DESC, id DESC").Find(&flows).Error
	return flows, err
}

// List 分页查询固定场景资产。
func (r *AIFlowAssetRepo) List(ctx context.Context, filter AIFlowAssetFilter) ([]model.AIFlowAsset, int64, error) {
	q := r.buildListQuery(ctx, filter)

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var flows []model.AIFlowAsset
	offset := (filter.Page - 1) * filter.PageSize
	err := q.Order("updated_at DESC, id DESC").
		Offset(offset).
		Limit(filter.PageSize).
		Find(&flows).Error
	return flows, total, err
}

// GetByID 按 ID 查询固定场景资产。
func (r *AIFlowAssetRepo) GetByID(ctx context.Context, id uint) (*model.AIFlowAsset, error) {
	var flow model.AIFlowAsset
	if err := r.db.WithContext(ctx).First(&flow, id).Error; err != nil {
		return nil, err
	}
	return &flow, nil
}

// GetByProjectAndKey 按项目和稳定 Key 查询固定场景。
func (r *AIFlowAssetRepo) GetByProjectAndKey(ctx context.Context, projectID uint, flowKey string) (*model.AIFlowAsset, error) {
	var flow model.AIFlowAsset
	err := r.db.WithContext(ctx).
		Where("project_id = ? AND flow_key = ?", projectID, flowKey).
		First(&flow).Error
	if err != nil {
		return nil, err
	}
	return &flow, nil
}

// Create 在事务中创建固定场景资产。
func (r *AIFlowAssetRepo) Create(ctx context.Context, tx *gorm.DB, flow *model.AIFlowAsset) error {
	return r.getDB(tx).WithContext(ctx).Create(flow).Error
}

// UpdateFields 在事务中更新固定场景资产字段。
func (r *AIFlowAssetRepo) UpdateFields(ctx context.Context, tx *gorm.DB, id uint, fields map[string]interface{}) error {
	return r.getDB(tx).WithContext(ctx).
		Model(&model.AIFlowAsset{}).
		Where("id = ?", id).
		Updates(fields).Error
}

// Delete 在事务中物理删除固定场景草稿。
func (r *AIFlowAssetRepo) Delete(ctx context.Context, tx *gorm.DB, id uint) error {
	return r.getDB(tx).WithContext(ctx).Delete(&model.AIFlowAsset{}, id).Error
}

// CreateVersion 在事务中创建固定场景版本。
func (r *AIFlowAssetRepo) CreateVersion(ctx context.Context, tx *gorm.DB, version *model.AIFlowAssetVersion) error {
	return r.getDB(tx).WithContext(ctx).Create(version).Error
}

// MaxVersionNo 查询固定场景最大版本号。
func (r *AIFlowAssetRepo) MaxVersionNo(ctx context.Context, flowID uint) (int, error) {
	var maxNo *int
	err := r.db.WithContext(ctx).Model(&model.AIFlowAssetVersion{}).
		Where("flow_id = ?", flowID).
		Select("MAX(version_no)").Scan(&maxNo).Error
	if err != nil {
		return 0, err
	}
	if maxNo == nil {
		return 0, nil
	}
	return *maxNo, nil
}

// GetLatestPublishedVersion 查询固定场景最近发布版本。
func (r *AIFlowAssetRepo) GetLatestPublishedVersion(ctx context.Context, flowID uint) (*model.AIFlowAssetVersion, error) {
	var version model.AIFlowAssetVersion
	err := r.db.WithContext(ctx).
		Where("flow_id = ? AND version_status = ?", flowID, model.AIFlowAssetStatusPublished).
		Order("version_no DESC").
		First(&version).Error
	if err != nil {
		return nil, err
	}
	return &version, nil
}

// GetVersionByID 按 ID 查询固定场景版本。
func (r *AIFlowAssetRepo) GetVersionByID(ctx context.Context, versionID uint) (*model.AIFlowAssetVersion, error) {
	var version model.AIFlowAssetVersion
	err := r.db.WithContext(ctx).First(&version, versionID).Error
	if err != nil {
		return nil, err
	}
	return &version, nil
}

// ListVersions 查询固定场景版本列表。
func (r *AIFlowAssetRepo) ListVersions(ctx context.Context, flowID uint) ([]model.AIFlowAssetVersion, error) {
	var versions []model.AIFlowAssetVersion
	err := r.db.WithContext(ctx).
		Where("flow_id = ?", flowID).
		Order("version_no DESC").
		Find(&versions).Error
	return versions, err
}

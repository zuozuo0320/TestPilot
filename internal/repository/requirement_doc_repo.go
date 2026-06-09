// requirement_doc_repo.go — 需求文档数据访问层
//
// 提供需求文档表的 CRUD 操作，包括：
//   - 文档创建（上传/粘贴）
//   - 分页列表（含任务数、用例数统计）
//   - CAS 状态推进（解析状态）
//   - 软删除
//   - stuck 文档扫描（兜底 worker 用）
//
// ❗ 在事务内调用的方法必须接受 tx *gorm.DB 参数，
// 通过 getDB(tx) 助手函数决定使用事务连接还是默认连接。
package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"testpilot/internal/model"
)

// RequirementDocFilter 需求文档列表筛选参数
type RequirementDocFilter struct {
	Keyword     string // 标题模糊搜索
	ParseStatus string // 单值或逗号分隔
	SourceType  string // upload_file / paste_text
	CreatedBy   uint   // 上传人筛选
	Page        int
	PageSize    int
}

// RequirementDocRepository 需求文档数据访问层接口
type RequirementDocRepository interface {
	Create(ctx context.Context, doc *model.RequirementDoc) error
	CreateTx(ctx context.Context, tx *gorm.DB, doc *model.RequirementDoc) error
	FindByID(ctx context.Context, id uint) (*model.RequirementDoc, error)
	FindByIDForUpdate(ctx context.Context, tx *gorm.DB, id uint) (*model.RequirementDoc, error)
	Update(ctx context.Context, doc *model.RequirementDoc) error
	UpdateFields(ctx context.Context, tx *gorm.DB, id uint, fields map[string]interface{}) error
	SoftDelete(ctx context.Context, id uint) error
	ListPaged(ctx context.Context, projectID uint, f RequirementDocFilter) ([]model.RequirementDoc, int64, error)

	// CAS 状态推进：解析状态变更
	CASParseStatus(ctx context.Context, id uint, fromStatuses []string, toStatus string, extraFields map[string]interface{}) (int64, error)

	// stuck 文档扫描：查找 parsing 超时的文档
	FindStuckParsing(ctx context.Context, timeout time.Duration, limit int) ([]model.RequirementDoc, error)

	// 统计：按项目统计文档数
	CountByProject(ctx context.Context, projectID uint) (int64, error)
}

// requirementDocRepo RequirementDocRepository 的 GORM 实现
type requirementDocRepo struct {
	db *gorm.DB
}

// NewRequirementDocRepo 创建需求文档仓库实例
func NewRequirementDocRepo(db *gorm.DB) RequirementDocRepository {
	return &requirementDocRepo{db: db}
}

// getDB 事务连接助手
func (r *requirementDocRepo) getDB(tx *gorm.DB) *gorm.DB {
	if tx != nil {
		return tx
	}
	return r.db
}

// Create 创建需求文档记录
func (r *requirementDocRepo) Create(ctx context.Context, doc *model.RequirementDoc) error {
	return r.db.WithContext(ctx).Create(doc).Error
}

// CreateTx 在事务中创建需求文档记录
func (r *requirementDocRepo) CreateTx(ctx context.Context, tx *gorm.DB, doc *model.RequirementDoc) error {
	return r.getDB(tx).WithContext(ctx).Create(doc).Error
}

// FindByID 按 ID 查询文档（排除软删）
func (r *requirementDocRepo) FindByID(ctx context.Context, id uint) (*model.RequirementDoc, error) {
	var doc model.RequirementDoc
	if err := r.db.WithContext(ctx).First(&doc, id).Error; err != nil {
		return nil, err
	}
	return &doc, nil
}

// FindByIDForUpdate 行锁查询，用于事务内防并发（SELECT ... FOR UPDATE）
func (r *requirementDocRepo) FindByIDForUpdate(ctx context.Context, tx *gorm.DB, id uint) (*model.RequirementDoc, error) {
	var doc model.RequirementDoc
	err := r.getDB(tx).WithContext(ctx).
		Set("gorm:query_option", "FOR UPDATE").
		First(&doc, id).Error
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

// Update 全字段更新文档
func (r *requirementDocRepo) Update(ctx context.Context, doc *model.RequirementDoc) error {
	return r.db.WithContext(ctx).Save(doc).Error
}

// UpdateFields 按字段更新文档（支持事务）
func (r *requirementDocRepo) UpdateFields(ctx context.Context, tx *gorm.DB, id uint, fields map[string]interface{}) error {
	return r.getDB(tx).WithContext(ctx).
		Model(&model.RequirementDoc{}).
		Where("id = ?", id).
		Updates(fields).Error
}

// SoftDelete 软删除文档
func (r *requirementDocRepo) SoftDelete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&model.RequirementDoc{}, id).Error
}

// ListPaged 分页查询需求文档列表
func (r *requirementDocRepo) ListPaged(ctx context.Context, projectID uint, f RequirementDocFilter) ([]model.RequirementDoc, int64, error) {
	query := r.db.WithContext(ctx).
		Model(&model.RequirementDoc{}).
		Where("project_id = ?", projectID)

	// 筛选条件
	if f.Keyword != "" {
		query = query.Where("title LIKE ?", "%"+f.Keyword+"%")
	}
	if f.ParseStatus != "" {
		query = query.Where("parse_status = ?", f.ParseStatus)
	}
	if f.SourceType != "" {
		query = query.Where("source_type = ?", f.SourceType)
	}
	if f.CreatedBy > 0 {
		query = query.Where("created_by = ?", f.CreatedBy)
	}

	// 计数
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 分页查询
	var docs []model.RequirementDoc
	offset := (f.Page - 1) * f.PageSize
	err := query.
		Order("created_at DESC").
		Offset(offset).
		Limit(f.PageSize).
		Find(&docs).Error
	if err != nil {
		return nil, 0, err
	}

	return docs, total, nil
}

// CASParseStatus CAS 推进解析状态。
// 仅在当前状态属于 fromStatuses 时才更新，返回影响行数。
// affected_rows=0 表示被其他节点抢先处理。
func (r *requirementDocRepo) CASParseStatus(ctx context.Context, id uint, fromStatuses []string, toStatus string, extraFields map[string]interface{}) (int64, error) {
	updates := map[string]interface{}{
		"parse_status": toStatus,
		"updated_at":   time.Now(),
		"lock_version": gorm.Expr("lock_version + 1"),
	}
	for k, v := range extraFields {
		updates[k] = v
	}

	result := r.db.WithContext(ctx).
		Model(&model.RequirementDoc{}).
		Where("id = ? AND parse_status IN ?", id, fromStatuses).
		Updates(updates)

	return result.RowsAffected, result.Error
}

// FindStuckParsing 查找卡在 parsing 状态超时的文档（兜底 worker 扫描）
func (r *requirementDocRepo) FindStuckParsing(ctx context.Context, timeout time.Duration, limit int) ([]model.RequirementDoc, error) {
	cutoff := time.Now().Add(-timeout)
	var docs []model.RequirementDoc
	err := r.db.WithContext(ctx).
		Where("parse_status = ? AND parse_started_at < ?", model.DocParseStatusParsing, cutoff).
		Limit(limit).
		Find(&docs).Error
	return docs, err
}

// CountByProject 统计项目下文档数量
func (r *requirementDocRepo) CountByProject(ctx context.Context, projectID uint) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&model.RequirementDoc{}).
		Where("project_id = ?", projectID).
		Count(&count).Error
	return count, err
}

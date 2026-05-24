// requirement_gen_task_repo.go — 需求智生-生成任务数据访问层
//
// 提供生成任务表的 CRUD 操作，包括：
//   - 任务创建
//   - 分页列表（含文档标题、Skill 名称、创建人）
//   - CAS 状态推进
//   - 心跳更新
//   - 超时任务扫描（兜底 worker 用）
//   - 统计：项目活跃任务数（并发配额检查）
//
// ❗ 事务内调用的方法必须接受 tx *gorm.DB 参数。
package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"testpilot/internal/model"
)

// RequirementGenTaskFilter 生成任务列表筛选参数
type RequirementGenTaskFilter struct {
	Status           string // 单值或逗号分隔
	RequirementDocID uint   // 按文档筛选
	SkillID          uint   // 按 Skill 筛选
	CreatedBy        uint   // 创建人筛选
	Page             int
	PageSize         int
}

// RequirementGenTaskRepository 生成任务数据访问层接口
type RequirementGenTaskRepository interface {
	Create(ctx context.Context, task *model.RequirementGenTask) error
	FindByID(ctx context.Context, id uint) (*model.RequirementGenTask, error)
	FindByIDForUpdate(ctx context.Context, tx *gorm.DB, id uint) (*model.RequirementGenTask, error)
	Update(ctx context.Context, task *model.RequirementGenTask) error
	UpdateFields(ctx context.Context, tx *gorm.DB, id uint, fields map[string]interface{}) error
	ListPaged(ctx context.Context, projectID uint, f RequirementGenTaskFilter) ([]model.RequirementGenTask, int64, error)

	// CAS 状态推进：UPDATE ... WHERE status IN (?) AND lock_version = ?
	CASStatus(ctx context.Context, id uint, fromStatuses []string, lockVersion int, toStatus string, extraFields map[string]interface{}) (int64, error)

	// 心跳更新：由 Executor 定期调用
	UpdateHeartbeat(ctx context.Context, id uint) error

	// 超时任务扫描：查找 RUNNING 但心跳超时的任务
	FindStuckRunning(ctx context.Context, timeout time.Duration, limit int) ([]model.RequirementGenTask, error)

	// 统计：项目内活跃任务数（PENDING + RUNNING）
	CountActiveByProject(ctx context.Context, projectID uint) (int64, error)

	// 统计：全局活跃任务数（PENDING + RUNNING）
	CountActiveGlobal(ctx context.Context) (int64, error)

	// 更新产物计数（generated_count / adopted_count / discarded_count）
	IncrAdoptedCount(ctx context.Context, tx *gorm.DB, id uint, delta int) error
	IncrDiscardedCount(ctx context.Context, tx *gorm.DB, id uint, delta int) error
}

// requirementGenTaskRepo RequirementGenTaskRepository 的 GORM 实现
type requirementGenTaskRepo struct {
	db *gorm.DB
}

// NewRequirementGenTaskRepo 创建生成任务仓库实例
func NewRequirementGenTaskRepo(db *gorm.DB) RequirementGenTaskRepository {
	return &requirementGenTaskRepo{db: db}
}

// getDB 事务连接助手
func (r *requirementGenTaskRepo) getDB(tx *gorm.DB) *gorm.DB {
	if tx != nil {
		return tx
	}
	return r.db
}

// Create 创建生成任务
func (r *requirementGenTaskRepo) Create(ctx context.Context, task *model.RequirementGenTask) error {
	return r.db.WithContext(ctx).Create(task).Error
}

// FindByID 按 ID 查询任务
func (r *requirementGenTaskRepo) FindByID(ctx context.Context, id uint) (*model.RequirementGenTask, error) {
	var task model.RequirementGenTask
	if err := r.db.WithContext(ctx).First(&task, id).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

// FindByIDForUpdate 行锁查询任务（SELECT ... FOR UPDATE）
func (r *requirementGenTaskRepo) FindByIDForUpdate(ctx context.Context, tx *gorm.DB, id uint) (*model.RequirementGenTask, error) {
	var task model.RequirementGenTask
	err := r.getDB(tx).WithContext(ctx).
		Set("gorm:query_option", "FOR UPDATE").
		First(&task, id).Error
	if err != nil {
		return nil, err
	}
	return &task, nil
}

// Update 全字段更新任务
func (r *requirementGenTaskRepo) Update(ctx context.Context, task *model.RequirementGenTask) error {
	return r.db.WithContext(ctx).Save(task).Error
}

// UpdateFields 按字段更新任务（支持事务）
func (r *requirementGenTaskRepo) UpdateFields(ctx context.Context, tx *gorm.DB, id uint, fields map[string]interface{}) error {
	return r.getDB(tx).WithContext(ctx).
		Model(&model.RequirementGenTask{}).
		Where("id = ?", id).
		Updates(fields).Error
}

// ListPaged 分页查询生成任务列表
func (r *requirementGenTaskRepo) ListPaged(ctx context.Context, projectID uint, f RequirementGenTaskFilter) ([]model.RequirementGenTask, int64, error) {
	query := r.db.WithContext(ctx).
		Model(&model.RequirementGenTask{}).
		Where("project_id = ?", projectID)

	if f.Status != "" {
		query = query.Where("status = ?", f.Status)
	}
	if f.RequirementDocID > 0 {
		query = query.Where("requirement_doc_id = ?", f.RequirementDocID)
	}
	if f.SkillID > 0 {
		query = query.Where("skill_id = ?", f.SkillID)
	}
	if f.CreatedBy > 0 {
		query = query.Where("created_by = ?", f.CreatedBy)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var tasks []model.RequirementGenTask
	offset := (f.Page - 1) * f.PageSize
	err := query.
		Order("created_at DESC").
		Offset(offset).
		Limit(f.PageSize).
		Find(&tasks).Error
	if err != nil {
		return nil, 0, err
	}

	return tasks, total, nil
}

// CASStatus CAS 状态推进：仅当前状态属于 fromStatuses 且 lock_version 匹配时才更新。
// 返回影响行数，affected_rows=0 表示并发冲突。
func (r *requirementGenTaskRepo) CASStatus(ctx context.Context, id uint, fromStatuses []string, lockVersion int, toStatus string, extraFields map[string]interface{}) (int64, error) {
	updates := map[string]interface{}{
		"status":       toStatus,
		"updated_at":   time.Now(),
		"lock_version": gorm.Expr("lock_version + 1"),
	}
	for k, v := range extraFields {
		updates[k] = v
	}

	result := r.db.WithContext(ctx).
		Model(&model.RequirementGenTask{}).
		Where("id = ? AND status IN ? AND lock_version = ?", id, fromStatuses, lockVersion).
		Updates(updates)

	return result.RowsAffected, result.Error
}

// UpdateHeartbeat 更新心跳时间戳
func (r *requirementGenTaskRepo) UpdateHeartbeat(ctx context.Context, id uint) error {
	now := time.Now()
	return r.db.WithContext(ctx).
		Model(&model.RequirementGenTask{}).
		Where("id = ? AND status = ?", id, model.GenTaskStatusRunning).
		Update("last_heartbeat_at", now).Error
}

// FindStuckRunning 查找 RUNNING 但心跳超时的任务（兜底 worker）
func (r *requirementGenTaskRepo) FindStuckRunning(ctx context.Context, timeout time.Duration, limit int) ([]model.RequirementGenTask, error) {
	cutoff := time.Now().Add(-timeout)
	var tasks []model.RequirementGenTask
	err := r.db.WithContext(ctx).
		Where("status = ? AND last_heartbeat_at < ?", model.GenTaskStatusRunning, cutoff).
		Limit(limit).
		Find(&tasks).Error
	return tasks, err
}

// CountActiveByProject 统计项目内活跃任务数（PENDING + RUNNING）
func (r *requirementGenTaskRepo) CountActiveByProject(ctx context.Context, projectID uint) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&model.RequirementGenTask{}).
		Where("project_id = ? AND status IN ?", projectID, []string{model.GenTaskStatusPending, model.GenTaskStatusRunning}).
		Count(&count).Error
	return count, err
}

// CountActiveGlobal 统计全局活跃任务数（PENDING + RUNNING）
func (r *requirementGenTaskRepo) CountActiveGlobal(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&model.RequirementGenTask{}).
		Where("status IN ?", []string{model.GenTaskStatusPending, model.GenTaskStatusRunning}).
		Count(&count).Error
	return count, err
}

// IncrAdoptedCount 原子递增 adopted_count
func (r *requirementGenTaskRepo) IncrAdoptedCount(ctx context.Context, tx *gorm.DB, id uint, delta int) error {
	return r.getDB(tx).WithContext(ctx).
		Model(&model.RequirementGenTask{}).
		Where("id = ?", id).
		Update("adopted_count", gorm.Expr("adopted_count + ?", delta)).Error
}

// IncrDiscardedCount 原子递增 discarded_count
func (r *requirementGenTaskRepo) IncrDiscardedCount(ctx context.Context, tx *gorm.DB, id uint, delta int) error {
	return r.getDB(tx).WithContext(ctx).
		Model(&model.RequirementGenTask{}).
		Where("id = ?", id).
		Update("discarded_count", gorm.Expr("discarded_count + ?", delta)).Error
}

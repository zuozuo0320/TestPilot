// requirement_gen_result_repo.go — 需求智生-AI 产物数据访问层
//
// 提供 AI 产物（预览池）的数据库操作，包括：
//   - 批量创建（回调写入）
//   - 按任务查询全部产物
//   - 单条采纳 / 丢弃（CAS 更新）
//   - 批量采纳
//   - 按任务统计已采纳 / 已丢弃数量
//
// ❗ 事务内调用的方法必须接受 tx *gorm.DB 参数。
package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"testpilot/internal/model"
)

// RequirementGenResultRepository AI 产物数据访问层接口
type RequirementGenResultRepository interface {
	// 批量创建产物（Executor 回调时写入）
	BatchCreate(ctx context.Context, tx *gorm.DB, results []model.RequirementGenResult) error

	// 按任务 ID 查询全部产物（按 seq_no 升序）
	ListByTaskID(ctx context.Context, taskID uint) ([]model.RequirementGenResult, error)

	// 按 ID 查询单条产物
	FindByID(ctx context.Context, id uint) (*model.RequirementGenResult, error)

	// 按 ID 行锁查询（用于采纳/丢弃的事务保护）
	FindByIDForUpdate(ctx context.Context, tx *gorm.DB, id uint) (*model.RequirementGenResult, error)

	// CAS 采纳：仅未采纳且未丢弃时可操作
	CASAdopt(ctx context.Context, tx *gorm.DB, id uint, lockVersion int, adoptedBy uint, adoptedCaseID uint) (int64, error)

	// CAS 丢弃：仅未采纳且未丢弃时可操作
	CASDiscard(ctx context.Context, tx *gorm.DB, id uint, lockVersion int, discardedBy uint) (int64, error)

	// 批量标记丢弃（用于任务关闭时丢弃所有 pending 产物）
	BatchDiscardByTaskID(ctx context.Context, tx *gorm.DB, taskID uint, discardedBy uint) (int64, error)

	// 按任务 ID 删除产物
	DeleteByTaskID(ctx context.Context, tx *gorm.DB, taskID uint) error

	// 统计：按任务 ID 查询各状态数量
	CountByTaskID(ctx context.Context, taskID uint) (total int64, adopted int64, discarded int64, err error)

	// 按用例 ID 查询是否有产物指向该用例（溯源查询）
	FindByAdoptedCaseID(ctx context.Context, caseID uint) (*model.RequirementGenResult, error)
}

// requirementGenResultRepo RequirementGenResultRepository 的 GORM 实现
type requirementGenResultRepo struct {
	db *gorm.DB
}

// NewRequirementGenResultRepo 创建 AI 产物仓库实例
func NewRequirementGenResultRepo(db *gorm.DB) RequirementGenResultRepository {
	return &requirementGenResultRepo{db: db}
}

// getDB 事务连接助手
func (r *requirementGenResultRepo) getDB(tx *gorm.DB) *gorm.DB {
	if tx != nil {
		return tx
	}
	return r.db
}

// BatchCreate 批量创建产物
func (r *requirementGenResultRepo) BatchCreate(ctx context.Context, tx *gorm.DB, results []model.RequirementGenResult) error {
	if len(results) == 0 {
		return nil
	}
	return r.getDB(tx).WithContext(ctx).Create(&results).Error
}

// ListByTaskID 按任务 ID 查询全部产物
func (r *requirementGenResultRepo) ListByTaskID(ctx context.Context, taskID uint) ([]model.RequirementGenResult, error) {
	var results []model.RequirementGenResult
	err := r.db.WithContext(ctx).
		Where("task_id = ?", taskID).
		Order("seq_no ASC").
		Find(&results).Error
	return results, err
}

// FindByID 按 ID 查询单条产物
func (r *requirementGenResultRepo) FindByID(ctx context.Context, id uint) (*model.RequirementGenResult, error) {
	var result model.RequirementGenResult
	if err := r.db.WithContext(ctx).First(&result, id).Error; err != nil {
		return nil, err
	}
	return &result, nil
}

// FindByIDForUpdate 行锁查询产物
func (r *requirementGenResultRepo) FindByIDForUpdate(ctx context.Context, tx *gorm.DB, id uint) (*model.RequirementGenResult, error) {
	var result model.RequirementGenResult
	err := r.getDB(tx).WithContext(ctx).
		Set("gorm:query_option", "FOR UPDATE").
		First(&result, id).Error
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// CASAdopt CAS 采纳产物：仅 adopted=false AND discarded=false AND lock_version 匹配时更新
func (r *requirementGenResultRepo) CASAdopt(ctx context.Context, tx *gorm.DB, id uint, lockVersion int, adoptedBy uint, adoptedCaseID uint) (int64, error) {
	now := time.Now()
	result := r.getDB(tx).WithContext(ctx).
		Model(&model.RequirementGenResult{}).
		Where("id = ? AND adopted = ? AND discarded = ? AND lock_version = ?", id, false, false, lockVersion).
		Updates(map[string]interface{}{
			"adopted":         true,
			"adopted_by":      adoptedBy,
			"adopted_case_id": adoptedCaseID,
			"adopted_at":      now,
			"lock_version":    gorm.Expr("lock_version + 1"),
		})
	return result.RowsAffected, result.Error
}

// CASDiscard CAS 丢弃产物：仅 adopted=false AND discarded=false AND lock_version 匹配时更新
func (r *requirementGenResultRepo) CASDiscard(ctx context.Context, tx *gorm.DB, id uint, lockVersion int, discardedBy uint) (int64, error) {
	now := time.Now()
	result := r.getDB(tx).WithContext(ctx).
		Model(&model.RequirementGenResult{}).
		Where("id = ? AND adopted = ? AND discarded = ? AND lock_version = ?", id, false, false, lockVersion).
		Updates(map[string]interface{}{
			"discarded":    true,
			"discarded_by": discardedBy,
			"discarded_at": now,
			"lock_version": gorm.Expr("lock_version + 1"),
		})
	return result.RowsAffected, result.Error
}

// BatchDiscardByTaskID 批量丢弃任务下所有 pending 产物（任务关闭时使用）
func (r *requirementGenResultRepo) BatchDiscardByTaskID(ctx context.Context, tx *gorm.DB, taskID uint, discardedBy uint) (int64, error) {
	now := time.Now()
	result := r.getDB(tx).WithContext(ctx).
		Model(&model.RequirementGenResult{}).
		Where("task_id = ? AND adopted = ? AND discarded = ?", taskID, false, false).
		Updates(map[string]interface{}{
			"discarded":    true,
			"discarded_by": discardedBy,
			"discarded_at": now,
			"lock_version": gorm.Expr("lock_version + 1"),
		})
	return result.RowsAffected, result.Error
}

// DeleteByTaskID 按任务 ID 删除产物
func (r *requirementGenResultRepo) DeleteByTaskID(ctx context.Context, tx *gorm.DB, taskID uint) error {
	return r.getDB(tx).WithContext(ctx).
		Where("task_id = ?", taskID).
		Delete(&model.RequirementGenResult{}).Error
}

// CountByTaskID 按任务 ID 统计各状态产物数量
func (r *requirementGenResultRepo) CountByTaskID(ctx context.Context, taskID uint) (total int64, adopted int64, discarded int64, err error) {
	err = r.db.WithContext(ctx).
		Model(&model.RequirementGenResult{}).
		Where("task_id = ?", taskID).
		Count(&total).Error
	if err != nil {
		return
	}

	err = r.db.WithContext(ctx).
		Model(&model.RequirementGenResult{}).
		Where("task_id = ? AND adopted = ?", taskID, true).
		Count(&adopted).Error
	if err != nil {
		return
	}

	err = r.db.WithContext(ctx).
		Model(&model.RequirementGenResult{}).
		Where("task_id = ? AND discarded = ?", taskID, true).
		Count(&discarded).Error
	return
}

// FindByAdoptedCaseID 按已采纳的用例 ID 查询关联产物（溯源）
func (r *requirementGenResultRepo) FindByAdoptedCaseID(ctx context.Context, caseID uint) (*model.RequirementGenResult, error) {
	var result model.RequirementGenResult
	err := r.db.WithContext(ctx).
		Where("adopted_case_id = ? AND adopted = ?", caseID, true).
		First(&result).Error
	if err != nil {
		return nil, err
	}
	return &result, nil
}

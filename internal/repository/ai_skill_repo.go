// ai_skill_repo.go — 需求智生-Skill 模板数据访问层
//
// 提供 AI Skill 的数据库操作，包括：
//   - Skill CRUD（系统内置 + 项目级）
//   - 有效 Skill 列表（系统 + 项目覆写合并）
//   - CAS 更新（防并发编辑冲突）
//   - 按 skill_key 查询（覆写逻辑中使用）
//
// ❗ 事务内调用的方法必须接受 tx *gorm.DB 参数。
package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"testpilot/internal/model"
)

// AISkillFilter Skill 列表筛选参数
type AISkillFilter struct {
	Keyword  string // 名称模糊搜索
	Scope    string // 作用域筛选
	IsActive *bool  // 启用状态筛选
	Page     int
	PageSize int
}

// AISkillRepository Skill 模板数据访问层接口
type AISkillRepository interface {
	Create(ctx context.Context, skill *model.AISkill) error
	FindByID(ctx context.Context, id uint) (*model.AISkill, error)
	FindByIDForUpdate(ctx context.Context, tx *gorm.DB, id uint) (*model.AISkill, error)
	Update(ctx context.Context, skill *model.AISkill) error
	SoftDelete(ctx context.Context, id uint) error

	// CAS 更新：防并发编辑冲突
	CASUpdate(ctx context.Context, id uint, lockVersion int, fields map[string]interface{}) (int64, error)

	// 按 project_id + skill_key 精确查询（覆写判断）
	FindByProjectAndKey(ctx context.Context, projectID uint, skillKey string) (*model.AISkill, error)

	// 列出系统内置 Skill (project_id=0, is_active=true)
	ListSystemSkills(ctx context.Context) ([]model.AISkill, error)

	// 列出项目级 Skill（含系统 Skill 被项目覆写的情况）
	ListProjectSkills(ctx context.Context, projectID uint) ([]model.AISkill, error)

	// 分页查询（管理后台使用）
	ListPaged(ctx context.Context, projectID uint, f AISkillFilter) ([]model.AISkill, int64, error)

	// 按 ID 列表批量查询（任务创建校验用）
	FindByIDs(ctx context.Context, ids []uint) ([]model.AISkill, error)
}

// aiSkillRepo AISkillRepository 的 GORM 实现
type aiSkillRepo struct {
	db *gorm.DB
}

// NewAISkillRepo 创建 Skill 仓库实例
func NewAISkillRepo(db *gorm.DB) AISkillRepository {
	return &aiSkillRepo{db: db}
}

// getDB 事务连接助手
func (r *aiSkillRepo) getDB(tx *gorm.DB) *gorm.DB {
	if tx != nil {
		return tx
	}
	return r.db
}

// Create 创建 Skill
func (r *aiSkillRepo) Create(ctx context.Context, skill *model.AISkill) error {
	return r.db.WithContext(ctx).Create(skill).Error
}

// FindByID 按 ID 查询 Skill（排除软删）
func (r *aiSkillRepo) FindByID(ctx context.Context, id uint) (*model.AISkill, error) {
	var skill model.AISkill
	if err := r.db.WithContext(ctx).First(&skill, id).Error; err != nil {
		return nil, err
	}
	return &skill, nil
}

// FindByIDForUpdate 行锁查询 Skill
func (r *aiSkillRepo) FindByIDForUpdate(ctx context.Context, tx *gorm.DB, id uint) (*model.AISkill, error) {
	var skill model.AISkill
	err := r.getDB(tx).WithContext(ctx).
		Set("gorm:query_option", "FOR UPDATE").
		First(&skill, id).Error
	if err != nil {
		return nil, err
	}
	return &skill, nil
}

// Update 全字段更新 Skill
func (r *aiSkillRepo) Update(ctx context.Context, skill *model.AISkill) error {
	return r.db.WithContext(ctx).Save(skill).Error
}

// SoftDelete 软删除 Skill
func (r *aiSkillRepo) SoftDelete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&model.AISkill{}, id).Error
}

// CASUpdate CAS 更新 Skill：仅 lock_version 匹配时更新，返回影响行数
func (r *aiSkillRepo) CASUpdate(ctx context.Context, id uint, lockVersion int, fields map[string]interface{}) (int64, error) {
	fields["updated_at"] = time.Now()
	fields["lock_version"] = gorm.Expr("lock_version + 1")

	result := r.db.WithContext(ctx).
		Model(&model.AISkill{}).
		Where("id = ? AND lock_version = ?", id, lockVersion).
		Updates(fields)
	return result.RowsAffected, result.Error
}

// FindByProjectAndKey 按 project_id + skill_key 精确查询
func (r *aiSkillRepo) FindByProjectAndKey(ctx context.Context, projectID uint, skillKey string) (*model.AISkill, error) {
	var skill model.AISkill
	err := r.db.WithContext(ctx).
		Where("project_id = ? AND skill_key = ?", projectID, skillKey).
		First(&skill).Error
	if err != nil {
		return nil, err
	}
	return &skill, nil
}

// ListSystemSkills 列出所有系统内置启用 Skill
func (r *aiSkillRepo) ListSystemSkills(ctx context.Context) ([]model.AISkill, error) {
	var skills []model.AISkill
	err := r.db.WithContext(ctx).
		Where("project_id = ? AND is_system = ? AND is_active = ?", 0, true, true).
		Order("sort_order ASC, id ASC").
		Find(&skills).Error
	return skills, err
}

// ListProjectSkills 列出项目可用的 Skill 列表。
// 返回系统 Skill + 该项目自建 Skill（已激活的），不含软删除。
func (r *aiSkillRepo) ListProjectSkills(ctx context.Context, projectID uint) ([]model.AISkill, error) {
	var skills []model.AISkill
	err := r.db.WithContext(ctx).
		Where("(project_id = 0 OR project_id = ?) AND is_active = ?", projectID, true).
		Order("sort_order ASC, id ASC").
		Find(&skills).Error
	return skills, err
}

// ListPaged 分页查询 Skill（管理后台用）
func (r *aiSkillRepo) ListPaged(ctx context.Context, projectID uint, f AISkillFilter) ([]model.AISkill, int64, error) {
	query := r.db.WithContext(ctx).
		Model(&model.AISkill{}).
		Where("(project_id = 0 OR project_id = ?)", projectID)

	if f.Keyword != "" {
		query = query.Where("name LIKE ?", "%"+f.Keyword+"%")
	}
	if f.Scope != "" {
		query = query.Where("scope = ?", f.Scope)
	}
	if f.IsActive != nil {
		query = query.Where("is_active = ?", *f.IsActive)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var skills []model.AISkill
	offset := (f.Page - 1) * f.PageSize
	err := query.
		Order("sort_order ASC, id ASC").
		Offset(offset).
		Limit(f.PageSize).
		Find(&skills).Error
	if err != nil {
		return nil, 0, err
	}

	return skills, total, nil
}

// FindByIDs 按 ID 列表批量查询 Skill
func (r *aiSkillRepo) FindByIDs(ctx context.Context, ids []uint) ([]model.AISkill, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var skills []model.AISkill
	err := r.db.WithContext(ctx).
		Where("id IN ?", ids).
		Find(&skills).Error
	return skills, err
}

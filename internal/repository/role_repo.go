// role_repo.go — 角色数据访问层
package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"testpilot/internal/model"
)

// RoleRepository 角色数据访问接口
type RoleRepository interface {
	// FindByID 根据 ID 查找角色
	FindByID(ctx context.Context, id uint) (*model.Role, error)
	// FindByIDs 根据 ID 列表查找角色（数量不匹配时返回错误）
	FindByIDs(ctx context.Context, ids []uint) ([]model.Role, error)
	// List 获取全部角色列表
	List(ctx context.Context) ([]model.Role, error)
	// Create 创建角色
	Create(ctx context.Context, role *model.Role) error
	// Updates 更新角色字段
	Updates(ctx context.Context, id uint, fields map[string]any) error
	// Delete 删除角色
	Delete(ctx context.Context, id uint) error
	// CountUsers 统计使用此角色的用户数
	CountUsers(ctx context.Context, roleID uint) (int64, error)

	// ---- 事务版本 ----

	// CreateTx 在事务中创建角色
	CreateTx(tx *gorm.DB, role *model.Role) error
	// UpdatesTx 在事务中更新角色
	UpdatesTx(tx *gorm.DB, id uint, fields map[string]any) error
	// DeleteTx 在事务中删除角色
	DeleteTx(tx *gorm.DB, role *model.Role) error
}

// roleRepo RoleRepository 的 GORM 实现
type roleRepo struct {
	db *gorm.DB
}

// NewRoleRepo 创建角色仓库
func NewRoleRepo(db *gorm.DB) RoleRepository {
	return &roleRepo{db: db}
}

func (r *roleRepo) FindByID(ctx context.Context, id uint) (*model.Role, error) {
	var role model.Role
	if err := r.db.WithContext(ctx).First(&role, id).Error; err != nil {
		return nil, err
	}
	return &role, nil
}

func (r *roleRepo) FindByIDs(ctx context.Context, ids []uint) ([]model.Role, error) {
	var roles []model.Role
	if err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&roles).Error; err != nil {
		return nil, err
	}
	return roles, nil
}

// List 获取全部角色列表（含关联用户数）
// 使用 Raw().Rows() + 手动 Scan 绕过 gorm:"-" 标签限制
func (r *roleRepo) List(ctx context.Context) ([]model.Role, error) {
	// 定义局部扫描结构体（无 gorm 标签限制）
	type roleRow struct {
		ID          uint       `gorm:"column:id"`
		Name        string     `gorm:"column:name"`
		DisplayName string     `gorm:"column:display_name"`
		Description string     `gorm:"column:description"`
		UserCount   int64      `gorm:"column:user_count"`
		CreatedAt   time.Time  `gorm:"column:created_at"`
		UpdatedAt   time.Time  `gorm:"column:updated_at"`
	}
	var rows []roleRow
	err := r.db.WithContext(ctx).Raw(`
		SELECT roles.id, roles.name, roles.display_name, roles.description,
			(SELECT COUNT(*) FROM user_roles WHERE user_roles.role_id = roles.id) AS user_count,
			roles.created_at, roles.updated_at
		FROM roles
		WHERE roles.deleted_at IS NULL
		ORDER BY roles.id ASC
	`).Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	roles := make([]model.Role, 0, len(rows))
	for _, row := range rows {
		roles = append(roles, model.Role{
			ID:          row.ID,
			Name:        row.Name,
			DisplayName: row.DisplayName,
			Description: row.Description,
			UserCount:   row.UserCount,
			CreatedAt:   row.CreatedAt,
			UpdatedAt:   row.UpdatedAt,
		})
	}
	return roles, nil
}

func (r *roleRepo) Create(ctx context.Context, role *model.Role) error {
	return r.db.WithContext(ctx).Create(role).Error
}

func (r *roleRepo) Updates(ctx context.Context, id uint, fields map[string]any) error {
	return r.db.WithContext(ctx).Model(&model.Role{}).Where("id = ?", id).Updates(fields).Error
}

func (r *roleRepo) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&model.Role{}, id).Error
}

func (r *roleRepo) CountUsers(ctx context.Context, roleID uint) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.UserRole{}).Where("role_id = ?", roleID).Count(&count).Error
	return count, err
}

// ---- 事务版本 ----

func (r *roleRepo) CreateTx(tx *gorm.DB, role *model.Role) error {
	return tx.Create(role).Error
}

func (r *roleRepo) UpdatesTx(tx *gorm.DB, id uint, fields map[string]any) error {
	return tx.Model(&model.Role{}).Where("id = ?", id).Updates(fields).Error
}

func (r *roleRepo) DeleteTx(tx *gorm.DB, role *model.Role) error {
	return tx.Delete(role).Error
}

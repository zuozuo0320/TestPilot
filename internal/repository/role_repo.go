// role_repo.go — 角色数据访问层
package repository

import (
	"context"

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

func (r *roleRepo) List(ctx context.Context) ([]model.Role, error) {
	var roles []model.Role
	if err := r.db.WithContext(ctx).Order("id asc").Find(&roles).Error; err != nil {
		return nil, err
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

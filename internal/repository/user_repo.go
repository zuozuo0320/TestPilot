// user_repo.go — 用户数据访问层
package repository

import (
	"context"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"testpilot/internal/model"
)

// UserListFilter 用户列表筛选条件
type UserListFilter struct {
	Keyword string // 关键词：模糊匹配姓名/邮箱
	RoleID  uint   // 角色 ID：筛选拥有该角色的用户
	Status  string // 状态：active / disabled
}

// UserRepository 用户数据访问接口
type UserRepository interface {
	// FindByID 根据 ID 查找用户
	FindByID(ctx context.Context, id uint) (*model.User, error)
	// FindByIDUnscoped 根据 ID 查找用户（含已删除）
	FindByIDUnscoped(ctx context.Context, id uint) (*model.User, error)
	// FindByEmail 根据邮箱查找用户
	FindByEmail(ctx context.Context, email string) (*model.User, error)
	// List 获取全部用户列表
	List(ctx context.Context) ([]model.User, error)
	// ListFiltered 按筛选条件获取用户列表
	ListFiltered(ctx context.Context, filter UserListFilter) ([]model.User, error)
	// ExistsByEmail 检查邮箱是否已存在（排除指定用户）
	ExistsByEmail(ctx context.Context, email string, excludeID uint) (bool, error)
	// ExistsByPhone 检查手机号是否已存在（排除指定用户）
	ExistsByPhone(ctx context.Context, phone string, excludeID uint) (bool, error)
	// HasRoleName 判断用户是否拥有指定角色名
	HasRoleName(ctx context.Context, userID uint, roleName string) (bool, error)
	// Create 创建用户
	Create(ctx context.Context, user *model.User) error
	// Updates 更新用户字段
	Updates(ctx context.Context, id uint, fields map[string]any) error
	// SoftDelete 逻辑删除用户
	SoftDelete(ctx context.Context, id uint) error
	// GetRoleIDs 获取用户绑定的角色 ID 列表
	GetRoleIDs(ctx context.Context, userID uint) ([]uint, error)
	// GetProjectIDs 获取用户绑定的项目 ID 列表
	GetProjectIDs(ctx context.Context, userID uint) ([]uint, error)

	// ---- 事务版本 ----

	// CreateTx 在事务中创建用户
	CreateTx(tx *gorm.DB, user *model.User) error
	// UpdatesTx 在事务中更新用户字段
	UpdatesTx(tx *gorm.DB, id uint, fields map[string]any) error
	// SoftDeleteTx 在事务中逻辑删除用户
	SoftDeleteTx(tx *gorm.DB, user *model.User) error
	// ReplaceRolesTx 在事务中替换用户角色绑定
	ReplaceRolesTx(tx *gorm.DB, userID uint, roleIDs []uint) error
	// ReplaceProjectsTx 在事务中替换用户项目绑定
	ReplaceProjectsTx(tx *gorm.DB, userID uint, projectIDs []uint) error
	// SyncProjectMembersTx 在事务中同步用户项目成员记录
	SyncProjectMembersTx(tx *gorm.DB, userID uint, projectIDs []uint) error
	// CleanupRelationsTx 在事务中清理用户所有关联（角色/项目/成员）
	CleanupRelationsTx(tx *gorm.DB, userID uint) error
}

// userRepo UserRepository 的 GORM 实现
type userRepo struct {
	db *gorm.DB
}

// NewUserRepo 创建用户仓库
func NewUserRepo(db *gorm.DB) UserRepository {
	return &userRepo{db: db}
}

func (r *userRepo) FindByID(ctx context.Context, id uint) (*model.User, error) {
	var user model.User
	if err := r.db.WithContext(ctx).First(&user, id).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *userRepo) FindByIDUnscoped(ctx context.Context, id uint) (*model.User, error) {
	var user model.User
	if err := r.db.WithContext(ctx).Unscoped().First(&user, id).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *userRepo) FindByEmail(ctx context.Context, email string) (*model.User, error) {
	var user model.User
	if err := r.db.WithContext(ctx).Where("email = ?", email).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *userRepo) List(ctx context.Context) ([]model.User, error) {
	var users []model.User
	if err := r.db.WithContext(ctx).Order("id asc").Find(&users).Error; err != nil {
		return nil, err
	}
	return users, nil
}

// ListFiltered 按筛选条件查询用户列表
// keyword 模糊匹配姓名或邮箱，role_id 通过 user_roles 关联表筛选，status 筛选启用/禁用
func (r *userRepo) ListFiltered(ctx context.Context, filter UserListFilter) ([]model.User, error) {
	q := r.db.WithContext(ctx).Model(&model.User{})

	// 关键词模糊搜索
	if kw := strings.TrimSpace(filter.Keyword); kw != "" {
		like := "%" + kw + "%"
		q = q.Where("name LIKE ? OR email LIKE ?", like, like)
	}

	// 角色筛选：通过 user_roles 关联表子查询
	if filter.RoleID > 0 {
		q = q.Where("id IN (SELECT user_id FROM user_roles WHERE role_id = ?)", filter.RoleID)
	}

	// 状态筛选
	switch filter.Status {
	case "active":
		q = q.Where("active = ?", true)
	case "disabled":
		q = q.Where("active = ?", false)
	}

	var users []model.User
	if err := q.Order("id asc").Find(&users).Error; err != nil {
		return nil, err
	}
	return users, nil
}

func (r *userRepo) ExistsByEmail(ctx context.Context, email string, excludeID uint) (bool, error) {
	if strings.TrimSpace(email) == "" {
		return false, nil
	}
	query := r.db.WithContext(ctx).Unscoped().Model(&model.User{}).
		Where("email = ?", email).
		Where("deleted_at IS NULL")
	if excludeID > 0 {
		query = query.Where("id <> ?", excludeID)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *userRepo) ExistsByPhone(ctx context.Context, phone string, excludeID uint) (bool, error) {
	if strings.TrimSpace(phone) == "" {
		return false, nil
	}
	query := r.db.WithContext(ctx).Unscoped().Model(&model.User{}).
		Where("phone = ?", phone).
		Where("deleted_at IS NULL")
	if excludeID > 0 {
		query = query.Where("id <> ?", excludeID)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *userRepo) HasRoleName(ctx context.Context, userID uint, roleName string) (bool, error) {
	if userID == 0 || strings.TrimSpace(roleName) == "" {
		return false, nil
	}
	var count int64
	err := r.db.WithContext(ctx).Model(&model.UserRole{}).
		Joins("JOIN roles ON roles.id = user_roles.role_id").
		Where("user_roles.user_id = ? AND LOWER(roles.name) = ?", userID, strings.ToLower(strings.TrimSpace(roleName))).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *userRepo) Create(ctx context.Context, user *model.User) error {
	return r.db.WithContext(ctx).Create(user).Error
}

func (r *userRepo) Updates(ctx context.Context, id uint, fields map[string]any) error {
	return r.db.WithContext(ctx).Model(&model.User{}).Where("id = ?", id).Updates(fields).Error
}

func (r *userRepo) SoftDelete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&model.User{}, id).Error
}

// GetRoleIDs 获取用户绑定的角色 ID 列表
func (r *userRepo) GetRoleIDs(ctx context.Context, userID uint) ([]uint, error) {
	var ids []uint
	err := r.db.WithContext(ctx).Model(&model.UserRole{}).Where("user_id = ?", userID).Pluck("role_id", &ids).Error
	return ids, err
}

// GetProjectIDs 获取用户绑定的项目 ID 列表
func (r *userRepo) GetProjectIDs(ctx context.Context, userID uint) ([]uint, error) {
	var ids []uint
	err := r.db.WithContext(ctx).Model(&model.UserProject{}).Where("user_id = ?", userID).Pluck("project_id", &ids).Error
	return ids, err
}

// ---- 事务版本 ----

func (r *userRepo) CreateTx(tx *gorm.DB, user *model.User) error {
	return tx.Create(user).Error
}

func (r *userRepo) UpdatesTx(tx *gorm.DB, id uint, fields map[string]any) error {
	return tx.Model(&model.User{}).Where("id = ?", id).Updates(fields).Error
}

func (r *userRepo) SoftDeleteTx(tx *gorm.DB, user *model.User) error {
	return tx.Delete(user).Error
}

func (r *userRepo) ReplaceRolesTx(tx *gorm.DB, userID uint, roleIDs []uint) error {
	if err := tx.Where("user_id = ?", userID).Delete(&model.UserRole{}).Error; err != nil {
		return err
	}
	items := make([]model.UserRole, 0, len(roleIDs))
	for _, roleID := range roleIDs {
		items = append(items, model.UserRole{UserID: userID, RoleID: roleID})
	}
	return tx.Create(&items).Error
}

func (r *userRepo) ReplaceProjectsTx(tx *gorm.DB, userID uint, projectIDs []uint) error {
	if err := tx.Where("user_id = ?", userID).Delete(&model.UserProject{}).Error; err != nil {
		return err
	}
	items := make([]model.UserProject, 0, len(projectIDs))
	for _, projectID := range projectIDs {
		items = append(items, model.UserProject{UserID: userID, ProjectID: projectID})
	}
	return tx.Create(&items).Error
}

func (r *userRepo) SyncProjectMembersTx(tx *gorm.DB, userID uint, projectIDs []uint) error {
	if len(projectIDs) == 0 {
		return nil
	}
	if err := tx.Where("user_id = ?", userID).Where("project_id NOT IN ?", projectIDs).Delete(&model.ProjectMember{}).Error; err != nil {
		return err
	}
	for _, projectID := range projectIDs {
		member := model.ProjectMember{ProjectID: projectID, UserID: userID, Role: model.MemberRoleMember}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "project_id"}, {Name: "user_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"updated_at"}),
		}).Create(&member).Error; err != nil {
			return err
		}
	}
	return nil
}

func (r *userRepo) CleanupRelationsTx(tx *gorm.DB, userID uint) error {
	if err := tx.Where("user_id = ?", userID).Delete(&model.UserRole{}).Error; err != nil {
		return err
	}
	if err := tx.Where("user_id = ?", userID).Delete(&model.UserProject{}).Error; err != nil {
		return err
	}
	return tx.Where("user_id = ?", userID).Delete(&model.ProjectMember{}).Error
}

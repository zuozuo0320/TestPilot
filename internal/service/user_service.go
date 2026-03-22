// user_service.go — 用户管理业务逻辑
package service

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"

	"testpilot/internal/model"
	pkgauth "testpilot/internal/pkg/auth"
	"testpilot/internal/repository"
)

// CreateUserInput 创建用户输入
type CreateUserInput struct {
	Name       string
	Email      string
	Phone      string
	Password   string // 初始密码（FR-02-12）
	Role       string
	RoleIDs    []uint
	ProjectIDs []uint
}

// UpdateUserInput 更新用户输入
// 注意：不包含 Email 字段，邮箱在任何场景下均不可修改（业务规则 #7）
type UpdateUserInput struct {
	Name       *string
	Phone      *string
	Avatar     *string
	Active     *bool
	RoleIDs    []uint
	ProjectIDs []uint
}

// passwordRegex 密码复杂度规则：≥8位，须包含大写字母、小写字母和数字
var (
	passwordMinLen   = 8
	passwordHasUpper = regexp.MustCompile(`[A-Z]`)
	passwordHasLower = regexp.MustCompile(`[a-z]`)
	passwordHasDigit = regexp.MustCompile(`[0-9]`)
)

// validatePassword 校验密码复杂度
// 规则：≥8位，须包含大写字母、小写字母和数字
func validatePassword(password string) error {
	if len(password) < passwordMinLen {
		return ErrPasswordTooWeak
	}
	if !passwordHasUpper.MatchString(password) || !passwordHasLower.MatchString(password) || !passwordHasDigit.MatchString(password) {
		return ErrPasswordTooWeak
	}
	return nil
}

// UserService 用户管理服务
type UserService struct {
	userRepo    repository.UserRepository
	roleRepo    repository.RoleRepository
	projectRepo repository.ProjectRepository
	auditRepo   repository.AuditRepository
	txMgr       *repository.TxManager
}

// NewUserService 创建用户服务
func NewUserService(
	userRepo repository.UserRepository,
	roleRepo repository.RoleRepository,
	projectRepo repository.ProjectRepository,
	auditRepo repository.AuditRepository,
	txMgr *repository.TxManager,
) *UserService {
	return &UserService{
		userRepo:    userRepo,
		roleRepo:    roleRepo,
		projectRepo: projectRepo,
		auditRepo:   auditRepo,
		txMgr:       txMgr,
	}
}

// List 获取用户列表
func (s *UserService) List(ctx context.Context) ([]model.User, error) {
	return s.userRepo.List(ctx)
}

// GetRoleIDs 获取用户绑定的角色 ID 列表（用于编辑页面预填充）
func (s *UserService) GetRoleIDs(ctx context.Context, userID uint) ([]uint, error) {
	return s.userRepo.GetRoleIDs(ctx, userID)
}

// GetProjectIDs 获取用户绑定的项目 ID 列表（用于编辑页面预填充）
func (s *UserService) GetProjectIDs(ctx context.Context, userID uint) ([]uint, error) {
	return s.userRepo.GetProjectIDs(ctx, userID)
}

// Create 创建用户
// 管理员创建用户时需指定初始密码，默认绑定「快速开始」项目
func (s *UserService) Create(ctx context.Context, actorID uint, input CreateUserInput) (*model.User, error) {
	// 基础校验
	if input.Name == "" || input.Email == "" {
		return nil, ErrBadRequest("MISSING_FIELDS", "name/email is required")
	}
	if !isValidPersonName(input.Name) {
		return nil, ErrBadRequest("INVALID_NAME", "name is invalid")
	}
	if !isValidEmail(input.Email) {
		return nil, ErrBadRequest("INVALID_EMAIL", "email is invalid")
	}
	if input.Phone != "" && !isValidPhone(input.Phone) {
		return nil, ErrBadRequest("INVALID_PHONE", "phone is invalid")
	}
	if len(input.RoleIDs) == 0 {
		return nil, ErrBadRequest("MISSING_ROLE_IDS", "role_ids is required")
	}
	if len(input.ProjectIDs) == 0 {
		return nil, ErrBadRequest("MISSING_PROJECT_IDS", "project_ids is required")
	}

	// 密码校验（FR-02-12）
	if input.Password == "" {
		return nil, ErrBadRequest("MISSING_PASSWORD", "password is required")
	}
	if err := validatePassword(input.Password); err != nil {
		return nil, err
	}

	// 唯一性校验
	if exists, err := s.userRepo.ExistsByEmail(ctx, input.Email, 0); err != nil {
		return nil, ErrInternal("DB_ERROR", err)
	} else if exists {
		return nil, ErrEmailExists
	}
	if input.Phone != "" {
		if exists, err := s.userRepo.ExistsByPhone(ctx, input.Phone, 0); err != nil {
			return nil, ErrInternal("DB_ERROR", err)
		} else if exists {
			return nil, ErrPhoneExists
		}
	}

	// 角色验证：不可分配 admin
	roles, err := s.roleRepo.FindByIDs(ctx, input.RoleIDs)
	if err != nil || len(roles) != len(input.RoleIDs) {
		return nil, ErrBadRequest("INVALID_ROLE_IDS", "role_ids contains invalid id")
	}
	if containsRoleName(roles, model.GlobalRoleAdmin) {
		return nil, ErrBadRequest("ADMIN_ASSIGN_BLOCKED", "创建用户时不可分配 admin 角色")
	}

	// 项目验证
	allExist, err := s.projectRepo.ExistAll(ctx, input.ProjectIDs)
	if err != nil {
		return nil, ErrInternal("DB_ERROR", err)
	}
	if !allExist {
		return nil, ErrBadRequest("INVALID_PROJECT_IDS", "project_ids contains invalid id")
	}

	// 确定缓存主角色
	globalRole := input.Role
	if !model.IsValidGlobalRole(globalRole) {
		globalRole = strings.ToLower(strings.TrimSpace(roles[0].Name))
		if !model.IsValidGlobalRole(globalRole) {
			globalRole = model.GlobalRoleReadonly
		}
	}

	// 密码哈希
	passwordHash, err := pkgauth.HashPassword(input.Password)
	if err != nil {
		return nil, fmt.Errorf("hash password failed: %w", err)
	}

	entity := model.User{
		Name:         input.Name,
		Email:        input.Email,
		Phone:        input.Phone,
		PasswordHash: passwordHash,
		Role:         globalRole,
		Active:       true,
	}

	err = s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		if err := s.userRepo.CreateTx(tx, &entity); err != nil {
			return err
		}
		if err := s.userRepo.ReplaceRolesTx(tx, entity.ID, input.RoleIDs); err != nil {
			return err
		}
		if err := s.userRepo.ReplaceProjectsTx(tx, entity.ID, input.ProjectIDs); err != nil {
			return err
		}
		if err := s.userRepo.SyncProjectMembersTx(tx, entity.ID, input.ProjectIDs); err != nil {
			return err
		}
		return s.auditRepo.WriteLogTx(tx, actorID, "user.create", "user", entity.ID, nil, entity)
	})
	if err != nil {
		if isDuplicateError(err) {
			return nil, ErrConflict("USER_EXISTS", "user already exists")
		}
		return nil, ErrInternal("TX_ERROR", err)
	}
	return &entity, nil
}

// Update 更新用户
// 邮箱不可修改（后端硬拒绝），其他字段可选更新
func (s *UserService) Update(ctx context.Context, actorID, userID uint, input UpdateUserInput) (*model.User, error) {
	target, err := s.userRepo.FindByIDUnscoped(ctx, userID)
	if err != nil {
		return nil, ErrUserNotFound
	}
	if target.DeletedAt.Valid {
		return nil, ErrBadRequest("USER_DELETED", "cannot update deleted user")
	}

	updates := map[string]any{}
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if !isValidPersonName(name) {
			return nil, ErrBadRequest("INVALID_NAME", "name is invalid")
		}
		updates["name"] = name
	}
	if input.Phone != nil {
		phone := strings.TrimSpace(*input.Phone)
		if phone != "" {
			if !isValidPhone(phone) {
				return nil, ErrBadRequest("INVALID_PHONE", "phone is invalid")
			}
			if exists, _ := s.userRepo.ExistsByPhone(ctx, phone, target.ID); exists {
				return nil, ErrPhoneExists
			}
		}
		updates["phone"] = phone
	}
	if input.Avatar != nil {
		updates["avatar"] = strings.TrimSpace(*input.Avatar)
	}
	if input.Active != nil {
		updates["active"] = *input.Active
	}

	roleIDs := uniqueUint(input.RoleIDs)
	projectIDs := uniqueUint(input.ProjectIDs)

	if input.RoleIDs != nil {
		if len(roleIDs) == 0 {
			return nil, ErrBadRequest("MISSING_ROLE_IDS", "至少保留一个角色")
		}
		roles, err := s.roleRepo.FindByIDs(ctx, roleIDs)
		if err != nil || len(roles) != len(roleIDs) {
			return nil, ErrBadRequest("INVALID_ROLE_IDS", "role_ids contains invalid id")
		}
	}
	if input.ProjectIDs != nil {
		if len(projectIDs) == 0 {
			return nil, ErrBadRequest("MISSING_PROJECT_IDS", "至少绑定一个项目")
		}
		allExist, err := s.projectRepo.ExistAll(ctx, projectIDs)
		if err != nil || !allExist {
			return nil, ErrBadRequest("INVALID_PROJECT_IDS", "project_ids contains invalid id")
		}
	}

	before := *target
	err = s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		if len(updates) > 0 {
			if err := s.userRepo.UpdatesTx(tx, target.ID, updates); err != nil {
				return err
			}
		}
		if input.RoleIDs != nil {
			if err := s.userRepo.ReplaceRolesTx(tx, target.ID, roleIDs); err != nil {
				return err
			}
		}
		if input.ProjectIDs != nil {
			if err := s.userRepo.ReplaceProjectsTx(tx, target.ID, projectIDs); err != nil {
				return err
			}
			if err := s.userRepo.SyncProjectMembersTx(tx, target.ID, projectIDs); err != nil {
				return err
			}
		}
		var after model.User
		if err := tx.First(&after, target.ID).Error; err != nil {
			return err
		}
		return s.auditRepo.WriteLogTx(tx, actorID, "user.update", "user", target.ID, before, after)
	})
	if err != nil {
		return nil, ErrInternal("TX_ERROR", err)
	}
	updated, _ := s.userRepo.FindByID(ctx, target.ID)
	return updated, nil
}

// Delete 逻辑删除用户
// 删除后自动从所有项目成员中移除（跨模块联动规则 #1）
func (s *UserService) Delete(ctx context.Context, actorID, userID uint) error {
	target, err := s.userRepo.FindByID(ctx, userID)
	if err != nil {
		return ErrUserNotFound
	}
	if strings.EqualFold(strings.TrimSpace(target.Role), model.GlobalRoleAdmin) {
		return ErrAdminCannotBeDeleted
	}
	hasAdmin, err := s.userRepo.HasRoleName(ctx, target.ID, model.GlobalRoleAdmin)
	if err != nil {
		return ErrInternal("DB_ERROR", err)
	}
	if hasAdmin {
		return ErrAdminCannotBeDeleted
	}

	before := *target
	return s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		if err := s.userRepo.SoftDeleteTx(tx, target); err != nil {
			return err
		}
		if err := s.userRepo.CleanupRelationsTx(tx, target.ID); err != nil {
			return err
		}
		return s.auditRepo.WriteLogTx(tx, actorID, "user.delete", "user", target.ID, before, map[string]any{"deleted_at": time.Now()})
	})
}

// ResetPassword 管理员重置用户密码（不需要旧密码）（FR-02-14）
func (s *UserService) ResetPassword(ctx context.Context, actorID, userID uint, newPassword string) error {
	if err := validatePassword(newPassword); err != nil {
		return err
	}
	target, err := s.userRepo.FindByID(ctx, userID)
	if err != nil {
		return ErrUserNotFound
	}
	hash, err := pkgauth.HashPassword(newPassword)
	if err != nil {
		return ErrInternal("HASH_ERROR", err)
	}
	if err := s.userRepo.Updates(ctx, target.ID, map[string]any{"password_hash": hash}); err != nil {
		return ErrInternal("DB_ERROR", err)
	}
	return nil
}

// ChangePassword 用户修改自己的密码（需验证旧密码）（FR-02-13）
func (s *UserService) ChangePassword(ctx context.Context, userID uint, oldPassword, newPassword string) error {
	target, err := s.userRepo.FindByID(ctx, userID)
	if err != nil {
		return ErrUserNotFound
	}
	// 验证旧密码
	if !pkgauth.CheckPassword(oldPassword, target.PasswordHash) {
		return ErrOldPasswordWrong
	}
	// 校验新密码复杂度
	if err := validatePassword(newPassword); err != nil {
		return err
	}
	hash, err := pkgauth.HashPassword(newPassword)
	if err != nil {
		return ErrInternal("HASH_ERROR", err)
	}
	return s.userRepo.Updates(ctx, target.ID, map[string]any{"password_hash": hash})
}

// ToggleActive 启用/禁用用户（FR-02-15）
// 禁用后用户无法登录，提示「账号已被禁用」
func (s *UserService) ToggleActive(ctx context.Context, actorID, userID uint, active bool) error {
	target, err := s.userRepo.FindByID(ctx, userID)
	if err != nil {
		return ErrUserNotFound
	}
	// admin 不可被禁用
	if !active {
		hasAdmin, err := s.userRepo.HasRoleName(ctx, target.ID, model.GlobalRoleAdmin)
		if err != nil {
			return ErrInternal("DB_ERROR", err)
		}
		if hasAdmin || strings.EqualFold(target.Role, model.GlobalRoleAdmin) {
			return ErrBadRequest("ADMIN_PROTECTED", "admin 用户不可被禁用")
		}
	}
	return s.userRepo.Updates(ctx, target.ID, map[string]any{"active": active})
}

// AssignRoles 分配角色
func (s *UserService) AssignRoles(ctx context.Context, actorID, userID uint, roleIDs []uint) error {
	roleIDs = uniqueUint(roleIDs)
	if len(roleIDs) == 0 {
		return ErrBadRequest("MISSING_ROLE_IDS", "role_ids is required")
	}
	roles, err := s.roleRepo.FindByIDs(ctx, roleIDs)
	if err != nil || len(roles) != len(roleIDs) {
		return ErrBadRequest("INVALID_ROLE_IDS", "role_ids contains invalid id")
	}
	return s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		if err := s.userRepo.ReplaceRolesTx(tx, userID, roleIDs); err != nil {
			return err
		}
		return s.auditRepo.WriteLogTx(tx, actorID, "user.assign_roles", "user", userID, nil, map[string]any{"role_ids": roleIDs})
	})
}

// AssignProjects 分配项目
func (s *UserService) AssignProjects(ctx context.Context, actorID, userID uint, projectIDs []uint) error {
	projectIDs = uniqueUint(projectIDs)
	if len(projectIDs) == 0 {
		return ErrBadRequest("MISSING_PROJECT_IDS", "project_ids is required")
	}
	allExist, err := s.projectRepo.ExistAll(ctx, projectIDs)
	if err != nil || !allExist {
		return ErrBadRequest("INVALID_PROJECT_IDS", "project_ids contains invalid id")
	}
	return s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		if err := s.userRepo.ReplaceProjectsTx(tx, userID, projectIDs); err != nil {
			return err
		}
		if err := s.userRepo.SyncProjectMembersTx(tx, userID, projectIDs); err != nil {
			return err
		}
		return s.auditRepo.WriteLogTx(tx, actorID, "user.assign_projects", "user", userID, nil, map[string]any{"project_ids": projectIDs})
	})
}

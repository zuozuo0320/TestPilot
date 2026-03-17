// user_service.go — 用户管理业务逻辑
package service

import (
	"context"
	"fmt"
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
	Role       string
	RoleIDs    []uint
	ProjectIDs []uint
}

// UpdateUserInput 更新用户输入
type UpdateUserInput struct {
	Name       *string
	Email      *string
	Phone      *string
	Avatar     *string
	Active     *bool
	RoleIDs    []uint
	ProjectIDs []uint
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

// Create 创建用户
func (s *UserService) Create(ctx context.Context, actorID uint, input CreateUserInput) (*model.User, error) {
	// 校验
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

	// 唯一性
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

	// 角色验证
	roles, err := s.roleRepo.FindByIDs(ctx, input.RoleIDs)
	if err != nil || len(roles) != len(input.RoleIDs) {
		return nil, ErrBadRequest("INVALID_ROLE_IDS", "role_ids contains invalid id")
	}
	if containsRoleName(roles, model.GlobalRoleAdmin) {
		return nil, ErrBadRequest("ADMIN_ASSIGN_BLOCKED", "admin role cannot be assigned when creating user")
	}

	// 项目验证
	allExist, err := s.projectRepo.ExistAll(ctx, input.ProjectIDs)
	if err != nil {
		return nil, ErrInternal("DB_ERROR", err)
	}
	if !allExist {
		return nil, ErrBadRequest("INVALID_PROJECT_IDS", "project_ids contains invalid id")
	}

	// 全局角色
	globalRole := input.Role
	if !model.IsValidGlobalRole(globalRole) {
		globalRole = strings.ToLower(strings.TrimSpace(roles[0].Name))
		if !model.IsValidGlobalRole(globalRole) {
			globalRole = model.GlobalRoleTester
		}
	}

	// 为新用户生成默认密码哈希
	defaultHash, err := pkgauth.HashPassword("TestPilot@2026")
	if err != nil {
		return nil, fmt.Errorf("hash default password failed: %w", err)
	}

	entity := model.User{
		Name:         input.Name,
		Email:        input.Email,
		Phone:        input.Phone,
		Avatar:       "https://api.dicebear.com/7.x/initials/svg?seed=TestPilot",
		PasswordHash: defaultHash,
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
	if input.Email != nil {
		email := strings.ToLower(strings.TrimSpace(*input.Email))
		if !isValidEmail(email) {
			return nil, ErrBadRequest("INVALID_EMAIL", "email is invalid")
		}
		if exists, _ := s.userRepo.ExistsByEmail(ctx, email, target.ID); exists {
			return nil, ErrEmailExists
		}
		updates["email"] = email
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
			return nil, ErrBadRequest("MISSING_ROLE_IDS", "role_ids is required")
		}
		roles, err := s.roleRepo.FindByIDs(ctx, roleIDs)
		if err != nil || len(roles) != len(roleIDs) {
			return nil, ErrBadRequest("INVALID_ROLE_IDS", "role_ids contains invalid id")
		}
	}
	if input.ProjectIDs != nil {
		if len(projectIDs) == 0 {
			return nil, ErrBadRequest("MISSING_PROJECT_IDS", "project_ids is required")
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

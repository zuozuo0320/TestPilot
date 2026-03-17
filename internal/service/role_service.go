// role_service.go — 角色管理业务逻辑
package service

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"testpilot/internal/model"
	"testpilot/internal/repository"
)

// RoleService 角色管理服务
type RoleService struct {
	roleRepo  repository.RoleRepository
	auditRepo repository.AuditRepository
	txMgr     *repository.TxManager
}

// NewRoleService 创建角色服务
func NewRoleService(roleRepo repository.RoleRepository, auditRepo repository.AuditRepository, txMgr *repository.TxManager) *RoleService {
	return &RoleService{roleRepo: roleRepo, auditRepo: auditRepo, txMgr: txMgr}
}

// List 获取角色列表
func (s *RoleService) List(ctx context.Context) ([]model.Role, error) {
	return s.roleRepo.List(ctx)
}

// Create 创建角色
func (s *RoleService) Create(ctx context.Context, actorID uint, name, description string) (*model.Role, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrBadRequest("MISSING_NAME", "name is required")
	}
	entity := model.Role{Name: name, Description: strings.TrimSpace(description)}
	err := s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		if err := s.roleRepo.CreateTx(tx, &entity); err != nil {
			return err
		}
		return s.auditRepo.WriteLogTx(tx, actorID, "role.create", "role", entity.ID, nil, entity)
	})
	if err != nil {
		if isDuplicateError(err) {
			return nil, ErrConflict("ROLE_EXISTS", "role already exists")
		}
		return nil, ErrInternal("TX_ERROR", err)
	}
	return &entity, nil
}

// Update 更新角色
func (s *RoleService) Update(ctx context.Context, actorID, roleID uint, name, description *string) (*model.Role, error) {
	before, err := s.roleRepo.FindByID(ctx, roleID)
	if err != nil {
		return nil, ErrRoleNotFound
	}
	updates := map[string]any{}
	if name != nil {
		n := strings.TrimSpace(*name)
		if n == "" {
			return nil, ErrBadRequest("INVALID_NAME", "name is invalid")
		}
		updates["name"] = n
	}
	if description != nil {
		updates["description"] = strings.TrimSpace(*description)
	}
	if len(updates) == 0 {
		return nil, ErrBadRequest("NO_FIELDS", "no valid fields to update")
	}
	err = s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		if err := s.roleRepo.UpdatesTx(tx, roleID, updates); err != nil {
			return err
		}
		var after model.Role
		if err := tx.First(&after, roleID).Error; err != nil {
			return err
		}
		return s.auditRepo.WriteLogTx(tx, actorID, "role.update", "role", roleID, before, after)
	})
	if err != nil {
		if isDuplicateError(err) {
			return nil, ErrConflict("ROLE_EXISTS", "role already exists")
		}
		return nil, ErrInternal("TX_ERROR", err)
	}
	updated, _ := s.roleRepo.FindByID(ctx, roleID)
	return updated, nil
}

// Delete 删除角色
func (s *RoleService) Delete(ctx context.Context, actorID, roleID uint) error {
	role, err := s.roleRepo.FindByID(ctx, roleID)
	if err != nil {
		return ErrRoleNotFound
	}
	if model.IsPresetSystemRole(role.Name) {
		return ErrPresetRoleProtected
	}
	used, err := s.roleRepo.CountUsers(ctx, roleID)
	if err != nil {
		return ErrInternal("DB_ERROR", err)
	}
	if used > 0 {
		return ErrRoleInUse
	}
	return s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		if err := s.roleRepo.DeleteTx(tx, role); err != nil {
			return err
		}
		return s.auditRepo.WriteLogTx(tx, actorID, "role.delete", "role", roleID, role, map[string]any{"deleted_at": time.Now()})
	})
}

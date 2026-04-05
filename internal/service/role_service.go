// role_service.go — 角色管理业务逻辑
package service

import (
	"context"
	"fmt"
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

// List 获取角色列表（含关联用户数和 display_name）
func (s *RoleService) List(ctx context.Context) ([]model.Role, error) {
	return s.roleRepo.List(ctx)
}

// Create 创建角色
// name 为英文标识（唯一），displayName 为中文显示名，description 为描述
func (s *RoleService) Create(ctx context.Context, actorID uint, name, displayName, description string) (*model.Role, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrBadRequest(CodeParamsError, "name is required")
	}
	entity := model.Role{
		Name:        name,
		DisplayName: strings.TrimSpace(displayName),
		Description: strings.TrimSpace(description),
	}
	err := s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		if err := s.roleRepo.CreateTx(tx, &entity); err != nil {
			return err
		}
		return s.auditRepo.WriteLogTx(tx, actorID, "role.create", "role", entity.ID, nil, entity)
	})
	if err != nil {
		if isDuplicateError(err) {
			return nil, ErrConflict(CodeConflict, "role already exists")
		}
		return nil, ErrInternal(CodeInternal, err)
	}
	return &entity, nil
}

// Update 更新角色
// 预置角色禁止修改 name（标识名），仅允许修改 display_name 和 description
func (s *RoleService) Update(ctx context.Context, actorID, roleID uint, name, displayName, description *string) (*model.Role, error) {
	before, err := s.roleRepo.FindByID(ctx, roleID)
	if err != nil {
		return nil, ErrRoleNotFound
	}
	updates := map[string]any{}

	// 预置角色禁止修改 name
	if name != nil {
		if model.IsPresetSystemRole(before.Name) {
			return nil, ErrBadRequest(CodeParamsError, "预置角色的标识名不可修改")
		}
		n := strings.TrimSpace(*name)
		if n == "" {
			return nil, ErrBadRequest(CodeParamsError, "name is invalid")
		}
		updates["name"] = n
	}

	if displayName != nil {
		updates["display_name"] = strings.TrimSpace(*displayName)
	}

	if description != nil {
		updates["description"] = strings.TrimSpace(*description)
	}

	if len(updates) == 0 {
		return nil, ErrBadRequest(CodeParamsError, "no valid fields to update")
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
			return nil, ErrConflict(CodeConflict, "role already exists")
		}
		return nil, ErrInternal(CodeInternal, err)
	}
	updated, _ := s.roleRepo.FindByID(ctx, roleID)
	return updated, nil
}

// Delete 删除角色
// 预置角色禁止删除；自定义角色需先解除所有用户关联后才可删除
func (s *RoleService) Delete(ctx context.Context, actorID, roleID uint) error {
	role, err := s.roleRepo.FindByID(ctx, roleID)
	if err != nil {
		return ErrRoleNotFound
	}
	// 预置角色不可删除
	if model.IsPresetSystemRole(role.Name) {
		return ErrPresetRoleProtected
	}
	// 检查关联用户数
	used, err := s.roleRepo.CountUsers(ctx, roleID)
	if err != nil {
		return ErrInternal(CodeInternal, err)
	}
	if used > 0 {
		return ErrConflict(CodeConflict, fmt.Sprintf("该角色已关联 %d 个用户，请先解除关联后再删除", used))
	}
	return s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		if err := s.roleRepo.DeleteTx(tx, role); err != nil {
			return err
		}
		return s.auditRepo.WriteLogTx(tx, actorID, "role.delete", "role", roleID, role, map[string]any{"deleted_at": time.Now()})
	})
}

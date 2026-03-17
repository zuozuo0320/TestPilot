// profile_service.go — 个人中心业务逻辑
package service

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"testpilot/internal/model"
	"testpilot/internal/repository"
)

// ProfileService 个人中心服务
type ProfileService struct {
	userRepo  repository.UserRepository
	auditRepo repository.AuditRepository
	txMgr     *repository.TxManager
}

// NewProfileService 创建个人中心服务
func NewProfileService(userRepo repository.UserRepository, auditRepo repository.AuditRepository, txMgr *repository.TxManager) *ProfileService {
	return &ProfileService{userRepo: userRepo, auditRepo: auditRepo, txMgr: txMgr}
}

// UpdateProfileInput 更新资料输入
type UpdateProfileInput struct {
	Name   *string
	Email  *string
	Phone  *string
	Avatar *string
}

// Update 更新个人资料
func (s *ProfileService) Update(ctx context.Context, actor model.User, input UpdateProfileInput) (*model.User, error) {
	updates := map[string]any{}
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if !isValidPersonName(name) {
			return nil, ErrBadRequest("INVALID_NAME", "name is invalid")
		}
		updates["name"] = name
	}
	if input.Email != nil {
		return nil, ErrBadRequest("EMAIL_READONLY", "email cannot be modified")
	}
	if input.Phone != nil {
		phone := strings.TrimSpace(*input.Phone)
		if phone != "" {
			if !isValidPhone(phone) {
				return nil, ErrBadRequest("INVALID_PHONE", "phone is invalid")
			}
			if exists, _ := s.userRepo.ExistsByPhone(ctx, phone, actor.ID); exists {
				return nil, ErrPhoneExists
			}
		}
		updates["phone"] = phone
	}
	if input.Avatar != nil {
		updates["avatar"] = strings.TrimSpace(*input.Avatar)
	}
	if len(updates) == 0 {
		return nil, ErrBadRequest("NO_FIELDS", "no valid fields to update")
	}

	before := actor
	err := s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		if err := s.userRepo.UpdatesTx(tx, actor.ID, updates); err != nil {
			return err
		}
		var after model.User
		if err := tx.First(&after, actor.ID).Error; err != nil {
			return err
		}
		return s.auditRepo.WriteLogTx(tx, actor.ID, "profile.update", "user", actor.ID, before, after)
	})
	if err != nil {
		return nil, ErrInternal("TX_ERROR", err)
	}
	updated, _ := s.userRepo.FindByID(ctx, actor.ID)
	return updated, nil
}

// UpdateAvatar 更新头像
func (s *ProfileService) UpdateAvatar(ctx context.Context, actor model.User, avatarURL string) error {
	before := actor
	return s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		if err := s.userRepo.UpdatesTx(tx, actor.ID, map[string]any{"avatar": avatarURL}); err != nil {
			return err
		}
		var after model.User
		if err := tx.First(&after, actor.ID).Error; err != nil {
			return err
		}
		return s.auditRepo.WriteLogTx(tx, actor.ID, "profile.avatar_upload", "user", actor.ID, before, after)
	})
}

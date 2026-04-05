// auth_service.go — 认证服务（JWT + bcrypt）
package service

import (
	"context"
	"fmt"
	"time"

	"testpilot/internal/model"
	pkgauth "testpilot/internal/pkg/auth"
	"testpilot/internal/repository"
)

// AuthService 认证服务
type AuthService struct {
	userRepo repository.UserRepository
	jwtCfg   pkgauth.JWTConfig
}

// NewAuthService 创建认证服务
func NewAuthService(userRepo repository.UserRepository, jwtCfg pkgauth.JWTConfig) *AuthService {
	return &AuthService{userRepo: userRepo, jwtCfg: jwtCfg}
}

// AuthResult 登录成功结果
type AuthResult struct {
	AccessToken  string     `json:"access_token"`
	RefreshToken string     `json:"refresh_token"`
	ExpiresAt    int64      `json:"expires_at"`
	User         model.User `json:"user"`
}

// Login 用户登录：bcrypt 校验 + JWT 签发
// 禁用用户返回「账号已被禁用」，成功后更新 last_login_at
func (s *AuthService) Login(ctx context.Context, email, password string) (*AuthResult, error) {
	if email == "" || password == "" {
		return nil, ErrBadRequest(CodeParamsError, "email and password are required")
	}
	user, err := s.userRepo.FindByEmail(ctx, email)
	if err != nil {
		return nil, ErrUnauthorized(CodeUnauthorized, "invalid email or password")
	}
	if user.DeletedAt.Valid {
		return nil, ErrUnauthorized(CodeUnauthorized, "user has been deleted")
	}
	if !user.Active {
		return nil, ErrUserDisabled
	}

	// 校验密码（兼容旧数据：无密码哈希时使用硬编码默认密码）
	if user.PasswordHash != "" {
		if !pkgauth.CheckPassword(password, user.PasswordHash) {
			return nil, ErrUnauthorized(CodeUnauthorized, "invalid email or password")
		}
	} else {
		// 兼容旧数据：PasswordHash 为空时仅接受默认密码
		if password != "TestPilot@2026" {
			return nil, ErrUnauthorized(CodeUnauthorized, "invalid email or password")
		}
	}

	// 更新最后登录时间
	_ = s.userRepo.Updates(ctx, user.ID, map[string]any{"last_login_at": time.Now()})

	// 签发 JWT
	tokenPair, err := pkgauth.GenerateTokenPair(s.jwtCfg, user.ID, user.Role)
	if err != nil {
		return nil, fmt.Errorf("generate token failed: %w", err)
	}

	return &AuthResult{
		AccessToken:  tokenPair.AccessToken,
		RefreshToken: tokenPair.RefreshToken,
		ExpiresAt:    tokenPair.ExpiresAt,
		User:         *user,
	}, nil
}

// RefreshToken 使用 refresh_token 换取新 access_token
func (s *AuthService) RefreshToken(ctx context.Context, refreshTokenStr string) (*AuthResult, error) {
	claims, err := pkgauth.ParseToken(s.jwtCfg.Secret, refreshTokenStr)
	if err != nil {
		return nil, ErrUnauthorized(CodeUnauthorized, err.Error())
	}
	user, findErr := s.userRepo.FindByIDUnscoped(ctx, claims.UserID)
	if findErr != nil {
		return nil, ErrUnauthorized(CodeUnauthorized, "user not found")
	}
	if user.DeletedAt.Valid || !user.Active {
		return nil, ErrForbidden(CodeForbidden, "user is unavailable")
	}
	tokenPair, err := pkgauth.GenerateTokenPair(s.jwtCfg, user.ID, user.Role)
	if err != nil {
		return nil, fmt.Errorf("generate token failed: %w", err)
	}
	return &AuthResult{
		AccessToken:  tokenPair.AccessToken,
		RefreshToken: tokenPair.RefreshToken,
		ExpiresAt:    tokenPair.ExpiresAt,
		User:         *user,
	}, nil
}

// FindUserForAuth 认证中间件用：通过 JWT claims.UserID 查找用户
func (s *AuthService) FindUserForAuth(ctx context.Context, userID uint) (*model.User, error) {
	user, err := s.userRepo.FindByIDUnscoped(ctx, userID)
	if err != nil {
		return nil, ErrUnauthorized(CodeUnauthorized, "user not found")
	}
	if user.DeletedAt.Valid {
		return nil, ErrUnauthorized(CodeUnauthorized, "user deleted")
	}
	if !user.Active {
		return nil, ErrForbidden(CodeForbidden, "user is frozen")
	}
	return user, nil
}

// JWTConfig 暴露 JWT 配置（供 middleware 使用）
func (s *AuthService) JWTConfig() pkgauth.JWTConfig {
	return s.jwtCfg
}

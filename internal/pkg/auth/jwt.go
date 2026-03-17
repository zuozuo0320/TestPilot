// jwt.go — JWT 签发、验证、刷新
package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// TokenClaims JWT 自定义 Claims
type TokenClaims struct {
	UserID uint   `json:"sub"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

// JWTConfig JWT 配置
type JWTConfig struct {
	Secret            string        // 签名密钥
	AccessExpiration  time.Duration // access_token 有效期
	RefreshExpiration time.Duration // refresh_token 有效期
}

// DefaultConfig 默认 JWT 配置
func DefaultConfig(secret string) JWTConfig {
	return JWTConfig{
		Secret:            secret,
		AccessExpiration:  2 * time.Hour,
		RefreshExpiration: 7 * 24 * time.Hour,
	}
}

// TokenPair access_token + refresh_token
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    int64  `json:"expires_at"` // access_token 过期时间 unix 秒
}

// GenerateTokenPair 签发 access + refresh token 对
func GenerateTokenPair(cfg JWTConfig, userID uint, role string) (*TokenPair, error) {
	now := time.Now()

	// Access Token
	accessClaims := TokenClaims{
		UserID: userID,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(cfg.AccessExpiration)),
			IssuedAt:  jwt.NewNumericDate(now),
			Subject:   fmt.Sprintf("%d", userID),
		},
	}
	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
	accessStr, err := accessToken.SignedString([]byte(cfg.Secret))
	if err != nil {
		return nil, fmt.Errorf("sign access token failed: %w", err)
	}

	// Refresh Token（更长有效期，不含 role，仅含 userID）
	refreshClaims := TokenClaims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(cfg.RefreshExpiration)),
			IssuedAt:  jwt.NewNumericDate(now),
			Subject:   fmt.Sprintf("%d", userID),
		},
	}
	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
	refreshStr, err := refreshToken.SignedString([]byte(cfg.Secret))
	if err != nil {
		return nil, fmt.Errorf("sign refresh token failed: %w", err)
	}

	return &TokenPair{
		AccessToken:  accessStr,
		RefreshToken: refreshStr,
		ExpiresAt:    accessClaims.ExpiresAt.Unix(),
	}, nil
}

// ParseToken 解析并验证 JWT token
func ParseToken(secret, tokenStr string) (*TokenClaims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &TokenClaims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, fmt.Errorf("token expired")
		}
		return nil, fmt.Errorf("invalid token: %w", err)
	}
	claims, ok := token.Claims.(*TokenClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}
	return claims, nil
}

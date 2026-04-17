package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"testpilot/internal/model"
	pkgauth "testpilot/internal/pkg/auth"
)

func TestAuthService_LoginSuccess(t *testing.T) {
	db := testDB(t)
	userRepo, _, _, _, _ := testRepos(db)
	seedAdmin(t, db)
	svc := NewAuthService(userRepo, testJWTConfig())

	result, err := svc.Login(context.Background(), "admin@test.local", "TestPilot@2026")
	require.NoError(t, err)
	assert.NotEmpty(t, result.AccessToken)
	assert.NotEmpty(t, result.RefreshToken)
	assert.Equal(t, uint(1), result.User.ID)
	assert.Equal(t, "Admin", result.User.Name)
}

func TestAuthService_LoginWrongPassword(t *testing.T) {
	db := testDB(t)
	userRepo, _, _, _, _ := testRepos(db)
	seedAdmin(t, db)
	svc := NewAuthService(userRepo, testJWTConfig())

	_, err := svc.Login(context.Background(), "admin@test.local", "wrong-password")
	require.Error(t, err)
	bizErr, ok := err.(*BizError)
	require.True(t, ok)
	assert.Equal(t, 401, bizErr.Status)
	assert.Equal(t, CodeUnauthorized, bizErr.Code)
}

func TestAuthService_LoginEmailNotFound(t *testing.T) {
	db := testDB(t)
	userRepo, _, _, _, _ := testRepos(db)
	svc := NewAuthService(userRepo, testJWTConfig())

	_, err := svc.Login(context.Background(), "nobody@test.local", "TestPilot@2026")
	require.Error(t, err)
	bizErr, ok := err.(*BizError)
	require.True(t, ok)
	assert.Equal(t, 401, bizErr.Status)
}

func TestAuthService_LoginFrozenUser(t *testing.T) {
	db := testDB(t)
	userRepo, _, _, _, _ := testRepos(db)
	hash, _ := pkgauth.HashPassword("TestPilot@2026")
	frozen := model.User{
		ID: 10, Name: "Frozen", Email: "frozen@test.local",
		Role: model.GlobalRoleTester, Active: true, PasswordHash: hash,
	}
	db.Create(&frozen)
	// GORM 忽略 Active=false（零值），需要单独 Update
	db.Model(&model.User{}).Where("id = ?", 10).Update("active", false)
	svc := NewAuthService(userRepo, testJWTConfig())

	_, err := svc.Login(context.Background(), "frozen@test.local", "TestPilot@2026")
	require.Error(t, err)
	bizErr, ok := err.(*BizError)
	require.True(t, ok)
	assert.Equal(t, 403, bizErr.Status)
	// 登录接口对被禁用用户统一返回 CodeUserDisabled。
	assert.Equal(t, CodeUserDisabled, bizErr.Code)
}

func TestAuthService_LoginEmptyFields(t *testing.T) {
	db := testDB(t)
	userRepo, _, _, _, _ := testRepos(db)
	svc := NewAuthService(userRepo, testJWTConfig())

	_, err := svc.Login(context.Background(), "", "")
	require.Error(t, err)
	bizErr, ok := err.(*BizError)
	require.True(t, ok)
	assert.Equal(t, 400, bizErr.Status)
}

func TestAuthService_RefreshToken(t *testing.T) {
	db := testDB(t)
	userRepo, _, _, _, _ := testRepos(db)
	seedAdmin(t, db)
	cfg := testJWTConfig()
	svc := NewAuthService(userRepo, cfg)

	// 先登录获取 refresh_token
	loginResult, err := svc.Login(context.Background(), "admin@test.local", "TestPilot@2026")
	require.NoError(t, err)

	// 用 refresh_token 刷新
	refreshResult, err := svc.RefreshToken(context.Background(), loginResult.RefreshToken)
	require.NoError(t, err)
	assert.NotEmpty(t, refreshResult.AccessToken)
	assert.NotEmpty(t, refreshResult.RefreshToken)
	assert.Equal(t, uint(1), refreshResult.User.ID)
}

func TestAuthService_RefreshTokenInvalid(t *testing.T) {
	db := testDB(t)
	userRepo, _, _, _, _ := testRepos(db)
	svc := NewAuthService(userRepo, testJWTConfig())

	_, err := svc.RefreshToken(context.Background(), "invalid-token")
	require.Error(t, err)
	bizErr, ok := err.(*BizError)
	require.True(t, ok)
	assert.Equal(t, 401, bizErr.Status)
}

func TestAuthService_FindUserForAuth(t *testing.T) {
	db := testDB(t)
	userRepo, _, _, _, _ := testRepos(db)
	seedAdmin(t, db)
	svc := NewAuthService(userRepo, testJWTConfig())

	user, err := svc.FindUserForAuth(context.Background(), 1)
	require.NoError(t, err)
	assert.Equal(t, "Admin", user.Name)

	_, err = svc.FindUserForAuth(context.Background(), 999)
	require.Error(t, err)
}

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"testpilot/internal/model"
	"testpilot/internal/repository"
)

func TestUserService_CreateSuccess(t *testing.T) {
	db := testDB(t)
	userRepo, roleRepo, projectRepo, auditRepo, txMgr := testRepos(db)
	seedRoles(t, db)
	seedProject(t, db)
	svc := NewUserService(userRepo, roleRepo, projectRepo, auditRepo, txMgr)

	user, err := svc.Create(context.Background(), 1, CreateUserInput{
		Name: "New User", Email: "new@test.local", Phone: "13800001111",
		Role: "tester", RoleIDs: []uint{2}, ProjectIDs: []uint{1},
	})
	require.NoError(t, err)
	assert.NotZero(t, user.ID)
	assert.Equal(t, "New User", user.Name)
	assert.NotEmpty(t, user.PasswordHash)
}

func TestUserService_CreateDuplicateEmail(t *testing.T) {
	db := testDB(t)
	userRepo, roleRepo, projectRepo, auditRepo, txMgr := testRepos(db)
	seedAdmin(t, db)
	seedRoles(t, db)
	seedProject(t, db)
	svc := NewUserService(userRepo, roleRepo, projectRepo, auditRepo, txMgr)

	_, err := svc.Create(context.Background(), 1, CreateUserInput{
		Name: "Dup", Email: "admin@test.local", Phone: "13800002222",
		Role: "tester", RoleIDs: []uint{2}, ProjectIDs: []uint{1},
	})
	require.Error(t, err)
	bizErr, ok := err.(*BizError)
	require.True(t, ok)
	assert.Equal(t, 409, bizErr.Status)
}

func TestUserService_CreateAdminAssignBlocked(t *testing.T) {
	db := testDB(t)
	userRepo, roleRepo, projectRepo, auditRepo, txMgr := testRepos(db)
	seedRoles(t, db)
	seedProject(t, db)
	svc := NewUserService(userRepo, roleRepo, projectRepo, auditRepo, txMgr)

	_, err := svc.Create(context.Background(), 1, CreateUserInput{
		Name: "BadAdmin", Email: "badmin@test.local",
		Role: "admin", RoleIDs: []uint{1}, ProjectIDs: []uint{1},
	})
	require.Error(t, err)
	bizErr, ok := err.(*BizError)
	require.True(t, ok)
	assert.Equal(t, 400, bizErr.Status)
	assert.Equal(t, "ADMIN_ASSIGN_BLOCKED", bizErr.Code)
}

func TestUserService_CreateMissingFields(t *testing.T) {
	db := testDB(t)
	userRepo, roleRepo, projectRepo, auditRepo, txMgr := testRepos(db)
	svc := NewUserService(userRepo, roleRepo, projectRepo, auditRepo, txMgr)

	_, err := svc.Create(context.Background(), 1, CreateUserInput{})
	require.Error(t, err)
	bizErr, ok := err.(*BizError)
	require.True(t, ok)
	assert.Equal(t, 400, bizErr.Status)
}

func TestUserService_DeleteAdminProtected(t *testing.T) {
	db := testDB(t)
	userRepo, roleRepo, projectRepo, auditRepo, txMgr := testRepos(db)
	seedAdmin(t, db)
	seedRoles(t, db)
	svc := NewUserService(userRepo, roleRepo, projectRepo, auditRepo, txMgr)

	err := svc.Delete(context.Background(), 1, 1)
	require.Error(t, err)
	bizErr, ok := err.(*BizError)
	require.True(t, ok)
	assert.Equal(t, 409, bizErr.Status)
}

func TestUserService_DeleteSuccess(t *testing.T) {
	db := testDB(t)
	userRepo, roleRepo, projectRepo, auditRepo, txMgr := testRepos(db)
	seedAdmin(t, db)
	seedTester(t, db)
	seedRoles(t, db)
	seedProject(t, db)

	// 给 tester 分配角色和项目关系
	db.Create(&model.UserRole{UserID: 2, RoleID: 2})
	db.Create(&model.UserProject{UserID: 2, ProjectID: 1})

	svc := NewUserService(userRepo, roleRepo, projectRepo, auditRepo, txMgr)
	err := svc.Delete(context.Background(), 1, 2)
	require.NoError(t, err)

	// 验证已逻辑删除
	_, findErr := userRepo.FindByID(context.Background(), 2)
	assert.Error(t, findErr)
}

func TestUserService_List(t *testing.T) {
	db := testDB(t)
	userRepo, roleRepo, projectRepo, auditRepo, txMgr := testRepos(db)
	seedAdmin(t, db)
	seedTester(t, db)
	svc := NewUserService(userRepo, roleRepo, projectRepo, auditRepo, txMgr)

	users, err := svc.List(context.Background())
	require.NoError(t, err)
	assert.Len(t, users, 2)
}

func TestUserService_AssignRolesInvalid(t *testing.T) {
	db := testDB(t)
	userRepo, roleRepo, projectRepo, auditRepo, txMgr := testRepos(db)
	seedAdmin(t, db)
	seedRoles(t, db)
	svc := NewUserService(userRepo, roleRepo, projectRepo, auditRepo, txMgr)

	// 空 roleIDs
	err := svc.AssignRoles(context.Background(), 1, 1, []uint{})
	require.Error(t, err)

	// 无效 roleID
	err = svc.AssignRoles(context.Background(), 1, 1, []uint{999})
	require.Error(t, err)
}

func TestUserService_AssignProjects(t *testing.T) {
	db := testDB(t)
	userRepo, roleRepo, projectRepo, auditRepo, txMgr := testRepos(db)
	seedAdmin(t, db)
	seedProject(t, db)

	// 额外种入 test case and execution repos (needed by txMgr)
	testCaseRepo := repository.NewTestCaseRepo(db)
	_ = testCaseRepo

	svc := NewUserService(userRepo, roleRepo, projectRepo, auditRepo, txMgr)

	err := svc.AssignProjects(context.Background(), 1, 1, []uint{1})
	require.NoError(t, err)

	// 无效 projectID
	err = svc.AssignProjects(context.Background(), 1, 1, []uint{999})
	require.Error(t, err)
}

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"testpilot/internal/model"
)

func TestRoleService_CreateSuccess(t *testing.T) {
	db := testDB(t)
	_, roleRepo, _, auditRepo, txMgr := testRepos(db)
	seedAdmin(t, db)
	svc := NewRoleService(roleRepo, auditRepo, txMgr)

	role, err := svc.Create(context.Background(), 1, "devops", "DevOps", "devops role")
	require.NoError(t, err)
	assert.NotZero(t, role.ID)
	assert.Equal(t, "devops", role.Name)
}

func TestRoleService_CreateDuplicate(t *testing.T) {
	db := testDB(t)
	_, roleRepo, _, auditRepo, txMgr := testRepos(db)
	seedRoles(t, db)
	svc := NewRoleService(roleRepo, auditRepo, txMgr)

	_, err := svc.Create(context.Background(), 1, "admin", "Admin", "dup admin")
	require.Error(t, err)
}

func TestRoleService_DeletePresetProtected(t *testing.T) {
	db := testDB(t)
	_, roleRepo, _, auditRepo, txMgr := testRepos(db)
	seedRoles(t, db)
	svc := NewRoleService(roleRepo, auditRepo, txMgr)

	err := svc.Delete(context.Background(), 1, 1) // admin role
	require.Error(t, err)
	bizErr, ok := err.(*BizError)
	require.True(t, ok)
	assert.Equal(t, 409, bizErr.Status)
	assert.Contains(t, bizErr.Message, "preset")
}

func TestRoleService_DeleteInUse(t *testing.T) {
	db := testDB(t)
	_, roleRepo, _, auditRepo, txMgr := testRepos(db)
	seedAdmin(t, db)
	// 创建非预置角色，并分配给用户
	customRole := model.Role{Name: "custom-used", Description: "custom in use"}
	db.Create(&customRole)
	db.Create(&model.UserRole{UserID: 1, RoleID: customRole.ID})
	svc := NewRoleService(roleRepo, auditRepo, txMgr)

	err := svc.Delete(context.Background(), 1, customRole.ID)
	require.Error(t, err)
	bizErr, ok := err.(*BizError)
	require.True(t, ok)
	assert.Equal(t, 409, bizErr.Status)
	assert.Contains(t, bizErr.Message, "关联")
}

func TestRoleService_DeleteSuccess(t *testing.T) {
	db := testDB(t)
	_, roleRepo, _, auditRepo, txMgr := testRepos(db)
	seedAdmin(t, db)
	// 创建一个可删除的非预置角色
	custom := model.Role{Name: "custom-role", Description: "test"}
	db.Create(&custom)
	svc := NewRoleService(roleRepo, auditRepo, txMgr)

	err := svc.Delete(context.Background(), 1, custom.ID)
	require.NoError(t, err)
}

func TestRoleService_List(t *testing.T) {
	db := testDB(t)
	_, roleRepo, _, auditRepo, txMgr := testRepos(db)
	seedRoles(t, db)
	svc := NewRoleService(roleRepo, auditRepo, txMgr)

	roles, err := svc.List(context.Background())
	require.NoError(t, err)
	assert.Len(t, roles, 3)
}

func TestRoleService_UpdateSuccess(t *testing.T) {
	db := testDB(t)
	_, roleRepo, _, auditRepo, txMgr := testRepos(db)
	seedAdmin(t, db)
	svc := NewRoleService(roleRepo, auditRepo, txMgr)

	// 这里先创建一个自定义角色，再覆盖“允许修改标识名”的更新路径，避免误撞预置角色保护规则。
	created, err := svc.Create(context.Background(), 1, "custom-editor", "自定义角色", "original desc")
	require.NoError(t, err)

	name := "custom-editor-updated"
	displayName := "自定义角色-已更新"
	desc := "updated desc"
	updated, err := svc.Update(context.Background(), 1, created.ID, &name, &displayName, &desc)
	require.NoError(t, err)
	assert.Equal(t, "custom-editor-updated", updated.Name)
	assert.Equal(t, "自定义角色-已更新", updated.DisplayName)
	assert.Equal(t, "updated desc", updated.Description)
}

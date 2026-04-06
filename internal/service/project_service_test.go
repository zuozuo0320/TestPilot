package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"testpilot/internal/model"
)

func TestProjectService_CreateSuccess(t *testing.T) {
	db := testDB(t)
	userRepo, _, projectRepo, auditRepo, txMgr := testRepos(db)
	seedAdmin(t, db)
	svc := NewProjectService(testLogger(), projectRepo, userRepo, auditRepo, txMgr)

	project, err := svc.Create(context.Background(), 1, CreateProjectInput{Name: "Test Project", Description: "desc"})
	require.NoError(t, err)
	assert.NotZero(t, project.ID)
	assert.Equal(t, "Test Project", project.Name)
	assert.Equal(t, uint(1), project.OwnerID)
}

func TestProjectService_ListAdmin(t *testing.T) {
	db := testDB(t)
	userRepo, _, projectRepo, auditRepo, txMgr := testRepos(db)
	admin := seedAdmin(t, db)
	seedProject(t, db)
	svc := NewProjectService(testLogger(), projectRepo, userRepo, auditRepo, txMgr)

	projects, err := svc.List(context.Background(), admin)
	require.NoError(t, err)
	assert.NotEmpty(t, projects)
}

func TestProjectService_RequireAccess_AdminBypass(t *testing.T) {
	db := testDB(t)
	userRepo, _, projectRepo, auditRepo, txMgr := testRepos(db)
	admin := seedAdmin(t, db)
	seedProject(t, db)
	svc := NewProjectService(testLogger(), projectRepo, userRepo, auditRepo, txMgr)

	err := svc.RequireAccess(context.Background(), admin, 1)
	require.NoError(t, err)
}

func TestProjectService_RequireAccess_NonMemberDenied(t *testing.T) {
	db := testDB(t)
	userRepo, _, projectRepo, auditRepo, txMgr := testRepos(db)
	seedProject(t, db)
	outsider := model.User{
		ID: 99, Name: "Outsider", Email: "out@test.local",
		Role: model.GlobalRoleTester, Active: true,
	}
	db.Create(&outsider)
	svc := NewProjectService(testLogger(), projectRepo, userRepo, auditRepo, txMgr)

	err := svc.RequireAccess(context.Background(), outsider, 1)
	require.Error(t, err)
	bizErr, ok := err.(*BizError)
	require.True(t, ok)
	assert.Equal(t, 403, bizErr.Status)
}

func TestProjectService_AddMember(t *testing.T) {
	db := testDB(t)
	userRepo, _, projectRepo, auditRepo, txMgr := testRepos(db)
	seedAdmin(t, db)
	seedTester(t, db)
	seedProject(t, db)
	svc := NewProjectService(testLogger(), projectRepo, userRepo, auditRepo, txMgr)

	member, err := svc.AddMember(context.Background(), 1, 2, model.MemberRoleMember)
	require.NoError(t, err)
	assert.Equal(t, uint(2), member.UserID)
}

func TestProjectService_UpdateOwnerSuccess(t *testing.T) {
	db := testDB(t)
	userRepo, _, projectRepo, auditRepo, txMgr := testRepos(db)
	seedAdmin(t, db)
	seedTester(t, db)
	seedProject(t, db)
	require.NoError(t, db.Create(&model.ProjectMember{ProjectID: 1, UserID: 1, Role: model.MemberRoleOwner}).Error)
	svc := NewProjectService(testLogger(), projectRepo, userRepo, auditRepo, txMgr)

	ownerID := uint(2)
	project, err := svc.Update(context.Background(), 1, 1, UpdateProjectInput{OwnerID: &ownerID})
	require.NoError(t, err)
	assert.Equal(t, uint(2), project.OwnerID)

	var members []model.ProjectMember
	require.NoError(t, db.Where("project_id = ?", 1).Order("user_id asc").Find(&members).Error)
	require.Len(t, members, 2)
	assert.Equal(t, model.MemberRoleMember, members[0].Role)
	assert.Equal(t, model.MemberRoleOwner, members[1].Role)
}

func TestProjectService_RemoveOwnerRejected(t *testing.T) {
	db := testDB(t)
	userRepo, _, projectRepo, auditRepo, txMgr := testRepos(db)
	seedAdmin(t, db)
	seedProject(t, db)
	require.NoError(t, db.Create(&model.ProjectMember{ProjectID: 1, UserID: 1, Role: model.MemberRoleOwner}).Error)
	svc := NewProjectService(testLogger(), projectRepo, userRepo, auditRepo, txMgr)

	err := svc.RemoveMember(context.Background(), 1, 1)
	require.Error(t, err)
	bizErr, ok := err.(*BizError)
	require.True(t, ok)
	assert.Equal(t, CodeProjectOwnerLocked, bizErr.Code)
}

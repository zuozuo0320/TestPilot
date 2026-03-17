package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"testpilot/internal/model"
	"testpilot/internal/repository"
)

func TestProjectService_CreateSuccess(t *testing.T) {
	db := testDB(t)
	_, _, projectRepo, _, _ := testRepos(db)
	userRepo := repository.NewUserRepo(db)
	seedAdmin(t, db)
	svc := NewProjectService(projectRepo, userRepo)

	project, err := svc.Create(context.Background(), 1, "Test Project", "desc")
	require.NoError(t, err)
	assert.NotZero(t, project.ID)
	assert.Equal(t, "Test Project", project.Name)
}

func TestProjectService_ListAdmin(t *testing.T) {
	db := testDB(t)
	_, _, projectRepo, _, _ := testRepos(db)
	userRepo := repository.NewUserRepo(db)
	admin := seedAdmin(t, db)
	seedProject(t, db)
	svc := NewProjectService(projectRepo, userRepo)

	projects, err := svc.List(context.Background(), admin)
	require.NoError(t, err)
	assert.NotEmpty(t, projects)
}

func TestProjectService_RequireAccess_AdminBypass(t *testing.T) {
	db := testDB(t)
	_, _, projectRepo, _, _ := testRepos(db)
	userRepo := repository.NewUserRepo(db)
	admin := seedAdmin(t, db)
	seedProject(t, db)
	svc := NewProjectService(projectRepo, userRepo)

	err := svc.RequireAccess(context.Background(), admin, 1)
	require.NoError(t, err)
}

func TestProjectService_RequireAccess_NonMemberDenied(t *testing.T) {
	db := testDB(t)
	_, _, projectRepo, _, _ := testRepos(db)
	userRepo := repository.NewUserRepo(db)
	seedProject(t, db)
	outsider := model.User{
		ID: 99, Name: "Outsider", Email: "out@test.local",
		Role: model.GlobalRoleTester, Active: true,
	}
	db.Create(&outsider)
	svc := NewProjectService(projectRepo, userRepo)

	err := svc.RequireAccess(context.Background(), outsider, 1)
	require.Error(t, err)
	bizErr, ok := err.(*BizError)
	require.True(t, ok)
	assert.Equal(t, 403, bizErr.Status)
}

func TestProjectService_AddMember(t *testing.T) {
	db := testDB(t)
	_, _, projectRepo, _, _ := testRepos(db)
	userRepo := repository.NewUserRepo(db)
	seedAdmin(t, db)
	seedTester(t, db)
	seedProject(t, db)
	svc := NewProjectService(projectRepo, userRepo)

	member, err := svc.AddMember(context.Background(), 1, 2, model.MemberRoleMember)
	require.NoError(t, err)
	assert.Equal(t, uint(2), member.UserID)
}

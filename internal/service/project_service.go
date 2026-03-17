// project_service.go — 项目管理业务逻辑
package service

import (
	"context"
	"strings"

	"testpilot/internal/model"
	"testpilot/internal/repository"
)

// ProjectService 项目管理服务
type ProjectService struct {
	projectRepo repository.ProjectRepository
	userRepo    repository.UserRepository
}

// NewProjectService 创建项目服务
func NewProjectService(projectRepo repository.ProjectRepository, userRepo repository.UserRepository) *ProjectService {
	return &ProjectService{projectRepo: projectRepo, userRepo: userRepo}
}

// Create 创建项目
func (s *ProjectService) Create(ctx context.Context, userID uint, name, description string) (*model.Project, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrBadRequest("MISSING_NAME", "project name is required")
	}
	project := model.Project{Name: name, Description: strings.TrimSpace(description)}
	if err := s.projectRepo.Create(ctx, &project); err != nil {
		if isDuplicateError(err) {
			return nil, ErrConflict("PROJECT_EXISTS", "project already exists")
		}
		return nil, ErrInternal("DB_ERROR", err)
	}
	member := model.ProjectMember{ProjectID: project.ID, UserID: userID, Role: model.MemberRoleOwner}
	if err := s.projectRepo.AddMember(ctx, &member); err != nil {
		return nil, ErrInternal("DB_ERROR", err)
	}
	return &project, nil
}

// List 获取项目列表（admin 看全部，其他看自己的）
func (s *ProjectService) List(ctx context.Context, user model.User) ([]model.Project, error) {
	if user.Role == model.GlobalRoleAdmin {
		return s.projectRepo.List(ctx)
	}
	return s.projectRepo.ListByUserID(ctx, user.ID)
}

// AddMember 添加项目成员
func (s *ProjectService) AddMember(ctx context.Context, projectID, userID uint, role string) (*model.ProjectMember, error) {
	if userID == 0 || !model.IsValidMemberRole(role) {
		return nil, ErrBadRequest("INVALID_PARAMS", "user_id/role is invalid")
	}
	if _, err := s.userRepo.FindByID(ctx, userID); err != nil {
		return nil, ErrNotFound("USER_NOT_FOUND", "target user not found")
	}
	member := model.ProjectMember{ProjectID: projectID, UserID: userID, Role: role}
	if err := s.projectRepo.AddMember(ctx, &member); err != nil {
		return nil, ErrInternal("DB_ERROR", err)
	}
	return &member, nil
}

// ListMembers 获取项目成员列表
func (s *ProjectService) ListMembers(ctx context.Context, projectID uint) ([]model.ProjectMember, error) {
	return s.projectRepo.ListMembers(ctx, projectID)
}

// RequireAccess 校验用户是否有项目访问权限
func (s *ProjectService) RequireAccess(ctx context.Context, user model.User, projectID uint) error {
	exists, err := s.projectRepo.Exists(ctx, projectID)
	if err != nil || !exists {
		return ErrProjectNotFound
	}
	if user.Role == model.GlobalRoleAdmin {
		return nil
	}
	isMember, err := s.projectRepo.IsMember(ctx, projectID, user.ID)
	if err != nil {
		return ErrInternal("DB_ERROR", err)
	}
	if !isMember {
		return ErrNoProjectAccess
	}
	return nil
}

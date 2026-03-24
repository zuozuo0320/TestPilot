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
	auditRepo   repository.AuditRepository
	txMgr       *repository.TxManager
}

// NewProjectService 创建项目服务
func NewProjectService(
	projectRepo repository.ProjectRepository,
	userRepo repository.UserRepository,
	auditRepo repository.AuditRepository,
	txMgr *repository.TxManager,
) *ProjectService {
	return &ProjectService{
		projectRepo: projectRepo,
		userRepo:    userRepo,
		auditRepo:   auditRepo,
		txMgr:       txMgr,
	}
}

// Create 创建项目
// 自动设置 status=active，创建后将创建者设为 owner
func (s *ProjectService) Create(ctx context.Context, userID uint, name, description, avatar string) (*model.Project, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrBadRequest("MISSING_NAME", "project name is required")
	}
	// 项目名称全局唯一
	exists, err := s.projectRepo.ExistsByName(ctx, name, 0)
	if err != nil {
		return nil, ErrInternal("DB_ERROR", err)
	}
	if exists {
		return nil, ErrConflict("PROJECT_EXISTS", "项目名称已存在")
	}
	project := model.Project{
		Name:        name,
		Description: strings.TrimSpace(description),
		Avatar:      strings.TrimSpace(avatar),
		Status:      model.ProjectStatusActive,
	}
	if err := s.projectRepo.Create(ctx, &project); err != nil {
		return nil, ErrInternal("DB_ERROR", err)
	}
	// 将创建者设为项目 owner
	member := model.ProjectMember{ProjectID: project.ID, UserID: userID, Role: model.MemberRoleOwner}
	if err := s.projectRepo.AddMember(ctx, &member); err != nil {
		return nil, ErrInternal("DB_ERROR", err)
	}
	return &project, nil
}

// Update 更新项目（名称、描述、头像）
// 名称唯一性校验，归档项目不可编辑
func (s *ProjectService) Update(ctx context.Context, actorID, projectID uint, name, description, avatar *string) (*model.Project, error) {
	project, err := s.projectRepo.FindByID(ctx, projectID)
	if err != nil {
		return nil, ErrProjectNotFound
	}
	if model.IsArchivedProject(project.Status) {
		return nil, ErrProjectArchived
	}
	updates := map[string]any{}
	if name != nil {
		n := strings.TrimSpace(*name)
		if n == "" {
			return nil, ErrBadRequest("INVALID_NAME", "project name is required")
		}
		// 名称唯一性
		exists, err := s.projectRepo.ExistsByName(ctx, n, projectID)
		if err != nil {
			return nil, ErrInternal("DB_ERROR", err)
		}
		if exists {
			return nil, ErrConflict("PROJECT_EXISTS", "项目名称已存在")
		}
		updates["name"] = n
	}
	if description != nil {
		updates["description"] = strings.TrimSpace(*description)
	}
	if avatar != nil {
		updates["avatar"] = strings.TrimSpace(*avatar)
	}
	if len(updates) == 0 {
		return nil, ErrBadRequest("NO_FIELDS", "no valid fields to update")
	}
	if err := s.projectRepo.Updates(ctx, projectID, updates); err != nil {
		return nil, ErrInternal("DB_ERROR", err)
	}
	return s.projectRepo.FindByID(ctx, projectID)
}

// List 获取项目列表（admin 看全部，其他看自己的）
// 项目选择器和项目管理页面使用不同的可见范围
func (s *ProjectService) List(ctx context.Context, user model.User) ([]model.Project, error) {
	if user.Role == model.GlobalRoleAdmin {
		return s.projectRepo.List(ctx)
	}
	return s.projectRepo.ListByUserIDIncludeArchived(ctx, user.ID)
}

// ListForSelector 获取项目选择器的项目列表
// admin 看全部，普通用户只看已加入且活跃的项目
func (s *ProjectService) ListForSelector(ctx context.Context, user model.User) ([]model.Project, error) {
	if user.Role == model.GlobalRoleAdmin {
		return s.projectRepo.List(ctx)
	}
	return s.projectRepo.ListByUserID(ctx, user.ID)
}

// Archive 归档项目
// 归档后数据只读可查，不可新增/编辑任何业务数据
func (s *ProjectService) Archive(ctx context.Context, actorID, projectID uint) error {
	project, err := s.projectRepo.FindByID(ctx, projectID)
	if err != nil {
		return ErrProjectNotFound
	}
	// 种子项目不可归档
	if model.IsSeedProject(project.Name) {
		return ErrSeedProjectProtected
	}
	if model.IsArchivedProject(project.Status) {
		return ErrProjectArchived
	}
	return s.projectRepo.Updates(ctx, projectID, repository.ArchiveFields())
}

// Restore 恢复已归档的项目（仅 admin 可操作）
func (s *ProjectService) Restore(ctx context.Context, actorID, projectID uint) error {
	project, err := s.projectRepo.FindByID(ctx, projectID)
	if err != nil {
		return ErrProjectNotFound
	}
	if !model.IsArchivedProject(project.Status) {
		return ErrProjectNotArchived
	}
	return s.projectRepo.Updates(ctx, projectID, repository.RestoreFields())
}

// Delete 删除项目
// 前提：已归档 + 无用例 + 无缺陷 + 非种子项目
func (s *ProjectService) Delete(ctx context.Context, actorID, projectID uint) error {
	project, err := s.projectRepo.FindByID(ctx, projectID)
	if err != nil {
		return ErrProjectNotFound
	}
	// 种子项目不可删除
	if model.IsSeedProject(project.Name) {
		return ErrSeedProjectProtected
	}
	// 必须已归档
	if !model.IsArchivedProject(project.Status) {
		return ErrBadRequest("NOT_ARCHIVED", "项目必须先归档后才可删除")
	}
	// 检查用例数
	tcCount, err := s.projectRepo.CountTestCases(ctx, projectID)
	if err != nil {
		return ErrInternal("DB_ERROR", err)
	}
	if tcCount > 0 {
		return ErrProjectNotEmpty
	}
	// 检查缺陷数
	defectCount, err := s.projectRepo.CountDefects(ctx, projectID)
	if err != nil {
		return ErrInternal("DB_ERROR", err)
	}
	if defectCount > 0 {
		return ErrProjectNotEmpty
	}
	// 清理成员关系后删除项目
	if err := s.projectRepo.DeleteAllMembers(ctx, projectID); err != nil {
		return ErrInternal("DB_ERROR", err)
	}
	return s.projectRepo.Delete(ctx, projectID)
}

// AddMember 添加项目成员
func (s *ProjectService) AddMember(ctx context.Context, projectID, userID uint, role string) (*model.ProjectMember, error) {
	if userID == 0 || !model.IsValidMemberRole(role) {
		return nil, ErrBadRequest("INVALID_PARAMS", "user_id/role is invalid")
	}
	// 检查项目是否存在
	project, err := s.projectRepo.FindByID(ctx, projectID)
	if err != nil {
		return nil, ErrProjectNotFound
	}
	if model.IsArchivedProject(project.Status) {
		return nil, ErrProjectArchived
	}
	// 检查目标用户是否存在
	if _, err := s.userRepo.FindByID(ctx, userID); err != nil {
		return nil, ErrNotFound("USER_NOT_FOUND", "target user not found")
	}
	member := model.ProjectMember{ProjectID: projectID, UserID: userID, Role: role}
	if err := s.projectRepo.AddMember(ctx, &member); err != nil {
		return nil, ErrInternal("DB_ERROR", err)
	}
	return &member, nil
}

// RemoveMember 移除项目成员
// 受保护成员（全局角色为 admin/manager）不可被移除，防止 API 层绕过前端限制
func (s *ProjectService) RemoveMember(ctx context.Context, projectID, userID uint) error {
	project, err := s.projectRepo.FindByID(ctx, projectID)
	if err != nil {
		return ErrProjectNotFound
	}
	if model.IsArchivedProject(project.Status) {
		return ErrProjectArchived
	}
	// 校验目标用户的全局角色，admin/manager 为受保护成员不可移除
	targetUser, err := s.userRepo.FindByID(ctx, userID)
	if err != nil {
		return ErrNotFound("USER_NOT_FOUND", "target user not found")
	}
	// 这里同时检查主角色缓存和角色绑定表，避免多角色成员绕过前端禁删限制。
	isProtected, err := s.hasProtectedGlobalRole(ctx, targetUser)
	if err != nil {
		return err
	}
	if isProtected {
		return ErrForbidden("PROTECTED_MEMBER", "admin/manager 角色的成员不可移除")
	}
	return s.projectRepo.RemoveMember(ctx, projectID, userID)
}

// ListMembers 获取项目成员列表
func (s *ProjectService) ListMembers(ctx context.Context, projectID uint) ([]model.ProjectMember, error) {
	return s.projectRepo.ListMembers(ctx, projectID)
}

// RequireAccess 校验用户是否有项目访问权限
// admin 自动拥有所有项目访问权
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

// hasProtectedGlobalRole 判断目标用户是否拥有受保护的全局角色。
// 这里同时检查缓存主角色和角色关联表，避免多角色场景下被绕过。
func (s *ProjectService) hasProtectedGlobalRole(ctx context.Context, user *model.User) (bool, error) {
	if user == nil {
		return false, ErrNotFound("USER_NOT_FOUND", "target user not found")
	}
	if model.IsProtectedGlobalRole(strings.TrimSpace(user.Role)) {
		return true, nil
	}

	hasAdmin, err := s.userRepo.HasRoleName(ctx, user.ID, model.GlobalRoleAdmin)
	if err != nil {
		return false, ErrInternal("DB_ERROR", err)
	}
	if hasAdmin {
		return true, nil
	}

	hasManager, err := s.userRepo.HasRoleName(ctx, user.ID, model.GlobalRoleManager)
	if err != nil {
		return false, ErrInternal("DB_ERROR", err)
	}
	// manager 既可能落在主角色缓存，也可能只存在于角色绑定表，所以这里需要补一次关联查询。
	return hasManager, nil
}

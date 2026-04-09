// project_service.go — 项目管理业务逻辑
package service

import (
	"context"
	"log/slog"
	"math"
	"strings"

	"gorm.io/gorm"
	"testpilot/internal/model"
	"testpilot/internal/repository"
)

type CreateProjectInput struct {
	Name        string
	Description string
	Avatar      string
	OwnerID     *uint
}

type UpdateProjectInput struct {
	Name        *string
	Description *string
	Avatar      *string
	OwnerID     *uint
}

// ProjectService 项目管理服务
type ProjectService struct {
	logger      *slog.Logger
	projectRepo repository.ProjectRepository
	userRepo    repository.UserRepository
	auditRepo   repository.AuditRepository
	txMgr       *repository.TxManager
}

// NewProjectService 创建项目服务
func NewProjectService(
	logger *slog.Logger,
	projectRepo repository.ProjectRepository,
	userRepo repository.UserRepository,
	auditRepo repository.AuditRepository,
	txMgr *repository.TxManager,
) *ProjectService {
	return &ProjectService{
		logger:      logger.With("module", "project"),
		projectRepo: projectRepo,
		userRepo:    userRepo,
		auditRepo:   auditRepo,
		txMgr:       txMgr,
	}
}

// Create 创建项目
// 自动设置 status=active，创建后将创建者设为 owner
func (s *ProjectService) Create(ctx context.Context, userID uint, input CreateProjectInput) (*model.Project, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, ErrBadRequest(CodeParamsError, "project name is required")
	}
	// 项目名称全局唯一
	exists, err := s.projectRepo.ExistsByName(ctx, name, 0)
	if err != nil {
		s.logger.Error("check project name exists failed", "actor_id", userID, "project_name", name, "error", err)
		return nil, ErrInternal(CodeInternal, err)
	}
	if exists {
		return nil, ErrConflict(CodeConflict, "项目名称已存在")
	}
	ownerID, err := s.resolveOwnerID(ctx, userID, input.OwnerID)
	if err != nil {
		return nil, err
	}
	project := model.Project{
		Name:        name,
		Description: strings.TrimSpace(input.Description),
		Avatar:      strings.TrimSpace(input.Avatar),
		OwnerID:     ownerID,
		Status:      model.ProjectStatusActive,
	}
	s.logger.Info("create project start", "actor_id", userID, "owner_id", ownerID, "project_name", name)
	if err := s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		if err := s.projectRepo.CreateTx(ctx, tx, &project); err != nil {
			s.logger.Error("create project record failed", "actor_id", userID, "project_name", name, "error", err)
			return err
		}
		ownerMember := model.ProjectMember{ProjectID: project.ID, UserID: ownerID, Role: model.MemberRoleOwner}
		if err := s.projectRepo.AddMemberTx(ctx, tx, &ownerMember); err != nil {
			s.logger.Error("create project owner member failed", "actor_id", userID, "project_id", project.ID, "owner_id", ownerID, "error", err)
			return err
		}
		if ownerID != userID {
			creatorMember := model.ProjectMember{ProjectID: project.ID, UserID: userID, Role: model.MemberRoleMember}
			if err := s.projectRepo.AddMemberTx(ctx, tx, &creatorMember); err != nil {
				s.logger.Error("create project creator member failed", "actor_id", userID, "project_id", project.ID, "error", err)
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}
	created, err := s.projectRepo.FindByID(ctx, project.ID)
	if err != nil {
		s.logger.Error("load created project failed", "actor_id", userID, "project_id", project.ID, "error", err)
		return nil, ErrInternal(CodeInternal, err)
	}
	s.decorateProject(created)
	s.logger.Info("create project success", "actor_id", userID, "project_id", created.ID, "owner_id", created.OwnerID)
	return created, nil
}

// Update 更新项目（名称、描述、头像）
// 名称唯一性校验，归档项目不可编辑
func (s *ProjectService) Update(ctx context.Context, actorID, projectID uint, input UpdateProjectInput) (*model.Project, error) {
	project, err := s.projectRepo.FindByID(ctx, projectID)
	if err != nil {
		return nil, ErrProjectNotFound
	}
	if model.IsArchivedProject(project.Status) {
		return nil, ErrProjectArchived
	}
	updates := map[string]any{}
	if input.Name != nil {
		n := strings.TrimSpace(*input.Name)
		if n == "" {
			return nil, ErrBadRequest(CodeParamsError, "project name is required")
		}
		// 名称唯一性
		exists, err := s.projectRepo.ExistsByName(ctx, n, projectID)
		if err != nil {
			s.logger.Error("check project name exists failed", "actor_id", actorID, "project_id", projectID, "project_name", n, "error", err)
			return nil, ErrInternal(CodeInternal, err)
		}
		if exists {
			return nil, ErrConflict(CodeConflict, "项目名称已存在")
		}
		updates["name"] = n
	}
	if input.Description != nil {
		updates["description"] = strings.TrimSpace(*input.Description)
	}
	if input.Avatar != nil {
		updates["avatar"] = strings.TrimSpace(*input.Avatar)
	}
	var nextOwnerID uint
	ownerChanged := false
	if input.OwnerID != nil {
		nextOwnerID, err = s.resolveOwnerID(ctx, actorID, input.OwnerID)
		if err != nil {
			return nil, err
		}
		ownerChanged = nextOwnerID != project.OwnerID
		if ownerChanged {
			updates["owner_id"] = nextOwnerID
		}
	}
	if len(updates) == 0 && !ownerChanged {
		return nil, ErrBadRequest(CodeParamsError, "no valid fields to update")
	}
	s.logger.Info("update project start", "actor_id", actorID, "project_id", projectID, "owner_changed", ownerChanged)
	if err := s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		if len(updates) > 0 {
			if err := s.projectRepo.UpdatesTx(ctx, tx, projectID, updates); err != nil {
				s.logger.Error("update project fields failed", "actor_id", actorID, "project_id", projectID, "error", err)
				return err
			}
		}
		if !ownerChanged {
			return nil
		}
		ownerMember := model.ProjectMember{ProjectID: projectID, UserID: nextOwnerID, Role: model.MemberRoleOwner}
		if err := s.projectRepo.AddMemberTx(ctx, tx, &ownerMember); err != nil {
			s.logger.Error("upsert new owner member failed", "actor_id", actorID, "project_id", projectID, "owner_id", nextOwnerID, "error", err)
			return err
		}
		if project.OwnerID > 0 && project.OwnerID != nextOwnerID {
			oldOwnerMember := model.ProjectMember{ProjectID: projectID, UserID: project.OwnerID, Role: model.MemberRoleMember}
			if err := s.projectRepo.AddMemberTx(ctx, tx, &oldOwnerMember); err != nil {
				s.logger.Error("demote old owner failed", "actor_id", actorID, "project_id", projectID, "old_owner_id", project.OwnerID, "error", err)
				return err
			}
		}
		if err := s.projectRepo.DemoteOtherOwnersTx(ctx, tx, projectID, nextOwnerID); err != nil {
			s.logger.Error("cleanup redundant owners failed", "actor_id", actorID, "project_id", projectID, "owner_id", nextOwnerID, "error", err)
			return err
		}
		return nil
	}); err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}
	updated, err := s.projectRepo.FindByID(ctx, projectID)
	if err != nil {
		s.logger.Error("load updated project failed", "actor_id", actorID, "project_id", projectID, "error", err)
		return nil, ErrInternal(CodeInternal, err)
	}
	s.decorateProject(updated)
	s.logger.Info("update project success", "actor_id", actorID, "project_id", projectID, "owner_id", updated.OwnerID)
	return updated, nil
}

// List 获取项目列表（admin 看全部，其他看自己的）
// 项目选择器和项目管理页面使用不同的可见范围
func (s *ProjectService) List(ctx context.Context, user model.User) ([]model.Project, error) {
	var (
		projects []model.Project
		err      error
	)
	if user.Role == model.GlobalRoleAdmin {
		projects, err = s.projectRepo.List(ctx)
	} else {
		projects, err = s.projectRepo.ListByUserIDIncludeArchived(ctx, user.ID)
	}
	if err != nil {
		return nil, err
	}
	s.decorateProjects(projects)
	return projects, nil
}

// ListForSelector 获取项目选择器的项目列表
// admin 看全部，普通用户只看已加入且活跃的项目
func (s *ProjectService) ListForSelector(ctx context.Context, user model.User) ([]model.Project, error) {
	var (
		projects []model.Project
		err      error
	)
	if user.Role == model.GlobalRoleAdmin {
		projects, err = s.projectRepo.List(ctx)
	} else {
		projects, err = s.projectRepo.ListByUserID(ctx, user.ID)
	}
	if err != nil {
		return nil, err
	}
	s.decorateProjects(projects)
	return projects, nil
}

func (s *ProjectService) decorateProjects(projects []model.Project) {
	for index := range projects {
		s.decorateProject(&projects[index])
	}
}

func (s *ProjectService) decorateProject(project *model.Project) {
	if project == nil {
		return
	}
	project.TestCaseTotalCount = project.TestCaseCount
	project.TestProgress = calculateProjectProgress(project.TestCaseTotalCount, project.ExecutedTestCaseCount)
	if project.TestCaseTotalCount == 0 {
		project.QualityStatus = model.ProjectQualityStatusUnknown
		project.QualityReason = model.ProjectQualityReasonNoTestCases
		return
	}
	if project.LatestRunPassRate != nil && project.LatestRunResultCount > 0 {
		applyProjectQualityByPassRate(
			project,
			*project.LatestRunPassRate,
			model.ProjectQualityReasonLatestRunPassRateGE95,
			model.ProjectQualityReasonLatestRunPassRate80To95,
			model.ProjectQualityReasonLatestRunPassRateBelow80,
		)
		return
	}
	if project.ExecutedTestCaseCount > 0 {
		casePassRate := (float64(project.PassedTestCaseCount) / float64(project.ExecutedTestCaseCount)) * 100
		applyProjectQualityByPassRate(
			project,
			casePassRate,
			model.ProjectQualityReasonCasePassRateGE95,
			model.ProjectQualityReasonCasePassRate80To95,
			model.ProjectQualityReasonCasePassRateBelow80,
		)
		return
	}
	project.QualityStatus = model.ProjectQualityStatusUnknown
	project.QualityReason = model.ProjectQualityReasonNoExecutionData
}

func applyProjectQualityByPassRate(
	project *model.Project,
	passRate float64,
	stableReason string,
	degradedReason string,
	failingReason string,
) {
	switch {
	case passRate >= 95:
		project.QualityStatus = model.ProjectQualityStatusStable
		project.QualityReason = stableReason
	case passRate >= 80:
		project.QualityStatus = model.ProjectQualityStatusDegraded
		project.QualityReason = degradedReason
	default:
		project.QualityStatus = model.ProjectQualityStatusFailing
		project.QualityReason = failingReason
	}
}

func calculateProjectProgress(total, executed int64) float64 {
	if total <= 0 || executed <= 0 {
		return 0
	}
	progress := (float64(executed) / float64(total)) * 100
	if progress > 100 {
		progress = 100
	}
	return math.Round(progress*10) / 10
}

// Archive 归档项目
// 归档后数据只读可查，不可新增/编辑任何业务数据
func (s *ProjectService) Archive(ctx context.Context, actorID, projectID uint) error {
	project, err := s.projectRepo.FindByID(ctx, projectID)
	if err != nil {
		return ErrProjectNotFound
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
// 前提：已归档 + 无用例 + 无缺陷
func (s *ProjectService) Delete(ctx context.Context, actorID, projectID uint) error {
	project, err := s.projectRepo.FindByID(ctx, projectID)
	if err != nil {
		return ErrProjectNotFound
	}
	// 必须已归档
	if !model.IsArchivedProject(project.Status) {
		return ErrBadRequest(CodeParamsError, "项目必须先归档后才可删除")
	}
	// 检查用例数
	tcCount, err := s.projectRepo.CountTestCases(ctx, projectID)
	if err != nil {
		return ErrInternal(CodeInternal, err)
	}
	if tcCount > 0 {
		return ErrProjectNotEmpty
	}
	// 检查缺陷数
	defectCount, err := s.projectRepo.CountDefects(ctx, projectID)
	if err != nil {
		return ErrInternal(CodeInternal, err)
	}
	if defectCount > 0 {
		return ErrProjectNotEmpty
	}
	// 清理成员关系后删除项目
	if err := s.projectRepo.DeleteAllMembers(ctx, projectID); err != nil {
		return ErrInternal(CodeInternal, err)
	}
	return s.projectRepo.Delete(ctx, projectID)
}

// AddMember 添加项目成员
func (s *ProjectService) AddMember(ctx context.Context, projectID, userID uint, role string) (*model.ProjectMember, error) {
	if userID == 0 || !model.IsValidMemberRole(role) {
		return nil, ErrBadRequest(CodeParamsError, "user_id/role is invalid")
	}
	if role == model.MemberRoleOwner {
		return nil, ErrBadRequest(CodeProjectOwnerByMember, "请通过项目编辑修改负责人，成员接口不支持直接设置负责人")
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
	targetUser, err := s.userRepo.FindByID(ctx, userID)
	if err != nil {
		return nil, ErrNotFound(CodeNotFound, "target user not found")
	}
	if !targetUser.Active {
		return nil, ErrBadRequest(CodeProjectOwnerInvalid, "目标用户已禁用，不可添加为项目成员")
	}
	member := model.ProjectMember{ProjectID: projectID, UserID: userID, Role: role}
	if err := s.projectRepo.AddMember(ctx, &member); err != nil {
		s.logger.Error("add project member failed", "project_id", projectID, "user_id", userID, "error", err)
		return nil, ErrInternal(CodeInternal, err)
	}
	s.logger.Info("add project member success", "project_id", projectID, "user_id", userID)
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
	if project.OwnerID == userID {
		return ErrBadRequest(CodeProjectOwnerLocked, "当前负责人不可直接移除，请先转交负责人")
	}
	// 校验目标用户的全局角色，admin/manager 为受保护成员不可移除
	targetUser, err := s.userRepo.FindByID(ctx, userID)
	if err != nil {
		return ErrNotFound(CodeNotFound, "target user not found")
	}
	// 这里同时检查主角色缓存和角色绑定表，避免多角色成员绕过前端禁删限制。
	isProtected, err := s.hasProtectedGlobalRole(ctx, targetUser)
	if err != nil {
		return err
	}
	if isProtected {
		return ErrForbidden(CodeForbidden, "admin/manager 角色的成员不可移除")
	}
	if err := s.projectRepo.RemoveMember(ctx, projectID, userID); err != nil {
		s.logger.Error("remove project member failed", "project_id", projectID, "user_id", userID, "error", err)
		return ErrInternal(CodeInternal, err)
	}
	s.logger.Info("remove project member success", "project_id", projectID, "user_id", userID)
	return nil
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
		return ErrInternal(CodeInternal, err)
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
		return false, ErrNotFound(CodeNotFound, "target user not found")
	}
	if model.IsProtectedGlobalRole(strings.TrimSpace(user.Role)) {
		return true, nil
	}

	hasAdmin, err := s.userRepo.HasRoleName(ctx, user.ID, model.GlobalRoleAdmin)
	if err != nil {
		return false, ErrInternal(CodeInternal, err)
	}
	if hasAdmin {
		return true, nil
	}

	hasManager, err := s.userRepo.HasRoleName(ctx, user.ID, model.GlobalRoleManager)
	if err != nil {
		return false, ErrInternal(CodeInternal, err)
	}
	// manager 既可能落在主角色缓存，也可能只存在于角色绑定表，所以这里需要补一次关联查询。
	return hasManager, nil
}

func (s *ProjectService) resolveOwnerID(ctx context.Context, actorID uint, ownerID *uint) (uint, error) {
	targetOwnerID := actorID
	if ownerID != nil {
		targetOwnerID = *ownerID
	}
	ownerUser, err := s.userRepo.FindByID(ctx, targetOwnerID)
	if err != nil {
		return 0, ErrBadRequest(CodeProjectOwnerInvalid, "负责人用户不存在")
	}
	if !ownerUser.Active {
		return 0, ErrBadRequest(CodeProjectOwnerInvalid, "负责人用户已禁用")
	}
	return targetOwnerID, nil
}

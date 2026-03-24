// project_repo.go — 项目与成员数据访问层
package repository

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"testpilot/internal/model"
)

// ProjectRepository 项目数据访问接口
type ProjectRepository interface {
	// FindByID 根据 ID 查找项目
	FindByID(ctx context.Context, id uint) (*model.Project, error)
	// Exists 检查项目是否存在
	Exists(ctx context.Context, id uint) (bool, error)
	// List 获取全部项目列表（含成员数、用例数）
	List(ctx context.Context) ([]model.Project, error)
	// ListByUserID 获取用户参与的项目列表（排除已归档项目）
	ListByUserID(ctx context.Context, userID uint) ([]model.Project, error)
	// ListByUserIDIncludeArchived 获取用户参与的项目列表（包含归档项目）
	ListByUserIDIncludeArchived(ctx context.Context, userID uint) ([]model.Project, error)
	// Create 创建项目
	Create(ctx context.Context, project *model.Project) error
	// Updates 更新项目字段
	Updates(ctx context.Context, id uint, fields map[string]any) error
	// ExistAll 检查项目 ID 列表是否全部存在
	ExistAll(ctx context.Context, ids []uint) (bool, error)
	// ExistsByName 检查项目名是否已存在（排除指定 ID）
	ExistsByName(ctx context.Context, name string, excludeID uint) (bool, error)
	// Delete 物理删除项目
	Delete(ctx context.Context, id uint) error
	// CountTestCases 统计项目下的用例数
	CountTestCases(ctx context.Context, projectID uint) (int64, error)
	// CountDefects 统计项目下的缺陷数
	CountDefects(ctx context.Context, projectID uint) (int64, error)
	// CountMembers 统计项目的成员数
	CountMembers(ctx context.Context, projectID uint) (int64, error)

	// ---- 成员管理 ----

	// IsMember 判断用户是否是项目成员
	IsMember(ctx context.Context, projectID, userID uint) (bool, error)
	// AddMember 添加或更新项目成员
	AddMember(ctx context.Context, member *model.ProjectMember) error
	// RemoveMember 移除项目成员
	RemoveMember(ctx context.Context, projectID, userID uint) error
	// ListMembers 获取项目成员列表
	ListMembers(ctx context.Context, projectID uint) ([]model.ProjectMember, error)
	// DeleteAllMembers 删除项目所有成员记录
	DeleteAllMembers(ctx context.Context, projectID uint) error
}

// projectRepo ProjectRepository 的 GORM 实现
type projectRepo struct {
	db *gorm.DB
}

// NewProjectRepo 创建项目仓库
func NewProjectRepo(db *gorm.DB) ProjectRepository {
	return &projectRepo{db: db}
}

// FindByID 根据 ID 查找项目
func (r *projectRepo) FindByID(ctx context.Context, id uint) (*model.Project, error) {
	var project model.Project
	if err := r.db.WithContext(ctx).First(&project, id).Error; err != nil {
		return nil, err
	}
	return &project, nil
}

// Exists 检查项目是否存在
func (r *projectRepo) Exists(ctx context.Context, id uint) (bool, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&model.Project{}).Where("id = ?", id).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// projectRow 局部扫描结构体（无 gorm:"-" 标签限制），用于 Raw().Scan() 接收虚拟字段
type projectRow struct {
	ID            uint       `gorm:"column:id"`
	Name          string     `gorm:"column:name"`
	Description   string     `gorm:"column:description"`
	Avatar        string     `gorm:"column:avatar"`
	Status        string     `gorm:"column:status"`
	ArchivedAt    *time.Time `gorm:"column:archived_at"`
	CreatedAt     time.Time  `gorm:"column:created_at"`
	UpdatedAt     time.Time  `gorm:"column:updated_at"`
	MemberCount   int64      `gorm:"column:member_count"`
	TestCaseCount int64      `gorm:"column:testcase_count"`
}

// toModel 将扫描行转换为 model.Project
func (pr projectRow) toModel() model.Project {
	return model.Project{
		ID:            pr.ID,
		Name:          pr.Name,
		Description:   pr.Description,
		Avatar:        pr.Avatar,
		Status:        pr.Status,
		ArchivedAt:    pr.ArchivedAt,
		CreatedAt:     pr.CreatedAt,
		UpdatedAt:     pr.UpdatedAt,
		MemberCount:   pr.MemberCount,
		TestCaseCount: pr.TestCaseCount,
	}
}

// List 获取全部项目列表，按活跃排前、归档排后，包含成员数和用例数统计
func (r *projectRepo) List(ctx context.Context) ([]model.Project, error) {
	var rows []projectRow
	err := r.db.WithContext(ctx).Raw(`
		SELECT projects.id, projects.name, projects.description, projects.avatar, projects.status,
			projects.archived_at, projects.created_at, projects.updated_at,
			(SELECT COUNT(*) FROM project_members WHERE project_members.project_id = projects.id) AS member_count,
			(SELECT COUNT(*) FROM test_cases WHERE test_cases.project_id = projects.id) AS testcase_count
		FROM projects
		ORDER BY CASE WHEN projects.status = 'archived' THEN 1 ELSE 0 END ASC, projects.updated_at DESC
	`).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	projects := make([]model.Project, 0, len(rows))
	for _, row := range rows {
		projects = append(projects, row.toModel())
	}
	return projects, nil
}

// ListByUserID 获取用户参与的项目列表（排除已归档项目），含统计数据
func (r *projectRepo) ListByUserID(ctx context.Context, userID uint) ([]model.Project, error) {
	var rows []projectRow
	err := r.db.WithContext(ctx).Raw(`
		SELECT projects.id, projects.name, projects.description, projects.avatar, projects.status,
			projects.archived_at, projects.created_at, projects.updated_at,
			(SELECT COUNT(*) FROM project_members WHERE project_members.project_id = projects.id) AS member_count,
			(SELECT COUNT(*) FROM test_cases WHERE test_cases.project_id = projects.id) AS testcase_count
		FROM projects
		JOIN project_members pm ON pm.project_id = projects.id
		WHERE pm.user_id = ? AND projects.status = ?
		ORDER BY projects.updated_at DESC
	`, userID, model.ProjectStatusActive).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	projects := make([]model.Project, 0, len(rows))
	for _, row := range rows {
		projects = append(projects, row.toModel())
	}
	return projects, nil
}

// ListByUserIDIncludeArchived 获取用户参与的项目列表（包含归档），含统计数据
func (r *projectRepo) ListByUserIDIncludeArchived(ctx context.Context, userID uint) ([]model.Project, error) {
	var rows []projectRow
	err := r.db.WithContext(ctx).Raw(`
		SELECT projects.id, projects.name, projects.description, projects.avatar, projects.status,
			projects.archived_at, projects.created_at, projects.updated_at,
			(SELECT COUNT(*) FROM project_members WHERE project_members.project_id = projects.id) AS member_count,
			(SELECT COUNT(*) FROM test_cases WHERE test_cases.project_id = projects.id) AS testcase_count
		FROM projects
		JOIN project_members pm ON pm.project_id = projects.id
		WHERE pm.user_id = ?
		ORDER BY CASE WHEN projects.status = 'archived' THEN 1 ELSE 0 END ASC, projects.updated_at DESC
	`, userID).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	projects := make([]model.Project, 0, len(rows))
	for _, row := range rows {
		projects = append(projects, row.toModel())
	}
	return projects, nil
}

// Create 创建项目
func (r *projectRepo) Create(ctx context.Context, project *model.Project) error {
	return r.db.WithContext(ctx).Create(project).Error
}

// Updates 更新项目字段
func (r *projectRepo) Updates(ctx context.Context, id uint, fields map[string]any) error {
	return r.db.WithContext(ctx).Model(&model.Project{}).Where("id = ?", id).Updates(fields).Error
}

// ExistAll 检查项目 ID 列表是否全部存在
func (r *projectRepo) ExistAll(ctx context.Context, ids []uint) (bool, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&model.Project{}).Where("id IN ?", ids).Count(&count).Error; err != nil {
		return false, err
	}
	return int(count) == len(ids), nil
}

// ExistsByName 检查项目名是否已存在（排除指定 ID），用于唯一性校验
func (r *projectRepo) ExistsByName(ctx context.Context, name string, excludeID uint) (bool, error) {
	query := r.db.WithContext(ctx).Model(&model.Project{}).Where("name = ?", name)
	if excludeID > 0 {
		query = query.Where("id <> ?", excludeID)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// Delete 物理删除项目
func (r *projectRepo) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&model.Project{}, id).Error
}

// CountTestCases 统计项目下的用例数（排除已软删除的）
func (r *projectRepo) CountTestCases(ctx context.Context, projectID uint) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.TestCase{}).
		Where("project_id = ?", projectID).Count(&count).Error
	return count, err
}

// CountDefects 统计项目下的缺陷数
func (r *projectRepo) CountDefects(ctx context.Context, projectID uint) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.Defect{}).
		Where("project_id = ?", projectID).Count(&count).Error
	return count, err
}

// CountMembers 统计项目的成员数
func (r *projectRepo) CountMembers(ctx context.Context, projectID uint) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.ProjectMember{}).
		Where("project_id = ?", projectID).Count(&count).Error
	return count, err
}

// IsMember 判断用户是否是项目成员
func (r *projectRepo) IsMember(ctx context.Context, projectID, userID uint) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.ProjectMember{}).
		Where("project_id = ? AND user_id = ?", projectID, userID).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// AddMember 添加或更新项目成员（upsert on project_id+user_id）
func (r *projectRepo) AddMember(ctx context.Context, member *model.ProjectMember) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "project_id"}, {Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"role", "updated_at"}),
	}).Create(member).Error
}

// RemoveMember 移除项目成员
func (r *projectRepo) RemoveMember(ctx context.Context, projectID, userID uint) error {
	return r.db.WithContext(ctx).
		Where("project_id = ? AND user_id = ?", projectID, userID).
		Delete(&model.ProjectMember{}).Error
}

// ListMembers 获取项目成员列表（预加载 User 信息）
func (r *projectRepo) ListMembers(ctx context.Context, projectID uint) ([]model.ProjectMember, error) {
	var members []model.ProjectMember
	err := r.db.WithContext(ctx).Preload("User").
		Where("project_id = ?", projectID).
		Order("created_at asc").
		Find(&members).Error
	if err != nil {
		return nil, err
	}
	// 补齐成员用户的多角色显示名，供前端直接展示标签。
	if err := r.fillMemberRoleNames(ctx, members); err != nil {
		return nil, err
	}
	return members, nil
}

// DeleteAllMembers 删除项目所有成员记录（用于删除空项目时级联清理）
func (r *projectRepo) DeleteAllMembers(ctx context.Context, projectID uint) error {
	return r.db.WithContext(ctx).Where("project_id = ?", projectID).Delete(&model.ProjectMember{}).Error
}

// ---- 工具函数 ----

// archiveFields 返回归档操作的更新字段
func ArchiveFields() map[string]any {
	now := time.Now()
	return map[string]any{
		"status":      model.ProjectStatusArchived,
		"archived_at": &now,
	}
}

// restoreFields 返回恢复操作的更新字段
func RestoreFields() map[string]any {
	return map[string]any{
		"status":      model.ProjectStatusActive,
		"archived_at": nil,
	}
}

// memberRoleNameRow 承接成员角色查询结果。
type memberRoleNameRow struct {
	UserID   uint   `gorm:"column:user_id"`
	RoleName string `gorm:"column:role_name"`
}

// fillMemberRoleNames 批量补齐成员用户的角色显示名，避免前端只能看到缓存主角色。
func (r *projectRepo) fillMemberRoleNames(ctx context.Context, members []model.ProjectMember) error {
	if len(members) == 0 {
		return nil
	}
	userIDs := make([]uint, 0, len(members))
	seen := make(map[uint]struct{}, len(members))
	for _, member := range members {
		if _, ok := seen[member.UserID]; ok {
			continue
		}
		seen[member.UserID] = struct{}{}
		userIDs = append(userIDs, member.UserID)
	}

	var rows []memberRoleNameRow
	err := r.db.WithContext(ctx).
		Table("user_roles").
		Select("user_roles.user_id, COALESCE(NULLIF(roles.display_name, ''), roles.name) AS role_name").
		Joins("JOIN roles ON roles.id = user_roles.role_id").
		Where("user_roles.user_id IN ?", userIDs).
		Order("user_roles.user_id ASC, user_roles.role_id ASC").
		Scan(&rows).Error
	if err != nil {
		return err
	}

	roleNameMap := make(map[uint][]string, len(userIDs))
	for _, row := range rows {
		roleName := strings.TrimSpace(row.RoleName)
		if roleName == "" {
			continue
		}
		roleNameMap[row.UserID] = append(roleNameMap[row.UserID], roleName)
	}

	for i := range members {
		roleNames := roleNameMap[members[i].UserID]
		if len(roleNames) == 0 {
			fallbackRole := strings.TrimSpace(members[i].User.Role)
			if fallbackRole != "" {
				roleNames = []string{fallbackRole}
			} else {
				roleNames = []string{}
			}
		}
		members[i].User.RoleNames = roleNames
	}
	return nil
}

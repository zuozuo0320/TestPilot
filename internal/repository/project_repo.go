// project_repo.go — 项目与成员数据访问层
package repository

import (
	"context"

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
	// List 获取全部项目列表
	List(ctx context.Context) ([]model.Project, error)
	// ListByUserID 获取用户参与的项目列表
	ListByUserID(ctx context.Context, userID uint) ([]model.Project, error)
	// Create 创建项目
	Create(ctx context.Context, project *model.Project) error
	// ExistAll 检查项目 ID 列表是否全部存在
	ExistAll(ctx context.Context, ids []uint) (bool, error)

	// ---- 成员管理 ----

	// IsMember 判断用户是否是项目成员
	IsMember(ctx context.Context, projectID, userID uint) (bool, error)
	// AddMember 添加或更新项目成员
	AddMember(ctx context.Context, member *model.ProjectMember) error
	// ListMembers 获取项目成员列表
	ListMembers(ctx context.Context, projectID uint) ([]model.ProjectMember, error)
}

// projectRepo ProjectRepository 的 GORM 实现
type projectRepo struct {
	db *gorm.DB
}

// NewProjectRepo 创建项目仓库
func NewProjectRepo(db *gorm.DB) ProjectRepository {
	return &projectRepo{db: db}
}

func (r *projectRepo) FindByID(ctx context.Context, id uint) (*model.Project, error) {
	var project model.Project
	if err := r.db.WithContext(ctx).First(&project, id).Error; err != nil {
		return nil, err
	}
	return &project, nil
}

func (r *projectRepo) Exists(ctx context.Context, id uint) (bool, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&model.Project{}).Where("id = ?", id).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *projectRepo) List(ctx context.Context) ([]model.Project, error) {
	var projects []model.Project
	if err := r.db.WithContext(ctx).Order("projects.id asc").Find(&projects).Error; err != nil {
		return nil, err
	}
	return projects, nil
}

func (r *projectRepo) ListByUserID(ctx context.Context, userID uint) ([]model.Project, error) {
	var projects []model.Project
	err := r.db.WithContext(ctx).
		Joins("JOIN project_members pm ON pm.project_id = projects.id").
		Where("pm.user_id = ?", userID).
		Order("projects.id asc").
		Find(&projects).Error
	if err != nil {
		return nil, err
	}
	return projects, nil
}

func (r *projectRepo) Create(ctx context.Context, project *model.Project) error {
	return r.db.WithContext(ctx).Create(project).Error
}

func (r *projectRepo) ExistAll(ctx context.Context, ids []uint) (bool, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&model.Project{}).Where("id IN ?", ids).Count(&count).Error; err != nil {
		return false, err
	}
	return int(count) == len(ids), nil
}

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

func (r *projectRepo) AddMember(ctx context.Context, member *model.ProjectMember) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "project_id"}, {Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"role", "updated_at"}),
	}).Create(member).Error
}

func (r *projectRepo) ListMembers(ctx context.Context, projectID uint) ([]model.ProjectMember, error) {
	var members []model.ProjectMember
	err := r.db.WithContext(ctx).Preload("User").
		Where("project_id = ?", projectID).
		Order("id asc").
		Find(&members).Error
	return members, err
}

// script_repo.go — 脚本数据访问层
package repository

import (
	"context"

	"gorm.io/gorm"

	"testpilot/internal/model"
)

// ScriptRepository 脚本数据访问接口
type ScriptRepository interface {
	// Create 创建脚本
	Create(ctx context.Context, script *model.Script) error
	// List 获取项目脚本列表
	List(ctx context.Context, projectID uint) ([]model.Script, error)
	// BelongsToProject 检查脚本是否属于指定项目
	BelongsToProject(ctx context.Context, id, projectID uint) (bool, error)
	// Count 统计项目脚本数量
	Count(ctx context.Context, projectID uint) (int64, error)
	// LinkTestCase 关联脚本与用例
	LinkTestCase(ctx context.Context, testCaseID, scriptID uint) error
}

// scriptRepo ScriptRepository 的 GORM 实现
type scriptRepo struct {
	db *gorm.DB
}

// NewScriptRepo 创建脚本仓库
func NewScriptRepo(db *gorm.DB) ScriptRepository {
	return &scriptRepo{db: db}
}

func (r *scriptRepo) Create(ctx context.Context, script *model.Script) error {
	return r.db.WithContext(ctx).Create(script).Error
}

func (r *scriptRepo) List(ctx context.Context, projectID uint) ([]model.Script, error) {
	var scripts []model.Script
	err := r.db.WithContext(ctx).Where("project_id = ?", projectID).Order("id asc").Find(&scripts).Error
	return scripts, err
}

func (r *scriptRepo) BelongsToProject(ctx context.Context, id, projectID uint) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.Script{}).Where("id = ? AND project_id = ?", id, projectID).Count(&count).Error
	return count > 0, err
}

func (r *scriptRepo) Count(ctx context.Context, projectID uint) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.Script{}).Where("project_id = ?", projectID).Count(&count).Error
	return count, err
}

func (r *scriptRepo) LinkTestCase(ctx context.Context, testCaseID, scriptID uint) error {
	link := model.TestCaseScript{TestCaseID: testCaseID, ScriptID: scriptID}
	return r.db.WithContext(ctx).Where(&link).FirstOrCreate(&link).Error
}

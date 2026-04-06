// execution_repo.go — 执行与结果数据访问层
package repository

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"testpilot/internal/model"
)

// ExecutionRepository 执行数据访问接口
type ExecutionRepository interface {
	// CreateRunTx 在事务中创建执行记录
	CreateRunTx(tx *gorm.DB, run *model.Run) error
	// SaveRunTx 在事务中保存执行记录（更新状态）
	SaveRunTx(tx *gorm.DB, run *model.Run) error
	// CreateResultTx 在事务中创建执行结果
	CreateResultTx(tx *gorm.DB, result *model.RunResult) error
	// FindRun 查找执行记录
	FindRun(ctx context.Context, runID, projectID uint) (*model.Run, error)
	// ListResults 获取执行结果列表
	ListResults(ctx context.Context, runID, projectID uint) ([]model.RunResult, error)
	// FindResultByID 查找执行结果
	FindResultByID(ctx context.Context, resultID, projectID uint) (*model.RunResult, error)
	// LatestRun 获取项目最新执行记录
	LatestRun(ctx context.Context, projectID uint) (*model.Run, error)
	// CountResultsByRun 统计执行总数和通过数
	CountResultsByRun(ctx context.Context, runID uint) (total int64, passed int64, err error)
	// ResolveScripts 根据模式解析脚本列表
	ResolveScripts(ctx context.Context, projectID uint, mode string, scriptID uint, scriptIDs []uint) ([]model.Script, error)
	// CountRuns 统计项目执行次数
	CountRuns(ctx context.Context, projectID uint) (int64, error)
	// ListLinkedTestCaseIDsByScriptIDs 获取脚本关联的用例 ID 列表
	ListLinkedTestCaseIDsByScriptIDs(ctx context.Context, projectID uint, scriptIDs []uint) (map[uint][]uint, error)
	// UpdateTestCaseExecResultsTx 在事务中批量回写用例执行结果
	UpdateTestCaseExecResultsTx(tx *gorm.DB, projectID uint, ids []uint, execResult string) error
}

// executionRepo ExecutionRepository 的 GORM 实现
type executionRepo struct {
	db *gorm.DB
}

// NewExecutionRepo 创建执行仓库
func NewExecutionRepo(db *gorm.DB) ExecutionRepository {
	return &executionRepo{db: db}
}

func (r *executionRepo) CreateRunTx(tx *gorm.DB, run *model.Run) error {
	return tx.Create(run).Error
}

func (r *executionRepo) SaveRunTx(tx *gorm.DB, run *model.Run) error {
	return tx.Save(run).Error
}

func (r *executionRepo) CreateResultTx(tx *gorm.DB, result *model.RunResult) error {
	return tx.Create(result).Error
}

func (r *executionRepo) FindRun(ctx context.Context, runID, projectID uint) (*model.Run, error) {
	var run model.Run
	if err := r.db.WithContext(ctx).Where("id = ? AND project_id = ?", runID, projectID).First(&run).Error; err != nil {
		return nil, err
	}
	return &run, nil
}

func (r *executionRepo) ListResults(ctx context.Context, runID, projectID uint) ([]model.RunResult, error) {
	var results []model.RunResult
	err := r.db.WithContext(ctx).Where("run_id = ? AND project_id = ?", runID, projectID).Order("id asc").Find(&results).Error
	return results, err
}

func (r *executionRepo) FindResultByID(ctx context.Context, resultID, projectID uint) (*model.RunResult, error) {
	var result model.RunResult
	if err := r.db.WithContext(ctx).Where("id = ? AND project_id = ?", resultID, projectID).First(&result).Error; err != nil {
		return nil, err
	}
	return &result, nil
}

func (r *executionRepo) LatestRun(ctx context.Context, projectID uint) (*model.Run, error) {
	var run model.Run
	if err := r.db.WithContext(ctx).Where("project_id = ?", projectID).Order("id desc").First(&run).Error; err != nil {
		return nil, err
	}
	return &run, nil
}

func (r *executionRepo) CountResultsByRun(ctx context.Context, runID uint) (int64, int64, error) {
	var total int64
	if err := r.db.WithContext(ctx).Model(&model.RunResult{}).Where("run_id = ?", runID).Count(&total).Error; err != nil {
		return 0, 0, err
	}
	var passed int64
	if err := r.db.WithContext(ctx).Model(&model.RunResult{}).Where("run_id = ? AND status = ?", runID, "passed").Count(&passed).Error; err != nil {
		return 0, 0, err
	}
	return total, passed, nil
}

func (r *executionRepo) CountRuns(ctx context.Context, projectID uint) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.Run{}).Where("project_id = ?", projectID).Count(&count).Error
	return count, err
}

func (r *executionRepo) ListLinkedTestCaseIDsByScriptIDs(ctx context.Context, projectID uint, scriptIDs []uint) (map[uint][]uint, error) {
	result := make(map[uint][]uint)
	if len(scriptIDs) == 0 {
		return result, nil
	}
	type row struct {
		ScriptID   uint `gorm:"column:script_id"`
		TestCaseID uint `gorm:"column:test_case_id"`
	}
	var rows []row
	err := r.db.WithContext(ctx).
		Table("test_case_scripts AS tcs").
		Select("tcs.script_id, tcs.test_case_id").
		Joins("JOIN test_cases tc ON tc.id = tcs.test_case_id").
		Where("tc.project_id = ? AND tcs.script_id IN ?", projectID, scriptIDs).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, item := range rows {
		result[item.ScriptID] = append(result[item.ScriptID], item.TestCaseID)
	}
	return result, nil
}

func (r *executionRepo) UpdateTestCaseExecResultsTx(tx *gorm.DB, projectID uint, ids []uint, execResult string) error {
	if len(ids) == 0 {
		return nil
	}
	return tx.Model(&model.TestCase{}).
		Where("project_id = ? AND id IN ?", projectID, ids).
		Update("exec_result", execResult).Error
}

func (r *executionRepo) ResolveScripts(ctx context.Context, projectID uint, mode string, scriptID uint, scriptIDs []uint) ([]model.Script, error) {
	var scripts []model.Script
	switch mode {
	case "all":
		if err := r.db.WithContext(ctx).Where("project_id = ?", projectID).Order("id asc").Find(&scripts).Error; err != nil {
			return nil, err
		}
		if len(scripts) == 0 {
			return nil, fmt.Errorf("no scripts in project")
		}
		return scripts, nil
	case "one":
		if scriptID == 0 {
			return nil, fmt.Errorf("script_id is required when mode=one")
		}
		var script model.Script
		if err := r.db.WithContext(ctx).Where("id = ? AND project_id = ?", scriptID, projectID).First(&script).Error; err != nil {
			return nil, fmt.Errorf("script not found in project")
		}
		return []model.Script{script}, nil
	case "batch":
		if len(scriptIDs) == 0 {
			return nil, fmt.Errorf("script_ids is required when mode=batch")
		}
		if err := r.db.WithContext(ctx).Where("project_id = ? AND id IN ?", projectID, scriptIDs).Order("id asc").Find(&scripts).Error; err != nil {
			return nil, err
		}
		if len(scripts) != len(scriptIDs) {
			return nil, fmt.Errorf("some script_ids are missing or outside project")
		}
		return scripts, nil
	default:
		return nil, fmt.Errorf("mode should be all/one/batch")
	}
}

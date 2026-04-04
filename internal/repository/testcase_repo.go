// testcase_repo.go — 测试用例数据访问层
package repository

import (
	"context"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"testpilot/internal/model"
)

// TestCaseFilter 用例列表筛选参数
type TestCaseFilter struct {
	Keyword      string // 关键字（标题/标签/ID）
	Level        string // 级别筛选
	ReviewResult string // 评审结果筛选
	ExecResult   string // 执行结果筛选
	Tags         string // 标签筛选
	ModuleID     *uint  // 目录模块筛选
	ModulePath   string // 目录路径筛选（支持子目录前缀匹配）
	CreatedByID  *uint  // 创建人筛选
	UpdatedByID  *uint  // 更新人筛选
	CreatedAfter  string // 创建时间起始
	CreatedBefore string // 创建时间截止
	UpdatedAfter  string // 更新时间起始
	UpdatedBefore string // 更新时间截止
	SortBy       string // 排序字段
	SortOrder    string // 排序方向 asc/desc
	Page         int    // 页码
	PageSize     int    // 每页条数
}

// TestCaseListItem 用例列表项（含创建/更新人姓名）
type TestCaseListItem struct {
	ID            uint   `json:"id"`
	ProjectID     uint   `json:"project_id"`
	Title         string `json:"title"`
	Level         string `json:"level"`
	ReviewResult  string `json:"review_result"`
	ExecResult    string `json:"exec_result"`
	ModuleID      uint   `json:"module_id"`
	ModulePath    string `json:"module_path"`
	Tags          string `json:"tags"`
	Precondition  string `json:"precondition"`
	Steps         string `json:"steps"`
	Remark        string `json:"remark"`
	Priority      string `json:"priority"`
	CreatedBy     uint   `json:"created_by"`
	CreatedByName string `json:"created_by_name"`
	UpdatedBy     uint   `json:"updated_by"`
	UpdatedByName string `json:"updated_by_name"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

// TestCaseRepository 用例数据访问接口
type TestCaseRepository interface {
	FindByID(ctx context.Context, id, projectID uint) (*model.TestCase, error)
	ListPaged(ctx context.Context, projectID uint, filter TestCaseFilter) ([]TestCaseListItem, int64, error)
	Create(ctx context.Context, tc *model.TestCase) error
	Updates(ctx context.Context, tc *model.TestCase, fields map[string]any) error
	Delete(ctx context.Context, id, projectID uint) (int64, error)
	BelongsToProject(ctx context.Context, id, projectID uint) (bool, error)
	CountByProject(ctx context.Context, projectID uint) (int64, error)
	CountByExecResult(ctx context.Context, projectID uint) (map[string]int64, error)
	BatchDelete(ctx context.Context, projectID uint, ids []uint) (int64, error)
	BatchUpdateLevel(ctx context.Context, projectID uint, ids []uint, level string) (int64, error)
	BatchMove(ctx context.Context, projectID uint, ids []uint, moduleID uint, modulePath string) (int64, error)
	UpdateModulePathsByPrefix(ctx context.Context, projectID uint, oldPrefix, newPrefix string) error
	CloneCase(ctx context.Context, projectID, sourceID, userID uint) (*model.TestCase, error)
	CountByModule(ctx context.Context, moduleID uint) (int64, error)
	DB(ctx context.Context) *gorm.DB
}

// testCaseRepo TestCaseRepository 的 GORM 实现
type testCaseRepo struct {
	db *gorm.DB
}

// NewTestCaseRepo 创建用例仓库
func NewTestCaseRepo(db *gorm.DB) TestCaseRepository {
	return &testCaseRepo{db: db}
}

func (r *testCaseRepo) FindByID(ctx context.Context, id, projectID uint) (*model.TestCase, error) {
	var tc model.TestCase
	if err := r.db.WithContext(ctx).Where("id = ? AND project_id = ?", id, projectID).First(&tc).Error; err != nil {
		return nil, err
	}
	return &tc, nil
}

func (r *testCaseRepo) ListPaged(ctx context.Context, projectID uint, f TestCaseFilter) ([]TestCaseListItem, int64, error) {
	baseQuery := r.db.WithContext(ctx).Model(&model.TestCase{}).Where("project_id = ?", projectID)

	// 关键字搜索
	if f.Keyword != "" {
		like := "%" + f.Keyword + "%"
		if idKey, err := strconv.Atoi(f.Keyword); err == nil && idKey > 0 {
			baseQuery = baseQuery.Where("id = ? OR title LIKE ? OR tags LIKE ?", idKey, like, like)
		} else {
			baseQuery = baseQuery.Where("title LIKE ? OR tags LIKE ?", like, like)
		}
	}
	if f.Level != "" {
		baseQuery = baseQuery.Where("level = ?", f.Level)
	}
	if f.ReviewResult != "" {
		baseQuery = baseQuery.Where("review_result = ?", f.ReviewResult)
	}
	if f.ExecResult != "" {
		baseQuery = baseQuery.Where("exec_result = ?", f.ExecResult)
	}
	if f.Tags != "" {
		baseQuery = baseQuery.Where("tags LIKE ?", "%"+f.Tags+"%")
	}
	if f.ModuleID != nil {
		baseQuery = baseQuery.Where("module_id = ?", *f.ModuleID)
	}
	if f.ModulePath != "" {
		// 未规划用例精确匹配；其他目录支持子目录前缀匹配
		if f.ModulePath == "/未规划用例" {
			baseQuery = baseQuery.Where("(module_path = ? OR module_path = '' OR module_path = '/' OR module_path IS NULL)", f.ModulePath)
		} else {
			baseQuery = baseQuery.Where("(module_path = ? OR module_path LIKE ?)", f.ModulePath, f.ModulePath+"/%")
		}
	}
	if f.CreatedByID != nil {
		baseQuery = baseQuery.Where("created_by = ?", *f.CreatedByID)
	}
	if f.UpdatedByID != nil {
		baseQuery = baseQuery.Where("updated_by = ?", *f.UpdatedByID)
	}
	if f.CreatedAfter != "" {
		baseQuery = baseQuery.Where("test_cases.created_at >= ?", f.CreatedAfter)
	}
	if f.CreatedBefore != "" {
		baseQuery = baseQuery.Where("test_cases.created_at <= ?", f.CreatedBefore)
	}
	if f.UpdatedAfter != "" {
		baseQuery = baseQuery.Where("test_cases.updated_at >= ?", f.UpdatedAfter)
	}
	if f.UpdatedBefore != "" {
		baseQuery = baseQuery.Where("test_cases.updated_at <= ?", f.UpdatedBefore)
	}

	// 总数
	var total int64
	if err := baseQuery.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 排序
	orderColumn := "test_cases.updated_at"
	switch f.SortBy {
	case "id":
		orderColumn = "test_cases.id"
	case "created_at":
		orderColumn = "test_cases.created_at"
	}
	sortOrder := "desc"
	if f.SortOrder == "asc" {
		sortOrder = "asc"
	}

	// 查询
	var items []TestCaseListItem
	offset := (f.Page - 1) * f.PageSize
	err := baseQuery.
		Select("test_cases.id, test_cases.project_id, test_cases.title, test_cases.level, test_cases.review_result, test_cases.exec_result, test_cases.module_id, test_cases.module_path, test_cases.tags, test_cases.precondition, test_cases.steps, test_cases.remark, test_cases.priority, test_cases.created_by, test_cases.updated_by, test_cases.created_at, test_cases.updated_at, cu.name AS created_by_name, uu.name AS updated_by_name").
		Joins("LEFT JOIN users cu ON cu.id = test_cases.created_by").
		Joins("LEFT JOIN users uu ON uu.id = test_cases.updated_by").
		Order(orderColumn + " " + sortOrder).
		Offset(offset).
		Limit(f.PageSize).
		Scan(&items).Error
	if err != nil {
		return nil, 0, err
	}

	// 补全空姓名
	for i := range items {
		if strings.TrimSpace(items[i].CreatedByName) == "" {
			items[i].CreatedByName = "-"
		}
		if strings.TrimSpace(items[i].UpdatedByName) == "" {
			items[i].UpdatedByName = "-"
		}
	}

	return items, total, nil
}

func (r *testCaseRepo) Create(ctx context.Context, tc *model.TestCase) error {
	return r.db.WithContext(ctx).Create(tc).Error
}

func (r *testCaseRepo) Updates(ctx context.Context, tc *model.TestCase, fields map[string]any) error {
	return r.db.WithContext(ctx).Model(tc).Updates(fields).Error
}

func (r *testCaseRepo) Delete(ctx context.Context, id, projectID uint) (int64, error) {
	result := r.db.WithContext(ctx).Where("id = ? AND project_id = ?", id, projectID).Delete(&model.TestCase{})
	return result.RowsAffected, result.Error
}

func (r *testCaseRepo) BelongsToProject(ctx context.Context, id, projectID uint) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.TestCase{}).Where("id = ? AND project_id = ?", id, projectID).Count(&count).Error
	return count > 0, err
}

func (r *testCaseRepo) CountByProject(ctx context.Context, projectID uint) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.TestCase{}).Where("project_id = ?", projectID).Count(&count).Error
	return count, err
}

// CountByExecResult 按执行结果分组统计用例数
func (r *testCaseRepo) CountByExecResult(ctx context.Context, projectID uint) (map[string]int64, error) {
	type row struct {
		ExecResult string
		Cnt        int64
	}
	var rows []row
	err := r.db.WithContext(ctx).
		Model(&model.TestCase{}).
		Select("exec_result, COUNT(*) as cnt").
		Where("project_id = ?", projectID).
		Group("exec_result").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make(map[string]int64)
	for _, r := range rows {
		result[r.ExecResult] = r.Cnt
	}
	return result, nil
}

func (r *testCaseRepo) BatchDelete(ctx context.Context, projectID uint, ids []uint) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	result := r.db.WithContext(ctx).Where("project_id = ? AND id IN ?", projectID, ids).Delete(&model.TestCase{})
	return result.RowsAffected, result.Error
}

func (r *testCaseRepo) BatchUpdateLevel(ctx context.Context, projectID uint, ids []uint, level string) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	result := r.db.WithContext(ctx).Model(&model.TestCase{}).
		Where("project_id = ? AND id IN ?", projectID, ids).
		Update("level", level)
	return result.RowsAffected, result.Error
}

func (r *testCaseRepo) BatchMove(ctx context.Context, projectID uint, ids []uint, moduleID uint, modulePath string) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	result := r.db.WithContext(ctx).Model(&model.TestCase{}).
		Where("project_id = ? AND id IN ?", projectID, ids).
		Updates(map[string]any{"module_id": moduleID, "module_path": modulePath})
	return result.RowsAffected, result.Error
}

func (r *testCaseRepo) CloneCase(ctx context.Context, projectID, sourceID, userID uint) (*model.TestCase, error) {
	source, err := r.FindByID(ctx, sourceID, projectID)
	if err != nil {
		return nil, err
	}
	clone := &model.TestCase{
		ProjectID:    source.ProjectID,
		Title:        source.Title + " (副本)",
		Level:        source.Level,
		ReviewResult: "未评审",
		ExecResult:   "未执行",
		ModuleID:     source.ModuleID,
		ModulePath:   source.ModulePath,
		Tags:         source.Tags,
		Precondition: source.Precondition,
		Steps:        source.Steps,
		Remark:       source.Remark,
		Priority:     source.Priority,
		CreatedBy:    userID,
		UpdatedBy:    userID,
	}
	if err := r.db.WithContext(ctx).Create(clone).Error; err != nil {
		return nil, err
	}
	return clone, nil
}

func (r *testCaseRepo) UpdateModulePathsByPrefix(ctx context.Context, projectID uint, oldPrefix, newPrefix string) error {
	// 使用 SQLite / MySQL 兼容的字符串替换函数
	// 注意：在重命名 /内容 为 /新内容 时，/内容/子项 应该变为 /新内容/子项
	// 我们使用 LIKE 和 REPLACE
	query := r.db.WithContext(ctx).Model(&model.TestCase{}).
		Where("project_id = ? AND (module_path = ? OR module_path LIKE ?)", projectID, oldPrefix, oldPrefix+"/%")

	// GORM 自动处理不同数据库的字符串操作（在 SQLite 中也是 REPLACE）
	return query.Update("module_path", gorm.Expr("REPLACE(module_path, ?, ?)", oldPrefix, newPrefix)).Error
}

func (r *testCaseRepo) CountByModule(ctx context.Context, moduleID uint) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.TestCase{}).Where("module_id = ?", moduleID).Count(&count).Error
	return count, err
}

func (r *testCaseRepo) DB(ctx context.Context) *gorm.DB {
	return r.db.WithContext(ctx)
}

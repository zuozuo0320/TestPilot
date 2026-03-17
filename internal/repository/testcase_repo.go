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
	ModulePath    string `json:"module_path"`
	Tags          string `json:"tags"`
	Steps         string `json:"steps"`
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
	// FindByID 根据 ID + 项目 ID 查找用例
	FindByID(ctx context.Context, id, projectID uint) (*model.TestCase, error)
	// ListPaged 分页查询用例（支持筛选排序）
	ListPaged(ctx context.Context, projectID uint, filter TestCaseFilter) ([]TestCaseListItem, int64, error)
	// Create 创建用例
	Create(ctx context.Context, tc *model.TestCase) error
	// Updates 更新用例字段
	Updates(ctx context.Context, tc *model.TestCase, fields map[string]any) error
	// Delete 删除用例
	Delete(ctx context.Context, id, projectID uint) (int64, error)
	// BelongsToProject 检查用例是否属于指定项目
	BelongsToProject(ctx context.Context, id, projectID uint) (bool, error)
	// CountByProject 统计项目用例数量
	CountByProject(ctx context.Context, projectID uint) (int64, error)
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
		Select("test_cases.id, test_cases.project_id, test_cases.title, test_cases.level, test_cases.review_result, test_cases.exec_result, test_cases.module_path, test_cases.tags, test_cases.steps, test_cases.priority, test_cases.created_by, test_cases.updated_by, test_cases.created_at, test_cases.updated_at, cu.name AS created_by_name, uu.name AS updated_by_name").
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

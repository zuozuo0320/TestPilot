// tag_repo.go — 标签数据访问层
//
// 提供标签及用例-标签关联表的数据库操作，包括：
//   - 标签 CRUD（创建、查询、更新、删除）
//   - 分页列表（带用例数统计 + 创建人 JOIN 查询）
//   - 候选列表（轻量版，仅 id/name/color）
//   - 关联表操作（创建/删除/替换/复制关联关系）
//
// ❗ 在事务内调用的方法必须接受 tx *gorm.DB 参数，
// 通过 getDB(tx) 助手函数决定使用事务连接还是默认连接。
package repository

import (
	"context"

	"gorm.io/gorm"

	"testpilot/internal/model"
)

// TagFilter 标签列表筛选参数（由 Handler 层解析请求参数后构建）
type TagFilter struct {
	Keyword  string // 名称模糊搜索关键词
	Page     int    // 页码（从 1 开始）
	PageSize int    // 每页条数（默认 20，最大 100）
	SortBy   string // 排序字段: case_count | name | created_at
}

// TagBrief 轻量标签信息（用于标签选择器 / 用例列表填充，不含统计字段）
type TagBrief struct {
	ID    uint   `json:"id"`    // 标签 ID
	Name  string `json:"name"`  // 标签名称
	Color string `json:"color"` // 标签颜色
}

// TagRepository 标签数据访问层接口。
// 所有方法签名必须接受 context.Context；
// 在事务内调用的方法必须接受 tx *gorm.DB 参数。
type TagRepository interface {
	Create(ctx context.Context, tag *model.Tag) error                                      // 创建标签
	FindByID(ctx context.Context, id uint) (*model.Tag, error)                              // 按 ID 查询标签
	Update(ctx context.Context, tag *model.Tag, fields map[string]any) error                 // 更新标签指定字段
	Delete(ctx context.Context, tx *gorm.DB, id uint) error                                 // 删除标签（支持事务）
	ListPaged(ctx context.Context, projectID uint, f TagFilter) ([]model.Tag, int64, error) // 分页查询（含用例数+创建人）
	ListOptions(ctx context.Context, projectID uint, keyword string) ([]TagBrief, error)    // 候选列表（轻量）
	CountByProject(ctx context.Context, projectID uint) (int64, error)                      // 统计项目下标签数量

	// ── 关联表操作（test_case_tags） ──
	CreateRels(ctx context.Context, tx *gorm.DB, rels []model.TestCaseTag) error              // 批量创建用例-标签关联
	DeleteRelsByTagID(ctx context.Context, tx *gorm.DB, tagID uint) (int64, error)            // 按标签 ID 删除所有关联
	DeleteRelsByTestCaseID(ctx context.Context, tx *gorm.DB, testcaseID uint) error           // 按用例 ID 删除所有关联
	DeleteRelsByTestCaseIDs(ctx context.Context, tx *gorm.DB, testcaseIDs []uint) error       // 批量按用例 ID 删除关联
	ReplaceTestCaseTags(ctx context.Context, tx *gorm.DB, testcaseID uint, tagIDs []uint) error // 替换用例的全部标签（先删后建）
	CopyTestCaseTags(ctx context.Context, tx *gorm.DB, srcTestCaseID, dstTestCaseID uint) error // 复制用例标签（用于克隆用例）
	ListByTestCaseIDs(ctx context.Context, testcaseIDs []uint) (map[uint][]TagBrief, error)     // 批量查询用例关联的标签
	DeleteByProjectID(ctx context.Context, tx *gorm.DB, projectID uint) error                   // 删除项目下所有标签
	DeleteRelsByProjectID(ctx context.Context, tx *gorm.DB, projectID uint) error                // 删除项目下所有标签关联
}

// tagRepo TagRepository 的 GORM 实现
type tagRepo struct {
	db *gorm.DB // 数据库连接实例
}

// NewTagRepo 创建标签仓库实例
func NewTagRepo(db *gorm.DB) TagRepository {
	return &tagRepo{db: db}
}

// getDB 事务连接助手：如果 tx 不为 nil 则使用事务连接，否则使用默认连接。
// 这是防止 SQLite 死锁的关键设计（参见后端规范 §2.2 事务管理范式）。
func (r *tagRepo) getDB(tx *gorm.DB) *gorm.DB {
	if tx != nil {
		return tx
	}
	return r.db
}

// Create 创建标签（非事务操作，使用默认连接）
func (r *tagRepo) Create(ctx context.Context, tag *model.Tag) error {
	return r.db.WithContext(ctx).Create(tag).Error
}

// FindByID 按 ID 查询标签，不存在时返回 gorm.ErrRecordNotFound
func (r *tagRepo) FindByID(ctx context.Context, id uint) (*model.Tag, error) {
	var tag model.Tag
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&tag).Error; err != nil {
		return nil, err
	}
	return &tag, nil
}

// Update 更新指定字段（非事务操作）
func (r *tagRepo) Update(ctx context.Context, tag *model.Tag, fields map[string]any) error {
	return r.db.WithContext(ctx).Model(tag).Updates(fields).Error
}

// Delete 删除标签（支持事务，通过 getDB(tx) 决定使用哪个连接）
func (r *tagRepo) Delete(ctx context.Context, tx *gorm.DB, id uint) error {
	return r.getDB(tx).WithContext(ctx).Where("id = ?", id).Delete(&model.Tag{}).Error
}

// ListPaged 分页查询标签列表。
// 使用两次独立查询：
//   1. Count 查询 — 获取符合条件的总记录数
//   2. Data 查询  — 获取当前页数据，JOIN users 填充创建人信息，子查询统计关联用例数
//
// 两次查询使用独立 session，避免 Count 的 Select/Order 污染数据查询。
func (r *tagRepo) ListPaged(ctx context.Context, projectID uint, f TagFilter) ([]model.Tag, int64, error) {
	db := r.db.WithContext(ctx)

	// 第一次查询：统计符合条件的总记录数（独立 session）
	countQuery := db.Model(&model.Tag{}).Where("tags.project_id = ?", projectID)
	if f.Keyword != "" {
		countQuery = countQuery.Where("tags.name LIKE ?", "%"+f.Keyword+"%")
	}
	var total int64
	if err := countQuery.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 分页参数安全校正
	page, pageSize := f.Page, f.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	// 第二次查询：获取当前页数据（独立 session）
	dataQuery := db.Model(&model.Tag{}).Where("tags.project_id = ?", projectID)
	if f.Keyword != "" {
		dataQuery = dataQuery.Where("tags.name LIKE ?", "%"+f.Keyword+"%")
	}

	var tags []model.Tag
	err := dataQuery.
		// 子查询统计关联用例数，JOIN users 填充创建人信息
		Select(`tags.*,
			COALESCE((SELECT COUNT(*) FROM test_case_tags tt JOIN test_cases tc ON tc.id = tt.test_case_id WHERE tt.tag_id = tags.id), 0) AS case_count,
			COALESCE(u.name, '') AS created_by_name,
			COALESCE(u.avatar, '') AS created_by_avatar`).
		Joins("LEFT JOIN users u ON u.id = tags.created_by").
		Order(tagSortClause(f.SortBy)).
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Scan(&tags).Error

	return tags, total, err
}

// tagSortClause 根据排序字段生成 ORDER BY 子句
func tagSortClause(sortBy string) string {
	switch sortBy {
	case "case_count":
		return "case_count DESC"
	case "name":
		return "tags.name ASC"
	default:
		return "tags.created_at DESC"
	}
}

// ListOptions 候选列表（轻量版，仅 id/name/color，不分页，按名称升序）
func (r *tagRepo) ListOptions(ctx context.Context, projectID uint, keyword string) ([]TagBrief, error) {
	q := r.db.WithContext(ctx).Model(&model.Tag{}).
		Select("id, name, color").
		Where("project_id = ?", projectID)

	if keyword != "" {
		q = q.Where("name LIKE ?", "%"+keyword+"%")
	}

	var options []TagBrief
	err := q.Order("name ASC").Find(&options).Error
	return options, err
}

// CountByProject 统计项目下的标签总数（用于限额检查）
func (r *tagRepo) CountByProject(ctx context.Context, projectID uint) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.Tag{}).Where("project_id = ?", projectID).Count(&count).Error
	return count, err
}

// ── 关联表操作（test_case_tags） ──

// CreateRels 批量创建用例-标签关联（空切片时直接返回）
func (r *tagRepo) CreateRels(ctx context.Context, tx *gorm.DB, rels []model.TestCaseTag) error {
	if len(rels) == 0 {
		return nil
	}
	return r.getDB(tx).WithContext(ctx).Create(&rels).Error
}

// DeleteRelsByTagID 删除指定标签的所有关联记录，返回受影响的行数
func (r *tagRepo) DeleteRelsByTagID(ctx context.Context, tx *gorm.DB, tagID uint) (int64, error) {
	result := r.getDB(tx).WithContext(ctx).Where("tag_id = ?", tagID).Delete(&model.TestCaseTag{})
	return result.RowsAffected, result.Error
}

// DeleteRelsByTestCaseID 删除指定用例的所有标签关联
func (r *tagRepo) DeleteRelsByTestCaseID(ctx context.Context, tx *gorm.DB, testcaseID uint) error {
	return r.getDB(tx).WithContext(ctx).Where("test_case_id = ?", testcaseID).Delete(&model.TestCaseTag{}).Error
}

// DeleteRelsByTestCaseIDs 批量删除多个用例的标签关联（用于批量删除用例场景）
func (r *tagRepo) DeleteRelsByTestCaseIDs(ctx context.Context, tx *gorm.DB, testcaseIDs []uint) error {
	if len(testcaseIDs) == 0 {
		return nil
	}
	return r.getDB(tx).WithContext(ctx).Where("test_case_id IN ?", testcaseIDs).Delete(&model.TestCaseTag{}).Error
}

// ReplaceTestCaseTags 替换用例的全部标签：先删除旧关联，再建立新关联（在事务内执行）
func (r *tagRepo) ReplaceTestCaseTags(ctx context.Context, tx *gorm.DB, testcaseID uint, tagIDs []uint) error {
	d := r.getDB(tx).WithContext(ctx)
	// 先删除该用例的所有旧标签关联
	if err := d.Where("test_case_id = ?", testcaseID).Delete(&model.TestCaseTag{}).Error; err != nil {
		return err
	}
	// 标签列表为空则仅清除关联，不建立新关联
	if len(tagIDs) == 0 {
		return nil
	}
	// 批量建立新关联
	var rels []model.TestCaseTag
	for _, tagID := range tagIDs {
		rels = append(rels, model.TestCaseTag{TestCaseID: testcaseID, TagID: tagID})
	}
	return d.Create(&rels).Error
}

// CopyTestCaseTags 复制源用例的所有标签关联到目标用例（用于用例克隆场景）
func (r *tagRepo) CopyTestCaseTags(ctx context.Context, tx *gorm.DB, srcTestCaseID, dstTestCaseID uint) error {
	d := r.getDB(tx).WithContext(ctx)
	// 查询源用例的所有标签关联
	var srcRels []model.TestCaseTag
	if err := d.Where("test_case_id = ?", srcTestCaseID).Find(&srcRels).Error; err != nil {
		return err
	}
	// 源用例没有标签则无需复制
	if len(srcRels) == 0 {
		return nil
	}
	// 以目标用例 ID 重建关联记录
	var newRels []model.TestCaseTag
	for _, rel := range srcRels {
		newRels = append(newRels, model.TestCaseTag{TestCaseID: dstTestCaseID, TagID: rel.TagID})
	}
	return d.Create(&newRels).Error
}

// ListByTestCaseIDs 批量查询多个用例关联的标签信息。
// 返回 map[testCaseID][]TagBrief，用于用例列表页一次性填充所有标签显示。
func (r *tagRepo) ListByTestCaseIDs(ctx context.Context, testcaseIDs []uint) (map[uint][]TagBrief, error) {
	result := make(map[uint][]TagBrief)
	if len(testcaseIDs) == 0 {
		return result, nil
	}

	// 中间结构体：接收 JOIN 查询结果
	type row struct {
		TestCaseID uint   `gorm:"column:test_case_id"` // 用例 ID
		TagID      uint   `gorm:"column:id"`           // 标签 ID
		Name       string `gorm:"column:name"`         // 标签名称
		Color      string `gorm:"column:color"`        // 标签颜色
	}

	var rows []row
	// JOIN tags 表获取标签信息，按名称升序
	err := r.db.WithContext(ctx).
		Table("test_case_tags tt").
		Select("tt.test_case_id, t.id, t.name, t.color").
		Joins("JOIN tags t ON t.id = tt.tag_id").
		Where("tt.test_case_id IN ?", testcaseIDs).
		Order("t.name ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	// 按用例 ID 分组
	for _, r := range rows {
		result[r.TestCaseID] = append(result[r.TestCaseID], TagBrief{
			ID:    r.TagID,
			Name:  r.Name,
			Color: r.Color,
		})
	}
	return result, nil
}

// DeleteByProjectID 删除项目下的所有标签（用于项目归档/删除场景）
func (r *tagRepo) DeleteByProjectID(ctx context.Context, tx *gorm.DB, projectID uint) error {
	return r.getDB(tx).WithContext(ctx).Where("project_id = ?", projectID).Delete(&model.Tag{}).Error
}

// DeleteRelsByProjectID 删除项目下所有标签的关联记录（通过子查询定位属于该项目的标签）
func (r *tagRepo) DeleteRelsByProjectID(ctx context.Context, tx *gorm.DB, projectID uint) error {
	return r.getDB(tx).WithContext(ctx).
		Where("tag_id IN (SELECT id FROM tags WHERE project_id = ?)", projectID).
		Delete(&model.TestCaseTag{}).Error
}

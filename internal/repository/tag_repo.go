// tag_repo.go — 标签数据访问层
package repository

import (
	"context"

	"gorm.io/gorm"

	"testpilot/internal/model"
)

// TagFilter 标签列表筛选参数
type TagFilter struct {
	Keyword  string
	Page     int
	PageSize int
}

// TagBrief 轻量标签信息（用于选择器 / 用例列表填充）
type TagBrief struct {
	ID    uint   `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

// TagRepository 标签仓库接口
type TagRepository interface {
	Create(ctx context.Context, tag *model.Tag) error
	FindByID(ctx context.Context, id uint) (*model.Tag, error)
	Update(ctx context.Context, tag *model.Tag, fields map[string]any) error
	Delete(ctx context.Context, id uint) error
	ListPaged(ctx context.Context, projectID uint, f TagFilter) ([]model.Tag, int64, error)
	ListOptions(ctx context.Context, projectID uint, keyword string) ([]TagBrief, error)
	CountByProject(ctx context.Context, projectID uint) (int64, error)

	// 关联表操作
	CreateRels(ctx context.Context, tx *gorm.DB, rels []model.TestCaseTag) error
	DeleteRelsByTagID(ctx context.Context, tx *gorm.DB, tagID uint) (int64, error)
	DeleteRelsByTestCaseID(ctx context.Context, tx *gorm.DB, testcaseID uint) error
	DeleteRelsByTestCaseIDs(ctx context.Context, tx *gorm.DB, testcaseIDs []uint) error
	ReplaceTestCaseTags(ctx context.Context, tx *gorm.DB, testcaseID uint, tagIDs []uint) error
	CopyTestCaseTags(ctx context.Context, tx *gorm.DB, srcTestCaseID, dstTestCaseID uint) error
	ListByTestCaseIDs(ctx context.Context, testcaseIDs []uint) (map[uint][]TagBrief, error)
	DeleteByProjectID(ctx context.Context, tx *gorm.DB, projectID uint) error
	DeleteRelsByProjectID(ctx context.Context, tx *gorm.DB, projectID uint) error
}

// tagRepo TagRepository 的 GORM 实现
type tagRepo struct {
	db *gorm.DB
}

func NewTagRepo(db *gorm.DB) TagRepository {
	return &tagRepo{db: db}
}

func (r *tagRepo) getDB(tx *gorm.DB) *gorm.DB {
	if tx != nil {
		return tx
	}
	return r.db
}

func (r *tagRepo) Create(ctx context.Context, tag *model.Tag) error {
	return r.db.WithContext(ctx).Create(tag).Error
}

func (r *tagRepo) FindByID(ctx context.Context, id uint) (*model.Tag, error) {
	var tag model.Tag
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&tag).Error; err != nil {
		return nil, err
	}
	return &tag, nil
}

func (r *tagRepo) Update(ctx context.Context, tag *model.Tag, fields map[string]any) error {
	return r.db.WithContext(ctx).Model(tag).Updates(fields).Error
}

func (r *tagRepo) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.Tag{}).Error
}

func (r *tagRepo) ListPaged(ctx context.Context, projectID uint, f TagFilter) ([]model.Tag, int64, error) {
	baseQuery := r.db.WithContext(ctx).Model(&model.Tag{}).Where("tags.project_id = ?", projectID)

	if f.Keyword != "" {
		baseQuery = baseQuery.Where("tags.name LIKE ?", "%"+f.Keyword+"%")
	}

	var total int64
	if err := baseQuery.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page, pageSize := f.Page, f.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	var tags []model.Tag
	err := baseQuery.
		Select(`tags.*,
			COALESCE((SELECT COUNT(*) FROM test_case_tags tt JOIN test_cases tc ON tc.id = tt.test_case_id WHERE tt.tag_id = tags.id), 0) AS case_count,
			COALESCE(u.name, '') AS created_by_name,
			COALESCE(u.avatar, '') AS created_by_avatar`).
		Joins("LEFT JOIN users u ON u.id = tags.created_by").
		Order("tags.created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Scan(&tags).Error

	return tags, total, err
}

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

func (r *tagRepo) CountByProject(ctx context.Context, projectID uint) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.Tag{}).Where("project_id = ?", projectID).Count(&count).Error
	return count, err
}

// ── 关联表操作 ──

func (r *tagRepo) CreateRels(ctx context.Context, tx *gorm.DB, rels []model.TestCaseTag) error {
	if len(rels) == 0 {
		return nil
	}
	return r.getDB(tx).WithContext(ctx).Create(&rels).Error
}

func (r *tagRepo) DeleteRelsByTagID(ctx context.Context, tx *gorm.DB, tagID uint) (int64, error) {
	result := r.getDB(tx).WithContext(ctx).Where("tag_id = ?", tagID).Delete(&model.TestCaseTag{})
	return result.RowsAffected, result.Error
}

func (r *tagRepo) DeleteRelsByTestCaseID(ctx context.Context, tx *gorm.DB, testcaseID uint) error {
	return r.getDB(tx).WithContext(ctx).Where("test_case_id = ?", testcaseID).Delete(&model.TestCaseTag{}).Error
}

func (r *tagRepo) DeleteRelsByTestCaseIDs(ctx context.Context, tx *gorm.DB, testcaseIDs []uint) error {
	if len(testcaseIDs) == 0 {
		return nil
	}
	return r.getDB(tx).WithContext(ctx).Where("test_case_id IN ?", testcaseIDs).Delete(&model.TestCaseTag{}).Error
}

func (r *tagRepo) ReplaceTestCaseTags(ctx context.Context, tx *gorm.DB, testcaseID uint, tagIDs []uint) error {
	d := r.getDB(tx).WithContext(ctx)
	// 先删除旧关联
	if err := d.Where("test_case_id = ?", testcaseID).Delete(&model.TestCaseTag{}).Error; err != nil {
		return err
	}
	// 建立新关联
	if len(tagIDs) == 0 {
		return nil
	}
	var rels []model.TestCaseTag
	for _, tagID := range tagIDs {
		rels = append(rels, model.TestCaseTag{TestCaseID: testcaseID, TagID: tagID})
	}
	return d.Create(&rels).Error
}

func (r *tagRepo) CopyTestCaseTags(ctx context.Context, tx *gorm.DB, srcTestCaseID, dstTestCaseID uint) error {
	d := r.getDB(tx).WithContext(ctx)
	var srcRels []model.TestCaseTag
	if err := d.Where("test_case_id = ?", srcTestCaseID).Find(&srcRels).Error; err != nil {
		return err
	}
	if len(srcRels) == 0 {
		return nil
	}
	var newRels []model.TestCaseTag
	for _, rel := range srcRels {
		newRels = append(newRels, model.TestCaseTag{TestCaseID: dstTestCaseID, TagID: rel.TagID})
	}
	return d.Create(&newRels).Error
}

func (r *tagRepo) ListByTestCaseIDs(ctx context.Context, testcaseIDs []uint) (map[uint][]TagBrief, error) {
	result := make(map[uint][]TagBrief)
	if len(testcaseIDs) == 0 {
		return result, nil
	}

	type row struct {
		TestCaseID uint   `gorm:"column:test_case_id"`
		TagID      uint   `gorm:"column:id"`
		Name       string `gorm:"column:name"`
		Color      string `gorm:"column:color"`
	}

	var rows []row
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

	for _, r := range rows {
		result[r.TestCaseID] = append(result[r.TestCaseID], TagBrief{
			ID:    r.TagID,
			Name:  r.Name,
			Color: r.Color,
		})
	}
	return result, nil
}

func (r *tagRepo) DeleteByProjectID(ctx context.Context, tx *gorm.DB, projectID uint) error {
	return r.getDB(tx).WithContext(ctx).Where("project_id = ?", projectID).Delete(&model.Tag{}).Error
}

func (r *tagRepo) DeleteRelsByProjectID(ctx context.Context, tx *gorm.DB, projectID uint) error {
	// 删除属于该项目的所有标签的关联记录
	return r.getDB(tx).WithContext(ctx).
		Where("tag_id IN (SELECT id FROM tags WHERE project_id = ?)", projectID).
		Delete(&model.TestCaseTag{}).Error
}

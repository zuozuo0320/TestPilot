// tag_service_test.go — TagService 单元测试
//
// 采用表驱动测试范式，覆盖以下场景：
//   - Create: 正常创建、名称校验（过短/为空/过长）、颜色校验、默认色、重复名、项目限额、描述持久化
//   - Update: 正常更新、不存在、跨项目、无字段、无效颜色、重复名检测
//   - Delete: 正常删除、重复删除、不存在、跨项目、级联解除关联
//   - ListPaged: 分页、关键词搜索、第二页数据、排序
//   - ListOptions: 候选列表、关键词过滤
//   - 标签关联: ReplaceTestCaseTags、CopyTestCaseTags、ListByTestCaseIDs、DeleteRelsByTestCaseID
//
// 每个测试用例使用独立的 SQLite 内存库，确保测试间无状态污染。
package service

import (
	"context"
	"testing"

	"gorm.io/gorm"

	"testpilot/internal/model"
	"testpilot/internal/repository"
)

// tagTestEnv 测试环境：包含 TagService、TagRepository 和底层 DB
type tagTestEnv struct {
	svc     *TagService
	tagRepo repository.TagRepository
	db      *gorm.DB
	ctx     context.Context
}

// newTagService 创建测试用 TagService。
// 每次调用都会创建一个全新的 SQLite 内存库，并预置管理员账号和测试项目，
// 确保测试用例之间完全独立、无状态污染。
func newTagService(t *testing.T) (*TagService, context.Context) {
	t.Helper()
	env := newTagTestEnv(t)
	return env.svc, env.ctx
}

// newTagTestEnv 创建完整测试环境（需要直接访问 repo 或 db 时使用）
func newTagTestEnv(t *testing.T) tagTestEnv {
	t.Helper()
	db := testDB(t)
	seedAdmin(t, db)
	seedProject(t, db)
	tagRepo := repository.NewTagRepo(db)
	auditRepo := repository.NewAuditRepo(db)
	txMgr := repository.NewTxManager(db)
	svc := NewTagService(tagRepo, auditRepo, txMgr, testLogger())
	return tagTestEnv{svc: svc, tagRepo: tagRepo, db: db, ctx: context.Background()}
}

// seedTestCase 在测试库中创建一条测试用例
func seedTestCase(t *testing.T, db *gorm.DB, id uint, projectID uint, title string) {
	t.Helper()
	tc := model.TestCase{ID: id, ProjectID: projectID, Title: title, Status: "draft", Version: "V1", Level: "P1", CreatedBy: 1, UpdatedBy: 1}
	if err := db.Create(&tc).Error; err != nil {
		t.Fatalf("seed testcase %d: %v", id, err)
	}
}

// ========== Create 创建标签测试 ==========

// TestTagService_Create 表驱动测试：覆盖名称校验、颜色校验、默认色等场景
func TestTagService_Create(t *testing.T) {
	tests := []struct {
		name    string         // 用例名称
		input   CreateTagInput // 输入参数
		wantErr bool           // 是否期望报错
		errCode int            // 期望的错误码（0 表示不检查）
	}{
		{
			name:    "正常创建",
			input:   CreateTagInput{Name: "冒烟测试", Color: "#3B82F6", Description: "smoke"},
			wantErr: false,
		},
		{
			name:    "名称过短",
			input:   CreateTagInput{Name: "x", Color: "#3B82F6"},
			wantErr: true,
			errCode: CodeParamsError,
		},
		{
			name:    "名称为空",
			input:   CreateTagInput{Name: "", Color: "#3B82F6"},
			wantErr: true,
			errCode: CodeParamsError,
		},
		{
			name:    "名称过长(51字符)",
			input:   CreateTagInput{Name: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaA", Color: "#3B82F6"},
			wantErr: true,
			errCode: CodeParamsError,
		},
		{
			name:    "颜色格式无效",
			input:   CreateTagInput{Name: "有效名称", Color: "red"},
			wantErr: true,
			errCode: CodeTagColorInvalid,
		},
		{
			name:    "颜色为空自动使用默认色",
			input:   CreateTagInput{Name: "默认颜色标签", Color: ""},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, ctx := newTagService(t)
			tag, err := svc.Create(ctx, 1, 1, tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				// 校验错误码是否符合预期
				if tt.errCode != 0 {
					if se, ok := err.(*BizError); ok {
						if se.Code != tt.errCode {
							t.Errorf("error code = %d, want %d", se.Code, tt.errCode)
						}
					}
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				// 成功时应返回带有 ID 的标签
				if tag == nil || tag.ID == 0 {
					t.Fatal("expected valid tag with ID")
				}
			}
		})
	}
}

// TestTagService_Create_DuplicateName 测试同项目下创建重复名称标签应返回 CodeTagNameDuplicate
func TestTagService_Create_DuplicateName(t *testing.T) {
	svc, ctx := newTagService(t)
	_, err := svc.Create(ctx, 1, 1, CreateTagInput{Name: "重复标签", Color: "#3B82F6"})
	if err != nil {
		t.Fatalf("first create failed: %v", err)
	}
	_, err = svc.Create(ctx, 1, 1, CreateTagInput{Name: "重复标签", Color: "#EF4444"})
	if err == nil {
		t.Fatal("expected duplicate error, got nil")
	}
	if se, ok := err.(*BizError); ok {
		if se.Code != CodeTagNameDuplicate {
			t.Errorf("error code = %d, want %d", se.Code, CodeTagNameDuplicate)
		}
	}
}

// TestTagService_Create_ProjectLimit 测试项目标签数量上限（100 个）的限制逻辑
func TestTagService_Create_ProjectLimit(t *testing.T) {
	svc, ctx := newTagService(t)
	// 先创建 100 个标签填满限额
	for i := 0; i < tagLimitPerProject; i++ {
		name := "tag_" + string(rune('A'+i/26)) + string(rune('a'+i%26))
		_, err := svc.Create(ctx, 1, 1, CreateTagInput{Name: name, Color: "#3B82F6"})
		if err != nil {
			t.Fatalf("create tag %d failed: %v", i, err)
		}
	}
	// 第 101 个标签应触发限额错误
	_, err := svc.Create(ctx, 1, 1, CreateTagInput{Name: "超出上限标签", Color: "#3B82F6"})
	if err == nil {
		t.Fatal("expected limit exceeded error")
	}
	if se, ok := err.(*BizError); ok {
		if se.Code != CodeTagLimitExceeded {
			t.Errorf("error code = %d, want %d", se.Code, CodeTagLimitExceeded)
		}
	}
}

// ========== Update 更新标签测试 ==========

// TestTagService_Update 表驱动测试：更新名称、更新颜色、无字段更新
func TestTagService_Update(t *testing.T) {
	svc, ctx := newTagService(t)
	tag, _ := svc.Create(ctx, 1, 1, CreateTagInput{Name: "待更新", Color: "#3B82F6"})

	newName := "已更新"
	newColor := "#EF4444"
	tests := []struct {
		name    string
		input   UpdateTagInput
		wantErr bool
	}{
		{
			name:    "更新名称",
			input:   UpdateTagInput{Name: &newName},
			wantErr: false,
		},
		{
			name:    "更新颜色",
			input:   UpdateTagInput{Color: &newColor},
			wantErr: false,
		},
		{
			name:    "无字段更新",
			input:   UpdateTagInput{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := svc.Update(ctx, 1, tag.ID, 1, tt.input)
			if tt.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// TestTagService_Update_NotFound 测试更新不存在的标签应返回 CodeTagNotFound
func TestTagService_Update_NotFound(t *testing.T) {
	svc, ctx := newTagService(t)
	newName := "测试"
	err := svc.Update(ctx, 1, 99999, 1, UpdateTagInput{Name: &newName})
	if err == nil {
		t.Fatal("expected not found error")
	}
	if se, ok := err.(*BizError); ok {
		if se.Code != CodeTagNotFound {
			t.Errorf("code = %d, want %d", se.Code, CodeTagNotFound)
		}
	}
}

// TestTagService_Update_WrongProject 测试跨项目更新应被拒绝（项目 ID 不匹配）
func TestTagService_Update_WrongProject(t *testing.T) {
	svc, ctx := newTagService(t)
	tag, _ := svc.Create(ctx, 1, 1, CreateTagInput{Name: "项目1标签", Color: "#3B82F6"})
	newName := "改名"
	err := svc.Update(ctx, 999, tag.ID, 1, UpdateTagInput{Name: &newName})
	if err == nil {
		t.Fatal("expected not found error for wrong project")
	}
}

// TestTagService_Update_InvalidColor 测试更新时使用无效颜色格式应被拒绝
func TestTagService_Update_InvalidColor(t *testing.T) {
	svc, ctx := newTagService(t)
	tag, _ := svc.Create(ctx, 1, 1, CreateTagInput{Name: "颜色测试", Color: "#3B82F6"})
	badColor := "invalid"
	err := svc.Update(ctx, 1, tag.ID, 1, UpdateTagInput{Color: &badColor})
	if err == nil {
		t.Fatal("expected color validation error")
	}
}

// ========== Delete 删除标签测试 ==========

// TestTagService_Delete 测试正常删除和重复删除（第二次应失败）
func TestTagService_Delete(t *testing.T) {
	svc, ctx := newTagService(t)
	tag, _ := svc.Create(ctx, 1, 1, CreateTagInput{Name: "待删除", Color: "#3B82F6"})

	unlinked, err := svc.Delete(ctx, 1, tag.ID)
	if err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if unlinked != 0 {
		t.Errorf("unlinked = %d, want 0", unlinked)
	}

	// 再次删除应该失败
	_, err = svc.Delete(ctx, 1, tag.ID)
	if err == nil {
		t.Fatal("expected not found on second delete")
	}
}

// TestTagService_Delete_NotFound 测试删除不存在的标签应返回 404
func TestTagService_Delete_NotFound(t *testing.T) {
	svc, ctx := newTagService(t)
	_, err := svc.Delete(ctx, 1, 99999)
	if err == nil {
		t.Fatal("expected not found error")
	}
}

// TestTagService_Delete_WrongProject 测试跨项目删除应被拒绝（项目 ID 不匹配时不应触发事务）
func TestTagService_Delete_WrongProject(t *testing.T) {
	svc, ctx := newTagService(t)
	tag, _ := svc.Create(ctx, 1, 1, CreateTagInput{Name: "项目隔离", Color: "#3B82F6"})
	// projectID=999 与标签实际归属的 projectID=1 不匹配，应返回不存在
	_, err := svc.Delete(ctx, 999, tag.ID)
	if err == nil {
		t.Fatal("expected not found for wrong project")
	}
	if se, ok := err.(*BizError); ok {
		if se.Code != CodeTagNotFound {
			t.Errorf("code = %d, want %d", se.Code, CodeTagNotFound)
		}
	}
}

// ========== ListPaged 分页查询测试 ==========

// TestTagService_ListPaged 测试基本分页查询：创建 5 个标签后查询总数和列表长度
func TestTagService_ListPaged(t *testing.T) {
	svc, ctx := newTagService(t)
	for i := 0; i < 5; i++ {
		name := "列表标签" + string(rune('A'+i))
		svc.Create(ctx, 1, 1, CreateTagInput{Name: name, Color: "#3B82F6"})
	}

	tags, total, err := svc.ListPaged(ctx, 1, repository.TagFilter{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if total != 5 {
		t.Errorf("total = %d, want 5", total)
	}
	if len(tags) != 5 {
		t.Errorf("len(tags) = %d, want 5", len(tags))
	}
}

// TestTagService_ListPaged_Keyword 测试关键词搜索：搜索“测试”应匹配 2 条
func TestTagService_ListPaged_Keyword(t *testing.T) {
	svc, ctx := newTagService(t)
	svc.Create(ctx, 1, 1, CreateTagInput{Name: "冒烟测试", Color: "#3B82F6"})
	svc.Create(ctx, 1, 1, CreateTagInput{Name: "回归测试", Color: "#EF4444"})
	svc.Create(ctx, 1, 1, CreateTagInput{Name: "性能优化", Color: "#10B981"})

	tags, total, err := svc.ListPaged(ctx, 1, repository.TagFilter{Keyword: "测试", Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
	if len(tags) != 2 {
		t.Errorf("len(tags) = %d, want 2", len(tags))
	}
}

// TestTagService_ListPaged_Pagination 测试分页：15 条数据第 2 页应返回 5 条
func TestTagService_ListPaged_Pagination(t *testing.T) {
	svc, ctx := newTagService(t)
	for i := 0; i < 15; i++ {
		name := "分页标签" + string(rune('A'+i))
		svc.Create(ctx, 1, 1, CreateTagInput{Name: name, Color: "#3B82F6"})
	}

	tags, total, err := svc.ListPaged(ctx, 1, repository.TagFilter{Page: 2, PageSize: 10})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if total != 15 {
		t.Errorf("total = %d, want 15", total)
	}
	if len(tags) != 5 {
		t.Errorf("len(tags) = %d, want 5 (page 2 of 15)", len(tags))
	}
}

// ========== Create 补充测试 ==========

// TestTagService_Create_DefaultColor 测试空颜色时自动分配默认蓝色 #3B82F6
func TestTagService_Create_DefaultColor(t *testing.T) {
	svc, ctx := newTagService(t)
	tag, err := svc.Create(ctx, 1, 1, CreateTagInput{Name: "默认颜色", Color: ""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tag.Color != "#3B82F6" {
		t.Errorf("color = %q, want #3B82F6", tag.Color)
	}
}

// TestTagService_Create_DescriptionPersistence 测试描述字段正确持久化
func TestTagService_Create_DescriptionPersistence(t *testing.T) {
	env := newTagTestEnv(t)
	desc := "这是一个冒烟测试标签"
	tag, err := env.svc.Create(env.ctx, 1, 1, CreateTagInput{Name: "描述测试", Color: "#3B82F6", Description: desc})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 通过 repo 重新查询验证持久化
	found, err := env.tagRepo.FindByID(env.ctx, tag.ID)
	if err != nil {
		t.Fatalf("find by id failed: %v", err)
	}
	if found.Description != desc {
		t.Errorf("description = %q, want %q", found.Description, desc)
	}
}

// TestTagService_Create_NameTrimSpaces 测试名称前后空格会被自动去除
func TestTagService_Create_NameTrimSpaces(t *testing.T) {
	env := newTagTestEnv(t)
	tag, err := env.svc.Create(env.ctx, 1, 1, CreateTagInput{Name: "  前后空格  ", Color: "#3B82F6"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tag.Name != "前后空格" {
		t.Errorf("name = %q, want %q", tag.Name, "前后空格")
	}
}

// ========== Update 补充测试 ==========

// TestTagService_Update_DuplicateName 测试更新为同项目下已存在的标签名应返回重复错误
func TestTagService_Update_DuplicateName(t *testing.T) {
	svc, ctx := newTagService(t)
	svc.Create(ctx, 1, 1, CreateTagInput{Name: "标签A", Color: "#3B82F6"})
	tagB, _ := svc.Create(ctx, 1, 1, CreateTagInput{Name: "标签B", Color: "#EF4444"})

	// 尝试把 B 改名为 A
	nameA := "标签A"
	err := svc.Update(ctx, 1, tagB.ID, 1, UpdateTagInput{Name: &nameA})
	if err == nil {
		t.Fatal("expected duplicate name error, got nil")
	}
	if se, ok := err.(*BizError); ok {
		if se.Code != CodeTagNameDuplicate {
			t.Errorf("error code = %d, want %d", se.Code, CodeTagNameDuplicate)
		}
	}
}

// TestTagService_Update_Description 测试单独更新描述字段
func TestTagService_Update_Description(t *testing.T) {
	env := newTagTestEnv(t)
	tag, _ := env.svc.Create(env.ctx, 1, 1, CreateTagInput{Name: "描述更新", Color: "#3B82F6"})

	newDesc := "更新后的描述"
	err := env.svc.Update(env.ctx, 1, tag.ID, 1, UpdateTagInput{Description: &newDesc})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found, _ := env.tagRepo.FindByID(env.ctx, tag.ID)
	if found.Description != newDesc {
		t.Errorf("description = %q, want %q", found.Description, newDesc)
	}
}

// ========== Delete 补充测试 ==========

// TestTagService_Delete_CascadeUnlink 测试删除标签时级联解除用例关联，并返回正确的 unlinked 数量
func TestTagService_Delete_CascadeUnlink(t *testing.T) {
	env := newTagTestEnv(t)
	// 创建标签
	tag, _ := env.svc.Create(env.ctx, 1, 1, CreateTagInput{Name: "级联删除", Color: "#3B82F6"})

	// 创建 3 条测试用例并关联到该标签
	for i := uint(1); i <= 3; i++ {
		seedTestCase(t, env.db, i, 1, "用例"+string(rune('A'+i-1)))
		env.tagRepo.ReplaceTestCaseTags(env.ctx, nil, i, []uint{tag.ID})
	}

	// 删除标签，应返回 unlinked=3
	unlinked, err := env.svc.Delete(env.ctx, 1, tag.ID)
	if err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if unlinked != 3 {
		t.Errorf("unlinked = %d, want 3", unlinked)
	}

	// 确认关联表中已无该标签的记录
	tagMap, _ := env.tagRepo.ListByTestCaseIDs(env.ctx, []uint{1, 2, 3})
	for _, tags := range tagMap {
		for _, tb := range tags {
			if tb.ID == tag.ID {
				t.Error("tag relation still exists after delete")
			}
		}
	}
}

// ========== ListOptions 候选列表测试 ==========

// TestTagService_ListOptions 测试候选列表基本功能
func TestTagService_ListOptions(t *testing.T) {
	svc, ctx := newTagService(t)
	svc.Create(ctx, 1, 1, CreateTagInput{Name: "冒烟测试", Color: "#3B82F6"})
	svc.Create(ctx, 1, 1, CreateTagInput{Name: "回归测试", Color: "#EF4444"})
	svc.Create(ctx, 1, 1, CreateTagInput{Name: "性能优化", Color: "#10B981"})

	options, err := svc.ListOptions(ctx, 1, "")
	if err != nil {
		t.Fatalf("list options failed: %v", err)
	}
	if len(options) != 3 {
		t.Errorf("len(options) = %d, want 3", len(options))
	}
	// 验证返回的字段完整性
	for _, opt := range options {
		if opt.ID == 0 || opt.Name == "" || opt.Color == "" {
			t.Errorf("incomplete option: %+v", opt)
		}
	}
}

// TestTagService_ListOptions_Keyword 测试候选列表关键词过滤
func TestTagService_ListOptions_Keyword(t *testing.T) {
	svc, ctx := newTagService(t)
	svc.Create(ctx, 1, 1, CreateTagInput{Name: "冒烟测试", Color: "#3B82F6"})
	svc.Create(ctx, 1, 1, CreateTagInput{Name: "回归测试", Color: "#EF4444"})
	svc.Create(ctx, 1, 1, CreateTagInput{Name: "性能优化", Color: "#10B981"})

	options, err := svc.ListOptions(ctx, 1, "测试")
	if err != nil {
		t.Fatalf("list options failed: %v", err)
	}
	if len(options) != 2 {
		t.Errorf("len(options) = %d, want 2", len(options))
	}
}

// TestTagService_ListOptions_EmptyProject 测试空项目的候选列表应返回空切片
func TestTagService_ListOptions_EmptyProject(t *testing.T) {
	svc, ctx := newTagService(t)
	options, err := svc.ListOptions(ctx, 1, "")
	if err != nil {
		t.Fatalf("list options failed: %v", err)
	}
	if len(options) != 0 {
		t.Errorf("len(options) = %d, want 0", len(options))
	}
}

// TestTagService_ListOptions_ProjectIsolation 测试项目间标签隔离
func TestTagService_ListOptions_ProjectIsolation(t *testing.T) {
	env := newTagTestEnv(t)
	// 在项目1创建标签
	env.svc.Create(env.ctx, 1, 1, CreateTagInput{Name: "项目1标签", Color: "#3B82F6"})

	// 查询项目999的标签，应为空
	options, err := env.svc.ListOptions(env.ctx, 999, "")
	if err != nil {
		t.Fatalf("list options failed: %v", err)
	}
	if len(options) != 0 {
		t.Errorf("len(options) = %d, want 0 (project isolation)", len(options))
	}
}

// ========== 标签关联操作测试 ==========

// TestTagRepo_ReplaceTestCaseTags 测试替换用例标签：先设置再替换，验证最终关联正确
func TestTagRepo_ReplaceTestCaseTags(t *testing.T) {
	env := newTagTestEnv(t)
	seedTestCase(t, env.db, 1, 1, "替换标签用例")
	tagA, _ := env.svc.Create(env.ctx, 1, 1, CreateTagInput{Name: "标签A", Color: "#3B82F6"})
	tagB, _ := env.svc.Create(env.ctx, 1, 1, CreateTagInput{Name: "标签B", Color: "#EF4444"})
	tagC, _ := env.svc.Create(env.ctx, 1, 1, CreateTagInput{Name: "标签C", Color: "#10B981"})

	// 初始关联 A, B
	err := env.tagRepo.ReplaceTestCaseTags(env.ctx, nil, 1, []uint{tagA.ID, tagB.ID})
	if err != nil {
		t.Fatalf("first replace failed: %v", err)
	}
	tagMap, _ := env.tagRepo.ListByTestCaseIDs(env.ctx, []uint{1})
	if len(tagMap[1]) != 2 {
		t.Fatalf("after first replace: len = %d, want 2", len(tagMap[1]))
	}

	// 替换为 B, C（A 应被移除）
	err = env.tagRepo.ReplaceTestCaseTags(env.ctx, nil, 1, []uint{tagB.ID, tagC.ID})
	if err != nil {
		t.Fatalf("second replace failed: %v", err)
	}
	tagMap, _ = env.tagRepo.ListByTestCaseIDs(env.ctx, []uint{1})
	if len(tagMap[1]) != 2 {
		t.Fatalf("after second replace: len = %d, want 2", len(tagMap[1]))
	}
	// 检查 A 不在、B C 在
	ids := map[uint]bool{}
	for _, tb := range tagMap[1] {
		ids[tb.ID] = true
	}
	if ids[tagA.ID] {
		t.Error("tagA should have been removed")
	}
	if !ids[tagB.ID] || !ids[tagC.ID] {
		t.Error("tagB and tagC should be present")
	}
}

// TestTagRepo_ReplaceTestCaseTags_ClearAll 测试传空 tagIDs 清除所有关联
func TestTagRepo_ReplaceTestCaseTags_ClearAll(t *testing.T) {
	env := newTagTestEnv(t)
	seedTestCase(t, env.db, 1, 1, "清空标签用例")
	tag, _ := env.svc.Create(env.ctx, 1, 1, CreateTagInput{Name: "待清除", Color: "#3B82F6"})
	env.tagRepo.ReplaceTestCaseTags(env.ctx, nil, 1, []uint{tag.ID})

	// 用空切片替换，应清除所有关联
	err := env.tagRepo.ReplaceTestCaseTags(env.ctx, nil, 1, []uint{})
	if err != nil {
		t.Fatalf("clear tags failed: %v", err)
	}
	tagMap, _ := env.tagRepo.ListByTestCaseIDs(env.ctx, []uint{1})
	if len(tagMap[1]) != 0 {
		t.Errorf("after clear: len = %d, want 0", len(tagMap[1]))
	}
}

// TestTagRepo_CopyTestCaseTags 测试复制用例标签到新用例
func TestTagRepo_CopyTestCaseTags(t *testing.T) {
	env := newTagTestEnv(t)
	seedTestCase(t, env.db, 1, 1, "源用例")
	seedTestCase(t, env.db, 2, 1, "目标用例")
	tagA, _ := env.svc.Create(env.ctx, 1, 1, CreateTagInput{Name: "标签A", Color: "#3B82F6"})
	tagB, _ := env.svc.Create(env.ctx, 1, 1, CreateTagInput{Name: "标签B", Color: "#EF4444"})
	env.tagRepo.ReplaceTestCaseTags(env.ctx, nil, 1, []uint{tagA.ID, tagB.ID})

	// 复制源用例的标签到目标用例
	err := env.tagRepo.CopyTestCaseTags(env.ctx, nil, 1, 2)
	if err != nil {
		t.Fatalf("copy tags failed: %v", err)
	}

	// 验证目标用例获得了相同的标签
	tagMap, _ := env.tagRepo.ListByTestCaseIDs(env.ctx, []uint{2})
	if len(tagMap[2]) != 2 {
		t.Fatalf("target case tags: len = %d, want 2", len(tagMap[2]))
	}

	// 验证源用例标签未受影响
	srcMap, _ := env.tagRepo.ListByTestCaseIDs(env.ctx, []uint{1})
	if len(srcMap[1]) != 2 {
		t.Errorf("source case tags should remain: len = %d, want 2", len(srcMap[1]))
	}
}

// TestTagRepo_CopyTestCaseTags_EmptySource 测试复制无标签的用例（应无报错）
func TestTagRepo_CopyTestCaseTags_EmptySource(t *testing.T) {
	env := newTagTestEnv(t)
	seedTestCase(t, env.db, 1, 1, "空标签源")
	seedTestCase(t, env.db, 2, 1, "目标")
	err := env.tagRepo.CopyTestCaseTags(env.ctx, nil, 1, 2)
	if err != nil {
		t.Fatalf("copy from empty source should not error: %v", err)
	}
}

// TestTagRepo_ListByTestCaseIDs 测试批量查询多个用例的标签
func TestTagRepo_ListByTestCaseIDs(t *testing.T) {
	env := newTagTestEnv(t)
	seedTestCase(t, env.db, 1, 1, "用例1")
	seedTestCase(t, env.db, 2, 1, "用例2")
	seedTestCase(t, env.db, 3, 1, "用例3")
	tagA, _ := env.svc.Create(env.ctx, 1, 1, CreateTagInput{Name: "标签A", Color: "#3B82F6"})
	tagB, _ := env.svc.Create(env.ctx, 1, 1, CreateTagInput{Name: "标签B", Color: "#EF4444"})

	env.tagRepo.ReplaceTestCaseTags(env.ctx, nil, 1, []uint{tagA.ID, tagB.ID}) // 用例1: A,B
	env.tagRepo.ReplaceTestCaseTags(env.ctx, nil, 2, []uint{tagA.ID})          // 用例2: A
	// 用例3 无标签

	tagMap, err := env.tagRepo.ListByTestCaseIDs(env.ctx, []uint{1, 2, 3})
	if err != nil {
		t.Fatalf("list by ids failed: %v", err)
	}
	if len(tagMap[1]) != 2 {
		t.Errorf("case 1 tags: %d, want 2", len(tagMap[1]))
	}
	if len(tagMap[2]) != 1 {
		t.Errorf("case 2 tags: %d, want 1", len(tagMap[2]))
	}
	if len(tagMap[3]) != 0 {
		t.Errorf("case 3 tags: %d, want 0", len(tagMap[3]))
	}
}

// TestTagRepo_ListByTestCaseIDs_EmptyInput 测试空用例 ID 列表应返回空 map
func TestTagRepo_ListByTestCaseIDs_EmptyInput(t *testing.T) {
	env := newTagTestEnv(t)
	tagMap, err := env.tagRepo.ListByTestCaseIDs(env.ctx, []uint{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tagMap) != 0 {
		t.Errorf("expected empty map, got %d entries", len(tagMap))
	}
}

// TestTagRepo_DeleteRelsByTestCaseID 测试按用例 ID 删除关联后标签本体不受影响
func TestTagRepo_DeleteRelsByTestCaseID(t *testing.T) {
	env := newTagTestEnv(t)
	seedTestCase(t, env.db, 1, 1, "删除关联用例")
	tag, _ := env.svc.Create(env.ctx, 1, 1, CreateTagInput{Name: "不被删除", Color: "#3B82F6"})
	env.tagRepo.ReplaceTestCaseTags(env.ctx, nil, 1, []uint{tag.ID})

	// 删除用例1的标签关联
	err := env.tagRepo.DeleteRelsByTestCaseID(env.ctx, nil, 1)
	if err != nil {
		t.Fatalf("delete rels failed: %v", err)
	}

	// 关联已被删除
	tagMap, _ := env.tagRepo.ListByTestCaseIDs(env.ctx, []uint{1})
	if len(tagMap[1]) != 0 {
		t.Errorf("case 1 should have no tags after delete rels, got %d", len(tagMap[1]))
	}

	// 标签本体仍然存在
	found, err := env.tagRepo.FindByID(env.ctx, tag.ID)
	if err != nil || found == nil {
		t.Error("tag entity should still exist after deleting relations")
	}
}

// TestTagRepo_DeleteRelsByTestCaseIDs_Batch 测试批量删除多个用例的标签关联
func TestTagRepo_DeleteRelsByTestCaseIDs_Batch(t *testing.T) {
	env := newTagTestEnv(t)
	seedTestCase(t, env.db, 1, 1, "批量删除1")
	seedTestCase(t, env.db, 2, 1, "批量删除2")
	tag, _ := env.svc.Create(env.ctx, 1, 1, CreateTagInput{Name: "共享标签", Color: "#3B82F6"})
	env.tagRepo.ReplaceTestCaseTags(env.ctx, nil, 1, []uint{tag.ID})
	env.tagRepo.ReplaceTestCaseTags(env.ctx, nil, 2, []uint{tag.ID})

	// 批量删除
	err := env.tagRepo.DeleteRelsByTestCaseIDs(env.ctx, nil, []uint{1, 2})
	if err != nil {
		t.Fatalf("batch delete rels failed: %v", err)
	}

	tagMap, _ := env.tagRepo.ListByTestCaseIDs(env.ctx, []uint{1, 2})
	if len(tagMap[1])+len(tagMap[2]) != 0 {
		t.Error("all relations should be deleted")
	}
}

// ========== ListPaged 排序测试 ==========

// TestTagService_ListPaged_SortByName 测试按名称升序排序
func TestTagService_ListPaged_SortByName(t *testing.T) {
	svc, ctx := newTagService(t)
	svc.Create(ctx, 1, 1, CreateTagInput{Name: "Charlie", Color: "#3B82F6"})
	svc.Create(ctx, 1, 1, CreateTagInput{Name: "Alpha", Color: "#EF4444"})
	svc.Create(ctx, 1, 1, CreateTagInput{Name: "Bravo", Color: "#10B981"})

	tags, _, err := svc.ListPaged(ctx, 1, repository.TagFilter{Page: 1, PageSize: 10, SortBy: "name"})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(tags) != 3 {
		t.Fatalf("len = %d, want 3", len(tags))
	}
	if tags[0].Name != "Alpha" || tags[1].Name != "Bravo" || tags[2].Name != "Charlie" {
		t.Errorf("sort order wrong: %s, %s, %s", tags[0].Name, tags[1].Name, tags[2].Name)
	}
}

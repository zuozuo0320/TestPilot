// tag_service_test.go — TagService 单元测试
//
// 采用表驱动测试范式，覆盖以下场景：
//   - Create: 正常创建、名称校验（过短/为空/过长）、颜色校验、默认色、重复名、项目限额
//   - Update: 正常更新、不存在、跨项目、无字段、无效颜色
//   - Delete: 正常删除、重复删除、不存在、跨项目
//   - ListPaged: 分页、关键词搜索、第二页数据
//
// 每个测试用例使用独立的 SQLite 内存库，确保测试间无状态污染。
package service

import (
	"context"
	"testing"

	"testpilot/internal/repository"
)

// newTagService 创建测试用 TagService。
// 每次调用都会创建一个全新的 SQLite 内存库，并预置管理员账号和测试项目，
// 确保测试用例之间完全独立、无状态污染。
func newTagService(t *testing.T) (*TagService, context.Context) {
	t.Helper()
	db := testDB(t)          // 创建 SQLite 内存库 + 自动迁移
	seedAdmin(t, db)          // 预置管理员用户（ID=1）
	seedProject(t, db)        // 预置测试项目（ID=1）
	tagRepo := repository.NewTagRepo(db)
	auditRepo := repository.NewAuditRepo(db)
	txMgr := repository.NewTxManager(db)
	svc := NewTagService(tagRepo, auditRepo, txMgr, testLogger())
	return svc, context.Background()
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

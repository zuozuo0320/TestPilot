package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"testpilot/internal/model"
	"testpilot/internal/repository"
)

func TestTestCaseService_CreateSuccess(t *testing.T) {
	db := testDB(t)
	seedAdmin(t, db)
	seedProject(t, db)
	tcRepo := repository.NewTestCaseRepo(db)
	svc := NewTestCaseService(tcRepo, repository.NewCaseHistoryRepo(db))

	tc, err := svc.Create(context.Background(), 1, 1, CreateTestCaseInput{
		Title: "TC-1", Level: "P0", Priority: "high",
	})
	require.NoError(t, err)
	assert.NotZero(t, tc.ID)
	assert.Equal(t, "TC-1", tc.Title)
	assert.Equal(t, "P0", tc.Level)
	assert.Equal(t, "high", tc.Priority)
}

func TestTestCaseService_CreateDefaults(t *testing.T) {
	db := testDB(t)
	seedAdmin(t, db)
	seedProject(t, db)
	tcRepo := repository.NewTestCaseRepo(db)
	svc := NewTestCaseService(tcRepo, repository.NewCaseHistoryRepo(db))

	// 不传 level/review_result/exec_result → 使用默认值
	tc, err := svc.Create(context.Background(), 1, 1, CreateTestCaseInput{
		Title: "TC-DEFAULT",
	})
	require.NoError(t, err)
	assert.Equal(t, "P1", tc.Level)            // 默认
	assert.Equal(t, "未评审", tc.ReviewResult)     // 默认
	assert.Equal(t, "未执行", tc.ExecResult)       // 默认
	assert.Equal(t, "medium", tc.Priority)      // 默认
	assert.Equal(t, "/未分类", tc.ModulePath)      // 默认
}

func TestTestCaseService_ListPaged(t *testing.T) {
	db := testDB(t)
	seedAdmin(t, db)
	seedProject(t, db)
	tcRepo := repository.NewTestCaseRepo(db)
	svc := NewTestCaseService(tcRepo, repository.NewCaseHistoryRepo(db))

	// 创建 3 个用例
	for i := 0; i < 3; i++ {
		tc := model.TestCase{
			ProjectID: 1, Title: "TC-" + string(rune('A'+i)),
			Level: "P0", CreatedBy: 1, UpdatedBy: 1,
		}
		db.Create(&tc)
	}

	items, total, err := svc.ListPaged(context.Background(), 1, repository.TestCaseFilter{
		Page: 1, PageSize: 2,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)
	assert.Len(t, items, 2) // pageSize=2
}

func TestTestCaseService_UpdateSuccess(t *testing.T) {
	db := testDB(t)
	seedAdmin(t, db)
	seedProject(t, db)
	tcRepo := repository.NewTestCaseRepo(db)
	svc := NewTestCaseService(tcRepo, repository.NewCaseHistoryRepo(db))

	tc := model.TestCase{
		ProjectID: 1, Title: "Original", Level: "P0",
		CreatedBy: 1, UpdatedBy: 1,
	}
	db.Create(&tc)

	newTitle := "Updated"
	newLevel := "P1"
	updated, err := svc.Update(context.Background(), 1, tc.ID, UpdateTestCaseInput{
		Title: &newTitle, Level: &newLevel,
	})
	require.NoError(t, err)
	assert.Equal(t, "Updated", updated.Title)
	assert.Equal(t, "P1", updated.Level)
}

func TestTestCaseService_DeleteSuccess(t *testing.T) {
	db := testDB(t)
	seedProject(t, db)
	tcRepo := repository.NewTestCaseRepo(db)
	svc := NewTestCaseService(tcRepo, repository.NewCaseHistoryRepo(db))

	tc := model.TestCase{
		ProjectID: 1, Title: "ToDelete", Level: "P0",
		CreatedBy: 1, UpdatedBy: 1,
	}
	db.Create(&tc)

	err := svc.Delete(context.Background(), 1, tc.ID)
	require.NoError(t, err)
}

func TestTestCaseService_DeleteNotFound(t *testing.T) {
	db := testDB(t)
	seedProject(t, db)
	tcRepo := repository.NewTestCaseRepo(db)
	svc := NewTestCaseService(tcRepo, repository.NewCaseHistoryRepo(db))

	err := svc.Delete(context.Background(), 1, 9999)
	require.Error(t, err)
}

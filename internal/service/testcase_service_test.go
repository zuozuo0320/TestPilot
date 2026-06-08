package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"testpilot/internal/model"
	"testpilot/internal/repository"
)

func newTestCaseSvcForTest(db *gorm.DB) *TestCaseService {
	tcRepo := repository.NewTestCaseRepo(db)
	return NewTestCaseService(
		tcRepo,
		repository.NewCaseHistoryRepo(db),
		repository.NewAuditRepo(db),
		repository.NewTagRepo(db),
		repository.NewCaseReviewRepo(db),
		repository.NewTxManager(db),
	)
}

func TestTestCaseService_CreateSuccess(t *testing.T) {
	db := testDB(t)
	seedAdmin(t, db)
	seedProject(t, db)
	svc := newTestCaseSvcForTest(db)

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
	svc := newTestCaseSvcForTest(db)

	// 不传 level/review_result/exec_result → 使用默认值
	tc, err := svc.Create(context.Background(), 1, 1, CreateTestCaseInput{
		Title: "TC-DEFAULT",
	})
	require.NoError(t, err)
	assert.Equal(t, "P1", tc.Level)         // 默认
	assert.Equal(t, "未评审", tc.ReviewResult) // 默认
	assert.Equal(t, "未执行", tc.ExecResult)   // 默认
	assert.Equal(t, "medium", tc.Priority)  // 默认
	assert.Equal(t, "/未分类", tc.ModulePath)  // 默认
}

func TestTestCaseService_ListPaged(t *testing.T) {
	db := testDB(t)
	seedAdmin(t, db)
	seedProject(t, db)
	svc := newTestCaseSvcForTest(db)

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

func TestTestCaseService_ListPaged_UsesItemFinalResult(t *testing.T) {
	db := testDB(t)
	seedAdmin(t, db)
	seedTester(t, db)
	seedProject(t, db)
	svc := newTestCaseSvcForTest(db)

	tc := model.TestCase{
		ID:           201,
		ProjectID:    1,
		Title:        "评审结果一致性用例",
		Status:       model.TestCaseStatusActive,
		ReviewResult: model.CaseReviewResultApproved,
		Level:        "P0",
		CreatedBy:    1,
		UpdatedBy:    1,
	}
	require.NoError(t, db.Create(&tc).Error)
	review := model.CaseReview{
		ID:         21,
		ProjectID:  1,
		Name:       "一致性评审",
		ReviewMode: model.ReviewModeSingle,
		Status:     model.ReviewPlanStatusInProgress,
		CreatedBy:  1,
		UpdatedBy:  1,
	}
	require.NoError(t, db.Create(&review).Error)
	item := model.CaseReviewItem{
		ID:             31,
		ReviewID:       review.ID,
		ProjectID:      1,
		TestCaseID:     tc.ID,
		TitleSnapshot:  tc.Title,
		ReviewStatus:   model.ReviewItemStatusCompleted,
		FinalResult:    model.ReviewResultApproved,
		CurrentRoundNo: 1,
		CreatedBy:      1,
		UpdatedBy:      1,
	}
	require.NoError(t, db.Create(&item).Error)
	require.NoError(t, db.Create(&model.CaseReviewItemReviewer{
		ReviewID:     review.ID,
		ReviewItemID: item.ID,
		ProjectID:    1,
		ReviewerID:   1,
		ReviewStatus: model.ReviewerStatusReviewed,
		ReviewRole:   model.ReviewRolePrimary,
		LatestResult: model.ReviewResultRejected,
	}).Error)

	items, total, err := svc.ListPaged(context.Background(), 1, repository.TestCaseFilter{
		Page: 1, PageSize: 10,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, items, 1)
	assert.Equal(t, model.TestCaseStatusActive, items[0].Status)
	assert.Equal(t, model.CaseReviewResultApproved, items[0].ReviewResult)
}

func TestTestCaseService_UpdateSuccess(t *testing.T) {
	db := testDB(t)
	seedAdmin(t, db)
	seedProject(t, db)
	svc := newTestCaseSvcForTest(db)

	tc := model.TestCase{
		ProjectID: 1, Title: "Original", Level: "P0",
		CreatedBy: 1, UpdatedBy: 1,
	}
	db.Create(&tc)

	newTitle := "Updated"
	newLevel := "P1"
	updated, err := svc.Update(context.Background(), 1, tc.ID, 1, UpdateTestCaseInput{
		Title: &newTitle, Level: &newLevel,
	})
	require.NoError(t, err)
	assert.Equal(t, "Updated", updated.Title)
	assert.Equal(t, "P1", updated.Level)
}

func TestTestCaseService_Update_AutoResubmitsReturnedReviewItem(t *testing.T) {
	cases := []struct {
		name                string
		finalResult         string
		initialReviewStatus string
		wantReviewStatus    string
	}{
		{
			name:                "打回修订后保存自动重新提审",
			finalResult:         model.ReviewResultNeedsUpdate,
			initialReviewStatus: model.ReviewPlanStatusInProgress,
			wantReviewStatus:    model.ReviewPlanStatusInProgress,
		},
		{
			name:                "拒绝后保存自动重新提审",
			finalResult:         model.ReviewResultRejected,
			initialReviewStatus: model.ReviewPlanStatusInProgress,
			wantReviewStatus:    model.ReviewPlanStatusInProgress,
		},
		{
			name:                "已完成评审计划内打回用例保存后重新打开复审",
			finalResult:         model.ReviewResultNeedsUpdate,
			initialReviewStatus: model.ReviewPlanStatusCompleted,
			wantReviewStatus:    model.ReviewPlanStatusInProgress,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := testDB(t)
			seedAdmin(t, db)
			seedTester(t, db)
			seedProject(t, db)
			svc := newTestCaseSvcForTest(db)

			testcase := model.TestCase{
				ID:           301,
				ProjectID:    1,
				Title:        "待修订用例",
				Status:       model.TestCaseStatusDraft,
				ReviewResult: model.CaseReviewResultNeedsUpdate,
				Level:        "P1",
				CreatedBy:    2,
				UpdatedBy:    2,
			}
			require.NoError(t, db.Create(&testcase).Error)
			review := model.CaseReview{
				ID:         41,
				ProjectID:  1,
				Name:       "自动提审评审",
				ReviewMode: model.ReviewModeSingle,
				Status:     tc.initialReviewStatus,
				CreatedBy:  1,
				UpdatedBy:  1,
			}
			require.NoError(t, db.Create(&review).Error)
			item := model.CaseReviewItem{
				ID:             51,
				ReviewID:       review.ID,
				ProjectID:      1,
				TestCaseID:     testcase.ID,
				TitleSnapshot:  testcase.Title,
				ReviewStatus:   model.ReviewItemStatusCompleted,
				FinalResult:    tc.finalResult,
				CurrentRoundNo: 1,
				LatestComment:  "请修订",
				CreatedBy:      1,
				UpdatedBy:      1,
			}
			require.NoError(t, db.Create(&item).Error)
			require.NoError(t, db.Create(&model.CaseReviewItemReviewer{
				ReviewID:      review.ID,
				ReviewItemID:  item.ID,
				ProjectID:     1,
				ReviewerID:    1,
				ReviewStatus:  model.ReviewerStatusReviewed,
				ReviewRole:    model.ReviewRolePrimary,
				LatestResult:  tc.finalResult,
				LatestComment: "请修订",
			}).Error)

			newTitle := "已修订用例"
			updated, err := svc.Update(context.Background(), 1, testcase.ID, 2, UpdateTestCaseInput{
				Title: &newTitle,
			})
			require.NoError(t, err)
			assert.Equal(t, model.TestCaseStatusPending, updated.Status)
			assert.Equal(t, model.CaseReviewResultResubmit, updated.ReviewResult)

			var updatedItem model.CaseReviewItem
			require.NoError(t, db.First(&updatedItem, item.ID).Error)
			assert.Equal(t, model.ReviewItemStatusPending, updatedItem.ReviewStatus)
			assert.Equal(t, model.ReviewResultPending, updatedItem.FinalResult)
			assert.Equal(t, 2, updatedItem.CurrentRoundNo)
			assert.Empty(t, updatedItem.LatestComment)

			var reviewer model.CaseReviewItemReviewer
			require.NoError(t, db.Where("review_item_id = ? AND reviewer_id = ?", item.ID, 1).First(&reviewer).Error)
			assert.Equal(t, model.ReviewerStatusPending, reviewer.ReviewStatus)
			assert.Empty(t, reviewer.LatestResult)
			assert.Empty(t, reviewer.LatestComment)

			var updatedReview model.CaseReview
			require.NoError(t, db.First(&updatedReview, review.ID).Error)
			assert.Equal(t, 1, updatedReview.PendingCount)
			assert.Equal(t, 0, updatedReview.NeedsUpdateCount)
			assert.Equal(t, 0, updatedReview.RejectedCount)
			assert.Equal(t, tc.wantReviewStatus, updatedReview.Status)
		})
	}
}

func TestTestCaseService_DeleteSuccess(t *testing.T) {
	db := testDB(t)
	seedProject(t, db)
	svc := newTestCaseSvcForTest(db)

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
	svc := newTestCaseSvcForTest(db)

	err := svc.Delete(context.Background(), 1, 9999)
	require.Error(t, err)
}

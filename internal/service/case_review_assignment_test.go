// case_review_assignment_test.go — v0.2 评审人派生 + 自审校验单测
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

// ─── 纯函数单测：deriveAssignment ───

// TestDeriveAssignment_WithPrimary Primary 显式指定时优先；Shadow 去重 & 排除 Primary
func TestDeriveAssignment_WithPrimary(t *testing.T) {
	a, err := deriveAssignment(10, []uint{20, 30, 10, 0, 20}, nil)
	require.NoError(t, err)
	assert.Equal(t, uint(10), a.PrimaryID)
	assert.Equal(t, []uint{20, 30}, a.ShadowIDs, "应去重且排除 Primary 与 0")
}

// TestDeriveAssignment_Legacy 只传 legacyIDs 时：首元素当 Primary，其余 Shadow（去重）
func TestDeriveAssignment_Legacy(t *testing.T) {
	a, err := deriveAssignment(0, nil, []uint{5, 6, 7, 5, 6})
	require.NoError(t, err)
	assert.Equal(t, uint(5), a.PrimaryID)
	assert.Equal(t, []uint{6, 7}, a.ShadowIDs)
}

// TestDeriveAssignment_Empty Primary 缺失时返回 BizError
func TestDeriveAssignment_Empty(t *testing.T) {
	_, err := deriveAssignment(0, nil, nil)
	require.Error(t, err)
	bizErr, ok := err.(*BizError)
	require.True(t, ok)
	assert.Equal(t, CodeReviewPrimaryRequired, bizErr.NumericCode())
}

// ─── 纯函数单测：ensureNoSelfReview ───

// TestEnsureNoSelfReview_Primary Author 作为 Primary 被拦截
func TestEnsureNoSelfReview_Primary(t *testing.T) {
	tc := &model.TestCase{CreatedBy: 7}
	err := ensureNoSelfReview(tc, ReviewerAssignment{PrimaryID: 7, ShadowIDs: []uint{8}})
	require.Error(t, err)
	bizErr, ok := err.(*BizError)
	require.True(t, ok)
	assert.Equal(t, CodeReviewSelfReviewForbidden, bizErr.NumericCode())
}

// TestEnsureNoSelfReview_Shadow Author 出现在 Shadow 也被拦截
func TestEnsureNoSelfReview_Shadow(t *testing.T) {
	tc := &model.TestCase{CreatedBy: 7}
	err := ensureNoSelfReview(tc, ReviewerAssignment{PrimaryID: 1, ShadowIDs: []uint{2, 7, 9}})
	require.Error(t, err)
}

// TestEnsureNoSelfReview_NoAuthor Author 不在名单时放行
func TestEnsureNoSelfReview_NoAuthor(t *testing.T) {
	tc := &model.TestCase{CreatedBy: 7}
	err := ensureNoSelfReview(tc, ReviewerAssignment{PrimaryID: 1, ShadowIDs: []uint{2, 3}})
	assert.NoError(t, err)
}

// ─── 集成测试：CreateReview / LinkItems 自审路径 ───

// selfReviewEnv 组装 CreateReview 自审校验的依赖
type selfReviewEnv struct {
	ctx         context.Context
	db          *gorm.DB
	svc         *CaseReviewService
	projectRepo repository.ProjectRepository
	projectID   uint
	adminID     uint
	testerID    uint
	testcaseID  uint
}

func newSelfReviewEnv(t *testing.T) *selfReviewEnv {
	t.Helper()
	db := testDB(t)
	seedAdmin(t, db)
	seedTester(t, db)
	seedProject(t, db)

	// Tester 创建用例（Tester 因此是 Author）
	tc := model.TestCase{
		ID:            100,
		ProjectID:     1,
		Title:         "自审测试用例",
		Precondition:  "账号已登录",
		Steps:         "1. 发起请求；2. 观察响应；3. 验证结果符合预期",
		Postcondition: "无额外副作用",
		Level:         "P1",
		Status:        model.TestCaseStatusPending,
		Version:       "V1",
		CreatedBy:     2, // Tester 是 Author
	}
	require.NoError(t, db.Create(&tc).Error)

	txMgr := repository.NewTxManager(db)
	reviewRepo := repository.NewCaseReviewRepo(db)
	recordRepo := repository.NewCaseReviewRecordRepo(db)
	testCaseRepo := repository.NewTestCaseRepo(db)
	userRepo := repository.NewUserRepo(db)
	projectRepo := repository.NewProjectRepo(db)
	attRepo := repository.NewCaseReviewAttachmentRepo(db)
	svc := NewCaseReviewService(reviewRepo, recordRepo, testCaseRepo, userRepo, projectRepo, attRepo, txMgr, testLogger())

	return &selfReviewEnv{
		ctx:         context.Background(),
		db:          db,
		svc:         svc,
		projectRepo: projectRepo,
		projectID:   1,
		adminID:     1,
		testerID:    2,
		testcaseID:  tc.ID,
	}
}

// TestCaseReviewService_CreateReview_RejectSelfReview
// Tester 作为 Primary 去评审自己写的用例，默认 settings 应禁止
func TestCaseReviewService_CreateReview_RejectSelfReview(t *testing.T) {
	env := newSelfReviewEnv(t)

	// Tester（=Author）创建评审，Primary=Tester
	_, err := env.svc.CreateReview(env.ctx, env.projectID, env.testerID, CreateReviewInput{
		Name:                     "Self Review Plan",
		ReviewMode:               model.ReviewModeSingle,
		DefaultReviewerIDs:       []uint{env.testerID},
		DefaultPrimaryReviewerID: env.testerID,
		TestCaseIDs:              []uint{env.testcaseID},
	})
	require.Error(t, err)
	bizErr, ok := err.(*BizError)
	require.True(t, ok)
	assert.Equal(t, CodeReviewSelfReviewForbidden, bizErr.NumericCode())
}

// TestCaseReviewService_CreateReview_AllowSelfReview
// 开启 settings.allow_self_review=true 后允许自审
func TestCaseReviewService_CreateReview_AllowSelfReview(t *testing.T) {
	env := newSelfReviewEnv(t)

	// 开启自审：直接 SQL 写 settings 字段
	require.NoError(t, env.db.Exec(`UPDATE projects SET settings = ? WHERE id = ?`, `{"allow_self_review":true}`, env.projectID).Error)

	// 回读确认 FindByID 真的能把 settings 返回出来（曾经因 projectSelectColumns 漏列而失效，回归守卫）
	project2, err := env.projectRepo.FindByID(env.ctx, env.projectID)
	require.NoError(t, err)
	require.True(t, project2.ParseSettings().AllowSelfReview, "projectSelectColumns 必须包含 settings 列")

	// 现在 Tester 可以评审自己的用例
	review, err := env.svc.CreateReview(env.ctx, env.projectID, env.testerID, CreateReviewInput{
		Name:                     "Self Review Plan",
		ReviewMode:               model.ReviewModeSingle,
		DefaultReviewerIDs:       []uint{env.testerID},
		DefaultPrimaryReviewerID: env.testerID,
		TestCaseIDs:              []uint{env.testcaseID},
	})
	require.NoError(t, err)
	assert.NotZero(t, review.ID)
	assert.Equal(t, env.testerID, review.ModeratorID, "ModeratorID 默认等于创建者")
	assert.True(t, review.AIEnabled, "AIEnabled 默认开启")
}

// TestCaseReviewService_CreateReview_DefaultModerator
// 不显式传 ModeratorID 时，默认等于创建者
func TestCaseReviewService_CreateReview_DefaultModerator(t *testing.T) {
	env := newSelfReviewEnv(t)

	review, err := env.svc.CreateReview(env.ctx, env.projectID, env.adminID, CreateReviewInput{
		Name:                     "Normal Plan",
		ReviewMode:               model.ReviewModeSingle,
		DefaultReviewerIDs:       []uint{env.adminID},
		DefaultPrimaryReviewerID: env.adminID,
		TestCaseIDs:              []uint{env.testcaseID},
	})
	require.NoError(t, err)
	assert.Equal(t, env.adminID, review.ModeratorID)
	assert.True(t, review.AIEnabled)
}

// TestCaseReviewService_BatchResubmit_ReopensCompletedPlan
// 已完成计划中的打回用例允许重新提审，并把计划重新打开为进行中。
func TestCaseReviewService_BatchResubmit_ReopensCompletedPlan(t *testing.T) {
	env := newSelfReviewEnv(t)

	require.NoError(t, env.db.Model(&model.TestCase{}).
		Where("id = ? AND project_id = ?", env.testcaseID, env.projectID).
		Updates(map[string]any{
			"status":        model.TestCaseStatusDraft,
			"review_result": model.CaseReviewResultNeedsUpdate,
		}).Error)

	review := model.CaseReview{
		ID:         200,
		ProjectID:  env.projectID,
		Name:       "已完成后重新提审",
		ReviewMode: model.ReviewModeSingle,
		Status:     model.ReviewPlanStatusCompleted,
		CreatedBy:  env.adminID,
		UpdatedBy:  env.adminID,
	}
	require.NoError(t, env.db.Create(&review).Error)
	item := model.CaseReviewItem{
		ID:             201,
		ReviewID:       review.ID,
		ProjectID:      env.projectID,
		TestCaseID:     env.testcaseID,
		TitleSnapshot:  "自审测试用例",
		ReviewStatus:   model.ReviewItemStatusCompleted,
		FinalResult:    model.ReviewResultNeedsUpdate,
		CurrentRoundNo: 1,
		LatestComment:  "请修订",
		CreatedBy:      env.adminID,
		UpdatedBy:      env.adminID,
	}
	require.NoError(t, env.db.Create(&item).Error)
	require.NoError(t, env.db.Create(&model.CaseReviewItemReviewer{
		ReviewID:      review.ID,
		ReviewItemID:  item.ID,
		ProjectID:     env.projectID,
		ReviewerID:    env.adminID,
		ReviewStatus:  model.ReviewerStatusReviewed,
		ReviewRole:    model.ReviewRolePrimary,
		LatestResult:  model.ReviewResultNeedsUpdate,
		LatestComment: "请修订",
	}).Error)

	err := env.svc.BatchResubmit(env.ctx, env.projectID, review.ID, env.adminID, []uint{item.ID})
	require.NoError(t, err)

	var updatedItem model.CaseReviewItem
	require.NoError(t, env.db.First(&updatedItem, item.ID).Error)
	assert.Equal(t, model.ReviewItemStatusPending, updatedItem.ReviewStatus)
	assert.Equal(t, model.ReviewResultPending, updatedItem.FinalResult)
	assert.Equal(t, 2, updatedItem.CurrentRoundNo)
	assert.Empty(t, updatedItem.LatestComment)

	var reviewer model.CaseReviewItemReviewer
	require.NoError(t, env.db.Where("review_item_id = ?", item.ID).First(&reviewer).Error)
	assert.Equal(t, model.ReviewerStatusPending, reviewer.ReviewStatus)
	assert.Empty(t, reviewer.LatestResult)
	assert.Empty(t, reviewer.LatestComment)

	var updatedReview model.CaseReview
	require.NoError(t, env.db.First(&updatedReview, review.ID).Error)
	assert.Equal(t, model.ReviewPlanStatusInProgress, updatedReview.Status)
	assert.Equal(t, 1, updatedReview.PendingCount)

	var updatedCase model.TestCase
	require.NoError(t, env.db.First(&updatedCase, env.testcaseID).Error)
	assert.Equal(t, model.TestCaseStatusPending, updatedCase.Status)
	assert.Equal(t, model.CaseReviewResultResubmit, updatedCase.ReviewResult)
}

// TestCaseReviewService_BatchResubmit_RejectsApprovedItem
// 已通过用例不能被普通重新提审拉回待审，避免破坏最终通过结论。
func TestCaseReviewService_BatchResubmit_RejectsApprovedItem(t *testing.T) {
	env := newSelfReviewEnv(t)

	review := model.CaseReview{
		ID:         210,
		ProjectID:  env.projectID,
		Name:       "通过用例不可重提",
		ReviewMode: model.ReviewModeSingle,
		Status:     model.ReviewPlanStatusCompleted,
		CreatedBy:  env.adminID,
		UpdatedBy:  env.adminID,
	}
	require.NoError(t, env.db.Create(&review).Error)
	item := model.CaseReviewItem{
		ID:             211,
		ReviewID:       review.ID,
		ProjectID:      env.projectID,
		TestCaseID:     env.testcaseID,
		TitleSnapshot:  "自审测试用例",
		ReviewStatus:   model.ReviewItemStatusCompleted,
		FinalResult:    model.ReviewResultApproved,
		CurrentRoundNo: 1,
		CreatedBy:      env.adminID,
		UpdatedBy:      env.adminID,
	}
	require.NoError(t, env.db.Create(&item).Error)

	err := env.svc.BatchResubmit(env.ctx, env.projectID, review.ID, env.adminID, []uint{item.ID})
	require.Error(t, err)
	bizErr, ok := err.(*BizError)
	require.True(t, ok)
	assert.Equal(t, CodeReviewStatusInvalid, bizErr.NumericCode())
}

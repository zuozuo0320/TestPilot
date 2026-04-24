// case_review_submit_service_test.go — 评审提交服务单元测试
//
// 核心场景：验证"提交评审权限不依赖全局角色，而依赖是否为该评审项的指定评审人"。
// 该测试是 2026-04-22 修复的回归测试：当时 Handler 层错误地用 requireRole(Manager|Reviewer)
// 硬挡 tester，导致 tester 即使被指派为评审人也无法提交。修复后权限判定下沉到 Service 层。
package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"testpilot/internal/model"
	"testpilot/internal/repository"
)

// caseReviewTestEnv 评审提交测试环境
type caseReviewTestEnv struct {
	svc        *CaseReviewSubmitService
	reviewRepo repository.CaseReviewRepository
	db         *gorm.DB
	ctx        context.Context
}

// newCaseReviewSubmitEnv 构建带评审计划与用例的测试环境
// 默认种入：admin(1) / tester(2) 两个用户、一个项目、一个用例、一个处于 in_progress 的评审计划
func newCaseReviewSubmitEnv(t *testing.T, reviewMode string) caseReviewTestEnv {
	t.Helper()
	db := testDB(t)
	seedAdmin(t, db)
	seedTester(t, db)
	seedRoles(t, db)
	seedProject(t, db)

	// 创建一条测试用例（用于评审项的 testcase_id 外键关联）
	tc := model.TestCase{
		ID:        101,
		ProjectID: 1,
		Title:     "登录功能",
		CreatedBy: 1,
		Status:    "draft",
	}
	if err := db.Create(&tc).Error; err != nil {
		t.Fatalf("seed testcase: %v", err)
	}

	// 创建评审计划（in_progress 状态，reviewMode 可选 single/parallel）
	review := model.CaseReview{
		ID:         1,
		ProjectID:  1,
		Name:       "回归测试评审",
		ReviewMode: reviewMode,
		Status:     model.ReviewPlanStatusInProgress,
		CreatedBy:  1,
	}
	if err := db.Create(&review).Error; err != nil {
		t.Fatalf("seed review: %v", err)
	}

	// 创建评审项（对应 testcase 101）
	item := model.CaseReviewItem{
		ID:             10,
		ReviewID:       1,
		ProjectID:      1,
		TestCaseID:     101,
		CurrentRoundNo: 1,
		ReviewStatus:   model.ReviewItemStatusPending,
		FinalResult:    model.ReviewResultPending,
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("seed item: %v", err)
	}

	reviewRepo := repository.NewCaseReviewRepo(db)
	recordRepo := repository.NewCaseReviewRecordRepo(db)
	testCaseRepo := repository.NewTestCaseRepo(db)
	txMgr := repository.NewTxManager(db)

	svc := NewCaseReviewSubmitService(reviewRepo, recordRepo, testCaseRepo, txMgr, testLogger())

	return caseReviewTestEnv{
		svc:        svc,
		reviewRepo: reviewRepo,
		db:         db,
		ctx:        context.Background(),
	}
}

// assignReviewer 为评审项指派评审人（构造 item_reviewer 记录）
func (env *caseReviewTestEnv) assignReviewer(t *testing.T, itemID, userID uint) {
	t.Helper()
	r := model.CaseReviewItemReviewer{
		ReviewID:     1,
		ReviewItemID: itemID,
		ReviewerID:   userID,
		ReviewStatus: model.ReviewerStatusPending,
	}
	if err := env.db.Create(&r).Error; err != nil {
		t.Fatalf("assign reviewer: %v", err)
	}
}

// ─── 表驱动测试：权限相关场景 ───

// TestSubmitReview_PermissionMatrix 覆盖"提交评审权限"的核心场景：
// 只有被指派为本评审项评审人的用户可提交，且与全局角色无关。
// 这是 2026-04-22 修复的回归测试 —— 确保删除 Handler 的 requireRole 后，
// Service 层仍能正确拦截越权访问。
func TestSubmitReview_PermissionMatrix(t *testing.T) {
	type userSetup struct {
		userID      uint
		isReviewer  bool // 是否被指派为该 item 的评审人
		wantSuccess bool
		wantCode    int // 失败时期望的 BizError code
	}

	cases := []struct {
		name  string
		setup userSetup
	}{
		{
			name: "tester 作为指定评审人应成功",
			setup: userSetup{
				userID: 2, isReviewer: true, wantSuccess: true,
			},
		},
		{
			name: "tester 未被指派应被拒绝（CodeReviewForbidden）",
			setup: userSetup{
				userID: 2, isReviewer: false, wantSuccess: false, wantCode: CodeReviewForbidden,
			},
		},
		{
			name: "admin 未被指派也应被拒绝（不享受全局角色越权）",
			setup: userSetup{
				userID: 1, isReviewer: false, wantSuccess: false, wantCode: CodeReviewForbidden,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newCaseReviewSubmitEnv(t, model.ReviewModeSingle)
			if tc.setup.isReviewer {
				env.assignReviewer(t, 10, tc.setup.userID)
			}

			out, err := env.svc.SubmitReview(env.ctx, 1, 1, 10, tc.setup.userID, SubmitReviewInput{
				Result:  model.ReviewResultApproved,
				Comment: "lgtm",
			})

			if tc.setup.wantSuccess {
				require.NoError(t, err)
				assert.Equal(t, model.ReviewItemStatusCompleted, out.ReviewStatus)
				assert.Equal(t, model.ReviewResultApproved, out.FinalResult)
				return
			}
			require.Error(t, err)
			bizErr, ok := err.(*BizError)
			require.True(t, ok, "期望 BizError，实际得到 %T", err)
			assert.Equal(t, tc.setup.wantCode, bizErr.Code)
		})
	}
}

// TestSubmitReview_InvalidResult 校验非法评审结果应被参数校验拦截
func TestSubmitReview_InvalidResult(t *testing.T) {
	env := newCaseReviewSubmitEnv(t, model.ReviewModeSingle)
	env.assignReviewer(t, 10, 2) // tester 是评审人

	cases := []string{"", "unknown", "pending", "PASS"}
	for _, result := range cases {
		t.Run("result="+result, func(t *testing.T) {
			_, err := env.svc.SubmitReview(env.ctx, 1, 1, 10, 2, SubmitReviewInput{
				Result: result,
			})
			require.Error(t, err)
			bizErr, ok := err.(*BizError)
			require.True(t, ok)
			assert.Equal(t, CodeReviewStatusInvalid, bizErr.Code)
		})
	}
}

// TestSubmitReview_ClosedReview 已关闭的评审计划不允许再提交
func TestSubmitReview_ClosedReview(t *testing.T) {
	env := newCaseReviewSubmitEnv(t, model.ReviewModeSingle)
	env.assignReviewer(t, 10, 2)

	// 关闭评审计划
	if err := env.db.Model(&model.CaseReview{}).Where("id = ?", 1).
		Update("status", model.ReviewPlanStatusClosed).Error; err != nil {
		t.Fatalf("close review: %v", err)
	}

	_, err := env.svc.SubmitReview(env.ctx, 1, 1, 10, 2, SubmitReviewInput{
		Result: model.ReviewResultApproved,
	})
	require.Error(t, err)
	bizErr, ok := err.(*BizError)
	require.True(t, ok)
	assert.Equal(t, CodeReviewStatusInvalid, bizErr.Code)
}

// TestSubmitReview_NonexistentReview 评审计划不存在应返回 404
func TestSubmitReview_NonexistentReview(t *testing.T) {
	env := newCaseReviewSubmitEnv(t, model.ReviewModeSingle)
	env.assignReviewer(t, 10, 2)

	_, err := env.svc.SubmitReview(env.ctx, 1, 9999, 10, 2, SubmitReviewInput{
		Result: model.ReviewResultApproved,
	})
	require.Error(t, err)
	bizErr, ok := err.(*BizError)
	require.True(t, ok)
	assert.Equal(t, CodeReviewNotFound, bizErr.Code)
}

// TestSubmitReview_ItemNotBelongToReview 评审项不属于此计划应被拦截
func TestSubmitReview_ItemNotBelongToReview(t *testing.T) {
	env := newCaseReviewSubmitEnv(t, model.ReviewModeSingle)
	env.assignReviewer(t, 10, 2)

	// 再建一个评审计划（ID=2）
	extraReview := model.CaseReview{
		ID: 2, ProjectID: 1, Name: "另一个评审",
		ReviewMode: model.ReviewModeSingle,
		Status:     model.ReviewPlanStatusInProgress, CreatedBy: 1,
	}
	require.NoError(t, env.db.Create(&extraReview).Error)

	// 用 reviewID=2 但 itemID=10（item 10 属于 review 1）
	_, err := env.svc.SubmitReview(env.ctx, 1, 2, 10, 2, SubmitReviewInput{
		Result: model.ReviewResultApproved,
	})
	require.Error(t, err)
	bizErr, ok := err.(*BizError)
	require.True(t, ok)
	assert.Equal(t, CodeReviewItemNotFound, bizErr.Code)
}

// TestSubmitReview_UpdatesAggregatedResult 验证提交后 item 的聚合结果与状态已更新
func TestSubmitReview_UpdatesAggregatedResult(t *testing.T) {
	env := newCaseReviewSubmitEnv(t, model.ReviewModeSingle)
	env.assignReviewer(t, 10, 2)

	out, err := env.svc.SubmitReview(env.ctx, 1, 1, 10, 2, SubmitReviewInput{
		Result:  model.ReviewResultRejected,
		Comment: "用例描述不清晰",
	})
	require.NoError(t, err)

	assert.Equal(t, model.ReviewItemStatusCompleted, out.ReviewStatus)
	assert.Equal(t, model.ReviewResultRejected, out.FinalResult)
	assert.Equal(t, 1, out.CurrentRoundNo)

	// 校验 DB 中的 item 记录已同步
	var item model.CaseReviewItem
	require.NoError(t, env.db.First(&item, 10).Error)
	assert.Equal(t, model.ReviewItemStatusCompleted, item.ReviewStatus)
	assert.Equal(t, model.ReviewResultRejected, item.FinalResult)

	// 校验 reviewer 记录已被更新为 reviewed
	var reviewer model.CaseReviewItemReviewer
	require.NoError(t, env.db.Where("review_item_id = ? AND reviewer_id = ?", 10, 2).
		First(&reviewer).Error)
	assert.Equal(t, model.ReviewerStatusReviewed, reviewer.ReviewStatus)
	assert.Equal(t, model.ReviewResultRejected, reviewer.LatestResult)
}

// TestSubmitReview_ParallelMode_AllMustSubmit 多人评审：单人提交不应结束评审
func TestSubmitReview_ParallelMode_AllMustSubmit(t *testing.T) {
	env := newCaseReviewSubmitEnv(t, model.ReviewModeParallel)
	// 给 item 10 指派两位评审人
	env.assignReviewer(t, 10, 1) // admin
	env.assignReviewer(t, 10, 2) // tester

	// 仅 tester 先提交
	out, err := env.svc.SubmitReview(env.ctx, 1, 1, 10, 2, SubmitReviewInput{
		Result: model.ReviewResultApproved,
	})
	require.NoError(t, err)
	// 还有一位评审人未提交，item 应仍处于 pending（或 reviewing）而非 completed
	assert.NotEqual(t, model.ReviewItemStatusCompleted, out.ReviewStatus)
}

// ─── listUsersLookup 依赖：UserService.ListFiltered Status=active 过滤 ───

// TestUserService_ListFiltered_ActiveOnly 验证 listUsersLookup 依赖的 active 过滤逻辑：
// 禁用账号不应出现在可选用户列表（避免把离职用户作为评审人候选）。
func TestUserService_ListFiltered_ActiveOnly(t *testing.T) {
	db := testDB(t)
	userRepo, roleRepo, projectRepo, auditRepo, txMgr := testRepos(db)
	svc := NewUserService(userRepo, roleRepo, projectRepo, auditRepo, txMgr)

	// 种入：1 个 active admin + 1 个 active tester + 1 个 disabled 用户
	seedAdmin(t, db)
	seedTester(t, db)
	disabled := model.User{
		ID: 99, Name: "已离职", Email: "left@test.local",
		Role: model.GlobalRoleTester, Active: true, // 先建为 active，再显式 Update 为 false，避开 GORM default:true 覆盖
	}
	require.NoError(t, db.Create(&disabled).Error)
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", 99).Update("active", false).Error)

	users, err := svc.ListFiltered(context.Background(), repository.UserListFilter{
		Status: "active",
	})
	require.NoError(t, err)

	// 应只返回 2 个启用用户，禁用用户被过滤掉
	assert.Len(t, users, 2)
	for _, u := range users {
		assert.True(t, u.Active, "禁用用户不应出现在 active 过滤结果里: %s", u.Email)
		assert.NotEqual(t, uint(99), u.ID)
	}
}

// TestUserService_ListFiltered_Keyword 验证关键词模糊过滤
func TestUserService_ListFiltered_Keyword(t *testing.T) {
	db := testDB(t)
	userRepo, roleRepo, projectRepo, auditRepo, txMgr := testRepos(db)
	svc := NewUserService(userRepo, roleRepo, projectRepo, auditRepo, txMgr)
	seedAdmin(t, db)
	seedTester(t, db)

	// 按 email 搜索
	users, err := svc.ListFiltered(context.Background(), repository.UserListFilter{
		Keyword: "tester",
		Status:  "active",
	})
	require.NoError(t, err)
	assert.Len(t, users, 1)
	assert.Equal(t, "tester@test.local", users[0].Email)

	// 按姓名搜索
	users, err = svc.ListFiltered(context.Background(), repository.UserListFilter{
		Keyword: "Admin",
		Status:  "active",
	})
	require.NoError(t, err)
	assert.Len(t, users, 1)
	assert.Equal(t, "Admin", users[0].Name)
}

// ─── GetReviewSummary 汇总接口 ───

// TestGetReviewSummary 覆盖评审汇总接口的核心场景：
// - 按 status 分桶计数
// - "我待评审数"仅统计 active 计划内被指派给当前用户且 pending 的评审人记录
// - 关闭/已完成计划内的 pending 记录不应计入
func TestGetReviewSummary(t *testing.T) {
	env := newCaseReviewSubmitEnv(t, model.ReviewModeSingle)
	// env 已种入 review 1 (in_progress) + item 10

	// 种入其它状态的计划各一条
	extra := []model.CaseReview{
		{ID: 2, ProjectID: 1, Name: "未开始", ReviewMode: model.ReviewModeSingle, Status: model.ReviewPlanStatusNotStarted, CreatedBy: 1},
		{ID: 3, ProjectID: 1, Name: "已完成", ReviewMode: model.ReviewModeSingle, Status: model.ReviewPlanStatusCompleted, CreatedBy: 1},
		{ID: 4, ProjectID: 1, Name: "已关闭", ReviewMode: model.ReviewModeSingle, Status: model.ReviewPlanStatusClosed, CreatedBy: 1},
		{ID: 5, ProjectID: 1, Name: "进行中2", ReviewMode: model.ReviewModeSingle, Status: model.ReviewPlanStatusInProgress, CreatedBy: 1},
	}
	require.NoError(t, env.db.Create(&extra).Error)

	// 构造 "我待评审" 场景：
	// - review 1 / item 10 指派给 tester(2) pending  → 计入
	// - review 5 另建一 item，指派给 tester(2) pending → 计入
	// - review 3（已完成）的 item 指派给 tester(2) pending → 不计入
	// - review 4（已关闭）的 item 指派给 tester(2) pending → 不计入
	env.assignReviewer(t, 10, 2)

	activeItem := model.CaseReviewItem{
		ID: 20, ReviewID: 5, ProjectID: 1, TestCaseID: 101,
		CurrentRoundNo: 1, ReviewStatus: model.ReviewItemStatusPending, FinalResult: model.ReviewResultPending,
	}
	require.NoError(t, env.db.Create(&activeItem).Error)
	require.NoError(t, env.db.Create(&model.CaseReviewItemReviewer{
		ReviewID: 5, ReviewItemID: 20, ReviewerID: 2, ReviewStatus: model.ReviewerStatusPending,
	}).Error)

	completedItem := model.CaseReviewItem{
		ID: 30, ReviewID: 3, ProjectID: 1, TestCaseID: 101,
		CurrentRoundNo: 1, ReviewStatus: model.ReviewItemStatusCompleted, FinalResult: model.ReviewResultApproved,
	}
	require.NoError(t, env.db.Create(&completedItem).Error)
	require.NoError(t, env.db.Create(&model.CaseReviewItemReviewer{
		ReviewID: 3, ReviewItemID: 30, ReviewerID: 2, ReviewStatus: model.ReviewerStatusPending,
	}).Error)

	closedItem := model.CaseReviewItem{
		ID: 40, ReviewID: 4, ProjectID: 1, TestCaseID: 101,
		CurrentRoundNo: 1, ReviewStatus: model.ReviewItemStatusPending, FinalResult: model.ReviewResultPending,
	}
	require.NoError(t, env.db.Create(&closedItem).Error)
	require.NoError(t, env.db.Create(&model.CaseReviewItemReviewer{
		ReviewID: 4, ReviewItemID: 40, ReviewerID: 2, ReviewStatus: model.ReviewerStatusPending,
	}).Error)

	// 构造 service 实例
	txMgr := repository.NewTxManager(env.db)
	svc := NewCaseReviewService(
		env.reviewRepo,
		repository.NewCaseReviewRecordRepo(env.db),
		repository.NewTestCaseRepo(env.db),
		repository.NewUserRepo(env.db),
		repository.NewProjectRepo(env.db),
		repository.NewCaseReviewAttachmentRepo(env.db),
		txMgr,
		testLogger(),
	)

	summary, err := svc.GetReviewSummary(env.ctx, 1, 2)
	require.NoError(t, err)

	// 计划维度分桶
	assert.EqualValues(t, 5, summary.TotalPlans)
	assert.EqualValues(t, 1, summary.NotStartedPlans)
	assert.EqualValues(t, 2, summary.InProgressPlans) // review 1 + review 5
	assert.EqualValues(t, 1, summary.CompletedPlans)
	assert.EqualValues(t, 1, summary.ClosedPlans)

	// 我待评审：仅 review 1 / review 5 内的 pending 记录应计入（共 2 条）
	assert.EqualValues(t, 2, summary.MyPendingItems)
}

// TestGetReviewSummary_EmptyProject 空项目应返回全零但不报错
func TestGetReviewSummary_EmptyProject(t *testing.T) {
	db := testDB(t)
	seedAdmin(t, db)
	seedProject(t, db)
	reviewRepo := repository.NewCaseReviewRepo(db)
	txMgr := repository.NewTxManager(db)
	svc := NewCaseReviewService(
		reviewRepo,
		repository.NewCaseReviewRecordRepo(db),
		repository.NewTestCaseRepo(db),
		repository.NewUserRepo(db),
		repository.NewProjectRepo(db),
		repository.NewCaseReviewAttachmentRepo(db),
		txMgr,
		testLogger(),
	)

	summary, err := svc.GetReviewSummary(context.Background(), 1, 1)
	require.NoError(t, err)
	assert.EqualValues(t, 0, summary.TotalPlans)
	assert.EqualValues(t, 0, summary.MyPendingItems)
}

// ─── 为避免未使用变量告警（future-proof） ───
var _ = time.Now

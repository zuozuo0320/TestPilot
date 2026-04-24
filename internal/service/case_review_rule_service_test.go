// case_review_rule_service_test.go — Layer 1 规则引擎 Service 层单测
package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"testpilot/internal/model"
	"testpilot/internal/repository"
)

// ruleSvcEnv 组装 Rule + Defect Service 所需的依赖
type ruleSvcEnv struct {
	ctx           context.Context
	svc           *CaseReviewRuleService
	defectSvc     *CaseReviewDefectService
	reviewRepo    repository.CaseReviewRepository
	defectRepo    repository.CaseReviewDefectRepository
	testCaseRepo  repository.TestCaseRepository
	projectID     uint
	reviewID      uint
	itemID        uint
	testcaseID    uint
	authorUserID  uint
	primaryUserID uint
}

// newRuleSvcEnv 为每个测试准备一条完整的评审链路：
// Admin(ID=1) 作为 Primary、Tester(ID=2) 作为 Author（创建用例）。
// 可选参数 tcOverrides 用于构造不合规用例。
func newRuleSvcEnv(t *testing.T, tcOverrides ...func(*model.TestCase)) *ruleSvcEnv {
	t.Helper()
	db := testDB(t)
	seedAdmin(t, db)
	seedTester(t, db)
	seedProject(t, db)

	// 合规用例（默认全部字段齐全）
	tc := model.TestCase{
		ID:            100,
		ProjectID:     1,
		Title:         "登录成功用例",
		Precondition:  "用户已注册且账号正常",
		Steps:         "1. 打开登录页；2. 输入合法账号密码；3. 点击登录按钮",
		Postcondition: "停留在首页并展示欢迎词",
		Level:         "P1",
		Status:        model.TestCaseStatusPending,
		Version:       "V1",
		CreatedBy:     2, // Tester 是 Author
	}
	for _, fn := range tcOverrides {
		fn(&tc)
	}
	require.NoError(t, db.Create(&tc).Error)

	// 评审计划
	review := model.CaseReview{
		ID:          1,
		ProjectID:   1,
		Name:        "Rule Engine Test Plan",
		ReviewMode:  model.ReviewModeSingle,
		Status:      model.ReviewPlanStatusInProgress,
		ModeratorID: 1,
		AIEnabled:   true,
		CreatedBy:   1,
		UpdatedBy:   1,
	}
	require.NoError(t, db.Create(&review).Error)

	// 评审项（Primary=Admin）
	item := model.CaseReviewItem{
		ID:              10,
		ReviewID:        1,
		ProjectID:       1,
		TestCaseID:      tc.ID,
		TestCaseVersion: "V1",
		TitleSnapshot:   tc.Title,
		ReviewStatus:    model.ReviewItemStatusPending,
		FinalResult:     model.ReviewResultPending,
		CurrentRoundNo:  1,
		AIGateStatus:    model.AIGateStatusNotStarted,
		CreatedBy:       1,
		UpdatedBy:       1,
	}
	require.NoError(t, db.Create(&item).Error)
	require.NoError(t, db.Create(&model.CaseReviewItemReviewer{
		ReviewID:     1,
		ReviewItemID: 10,
		ProjectID:    1,
		ReviewerID:   1,
		ReviewStatus: model.ReviewerStatusPending,
		ReviewRole:   model.ReviewRolePrimary,
	}).Error)

	// 装配 repo + service
	txMgr := repository.NewTxManager(db)
	reviewRepo := repository.NewCaseReviewRepo(db)
	defectRepo := repository.NewCaseReviewDefectRepo(db)
	testCaseRepo := repository.NewTestCaseRepo(db)
	defectSvc := NewCaseReviewDefectService(defectRepo, reviewRepo, testCaseRepo, txMgr, testLogger())
	ruleSvc := NewCaseReviewRuleService(reviewRepo, testCaseRepo, defectRepo, defectSvc, txMgr, testLogger())

	return &ruleSvcEnv{
		ctx:           context.Background(),
		svc:           ruleSvc,
		defectSvc:     defectSvc,
		reviewRepo:    reviewRepo,
		defectRepo:    defectRepo,
		testCaseRepo:  testCaseRepo,
		projectID:     1,
		reviewID:      1,
		itemID:        10,
		testcaseID:    tc.ID,
		authorUserID:  2,
		primaryUserID: 1,
	}
}

// TestRuleService_Passed 完整用例应该通过规则引擎；不产生缺陷
func TestRuleService_Passed(t *testing.T) {
	env := newRuleSvcEnv(t)

	report, err := env.svc.RunOnItem(env.ctx, env.projectID, env.reviewID, env.itemID, env.primaryUserID)
	require.NoError(t, err)
	assert.True(t, report.Passed)
	assert.Equal(t, model.AIGateStatusPassed, report.AIGateStatus)
	assert.Empty(t, report.DefectIDs)

	// 校验 DB 真实落盘
	defects, err := env.defectRepo.ListByItemID(env.ctx, env.itemID)
	require.NoError(t, err)
	assert.Empty(t, defects)
}

// TestRuleService_Failed_TitleEmpty 空标题属 Critical，应产生缺陷
func TestRuleService_Failed_TitleEmpty(t *testing.T) {
	env := newRuleSvcEnv(t, func(tc *model.TestCase) {
		tc.Title = ""
	})

	report, err := env.svc.RunOnItem(env.ctx, env.projectID, env.reviewID, env.itemID, env.primaryUserID)
	require.NoError(t, err)
	assert.False(t, report.Passed)
	assert.Equal(t, model.AIGateStatusFailed, report.AIGateStatus)
	assert.GreaterOrEqual(t, report.CriticalCount, 1)
	assert.NotEmpty(t, report.DefectIDs, "失败时必须生成至少一个 Action Item")

	// 产生的缺陷 source 必须是 ai_gate
	defects, err := env.defectRepo.ListByItemID(env.ctx, env.itemID)
	require.NoError(t, err)
	require.NotEmpty(t, defects)
	for _, d := range defects {
		assert.Equal(t, model.DefectSourceAIGate, d.Source)
		assert.Equal(t, model.DefectStatusOpen, d.Status)
	}
}

// TestRuleService_Rerun_IdempotentReplacesOpenAIGateDefects
// Rerun 应覆盖旧的 ai_gate + open 缺陷（不增长），保持同规格产出
func TestRuleService_Rerun_IdempotentReplacesOpenAIGateDefects(t *testing.T) {
	env := newRuleSvcEnv(t, func(tc *model.TestCase) {
		tc.Steps = "" // Critical
		tc.Level = "" // Major
	})

	r1, err := env.svc.RunOnItem(env.ctx, env.projectID, env.reviewID, env.itemID, env.primaryUserID)
	require.NoError(t, err)
	assert.False(t, r1.Passed)
	firstCount := len(r1.DefectIDs)
	require.Greater(t, firstCount, 0)

	// 第二次 rerun：同样的用例 + 同样的规则，应得到同样数量（不重复累加）
	r2, err := env.svc.RunOnItem(env.ctx, env.projectID, env.reviewID, env.itemID, env.primaryUserID)
	require.NoError(t, err)
	assert.False(t, r2.Passed)
	assert.Equal(t, firstCount, len(r2.DefectIDs), "rerun 不应重复累加缺陷")

	defects, err := env.defectRepo.ListByItemID(env.ctx, env.itemID)
	require.NoError(t, err)
	assert.Len(t, defects, firstCount)
}

// TestRuleService_Rerun_PreservesResolvedAIGateDefects
// Author 已处理（resolved/disputed）的 ai_gate 缺陷不应被 rerun 清掉
func TestRuleService_Rerun_PreservesResolvedAIGateDefects(t *testing.T) {
	env := newRuleSvcEnv(t, func(tc *model.TestCase) {
		tc.Steps = ""
	})

	// 第一次 run：产生 open + ai_gate 缺陷
	r1, err := env.svc.RunOnItem(env.ctx, env.projectID, env.reviewID, env.itemID, env.primaryUserID)
	require.NoError(t, err)
	require.NotEmpty(t, r1.DefectIDs)

	// Author Resolve 其中一条
	err = env.defectSvc.Resolve(env.ctx, env.projectID, r1.DefectIDs[0], env.authorUserID, ResolveDefectInput{Note: "已补充"})
	require.NoError(t, err)

	// 第二次 rerun：resolved 的应保留
	_, err = env.svc.RunOnItem(env.ctx, env.projectID, env.reviewID, env.itemID, env.primaryUserID)
	require.NoError(t, err)
	defects, err := env.defectRepo.ListByItemID(env.ctx, env.itemID)
	require.NoError(t, err)

	var resolvedCount int
	for _, d := range defects {
		if d.Status == model.DefectStatusResolved {
			resolvedCount++
		}
	}
	assert.GreaterOrEqual(t, resolvedCount, 1, "resolved 状态的缺陷不应被 rerun 清除")
}

// TestRuleService_Rerun_SkipsDuplicateOfDisputed
// 回归 2026-04-23 UI bug：当同规则已有 disputed 历史时，rerun 不应再生成一条 open 同 title 新条，
// 否则界面会同时出现「有异议」+「待处理」两条重复 Action Item。
func TestRuleService_Rerun_SkipsDuplicateOfDisputed(t *testing.T) {
	env := newRuleSvcEnv(t, func(tc *model.TestCase) {
		tc.Steps = "" // 触发 RuleStepsRequired / Critical
	})

	// 第一次 run 产生 1 条 open
	r1, err := env.svc.RunOnItem(env.ctx, env.projectID, env.reviewID, env.itemID, env.primaryUserID)
	require.NoError(t, err)
	require.Len(t, r1.DefectIDs, 1)

	// Author 对这条提异议 → 变 disputed
	err = env.defectSvc.Dispute(env.ctx, env.projectID, r1.DefectIDs[0], env.authorUserID, DisputeDefectInput{Reason: "设计上步骤允许空"})
	require.NoError(t, err)

	// 第二次 rerun：Steps 仍然空，会再次触发相同规则
	r2, err := env.svc.RunOnItem(env.ctx, env.projectID, env.reviewID, env.itemID, env.primaryUserID)
	require.NoError(t, err)
	require.Empty(t, r2.DefectIDs, "已有 disputed 同 title 时，rerun 不应再产生 open 新条")

	// 总体 defect 数仍为 1（disputed 那条保留）
	defects, err := env.defectRepo.ListByItemID(env.ctx, env.itemID)
	require.NoError(t, err)
	require.Len(t, defects, 1)
	assert.Equal(t, model.DefectStatusDisputed, defects[0].Status)
}

// TestRuleService_ProjectIsolation 跨项目的 itemID 必须被拒绝
func TestRuleService_ProjectIsolation(t *testing.T) {
	env := newRuleSvcEnv(t)

	// 用错的 projectID 调用
	_, err := env.svc.RunOnItem(env.ctx, 999, env.reviewID, env.itemID, env.primaryUserID)
	require.Error(t, err)
}

// TestRuleService_ReviewMismatch 评审项不属于指定计划时应拒绝
func TestRuleService_ReviewMismatch(t *testing.T) {
	env := newRuleSvcEnv(t)

	// 用错的 reviewID
	_, err := env.svc.RunOnItem(env.ctx, env.projectID, 999, env.itemID, env.primaryUserID)
	require.Error(t, err)
}

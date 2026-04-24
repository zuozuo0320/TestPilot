// case_review_rule_service.go — v0.2 Layer 1 规则引擎的 Service 包装
//
// 职责：
//   - 把 reviewrule.Evaluate 的纯函数结果持久化为 case_review_items.ai_gate_status
//   - 规则未通过时，为每个 critical/major finding 调用 DefectService 生成 Action Item
//   - 同一评审项重复 Rerun 会幂等地刷新门禁状态并覆盖旧的 AI 门禁缺陷
//
// 设计要点：
//   - Layer 1 规则是纯函数 O(1) 耗时，无需异步；直接同步返回结果即可
//   - Minor 等级不阻断主状态机，只会作为提示写在 Finding 列表里
//   - 本服务不关心 Primary/Shadow 角色；那是 CaseReviewSubmitService 的职责
package service

import (
	"context"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"testpilot/internal/model"
	"testpilot/internal/repository"
	"testpilot/internal/service/reviewrule"
)

// CaseReviewRuleService 规则引擎 Service 包装。
//
// 为什么不把规则执行放进 CaseReviewService：
//   - 规则执行是典型的 side-effect 流程（写 DB + 产 Defect），单独抽一层更好做 Mock
//   - Phase 2 接入 Layer 2 LLM 时，这个 Service 会被复用作为"门禁聚合器"
type CaseReviewRuleService struct {
	reviewRepo   repository.CaseReviewRepository
	testCaseRepo repository.TestCaseRepository
	defectRepo   repository.CaseReviewDefectRepository
	defectSvc    *CaseReviewDefectService
	txMgr        *repository.TxManager
	logger       *slog.Logger
}

// NewCaseReviewRuleService 构造 CaseReviewRuleService
func NewCaseReviewRuleService(
	reviewRepo repository.CaseReviewRepository,
	testCaseRepo repository.TestCaseRepository,
	defectRepo repository.CaseReviewDefectRepository,
	defectSvc *CaseReviewDefectService,
	txMgr *repository.TxManager,
	logger *slog.Logger,
) *CaseReviewRuleService {
	return &CaseReviewRuleService{
		reviewRepo:   reviewRepo,
		testCaseRepo: testCaseRepo,
		defectRepo:   defectRepo,
		defectSvc:    defectSvc,
		txMgr:        txMgr,
		logger:       logger.With("module", "case_review_rule"),
	}
}

// RunReport Handler 返回给前端的规则运行报告
type RunReport struct {
	ItemID        uint                 `json:"item_id"`
	AIGateStatus  string               `json:"ai_gate_status"`
	Passed        bool                 `json:"passed"`
	Findings      []reviewrule.Finding `json:"findings"`
	CriticalCount int                  `json:"critical_count"`
	MajorCount    int                  `json:"major_count"`
	MinorCount    int                  `json:"minor_count"`
	DefectIDs     []uint               `json:"defect_ids"`
	RunAt         time.Time            `json:"run_at"`
}

// PlanItemSummary 计划级 AI 评审报告里的单条评审项摘要
type PlanItemSummary struct {
	ItemID        uint   `json:"item_id"`
	TestCaseID    uint   `json:"testcase_id"`
	TitleSnapshot string `json:"title_snapshot"`
	AIGateStatus  string `json:"ai_gate_status"`
	Passed        bool   `json:"passed"`
	CriticalCount int    `json:"critical_count"`
	MajorCount    int    `json:"major_count"`
	MinorCount    int    `json:"minor_count"`
	DefectCount   int    `json:"defect_count"`
	// Error 非空表示该条 item 的规则执行失败（不影响整体流程，但需要前端展示）
	Error string `json:"error,omitempty"`
}

// PlanRunReport 计划级 AI 评审聚合报告（run-all 的响应体）
type PlanRunReport struct {
	ReviewID    uint              `json:"review_id"`
	TotalCount  int               `json:"total_count"`
	PassedCount int               `json:"passed_count"`
	FailedCount int               `json:"failed_count"`
	ErrorCount  int               `json:"error_count"`
	Items       []PlanItemSummary `json:"items"`
	RunAt       time.Time         `json:"run_at"`
}

// RunOnReview 对某个评审计划下**所有**评审项批量执行 Layer 1 规则引擎。
// 顺序：按评审项 sort_order / id 升序；单条失败不阻断后续条目。
// 返回的 PlanRunReport 里每条 item 都带上 title + 门禁状态 + finding 统计，
// 前端可以直接按此聚合视图呈现"AI 评审报告"。
func (s *CaseReviewRuleService) RunOnReview(ctx context.Context, projectID, reviewID, userID uint) (*PlanRunReport, error) {
	s.logger.Info("rule engine plan run start", "project_id", projectID, "review_id", reviewID, "user_id", userID)

	// 仅拉取最大 500 条评审项；Phase 1 够用，Phase 2 改为流式 / 队列化
	items, _, err := s.reviewRepo.ListItems(ctx, reviewID, projectID, repository.CaseReviewItemFilter{
		Page: 1, PageSize: 500,
	})
	if err != nil {
		return nil, err
	}

	report := &PlanRunReport{
		ReviewID:   reviewID,
		TotalCount: len(items),
		RunAt:      time.Now(),
		Items:      make([]PlanItemSummary, 0, len(items)),
	}

	for i := range items {
		item := &items[i]
		summary := PlanItemSummary{
			ItemID:        item.ID,
			TestCaseID:    item.TestCaseID,
			TitleSnapshot: item.TitleSnapshot,
		}
		r, runErr := s.RunOnItem(ctx, projectID, reviewID, item.ID, userID)
		if runErr != nil {
			summary.Error = runErr.Error()
			summary.AIGateStatus = item.AIGateStatus
			report.ErrorCount++
		} else {
			summary.AIGateStatus = r.AIGateStatus
			summary.Passed = r.Passed
			summary.CriticalCount = r.CriticalCount
			summary.MajorCount = r.MajorCount
			summary.MinorCount = r.MinorCount
			summary.DefectCount = len(r.DefectIDs)
			if r.Passed {
				report.PassedCount++
			} else {
				report.FailedCount++
			}
		}
		report.Items = append(report.Items, summary)
	}

	s.logger.Info("rule engine plan run done",
		"review_id", reviewID,
		"total", report.TotalCount,
		"passed", report.PassedCount,
		"failed", report.FailedCount,
		"error", report.ErrorCount,
	)
	return report, nil
}

// RunOnItem 对单个评审项执行 Layer 1 规则引擎：
//   - 读取 item + testcase 快照
//   - 同步运行 reviewrule.Evaluate
//   - 事务内更新 item.ai_gate_status；failed 时覆盖创建 AI 门禁缺陷
//   - 返回 RunReport 供 Handler 直接序列化为响应体
//
// userID 为触发者（手动 rerun 的用户，或 Phase 2 系统 worker 的系统用户 ID），
// 最终写入新生成 defect 的 created_by，便于审计。
func (s *CaseReviewRuleService) RunOnItem(ctx context.Context, projectID, reviewID, itemID, userID uint) (*RunReport, error) {
	s.logger.Info("rule engine run start", "project_id", projectID, "review_id", reviewID, "item_id", itemID, "user_id", userID)

	item, err := s.reviewRepo.GetItemByID(ctx, nil, itemID)
	if err != nil {
		return nil, ErrNotFound(CodeReviewItemNotFound, "评审项不存在")
	}
	if item.ProjectID != projectID || item.ReviewID != reviewID {
		return nil, ErrBadRequest(CodeReviewItemMismatch, "评审项不属于当前评审计划")
	}

	tc, err := s.testCaseRepo.FindByID(ctx, item.TestCaseID, projectID)
	if err != nil {
		return nil, ErrNotFound(CodeTestCaseNotFound, "测试用例不存在")
	}

	report := reviewrule.Evaluate(tc)
	now := time.Now()

	// 先落 running 再写最终状态，可以让前端看到短暂的 running 态；
	// 但 Layer 1 是 O(1) 纯函数，实际没必要；直接一次性写终态即可。
	gateStatus := model.AIGateStatusPassed
	if !report.Passed {
		gateStatus = model.AIGateStatusFailed
	}

	var defectIDs []uint
	err = s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		// 1) 先查出**非 open** 的 AI 门禁历史缺陷 title 集合。
		// 这些缺陷（disputed / resolved）代表 Author 已处理或正在走异议流程，
		// rerun 不应再为同规则复制一条 open 新条导致 UI 重复展示。
		existing, lerr := s.defectRepo.ListByItemID(ctx, itemID)
		if lerr != nil {
			return lerr
		}
		preservedTitles := make(map[string]struct{}, len(existing))
		for _, d := range existing {
			if d.Source == model.DefectSourceAIGate && d.Status != model.DefectStatusOpen {
				preservedTitles[d.Title] = struct{}{}
			}
		}

		// 2) 覆盖旧的 AI 门禁缺陷（只删 open；disputed/resolved 保留）
		if err := s.defectRepo.DeleteAIGateOpenByItem(ctx, tx, itemID); err != nil {
			return err
		}

		// 3) 写 item.ai_gate_status
		if err := s.reviewRepo.UpdateItem(ctx, tx, item, map[string]any{
			"ai_gate_status": gateStatus,
		}); err != nil {
			return err
		}

		// 4) 未通过则为每个 critical/major finding 创建缺陷，
		// 但跳过已有 disputed/resolved 同 title 的规则，避免重复堆积。
		if !report.Passed {
			for _, f := range report.Findings {
				// Minor 不产生 Action Item，只作为提示返回给前端
				if f.Severity == model.ReviewSeverityMinor {
					continue
				}
				title := f.Rule
				if f.Message != "" {
					title = f.Rule + "：" + f.Message
				}
				if _, dup := preservedTitles[title]; dup {
					s.logger.Debug("skip duplicate ai_gate defect (has non-open history)",
						"item_id", itemID, "title", title)
					continue
				}
				defect, cerr := s.defectSvc.CreateAIGateDefect(ctx, tx, CreateAIGateDefectInput{
					ReviewID:     item.ReviewID,
					ReviewItemID: item.ID,
					ProjectID:    projectID,
					Severity:     f.Severity,
					Title:        title,
					CreatedBy:    userID,
				})
				if cerr != nil {
					return cerr
				}
				defectIDs = append(defectIDs, defect.ID)
			}
		}
		return nil
	})
	if err != nil {
		s.logger.Error("rule engine run failed", "item_id", itemID, "error", err)
		return nil, err
	}

	s.logger.Info("rule engine run success",
		"item_id", itemID,
		"gate_status", gateStatus,
		"critical", report.CriticalCount,
		"major", report.MajorCount,
		"minor", report.MinorCount,
		"defect_count", len(defectIDs),
	)
	return &RunReport{
		ItemID:        itemID,
		AIGateStatus:  gateStatus,
		Passed:        report.Passed,
		Findings:      report.Findings,
		CriticalCount: report.CriticalCount,
		MajorCount:    report.MajorCount,
		MinorCount:    report.MinorCount,
		DefectIDs:     defectIDs,
		RunAt:         now,
	}, nil
}

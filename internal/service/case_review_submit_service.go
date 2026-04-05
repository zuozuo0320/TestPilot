// case_review_submit_service.go — 用例评审提交服务（单条 + 批量评审提交事务）
package service

import (
	"context"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"testpilot/internal/model"
	"testpilot/internal/repository"
)

// SubmitReviewInput 单条评审提交输入
type SubmitReviewInput struct {
	Result  string // approved / rejected / needs_update
	Comment string
}

// SubmitReviewOutput 单条评审提交输出
type SubmitReviewOutput struct {
	ItemID             uint   `json:"item_id"`
	ReviewStatus       string `json:"review_status"`
	FinalResult        string `json:"final_result"`
	CurrentRoundNo     int    `json:"current_round_no"`
	TestCaseStatus     string `json:"testcase_status"`
	TestCaseReviewResult string `json:"testcase_review_result"`
	NextPendingItemID  *uint  `json:"next_pending_item_id"`
}

// BatchReviewInput 批量评审输入
type BatchReviewInput struct {
	ItemIDs []uint
	Result  string
	Comment string
}

// BatchReviewOutput 批量评审输出
type BatchReviewOutput struct {
	SuccessCount int      `json:"success_count"`
	FailCount    int      `json:"fail_count"`
	FailReasons  []string `json:"fail_reasons,omitempty"`
}

// CaseReviewSubmitService 评审提交服务
type CaseReviewSubmitService struct {
	reviewRepo   repository.CaseReviewRepository
	recordRepo   repository.CaseReviewRecordRepository
	testCaseRepo repository.TestCaseRepository
	txMgr        *repository.TxManager
	logger       *slog.Logger
}

// NewCaseReviewSubmitService 创建评审提交服务
func NewCaseReviewSubmitService(
	reviewRepo repository.CaseReviewRepository,
	recordRepo repository.CaseReviewRecordRepository,
	testCaseRepo repository.TestCaseRepository,
	txMgr *repository.TxManager,
	logger *slog.Logger,
) *CaseReviewSubmitService {
	return &CaseReviewSubmitService{
		reviewRepo:   reviewRepo,
		recordRepo:   recordRepo,
		testCaseRepo: testCaseRepo,
		txMgr:        txMgr,
		logger:       logger,
	}
}


// SubmitReview 单条评审提交（核心事务）
func (s *CaseReviewSubmitService) SubmitReview(ctx context.Context, projectID, reviewID, itemID, userID uint, input SubmitReviewInput) (*SubmitReviewOutput, error) {
	s.logger.Info("submit review start", "project_id", projectID, "review_id", reviewID, "item_id", itemID, "user_id", userID, "result", input.Result)

	// 校验评审结果
	if !isValidReviewResult(input.Result) {
		return nil, ErrBadRequest(CodeReviewStatusInvalid, "评审结果必须为 approved/rejected/needs_update")
	}

	// 获取评审计划
	review, err := s.reviewRepo.GetReviewByID(ctx, reviewID, projectID)
	if err != nil {
		return nil, ErrNotFound(CodeReviewNotFound, "评审计划不存在")
	}
	if review.Status == model.ReviewPlanStatusClosed {
		return nil, ErrBadRequest(CodeReviewStatusInvalid, "评审计划已关闭")
	}

	output := &SubmitReviewOutput{}

	err = s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		// 1. 获取评审项
		item, err := s.reviewRepo.GetItemByID(ctx, tx, itemID)
		if err != nil {
			return ErrNotFound(CodeReviewItemNotFound, "评审项不存在")
		}
		if item.ReviewID != reviewID {
			return ErrBadRequest(CodeReviewItemNotFound, "评审项不属于此计划")
		}

		// 2. 校验是否为该 item 的评审人
		reviewer, err := s.reviewRepo.GetReviewer(ctx, tx, itemID, userID)
		if err != nil {
			return ErrBadRequest(CodeReviewForbidden, "当前用户不是该评审项的指定评审人")
		}

		// 3. 写入评审记录
		now := time.Now()
		record := &model.CaseReviewRecord{
			ReviewID:     reviewID,
			ReviewItemID: itemID,
			ProjectID:    projectID,
			TestCaseID:   item.TestCaseID,
			ReviewerID:   userID,
			RoundNo:      item.CurrentRoundNo,
			Result:       input.Result,
			Comment:      input.Comment,
		}
		// AggregateResultAfterSubmit 在聚合后填充

		// 4. 更新评审人状态
		if err := s.reviewRepo.UpdateReviewer(ctx, tx, reviewer, map[string]any{
			"review_status":  model.ReviewerStatusReviewed,
			"latest_result":  input.Result,
			"latest_comment": input.Comment,
			"reviewed_at":    now,
		}); err != nil {
			return err
		}

		// 5. 聚合该 item 的最终结果
		aggregatedResult, reviewStatus, err := s.aggregateItemResult(ctx, tx, item.ID, review.ReviewMode)
		if err != nil {
			return err
		}
		s.logger.Info("aggregated item result", "item_id", itemID, "result", aggregatedResult, "status", reviewStatus)


		// 填充记录的聚合结果快照
		record.AggregateResultAfterSubmit = aggregatedResult
		if err := s.recordRepo.Create(ctx, tx, record); err != nil {
			return err
		}

		// 6. 更新评审项
		itemFields := map[string]any{
			"review_status":  reviewStatus,
			"final_result":   aggregatedResult,
			"latest_comment": input.Comment,
			"updated_by":     userID,
		}
		if reviewStatus == model.ReviewItemStatusCompleted {
			itemFields["reviewed_at"] = now
		}
		if err := s.reviewRepo.UpdateItem(ctx, tx, item, itemFields); err != nil {
			return err
		}

		// 7. 如已形成最终结果，回写主表
		tcStatus, tcReviewResult := mapResultToTestCase(aggregatedResult)
		if reviewStatus == model.ReviewItemStatusCompleted {
			if err := s.writebackTestCase(ctx, tx, item.TestCaseID, projectID, tcStatus, tcReviewResult); err != nil {
				return err
			}
		}

		// 8. 重算计划统计
		if err := s.reviewRepo.RecalcReviewStats(ctx, tx, reviewID); err != nil {
			return err
		}

		// 构建输出
		output.ItemID = itemID
		output.ReviewStatus = reviewStatus
		output.FinalResult = aggregatedResult
		output.CurrentRoundNo = item.CurrentRoundNo
		output.TestCaseStatus = tcStatus
		output.TestCaseReviewResult = tcReviewResult

		// 9. 查找下一条待评审项
		nextItem, err := s.reviewRepo.FindNextPendingItem(ctx, reviewID, itemID)
		if err == nil && nextItem != nil {
			output.NextPendingItemID = &nextItem.ID
		}

		return nil
	})

	if err != nil {
		s.logger.Error("submit review failed", "item_id", itemID, "error", err)
		return nil, err
	}
	s.logger.Info("submit review success", "item_id", itemID, "review_status", output.ReviewStatus, "final_result", output.FinalResult)
	return output, nil
}

// BatchReview 批量评审
func (s *CaseReviewSubmitService) BatchReview(ctx context.Context, projectID, reviewID, userID uint, input BatchReviewInput) (*BatchReviewOutput, error) {
	output := &BatchReviewOutput{}
	for _, itemID := range input.ItemIDs {
		_, err := s.SubmitReview(ctx, projectID, reviewID, itemID, userID, SubmitReviewInput{
			Result:  input.Result,
			Comment: input.Comment,
		})
		if err != nil {
			output.FailCount++
			output.FailReasons = append(output.FailReasons, err.Error())
		} else {
			output.SuccessCount++
		}
	}
	return output, nil
}

// ListItemRecords 获取评审记录
func (s *CaseReviewSubmitService) ListItemRecords(ctx context.Context, itemID uint, roundNo *int, page, pageSize int) ([]model.CaseReviewRecord, int64, error) {
	return s.recordRepo.ListByItemID(ctx, itemID, roundNo, page, pageSize)
}

// ─── 聚合规则 ───

// aggregateItemResult 根据评审模式聚合评审项最终结果
func (s *CaseReviewSubmitService) aggregateItemResult(ctx context.Context, tx *gorm.DB, itemID uint, reviewMode string) (finalResult, reviewStatus string, err error) {
	reviewers, err := s.reviewRepo.ListReviewersByItemID(ctx, tx, itemID)
	if err != nil {
		return "", "", err
	}

	switch reviewMode {
	case model.ReviewModeSingle:
		return s.aggregateSingleMode(reviewers)
	case model.ReviewModeParallel:
		return s.aggregateParallelMode(reviewers)
	default:
		return s.aggregateSingleMode(reviewers)
	}
}

// aggregateSingleMode 单人评审聚合：最后一次有效提交作为最终结果
// 注意：reviewers 已按 reviewed_at DESC, id DESC 排序（见 ListReviewersByItemID）
func (s *CaseReviewSubmitService) aggregateSingleMode(reviewers []model.CaseReviewItemReviewer) (string, string, error) {
	// 取第一个已提交的评审人（= 最晚提交的）
	for _, r := range reviewers {
		if r.ReviewStatus == model.ReviewerStatusReviewed && r.LatestResult != "" {
			return r.LatestResult, model.ReviewItemStatusCompleted, nil
		}
	}
	return model.ReviewResultPending, model.ReviewItemStatusPending, nil
}

// aggregateParallelMode 多人评审聚合
func (s *CaseReviewSubmitService) aggregateParallelMode(reviewers []model.CaseReviewItemReviewer) (string, string, error) {
	pendingCount := 0
	hasRejected := false
	hasNeedsUpdate := false
	allApproved := true

	for _, r := range reviewers {
		if r.ReviewStatus == model.ReviewerStatusPending {
			pendingCount++
			allApproved = false
			continue
		}
		switch r.LatestResult {
		case model.ReviewResultRejected:
			hasRejected = true
			allApproved = false
		case model.ReviewResultNeedsUpdate:
			hasNeedsUpdate = true
			allApproved = false
		case model.ReviewResultApproved:
			// ok
		default:
			allApproved = false
		}
	}

	// 还有评审人未提交
	if pendingCount > 0 {
		// 即使有人已驳回，也需等所有人提交后再出最终结果（按需求文档）
		// 但如果已有 rejected，可提前标记为 reviewing
		if hasRejected || hasNeedsUpdate {
			return model.ReviewResultPending, model.ReviewItemStatusReviewing, nil
		}
		return model.ReviewResultPending, model.ReviewItemStatusReviewing, nil
	}

	// 所有评审人都已提交
	if hasRejected {
		return model.ReviewResultRejected, model.ReviewItemStatusCompleted, nil
	}
	if allApproved {
		return model.ReviewResultApproved, model.ReviewItemStatusCompleted, nil
	}
	if hasNeedsUpdate {
		return model.ReviewResultNeedsUpdate, model.ReviewItemStatusCompleted, nil
	}

	return model.ReviewResultPending, model.ReviewItemStatusPending, nil
}

// ─── 工具方法 ───

func isValidReviewResult(result string) bool {
	switch result {
	case model.ReviewResultApproved, model.ReviewResultRejected, model.ReviewResultNeedsUpdate:
		return true
	}
	return false
}

// mapResultToTestCase 将评审最终结果映射为用例主表字段
func mapResultToTestCase(finalResult string) (status, reviewResult string) {
	switch finalResult {
	case model.ReviewResultApproved:
		return model.TestCaseStatusActive, model.CaseReviewResultApproved
	case model.ReviewResultRejected:
		return model.TestCaseStatusDraft, model.CaseReviewResultRejected
	case model.ReviewResultNeedsUpdate:
		return model.TestCaseStatusDraft, model.CaseReviewResultNeedsUpdate
	default:
		return model.TestCaseStatusPending, model.CaseReviewResultPending
	}
}

// writebackTestCase 统一回写 test_cases 主表
func (s *CaseReviewSubmitService) writebackTestCase(ctx context.Context, tx *gorm.DB, testcaseID, projectID uint, status, reviewResult string) error {
	return tx.WithContext(ctx).
		Model(&model.TestCase{}).
		Where("id = ? AND project_id = ?", testcaseID, projectID).
		Updates(map[string]any{
			"status":        status,
			"review_result": reviewResult,
		}).Error
}

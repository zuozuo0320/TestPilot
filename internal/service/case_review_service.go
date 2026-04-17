// case_review_service.go — 用例评审计划管理服务
package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"testpilot/internal/model"
	"testpilot/internal/repository"
)

// CaseReviewService 评审计划管理服务
type CaseReviewService struct {
	reviewRepo     repository.CaseReviewRepository
	recordRepo     repository.CaseReviewRecordRepository
	testCaseRepo   repository.TestCaseRepository
	userRepo       repository.UserRepository
	attachmentRepo repository.CaseReviewAttachmentRepository
	txMgr          *repository.TxManager
	logger         *slog.Logger
}

// NewCaseReviewService 创建评审管理服务
func NewCaseReviewService(
	reviewRepo repository.CaseReviewRepository,
	recordRepo repository.CaseReviewRecordRepository,
	testCaseRepo repository.TestCaseRepository,
	userRepo repository.UserRepository,
	attachmentRepo repository.CaseReviewAttachmentRepository,
	txMgr *repository.TxManager,
	logger *slog.Logger,
) *CaseReviewService {
	return &CaseReviewService{
		reviewRepo:     reviewRepo,
		recordRepo:     recordRepo,
		testCaseRepo:   testCaseRepo,
		userRepo:       userRepo,
		attachmentRepo: attachmentRepo,
		txMgr:          txMgr,
		logger:         logger,
	}
}

// ─── Input 结构 ───

// CreateReviewInput 创建评审计划输入
type CreateReviewInput struct {
	Name               string
	ModuleID           uint
	ReviewMode         string
	DefaultReviewerIDs []uint
	PlannedStartAt     *time.Time
	PlannedEndAt       *time.Time
	Description        string
	TestCaseIDs        []uint
	AutoSubmit         bool
}

// UpdateReviewInput 更新评审计划输入
type UpdateReviewInput struct {
	Name               string
	ModuleID           *uint
	ReviewMode         string
	DefaultReviewerIDs []uint
	PlannedStartAt     *time.Time
	PlannedEndAt       *time.Time
	Description        string
}

// LinkItemsInput 关联用例输入
type LinkItemsInput struct {
	Items      []LinkItemEntry
	AutoSubmit bool
}

// LinkItemEntry 关联用例条目
type LinkItemEntry struct {
	TestCaseID  uint
	ReviewerIDs []uint
}

// CopyReviewInput 复制评审计划输入
type CopyReviewInput struct {
	Name           string
	IncludeCases   bool
	ResetReviewers bool
}

// ─── 评审计划 CRUD ───

// CreateReview 创建评审计划（事务：创建计划 + 关联用例 + 分配评审人 + 可选提审）
func (s *CaseReviewService) CreateReview(ctx context.Context, projectID, userID uint, input CreateReviewInput) (*model.CaseReview, error) {
	s.logger.Info("create review start", "project_id", projectID, "user_id", userID, "name", input.Name)

	if input.Name == "" {
		return nil, ErrBadRequest(CodeReviewMissingName, "评审计划名称不能为空")
	}
	if input.ReviewMode != model.ReviewModeSingle && input.ReviewMode != model.ReviewModeParallel {
		return nil, ErrBadRequest(CodeReviewStatusInvalid, "评审模式必须为 single 或 parallel")
	}
	if len(input.DefaultReviewerIDs) == 0 {
		return nil, ErrBadRequest(CodeReviewEmptyReviewer, "未配置评审人")
	}

	// 序列化默认评审人
	defReviewerJSON, _ := json.Marshal(input.DefaultReviewerIDs)

	var review model.CaseReview
	err := s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		review = model.CaseReview{
			ProjectID:          projectID,
			Name:               input.Name,
			ModuleID:           input.ModuleID,
			ReviewMode:         input.ReviewMode,
			Status:             model.ReviewPlanStatusNotStarted,
			Description:        input.Description,
			PlannedStartAt:     input.PlannedStartAt,
			PlannedEndAt:       input.PlannedEndAt,
			DefaultReviewerIDs: string(defReviewerJSON),
			CreatedBy:          userID,
			UpdatedBy:          userID,
		}
		if err := s.reviewRepo.CreateReview(ctx, tx, &review); err != nil {
			return err
		}

		// 关联用例
		if len(input.TestCaseIDs) > 0 {
			if err := s.linkItemsInternal(ctx, tx, review.ID, projectID, userID, input.DefaultReviewerIDs, input.TestCaseIDs, input.AutoSubmit); err != nil {
				return err
			}
			// 重算统计
			return s.reviewRepo.RecalcReviewStats(ctx, tx, review.ID)
		}
		return nil
	})
	if err != nil {
		s.logger.Error("create review failed", "error", err)
		return nil, err
	}
	s.logger.Info("create review success", "review_id", review.ID)
	return &review, nil
}

// GetReview 获取评审详情
func (s *CaseReviewService) GetReview(ctx context.Context, projectID, reviewID uint) (*model.CaseReview, error) {
	review, err := s.reviewRepo.GetReviewByID(ctx, reviewID, projectID)
	if err != nil {
		return nil, ErrNotFound(CodeReviewNotFound, "评审计划不存在")
	}
	s.fillReviewCreatorFields(ctx, review)
	return review, nil
}

// ListReviews 列表查询
func (s *CaseReviewService) ListReviews(ctx context.Context, projectID, currentUserID uint, f repository.CaseReviewFilter) ([]model.CaseReview, int64, error) {
	reviews, total, err := s.reviewRepo.ListReviews(ctx, projectID, currentUserID, f)
	if err != nil {
		return nil, 0, err
	}
	s.batchFillReviewCreatorFields(ctx, reviews)
	return reviews, total, nil
}

// UpdateReview 更新评审计划基本信息
func (s *CaseReviewService) UpdateReview(ctx context.Context, projectID, reviewID, userID uint, input UpdateReviewInput) error {
	s.logger.Info("update review start", "project_id", projectID, "review_id", reviewID, "user_id", userID)

	review, err := s.reviewRepo.GetReviewByID(ctx, reviewID, projectID)
	if err != nil {
		return ErrNotFound(CodeReviewNotFound, "评审计划不存在")
	}
	if review.Status == model.ReviewPlanStatusClosed {
		return ErrBadRequest(CodeReviewStatusInvalid, "已关闭的计划不允许编辑")
	}

	fields := map[string]any{"updated_by": userID}
	if input.Name != "" {
		fields["name"] = input.Name
	}
	if input.ModuleID != nil {
		fields["module_id"] = *input.ModuleID
	}
	if input.ReviewMode != "" {
		if review.Status == model.ReviewPlanStatusCompleted {
			return ErrBadRequest(CodeReviewStatusInvalid, "已完成的计划不允许修改评审模式")
		}
		fields["review_mode"] = input.ReviewMode
	}
	if input.Description != "" {
		fields["description"] = input.Description
	}
	if len(input.DefaultReviewerIDs) > 0 {
		defJSON, _ := json.Marshal(input.DefaultReviewerIDs)
		fields["default_reviewer_ids"] = string(defJSON)
	}
	if input.PlannedStartAt != nil {
		fields["planned_start_at"] = input.PlannedStartAt
	}
	if input.PlannedEndAt != nil {
		fields["planned_end_at"] = input.PlannedEndAt
	}
	err = s.reviewRepo.UpdateReview(ctx, nil, review, fields)
	if err != nil {
		s.logger.Error("update review failed", "review_id", reviewID, "error", err)
	} else {
		s.logger.Info("update review success", "review_id", reviewID)
	}
	return err
}

// DeleteReview 删除评审计划
func (s *CaseReviewService) DeleteReview(ctx context.Context, projectID, reviewID uint) error {
	s.logger.Warn("delete review request", "project_id", projectID, "review_id", reviewID)

	review, err := s.reviewRepo.GetReviewByID(ctx, reviewID, projectID)
	if err != nil {
		return ErrNotFound(CodeReviewNotFound, "评审计划不存在")
	}

	// 已关闭(closed) 或 未开始(not_started) 的计划可以直接删除
	// 进行中(in_progress) / 已完成(completed) 且有评审记录时需先关闭
	if review.Status != model.ReviewPlanStatusNotStarted && review.Status != model.ReviewPlanStatusClosed {
		hasRecords, _ := s.recordRepo.HasRecordsByReviewID(ctx, reviewID)
		if hasRecords {
			return ErrBadRequest(CodeReviewStatusInvalid, "进行中或已完成的计划有评审记录，请先关闭后再删除")
		}
	}

	err = s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		s.reviewRepo.DeleteReviewersByReviewID(ctx, tx, reviewID)
		s.reviewRepo.DeleteItemsByReviewID(ctx, tx, reviewID)
		s.recordRepo.DeleteByReviewID(ctx, tx, reviewID)
		if s.attachmentRepo != nil {
			if delErr := s.attachmentRepo.DeleteByReviewID(ctx, tx, reviewID); delErr != nil {
				return delErr
			}
		}
		return s.reviewRepo.DeleteReview(ctx, tx, reviewID, projectID)
	})
	if err != nil {
		s.logger.Error("delete review failed", "review_id", reviewID, "error", err)
	} else {
		s.logger.Info("delete review success", "review_id", reviewID)
	}
	return err
}

// CloseReview 关闭评审计划
func (s *CaseReviewService) CloseReview(ctx context.Context, projectID, reviewID, userID uint) error {
	review, err := s.reviewRepo.GetReviewByID(ctx, reviewID, projectID)
	if err != nil {
		return ErrNotFound(CodeReviewNotFound, "评审计划不存在")
	}
	if review.Status == model.ReviewPlanStatusClosed {
		return ErrBadRequest(CodeReviewStatusInvalid, "计划已经是关闭状态")
	}
	return s.reviewRepo.UpdateReview(ctx, nil, review, map[string]any{
		"status":     model.ReviewPlanStatusClosed,
		"updated_by": userID,
	})
}

// CopyReview 复制评审计划
func (s *CaseReviewService) CopyReview(ctx context.Context, projectID, reviewID, userID uint, input CopyReviewInput) (*model.CaseReview, error) {
	s.logger.Info("copy review start", "project_id", projectID, "from_review_id", reviewID, "user_id", userID)
	srcReview, err := s.reviewRepo.GetReviewByID(ctx, reviewID, projectID)
	if err != nil {
		return nil, ErrNotFound(CodeReviewNotFound, "源评审计划不存在")
	}

	name := input.Name
	if name == "" {
		name = srcReview.Name + "-复制"
	}

	var newReview model.CaseReview
	err = s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		newReview = model.CaseReview{
			ProjectID:          projectID,
			Name:               name,
			ModuleID:           srcReview.ModuleID,
			ReviewMode:         srcReview.ReviewMode,
			Status:             model.ReviewPlanStatusNotStarted,
			Description:        srcReview.Description,
			DefaultReviewerIDs: srcReview.DefaultReviewerIDs,
			PlannedStartAt:     srcReview.PlannedStartAt,
			PlannedEndAt:       srcReview.PlannedEndAt,
			CreatedBy:          userID,
			UpdatedBy:          userID,
		}
		if err := s.reviewRepo.CreateReview(ctx, tx, &newReview); err != nil {
			return err
		}

		if input.IncludeCases {
			// 获取源计划所有评审项
			srcItems, _, err := s.reviewRepo.ListItems(ctx, reviewID, projectID, repository.CaseReviewItemFilter{Page: 1, PageSize: 9999})
			if err != nil {
				return err
			}

			var newItems []model.CaseReviewItem
			for i, si := range srcItems {
				newItems = append(newItems, model.CaseReviewItem{
					ReviewID:        newReview.ID,
					ProjectID:       projectID,
					TestCaseID:      si.TestCaseID,
					TestCaseVersion: si.TestCaseVersion,
					ModuleID:        si.ModuleID,
					TitleSnapshot:   si.TitleSnapshot,
					ReviewStatus:    model.ReviewItemStatusPending,
					FinalResult:     model.ReviewResultPending,
					CurrentRoundNo:  1,
					SortOrder:       i,
					CreatedBy:       userID,
					UpdatedBy:       userID,
				})
			}
			if err := s.reviewRepo.CreateItems(ctx, tx, newItems); err != nil {
				return err
			}

			// 复制评审人
			if !input.ResetReviewers && len(newItems) > 0 {
				srcReviewers, err := s.reviewRepo.ListReviewersByReviewID(ctx, reviewID)
				if err != nil {
					return err
				}
				// 按 src_item_id -> testcase_id 建映射
				srcItemMap := make(map[uint]uint)
				for _, si := range srcItems {
					srcItemMap[si.ID] = si.TestCaseID
				}
				// CreateItems 后 GORM 已回填 ID 到 newItems，直接使用
				newCaseMap := make(map[uint]uint) // testcase_id -> new_item_id
				for _, ni := range newItems {
					newCaseMap[ni.TestCaseID] = ni.ID
				}
				var newReviewers []model.CaseReviewItemReviewer
				for _, sr := range srcReviewers {
					tcID := srcItemMap[sr.ReviewItemID]
					newItemID, ok := newCaseMap[tcID]
					if !ok {
						continue
					}
					newReviewers = append(newReviewers, model.CaseReviewItemReviewer{
						ReviewID:     newReview.ID,
						ReviewItemID: newItemID,
						ProjectID:    projectID,
						ReviewerID:   sr.ReviewerID,
						ReviewStatus: model.ReviewerStatusPending,
					})
				}
				if len(newReviewers) > 0 {
					if err := s.reviewRepo.CreateReviewers(ctx, tx, newReviewers); err != nil {
						return err
					}
				}
			}
			return s.reviewRepo.RecalcReviewStats(ctx, tx, newReview.ID)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &newReview, nil
}

// ─── 评审项管理 ───

// LinkItems 关联用例到评审计划
func (s *CaseReviewService) LinkItems(ctx context.Context, projectID, reviewID, userID uint, input LinkItemsInput) error {
	s.logger.Info("link items start", "project_id", projectID, "review_id", reviewID, "user_id", userID, "count", len(input.Items))
	review, err := s.reviewRepo.GetReviewByID(ctx, reviewID, projectID)
	if err != nil {
		return ErrNotFound(CodeReviewNotFound, "评审计划不存在")
	}
	if review.Status == model.ReviewPlanStatusClosed || review.Status == model.ReviewPlanStatusCompleted {
		return ErrBadRequest(CodeReviewStatusInvalid, "当前计划状态不允许关联用例")
	}

	reviewerMap := make(map[uint][]uint)
	for _, item := range input.Items {
		if len(item.ReviewerIDs) > 0 {
			reviewerMap[item.TestCaseID] = item.ReviewerIDs
		}
	}

	// [FIX #4] 从持久化的 default_reviewer_ids JSON 字段中获取默认评审人
	defaultReviewerIDs := review.ParseDefaultReviewerIDs()

	return s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		for _, entry := range input.Items {
			rIDs := reviewerMap[entry.TestCaseID]
			if len(rIDs) == 0 {
				rIDs = defaultReviewerIDs
			}
			if len(rIDs) == 0 {
				return ErrBadRequest(CodeReviewEmptyReviewer, "请先为评审计划配置默认评审人或为本次用例指定评审人")
			}
			if err := s.linkItemsInternal(ctx, tx, reviewID, projectID, userID, rIDs, []uint{entry.TestCaseID}, input.AutoSubmit); err != nil {
				return err
			}
		}
		return s.reviewRepo.RecalcReviewStats(ctx, tx, reviewID)
	})
}

// UnlinkItems 移除关联用例
func (s *CaseReviewService) UnlinkItems(ctx context.Context, projectID, reviewID uint, itemIDs []uint) error {
	s.logger.Warn("unlink items request", "project_id", projectID, "review_id", reviewID, "item_count", len(itemIDs))

	// 状态守卫：已关闭/已完成的计划不可修改
	review, err := s.reviewRepo.GetReviewByID(ctx, reviewID, projectID)
	if err != nil {
		return ErrNotFound(CodeReviewNotFound, "评审计划不存在")
	}
	if review.Status == model.ReviewPlanStatusClosed || review.Status == model.ReviewPlanStatusCompleted {
		return ErrBadRequest(CodeReviewStatusInvalid, "已关闭或已完成的评审计划不可修改")
	}

	// [FIX #2] 前置归属校验：确保 item 确实属于本 review+project
	if err := s.validateItemOwnership(ctx, nil, reviewID, projectID, itemIDs); err != nil {
		return err
	}

	// 检查是否有评审记录
	hasRecords, _ := s.recordRepo.HasRecordsByItemIDs(ctx, itemIDs)
	if hasRecords {
		return ErrBadRequest(CodeReviewStatusInvalid, "已有评审记录的用例不可移除")
	}

	return s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		s.reviewRepo.DeleteReviewersByItemIDs(ctx, tx, itemIDs)
		if s.attachmentRepo != nil {
			if err := s.attachmentRepo.DeleteByItemIDs(ctx, tx, itemIDs); err != nil {
				return err
			}
		}
		if err := s.reviewRepo.DeleteItems(ctx, tx, reviewID, itemIDs); err != nil {
			return err
		}
		return s.reviewRepo.RecalcReviewStats(ctx, tx, reviewID)
	})
}

// ListItems 获取评审项列表
func (s *CaseReviewService) ListItems(ctx context.Context, projectID, reviewID uint, f repository.CaseReviewItemFilter) ([]model.CaseReviewItem, int64, error) {
	items, total, err := s.reviewRepo.ListItems(ctx, reviewID, projectID, f)
	if err != nil {
		return nil, 0, err
	}
	// 填充评审人信息
	for i := range items {
		reviewers, _ := s.reviewRepo.ListReviewersByItemID(ctx, nil, items[i].ID)
		items[i].Reviewers = reviewers
	}
	return items, total, nil
}

// BatchReassign 批量修改评审人
func (s *CaseReviewService) BatchReassign(ctx context.Context, projectID, reviewID, userID uint, itemIDs []uint, reviewerIDs []uint) error {
	s.logger.Info("batch reassign start", "review_id", reviewID, "item_count", len(itemIDs), "reviewer_count", len(reviewerIDs))
	if len(reviewerIDs) == 0 {
		return ErrBadRequest(CodeReviewEmptyReviewer, "评审人不能为空")
	}

	// 状态守卫：已关闭/已完成的计划不可修改
	review, err := s.reviewRepo.GetReviewByID(ctx, reviewID, projectID)
	if err != nil {
		return ErrNotFound(CodeReviewNotFound, "评审计划不存在")
	}
	if review.Status == model.ReviewPlanStatusClosed || review.Status == model.ReviewPlanStatusCompleted {
		return ErrBadRequest(CodeReviewStatusInvalid, "已关闭或已完成的评审计划不可修改")
	}

	// [FIX #2] 前置归属校验
	if err = s.validateItemOwnership(ctx, nil, reviewID, projectID, itemIDs); err != nil {
		return err
	}

	return s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		// 删除旧评审人并重建
		if err := s.reviewRepo.DeleteReviewersByItemIDs(ctx, tx, itemIDs); err != nil {
			return err
		}
		var newReviewers []model.CaseReviewItemReviewer
		for _, itemID := range itemIDs {
			item, err := s.reviewRepo.GetItemByID(ctx, tx, itemID)
			if err != nil {
				return ErrNotFound(CodeReviewItemNotFound, "评审项不存在")
			}
			for _, rID := range reviewerIDs {
				newReviewers = append(newReviewers, model.CaseReviewItemReviewer{
					ReviewID:     reviewID,
					ReviewItemID: itemID,
					ProjectID:    projectID,
					ReviewerID:   rID,
					ReviewStatus: model.ReviewerStatusPending,
				})
			}
			// [FIX #5] 如果该评审项已完成，重置并回写主表
			if item.FinalResult != model.ReviewResultPending {
				if err := s.reviewRepo.UpdateItem(ctx, tx, item, map[string]any{
					"review_status":    model.ReviewItemStatusPending,
					"final_result":     model.ReviewResultPending,
					"current_round_no": item.CurrentRoundNo + 1,
					"reviewed_at":      nil,
					"latest_comment":   nil,
					"updated_by":       userID,
				}); err != nil {
					return err
				}
				// 回写 test_cases 主表为 pending
				if err := s.writebackTestCase(ctx, tx, item.TestCaseID, projectID, model.TestCaseStatusPending, model.CaseReviewResultPending); err != nil {
					return err
				}
			}
		}
		if err := s.reviewRepo.CreateReviewers(ctx, tx, newReviewers); err != nil {
			return err
		}
		// [FIX #5] 重算计划统计
		return s.reviewRepo.RecalcReviewStats(ctx, tx, reviewID)
	})
}

// BatchResubmit 批量重新提审
func (s *CaseReviewService) BatchResubmit(ctx context.Context, projectID, reviewID, userID uint, itemIDs []uint) error {
	s.logger.Info("batch resubmit start", "review_id", reviewID, "item_count", len(itemIDs))

	// 状态守卫：已关闭/已完成的计划不可修改
	review, err := s.reviewRepo.GetReviewByID(ctx, reviewID, projectID)
	if err != nil {
		return ErrNotFound(CodeReviewNotFound, "评审计划不存在")
	}
	if review.Status == model.ReviewPlanStatusClosed || review.Status == model.ReviewPlanStatusCompleted {
		return ErrBadRequest(CodeReviewStatusInvalid, "已关闭或已完成的评审计划不可修改")
	}

	// [FIX #2] 前置归属校验
	if err = s.validateItemOwnership(ctx, nil, reviewID, projectID, itemIDs); err != nil {
		return err
	}

	return s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		for _, itemID := range itemIDs {
			item, err := s.reviewRepo.GetItemByID(ctx, tx, itemID)
			if err != nil {
				continue
			}
			// 重置评审项
			if err := s.reviewRepo.UpdateItem(ctx, tx, item, map[string]any{
				"review_status":    model.ReviewItemStatusPending,
				"final_result":     model.ReviewResultPending,
				"current_round_no": item.CurrentRoundNo + 1,
				"reviewed_at":      nil,
				"latest_comment":   nil,
				"updated_by":       userID,
			}); err != nil {
				return err
			}
			// 重置评审人状态
			if err := s.reviewRepo.ResetReviewersByItemID(ctx, tx, itemID); err != nil {
				return err
			}
			// 回写主表
			if err := s.writebackTestCase(ctx, tx, item.TestCaseID, item.ProjectID, model.TestCaseStatusPending, model.CaseReviewResultResubmit); err != nil {
				return err
			}
		}
		return s.reviewRepo.RecalcReviewStats(ctx, tx, reviewID)
	})
}

// ─── 内部方法 ───

// linkItemsInternal 内部方法：批量关联用例到评审计划
func (s *CaseReviewService) linkItemsInternal(ctx context.Context, tx *gorm.DB, reviewID, projectID, userID uint, reviewerIDs, testcaseIDs []uint, autoSubmit bool) error {
	for _, tcID := range testcaseIDs {
		// 冲突检测
		conflict, _ := s.reviewRepo.HasActiveReviewForCase(ctx, projectID, tcID, reviewID)
		if conflict {
			return ErrBadRequest(CodeReviewStatusInvalid, "用例已存在进行中的评审计划")
		}

		// 获取用例信息
		tc, err := s.testCaseRepo.FindByID(ctx, tcID, projectID)
		if err != nil {
			return ErrNotFound(CodeReviewItemNotFound, "测试用例不存在")
		}

		// [FIX #1] 使用指针传入 Create，GORM 回填 ID 到同一变量
		item := &model.CaseReviewItem{
			ReviewID:        reviewID,
			ProjectID:       projectID,
			TestCaseID:      tcID,
			TestCaseVersion: tc.Version,
			ModuleID:        tc.ModuleID,
			TitleSnapshot:   tc.Title,
			ReviewStatus:    model.ReviewItemStatusPending,
			FinalResult:     model.ReviewResultPending,
			CurrentRoundNo:  1,
			CreatedBy:       userID,
			UpdatedBy:       userID,
		}
		if err := s.reviewRepo.CreateItem(ctx, tx, item); err != nil {
			return err
		}
		// item.ID 此时已被 GORM 回填为真实数据库 ID

		// 分配评审人
		var reviewers []model.CaseReviewItemReviewer
		for _, rID := range reviewerIDs {
			reviewers = append(reviewers, model.CaseReviewItemReviewer{
				ReviewID:     reviewID,
				ReviewItemID: item.ID,
				ProjectID:    projectID,
				ReviewerID:   rID,
				ReviewStatus: model.ReviewerStatusPending,
			})
		}
		if err := s.reviewRepo.CreateReviewers(ctx, tx, reviewers); err != nil {
			return err
		}

		// 可选自动提审
		if autoSubmit {
			if err := s.writebackTestCase(ctx, tx, tcID, projectID, model.TestCaseStatusPending, model.CaseReviewResultPending); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateItemOwnership [FIX #2] 校验 itemIDs 全部属于指定的 reviewID + projectID
func (s *CaseReviewService) validateItemOwnership(ctx context.Context, tx *gorm.DB, reviewID, projectID uint, itemIDs []uint) error {
	count, err := s.reviewRepo.CountItemsByOwnership(ctx, tx, reviewID, projectID, itemIDs)
	if err != nil {
		return err
	}
	if count != int64(len(itemIDs)) {
		return ErrBadRequest(CodeReviewItemMismatch, "部分评审项不属于当前评审计划")
	}
	return nil
}

// ValidateItemOwnership 公开方法：校验单个 itemID 归属（供 handler 层调用）
func (s *CaseReviewService) ValidateItemOwnership(ctx context.Context, reviewID, projectID, itemID uint) error {
	return s.validateItemOwnership(ctx, nil, reviewID, projectID, []uint{itemID})
}

// writebackTestCase 统一回写 test_cases 主表
func (s *CaseReviewService) writebackTestCase(ctx context.Context, tx *gorm.DB, testcaseID, projectID uint, status, reviewResult string) error {
	return tx.WithContext(ctx).
		Model(&model.TestCase{}).
		Where("id = ? AND project_id = ?", testcaseID, projectID).
		Updates(map[string]any{
			"status":        status,
			"review_result": reviewResult,
		}).Error
}

// fillReviewCreatorFields 回填单个评审计划的创建人姓名与头像。
// 评审列表和详情页都需要统一展示创建人信息，因此在 service 层集中补齐。
func (s *CaseReviewService) fillReviewCreatorFields(ctx context.Context, review *model.CaseReview) {
	if review == nil || review.CreatedBy == 0 {
		return
	}

	user, err := s.userRepo.FindByID(ctx, review.CreatedBy)
	if err != nil || user == nil {
		return
	}

	review.CreatedByName = user.Name
	review.CreatedByAvatar = user.Avatar
}

// batchFillReviewCreatorFields 批量回填评审计划创建人信息。
// 使用批量查用户避免列表页出现 N+1 查询。
func (s *CaseReviewService) batchFillReviewCreatorFields(ctx context.Context, reviews []model.CaseReview) {
	if len(reviews) == 0 {
		return
	}

	userIDs := make([]uint, 0, len(reviews))
	seen := make(map[uint]struct{}, len(reviews))
	for _, review := range reviews {
		if review.CreatedBy == 0 {
			continue
		}
		if _, exists := seen[review.CreatedBy]; exists {
			continue
		}
		seen[review.CreatedBy] = struct{}{}
		userIDs = append(userIDs, review.CreatedBy)
	}
	if len(userIDs) == 0 {
		return
	}

	users, err := s.userRepo.FindByIDs(ctx, userIDs)
	if err != nil {
		return
	}

	userMap := make(map[uint]model.User, len(users))
	for _, user := range users {
		userMap[user.ID] = user
	}

	for i := range reviews {
		user, ok := userMap[reviews[i].CreatedBy]
		if !ok {
			continue
		}
		reviews[i].CreatedByName = user.Name
		reviews[i].CreatedByAvatar = user.Avatar
	}
}

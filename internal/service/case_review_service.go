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
	projectRepo    repository.ProjectRepository // v0.2：读项目 settings.allow_self_review
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
	projectRepo repository.ProjectRepository,
	attachmentRepo repository.CaseReviewAttachmentRepository,
	txMgr *repository.TxManager,
	logger *slog.Logger,
) *CaseReviewService {
	return &CaseReviewService{
		reviewRepo:     reviewRepo,
		recordRepo:     recordRepo,
		testCaseRepo:   testCaseRepo,
		userRepo:       userRepo,
		projectRepo:    projectRepo,
		attachmentRepo: attachmentRepo,
		txMgr:          txMgr,
		logger:         logger.With("module", "case_review"),
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

	// v0.2 新增字段
	// ModeratorID Moderator 用户 ID；0 表示使用创建者作为默认。
	ModeratorID uint
	// AIEnabled AI 门禁总开关；nil 表示默认开启。
	AIEnabled *bool
	// DefaultPrimaryReviewerID 默认主评人。不传时取 DefaultReviewerIDs[0]（兼容）。
	DefaultPrimaryReviewerID uint
	// DefaultShadowReviewerIDs 默认陪审列表。不传时取 DefaultReviewerIDs[1:]（兼容）。
	DefaultShadowReviewerIDs []uint
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
	ReviewerIDs []uint // 兼容字段：小于 v0.2 时直接用，首元素当 Primary

	// v0.2 新增字段
	PrimaryReviewerID uint   // 本条目的主评人 ID，0 则回退 ReviewerIDs[0]
	ShadowReviewerIDs []uint // 本条目的陪审列表
}

// ReviewerAssignment v0.2 评审角色分配结果：同项唯一 Primary + 任意 Shadow
type ReviewerAssignment struct {
	PrimaryID uint
	ShadowIDs []uint
}

// AllReviewerIDs 返回 Primary+Shadow 全部评审人 ID（保留顺序）
func (a ReviewerAssignment) AllReviewerIDs() []uint {
	if a.PrimaryID == 0 && len(a.ShadowIDs) == 0 {
		return nil
	}
	out := make([]uint, 0, 1+len(a.ShadowIDs))
	out = append(out, a.PrimaryID)
	out = append(out, a.ShadowIDs...)
	return out
}

// CopyReviewInput 复制评审计划输入
type CopyReviewInput struct {
	Name           string
	IncludeCases   bool
	ResetReviewers bool
}

// ─── 评审计划 CRUD ───

// CreateReview 创建评审计划（事务：创建计划 + 关联用例 + 分配评审人 + 可选提审）
// v0.2：默认填充 Moderator=userID、AIEnabled=true；支持 Primary+Shadow 角色分配。
func (s *CaseReviewService) CreateReview(ctx context.Context, projectID, userID uint, input CreateReviewInput) (*model.CaseReview, error) {
	s.logger.Info("create review start", "project_id", projectID, "user_id", userID, "name", input.Name)

	if input.Name == "" {
		return nil, ErrBadRequest(CodeReviewMissingName, "评审计划名称不能为空")
	}
	if input.ReviewMode != model.ReviewModeSingle && input.ReviewMode != model.ReviewModeParallel {
		return nil, ErrBadRequest(CodeReviewStatusInvalid, "评审模式必须为 single 或 parallel")
	}

	// v0.2：派生默认 assignment。若 Primary/Shadow 显式给出则优先；否则回退到 DefaultReviewerIDs。
	defaultAssign, err := deriveAssignment(input.DefaultPrimaryReviewerID, input.DefaultShadowReviewerIDs, input.DefaultReviewerIDs)
	if err != nil {
		return nil, err
	}
	// 兼容：DefaultReviewerIDs 作为序列化字段继续保留；未传时也从 assignment 回填一份
	allReviewerIDs := input.DefaultReviewerIDs
	if len(allReviewerIDs) == 0 {
		allReviewerIDs = defaultAssign.AllReviewerIDs()
	}
	defReviewerJSON, _ := json.Marshal(allReviewerIDs)

	// v0.2：Moderator 与 AIEnabled 默认值
	moderatorID := input.ModeratorID
	if moderatorID == 0 {
		moderatorID = userID
	}
	aiEnabled := true
	if input.AIEnabled != nil {
		aiEnabled = *input.AIEnabled
	}

	// 读项目 settings，判断是否允许自审
	settings, err := s.loadProjectSettings(ctx, projectID)
	if err != nil {
		return nil, err
	}

	var review model.CaseReview
	err = s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
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
			ModeratorID:        moderatorID,
			AIEnabled:          aiEnabled,
			CreatedBy:          userID,
			UpdatedBy:          userID,
		}
		if err := s.reviewRepo.CreateReview(ctx, tx, &review); err != nil {
			return err
		}

		// 关联用例
		if len(input.TestCaseIDs) > 0 {
			if err := s.linkItemsInternal(ctx, tx, review.ID, projectID, userID, defaultAssign, input.TestCaseIDs, settings.AllowSelfReview, input.AutoSubmit); err != nil {
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
	s.logger.Info("create review success", "review_id", review.ID, "moderator_id", moderatorID, "ai_enabled", aiEnabled)
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

// ReviewSummary 评审计划项目维度汇总。
// 用于评审流程页顶部卡片以及"我评审的"徽标，字段均为项目级全局数字，
// 与分页列表无关。
type ReviewSummary struct {
	TotalPlans      int64 `json:"total_plans"`       // 全部计划（不含已软删除）
	NotStartedPlans int64 `json:"not_started_plans"` // 未开始
	InProgressPlans int64 `json:"in_progress_plans"` // 进行中
	CompletedPlans  int64 `json:"completed_plans"`   // 已完成
	ClosedPlans     int64 `json:"closed_plans"`      // 已关闭
	MyPendingItems  int64 `json:"my_pending_items"`  // 当前用户待评审项数
}

// GetReviewSummary 获取项目级评审汇总信息，供页面顶部卡片与徽标消费。
// 参数:
//   - ctx:       请求上下文
//   - projectID: 项目 ID（用于权限与范围过滤）
//   - userID:    当前登录用户 ID（用于计算"我待评审"口径）
func (s *CaseReviewService) GetReviewSummary(ctx context.Context, projectID, userID uint) (*ReviewSummary, error) {
	byStatus, err := s.reviewRepo.CountReviewsByStatus(ctx, projectID)
	if err != nil {
		return nil, err
	}
	myPending, err := s.reviewRepo.CountMyPendingReviewItems(ctx, projectID, userID)
	if err != nil {
		return nil, err
	}
	summary := &ReviewSummary{
		NotStartedPlans: byStatus[model.ReviewPlanStatusNotStarted],
		InProgressPlans: byStatus[model.ReviewPlanStatusInProgress],
		CompletedPlans:  byStatus[model.ReviewPlanStatusCompleted],
		ClosedPlans:     byStatus[model.ReviewPlanStatusClosed],
		MyPendingItems:  myPending,
	}
	summary.TotalPlans = summary.NotStartedPlans + summary.InProgressPlans + summary.CompletedPlans + summary.ClosedPlans
	return summary, nil
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
					role := sr.ReviewRole
					if role == "" {
						role = model.ReviewRolePrimary
					}
					newReviewers = append(newReviewers, model.CaseReviewItemReviewer{
						ReviewID:     newReview.ID,
						ReviewItemID: newItemID,
						ProjectID:    projectID,
						ReviewerID:   sr.ReviewerID,
						ReviewStatus: model.ReviewerStatusPending,
						ReviewRole:   role,
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
// v0.2：支持按条目指定 Primary/Shadow；未指定时回退到计划默认评审人。
func (s *CaseReviewService) LinkItems(ctx context.Context, projectID, reviewID, userID uint, input LinkItemsInput) error {
	s.logger.Info("link items start", "project_id", projectID, "review_id", reviewID, "user_id", userID, "count", len(input.Items))
	review, err := s.reviewRepo.GetReviewByID(ctx, reviewID, projectID)
	if err != nil {
		return ErrNotFound(CodeReviewNotFound, "评审计划不存在")
	}
	if review.Status == model.ReviewPlanStatusClosed || review.Status == model.ReviewPlanStatusCompleted {
		return ErrBadRequest(CodeReviewStatusInvalid, "当前计划状态不允许关联用例")
	}

	// 计划默认评审人：v0.2 把 DefaultReviewerIDs 首个作为 Primary，其余 Shadow
	defaultReviewerIDs := review.ParseDefaultReviewerIDs()
	defaultAssign, _ := deriveAssignment(0, nil, defaultReviewerIDs) // 缺失 Primary 时后面再拦截

	settings, err := s.loadProjectSettings(ctx, projectID)
	if err != nil {
		return err
	}

	return s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		for _, entry := range input.Items {
			assign, err := deriveAssignment(entry.PrimaryReviewerID, entry.ShadowReviewerIDs, entry.ReviewerIDs)
			if err != nil {
				// 条目未指定则尝试使用计划默认
				if defaultAssign.PrimaryID == 0 {
					return ErrBadRequest(CodeReviewEmptyReviewer, "请先为评审计划配置默认评审人或为本次用例指定评审人")
				}
				assign = defaultAssign
			}
			if err := s.linkItemsInternal(ctx, tx, reviewID, projectID, userID, assign, []uint{entry.TestCaseID}, settings.AllowSelfReview, input.AutoSubmit); err != nil {
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
// v0.2：对外签名不变；首元素自动视作 Primary，其余 Shadow；同时做自审校验。
func (s *CaseReviewService) BatchReassign(ctx context.Context, projectID, reviewID, userID uint, itemIDs []uint, reviewerIDs []uint) error {
	s.logger.Info("batch reassign start", "review_id", reviewID, "item_count", len(itemIDs), "reviewer_count", len(reviewerIDs))
	if len(reviewerIDs) == 0 {
		return ErrBadRequest(CodeReviewEmptyReviewer, "评审人不能为空")
	}

	// v0.2：派生 Primary/Shadow 角色
	assign, err := deriveAssignment(0, nil, reviewerIDs)
	if err != nil {
		return err
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

	// v0.2：读项目 settings 判断是否允许自审
	settings, err := s.loadProjectSettings(ctx, projectID)
	if err != nil {
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
			// v0.2 自审校验
			if !settings.AllowSelfReview {
				tc, tcErr := s.testCaseRepo.FindByID(ctx, item.TestCaseID, projectID)
				if tcErr == nil {
					if selfErr := ensureNoSelfReview(tc, assign); selfErr != nil {
						return selfErr
					}
				}
			}
			newReviewers = append(newReviewers, buildReviewerRows(reviewID, itemID, projectID, assign)...)
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
// v0.2：接受 ReviewerAssignment 明确 Primary/Shadow 角色；allowSelfReview 控制自审豁免。
func (s *CaseReviewService) linkItemsInternal(
	ctx context.Context,
	tx *gorm.DB,
	reviewID, projectID, userID uint,
	assign ReviewerAssignment,
	testcaseIDs []uint,
	allowSelfReview bool,
	autoSubmit bool,
) error {
	if assign.PrimaryID == 0 {
		return ErrBadRequest(CodeReviewPrimaryRequired, "必须指定唯一主评人")
	}
	for _, tcID := range testcaseIDs {
		// 冲突检测：注意必须传 tx，避免 SQLite 单写者死锁
		conflict, _ := s.reviewRepo.HasActiveReviewForCase(ctx, tx, projectID, tcID, reviewID)
		if conflict {
			return ErrBadRequest(CodeReviewStatusInvalid, "用例已存在进行中的评审计划")
		}

		// 获取用例信息
		tc, err := s.testCaseRepo.FindByID(ctx, tcID, projectID)
		if err != nil {
			return ErrNotFound(CodeReviewItemNotFound, "测试用例不存在")
		}

		// v0.2：自审校验 —— Author 不能出现在 Primary/Shadow 名单（除非 AllowSelfReview=true）
		if !allowSelfReview {
			if err := ensureNoSelfReview(tc, assign); err != nil {
				return err
			}
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
			AIGateStatus:    model.AIGateStatusNotStarted, // v0.2：门禁状态独立字段
			CreatedBy:       userID,
			UpdatedBy:       userID,
		}
		if err := s.reviewRepo.CreateItem(ctx, tx, item); err != nil {
			return err
		}
		// item.ID 此时已被 GORM 回填为真实数据库 ID

		// 分配评审人（区分 Primary / Shadow 角色）
		reviewers := buildReviewerRows(reviewID, item.ID, projectID, assign)
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

// buildReviewerRows 按 assignment 构造评审人记录，写入 ReviewRole
func buildReviewerRows(reviewID, itemID, projectID uint, assign ReviewerAssignment) []model.CaseReviewItemReviewer {
	rows := make([]model.CaseReviewItemReviewer, 0, 1+len(assign.ShadowIDs))
	rows = append(rows, model.CaseReviewItemReviewer{
		ReviewID:     reviewID,
		ReviewItemID: itemID,
		ProjectID:    projectID,
		ReviewerID:   assign.PrimaryID,
		ReviewStatus: model.ReviewerStatusPending,
		ReviewRole:   model.ReviewRolePrimary,
	})
	seen := map[uint]struct{}{assign.PrimaryID: {}}
	for _, sid := range assign.ShadowIDs {
		if sid == 0 {
			continue
		}
		if _, dup := seen[sid]; dup {
			continue
		}
		seen[sid] = struct{}{}
		rows = append(rows, model.CaseReviewItemReviewer{
			ReviewID:     reviewID,
			ReviewItemID: itemID,
			ProjectID:    projectID,
			ReviewerID:   sid,
			ReviewStatus: model.ReviewerStatusPending,
			ReviewRole:   model.ReviewRoleShadow,
		})
	}
	return rows
}

// ensureNoSelfReview 禁止 Author 评审自己用例（Primary 与 Shadow 均不允许）
func ensureNoSelfReview(tc *model.TestCase, assign ReviewerAssignment) error {
	if tc == nil || tc.CreatedBy == 0 {
		return nil
	}
	if assign.PrimaryID == tc.CreatedBy {
		return ErrBadRequest(CodeReviewSelfReviewForbidden, "主评人不能是用例作者（allow_self_review 未开启）")
	}
	for _, sid := range assign.ShadowIDs {
		if sid == tc.CreatedBy {
			return ErrBadRequest(CodeReviewSelfReviewForbidden, "陪审不能包含用例作者（allow_self_review 未开启）")
		}
	}
	return nil
}

// deriveAssignment 从请求参数派生 ReviewerAssignment：
//   - 优先采用 primaryID + shadowIDs 显式指定
//   - 否则回退到 legacyIDs：首元素当 Primary，其余当 Shadow
//   - Primary 为 0 时返回空 assignment + 错误，由上层决定是否回退默认值
func deriveAssignment(primaryID uint, shadowIDs, legacyIDs []uint) (ReviewerAssignment, error) {
	if primaryID != 0 {
		return ReviewerAssignment{
			PrimaryID: primaryID,
			ShadowIDs: dedupExclude(shadowIDs, primaryID),
		}, nil
	}
	if len(legacyIDs) == 0 {
		return ReviewerAssignment{}, ErrBadRequest(CodeReviewPrimaryRequired, "必须指定唯一主评人")
	}
	pid := legacyIDs[0]
	return ReviewerAssignment{
		PrimaryID: pid,
		ShadowIDs: dedupExclude(legacyIDs[1:], pid),
	}, nil
}

// dedupExclude 去重并排除 exclude 值，返回去重后的新切片（不原地修改）
func dedupExclude(ids []uint, exclude uint) []uint {
	if len(ids) == 0 {
		return nil
	}
	out := make([]uint, 0, len(ids))
	seen := map[uint]struct{}{}
	for _, id := range ids {
		if id == 0 || id == exclude {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// loadProjectSettings 读取项目 settings；项目不存在时回退到默认（禁止自审）
func (s *CaseReviewService) loadProjectSettings(ctx context.Context, projectID uint) (model.ProjectSettings, error) {
	if s.projectRepo == nil {
		return model.ProjectSettings{}, nil
	}
	project, err := s.projectRepo.FindByID(ctx, projectID)
	if err != nil {
		return model.ProjectSettings{}, ErrNotFound(CodeProjectNotFound, "项目不存在")
	}
	return project.ParseSettings(), nil
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

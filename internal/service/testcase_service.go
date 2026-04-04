// testcase_service.go — 测试用例业务逻辑
package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"testpilot/internal/model"
	"testpilot/internal/repository"
)

// TestCaseService 用例管理服务
type TestCaseService struct {
	testCaseRepo    repository.TestCaseRepository
	caseHistoryRepo *repository.CaseHistoryRepo
	auditRepo       repository.AuditRepository
}

// NewTestCaseService 创建用例服务
func NewTestCaseService(repo repository.TestCaseRepository, historyRepo *repository.CaseHistoryRepo, auditRepo repository.AuditRepository) *TestCaseService {
	return &TestCaseService{
		testCaseRepo:    repo,
		caseHistoryRepo: historyRepo,
		auditRepo:       auditRepo,
	}
}

// CreateTestCaseInput 创建用例输入
type CreateTestCaseInput struct {
	Title        string
	Level        string
	ReviewResult string
	ExecResult   string
	ModuleID     uint
	ModulePath   string
	Tags         string
	Precondition string
	Steps        string
	Remark       string
	Priority     string
}

// Create 创建用例
func (s *TestCaseService) Create(ctx context.Context, projectID, userID uint, input CreateTestCaseInput) (*model.TestCase, error) {
	if strings.TrimSpace(input.Title) == "" {
		return nil, ErrBadRequest("MISSING_TITLE", "title is required")
	}
	priority := strings.ToLower(strings.TrimSpace(input.Priority))
	if priority == "" {
		priority = "medium"
	}
	level := strings.ToUpper(strings.TrimSpace(input.Level))
	if level == "" {
		level = "P1"
	}
	reviewResult := strings.TrimSpace(input.ReviewResult)
	if reviewResult == "" {
		reviewResult = "未评审"
	}
	execResult := strings.TrimSpace(input.ExecResult)
	if execResult == "" {
		execResult = "未执行"
	}
	modulePath := strings.TrimSpace(input.ModulePath)
	if modulePath == "" {
		modulePath = "/未分类"
	}

	entity := model.TestCase{
		ProjectID: projectID, Title: strings.TrimSpace(input.Title),
		Status:       model.TestCaseStatusDraft, // 初始为草稿
		Version:      "V1",                      // 初始为 V1
		Level:        level,
		ReviewResult: reviewResult,
		ExecResult:   execResult,
		ModuleID:     input.ModuleID, ModulePath: modulePath,
		Tags: strings.TrimSpace(input.Tags),
		Precondition: input.Precondition,
		Steps:        strings.TrimSpace(input.Steps),
		Remark:       input.Remark,
		Priority:     priority,
		CreatedBy:    userID, UpdatedBy: userID,
	}
	if err := s.testCaseRepo.Create(ctx, &entity); err != nil {
		if isDuplicateError(err) {
			return nil, ErrConflict("TESTCASE_EXISTS", "testcase already exists")
		}
		return nil, ErrInternal("DB_ERROR", err)
	}
	return &entity, nil
}

// ListPaged 分页查询用例
func (s *TestCaseService) ListPaged(ctx context.Context, projectID uint, filter repository.TestCaseFilter) ([]repository.TestCaseListItem, int64, error) {
	return s.testCaseRepo.ListPaged(ctx, projectID, filter)
}

// UpdateTestCaseInput 更新用例输入
type UpdateTestCaseInput struct {
	Title        *string
	Level        *string
	ReviewResult *string
	ExecResult   *string
	ModuleID     *uint
	ModulePath   *string
	Tags         *string
	Precondition *string
	Steps        *string
	Remark       *string
	Priority     *string
}

// Update 更新用例
func (s *TestCaseService) Update(ctx context.Context, projectID, testCaseID, userID uint, input UpdateTestCaseInput) (*model.TestCase, error) {
	entity, err := s.testCaseRepo.FindByID(ctx, testCaseID, projectID)
	if err != nil {
		return nil, ErrTestCaseNotFound
	}

	// 实质变更检测：如果状态是 active，修改核心字段会触发升版并回到 draft
	isSubstantialChange := false

	updates := map[string]any{}
	if input.Title != nil {
		t := strings.TrimSpace(*input.Title)
		if t == "" {
			return nil, ErrBadRequest("MISSING_TITLE", "title is required")
		}
		if t != entity.Title {
			updates["title"] = t
			isSubstantialChange = true
		}
	}
	if input.Level != nil {
		l := strings.ToUpper(strings.TrimSpace(*input.Level))
		if l == "" {
			l = "P1"
		}
		if l != entity.Level {
			updates["level"] = l
			isSubstantialChange = true
		}
	}
	if input.ReviewResult != nil {
		rr := strings.TrimSpace(*input.ReviewResult)
		if rr == "" {
			rr = "未评审"
		}
		updates["review_result"] = rr
	}
	if input.ExecResult != nil {
		er := strings.TrimSpace(*input.ExecResult)
		if er == "" {
			er = "未执行"
		}
		updates["exec_result"] = er
	}
	if input.ModulePath != nil {
		mp := strings.TrimSpace(*input.ModulePath)
		if mp == "" {
			mp = "/未分类"
		}
		updates["module_path"] = mp
	}
	if input.Tags != nil {
		ts := strings.TrimSpace(*input.Tags)
		if ts != entity.Tags {
			updates["tags"] = ts
			// 标签变更通常不视为核心实质变更，但为了严谨性可根据需求决定。
			// 这里遵循 PRD：目录移动、标签调整、排序变化不属于实质变更。
		}
	}
	if input.Precondition != nil {
		if *input.Precondition != entity.Precondition {
			updates["precondition"] = *input.Precondition
			isSubstantialChange = true
		}
	}
	if input.Steps != nil {
		s := strings.TrimSpace(*input.Steps)
		if s != entity.Steps {
			updates["steps"] = s
			isSubstantialChange = true
		}
	}
	if input.Remark != nil {
		if *input.Remark != entity.Remark {
			updates["remark"] = *input.Remark
			isSubstantialChange = true
		}
	}
	if input.ModuleID != nil {
		updates["module_id"] = *input.ModuleID
	}
	if input.Priority != nil {
		p := strings.ToLower(strings.TrimSpace(*input.Priority))
		if p == "" {
			p = "medium"
		}
		if p != entity.Priority {
			updates["priority"] = p
			isSubstantialChange = true
		}
	}

	// 状态流转规则逻辑
	if entity.Status == model.TestCaseStatusActive && isSubstantialChange {
		// 已生效用例发生实质变更：保存快照，版本 +1，状态重置为草稿
		_ = s.saveHistorySnapshot(ctx, entity, userID, "version_bump")

		updates["status"] = model.TestCaseStatusDraft
		updates["version"] = nextVersion(entity.Version)
		updates["review_result"] = "未评审"
		updates["exec_result"] = "未执行"
	} else if entity.Status == model.TestCaseStatusPending && isSubstantialChange {
		// 待评审状态禁止直接编辑核心内容
		return nil, ErrBadRequest("STATUS_LOCKED", "pending case cannot be edited, please retract first")
	} else if entity.Status == model.TestCaseStatusDiscarded {
		// 已废弃状态禁止编辑
		return nil, ErrBadRequest("STATUS_LOCKED", "discarded case is read-only")
	}
	if len(updates) == 0 {
		return nil, ErrBadRequest("NO_FIELDS", "no fields to update")
	}
	if err := s.testCaseRepo.Updates(ctx, entity, updates); err != nil {
		if isDuplicateError(err) {
			return nil, ErrConflict("TESTCASE_EXISTS", "testcase already exists")
		}
		return nil, ErrInternal("DB_ERROR", err)
	}
	updated, _ := s.testCaseRepo.FindByID(ctx, testCaseID, projectID)
	return updated, nil
}

// Delete 删除用例
func (s *TestCaseService) Delete(ctx context.Context, projectID, testCaseID uint) error {
	rows, err := s.testCaseRepo.Delete(ctx, testCaseID, projectID)
	if err != nil {
		return ErrInternal("DB_ERROR", err)
	}
	if rows == 0 {
		return ErrTestCaseNotFound
	}
	return nil
}

// BatchDelete 批量删除用例
func (s *TestCaseService) BatchDelete(ctx context.Context, projectID uint, ids []uint) (int64, error) {
	if len(ids) == 0 {
		return 0, ErrBadRequest("EMPTY_IDS", "no IDs provided")
	}
	return s.testCaseRepo.BatchDelete(ctx, projectID, ids)
}

// BatchUpdateLevel 批量修改等级
func (s *TestCaseService) BatchUpdateLevel(ctx context.Context, projectID uint, ids []uint, level string) (int64, error) {
	if len(ids) == 0 {
		return 0, ErrBadRequest("EMPTY_IDS", "no IDs provided")
	}
	l := strings.ToUpper(strings.TrimSpace(level))
	if l == "" {
		return 0, ErrBadRequest("MISSING_LEVEL", "level is required")
	}
	return s.testCaseRepo.BatchUpdateLevel(ctx, projectID, ids, l)
}

// BatchMove 批量移动用例到另一目录
func (s *TestCaseService) BatchMove(ctx context.Context, projectID uint, ids []uint, moduleID uint, modulePath string) (int64, error) {
	if len(ids) == 0 {
		return 0, ErrBadRequest("EMPTY_IDS", "no IDs provided")
	}
	return s.testCaseRepo.BatchMove(ctx, projectID, ids, moduleID, modulePath)
}

// CloneCase 复制用例
func (s *TestCaseService) CloneCase(ctx context.Context, projectID, sourceID, userID uint) (*model.TestCase, error) {
	entity, err := s.testCaseRepo.CloneCase(ctx, projectID, sourceID, userID)
	if err != nil {
		return nil, err
	}
	// 审计
	_ = s.auditRepo.WriteLogTx(s.testCaseRepo.DB(ctx), userID, "clone", "testcase", entity.ID, "source:"+strconv.Itoa(int(sourceID)), "cloned")
	return entity, nil
}

// SubmitReview 提交评审 (Draft -> Pending)
func (s *TestCaseService) SubmitReview(ctx context.Context, projectID, testCaseID, userID uint) error {
	entity, err := s.testCaseRepo.FindByID(ctx, testCaseID, projectID)
	if err != nil {
		return ErrTestCaseNotFound
	}
	if entity.Status != model.TestCaseStatusDraft {
		return ErrBadRequest("INVALID_STATUS", "only draft can be submitted for review")
	}
	updates := map[string]any{
		"status":        model.TestCaseStatusPending,
		"review_result": "待评审",
	}
	if err := s.testCaseRepo.Updates(ctx, entity, updates); err != nil {
		return err
	}
	// 审计
	_ = s.auditRepo.WriteLogTx(s.testCaseRepo.DB(ctx), userID, "submit_review", "testcase", testCaseID, "draft", "pending")
	return nil
}

// Discard 废弃用例 (Active -> Discarded)
func (s *TestCaseService) Discard(ctx context.Context, projectID, testCaseID, userID uint) error {
	entity, err := s.testCaseRepo.FindByID(ctx, testCaseID, projectID)
	if err != nil {
		return ErrTestCaseNotFound
	}
	if entity.Status != model.TestCaseStatusActive {
		return ErrBadRequest("INVALID_STATUS", "only active case can be discarded")
	}
	updates := map[string]any{
		"status": model.TestCaseStatusDiscarded,
	}
	if err := s.testCaseRepo.Updates(ctx, entity, updates); err != nil {
		return err
	}
	// 审计
	_ = s.auditRepo.WriteLogTx(s.testCaseRepo.DB(ctx), userID, "discard", "testcase", testCaseID, "active", "discarded")
	return nil
}

// Recover 恢复用例 (Discarded -> Draft)
func (s *TestCaseService) Recover(ctx context.Context, projectID, testCaseID, userID uint) error {
	entity, err := s.testCaseRepo.FindByID(ctx, testCaseID, projectID)
	if err != nil {
		return ErrTestCaseNotFound
	}
	if entity.Status != model.TestCaseStatusDiscarded {
		return ErrBadRequest("INVALID_STATUS", "only discarded case can be recovered")
	}
	updates := map[string]any{
		"status":        model.TestCaseStatusDraft,
		"review_result": "未评审",
		"exec_result":   "未执行",
	}
	if err := s.testCaseRepo.Updates(ctx, entity, updates); err != nil {
		return err
	}
	// 审计
	_ = s.auditRepo.WriteLogTx(s.testCaseRepo.DB(ctx), userID, "recover", "testcase", testCaseID, "discarded", "draft")
	return nil
}

// saveHistorySnapshot 保存用例快照（用于版本回溯）
func (s *TestCaseService) saveHistorySnapshot(ctx context.Context, entity *model.TestCase, userID uint, action string) error {
	history := model.CaseHistory{
		TestCaseID: entity.ID,
		Action:     action,
		FieldName:  "full_snapshot",
		OldValue:   fmt.Sprintf("Title:%s;Steps:%s;Precondition:%s", entity.Title, entity.Steps, entity.Precondition),
		NewValue:   "version:" + entity.Version,
		ChangedBy:  userID,
	}
	return s.caseHistoryRepo.Create(s.testCaseRepo.DB(ctx), &history)
}

// nextVersion 版本号递增辅助函数 (e.g. V1 -> V2)
func nextVersion(current string) string {
	if !strings.HasPrefix(current, "V") {
		return "V1"
	}
	numStr := strings.TrimPrefix(current, "V")
	num, err := strconv.Atoi(numStr)
	if err != nil {
		return "V1"
	}
	return "V" + strconv.Itoa(num+1)
}

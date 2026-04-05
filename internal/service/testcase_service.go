// testcase_service.go 鈥?娴嬭瘯鐢ㄤ緥涓氬姟閫昏緫
package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"testpilot/internal/model"
	"testpilot/internal/repository"
)

// TestCaseService 鐢ㄤ緥绠＄悊鏈嶅姟
type TestCaseService struct {
	testCaseRepo    repository.TestCaseRepository
	caseHistoryRepo *repository.CaseHistoryRepo
	auditRepo       repository.AuditRepository
}

// NewTestCaseService 鍒涘缓鐢ㄤ緥鏈嶅姟
func NewTestCaseService(repo repository.TestCaseRepository, historyRepo *repository.CaseHistoryRepo, auditRepo repository.AuditRepository) *TestCaseService {
	return &TestCaseService{
		testCaseRepo:    repo,
		caseHistoryRepo: historyRepo,
		auditRepo:       auditRepo,
	}
}

// CreateTestCaseInput 鍒涘缓鐢ㄤ緥杈撳叆
type CreateTestCaseInput struct {
	Title        string
	Level        string
	ExecResult   string
	ModuleID     uint
	ModulePath   string
	Tags         string
	Precondition string
	Steps        string
	Remark       string
	Priority     string
}

// Create 鍒涘缓鐢ㄤ緥
func (s *TestCaseService) Create(ctx context.Context, projectID, userID uint, input CreateTestCaseInput) (*model.TestCase, error) {
	if strings.TrimSpace(input.Title) == "" {
		return nil, ErrBadRequest(CodeParamsError, "title is required")
	}

	priority := strings.ToLower(strings.TrimSpace(input.Priority))
	if priority == "" {
		priority = "medium"
	}

	level := strings.ToUpper(strings.TrimSpace(input.Level))
	if level == "" {
		level = "P1"
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
		ProjectID:    projectID,
		Title:        strings.TrimSpace(input.Title),
		Status:       model.TestCaseStatusDraft,
		Version:      "V1",
		Level:        level,
		ReviewResult: model.CaseReviewResultNotReviewed,
		ExecResult:   execResult,
		ModuleID:     input.ModuleID,
		ModulePath:   modulePath,
		Tags:         strings.TrimSpace(input.Tags),
		Precondition: input.Precondition,
		Steps:        strings.TrimSpace(input.Steps),
		Remark:       input.Remark,
		Priority:     priority,
		CreatedBy:    userID,
		UpdatedBy:    userID,
	}
	if err := s.testCaseRepo.Create(ctx, &entity); err != nil {
		if isDuplicateError(err) {
			return nil, ErrConflict(CodeConflict, "testcase already exists")
		}
		return nil, ErrInternal(CodeInternal, err)
	}
	return &entity, nil
}

// ListPaged 鍒嗛〉鏌ヨ鐢ㄤ緥
func (s *TestCaseService) ListPaged(ctx context.Context, projectID uint, filter repository.TestCaseFilter) ([]repository.TestCaseListItem, int64, error) {
	return s.testCaseRepo.ListPaged(ctx, projectID, filter)
}

// UpdateTestCaseInput 鏇存柊鐢ㄤ緥杈撳叆
type UpdateTestCaseInput struct {
	Title        *string
	Level        *string
	ExecResult   *string
	ModuleID     *uint
	ModulePath   *string
	Tags         *string
	Precondition *string
	Steps        *string
	Remark       *string
	Priority     *string
}

// Update 鏇存柊鐢ㄤ緥
func (s *TestCaseService) Update(ctx context.Context, projectID, testCaseID, userID uint, input UpdateTestCaseInput) (*model.TestCase, error) {
	entity, err := s.testCaseRepo.FindByID(ctx, testCaseID, projectID)
	if err != nil {
		return nil, ErrTestCaseNotFound
	}

	isSubstantialChange := false
	updates := map[string]any{}

	if input.Title != nil {
		title := strings.TrimSpace(*input.Title)
		if title == "" {
			return nil, ErrBadRequest(CodeParamsError, "title is required")
		}
		if title != entity.Title {
			updates["title"] = title
			isSubstantialChange = true
		}
	}
	if input.Level != nil {
		level := strings.ToUpper(strings.TrimSpace(*input.Level))
		if level == "" {
			level = "P1"
		}
		if level != entity.Level {
			updates["level"] = level
			isSubstantialChange = true
		}
	}
	if input.ExecResult != nil {
		execResult := strings.TrimSpace(*input.ExecResult)
		if execResult == "" {
			execResult = "未执行"
		}
		updates["exec_result"] = execResult
	}
	if input.ModulePath != nil {
		modulePath := strings.TrimSpace(*input.ModulePath)
		if modulePath == "" {
			modulePath = "/未分类"
		}
		updates["module_path"] = modulePath
	}
	if input.Tags != nil {
		tags := strings.TrimSpace(*input.Tags)
		if tags != entity.Tags {
			updates["tags"] = tags
		}
	}
	if input.Precondition != nil && *input.Precondition != entity.Precondition {
		updates["precondition"] = *input.Precondition
		isSubstantialChange = true
	}
	if input.Steps != nil {
		steps := strings.TrimSpace(*input.Steps)
		if steps != entity.Steps {
			updates["steps"] = steps
			isSubstantialChange = true
		}
	}
	if input.Remark != nil && *input.Remark != entity.Remark {
		updates["remark"] = *input.Remark
	}
	if input.ModuleID != nil {
		updates["module_id"] = *input.ModuleID
	}
	if input.Priority != nil {
		priority := strings.ToLower(strings.TrimSpace(*input.Priority))
		if priority == "" {
			priority = "medium"
		}
		if priority != entity.Priority {
			updates["priority"] = priority
			isSubstantialChange = true
		}
	}

	if entity.Status == model.TestCaseStatusActive && isSubstantialChange {
		_ = s.saveHistorySnapshot(ctx, entity, userID, "version_bump")
		updates["status"] = model.TestCaseStatusDraft
		updates["version"] = nextVersion(entity.Version)
		updates["review_result"] = model.CaseReviewResultNotReviewed
		updates["exec_result"] = "未执行"
	} else if entity.Status == model.TestCaseStatusPending && isSubstantialChange {
		return nil, ErrBadRequest(CodeParamsError, "pending case cannot be edited, please retract first")
	} else if entity.Status == model.TestCaseStatusDiscarded {
		return nil, ErrBadRequest(CodeParamsError, "discarded case is read-only")
	}

	if len(updates) == 0 {
		return nil, ErrBadRequest(CodeParamsError, "no fields to update")
	}
	if err := s.testCaseRepo.Updates(ctx, entity, updates); err != nil {
		if isDuplicateError(err) {
			return nil, ErrConflict(CodeConflict, "testcase already exists")
		}
		return nil, ErrInternal(CodeInternal, err)
	}
	updated, _ := s.testCaseRepo.FindByID(ctx, testCaseID, projectID)
	return updated, nil
}

// Delete 鍒犻櫎鐢ㄤ緥
func (s *TestCaseService) Delete(ctx context.Context, projectID, testCaseID uint) error {
	rows, err := s.testCaseRepo.Delete(ctx, testCaseID, projectID)
	if err != nil {
		return ErrInternal(CodeInternal, err)
	}
	if rows == 0 {
		return ErrTestCaseNotFound
	}
	return nil
}

// BatchDelete 鎵归噺鍒犻櫎鐢ㄤ緥
func (s *TestCaseService) BatchDelete(ctx context.Context, projectID uint, ids []uint) (int64, error) {
	if len(ids) == 0 {
		return 0, ErrBadRequest(CodeParamsError, "no IDs provided")
	}
	return s.testCaseRepo.BatchDelete(ctx, projectID, ids)
}

// BatchUpdateLevel 鎵归噺淇敼绛夌骇
func (s *TestCaseService) BatchUpdateLevel(ctx context.Context, projectID uint, ids []uint, level string) (int64, error) {
	if len(ids) == 0 {
		return 0, ErrBadRequest(CodeParamsError, "no IDs provided")
	}
	l := strings.ToUpper(strings.TrimSpace(level))
	if l == "" {
		return 0, ErrBadRequest(CodeParamsError, "level is required")
	}
	return s.testCaseRepo.BatchUpdateLevel(ctx, projectID, ids, l)
}

// BatchMove 鎵归噺绉诲姩鐢ㄤ緥鍒板彟涓€鐩綍
func (s *TestCaseService) BatchMove(ctx context.Context, projectID uint, ids []uint, moduleID uint, modulePath string) (int64, error) {
	if len(ids) == 0 {
		return 0, ErrBadRequest(CodeParamsError, "no IDs provided")
	}
	return s.testCaseRepo.BatchMove(ctx, projectID, ids, moduleID, modulePath)
}

// CloneCase 澶嶅埗鐢ㄤ緥
func (s *TestCaseService) CloneCase(ctx context.Context, projectID, sourceID, userID uint) (*model.TestCase, error) {
	entity, err := s.testCaseRepo.CloneCase(ctx, projectID, sourceID, userID)
	if err != nil {
		return nil, err
	}
	// 瀹¤
	_ = s.auditRepo.WriteLogTx(s.testCaseRepo.DB(ctx), userID, "clone", "testcase", entity.ID, "source:"+strconv.Itoa(int(sourceID)), "cloned")
	return entity, nil
}

// Discard 搴熷純鐢ㄤ緥 (Active -> Discarded)
func (s *TestCaseService) Discard(ctx context.Context, projectID, testCaseID, userID uint) error {
	entity, err := s.testCaseRepo.FindByID(ctx, testCaseID, projectID)
	if err != nil {
		return ErrTestCaseNotFound
	}
	if entity.Status != model.TestCaseStatusActive {
		return ErrBadRequest(CodeParamsError, "only active case can be discarded")
	}
	updates := map[string]any{
		"status": model.TestCaseStatusDiscarded,
	}
	if err := s.testCaseRepo.Updates(ctx, entity, updates); err != nil {
		return err
	}
	// 瀹¤
	_ = s.auditRepo.WriteLogTx(s.testCaseRepo.DB(ctx), userID, "discard", "testcase", testCaseID, "active", "discarded")
	return nil
}

// Recover 鎭㈠鐢ㄤ緥 (Discarded -> Draft)
func (s *TestCaseService) Recover(ctx context.Context, projectID, testCaseID, userID uint) error {
	entity, err := s.testCaseRepo.FindByID(ctx, testCaseID, projectID)
	if err != nil {
		return ErrTestCaseNotFound
	}
	if entity.Status != model.TestCaseStatusDiscarded {
		return ErrBadRequest(CodeParamsError, "only discarded case can be recovered")
	}
	updates := map[string]any{
		"status":        model.TestCaseStatusDraft,
		"review_result": model.CaseReviewResultNotReviewed,
		"exec_result":   "未执行",
	}
	if err := s.testCaseRepo.Updates(ctx, entity, updates); err != nil {
		return err
	}
	// 瀹¤
	_ = s.auditRepo.WriteLogTx(s.testCaseRepo.DB(ctx), userID, "recover", "testcase", testCaseID, "discarded", "draft")
	return nil
}

// saveHistorySnapshot 淇濆瓨鐢ㄤ緥蹇収锛堢敤浜庣増鏈洖婧級
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

// nextVersion 鐗堟湰鍙烽€掑杈呭姪鍑芥暟 (e.g. V1 -> V2)
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

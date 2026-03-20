// testcase_service.go — 测试用例业务逻辑
package service

import (
	"context"
	"strings"

	"testpilot/internal/model"
	"testpilot/internal/repository"
)

// TestCaseService 用例管理服务
type TestCaseService struct {
	testCaseRepo    repository.TestCaseRepository
	caseHistoryRepo *repository.CaseHistoryRepo
}

// NewTestCaseService 创建用例服务
func NewTestCaseService(repo repository.TestCaseRepository, historyRepo *repository.CaseHistoryRepo) *TestCaseService {
	return &TestCaseService{testCaseRepo: repo, caseHistoryRepo: historyRepo}
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
		Level: level, ReviewResult: reviewResult, ExecResult: execResult,
		ModuleID: input.ModuleID, ModulePath: modulePath,
		Tags: strings.TrimSpace(input.Tags),
		Precondition: input.Precondition,
		Steps: strings.TrimSpace(input.Steps),
		Remark: input.Remark,
		Priority: priority,
		CreatedBy: userID, UpdatedBy: userID,
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
func (s *TestCaseService) Update(ctx context.Context, projectID, testCaseID uint, input UpdateTestCaseInput) (*model.TestCase, error) {
	entity, err := s.testCaseRepo.FindByID(ctx, testCaseID, projectID)
	if err != nil {
		return nil, ErrTestCaseNotFound
	}

	updates := map[string]any{}
	if input.Title != nil {
		t := strings.TrimSpace(*input.Title)
		if t == "" {
			return nil, ErrBadRequest("MISSING_TITLE", "title is required")
		}
		updates["title"] = t
	}
	if input.Level != nil {
		l := strings.ToUpper(strings.TrimSpace(*input.Level))
		if l == "" {
			l = "P1"
		}
		updates["level"] = l
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
		updates["tags"] = strings.TrimSpace(*input.Tags)
	}
	if input.Precondition != nil {
		updates["precondition"] = *input.Precondition
	}
	if input.Steps != nil {
		updates["steps"] = strings.TrimSpace(*input.Steps)
	}
	if input.Remark != nil {
		updates["remark"] = *input.Remark
	}
	if input.ModuleID != nil {
		updates["module_id"] = *input.ModuleID
	}
	if input.Priority != nil {
		p := strings.ToLower(strings.TrimSpace(*input.Priority))
		if p == "" {
			p = "medium"
		}
		updates["priority"] = p
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
	return s.testCaseRepo.CloneCase(ctx, projectID, sourceID, userID)
}

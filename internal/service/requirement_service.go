// requirement_service.go — 需求管理业务逻辑
package service

import (
	"context"
	"strings"

	"testpilot/internal/model"
	"testpilot/internal/repository"
)

// RequirementService 需求管理服务
type RequirementService struct {
	requirementRepo repository.RequirementRepository
	testCaseRepo    repository.TestCaseRepository
}

// NewRequirementService 创建需求服务
func NewRequirementService(reqRepo repository.RequirementRepository, tcRepo repository.TestCaseRepository) *RequirementService {
	return &RequirementService{requirementRepo: reqRepo, testCaseRepo: tcRepo}
}

// Create 创建需求
func (s *RequirementService) Create(ctx context.Context, projectID uint, title, content string) (*model.Requirement, error) {
	if strings.TrimSpace(title) == "" {
		return nil, ErrBadRequest(CodeParamsError, "title is required")
	}
	entity := model.Requirement{ProjectID: projectID, Title: strings.TrimSpace(title), Content: strings.TrimSpace(content)}
	if err := s.requirementRepo.Create(ctx, &entity); err != nil {
		if isDuplicateError(err) {
			return nil, ErrConflict(CodeConflict, "requirement already exists")
		}
		return nil, ErrInternal(CodeInternal, err)
	}
	return &entity, nil
}

// List 获取需求列表
func (s *RequirementService) List(ctx context.Context, projectID uint) ([]model.Requirement, error) {
	return s.requirementRepo.List(ctx, projectID)
}

// LinkTestCase 关联需求与用例
func (s *RequirementService) LinkTestCase(ctx context.Context, projectID, requirementID, testCaseID uint) error {
	reqBelong, _ := s.requirementRepo.BelongsToProject(ctx, requirementID, projectID)
	tcBelong, _ := s.testCaseRepo.BelongsToProject(ctx, testCaseID, projectID)
	if !reqBelong || !tcBelong {
		return ErrNotFound(CodeNotFound, "requirement or testcase not found in project")
	}
	return s.requirementRepo.LinkTestCase(ctx, requirementID, testCaseID)
}

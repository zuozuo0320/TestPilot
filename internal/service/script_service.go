// script_service.go — 脚本管理业务逻辑
package service

import (
	"context"
	"strings"

	"testpilot/internal/model"
	"testpilot/internal/repository"
)

// ScriptService 脚本管理服务
type ScriptService struct {
	scriptRepo   repository.ScriptRepository
	testCaseRepo repository.TestCaseRepository
}

// NewScriptService 创建脚本服务
func NewScriptService(scriptRepo repository.ScriptRepository, tcRepo repository.TestCaseRepository) *ScriptService {
	return &ScriptService{scriptRepo: scriptRepo, testCaseRepo: tcRepo}
}

// Create 创建脚本
func (s *ScriptService) Create(ctx context.Context, projectID uint, name, path, scriptType string) (*model.Script, error) {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(path) == "" {
		return nil, ErrBadRequest("MISSING_FIELDS", "name/path is required")
	}
	st := strings.ToLower(strings.TrimSpace(scriptType))
	if st == "" {
		st = "cypress"
	}
	entity := model.Script{ProjectID: projectID, Name: strings.TrimSpace(name), Path: strings.TrimSpace(path), Type: st}
	if err := s.scriptRepo.Create(ctx, &entity); err != nil {
		if isDuplicateError(err) {
			return nil, ErrConflict("SCRIPT_EXISTS", "script already exists")
		}
		return nil, ErrInternal("DB_ERROR", err)
	}
	return &entity, nil
}

// List 获取脚本列表
func (s *ScriptService) List(ctx context.Context, projectID uint) ([]model.Script, error) {
	return s.scriptRepo.List(ctx, projectID)
}

// LinkTestCase 关联用例与脚本
func (s *ScriptService) LinkTestCase(ctx context.Context, projectID, testCaseID, scriptID uint) error {
	tcBelong, _ := s.testCaseRepo.BelongsToProject(ctx, testCaseID, projectID)
	scBelong, _ := s.scriptRepo.BelongsToProject(ctx, scriptID, projectID)
	if !tcBelong || !scBelong {
		return ErrNotFound("ENTITY_NOT_FOUND", "testcase or script not found in project")
	}
	return s.scriptRepo.LinkTestCase(ctx, testCaseID, scriptID)
}

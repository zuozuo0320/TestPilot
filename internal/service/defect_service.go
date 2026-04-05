// defect_service.go — 缺陷管理业务逻辑
package service

import (
	"context"
	"strings"

	"testpilot/internal/model"
	"testpilot/internal/repository"
)

// DefectService 缺陷管理服务
type DefectService struct {
	defectRepo    repository.DefectRepository
	executionRepo repository.ExecutionRepository
}

// NewDefectService 创建缺陷服务
func NewDefectService(defectRepo repository.DefectRepository, executionRepo repository.ExecutionRepository) *DefectService {
	return &DefectService{defectRepo: defectRepo, executionRepo: executionRepo}
}

// Create 创建缺陷
func (s *DefectService) Create(ctx context.Context, projectID, userID, runResultID uint, title, description, severity string) (*model.Defect, error) {
	if runResultID == 0 || strings.TrimSpace(title) == "" {
		return nil, ErrBadRequest(CodeParamsError, "run_result_id/title is required")
	}
	sev := strings.ToLower(strings.TrimSpace(severity))
	if sev == "" {
		sev = "medium"
	}
	if !isValidSeverity(sev) {
		return nil, ErrBadRequest(CodeParamsError, "severity should be low/medium/high/critical")
	}
	if _, err := s.executionRepo.FindResultByID(ctx, runResultID, projectID); err != nil {
		return nil, ErrNotFound(CodeNotFound, "run result not found")
	}
	defect := model.Defect{
		ProjectID: projectID, RunResultID: runResultID,
		Title: strings.TrimSpace(title), Description: strings.TrimSpace(description),
		Severity: sev, Status: "open", CreatedBy: userID,
	}
	if err := s.defectRepo.Create(ctx, &defect); err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}
	return &defect, nil
}

// List 获取缺陷列表
func (s *DefectService) List(ctx context.Context, projectID uint) ([]model.Defect, error) {
	return s.defectRepo.List(ctx, projectID)
}

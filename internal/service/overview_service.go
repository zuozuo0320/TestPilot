// overview_service.go — 项目概览业务逻辑
package service

import (
	"context"

	"testpilot/internal/repository"
)

// OverviewService 概览服务
type OverviewService struct {
	projectRepo     repository.ProjectRepository
	requirementRepo repository.RequirementRepository
	testCaseRepo    repository.TestCaseRepository
	scriptRepo      repository.ScriptRepository
	executionRepo   repository.ExecutionRepository
	defectRepo      repository.DefectRepository
}

// NewOverviewService 创建概览服务
func NewOverviewService(
	projectRepo repository.ProjectRepository,
	reqRepo repository.RequirementRepository,
	tcRepo repository.TestCaseRepository,
	scRepo repository.ScriptRepository,
	execRepo repository.ExecutionRepository,
	defRepo repository.DefectRepository,
) *OverviewService {
	return &OverviewService{
		projectRepo: projectRepo, requirementRepo: reqRepo,
		testCaseRepo: tcRepo, scriptRepo: scRepo,
		executionRepo: execRepo, defectRepo: defRepo,
	}
}

// OverviewResult 概览结果
type OverviewResult struct {
	ProjectName  string  `json:"project_name"`
	Counts       Counts  `json:"counts"`
	LatestRun    any     `json:"latest_run"`
	QualityGate  any     `json:"quality_gate"`
}

// Counts 实体计数
type Counts struct {
	Requirements int64 `json:"requirements"`
	TestCases    int64 `json:"testcases"`
	Scripts      int64 `json:"scripts"`
	Runs         int64 `json:"runs"`
	Defects      int64 `json:"defects"`
}

// GetOverview 获取项目概览
func (s *OverviewService) GetOverview(ctx context.Context, projectID uint) (map[string]any, error) {
	project, err := s.projectRepo.FindByID(ctx, projectID)
	if err != nil {
		return nil, ErrProjectNotFound
	}

	reqCount, _ := s.requirementRepo.Count(ctx, projectID)
	tcCount, _ := s.testCaseRepo.CountByProject(ctx, projectID)
	scCount, _ := s.scriptRepo.Count(ctx, projectID)
	runCount, _ := s.executionRepo.CountRuns(ctx, projectID)
	defCount, _ := s.defectRepo.Count(ctx, projectID)

	summary := map[string]any{
		"project": project,
		"counts": map[string]any{
			"requirements": reqCount, "testcases": tcCount,
			"scripts": scCount, "runs": runCount, "defects": defCount,
		},
		"latest_run":   map[string]any{},
		"quality_gate": map[string]any{"status": "no_runs", "reason": "no execution data"},
	}

	latestRun, err := s.executionRepo.LatestRun(ctx, projectID)
	if err != nil {
		return summary, nil
	}

	totalResults, passedResults, err := s.executionRepo.CountResultsByRun(ctx, latestRun.ID)
	if err != nil {
		return nil, ErrInternal("DB_ERROR", err)
	}
	passRate := 0.0
	if totalResults > 0 {
		passRate = (float64(passedResults) / float64(totalResults)) * 100
	}
	qualityStatus := "blocked"
	reason := "pass_rate_below_threshold"
	if totalResults == 0 {
		qualityStatus = "no_runs"
		reason = "no execution data"
	} else if passRate >= 95 {
		qualityStatus = "pass"
		reason = "pass_rate_meets_threshold"
	}

	summary["latest_run"] = map[string]any{
		"id": latestRun.ID, "status": latestRun.Status, "mode": latestRun.Mode,
		"created_at": latestRun.CreatedAt, "total_results": totalResults,
		"passed_results": passedResults, "pass_rate": passRate,
	}
	summary["quality_gate"] = map[string]any{
		"status": qualityStatus, "threshold": 95,
		"pass_rate": passRate, "latest_run": latestRun.ID, "reason": reason,
	}
	return summary, nil
}

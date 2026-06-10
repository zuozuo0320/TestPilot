package service

import (
	"errors"
	"testing"

	"testpilot/internal/model"
	"testpilot/internal/repository"
)

func newTestAIAssertionAssetService(t *testing.T) (*AIAssertionAssetService, *repository.AIAssertionAssetRepo, model.User, model.Project) {
	t.Helper()

	db := testDB(t)
	seedRoles(t, db)
	manager := seedManager(t, db)
	project := seedProject(t, db)
	_, _, projectRepo, _, txMgr := testRepos(db)

	assertionRepo := repository.NewAIAssertionAssetRepo(db)
	refRepo := repository.NewAIAssetReferenceRepo(db)
	svc := NewAIAssertionAssetService(
		testLogger(),
		assertionRepo,
		refRepo,
		projectRepo,
		repository.NewUserRepo(db),
		txMgr,
	)
	return svc, assertionRepo, manager, project
}

func validSaveAssertionInput(projectID uint, assertionKey string) AssertionAssetSaveInput {
	return AssertionAssetSaveInput{
		ProjectID:         projectID,
		AssertionKey:      assertionKey,
		AssertionName:     "页面标题存在",
		AssertionType:     model.AIAssertionTypeTextContains,
		Description:       "用于断言资产删除治理测试",
		ParamSchema:       []byte(`{"type":"object","properties":{"text":{"type":"string"}}}`),
		Implementation:    []byte(`{"template":"await expect(page.getByText(text)).toBeVisible()"}`),
		FailureMessageTpl: "页面标题不存在",
		EvidenceConfig:    []byte(`{"screenshot":"ON_FAILURE","trace":true}`),
		AllowAIReuse:      true,
	}
}

func TestAIAssertionAssetServiceDeleteDraftOnlyWhenUnreferenced(t *testing.T) {
	svc, assertionRepo, manager, project := newTestAIAssertionAssetService(t)
	assertion, err := svc.Create(t.Context(), manager.ID, validSaveAssertionInput(project.ID, "delete_assertion"))
	if err != nil {
		t.Fatalf("create assertion draft: %v", err)
	}

	if err := svc.Delete(t.Context(), project.ID, assertion.ID); err != nil {
		t.Fatalf("delete assertion draft: %v", err)
	}
	if _, err := assertionRepo.GetByID(t.Context(), assertion.ID); err == nil {
		t.Fatalf("expected assertion to be deleted")
	}
}

func TestAIAssertionAssetServiceDeleteRejectsPublishedOrReferenced(t *testing.T) {
	svc, _, manager, project := newTestAIAssertionAssetService(t)
	published, err := svc.Create(t.Context(), manager.ID, validSaveAssertionInput(project.ID, "delete_assertion_published"))
	if err != nil {
		t.Fatalf("create assertion draft: %v", err)
	}
	if _, err := svc.Publish(t.Context(), manager.ID, project.ID, published.ID); err != nil {
		t.Fatalf("publish assertion: %v", err)
	}
	if err := svc.Delete(t.Context(), project.ID, published.ID); err == nil {
		t.Fatalf("expected published assertion delete to fail")
	}

	referenced, err := svc.Create(t.Context(), manager.ID, validSaveAssertionInput(project.ID, "delete_assertion_ref"))
	if err != nil {
		t.Fatalf("create referenced assertion draft: %v", err)
	}
	if err := svc.refRepo.ReplaceForSource(t.Context(), nil, model.AIAssetRefSourceScenario, 1000, []model.AIAssetReference{{
		SourceType: model.AIAssetRefSourceScenario,
		SourceID:   1000,
		TargetType: model.AIAssetRefTargetAssertion,
		TargetID:   referenced.ID,
	}}); err != nil {
		t.Fatalf("create artificial assertion reference: %v", err)
	}
	err = svc.Delete(t.Context(), project.ID, referenced.ID)
	if err == nil {
		t.Fatalf("expected referenced assertion delete to fail")
	}
	var bizErr *BizError
	if !errors.As(err, &bizErr) || bizErr.Code != CodeConflict {
		t.Fatalf("expected conflict BizError, got %T %[1]v", err)
	}
}

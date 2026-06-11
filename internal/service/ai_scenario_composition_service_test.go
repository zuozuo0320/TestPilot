package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"testpilot/internal/model"
	"testpilot/internal/repository"
)

func newTestAIScenarioCompositionService(t *testing.T) (*AIScenarioCompositionService, *repository.AIScenarioCompositionRepo, model.User, model.Project) {
	t.Helper()

	db := testDB(t)
	seedRoles(t, db)
	manager := seedManager(t, db)
	project := seedProject(t, db)
	_, _, projectRepo, _, txMgr := testRepos(db)

	flowRepo := repository.NewAIFlowAssetRepo(db)
	assertionRepo := repository.NewAIAssertionAssetRepo(db)
	refRepo := repository.NewAIAssetReferenceRepo(db)
	scenarioRepo := repository.NewAIScenarioCompositionRepo(db)
	svc := NewAIScenarioCompositionService(
		testLogger(),
		scenarioRepo,
		flowRepo,
		assertionRepo,
		refRepo,
		repository.NewAIScriptRepo(db),
		projectRepo,
		repository.NewUserRepo(db),
		txMgr,
		nil,
		"",
		"",
		"",
	)
	return svc, scenarioRepo, manager, project
}

func seedPublishedFlowAsset(t *testing.T, repo *repository.AIFlowAssetRepo, projectID, userID uint, flowKey string) model.AIFlowAsset {
	t.Helper()

	flow := &model.AIFlowAsset{
		ProjectID:              projectID,
		FlowKey:                flowKey,
		FlowName:               "登录系统",
		Status:                 model.AIFlowAssetStatusPublished,
		InputSchemaJSON:        model.RawJSON(`{}`),
		OutputSchemaJSON:       model.RawJSON(`{}`),
		PreconditionsJSON:      model.RawJSON(`["浏览器已打开登录页"]`),
		PostconditionsJSON:     model.RawJSON(`["登录成功"]`),
		DSLJSON:                model.RawJSON(`{"schema_version":"1.0","generation_steps":[{"action_type":"NAVIGATE","page_url":"${env.BASE_URL}"}]}`),
		CodeSnapshot:           "export async function login() {}",
		TagsJSON:               model.RawJSON(`["登录"]`),
		AllowAIReuse:           true,
		LatestValidationStatus: model.AIValidationStatusPassed,
		CreatedBy:              userID,
		UpdatedBy:              userID,
	}
	if err := repo.Create(t.Context(), nil, flow); err != nil {
		t.Fatalf("create flow asset: %v", err)
	}
	version := &model.AIFlowAssetVersion{
		FlowID:           flow.ID,
		VersionNo:        1,
		VersionStatus:    model.AIFlowAssetStatusPublished,
		DSLJSON:          flow.DSLJSON,
		CodeSnapshot:     flow.CodeSnapshot,
		InputSchemaJSON:  flow.InputSchemaJSON,
		OutputSchemaJSON: flow.OutputSchemaJSON,
		ChangeSummary:    "首次发布",
		ValidationStatus: model.AIValidationStatusPassed,
		CreatedBy:        userID,
	}
	if err := repo.CreateVersion(t.Context(), nil, version); err != nil {
		t.Fatalf("create flow version: %v", err)
	}
	return *flow
}

func seedPublishedAssertionAsset(t *testing.T, repo *repository.AIAssertionAssetRepo, projectID, userID uint, assertionKey string) model.AIAssertionAsset {
	t.Helper()

	assertion := &model.AIAssertionAsset{
		ProjectID:              projectID,
		AssertionKey:           assertionKey,
		AssertionName:          "页面标题存在",
		AssertionType:          model.AIAssertionTypeTextContains,
		ParamSchemaJSON:        model.RawJSON(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`),
		ImplementationJSON:     model.RawJSON(`{"template":"await expect(page.getByText(text)).toBeVisible()"}`),
		FailureMessageTpl:      "页面标题不存在",
		EvidenceConfigJSON:     model.RawJSON(`{"screenshot":"ON_FAILURE","trace":true}`),
		Status:                 model.AIAssertionAssetStatusPublished,
		AllowAIReuse:           true,
		LatestValidationStatus: model.AIValidationStatusPassed,
		CreatedBy:              userID,
		UpdatedBy:              userID,
	}
	if err := repo.Create(t.Context(), nil, assertion); err != nil {
		t.Fatalf("create assertion asset: %v", err)
	}
	return *assertion
}

func assertContainsAll(t *testing.T, text string, fragments []string) {
	t.Helper()

	for _, fragment := range fragments {
		if !strings.Contains(text, fragment) {
			t.Fatalf("expected generated code to contain %q, got:\n%s", fragment, text)
		}
	}
}

func waitForCompositionValidationStatus(t *testing.T, repo *repository.AIScenarioCompositionRepo, compositionID uint, status string) model.AICompositionValidation {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	var latest model.AICompositionValidation
	for time.Now().Before(deadline) {
		validations, err := repo.ListValidations(t.Context(), compositionID)
		if err != nil {
			t.Fatalf("list validations: %v", err)
		}
		if len(validations) > 0 {
			latest = validations[0]
			if latest.Status == status {
				results, resultErr := repo.ListAssertionResults(t.Context(), latest.ID)
				if resultErr != nil {
					t.Fatalf("list assertion results: %v", resultErr)
				}
				latest.AssertionResults = results
				return latest
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("validation did not reach %s, latest=%+v", status, latest)
	return latest
}

func seedPublishedFlowAssetWithDSL(t *testing.T, repo *repository.AIFlowAssetRepo, projectID, userID uint, flowKey string, dsl model.RawJSON) model.AIFlowAsset {
	t.Helper()

	flow := seedPublishedFlowAsset(t, repo, projectID, userID, flowKey)
	if err := repo.UpdateFields(t.Context(), nil, flow.ID, map[string]interface{}{
		"dsl_json": dsl,
	}); err != nil {
		t.Fatalf("update flow dsl: %v", err)
	}
	versions, err := repo.ListVersions(t.Context(), flow.ID)
	if err != nil {
		t.Fatalf("list flow versions: %v", err)
	}
	if len(versions) == 0 {
		t.Fatalf("expected seeded flow version")
	}
	version := versions[0]
	if err := repo.CreateVersion(t.Context(), nil, &model.AIFlowAssetVersion{
		FlowID:           flow.ID,
		VersionNo:        version.VersionNo + 1,
		VersionStatus:    model.AIFlowAssetStatusPublished,
		DSLJSON:          dsl,
		CodeSnapshot:     flow.CodeSnapshot,
		InputSchemaJSON:  flow.InputSchemaJSON,
		OutputSchemaJSON: flow.OutputSchemaJSON,
		ChangeSummary:    "更新测试 DSL",
		ValidationStatus: model.AIValidationStatusPassed,
		CreatedBy:        userID,
	}); err != nil {
		t.Fatalf("create flow dsl version: %v", err)
	}
	flow.DSLJSON = dsl
	return flow
}

func TestAIScenarioCompositionServiceValidateIsIdempotent(t *testing.T) {
	svc, scenarioRepo, manager, project := newTestAIScenarioCompositionService(t)
	flow := seedPublishedFlowAsset(t, svc.flowRepo, project.ID, manager.ID, "login_system")
	assertion := seedPublishedAssertionAsset(t, svc.assertionRepo, project.ID, manager.ID, "title_visible")

	composition, err := svc.Create(t.Context(), manager.ID, ScenarioCompositionCreateInput{
		ProjectID:    project.ID,
		ScenarioKey:  "login_and_check",
		ScenarioName: "登录并检查标题",
		Description:  "复用登录固定场景并插入断言",
	})
	if err != nil {
		t.Fatalf("create composition: %v", err)
	}
	if _, err := svc.AddStep(t.Context(), manager.ID, composition.ID, ScenarioStepSaveInput{
		ProjectID:    project.ID,
		StepType:     model.AIScenarioStepTypeFlowCall,
		StepName:     "登录系统",
		RefFlowID:    &flow.ID,
		ParamMapping: json.RawMessage(`{"username":"${env.ADMIN_USER}"}`),
	}); err != nil {
		t.Fatalf("add flow step: %v", err)
	}
	assertionStep, err := svc.AddStep(t.Context(), manager.ID, composition.ID, ScenarioStepSaveInput{
		ProjectID:      project.ID,
		StepType:       model.AIScenarioStepTypeAssertion,
		StepName:       "检查页面标题",
		RefAssertionID: &assertion.ID,
		ParamMapping:   json.RawMessage(`{"text":"首页"}`),
	})
	if err != nil {
		t.Fatalf("add assertion step: %v", err)
	}
	if _, err := svc.GenerateCode(t.Context(), manager.ID, composition.ID, GenerateCompositionCodeInput{
		ProjectID: project.ID,
		Target:    "PLAYWRIGHT",
	}); err != nil {
		t.Fatalf("generate composition code: %v", err)
	}

	input := ValidateCompositionInput{
		ProjectID:      project.ID,
		Environment:    "test",
		Variables:      json.RawMessage(`{}`),
		IdempotencyKey: "validate-login-and-check",
	}
	first, err := svc.Validate(t.Context(), manager.ID, composition.ID, input)
	if err != nil {
		t.Fatalf("first validate: %v", err)
	}
	if first.Status != model.AICompositionValidationStatusRunning {
		t.Fatalf("expected validation to start asynchronously, got %s", first.Status)
	}
	second, err := svc.Validate(t.Context(), manager.ID, composition.ID, input)
	if err != nil {
		t.Fatalf("second validate: %v", err)
	}
	if first.ID == 0 || first.ID != second.ID {
		t.Fatalf("expected same validation by idempotency key, got first=%d second=%d", first.ID, second.ID)
	}
	finished := waitForCompositionValidationStatus(t, scenarioRepo, composition.ID, model.AICompositionValidationStatusPassed)
	if finished.ID != first.ID {
		t.Fatalf("expected finished validation to keep same id, got first=%d finished=%d", first.ID, finished.ID)
	}
	if len(finished.AssertionResults) != 1 {
		t.Fatalf("expected cached validation with assertion result, got %+v", finished.AssertionResults)
	}
	if finished.AssertionResults[0].StepID != fmt.Sprintf("step_%d", assertionStep.ID) {
		t.Fatalf("expected stable assertion step id, got %s", finished.AssertionResults[0].StepID)
	}
	validations, err := scenarioRepo.ListValidations(t.Context(), composition.ID)
	if err != nil {
		t.Fatalf("list validations: %v", err)
	}
	if len(validations) != 1 {
		t.Fatalf("expected one validation record, got %d", len(validations))
	}
}

func TestAIScenarioCompositionServiceGenerateCodeCompilesFlowDSLAndCustomAssertion(t *testing.T) {
	svc, _, manager, project := newTestAIScenarioCompositionService(t)
	flowDSL := model.RawJSON(`{
		"schema_version":"1.0",
		"generation_steps":[
			{"action_type":"NAVIGATE","page_url":"${env.BASE_URL}"},
			{"action_type":"CLICK","locator":"getByRole('button', { name: '新建任务' })"},
			{"action_type":"INPUT","locator":"getByRole('textbox', { name: '任务名称' })","input_value":"${variables.taskName}"}
		]
	}`)
	flow := seedPublishedFlowAssetWithDSL(t, svc.flowRepo, project.ID, manager.ID, "create_task_flow", flowDSL)
	assertion := seedPublishedAssertionAsset(t, svc.assertionRepo, project.ID, manager.ID, "task_visible")

	composition, err := svc.Create(t.Context(), manager.ID, ScenarioCompositionCreateInput{
		ProjectID:    project.ID,
		ScenarioKey:  "create_task_and_assert",
		ScenarioName: "创建任务并断言",
		Description:  "固定场景 DSL 应编译为真实 Playwright 操作",
	})
	if err != nil {
		t.Fatalf("create composition: %v", err)
	}
	if _, err := svc.AddStep(t.Context(), manager.ID, composition.ID, ScenarioStepSaveInput{
		ProjectID:    project.ID,
		StepType:     model.AIScenarioStepTypeFlowCall,
		StepName:     "创建任务",
		RefFlowID:    &flow.ID,
		ParamMapping: json.RawMessage(`{"taskName":"自动化任务"}`),
	}); err != nil {
		t.Fatalf("add flow step: %v", err)
	}
	if _, err := svc.AddStep(t.Context(), manager.ID, composition.ID, ScenarioStepSaveInput{
		ProjectID:      project.ID,
		StepType:       model.AIScenarioStepTypeAssertion,
		StepName:       "任务可见",
		RefAssertionID: &assertion.ID,
		ParamMapping:   json.RawMessage(`{"text":"自动化任务"}`),
	}); err != nil {
		t.Fatalf("add assertion step: %v", err)
	}

	result, err := svc.GenerateCode(t.Context(), manager.ID, composition.ID, GenerateCompositionCodeInput{
		ProjectID: project.ID,
		Target:    "PLAYWRIGHT",
	})
	if err != nil {
		t.Fatalf("generate composition code: %v", err)
	}
	assertContainsAll(t, result.GeneratedCode, []string{
		"await page.goto(String(resolveScenarioValue(ctx, \"${env.BASE_URL}\")))",
		"await page.getByRole('button', { name: '新建任务' }).click()",
		"await page.getByRole('textbox', { name: '任务名称' }).fill(String(resolveScenarioValue(ctx, \"${variables.taskName}\") ?? ''))",
		"const text = String(params[\"text\"] ?? '')",
		"await expect(page.getByText(text)).toBeVisible()",
	})
	if strings.Contains(result.GeneratedCode, "toBeDefined()") || strings.Contains(result.GeneratedCode, "return {}") {
		t.Fatalf("generated code still contains stub fragments:\n%s", result.GeneratedCode)
	}
}

func TestAIScenarioCompositionServiceDeleteDraftWithoutHistory(t *testing.T) {
	svc, scenarioRepo, manager, project := newTestAIScenarioCompositionService(t)
	composition, err := svc.Create(t.Context(), manager.ID, ScenarioCompositionCreateInput{
		ProjectID:    project.ID,
		ScenarioKey:  "delete_draft_composition",
		ScenarioName: "删除草稿编排",
		Description:  "无版本和验证历史时允许删除",
	})
	if err != nil {
		t.Fatalf("create composition: %v", err)
	}

	if err := svc.Delete(t.Context(), project.ID, composition.ID); err != nil {
		t.Fatalf("delete draft composition: %v", err)
	}
	if _, err := scenarioRepo.GetByID(t.Context(), composition.ID); err == nil {
		t.Fatalf("expected composition to be deleted")
	}
}

func TestAIScenarioCompositionServiceDeleteRejectsHistoricalDraft(t *testing.T) {
	svc, scenarioRepo, manager, project := newTestAIScenarioCompositionService(t)
	composition, err := svc.Create(t.Context(), manager.ID, ScenarioCompositionCreateInput{
		ProjectID:    project.ID,
		ScenarioKey:  "delete_historical_composition",
		ScenarioName: "已有历史的编排",
		Description:  "有验证历史时禁止物理删除",
	})
	if err != nil {
		t.Fatalf("create composition: %v", err)
	}
	if err := scenarioRepo.CreateValidation(t.Context(), nil, &model.AICompositionValidation{
		CompositionID: composition.ID,
		ProjectID:     project.ID,
		Status:        model.AICompositionValidationStatusFailed,
		CreatedBy:     manager.ID,
	}); err != nil {
		t.Fatalf("create validation: %v", err)
	}

	err = svc.Delete(t.Context(), project.ID, composition.ID)
	if err == nil {
		t.Fatalf("expected historical composition delete to fail")
	}
	var bizErr *BizError
	if !errors.As(err, &bizErr) || bizErr.Code != CodeConflict {
		t.Fatalf("expected conflict BizError, got %T %[1]v", err)
	}
}

func TestAIScenarioCompositionServiceUpdateRejectsFutureStepReference(t *testing.T) {
	svc, _, manager, project := newTestAIScenarioCompositionService(t)
	composition, err := svc.Create(t.Context(), manager.ID, ScenarioCompositionCreateInput{
		ProjectID:    project.ID,
		ScenarioKey:  "future_ref",
		ScenarioName: "未来步骤引用",
		Description:  "校验 DSL 引用顺序",
	})
	if err != nil {
		t.Fatalf("create composition: %v", err)
	}

	badDSL := json.RawMessage(fmt.Sprintf(`{
		"schema_version": "1.0",
		"scenario": {"scenario_key": "future_ref", "scenario_name": "未来步骤引用", "project_id": %d},
		"env": ["BASE_URL"],
		"steps": [
			{"id": "step_1", "name": "前置步骤", "type": "ATOMIC_ACTION", "enabled": true, "inputs": {"token": "${steps.step_2.outputs.token}"}, "depends_on": []},
			{"id": "step_2", "name": "未来步骤", "type": "ATOMIC_ACTION", "enabled": true, "inputs": {}, "depends_on": []}
		]
	}`, project.ID))
	_, err = svc.Update(t.Context(), manager.ID, composition.ID, ScenarioCompositionUpdateInput{
		ProjectID:        project.ID,
		ScenarioName:     composition.ScenarioName,
		Description:      composition.Description,
		DSL:              badDSL,
		ExpectedRevision: composition.Revision,
	})
	if err == nil {
		t.Fatalf("expected future step reference to be rejected")
	}
	var bizErr *BizError
	if !errors.As(err, &bizErr) || bizErr.Code != CodeParamsError {
		t.Fatalf("expected params BizError, got %T %[1]v", err)
	}
}

func TestAIScenarioCompositionServiceGenerateRejectsLowConfidenceUnconfirmedStep(t *testing.T) {
	svc, _, manager, project := newTestAIScenarioCompositionService(t)
	flow := seedPublishedFlowAsset(t, svc.flowRepo, project.ID, manager.ID, "login_low_confidence")

	composition, err := svc.Create(t.Context(), manager.ID, ScenarioCompositionCreateInput{
		ProjectID:    project.ID,
		ScenarioKey:  "low_confidence",
		ScenarioName: "低置信度确认",
		Description:  "AI 推荐步骤必须人工确认",
	})
	if err != nil {
		t.Fatalf("create composition: %v", err)
	}
	if _, err := svc.AddStep(t.Context(), manager.ID, composition.ID, ScenarioStepSaveInput{
		ProjectID:    project.ID,
		StepType:     model.AIScenarioStepTypeFlowCall,
		StepName:     "低置信度登录",
		RefFlowID:    &flow.ID,
		AIConfidence: 0.72,
	}); err != nil {
		t.Fatalf("add low confidence step: %v", err)
	}

	_, err = svc.GenerateCode(t.Context(), manager.ID, composition.ID, GenerateCompositionCodeInput{
		ProjectID: project.ID,
		Target:    "PLAYWRIGHT",
	})
	if err == nil {
		t.Fatalf("expected generate to reject low confidence step")
	}
	var bizErr *BizError
	if !errors.As(err, &bizErr) || bizErr.Code != CodeConflict {
		t.Fatalf("expected conflict BizError, got %T %[1]v", err)
	}
}

func TestAIScenarioCompositionServiceManualCodeLockBlocksRegeneration(t *testing.T) {
	svc, _, manager, project := newTestAIScenarioCompositionService(t)
	flowDSL := model.RawJSON(`{
		"schema_version":"1.0",
		"generation_steps":[
			{"action_type":"NAVIGATE","page_url":"${env.BASE_URL}"}
		]
	}`)
	flow := seedPublishedFlowAssetWithDSL(t, svc.flowRepo, project.ID, manager.ID, "locked_login_flow", flowDSL)
	composition, err := svc.Create(t.Context(), manager.ID, ScenarioCompositionCreateInput{
		ProjectID:    project.ID,
		ScenarioKey:  "manual_locked_code",
		ScenarioName: "人工锁定代码",
		Description:  "人工编辑后的代码必须阻止自动覆盖",
	})
	if err != nil {
		t.Fatalf("create composition: %v", err)
	}
	if _, err := svc.AddStep(t.Context(), manager.ID, composition.ID, ScenarioStepSaveInput{
		ProjectID:    project.ID,
		StepType:     model.AIScenarioStepTypeFlowCall,
		StepName:     "锁定登录",
		RefFlowID:    &flow.ID,
		ParamMapping: json.RawMessage(`{"baseUrl":"${env.BASE_URL}"}`),
	}); err != nil {
		t.Fatalf("add flow step: %v", err)
	}
	generated, err := svc.GenerateCode(t.Context(), manager.ID, composition.ID, GenerateCompositionCodeInput{
		ProjectID: project.ID,
		Target:    "PLAYWRIGHT",
	})
	if err != nil {
		t.Fatalf("generate code: %v", err)
	}
	if generated.GeneratedCode == "" {
		t.Fatalf("expected generated code")
	}

	manualCode := generated.GeneratedCode + "\n// manual patch\n"
	patched, err := svc.ManualUpdateCode(t.Context(), manager.ID, composition.ID, ManualUpdateCompositionCodeInput{
		ProjectID:        project.ID,
		GeneratedCode:    manualCode,
		ChangeSummary:    "人工补充等待逻辑",
		Locked:           true,
		ExpectedRevision: composition.Revision + 2,
	})
	if err != nil {
		t.Fatalf("manual update code: %v", err)
	}
	if patched.CodeEditStatus != model.AIScenarioCodeEditStatusLocked {
		t.Fatalf("expected code locked, got %s", patched.CodeEditStatus)
	}
	if !strings.Contains(patched.GeneratedCode, "manual patch") {
		t.Fatalf("expected manual code to be saved")
	}

	_, err = svc.GenerateCode(t.Context(), manager.ID, composition.ID, GenerateCompositionCodeInput{
		ProjectID: project.ID,
		Target:    "PLAYWRIGHT",
	})
	if err == nil {
		t.Fatalf("expected locked code to reject regeneration")
	}
	var bizErr *BizError
	if !errors.As(err, &bizErr) || bizErr.Code != CodeConflict {
		t.Fatalf("expected conflict BizError, got %T %[1]v", err)
	}

	unlocked, err := svc.SetCodeLock(t.Context(), manager.ID, composition.ID, LockCompositionCodeInput{
		ProjectID:     project.ID,
		Locked:        false,
		ChangeSummary: "解除锁定重新生成",
	})
	if err != nil {
		t.Fatalf("unlock code: %v", err)
	}
	if unlocked.CodeEditStatus != model.AIScenarioCodeEditStatusManualPatched {
		t.Fatalf("expected manual patched after unlock, got %s", unlocked.CodeEditStatus)
	}

	regenerated, err := svc.GenerateCode(t.Context(), manager.ID, composition.ID, GenerateCompositionCodeInput{
		ProjectID: project.ID,
		Target:    "PLAYWRIGHT",
	})
	if err != nil {
		t.Fatalf("regenerate after unlock: %v", err)
	}
	if strings.Contains(regenerated.GeneratedCode, "manual patch") {
		t.Fatalf("expected unlocked regeneration to overwrite manual code")
	}
	latest, err := svc.Get(t.Context(), project.ID, composition.ID)
	if err != nil {
		t.Fatalf("get latest composition: %v", err)
	}
	if latest.CodeEditStatus != model.AIScenarioCodeEditStatusAutoGenerated {
		t.Fatalf("expected auto generated after regeneration, got %s", latest.CodeEditStatus)
	}
}

func TestAIScenarioCompositionServiceDiffAndRollbackVersion(t *testing.T) {
	svc, scenarioRepo, manager, project := newTestAIScenarioCompositionService(t)
	flowDSL := model.RawJSON(`{
		"schema_version":"1.0",
		"generation_steps":[
			{"action_type":"NAVIGATE","page_url":"${env.BASE_URL}"}
		]
	}`)
	flow := seedPublishedFlowAssetWithDSL(t, svc.flowRepo, project.ID, manager.ID, "rollback_flow", flowDSL)
	composition, err := svc.Create(t.Context(), manager.ID, ScenarioCompositionCreateInput{
		ProjectID:    project.ID,
		ScenarioKey:  "rollback_composition",
		ScenarioName: "版本回滚编排",
		Description:  "验证版本 Diff 和回滚",
	})
	if err != nil {
		t.Fatalf("create composition: %v", err)
	}
	firstStep, err := svc.AddStep(t.Context(), manager.ID, composition.ID, ScenarioStepSaveInput{
		ProjectID:    project.ID,
		StepType:     model.AIScenarioStepTypeFlowCall,
		StepName:     "打开首页",
		RefFlowID:    &flow.ID,
		ParamMapping: json.RawMessage(`{"baseUrl":"${env.BASE_URL}"}`),
	})
	if err != nil {
		t.Fatalf("add first step: %v", err)
	}
	if firstStep.DSLStepID == "" {
		t.Fatalf("expected stable dsl step id")
	}
	if _, err := svc.GenerateCode(t.Context(), manager.ID, composition.ID, GenerateCompositionCodeInput{
		ProjectID: project.ID,
		Target:    "PLAYWRIGHT",
	}); err != nil {
		t.Fatalf("generate v1 code: %v", err)
	}
	v1Snapshot, err := svc.Get(t.Context(), project.ID, composition.ID)
	if err != nil {
		t.Fatalf("get v1 snapshot: %v", err)
	}
	version1 := &model.AIScenarioCompositionVersion{
		CompositionID: composition.ID,
		VersionNo:     1,
		VersionStatus: model.AIScenarioStatusPublished,
		DSLJSON:       v1Snapshot.DSLJSON,
		GeneratedCode: v1Snapshot.GeneratedCode,
		ChangeSummary: "V1",
		CreatedBy:     manager.ID,
	}
	if err := scenarioRepo.CreateVersion(t.Context(), nil, version1); err != nil {
		t.Fatalf("create version1: %v", err)
	}

	if _, err := svc.AddStep(t.Context(), manager.ID, composition.ID, ScenarioStepSaveInput{
		ProjectID:    project.ID,
		StepType:     model.AIScenarioStepTypeAtomicAction,
		StepName:     "等待稳定",
		AtomicAction: "wait",
		ParamMapping: json.RawMessage(`{"timeout_ms":100}`),
	}); err != nil {
		t.Fatalf("add second step: %v", err)
	}
	if _, err := svc.GenerateCode(t.Context(), manager.ID, composition.ID, GenerateCompositionCodeInput{
		ProjectID: project.ID,
		Target:    "PLAYWRIGHT",
	}); err != nil {
		t.Fatalf("generate v2 code: %v", err)
	}
	v2Snapshot, err := svc.Get(t.Context(), project.ID, composition.ID)
	if err != nil {
		t.Fatalf("get v2 snapshot: %v", err)
	}
	version2 := &model.AIScenarioCompositionVersion{
		CompositionID: composition.ID,
		VersionNo:     2,
		VersionStatus: model.AIScenarioStatusPublished,
		DSLJSON:       v2Snapshot.DSLJSON,
		GeneratedCode: v2Snapshot.GeneratedCode,
		ChangeSummary: "V2",
		CreatedBy:     manager.ID,
	}
	if err := scenarioRepo.CreateVersion(t.Context(), nil, version2); err != nil {
		t.Fatalf("create version2: %v", err)
	}

	diff, err := svc.DiffVersion(t.Context(), composition.ID, ScenarioVersionDiffInput{
		ProjectID:       project.ID,
		BaseVersionID:   version1.ID,
		TargetVersionID: version2.ID,
	})
	if err != nil {
		t.Fatalf("diff versions: %v", err)
	}
	if !diff.DSLChanged || !diff.CodeChanged {
		t.Fatalf("expected dsl and code diff, got %+v", diff)
	}
	if diff.DSLStats.AddedLines == 0 || len(diff.Summary) == 0 {
		t.Fatalf("expected useful diff stats, got %+v", diff.DSLStats)
	}

	locked, err := svc.ManualUpdateCode(t.Context(), manager.ID, composition.ID, ManualUpdateCompositionCodeInput{
		ProjectID:        project.ID,
		GeneratedCode:    v2Snapshot.GeneratedCode + "\n// locked patch",
		ChangeSummary:    "锁定当前代码",
		Locked:           true,
		ExpectedRevision: v2Snapshot.Revision,
	})
	if err != nil {
		t.Fatalf("lock current code: %v", err)
	}
	if locked.CodeEditStatus != model.AIScenarioCodeEditStatusLocked {
		t.Fatalf("expected locked status, got %s", locked.CodeEditStatus)
	}
	_, err = svc.RollbackVersion(t.Context(), manager.ID, composition.ID, ScenarioVersionRollbackInput{
		ProjectID: project.ID,
		VersionID: version1.ID,
	})
	if err == nil {
		t.Fatalf("expected locked rollback to fail")
	}
	var bizErr *BizError
	if !errors.As(err, &bizErr) || bizErr.Code != CodeConflict {
		t.Fatalf("expected conflict BizError, got %T %[1]v", err)
	}

	restored, err := svc.RollbackVersion(t.Context(), manager.ID, composition.ID, ScenarioVersionRollbackInput{
		ProjectID:          project.ID,
		VersionID:          version1.ID,
		OverrideLockedCode: true,
		ChangeSummary:      "覆盖锁定代码并回滚",
	})
	if err != nil {
		t.Fatalf("rollback with override: %v", err)
	}
	if restored.CodeEditStatus != model.AIScenarioCodeEditStatusAutoGenerated {
		t.Fatalf("expected auto generated after rollback, got %s", restored.CodeEditStatus)
	}
	if restored.GeneratedCode != version1.GeneratedCode {
		t.Fatalf("expected generated code restored")
	}
	if restored.CurrentVersionID == nil || *restored.CurrentVersionID != version1.ID {
		t.Fatalf("expected current version id %d, got %v", version1.ID, restored.CurrentVersionID)
	}
	if restored.LatestValidationStatus != model.AIValidationStatusNotValidated {
		t.Fatalf("expected validation reset, got %s", restored.LatestValidationStatus)
	}
	if len(restored.Steps) != 1 {
		t.Fatalf("expected one restored step, got %+v", restored.Steps)
	}
	if restored.Steps[0].DSLStepID != firstStep.DSLStepID {
		t.Fatalf("expected stable step id %s, got %s", firstStep.DSLStepID, restored.Steps[0].DSLStepID)
	}
}

func TestAIScenarioCompositionServiceAIPlanSkipsFailedAssets(t *testing.T) {
	svc, _, manager, project := newTestAIScenarioCompositionService(t)
	task := createPublishableTask(t, svc.aiScriptRepo, project.ID, manager.ID, 4401, model.AIValidationStatusPassed)
	okFlow := seedPublishedFlowAsset(t, svc.flowRepo, project.ID, manager.ID, "login_ok")
	failedFlow := seedPublishedFlowAsset(t, svc.flowRepo, project.ID, manager.ID, "login_failed_asset")
	if err := svc.flowRepo.UpdateFields(t.Context(), nil, failedFlow.ID, map[string]interface{}{
		"latest_validation_status": model.AIValidationStatusFailed,
	}); err != nil {
		t.Fatalf("mark failed flow: %v", err)
	}
	okAssertion := seedPublishedAssertionAsset(t, svc.assertionRepo, project.ID, manager.ID, "title_ok")
	failedAssertion := seedPublishedAssertionAsset(t, svc.assertionRepo, project.ID, manager.ID, "title_failed")
	if err := svc.assertionRepo.UpdateFields(t.Context(), nil, failedAssertion.ID, map[string]interface{}{
		"latest_validation_status": model.AIValidationStatusFailed,
	}); err != nil {
		t.Fatalf("mark failed assertion: %v", err)
	}

	result, err := svc.AIPlanFromTask(t.Context(), AIPlanFromTaskInput{
		ProjectID: project.ID,
		TaskID:    task.ID,
		MaxSteps:  10,
	})
	if err != nil {
		t.Fatalf("ai plan from task: %v", err)
	}
	keys := make(map[string]struct{})
	for _, step := range result.Steps {
		if step.FlowKey != "" {
			keys[step.FlowKey] = struct{}{}
		}
		if step.AssertionKey != "" {
			keys[step.AssertionKey] = struct{}{}
		}
	}
	if _, ok := keys[okFlow.FlowKey]; !ok {
		t.Fatalf("expected passed flow in plan, got %+v", result.Steps)
	}
	if _, ok := keys[okAssertion.AssertionKey]; !ok {
		t.Fatalf("expected passed assertion in plan, got %+v", result.Steps)
	}
	if _, ok := keys[failedFlow.FlowKey]; ok {
		t.Fatalf("failed flow should not be recommended: %+v", result.Steps)
	}
	if _, ok := keys[failedAssertion.AssertionKey]; ok {
		t.Fatalf("failed assertion should not be recommended: %+v", result.Steps)
	}
}

func TestAIScenarioCompositionServiceAIPlanRequiresConfirmedPassedTask(t *testing.T) {
	svc, _, manager, project := newTestAIScenarioCompositionService(t)
	task := createPublishableTask(t, svc.aiScriptRepo, project.ID, manager.ID, 4402, model.AIValidationStatusPassed)
	if err := svc.aiScriptRepo.UpdateTaskFields(t.Context(), task.ID, map[string]interface{}{
		"task_status": model.AITaskStatusPendingConfirm,
	}); err != nil {
		t.Fatalf("update task status: %v", err)
	}

	_, err := svc.AIPlanFromTask(t.Context(), AIPlanFromTaskInput{
		ProjectID: project.ID,
		TaskID:    task.ID,
		MaxSteps:  10,
	})
	if err == nil {
		t.Fatalf("expected unconfirmed task to be rejected")
	}
	var bizErr *BizError
	if !errors.As(err, &bizErr) || bizErr.Code != CodeConflict {
		t.Fatalf("expected conflict BizError, got %T %[1]v", err)
	}

	if err := svc.aiScriptRepo.UpdateTaskFields(t.Context(), task.ID, map[string]interface{}{
		"task_status":              model.AITaskStatusConfirmed,
		"latest_validation_status": model.AIValidationStatusFailed,
	}); err != nil {
		t.Fatalf("update validation status: %v", err)
	}
	_, err = svc.AIPlanFromTask(t.Context(), AIPlanFromTaskInput{
		ProjectID: project.ID,
		TaskID:    task.ID,
		MaxSteps:  10,
	})
	if err == nil {
		t.Fatalf("expected failed validation task to be rejected")
	}
}

func TestAIScenarioCompositionServiceAIPlanFusesConfirmedTaskWithSelectedFlow(t *testing.T) {
	svc, _, manager, project := newTestAIScenarioCompositionService(t)
	task := createPublishableTask(t, svc.aiScriptRepo, project.ID, manager.ID, 4403, model.AIValidationStatusPassed)
	extraTask := createPublishableTask(t, svc.aiScriptRepo, project.ID, manager.ID, 4404, model.AIValidationStatusPassed)
	flowDSL := model.RawJSON(`{
		"schema_version":"1.0",
		"generation_steps":[
			{"step_no":1,"action_type":"NAVIGATE","page_url":"${env.BASE_URL}/assets"},
			{"step_no":2,"action_type":"CLICK","locator":"page.getByRole('button', { name: '新建任务' })"}
		]
	}`)
	flow := seedPublishedFlowAssetWithDSL(t, svc.flowRepo, project.ID, manager.ID, "asset_scan_create_task", flowDSL)
	version, err := svc.aiScriptRepo.GetCurrentScriptVersion(t.Context(), task.ID)
	if err != nil {
		t.Fatalf("get current script version: %v", err)
	}
	version.StepModelJSON = model.JSONMap{
		"generation_steps": []interface{}{
			map[string]interface{}{"step_no": 1, "action_type": "NAVIGATE", "page_url": "${env.BASE_URL}/assets"},
			map[string]interface{}{"step_no": 2, "action_type": "CLICK", "locator": "page.getByRole('button', { name: '新建任务' })"},
			map[string]interface{}{"step_no": 3, "action_type": "INPUT", "locator": "page.getByRole('textbox', { name: '任务名称' })", "input_value": "资产扫描回归"},
		},
	}
	if err := svc.aiScriptRepo.UpdateScriptVersionFields(t.Context(), version.ID, map[string]interface{}{
		"step_model_json": version.StepModelJSON,
	}); err != nil {
		t.Fatalf("update script version step model: %v", err)
	}
	extraVersion, err := svc.aiScriptRepo.GetCurrentScriptVersion(t.Context(), extraTask.ID)
	if err != nil {
		t.Fatalf("get extra script version: %v", err)
	}
	extraVersion.StepModelJSON = model.JSONMap{
		"generation_steps": []interface{}{
			map[string]interface{}{"step_no": 1, "action_type": "CLICK", "locator": "page.getByRole('button', { name: '提交' })"},
		},
	}
	if err := svc.aiScriptRepo.UpdateScriptVersionFields(t.Context(), extraVersion.ID, map[string]interface{}{
		"step_model_json": extraVersion.StepModelJSON,
	}); err != nil {
		t.Fatalf("update extra script version step model: %v", err)
	}

	result, err := svc.AIPlanFromTask(t.Context(), AIPlanFromTaskInput{
		ProjectID:         project.ID,
		TaskID:            task.ID,
		PreferredFlowIDs:  []uint{flow.ID},
		AdditionalTaskIDs: []uint{extraTask.ID},
		OrderedSources: []AIPlanOrderedSourceInput{
			{Type: "FLOW", ID: flow.ID},
			{Type: "TASK", ID: task.ID},
			{Type: "TASK", ID: extraTask.ID},
		},
		MaxSteps: 10,
	})
	if err != nil {
		t.Fatalf("ai plan from task: %v", err)
	}
	if len(result.Steps) != 3 {
		t.Fatalf("expected flow call plus remaining recording steps, got %+v", result.Steps)
	}
	if result.Steps[0].Type != model.AIScenarioStepTypeFlowCall || result.Steps[0].FlowID != flow.ID {
		t.Fatalf("expected first step to reference selected flow, got %+v", result.Steps[0])
	}
	if result.Steps[1].Type != model.AIScenarioStepTypeAtomicAction || result.Steps[1].AtomicAction != "fill" {
		t.Fatalf("expected remaining recording step as fill atomic action, got %+v", result.Steps[1])
	}
	if result.Steps[1].Inputs["selector"] == "" || result.Steps[1].Inputs["value"] == "" {
		t.Fatalf("expected atomic action inputs to preserve recording data, got %+v", result.Steps[1].Inputs)
	}
	if result.Steps[2].Type != model.AIScenarioStepTypeAtomicAction || result.Steps[2].AtomicAction != "click" {
		t.Fatalf("expected additional recording script step as click atomic action, got %+v", result.Steps[2])
	}
}

func TestAIScenarioCompositionServiceAIPlanFallsBackToTracesAndKeepsRemainingSteps(t *testing.T) {
	svc, _, manager, project := newTestAIScenarioCompositionService(t)
	task := createPublishableTask(t, svc.aiScriptRepo, project.ID, manager.ID, 4405, model.AIValidationStatusPassed)
	version, err := svc.aiScriptRepo.GetCurrentScriptVersion(t.Context(), task.ID)
	if err != nil {
		t.Fatalf("get current script version: %v", err)
	}
	if err := svc.aiScriptRepo.UpdateScriptVersionFields(t.Context(), version.ID, map[string]interface{}{
		"step_model_json": model.JSONMap{},
	}); err != nil {
		t.Fatalf("clear script version step model: %v", err)
	}
	if err := svc.aiScriptRepo.BatchCreateTraces(t.Context(), []model.AIScriptTrace{
		{TaskID: task.ID, TraceNo: 1, ActionType: "NAVIGATE", PageURL: "https://foradar.baimaohui.net/workbench", ActionResult: "success"},
		{TaskID: task.ID, TraceNo: 2, ActionType: "CLICK", LocatorUsed: "locator('span').filter({ hasText: '任务管理' }).first()", ActionResult: "success"},
		{TaskID: task.ID, TraceNo: 3, ActionType: "CLICK", LocatorUsed: "locator('span').filter({ hasText: '资产探知' }).first()", ActionResult: "success"},
		{TaskID: task.ID, TraceNo: 4, ActionType: "CLICK", LocatorUsed: "getByText('查看任务').nth(2)", ActionResult: "success"},
		{TaskID: task.ID, TraceNo: 5, ActionType: "CLICK", LocatorUsed: "getByRole('button', { name: '新建任务' })", ActionResult: "success"},
		{TaskID: task.ID, TraceNo: 6, ActionType: "CLICK", LocatorUsed: "getByRole('button', { name: 'Close' })", ActionResult: "success"},
		{TaskID: task.ID, TraceNo: 7, ActionType: "CLICK", LocatorUsed: "locator('.el-table__row.hover-row .cell').first()", ActionResult: "success"},
		{TaskID: task.ID, TraceNo: 8, ActionType: "CLICK", LocatorUsed: "getByRole('button', { name: '新建任务' })", ActionResult: "success"},
		{TaskID: task.ID, TraceNo: 9, ActionType: "INPUT", LocatorUsed: "locator('textarea')", InputValueMasked: "11***06", ActionResult: "success"},
		{TaskID: task.ID, TraceNo: 10, ActionType: "CLICK", LocatorUsed: "getByRole('button', { name: '确定' })", ActionResult: "success"},
	}); err != nil {
		t.Fatalf("create traces: %v", err)
	}
	flowDSL := model.RawJSON(`{
		"schema_version":"1.0",
		"generation_steps":[
			{"step_no":1,"action_type":"NAVIGATE","page_url":"https://foradar.baimaohui.net/workbench"},
			{"step_no":2,"action_type":"CLICK","locator":"locator('span').filter({ hasText: '任务管理' }).first()"},
			{"step_no":3,"action_type":"CLICK","locator":"getByText('资产探知')"},
			{"step_no":4,"action_type":"CLICK","locator":"getByText('查看任务').nth(2)"},
			{"step_no":5,"action_type":"CLICK","locator":"getByRole('button', { name: '新建任务' })"}
		]
	}`)
	flow := seedPublishedFlowAssetWithDSL(t, svc.flowRepo, project.ID, manager.ID, "asset_scan_create_from_trace", flowDSL)

	result, err := svc.AIPlanFromTask(t.Context(), AIPlanFromTaskInput{
		ProjectID: project.ID,
		TaskID:    task.ID,
		OrderedSources: []AIPlanOrderedSourceInput{
			{Type: "FLOW", ID: flow.ID},
			{Type: "TASK", ID: task.ID},
		},
		MaxSteps: 20,
	})
	if err != nil {
		t.Fatalf("ai plan from task: %v", err)
	}
	if len(result.Steps) != 5 {
		t.Fatalf("expected flow call plus stable remaining trace actions, got %+v", result.Steps)
	}
	if result.Steps[0].Type != model.AIScenarioStepTypeFlowCall || result.Steps[0].FlowID != flow.ID {
		t.Fatalf("expected first step to reference selected flow, got %+v", result.Steps[0])
	}
	expectedActions := []string{"click", "click", "fill", "click"}
	for index, expectedAction := range expectedActions {
		step := result.Steps[index+1]
		if step.Type != model.AIScenarioStepTypeAtomicAction || step.AtomicAction != expectedAction {
			t.Fatalf("expected remaining step %d as %s atomic action, got %+v", index+1, expectedAction, step)
		}
	}
	if result.Steps[3].Inputs["value"] != "自动化验证任务" {
		t.Fatalf("expected masked input to be replaced with stable value, got %+v", result.Steps[3].Inputs)
	}
	foundTraceFallbackWarning := false
	for _, warning := range result.Warnings {
		if strings.Contains(warning, "已回退使用录制轨迹融合") {
			foundTraceFallbackWarning = true
		}
	}
	if !foundTraceFallbackWarning {
		t.Fatalf("expected trace fallback warning, got %+v", result.Warnings)
	}
}

func TestAIScenarioCompositionServiceGenerateCodeRejectsPartialFlow(t *testing.T) {
	svc, _, manager, project := newTestAIScenarioCompositionService(t)
	partialDSL := model.RawJSON(`{
		"schema_version":"1.0",
		"generation_steps":[
			{"action_type":"NAVIGATE","page_url":"${env.BASE_URL}"},
			{"action_type":"HOVER","locator":"getByRole('button')"}
		]
	}`)
	flow := seedPublishedFlowAssetWithDSL(t, svc.flowRepo, project.ID, manager.ID, "partial_flow", partialDSL)

	composition, err := svc.Create(t.Context(), manager.ID, ScenarioCompositionCreateInput{
		ProjectID:    project.ID,
		ScenarioKey:  "partial_flow_scenario",
		ScenarioName: "引用存量 PARTIAL 资产",
		Description:  "未显式确认时生成代码必须硬错误",
	})
	if err != nil {
		t.Fatalf("create composition: %v", err)
	}
	if _, err := svc.AddStep(t.Context(), manager.ID, composition.ID, ScenarioStepSaveInput{
		ProjectID: project.ID,
		StepType:  model.AIScenarioStepTypeFlowCall,
		StepName:  "引用 PARTIAL 固定场景",
		RefFlowID: &flow.ID,
	}); err != nil {
		t.Fatalf("add flow step: %v", err)
	}

	_, err = svc.GenerateCode(t.Context(), manager.ID, composition.ID, GenerateCompositionCodeInput{
		ProjectID: project.ID,
		Target:    "PLAYWRIGHT",
	})
	if err == nil {
		t.Fatalf("expected generate code to fail for partial flow")
	}
	var bizErr *BizError
	if !errors.As(err, &bizErr) {
		t.Fatalf("expected BizError, got %T", err)
	}
	if bizErr.Code != CodeAICompositionFlowCompileFailed {
		t.Fatalf("unexpected error code: %d", bizErr.Code)
	}
	if !strings.Contains(bizErr.Message, "partial_flow") || !strings.Contains(bizErr.Message, "步骤 2") {
		t.Fatalf("expected message to locate flow_key and step no, got %q", bizErr.Message)
	}
	data, ok := bizErr.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected structured data, got %T", bizErr.Data)
	}
	if data["flow_key"] != "partial_flow" {
		t.Fatalf("expected flow_key in data, got %+v", data)
	}

	result, err := svc.GenerateCode(t.Context(), manager.ID, composition.ID, GenerateCompositionCodeInput{
		ProjectID:      project.ID,
		Target:         "PLAYWRIGHT",
		ConfirmPartial: true,
	})
	if err != nil {
		t.Fatalf("expected confirm_partial generate to succeed, got %v", err)
	}
	foundSkipWarning := false
	for _, warning := range result.Warnings {
		if strings.Contains(warning, "已跳过") {
			foundSkipWarning = true
		}
	}
	if !foundSkipWarning {
		t.Fatalf("expected skip warning when confirming partial flow, got %+v", result.Warnings)
	}
}

func TestAIScenarioCompositionServiceConfirmPartialWritesAuditLog(t *testing.T) {
	svc, _, manager, project := newTestAIScenarioCompositionService(t)
	partialDSL := model.RawJSON(`{
		"schema_version":"1.0",
		"generation_steps":[
			{"action_type":"NAVIGATE","page_url":"${env.BASE_URL}"},
			{"action_type":"HOVER","locator":"getByRole('button')"}
		]
	}`)
	flow := seedPublishedFlowAssetWithDSL(t, svc.flowRepo, project.ID, manager.ID, "audit_partial_flow", partialDSL)

	composition, err := svc.Create(t.Context(), manager.ID, ScenarioCompositionCreateInput{
		ProjectID:    project.ID,
		ScenarioKey:  "audit_partial_scenario",
		ScenarioName: "确认 PARTIAL 审计",
		Description:  "确认引用 PARTIAL 资产时必须记录审计日志",
	})
	if err != nil {
		t.Fatalf("create composition: %v", err)
	}
	if _, err := svc.AddStep(t.Context(), manager.ID, composition.ID, ScenarioStepSaveInput{
		ProjectID: project.ID,
		StepType:  model.AIScenarioStepTypeFlowCall,
		StepName:  "引用 PARTIAL 固定场景",
		RefFlowID: &flow.ID,
	}); err != nil {
		t.Fatalf("add flow step: %v", err)
	}

	if _, err := svc.GenerateCode(t.Context(), manager.ID, composition.ID, GenerateCompositionCodeInput{
		ProjectID:      project.ID,
		Target:         "PLAYWRIGHT",
		ConfirmPartial: true,
	}); err != nil {
		t.Fatalf("confirm_partial generate: %v", err)
	}

	logs, err := svc.aiScriptRepo.ListOperationLogsByType(t.Context(), model.AIScriptOperationConfirmPartial)
	if err != nil {
		t.Fatalf("list operation logs: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected one confirm_partial audit log, got %d", len(logs))
	}
	log := logs[0]
	if log.OperatorID != manager.ID {
		t.Fatalf("expected operator %d, got %d", manager.ID, log.OperatorID)
	}
	if !strings.Contains(log.OperationDesc, composition.ScenarioKey) || !strings.Contains(log.OperationDesc, flow.FlowKey) {
		t.Fatalf("expected desc to contain scenario and flow key, got %q", log.OperationDesc)
	}
	if len([]rune(log.OperationDesc)) > 500 {
		t.Fatalf("expected desc truncated to 500 runes, got %d", len([]rune(log.OperationDesc)))
	}
}

func TestAIScenarioCompositionServiceRefreshFlowRefs(t *testing.T) {
	svc, scenarioRepo, manager, project := newTestAIScenarioCompositionService(t)
	flow := seedPublishedFlowAsset(t, svc.flowRepo, project.ID, manager.ID, "refresh_target_flow")

	composition, err := svc.Create(t.Context(), manager.ID, ScenarioCompositionCreateInput{
		ProjectID:    project.ID,
		ScenarioKey:  "refresh_flow_refs",
		ScenarioName: "升级引用版本",
		Description:  "FLOW_CALL 锁定版本升级到最新发布版本",
	})
	if err != nil {
		t.Fatalf("create composition: %v", err)
	}
	step, err := svc.AddStep(t.Context(), manager.ID, composition.ID, ScenarioStepSaveInput{
		ProjectID: project.ID,
		StepType:  model.AIScenarioStepTypeFlowCall,
		StepName:  "调用固定场景",
		RefFlowID: &flow.ID,
	})
	if err != nil {
		t.Fatalf("add flow step: %v", err)
	}
	if step.RefFlowVersionID == nil {
		t.Fatalf("expected step to lock flow version")
	}
	lockedVersionID := *step.RefFlowVersionID

	latest := &model.AIFlowAssetVersion{
		FlowID:           flow.ID,
		VersionNo:        2,
		VersionStatus:    model.AIFlowAssetStatusPublished,
		DSLJSON:          flow.DSLJSON,
		CodeSnapshot:     flow.CodeSnapshot,
		InputSchemaJSON:  flow.InputSchemaJSON,
		OutputSchemaJSON: flow.OutputSchemaJSON,
		ChangeSummary:    "发布 v2",
		ValidationStatus: model.AIValidationStatusPassed,
		CreatedBy:        manager.ID,
	}
	if err := svc.flowRepo.CreateVersion(t.Context(), nil, latest); err != nil {
		t.Fatalf("create flow v2: %v", err)
	}

	stale, err := svc.Get(t.Context(), project.ID, composition.ID)
	if err != nil {
		t.Fatalf("get stale composition: %v", err)
	}
	if len(stale.OutdatedFlowRefs) != 1 || stale.OutdatedFlowRefs[0].TargetID != flow.ID {
		t.Fatalf("expected one outdated flow ref, got %+v", stale.OutdatedFlowRefs)
	}

	otherFlowID := flow.ID + 999
	unchanged, err := svc.RefreshFlowRefs(t.Context(), manager.ID, composition.ID, RefreshFlowRefsInput{
		ProjectID: project.ID,
		FlowIDs:   []uint{otherFlowID},
	})
	if err != nil {
		t.Fatalf("refresh with non-matching filter: %v", err)
	}
	if len(unchanged.Steps) != 1 || *unchanged.Steps[0].RefFlowVersionID != lockedVersionID {
		t.Fatalf("expected filtered refresh to keep locked version, got %+v", unchanged.Steps)
	}

	refreshed, err := svc.RefreshFlowRefs(t.Context(), manager.ID, composition.ID, RefreshFlowRefsInput{
		ProjectID: project.ID,
	})
	if err != nil {
		t.Fatalf("refresh flow refs: %v", err)
	}
	if len(refreshed.Steps) != 1 || refreshed.Steps[0].RefFlowVersionID == nil {
		t.Fatalf("expected refreshed step with locked version, got %+v", refreshed.Steps)
	}
	if *refreshed.Steps[0].RefFlowVersionID != latest.ID {
		t.Fatalf("expected version upgraded to %d, got %d", latest.ID, *refreshed.Steps[0].RefFlowVersionID)
	}
	if len(refreshed.OutdatedFlowRefs) != 0 {
		t.Fatalf("expected no outdated refs after refresh, got %+v", refreshed.OutdatedFlowRefs)
	}

	if err := scenarioRepo.UpdateFields(t.Context(), nil, composition.ID, map[string]interface{}{
		"status": model.AIScenarioStatusArchived,
	}); err != nil {
		t.Fatalf("archive composition: %v", err)
	}
	_, err = svc.RefreshFlowRefs(t.Context(), manager.ID, composition.ID, RefreshFlowRefsInput{ProjectID: project.ID})
	if err == nil {
		t.Fatalf("expected archived composition refresh to fail")
	}
	var bizErr *BizError
	if !errors.As(err, &bizErr) || bizErr.Code != CodeConflict {
		t.Fatalf("expected conflict BizError, got %T %[1]v", err)
	}
}

func TestCompileAtomicActionTable(t *testing.T) {
	cases := []struct {
		name          string
		action        string
		params        string
		wantErr       bool
		wantFragments []string
	}{
		{
			name:          "select 生成 selectOption",
			action:        "select",
			params:        `{"selector":"#city","value":"beijing"}`,
			wantFragments: []string{"selectOption(String(inputs.value ?? ''))", "{ selected: true }"},
		},
		{
			name:    "select 缺少 selector 报错",
			action:  "select",
			params:  `{"value":"beijing"}`,
			wantErr: true,
		},
		{
			name:          "press 生成 press",
			action:        "press",
			params:        `{"selector":"#search","key":"Enter"}`,
			wantFragments: []string{".press(String(inputs.key ?? inputs.value ?? ''))", "{ pressed: true }"},
		},
		{
			name:    "press 缺少 key 报错",
			action:  "press",
			params:  `{"selector":"#search"}`,
			wantErr: true,
		},
		{
			name:    "press 缺少 selector 报错",
			action:  "press",
			params:  `{"key":"Enter"}`,
			wantErr: true,
		},
		{
			name:          "wait_for 生成定位器等待",
			action:        "wait_for",
			params:        `{"selector":"#result","timeout_ms":3000}`,
			wantFragments: []string{".waitFor({ state: 'visible', timeout: Number(inputs.timeout_ms || 5000) })", "{ waited: true }"},
		},
		{
			name:          "Playwright 表达式选择器不降级为 CSS",
			action:        "click",
			params:        `{"selector":"getByRole('button', { name: '确定' })"}`,
			wantFragments: []string{"await page.getByRole('button', { name: '确定' }).click()", "{ clicked: true }"},
		},
		{
			name:    "wait_for 缺少 selector 报错",
			action:  "wait_for",
			params:  `{"timeout_ms":3000}`,
			wantErr: true,
		},
		{
			name:    "未知原子操作报错",
			action:  "hover",
			params:  `{"selector":"#btn"}`,
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			step := model.AIScenarioStep{
				AtomicAction:     tc.action,
				ParamMappingJSON: model.RawJSON(tc.params),
			}
			line, err := compileAtomicAction(step, tc.params, "{}", "step_1")
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got code:\n%s", line)
				}
				var bizErr *BizError
				if !errors.As(err, &bizErr) || bizErr.Code != CodeParamsError {
					t.Fatalf("expected params BizError, got %T %[1]v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("compile atomic action: %v", err)
			}
			assertContainsAll(t, line, tc.wantFragments)
		})
	}
}

func TestCompileFlowDSLProducesStructuredFailures(t *testing.T) {
	cases := []struct {
		name       string
		dsl        string
		wantNo     int
		wantType   string
		wantReason string
	}{
		{
			name:       "缺少动作类型",
			dsl:        `{"steps":[{"page_url":"https://example.com"}]}`,
			wantNo:     1,
			wantType:   "",
			wantReason: "缺少动作类型",
		},
		{
			name:       "不支持的类型",
			dsl:        `{"steps":[{"type":"NAVIGATE","page_url":"x"},{"type":"HOVER"}]}`,
			wantNo:     2,
			wantType:   "HOVER",
			wantReason: "动作类型 HOVER 暂不支持",
		},
		{
			name:       "FLOW_CALL 缺少引用",
			dsl:        `{"steps":[{"type":"FLOW_CALL"}]}`,
			wantNo:     1,
			wantType:   "FLOW_CALL",
			wantReason: "缺少 flow_key 或 flow_id 引用",
		},
		{
			name:       "非法 JSON",
			dsl:        `{invalid`,
			wantNo:     0,
			wantType:   "",
			wantReason: "固定场景 DSL 为空或不是合法 JSON",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			failures := dryRunCompileFlowDSL(model.RawJSON(tc.dsl), nil)
			if len(failures) == 0 {
				t.Fatalf("expected failures")
			}
			failure := failures[len(failures)-1]
			if failure.StepNo != tc.wantNo || failure.StepType != tc.wantType || failure.Reason != tc.wantReason {
				t.Fatalf("unexpected failure: %+v", failure)
			}
		})
	}
}

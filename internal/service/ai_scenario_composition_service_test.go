package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

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
		DSLJSON:                model.RawJSON(`{"schema_version":"1.0","steps":[]}`),
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
	second, err := svc.Validate(t.Context(), manager.ID, composition.ID, input)
	if err != nil {
		t.Fatalf("second validate: %v", err)
	}
	if first.ID == 0 || first.ID != second.ID {
		t.Fatalf("expected same validation by idempotency key, got first=%d second=%d", first.ID, second.ID)
	}
	if len(second.AssertionResults) != 1 {
		t.Fatalf("expected cached validation with assertion result, got %+v", second.AssertionResults)
	}
	if second.AssertionResults[0].StepID != fmt.Sprintf("step_%d", assertionStep.ID) {
		t.Fatalf("expected stable assertion step id, got %s", second.AssertionResults[0].StepID)
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

package service

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"testpilot/internal/model"
	"testpilot/internal/repository"
)

func newTestAIFlowAssetService(t *testing.T) (*AIFlowAssetService, *repository.AIFlowAssetRepo, *repository.AIScriptRepo, model.User, model.Project) {
	t.Helper()

	db := testDB(t)
	seedRoles(t, db)
	manager := seedManager(t, db)
	project := seedProject(t, db)
	_, _, projectRepo, _, txMgr := testRepos(db)

	aiScriptRepo := repository.NewAIScriptRepo(db)
	flowRepo := repository.NewAIFlowAssetRepo(db)
	refRepo := repository.NewAIAssetReferenceRepo(db)
	svc := NewAIFlowAssetService(
		testLogger(),
		flowRepo,
		refRepo,
		aiScriptRepo,
		projectRepo,
		repository.NewUserRepo(db),
		txMgr,
	)
	return svc, flowRepo, aiScriptRepo, manager, project
}

func createPublishableTask(t *testing.T, repo *repository.AIScriptRepo, projectID, userID, taskID uint, validationStatus string) model.AIScriptTask {
	t.Helper()

	task := &model.AIScriptTask{
		ID:                     taskID,
		ProjectID:              projectID,
		ProjectKey:             "project_1",
		TaskName:               "登录系统",
		GenerationMode:         model.AIGenerationModeRecordingEnhanced,
		ScenarioDesc:           "录制登录系统流程",
		StartURL:               "https://example.com/login",
		TaskStatus:             model.AITaskStatusConfirmed,
		FrameworkType:          "Playwright",
		LatestValidationStatus: validationStatus,
		CreatedBy:              userID,
		UpdatedBy:              userID,
	}
	if err := repo.CreateTask(t.Context(), task); err != nil {
		t.Fatalf("create ai script task: %v", err)
	}

	version := &model.AIScriptVersion{
		TaskID:           task.ID,
		VersionNo:        1,
		FrameworkType:    "Playwright",
		ScriptName:       "login.spec.ts",
		ScriptStatus:     model.AIScriptStatusConfirmed,
		ValidationStatus: validationStatus,
		SourceType:       model.AISourceTypeAIEnhancedFromRecording,
		ScriptContent:    "import { test } from '@playwright/test'\n",
		StepModelJSON:    model.JSONMap{"steps": []interface{}{"goto", "fill", "click"}},
		IsCurrentFlag:    true,
		CreatedBy:        userID,
		UpdatedBy:        userID,
	}
	if err := repo.CreateScriptVersion(t.Context(), version); err != nil {
		t.Fatalf("create script version: %v", err)
	}
	if err := repo.UpdateTaskFields(t.Context(), task.ID, map[string]interface{}{
		"current_script_version_id": version.ID,
	}); err != nil {
		t.Fatalf("update task current version: %v", err)
	}
	return *task
}

func validPublishInput(projectID uint, flowKey string) PublishFlowAssetInput {
	return PublishFlowAssetInput{
		ProjectID:      projectID,
		FlowKey:        flowKey,
		FlowName:       "登录系统",
		Description:    "复用登录流程",
		Tags:           []string{"登录", "基础流程"},
		InputSchema:    json.RawMessage(`{"type":"object"}`),
		OutputSchema:   json.RawMessage(`{"type":"object"}`),
		Preconditions:  []string{"浏览器已打开登录页"},
		Postconditions: []string{"登录成功进入首页"},
		AllowAIReuse:   true,
		ChangeSummary:  "首次发布",
	}
}

func validSaveFlowInput(projectID uint, flowKey string, dsl json.RawMessage) SaveFlowAssetInput {
	return SaveFlowAssetInput{
		ProjectID:      projectID,
		FlowKey:        flowKey,
		FlowName:       "固定场景" + flowKey,
		Description:    "用于固定场景引用治理测试",
		Tags:           []string{"治理"},
		InputSchema:    json.RawMessage(`{"type":"object"}`),
		OutputSchema:   json.RawMessage(`{"type":"object"}`),
		Preconditions:  []string{"前置条件已满足"},
		Postconditions: []string{"后置条件已满足"},
		DSL:            dsl,
		CodeSnapshot:   "export async function flow() {}",
		AllowAIReuse:   true,
		ChangeSummary:  "测试发布",
	}
}

func emptyFlowDSL() json.RawMessage {
	return json.RawMessage(`{"schema_version":"1.0","steps":[{"type":"NAVIGATE","page_url":"${env.BASE_URL}"}]}`)
}

func flowCallDSL(flowKey string) json.RawMessage {
	return json.RawMessage(`{"schema_version":"1.0","steps":[{"type":"FLOW_CALL","ref":{"flow_key":"` + flowKey + `"}}]}`)
}

func createAndPublishFlow(t *testing.T, svc *AIFlowAssetService, userID, projectID uint, flowKey string, dsl json.RawMessage) model.AIFlowAsset {
	t.Helper()

	flow, err := svc.Create(t.Context(), userID, validSaveFlowInput(projectID, flowKey, dsl))
	if err != nil {
		t.Fatalf("create flow %s: %v", flowKey, err)
	}
	if _, err := svc.Publish(t.Context(), userID, projectID, flow.ID, "发布 "+flowKey); err != nil {
		t.Fatalf("publish flow %s: %v", flowKey, err)
	}
	flow, err = svc.Get(t.Context(), projectID, flow.ID)
	if err != nil {
		t.Fatalf("get flow %s: %v", flowKey, err)
	}
	return *flow
}

func TestAIFlowAssetServicePublishFromTaskSuccess(t *testing.T) {
	svc, flowRepo, aiScriptRepo, manager, project := newTestAIFlowAssetService(t)
	task := createPublishableTask(t, aiScriptRepo, project.ID, manager.ID, 4101, model.AIValidationStatusPassed)

	result, err := svc.PublishFromTask(t.Context(), manager.ID, task.ID, validPublishInput(project.ID, "login_system"))
	if err != nil {
		t.Fatalf("publish flow asset: %v", err)
	}
	if result.FlowID == 0 || result.FlowVersionID == 0 || result.Status != model.AIFlowAssetStatusPublished {
		t.Fatalf("unexpected publish result: %+v", result)
	}

	flow, err := flowRepo.GetByID(t.Context(), result.FlowID)
	if err != nil {
		t.Fatalf("query flow: %v", err)
	}
	if flow.FlowKey != "login_system" || flow.LatestValidationStatus != model.AIValidationStatusPassed {
		t.Fatalf("unexpected flow: %+v", flow)
	}

	versions, err := flowRepo.ListVersions(t.Context(), flow.ID)
	if err != nil {
		t.Fatalf("query flow versions: %v", err)
	}
	if len(versions) != 1 || versions[0].VersionNo != 1 {
		t.Fatalf("unexpected versions: %+v", versions)
	}
}

func TestAIFlowAssetServicePublishRejectsUnvalidatedScript(t *testing.T) {
	svc, _, aiScriptRepo, manager, project := newTestAIFlowAssetService(t)
	task := createPublishableTask(t, aiScriptRepo, project.ID, manager.ID, 4201, model.AIValidationStatusFailed)

	_, err := svc.PublishFromTask(t.Context(), manager.ID, task.ID, validPublishInput(project.ID, "login_failed"))
	if err == nil {
		t.Fatalf("expected publish to fail")
	}
	var bizErr *BizError
	if !errors.As(err, &bizErr) {
		t.Fatalf("expected BizError, got %T", err)
	}
	if bizErr.Code != CodeConflict {
		t.Fatalf("unexpected error code: %d", bizErr.Code)
	}
}

func TestAIFlowAssetServicePublishRejectsDuplicateKey(t *testing.T) {
	svc, _, aiScriptRepo, manager, project := newTestAIFlowAssetService(t)
	task := createPublishableTask(t, aiScriptRepo, project.ID, manager.ID, 4301, model.AIValidationStatusPassed)

	input := validPublishInput(project.ID, "login_duplicate")
	if _, err := svc.PublishFromTask(t.Context(), manager.ID, task.ID, input); err != nil {
		t.Fatalf("first publish flow asset: %v", err)
	}
	_, err := svc.PublishFromTask(t.Context(), manager.ID, task.ID, input)
	if err == nil {
		t.Fatalf("expected duplicate publish to fail")
	}
	var bizErr *BizError
	if !errors.As(err, &bizErr) {
		t.Fatalf("expected BizError, got %T", err)
	}
	if bizErr.Code != CodeConflict {
		t.Fatalf("unexpected error code: %d", bizErr.Code)
	}
}

func TestAIFlowAssetServiceDeleteDraftOnlyWhenUnreferenced(t *testing.T) {
	svc, flowRepo, _, manager, project := newTestAIFlowAssetService(t)
	flow, err := svc.Create(t.Context(), manager.ID, validSaveFlowInput(project.ID, "delete_draft", emptyFlowDSL()))
	if err != nil {
		t.Fatalf("create draft flow: %v", err)
	}

	if err := svc.Delete(t.Context(), project.ID, flow.ID); err != nil {
		t.Fatalf("delete draft flow: %v", err)
	}
	if _, err := flowRepo.GetByID(t.Context(), flow.ID); err == nil {
		t.Fatalf("expected flow to be deleted")
	}
}

func TestAIFlowAssetServiceDeleteRejectsPublishedOrReferenced(t *testing.T) {
	svc, _, _, manager, project := newTestAIFlowAssetService(t)
	published := createAndPublishFlow(t, svc, manager.ID, project.ID, "delete_published", emptyFlowDSL())

	if err := svc.Delete(t.Context(), project.ID, published.ID); err == nil {
		t.Fatalf("expected published flow delete to fail")
	}

	draft, err := svc.Create(t.Context(), manager.ID, validSaveFlowInput(project.ID, "delete_referenced", emptyFlowDSL()))
	if err != nil {
		t.Fatalf("create referenced draft flow: %v", err)
	}
	if err := svc.refRepo.ReplaceForSource(t.Context(), nil, model.AIAssetRefSourceScenario, 999, []model.AIAssetReference{{
		SourceType: model.AIAssetRefSourceScenario,
		SourceID:   999,
		TargetType: model.AIAssetRefTargetFlow,
		TargetID:   draft.ID,
	}}); err != nil {
		t.Fatalf("create artificial flow reference: %v", err)
	}
	err = svc.Delete(t.Context(), project.ID, draft.ID)
	if err == nil {
		t.Fatalf("expected referenced flow delete to fail")
	}
	var bizErr *BizError
	if !errors.As(err, &bizErr) || bizErr.Code != CodeConflict {
		t.Fatalf("expected conflict BizError, got %T %[1]v", err)
	}
}

func TestAIFlowAssetServiceReferencesIncludeFlowCallSources(t *testing.T) {
	svc, _, _, manager, project := newTestAIFlowAssetService(t)
	base := createAndPublishFlow(t, svc, manager.ID, project.ID, "base_login", emptyFlowDSL())
	caller := createAndPublishFlow(t, svc, manager.ID, project.ID, "caller_login", flowCallDSL(base.FlowKey))

	refs, err := svc.References(t.Context(), project.ID, base.ID)
	if err != nil {
		t.Fatalf("query references: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("expected one reference, got %+v", refs)
	}
	if refs[0].SourceType != model.AIAssetRefSourceFlow || refs[0].SourceID != caller.ID || refs[0].TargetID != base.ID {
		t.Fatalf("unexpected flow reference: %+v", refs[0])
	}
	if refs[0].TargetVersionID == nil || *refs[0].TargetVersionID == 0 {
		t.Fatalf("expected locked flow version reference: %+v", refs[0])
	}
}

func TestAIFlowAssetServiceRejectsFlowReferenceCycle(t *testing.T) {
	svc, _, _, manager, project := newTestAIFlowAssetService(t)
	base := createAndPublishFlow(t, svc, manager.ID, project.ID, "cycle_base", emptyFlowDSL())
	caller := createAndPublishFlow(t, svc, manager.ID, project.ID, "cycle_caller", flowCallDSL(base.FlowKey))

	_, err := svc.Update(t.Context(), manager.ID, base.ID, validSaveFlowInput(project.ID, base.FlowKey, flowCallDSL(caller.FlowKey)))
	if err == nil {
		t.Fatalf("expected cycle update to fail")
	}
	var bizErr *BizError
	if !errors.As(err, &bizErr) || bizErr.Code != CodeConflict {
		t.Fatalf("expected conflict BizError, got %T %[1]v", err)
	}
}

func TestAIFlowAssetServiceRejectsFlowReferenceDepthOverThree(t *testing.T) {
	svc, _, _, manager, project := newTestAIFlowAssetService(t)
	leaf := createAndPublishFlow(t, svc, manager.ID, project.ID, "depth_leaf", emptyFlowDSL())
	level2 := createAndPublishFlow(t, svc, manager.ID, project.ID, "depth_level_two", flowCallDSL(leaf.FlowKey))
	level1 := createAndPublishFlow(t, svc, manager.ID, project.ID, "depth_level_one", flowCallDSL(level2.FlowKey))
	root := createAndPublishFlow(t, svc, manager.ID, project.ID, "depth_root", flowCallDSL(level1.FlowKey))

	_, err := svc.Create(t.Context(), manager.ID, validSaveFlowInput(project.ID, "depth_too_deep", flowCallDSL(root.FlowKey)))
	if err == nil {
		t.Fatalf("expected depth over three to fail")
	}
	var bizErr *BizError
	if !errors.As(err, &bizErr) || bizErr.Code != CodeConflict {
		t.Fatalf("expected conflict BizError, got %T %[1]v", err)
	}
}

func TestAIFlowAssetServicePublishCompileGate(t *testing.T) {
	cases := []struct {
		name        string
		flowKey     string
		dsl         json.RawMessage
		wantErr     bool
		wantReasons []string
	}{
		{
			name:    "合法 DSL 允许发布",
			flowKey: "gate_ok",
			dsl:     json.RawMessage(`{"schema_version":"1.0","steps":[{"type":"NAVIGATE","page_url":"${env.BASE_URL}"},{"type":"CLICK","locator":"getByRole('button')"}]}`),
		},
		{
			name:        "缺少动作类型拒绝发布",
			flowKey:     "gate_missing_type",
			dsl:         json.RawMessage(`{"schema_version":"1.0","steps":[{"page_url":"https://example.com"}]}`),
			wantErr:     true,
			wantReasons: []string{"缺少动作类型"},
		},
		{
			name:        "不支持的步骤类型拒绝发布",
			flowKey:     "gate_unsupported",
			dsl:         json.RawMessage(`{"schema_version":"1.0","steps":[{"type":"HOVER","locator":"getByRole('button')"}]}`),
			wantErr:     true,
			wantReasons: []string{"动作类型 HOVER 暂不支持"},
		},
		{
			name:        "必填字段缺失拒绝发布",
			flowKey:     "gate_missing_field",
			dsl:         json.RawMessage(`{"schema_version":"1.0","steps":[{"type":"CLICK"}]}`),
			wantErr:     true,
			wantReasons: []string{"缺少点击选择器"},
		},
		{
			name:        "多条失败逐条列出",
			flowKey:     "gate_multi",
			dsl:         json.RawMessage(`{"schema_version":"1.0","steps":[{"type":"NAVIGATE"},{"type":"INPUT","input_value":"abc"}]}`),
			wantErr:     true,
			wantReasons: []string{"步骤 1", "缺少跳转 URL", "步骤 2", "缺少输入选择器"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, _, manager, project := newTestAIFlowAssetService(t)
			flow, err := svc.Create(t.Context(), manager.ID, validSaveFlowInput(project.ID, tc.flowKey, tc.dsl))
			if err != nil {
				t.Fatalf("create draft flow: %v", err)
			}
			_, err = svc.Publish(t.Context(), manager.ID, project.ID, flow.ID, "发布")
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("expected publish to succeed, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected publish to be rejected")
			}
			var bizErr *BizError
			if !errors.As(err, &bizErr) {
				t.Fatalf("expected BizError, got %T", err)
			}
			if bizErr.Code != CodeAIFlowCompileFailed {
				t.Fatalf("unexpected error code: %d", bizErr.Code)
			}
			for _, reason := range tc.wantReasons {
				if !strings.Contains(bizErr.Message, reason) {
					t.Fatalf("expected message to contain %q, got %q", reason, bizErr.Message)
				}
			}
			data, ok := bizErr.Data.(map[string]interface{})
			if !ok {
				t.Fatalf("expected structured data, got %T", bizErr.Data)
			}
			failures, ok := data["compile_failures"].([]model.FlowCompileFailure)
			if !ok || len(failures) == 0 {
				t.Fatalf("expected compile_failures in data, got %+v", data)
			}
			if failures[0].StepNo == 0 || failures[0].Reason == "" {
				t.Fatalf("expected populated failure fields, got %+v", failures[0])
			}
		})
	}
}

func TestAIFlowAssetServiceCompileCheck(t *testing.T) {
	cases := []struct {
		name         string
		flowKey      string
		dsl          json.RawMessage
		wantHealth   string
		wantFailures int
	}{
		{
			name:       "合法 DSL 返回 OK",
			flowKey:    "check_ok",
			dsl:        json.RawMessage(`{"schema_version":"1.0","steps":[{"type":"NAVIGATE","page_url":"${env.BASE_URL}"}]}`),
			wantHealth: model.AIFlowCompileHealthOK,
		},
		{
			name:         "不可编译步骤返回 PARTIAL",
			flowKey:      "check_partial",
			dsl:          json.RawMessage(`{"schema_version":"1.0","steps":[{"type":"HOVER"},{"type":"CLICK"}]}`),
			wantHealth:   model.AIFlowCompileHealthPartial,
			wantFailures: 2,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, _, manager, project := newTestAIFlowAssetService(t)
			flow, err := svc.Create(t.Context(), manager.ID, validSaveFlowInput(project.ID, tc.flowKey, tc.dsl))
			if err != nil {
				t.Fatalf("create draft flow: %v", err)
			}
			result, err := svc.CompileCheck(t.Context(), project.ID, flow.ID)
			if err != nil {
				t.Fatalf("compile check: %v", err)
			}
			if result.FlowID != flow.ID {
				t.Fatalf("unexpected flow id: %d", result.FlowID)
			}
			if result.CompileHealth != tc.wantHealth {
				t.Fatalf("expected health %s, got %s", tc.wantHealth, result.CompileHealth)
			}
			if len(result.CompileFailures) != tc.wantFailures {
				t.Fatalf("expected %d failures, got %+v", tc.wantFailures, result.CompileFailures)
			}
			if len(result.SupportedStepTypes) == 0 {
				t.Fatalf("expected supported step types list")
			}
		})
	}
}

func TestAIFlowAssetServiceCompileCheckPermission(t *testing.T) {
	svc, _, _, manager, project := newTestAIFlowAssetService(t)
	flow, err := svc.Create(t.Context(), manager.ID, validSaveFlowInput(project.ID, "check_perm", emptyFlowDSL()))
	if err != nil {
		t.Fatalf("create draft flow: %v", err)
	}

	_, err = svc.CompileCheck(t.Context(), project.ID+999, flow.ID)
	var bizErr *BizError
	if !errors.As(err, &bizErr) || bizErr.Code != CodeForbidden {
		t.Fatalf("expected forbidden BizError, got %T %[1]v", err)
	}

	_, err = svc.CompileCheck(t.Context(), project.ID, flow.ID+999)
	if !errors.As(err, &bizErr) || bizErr.Code != CodeNotFound {
		t.Fatalf("expected not found BizError, got %T %[1]v", err)
	}
}

func TestAIFlowAssetServiceGetMarksCompileHealth(t *testing.T) {
	svc, _, _, manager, project := newTestAIFlowAssetService(t)

	healthy, err := svc.Create(t.Context(), manager.ID, validSaveFlowInput(project.ID, "health_ok", emptyFlowDSL()))
	if err != nil {
		t.Fatalf("create healthy flow: %v", err)
	}
	if healthy.CompileHealth != model.AIFlowCompileHealthOK || len(healthy.CompileFailures) != 0 {
		t.Fatalf("expected OK health on create, got %s %+v", healthy.CompileHealth, healthy.CompileFailures)
	}

	partial, err := svc.Create(t.Context(), manager.ID, validSaveFlowInput(project.ID, "health_partial", json.RawMessage(`{"schema_version":"1.0","steps":[{"type":"HOVER"}]}`)))
	if err != nil {
		t.Fatalf("create partial flow: %v", err)
	}
	if partial.CompileHealth != model.AIFlowCompileHealthPartial || len(partial.CompileFailures) != 1 {
		t.Fatalf("expected PARTIAL health on create, got %s %+v", partial.CompileHealth, partial.CompileFailures)
	}

	fetched, err := svc.Get(t.Context(), project.ID, partial.ID)
	if err != nil {
		t.Fatalf("get partial flow: %v", err)
	}
	if fetched.CompileHealth != model.AIFlowCompileHealthPartial || len(fetched.CompileFailures) != 1 {
		t.Fatalf("expected PARTIAL health on get, got %s %+v", fetched.CompileHealth, fetched.CompileFailures)
	}
}

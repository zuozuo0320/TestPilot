package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"testpilot/internal/model"
	"testpilot/internal/repository"
)

// newTestPlannerService 构建带 AIModelConfigService 与可注入执行服务地址的编排服务。
func newTestPlannerService(t *testing.T, executorURL string, seedActiveModel bool) (*AIScenarioCompositionService, model.User, model.Project) {
	t.Helper()

	db := testDB(t)
	seedRoles(t, db)
	manager := seedManager(t, db)
	project := seedProject(t, db)
	_, _, projectRepo, _, txMgr := testRepos(db)

	modelRepo := repository.NewAIModelConfigRepo(db)
	if seedActiveModel {
		if err := modelRepo.Create(t.Context(), &model.AIModelConfig{
			Provider: "openai",
			Name:     "测试模型",
			ModelID:  "gpt-test",
			APIKey:   "sk-test",
			IsActive: true,
		}, nil); err != nil {
			t.Fatalf("seed active model: %v", err)
		}
	}
	aiModelSvc := NewAIModelConfigService(modelRepo, txMgr, executorURL, "test-key", testLogger())

	svc := NewAIScenarioCompositionService(
		testLogger(),
		repository.NewAIScenarioCompositionRepo(db),
		repository.NewAIFlowAssetRepo(db),
		repository.NewAIAssertionAssetRepo(db),
		repository.NewAIAssetReferenceRepo(db),
		repository.NewAIScriptRepo(db),
		projectRepo,
		repository.NewUserRepo(db),
		txMgr,
		aiModelSvc,
		executorURL,
		executorURL,
		"test-key",
	)
	return svc, manager, project
}

// newPlannerExecutorStub 启动同时处理 /config/model 与 /execute/plan 的假执行服务。
func newPlannerExecutorStub(t *testing.T, planHandler http.HandlerFunc) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/config/model", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/execute/plan", planHandler)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func plannerSuccessResponse(plan map[string]interface{}) []byte {
	body, _ := json.Marshal(map[string]interface{}{
		"success": true,
		"plan":    plan,
		"model":   "gpt-test",
		"usage": map[string]int{
			"prompt_tokens":     120,
			"completion_tokens": 80,
			"total_tokens":      200,
		},
		"error_message": "",
	})
	return body
}

func TestAIPlanFromTaskPlannerModes(t *testing.T) {
	cases := []struct {
		name            string
		plannerMode     string
		seedActiveModel bool
		planHandler     http.HandlerFunc
		wantUsed        string
		wantDegradedSub string
		wantSummaryPref bool
	}{
		{
			name:            "heuristic 模式跳过 LLM",
			plannerMode:     "heuristic",
			seedActiveModel: true,
			planHandler: func(w http.ResponseWriter, _ *http.Request) {
				t.Error("heuristic 模式不应调用 /execute/plan")
			},
			wantUsed: plannerUsedHeuristic,
		},
		{
			name:            "激活模型未配置时降级",
			plannerMode:     "auto",
			seedActiveModel: false,
			planHandler: func(w http.ResponseWriter, _ *http.Request) {
				t.Error("模型未配置时不应调用 /execute/plan")
			},
			wantUsed:        plannerUsedHeuristic,
			wantDegradedSub: fmt.Sprintf("[%d]", CodeAIPlannerModelNotConfigured),
			wantSummaryPref: true,
		},
		{
			name:            "输出连续两次非法时降级",
			plannerMode:     "llm",
			seedActiveModel: true,
			planHandler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"success":true,"plan":{"summary":""},"model":"gpt-test","usage":{},"error_message":""}`))
			},
			wantUsed:        plannerUsedHeuristic,
			wantDegradedSub: fmt.Sprintf("[%d]", CodeAIPlannerOutputInvalid),
			wantSummaryPref: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := newPlannerExecutorStub(t, tc.planHandler)
			svc, manager, project := newTestPlannerService(t, server.URL, tc.seedActiveModel)
			task := createPublishableTask(t, svc.aiScriptRepo, project.ID, manager.ID, 5501, model.AIValidationStatusPassed)
			seedPublishedFlowAsset(t, svc.flowRepo, project.ID, manager.ID, "login_planner")

			result, err := svc.AIPlanFromTask(t.Context(), AIPlanFromTaskInput{
				ProjectID:   project.ID,
				TaskID:      task.ID,
				PlannerMode: tc.plannerMode,
				OperatorID:  manager.ID,
			})
			if err != nil {
				t.Fatalf("AIPlanFromTask: %v", err)
			}
			if result.PlannerUsed != tc.wantUsed {
				t.Fatalf("expected planner_used %q, got %q", tc.wantUsed, result.PlannerUsed)
			}
			if tc.wantDegradedSub == "" && result.DegradedReason != "" {
				t.Fatalf("expected empty degraded_reason, got %q", result.DegradedReason)
			}
			if tc.wantDegradedSub != "" && !strings.Contains(result.DegradedReason, tc.wantDegradedSub) {
				t.Fatalf("expected degraded_reason to contain %q, got %q", tc.wantDegradedSub, result.DegradedReason)
			}
			if tc.wantSummaryPref && !strings.HasPrefix(result.Summary, "【启发式降级】") {
				t.Fatalf("expected summary degraded prefix, got %q", result.Summary)
			}

			logs, err := svc.aiScriptRepo.ListOperationLogsByType(t.Context(), model.AIScriptOperationAIPlan)
			if err != nil {
				t.Fatalf("list operation logs: %v", err)
			}
			if len(logs) != 1 {
				t.Fatalf("expected 1 AI_PLAN operation log, got %d", len(logs))
			}
			var desc map[string]interface{}
			if err := json.Unmarshal([]byte(logs[0].OperationDesc), &desc); err != nil {
				t.Fatalf("operation desc should be JSON: %v", err)
			}
			if desc["planner_used"] != tc.wantUsed {
				t.Fatalf("expected log planner_used %q, got %v", tc.wantUsed, desc["planner_used"])
			}
		})
	}
}

func TestAIPlanFromTaskLLMSuccess(t *testing.T) {
	var captured plannerExecutorRequest
	server := newPlannerExecutorStub(t, func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Errorf("decode planner request: %v", err)
			return
		}
		flowID := captured.Candidates[0].ID
		versionID := captured.Candidates[0].VersionID
		_, _ = w.Write(plannerSuccessResponse(map[string]interface{}{
			"plan_id":    "plan_llm_1",
			"summary":    "LLM 规划：复用登录固定场景",
			"confidence": 0.9,
			"steps": []map[string]interface{}{
				{
					"type":            model.AIScenarioStepTypeFlowCall,
					"flow_id":         flowID,
					"flow_version_id": versionID,
					"confidence":      0.9,
					"reason":          "录制包含登录流程",
					"inputs":          map[string]interface{}{"username": "${env.ADMIN_USER}"},
				},
				{
					"type":            model.AIScenarioStepTypeFlowCall,
					"flow_id":         flowID + 999,
					"flow_version_id": versionID,
					"confidence":      0.9,
					"reason":          "幻觉引用",
				},
				{
					"type":       model.AIScenarioStepTypeAtomicAction,
					"confidence": 0.8,
					"reason":     "敏感明文",
					"inputs":     map[string]interface{}{"password": "plain-secret-123"},
				},
			},
			"warnings": []string{},
		}))
	})
	svc, manager, project := newTestPlannerService(t, server.URL, true)
	task := createPublishableTask(t, svc.aiScriptRepo, project.ID, manager.ID, 5502, model.AIValidationStatusPassed)
	flow := seedPublishedFlowAsset(t, svc.flowRepo, project.ID, manager.ID, "login_llm")

	result, err := svc.AIPlanFromTask(t.Context(), AIPlanFromTaskInput{
		ProjectID:  project.ID,
		TaskID:     task.ID,
		OperatorID: manager.ID,
	})
	if err != nil {
		t.Fatalf("AIPlanFromTask: %v", err)
	}
	if result.PlannerUsed != plannerUsedLLM {
		t.Fatalf("expected planner_used LLM, got %q (degraded: %q)", result.PlannerUsed, result.DegradedReason)
	}
	if result.DegradedReason != "" {
		t.Fatalf("expected no degraded reason, got %q", result.DegradedReason)
	}
	if len(result.Steps) != 1 || result.Steps[0].FlowID != flow.ID {
		t.Fatalf("expected only the valid flow step adopted, got %+v", result.Steps)
	}
	joined := strings.Join(result.Warnings, "；")
	if !strings.Contains(joined, "不存在的固定场景") || !strings.Contains(joined, "敏感参数") {
		t.Fatalf("expected anti-hallucination and sensitive warnings, got %q", joined)
	}
	if len(captured.EnvKeys) != len(allowedCompositionEnvKeys) {
		t.Fatalf("expected env whitelist keys, got %v", captured.EnvKeys)
	}

	logs, err := svc.aiScriptRepo.ListOperationLogsByType(t.Context(), model.AIScriptOperationAIPlan)
	if err != nil || len(logs) != 1 {
		t.Fatalf("expected 1 AI_PLAN log, got %d (err=%v)", len(logs), err)
	}
	var desc map[string]interface{}
	if err := json.Unmarshal([]byte(logs[0].OperationDesc), &desc); err != nil {
		t.Fatalf("operation desc should be JSON: %v", err)
	}
	if desc["model"] != "gpt-test" || desc["total_tokens"] != float64(200) {
		t.Fatalf("expected model/token info in log, got %v", desc)
	}
}

func TestAIPlanFromTaskLLMTimeoutDegrades(t *testing.T) {
	server := newPlannerExecutorStub(t, func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(300 * time.Millisecond)
		_, _ = w.Write(plannerSuccessResponse(map[string]interface{}{}))
	})
	svc, manager, project := newTestPlannerService(t, server.URL, true)
	svc.plannerTimeout = 50 * time.Millisecond
	task := createPublishableTask(t, svc.aiScriptRepo, project.ID, manager.ID, 5503, model.AIValidationStatusPassed)
	seedPublishedFlowAsset(t, svc.flowRepo, project.ID, manager.ID, "login_timeout")

	result, err := svc.AIPlanFromTask(t.Context(), AIPlanFromTaskInput{
		ProjectID:  project.ID,
		TaskID:     task.ID,
		OperatorID: manager.ID,
	})
	if err != nil {
		t.Fatalf("AIPlanFromTask: %v", err)
	}
	if result.PlannerUsed != plannerUsedHeuristic {
		t.Fatalf("expected heuristic degrade, got %q", result.PlannerUsed)
	}
	if !strings.Contains(result.DegradedReason, fmt.Sprintf("[%d]", CodeAIPlannerTimeout)) {
		t.Fatalf("expected timeout degraded reason, got %q", result.DegradedReason)
	}
	if !strings.HasPrefix(result.Summary, "【启发式降级】") {
		t.Fatalf("expected degraded summary prefix, got %q", result.Summary)
	}
}

func TestValidateLLMPlanWeighting(t *testing.T) {
	svc := &AIScenarioCompositionService{}
	flow := model.AIFlowAsset{ID: 11, FlowKey: "login"}
	candidates := []aiPlanCandidate{
		{
			Flow: &flow,
			Step: AIPlanStep{Type: model.AIScenarioStepTypeFlowCall, FlowID: 11, FlowVersionID: 21, FlowKey: "login", Confidence: 0.8},
		},
	}
	raw := json.RawMessage(`{
		"plan_id": "plan_x",
		"summary": "测试加权",
		"confidence": 1,
		"steps": [
			{"type": "FLOW_CALL", "flow_id": 11, "flow_version_id": 21, "confidence": 1, "reason": "匹配"}
		],
		"warnings": []
	}`)
	result, err := svc.validateLLMPlan(1, raw, candidates, 10)
	if err != nil {
		t.Fatalf("validateLLMPlan: %v", err)
	}
	// 0.7*1 + 0.3*0.8 = 0.94
	if result.Steps[0].Confidence != 0.94 {
		t.Fatalf("expected weighted step confidence 0.94, got %v", result.Steps[0].Confidence)
	}
	if result.Confidence != 0.94 {
		t.Fatalf("expected weighted plan confidence 0.94, got %v", result.Confidence)
	}
	if result.Steps[0].FlowKey != "login" {
		t.Fatalf("expected flow_key backfilled, got %q", result.Steps[0].FlowKey)
	}
}

func TestValidateLLMPlanRejectsInvalidPayloads(t *testing.T) {
	svc := &AIScenarioCompositionService{}
	cases := []struct {
		name string
		raw  string
	}{
		{"未知字段", `{"plan_id":"p","summary":"s","confidence":0.9,"steps":[{"type":"AI_GENERATED","confidence":0.5,"reason":"r"}],"warnings":[],"extra":1}`},
		{"summary 为空", `{"plan_id":"p","summary":"","confidence":0.9,"steps":[{"type":"AI_GENERATED","confidence":0.5,"reason":"r"}],"warnings":[]}`},
		{"confidence 越界", `{"plan_id":"p","summary":"s","confidence":1.5,"steps":[{"type":"AI_GENERATED","confidence":0.5,"reason":"r"}],"warnings":[]}`},
		{"steps 为空", `{"plan_id":"p","summary":"s","confidence":0.9,"steps":[],"warnings":[]}`},
		{"全部步骤被剔除", `{"plan_id":"p","summary":"s","confidence":0.9,"steps":[{"type":"FLOW_CALL","flow_id":999,"flow_version_id":1,"confidence":0.5,"reason":"幻觉"}],"warnings":[]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.validateLLMPlan(1, json.RawMessage(tc.raw), nil, 10); err == nil {
				t.Fatalf("expected validation error for %s", tc.name)
			}
		})
	}
}

func TestValidatePlanStepInputs(t *testing.T) {
	cases := []struct {
		name    string
		inputs  map[string]interface{}
		wantBad bool
	}{
		{"合法表达式", map[string]interface{}{"username": "${env.ADMIN_USER}", "name": "${variables.name}"}, false},
		{"白名单外环境变量", map[string]interface{}{"key": "${env.SECRET_TOKEN}"}, true},
		{"不支持的表达式", map[string]interface{}{"key": "${magic.value}"}, true},
		{"敏感明文", map[string]interface{}{"password": "plain-text-pass"}, true},
		{"敏感引用表达式", map[string]interface{}{"password": "${env.ADMIN_PASSWORD}"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reason := validatePlanStepInputs(tc.inputs)
			if tc.wantBad && reason == "" {
				t.Fatalf("expected invalid inputs for %s", tc.name)
			}
			if !tc.wantBad && reason != "" {
				t.Fatalf("expected valid inputs, got %q", reason)
			}
		})
	}
}

func TestSanitizeRecordingSteps(t *testing.T) {
	stepModel := model.JSONMap{
		"steps": []interface{}{
			map[string]interface{}{"action_type": "input", "target": "密码输入框", "value": "real-password-123", "url": "https://example.com/login?token=abc"},
			map[string]interface{}{"actionType": "INPUT", "target": "用户名", "value": "admin"},
			map[string]interface{}{"type": "click", "target": "登录按钮"},
		},
	}
	steps := sanitizeRecordingSteps(stepModel)
	if len(steps) != 3 {
		t.Fatalf("expected 3 sanitized steps, got %d", len(steps))
	}
	if steps[0].Value != "${env.ADMIN_PASSWORD}" {
		t.Fatalf("expected password redacted, got %q", steps[0].Value)
	}
	if steps[0].URLPath != "/login" {
		t.Fatalf("expected URL path only, got %q", steps[0].URLPath)
	}
	if steps[1].Value != "${env.ADMIN_USER}" {
		t.Fatalf("expected username redacted, got %q", steps[1].Value)
	}
	if steps[2].ActionType != "CLICK" {
		t.Fatalf("expected normalized action type, got %q", steps[2].ActionType)
	}
}

func TestBuildPlannerContextTop30Prefilter(t *testing.T) {
	svc := &AIScenarioCompositionService{}
	candidates := make([]aiPlanCandidate, 0, 40)
	for i := 0; i < 40; i++ {
		flow := model.AIFlowAsset{ID: uint(i + 1), FlowKey: fmt.Sprintf("flow_%d", i+1), FlowName: "流程"}
		candidates = append(candidates, aiPlanCandidate{
			Score: float64(i),
			Flow:  &flow,
			Step:  AIPlanStep{Type: model.AIScenarioStepTypeFlowCall, FlowID: flow.ID, FlowVersionID: flow.ID},
		})
	}
	task := &model.AIScriptTask{TaskName: "任务", ScenarioDesc: "描述", StartURL: "https://example.com/home"}
	version := &model.AIScriptVersion{}
	ctx := svc.buildPlannerContext(task, version, candidates, 12)
	if len(ctx.Candidates) != plannerCandidateBudget {
		t.Fatalf("expected %d prefiltered candidates, got %d", plannerCandidateBudget, len(ctx.Candidates))
	}
	if ctx.Candidates[0].ID != 40 {
		t.Fatalf("expected highest score candidate first, got id %d", ctx.Candidates[0].ID)
	}
	if ctx.StartURLPath != "/home" {
		t.Fatalf("expected sanitized start url path, got %q", ctx.StartURLPath)
	}
}

func TestNormalizePlannerMode(t *testing.T) {
	cases := map[string]string{
		"":          plannerModeAuto,
		"auto":      plannerModeAuto,
		"LLM":       plannerModeLLM,
		"heuristic": plannerModeHeuristic,
		"unknown":   plannerModeAuto,
	}
	for input, want := range cases {
		if got := normalizePlannerMode(input); got != want {
			t.Fatalf("normalizePlannerMode(%q) = %q, want %q", input, got, want)
		}
	}
}

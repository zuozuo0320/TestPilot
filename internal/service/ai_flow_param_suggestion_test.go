package service

import (
	"encoding/json"
	"strings"
	"testing"

	"testpilot/internal/model"
)

func suggestionStepModel() model.JSONMap {
	return model.JSONMap{"steps": []interface{}{
		map[string]interface{}{"type": "GOTO", "url": "https://example.com/login"},
		map[string]interface{}{"type": "FILL", "selector": "#username", "semantic_name": "用户名输入框", "value": "admin"},
		map[string]interface{}{"type": "FILL", "selector": "#password", "semantic_name": "密码输入框", "value": "secret123"},
		map[string]interface{}{"type": "FILL", "selector": "#order-no", "semantic_name": "订单编号", "value": "20260610123456"},
		map[string]interface{}{"type": "FILL", "selector": "#keyword", "semantic_name": "搜索关键词", "value": "${inputs.keyword}"},
		map[string]interface{}{"type": "CLICK", "selector": "#submit"},
	}}
}

func TestAnalyzeFlowParamsSuggestions(t *testing.T) {
	result := analyzeFlowParams(suggestionStepModel())

	if len(result.Suggestions) != 3 {
		t.Fatalf("expected 3 suggestions, got %d: %+v", len(result.Suggestions), result.Suggestions)
	}

	byName := map[string]FlowParamSuggestion{}
	for _, item := range result.Suggestions {
		byName[item.Name] = item
	}

	baseURL, ok := byName["base_url"]
	if !ok || baseURL.Kind != FlowParamKindInput || baseURL.Example != "https://example.com/login" {
		t.Fatalf("base_url suggestion missing or wrong: %+v", byName)
	}
	username, ok := byName["username"]
	if !ok || username.Kind != FlowParamKindInput || username.Example != "admin" {
		t.Fatalf("username suggestion missing or wrong: %+v", byName)
	}
	password, ok := byName["PASSWORD"]
	if !ok || password.Kind != FlowParamKindEnv || !strings.Contains(password.Example, "${env.") {
		t.Fatalf("password env suggestion missing or wrong: %+v", byName)
	}

	if len(result.Excluded) != 1 || !strings.Contains(result.Excluded[0].Reason, "临时 ID") {
		t.Fatalf("expected runtime ID exclusion, got %+v", result.Excluded)
	}
}

func TestAnalyzeFlowParamsInputSchema(t *testing.T) {
	result := analyzeFlowParams(suggestionStepModel())

	var schema map[string]interface{}
	if err := json.Unmarshal(result.InputSchema, &schema); err != nil {
		t.Fatalf("input schema invalid json: %v", err)
	}
	if schema["type"] != "object" {
		t.Fatalf("schema type should be object, got %v", schema["type"])
	}
	properties, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("schema properties missing: %v", schema)
	}
	if _, ok := properties["base_url"]; !ok {
		t.Fatalf("schema should contain base_url: %v", properties)
	}
	if _, ok := properties["username"]; !ok {
		t.Fatalf("schema should contain username: %v", properties)
	}
	if _, ok := properties["PASSWORD"]; ok {
		t.Fatalf("env suggestion should not enter input schema: %v", properties)
	}
}

func TestAnalyzeFlowParamsEmptyStepModel(t *testing.T) {
	result := analyzeFlowParams(nil)
	if len(result.Suggestions) != 0 || len(result.Excluded) != 0 {
		t.Fatalf("expected empty result, got %+v", result)
	}
	var schema map[string]interface{}
	if err := json.Unmarshal(result.InputSchema, &schema); err != nil {
		t.Fatalf("input schema invalid json: %v", err)
	}
}

func TestDeriveParamName(t *testing.T) {
	cases := []struct {
		target string
		want   string
	}{
		{"用户名输入框", "username"},
		{"邮箱地址", "email"},
		{"#order-no input", "order_no_input"},
		{"按钮", "step_3_fill"},
	}
	for _, tc := range cases {
		if got := deriveParamName(tc.target, 3, "FILL"); got != tc.want {
			t.Fatalf("deriveParamName(%q) = %q, want %q", tc.target, got, tc.want)
		}
	}
}

func TestSuggestParamsFromTask(t *testing.T) {
	svc, _, aiScriptRepo, _, project := newTestAIFlowAssetService(t)
	task := createPublishableTask(t, aiScriptRepo, project.ID, 1, 4301, model.AIValidationStatusPassed)

	result, err := svc.SuggestParamsFromTask(t.Context(), project.ID, task.ID)
	if err != nil {
		t.Fatalf("suggest params: %v", err)
	}
	if result == nil || result.Suggestions == nil || result.InputSchema == nil {
		t.Fatalf("unexpected nil fields in result: %+v", result)
	}

	if _, err := svc.SuggestParamsFromTask(t.Context(), project.ID, 999999); err == nil {
		t.Fatal("expected not found error for missing task")
	}
	if _, err := svc.SuggestParamsFromTask(t.Context(), project.ID+1, task.ID); err == nil {
		t.Fatal("expected forbidden error for project mismatch")
	}
}

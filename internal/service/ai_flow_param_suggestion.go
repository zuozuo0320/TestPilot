// ai_flow_param_suggestion.go — 发布固定场景前的可参数化字段自动分析（需求 10.1/10.3）
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"gorm.io/gorm"

	"testpilot/internal/model"
)

const (
	// FlowParamKindInput 建议抽取为固定场景入参。
	FlowParamKindInput = "INPUT"
	// FlowParamKindEnv 敏感值建议改用 ${env.*} 引用，不进入入参 Schema。
	FlowParamKindEnv = "ENV"
)

// FlowParamSuggestion 表示一个可参数化字段建议。
type FlowParamSuggestion struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	ValueType  string `json:"value_type"`
	Example    string `json:"example"`
	StepNo     int    `json:"step_no"`
	ActionType string `json:"action_type"`
	Target     string `json:"target"`
	Reason     string `json:"reason"`
}

// FlowParamExclusion 表示按 10.3 抽取原则被排除的字段及原因。
type FlowParamExclusion struct {
	StepNo     int    `json:"step_no"`
	ActionType string `json:"action_type"`
	Target     string `json:"target"`
	Reason     string `json:"reason"`
}

// FlowParamSuggestionResult 是参数分析的整体输出，input_schema 可直接用作发布入参。
type FlowParamSuggestionResult struct {
	Suggestions []FlowParamSuggestion `json:"suggestions"`
	Excluded    []FlowParamExclusion  `json:"excluded"`
	InputSchema json.RawMessage       `json:"input_schema"`
}

var runtimeIDPattern = regexp.MustCompile(`^(\d{8,}|[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})$`)

var paramNameTranslations = []struct {
	keywords []string
	name     string
}{
	{[]string{"password", "密码"}, "password"},
	{[]string{"username", "用户名", "账号", "账户", "工号"}, "username"},
	{[]string{"验证码", "captcha"}, "captcha"},
	{[]string{"手机", "phone", "mobile"}, "phone"},
	{[]string{"邮箱", "email"}, "email"},
	{[]string{"名称", "name"}, "name"},
	{[]string{"搜索", "search", "keyword", "关键"}, "keyword"},
	{[]string{"日期", "时间", "date", "time"}, "date"},
	{[]string{"金额", "amount", "price", "价格"}, "amount"},
	{[]string{"备注", "描述", "remark", "description"}, "remark"},
}

// SuggestParamsFromTask 分析录制任务当前脚本版本的步骤模型，产出可参数化字段建议。
func (s *AIFlowAssetService) SuggestParamsFromTask(ctx context.Context, projectID, taskID uint) (*FlowParamSuggestionResult, error) {
	task, err := s.aiScriptRepo.GetTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound(CodeNotFound, "测试智编任务不存在")
		}
		return nil, ErrInternal(CodeInternal, err)
	}
	if task.ProjectID != projectID {
		return nil, ErrForbidden(CodeForbidden, "任务不属于当前项目")
	}
	version, err := s.aiScriptRepo.GetCurrentScriptVersion(ctx, taskID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrConflict(CodeConflict, "任务暂无当前脚本版本，无法分析参数")
		}
		return nil, ErrInternal(CodeInternal, err)
	}
	return analyzeFlowParams(version.StepModelJSON), nil
}

// analyzeFlowParams 按 10.3 抽取原则遍历步骤模型：优先抽取 URL 与业务输入值，
// 敏感值改为 env 引用建议，运行时临时 ID 排除并说明原因。
func analyzeFlowParams(stepModel model.JSONMap) *FlowParamSuggestionResult {
	result := &FlowParamSuggestionResult{
		Suggestions: []FlowParamSuggestion{},
		Excluded:    []FlowParamExclusion{},
	}
	rawSteps, _ := stepModel["steps"].([]interface{})
	usedNames := map[string]int{}
	sawBaseURL := false
	for index, raw := range rawSteps {
		step := objectFromAny(raw)
		if step == nil {
			continue
		}
		stepNo := index + 1
		actionType := strings.ToUpper(firstStringValue(step, "action_type", "actionType", "type"))
		target := firstStringValue(step, "semantic_name", "semanticName", "target", "selector", "locator", "element", "name")
		inputs := objectFromAny(step["inputs"])

		switch actionType {
		case "NAVIGATE", "GOTO":
			url := firstNonEmptyStringFromObjects([]map[string]interface{}{inputs, step}, "url", "page_url", "pageUrl")
			if url == "" || isParamExpression(url) {
				continue
			}
			name := "base_url"
			if sawBaseURL {
				name = uniqueParamName(usedNames, fmt.Sprintf("url_step_%d", stepNo))
			} else {
				sawBaseURL = true
				usedNames[name]++
			}
			result.Suggestions = append(result.Suggestions, FlowParamSuggestion{
				Name: name, Kind: FlowParamKindInput, ValueType: "string", Example: url,
				StepNo: stepNo, ActionType: actionType, Target: target,
				Reason: "页面跳转地址随环境变化，建议抽取为入参",
			})
		case "INPUT", "FILL", "SELECT", "KEY_PRESS":
			value := firstNonEmptyStringFromObjects([]map[string]interface{}{inputs, step}, "value", "input_value", "inputValue", "text")
			if value == "" || isParamExpression(value) {
				continue
			}
			if isSensitiveParamCandidate(target) {
				result.Suggestions = append(result.Suggestions, FlowParamSuggestion{
					Name: strings.ToUpper(deriveParamName(target, stepNo, actionType)), Kind: FlowParamKindEnv,
					ValueType: "string", Example: "${env." + strings.ToUpper(deriveParamName(target, stepNo, actionType)) + "}",
					StepNo: stepNo, ActionType: actionType, Target: target,
					Reason: "疑似敏感凭据，禁止明文入参，建议改用 ${env.*} 引用",
				})
				continue
			}
			if runtimeIDPattern.MatchString(strings.TrimSpace(value)) {
				result.Excluded = append(result.Excluded, FlowParamExclusion{
					StepNo: stepNo, ActionType: actionType, Target: target,
					Reason: "疑似运行时临时 ID/时间戳，除非确认来自上游步骤输出，否则不建议抽取",
				})
				continue
			}
			result.Suggestions = append(result.Suggestions, FlowParamSuggestion{
				Name: uniqueParamName(usedNames, deriveParamName(target, stepNo, actionType)), Kind: FlowParamKindInput,
				ValueType: "string", Example: value,
				StepNo: stepNo, ActionType: actionType, Target: target,
				Reason: "业务输入值，建议抽取为入参以便复用",
			})
		}
	}
	result.InputSchema = buildSuggestedInputSchema(result.Suggestions)
	return result
}

// buildSuggestedInputSchema 将 INPUT 类建议汇总为 JSON Schema 形式的入参定义。
func buildSuggestedInputSchema(suggestions []FlowParamSuggestion) json.RawMessage {
	properties := map[string]interface{}{}
	required := []string{}
	for _, item := range suggestions {
		if item.Kind != FlowParamKindInput {
			continue
		}
		properties[item.Name] = map[string]interface{}{
			"type":        item.ValueType,
			"description": item.Reason,
			"example":     item.Example,
		}
		required = append(required, item.Name)
	}
	sort.Strings(required)
	schema := map[string]interface{}{"type": "object", "properties": properties}
	if len(required) > 0 {
		schema["required"] = required
	}
	data, err := json.Marshal(schema)
	if err != nil {
		return json.RawMessage(`{"type":"object","properties":{}}`)
	}
	return data
}

func isParamExpression(value string) bool {
	return strings.Contains(value, "${")
}

func isSensitiveParamCandidate(target string) bool {
	return containsSensitiveDSLKey(target) || strings.Contains(target, "密码") || strings.Contains(target, "凭据")
}

// deriveParamName 从目标元素语义名推导参数名：优先常见业务词映射，
// 其次提取 ASCII 单词，最后回退到步骤序号命名。
func deriveParamName(target string, stepNo int, actionType string) string {
	lowered := strings.ToLower(target)
	for _, entry := range paramNameTranslations {
		for _, keyword := range entry.keywords {
			if strings.Contains(lowered, keyword) {
				return entry.name
			}
		}
	}
	var builder strings.Builder
	lastUnderscore := true
	for _, r := range lowered {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			builder.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore {
				builder.WriteRune('_')
				lastUnderscore = true
			}
		}
	}
	candidate := strings.Trim(builder.String(), "_")
	if len(candidate) >= 3 {
		if len(candidate) > 32 {
			candidate = candidate[:32]
		}
		return strings.Trim(candidate, "_")
	}
	return fmt.Sprintf("step_%d_%s", stepNo, strings.ToLower(actionType))
}

func uniqueParamName(used map[string]int, name string) string {
	used[name]++
	if used[name] == 1 {
		return name
	}
	return fmt.Sprintf("%s_%d", name, used[name])
}

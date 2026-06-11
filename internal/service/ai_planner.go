// ai_planner.go — LLM Planner 语义匹配：上下文构建、计划校验与启发式降级
package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"testpilot/internal/model"
)

const (
	defaultPlannerTimeout  = 60 * time.Second
	plannerCandidateBudget = 30
	plannerMaxAttempts     = 2

	plannerModeAuto      = "auto"
	plannerModeLLM       = "llm"
	plannerModeHeuristic = "heuristic"

	plannerUsedLLM       = "LLM"
	plannerUsedHeuristic = "HEURISTIC"

	plannerLLMWeight       = 0.7
	plannerHeuristicWeight = 0.3
)

// plannerRunMeta 记录一次 LLM Planner 调用的审计信息。
type plannerRunMeta struct {
	Model            string
	ModelConfigID    uint
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	DegradedReason   string
	Attempts         int
}

// plannerRecordingStep 表示脱敏后的录制标准化步骤。
type plannerRecordingStep struct {
	ActionType string `json:"action_type,omitempty"`
	Target     string `json:"target,omitempty"`
	Value      string `json:"value,omitempty"`
	URLPath    string `json:"url_path,omitempty"`
}

// plannerAsset 表示提供给 LLM 的候选资产清单条目。
type plannerAsset struct {
	Kind             string `json:"kind"`
	ID               uint   `json:"id"`
	VersionID        uint   `json:"version_id,omitempty"`
	Key              string `json:"key"`
	Name             string `json:"name"`
	Description      string `json:"description,omitempty"`
	InputSchema      string `json:"input_schema,omitempty"`
	OutputSchema     string `json:"output_schema,omitempty"`
	ParamSchema      string `json:"param_schema,omitempty"`
	Preconditions    string `json:"preconditions,omitempty"`
	Postconditions   string `json:"postconditions,omitempty"`
	ValidationStatus string `json:"validation_status,omitempty"`
	Tags             string `json:"tags,omitempty"`
}

// plannerExecutorRequest 是发送给执行服务 /execute/plan 的请求体。
type plannerExecutorRequest struct {
	TaskName       string                 `json:"task_name"`
	ScenarioDesc   string                 `json:"scenario_desc"`
	StartURLPath   string                 `json:"start_url_path"`
	RecordingSteps []plannerRecordingStep `json:"recording_steps"`
	Candidates     []plannerAsset         `json:"candidates"`
	EnvKeys        []string               `json:"env_keys"`
	MaxSteps       int                    `json:"max_steps"`
	ExpressionDoc  string                 `json:"expression_doc"`
}

// plannerExecutorResponse 是执行服务 /execute/plan 的响应体。
type plannerExecutorResponse struct {
	Success      bool            `json:"success"`
	Plan         json.RawMessage `json:"plan"`
	Model        string          `json:"model"`
	Usage        plannerUsage    `json:"usage"`
	ErrorMessage string          `json:"error_message"`
}

type plannerUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// llmPlanPayload 是 14.2 章定义的计划 JSON Schema。
type llmPlanPayload struct {
	PlanID     string        `json:"plan_id"`
	Summary    string        `json:"summary"`
	Confidence float64       `json:"confidence"`
	Steps      []llmPlanStep `json:"steps"`
	Warnings   []string      `json:"warnings"`
}

type llmPlanStep struct {
	Type          string                 `json:"type"`
	StepName      string                 `json:"step_name,omitempty"`
	FlowID        uint                   `json:"flow_id,omitempty"`
	FlowVersionID uint                   `json:"flow_version_id,omitempty"`
	FlowKey       string                 `json:"flow_key,omitempty"`
	AssertionID   uint                   `json:"assertion_id,omitempty"`
	AssertionKey  string                 `json:"assertion_key,omitempty"`
	AtomicAction  string                 `json:"atomic_action,omitempty"`
	Confidence    float64                `json:"confidence"`
	Reason        string                 `json:"reason"`
	Inputs        map[string]interface{} `json:"inputs,omitempty"`
}

func normalizePlannerMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case plannerModeLLM:
		return plannerModeLLM
	case plannerModeHeuristic:
		return plannerModeHeuristic
	default:
		return plannerModeAuto
	}
}

// runLLMPlanner 调用 LLM Planner 生成编排建议；失败时降级到启发式结果。
func (s *AIScenarioCompositionService) runLLMPlanner(
	ctx context.Context,
	task *model.AIScriptTask,
	sourceVersion *model.AIScriptVersion,
	candidates []aiPlanCandidate,
	maxSteps int,
	heuristic *AIPlanResult,
) (*AIPlanResult, plannerRunMeta) {
	meta := plannerRunMeta{}
	if s.aiModelSvc == nil {
		meta.DegradedReason = fmt.Sprintf("[%d] AI 模型配置服务未接入，已降级启发式规划", CodeAIPlannerModelNotConfigured)
		return degradedPlanResult(heuristic, meta.DegradedReason), meta
	}
	modelCfg, err := s.aiModelSvc.SyncActiveToExecutor(ctx)
	if err != nil {
		meta.DegradedReason = fmt.Sprintf("[%d] 激活模型未配置或同步失败，已降级启发式规划", CodeAIPlannerModelNotConfigured)
		s.logger.Warn("llm planner sync active model failed", "error", err)
		return degradedPlanResult(heuristic, meta.DegradedReason), meta
	}
	meta.ModelConfigID = modelCfg.ID
	meta.Model = modelCfg.ModelID

	reqBody := s.buildPlannerContext(task, sourceVersion, candidates, maxSteps)

	timeout := s.plannerTimeout
	if timeout <= 0 {
		timeout = defaultPlannerTimeout
	}

	invalidCount := 0
	for attempt := 1; attempt <= plannerMaxAttempts; attempt++ {
		meta.Attempts = attempt
		resp, callErr := s.callPlannerExecutor(ctx, timeout, reqBody)
		if callErr != nil {
			if errors.Is(callErr, context.DeadlineExceeded) {
				meta.DegradedReason = fmt.Sprintf("[%d] LLM Planner 调用超时（%s），已降级启发式规划", CodeAIPlannerTimeout, timeout)
				return degradedPlanResult(heuristic, meta.DegradedReason), meta
			}
			s.logger.Warn("llm planner executor call failed", "attempt", attempt, "error", callErr)
			invalidCount++
			continue
		}
		if resp.Model != "" {
			meta.Model = resp.Model
		}
		meta.PromptTokens += resp.Usage.PromptTokens
		meta.CompletionTokens += resp.Usage.CompletionTokens
		meta.TotalTokens += resp.Usage.TotalTokens
		if !resp.Success || len(resp.Plan) == 0 {
			s.logger.Warn("llm planner executor returned failure", "attempt", attempt, "error", resp.ErrorMessage)
			invalidCount++
			continue
		}
		result, validateErr := s.validateLLMPlan(task.ID, resp.Plan, candidates, maxSteps)
		if validateErr != nil {
			s.logger.Warn("llm planner output invalid", "attempt", attempt, "error", validateErr)
			invalidCount++
			continue
		}
		result.PlannerUsed = plannerUsedLLM
		return result, meta
	}
	if invalidCount >= plannerMaxAttempts {
		meta.DegradedReason = fmt.Sprintf("[%d] LLM 输出连续 %d 次不合法，已降级启发式规划", CodeAIPlannerOutputInvalid, plannerMaxAttempts)
	} else {
		meta.DegradedReason = fmt.Sprintf("[%d] LLM Planner 调用失败，已降级启发式规划", CodeAIPlannerOutputInvalid)
	}
	return degradedPlanResult(heuristic, meta.DegradedReason), meta
}

// degradedPlanResult 复制启发式结果并标注降级原因。
func degradedPlanResult(heuristic *AIPlanResult, reason string) *AIPlanResult {
	result := *heuristic
	result.Steps = append([]AIPlanStep(nil), heuristic.Steps...)
	result.Warnings = append([]string(nil), heuristic.Warnings...)
	result.PlannerUsed = plannerUsedHeuristic
	result.DegradedReason = reason
	if !strings.HasPrefix(result.Summary, "【启发式降级】") {
		result.Summary = "【启发式降级】" + result.Summary
	}
	return &result
}

// buildPlannerContext 构建 LLM Planner 上下文：脱敏录制步骤、候选资产清单、环境变量白名单。
func (s *AIScenarioCompositionService) buildPlannerContext(
	task *model.AIScriptTask,
	sourceVersion *model.AIScriptVersion,
	candidates []aiPlanCandidate,
	maxSteps int,
) plannerExecutorRequest {
	selected := candidates
	if len(selected) > plannerCandidateBudget {
		prefiltered := append([]aiPlanCandidate(nil), candidates...)
		sort.SliceStable(prefiltered, func(i, j int) bool {
			return prefiltered[i].Score > prefiltered[j].Score
		})
		selected = prefiltered[:plannerCandidateBudget]
	}
	assets := make([]plannerAsset, 0, len(selected))
	for _, candidate := range selected {
		switch {
		case candidate.Flow != nil:
			versionID := candidate.Step.FlowVersionID
			assets = append(assets, plannerAsset{
				Kind:             model.AIScenarioStepTypeFlowCall,
				ID:               candidate.Flow.ID,
				VersionID:        versionID,
				Key:              candidate.Flow.FlowKey,
				Name:             candidate.Flow.FlowName,
				Description:      candidate.Flow.Description,
				InputSchema:      string(candidate.Flow.InputSchemaJSON),
				OutputSchema:     string(candidate.Flow.OutputSchemaJSON),
				Preconditions:    string(candidate.Flow.PreconditionsJSON),
				Postconditions:   string(candidate.Flow.PostconditionsJSON),
				ValidationStatus: candidate.Flow.LatestValidationStatus,
				Tags:             string(candidate.Flow.TagsJSON),
			})
		case candidate.Assertion != nil:
			assets = append(assets, plannerAsset{
				Kind:             model.AIScenarioStepTypeAssertion,
				ID:               candidate.Assertion.ID,
				Key:              candidate.Assertion.AssertionKey,
				Name:             candidate.Assertion.AssertionName,
				Description:      candidate.Assertion.Description,
				ParamSchema:      string(candidate.Assertion.ParamSchemaJSON),
				ValidationStatus: candidate.Assertion.LatestValidationStatus,
			})
		}
	}
	envKeys := make([]string, 0, len(allowedCompositionEnvKeys))
	for key := range allowedCompositionEnvKeys {
		envKeys = append(envKeys, key)
	}
	sort.Strings(envKeys)
	return plannerExecutorRequest{
		TaskName:       task.TaskName,
		ScenarioDesc:   task.ScenarioDesc,
		StartURLPath:   sanitizeURLPath(task.StartURL),
		RecordingSteps: sanitizeRecordingSteps(sourceVersion.StepModelJSON),
		Candidates:     assets,
		EnvKeys:        envKeys,
		MaxSteps:       maxSteps,
		ExpressionDoc:  "参数表达式仅允许 ${env.<白名单KEY>}、${steps.<step_key>.outputs.<name>}、${variables.<name>}、${literal.<value>}",
	}
}

// sanitizeRecordingSteps 提取录制标准化步骤并对输入值脱敏。
func sanitizeRecordingSteps(stepModel model.JSONMap) []plannerRecordingStep {
	steps := []plannerRecordingStep{}
	if stepModel == nil {
		return steps
	}
	rawSteps, ok := stepModel["steps"].([]interface{})
	if !ok {
		return steps
	}
	for _, raw := range rawSteps {
		item := objectFromAny(raw)
		if item == nil {
			continue
		}
		step := plannerRecordingStep{
			ActionType: strings.ToUpper(firstStringValue(item, "action_type", "actionType", "type")),
			Target:     firstStringValue(item, "semantic_name", "semanticName", "target", "selector", "locator", "element", "name"),
			URLPath:    sanitizeURLPath(firstStringValue(item, "url", "page_url", "pageUrl")),
		}
		if value := firstStringValue(item, "value", "input_value", "inputValue", "text"); value != "" {
			step.Value = sanitizePlannerValue(step.Target, value)
		}
		steps = append(steps, step)
	}
	return steps
}

func firstStringValue(item map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if text, ok := item[key].(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

// sanitizePlannerValue 对疑似敏感的录制输入值替换为 ${env.*} 占位符。
func sanitizePlannerValue(target, value string) string {
	lowered := strings.ToLower(target + " " + value)
	switch {
	case strings.Contains(lowered, "password") || strings.Contains(lowered, "密码"):
		return "${env.ADMIN_PASSWORD}"
	case containsSensitiveDSLKey(target):
		return "${env.REDACTED}"
	case strings.Contains(lowered, "user") || strings.Contains(lowered, "账号") || strings.Contains(lowered, "用户名"):
		return "${env.ADMIN_USER}"
	}
	return value
}

// sanitizeURLPath 仅保留 URL 的路径部分。
func sanitizeURLPath(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Path == "" {
		return ""
	}
	return parsed.Path
}

// callPlannerExecutor 调用执行服务 /execute/plan，带独立超时。
func (s *AIScenarioCompositionService) callPlannerExecutor(ctx context.Context, timeout time.Duration, reqBody plannerExecutorRequest) (*plannerExecutorResponse, error) {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal planner request: %w", err)
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(callCtx, http.MethodPost, s.executorURL+"/execute/plan", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create planner request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if s.executorAPIKey != "" {
		req.Header.Set("X-API-Key", s.executorAPIKey)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		if errors.Is(callCtx.Err(), context.DeadlineExceeded) {
			return nil, context.DeadlineExceeded
		}
		return nil, fmt.Errorf("planner http call: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("executor returned HTTP %d: %s", resp.StatusCode, string(errBody))
	}
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("read planner response: %w", err)
	}
	var parsed plannerExecutorResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("decode planner response: %w", err)
	}
	return &parsed, nil
}

// validateLLMPlan 按 14.2 Schema 严格校验 LLM 输出，并做防幻觉引用校验与置信度加权。
func (s *AIScenarioCompositionService) validateLLMPlan(taskID uint, raw json.RawMessage, candidates []aiPlanCandidate, maxSteps int) (*AIPlanResult, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var payload llmPlanPayload
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("plan schema invalid: %w", err)
	}
	if strings.TrimSpace(payload.Summary) == "" {
		return nil, fmt.Errorf("plan schema invalid: summary 不能为空")
	}
	if payload.Confidence < 0 || payload.Confidence > 1 {
		return nil, fmt.Errorf("plan schema invalid: confidence 必须在 [0,1]")
	}
	if len(payload.Steps) == 0 {
		return nil, fmt.Errorf("plan schema invalid: steps 不能为空")
	}

	flowCandidates := map[uint]aiPlanCandidate{}
	assertionCandidates := map[uint]aiPlanCandidate{}
	for _, candidate := range candidates {
		if candidate.Flow != nil {
			flowCandidates[candidate.Flow.ID] = candidate
		}
		if candidate.Assertion != nil {
			assertionCandidates[candidate.Assertion.ID] = candidate
		}
	}

	warnings := append([]string{}, payload.Warnings...)
	steps := make([]AIPlanStep, 0, len(payload.Steps))
	var heuristicTotal float64
	for index, step := range payload.Steps {
		if len(steps) >= maxSteps {
			warnings = append(warnings, fmt.Sprintf("计划步骤超过上限 %d，已截断", maxSteps))
			break
		}
		if step.Confidence < 0 || step.Confidence > 1 {
			warnings = append(warnings, fmt.Sprintf("第 %d 个步骤置信度非法，已剔除", index+1))
			continue
		}
		heuristicConfidence := 0.62
		switch step.Type {
		case model.AIScenarioStepTypeFlowCall:
			candidate, ok := flowCandidates[step.FlowID]
			if !ok || candidate.Step.FlowVersionID != step.FlowVersionID {
				warnings = append(warnings, fmt.Sprintf("第 %d 个步骤引用了不存在的固定场景资产，已剔除", index+1))
				continue
			}
			heuristicConfidence = candidate.Step.Confidence
			step.FlowKey = candidate.Step.FlowKey
		case model.AIScenarioStepTypeAssertion:
			candidate, ok := assertionCandidates[step.AssertionID]
			if !ok {
				warnings = append(warnings, fmt.Sprintf("第 %d 个步骤引用了不存在的断言资产，已剔除", index+1))
				continue
			}
			heuristicConfidence = candidate.Step.Confidence
			step.AssertionKey = candidate.Step.AssertionKey
		case model.AIScenarioStepTypeAtomicAction, model.AIScenarioStepTypeAIGenerated:
			if strings.TrimSpace(step.Reason) == "" {
				warnings = append(warnings, fmt.Sprintf("第 %d 个占位步骤缺少说明，已剔除", index+1))
				continue
			}
			if step.Type == model.AIScenarioStepTypeAtomicAction && strings.TrimSpace(step.AtomicAction) == "" {
				warnings = append(warnings, fmt.Sprintf("第 %d 个原子步骤缺少 atomic_action，已剔除", index+1))
				continue
			}
		default:
			warnings = append(warnings, fmt.Sprintf("第 %d 个步骤类型 %q 不受支持，已剔除", index+1, step.Type))
			continue
		}
		if reason := validatePlanStepInputs(step.Inputs); reason != "" {
			warnings = append(warnings, fmt.Sprintf("第 %d 个步骤参数不合规（%s），已剔除", index+1, reason))
			continue
		}
		weighted := roundConfidence(plannerLLMWeight*step.Confidence + plannerHeuristicWeight*heuristicConfidence)
		heuristicTotal += heuristicConfidence
		steps = append(steps, AIPlanStep{
			Type:          step.Type,
			StepName:      step.StepName,
			FlowID:        step.FlowID,
			FlowVersionID: step.FlowVersionID,
			FlowKey:       step.FlowKey,
			AssertionID:   step.AssertionID,
			AssertionKey:  step.AssertionKey,
			AtomicAction:  step.AtomicAction,
			Confidence:    weighted,
			Reason:        step.Reason,
			Inputs:        step.Inputs,
		})
	}
	if len(steps) == 0 {
		return nil, fmt.Errorf("plan invalid: 所有步骤均未通过校验")
	}
	confidence := roundConfidence(plannerLLMWeight*payload.Confidence + plannerHeuristicWeight*(heuristicTotal/float64(len(steps))))
	if confidence < 0.75 {
		warnings = append(warnings, "整体置信度低于 75%，建议逐条确认后再生成编排草稿")
	}
	planID := strings.TrimSpace(payload.PlanID)
	if planID == "" {
		planID = fmt.Sprintf("plan_%d_%d", taskID, time.Now().Unix())
	}
	return &AIPlanResult{
		PlanID:     planID,
		Confidence: confidence,
		Summary:    payload.Summary,
		Steps:      steps,
		Warnings:   warnings,
	}, nil
}

// validatePlanStepInputs 校验步骤参数表达式与敏感明文；返回非空字符串表示不合规原因。
func validatePlanStepInputs(inputs map[string]interface{}) string {
	for key, value := range inputs {
		text, ok := value.(string)
		if !ok {
			continue
		}
		if containsSensitiveDSLKey(key) && isPlainSensitiveValue(text) {
			return fmt.Sprintf("敏感参数 %s 不允许明文", key)
		}
		for _, match := range dslReferencePattern.FindAllStringSubmatch(text, -1) {
			ref := strings.TrimSpace(match[1])
			switch {
			case strings.HasPrefix(ref, "env."):
				envKey := strings.TrimPrefix(ref, "env.")
				if _, allowed := allowedCompositionEnvKeys[envKey]; !allowed {
					return fmt.Sprintf("环境变量 %s 不在白名单内", envKey)
				}
			case strings.HasPrefix(ref, "steps."), strings.HasPrefix(ref, "variables."), strings.HasPrefix(ref, "literal."):
			default:
				return fmt.Sprintf("引用了不支持的参数表达式 ${%s}", ref)
			}
		}
	}
	return ""
}

// recordAIPlanOperationLog 记录 AI 规划操作日志（模型 ID、耗时、token 用量、降级原因等）。
func (s *AIScenarioCompositionService) recordAIPlanOperationLog(
	ctx context.Context,
	input AIPlanFromTaskInput,
	mode string,
	result *AIPlanResult,
	meta plannerRunMeta,
	elapsed time.Duration,
	candidateCount int,
) {
	if input.OperatorID == 0 {
		return
	}
	operatorName := ""
	if operator, err := s.userRepo.FindByID(ctx, input.OperatorID); err == nil && operator != nil {
		operatorName = operator.Name
	}
	descPayload := map[string]interface{}{
		"planner_mode":      mode,
		"planner_used":      result.PlannerUsed,
		"model":             meta.Model,
		"model_config_id":   meta.ModelConfigID,
		"duration_ms":       elapsed.Milliseconds(),
		"prompt_tokens":     meta.PromptTokens,
		"completion_tokens": meta.CompletionTokens,
		"total_tokens":      meta.TotalTokens,
		"degraded_reason":   result.DegradedReason,
		"candidate_count":   candidateCount,
		"adopted_count":     len(result.Steps),
	}
	desc := ""
	if data, err := json.Marshal(descPayload); err == nil {
		desc = string(data)
	}
	if runes := []rune(desc); len(runes) > 500 {
		desc = string(runes[:500])
	}
	taskID := input.TaskID
	logEntry := &model.AIScriptOperationLog{
		TaskID:        &taskID,
		OperationType: model.AIScriptOperationAIPlan,
		OperatorID:    input.OperatorID,
		OperatorName:  operatorName,
		OperationDesc: desc,
	}
	if err := s.aiScriptRepo.CreateOperationLog(ctx, logEntry); err != nil {
		s.logger.Warn("record ai plan operation log failed", "task_id", input.TaskID, "error", err)
	}
}

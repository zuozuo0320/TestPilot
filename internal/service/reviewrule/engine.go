// Package reviewrule 实现用例评审 v0.2 Layer 1 规则引擎（纯函数）。
//
// 本包零依赖、零副作用：对测试用例快照执行一组字段完整性规则，
// 产出 RuleReport。Service 层负责把报告持久化为 case_review_items.ai_gate_status
// 与 case_review_ai_reports（Phase 2）。
//
// Phase 1 仅覆盖规则引擎层，不包含 LLM 检查；所有规则耗时 O(1)。
package reviewrule

import (
	"strings"
	"unicode/utf8"

	"testpilot/internal/model"
)

// Severity 复用 model 层严重度常量，避免双套枚举产生漂移
type Severity = string

// Finding 单条规则命中结果。
// Severity 只会是 model.ReviewSeverityCritical / Major / Minor 三者之一。
type Finding struct {
	// ID 规则 ID（稳定串，供 UI 本地化、前端筛选、Primary 反馈 ai_feedback 使用）
	ID string `json:"id"`
	// Rule 规则名（中文可读，用于 UI 展示）
	Rule string `json:"rule"`
	// Message 人类可读信息（附带上下文，例如字段值摘要）
	Message string `json:"message"`
	// Severity critical / major / minor
	Severity Severity `json:"severity"`
	// Field 相关字段名（前端可据此跳转到表单对应字段）
	Field string `json:"field"`
	// Value 字段当前值摘要（已截断，避免日志/响应膨胀）
	Value string `json:"value,omitempty"`
}

// RuleReport 规则引擎最终报告。Passed 仅在 CriticalCount==0 且 MajorCount==0 时为 true；
// Minor 不影响通过判定（与方案 §5.1 "Critical / Major 阻断，Minor 提示" 一致）。
type RuleReport struct {
	Passed        bool      `json:"passed"`
	Findings      []Finding `json:"findings"`
	CriticalCount int       `json:"critical_count"`
	MajorCount    int       `json:"major_count"`
	MinorCount    int       `json:"minor_count"`
}

// 规则 ID 常量（稳定串，禁止随意改动，前端/Prompt 迭代都会依赖）
const (
	RuleTitleRequired         = "RULE_TITLE_REQUIRED"
	RuleTitleLenMax           = "RULE_TITLE_LEN_MAX"
	RulePreconditionRequired  = "RULE_PRECONDITION_REQUIRED"
	RuleStepsRequired         = "RULE_STEPS_REQUIRED"
	RuleStepsMinLen           = "RULE_STEPS_MIN_LEN"
	RulePostconditionRequired = "RULE_POSTCONDITION_REQUIRED"
	RuleLevelRequired         = "RULE_LEVEL_REQUIRED"
)

// 可配置阈值（Phase 1 硬编码，Phase 2 可迁入项目 settings）
const (
	// TitleMaxRunes 标题长度上限，超出仅告警（minor），不阻断
	TitleMaxRunes = 120
	// StepsMinLen 步骤最短字符数（去空白后），低于此值视为低质量
	StepsMinLen = 20
	// ValueSummaryMaxRunes 报告里字段值摘要的最大字符数，防止日志膨胀
	ValueSummaryMaxRunes = 60
)

// Evaluate 对一个测试用例快照执行全部规则，返回独立报告。
// 调用方传入 nil 视为"空用例"，会命中全部必填规则。
//
// 本函数保证：
//   - 不修改入参
//   - 不访问 DB、网络、文件
//   - 同一输入恒得同一输出（幂等）
func Evaluate(tc *model.TestCase) RuleReport {
	var findings []Finding

	title := ""
	precondition := ""
	postcondition := ""
	steps := ""
	level := ""
	if tc != nil {
		title = strings.TrimSpace(tc.Title)
		precondition = strings.TrimSpace(tc.Precondition)
		postcondition = strings.TrimSpace(tc.Postcondition)
		steps = strings.TrimSpace(tc.Steps)
		level = strings.TrimSpace(tc.Level)
	}

	// ---- R1: 标题必填 ----
	if title == "" {
		findings = append(findings, Finding{
			ID:       RuleTitleRequired,
			Rule:     "标题必填",
			Message:  "用例标题为空",
			Severity: model.ReviewSeverityCritical,
			Field:    "title",
		})
	} else if utf8.RuneCountInString(title) > TitleMaxRunes {
		// ---- R2: 标题长度上限（仅提示） ----
		findings = append(findings, Finding{
			ID:       RuleTitleLenMax,
			Rule:     "标题过长",
			Message:  "标题字符数超过 120，建议精简",
			Severity: model.ReviewSeverityMinor,
			Field:    "title",
			Value:    summary(title),
		})
	}

	// ---- R3: 前置条件必填 ----
	if precondition == "" {
		findings = append(findings, Finding{
			ID:       RulePreconditionRequired,
			Rule:     "前置条件必填",
			Message:  "用例前置条件为空，评审人无法判断执行前提",
			Severity: model.ReviewSeverityMajor,
			Field:    "precondition",
		})
	}

	// ---- R4/R5: 步骤 ----
	if steps == "" {
		findings = append(findings, Finding{
			ID:       RuleStepsRequired,
			Rule:     "步骤必填",
			Message:  "用例步骤为空",
			Severity: model.ReviewSeverityCritical,
			Field:    "steps",
		})
	} else if utf8.RuneCountInString(steps) < StepsMinLen {
		// 低于最短长度视作"低质量步骤"，major 提示
		findings = append(findings, Finding{
			ID:       RuleStepsMinLen,
			Rule:     "步骤过于简略",
			Message:  "步骤字符数过少，建议补充操作细节与预期结果",
			Severity: model.ReviewSeverityMajor,
			Field:    "steps",
			Value:    summary(steps),
		})
	}

	// ---- R6: 后置条件（提示） ----
	if postcondition == "" {
		findings = append(findings, Finding{
			ID:       RulePostconditionRequired,
			Rule:     "后置条件建议补充",
			Message:  "后置条件为空，建议补充清理/回滚逻辑",
			Severity: model.ReviewSeverityMinor,
			Field:    "postcondition",
		})
	}

	// ---- R7: 优先级/等级 ----
	if level == "" {
		findings = append(findings, Finding{
			ID:       RuleLevelRequired,
			Rule:     "等级必填",
			Message:  "用例等级为空，无法纳入回归范围评估",
			Severity: model.ReviewSeverityMajor,
			Field:    "level",
		})
	}

	report := RuleReport{Findings: findings}
	for _, f := range findings {
		switch f.Severity {
		case model.ReviewSeverityCritical:
			report.CriticalCount++
		case model.ReviewSeverityMajor:
			report.MajorCount++
		case model.ReviewSeverityMinor:
			report.MinorCount++
		}
	}
	// Critical 或 Major 任一存在都视为未通过（Minor 只提示）
	report.Passed = report.CriticalCount == 0 && report.MajorCount == 0
	return report
}

// summary 按 rune 截断字段值摘要，避免日志/响应膨胀
func summary(s string) string {
	if utf8.RuneCountInString(s) <= ValueSummaryMaxRunes {
		return s
	}
	runes := []rune(s)
	return string(runes[:ValueSummaryMaxRunes]) + "…"
}

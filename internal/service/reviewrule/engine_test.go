package reviewrule

import (
	"strings"
	"testing"

	"testpilot/internal/model"
)

// buildGoodCase 构造一个全部规则通过的基线用例
func buildGoodCase() *model.TestCase {
	return &model.TestCase{
		Title:         "登录成功后跳转首页",
		Level:         "P1",
		Precondition:  "用户已在注册表中，处于正常状态",
		Postcondition: "登录后会话有效，刷新仍保持登录",
		Steps:         "1. 打开登录页；2. 输入有效邮箱和密码；3. 点击登录按钮。预期：跳转到 /home 并看到用户名。",
	}
}

// TestEvaluate_PassedBaseline 完备用例无 finding
func TestEvaluate_PassedBaseline(t *testing.T) {
	tc := buildGoodCase()
	report := Evaluate(tc)
	if !report.Passed {
		t.Fatalf("expected passed, got findings=%+v", report.Findings)
	}
	if report.CriticalCount != 0 || report.MajorCount != 0 {
		t.Fatalf("critical/major should be zero, got %+v", report)
	}
}

// TestEvaluate_NilCase 传入 nil 视为完全空用例，命中全部必填
func TestEvaluate_NilCase(t *testing.T) {
	report := Evaluate(nil)
	if report.Passed {
		t.Fatal("nil case must not pass")
	}
	wantRules := []string{
		RuleTitleRequired,
		RulePreconditionRequired,
		RuleStepsRequired,
		RulePostconditionRequired,
		RuleLevelRequired,
	}
	if !hasAllFindings(report, wantRules) {
		t.Fatalf("missing rules in findings, got=%+v, want=%v", report.Findings, wantRules)
	}
}

// TestEvaluate_TableDriven 单字段缺失覆盖
func TestEvaluate_TableDriven(t *testing.T) {
	tests := []struct {
		name             string
		mutate           func(tc *model.TestCase)
		wantRuleID       string
		wantSeverity     Severity
		wantPassedFalse  bool // 期望 Passed=false（Critical/Major 都是 false）
		wantPassedFalseM bool // 期望 Passed=false（Minor 则是 true）
	}{
		{
			name:            "title_required",
			mutate:          func(tc *model.TestCase) { tc.Title = "" },
			wantRuleID:      RuleTitleRequired,
			wantSeverity:    model.ReviewSeverityCritical,
			wantPassedFalse: true,
		},
		{
			name:             "title_too_long",
			mutate:           func(tc *model.TestCase) { tc.Title = strings.Repeat("长", 125) },
			wantRuleID:       RuleTitleLenMax,
			wantSeverity:     model.ReviewSeverityMinor,
			wantPassedFalseM: true, // Minor 不影响 Passed
		},
		{
			name:            "precondition_required",
			mutate:          func(tc *model.TestCase) { tc.Precondition = "" },
			wantRuleID:      RulePreconditionRequired,
			wantSeverity:    model.ReviewSeverityMajor,
			wantPassedFalse: true,
		},
		{
			name:            "steps_required",
			mutate:          func(tc *model.TestCase) { tc.Steps = "" },
			wantRuleID:      RuleStepsRequired,
			wantSeverity:    model.ReviewSeverityCritical,
			wantPassedFalse: true,
		},
		{
			name:            "steps_too_short",
			mutate:          func(tc *model.TestCase) { tc.Steps = "点一下" },
			wantRuleID:      RuleStepsMinLen,
			wantSeverity:    model.ReviewSeverityMajor,
			wantPassedFalse: true,
		},
		{
			name:             "postcondition_empty_minor_only",
			mutate:           func(tc *model.TestCase) { tc.Postcondition = "" },
			wantRuleID:       RulePostconditionRequired,
			wantSeverity:     model.ReviewSeverityMinor,
			wantPassedFalseM: true,
		},
		{
			name:            "level_required",
			mutate:          func(tc *model.TestCase) { tc.Level = "" },
			wantRuleID:      RuleLevelRequired,
			wantSeverity:    model.ReviewSeverityMajor,
			wantPassedFalse: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := buildGoodCase()
			tt.mutate(tc)
			report := Evaluate(tc)

			var got *Finding
			for i := range report.Findings {
				if report.Findings[i].ID == tt.wantRuleID {
					got = &report.Findings[i]
					break
				}
			}
			if got == nil {
				t.Fatalf("expected finding %s, got=%+v", tt.wantRuleID, report.Findings)
			}
			if got.Severity != tt.wantSeverity {
				t.Fatalf("severity mismatch: want=%s got=%s", tt.wantSeverity, got.Severity)
			}
			if tt.wantPassedFalse && report.Passed {
				t.Fatalf("expected passed=false when %s triggered", tt.wantRuleID)
			}
			if tt.wantPassedFalseM && !report.Passed {
				t.Fatalf("minor-only mutation should keep Passed=true, got findings=%+v", report.Findings)
			}
		})
	}
}

// TestEvaluate_SummaryTruncation 验证长字段摘要被截断
func TestEvaluate_SummaryTruncation(t *testing.T) {
	tc := buildGoodCase()
	tc.Title = strings.Repeat("甲", 200) // 超长
	report := Evaluate(tc)
	for _, f := range report.Findings {
		if f.ID == RuleTitleLenMax {
			if utf8Count(f.Value) > ValueSummaryMaxRunes+1 { // +1 允许 "…"
				t.Fatalf("summary should be truncated, got rune count=%d", utf8Count(f.Value))
			}
			return
		}
	}
	t.Fatalf("expected %s finding", RuleTitleLenMax)
}

// utf8Count 返回 rune 数，避免引入额外包
func utf8Count(s string) int {
	return len([]rune(s))
}

// hasAllFindings 检查 report 是否包含指定 ruleID 集合
func hasAllFindings(r RuleReport, ruleIDs []string) bool {
	seen := make(map[string]bool, len(r.Findings))
	for _, f := range r.Findings {
		seen[f.ID] = true
	}
	for _, id := range ruleIDs {
		if !seen[id] {
			return false
		}
	}
	return true
}

// ai_regression.go — 阶段三（18.3）：已发布编排定时回归、AI 修复建议与编排指标记录
package model

import "time"

// 回归执行触发方式
const (
	AIRegressionTriggerScheduled = "SCHEDULED"
	AIRegressionTriggerManual    = "MANUAL"
)

// 回归执行状态
const (
	AIRegressionExecutionStatusPassed  = "PASSED"
	AIRegressionExecutionStatusFailed  = "FAILED"
	AIRegressionExecutionStatusError   = "ERROR"
	AIRegressionExecutionStatusSkipped = "SKIPPED"
)

// AI 修复建议状态：建议只生成 Diff，必须人工确认后才能应用（14.3 约束）
const (
	AIRepairSuggestionStatusPending  = "PENDING"
	AIRepairSuggestionStatusAdopted  = "ADOPTED"
	AIRepairSuggestionStatusRejected = "REJECTED"
	AIRepairSuggestionStatusFailed   = "FAILED"
)

// AIScriptOperationAIRepair AI 修复建议相关操作日志类型
const (
	AIScriptOperationAIRepair      = "AI_REPAIR"
	AIScriptOperationAIRepairApply = "AI_REPAIR_APPLY"
)

// AIRegressionPlan 已发布编排的定时回归计划
type AIRegressionPlan struct {
	ID              uint       `json:"id" gorm:"primaryKey"`
	ProjectID       uint       `json:"project_id" gorm:"not null;index;uniqueIndex:uk_ai_regression_plan_comp"`
	CompositionID   uint       `json:"composition_id" gorm:"not null;uniqueIndex:uk_ai_regression_plan_comp"`
	Name            string     `json:"name" gorm:"size:128;not null"`
	IntervalMinutes int        `json:"interval_minutes" gorm:"not null;default:60"`
	Enabled         bool       `json:"enabled" gorm:"not null;default:true;index"`
	NextRunAt       *time.Time `json:"next_run_at" gorm:"index"`
	LastRunAt       *time.Time `json:"last_run_at"`
	LastStatus      string     `json:"last_status" gorm:"size:32"`
	CreatedBy       uint       `json:"created_by" gorm:"not null"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedBy       uint       `json:"updated_by" gorm:"not null"`
	UpdatedAt       time.Time  `json:"updated_at"`

	CompositionName   string `json:"composition_name,omitempty" gorm:"-"`
	CompositionStatus string `json:"composition_status,omitempty" gorm:"-"`
}

// TableName 指定回归计划表名。
func (AIRegressionPlan) TableName() string {
	return "ai_regression_plan"
}

// AIRegressionExecution 单次回归执行记录
type AIRegressionExecution struct {
	ID                 uint       `json:"id" gorm:"primaryKey"`
	PlanID             uint       `json:"plan_id" gorm:"not null;index"`
	ProjectID          uint       `json:"project_id" gorm:"not null;index"`
	CompositionID      uint       `json:"composition_id" gorm:"not null;index"`
	ValidationID       *uint      `json:"validation_id" gorm:"index"`
	TriggerType        string     `json:"trigger_type" gorm:"size:32;not null"`
	Status             string     `json:"status" gorm:"size:32;not null;index"`
	FailureSummary     string     `json:"failure_summary" gorm:"size:1000"`
	RepairSuggestionID *uint      `json:"repair_suggestion_id" gorm:"index"`
	StartedAt          *time.Time `json:"started_at"`
	FinishedAt         *time.Time `json:"finished_at"`
	DurationMs         int64      `json:"duration_ms"`
	CreatedAt          time.Time  `json:"created_at"`

	CompositionName string `json:"composition_name,omitempty" gorm:"-"`
}

// TableName 指定回归执行记录表名。
func (AIRegressionExecution) TableName() string {
	return "ai_regression_execution"
}

// AIRepairSuggestion 回归失败后 AI 生成的修复 Diff 建议，人工确认后才能应用
type AIRepairSuggestion struct {
	ID               uint       `json:"id" gorm:"primaryKey"`
	ProjectID        uint       `json:"project_id" gorm:"not null;index"`
	CompositionID    uint       `json:"composition_id" gorm:"not null;index"`
	ExecutionID      uint       `json:"execution_id" gorm:"not null;index"`
	Status           string     `json:"status" gorm:"size:32;not null;index"`
	Summary          string     `json:"summary" gorm:"size:1000"`
	DiffContent      string     `json:"diff_content" gorm:"type:longtext"`
	PatchedCode      string     `json:"patched_code" gorm:"type:longtext"`
	Model            string     `json:"model" gorm:"size:128"`
	ModelConfigID    uint       `json:"model_config_id"`
	PromptTokens     int        `json:"prompt_tokens"`
	CompletionTokens int        `json:"completion_tokens"`
	TotalTokens      int        `json:"total_tokens"`
	ErrorMessage     string     `json:"error_message" gorm:"size:1000"`
	ConfirmedBy      *uint      `json:"confirmed_by"`
	ConfirmedAt      *time.Time `json:"confirmed_at"`
	CreatedAt        time.Time  `json:"created_at"`

	CompositionName string `json:"composition_name,omitempty" gorm:"-"`
}

// TableName 指定修复建议表名。
func (AIRepairSuggestion) TableName() string {
	return "ai_repair_suggestion"
}

// AIPlanRecord AI 编排计划指标记录：复用命中、采纳、首次验证结果与人工干预步骤
type AIPlanRecord struct {
	ID                    uint      `json:"id" gorm:"primaryKey"`
	ProjectID             uint      `json:"project_id" gorm:"not null;index"`
	TaskID                uint      `json:"task_id" gorm:"not null;index"`
	PlanID                string    `json:"plan_id" gorm:"size:64;not null;uniqueIndex"`
	PlannerUsed           string    `json:"planner_used" gorm:"size:32"`
	TotalSteps            int       `json:"total_steps"`
	FlowCallSteps         int       `json:"flow_call_steps"`
	AssertionSteps        int       `json:"assertion_steps"`
	CompositionID         *uint     `json:"composition_id" gorm:"index"`
	AdoptedSteps          int       `json:"adopted_steps"`
	ModifiedSteps         int       `json:"modified_steps"`
	ManualConfirmSteps    int       `json:"manual_confirm_steps"`
	FirstValidationStatus string    `json:"first_validation_status" gorm:"size:32"`
	CreatedBy             uint      `json:"created_by" gorm:"not null"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

// TableName 指定计划指标记录表名。
func (AIPlanRecord) TableName() string {
	return "ai_plan_record"
}

// ai_script.go — 测试智编模块 GORM 模型
package model

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"
)

// ── 枚举常量 ──

// 任务状态
const (
	AITaskStatusDraft             = "DRAFT"
	AITaskStatusPendingExecute    = "PENDING_EXECUTE"
	AITaskStatusRunning           = "RUNNING"
	AITaskStatusGenerateSuccess   = "GENERATE_SUCCESS"
	AITaskStatusGenerateFailed    = "GENERATE_FAILED"
	AITaskStatusPendingConfirm    = "PENDING_CONFIRM"
	AITaskStatusPendingRevalidate = "PENDING_REVALIDATE"
	AITaskStatusConfirmed         = "CONFIRMED"
	AITaskStatusDiscarded         = "DISCARDED"
	AITaskStatusManualReview      = "MANUAL_REVIEW"
)

// 脚本状态
const (
	AIScriptStatusDraft             = "DRAFT"
	AIScriptStatusPendingConfirm    = "PENDING_CONFIRM"
	AIScriptStatusPendingRevalidate = "PENDING_REVALIDATE"
	AIScriptStatusConfirmed         = "CONFIRMED"
	AIScriptStatusDiscarded         = "DISCARDED"
)

// 验证状态
const (
	AIValidationStatusNotValidated = "NOT_VALIDATED"
	AIValidationStatusValidating   = "VALIDATING"
	AIValidationStatusPassed       = "PASSED"
	AIValidationStatusFailed       = "FAILED"
	AIValidationStatusError        = "ERROR"
)

// 生成来源
const (
	AISourceTypePlaywrightRecorded      = "PLAYWRIGHT_RECORDED"
	AISourceTypeAIEnhancedFromRecording = "AI_ENHANCED_FROM_RECORDING"
	AISourceTypeAIGenerated             = "AI_GENERATED"
	AISourceTypeHumanEdited             = "HUMAN_EDITED"
	AISourceTypeMixed                   = "MIXED"
)

// 生成模式
const (
	AIGenerationModeRecordingEnhanced = "RECORDING_ENHANCED"
	AIGenerationModeAIDirect          = "AI_DIRECT"
)

// 录制状态
const (
	AIRecordingStatusInit      = "INIT"
	AIRecordingStatusRecording = "RECORDING"
	AIRecordingStatusFinished  = "FINISHED"
	AIRecordingStatusFailed    = "FAILED"
)

// V1 版本状态（独立于任务状态，支持版本级生命周期管理）
const (
	AIVersionStatusRecorded             = "RECORDED"
	AIVersionStatusGenerating           = "GENERATING"
	AIVersionStatusManualReviewRequired = "MANUAL_REVIEW_REQUIRED"
	AIVersionStatusGenerated            = "GENERATED"
	AIVersionStatusValidating           = "VALIDATING"
	AIVersionStatusValidateSuccess      = "VALIDATE_SUCCESS"
	AIVersionStatusValidateFailed       = "VALIDATE_FAILED"
	AIVersionStatusArchived             = "ARCHIVED"
)

// 轨迹动作类型
const (
	AITraceActionNavigate        = "NAVIGATE"
	AITraceActionClick           = "CLICK"
	AITraceActionInput           = "INPUT"
	AITraceActionSelect          = "SELECT"
	AITraceActionUpload          = "UPLOAD"
	AITraceActionScroll          = "SCROLL"
	AITraceActionWait            = "WAIT"
	AITraceActionAssertCandidate = "ASSERT_CANDIDATE"
	AITraceActionCustom          = "CUSTOM"
)

// ── JSON 辅助类型 ──

// JSONMap 用于 GORM JSON 字段的序列化/反序列化
type JSONMap map[string]interface{}

func (j JSONMap) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	b, err := json.Marshal(j)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

func (j *JSONMap) Scan(value interface{}) error {
	if value == nil {
		*j = nil
		return nil
	}
	var bytes []byte
	switch v := value.(type) {
	case string:
		bytes = []byte(v)
	case []byte:
		bytes = v
	default:
		return errors.New("unsupported type for JSONMap")
	}
	return json.Unmarshal(bytes, j)
}

// RawJSON 用于存储原始 JSON 内容的类型。
// GORM 中以 string 形式读写 MySQL JSON 列；
// json.Marshal 时直接输出原始 JSON（不转义为字符串）。
type RawJSON []byte

func (r RawJSON) Value() (driver.Value, error) {
	if r == nil {
		return nil, nil
	}
	return string(r), nil
}

func (r *RawJSON) Scan(value interface{}) error {
	if value == nil {
		*r = nil
		return nil
	}
	switch v := value.(type) {
	case string:
		*r = []byte(v)
	case []byte:
		*r = append([]byte(nil), v...) // 复制一份，避免引用底层 buffer
	default:
		return errors.New("unsupported type for RawJSON")
	}
	return nil
}

func (r RawJSON) MarshalJSON() ([]byte, error) {
	if r == nil {
		return []byte("null"), nil
	}
	return r, nil
}

func (r *RawJSON) UnmarshalJSON(data []byte) error {
	if data == nil {
		*r = nil
		return nil
	}
	*r = append([]byte(nil), data...)
	return nil
}

// ── 模型定义 ──

// AIScriptTask 测试智编-生成任务主表
type AIScriptTask struct {
	ID                     uint       `json:"id" gorm:"primaryKey"`
	ProjectID              uint       `json:"project_id" gorm:"not null;index:idx_ai_task_project_status"`
	ProjectKey             string     `json:"project_key" gorm:"size:64"`
	TaskName               string     `json:"task_name" gorm:"size:128;not null"`
	GenerationMode         string     `json:"generation_mode" gorm:"size:32;not null;default:RECORDING_ENHANCED;index:idx_ai_task_mode_status"`
	ScenarioDesc           string     `json:"scenario_desc" gorm:"type:text;not null"`
	StartURL               string     `json:"start_url" gorm:"size:512;not null"`
	AccountRef             string     `json:"account_ref" gorm:"size:256"`
	EnvConfigJSON          JSONMap    `json:"env_config" gorm:"type:json"`
	TaskStatus             string     `json:"task_status" gorm:"size:32;not null;index:idx_ai_task_project_status;index:idx_ai_task_mode_status"`
	FrameworkType          string     `json:"framework_type" gorm:"size:32;not null;default:Playwright"`
	LatestRecordingID      *uint      `json:"latest_recording_id"`
	CurrentScriptVersionID *uint      `json:"current_script_version_id" gorm:"index"`
	LatestValidationID     *uint      `json:"latest_validation_id"`
	LatestValidationStatus string     `json:"latest_validation_status" gorm:"size:32"`
	LatestExecuteAt        *time.Time `json:"latest_execute_at"`
	LatestConfirmedAt      *time.Time `json:"latest_confirmed_at"`
	LatestConfirmedBy      *uint      `json:"latest_confirmed_by"`
	DiscardReason          string     `json:"discard_reason" gorm:"size:255"`
	Remark                 string     `json:"remark" gorm:"size:500"`
	CreatedBy              uint       `json:"created_by" gorm:"not null;index"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedBy              uint       `json:"updated_by" gorm:"not null"`
	UpdatedAt              time.Time  `json:"updated_at"`

	// 虚拟字段（不入库）
	ProjectName string           `json:"project_name" gorm:"-"`
	CreatedName string           `json:"created_name" gorm:"-"`
	CaseCount   int64            `json:"case_count" gorm:"-"`
	CaseTags    []string         `json:"case_tags" gorm:"-"`
	Permissions *ActionPermissions `json:"permissions,omitempty" gorm:"-"`
}

// AIScriptTaskCaseRel 测试智编-任务与用例关系表
type AIScriptTaskCaseRel struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	TaskID    uint      `json:"task_id" gorm:"not null;uniqueIndex:uk_ai_task_case"`
	CaseID    uint      `json:"case_id" gorm:"not null;uniqueIndex:uk_ai_task_case;index"`
	CreatedBy uint      `json:"created_by" gorm:"not null"`
	CreatedAt time.Time `json:"created_at"`
}

// AIScriptVersion 测试智编-脚本版本表
type AIScriptVersion struct {
	ID                 uint       `json:"id" gorm:"primaryKey"`
	TaskID             uint       `json:"task_id" gorm:"not null;uniqueIndex:uk_ai_task_version;index:idx_ai_script_current"`
	VersionNo          int        `json:"version_no" gorm:"not null;uniqueIndex:uk_ai_task_version"`
	FrameworkType      string     `json:"framework_type" gorm:"size:32;not null;default:Playwright"`
	ScriptName         string     `json:"script_name" gorm:"size:128"`
	ScriptStatus       string     `json:"script_status" gorm:"size:32;not null"`
	ValidationStatus   string     `json:"validation_status" gorm:"size:32;not null"`
	SourceType         string     `json:"source_type" gorm:"size:32;not null"`
	SourceRecordingID  *uint      `json:"source_recording_id"`
	ScriptContent      string     `json:"script_content" gorm:"type:longtext;not null"`
	StepModelJSON      JSONMap    `json:"step_model_json" gorm:"type:json"`
	CommentText        string     `json:"comment_text" gorm:"size:500"`
	BasedOnVersionID   *uint      `json:"based_on_version_id"`
	IsCurrentFlag      bool       `json:"is_current_flag" gorm:"not null;default:false;index:idx_ai_script_current"`
	LatestValidationID *uint      `json:"latest_validation_id"`
	ConfirmedBy        *uint      `json:"confirmed_by"`
	ConfirmedAt        *time.Time `json:"confirmed_at"`
	CreatedBy          uint       `json:"created_by" gorm:"not null"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedBy          uint       `json:"updated_by" gorm:"not null"`
	UpdatedAt          time.Time  `json:"updated_at"`

	// V1 多项目工程化字段
	ProjectKeySnapshot    string  `json:"project_key_snapshot" gorm:"size:64"`
	VersionStatus         string  `json:"version_status" gorm:"size:32"`
	GenerationSummary     string  `json:"generation_summary" gorm:"type:text"`
	ManualReviewStatus    string  `json:"manual_review_status" gorm:"size:32"`
	RegistrySnapshotJSON  RawJSON `json:"registry_snapshot" gorm:"type:json;column:registry_snapshot_json"`
	WorkspaceRootSnapshot string  `json:"workspace_root_snapshot" gorm:"size:256"`
	BaseFixtureHash       string  `json:"base_fixture_hash" gorm:"size:64"`

	// 虚拟字段
	CreatedName string `json:"created_name" gorm:"-"`
}

// AIScriptValidation 测试智编-回放验证记录表
type AIScriptValidation struct {
	ID                   uint       `json:"id" gorm:"primaryKey"`
	ScriptVersionID      uint       `json:"script_version_id" gorm:"not null;index:idx_ai_validation_script"`
	TaskID               uint       `json:"task_id" gorm:"not null;index:idx_ai_validation_task"`
	TriggerType          string     `json:"trigger_type" gorm:"size:32;not null;default:MANUAL"`
	ValidationStatus     string     `json:"validation_status" gorm:"size:32;not null"`
	TotalStepCount       int        `json:"total_step_count"`
	PassedStepCount      int        `json:"passed_step_count"`
	FailedStepNo         *int       `json:"failed_step_no"`
	FailReason           string     `json:"fail_reason" gorm:"type:text"`
	AssertionSummaryJSON RawJSON    `json:"assertion_summary" gorm:"type:json;column:assertion_summary_json"`
	ExecutionLogsJSON    RawJSON    `json:"-" gorm:"type:json;column:execution_logs_json"`
	StartedAt            time.Time  `json:"started_at"`
	FinishedAt           *time.Time `json:"finished_at"`
	DurationMs           *int64     `json:"duration_ms"`
	TriggeredBy          uint       `json:"triggered_by" gorm:"not null"`
	CreatedAt            time.Time  `json:"created_at"`

	// 虚拟字段（API 序列化用，不存 DB）
	TriggeredName string                `json:"triggered_name" gorm:"-"`
	Logs          json.RawMessage       `json:"logs" gorm:"-"`
	Screenshots   []AIScriptEvidence    `json:"screenshots" gorm:"-"`
}

// AIScriptTrace 测试智编-结构化轨迹表
type AIScriptTrace struct {
	ID               uint      `json:"id" gorm:"primaryKey"`
	TaskID           uint      `json:"task_id" gorm:"not null;uniqueIndex:uk_ai_task_trace_no"`
	TraceNo          int       `json:"trace_no" gorm:"not null;uniqueIndex:uk_ai_task_trace_no"`
	ActionType       string    `json:"action_type" gorm:"size:32;not null"`
	PageURL          string    `json:"page_url" gorm:"size:1024"`
	TargetSummary    string    `json:"target_summary" gorm:"size:512"`
	LocatorUsed      string    `json:"locator_used" gorm:"size:1024"`
	InputValueMasked string    `json:"input_value_masked" gorm:"type:text"`
	ActionResult     string    `json:"action_result" gorm:"size:64"`
	ErrorMessage     string    `json:"error_message" gorm:"type:text"`
	ScreenshotURL    string    `json:"screenshot_url" gorm:"size:1024"`
	OccurredAt       string    `json:"occurred_at" gorm:"size:32"`
	CreatedAt        time.Time `json:"created_at"`
}

// AIScriptEvidence 测试智编-证据表
type AIScriptEvidence struct {
	ID              uint      `json:"id" gorm:"primaryKey"`
	TaskID          uint      `json:"task_id" gorm:"not null;index:idx_ai_evidence_task"`
	ScriptVersionID *uint     `json:"script_version_id" gorm:"index"`
	ValidationID    *uint     `json:"validation_id" gorm:"index"`
	EvidenceType    string    `json:"evidence_type" gorm:"size:32;not null"` // SCREENSHOT / LOG / DOM / OTHER
	FileName        string    `json:"file_name" gorm:"size:255"`
	FileURL         string    `json:"file_url" gorm:"size:1024"`
	ContentText     string    `json:"content_text" gorm:"type:longtext"`
	TraceNo         *int      `json:"trace_no"`
	Caption         string    `json:"caption" gorm:"size:255"`
	CreatedBy       *uint     `json:"created_by"`
	CreatedAt       time.Time `json:"created_at"`
}

// AIScriptOperationLog 测试智编-操作审计日志表
type AIScriptOperationLog struct {
	ID              uint      `json:"id" gorm:"primaryKey"`
	TaskID          *uint     `json:"task_id" gorm:"index:idx_ai_oplog_task"`
	ScriptVersionID *uint     `json:"script_version_id"`
	OperationType   string    `json:"operation_type" gorm:"size:64;not null"`
	OperatorID      uint      `json:"operator_id" gorm:"not null;index"`
	OperatorName    string    `json:"operator_name" gorm:"size:128"`
	OperationDesc   string    `json:"operation_desc" gorm:"size:500"`
	CreatedAt       time.Time `json:"created_at"`
}

// AIScriptRecordingSession 测试智编-录制会话表
type AIScriptRecordingSession struct {
	ID               uint       `json:"id" gorm:"primaryKey"`
	TaskID           uint       `json:"task_id" gorm:"not null;index:idx_ai_recording_task"`
	RecorderType     string     `json:"recorder_type" gorm:"size:32;not null;default:PLAYWRIGHT_CODEGEN"`
	RecordingStatus  string     `json:"recording_status" gorm:"size:32;not null;index:idx_ai_recording_status"`
	RawScriptContent string     `json:"raw_script_content" gorm:"type:longtext"`
	StepModelJSON    JSONMap    `json:"step_model_json" gorm:"type:json"`
	ArtifactRefsJSON JSONMap    `json:"artifact_refs" gorm:"type:json"`
	FailReason       string     `json:"fail_reason" gorm:"type:text"`
	CreatedBy        uint       `json:"created_by" gorm:"not null"`
	CreatedAt        time.Time  `json:"created_at"`
	FinishedAt       *time.Time `json:"finished_at"`
}

// ActionPermissions 操作权限标识（不入库，用于接口返回）
type ActionPermissions struct {
	CanExecute  bool `json:"can_execute"`
	CanValidate bool `json:"can_validate"`
	CanEdit     bool `json:"can_edit"`
	CanConfirm  bool `json:"can_confirm"`
	CanExport   bool `json:"can_export"`
	CanDiscard  bool `json:"can_discard"`
	CanDelete   bool `json:"can_delete"`
}

// ── V1 多项目工程化新增模型 ──

// AIScriptFile 测试智编-生成文件明细表
// 记录每次生成产出的所有文件（spec / page / shared / fixture / registry）
type AIScriptFile struct {
	ID                   uint      `json:"id" gorm:"primaryKey"`
	ProjectID            uint      `json:"project_id" gorm:"not null;index:idx_script_file_project"`
	TaskID               uint      `json:"task_id" gorm:"not null;index:idx_script_file_task"`
	VersionID            uint      `json:"version_id" gorm:"not null;index:idx_script_file_version;uniqueIndex:uk_version_path"`
	FileType             string    `json:"file_type" gorm:"size:32;not null"`             // spec / page / shared / fixture / registry
	RelativePath         string    `json:"relative_path" gorm:"size:512;not null;uniqueIndex:uk_version_path"` // 相对项目根的路径
	Content              string    `json:"content" gorm:"type:longtext"`
	ContentHash          string    `json:"content_hash" gorm:"size:64"`
	SourceKind           string    `json:"source_kind" gorm:"size:32"`                    // create / update / generated / rebuilt
	ManualReviewRequired bool      `json:"manual_review_required" gorm:"default:false"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

// AIScriptWorkspaceLock 项目级工作区锁表
// 防止同一项目工作区被并发写操作破坏
type AIScriptWorkspaceLock struct {
	ID             uint      `json:"id" gorm:"primaryKey"`
	ProjectID      uint      `json:"project_id" gorm:"not null;uniqueIndex:uk_lock_project"`
	LockKey        string    `json:"lock_key" gorm:"size:128;not null"`
	LockType       string    `json:"lock_type" gorm:"size:32;not null"` // workspace_write / validate_run
	OwnerTaskID    *uint     `json:"owner_task_id"`
	OwnerVersionID *uint     `json:"owner_version_id"`
	OwnerRequestID string    `json:"owner_request_id" gorm:"size:64"`
	HeartbeatAt    time.Time `json:"heartbeat_at"`
	ExpiresAt      time.Time `json:"expires_at"`
	Status         string    `json:"status" gorm:"size:32;not null"` // active / released / expired
	CreatedAt      time.Time `json:"created_at"`
}

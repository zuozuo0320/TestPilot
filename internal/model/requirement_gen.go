// Package model — 需求智生模块实体定义
//
// 包含 4 张新表的 GORM 模型：
//   - RequirementDoc      需求文档表
//   - RequirementGenTask  生成任务表
//   - RequirementGenResult AI 产物表（预览池）
//   - AISkill             Skill 模板表
//
// 并发设计：所有可被并发修改的实体携带 lock_version 字段，
// Service 层通过 CAS (UPDATE ... WHERE lock_version=?) 保护。
package model

import (
	"time"

	"gorm.io/gorm"
)

// ===================== 需求智生模块常量 =====================

const (
	// ---- 需求文档解析状态 ----
	DocParseStatusNotParsed   = "not_parsed"
	DocParseStatusParsing     = "parsing"
	DocParseStatusParsed      = "parsed"
	DocParseStatusParseFailed = "parse_failed"

	// ---- 需求文档来源类型 ----
	DocSourceTypeUpload      = "upload_file"
	DocSourceTypePaste       = "paste_text"
	DocSourceTypeGitLabIssue = "gitlab_issue"

	// ---- 需求文档外部来源同步状态 ----
	DocSourceSyncStatusSynced = "synced"
	DocSourceSyncStatusFailed = "failed"

	// ---- 生成任务状态 ----
	GenTaskStatusPending        = "PENDING"
	GenTaskStatusRunning        = "RUNNING"
	GenTaskStatusSuccess        = "SUCCESS"
	GenTaskStatusFailed         = "FAILED"
	GenTaskStatusPartialAdopted = "PARTIAL_ADOPTED"
	GenTaskStatusFullyClosed    = "FULLY_CLOSED"

	// ---- Skill 作用域 ----
	SkillScopeFunctional  = "functional"
	SkillScopeAPI         = "api"
	SkillScopeCompat      = "compat"
	SkillScopeSecurity    = "security"
	SkillScopeBoundary    = "boundary"
	SkillScopeState       = "state"
	SkillScopePerformance = "performance"
	SkillScopeE2E         = "e2e"
	SkillScopeCustom      = "custom"
)

// ===================== RequirementDoc 需求文档表 =====================

// RequirementDoc 需求文档实体。
// 支持文件上传和粘贴文本两种来源；文件上传后异步解析为纯文本。
type RequirementDoc struct {
	ID                uint           `json:"id" gorm:"primaryKey"`
	ProjectID         uint           `json:"project_id" gorm:"not null;index:idx_rd_project_created;index:idx_rd_project_status"`
	Title             string         `json:"title" gorm:"size:200;not null"`
	SourceType        string         `json:"source_type" gorm:"size:20;not null"`           // upload_file / paste_text
	FileFormat        string         `json:"file_format" gorm:"size:20;not null"`           // docx / pdf / md / txt / text
	FilePath          string         `json:"file_path" gorm:"size:500;not null;default:''"` // 原始文件相对路径
	FileSize          int64          `json:"file_size" gorm:"not null;default:0"`
	RawContent        *string        `json:"raw_content" gorm:"type:longtext"`              // 解析后纯文本
	WordCount         int            `json:"word_count" gorm:"not null;default:0"`          // 截断后字数
	OriginalWordCount int            `json:"original_word_count" gorm:"not null;default:0"` // 截断前原始字数
	Truncated         bool           `json:"truncated" gorm:"not null;default:false"`
	ParseStatus       string         `json:"parse_status" gorm:"size:20;not null;default:not_parsed;index:idx_rd_project_status;index:idx_rd_parse_stuck"`
	ParseError        string         `json:"parse_error" gorm:"size:1000;not null;default:''"`
	ParseStartedAt    *time.Time     `json:"parse_started_at" gorm:"index:idx_rd_parse_stuck"` // 上次开始解析时间
	Remark            string         `json:"remark" gorm:"size:500;not null;default:''"`
	LockVersion       int            `json:"lock_version" gorm:"not null;default:0"`
	CreatedBy         uint           `json:"created_by" gorm:"not null;index:idx_rd_creator"`
	CreatedAt         time.Time      `json:"created_at" gorm:"index:idx_rd_project_created"`
	UpdatedAt         time.Time      `json:"updated_at"`
	DeletedAt         gorm.DeletedAt `json:"deleted_at" gorm:"index:idx_rd_deleted"`

	// 虚拟字段（不入库，API 返回时填充）
	TaskCount       int64  `json:"task_count" gorm:"-"`
	CaseCount       int64  `json:"case_count" gorm:"-"`
	CreatedByName   string `json:"created_by_name,omitempty" gorm:"-"`
	CreatedByAvatar string `json:"created_by_avatar,omitempty" gorm:"-"`
	SourceURL       string `json:"source_url,omitempty" gorm:"-"`
	SyncStatus      string `json:"sync_status,omitempty" gorm:"-"`
}

// TableName 指定表名
func (RequirementDoc) TableName() string {
	return "requirement_docs"
}

// RequirementDocSource 需求文档外部来源追溯表。
// 用于记录 GitLab Issue 等外部需求源与 requirement_docs 的映射关系。
type RequirementDocSource struct {
	ID                  uint       `json:"id" gorm:"primaryKey"`
	ProjectID           uint       `json:"project_id" gorm:"not null;index:idx_rds_project;uniqueIndex:uk_rds_external"`
	RequirementDocID    uint       `json:"requirement_doc_id" gorm:"not null;index:idx_rds_doc"`
	SourceType          string     `json:"source_type" gorm:"size:30;not null;uniqueIndex:uk_rds_external"`
	ExternalSystem      string     `json:"external_system" gorm:"size:30;not null"`
	SourceURL           string     `json:"source_url" gorm:"size:1000;not null;default:''"`
	ExternalProjectID   string     `json:"external_project_id" gorm:"size:100;not null;default:''"`
	ExternalProjectPath string     `json:"external_project_path" gorm:"size:500;not null;default:''"`
	ExternalIssueIID    int        `json:"external_issue_iid" gorm:"not null;default:0"`
	ExternalKey         string     `json:"external_key" gorm:"size:128;not null;uniqueIndex:uk_rds_external"`
	VersionNo           int        `json:"version_no" gorm:"not null;default:1;uniqueIndex:uk_rds_external"`
	ExternalUpdatedAt   *time.Time `json:"external_updated_at"`
	LastSyncedAt        *time.Time `json:"last_synced_at"`
	SyncStatus          string     `json:"sync_status" gorm:"size:20;not null;default:synced"`
	SyncError           string     `json:"sync_error" gorm:"size:1000;not null;default:''"`
	CreatedBy           uint       `json:"created_by" gorm:"not null;default:0"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

// TableName 指定表名
func (RequirementDocSource) TableName() string {
	return "requirement_doc_sources"
}

// ===================== RequirementGenTask 生成任务表 =====================

// RequirementGenTask 需求智生-生成任务实体。
// 异步任务模式：PENDING → RUNNING → SUCCESS/FAILED → PARTIAL_ADOPTED/FULLY_CLOSED。
type RequirementGenTask struct {
	ID               uint   `json:"id" gorm:"primaryKey"`
	ProjectID        uint   `json:"project_id" gorm:"not null;index:idx_rgt_project_status;index:idx_rgt_project_created"`
	RequirementDocID uint   `json:"requirement_doc_id" gorm:"not null;index:idx_rgt_doc"`
	SkillID          uint   `json:"skill_id" gorm:"not null;index:idx_rgt_skill"`
	AIModelConfigID  uint   `json:"ai_model_config_id" gorm:"not null"`
	AIModelSnapshot  string `json:"ai_model_snapshot" gorm:"size:200;not null;default:''"` // provider/model_id 文本快照
	TaskName         string `json:"task_name" gorm:"size:200;not null"`
	TargetModuleID   uint   `json:"target_module_id" gorm:"not null;default:0"`
	DefaultLevel     string `json:"default_level" gorm:"size:10;not null;default:P2"`
	MaxCases         int    `json:"max_cases" gorm:"not null;default:30"`
	ExtraPrompt      string `json:"extra_prompt" gorm:"type:text"`   // 用户补充上下文
	SkillSnapshot    string `json:"skill_snapshot" gorm:"type:text"` // 所有匹配 Skill 的 JSON 快照
	ParentTaskID     uint   `json:"parent_task_id" gorm:"not null;default:0;index:idx_rgt_parent"`

	Status     string `json:"status" gorm:"size:20;not null;default:PENDING;index:idx_rgt_project_status;index:idx_rgt_status_heartbeat"`
	FailReason string `json:"fail_reason" gorm:"size:1000;not null;default:''"`

	GeneratedCount int `json:"generated_count" gorm:"not null;default:0"`
	AdoptedCount   int `json:"adopted_count" gorm:"not null;default:0"`
	DiscardedCount int `json:"discarded_count" gorm:"not null;default:0"`

	PromptTokens     int   `json:"prompt_tokens" gorm:"not null;default:0"`
	CompletionTokens int   `json:"completion_tokens" gorm:"not null;default:0"`
	DurationMs       int64 `json:"duration_ms" gorm:"not null;default:0"`

	StartedAt       *time.Time `json:"started_at"`
	FinishedAt      *time.Time `json:"finished_at"`
	LastHeartbeatAt *time.Time `json:"last_heartbeat_at" gorm:"index:idx_rgt_status_heartbeat"`

	ExecutorNodeID string `json:"executor_node_id" gorm:"size:64;not null;default:''"`
	RequestID      string `json:"request_id" gorm:"size:64;not null;default:''"`

	LockVersion int       `json:"lock_version" gorm:"not null;default:0"`
	CreatedBy   uint      `json:"created_by" gorm:"not null;index:idx_rgt_creator"`
	CreatedAt   time.Time `json:"created_at" gorm:"index:idx_rgt_project_created"`
	UpdatedAt   time.Time `json:"updated_at"`

	// 虚拟字段（不入库，API 返回时填充）
	RequirementDocTitle string `json:"requirement_doc_title,omitempty" gorm:"-"`
	SkillName           string `json:"skill_name,omitempty" gorm:"-"`
	CreatedByName       string `json:"created_by_name,omitempty" gorm:"-"`
}

// TableName 指定表名
func (RequirementGenTask) TableName() string {
	return "requirement_gen_tasks"
}

// IsTerminal 判断任务是否已为终态
func (t *RequirementGenTask) IsTerminal() bool {
	switch t.Status {
	case GenTaskStatusSuccess, GenTaskStatusFailed, GenTaskStatusPartialAdopted, GenTaskStatusFullyClosed:
		return true
	default:
		return false
	}
}

// ===================== RequirementGenResult AI 产物表 =====================

// RequirementGenResult 需求智生-AI 产物实体（预览池）。
// 每条产物对应 LLM 生成的一个测试用例草稿，用户可预览后采纳入库或丢弃。
type RequirementGenResult struct {
	ID        uint `json:"id" gorm:"primaryKey"`
	TaskID    uint `json:"task_id" gorm:"not null;uniqueIndex:uk_task_seq;index:idx_rgr_task_adopted;index:idx_rgr_task_discarded"`
	ProjectID uint `json:"project_id" gorm:"not null"` // 冗余项目 ID
	SeqNo     int  `json:"seq_no" gorm:"not null;uniqueIndex:uk_task_seq"`

	Title         string  `json:"title" gorm:"size:200;not null"`
	Level         string  `json:"level" gorm:"size:10;not null;default:P2"`
	Precondition  string  `json:"precondition" gorm:"type:text"`
	Steps         string  `json:"steps" gorm:"type:longtext"` // JSON 数组
	Postcondition string  `json:"postcondition" gorm:"type:text"`
	Remark        string  `json:"remark" gorm:"type:text"`
	TagsSuggested string  `json:"tags_suggested" gorm:"size:500;not null;default:''"` // 逗号分隔
	AIConfidence  float64 `json:"ai_confidence" gorm:"type:decimal(3,2)"`
	RawJSON       string  `json:"raw_json,omitempty" gorm:"type:longtext"` // LLM 原始输出

	Adopted       bool       `json:"adopted" gorm:"not null;default:false;index:idx_rgr_task_adopted"`
	AdoptedCaseID uint       `json:"adopted_case_id" gorm:"not null;default:0;index:idx_rgr_adopted_case"`
	AdoptedAt     *time.Time `json:"adopted_at"`
	AdoptedBy     uint       `json:"adopted_by" gorm:"not null;default:0"`

	Discarded   bool       `json:"discarded" gorm:"not null;default:false;index:idx_rgr_task_discarded"`
	DiscardedAt *time.Time `json:"discarded_at"`
	DiscardedBy uint       `json:"discarded_by" gorm:"not null;default:0"`

	LockVersion int       `json:"lock_version" gorm:"not null;default:0"`
	CreatedAt   time.Time `json:"created_at"`
}

// TableName 指定表名
func (RequirementGenResult) TableName() string {
	return "requirement_gen_results"
}

// IsPending 产物是否处于待处理状态（未采纳且未丢弃）
func (r *RequirementGenResult) IsPending() bool {
	return !r.Adopted && !r.Discarded
}

// ===================== AISkill Skill 模板表 =====================

// AISkill 需求智生-Skill 模板实体。
// 系统内置 Skill (project_id=0, is_system=true) 全局可用；
// 项目副本 (project_id>0, is_system=false) 可覆写系统内置同 skill_key 的 Skill。
type AISkill struct {
	ID             uint           `json:"id" gorm:"primaryKey"`
	ProjectID      uint           `json:"project_id" gorm:"not null;default:0;uniqueIndex:uk_skill_project_key"` // 0=系统内置
	SkillKey       string         `json:"skill_key" gorm:"size:50;not null;uniqueIndex:uk_skill_project_key"`
	Name           string         `json:"name" gorm:"size:100;not null"`
	Scope          string         `json:"scope" gorm:"size:20;not null"` // functional/api/compat/security/custom
	Description    string         `json:"description" gorm:"size:500;not null;default:''"`
	PromptTemplate string         `json:"prompt_template,omitempty" gorm:"type:longtext;not null"` // 含占位符的提示词模板
	OutputSchema   string         `json:"output_schema" gorm:"size:50;not null;default:standard_case_v1"`
	IsSystem       bool           `json:"is_system" gorm:"not null;default:false"`
	IsActive       bool           `json:"is_active" gorm:"not null;default:true;index:idx_skill_active"`
	SortOrder      int            `json:"sort_order" gorm:"not null;default:0"`
	LockVersion    int            `json:"lock_version" gorm:"not null;default:0"`
	CreatedBy      uint           `json:"created_by" gorm:"not null;default:0"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	DeletedAt      gorm.DeletedAt `json:"deleted_at" gorm:"index:idx_skill_deleted"`

	// 虚拟字段（API 返回时填充）
	EffectiveSource string `json:"effective_source,omitempty" gorm:"-"` // system / project_override
}

// TableName 指定表名
func (AISkill) TableName() string {
	return "ai_skills"
}

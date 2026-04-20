package model

import (
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

const (
	// ---- 全局角色标识 ----
	GlobalRoleAdmin     = "admin"
	GlobalRoleManager   = "manager"
	GlobalRoleTester    = "tester"
	GlobalRoleReviewer  = "reviewer"
	GlobalRoleDeveloper = "developer"
	GlobalRoleReadonly  = "readonly"

	// ---- 项目成员角色 ----
	MemberRoleOwner  = "owner"
	MemberRoleMember = "member"

	// ---- 项目状态 ----
	ProjectStatusActive   = "active"
	ProjectStatusArchived = "archived"

	// ---- 项目质量状态 ----
	ProjectQualityStatusStable   = "stable"
	ProjectQualityStatusDegraded = "degraded"
	ProjectQualityStatusFailing  = "failing"
	ProjectQualityStatusUnknown  = "unknown"

	// ---- 项目质量状态原因 ----
	ProjectQualityReasonNoTestCases              = "no_testcases"
	ProjectQualityReasonNoExecutionData          = "no_execution_data"
	ProjectQualityReasonLatestRunPassRateGE95    = "latest_run_pass_rate_ge_95"
	ProjectQualityReasonLatestRunPassRate80To95  = "latest_run_pass_rate_between_80_95"
	ProjectQualityReasonLatestRunPassRateBelow80 = "latest_run_pass_rate_lt_80"
	ProjectQualityReasonCasePassRateGE95         = "case_exec_pass_rate_ge_95"
	ProjectQualityReasonCasePassRate80To95       = "case_exec_pass_rate_between_80_95"
	ProjectQualityReasonCasePassRateBelow80      = "case_exec_pass_rate_lt_80"

	// ---- 测试用例状态 ----
	TestCaseStatusDraft     = "draft"     // 草稿
	TestCaseStatusPending   = "pending"   // 待评审
	TestCaseStatusActive    = "active"    // 已生效
	TestCaseStatusDiscarded = "discarded" // 已废弃

	// ---- 种子数据标识 ----
	SeedProjectName = "AiSight Demo"

	// ---- 用例评审：评审模式 ----
	ReviewModeSingle   = "single"   // 单人评审
	ReviewModeParallel = "parallel" // 多人评审

	// ---- 用例评审：计划状态 ----
	ReviewPlanStatusNotStarted = "not_started" // 未开始
	ReviewPlanStatusInProgress = "in_progress" // 进行中
	ReviewPlanStatusCompleted  = "completed"   // 已完成
	ReviewPlanStatusClosed     = "closed"      // 已关闭

	// ---- 用例评审：评审项状态 ----
	ReviewItemStatusPending   = "pending"   // 待评审
	ReviewItemStatusReviewing = "reviewing" // 评审中
	ReviewItemStatusCompleted = "completed" // 已出最终结果

	// ---- 用例评审：评审结果 ----
	ReviewResultPending     = "pending"      // 待评审
	ReviewResultApproved    = "approved"     // 通过
	ReviewResultRejected    = "rejected"     // 不通过
	ReviewResultNeedsUpdate = "needs_update" // 建议修改

	// ---- 用例评审：评审人处理状态 ----
	ReviewerStatusPending  = "pending"  // 尚未评审
	ReviewerStatusReviewed = "reviewed" // 已提交评审

	// ---- 用例主表 review_result 回写文案 ----
	CaseReviewResultNotReviewed = "未评审"
	CaseReviewResultPending     = "待评审"
	CaseReviewResultResubmit    = "重新提审"
	CaseReviewResultApproved    = "已通过"
	CaseReviewResultRejected    = "已驳回"
	CaseReviewResultNeedsUpdate = "建议修改"
)

// PresetRoleDisplayNames 预置角色的中文显示名映射
var PresetRoleDisplayNames = map[string]string{
	GlobalRoleAdmin:     "系统管理员",
	GlobalRoleManager:   "项目管理员",
	GlobalRoleTester:    "测试工程师",
	GlobalRoleReviewer:  "评审员",
	GlobalRoleDeveloper: "开发工程师",
	GlobalRoleReadonly:  "只读访客",
}

// User 用户实体
type User struct {
	ID           uint           `json:"id" gorm:"primaryKey"`
	Name         string         `json:"name" gorm:"size:80;not null"`
	Email        string         `json:"email" gorm:"size:120;uniqueIndex;not null"`
	Phone        string         `json:"phone" gorm:"size:30;index"`
	Avatar       string         `json:"avatar" gorm:"size:500"`
	PasswordHash string         `json:"-" gorm:"column:password_hash;size:255;not null;default:''"`
	Role         string         `json:"role" gorm:"size:20;not null;default:readonly;index"` // 缓存主角色，兼容 JWT/旧逻辑
	RoleNames    []string       `json:"role_names" gorm:"-"`                                 // 虚拟字段：成员管理等场景回填多角色显示名
	Active       bool           `json:"active" gorm:"not null;default:true"`
	LastLoginAt  *time.Time     `json:"last_login_at" gorm:"index"`
	DeletedAt    gorm.DeletedAt `json:"deleted_at" gorm:"index"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// Role 角色实体
type Role struct {
	ID          uint           `json:"id" gorm:"primaryKey"`
	Name        string         `json:"name" gorm:"size:80;uniqueIndex;not null"`
	DisplayName string         `json:"display_name" gorm:"size:80"`
	Description string         `json:"description" gorm:"size:500"`
	UserCount   int64          `json:"user_count" gorm:"-"` // 虚拟字段：SQL 子查询填充
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `json:"deleted_at" gorm:"index"`
}

type UserRole struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	UserID    uint      `json:"user_id" gorm:"not null;index:idx_user_role,unique"`
	RoleID    uint      `json:"role_id" gorm:"not null;index:idx_user_role,unique"`
	CreatedAt time.Time `json:"created_at"`
}

type UserProject struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	UserID    uint      `json:"user_id" gorm:"not null;index:idx_user_project,unique"`
	ProjectID uint      `json:"project_id" gorm:"not null;index:idx_user_project,unique"`
	CreatedAt time.Time `json:"created_at"`
}

type AuditLog struct {
	ID         uint      `json:"id" gorm:"primaryKey"`
	ActorID    uint      `json:"actor_id" gorm:"not null;index"`
	Action     string    `json:"action" gorm:"size:80;not null"`
	TargetType string    `json:"target_type" gorm:"size:50;not null;index"`
	TargetID   uint      `json:"target_id" gorm:"not null;index"`
	BeforeData string    `json:"before_data" gorm:"type:text"`
	AfterData  string    `json:"after_data" gorm:"type:text"`
	CreatedAt  time.Time `json:"created_at"`
}

// Project 项目实体
type Project struct {
	ID          uint       `json:"id" gorm:"primaryKey"`
	Name        string     `json:"name" gorm:"size:120;uniqueIndex;not null"`
	Description string     `json:"description" gorm:"size:500"`
	Avatar      string     `json:"avatar" gorm:"size:500"`
	OwnerID     uint       `json:"owner_id" gorm:"not null;default:0;index"`
	Status      string     `json:"status" gorm:"size:20;not null;default:active;index"`
	ArchivedAt  *time.Time `json:"archived_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`

	// 虚拟字段（不入库，API 返回时填充）
	MemberCount           int64    `json:"member_count" gorm:"-"`            // 虚拟字段：SQL 子查询填充
	TestCaseCount         int64    `json:"testcase_count" gorm:"-"`          // 虚拟字段：SQL 子查询填充
	TestCaseTotalCount    int64    `json:"testcase_total_count" gorm:"-"`    // 虚拟字段：兼容项目列表真实摘要口径
	ExecutedTestCaseCount int64    `json:"executed_testcase_count" gorm:"-"` // 虚拟字段：SQL 子查询填充
	TestProgress          float64  `json:"test_progress" gorm:"-"`           // 虚拟字段：service 层计算
	QualityStatus         string   `json:"quality_status" gorm:"-"`          // 虚拟字段：service 层计算
	QualityReason         string   `json:"quality_reason" gorm:"-"`          // 虚拟字段：service 层计算
	LatestRunPassRate     *float64 `json:"latest_run_pass_rate" gorm:"-"`    // 虚拟字段：SQL 子查询填充
	LatestRunResultCount  int64    `json:"-" gorm:"-"`                       // 虚拟字段：仅供 service 判定质量状态
	PassedTestCaseCount   int64    `json:"-" gorm:"-"`                       // 虚拟字段：仅供 service 回退计算质量状态
	OwnerName             string   `json:"owner_name" gorm:"-"`
	OwnerAvatar           string   `json:"owner_avatar" gorm:"-"`
}

type ProjectMember struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	ProjectID uint      `json:"project_id" gorm:"not null;index:idx_project_user,unique"`
	Project   Project   `json:"-"`
	UserID    uint      `json:"user_id" gorm:"not null;index:idx_project_user,unique"`
	User      User      `json:"user"`
	Role      string    `json:"role" gorm:"size:20;not null"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Requirement struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	ProjectID uint      `json:"project_id" gorm:"not null;index;uniqueIndex:idx_project_requirement_title"`
	Title     string    `json:"title" gorm:"size:200;not null;uniqueIndex:idx_project_requirement_title"`
	Content   string    `json:"content" gorm:"type:text"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type TestCase struct {
	ID           uint      `json:"id" gorm:"primaryKey"`
	ProjectID    uint      `json:"project_id" gorm:"not null;index;uniqueIndex:idx_project_testcase_title"`
	Title        string    `json:"title" gorm:"size:200;not null;uniqueIndex:idx_project_testcase_title"`
	Status       string    `json:"status" gorm:"size:20;not null;default:draft;index"` // 草稿(draft)/待评审(pending)/已生效(active)/已废弃(discarded)
	Version      string    `json:"version" gorm:"size:20;not null;default:V1"`         // 版本号，如 V1, V2...
	Level        string    `json:"level" gorm:"size:10;default:P1"`
	ReviewResult string    `json:"review_result" gorm:"size:30;default:未评审"`
	ExecResult   string    `json:"exec_result" gorm:"size:30;default:未执行"`
	ModuleID     uint      `json:"module_id" gorm:"default:0;index"`
	ModulePath   string    `json:"module_path" gorm:"size:255;default:/"`
	Tags         string    `json:"tags" gorm:"size:500"`
	Precondition  string    `json:"precondition" gorm:"type:text"`
	Postcondition string    `json:"postcondition" gorm:"type:text"`
	Steps         string    `json:"steps" gorm:"type:text"`
	Remark       string    `json:"remark" gorm:"type:text"`
	Priority     string    `json:"priority" gorm:"size:20;default:medium"`
	CreatedBy    uint      `json:"created_by" gorm:"not null;default:0;index"`
	UpdatedBy    uint      `json:"updated_by" gorm:"not null;default:0;index"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`

	// 虚拟字段：用例评审模块关联摘要
	InReview           bool   `json:"in_review" gorm:"-"`
	CurrentReviewID    uint   `json:"current_review_id" gorm:"-"`
	CurrentReviewName  string `json:"current_review_name" gorm:"-"`
	RelatedReviewCount int64  `json:"related_review_count" gorm:"-"`
}

// Module 用例目录树节点
type Module struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	ProjectID uint      `json:"project_id" gorm:"not null;index"`
	ParentID  uint      `json:"parent_id" gorm:"default:0;index"`
	Name      string    `json:"name" gorm:"size:100;not null"`
	SortOrder int       `json:"sort_order" gorm:"default:0"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CaseAttachment 用例附件
type CaseAttachment struct {
	ID         uint      `json:"id" gorm:"primaryKey"`
	TestCaseID uint      `json:"testcase_id" gorm:"not null;index"`
	FileName   string    `json:"file_name" gorm:"size:255;not null"`
	FilePath   string    `json:"file_path" gorm:"size:500;not null"`
	FileSize   int64     `json:"file_size"`
	MimeType   string    `json:"mime_type" gorm:"size:100"`
	CreatedBy  uint      `json:"created_by" gorm:"default:0"`
	CreatedAt  time.Time `json:"created_at"`
}

// CaseHistory 用例编辑历史
type CaseHistory struct {
	ID         uint      `json:"id" gorm:"primaryKey"`
	TestCaseID uint      `json:"testcase_id" gorm:"not null;index"`
	Action     string    `json:"action" gorm:"size:20;not null"`
	FieldName  string    `json:"field_name" gorm:"size:50"`
	OldValue   string    `json:"old_value" gorm:"type:longtext"`
	NewValue   string    `json:"new_value" gorm:"type:longtext"`
	ChangedBy  uint      `json:"changed_by" gorm:"not null;index"`
	CreatedAt  time.Time `json:"created_at"`
}

// CaseRelation 用例关联关系
type CaseRelation struct {
	ID           uint      `json:"id" gorm:"primaryKey"`
	SourceCaseID uint      `json:"source_case_id" gorm:"not null;index"`
	TargetCaseID uint      `json:"target_case_id" gorm:"not null;index"`
	RelationType string    `json:"relation_type" gorm:"size:20;not null"`
	CreatedBy    uint      `json:"created_by" gorm:"default:0"`
	CreatedAt    time.Time `json:"created_at"`
}

type Script struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	ProjectID uint      `json:"project_id" gorm:"not null;index;uniqueIndex:idx_project_script_name"`
	Name      string    `json:"name" gorm:"size:200;not null;uniqueIndex:idx_project_script_name"`
	Path      string    `json:"path" gorm:"size:255;not null"`
	Type      string    `json:"type" gorm:"size:30;default:cypress"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type RequirementTestCase struct {
	RequirementID uint      `json:"requirement_id" gorm:"primaryKey"`
	TestCaseID    uint      `json:"testcase_id" gorm:"primaryKey"`
	CreatedAt     time.Time `json:"created_at"`
}

type TestCaseScript struct {
	TestCaseID uint      `json:"testcase_id" gorm:"primaryKey"`
	ScriptID   uint      `json:"script_id" gorm:"primaryKey"`
	CreatedAt  time.Time `json:"created_at"`
}

type Run struct {
	ID          uint      `json:"id" gorm:"primaryKey"`
	ProjectID   uint      `json:"project_id" gorm:"not null;index"`
	TriggeredBy uint      `json:"triggered_by" gorm:"not null;index"`
	Mode        string    `json:"mode" gorm:"size:20;not null"`
	Status      string    `json:"status" gorm:"size:20;not null"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type RunResult struct {
	ID         uint      `json:"id" gorm:"primaryKey"`
	RunID      uint      `json:"run_id" gorm:"not null;index"`
	ProjectID  uint      `json:"project_id" gorm:"not null;index"`
	ScriptID   uint      `json:"script_id" gorm:"not null;index"`
	Status     string    `json:"status" gorm:"size:20;not null"`
	Output     string    `json:"output" gorm:"type:text"`
	DurationMS int       `json:"duration_ms"`
	CreatedAt  time.Time `json:"created_at"`
}

type Defect struct {
	ID          uint      `json:"id" gorm:"primaryKey"`
	ProjectID   uint      `json:"project_id" gorm:"not null;index"`
	RunResultID uint      `json:"run_result_id" gorm:"not null;index"`
	Title       string    `json:"title" gorm:"size:200;not null"`
	Description string    `json:"description" gorm:"type:text"`
	Severity    string    `json:"severity" gorm:"size:20;default:medium"`
	Status      string    `json:"status" gorm:"size:20;default:open"`
	CreatedBy   uint      `json:"created_by" gorm:"not null;index"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ===================== 用例评审模块 =====================

// CaseReview 评审计划主表
type CaseReview struct {
	ID                 uint       `json:"id" gorm:"primaryKey"`
	ProjectID          uint       `json:"project_id" gorm:"not null;index:idx_cr_proj_status;index:idx_cr_proj_creator;index:idx_cr_proj_mode;index:idx_cr_proj_module;index:idx_cr_proj_created"`
	Name               string     `json:"name" gorm:"size:128;not null"`
	ModuleID           uint       `json:"module_id" gorm:"default:0;index:idx_cr_proj_module"`
	ReviewMode         string     `json:"review_mode" gorm:"size:16;not null;index:idx_cr_proj_mode"`
	Status             string     `json:"status" gorm:"size:16;not null;default:not_started;index:idx_cr_proj_status"`
	Description        string     `json:"description" gorm:"size:500"`
	DefaultReviewerIDs string     `json:"default_reviewer_ids" gorm:"size:1000;default:'[]'"` // [FIX #4] JSON 数组
	PlannedStartAt     *time.Time `json:"planned_start_at"`
	PlannedEndAt       *time.Time `json:"planned_end_at"`
	CaseTotalCount     int        `json:"case_total_count" gorm:"not null;default:0"`
	PendingCount       int        `json:"pending_count" gorm:"not null;default:0"`
	ApprovedCount      int        `json:"approved_count" gorm:"not null;default:0"`
	RejectedCount      int        `json:"rejected_count" gorm:"not null;default:0"`
	NeedsUpdateCount   int        `json:"needs_update_count" gorm:"not null;default:0"`
	PassRate           float64    `json:"pass_rate" gorm:"type:decimal(5,2);not null;default:0"`
	CreatedBy          uint       `json:"created_by" gorm:"not null;index:idx_cr_proj_creator"`
	UpdatedBy          uint       `json:"updated_by" gorm:"not null"`
	CreatedAt          time.Time  `json:"created_at" gorm:"index:idx_cr_proj_created"`
	UpdatedAt          time.Time  `json:"updated_at"`

	// 虚拟字段（不入库，API 返回时填充）
	CreatedByName   string   `json:"created_by_name,omitempty" gorm:"-"`
	CreatedByAvatar string   `json:"created_by_avatar,omitempty" gorm:"-"`
	ReviewerIDList  []uint   `json:"reviewer_ids,omitempty" gorm:"-"`
	ReviewerNames   []string `json:"reviewer_names,omitempty" gorm:"-"`
}

// ParseDefaultReviewerIDs 解析 JSON 格式的默认评审人 ID 列表
func (cr *CaseReview) ParseDefaultReviewerIDs() []uint {
	if cr.DefaultReviewerIDs == "" || cr.DefaultReviewerIDs == "[]" {
		return nil
	}
	var ids []uint
	_ = json.Unmarshal([]byte(cr.DefaultReviewerIDs), &ids)
	return ids
}

// CaseReviewItem 评审计划-用例关联表
type CaseReviewItem struct {
	ID              uint       `json:"id" gorm:"primaryKey"`
	ReviewID        uint       `json:"review_id" gorm:"not null;uniqueIndex:uk_review_case;index:idx_ri_review_status;index:idx_ri_review_updated"`
	ProjectID       uint       `json:"project_id" gorm:"not null;index:idx_ri_proj_case;index:idx_ri_proj_module"`
	TestCaseID      uint       `json:"testcase_id" gorm:"not null;uniqueIndex:uk_review_case;index:idx_ri_proj_case"`
	TestCaseVersion string     `json:"testcase_version" gorm:"size:20;not null"`
	ModuleID        uint       `json:"module_id" gorm:"default:0;index:idx_ri_proj_module"`
	TitleSnapshot   string     `json:"title_snapshot" gorm:"size:200;not null"`
	ReviewStatus    string     `json:"review_status" gorm:"size:16;not null;default:pending;index:idx_ri_review_status"`
	FinalResult     string     `json:"final_result" gorm:"size:16;not null;default:pending;index:idx_ri_review_status"`
	CurrentRoundNo  int        `json:"current_round_no" gorm:"not null;default:1"`
	ReviewedAt      *time.Time `json:"reviewed_at"`
	LatestComment   string     `json:"latest_comment" gorm:"size:1000"`
	SortOrder       int        `json:"sort_order" gorm:"not null;default:0"`
	CreatedBy       uint       `json:"created_by" gorm:"not null"`
	UpdatedBy       uint       `json:"updated_by" gorm:"not null"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at" gorm:"index:idx_ri_review_updated"`

	// 虚拟字段
	Reviewers []CaseReviewItemReviewer `json:"reviewers,omitempty" gorm:"-"`
}

// CaseReviewItemReviewer 评审项-评审人当前分配表
type CaseReviewItemReviewer struct {
	ID            uint       `json:"id" gorm:"primaryKey"`
	ReviewID      uint       `json:"review_id" gorm:"not null;index:idx_ir_review_reviewer"`
	ReviewItemID  uint       `json:"review_item_id" gorm:"not null;uniqueIndex:uk_item_reviewer;index:idx_ir_item_status"`
	ProjectID     uint       `json:"project_id" gorm:"not null;index:idx_ir_proj_reviewer"`
	ReviewerID    uint       `json:"reviewer_id" gorm:"not null;uniqueIndex:uk_item_reviewer;index:idx_ir_proj_reviewer;index:idx_ir_review_reviewer"`
	ReviewStatus  string     `json:"review_status" gorm:"size:16;not null;default:pending;index:idx_ir_proj_reviewer;index:idx_ir_item_status"`
	LatestResult  string     `json:"latest_result" gorm:"size:16"`
	LatestComment string     `json:"latest_comment" gorm:"size:1000"`
	ReviewedAt    *time.Time `json:"reviewed_at"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`

	// 虚拟字段
	ReviewerName string `json:"reviewer_name,omitempty" gorm:"->;-:migration"`
}

// CaseReviewRecord 评审记录表（append-only，只新增不更新）
type CaseReviewRecord struct {
	ID                         uint      `json:"id" gorm:"primaryKey"`
	ReviewID                   uint      `json:"review_id" gorm:"not null;index:idx_rr_review"`
	ReviewItemID               uint      `json:"review_item_id" gorm:"not null;index:idx_rr_item_round"`
	ProjectID                  uint      `json:"project_id" gorm:"not null;index:idx_rr_proj_reviewer;index:idx_rr_proj_case"`
	TestCaseID                 uint      `json:"testcase_id" gorm:"not null;index:idx_rr_proj_case"`
	ReviewerID                 uint      `json:"reviewer_id" gorm:"not null;index:idx_rr_proj_reviewer"`
	RoundNo                    int       `json:"round_no" gorm:"not null;index:idx_rr_item_round"`
	Result                     string    `json:"result" gorm:"size:16;not null"`
	Comment                    string    `json:"comment" gorm:"size:2000"`
	AggregateResultAfterSubmit string    `json:"aggregate_result_after_submit" gorm:"size:16;not null"`
	CreatedAt                  time.Time `json:"created_at" gorm:"index:idx_rr_item_round;index:idx_rr_proj_reviewer;index:idx_rr_proj_case;index:idx_rr_review"`

	// 虚拟字段：读模式，不参与 migration，但允许 Scan 回填 reviewer_name 列
	ReviewerName string `json:"reviewer_name,omitempty" gorm:"->;-:migration"`
}

// CaseReviewAttachment 评审项附件（独立于用例正式附件）
// 作为评审过程中的证据存在，可追溯到具体评审计划/评审项/轮次；
// 用例详情页只读镜像展示，不影响用例本身的附件清单。
type CaseReviewAttachment struct {
	ID           uint      `json:"id" gorm:"primaryKey"`
	ReviewID     uint      `json:"review_id" gorm:"not null;index:idx_ra_review"`
	ReviewItemID uint      `json:"review_item_id" gorm:"not null;index:idx_ra_item"`
	ProjectID    uint      `json:"project_id" gorm:"not null;index:idx_ra_proj"`
	TestCaseID   uint      `json:"testcase_id" gorm:"not null;index:idx_ra_case"`
	RoundNo      int       `json:"round_no" gorm:"not null;default:1"`
	FileName     string    `json:"file_name" gorm:"size:255;not null"`
	FilePath     string    `json:"file_path" gorm:"size:500;not null"`
	FileSize     int64     `json:"file_size"`
	MimeType     string    `json:"mime_type" gorm:"size:100"`
	CreatedBy    uint      `json:"created_by" gorm:"default:0"`
	CreatedAt    time.Time `json:"created_at"`

	// 虚拟字段：读模式，不参与 migration
	UploaderName string `json:"uploader_name,omitempty" gorm:"->;-:migration"`
	ReviewName   string `json:"review_name,omitempty" gorm:"->;-:migration"`
}

func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&Role{},
		&UserRole{},
		&UserProject{},
		&AuditLog{},

		&Project{},
		&ProjectMember{},
		&Requirement{},
		&TestCase{},
		&Module{},
		&CaseAttachment{},
		&CaseHistory{},
		&CaseRelation{},
		&Script{},
		&RequirementTestCase{},
		&TestCaseScript{},
		&Run{},
		&RunResult{},
		&Defect{},

		// 测试智编模块
		&AIScriptTask{},
		&AIScriptTaskCaseRel{},
		&AIScriptRecordingSession{},
		&AIScriptVersion{},
		&AIScriptValidation{},
		&AIScriptTrace{},
		&AIScriptEvidence{},
		&AIScriptOperationLog{},

		// V1 多项目工程化
		&AIScriptFile{},
		&AIScriptWorkspaceLock{},

		// 用例评审模块
		&CaseReview{},
		&CaseReviewItem{},
		&CaseReviewItemReviewer{},
		&CaseReviewRecord{},
		&CaseReviewAttachment{},

		// 标签管理模块
		&Tag{},
		&TestCaseTag{},
	)
}

// IsPresetSystemRole 判断是否为系统预置角色（不可删除）
func IsPresetSystemRole(role string) bool {
	switch role {
	case GlobalRoleAdmin, GlobalRoleManager, GlobalRoleTester, GlobalRoleReviewer, GlobalRoleDeveloper, GlobalRoleReadonly:
		return true
	default:
		return false
	}
}

// IsValidGlobalRole 判断是否为合法的全局角色名
func IsValidGlobalRole(role string) bool {
	return IsPresetSystemRole(role)
}

// IsProtectedGlobalRole 判断是否为受保护的全局角色（admin/manager）
// 受保护角色的成员不可从项目中移除
func IsProtectedGlobalRole(role string) bool {
	switch role {
	case GlobalRoleAdmin, GlobalRoleManager:
		return true
	default:
		return false
	}
}

// IsSeedProject 判断是否为种子项目（不可删除/归档）
func IsSeedProject(name string) bool {
	return name == SeedProjectName
}

// IsArchivedProject 判断项目是否已归档
func IsArchivedProject(status string) bool {
	return status == ProjectStatusArchived
}

// Tag 标签实体
type Tag struct {
	ID          uint      `json:"id" gorm:"primaryKey"`
	ProjectID   uint      `json:"project_id" gorm:"not null;uniqueIndex:uk_project_tag_name;index"`
	Name        string    `json:"name" gorm:"size:50;not null;uniqueIndex:uk_project_tag_name"`
	Color       string    `json:"color" gorm:"size:7;not null;default:#3B82F6"`
	Description string    `json:"description" gorm:"size:200;default:''"`
	CreatedBy   uint      `json:"created_by" gorm:"not null;default:0"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`

	// 虚拟字段（只读，Scan 可填充，不参与写入和迁移）
	CaseCount       int64  `json:"case_count" gorm:"->;-:migration"`
	CreatedByName   string `json:"created_by_name" gorm:"->;-:migration"`
	CreatedByAvatar string `json:"created_by_avatar" gorm:"->;-:migration"`
}

// TestCaseTag 用例-标签关联
type TestCaseTag struct {
	TestCaseID uint      `json:"testcase_id" gorm:"primaryKey"`
	TagID      uint      `json:"tag_id" gorm:"primaryKey;index"`
	CreatedAt  time.Time `json:"created_at"`
}

func IsValidMemberRole(role string) bool {
	switch role {
	case MemberRoleOwner, MemberRoleMember:
		return true
	default:
		return false
	}
}

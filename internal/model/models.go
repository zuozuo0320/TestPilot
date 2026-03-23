package model

import (
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

	// ---- 种子数据标识 ----
	SeedProjectName = "AiSight Demo"
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
	Status      string     `json:"status" gorm:"size:20;not null;default:active;index"`
	ArchivedAt  *time.Time `json:"archived_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`

	// 虚拟字段（不入库，API 返回时填充）
	MemberCount   int64 `json:"member_count" gorm:"-"` // 虚拟字段：SQL 子查询填充
	TestCaseCount int64 `json:"testcase_count" gorm:"-"` // 虚拟字段：SQL 子查询填充
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
	Level        string    `json:"level" gorm:"size:10;default:P1"`
	ReviewResult string    `json:"review_result" gorm:"size:30;default:未评审"`
	ExecResult   string    `json:"exec_result" gorm:"size:30;default:未执行"`
	ModuleID     uint      `json:"module_id" gorm:"default:0;index"`
	ModulePath   string    `json:"module_path" gorm:"size:255;default:/"`
	Tags         string    `json:"tags" gorm:"size:500"`
	Precondition string    `json:"precondition" gorm:"type:longtext"`
	Steps        string    `json:"steps" gorm:"type:longtext"`
	Remark       string    `json:"remark" gorm:"type:longtext"`
	Priority     string    `json:"priority" gorm:"size:20;default:medium"`
	CreatedBy    uint      `json:"created_by" gorm:"not null;default:0;index"`
	UpdatedBy    uint      `json:"updated_by" gorm:"not null;default:0;index"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
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

func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&Role{},
		&UserRole{},
		&UserProject{},
		&AuditLog{},

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

// IsSeedProject 判断是否为种子项目（不可删除/归档）
func IsSeedProject(name string) bool {
	return name == SeedProjectName
}

// IsArchivedProject 判断项目是否已归档
func IsArchivedProject(status string) bool {
	return status == ProjectStatusArchived
}

func IsValidMemberRole(role string) bool {
	switch role {
	case MemberRoleOwner, MemberRoleMember:
		return true
	default:
		return false
	}
}

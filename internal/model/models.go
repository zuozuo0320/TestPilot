package model

import (
	"time"

	"gorm.io/gorm"
)

const (
	GlobalRoleAdmin   = "admin"
	GlobalRoleManager = "manager"
	GlobalRoleTester  = "tester"
	GlobalRoleReviewer = "reviewer"
	GlobalRoleReadonly = "readonly"

	MemberRoleOwner  = "owner"
	MemberRoleMember = "member"
)

type User struct {
	ID           uint           `json:"id" gorm:"primaryKey"`
	Name         string         `json:"name" gorm:"size:80;not null"`
	Email        string         `json:"email" gorm:"size:120;index;not null"`
	Phone        string         `json:"phone" gorm:"size:30;index"`
	Avatar       string         `json:"avatar" gorm:"size:500"`
	PasswordHash string         `json:"-" gorm:"column:password_hash;size:255;not null;default:''"`
	Role         string         `json:"role" gorm:"size:20;not null;index"`
	Active       bool           `json:"active" gorm:"not null;default:true"`
	DeletedAt    gorm.DeletedAt `json:"deleted_at" gorm:"index"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

type Role struct {
	ID          uint           `json:"id" gorm:"primaryKey"`
	Name        string         `json:"name" gorm:"size:80;uniqueIndex;not null"`
	Description string         `json:"description" gorm:"size:500"`
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

type Project struct {
	ID          uint      `json:"id" gorm:"primaryKey"`
	Name        string    `json:"name" gorm:"size:120;uniqueIndex;not null"`
	Description string    `json:"description" gorm:"size:500"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
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
	ModulePath   string    `json:"module_path" gorm:"size:255;default:/未分类"`
	Tags         string    `json:"tags" gorm:"size:255"`
	Steps        string    `json:"steps" gorm:"type:text"`
	Priority     string    `json:"priority" gorm:"size:20;default:medium"`
	CreatedBy    uint      `json:"created_by" gorm:"not null;default:0;index"`
	UpdatedBy    uint      `json:"updated_by" gorm:"not null;default:0;index"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
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
		&Script{},
		&RequirementTestCase{},
		&TestCaseScript{},
		&Run{},
		&RunResult{},
		&Defect{},
	)
}

func IsPresetSystemRole(role string) bool {
	switch role {
	case GlobalRoleAdmin, GlobalRoleManager, GlobalRoleTester, GlobalRoleReviewer, GlobalRoleReadonly:
		return true
	default:
		return false
	}
}

func IsValidGlobalRole(role string) bool {
	switch role {
	case GlobalRoleAdmin, GlobalRoleManager, GlobalRoleTester:
		return true
	default:
		return false
	}
}

func IsValidMemberRole(role string) bool {
	switch role {
	case MemberRoleOwner, MemberRoleMember:
		return true
	default:
		return false
	}
}

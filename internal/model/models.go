package model

import (
	"time"

	"gorm.io/gorm"
)

const (
	GlobalRoleAdmin   = "admin"
	GlobalRoleManager = "manager"
	GlobalRoleTester  = "tester"

	MemberRoleOwner  = "owner"
	MemberRoleMember = "member"
)

type User struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	Name      string    `json:"name" gorm:"size:80;not null"`
	Email     string    `json:"email" gorm:"size:120;uniqueIndex;not null"`
	Role      string    `json:"role" gorm:"size:20;not null;index"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
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
	ID        uint      `json:"id" gorm:"primaryKey"`
	ProjectID uint      `json:"project_id" gorm:"not null;index;uniqueIndex:idx_project_testcase_title"`
	Title     string    `json:"title" gorm:"size:200;not null;uniqueIndex:idx_project_testcase_title"`
	Steps     string    `json:"steps" gorm:"type:text"`
	Priority  string    `json:"priority" gorm:"size:20;default:medium"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
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
		&User{},
		&Project{},
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

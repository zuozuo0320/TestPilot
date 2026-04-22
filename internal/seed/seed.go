package seed

import (
	"fmt"
	"log/slog"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"testpilot/internal/model"
	pkgauth "testpilot/internal/pkg/auth"
)

// bootstrapSeedContext 保存基础引导数据的关键信息，供演示数据阶段复用。
type bootstrapSeedContext struct {
	Users        []model.User
	RoleIDByName map[string]uint
	Project      model.Project
}

// Seed 写入完整演示数据。
// 该入口用于手工初始化完整 demo 环境，包含基础账号/角色以及演示项目、用例、脚本。
func Seed(db *gorm.DB, logger *slog.Logger) error {
	context, err := ensureBootstrapData(db)
	if err != nil {
		return err
	}
	return seedDemoData(db, logger, context)
}

// SeedBootstrap 仅写入基础引导数据。
// 默认启动时会自动初始化基础账号、角色，以及空的默认初始项目 AiSight Demo，
// 但不会自动注入示例用例、示例脚本和演示需求。
func SeedBootstrap(db *gorm.DB, logger *slog.Logger) error {
	context, err := ensureBootstrapData(db)
	if err != nil {
		return err
	}
	logger.Info("bootstrap seed completed", "users", len(context.Users), "roles", len(context.RoleIDByName), "project_id", context.Project.ID, "project_name", context.Project.Name)
	return nil
}

// SeedDemo 写入演示项目相关数据。
// 它会先确保基础账号/角色已存在，再补齐演示项目、需求、用例和脚本。
func SeedDemo(db *gorm.DB, logger *slog.Logger) error {
	context, err := ensureBootstrapData(db)
	if err != nil {
		return err
	}
	return seedDemoData(db, logger, context)
}

// CleanupDemoData 清理默认项目下的演示内容。
// 该函数会保留默认初始项目 AiSight Demo 以及默认成员关系，仅删除示例用例、
// 示例脚本、演示需求和相关衍生数据，让系统回到“有默认项目、但项目为空”的状态。
func CleanupDemoData(db *gorm.DB, logger *slog.Logger) error {
	context, err := ensureBootstrapData(db)
	if err != nil {
		return err
	}
	project := context.Project

	var (
		testCaseIDs []uint
		scriptIDs   []uint
		taskIDs     []uint
	)

	if err := db.Model(&model.TestCase{}).Where("project_id = ?", project.ID).Pluck("id", &testCaseIDs).Error; err != nil {
		return fmt.Errorf("collect demo testcase ids failed: %w", err)
	}
	if err := db.Model(&model.Script{}).Where("project_id = ?", project.ID).Pluck("id", &scriptIDs).Error; err != nil {
		return fmt.Errorf("collect demo script ids failed: %w", err)
	}
	if err := db.Model(&model.AIScriptTask{}).Where("project_id = ?", project.ID).Pluck("id", &taskIDs).Error; err != nil {
		return fmt.Errorf("collect demo ai task ids failed: %w", err)
	}

	if err := db.Transaction(func(tx *gorm.DB) error {
		if len(testCaseIDs) > 0 {
			if err := tx.Where("test_case_id IN ?", testCaseIDs).Delete(&model.CaseAttachment{}).Error; err != nil {
				return fmt.Errorf("delete case attachments failed: %w", err)
			}
			if err := tx.Where("test_case_id IN ?", testCaseIDs).Delete(&model.CaseHistory{}).Error; err != nil {
				return fmt.Errorf("delete case histories failed: %w", err)
			}
			if err := tx.Where("source_case_id IN ? OR target_case_id IN ?", testCaseIDs, testCaseIDs).Delete(&model.CaseRelation{}).Error; err != nil {
				return fmt.Errorf("delete case relations failed: %w", err)
			}
			if err := tx.Where("test_case_id IN ?", testCaseIDs).Delete(&model.RequirementTestCase{}).Error; err != nil {
				return fmt.Errorf("delete requirement-testcase links failed: %w", err)
			}
			if err := tx.Where("test_case_id IN ?", testCaseIDs).Delete(&model.TestCaseScript{}).Error; err != nil {
				return fmt.Errorf("delete testcase-script links failed: %w", err)
			}
		}

		if len(scriptIDs) > 0 {
			if err := tx.Where("script_id IN ?", scriptIDs).Delete(&model.RunResult{}).Error; err != nil {
				return fmt.Errorf("delete run results by script failed: %w", err)
			}
			if err := tx.Where("script_id IN ?", scriptIDs).Delete(&model.TestCaseScript{}).Error; err != nil {
				return fmt.Errorf("delete testcase-script links by script failed: %w", err)
			}
		}

		if len(taskIDs) > 0 {
			if err := tx.Where("task_id IN ?", taskIDs).Delete(&model.AIScriptTaskCaseRel{}).Error; err != nil {
				return fmt.Errorf("delete ai task-case relations failed: %w", err)
			}
			if err := tx.Where("task_id IN ?", taskIDs).Delete(&model.AIScriptRecordingSession{}).Error; err != nil {
				return fmt.Errorf("delete ai recording sessions failed: %w", err)
			}
			if err := tx.Where("task_id IN ?", taskIDs).Delete(&model.AIScriptTrace{}).Error; err != nil {
				return fmt.Errorf("delete ai traces failed: %w", err)
			}
			if err := tx.Where("task_id IN ?", taskIDs).Delete(&model.AIScriptEvidence{}).Error; err != nil {
				return fmt.Errorf("delete ai evidences by task failed: %w", err)
			}
			if err := tx.Where("task_id IN ?", taskIDs).Delete(&model.AIScriptValidation{}).Error; err != nil {
				return fmt.Errorf("delete ai validations failed: %w", err)
			}
			if err := tx.Where("task_id IN ?", taskIDs).Delete(&model.AIScriptOperationLog{}).Error; err != nil {
				return fmt.Errorf("delete ai operation logs by task failed: %w", err)
			}
			if err := tx.Where("task_id IN ?", taskIDs).Delete(&model.AIScriptVersion{}).Error; err != nil {
				return fmt.Errorf("delete ai versions failed: %w", err)
			}
			if err := tx.Where("id IN ?", taskIDs).Delete(&model.AIScriptTask{}).Error; err != nil {
				return fmt.Errorf("delete ai tasks failed: %w", err)
			}
		}

		if err := tx.Where("project_id = ?", project.ID).Delete(&model.AIScriptFile{}).Error; err != nil {
			return fmt.Errorf("delete ai files failed: %w", err)
		}
		if err := tx.Where("project_id = ?", project.ID).Delete(&model.AIScriptWorkspaceLock{}).Error; err != nil {
			return fmt.Errorf("delete ai workspace locks failed: %w", err)
		}
		if err := tx.Where("project_id = ?", project.ID).Delete(&model.CaseReviewRecord{}).Error; err != nil {
			return fmt.Errorf("delete case review records failed: %w", err)
		}
		if err := tx.Where("project_id = ?", project.ID).Delete(&model.CaseReviewItemReviewer{}).Error; err != nil {
			return fmt.Errorf("delete case review item reviewers failed: %w", err)
		}
		if err := tx.Where("project_id = ?", project.ID).Delete(&model.CaseReviewItem{}).Error; err != nil {
			return fmt.Errorf("delete case review items failed: %w", err)
		}
		if err := tx.Where("project_id = ?", project.ID).Delete(&model.CaseReview{}).Error; err != nil {
			return fmt.Errorf("delete case reviews failed: %w", err)
		}
		if err := tx.Where("project_id = ?", project.ID).Delete(&model.Defect{}).Error; err != nil {
			return fmt.Errorf("delete defects failed: %w", err)
		}
		if err := tx.Where("project_id = ?", project.ID).Delete(&model.RunResult{}).Error; err != nil {
			return fmt.Errorf("delete run results failed: %w", err)
		}
		if err := tx.Where("project_id = ?", project.ID).Delete(&model.Run{}).Error; err != nil {
			return fmt.Errorf("delete runs failed: %w", err)
		}
		if err := tx.Where("project_id = ?", project.ID).Delete(&model.Requirement{}).Error; err != nil {
			return fmt.Errorf("delete requirements failed: %w", err)
		}
		if err := tx.Where("project_id = ?", project.ID).Delete(&model.TestCase{}).Error; err != nil {
			return fmt.Errorf("delete testcases failed: %w", err)
		}
		if err := tx.Where("project_id = ?", project.ID).Delete(&model.Script{}).Error; err != nil {
			return fmt.Errorf("delete scripts failed: %w", err)
		}
		if err := tx.Where("project_id = ?", project.ID).Delete(&model.Module{}).Error; err != nil {
			return fmt.Errorf("delete modules failed: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}

	logger.Info("demo project cleanup completed", "project_id", project.ID, "project_name", project.Name, "testcase_count", len(testCaseIDs), "script_count", len(scriptIDs), "ai_task_count", len(taskIDs))
	return nil
}

// ensureBootstrapData 确保系统基础账号、角色、默认初始项目和成员关系已初始化。
// 这部分数据属于平台可登录、可进入的最小集合，需保持幂等。
func ensureBootstrapData(db *gorm.DB) (*bootstrapSeedContext, error) {
	defaultHash, err := pkgauth.HashPassword("TestPilot@2026")
	if err != nil {
		return nil, fmt.Errorf("hash default password failed: %w", err)
	}

	users := []model.User{
		{Name: "Alice Admin", Email: "admin@testpilot.local", Role: model.GlobalRoleAdmin, Active: true, PasswordHash: defaultHash},
		{Name: "Mia Manager", Email: "manager@testpilot.local", Role: model.GlobalRoleManager, Active: true, PasswordHash: defaultHash},
		{Name: "Tom Tester", Email: "tester@testpilot.local", Role: model.GlobalRoleTester, Active: true, PasswordHash: defaultHash},
	}

	for index := range users {
		if err := db.Where(model.User{Email: users[index].Email}).FirstOrCreate(&users[index]).Error; err != nil {
			return nil, fmt.Errorf("seed user failed: %w", err)
		}
		if err := db.Model(&model.User{}).Where("id = ?", users[index].ID).Updates(map[string]any{
			"name":   users[index].Name,
			"role":   users[index].Role,
			"active": true,
		}).Error; err != nil {
			return nil, fmt.Errorf("seed user update failed: %w", err)
		}
	}

	roles := []model.Role{
		{Name: model.GlobalRoleAdmin, DisplayName: "系统管理员", Description: "全权限，系统配置、用户/角色/项目管理"},
		{Name: model.GlobalRoleManager, DisplayName: "项目管理员", Description: "项目级管理权限，可管理项目成员、配置模块"},
		{Name: model.GlobalRoleTester, DisplayName: "测试工程师", Description: "用例增删改、执行、缺陷管理、参与评审、导入导出"},
		{Name: model.GlobalRoleReviewer, DisplayName: "评审员", Description: "评审用例、修改评审状态，不可增删用例"},
		{Name: model.GlobalRoleDeveloper, DisplayName: "开发工程师", Description: "查看用例+缺陷，认领/修复缺陷，不可改用例"},
		{Name: model.GlobalRoleReadonly, DisplayName: "只读访客", Description: "纯查看，不可修改任何数据"},
	}

	roleIDByName := map[string]uint{}
	for index := range roles {
		var existing model.Role
		if err := db.Unscoped().Where("name = ?", roles[index].Name).First(&existing).Error; err != nil {
			if err != gorm.ErrRecordNotFound {
				return nil, fmt.Errorf("seed role query failed: %w", err)
			}
			if err := db.Create(&roles[index]).Error; err != nil {
				return nil, fmt.Errorf("seed role create failed: %w", err)
			}
			roleIDByName[roles[index].Name] = roles[index].ID
			continue
		}

		if err := db.Unscoped().Model(&model.Role{}).Where("id = ?", existing.ID).Updates(map[string]any{
			"name":         roles[index].Name,
			"display_name": roles[index].DisplayName,
			"description":  roles[index].Description,
			"deleted_at":   nil,
		}).Error; err != nil {
			return nil, fmt.Errorf("seed role update failed: %w", err)
		}
		roleIDByName[roles[index].Name] = existing.ID
	}

	userRoles := []model.UserRole{
		{UserID: users[0].ID, RoleID: roleIDByName[model.GlobalRoleAdmin]},
		{UserID: users[1].ID, RoleID: roleIDByName[model.GlobalRoleManager]},
		{UserID: users[2].ID, RoleID: roleIDByName[model.GlobalRoleTester]},
	}
	for _, userRole := range userRoles {
		if userRole.UserID == 0 || userRole.RoleID == 0 {
			continue
		}
		var count int64
		db.Model(&model.UserRole{}).Where("user_id = ? AND role_id = ?", userRole.UserID, userRole.RoleID).Count(&count)
		if count == 0 {
			item := userRole
			if err := db.Create(&item).Error; err != nil {
				return nil, fmt.Errorf("seed user-role failed: %w", err)
			}
		}
	}

	project, err := ensureDefaultProject(db, users)
	if err != nil {
		return nil, err
	}

	return &bootstrapSeedContext{
		Users:        users,
		RoleIDByName: roleIDByName,
		Project:      *project,
	}, nil
}

// seedDemoData 写入演示项目相关数据。
// 该函数只在手工执行 demo seed 或显式开启 AUTO_SEED_DEMO 时运行。
func seedDemoData(db *gorm.DB, logger *slog.Logger, context *bootstrapSeedContext) error {
	if context == nil || len(context.Users) < 3 {
		return fmt.Errorf("bootstrap users are not ready")
	}

	users := context.Users

	// 清理历史遗留的旧名称项目（一次性操作，幂等安全）
	for _, oldName := range []string{"Demo Project", "示例项目", "快速开始"} {
		var oldProject model.Project
		if err := db.Where("name = ?", oldName).First(&oldProject).Error; err == nil {
			db.Where("project_id = ?", oldProject.ID).Delete(&model.ProjectMember{})
			db.Where("project_id = ?", oldProject.ID).Delete(&model.UserProject{})
			db.Where("project_id = ?", oldProject.ID).Delete(&model.Requirement{})
			db.Where("project_id = ?", oldProject.ID).Delete(&model.TestCase{})
			db.Where("project_id = ?", oldProject.ID).Delete(&model.Script{})
			db.Where("project_id = ?", oldProject.ID).Delete(&model.Module{})
			db.Delete(&oldProject)
			logger.Info("cleaned up legacy project", "name", oldName, "id", oldProject.ID)
		}
	}

	project := context.Project
	if err := db.Model(&model.Project{}).Where("id = ?", project.ID).Updates(map[string]any{
		"description": "包含示例用例与基础模块，帮助你快速熟悉平台",
		"status":      model.ProjectStatusActive,
	}).Error; err != nil {
		return fmt.Errorf("update demo project description failed: %w", err)
	}

	requirements := []model.Requirement{
		{ProjectID: project.ID, Title: "REQ-LOGIN", Content: "User can login with email/password"},
		{ProjectID: project.ID, Title: "REQ-ORDER", Content: "User can create order from cart"},
	}
	for index := range requirements {
		if err := db.Where(model.Requirement{ProjectID: project.ID, Title: requirements[index].Title}).
			FirstOrCreate(&requirements[index]).Error; err != nil {
			return fmt.Errorf("seed requirement failed: %w", err)
		}
	}

	testCases := []model.TestCase{
		{
			ProjectID:    project.ID,
			Title:        "TC-LOGIN-SUCCESS",
			Level:        "P0",
			ReviewResult: "已通过",
			ExecResult:   "成功",
			ModulePath:   "/登录",
			Tags:         "smoke,auth",
			Steps:        "1. open login page 2. input valid creds 3. submit",
			Priority:     "high",
			CreatedBy:    users[0].ID,
			UpdatedBy:    users[1].ID,
		},
		{
			ProjectID:    project.ID,
			Title:        "TC-ORDER-CREATE",
			Level:        "P1",
			ReviewResult: "未评审",
			ExecResult:   "未执行",
			ModulePath:   "/内容/文章",
			Tags:         "regression",
			Steps:        "1. add cart 2. checkout 3. confirm",
			Priority:     "medium",
			CreatedBy:    users[1].ID,
			UpdatedBy:    users[1].ID,
		},
	}
	for index := range testCases {
		if err := db.Where(model.TestCase{ProjectID: project.ID, Title: testCases[index].Title}).
			FirstOrCreate(&testCases[index]).Error; err != nil {
			return fmt.Errorf("seed testcase failed: %w", err)
		}
	}

	scripts := []model.Script{
		{ProjectID: project.ID, Name: "login.cy.ts", Path: "cypress/e2e/login.cy.ts", Type: "cypress"},
		{ProjectID: project.ID, Name: "order.cy.ts", Path: "cypress/e2e/order.cy.ts", Type: "cypress"},
	}
	for index := range scripts {
		if err := db.Where(model.Script{ProjectID: project.ID, Name: scripts[index].Name}).
			FirstOrCreate(&scripts[index]).Error; err != nil {
			return fmt.Errorf("seed script failed: %w", err)
		}
	}

	linksRT := []model.RequirementTestCase{
		{RequirementID: requirements[0].ID, TestCaseID: testCases[0].ID},
		{RequirementID: requirements[1].ID, TestCaseID: testCases[1].ID},
	}
	for _, link := range linksRT {
		item := link
		if err := db.Where(&item).FirstOrCreate(&item).Error; err != nil {
			return fmt.Errorf("seed requirement-testcase link failed: %w", err)
		}
	}

	linksTS := []model.TestCaseScript{
		{TestCaseID: testCases[0].ID, ScriptID: scripts[0].ID},
		{TestCaseID: testCases[1].ID, ScriptID: scripts[1].ID},
	}
	for _, link := range linksTS {
		item := link
		if err := db.Where(&item).FirstOrCreate(&item).Error; err != nil {
			return fmt.Errorf("seed testcase-script link failed: %w", err)
		}
	}

	logger.Info("demo seed completed", "project_id", project.ID, "users", len(users), "requirements", len(requirements), "testcases", len(testCases), "scripts", len(scripts))
	return nil
}

// ensureDefaultProject 确保默认初始项目存在，并把默认账号加入该项目。
// 该项目作为系统首次进入时的基础工作空间，默认保持为空项目。
func ensureDefaultProject(db *gorm.DB, users []model.User) (*model.Project, error) {
	if len(users) < 3 {
		return nil, fmt.Errorf("bootstrap users are not ready")
	}

	project := model.Project{
		Name:        model.SeedProjectName,
		Description: "系统默认初始项目，作为平台首次进入时的基础工作空间",
		OwnerID:     users[0].ID,
		Status:      model.ProjectStatusActive,
	}
	if err := db.Where(model.Project{Name: project.Name}).Assign(model.Project{
		Description: project.Description,
		OwnerID:     project.OwnerID,
		Status:      model.ProjectStatusActive,
	}).FirstOrCreate(&project).Error; err != nil {
		return nil, fmt.Errorf("seed default project failed: %w", err)
	}

	userProjects := []model.UserProject{
		{UserID: users[0].ID, ProjectID: project.ID},
		{UserID: users[1].ID, ProjectID: project.ID},
		{UserID: users[2].ID, ProjectID: project.ID},
	}
	for _, userProject := range userProjects {
		item := userProject
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&item).Error; err != nil {
			return nil, fmt.Errorf("seed default user-project failed: %w", err)
		}
	}

	members := []model.ProjectMember{
		{ProjectID: project.ID, UserID: users[0].ID, Role: model.MemberRoleOwner},
		{ProjectID: project.ID, UserID: users[1].ID, Role: model.MemberRoleMember},
		{ProjectID: project.ID, UserID: users[2].ID, Role: model.MemberRoleMember},
	}
	for _, member := range members {
		item := member
		if err := db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "project_id"}, {Name: "user_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"role", "updated_at"}),
		}).Create(&item).Error; err != nil {
			return nil, fmt.Errorf("seed default member failed: %w", err)
		}
	}

	return &project, nil
}

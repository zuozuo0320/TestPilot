package seed

import (
	"fmt"
	"log/slog"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"testpilot/internal/model"
	pkgauth "testpilot/internal/pkg/auth"
)

func Seed(db *gorm.DB, logger *slog.Logger) error {
	// 默认密码哈希
	defaultHash, err := pkgauth.HashPassword("TestPilot@2026")
	if err != nil {
		return fmt.Errorf("hash default password failed: %w", err)
	}

	users := []model.User{
		{Name: "Alice Admin", Email: "admin@testpilot.local", Role: model.GlobalRoleAdmin, Active: true, PasswordHash: defaultHash},
		{Name: "Mia Manager", Email: "manager@testpilot.local", Role: model.GlobalRoleManager, Active: true, PasswordHash: defaultHash},
		{Name: "Tom Tester", Email: "tester@testpilot.local", Role: model.GlobalRoleTester, Active: true, PasswordHash: defaultHash},
	}

	for i := range users {
		if err := db.Where(model.User{Email: users[i].Email}).FirstOrCreate(&users[i]).Error; err != nil {
			return fmt.Errorf("seed user failed: %w", err)
		}
		if err := db.Model(&model.User{}).Where("id = ?", users[i].ID).Updates(map[string]any{
			"name":   users[i].Name,
			"role":   users[i].Role,
			"active": true,
		}).Error; err != nil {
			return fmt.Errorf("seed user update failed: %w", err)
		}
	}

	// ---- 预置角色（含 display_name）----
	roles := []model.Role{
		{Name: model.GlobalRoleAdmin, DisplayName: "系统管理员", Description: "全权限，系统配置、用户/角色/项目管理"},
		{Name: model.GlobalRoleManager, DisplayName: "项目管理员", Description: "项目级管理权限，可管理项目成员、配置模块"},
		{Name: model.GlobalRoleTester, DisplayName: "测试工程师", Description: "用例增删改、执行、提交缺陷、导入导出"},
		{Name: model.GlobalRoleReviewer, DisplayName: "评审员", Description: "评审用例、修改评审状态，不可增删用例"},
		{Name: model.GlobalRoleDeveloper, DisplayName: "开发工程师", Description: "查看用例+缺陷，认领/修复缺陷，不可改用例"},
		{Name: model.GlobalRoleReadonly, DisplayName: "只读访客", Description: "纯查看，不可修改任何数据"},
	}
	roleIDByName := map[string]uint{}
	for i := range roles {
		var existing model.Role
		if err := db.Unscoped().Where("name = ?", roles[i].Name).First(&existing).Error; err != nil {
			if err != gorm.ErrRecordNotFound {
				return fmt.Errorf("seed role query failed: %w", err)
			}
			if err := db.Create(&roles[i]).Error; err != nil {
				return fmt.Errorf("seed role create failed: %w", err)
			}
			roleIDByName[roles[i].Name] = roles[i].ID
			continue
		}

		if err := db.Unscoped().Model(&model.Role{}).Where("id = ?", existing.ID).Updates(map[string]any{
			"name":         roles[i].Name,
			"display_name": roles[i].DisplayName,
			"description":  roles[i].Description,
			"deleted_at":   nil,
		}).Error; err != nil {
			return fmt.Errorf("seed role update failed: %w", err)
		}
		roleIDByName[roles[i].Name] = existing.ID
	}

	// ---- 预置用户-角色绑定（幂等：先查后插）----
	userRoles := []model.UserRole{
		{UserID: users[0].ID, RoleID: roleIDByName[model.GlobalRoleAdmin]},
		{UserID: users[1].ID, RoleID: roleIDByName[model.GlobalRoleManager]},
		{UserID: users[2].ID, RoleID: roleIDByName[model.GlobalRoleTester]},
	}
	for _, ur := range userRoles {
		if ur.UserID == 0 || ur.RoleID == 0 {
			continue // 安全跳过无效 ID
		}
		var count int64
		db.Model(&model.UserRole{}).Where("user_id = ? AND role_id = ?", ur.UserID, ur.RoleID).Count(&count)
		if count == 0 {
			item := ur
			if err := db.Create(&item).Error; err != nil {
				return fmt.Errorf("seed user-role failed: %w", err)
			}
		}
	}

	// 清理历史遗留的旧名称项目（一次性操作，幂等安全）
	for _, oldName := range []string{"Demo Project", "示例项目", "快速开始"} {
		var oldProject model.Project
		if err := db.Where("name = ?", oldName).First(&oldProject).Error; err == nil {
			// 删除关联数据
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

	project := model.Project{
		Name:        model.SeedProjectName,
		Description: "包含示例用例与基础模块，帮助你快速熟悉平台",
		OwnerID:     users[0].ID,
		Status:      model.ProjectStatusActive,
	}
	if err := db.Where(model.Project{Name: project.Name}).Assign(model.Project{
		Description: project.Description,
		OwnerID:     project.OwnerID,
		Status:      model.ProjectStatusActive,
	}).FirstOrCreate(&project).Error; err != nil {
		return fmt.Errorf("seed project failed: %w", err)
	}

	userProjects := []model.UserProject{
		{UserID: users[0].ID, ProjectID: project.ID},
		{UserID: users[1].ID, ProjectID: project.ID},
		{UserID: users[2].ID, ProjectID: project.ID},
	}
	for _, up := range userProjects {
		item := up
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&item).Error; err != nil {
			return fmt.Errorf("seed user-project failed: %w", err)
		}
	}

	members := []model.ProjectMember{
		{ProjectID: project.ID, UserID: users[0].ID, Role: model.MemberRoleOwner},
		{ProjectID: project.ID, UserID: users[1].ID, Role: model.MemberRoleMember},
		{ProjectID: project.ID, UserID: users[2].ID, Role: model.MemberRoleMember},
	}
	for _, m := range members {
		member := m
		if err := db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "project_id"}, {Name: "user_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"role", "updated_at"}),
		}).Create(&member).Error; err != nil {
			return fmt.Errorf("seed member failed: %w", err)
		}
	}

	requirements := []model.Requirement{
		{ProjectID: project.ID, Title: "REQ-LOGIN", Content: "User can login with email/password"},
		{ProjectID: project.ID, Title: "REQ-ORDER", Content: "User can create order from cart"},
	}
	for i := range requirements {
		if err := db.Where(model.Requirement{ProjectID: project.ID, Title: requirements[i].Title}).
			FirstOrCreate(&requirements[i]).Error; err != nil {
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
	for i := range testCases {
		if err := db.Where(model.TestCase{ProjectID: project.ID, Title: testCases[i].Title}).
			FirstOrCreate(&testCases[i]).Error; err != nil {
			return fmt.Errorf("seed testcase failed: %w", err)
		}
	}

	scripts := []model.Script{
		{ProjectID: project.ID, Name: "login.cy.ts", Path: "cypress/e2e/login.cy.ts", Type: "cypress"},
		{ProjectID: project.ID, Name: "order.cy.ts", Path: "cypress/e2e/order.cy.ts", Type: "cypress"},
	}
	for i := range scripts {
		if err := db.Where(model.Script{ProjectID: project.ID, Name: scripts[i].Name}).
			FirstOrCreate(&scripts[i]).Error; err != nil {
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

	logger.Info("seed completed", "project_id", project.ID, "users", len(users), "requirements", len(requirements), "testcases", len(testCases), "scripts", len(scripts))
	return nil
}

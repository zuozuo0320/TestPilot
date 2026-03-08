package seed

import (
	"fmt"
	"log/slog"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"testpilot/internal/model"
)

func Seed(db *gorm.DB, logger *slog.Logger) error {
	users := []model.User{
		{Name: "Alice Admin", Email: "admin@testpilot.local", Role: model.GlobalRoleAdmin, Active: true},
		{Name: "Mia Manager", Email: "manager@testpilot.local", Role: model.GlobalRoleManager, Active: true},
		{Name: "Tom Tester", Email: "tester@testpilot.local", Role: model.GlobalRoleTester, Active: true},
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

	roles := []model.Role{
		{Name: model.GlobalRoleAdmin, Description: "系统管理员"},
		{Name: model.GlobalRoleManager, Description: "项目管理员"},
		{Name: model.GlobalRoleTester, Description: "测试工程师"},
		{Name: model.GlobalRoleReviewer, Description: "评审角色"},
		{Name: model.GlobalRoleReadonly, Description: "只读角色"},
	}
	roleIDByName := map[string]uint{}
	for i := range roles {
		if err := db.Where(model.Role{Name: roles[i].Name}).Assign(model.Role{Description: roles[i].Description}).FirstOrCreate(&roles[i]).Error; err != nil {
			return fmt.Errorf("seed role failed: %w", err)
		}
		roleIDByName[roles[i].Name] = roles[i].ID
	}

	userRoles := []model.UserRole{
		{UserID: users[0].ID, RoleID: roleIDByName[model.GlobalRoleAdmin]},
		{UserID: users[1].ID, RoleID: roleIDByName[model.GlobalRoleManager]},
		{UserID: users[2].ID, RoleID: roleIDByName[model.GlobalRoleTester]},
	}
	for _, ur := range userRoles {
		item := ur
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&item).Error; err != nil {
			return fmt.Errorf("seed user-role failed: %w", err)
		}
	}

	project := model.Project{Name: "Demo Project", Description: "TestPilot runnable demo project"}
	if err := db.Where(model.Project{Name: project.Name}).FirstOrCreate(&project).Error; err != nil {
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
		{ProjectID: project.ID, UserID: users[1].ID, Role: model.MemberRoleOwner},
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

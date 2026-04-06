// test_helpers_test.go — Service 层测试公共辅助
package service

import (
	"fmt"
	"io"
	"log/slog"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"testpilot/internal/model"
	pkgauth "testpilot/internal/pkg/auth"
	"testpilot/internal/repository"
)

// testDB 创建 SQLite 内存数据库 + 自动迁移（每个测试独立）
func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// testRepos 创建所有仓库实例
func testRepos(db *gorm.DB) (
	repository.UserRepository,
	repository.RoleRepository,
	repository.ProjectRepository,
	repository.AuditRepository,
	*repository.TxManager,
) {
	return repository.NewUserRepo(db),
		repository.NewRoleRepo(db),
		repository.NewProjectRepo(db),
		repository.NewAuditRepo(db),
		repository.NewTxManager(db)
}

// testJWTConfig 测试用 JWT 配置
func testJWTConfig() pkgauth.JWTConfig {
	return pkgauth.DefaultConfig("test-secret-key-32bytes-long!!")
}

// testLogger 静默 logger
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// seedAdmin 种入 admin 用户（ID=1）
func seedAdmin(t *testing.T, db *gorm.DB) model.User {
	t.Helper()
	hash, _ := pkgauth.HashPassword("TestPilot@2026")
	admin := model.User{
		ID: 1, Name: "Admin", Email: "admin@test.local",
		Role: model.GlobalRoleAdmin, Active: true, PasswordHash: hash,
	}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	return admin
}

// seedTester 种入 tester 用户（ID=2）
func seedTester(t *testing.T, db *gorm.DB) model.User {
	t.Helper()
	hash, _ := pkgauth.HashPassword("TestPilot@2026")
	tester := model.User{
		ID: 2, Name: "Tester", Email: "tester@test.local",
		Role: model.GlobalRoleTester, Active: true, PasswordHash: hash,
	}
	if err := db.Create(&tester).Error; err != nil {
		t.Fatalf("seed tester: %v", err)
	}
	return tester
}

// seedRoles 种入基础角色
func seedRoles(t *testing.T, db *gorm.DB) []model.Role {
	t.Helper()
	roles := []model.Role{
		{ID: 1, Name: "admin", Description: "admin"},
		{ID: 2, Name: "tester", Description: "tester"},
		{ID: 3, Name: "reviewer", Description: "reviewer"},
	}
	if err := db.Create(&roles).Error; err != nil {
		t.Fatalf("seed roles: %v", err)
	}
	return roles
}

// seedProject 种入项目
func seedProject(t *testing.T, db *gorm.DB) model.Project {
	t.Helper()
	project := model.Project{ID: 1, Name: "Demo", Description: "demo", OwnerID: 1}
	if err := db.Create(&project).Error; err != nil {
		t.Fatalf("seed project: %v", err)
	}
	return project
}

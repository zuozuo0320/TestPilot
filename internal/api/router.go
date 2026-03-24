// router.go — API 结构体定义 + 路由注册（P4: 统一响应 + Request ID）
package api

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"testpilot/internal/dto/response"
	"testpilot/internal/repository"
	"testpilot/internal/service"
)

// Dependencies 外部依赖注入（服务层）
type Dependencies struct {
	Logger             *slog.Logger
	AuthService        *service.AuthService
	UserService        *service.UserService
	RoleService        *service.RoleService
	ProjectService     *service.ProjectService
	TestCaseService    *service.TestCaseService
	ProfileService     *service.ProfileService
	ExecutionService   *service.ExecutionService
	DefectService      *service.DefectService
	RequirementService *service.RequirementService
	ScriptService      *service.ScriptService
	OverviewService    *service.OverviewService
	AuditService       *service.AuditService
	ModuleService      *service.ModuleService
	AttachmentService  *service.AttachmentService
	CaseHistoryRepo    *repository.CaseHistoryRepo
	CaseRelationRepo   *repository.CaseRelationRepo
	XlsxService        *service.XlsxService
}

// API 核心结构体
type API struct {
	logger          *slog.Logger
	allowedOrigins  []string
	authSvc         *service.AuthService
	userSvc         *service.UserService
	roleSvc         *service.RoleService
	projectSvc      *service.ProjectService
	testCaseSvc     *service.TestCaseService
	profileSvc      *service.ProfileService
	executionSvc    *service.ExecutionService
	defectSvc       *service.DefectService
	requirementSvc  *service.RequirementService
	scriptSvc       *service.ScriptService
	overviewSvc     *service.OverviewService
	auditSvc        *service.AuditService
	moduleSvc       *service.ModuleService
	attachmentSvc   *service.AttachmentService
	caseHistoryRepo *repository.CaseHistoryRepo
	caseRelationRepo *repository.CaseRelationRepo
	xlsxSvc          *service.XlsxService
}

// NewRouter 创建路由引擎并注册所有路由
func NewRouter(deps Dependencies, corsOrigins string) http.Handler {
	a := &API{
		logger:          deps.Logger,
		allowedOrigins:  parseAllowedOrigins(corsOrigins),
		authSvc:         deps.AuthService,
		userSvc:         deps.UserService,
		roleSvc:         deps.RoleService,
		projectSvc:      deps.ProjectService,
		testCaseSvc:     deps.TestCaseService,
		profileSvc:      deps.ProfileService,
		executionSvc:    deps.ExecutionService,
		defectSvc:       deps.DefectService,
		requirementSvc:  deps.RequirementService,
		scriptSvc:       deps.ScriptService,
		overviewSvc:     deps.OverviewService,
		auditSvc:        deps.AuditService,
		moduleSvc:       deps.ModuleService,
		attachmentSvc:   deps.AttachmentService,
		caseHistoryRepo: deps.CaseHistoryRepo,
		caseRelationRepo: deps.CaseRelationRepo,
		xlsxSvc:          deps.XlsxService,
	}

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()

	// 全局中间件
	r.Use(a.requestIDMiddleware()) // 分配 request_id
	r.Use(a.recoveryMiddleware())  // panic 恢复
	r.Use(a.requestLogger())       // 请求日志（含 request_id）
	r.Use(a.corsMiddleware())      // CORS

	// 静态文件
	r.Static("/uploads", "./uploads")

	// ========== 公开接口 ==========
	r.GET("/health", func(c *gin.Context) {
		response.OK(c, gin.H{"status": "ok"})
	})

	v1 := r.Group("/api/v1")
	v1.POST("/auth/login", a.login)
	v1.POST("/auth/refresh", a.refreshToken)

	// ========== 需认证接口 ==========
	auth := v1.Group("")
	auth.Use(a.authMiddleware())

	// ---- 用户管理 ----
	auth.GET("/users", a.listUsers)
	auth.POST("/users", a.createUser)
	auth.PUT("/users/:userID", a.updateUser)
	auth.DELETE("/users/:userID", a.deleteUser)
	auth.PUT("/users/:userID/reset-password", a.resetPassword)
	auth.PUT("/users/:userID/toggle-active", a.toggleActive)
	auth.POST("/users/:userID/roles", a.assignUserRoles)
	auth.POST("/users/:userID/projects", a.assignUserProjects)
	auth.GET("/users/me/profile", a.getProfile)
	auth.PUT("/users/me/profile", a.updateProfile)
	auth.PUT("/users/me/password", a.changePassword)
	auth.POST("/users/me/avatar", a.uploadMyAvatar)

	// ---- 角色管理 ----
	auth.GET("/roles", a.listRoles)
	auth.POST("/roles", a.createRole)
	auth.PUT("/roles/:roleID", a.updateRole)
	auth.DELETE("/roles/:roleID", a.deleteRole)

	// ---- 项目管理 ----
	auth.GET("/projects", a.listProjects)
	auth.POST("/projects", a.createProject)
	auth.PUT("/projects/:projectID", a.updateProject)
	auth.POST("/projects/:projectID/archive", a.archiveProject)
	auth.POST("/projects/:projectID/restore", a.restoreProject)
	auth.DELETE("/projects/:projectID", a.deleteProject)
	auth.POST("/projects/:projectID/members", a.addProjectMember)
	auth.GET("/projects/:projectID/members", a.listProjectMembers)
	auth.DELETE("/projects/:projectID/members/:userID", a.removeProjectMember)
	auth.POST("/projects/:projectID/avatar", a.uploadProjectAvatar)
	auth.POST("/projects/:projectID/requirements", a.createRequirement)
	auth.GET("/projects/:projectID/requirements", a.listRequirements)
	auth.POST("/projects/:projectID/testcases", a.createTestCase)
	auth.GET("/projects/:projectID/testcases", a.listTestCases)
	// Static testcase sub-paths MUST come before :testcaseID params (Gin requirement)
	auth.POST("/projects/:projectID/testcases/batch-delete", a.batchDeleteTestCases)
	auth.POST("/projects/:projectID/testcases/batch-update-level", a.batchUpdateLevel)
	auth.POST("/projects/:projectID/testcases/batch-move", a.batchMoveTestCases)
	auth.GET("/projects/:projectID/testcases/export", a.exportTestCasesXlsx)
	auth.POST("/projects/:projectID/testcases/import", a.importTestCasesXlsx)
	// Parameterized :testcaseID routes
	auth.PUT("/projects/:projectID/testcases/:testcaseID", a.updateTestCase)
	auth.DELETE("/projects/:projectID/testcases/:testcaseID", a.deleteTestCase)
	auth.POST("/projects/:projectID/testcases/:testcaseID/clone", a.cloneTestCase)
	auth.GET("/projects/:projectID/testcases/:testcaseID/history", a.listCaseHistory)
	auth.GET("/projects/:projectID/testcases/:testcaseID/relations", a.listCaseRelations)
	auth.POST("/projects/:projectID/testcases/:testcaseID/relations", a.createCaseRelation)
	auth.DELETE("/projects/:projectID/testcases/:testcaseID/relations/:relationID", a.deleteCaseRelation)
	auth.POST("/projects/:projectID/testcases/:testcaseID/attachments", a.uploadAttachment)
	auth.GET("/projects/:projectID/testcases/:testcaseID/attachments", a.listAttachments)
	auth.DELETE("/projects/:projectID/attachments/:attachmentID", a.deleteAttachment)
	auth.GET("/projects/:projectID/attachments/:attachmentID/download", a.downloadAttachment)
	auth.GET("/projects/:projectID/modules", a.listModules)
	auth.POST("/projects/:projectID/modules", a.createModule)
	auth.PUT("/projects/:projectID/modules/:moduleID", a.renameModule)
	auth.PUT("/projects/:projectID/modules/:moduleID/move", a.moveModule)
	auth.DELETE("/projects/:projectID/modules/:moduleID", a.deleteModule)
	auth.POST("/projects/:projectID/scripts", a.createScript)
	auth.GET("/projects/:projectID/scripts", a.listScripts)
	auth.POST("/projects/:projectID/requirements/:requirementID/testcases/:testcaseID", a.linkRequirementAndTestCase)
	auth.POST("/projects/:projectID/testcases/:testcaseID/scripts/:scriptID", a.linkTestCaseAndScript)
	auth.POST("/projects/:projectID/runs", a.createRun)
	auth.GET("/projects/:projectID/runs/:runID/results", a.listRunResults)
	auth.POST("/projects/:projectID/defects", a.createDefect)
	auth.GET("/projects/:projectID/defects", a.listDefects)
	auth.GET("/projects/:projectID/overview", a.projectDemoOverview)
	auth.POST("/webhooks/gitlab", a.mockGitLabWebhook)
	auth.GET("/audit-logs", a.listAuditLogs)

	return r
}

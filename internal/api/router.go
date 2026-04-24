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
	Logger                      *slog.Logger
	AuthService                 *service.AuthService
	UserService                 *service.UserService
	RoleService                 *service.RoleService
	ProjectService              *service.ProjectService
	TestCaseService             *service.TestCaseService
	ProfileService              *service.ProfileService
	ExecutionService            *service.ExecutionService
	DefectService               *service.DefectService
	RequirementService          *service.RequirementService
	ScriptService               *service.ScriptService
	OverviewService             *service.OverviewService
	AuditService                *service.AuditService
	ModuleService               *service.ModuleService
	AttachmentService           *service.AttachmentService
	CaseHistoryRepo             *repository.CaseHistoryRepo
	CaseRelationRepo            *repository.CaseRelationRepo
	XlsxService                 *service.XlsxService
	AIScriptService             *service.AIScriptService
	CaseReviewService           *service.CaseReviewService
	CaseReviewSubmitService     *service.CaseReviewSubmitService
	CaseReviewAttachmentService *service.CaseReviewAttachmentService
	CaseReviewRuleService       *service.CaseReviewRuleService
	CaseReviewDefectService     *service.CaseReviewDefectService
	TagService                  *service.TagService
	ExecutorURL                 string
	ExecutorAPIKey              string
}

// API 核心结构体
type API struct {
	logger                  *slog.Logger
	allowedOrigins          []string
	authSvc                 *service.AuthService
	userSvc                 *service.UserService
	roleSvc                 *service.RoleService
	projectSvc              *service.ProjectService
	testCaseSvc             *service.TestCaseService
	profileSvc              *service.ProfileService
	executionSvc            *service.ExecutionService
	defectSvc               *service.DefectService
	requirementSvc          *service.RequirementService
	scriptSvc               *service.ScriptService
	overviewSvc             *service.OverviewService
	auditSvc                *service.AuditService
	moduleSvc               *service.ModuleService
	attachmentSvc           *service.AttachmentService
	caseHistoryRepo         *repository.CaseHistoryRepo
	caseRelationRepo        *repository.CaseRelationRepo
	xlsxSvc                 *service.XlsxService
	aiScriptSvc             *service.AIScriptService
	caseReviewSvc           *service.CaseReviewService
	caseReviewSubmitSvc     *service.CaseReviewSubmitService
	caseReviewAttachmentSvc *service.CaseReviewAttachmentService
	caseReviewRuleSvc       *service.CaseReviewRuleService
	caseReviewDefectSvc     *service.CaseReviewDefectService
	tagSvc                  *service.TagService
	executorURL             string
	executorAPIKey          string
}

// NewRouter 创建路由引擎并注册所有路由
func NewRouter(deps Dependencies, corsOrigins string) http.Handler {
	a := &API{
		logger:                  deps.Logger,
		allowedOrigins:          parseAllowedOrigins(corsOrigins),
		authSvc:                 deps.AuthService,
		userSvc:                 deps.UserService,
		roleSvc:                 deps.RoleService,
		projectSvc:              deps.ProjectService,
		testCaseSvc:             deps.TestCaseService,
		profileSvc:              deps.ProfileService,
		executionSvc:            deps.ExecutionService,
		defectSvc:               deps.DefectService,
		requirementSvc:          deps.RequirementService,
		scriptSvc:               deps.ScriptService,
		overviewSvc:             deps.OverviewService,
		auditSvc:                deps.AuditService,
		moduleSvc:               deps.ModuleService,
		attachmentSvc:           deps.AttachmentService,
		caseHistoryRepo:         deps.CaseHistoryRepo,
		caseRelationRepo:        deps.CaseRelationRepo,
		xlsxSvc:                 deps.XlsxService,
		aiScriptSvc:             deps.AIScriptService,
		caseReviewSvc:           deps.CaseReviewService,
		caseReviewSubmitSvc:     deps.CaseReviewSubmitService,
		caseReviewAttachmentSvc: deps.CaseReviewAttachmentService,
		caseReviewRuleSvc:       deps.CaseReviewRuleService,
		caseReviewDefectSvc:     deps.CaseReviewDefectService,
		tagSvc:                  deps.TagService,
		executorURL:             deps.ExecutorURL,
		executorAPIKey:          deps.ExecutorAPIKey,
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
	// 轻量查询接口：仅返回 id/name/email/avatar，所有认证用户可用
	// 用于评审人、被指派人等下拉选择；必须注册在 /users/:userID 之前以避免路由冲突
	auth.GET("/users/lookup", a.listUsersLookup)
	auth.POST("/users", a.createUser)
	auth.PUT("/users/:userID", a.updateUser)
	auth.DELETE("/users/:userID", a.deleteUser)
	auth.PUT("/users/:userID/reset-password", a.resetPassword)
	auth.PUT("/users/:userID/toggle-active", a.toggleActive)
	auth.POST("/users/:userID/roles", a.assignUserRoles)
	auth.POST("/users/:userID/projects", a.assignUserProjects)
	auth.POST("/users/:userID/avatar", a.uploadUserAvatar)
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
	auth.POST("/projects/:projectID/testcases/batch-tag", a.batchTagTestCases)
	auth.GET("/projects/:projectID/testcases/export", a.exportTestCasesXlsx)
	auth.GET("/projects/:projectID/testcases/export-report", a.exportReportXlsx)
	auth.POST("/projects/:projectID/testcases/import", a.importTestCasesXlsx)
	auth.POST("/projects/:projectID/testcases/:testcaseID/analyze", a.analyzeTestCase)
	// Parameterized :testcaseID routes
	auth.GET("/projects/:projectID/testcases/:testcaseID", a.getTestCase)
	auth.PUT("/projects/:projectID/testcases/:testcaseID", a.updateTestCase)
	auth.DELETE("/projects/:projectID/testcases/:testcaseID", a.deleteTestCase)
	// Single testcase operations (Use singular "testcase" to avoid Gin POST conflict with static "testcases" batch routes)
	auth.POST("/projects/:projectID/testcase/:testcaseID/clone", a.cloneTestCase)
	auth.POST("/projects/:projectID/testcase/:testcaseID/discard", a.discardTestCase)
	auth.POST("/projects/:projectID/testcase/:testcaseID/recover", a.recoverTestCase)
	auth.GET("/projects/:projectID/testcases/:testcaseID/history", a.listCaseHistory)
	auth.GET("/projects/:projectID/testcases/:testcaseID/activities", a.listCaseActivities)
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

	// ---- 测试智编 ----
	aiScript := auth.Group("/ai-script")
	// 任务
	aiScript.GET("/tasks", a.listAIScriptTasks)
	aiScript.POST("/tasks", a.createAIScriptTask)
	aiScript.POST("/tasks/batch-discard", a.batchDiscardTasks)
	aiScript.POST("/tasks/batch-delete", a.batchDeleteTasks)
	aiScript.GET("/tasks/:taskID", a.getAIScriptTask)
	aiScript.POST("/tasks/:taskID/execute", a.executeAIScriptTask)
	aiScript.POST("/tasks/:taskID/discard", a.discardTask)
	aiScript.DELETE("/tasks/:taskID", a.deleteTask)
	aiScript.POST("/tasks/:taskID/clone", a.cloneTask)
	aiScript.POST("/tasks/:taskID/cases/update", a.updateTaskCases)
	// 录制
	aiScript.POST("/tasks/:taskID/recording/start", a.startRecording)
	aiScript.POST("/tasks/:taskID/recording/finish", a.finishRecording)
	aiScript.POST("/tasks/:taskID/recording/fail", a.failRecording)
	aiScript.GET("/tasks/:taskID/recordings/latest", a.getLatestRecording)
	// 脚本版本
	aiScript.GET("/tasks/:taskID/versions", a.getAIScriptVersions)
	aiScript.GET("/tasks/:taskID/current-script", a.getCurrentAIScript)
	aiScript.POST("/tasks/:taskID/edit-script", a.editAIScript)
	// 脚本操作
	aiScript.POST("/scripts/:scriptID/confirm", a.confirmScript)
	aiScript.POST("/scripts/:scriptID/discard", a.discardScript)
	aiScript.GET("/scripts/:scriptID/export", a.exportScript)
	// 验证
	aiScript.POST("/tasks/:taskID/validate", a.triggerAIScriptValidation)
	aiScript.GET("/scripts/:scriptID/validations", a.getValidationHistory)
	aiScript.GET("/validations/latest", a.getAIScriptLatestValidation)
	// 轨迹与证据
	aiScript.GET("/tasks/:taskID/traces", a.getAIScriptTraces)
	aiScript.GET("/tasks/:taskID/evidences", a.getAIScriptEvidences)

	// ---- 用例评审 ----
	caseReview := auth.Group("/projects/:projectID/case-reviews")
	caseReview.GET("", a.listReviews)
	// 汇总接口：项目级全局统计（计划分状态计数 + 我待评审数）
	// 必须注册在 /:reviewID 之前，防止被动态路由吞掉
	caseReview.GET("/summary", a.getReviewSummary)
	caseReview.POST("", a.createReview)
	caseReview.GET("/:reviewID", a.getReview)
	caseReview.PUT("/:reviewID", a.updateReview)
	caseReview.DELETE("/:reviewID", a.deleteReview)
	caseReview.POST("/:reviewID/close", a.closeReview)
	caseReview.POST("/:reviewID/copy", a.copyReview)
	caseReview.GET("/:reviewID/items", a.listItems)
	caseReview.POST("/:reviewID/items/link", a.linkItems)
	caseReview.POST("/:reviewID/items/unlink", a.unlinkItems)
	caseReview.POST("/:reviewID/items/batch-review", a.batchReviewItems)
	caseReview.POST("/:reviewID/items/batch-reassign", a.batchReassignReviewers)
	caseReview.POST("/:reviewID/items/batch-resubmit", a.batchResubmitItems)
	caseReview.POST("/:reviewID/items/:itemID/review", a.submitItemReview)
	caseReview.GET("/:reviewID/items/:itemID/records", a.listItemRecords)
	// 评审附件：item 维度
	caseReview.POST("/:reviewID/items/:itemID/attachments", a.uploadReviewAttachment)
	caseReview.GET("/:reviewID/items/:itemID/attachments", a.listReviewAttachments)

	// ---- 用例评审 v0.2 新增 ----
	// 规则引擎：单项 rerun / 计划级一键全量
	caseReview.POST("/:reviewID/items/:itemID/ai-gate/rerun", a.rerunAIGate)
	caseReview.POST("/:reviewID/ai-gate/run-all", a.runPlanAIGate)
	// Action Items：单项列表 / 计划级列表（跨项聚合）
	caseReview.GET("/:reviewID/items/:itemID/defects", a.listItemDefects)
	caseReview.GET("/:reviewID/defects", a.listReviewDefects)

	// Action Items 单项操作（与 case-reviews 并列，使用独立路径便于前端直链）
	auth.GET("/projects/:projectID/case-review-defects/:defectID", a.getDefect)
	auth.POST("/projects/:projectID/case-review-defects/:defectID/resolve", a.resolveDefect)
	auth.POST("/projects/:projectID/case-review-defects/:defectID/dispute", a.disputeDefect)
	auth.POST("/projects/:projectID/case-review-defects/:defectID/reopen", a.reopenDefect)

	// 项目 settings（v0.2 引入，用于开关自审等）
	auth.GET("/projects/:projectID/settings", a.getProjectSettings)
	auth.PUT("/projects/:projectID/settings", a.updateProjectSettings)

	// 评审附件：按用例聚合（只读镜像）+ 附件自身操作
	auth.GET("/projects/:projectID/testcases/:testcaseID/review-attachments", a.listReviewAttachmentsByTestCase)
	auth.DELETE("/projects/:projectID/review-attachments/:attachmentID", a.deleteReviewAttachment)
	auth.GET("/projects/:projectID/review-attachments/:attachmentID/download", a.downloadReviewAttachment)

	// ---- 标签管理 ----
	tags := auth.Group("/projects/:projectID/tags")
	tags.GET("", a.listTags)
	tags.GET("/options", a.listTagOptions)
	tags.POST("", a.createTag)
	tags.PUT("/:tagID", a.updateTag)
	tags.DELETE("/:tagID", a.deleteTag)

	return r
}

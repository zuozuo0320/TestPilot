package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"testpilot/internal/execution"
	"testpilot/internal/model"
)

const currentUserKey = "current-user"

var defaultAllowedOrigins = []string{
	"http://localhost:5173",
	"http://127.0.0.1:5173",
	"http://localhost:3000",
	"http://127.0.0.1:3000",
}

type Dependencies struct {
	DB             *gorm.DB
	Redis          *redis.Client
	Logger         *slog.Logger
	Executor       *execution.MockExecutor
	AllowedOrigins string
}

type API struct {
	db             *gorm.DB
	redis          *redis.Client
	logger         *slog.Logger
	executor       *execution.MockExecutor
	allowedOrigins []string
}

type createUserRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

type createProjectRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type addMemberRequest struct {
	UserID uint   `json:"user_id"`
	Role   string `json:"role"`
}

type createRequirementRequest struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

type createTestCaseRequest struct {
	Title    string `json:"title"`
	Steps    string `json:"steps"`
	Priority string `json:"priority"`
}

type createScriptRequest struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"`
}

type createRunRequest struct {
	Mode      string `json:"mode"`
	ScriptID  uint   `json:"script_id"`
	ScriptIDs []uint `json:"script_ids"`
}

type createDefectRequest struct {
	RunResultID uint   `json:"run_result_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Severity    string `json:"severity"`
}

type loginRequest struct {
	Email string `json:"email"`
}

func NewRouter(deps Dependencies) *gin.Engine {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	if deps.Executor == nil {
		deps.Executor = execution.NewMockExecutor(deps.Logger, 0.25)
	}

	api := &API{
		db:             deps.DB,
		redis:          deps.Redis,
		logger:         deps.Logger,
		executor:       deps.Executor,
		allowedOrigins: parseAllowedOrigins(deps.AllowedOrigins),
	}

	router := gin.New()
	router.Use(gin.Recovery(), api.corsMiddleware(), api.requestLogger())

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	router.POST("/api/v1/auth/login", api.login)

	v1 := router.Group("/api/v1")
	v1.Use(api.authMiddleware())
	{
		v1.GET("/users", api.listUsers)
		v1.POST("/users", api.createUser)

		v1.POST("/projects", api.createProject)
		v1.GET("/projects", api.listProjects)
		v1.POST("/projects/:projectID/members", api.addProjectMember)
		v1.GET("/projects/:projectID/members", api.listProjectMembers)

		v1.POST("/projects/:projectID/requirements", api.createRequirement)
		v1.GET("/projects/:projectID/requirements", api.listRequirements)

		v1.POST("/projects/:projectID/testcases", api.createTestCase)
		v1.GET("/projects/:projectID/testcases", api.listTestCases)

		v1.POST("/projects/:projectID/scripts", api.createScript)
		v1.GET("/projects/:projectID/scripts", api.listScripts)

		v1.POST("/projects/:projectID/requirements/:requirementID/testcases/:testcaseID/link", api.linkRequirementAndTestCase)
		v1.POST("/projects/:projectID/testcases/:testcaseID/scripts/:scriptID/link", api.linkTestCaseAndScript)

		v1.POST("/projects/:projectID/runs", api.createRun)
		v1.GET("/projects/:projectID/runs/:runID/results", api.listRunResults)

		v1.POST("/projects/:projectID/defects", api.createDefect)
		v1.GET("/projects/:projectID/defects", api.listDefects)
		v1.GET("/projects/:projectID/demo-overview", api.projectDemoOverview)

		v1.POST("/integrations/mock-gitlab/webhook", api.mockGitLabWebhook)
	}

	return router
}

func (a *API) requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}

		a.logger.Info("http_request",
			"method", c.Request.Method,
			"path", path,
			"status", c.Writer.Status(),
			"latency_ms", time.Since(start).Milliseconds(),
			"client_ip", c.ClientIP(),
		)
	}
}

func (a *API) corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" && a.isAllowedOrigin(origin) {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Access-Control-Allow-Headers", "Content-Type, X-User-ID")
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			c.Header("Vary", "Origin")
		}

		if c.Request.Method == http.MethodOptions {
			c.Status(http.StatusNoContent)
			c.Abort()
			return
		}

		c.Next()
	}
}

func (a *API) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		userIDText := c.GetHeader("X-User-ID")
		if userIDText == "" {
			respondError(c, http.StatusUnauthorized, "missing X-User-ID header")
			c.Abort()
			return
		}

		userID64, err := strconv.ParseUint(userIDText, 10, 64)
		if err != nil || userID64 == 0 {
			respondError(c, http.StatusUnauthorized, "invalid X-User-ID header")
			c.Abort()
			return
		}

		var user model.User
		if err := a.db.First(&user, uint(userID64)).Error; err != nil {
			respondError(c, http.StatusUnauthorized, "user not found")
			c.Abort()
			return
		}

		c.Set(currentUserKey, user)
		c.Next()
	}
}

func (a *API) login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" {
		respondError(c, http.StatusBadRequest, "email is required")
		return
	}

	var user model.User
	if err := a.db.Where("email = ?", email).First(&user).Error; err != nil {
		respondError(c, http.StatusUnauthorized, "invalid credentials")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token":   fmt.Sprintf("demo-user-%d", user.ID),
		"user_id": user.ID,
		"user":    user,
	})
}

func (a *API) listUsers(c *gin.Context) {
	user := currentUser(c)
	if !a.requireRole(c, user, model.GlobalRoleAdmin, model.GlobalRoleManager) {
		return
	}

	var users []model.User
	if err := a.db.Order("id asc").Find(&users).Error; err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{"users": users})
}

func (a *API) createUser(c *gin.Context) {
	user := currentUser(c)
	if !a.requireRole(c, user, model.GlobalRoleAdmin) {
		return
	}

	var req createUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	req.Role = strings.ToLower(strings.TrimSpace(req.Role))

	if req.Name == "" || req.Email == "" || !model.IsValidGlobalRole(req.Role) {
		respondError(c, http.StatusBadRequest, "name/email/role is invalid")
		return
	}

	entity := model.User{Name: req.Name, Email: req.Email, Role: req.Role}
	if err := a.db.Create(&entity).Error; err != nil {
		if isDuplicateError(err) {
			respondError(c, http.StatusConflict, "user already exists")
			return
		}
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}

	c.JSON(http.StatusCreated, entity)
}

func (a *API) createProject(c *gin.Context) {
	user := currentUser(c)
	if !a.requireRole(c, user, model.GlobalRoleAdmin, model.GlobalRoleManager) {
		return
	}

	var req createProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		respondError(c, http.StatusBadRequest, "project name is required")
		return
	}

	project := model.Project{Name: req.Name, Description: strings.TrimSpace(req.Description)}
	if err := a.db.Create(&project).Error; err != nil {
		if isDuplicateError(err) {
			respondError(c, http.StatusConflict, "project already exists")
			return
		}
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}

	member := model.ProjectMember{ProjectID: project.ID, UserID: user.ID, Role: model.MemberRoleOwner}
	if err := a.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "project_id"}, {Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"role", "updated_at"}),
	}).Create(&member).Error; err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}

	c.JSON(http.StatusCreated, project)
}

func (a *API) listProjects(c *gin.Context) {
	user := currentUser(c)

	var projects []model.Project
	query := a.db.Model(&model.Project{})
	if user.Role != model.GlobalRoleAdmin {
		query = query.Joins("JOIN project_members pm ON pm.project_id = projects.id").Where("pm.user_id = ?", user.ID)
	}

	if err := query.Order("projects.id asc").Find(&projects).Error; err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{"projects": projects})
}

func (a *API) addProjectMember(c *gin.Context) {
	requestUser := currentUser(c)
	if !a.requireRole(c, requestUser, model.GlobalRoleAdmin, model.GlobalRoleManager) {
		return
	}

	projectID, ok := parseUintParam(c, "projectID")
	if !ok || !a.requireProjectAccess(c, requestUser, projectID) {
		return
	}

	var req addMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	if req.UserID == 0 || !model.IsValidMemberRole(req.Role) {
		respondError(c, http.StatusBadRequest, "user_id/role is invalid")
		return
	}

	var targetUser model.User
	if err := a.db.First(&targetUser, req.UserID).Error; err != nil {
		respondError(c, http.StatusNotFound, "target user not found")
		return
	}

	member := model.ProjectMember{ProjectID: projectID, UserID: req.UserID, Role: req.Role}
	if err := a.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "project_id"}, {Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"role", "updated_at"}),
	}).Create(&member).Error; err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "member added", "member": member})
}

func (a *API) listProjectMembers(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok || !a.requireProjectAccess(c, user, projectID) {
		return
	}

	var members []model.ProjectMember
	if err := a.db.Preload("User").Where("project_id = ?", projectID).Order("id asc").Find(&members).Error; err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{"members": members})
}

func (a *API) createRequirement(c *gin.Context) {
	user := currentUser(c)
	if !a.requireRole(c, user, model.GlobalRoleAdmin, model.GlobalRoleManager) {
		return
	}

	projectID, ok := parseUintParam(c, "projectID")
	if !ok || !a.requireProjectAccess(c, user, projectID) {
		return
	}

	var req createRequirementRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		respondError(c, http.StatusBadRequest, "title is required")
		return
	}

	entity := model.Requirement{ProjectID: projectID, Title: strings.TrimSpace(req.Title), Content: strings.TrimSpace(req.Content)}
	if err := a.db.Create(&entity).Error; err != nil {
		if isDuplicateError(err) {
			respondError(c, http.StatusConflict, "requirement already exists")
			return
		}
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusCreated, entity)
}

func (a *API) listRequirements(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok || !a.requireProjectAccess(c, user, projectID) {
		return
	}

	var entities []model.Requirement
	if err := a.db.Where("project_id = ?", projectID).Order("id asc").Find(&entities).Error; err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"requirements": entities})
}

func (a *API) createTestCase(c *gin.Context) {
	user := currentUser(c)
	if !a.requireRole(c, user, model.GlobalRoleAdmin, model.GlobalRoleManager) {
		return
	}

	projectID, ok := parseUintParam(c, "projectID")
	if !ok || !a.requireProjectAccess(c, user, projectID) {
		return
	}

	var req createTestCaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		respondError(c, http.StatusBadRequest, "title is required")
		return
	}
	priority := strings.ToLower(strings.TrimSpace(req.Priority))
	if priority == "" {
		priority = "medium"
	}

	entity := model.TestCase{ProjectID: projectID, Title: strings.TrimSpace(req.Title), Steps: strings.TrimSpace(req.Steps), Priority: priority}
	if err := a.db.Create(&entity).Error; err != nil {
		if isDuplicateError(err) {
			respondError(c, http.StatusConflict, "testcase already exists")
			return
		}
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusCreated, entity)
}

func (a *API) listTestCases(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok || !a.requireProjectAccess(c, user, projectID) {
		return
	}

	var entities []model.TestCase
	if err := a.db.Where("project_id = ?", projectID).Order("id asc").Find(&entities).Error; err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"testcases": entities})
}

func (a *API) createScript(c *gin.Context) {
	user := currentUser(c)
	if !a.requireRole(c, user, model.GlobalRoleAdmin, model.GlobalRoleManager) {
		return
	}

	projectID, ok := parseUintParam(c, "projectID")
	if !ok || !a.requireProjectAccess(c, user, projectID) {
		return
	}

	var req createScriptRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Path) == "" {
		respondError(c, http.StatusBadRequest, "name/path is required")
		return
	}

	scriptType := strings.ToLower(strings.TrimSpace(req.Type))
	if scriptType == "" {
		scriptType = "cypress"
	}
	entity := model.Script{ProjectID: projectID, Name: strings.TrimSpace(req.Name), Path: strings.TrimSpace(req.Path), Type: scriptType}
	if err := a.db.Create(&entity).Error; err != nil {
		if isDuplicateError(err) {
			respondError(c, http.StatusConflict, "script already exists")
			return
		}
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusCreated, entity)
}

func (a *API) listScripts(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok || !a.requireProjectAccess(c, user, projectID) {
		return
	}

	var scripts []model.Script
	if err := a.db.Where("project_id = ?", projectID).Order("id asc").Find(&scripts).Error; err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"scripts": scripts})
}

func (a *API) linkRequirementAndTestCase(c *gin.Context) {
	user := currentUser(c)
	if !a.requireRole(c, user, model.GlobalRoleAdmin, model.GlobalRoleManager, model.GlobalRoleTester) {
		return
	}

	projectID, ok := parseUintParam(c, "projectID")
	if !ok || !a.requireProjectAccess(c, user, projectID) {
		return
	}
	requirementID, ok := parseUintParam(c, "requirementID")
	if !ok {
		return
	}
	testCaseID, ok := parseUintParam(c, "testcaseID")
	if !ok {
		return
	}

	if !a.belongsToProject(&model.Requirement{}, requirementID, projectID) || !a.belongsToProject(&model.TestCase{}, testCaseID, projectID) {
		respondError(c, http.StatusNotFound, "requirement or testcase not found in project")
		return
	}

	link := model.RequirementTestCase{RequirementID: requirementID, TestCaseID: testCaseID}
	if err := a.db.Where(&link).FirstOrCreate(&link).Error; err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "requirement linked to testcase", "link": link})
}

func (a *API) linkTestCaseAndScript(c *gin.Context) {
	user := currentUser(c)
	if !a.requireRole(c, user, model.GlobalRoleAdmin, model.GlobalRoleManager, model.GlobalRoleTester) {
		return
	}

	projectID, ok := parseUintParam(c, "projectID")
	if !ok || !a.requireProjectAccess(c, user, projectID) {
		return
	}
	testCaseID, ok := parseUintParam(c, "testcaseID")
	if !ok {
		return
	}
	scriptID, ok := parseUintParam(c, "scriptID")
	if !ok {
		return
	}

	if !a.belongsToProject(&model.TestCase{}, testCaseID, projectID) || !a.belongsToProject(&model.Script{}, scriptID, projectID) {
		respondError(c, http.StatusNotFound, "testcase or script not found in project")
		return
	}

	link := model.TestCaseScript{TestCaseID: testCaseID, ScriptID: scriptID}
	if err := a.db.Where(&link).FirstOrCreate(&link).Error; err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "testcase linked to script", "link": link})
}

func (a *API) createRun(c *gin.Context) {
	user := currentUser(c)
	if !a.requireRole(c, user, model.GlobalRoleAdmin, model.GlobalRoleManager, model.GlobalRoleTester) {
		return
	}

	projectID, ok := parseUintParam(c, "projectID")
	if !ok || !a.requireProjectAccess(c, user, projectID) {
		return
	}

	var req createRunRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	req.Mode = strings.ToLower(strings.TrimSpace(req.Mode))

	scripts, err := a.resolveScriptsForRun(projectID, req)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	run := model.Run{ProjectID: projectID, TriggeredBy: user.ID, Mode: req.Mode, Status: "running"}
	results := make([]model.RunResult, 0, len(scripts))
	overallStatus := "passed"

	tx := a.db.Begin()
	if tx.Error != nil {
		respondError(c, http.StatusInternalServerError, tx.Error.Error())
		return
	}

	if err := tx.Create(&run).Error; err != nil {
		tx.Rollback()
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}

	for _, script := range scripts {
		execResult := a.executor.RunScript(script)
		if execResult.Status == "failed" {
			overallStatus = "failed"
		}

		result := model.RunResult{
			RunID:      run.ID,
			ProjectID:  projectID,
			ScriptID:   script.ID,
			Status:     execResult.Status,
			Output:     execResult.Output,
			DurationMS: execResult.DurationMS,
		}
		if err := tx.Create(&result).Error; err != nil {
			tx.Rollback()
			respondError(c, http.StatusInternalServerError, err.Error())
			return
		}
		results = append(results, result)
	}

	run.Status = overallStatus
	if err := tx.Save(&run).Error; err != nil {
		tx.Rollback()
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}

	if err := tx.Commit().Error; err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}

	a.cacheRunStatus(c.Request.Context(), run.ID, overallStatus)

	c.JSON(http.StatusCreated, gin.H{"run": run, "results": results})
}

func (a *API) listRunResults(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok || !a.requireProjectAccess(c, user, projectID) {
		return
	}
	runID, ok := parseUintParam(c, "runID")
	if !ok {
		return
	}

	var run model.Run
	if err := a.db.Where("id = ? AND project_id = ?", runID, projectID).First(&run).Error; err != nil {
		respondError(c, http.StatusNotFound, "run not found")
		return
	}

	var results []model.RunResult
	if err := a.db.Where("run_id = ? AND project_id = ?", runID, projectID).Order("id asc").Find(&results).Error; err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"run": run, "results": results})
}

func (a *API) createDefect(c *gin.Context) {
	user := currentUser(c)
	if !a.requireRole(c, user, model.GlobalRoleAdmin, model.GlobalRoleManager, model.GlobalRoleTester) {
		return
	}

	projectID, ok := parseUintParam(c, "projectID")
	if !ok || !a.requireProjectAccess(c, user, projectID) {
		return
	}

	var req createDefectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	if req.RunResultID == 0 || strings.TrimSpace(req.Title) == "" {
		respondError(c, http.StatusBadRequest, "run_result_id/title is required")
		return
	}

	severity := strings.ToLower(strings.TrimSpace(req.Severity))
	if severity == "" {
		severity = "medium"
	}
	if !isValidSeverity(severity) {
		respondError(c, http.StatusBadRequest, "severity should be low/medium/high/critical")
		return
	}

	var runResult model.RunResult
	if err := a.db.Where("id = ? AND project_id = ?", req.RunResultID, projectID).First(&runResult).Error; err != nil {
		respondError(c, http.StatusNotFound, "run result not found")
		return
	}

	defect := model.Defect{
		ProjectID:   projectID,
		RunResultID: req.RunResultID,
		Title:       strings.TrimSpace(req.Title),
		Description: strings.TrimSpace(req.Description),
		Severity:    severity,
		Status:      "open",
		CreatedBy:   user.ID,
	}
	if err := a.db.Create(&defect).Error; err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}

	c.JSON(http.StatusCreated, defect)
}

func (a *API) listDefects(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok || !a.requireProjectAccess(c, user, projectID) {
		return
	}

	var defects []model.Defect
	if err := a.db.Where("project_id = ?", projectID).Order("id asc").Find(&defects).Error; err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"defects": defects})
}

func (a *API) projectDemoOverview(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok || !a.requireProjectAccess(c, user, projectID) {
		return
	}

	var project model.Project
	if err := a.db.First(&project, projectID).Error; err != nil {
		respondError(c, http.StatusNotFound, "project not found")
		return
	}

	summary := gin.H{
		"project": project,
		"counts":  gin.H{},
		"latest_run": gin.H{},
		"quality_gate": gin.H{
			"status": "no_runs",
			"reason": "no execution data",
		},
	}

	counts := map[string]int64{}
	countTargets := map[string]any{
		"requirements": &model.Requirement{},
		"testcases":    &model.TestCase{},
		"scripts":      &model.Script{},
		"runs":         &model.Run{},
		"defects":      &model.Defect{},
	}

	for key, target := range countTargets {
		var count int64
		if err := a.db.Model(target).Where("project_id = ?", projectID).Count(&count).Error; err != nil {
			respondError(c, http.StatusInternalServerError, err.Error())
			return
		}
		counts[key] = count
	}
	summary["counts"] = counts

	var latestRun model.Run
	if err := a.db.Where("project_id = ?", projectID).Order("id desc").First(&latestRun).Error; err != nil {
		if !isNotFound(err) {
			respondError(c, http.StatusInternalServerError, err.Error())
			return
		}
		c.JSON(http.StatusOK, summary)
		return
	}

	var totalResults int64
	if err := a.db.Model(&model.RunResult{}).Where("run_id = ?", latestRun.ID).Count(&totalResults).Error; err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}

	var passedResults int64
	if err := a.db.Model(&model.RunResult{}).Where("run_id = ? AND status = ?", latestRun.ID, "passed").Count(&passedResults).Error; err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}

	passRate := 0.0
	if totalResults > 0 {
		passRate = (float64(passedResults) / float64(totalResults)) * 100
	}

	qualityStatus := "blocked"
	reason := "pass_rate_below_threshold"
	if totalResults == 0 {
		qualityStatus = "no_runs"
		reason = "no execution data"
	} else if passRate >= 95 {
		qualityStatus = "pass"
		reason = "pass_rate_meets_threshold"
	}

	summary["latest_run"] = gin.H{
		"id":             latestRun.ID,
		"status":         latestRun.Status,
		"mode":           latestRun.Mode,
		"created_at":     latestRun.CreatedAt,
		"total_results":  totalResults,
		"passed_results": passedResults,
		"pass_rate":      passRate,
	}
	summary["quality_gate"] = gin.H{
		"status":      qualityStatus,
		"threshold":   95,
		"pass_rate":   passRate,
		"latest_run":  latestRun.ID,
		"reason":      reason,
	}

	c.JSON(http.StatusOK, summary)
}

func (a *API) mockGitLabWebhook(c *gin.Context) {
	var payload map[string]any
	if err := c.ShouldBindJSON(&payload); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	a.logger.Info("mock_gitlab_webhook", "payload", payload)
	c.JSON(http.StatusOK, gin.H{"message": "mock webhook accepted"})
}

func (a *API) resolveScriptsForRun(projectID uint, req createRunRequest) ([]model.Script, error) {
	var scripts []model.Script
	switch req.Mode {
	case "all":
		if err := a.db.Where("project_id = ?", projectID).Order("id asc").Find(&scripts).Error; err != nil {
			return nil, err
		}
		if len(scripts) == 0 {
			return nil, fmt.Errorf("no scripts in project")
		}
		return scripts, nil
	case "one":
		if req.ScriptID == 0 {
			return nil, fmt.Errorf("script_id is required when mode=one")
		}
		var script model.Script
		if err := a.db.Where("id = ? AND project_id = ?", req.ScriptID, projectID).First(&script).Error; err != nil {
			return nil, fmt.Errorf("script not found in project")
		}
		return []model.Script{script}, nil
	case "batch":
		ids := uniqueUint(req.ScriptIDs)
		if len(ids) == 0 {
			return nil, fmt.Errorf("script_ids is required when mode=batch")
		}
		if err := a.db.Where("project_id = ? AND id IN ?", projectID, ids).Order("id asc").Find(&scripts).Error; err != nil {
			return nil, err
		}
		if len(scripts) != len(ids) {
			return nil, fmt.Errorf("some script_ids are missing or outside project")
		}
		return scripts, nil
	default:
		return nil, fmt.Errorf("mode should be all/one/batch")
	}
}

func (a *API) cacheRunStatus(ctx context.Context, runID uint, status string) {
	if a.redis == nil {
		return
	}

	cacheCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	if err := a.redis.Set(cacheCtx, fmt.Sprintf("run:%d:status", runID), status, 30*time.Minute).Err(); err != nil {
		a.logger.Warn("cache run status failed", "run_id", runID, "error", err)
	}
}

func (a *API) belongsToProject(entity any, id, projectID uint) bool {
	var count int64
	err := a.db.Model(entity).Where("id = ? AND project_id = ?", id, projectID).Count(&count).Error
	return err == nil && count > 0
}

func (a *API) requireProjectAccess(c *gin.Context, user model.User, projectID uint) bool {
	if !a.projectExists(projectID) {
		respondError(c, http.StatusNotFound, "project not found")
		return false
	}
	if user.Role == model.GlobalRoleAdmin {
		return true
	}

	var count int64
	if err := a.db.Model(&model.ProjectMember{}).Where("project_id = ? AND user_id = ?", projectID, user.ID).Count(&count).Error; err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return false
	}
	if count == 0 {
		respondError(c, http.StatusForbidden, "no project access")
		return false
	}
	return true
}

func (a *API) projectExists(projectID uint) bool {
	var count int64
	if err := a.db.Model(&model.Project{}).Where("id = ?", projectID).Count(&count).Error; err != nil {
		return false
	}
	return count > 0
}

func (a *API) requireRole(c *gin.Context, user model.User, roles ...string) bool {
	if user.Role == model.GlobalRoleAdmin {
		return true
	}
	for _, role := range roles {
		if user.Role == role {
			return true
		}
	}
	respondError(c, http.StatusForbidden, "insufficient role")
	return false
}

func currentUser(c *gin.Context) model.User {
	value, ok := c.Get(currentUserKey)
	if !ok {
		return model.User{}
	}
	user, ok := value.(model.User)
	if !ok {
		return model.User{}
	}
	return user
}

func parseUintParam(c *gin.Context, key string) (uint, bool) {
	text := c.Param(key)
	value, err := strconv.ParseUint(text, 10, 64)
	if err != nil || value == 0 {
		respondError(c, http.StatusBadRequest, fmt.Sprintf("invalid path param: %s", key))
		return 0, false
	}
	return uint(value), true
}

func respondError(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"error": message})
}

func isDuplicateError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "duplicate") || strings.Contains(text, "unique")
}

func isValidSeverity(severity string) bool {
	switch severity {
	case "low", "medium", "high", "critical":
		return true
	default:
		return false
	}
}

func uniqueUint(values []uint) []uint {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[uint]struct{}, len(values))
	result := make([]uint, 0, len(values))
	for _, v := range values {
		if v == 0 {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		result = append(result, v)
	}
	return result
}

func isNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}

func parseAllowedOrigins(raw string) []string {
	parts := strings.Split(raw, ",")
	origins := make([]string, 0, len(parts))
	for _, p := range parts {
		item := strings.TrimSpace(p)
		if item == "" {
			continue
		}
		origins = append(origins, item)
	}
	if len(origins) == 0 {
		return append([]string(nil), defaultAllowedOrigins...)
	}
	return origins
}

func (a *API) isAllowedOrigin(origin string) bool {
	if origin == "" {
		return false
	}
	for _, allowed := range a.allowedOrigins {
		if allowed == "*" || strings.EqualFold(allowed, origin) {
			return true
		}
	}
	return false
}

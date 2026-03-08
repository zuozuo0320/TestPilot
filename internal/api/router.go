package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
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

const defaultUserAvatar = "https://api.dicebear.com/7.x/initials/svg?seed=TestPilot"

var phonePattern = regexp.MustCompile(`^1\d{10}$`)

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
	Name       string `json:"name"`
	Email      string `json:"email"`
	Phone      string `json:"phone"`
	Avatar     string `json:"avatar"`
	Role       string `json:"role"`
	RoleIDs    []uint `json:"role_ids"`
	ProjectIDs []uint `json:"project_ids"`
}

type updateUserRequest struct {
	Name       *string `json:"name"`
	Email      *string `json:"email"`
	Phone      *string `json:"phone"`
	Avatar     *string `json:"avatar"`
	Active     *bool   `json:"active"`
	RoleIDs    []uint  `json:"role_ids"`
	ProjectIDs []uint  `json:"project_ids"`
}

type createRoleRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type updateRoleRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

type updateProfileRequest struct {
	Name   *string `json:"name"`
	Email  *string `json:"email"`
	Phone  *string `json:"phone"`
	Avatar *string `json:"avatar"`
}

type assignUserRolesRequest struct {
	RoleIDs []uint `json:"role_ids"`
}

type assignUserProjectsRequest struct {
	ProjectIDs []uint `json:"project_ids"`
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
	Title        string `json:"title"`
	Level        string `json:"level"`
	ReviewResult string `json:"review_result"`
	ExecResult   string `json:"exec_result"`
	ModulePath   string `json:"module_path"`
	Tags         string `json:"tags"`
	Steps        string `json:"steps"`
	Priority     string `json:"priority"`
}

type updateTestCaseRequest struct {
	Title        *string `json:"title"`
	Level        *string `json:"level"`
	ReviewResult *string `json:"review_result"`
	ExecResult   *string `json:"exec_result"`
	ModulePath   *string `json:"module_path"`
	Tags         *string `json:"tags"`
	Steps        *string `json:"steps"`
	Priority     *string `json:"priority"`
}

type testCaseListItem struct {
	ID            uint      `json:"id"`
	ProjectID     uint      `json:"project_id"`
	Title         string    `json:"title"`
	Level         string    `json:"level"`
	ReviewResult  string    `json:"review_result"`
	ExecResult    string    `json:"exec_result"`
	ModulePath    string    `json:"module_path"`
	Tags          string    `json:"tags"`
	Steps         string    `json:"steps"`
	Priority      string    `json:"priority"`
	CreatedBy     uint      `json:"created_by"`
	CreatedByName string    `json:"created_by_name"`
	UpdatedBy     uint      `json:"updated_by"`
	UpdatedByName string    `json:"updated_by_name"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
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
	Email    string `json:"email"`
	Password string `json:"password"`
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
	router.Static("/uploads", "./uploads")

	router.POST("/api/v1/auth/login", api.login)

	v1 := router.Group("/api/v1")
	v1.Use(api.authMiddleware())
	{
		v1.GET("/users", api.listUsers)
		v1.POST("/users", api.createUser)
		v1.PUT("/users/:userID", api.updateUser)
		v1.DELETE("/users/:userID", api.deleteUser)
		v1.POST("/users/:userID/roles", api.assignUserRoles)
		v1.POST("/users/:userID/projects", api.assignUserProjects)
		v1.PUT("/users/me/profile", api.updateProfile)
		v1.POST("/users/me/avatar", api.uploadMyAvatar)

		v1.GET("/roles", api.listRoles)
		v1.POST("/roles", api.createRole)
		v1.PUT("/roles/:roleID", api.updateRole)
		v1.DELETE("/roles/:roleID", api.deleteRole)

		v1.POST("/projects", api.createProject)
		v1.GET("/projects", api.listProjects)
		v1.POST("/projects/:projectID/members", api.addProjectMember)
		v1.GET("/projects/:projectID/members", api.listProjectMembers)

		v1.POST("/projects/:projectID/requirements", api.createRequirement)
		v1.GET("/projects/:projectID/requirements", api.listRequirements)

		v1.POST("/projects/:projectID/testcases", api.createTestCase)
		v1.GET("/projects/:projectID/testcases", api.listTestCases)
		v1.PUT("/projects/:projectID/testcases/:testcaseID", api.updateTestCase)
		v1.DELETE("/projects/:projectID/testcases/:testcaseID", api.deleteTestCase)

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
		if err := a.db.Unscoped().First(&user, uint(userID64)).Error; err != nil {
			respondError(c, http.StatusUnauthorized, "user not found")
			c.Abort()
			return
		}
		if user.DeletedAt.Valid {
			respondError(c, http.StatusUnauthorized, "user deleted")
			c.Abort()
			return
		}
		if !user.Active {
			respondError(c, http.StatusForbidden, "user is frozen")
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
	password := strings.TrimSpace(req.Password)
	if email == "" || password == "" {
		respondError(c, http.StatusBadRequest, "email/password is required")
		return
	}

	var user model.User
	if err := a.db.Where("email = ?", email).First(&user).Error; err != nil {
		respondError(c, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if user.DeletedAt.Valid {
		respondError(c, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if !user.Active {
		respondError(c, http.StatusForbidden, "user is frozen")
		return
	}

	// Demo password strategy: same fixed password for seeded accounts.
	if password != "TestPilot@2026" {
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
	actor := currentUser(c)
	if !a.requireRole(c, actor, model.GlobalRoleAdmin) {
		return
	}

	var req createUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	req.Phone = strings.TrimSpace(req.Phone)
	req.Avatar = strings.TrimSpace(req.Avatar)
	req.Role = strings.ToLower(strings.TrimSpace(req.Role))
	req.RoleIDs = uniqueUint(req.RoleIDs)
	req.ProjectIDs = uniqueUint(req.ProjectIDs)

	if req.Name == "" || req.Email == "" {
		respondError(c, http.StatusBadRequest, "name/email is required")
		return
	}
	if !isValidPersonName(req.Name) {
		respondError(c, http.StatusBadRequest, "name is invalid")
		return
	}
	if !isValidEmail(req.Email) {
		respondError(c, http.StatusBadRequest, "email is invalid")
		return
	}
	if req.Phone != "" && !isValidPhone(req.Phone) {
		respondError(c, http.StatusBadRequest, "phone is invalid")
		return
	}
	if len(req.RoleIDs) == 0 {
		respondError(c, http.StatusBadRequest, "role_ids is required")
		return
	}
	if len(req.ProjectIDs) == 0 {
		respondError(c, http.StatusBadRequest, "project_ids is required")
		return
	}

	if exists, err := a.emailExists(req.Email, 0); err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	} else if exists {
		respondError(c, http.StatusConflict, "email already exists")
		return
	}
	if req.Phone != "" {
		if exists, err := a.phoneExists(req.Phone, 0); err != nil {
			respondError(c, http.StatusInternalServerError, err.Error())
			return
		} else if exists {
			respondError(c, http.StatusConflict, "phone already exists")
			return
		}
	}

	roles, err := a.fetchRoles(req.RoleIDs)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	if containsRoleName(roles, model.GlobalRoleAdmin) {
		respondError(c, http.StatusBadRequest, "admin role cannot be assigned when creating user")
		return
	}
	if err := a.ensureProjectsExist(req.ProjectIDs); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	globalRole := req.Role
	if !model.IsValidGlobalRole(globalRole) {
		globalRole = strings.ToLower(strings.TrimSpace(roles[0].Name))
		if !model.IsValidGlobalRole(globalRole) {
			globalRole = model.GlobalRoleTester
		}
	}

	entity := model.User{
		Name:   req.Name,
		Email:  req.Email,
		Phone:  req.Phone,
		Avatar: defaultUserAvatar,
		Role:   globalRole,
		Active: true,
	}

	err = a.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&entity).Error; err != nil {
			return err
		}
		if err := a.replaceUserRolesTx(tx, entity.ID, req.RoleIDs); err != nil {
			return err
		}
		if err := a.replaceUserProjectsTx(tx, entity.ID, req.ProjectIDs); err != nil {
			return err
		}
		if err := a.syncProjectMembersTx(tx, entity.ID, req.ProjectIDs); err != nil {
			return err
		}
		return a.writeAuditLogTx(tx, actor.ID, "user.create", "user", entity.ID, nil, entity)
	})
	if err != nil {
		if isDuplicateError(err) {
			respondError(c, http.StatusConflict, "user already exists")
			return
		}
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}

	c.JSON(http.StatusCreated, entity)
}

func (a *API) updateUser(c *gin.Context) {
	actor := currentUser(c)
	if !a.requireRole(c, actor, model.GlobalRoleAdmin) {
		return
	}
	userID, ok := parseUintParam(c, "userID")
	if !ok {
		return
	}

	var target model.User
	if err := a.db.Unscoped().First(&target, userID).Error; err != nil {
		respondError(c, http.StatusNotFound, "user not found")
		return
	}
	if target.DeletedAt.Valid {
		respondError(c, http.StatusBadRequest, "cannot update deleted user")
		return
	}

	var req updateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	updates := map[string]any{}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if !isValidPersonName(name) {
			respondError(c, http.StatusBadRequest, "name is invalid")
			return
		}
		updates["name"] = name
	}
	if req.Email != nil {
		email := strings.ToLower(strings.TrimSpace(*req.Email))
		if !isValidEmail(email) {
			respondError(c, http.StatusBadRequest, "email is invalid")
			return
		}
		if exists, err := a.emailExists(email, target.ID); err != nil {
			respondError(c, http.StatusInternalServerError, err.Error())
			return
		} else if exists {
			respondError(c, http.StatusConflict, "email already exists")
			return
		}
		updates["email"] = email
	}
	if req.Phone != nil {
		phone := strings.TrimSpace(*req.Phone)
		if phone != "" {
			if !isValidPhone(phone) {
				respondError(c, http.StatusBadRequest, "phone is invalid")
				return
			}
			if exists, err := a.phoneExists(phone, target.ID); err != nil {
				respondError(c, http.StatusInternalServerError, err.Error())
				return
			} else if exists {
				respondError(c, http.StatusConflict, "phone already exists")
				return
			}
		}
		updates["phone"] = phone
	}
	if req.Avatar != nil {
		updates["avatar"] = strings.TrimSpace(*req.Avatar)
	}
	if req.Active != nil {
		updates["active"] = *req.Active
	}

	roleIDs := uniqueUint(req.RoleIDs)
	projectIDs := uniqueUint(req.ProjectIDs)
	if req.RoleIDs != nil {
		if len(roleIDs) == 0 {
			respondError(c, http.StatusBadRequest, "role_ids is required")
			return
		}
		if _, err := a.fetchRoles(roleIDs); err != nil {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
	}
	if req.ProjectIDs != nil {
		if len(projectIDs) == 0 {
			respondError(c, http.StatusBadRequest, "project_ids is required")
			return
		}
		if err := a.ensureProjectsExist(projectIDs); err != nil {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
	}

	before := target
	err := a.db.Transaction(func(tx *gorm.DB) error {
		if len(updates) > 0 {
			if err := tx.Model(&model.User{}).Where("id = ?", target.ID).Updates(updates).Error; err != nil {
				return err
			}
		}
		if req.RoleIDs != nil {
			if err := a.replaceUserRolesTx(tx, target.ID, roleIDs); err != nil {
				return err
			}
		}
		if req.ProjectIDs != nil {
			if err := a.replaceUserProjectsTx(tx, target.ID, projectIDs); err != nil {
				return err
			}
			if err := a.syncProjectMembersTx(tx, target.ID, projectIDs); err != nil {
				return err
			}
		}
		var after model.User
		if err := tx.First(&after, target.ID).Error; err != nil {
			return err
		}
		return a.writeAuditLogTx(tx, actor.ID, "user.update", "user", target.ID, before, after)
	})
	if err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}

	var updated model.User
	_ = a.db.First(&updated, target.ID).Error
	c.JSON(http.StatusOK, updated)
}

func (a *API) deleteUser(c *gin.Context) {
	actor := currentUser(c)
	if !a.requireRole(c, actor, model.GlobalRoleAdmin) {
		return
	}
	userID, ok := parseUintParam(c, "userID")
	if !ok {
		return
	}

	var target model.User
	if err := a.db.First(&target, userID).Error; err != nil {
		respondError(c, http.StatusNotFound, "user not found")
		return
	}
	before := target

	err := a.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&target).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", target.ID).Delete(&model.UserRole{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", target.ID).Delete(&model.UserProject{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", target.ID).Delete(&model.ProjectMember{}).Error; err != nil {
			return err
		}
		return a.writeAuditLogTx(tx, actor.ID, "user.delete", "user", target.ID, before, gin.H{"deleted_at": time.Now()})
	})
	if err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "user deleted"})
}

func (a *API) assignUserRoles(c *gin.Context) {
	actor := currentUser(c)
	if !a.requireRole(c, actor, model.GlobalRoleAdmin) {
		return
	}
	userID, ok := parseUintParam(c, "userID")
	if !ok {
		return
	}
	var req assignUserRolesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	roleIDs := uniqueUint(req.RoleIDs)
	if len(roleIDs) == 0 {
		respondError(c, http.StatusBadRequest, "role_ids is required")
		return
	}
	if _, err := a.fetchRoles(roleIDs); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	err := a.db.Transaction(func(tx *gorm.DB) error {
		if err := a.replaceUserRolesTx(tx, userID, roleIDs); err != nil {
			return err
		}
		return a.writeAuditLogTx(tx, actor.ID, "user.assign_roles", "user", userID, nil, gin.H{"role_ids": roleIDs})
	})
	if err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "roles assigned"})
}

func (a *API) assignUserProjects(c *gin.Context) {
	actor := currentUser(c)
	if !a.requireRole(c, actor, model.GlobalRoleAdmin) {
		return
	}
	userID, ok := parseUintParam(c, "userID")
	if !ok {
		return
	}
	var req assignUserProjectsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	projectIDs := uniqueUint(req.ProjectIDs)
	if len(projectIDs) == 0 {
		respondError(c, http.StatusBadRequest, "project_ids is required")
		return
	}
	if err := a.ensureProjectsExist(projectIDs); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	err := a.db.Transaction(func(tx *gorm.DB) error {
		if err := a.replaceUserProjectsTx(tx, userID, projectIDs); err != nil {
			return err
		}
		if err := a.syncProjectMembersTx(tx, userID, projectIDs); err != nil {
			return err
		}
		return a.writeAuditLogTx(tx, actor.ID, "user.assign_projects", "user", userID, nil, gin.H{"project_ids": projectIDs})
	})
	if err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "projects assigned"})
}

func (a *API) updateProfile(c *gin.Context) {
	actor := currentUser(c)
	var req updateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	updates := map[string]any{}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if !isValidPersonName(name) {
			respondError(c, http.StatusBadRequest, "name is invalid")
			return
		}
		updates["name"] = name
	}
	if req.Email != nil {
		respondError(c, http.StatusBadRequest, "email cannot be modified")
		return
	}
	if req.Phone != nil {
		phone := strings.TrimSpace(*req.Phone)
		if phone != "" {
			if !isValidPhone(phone) {
				respondError(c, http.StatusBadRequest, "phone is invalid")
				return
			}
			if exists, err := a.phoneExists(phone, actor.ID); err != nil {
				respondError(c, http.StatusInternalServerError, err.Error())
				return
			} else if exists {
				respondError(c, http.StatusConflict, "phone already exists")
				return
			}
		}
		updates["phone"] = phone
	}
	if req.Avatar != nil {
		updates["avatar"] = strings.TrimSpace(*req.Avatar)
	}
	if len(updates) == 0 {
		respondError(c, http.StatusBadRequest, "no valid fields to update")
		return
	}

	before := actor
	err := a.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.User{}).Where("id = ?", actor.ID).Updates(updates).Error; err != nil {
			return err
		}
		var after model.User
		if err := tx.First(&after, actor.ID).Error; err != nil {
			return err
		}
		return a.writeAuditLogTx(tx, actor.ID, "profile.update", "user", actor.ID, before, after)
	})
	if err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}

	var updated model.User
	_ = a.db.First(&updated, actor.ID).Error
	c.JSON(http.StatusOK, updated)
}

func (a *API) uploadMyAvatar(c *gin.Context) {
	actor := currentUser(c)
	file, err := c.FormFile("avatar")
	if err != nil {
		respondError(c, http.StatusBadRequest, "avatar file is required")
		return
	}
	ext := strings.ToLower(filepath.Ext(file.Filename))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".webp":
	default:
		respondError(c, http.StatusBadRequest, "avatar type only supports jpg/jpeg/png/webp")
		return
	}
	if file.Size > 2*1024*1024 {
		respondError(c, http.StatusBadRequest, "avatar size cannot exceed 2MB")
		return
	}

	dir := filepath.Join("uploads", "avatars")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}
	filename := fmt.Sprintf("u%d_%d%s", actor.ID, time.Now().UnixNano(), ext)
	savePath := filepath.Join(dir, filename)
	if err := c.SaveUploadedFile(file, savePath); err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}
	avatarURL := "/uploads/avatars/" + filename

	before := actor
	if err := a.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.User{}).Where("id = ?", actor.ID).Update("avatar", avatarURL).Error; err != nil {
			return err
		}
		var after model.User
		if err := tx.First(&after, actor.ID).Error; err != nil {
			return err
		}
		return a.writeAuditLogTx(tx, actor.ID, "profile.avatar_upload", "user", actor.ID, before, after)
	}); err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{"avatar": avatarURL})
}

func (a *API) listRoles(c *gin.Context) {
	user := currentUser(c)
	if !a.requireRole(c, user, model.GlobalRoleAdmin, model.GlobalRoleManager) {
		return
	}
	var roles []model.Role
	if err := a.db.Order("id asc").Find(&roles).Error; err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"roles": roles})
}

func (a *API) createRole(c *gin.Context) {
	actor := currentUser(c)
	if !a.requireRole(c, actor, model.GlobalRoleAdmin) {
		return
	}
	var req createRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		respondError(c, http.StatusBadRequest, "name is required")
		return
	}
	entity := model.Role{Name: name, Description: strings.TrimSpace(req.Description)}
	err := a.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&entity).Error; err != nil {
			return err
		}
		return a.writeAuditLogTx(tx, actor.ID, "role.create", "role", entity.ID, nil, entity)
	})
	if err != nil {
		if isDuplicateError(err) {
			respondError(c, http.StatusConflict, "role already exists")
			return
		}
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusCreated, entity)
}

func (a *API) updateRole(c *gin.Context) {
	actor := currentUser(c)
	if !a.requireRole(c, actor, model.GlobalRoleAdmin) {
		return
	}
	roleID, ok := parseUintParam(c, "roleID")
	if !ok {
		return
	}
	var req updateRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	updates := map[string]any{}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			respondError(c, http.StatusBadRequest, "name is invalid")
			return
		}
		updates["name"] = name
	}
	if req.Description != nil {
		updates["description"] = strings.TrimSpace(*req.Description)
	}
	if len(updates) == 0 {
		respondError(c, http.StatusBadRequest, "no valid fields to update")
		return
	}
	var before model.Role
	if err := a.db.First(&before, roleID).Error; err != nil {
		respondError(c, http.StatusNotFound, "role not found")
		return
	}
	if err := a.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.Role{}).Where("id = ?", roleID).Updates(updates).Error; err != nil {
			return err
		}
		var after model.Role
		if err := tx.First(&after, roleID).Error; err != nil {
			return err
		}
		return a.writeAuditLogTx(tx, actor.ID, "role.update", "role", roleID, before, after)
	}); err != nil {
		if isDuplicateError(err) {
			respondError(c, http.StatusConflict, "role already exists")
			return
		}
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}
	var updated model.Role
	_ = a.db.First(&updated, roleID).Error
	c.JSON(http.StatusOK, updated)
}

func (a *API) deleteRole(c *gin.Context) {
	actor := currentUser(c)
	if !a.requireRole(c, actor, model.GlobalRoleAdmin) {
		return
	}
	roleID, ok := parseUintParam(c, "roleID")
	if !ok {
		return
	}
	var role model.Role
	if err := a.db.First(&role, roleID).Error; err != nil {
		respondError(c, http.StatusNotFound, "role not found")
		return
	}
	if model.IsPresetSystemRole(role.Name) {
		respondError(c, http.StatusConflict, "preset system role cannot be deleted")
		return
	}
	var used int64
	if err := a.db.Model(&model.UserRole{}).Where("role_id = ?", roleID).Count(&used).Error; err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}
	if used > 0 {
		respondError(c, http.StatusConflict, "role is in use")
		return
	}
	if err := a.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&role).Error; err != nil {
			return err
		}
		return a.writeAuditLogTx(tx, actor.ID, "role.delete", "role", roleID, role, gin.H{"deleted_at": time.Now()})
	}); err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "role deleted"})
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
	if !a.requireRole(c, user, model.GlobalRoleAdmin, model.GlobalRoleManager, model.GlobalRoleTester) {
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
	level := strings.ToUpper(strings.TrimSpace(req.Level))
	if level == "" {
		level = "P1"
	}
	reviewResult := strings.TrimSpace(req.ReviewResult)
	if reviewResult == "" {
		reviewResult = "未评审"
	}
	execResult := strings.TrimSpace(req.ExecResult)
	if execResult == "" {
		execResult = "未执行"
	}
	modulePath := strings.TrimSpace(req.ModulePath)
	if modulePath == "" {
		modulePath = "/未分类"
	}
	tags := strings.TrimSpace(req.Tags)

	entity := model.TestCase{
		ProjectID:    projectID,
		Title:        strings.TrimSpace(req.Title),
		Level:        level,
		ReviewResult: reviewResult,
		ExecResult:   execResult,
		ModulePath:   modulePath,
		Tags:         tags,
		Steps:        strings.TrimSpace(req.Steps),
		Priority:     priority,
		CreatedBy:    user.ID,
		UpdatedBy:    user.ID,
	}
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

	page := parsePositiveIntWithDefault(c.Query("page"), 1)
	pageSize := parsePositiveIntWithDefault(c.Query("pageSize"), 20)
	if pageSize > 100 {
		pageSize = 100
	}
	keyword := strings.TrimSpace(c.Query("keyword"))
	levelFilter := strings.ToUpper(strings.TrimSpace(c.Query("level")))
	reviewFilter := strings.TrimSpace(c.Query("review_result"))
	execFilter := strings.TrimSpace(c.Query("exec_result"))
	sortBy := strings.TrimSpace(c.Query("sortBy"))
	sortOrder := strings.ToLower(strings.TrimSpace(c.Query("sortOrder")))
	if sortOrder != "asc" {
		sortOrder = "desc"
	}

	baseQuery := a.db.Model(&model.TestCase{}).Where("project_id = ?", projectID)
	if keyword != "" {
		like := "%" + keyword + "%"
		if idKeyword, err := strconv.Atoi(keyword); err == nil && idKeyword > 0 {
			baseQuery = baseQuery.Where("id = ? OR title LIKE ? OR tags LIKE ?", idKeyword, like, like)
		} else {
			baseQuery = baseQuery.Where("title LIKE ? OR tags LIKE ?", like, like)
		}
	}
	if levelFilter != "" {
		baseQuery = baseQuery.Where("level = ?", levelFilter)
	}
	if reviewFilter != "" {
		baseQuery = baseQuery.Where("review_result = ?", reviewFilter)
	}
	if execFilter != "" {
		baseQuery = baseQuery.Where("exec_result = ?", execFilter)
	}

	var total int64
	if err := baseQuery.Count(&total).Error; err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}

	orderColumn := "test_cases.updated_at"
	switch sortBy {
	case "id":
		orderColumn = "test_cases.id"
	case "created_at":
		orderColumn = "test_cases.created_at"
	case "updated_at", "":
		orderColumn = "test_cases.updated_at"
	default:
		orderColumn = "test_cases.updated_at"
	}

	var items []testCaseListItem
	offset := (page - 1) * pageSize
	err := baseQuery.
		Select("test_cases.id, test_cases.project_id, test_cases.title, test_cases.level, test_cases.review_result, test_cases.exec_result, test_cases.module_path, test_cases.tags, test_cases.steps, test_cases.priority, test_cases.created_by, test_cases.updated_by, test_cases.created_at, test_cases.updated_at, cu.name AS created_by_name, uu.name AS updated_by_name").
		Joins("LEFT JOIN users cu ON cu.id = test_cases.created_by").
		Joins("LEFT JOIN users uu ON uu.id = test_cases.updated_by").
		Order(orderColumn + " " + sortOrder).
		Offset(offset).
		Limit(pageSize).
		Scan(&items).Error
	if err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}

	for i := range items {
		if strings.TrimSpace(items[i].CreatedByName) == "" {
			items[i].CreatedByName = "-"
		}
		if strings.TrimSpace(items[i].UpdatedByName) == "" {
			items[i].UpdatedByName = "-"
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"items":    items,
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
	})
}

func (a *API) updateTestCase(c *gin.Context) {
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

	var entity model.TestCase
	if err := a.db.Where("id = ? AND project_id = ?", testCaseID, projectID).First(&entity).Error; err != nil {
		respondError(c, http.StatusNotFound, "testcase not found")
		return
	}

	var req updateTestCaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	updates := map[string]any{}
	if req.Title != nil {
		title := strings.TrimSpace(*req.Title)
		if title == "" {
			respondError(c, http.StatusBadRequest, "title is required")
			return
		}
		updates["title"] = title
	}
	if req.Level != nil {
		level := strings.ToUpper(strings.TrimSpace(*req.Level))
		if level == "" {
			level = "P1"
		}
		updates["level"] = level
	}
	if req.ReviewResult != nil {
		reviewResult := strings.TrimSpace(*req.ReviewResult)
		if reviewResult == "" {
			reviewResult = "未评审"
		}
		updates["review_result"] = reviewResult
	}
	if req.ExecResult != nil {
		execResult := strings.TrimSpace(*req.ExecResult)
		if execResult == "" {
			execResult = "未执行"
		}
		updates["exec_result"] = execResult
	}
	if req.ModulePath != nil {
		modulePath := strings.TrimSpace(*req.ModulePath)
		if modulePath == "" {
			modulePath = "/未分类"
		}
		updates["module_path"] = modulePath
	}
	if req.Tags != nil {
		updates["tags"] = strings.TrimSpace(*req.Tags)
	}
	if req.Steps != nil {
		updates["steps"] = strings.TrimSpace(*req.Steps)
	}
	if req.Priority != nil {
		priority := strings.ToLower(strings.TrimSpace(*req.Priority))
		if priority == "" {
			priority = "medium"
		}
		updates["priority"] = priority
	}
	if len(updates) == 0 {
		respondError(c, http.StatusBadRequest, "no fields to update")
		return
	}

	if err := a.db.Model(&entity).Updates(updates).Error; err != nil {
		if isDuplicateError(err) {
			respondError(c, http.StatusConflict, "testcase already exists")
			return
		}
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}

	if err := a.db.First(&entity, entity.ID).Error; err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}

	c.JSON(http.StatusOK, entity)
}

func (a *API) deleteTestCase(c *gin.Context) {
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

	result := a.db.Where("id = ? AND project_id = ?", testCaseID, projectID).Delete(&model.TestCase{})
	if result.Error != nil {
		respondError(c, http.StatusInternalServerError, result.Error.Error())
		return
	}
	if result.RowsAffected == 0 {
		respondError(c, http.StatusNotFound, "testcase not found")
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "testcase deleted"})
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

func (a *API) writeAuditLogTx(tx *gorm.DB, actorID uint, action, targetType string, targetID uint, before any, after any) error {
	beforeJSON, err := marshalJSON(before)
	if err != nil {
		return err
	}
	afterJSON, err := marshalJSON(after)
	if err != nil {
		return err
	}
	log := model.AuditLog{
		ActorID:    actorID,
		Action:     action,
		TargetType: targetType,
		TargetID:   targetID,
		BeforeData: beforeJSON,
		AfterData:  afterJSON,
	}
	return tx.Create(&log).Error
}

func marshalJSON(v any) (string, error) {
	if v == nil {
		return "", nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (a *API) ensureProjectsExist(projectIDs []uint) error {
	if len(projectIDs) == 0 {
		return errors.New("project_ids is required")
	}
	var count int64
	if err := a.db.Model(&model.Project{}).Where("id IN ?", projectIDs).Count(&count).Error; err != nil {
		return err
	}
	if int(count) != len(projectIDs) {
		return errors.New("project_ids contains invalid id")
	}
	return nil
}

func (a *API) fetchRoles(roleIDs []uint) ([]model.Role, error) {
	if len(roleIDs) == 0 {
		return nil, errors.New("role_ids is required")
	}
	var roles []model.Role
	if err := a.db.Where("id IN ?", roleIDs).Find(&roles).Error; err != nil {
		return nil, err
	}
	if len(roles) != len(roleIDs) {
		return nil, errors.New("role_ids contains invalid id")
	}
	return roles, nil
}

func (a *API) replaceUserRolesTx(tx *gorm.DB, userID uint, roleIDs []uint) error {
	if len(roleIDs) == 0 {
		return errors.New("role_ids is required")
	}
	if err := tx.Where("user_id = ?", userID).Delete(&model.UserRole{}).Error; err != nil {
		return err
	}
	items := make([]model.UserRole, 0, len(roleIDs))
	for _, roleID := range roleIDs {
		items = append(items, model.UserRole{UserID: userID, RoleID: roleID})
	}
	return tx.Create(&items).Error
}

func (a *API) replaceUserProjectsTx(tx *gorm.DB, userID uint, projectIDs []uint) error {
	if len(projectIDs) == 0 {
		return errors.New("project_ids is required")
	}
	if err := tx.Where("user_id = ?", userID).Delete(&model.UserProject{}).Error; err != nil {
		return err
	}
	items := make([]model.UserProject, 0, len(projectIDs))
	for _, projectID := range projectIDs {
		items = append(items, model.UserProject{UserID: userID, ProjectID: projectID})
	}
	return tx.Create(&items).Error
}

func (a *API) syncProjectMembersTx(tx *gorm.DB, userID uint, projectIDs []uint) error {
	if len(projectIDs) == 0 {
		return nil
	}
	if err := tx.Where("user_id = ?", userID).Where("project_id NOT IN ?", projectIDs).Delete(&model.ProjectMember{}).Error; err != nil {
		return err
	}
	for _, projectID := range projectIDs {
		member := model.ProjectMember{ProjectID: projectID, UserID: userID, Role: model.MemberRoleMember}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "project_id"}, {Name: "user_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"updated_at"}),
		}).Create(&member).Error; err != nil {
			return err
		}
	}
	return nil
}

func (a *API) emailExists(email string, excludeUserID uint) (bool, error) {
	if strings.TrimSpace(email) == "" {
		return false, nil
	}
	query := a.db.Unscoped().Model(&model.User{}).
		Where("email = ?", email).
		Where("deleted_at IS NULL")
	if excludeUserID > 0 {
		query = query.Where("id <> ?", excludeUserID)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (a *API) phoneExists(phone string, excludeUserID uint) (bool, error) {
	if strings.TrimSpace(phone) == "" {
		return false, nil
	}
	query := a.db.Unscoped().Model(&model.User{}).
		Where("phone = ?", phone).
		Where("deleted_at IS NULL")
	if excludeUserID > 0 {
		query = query.Where("id <> ?", excludeUserID)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
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

func parsePositiveIntWithDefault(raw string, defaultValue int) int {
	if strings.TrimSpace(raw) == "" {
		return defaultValue
	}
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || v <= 0 {
		return defaultValue
	}
	return v
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

func containsRoleName(roles []model.Role, roleName string) bool {
	for _, item := range roles {
		if strings.EqualFold(strings.TrimSpace(item.Name), strings.TrimSpace(roleName)) {
			return true
		}
	}
	return false
}

func isValidPersonName(name string) bool {
	name = strings.TrimSpace(name)
	if len(name) < 2 || len(name) > 40 {
		return false
	}
	for _, r := range name {
		if r == ' ' || r == '·' || r == '-' || r == '_' {
			continue
		}
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= 0x4e00 && r <= 0x9fa5) {
			continue
		}
		return false
	}
	return true
}

func isValidEmail(email string) bool {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" || len(email) > 120 {
		return false
	}
	if strings.Count(email, "@") != 1 {
		return false
	}
	parts := strings.Split(email, "@")
	local, domain := parts[0], parts[1]
	if len(local) < 1 || len(domain) < 3 || !strings.Contains(domain, ".") {
		return false
	}
	if strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return false
	}
	return true
}

func isValidPhone(phone string) bool {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return false
	}
	return phonePattern.MatchString(phone)
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

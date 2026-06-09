// handler_gitlab_integration.go — GitLab Issue 集成 HTTP Handler
package api

import (
	"github.com/gin-gonic/gin"

	"testpilot/internal/dto/response"
	"testpilot/internal/model"
	"testpilot/internal/service"
)

// saveGitLabConfigRequest 保存 GitLab 集成配置请求。
type saveGitLabConfigRequest struct {
	BaseURL     string `json:"base_url" binding:"required,min=8,max=500"`
	ProjectPath string `json:"project_path" binding:"required,min=1,max=500"`
	Token       string `json:"token" binding:"max=500"`
	Enabled     *bool  `json:"enabled"`
}

// importGitLabIssueRequest 导入 GitLab Issue 请求。
type importGitLabIssueRequest struct {
	IssueURL        string `json:"issue_url" binding:"required,url,max=1000"`
	IncludeComments bool   `json:"include_comments"`
	AnalyzeImages   bool   `json:"analyze_images"`
}

// getGitLabConfig 查询项目 GitLab 集成配置
// @Summary 查询 GitLab 集成配置
// @Description 查询项目级 GitLab 集成配置，Token 只返回脱敏状态。
// @Tags 需求智生-GitLab
// @Produce json
// @Param projectID path int true "项目ID"
// @Success 200 {object} response.Result
// @Failure 403 {object} response.Result
// @Router /api/v1/projects/{projectID}/integrations/gitlab [get]
func (a *API) getGitLabConfig(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}

	config, err := a.gitLabIntegrationSvc.GetConfig(c.Request.Context(), projectID)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, config)
}

// saveGitLabConfig 保存项目 GitLab 集成配置
// @Summary 保存 GitLab 集成配置
// @Description 保存或替换项目级 GitLab 地址、项目路径与 Token，Token 不会回显；已配置 Token 时可留空保留原 Token。
// @Tags 需求智生-GitLab
// @Accept json
// @Produce json
// @Param projectID path int true "项目ID"
// @Param body body saveGitLabConfigRequest true "请求体"
// @Success 200 {object} response.Result
// @Failure 400,403 {object} response.Result
// @Router /api/v1/projects/{projectID}/integrations/gitlab [put]
func (a *API) saveGitLabConfig(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	if !requireRole(c, user, model.GlobalRoleAdmin, model.GlobalRoleManager) {
		return
	}

	var req saveGitLabConfigRequest
	if !bindJSON(c, &req) {
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	config, err := a.gitLabIntegrationSvc.SaveConfig(c.Request.Context(), service.SaveGitLabConfigInput{
		ProjectID:   projectID,
		BaseURL:     req.BaseURL,
		ProjectPath: req.ProjectPath,
		Token:       req.Token,
		Enabled:     enabled,
		ActorID:     user.ID,
	})
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, config)
}

// testGitLabConfig 测试项目 GitLab 集成配置
// @Summary 测试 GitLab 集成配置
// @Description 使用已保存 Token 调用 GitLab 用户接口，确认配置可用。
// @Tags 需求智生-GitLab
// @Produce json
// @Param projectID path int true "项目ID"
// @Success 200 {object} response.Result
// @Failure 401,403,412,503 {object} response.Result
// @Router /api/v1/projects/{projectID}/integrations/gitlab/test [post]
func (a *API) testGitLabConfig(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	if !requireRole(c, user, model.GlobalRoleAdmin, model.GlobalRoleManager) {
		return
	}

	if err := a.gitLabIntegrationSvc.TestConfig(c.Request.Context(), projectID); err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, gin.H{"ok": true})
}

// importGitLabIssue 导入 GitLab Issue 为需求文档
// @Summary 导入 GitLab Issue
// @Description 拉取 GitLab Issue 标题、描述和可选评论，生成已解析的需求文档。
// @Tags 需求智生-GitLab
// @Accept json
// @Produce json
// @Param projectID path int true "项目ID"
// @Param body body importGitLabIssueRequest true "请求体"
// @Success 201 {object} response.Result
// @Failure 400,401,404,412,503 {object} response.Result
// @Router /api/v1/projects/{projectID}/requirement-docs/gitlab-issues/import [post]
func (a *API) importGitLabIssue(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}

	var req importGitLabIssueRequest
	if !bindJSON(c, &req) {
		return
	}
	doc, err := a.gitLabIntegrationSvc.ImportIssue(c.Request.Context(), service.ImportGitLabIssueInput{
		ProjectID:       projectID,
		IssueURL:        req.IssueURL,
		IncludeComments: req.IncludeComments,
		AnalyzeImages:   req.AnalyzeImages,
		ActorID:         user.ID,
	})
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.Created(c, doc)
}

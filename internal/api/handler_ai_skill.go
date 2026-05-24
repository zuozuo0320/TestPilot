// handler_ai_skill.go — 需求智生-Skill 模板 HTTP Handler
//
// 外部 API（前端调用）：
//   GET    /projects/:projectID/ai-skills          项目可用 Skill 列表
//   GET    /projects/:projectID/ai-skills/:skillID  Skill 详情
//   POST   /projects/:projectID/ai-skills          创建项目 Skill
//   PUT    /projects/:projectID/ai-skills/:skillID  编辑 Skill
//   DELETE /projects/:projectID/ai-skills/:skillID  删除 Skill
//   POST   /projects/:projectID/ai-skills/:skillID/toggle 启用/禁用
package api

import (
	"github.com/gin-gonic/gin"

	"testpilot/internal/dto/response"
	"testpilot/internal/service"
)

// ========== 请求结构体 ==========

// createSkillRequest 创建 Skill
type createSkillRequest struct {
	SkillKey       string `json:"skill_key" binding:"required,min=2,max=50"`
	Name           string `json:"name" binding:"required,min=1,max=100"`
	Scope          string `json:"scope" binding:"required,oneof=functional api compat security custom"`
	Description    string `json:"description" binding:"max=500"`
	PromptTemplate string `json:"prompt_template" binding:"required,min=10"`
	OutputSchema   string `json:"output_schema" binding:"omitempty,max=50"`
}

// updateSkillRequest 编辑 Skill
type updateSkillRequest struct {
	Name           string `json:"name" binding:"omitempty,min=1,max=100"`
	Scope          string `json:"scope" binding:"omitempty,oneof=functional api compat security custom"`
	Description    string `json:"description" binding:"omitempty,max=500"`
	PromptTemplate string `json:"prompt_template" binding:"omitempty,min=10"`
	OutputSchema   string `json:"output_schema" binding:"omitempty,max=50"`
	LockVersion    int    `json:"lock_version" binding:"required,min=0"`
}

// toggleSkillRequest 启用/禁用
type toggleSkillRequest struct {
	IsActive bool `json:"is_active"`
}

// ========== Handler 方法 ==========

// listAISkills 项目可用 Skill 列表
// @Summary 项目可用 Skill 列表
// @Tags 需求智生-Skill
// @Produce json
// @Param projectID path int true "项目ID"
// @Success 200 {object} response.Result
// @Router /api/v1/projects/{projectID}/ai-skills [get]
func (a *API) listAISkills(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}

	skills, err := a.aiSkillSvc.ListForProject(c.Request.Context(), projectID)
	if err != nil {
		response.HandleError(c, err)
		return
	}

	response.OK(c, skills)
}

// getAISkill Skill 详情
// @Summary Skill 详情
// @Tags 需求智生-Skill
// @Produce json
// @Param projectID path int true "项目ID"
// @Param skillID path int true "Skill ID"
// @Success 200 {object} response.Result
// @Router /api/v1/projects/{projectID}/ai-skills/{skillID} [get]
func (a *API) getAISkill(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	skillID, ok := parseUintParam(c, "skillID")
	if !ok {
		return
	}

	skill, err := a.aiSkillSvc.GetByID(c.Request.Context(), projectID, skillID)
	if err != nil {
		response.HandleError(c, err)
		return
	}

	response.OK(c, skill)
}

// createAISkill 创建项目级 Skill
// @Summary 创建 Skill
// @Tags 需求智生-Skill
// @Accept json
// @Produce json
// @Param projectID path int true "项目ID"
// @Param body body createSkillRequest true "请求体"
// @Success 201 {object} response.Result
// @Failure 400,409 {object} response.Result
// @Router /api/v1/projects/{projectID}/ai-skills [post]
func (a *API) createAISkill(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}

	var req createSkillRequest
	if !bindJSON(c, &req) {
		return
	}

	skill, err := a.aiSkillSvc.Create(c.Request.Context(), service.CreateSkillInput{
		ProjectID:      projectID,
		SkillKey:       req.SkillKey,
		Name:           req.Name,
		Scope:          req.Scope,
		Description:    req.Description,
		PromptTemplate: req.PromptTemplate,
		OutputSchema:   req.OutputSchema,
		CreatedBy:      user.ID,
	})
	if err != nil {
		response.HandleError(c, err)
		return
	}

	response.Created(c, skill)
}

// updateAISkill 编辑 Skill（CAS）
// @Summary 编辑 Skill
// @Tags 需求智生-Skill
// @Accept json
// @Produce json
// @Param projectID path int true "项目ID"
// @Param skillID path int true "Skill ID"
// @Param body body updateSkillRequest true "请求体"
// @Success 200 {object} response.Result
// @Failure 409 {object} response.Result
// @Router /api/v1/projects/{projectID}/ai-skills/{skillID} [put]
func (a *API) updateAISkill(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	skillID, ok := parseUintParam(c, "skillID")
	if !ok {
		return
	}
	_ = user // 鉴权已通过

	var req updateSkillRequest
	if !bindJSON(c, &req) {
		return
	}

	skill, err := a.aiSkillSvc.Update(c.Request.Context(), service.UpdateSkillInput{
		ID:             skillID,
		ProjectID:      projectID,
		Name:           req.Name,
		Scope:          req.Scope,
		Description:    req.Description,
		PromptTemplate: req.PromptTemplate,
		OutputSchema:   req.OutputSchema,
		LockVersion:    req.LockVersion,
	})
	if err != nil {
		response.HandleError(c, err)
		return
	}

	response.OK(c, skill)
}

// deleteAISkill 删除 Skill
// @Summary 删除 Skill
// @Tags 需求智生-Skill
// @Produce json
// @Param projectID path int true "项目ID"
// @Param skillID path int true "Skill ID"
// @Success 200 {object} response.Result
// @Failure 403 {object} response.Result
// @Router /api/v1/projects/{projectID}/ai-skills/{skillID} [delete]
func (a *API) deleteAISkill(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	skillID, ok := parseUintParam(c, "skillID")
	if !ok {
		return
	}
	_ = user

	if err := a.aiSkillSvc.Delete(c.Request.Context(), projectID, skillID); err != nil {
		response.HandleError(c, err)
		return
	}

	response.OK(c, nil)
}

// toggleAISkill 启用/禁用 Skill
// @Summary 启用/禁用 Skill
// @Tags 需求智生-Skill
// @Accept json
// @Produce json
// @Param projectID path int true "项目ID"
// @Param skillID path int true "Skill ID"
// @Param body body toggleSkillRequest true "请求体"
// @Success 200 {object} response.Result
// @Router /api/v1/projects/{projectID}/ai-skills/{skillID}/toggle [post]
func (a *API) toggleAISkill(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	skillID, ok := parseUintParam(c, "skillID")
	if !ok {
		return
	}
	_ = user

	var req toggleSkillRequest
	if !bindJSON(c, &req) {
		return
	}

	if err := a.aiSkillSvc.ToggleActive(c.Request.Context(), projectID, skillID, req.IsActive); err != nil {
		response.HandleError(c, err)
		return
	}

	response.OK(c, nil)
}

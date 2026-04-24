// handler_case_review_v02.go — v0.2 新增 Handler：规则引擎 rerun、Action Items CRUD、项目 settings
//
// 本文件不替换 v0.1 的 handler_case_review.go；两者共同构成 API 层。
package api

import (
	"github.com/gin-gonic/gin"

	"testpilot/internal/dto/response"
	"testpilot/internal/model"
	"testpilot/internal/repository"
	"testpilot/internal/service"
)

// ═══════ 请求结构体 ═══════

// resolveDefectRequest Author 标记为已解决
type resolveDefectRequest struct {
	Note string `json:"note" binding:"max=1000"`
}

// disputeDefectRequest Author 对缺陷提异议
type disputeDefectRequest struct {
	Reason string `json:"reason" binding:"required,max=1000"`
}

// updateProjectSettingsRequest 更新项目 settings
type updateProjectSettingsRequest struct {
	// AllowSelfReview 是否允许 Author 评审自己的用例（默认 false）
	AllowSelfReview *bool `json:"allow_self_review"`
}

// ═══════ 规则引擎 ═══════

// rerunAIGate 手动触发 Layer 1 规则引擎重跑
// @Summary 规则引擎 rerun
// @Description 对单个评审项重新运行 Layer 1 规则引擎，刷新 AI 门禁状态并重建 ai_gate 类 Action Items
// @Tags CaseReview
// @Router /projects/{projectID}/case-reviews/{reviewID}/items/{itemID}/ai-gate/rerun [post]
func (a *API) rerunAIGate(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	reviewID, ok := parseUintParam(c, "reviewID")
	if !ok {
		return
	}
	itemID, ok := parseUintParam(c, "itemID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	if a.caseReviewRuleSvc == nil {
		response.Error(c, 500, 500001, "规则引擎服务未初始化")
		return
	}
	report, err := a.caseReviewRuleSvc.RunOnItem(c.Request.Context(), projectID, reviewID, itemID, user.ID)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, report)
}

// runPlanAIGate 对整个评审计划批量执行 Layer 1 规则引擎
// @Summary 计划级 AI 评审
// @Description 对评审计划内所有评审项批量运行规则引擎，返回聚合报告（含每条用例的门禁状态与 finding 数）
// @Tags CaseReview
// @Router /projects/{projectID}/case-reviews/{reviewID}/ai-gate/run-all [post]
func (a *API) runPlanAIGate(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	reviewID, ok := parseUintParam(c, "reviewID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	if a.caseReviewRuleSvc == nil {
		response.Error(c, 500, 500001, "规则引擎服务未初始化")
		return
	}
	report, err := a.caseReviewRuleSvc.RunOnReview(c.Request.Context(), projectID, reviewID, user.ID)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, report)
}

// ═══════ Action Items / Defects ═══════

// listReviewDefects 列出某评审计划下的 Action Items
// @Router /projects/{projectID}/case-reviews/{reviewID}/defects [get]
func (a *API) listReviewDefects(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	reviewID, ok := parseUintParam(c, "reviewID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	if a.caseReviewDefectSvc == nil {
		response.Error(c, 500, 500002, "缺陷服务未初始化")
		return
	}
	filter := repository.CaseReviewDefectFilter{
		ProjectID: projectID,
		ReviewID:  reviewID,
		Source:    c.Query("source"),
		Status:    c.Query("status"),
		Severity:  c.Query("severity"),
		Page:      parsePositiveIntWithDefault(c.Query("page"), 1),
		PageSize:  parsePositiveIntWithDefault(c.Query("pageSize"), 20),
	}
	items, total, err := a.caseReviewDefectSvc.List(c.Request.Context(), filter)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.Page(c, items, total, filter.Page, filter.PageSize)
}

// listItemDefects 列出单个评审项下的 Action Items（按创建时间升序）
// @Router /projects/{projectID}/case-reviews/{reviewID}/items/{itemID}/defects [get]
func (a *API) listItemDefects(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	itemID, ok := parseUintParam(c, "itemID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	if a.caseReviewDefectSvc == nil {
		response.Error(c, 500, 500002, "缺陷服务未初始化")
		return
	}
	items, err := a.caseReviewDefectSvc.ListByItemID(c.Request.Context(), itemID)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, gin.H{"items": items})
}

// getDefect 查询单条 Action Item
// @Router /projects/{projectID}/case-review-defects/{defectID} [get]
func (a *API) getDefect(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	defectID, ok := parseUintParam(c, "defectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	if a.caseReviewDefectSvc == nil {
		response.Error(c, 500, 500002, "缺陷服务未初始化")
		return
	}
	defect, err := a.caseReviewDefectSvc.Get(c.Request.Context(), projectID, defectID)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, defect)
}

// resolveDefect Author 将 Action Item 标记为已解决
// @Router /projects/{projectID}/case-review-defects/{defectID}/resolve [post]
func (a *API) resolveDefect(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	defectID, ok := parseUintParam(c, "defectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	var req resolveDefectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, 400001, "请求参数非法: "+err.Error())
		return
	}
	if a.caseReviewDefectSvc == nil {
		response.Error(c, 500, 500002, "缺陷服务未初始化")
		return
	}
	err := a.caseReviewDefectSvc.Resolve(c.Request.Context(), projectID, defectID, user.ID, service.ResolveDefectInput{Note: req.Note})
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, gin.H{"status": model.DefectStatusResolved})
}

// disputeDefect Author 对 Action Item 提异议
// @Router /projects/{projectID}/case-review-defects/{defectID}/dispute [post]
func (a *API) disputeDefect(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	defectID, ok := parseUintParam(c, "defectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	var req disputeDefectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, 400001, "请求参数非法: "+err.Error())
		return
	}
	if a.caseReviewDefectSvc == nil {
		response.Error(c, 500, 500002, "缺陷服务未初始化")
		return
	}
	err := a.caseReviewDefectSvc.Dispute(c.Request.Context(), projectID, defectID, user.ID, service.DisputeDefectInput{Reason: req.Reason})
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, gin.H{"status": model.DefectStatusDisputed})
}

// reopenDefect Moderator 重开 Action Item
// @Router /projects/{projectID}/case-review-defects/{defectID}/reopen [post]
func (a *API) reopenDefect(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	defectID, ok := parseUintParam(c, "defectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	if a.caseReviewDefectSvc == nil {
		response.Error(c, 500, 500002, "缺陷服务未初始化")
		return
	}
	err := a.caseReviewDefectSvc.Reopen(c.Request.Context(), projectID, defectID, user.ID)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, gin.H{"status": model.DefectStatusOpen})
}

// ═══════ 项目 settings ═══════

// getProjectSettings 查询项目级 settings
// @Router /projects/{projectID}/settings [get]
func (a *API) getProjectSettings(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	settings, err := a.projectSvc.GetSettings(c.Request.Context(), projectID)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, settings)
}

// updateProjectSettings 更新项目级 settings
// @Router /projects/{projectID}/settings [put]
func (a *API) updateProjectSettings(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	var req updateProjectSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, 400001, "请求参数非法: "+err.Error())
		return
	}
	input := service.UpdateProjectSettingsInput{AllowSelfReview: req.AllowSelfReview}
	settings, err := a.projectSvc.UpdateSettings(c.Request.Context(), projectID, user.ID, input)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, settings)
}

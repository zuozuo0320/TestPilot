// handler_ai_regression.go — 阶段三（18.3）：定时回归、AI 修复建议确认与编排指标看板接口
package api

import (
	"github.com/gin-gonic/gin"

	"testpilot/internal/dto/response"
	"testpilot/internal/model"
	"testpilot/internal/repository"
	"testpilot/internal/service"
)

type regressionPlanRequest struct {
	ProjectID       uint   `json:"project_id" binding:"required"`
	CompositionID   uint   `json:"composition_id"`
	Name            string `json:"name"`
	IntervalMinutes int    `json:"interval_minutes"`
	Enabled         *bool  `json:"enabled"`
}

type regressionProjectRequest struct {
	ProjectID uint `json:"project_id" binding:"required"`
}

type planAdoptionRequest struct {
	ProjectID          uint   `json:"project_id" binding:"required"`
	PlanID             string `json:"plan_id" binding:"required"`
	CompositionID      uint   `json:"composition_id" binding:"required"`
	AdoptedSteps       int    `json:"adopted_steps"`
	ModifiedSteps      int    `json:"modified_steps"`
	ManualConfirmSteps int    `json:"manual_confirm_steps"`
}

// createRegressionPlan 创建定时回归计划。
// @Summary 创建定时回归计划
// @Tags AIRegression
// @Accept json
// @Produce json
// @Param body body regressionPlanRequest true "回归计划参数"
// @Success 201 {object} response.Response{data=model.AIRegressionPlan}
// @Router /ai-script/regression/plans [post]
func (a *API) createRegressionPlan(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleManager, model.GlobalRoleTester, model.GlobalRoleDeveloper) {
		return
	}
	var req regressionPlanRequest
	if !bindJSON(c, &req) {
		return
	}
	if !a.requireProjectAccess(c, user, req.ProjectID) {
		return
	}
	plan, err := a.aiRegressionSvc.CreatePlan(c.Request.Context(), user.ID, service.RegressionPlanInput{
		ProjectID:       req.ProjectID,
		CompositionID:   req.CompositionID,
		Name:            req.Name,
		IntervalMinutes: req.IntervalMinutes,
		Enabled:         req.Enabled,
	})
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.Created(c, plan)
}

// listRegressionPlans 查询项目回归计划列表。
// @Summary 查询项目回归计划列表
// @Tags AIRegression
// @Produce json
// @Param project_id query int true "项目 ID"
// @Success 200 {object} response.Response{data=[]model.AIRegressionPlan}
// @Router /ai-script/regression/plans [get]
func (a *API) listRegressionPlans(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseProjectIDQuery(c)
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	plans, err := a.aiRegressionSvc.ListPlans(c.Request.Context(), projectID)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, plans)
}

// updateRegressionPlan 更新回归计划。
// @Summary 更新回归计划
// @Tags AIRegression
// @Accept json
// @Produce json
// @Param planID path int true "回归计划 ID"
// @Param body body regressionPlanRequest true "回归计划参数"
// @Success 200 {object} response.Response{data=model.AIRegressionPlan}
// @Router /ai-script/regression/plans/{planID} [put]
func (a *API) updateRegressionPlan(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleManager, model.GlobalRoleTester, model.GlobalRoleDeveloper) {
		return
	}
	planID, ok := parseUintParam(c, "planID")
	if !ok {
		return
	}
	var req regressionPlanRequest
	if !bindJSON(c, &req) {
		return
	}
	if !a.requireProjectAccess(c, user, req.ProjectID) {
		return
	}
	plan, err := a.aiRegressionSvc.UpdatePlan(c.Request.Context(), user.ID, planID, service.RegressionPlanInput{
		ProjectID:       req.ProjectID,
		CompositionID:   req.CompositionID,
		Name:            req.Name,
		IntervalMinutes: req.IntervalMinutes,
		Enabled:         req.Enabled,
	})
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, plan)
}

// deleteRegressionPlan 删除回归计划。
// @Summary 删除回归计划
// @Tags AIRegression
// @Produce json
// @Param planID path int true "回归计划 ID"
// @Param project_id query int true "项目 ID"
// @Success 200 {object} response.Response
// @Router /ai-script/regression/plans/{planID} [delete]
func (a *API) deleteRegressionPlan(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleManager, model.GlobalRoleTester, model.GlobalRoleDeveloper) {
		return
	}
	planID, ok := parseUintParam(c, "planID")
	if !ok {
		return
	}
	projectID, ok := parseProjectIDQuery(c)
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	if err := a.aiRegressionSvc.DeletePlan(c.Request.Context(), projectID, planID); err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, nil)
}

// triggerRegressionPlan 手动触发一次回归执行。
// @Summary 手动触发一次回归执行
// @Tags AIRegression
// @Accept json
// @Produce json
// @Param planID path int true "回归计划 ID"
// @Param body body regressionProjectRequest true "项目参数"
// @Success 200 {object} response.Response{data=model.AIRegressionExecution}
// @Router /ai-script/regression/plans/{planID}/trigger [post]
func (a *API) triggerRegressionPlan(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleManager, model.GlobalRoleTester, model.GlobalRoleDeveloper) {
		return
	}
	planID, ok := parseUintParam(c, "planID")
	if !ok {
		return
	}
	var req regressionProjectRequest
	if !bindJSON(c, &req) {
		return
	}
	if !a.requireProjectAccess(c, user, req.ProjectID) {
		return
	}
	execution, err := a.aiRegressionSvc.TriggerNow(c.Request.Context(), user.ID, req.ProjectID, planID)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, execution)
}

// listRegressionExecutions 分页查询回归执行记录。
// @Summary 分页查询回归执行记录
// @Tags AIRegression
// @Produce json
// @Param project_id query int true "项目 ID"
// @Param composition_id query int false "编排 ID"
// @Param status query string false "执行状态"
// @Param page query int false "页码"
// @Param pageSize query int false "每页数量"
// @Success 200 {object} response.Response{data=[]model.AIRegressionExecution}
// @Router /ai-script/regression/executions [get]
func (a *API) listRegressionExecutions(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseProjectIDQuery(c)
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	page := parsePositiveIntWithDefault(c.Query("page"), 1)
	pageSize := parsePositiveIntWithDefault(c.Query("pageSize"), 20)
	executions, total, err := a.aiRegressionSvc.ListExecutions(c.Request.Context(), repository.AIRegressionExecutionFilter{
		ProjectID:     projectID,
		CompositionID: uint(parsePositiveIntWithDefault(c.Query("composition_id"), 0)),
		Status:        c.Query("status"),
		Page:          page,
		PageSize:      pageSize,
	})
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.Page(c, executions, total, page, pageSize)
}

// listRepairSuggestions 分页查询 AI 修复建议。
// @Summary 分页查询 AI 修复建议
// @Tags AIRegression
// @Produce json
// @Param project_id query int true "项目 ID"
// @Param composition_id query int false "编排 ID"
// @Param status query string false "建议状态"
// @Param page query int false "页码"
// @Param pageSize query int false "每页数量"
// @Success 200 {object} response.Response{data=[]model.AIRepairSuggestion}
// @Router /ai-script/repair-suggestions [get]
func (a *API) listRepairSuggestions(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseProjectIDQuery(c)
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	page := parsePositiveIntWithDefault(c.Query("page"), 1)
	pageSize := parsePositiveIntWithDefault(c.Query("pageSize"), 20)
	suggestions, total, err := a.aiRegressionSvc.ListSuggestions(c.Request.Context(), repository.AIRepairSuggestionFilter{
		ProjectID:     projectID,
		CompositionID: uint(parsePositiveIntWithDefault(c.Query("composition_id"), 0)),
		Status:        c.Query("status"),
		Page:          page,
		PageSize:      pageSize,
	})
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.Page(c, suggestions, total, page, pageSize)
}

// getRepairSuggestion 查询修复建议详情。
// @Summary 查询修复建议详情
// @Tags AIRegression
// @Produce json
// @Param suggestionID path int true "修复建议 ID"
// @Param project_id query int true "项目 ID"
// @Success 200 {object} response.Response{data=model.AIRepairSuggestion}
// @Router /ai-script/repair-suggestions/{suggestionID} [get]
func (a *API) getRepairSuggestion(c *gin.Context) {
	user := currentUser(c)
	suggestionID, ok := parseUintParam(c, "suggestionID")
	if !ok {
		return
	}
	projectID, ok := parseProjectIDQuery(c)
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	suggestion, err := a.aiRegressionSvc.GetSuggestion(c.Request.Context(), projectID, suggestionID)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, suggestion)
}

// applyRepairSuggestion 人工确认应用修复建议（走手工补丁通道）。
// @Summary 人工确认应用修复建议
// @Tags AIRegression
// @Accept json
// @Produce json
// @Param suggestionID path int true "修复建议 ID"
// @Param body body regressionProjectRequest true "项目参数"
// @Success 200 {object} response.Response{data=model.AIScenarioComposition}
// @Router /ai-script/repair-suggestions/{suggestionID}/apply [post]
func (a *API) applyRepairSuggestion(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleManager, model.GlobalRoleTester, model.GlobalRoleDeveloper) {
		return
	}
	suggestionID, ok := parseUintParam(c, "suggestionID")
	if !ok {
		return
	}
	var req regressionProjectRequest
	if !bindJSON(c, &req) {
		return
	}
	if !a.requireProjectAccess(c, user, req.ProjectID) {
		return
	}
	composition, err := a.aiRegressionSvc.ApplySuggestion(c.Request.Context(), user.ID, req.ProjectID, suggestionID)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, composition)
}

// rejectRepairSuggestion 人工拒绝修复建议。
// @Summary 人工拒绝修复建议
// @Tags AIRegression
// @Accept json
// @Produce json
// @Param suggestionID path int true "修复建议 ID"
// @Param body body regressionProjectRequest true "项目参数"
// @Success 200 {object} response.Response{data=model.AIRepairSuggestion}
// @Router /ai-script/repair-suggestions/{suggestionID}/reject [post]
func (a *API) rejectRepairSuggestion(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleManager, model.GlobalRoleTester, model.GlobalRoleDeveloper) {
		return
	}
	suggestionID, ok := parseUintParam(c, "suggestionID")
	if !ok {
		return
	}
	var req regressionProjectRequest
	if !bindJSON(c, &req) {
		return
	}
	if !a.requireProjectAccess(c, user, req.ProjectID) {
		return
	}
	suggestion, err := a.aiRegressionSvc.RejectSuggestion(c.Request.Context(), user.ID, req.ProjectID, suggestionID)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, suggestion)
}

// recordPlanAdoption 上报「由计划生成编排」的采纳统计。
// @Summary 上报计划采纳统计
// @Tags AIRegression
// @Accept json
// @Produce json
// @Param body body planAdoptionRequest true "采纳统计参数"
// @Success 200 {object} response.Response{data=model.AIPlanRecord}
// @Router /ai-script/plan-records/adoption [post]
func (a *API) recordPlanAdoption(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleManager, model.GlobalRoleTester, model.GlobalRoleDeveloper) {
		return
	}
	var req planAdoptionRequest
	if !bindJSON(c, &req) {
		return
	}
	if !a.requireProjectAccess(c, user, req.ProjectID) {
		return
	}
	record, err := a.aiRegressionSvc.RecordPlanAdoption(c.Request.Context(), service.PlanAdoptionInput{
		ProjectID:          req.ProjectID,
		PlanID:             req.PlanID,
		CompositionID:      req.CompositionID,
		AdoptedSteps:       req.AdoptedSteps,
		ModifiedSteps:      req.ModifiedSteps,
		ManualConfirmSteps: req.ManualConfirmSteps,
	})
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, record)
}

// getOrchestrationMetrics 查询 18.3 编排指标看板数据。
// @Summary 查询编排指标看板数据
// @Tags AIRegression
// @Produce json
// @Param project_id query int true "项目 ID"
// @Param days query int false "统计天数（默认全部）"
// @Success 200 {object} response.Response{data=service.AIOrchestrationMetrics}
// @Router /metrics/orchestration [get]
func (a *API) getOrchestrationMetrics(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseProjectIDQuery(c)
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	days := parsePositiveIntWithDefault(c.Query("days"), 0)
	metrics, err := a.aiRegressionSvc.Metrics(c.Request.Context(), projectID, days)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, metrics)
}

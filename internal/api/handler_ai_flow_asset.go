// handler_ai_flow_asset.go — 测试智编固定场景资产 HTTP Handler
package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"testpilot/internal/dto/response"
	"testpilot/internal/model"
	"testpilot/internal/service"
)

type publishFlowAssetRequest struct {
	ProjectID      uint            `json:"project_id" binding:"required,min=1"`
	FlowKey        string          `json:"flow_key" binding:"required,min=1,max=128"`
	FlowName       string          `json:"flow_name" binding:"required,min=1,max=128"`
	Description    string          `json:"description" binding:"max=2000"`
	Tags           []string        `json:"tags"`
	InputSchema    json.RawMessage `json:"input_schema"`
	OutputSchema   json.RawMessage `json:"output_schema"`
	Preconditions  []string        `json:"preconditions" binding:"required,min=1"`
	Postconditions []string        `json:"postconditions" binding:"required,min=1"`
	AllowAIReuse   *bool           `json:"allow_ai_reuse"`
	ChangeSummary  string          `json:"change_summary" binding:"max=500"`
}

type saveFlowAssetRequest struct {
	ProjectID      uint            `json:"project_id" binding:"required,min=1"`
	FlowKey        string          `json:"flow_key" binding:"omitempty,max=128"`
	FlowName       string          `json:"flow_name" binding:"required,min=1,max=128"`
	Description    string          `json:"description" binding:"max=2000"`
	Tags           []string        `json:"tags"`
	InputSchema    json.RawMessage `json:"input_schema"`
	OutputSchema   json.RawMessage `json:"output_schema"`
	Preconditions  []string        `json:"preconditions" binding:"required,min=1"`
	Postconditions []string        `json:"postconditions" binding:"required,min=1"`
	DSL            json.RawMessage `json:"dsl"`
	CodeSnapshot   string          `json:"code_snapshot"`
	AllowAIReuse   *bool           `json:"allow_ai_reuse"`
	ChangeSummary  string          `json:"change_summary" binding:"max=500"`
}

type flowAssetActionRequest struct {
	ProjectID     uint   `json:"project_id" binding:"required,min=1"`
	ChangeSummary string `json:"change_summary" binding:"max=500"`
}

// listAIFlowAssets 获取固定场景列表。
// @Summary 获取固定场景列表
// @Description 分页查询指定项目下的固定场景资产，支持关键词、资产状态和最近验证状态筛选
// @Tags AIScriptFlow
// @Produce json
// @Param project_id query int true "项目 ID"
// @Param keyword query string false "关键词：场景名称、Key 或描述"
// @Param status query string false "资产状态：DRAFT / PUBLISHED / ARCHIVED"
// @Param validation_status query string false "最近验证状态：NOT_VALIDATED / VALIDATING / PASSED / FAILED / ERROR"
// @Param page query int false "页码，默认 1"
// @Param pageSize query int false "每页条数，默认 20，最大 100"
// @Success 200 {object} response.Response{data=[]model.AIFlowAsset}
// @Failure 400 {object} response.Response
// @Failure 403 {object} response.Response
// @Router /ai-script/flows [get]
func (a *API) listAIFlowAssets(c *gin.Context) {
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
	flows, total, err := a.aiFlowAssetSvc.List(c.Request.Context(), service.FlowAssetListInput{
		ProjectID:        projectID,
		Keyword:          c.Query("keyword"),
		Status:           c.Query("status"),
		ValidationStatus: c.Query("validation_status"),
		Page:             page,
		PageSize:         pageSize,
	})
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.Page(c, flows, total, page, pageSize)
}

// createAIFlowAsset 手动创建固定场景草稿。
// @Summary 创建固定场景
// @Description 手动创建可治理的固定业务场景草稿，发布后才能被编排引用
// @Tags AIScriptFlow
// @Accept json
// @Produce json
// @Param body body saveFlowAssetRequest true "创建参数"
// @Success 201 {object} response.Response{data=model.AIFlowAsset}
// @Failure 400 {object} response.Response
// @Failure 403 {object} response.Response
// @Failure 409 {object} response.Response
// @Router /ai-script/flows [post]
func (a *API) createAIFlowAsset(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleManager, model.GlobalRoleTester, model.GlobalRoleDeveloper) {
		return
	}
	var req saveFlowAssetRequest
	if !bindJSON(c, &req) {
		return
	}
	if !a.requireProjectAccess(c, user, req.ProjectID) {
		return
	}
	allowAIReuse := true
	if req.AllowAIReuse != nil {
		allowAIReuse = *req.AllowAIReuse
	}
	flow, err := a.aiFlowAssetSvc.Create(c.Request.Context(), user.ID, service.SaveFlowAssetInput{
		ProjectID:      req.ProjectID,
		FlowKey:        req.FlowKey,
		FlowName:       req.FlowName,
		Description:    req.Description,
		Tags:           req.Tags,
		InputSchema:    req.InputSchema,
		OutputSchema:   req.OutputSchema,
		Preconditions:  req.Preconditions,
		Postconditions: req.Postconditions,
		DSL:            req.DSL,
		CodeSnapshot:   req.CodeSnapshot,
		AllowAIReuse:   allowAIReuse,
		ChangeSummary:  req.ChangeSummary,
	})
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.Created(c, flow)
}

// getAIFlowAsset 获取固定场景详情。
// @Summary 获取固定场景详情
// @Description 查询固定场景详情，并按 project_id 校验项目归属
// @Tags AIScriptFlow
// @Produce json
// @Param flowID path int true "固定场景 ID"
// @Param project_id query int true "项目 ID"
// @Success 200 {object} response.Response{data=model.AIFlowAsset}
// @Failure 400 {object} response.Response
// @Failure 403 {object} response.Response
// @Failure 404 {object} response.Response
// @Router /ai-script/flows/{flowID} [get]
func (a *API) getAIFlowAsset(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseProjectIDQuery(c)
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	flowID, ok := parseUintParam(c, "flowID")
	if !ok {
		return
	}

	flow, err := a.aiFlowAssetSvc.Get(c.Request.Context(), projectID, flowID)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, flow)
}

// updateAIFlowAsset 更新固定场景资产。
// @Summary 更新固定场景
// @Description 更新固定场景的契约、DSL、代码快照和 AI 复用策略
// @Tags AIScriptFlow
// @Accept json
// @Produce json
// @Param flowID path int true "固定场景 ID"
// @Param body body saveFlowAssetRequest true "更新参数"
// @Success 200 {object} response.Response{data=model.AIFlowAsset}
// @Failure 400 {object} response.Response
// @Failure 403 {object} response.Response
// @Failure 404 {object} response.Response
// @Router /ai-script/flows/{flowID} [put]
func (a *API) updateAIFlowAsset(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleManager, model.GlobalRoleTester, model.GlobalRoleDeveloper) {
		return
	}
	flowID, ok := parseUintParam(c, "flowID")
	if !ok {
		return
	}
	var req saveFlowAssetRequest
	if !bindJSON(c, &req) {
		return
	}
	if !a.requireProjectAccess(c, user, req.ProjectID) {
		return
	}
	allowAIReuse := true
	if req.AllowAIReuse != nil {
		allowAIReuse = *req.AllowAIReuse
	}
	flow, err := a.aiFlowAssetSvc.Update(c.Request.Context(), user.ID, flowID, service.SaveFlowAssetInput{
		ProjectID:      req.ProjectID,
		FlowName:       req.FlowName,
		Description:    req.Description,
		Tags:           req.Tags,
		InputSchema:    req.InputSchema,
		OutputSchema:   req.OutputSchema,
		Preconditions:  req.Preconditions,
		Postconditions: req.Postconditions,
		DSL:            req.DSL,
		CodeSnapshot:   req.CodeSnapshot,
		AllowAIReuse:   allowAIReuse,
		ChangeSummary:  req.ChangeSummary,
	})
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, flow)
}

// listAIFlowAssetVersions 获取固定场景版本列表。
// @Summary 获取固定场景版本列表
// @Description 查询固定场景的不可变版本列表，并按 project_id 校验项目归属
// @Tags AIScriptFlow
// @Produce json
// @Param flowID path int true "固定场景 ID"
// @Param project_id query int true "项目 ID"
// @Success 200 {object} response.Response{data=[]model.AIFlowAssetVersion}
// @Failure 400 {object} response.Response
// @Failure 403 {object} response.Response
// @Failure 404 {object} response.Response
// @Router /ai-script/flows/{flowID}/versions [get]
func (a *API) listAIFlowAssetVersions(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseProjectIDQuery(c)
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	flowID, ok := parseUintParam(c, "flowID")
	if !ok {
		return
	}

	versions, err := a.aiFlowAssetSvc.ListVersions(c.Request.Context(), projectID, flowID)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, versions)
}

// publishAIFlowAsset 发布固定场景资产。
// @Summary 发布固定场景
// @Description 将固定场景草稿或编辑后的资产发布为可引用版本
// @Tags AIScriptFlow
// @Accept json
// @Produce json
// @Param flowID path int true "固定场景 ID"
// @Param body body flowAssetActionRequest true "发布参数"
// @Success 201 {object} response.Response{data=service.PublishFlowAssetResult}
// @Failure 400 {object} response.Response
// @Failure 403 {object} response.Response
// @Failure 404 {object} response.Response
// @Failure 409 {object} response.Response
// @Router /ai-script/flows/{flowID}/publish [post]
func (a *API) publishAIFlowAsset(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleManager, model.GlobalRoleTester, model.GlobalRoleDeveloper) {
		return
	}
	flowID, ok := parseUintParam(c, "flowID")
	if !ok {
		return
	}
	var req flowAssetActionRequest
	if !bindJSON(c, &req) {
		return
	}
	if !a.requireProjectAccess(c, user, req.ProjectID) {
		return
	}
	result, err := a.aiFlowAssetSvc.Publish(c.Request.Context(), user.ID, req.ProjectID, flowID, req.ChangeSummary)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.Created(c, result)
}

// archiveAIFlowAsset 归档固定场景资产。
// @Summary 归档固定场景
// @Description 归档后不能新增引用，历史编排仍保留锁定版本
// @Tags AIScriptFlow
// @Accept json
// @Produce json
// @Param flowID path int true "固定场景 ID"
// @Param body body flowAssetActionRequest true "归档参数"
// @Success 200 {object} response.Response{data=model.AIFlowAsset}
// @Failure 403 {object} response.Response
// @Failure 404 {object} response.Response
// @Router /ai-script/flows/{flowID}/archive [post]
func (a *API) archiveAIFlowAsset(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleManager) {
		return
	}
	flowID, ok := parseUintParam(c, "flowID")
	if !ok {
		return
	}
	var req flowAssetActionRequest
	if !bindJSON(c, &req) {
		return
	}
	if !a.requireProjectAccess(c, user, req.ProjectID) {
		return
	}
	flow, err := a.aiFlowAssetSvc.Archive(c.Request.Context(), user.ID, req.ProjectID, flowID)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, flow)
}

// deleteAIFlowAsset 删除未发布且未被引用的固定场景草稿。
// @Summary 删除固定场景草稿
// @Description 仅允许删除草稿且未被任何编排或固定场景引用的固定场景；已发布资产请归档
// @Tags AIScriptFlow
// @Produce json
// @Param flowID path int true "固定场景 ID"
// @Param project_id query int true "项目 ID"
// @Success 200 {object} response.Response
// @Failure 403 {object} response.Response
// @Failure 404 {object} response.Response
// @Failure 409 {object} response.Response
// @Router /ai-script/flows/{flowID} [delete]
func (a *API) deleteAIFlowAsset(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleManager, model.GlobalRoleTester, model.GlobalRoleDeveloper) {
		return
	}
	projectID, ok := parseProjectIDQuery(c)
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	flowID, ok := parseUintParam(c, "flowID")
	if !ok {
		return
	}
	if err := a.aiFlowAssetSvc.Delete(c.Request.Context(), projectID, flowID); err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, gin.H{"message": "固定场景草稿已删除"})
}

// listAIFlowReferences 查询固定场景引用关系。
// @Summary 查询固定场景引用关系
// @Description 查询直接引用该固定场景的编排或其他资产
// @Tags AIScriptFlow
// @Produce json
// @Param flowID path int true "固定场景 ID"
// @Param project_id query int true "项目 ID"
// @Success 200 {object} response.Response{data=[]model.AIAssetReference}
// @Failure 403 {object} response.Response
// @Failure 404 {object} response.Response
// @Router /ai-script/flows/{flowID}/references [get]
func (a *API) listAIFlowReferences(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseProjectIDQuery(c)
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	flowID, ok := parseUintParam(c, "flowID")
	if !ok {
		return
	}
	refs, err := a.aiFlowAssetSvc.References(c.Request.Context(), projectID, flowID)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, refs)
}

// publishAIFlowAssetFromTask 从已验证通过的录制任务发布固定场景。
// @Summary 从录制任务发布固定场景
// @Description 将已验证通过的测试智编任务发布为固定场景资产；同一项目下 flow_key 唯一，重复提交返回 409
// @Tags AIScriptFlow
// @Accept json
// @Produce json
// @Param taskID path int true "测试智编任务 ID"
// @Param body body publishFlowAssetRequest true "发布参数"
// @Success 201 {object} response.Response{data=service.PublishFlowAssetResult}
// @Failure 400 {object} response.Response
// @Failure 403 {object} response.Response
// @Failure 404 {object} response.Response
// @Failure 409 {object} response.Response
// @Router /ai-script/tasks/{taskID}/publish-flow [post]
func (a *API) publishAIFlowAssetFromTask(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleManager, model.GlobalRoleTester, model.GlobalRoleDeveloper) {
		return
	}
	taskID, ok := parseUintParam(c, "taskID")
	if !ok {
		return
	}

	var req publishFlowAssetRequest
	if !bindJSON(c, &req) {
		return
	}
	if !a.requireProjectAccess(c, user, req.ProjectID) {
		return
	}

	allowAIReuse := true
	if req.AllowAIReuse != nil {
		allowAIReuse = *req.AllowAIReuse
	}

	result, err := a.aiFlowAssetSvc.PublishFromTask(c.Request.Context(), user.ID, taskID, service.PublishFlowAssetInput{
		ProjectID:      req.ProjectID,
		FlowKey:        req.FlowKey,
		FlowName:       req.FlowName,
		Description:    req.Description,
		Tags:           req.Tags,
		InputSchema:    req.InputSchema,
		OutputSchema:   req.OutputSchema,
		Preconditions:  req.Preconditions,
		Postconditions: req.Postconditions,
		AllowAIReuse:   allowAIReuse,
		ChangeSummary:  req.ChangeSummary,
	})
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.Created(c, result)
}

func parseProjectIDQuery(c *gin.Context) (uint, bool) {
	raw := c.Query("project_id")
	if raw == "" {
		response.Error(c, http.StatusBadRequest, service.CodeParamsError, "project_id is required")
		return 0, false
	}
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || value == 0 {
		response.Error(c, http.StatusBadRequest, service.CodeParamsError, "project_id is invalid")
		return 0, false
	}
	return uint(value), true
}

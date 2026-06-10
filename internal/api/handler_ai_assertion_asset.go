// handler_ai_assertion_asset.go — 测试智编断言资产 HTTP Handler
package api

import (
	"encoding/json"

	"github.com/gin-gonic/gin"

	"testpilot/internal/dto/response"
	"testpilot/internal/model"
	"testpilot/internal/service"
)

type saveAssertionAssetRequest struct {
	ProjectID         uint            `json:"project_id" binding:"required,min=1"`
	AssertionKey      string          `json:"assertion_key" binding:"omitempty,max=128"`
	AssertionName     string          `json:"assertion_name" binding:"required,min=1,max=128"`
	AssertionType     string          `json:"assertion_type" binding:"required,max=32"`
	Description       string          `json:"description" binding:"max=2000"`
	ParamSchema       json.RawMessage `json:"param_schema"`
	Implementation    json.RawMessage `json:"implementation"`
	FailureMessageTpl string          `json:"failure_message_tpl" binding:"max=500"`
	EvidenceConfig    json.RawMessage `json:"evidence_config"`
	AllowAIReuse      *bool           `json:"allow_ai_reuse"`
}

// listAIAssertionAssets 获取断言资产列表。
// @Summary 获取断言资产列表
// @Description 分页查询指定项目下的断言资产，支持关键词、状态和断言类型筛选
// @Tags AIScriptAssertion
// @Produce json
// @Param project_id query int true "项目 ID"
// @Param keyword query string false "关键词"
// @Param status query string false "状态：DRAFT / PUBLISHED / ARCHIVED"
// @Param assertion_type query string false "断言类型"
// @Param page query int false "页码"
// @Param pageSize query int false "每页条数"
// @Success 200 {object} response.Response{data=[]model.AIAssertionAsset}
// @Router /ai-script/assertions [get]
func (a *API) listAIAssertionAssets(c *gin.Context) {
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
	assertions, total, err := a.aiAssertionAssetSvc.List(c.Request.Context(), service.AssertionAssetListInput{
		ProjectID: projectID,
		Keyword:   c.Query("keyword"),
		Status:    c.Query("status"),
		Type:      c.Query("assertion_type"),
		Page:      page,
		PageSize:  pageSize,
	})
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.Page(c, assertions, total, page, pageSize)
}

// getAIAssertionAsset 获取断言资产详情。
// @Summary 获取断言资产详情
// @Tags AIScriptAssertion
// @Produce json
// @Param assertionID path int true "断言资产 ID"
// @Param project_id query int true "项目 ID"
// @Success 200 {object} response.Response{data=model.AIAssertionAsset}
// @Router /ai-script/assertions/{assertionID} [get]
func (a *API) getAIAssertionAsset(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseProjectIDQuery(c)
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	assertionID, ok := parseUintParam(c, "assertionID")
	if !ok {
		return
	}
	assertion, err := a.aiAssertionAssetSvc.Get(c.Request.Context(), projectID, assertionID)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, assertion)
}

// createAIAssertionAsset 创建断言资产草稿。
// @Summary 创建断言资产
// @Tags AIScriptAssertion
// @Accept json
// @Produce json
// @Param body body saveAssertionAssetRequest true "创建参数"
// @Success 201 {object} response.Response{data=model.AIAssertionAsset}
// @Router /ai-script/assertions [post]
func (a *API) createAIAssertionAsset(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleManager, model.GlobalRoleTester, model.GlobalRoleDeveloper) {
		return
	}
	var req saveAssertionAssetRequest
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
	assertion, err := a.aiAssertionAssetSvc.Create(c.Request.Context(), user.ID, service.AssertionAssetSaveInput{
		ProjectID:         req.ProjectID,
		AssertionKey:      req.AssertionKey,
		AssertionName:     req.AssertionName,
		AssertionType:     req.AssertionType,
		Description:       req.Description,
		ParamSchema:       req.ParamSchema,
		Implementation:    req.Implementation,
		FailureMessageTpl: req.FailureMessageTpl,
		EvidenceConfig:    req.EvidenceConfig,
		AllowAIReuse:      allowAIReuse,
	})
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.Created(c, assertion)
}

// updateAIAssertionAsset 更新断言资产。
// @Summary 更新断言资产
// @Tags AIScriptAssertion
// @Accept json
// @Produce json
// @Param assertionID path int true "断言资产 ID"
// @Param body body saveAssertionAssetRequest true "更新参数"
// @Success 200 {object} response.Response{data=model.AIAssertionAsset}
// @Router /ai-script/assertions/{assertionID} [put]
func (a *API) updateAIAssertionAsset(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleManager, model.GlobalRoleTester, model.GlobalRoleDeveloper) {
		return
	}
	assertionID, ok := parseUintParam(c, "assertionID")
	if !ok {
		return
	}
	var req saveAssertionAssetRequest
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
	assertion, err := a.aiAssertionAssetSvc.Update(c.Request.Context(), user.ID, assertionID, service.AssertionAssetSaveInput{
		ProjectID:         req.ProjectID,
		AssertionName:     req.AssertionName,
		AssertionType:     req.AssertionType,
		Description:       req.Description,
		ParamSchema:       req.ParamSchema,
		Implementation:    req.Implementation,
		FailureMessageTpl: req.FailureMessageTpl,
		EvidenceConfig:    req.EvidenceConfig,
		AllowAIReuse:      allowAIReuse,
	})
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, assertion)
}

// publishAIAssertionAsset 发布断言资产。
// @Summary 发布断言资产
// @Tags AIScriptAssertion
// @Produce json
// @Param assertionID path int true "断言资产 ID"
// @Success 200 {object} response.Response{data=model.AIAssertionAsset}
// @Router /ai-script/assertions/{assertionID}/publish [post]
func (a *API) publishAIAssertionAsset(c *gin.Context) {
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
	assertionID, ok := parseUintParam(c, "assertionID")
	if !ok {
		return
	}
	assertion, err := a.aiAssertionAssetSvc.Publish(c.Request.Context(), user.ID, projectID, assertionID)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, assertion)
}

// archiveAIAssertionAsset 归档断言资产。
// @Summary 归档断言资产
// @Tags AIScriptAssertion
// @Produce json
// @Param assertionID path int true "断言资产 ID"
// @Success 200 {object} response.Response{data=model.AIAssertionAsset}
// @Router /ai-script/assertions/{assertionID}/archive [post]
func (a *API) archiveAIAssertionAsset(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleManager) {
		return
	}
	projectID, ok := parseProjectIDQuery(c)
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	assertionID, ok := parseUintParam(c, "assertionID")
	if !ok {
		return
	}
	assertion, err := a.aiAssertionAssetSvc.Archive(c.Request.Context(), user.ID, projectID, assertionID)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, assertion)
}

// deleteAIAssertionAsset 删除未发布且未被引用的断言草稿。
// @Summary 删除断言草稿
// @Description 仅允许删除草稿且未被编排引用的断言资产；已发布资产请归档
// @Tags AIScriptAssertion
// @Produce json
// @Param assertionID path int true "断言资产 ID"
// @Param project_id query int true "项目 ID"
// @Success 200 {object} response.Response
// @Failure 403 {object} response.Response
// @Failure 404 {object} response.Response
// @Failure 409 {object} response.Response
// @Router /ai-script/assertions/{assertionID} [delete]
func (a *API) deleteAIAssertionAsset(c *gin.Context) {
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
	assertionID, ok := parseUintParam(c, "assertionID")
	if !ok {
		return
	}
	if err := a.aiAssertionAssetSvc.Delete(c.Request.Context(), projectID, assertionID); err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, gin.H{"message": "断言草稿已删除"})
}

// listAIAssertionReferences 查询断言资产引用关系。
// @Summary 查询断言资产引用关系
// @Tags AIScriptAssertion
// @Produce json
// @Param assertionID path int true "断言资产 ID"
// @Param project_id query int true "项目 ID"
// @Success 200 {object} response.Response{data=[]model.AIAssetReference}
// @Router /ai-script/assertions/{assertionID}/references [get]
func (a *API) listAIAssertionReferences(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseProjectIDQuery(c)
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	assertionID, ok := parseUintParam(c, "assertionID")
	if !ok {
		return
	}
	refs, err := a.aiAssertionAssetSvc.References(c.Request.Context(), projectID, assertionID)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, refs)
}

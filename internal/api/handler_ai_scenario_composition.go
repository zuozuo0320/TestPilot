// handler_ai_scenario_composition.go — 测试智编场景编排 HTTP Handler
package api

import (
	"encoding/json"

	"github.com/gin-gonic/gin"

	"testpilot/internal/dto/response"
	"testpilot/internal/model"
	"testpilot/internal/service"
)

type createScenarioCompositionRequest struct {
	ProjectID    uint   `json:"project_id" binding:"required,min=1"`
	ScenarioKey  string `json:"scenario_key" binding:"required,min=1,max=128"`
	ScenarioName string `json:"scenario_name" binding:"required,min=1,max=128"`
	Description  string `json:"description" binding:"max=2000"`
}

type updateScenarioCompositionRequest struct {
	ProjectID        uint            `json:"project_id" binding:"required,min=1"`
	ScenarioName     string          `json:"scenario_name" binding:"required,min=1,max=128"`
	Description      string          `json:"description" binding:"max=2000"`
	DSL              json.RawMessage `json:"dsl"`
	ExpectedRevision int             `json:"expected_revision" binding:"omitempty,min=0"`
}

type saveScenarioStepRequest struct {
	ProjectID        uint            `json:"project_id" binding:"required,min=1"`
	StepType         string          `json:"step_type" binding:"required,max=32"`
	StepName         string          `json:"step_name" binding:"max=128"`
	RefFlowID        *uint           `json:"ref_flow_id"`
	RefFlowVersionID *uint           `json:"ref_flow_version_id"`
	RefAssertionID   *uint           `json:"ref_assertion_id"`
	ParamMapping     json.RawMessage `json:"param_mapping"`
	OutputMapping    json.RawMessage `json:"output_mapping"`
	AtomicAction     string          `json:"atomic_action" binding:"max=64"`
	CodeBlock        string          `json:"code_block"`
	ManualReviewed   bool            `json:"manual_reviewed"`
	AIConfidence     float64         `json:"ai_confidence"`
	Enabled          *bool           `json:"enabled"`
}

type reorderScenarioStepsRequest struct {
	ProjectID uint   `json:"project_id" binding:"required,min=1"`
	StepIDs   []uint `json:"step_ids" binding:"required,min=1"`
}

type generateScenarioCodeRequest struct {
	ProjectID      uint   `json:"project_id" binding:"required,min=1"`
	Force          bool   `json:"force"`
	Target         string `json:"target" binding:"omitempty,max=32"`
	ConfirmPartial bool   `json:"confirm_partial"`
}

type manualScenarioCodeRequest struct {
	ProjectID        uint   `json:"project_id" binding:"required,min=1"`
	GeneratedCode    string `json:"generated_code" binding:"required"`
	ChangeSummary    string `json:"change_summary" binding:"max=500"`
	Locked           bool   `json:"locked"`
	ExpectedRevision int    `json:"expected_revision" binding:"omitempty,min=0"`
}

type scenarioCodeLockRequest struct {
	ProjectID     uint   `json:"project_id" binding:"required,min=1"`
	Locked        bool   `json:"locked"`
	ChangeSummary string `json:"change_summary" binding:"max=500"`
}

type validateScenarioRequest struct {
	ProjectID      uint            `json:"project_id" binding:"required,min=1"`
	Environment    string          `json:"environment" binding:"max=64"`
	Variables      json.RawMessage `json:"variables"`
	IdempotencyKey string          `json:"idempotency_key" binding:"max=128"`
}

type publishScenarioRequest struct {
	ProjectID     uint   `json:"project_id" binding:"required,min=1"`
	ChangeSummary string `json:"change_summary" binding:"max=500"`
}

type rollbackScenarioVersionRequest struct {
	ProjectID          uint   `json:"project_id" binding:"required,min=1"`
	VersionID          uint   `json:"version_id" binding:"required,min=1"`
	OverrideLockedCode bool   `json:"override_locked_code"`
	ChangeSummary      string `json:"change_summary" binding:"max=500"`
}

type aiPlanFromTaskRequest struct {
	ProjectID       uint `json:"project_id" binding:"required,min=1"`
	TaskID          uint `json:"task_id" binding:"required,min=1"`
	SourceVersionID uint `json:"source_version_id"`
	MaxSteps        int  `json:"max_steps" binding:"omitempty,min=0,max=20"`
}

// listAIScenarioCompositions 获取场景编排列表。
// @Summary 获取场景编排列表
// @Description 分页查询复杂测试场景编排，支持状态、关键词和最近验证状态筛选
// @Tags AIScriptComposition
// @Produce json
// @Param project_id query int true "项目 ID"
// @Param keyword query string false "关键词"
// @Param status query string false "编排状态"
// @Param validation_status query string false "最近验证状态"
// @Param page query int false "页码"
// @Param pageSize query int false "每页条数"
// @Success 200 {object} response.Response{data=[]model.AIScenarioComposition}
// @Router /ai-script/compositions [get]
func (a *API) listAIScenarioCompositions(c *gin.Context) {
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
	compositions, total, err := a.aiScenarioCompositionSvc.List(c.Request.Context(), service.ScenarioCompositionListInput{
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
	response.Page(c, compositions, total, page, pageSize)
}

// createAIScenarioComposition 创建场景编排。
// @Summary 创建场景编排
// @Tags AIScriptComposition
// @Accept json
// @Produce json
// @Param body body createScenarioCompositionRequest true "创建参数"
// @Success 201 {object} response.Response{data=model.AIScenarioComposition}
// @Router /ai-script/compositions [post]
func (a *API) createAIScenarioComposition(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleManager, model.GlobalRoleTester, model.GlobalRoleDeveloper) {
		return
	}
	var req createScenarioCompositionRequest
	if !bindJSON(c, &req) {
		return
	}
	if !a.requireProjectAccess(c, user, req.ProjectID) {
		return
	}
	composition, err := a.aiScenarioCompositionSvc.Create(c.Request.Context(), user.ID, service.ScenarioCompositionCreateInput{
		ProjectID:    req.ProjectID,
		ScenarioKey:  req.ScenarioKey,
		ScenarioName: req.ScenarioName,
		Description:  req.Description,
	})
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.Created(c, composition)
}

// getAIScenarioComposition 获取场景编排详情。
// @Summary 获取场景编排详情
// @Tags AIScriptComposition
// @Produce json
// @Param compositionID path int true "场景编排 ID"
// @Param project_id query int true "项目 ID"
// @Success 200 {object} response.Response{data=model.AIScenarioComposition}
// @Router /ai-script/compositions/{compositionID} [get]
func (a *API) getAIScenarioComposition(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseProjectIDQuery(c)
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	compositionID, ok := parseUintParam(c, "compositionID")
	if !ok {
		return
	}
	composition, err := a.aiScenarioCompositionSvc.Get(c.Request.Context(), projectID, compositionID)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, composition)
}

// updateAIScenarioComposition 更新场景编排。
// @Summary 更新场景编排
// @Tags AIScriptComposition
// @Accept json
// @Produce json
// @Param compositionID path int true "场景编排 ID"
// @Param body body updateScenarioCompositionRequest true "更新参数"
// @Success 200 {object} response.Response{data=model.AIScenarioComposition}
// @Router /ai-script/compositions/{compositionID} [put]
func (a *API) updateAIScenarioComposition(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleManager, model.GlobalRoleTester, model.GlobalRoleDeveloper) {
		return
	}
	compositionID, ok := parseUintParam(c, "compositionID")
	if !ok {
		return
	}
	var req updateScenarioCompositionRequest
	if !bindJSON(c, &req) {
		return
	}
	if !a.requireProjectAccess(c, user, req.ProjectID) {
		return
	}
	composition, err := a.aiScenarioCompositionSvc.Update(c.Request.Context(), user.ID, compositionID, service.ScenarioCompositionUpdateInput{
		ProjectID:        req.ProjectID,
		ScenarioName:     req.ScenarioName,
		Description:      req.Description,
		DSL:              req.DSL,
		ExpectedRevision: req.ExpectedRevision,
	})
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, composition)
}

// addAIScenarioStep 添加编排步骤。
// @Summary 添加编排步骤
// @Tags AIScriptComposition
// @Accept json
// @Produce json
// @Param compositionID path int true "场景编排 ID"
// @Param body body saveScenarioStepRequest true "步骤参数"
// @Success 201 {object} response.Response{data=model.AIScenarioStep}
// @Router /ai-script/compositions/{compositionID}/steps [post]
func (a *API) addAIScenarioStep(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleManager, model.GlobalRoleTester, model.GlobalRoleDeveloper) {
		return
	}
	compositionID, ok := parseUintParam(c, "compositionID")
	if !ok {
		return
	}
	var req saveScenarioStepRequest
	if !bindJSON(c, &req) {
		return
	}
	if !a.requireProjectAccess(c, user, req.ProjectID) {
		return
	}
	step, err := a.aiScenarioCompositionSvc.AddStep(c.Request.Context(), user.ID, compositionID, buildScenarioStepInput(req))
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.Created(c, step)
}

// updateAIScenarioStep 更新编排步骤。
// @Summary 更新编排步骤
// @Tags AIScriptComposition
// @Accept json
// @Produce json
// @Param compositionID path int true "场景编排 ID"
// @Param stepID path int true "步骤 ID"
// @Param body body saveScenarioStepRequest true "步骤参数"
// @Success 200 {object} response.Response{data=model.AIScenarioStep}
// @Router /ai-script/compositions/{compositionID}/steps/{stepID} [put]
func (a *API) updateAIScenarioStep(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleManager, model.GlobalRoleTester, model.GlobalRoleDeveloper) {
		return
	}
	compositionID, ok := parseUintParam(c, "compositionID")
	if !ok {
		return
	}
	stepID, ok := parseUintParam(c, "stepID")
	if !ok {
		return
	}
	var req saveScenarioStepRequest
	if !bindJSON(c, &req) {
		return
	}
	if !a.requireProjectAccess(c, user, req.ProjectID) {
		return
	}
	step, err := a.aiScenarioCompositionSvc.UpdateStep(c.Request.Context(), user.ID, compositionID, stepID, buildScenarioStepInput(req))
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, step)
}

// deleteAIScenarioStep 删除编排步骤。
// @Summary 删除编排步骤
// @Tags AIScriptComposition
// @Produce json
// @Param compositionID path int true "场景编排 ID"
// @Param stepID path int true "步骤 ID"
// @Param project_id query int true "项目 ID"
// @Success 200 {object} response.Response
// @Router /ai-script/compositions/{compositionID}/steps/{stepID} [delete]
func (a *API) deleteAIScenarioStep(c *gin.Context) {
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
	compositionID, ok := parseUintParam(c, "compositionID")
	if !ok {
		return
	}
	stepID, ok := parseUintParam(c, "stepID")
	if !ok {
		return
	}
	if err := a.aiScenarioCompositionSvc.DeleteStep(c.Request.Context(), user.ID, projectID, compositionID, stepID); err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, gin.H{"message": "步骤已删除"})
}

// reorderAIScenarioSteps 调整编排步骤顺序。
// @Summary 调整编排步骤顺序
// @Tags AIScriptComposition
// @Accept json
// @Produce json
// @Param compositionID path int true "场景编排 ID"
// @Param body body reorderScenarioStepsRequest true "排序参数"
// @Success 200 {object} response.Response
// @Router /ai-script/compositions/{compositionID}/steps/reorder [put]
func (a *API) reorderAIScenarioSteps(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleManager, model.GlobalRoleTester, model.GlobalRoleDeveloper) {
		return
	}
	compositionID, ok := parseUintParam(c, "compositionID")
	if !ok {
		return
	}
	var req reorderScenarioStepsRequest
	if !bindJSON(c, &req) {
		return
	}
	if !a.requireProjectAccess(c, user, req.ProjectID) {
		return
	}
	if err := a.aiScenarioCompositionSvc.ReorderSteps(c.Request.Context(), user.ID, req.ProjectID, compositionID, req.StepIDs); err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, gin.H{"message": "步骤顺序已更新"})
}

// generateAIScenarioCode 生成编排代码。
// @Summary 生成编排代码
// @Tags AIScriptComposition
// @Accept json
// @Produce json
// @Param compositionID path int true "场景编排 ID"
// @Param body body generateScenarioCodeRequest true "生成参数"
// @Success 200 {object} response.Response{data=service.GenerateCompositionCodeResult}
// @Router /ai-script/compositions/{compositionID}/generate-code [post]
func (a *API) generateAIScenarioCode(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleManager, model.GlobalRoleTester, model.GlobalRoleDeveloper) {
		return
	}
	compositionID, ok := parseUintParam(c, "compositionID")
	if !ok {
		return
	}
	var req generateScenarioCodeRequest
	if !bindJSON(c, &req) {
		return
	}
	if !a.requireProjectAccess(c, user, req.ProjectID) {
		return
	}
	result, err := a.aiScenarioCompositionSvc.GenerateCode(c.Request.Context(), user.ID, compositionID, service.GenerateCompositionCodeInput{
		ProjectID:      req.ProjectID,
		Force:          req.Force,
		Target:         req.Target,
		ConfirmPartial: req.ConfirmPartial,
	})
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, result)
}

// manualUpdateAIScenarioCode 保存人工编辑后的编排生成代码。
// @Summary 保存人工编辑代码
// @Tags AIScriptComposition
// @Accept json
// @Produce json
// @Param compositionID path int true "场景编排 ID"
// @Param body body manualScenarioCodeRequest true "人工代码参数"
// @Success 200 {object} response.Response{data=model.AIScenarioComposition}
// @Router /ai-script/compositions/{compositionID}/generated-code [put]
func (a *API) manualUpdateAIScenarioCode(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleManager, model.GlobalRoleTester, model.GlobalRoleDeveloper) {
		return
	}
	compositionID, ok := parseUintParam(c, "compositionID")
	if !ok {
		return
	}
	var req manualScenarioCodeRequest
	if !bindJSON(c, &req) {
		return
	}
	if !a.requireProjectAccess(c, user, req.ProjectID) {
		return
	}
	composition, err := a.aiScenarioCompositionSvc.ManualUpdateCode(c.Request.Context(), user.ID, compositionID, service.ManualUpdateCompositionCodeInput{
		ProjectID:        req.ProjectID,
		GeneratedCode:    req.GeneratedCode,
		ChangeSummary:    req.ChangeSummary,
		Locked:           req.Locked,
		ExpectedRevision: req.ExpectedRevision,
	})
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, composition)
}

// setAIScenarioCodeLock 锁定或解除编排生成代码锁。
// @Summary 锁定或解除生成代码
// @Tags AIScriptComposition
// @Accept json
// @Produce json
// @Param compositionID path int true "场景编排 ID"
// @Param body body scenarioCodeLockRequest true "锁定参数"
// @Success 200 {object} response.Response{data=model.AIScenarioComposition}
// @Router /ai-script/compositions/{compositionID}/code-lock [post]
func (a *API) setAIScenarioCodeLock(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleManager, model.GlobalRoleTester, model.GlobalRoleDeveloper) {
		return
	}
	compositionID, ok := parseUintParam(c, "compositionID")
	if !ok {
		return
	}
	var req scenarioCodeLockRequest
	if !bindJSON(c, &req) {
		return
	}
	if !a.requireProjectAccess(c, user, req.ProjectID) {
		return
	}
	composition, err := a.aiScenarioCompositionSvc.SetCodeLock(c.Request.Context(), user.ID, compositionID, service.LockCompositionCodeInput{
		ProjectID:     req.ProjectID,
		Locked:        req.Locked,
		ChangeSummary: req.ChangeSummary,
	})
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, composition)
}

// validateAIScenarioComposition 验证编排。
// @Summary 验证编排
// @Tags AIScriptComposition
// @Accept json
// @Produce json
// @Param compositionID path int true "场景编排 ID"
// @Param body body validateScenarioRequest true "验证参数"
// @Success 201 {object} response.Response{data=model.AICompositionValidation}
// @Router /ai-script/compositions/{compositionID}/validate [post]
func (a *API) validateAIScenarioComposition(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleManager, model.GlobalRoleTester, model.GlobalRoleDeveloper, model.GlobalRoleReviewer) {
		return
	}
	compositionID, ok := parseUintParam(c, "compositionID")
	if !ok {
		return
	}
	var req validateScenarioRequest
	if !bindJSON(c, &req) {
		return
	}
	if !a.requireProjectAccess(c, user, req.ProjectID) {
		return
	}
	idempotencyKey := req.IdempotencyKey
	if idempotencyKey == "" {
		idempotencyKey = c.GetHeader("X-Idempotency-Key")
	}
	validation, err := a.aiScenarioCompositionSvc.Validate(c.Request.Context(), user.ID, compositionID, service.ValidateCompositionInput{
		ProjectID:      req.ProjectID,
		Environment:    req.Environment,
		Variables:      req.Variables,
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.Created(c, validation)
}

// publishAIScenarioComposition 发布编排。
// @Summary 发布编排
// @Tags AIScriptComposition
// @Accept json
// @Produce json
// @Param compositionID path int true "场景编排 ID"
// @Param body body publishScenarioRequest true "发布参数"
// @Success 201 {object} response.Response{data=model.AIScenarioCompositionVersion}
// @Router /ai-script/compositions/{compositionID}/publish [post]
func (a *API) publishAIScenarioComposition(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleManager, model.GlobalRoleTester, model.GlobalRoleDeveloper) {
		return
	}
	compositionID, ok := parseUintParam(c, "compositionID")
	if !ok {
		return
	}
	var req publishScenarioRequest
	if !bindJSON(c, &req) {
		return
	}
	if !a.requireProjectAccess(c, user, req.ProjectID) {
		return
	}
	version, err := a.aiScenarioCompositionSvc.Publish(c.Request.Context(), user.ID, compositionID, service.PublishCompositionInput{
		ProjectID:     req.ProjectID,
		ChangeSummary: req.ChangeSummary,
	})
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.Created(c, version)
}

// archiveAIScenarioComposition 归档编排。
// @Summary 归档编排
// @Tags AIScriptComposition
// @Accept json
// @Produce json
// @Param compositionID path int true "场景编排 ID"
// @Param body body publishScenarioRequest true "归档参数"
// @Success 200 {object} response.Response{data=model.AIScenarioComposition}
// @Router /ai-script/compositions/{compositionID}/archive [post]
func (a *API) archiveAIScenarioComposition(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleManager) {
		return
	}
	compositionID, ok := parseUintParam(c, "compositionID")
	if !ok {
		return
	}
	var req publishScenarioRequest
	if !bindJSON(c, &req) {
		return
	}
	if !a.requireProjectAccess(c, user, req.ProjectID) {
		return
	}
	composition, err := a.aiScenarioCompositionSvc.Archive(c.Request.Context(), user.ID, req.ProjectID, compositionID)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, composition)
}

// deleteAIScenarioComposition 删除无版本和验证历史的编排草稿。
// @Summary 删除场景编排草稿
// @Description 仅允许删除草稿且无发布版本、无验证历史的编排；已发布或已验证编排请归档
// @Tags AIScriptComposition
// @Produce json
// @Param compositionID path int true "场景编排 ID"
// @Param project_id query int true "项目 ID"
// @Success 200 {object} response.Response
// @Failure 403 {object} response.Response
// @Failure 404 {object} response.Response
// @Failure 409 {object} response.Response
// @Router /ai-script/compositions/{compositionID} [delete]
func (a *API) deleteAIScenarioComposition(c *gin.Context) {
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
	compositionID, ok := parseUintParam(c, "compositionID")
	if !ok {
		return
	}
	if err := a.aiScenarioCompositionSvc.Delete(c.Request.Context(), projectID, compositionID); err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, gin.H{"message": "场景编排草稿已删除"})
}

// listAIScenarioVersions 获取编排版本列表。
// @Summary 获取编排版本列表
// @Tags AIScriptComposition
// @Produce json
// @Param compositionID path int true "场景编排 ID"
// @Param project_id query int true "项目 ID"
// @Success 200 {object} response.Response{data=[]model.AIScenarioCompositionVersion}
// @Router /ai-script/compositions/{compositionID}/versions [get]
func (a *API) listAIScenarioVersions(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseProjectIDQuery(c)
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	compositionID, ok := parseUintParam(c, "compositionID")
	if !ok {
		return
	}
	versions, err := a.aiScenarioCompositionSvc.ListVersions(c.Request.Context(), projectID, compositionID)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, versions)
}

// diffAIScenarioVersions 获取两个编排版本的 DSL 和代码差异。
// @Summary 获取编排版本 Diff
// @Tags AIScriptComposition
// @Produce json
// @Param compositionID path int true "场景编排 ID"
// @Param project_id query int true "项目 ID"
// @Param base_version_id query int true "基准版本 ID"
// @Param target_version_id query int true "目标版本 ID"
// @Success 200 {object} response.Response{data=service.ScenarioVersionDiffResult}
// @Router /ai-script/compositions/{compositionID}/versions/diff [get]
func (a *API) diffAIScenarioVersions(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseProjectIDQuery(c)
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	compositionID, ok := parseUintParam(c, "compositionID")
	if !ok {
		return
	}
	result, err := a.aiScenarioCompositionSvc.DiffVersion(c.Request.Context(), compositionID, service.ScenarioVersionDiffInput{
		ProjectID:       projectID,
		BaseVersionID:   uint(parsePositiveIntWithDefault(c.Query("base_version_id"), 0)),
		TargetVersionID: uint(parsePositiveIntWithDefault(c.Query("target_version_id"), 0)),
	})
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, result)
}

// rollbackAIScenarioVersion 回滚编排到指定版本快照。
// @Summary 回滚编排版本
// @Tags AIScriptComposition
// @Accept json
// @Produce json
// @Param compositionID path int true "场景编排 ID"
// @Param body body rollbackScenarioVersionRequest true "回滚参数"
// @Success 200 {object} response.Response{data=model.AIScenarioComposition}
// @Router /ai-script/compositions/{compositionID}/versions/rollback [post]
func (a *API) rollbackAIScenarioVersion(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleManager, model.GlobalRoleTester, model.GlobalRoleDeveloper) {
		return
	}
	compositionID, ok := parseUintParam(c, "compositionID")
	if !ok {
		return
	}
	var req rollbackScenarioVersionRequest
	if !bindJSON(c, &req) {
		return
	}
	if !a.requireProjectAccess(c, user, req.ProjectID) {
		return
	}
	composition, err := a.aiScenarioCompositionSvc.RollbackVersion(c.Request.Context(), user.ID, compositionID, service.ScenarioVersionRollbackInput{
		ProjectID:          req.ProjectID,
		VersionID:          req.VersionID,
		OverrideLockedCode: req.OverrideLockedCode,
		ChangeSummary:      req.ChangeSummary,
	})
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, composition)
}

// listAIScenarioValidations 获取编排验证历史。
// @Summary 获取编排验证历史
// @Tags AIScriptComposition
// @Produce json
// @Param compositionID path int true "场景编排 ID"
// @Param project_id query int true "项目 ID"
// @Success 200 {object} response.Response{data=[]model.AICompositionValidation}
// @Router /ai-script/compositions/{compositionID}/validations [get]
func (a *API) listAIScenarioValidations(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseProjectIDQuery(c)
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	compositionID, ok := parseUintParam(c, "compositionID")
	if !ok {
		return
	}
	validations, err := a.aiScenarioCompositionSvc.ListValidations(c.Request.Context(), projectID, compositionID)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, validations)
}

// listAIScenarioReferences 获取编排引用关系。
// @Summary 获取编排引用关系
// @Tags AIScriptComposition
// @Produce json
// @Param compositionID path int true "场景编排 ID"
// @Param project_id query int true "项目 ID"
// @Success 200 {object} response.Response{data=[]model.AIAssetReference}
// @Router /ai-script/compositions/{compositionID}/references [get]
func (a *API) listAIScenarioReferences(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseProjectIDQuery(c)
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	compositionID, ok := parseUintParam(c, "compositionID")
	if !ok {
		return
	}
	refs, err := a.aiScenarioCompositionSvc.References(c.Request.Context(), projectID, compositionID)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, refs)
}

// createAIPlanFromTask 从录制任务生成编排建议。
// @Summary 从录制任务生成编排建议
// @Tags AIScriptComposition
// @Accept json
// @Produce json
// @Param body body aiPlanFromTaskRequest true "AI 规划参数"
// @Success 200 {object} response.Response{data=service.AIPlanResult}
// @Router /ai-script/compositions/ai-plan-from-task [post]
func (a *API) createAIPlanFromTask(c *gin.Context) {
	user := currentUser(c)
	var req aiPlanFromTaskRequest
	if !bindJSON(c, &req) {
		return
	}
	if !a.requireProjectAccess(c, user, req.ProjectID) {
		return
	}
	result, err := a.aiScenarioCompositionSvc.AIPlanFromTask(c.Request.Context(), service.AIPlanFromTaskInput{
		ProjectID:       req.ProjectID,
		TaskID:          req.TaskID,
		SourceVersionID: req.SourceVersionID,
		MaxSteps:        req.MaxSteps,
	})
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, result)
}

// optimizeAIScenarioComposition 获取 AI 优化建议。
// @Summary 获取 AI 优化建议
// @Tags AIScriptComposition
// @Accept json
// @Produce json
// @Param compositionID path int true "场景编排 ID"
// @Param body body publishScenarioRequest true "项目参数"
// @Success 200 {object} response.Response{data=service.AIPlanResult}
// @Router /ai-script/compositions/{compositionID}/ai-optimize [post]
func (a *API) optimizeAIScenarioComposition(c *gin.Context) {
	user := currentUser(c)
	compositionID, ok := parseUintParam(c, "compositionID")
	if !ok {
		return
	}
	var req publishScenarioRequest
	if !bindJSON(c, &req) {
		return
	}
	if !a.requireProjectAccess(c, user, req.ProjectID) {
		return
	}
	result, err := a.aiScenarioCompositionSvc.AISuggestAssertions(c.Request.Context(), req.ProjectID, compositionID)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, result)
}

// suggestAIScenarioAssertions 获取 AI 断言建议。
// @Summary 获取 AI 断言建议
// @Tags AIScriptComposition
// @Accept json
// @Produce json
// @Param compositionID path int true "场景编排 ID"
// @Param body body publishScenarioRequest true "项目参数"
// @Success 200 {object} response.Response{data=service.AIPlanResult}
// @Router /ai-script/compositions/{compositionID}/ai-suggest-assertions [post]
func (a *API) suggestAIScenarioAssertions(c *gin.Context) {
	a.optimizeAIScenarioComposition(c)
}

func buildScenarioStepInput(req saveScenarioStepRequest) service.ScenarioStepSaveInput {
	enabled := true
	enabledSpecified := false
	if req.Enabled != nil {
		enabled = *req.Enabled
		enabledSpecified = true
	}
	return service.ScenarioStepSaveInput{
		ProjectID:        req.ProjectID,
		StepType:         req.StepType,
		StepName:         req.StepName,
		RefFlowID:        req.RefFlowID,
		RefFlowVersionID: req.RefFlowVersionID,
		RefAssertionID:   req.RefAssertionID,
		ParamMapping:     req.ParamMapping,
		OutputMapping:    req.OutputMapping,
		AtomicAction:     req.AtomicAction,
		CodeBlock:        req.CodeBlock,
		ManualReviewed:   req.ManualReviewed,
		AIConfidence:     req.AIConfidence,
		Enabled:          enabled,
		EnabledSpecified: enabledSpecified,
	}
}

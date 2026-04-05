// handler_ai_script.go — 测试智编模块 HTTP Handler
package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"testpilot/internal/dto/response"
	"testpilot/internal/model"
	"testpilot/internal/service"
)

// ── 请求结构 ──

type createAIScriptTaskRequest struct {
	ProjectID      uint   `json:"project_id" binding:"required,min=1"`
	TaskName       string `json:"task_name" binding:"required,min=1,max=128"`
	GenerationMode string `json:"generation_mode" binding:"omitempty,max=32"`
	ScenarioDesc   string `json:"scenario_desc" binding:"required,min=1"`
	StartURL       string `json:"start_url" binding:"required,url,max=512"`
	AccountRef     string `json:"account_ref" binding:"max=256"`
	CaseIDs        []uint `json:"case_ids" binding:"required,min=1"`
	FrameworkType  string `json:"framework_type" binding:"omitempty,max=32"`
}

type editAIScriptRequest struct {
	ScriptContent string `json:"script_content" binding:"required,min=1"`
	CommentText   string `json:"comment_text" binding:"max=500"`
}

type triggerValidationRequest struct {
	ScriptVersionID uint   `json:"script_version_id" binding:"required,min=1"`
	Remark          string `json:"remark" binding:"max=255"`
}

type confirmScriptRequest struct {
	CommentText string `json:"comment_text" binding:"max=500"`
}

type discardScriptRequest struct {
	Reason string `json:"reason" binding:"required,max=255"`
}

type discardTaskRequest struct {
	Reason string `json:"reason" binding:"required,max=255"`
}

type taskFilterSnapshotRequest struct {
	ProjectID  uint   `json:"project_id"`
	Keyword    string `json:"keyword" binding:"omitempty,max=255"`
	TaskStatus string `json:"task_status" binding:"omitempty,max=32"`
}

type batchTaskSelectionRequest struct {
	SelectionMode   string                     `json:"selection_mode" binding:"required,oneof=IDS FILTER_ALL"`
	TaskIDs         []uint                     `json:"task_ids"`
	ExcludedTaskIDs []uint                     `json:"excluded_task_ids"`
	FilterSnapshot  *taskFilterSnapshotRequest `json:"filter_snapshot"`
	ExpectedTotal   int                        `json:"expected_total" binding:"omitempty,min=0"`
}

type batchDiscardTasksRequest struct {
	batchTaskSelectionRequest
	Reason string `json:"reason" binding:"required,max=255"`
}

type cloneTaskRequest struct {
	TaskName string `json:"task_name" binding:"required,min=1,max=128"`
}

type finishRecordingRequest struct {
	RecordingID       uint   `json:"recording_id" binding:"required,min=1"`
	RawScriptContent  string `json:"raw_script_content" binding:"required,min=1"`
	TriggerAIRefactor bool   `json:"trigger_ai_refactor"`
}

type failRecordingRequest struct {
	RecordingID uint   `json:"recording_id" binding:"required,min=1"`
	Reason      string `json:"reason" binding:"required,min=1,max=2000"`
}

type updateTaskCasesRequest struct {
	CaseIDs []uint `json:"case_ids" binding:"required,min=1"`
}

// ── 现有 Handler 方法 ──

// listAIScriptTasks 获取任务列表
func (a *API) listAIScriptTasks(c *gin.Context) {
	projectIDStr := c.Query("project_id")
	keyword := c.Query("keyword")
	taskStatus := c.Query("task_status")
	pageStr := c.DefaultQuery("page", "1")
	pageSizeStr := c.DefaultQuery("pageSize", "20")

	var projectID uint
	if projectIDStr != "" {
		id, _ := strconv.ParseUint(projectIDStr, 10, 64)
		projectID = uint(id)
	}
	page, _ := strconv.Atoi(pageStr)
	pageSize, _ := strconv.Atoi(pageSizeStr)

	tasks, total, err := a.aiScriptSvc.ListTasks(c.Request.Context(), projectID, keyword, taskStatus, page, pageSize)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.Page(c, tasks, total, page, pageSize)
}

// getAIScriptTask 获取任务详情（含权限标识）
func (a *API) getAIScriptTask(c *gin.Context) {
	user := currentUser(c)
	taskID, ok := parseUintParam(c, "taskID")
	if !ok {
		return
	}

	task, err := a.aiScriptSvc.GetTask(c.Request.Context(), taskID)
	if err != nil {
		response.HandleError(c, err)
		return
	}

	// 计算权限标识
	task.Permissions = a.aiScriptSvc.ComputePermissions(c.Request.Context(), user.ID, task)

	response.OK(c, task)
}

// createAIScriptTask 创建生成任务
func (a *API) createAIScriptTask(c *gin.Context) {
	user := currentUser(c)

	var req createAIScriptTaskRequest
	if !bindJSON(c, &req) {
		return
	}

	// 检查项目访问权限
	if !a.requireProjectAccess(c, user, req.ProjectID) {
		return
	}

	input := service.CreateTaskInput{
		ProjectID:      req.ProjectID,
		TaskName:       req.TaskName,
		GenerationMode: req.GenerationMode,
		ScenarioDesc:   req.ScenarioDesc,
		StartURL:       req.StartURL,
		AccountRef:     req.AccountRef,
		CaseIDs:        req.CaseIDs,
		FrameworkType:  req.FrameworkType,
	}

	task, err := a.aiScriptSvc.CreateTask(c.Request.Context(), user.ID, input)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.Created(c, task)
}

// executeAIScriptTask 触发执行生成（仅 AI_DIRECT 模式）
func (a *API) executeAIScriptTask(c *gin.Context) {
	user := currentUser(c)
	taskID, ok := parseUintParam(c, "taskID")
	if !ok {
		return
	}

	if err := a.aiScriptSvc.ExecuteTask(c.Request.Context(), user.ID, taskID); err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, gin.H{"message": "任务已提交执行"})
}

// getAIScriptVersions 获取脚本版本列表
func (a *API) getAIScriptVersions(c *gin.Context) {
	taskID, ok := parseUintParam(c, "taskID")
	if !ok {
		return
	}

	versions, err := a.aiScriptSvc.GetScriptVersions(c.Request.Context(), taskID)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, versions)
}

// getCurrentAIScript 获取当前脚本版本
func (a *API) getCurrentAIScript(c *gin.Context) {
	taskID, ok := parseUintParam(c, "taskID")
	if !ok {
		return
	}

	script, err := a.aiScriptSvc.GetCurrentScript(c.Request.Context(), taskID)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, script)
}

// editAIScript 编辑脚本（生成新版本）
func (a *API) editAIScript(c *gin.Context) {
	user := currentUser(c)
	taskID, ok := parseUintParam(c, "taskID")
	if !ok {
		return
	}

	var req editAIScriptRequest
	if !bindJSON(c, &req) {
		return
	}

	input := service.EditScriptInput{
		ScriptContent: req.ScriptContent,
		CommentText:   req.CommentText,
	}

	version, err := a.aiScriptSvc.EditScript(c.Request.Context(), user.ID, taskID, input)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.Created(c, version)
}

// triggerAIScriptValidation 触发回放验证
func (a *API) triggerAIScriptValidation(c *gin.Context) {
	user := currentUser(c)
	taskID, ok := parseUintParam(c, "taskID")
	if !ok {
		return
	}

	var req triggerValidationRequest
	if !bindJSON(c, &req) {
		return
	}

	validation, err := a.aiScriptSvc.TriggerValidation(c.Request.Context(), user.ID, taskID, req.ScriptVersionID)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.Created(c, validation)
}

// getAIScriptTraces 获取操作轨迹
func (a *API) getAIScriptTraces(c *gin.Context) {
	taskID, ok := parseUintParam(c, "taskID")
	if !ok {
		return
	}

	traces, err := a.aiScriptSvc.GetTraces(c.Request.Context(), taskID)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, traces)
}

// getAIScriptLatestValidation 获取最近验证结果
func (a *API) getAIScriptLatestValidation(c *gin.Context) {
	scriptVersionIDStr := c.Query("script_version_id")
	if scriptVersionIDStr == "" {
		response.HandleError(c, service.ErrBadRequest(service.CodeParamsError, "script_version_id is required"))
		return
	}
	scriptVersionID, _ := strconv.ParseUint(scriptVersionIDStr, 10, 64)

	validation, err := a.aiScriptSvc.GetLatestValidation(c.Request.Context(), uint(scriptVersionID))
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, validation)
}

// getAIScriptEvidences 获取证据列表
func (a *API) getAIScriptEvidences(c *gin.Context) {
	taskID, ok := parseUintParam(c, "taskID")
	if !ok {
		return
	}

	evidences, err := a.aiScriptSvc.GetEvidences(c.Request.Context(), taskID)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, evidences)
}

// ── 新增 Handler 方法（阶段一） ──

// confirmScript 确认脚本
func (a *API) confirmScript(c *gin.Context) {
	user := currentUser(c)
	scriptID, ok := parseUintParam(c, "scriptID")
	if !ok {
		return
	}

	if err := a.aiScriptSvc.ConfirmScript(c.Request.Context(), user.ID, scriptID); err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, gin.H{"message": "脚本已确认"})
}

// discardScript 废弃脚本版本
func (a *API) discardScript(c *gin.Context) {
	user := currentUser(c)
	scriptID, ok := parseUintParam(c, "scriptID")
	if !ok {
		return
	}

	var req discardScriptRequest
	if !bindJSON(c, &req) {
		return
	}

	if err := a.aiScriptSvc.DiscardScript(c.Request.Context(), user.ID, scriptID, req.Reason); err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, gin.H{"message": "脚本版本已废弃"})
}

// discardTask 废弃任务
func (a *API) discardTask(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleManager) {
		return
	}
	taskID, ok := parseUintParam(c, "taskID")
	if !ok {
		return
	}

	var req discardTaskRequest
	if !bindJSON(c, &req) {
		return
	}

	if err := a.aiScriptSvc.DiscardTask(c.Request.Context(), user.ID, taskID, req.Reason); err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, gin.H{"message": "任务已废弃"})
}

// batchDiscardTasks 批量废弃任务。
func (a *API) batchDiscardTasks(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleManager) {
		return
	}

	var req batchDiscardTasksRequest
	if !bindJSON(c, &req) {
		return
	}

	input := service.BatchDiscardTasksInput{
		BatchTaskSelectionInput: buildBatchTaskSelectionInput(req.batchTaskSelectionRequest),
		Reason:                  req.Reason,
	}

	result, err := a.aiScriptSvc.BatchDiscardTasks(c.Request.Context(), user.ID, input)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, result)
}

// deleteTask 删除已废弃任务
func (a *API) deleteTask(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleManager) {
		return
	}
	taskID, ok := parseUintParam(c, "taskID")
	if !ok {
		return
	}

	if err := a.aiScriptSvc.DeleteTask(c.Request.Context(), user.ID, taskID); err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, gin.H{"message": "任务已删除"})
}

// batchDeleteTasks 批量删除任务。
func (a *API) batchDeleteTasks(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleManager) {
		return
	}

	var req batchTaskSelectionRequest
	if !bindJSON(c, &req) {
		return
	}

	result, err := a.aiScriptSvc.BatchDeleteTasks(
		c.Request.Context(),
		user.ID,
		buildBatchTaskSelectionInput(req),
	)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, result)
}

// buildBatchTaskSelectionInput 将 HTTP 请求结构转换为 Service 层输入。
func buildBatchTaskSelectionInput(req batchTaskSelectionRequest) service.BatchTaskSelectionInput {
	var filterSnapshot *service.TaskFilterSnapshot
	if req.FilterSnapshot != nil {
		filterSnapshot = &service.TaskFilterSnapshot{
			ProjectID:  req.FilterSnapshot.ProjectID,
			Keyword:    req.FilterSnapshot.Keyword,
			TaskStatus: req.FilterSnapshot.TaskStatus,
		}
	}

	return service.BatchTaskSelectionInput{
		SelectionMode:   service.BatchTaskSelectionMode(req.SelectionMode),
		TaskIDs:         req.TaskIDs,
		ExcludedTaskIDs: req.ExcludedTaskIDs,
		FilterSnapshot:  filterSnapshot,
		ExpectedTotal:   req.ExpectedTotal,
	}
}

// cloneTask 克隆任务配置
func (a *API) cloneTask(c *gin.Context) {
	user := currentUser(c)
	taskID, ok := parseUintParam(c, "taskID")
	if !ok {
		return
	}

	var req cloneTaskRequest
	if !bindJSON(c, &req) {
		return
	}

	task, err := a.aiScriptSvc.CloneTask(c.Request.Context(), user.ID, taskID, req.TaskName)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.Created(c, task)
}

// startRecording 启动录制
func (a *API) startRecording(c *gin.Context) {
	user := currentUser(c)
	taskID, ok := parseUintParam(c, "taskID")
	if !ok {
		return
	}

	session, err := a.aiScriptSvc.StartRecording(c.Request.Context(), user.ID, taskID)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.Created(c, session)
}

// finishRecording 结束录制
func (a *API) finishRecording(c *gin.Context) {
	user := currentUser(c)
	taskID, ok := parseUintParam(c, "taskID")
	if !ok {
		return
	}

	var req finishRecordingRequest
	if !bindJSON(c, &req) {
		return
	}

	if err := a.aiScriptSvc.FinishRecording(c.Request.Context(), user.ID, taskID, req.RecordingID, req.RawScriptContent, req.TriggerAIRefactor); err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, gin.H{"message": "录制已结束"})
}

// getLatestRecording 获取最近录制结果
// failRecording 标记录制失败，避免异常会话长期卡在 RECORDING。
func (a *API) failRecording(c *gin.Context) {
	user := currentUser(c)
	taskID, ok := parseUintParam(c, "taskID")
	if !ok {
		return
	}

	var req failRecordingRequest
	if !bindJSON(c, &req) {
		return
	}

	if err := a.aiScriptSvc.FailRecording(c.Request.Context(), user.ID, taskID, req.RecordingID, req.Reason); err != nil {
		response.HandleError(c, err)
		return
	}

	response.OK(c, gin.H{"message": "recording failed"})
}

func (a *API) getLatestRecording(c *gin.Context) {
	taskID, ok := parseUintParam(c, "taskID")
	if !ok {
		return
	}

	session, err := a.aiScriptSvc.GetLatestRecording(c.Request.Context(), taskID)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, session)
}

// exportScript 导出脚本
func (a *API) exportScript(c *gin.Context) {
	scriptID, ok := parseUintParam(c, "scriptID")
	if !ok {
		return
	}

	content, fileName, err := a.aiScriptSvc.ExportScript(c.Request.Context(), scriptID)
	if err != nil {
		response.HandleError(c, err)
		return
	}

	c.Header("Content-Disposition", "attachment; filename="+fileName)
	c.Data(http.StatusOK, "application/octet-stream", []byte(content))
}

// getValidationHistory 获取验证历史
func (a *API) getValidationHistory(c *gin.Context) {
	scriptIDStr := c.Param("scriptID")
	scriptID, _ := strconv.ParseUint(scriptIDStr, 10, 64)
	if scriptID == 0 {
		response.HandleError(c, service.ErrBadRequest(service.CodeParamsError, "无效的脚本 ID"))
		return
	}

	validations, err := a.aiScriptSvc.GetValidationHistory(c.Request.Context(), uint(scriptID))
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, validations)
}

// updateTaskCases 更新任务关联用例
func (a *API) updateTaskCases(c *gin.Context) {
	user := currentUser(c)
	taskID, ok := parseUintParam(c, "taskID")
	if !ok {
		return
	}

	var req updateTaskCasesRequest
	if !bindJSON(c, &req) {
		return
	}

	if err := a.aiScriptSvc.UpdateTaskCases(c.Request.Context(), user.ID, taskID, req.CaseIDs); err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, gin.H{"message": "用例关联已更新"})
}

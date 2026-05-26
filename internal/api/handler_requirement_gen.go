// handler_requirement_gen.go — 需求智生-生成任务 & 产物 HTTP Handler
//
// 外部 API（前端调用）：
//
//	POST   /projects/:projectID/requirement-gen/tasks           创建生成任务
//	GET    /projects/:projectID/requirement-gen/tasks           任务列表
//	GET    /projects/:projectID/requirement-gen/tasks/:taskID   任务详情
//	DELETE /projects/:projectID/requirement-gen/tasks/:taskID   删除任务
//	GET    /projects/:projectID/requirement-gen/tasks/:taskID/results  产物列表
//	POST   /projects/:projectID/requirement-gen/results/:resultID/adopt   采纳
//	POST   /projects/:projectID/requirement-gen/results/:resultID/discard 丢弃
//	POST   /projects/:projectID/requirement-gen/tasks/:taskID/close       关闭任务
//
// 内部 API（Executor 回调）：
//
//	POST   /internal/requirement-gen/tasks/:taskID/callback  任务完成回调
//	POST   /internal/requirement-gen/tasks/:taskID/heartbeat 心跳
package api

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"testpilot/internal/dto/response"
	"testpilot/internal/repository"
	"testpilot/internal/service"
)

// ========== 请求结构体 ==========

// createGenTaskRequest 创建生成任务
type createGenTaskRequest struct {
	RequirementDocID uint   `json:"requirement_doc_id" binding:"required,min=1"`
	SkillID          uint   `json:"skill_id" binding:"required,min=1"`
	TaskName         string `json:"task_name" binding:"required,min=1,max=200"`
	TargetModuleID   uint   `json:"target_module_id"`
	DefaultLevel     string `json:"default_level" binding:"omitempty,oneof=P0 P1 P2 P3"`
	MaxCases         int    `json:"max_cases" binding:"omitempty,min=1,max=100"`
	ExtraPrompt      string `json:"extra_prompt" binding:"max=2000"`
}

// taskCallbackRequest Executor 任务完成回调
type taskCallbackRequest struct {
	Status           string                    `json:"status" binding:"required,oneof=success failed"`
	GeneratedCount   int                       `json:"generated_count"`
	PromptTokens     int                       `json:"prompt_tokens"`
	CompletionTokens int                       `json:"completion_tokens"`
	DurationMs       int64                     `json:"duration_ms"`
	FailReason       string                    `json:"fail_reason"`
	Results          []taskCallbackResultEntry `json:"results"`
}

// taskCallbackResultEntry 回调中的单条产物
type taskCallbackResultEntry struct {
	SeqNo         int     `json:"seq_no"`
	Title         string  `json:"title"`
	Level         string  `json:"level"`
	Precondition  string  `json:"precondition"`
	Steps         string  `json:"steps"`
	Postcondition string  `json:"postcondition"`
	Remark        string  `json:"remark"`
	TagsSuggested string  `json:"tags_suggested"`
	AIConfidence  float64 `json:"ai_confidence"`
	RawJSON       string  `json:"raw_json"`
}

// ========== Handler 方法 ==========

// createGenTask 创建生成任务
// @Summary 创建需求智生任务
// @Tags 需求智生-任务
// @Accept json
// @Produce json
// @Param projectID path int true "项目ID"
// @Param body body createGenTaskRequest true "请求体"
// @Success 201 {object} response.Result
// @Failure 400,429 {object} response.Result
// @Router /api/v1/projects/{projectID}/requirement-gen/tasks [post]
func (a *API) createGenTask(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}

	var req createGenTaskRequest
	if !bindJSON(c, &req) {
		return
	}

	// 获取当前激活模型信息（用于快照）
	activeModel, modelErr := a.aiModelConfigSvc.GetActive(c.Request.Context())
	if modelErr != nil {
		response.Error(c, http.StatusBadRequest, service.CodeReqGenNoActiveModel, "未配置激活的 AI 模型")
		return
	}

	task, err := a.reqGenTaskSvc.Create(c.Request.Context(), service.CreateGenTaskInput{
		ProjectID:        projectID,
		RequirementDocID: req.RequirementDocID,
		SkillID:          req.SkillID,
		AIModelConfigID:  activeModel.ID,
		AIModelSnapshot:  activeModel.Provider + "/" + activeModel.ModelID,
		TaskName:         req.TaskName,
		TargetModuleID:   req.TargetModuleID,
		DefaultLevel:     req.DefaultLevel,
		MaxCases:         req.MaxCases,
		ExtraPrompt:      req.ExtraPrompt,
		CreatedBy:        user.ID,
	})
	if err != nil {
		response.HandleError(c, err)
		return
	}

	response.Created(c, task)
}

// listGenTasks 生成任务列表
// @Summary 需求智生任务列表
// @Tags 需求智生-任务
// @Produce json
// @Param projectID path int true "项目ID"
// @Param status query string false "任务状态"
// @Param requirement_doc_id query int false "文档ID"
// @Param page query int false "页码" default(1)
// @Param page_size query int false "每页条数" default(20)
// @Success 200 {object} response.PageResult
// @Router /api/v1/projects/{projectID}/requirement-gen/tasks [get]
func (a *API) listGenTasks(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}

	page := parsePositiveIntWithDefault(c.Query("page"), 1)
	pageSize := parsePositiveIntWithDefault(c.Query("page_size"), 20)

	tasks, total, err := a.reqGenTaskSvc.ListPaged(c.Request.Context(), projectID, repository.RequirementGenTaskFilter{
		Status:           c.Query("status"),
		RequirementDocID: uint(parsePositiveIntWithDefault(c.Query("requirement_doc_id"), 0)),
		Page:             page,
		PageSize:         pageSize,
	})
	if err != nil {
		response.HandleError(c, err)
		return
	}

	response.Page(c, tasks, total, page, pageSize)
}

// getGenTask 生成任务详情
// @Summary 需求智生任务详情
// @Tags 需求智生-任务
// @Produce json
// @Param projectID path int true "项目ID"
// @Param taskID path int true "任务ID"
// @Success 200 {object} response.Result
// @Router /api/v1/projects/{projectID}/requirement-gen/tasks/{taskID} [get]
func (a *API) getGenTask(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	taskID, ok := parseUintParam(c, "taskID")
	if !ok {
		return
	}

	task, err := a.reqGenTaskSvc.GetByID(c.Request.Context(), projectID, taskID)
	if err != nil {
		response.HandleError(c, err)
		return
	}

	response.OK(c, task)
}

// deleteGenTask 删除生成任务
// @Summary 删除需求智生任务
// @Tags 需求智生-任务
// @Produce json
// @Param projectID path int true "项目ID"
// @Param taskID path int true "任务ID"
// @Success 200 {object} response.Result
// @Router /api/v1/projects/{projectID}/requirement-gen/tasks/{taskID} [delete]
func (a *API) deleteGenTask(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	taskID, ok := parseUintParam(c, "taskID")
	if !ok {
		return
	}

	err := a.reqGenTaskSvc.Delete(c.Request.Context(), projectID, taskID)
	if err != nil {
		response.HandleError(c, err)
		return
	}

	response.OK(c, nil)
}

// listGenResults 产物列表
// @Summary 查询任务产物列表
// @Tags 需求智生-产物
// @Produce json
// @Param projectID path int true "项目ID"
// @Param taskID path int true "任务ID"
// @Success 200 {object} response.Result
// @Router /api/v1/projects/{projectID}/requirement-gen/tasks/{taskID}/results [get]
func (a *API) listGenResults(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	taskID, ok := parseUintParam(c, "taskID")
	if !ok {
		return
	}

	results, err := a.reqGenTaskSvc.ListResults(c.Request.Context(), projectID, taskID)
	if err != nil {
		response.HandleError(c, err)
		return
	}

	response.OK(c, results)
}

// adoptGenResult 采纳产物
// @Summary 采纳AI产物
// @Tags 需求智生-产物
// @Produce json
// @Param projectID path int true "项目ID"
// @Param resultID path int true "产物ID"
// @Success 200 {object} response.Result
// @Failure 409 {object} response.Result
// @Router /api/v1/projects/{projectID}/requirement-gen/results/{resultID}/adopt [post]
func (a *API) adoptGenResult(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	resultID, ok := parseUintParam(c, "resultID")
	if !ok {
		return
	}

	ctx := c.Request.Context()

	// 1. 获取产物详情
	result, err := a.reqGenTaskSvc.GetResultByID(ctx, projectID, resultID)
	if err != nil {
		response.HandleError(c, err)
		return
	}

	// 2. 获取关联任务信息（用于 TargetModuleID）
	task, err := a.reqGenTaskSvc.GetByID(ctx, projectID, result.TaskID)
	if err != nil {
		response.HandleError(c, err)
		return
	}

	// 3. 创建测试用例入库
	tc, err := a.testCaseSvc.Create(ctx, projectID, user.ID, service.CreateTestCaseInput{
		Title:        result.Title,
		Level:        result.Level,
		Precondition: result.Precondition,
		Steps:        result.Steps,
		Remark:       result.Remark,
		ModuleID:     task.TargetModuleID,
	})
	if err != nil {
		slog.Error("采纳产物时创建用例失败", "error", err, "result_id", resultID, "project_id", projectID)
		response.HandleError(c, err)
		return
	}

	// 4. 标记产物为已采纳，关联新用例 ID
	err = a.reqGenTaskSvc.AdoptResult(ctx, service.AdoptResultInput{
		ResultID:      resultID,
		ProjectID:     projectID,
		AdoptedBy:     user.ID,
		AdoptedCaseID: tc.ID,
	})
	if err != nil {
		response.HandleError(c, err)
		return
	}

	response.OK(c, gin.H{"adopted_case_id": tc.ID})
}

// discardGenResult 丢弃产物
// @Summary 丢弃AI产物
// @Tags 需求智生-产物
// @Produce json
// @Param projectID path int true "项目ID"
// @Param resultID path int true "产物ID"
// @Success 200 {object} response.Result
// @Router /api/v1/projects/{projectID}/requirement-gen/results/{resultID}/discard [post]
func (a *API) discardGenResult(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	resultID, ok := parseUintParam(c, "resultID")
	if !ok {
		return
	}

	err := a.reqGenTaskSvc.DiscardResult(c.Request.Context(), projectID, resultID, user.ID)
	if err != nil {
		response.HandleError(c, err)
		return
	}

	response.OK(c, nil)
}

// closeGenTask 关闭任务
// @Summary 关闭需求智生任务
// @Tags 需求智生-任务
// @Produce json
// @Param projectID path int true "项目ID"
// @Param taskID path int true "任务ID"
// @Success 200 {object} response.Result
// @Router /api/v1/projects/{projectID}/requirement-gen/tasks/{taskID}/close [post]
func (a *API) closeGenTask(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	taskID, ok := parseUintParam(c, "taskID")
	if !ok {
		return
	}

	err := a.reqGenTaskSvc.CloseTask(c.Request.Context(), projectID, taskID, user.ID)
	if err != nil {
		response.HandleError(c, err)
		return
	}

	response.OK(c, nil)
}

// ========== 智能生成接口 ==========

// smartGenerateRequest 智能生成请求
type smartGenerateRequest struct {
	RequirementDocID uint   `json:"requirement_doc_id" binding:"required,min=1"`
	TaskNamePrefix   string `json:"task_name_prefix" binding:"max=200"`
	TargetModuleID   uint   `json:"target_module_id"`
	DefaultLevel     string `json:"default_level" binding:"omitempty,oneof=P0 P1 P2 P3"`
	MaxCasesPerSkill int    `json:"max_cases_per_skill" binding:"omitempty,min=1,max=100"`
	ExtraPrompt      string `json:"extra_prompt" binding:"max=2000"`
}

// smartGenerate 智能生成：AI 自动分析需求 → 匹配 Skill → 批量创建任务
// @Summary 智能生成测试用例
// @Tags 需求智生-任务
// @Accept json
// @Produce json
// @Param projectID path int true "项目ID"
// @Param body body smartGenerateRequest true "请求体"
// @Success 201 {object} response.Result
// @Failure 400,429,503 {object} response.Result
// @Router /api/v1/projects/{projectID}/requirement-gen/smart-generate [post]
func (a *API) smartGenerate(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}

	var req smartGenerateRequest
	if !bindJSON(c, &req) {
		return
	}

	// 获取当前激活模型信息
	activeModel, modelErr := a.aiModelConfigSvc.GetActive(c.Request.Context())
	if modelErr != nil {
		response.Error(c, http.StatusBadRequest, service.CodeReqGenNoActiveModel, "未配置激活的 AI 模型")
		return
	}

	result, err := a.reqGenTaskSvc.SmartGenerate(c.Request.Context(), service.SmartGenerateInput{
		ProjectID:        projectID,
		RequirementDocID: req.RequirementDocID,
		AIModelConfigID:  activeModel.ID,
		AIModelSnapshot:  activeModel.Provider + "/" + activeModel.ModelID,
		TaskNamePrefix:   req.TaskNamePrefix,
		TargetModuleID:   req.TargetModuleID,
		DefaultLevel:     req.DefaultLevel,
		MaxCasesPerSkill: req.MaxCasesPerSkill,
		ExtraPrompt:      req.ExtraPrompt,
		CreatedBy:        user.ID,
	})
	if err != nil {
		response.HandleError(c, err)
		return
	}

	response.Created(c, result)
}

// ========== 内部回调接口 ==========

// genTaskCallback Executor 任务完成回调
// @Summary 生成任务回调（内部）
// @Tags 需求智生-内部
// @Accept json
// @Produce json
// @Param taskID path int true "任务ID"
// @Param body body taskCallbackRequest true "回调数据"
// @Success 200 {object} response.Result
// @Router /internal/requirement-gen/tasks/{taskID}/callback [post]
func (a *API) genTaskCallback(c *gin.Context) {
	taskID, ok := parseUintParam(c, "taskID")
	if !ok {
		return
	}

	var req taskCallbackRequest
	if !bindJSON(c, &req) {
		return
	}

	ctx := c.Request.Context()
	var err error
	switch req.Status {
	case "success":
		items := make([]service.CallbackResultItem, 0, len(req.Results))
		for _, r := range req.Results {
			items = append(items, service.CallbackResultItem{
				SeqNo:         r.SeqNo,
				Title:         r.Title,
				Level:         r.Level,
				Precondition:  r.Precondition,
				Steps:         r.Steps,
				Postcondition: r.Postcondition,
				Remark:        r.Remark,
				TagsSuggested: r.TagsSuggested,
				AIConfidence:  r.AIConfidence,
				RawJSON:       r.RawJSON,
			})
		}
		err = a.reqGenTaskSvc.CallbackSuccess(ctx, service.CallbackSuccessInput{
			TaskID:           taskID,
			GeneratedCount:   req.GeneratedCount,
			PromptTokens:     req.PromptTokens,
			CompletionTokens: req.CompletionTokens,
			DurationMs:       req.DurationMs,
			Results:          items,
		})
	case "failed":
		err = a.reqGenTaskSvc.CallbackFail(ctx, service.CallbackFailInput{
			TaskID:     taskID,
			FailReason: req.FailReason,
		})
	}

	if err != nil {
		response.HandleError(c, err)
		return
	}

	response.OK(c, nil)
}

// genTaskHeartbeat Executor 心跳上报
// @Summary 任务心跳（内部）
// @Tags 需求智生-内部
// @Produce json
// @Param taskID path int true "任务ID"
// @Success 200 {object} response.Result
// @Router /internal/requirement-gen/tasks/{taskID}/heartbeat [post]
func (a *API) genTaskHeartbeat(c *gin.Context) {
	taskID, ok := parseUintParam(c, "taskID")
	if !ok {
		return
	}

	if err := a.reqGenTaskSvc.Heartbeat(c.Request.Context(), taskID); err != nil {
		response.HandleError(c, err)
		return
	}

	response.OK(c, nil)
}

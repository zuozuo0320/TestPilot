// handler_testcase.go 鈥?鐢ㄤ緥绠＄悊 Handler锛堝惈鎵归噺鎿嶄綔銆佸厠闅嗐€佸巻鍙层€佸叧鑱旓級
package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"testpilot/internal/dto/response"
	"testpilot/internal/model"
	"testpilot/internal/repository"
	"testpilot/internal/service"
)

func (a *API) createTestCase(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	var req createTestCaseRequest
	if !bindJSON(c, &req) {
		return
	}
	tc, err := a.testCaseSvc.Create(c.Request.Context(), projectID, user.ID, service.CreateTestCaseInput{
		Title:        strings.TrimSpace(req.Title),
		Level:        req.Level,
		ExecResult:   req.ExecResult,
		ModuleID:     req.ModuleID,
		ModulePath:   req.ModulePath,
		Tags:         req.Tags,
		TagIDs:       req.TagIDs,
		Precondition:  req.Precondition,
		Postcondition: req.Postcondition,
		Steps:         req.Steps,
		Remark:        req.Remark,
		Priority:      req.Priority,
	})
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.Created(c, tc)
}

func (a *API) listTestCases(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	filter := repository.TestCaseFilter{
		Page:          parsePositiveIntWithDefault(c.Query("page"), 1),
		PageSize:      parsePositiveIntWithDefault(c.Query("pageSize"), 20),
		Keyword:       strings.TrimSpace(c.Query("keyword")),
		Level:         c.Query("level"),
		ReviewResult:  c.Query("review_result"),
		ExecResult:    c.Query("exec_result"),
		Tags:          c.Query("tags"),
		ModulePath:    strings.TrimSpace(c.Query("module_path")),
		CreatedAfter:  c.Query("created_after"),
		CreatedBefore: c.Query("created_before"),
		UpdatedAfter:  c.Query("updated_after"),
		UpdatedBefore: c.Query("updated_before"),
		SortBy:        c.Query("sortBy"),
		SortOrder:     c.Query("sortOrder"),
	}
	// Optional module_id filter
	if mid := c.Query("module_id"); mid != "" {
		if v, err := strconv.ParseUint(mid, 10, 64); err == nil {
			moduleID := uint(v)
			filter.ModuleID = &moduleID
		}
	}
	// Optional created_by filter
	if cid := c.Query("created_by"); cid != "" {
		if v, err := strconv.ParseUint(cid, 10, 64); err == nil {
			createdBy := uint(v)
			filter.CreatedByID = &createdBy
		}
	}
	// Optional updated_by filter
	if uid := c.Query("updated_by"); uid != "" {
		if v, err := strconv.ParseUint(uid, 10, 64); err == nil {
			updatedBy := uint(v)
			filter.UpdatedByID = &updatedBy
		}
	}
	// Optional tag_ids filter (comma-separated, ignore invalid)
	if tagIDsStr := c.Query("tag_ids"); tagIDsStr != "" {
		for _, s := range strings.Split(tagIDsStr, ",") {
			if v, err := strconv.ParseUint(strings.TrimSpace(s), 10, 64); err == nil && v > 0 {
				filter.TagIDs = append(filter.TagIDs, uint(v))
			}
		}
	}
	items, total, err := a.testCaseSvc.ListPaged(c.Request.Context(), projectID, filter)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.Page(c, items, total, filter.Page, filter.PageSize)
}

// getTestCase 按 ID 查询单条用例（用于评审详情页回显 steps/precondition/postcondition）
func (a *API) getTestCase(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	tcID, ok := parseUintParam(c, "testcaseID")
	if !ok {
		return
	}
	tc, err := a.testCaseSvc.FindByID(c.Request.Context(), tcID, projectID)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, tc)
}

func (a *API) updateTestCase(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	tcID, ok := parseUintParam(c, "testcaseID")
	if !ok {
		return
	}
	var req updateTestCaseRequest
	if !bindJSON(c, &req) {
		return
	}
	updated, err := a.testCaseSvc.Update(c.Request.Context(), projectID, tcID, user.ID, service.UpdateTestCaseInput{
		Title:        req.Title,
		Level:        req.Level,
		ExecResult:   req.ExecResult,
		ModuleID:     req.ModuleID,
		ModulePath:   req.ModulePath,
		Tags:         req.Tags,
		TagIDs:       req.TagIDs,
		Precondition:  req.Precondition,
		Postcondition: req.Postcondition,
		Steps:         req.Steps,
		Remark:        req.Remark,
		Priority:      req.Priority,
	})
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, updated)
}

func (a *API) deleteTestCase(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	tcID, ok := parseUintParam(c, "testcaseID")
	if !ok {
		return
	}
	if err := a.testCaseSvc.Delete(c.Request.Context(), projectID, tcID); err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, gin.H{"deleted": true})
}

// ========== 鎵归噺鎿嶄綔 ==========

func (a *API) batchDeleteTestCases(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	var req batchDeleteRequest
	if !bindJSON(c, &req) {
		return
	}
	result, err := a.testCaseSvc.BatchDelete(c.Request.Context(), projectID, req.IDs)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, result)
}

func (a *API) batchUpdateLevel(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	var req batchUpdateLevelRequest
	if !bindJSON(c, &req) {
		return
	}
	affected, err := a.testCaseSvc.BatchUpdateLevel(c.Request.Context(), projectID, req.IDs, req.Level)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, gin.H{"affected": affected})
}

func (a *API) batchMoveTestCases(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	var req batchMoveRequest
	if !bindJSON(c, &req) {
		return
	}
	affected, err := a.testCaseSvc.BatchMove(c.Request.Context(), projectID, req.IDs, req.ModuleID, req.ModulePath)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, gin.H{"affected": affected})
}

// batchTagTestCases 批量为用例打标签
func (a *API) batchTagTestCases(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	var req batchTagRequest
	if !bindJSON(c, &req) {
		return
	}
	affected, err := a.testCaseSvc.BatchTag(c.Request.Context(), projectID, req.IDs, req.TagIDs)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, gin.H{"affected": affected})
}

// ========== 鐢ㄤ緥鍏嬮殕 ==========

func (a *API) cloneTestCase(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	tcID, ok := parseUintParam(c, "testcaseID")
	if !ok {
		return
	}
	cloned, err := a.testCaseSvc.CloneCase(c.Request.Context(), projectID, tcID, user.ID)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.Created(c, cloned)
}

// ========== 缂栬緫鍘嗗彶 ==========

func (a *API) listCaseHistory(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	tcID, ok := parseUintParam(c, "testcaseID")
	if !ok {
		return
	}
	page := parsePositiveIntWithDefault(c.Query("page"), 1)
	pageSize := parsePositiveIntWithDefault(c.Query("pageSize"), 20)
	items, total, err := a.caseHistoryRepo.ListByCaseID(tcID, page, pageSize)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.Page(c, items, total, page, pageSize)
}

// ========== 鐢ㄤ緥娲诲姩娴?==========

type activityItem struct {
	ID        uint      `json:"id"`
	ActorName string    `json:"actor_name"`
	Action    string    `json:"action"`
	Detail    string    `json:"detail"`
	Icon      string    `json:"icon"`
	CreatedAt time.Time `json:"created_at"`
}

func (a *API) listCaseActivities(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	tcID, ok := parseUintParam(c, "testcaseID")
	if !ok {
		return
	}
	limit := parsePositiveIntWithDefault(c.Query("limit"), 10)
	if limit > 50 {
		limit = 50
	}

	var activities []activityItem

	// 1. AuditLog for this testcase
	auditLogs, _ := a.auditSvc.ListByTarget(c.Request.Context(), "testcase", tcID, limit)
	for _, l := range auditLogs {
		activities = append(activities, activityItem{
			ID:        l.ID,
			ActorName: "", // fill later
			Action:    mapAuditAction(l.Action),
			Detail:    mapAuditDetail(l.Action, l.BeforeData, l.AfterData),
			Icon:      mapAuditIcon(l.Action),
			CreatedAt: l.CreatedAt,
		})
	}

	// 2. CaseHistory for field-level changes
	historyItems, _, _ := a.caseHistoryRepo.ListByCaseID(tcID, 1, limit)
	for _, h := range historyItems {
		activities = append(activities, activityItem{
			ID:        10000000 + h.ID, // offset to avoid ID collision
			ActorName: "",
			Action:    mapHistoryAction(h.Action, h.FieldName),
			Detail:    mapHistoryDetail(h.Action, h.FieldName, h.OldValue, h.NewValue),
			Icon:      mapHistoryIcon(h.Action),
			CreatedAt: h.CreatedAt,
		})
	}

	// Sort by time desc
	sort.Slice(activities, func(i, j int) bool {
		return activities[i].CreatedAt.After(activities[j].CreatedAt)
	})
	if len(activities) > limit {
		activities = activities[:limit]
	}

	// Batch fill actor names
	userIDSet := map[uint]bool{}
	for _, l := range auditLogs {
		userIDSet[l.ActorID] = true
	}
	for _, h := range historyItems {
		userIDSet[h.ChangedBy] = true
	}
	ids := make([]uint, 0, len(userIDSet))
	for id := range userIDSet {
		ids = append(ids, id)
	}
	userMap := map[uint]string{}
	if len(ids) > 0 {
		users, _ := a.userSvc.FindByIDs(c.Request.Context(), ids)
		for _, u := range users {
			userMap[u.ID] = u.Name
		}
	}
	// Map actor names back
	auditMap := map[uint]uint{}
	for _, l := range auditLogs {
		auditMap[l.ID] = l.ActorID
	}
	historyMap := map[uint]uint{}
	for _, h := range historyItems {
		historyMap[10000000+h.ID] = h.ChangedBy
	}
	for i := range activities {
		if uid, ok := auditMap[activities[i].ID]; ok {
			activities[i].ActorName = userMap[uid]
		} else if uid, ok := historyMap[activities[i].ID]; ok {
			activities[i].ActorName = userMap[uid]
		}
		if activities[i].ActorName == "" {
			activities[i].ActorName = "系统"
		}
	}

	response.OK(c, activities)
}

func mapAuditAction(action string) string {
	switch action {
	case "create":
		return "创建了用例"
	case "update":
		return "更新了用例"
	case "clone":
		return "克隆了用例"
	case "discard":
		return "废弃了用例"
	case "recover":
		return "恢复了用例"
	case "delete":
		return "删除了用例"
	default:
		return action
	}
}

func mapAuditDetail(action, before, after string) string {
	switch action {
	case "clone":
		return "来源: " + before
	default:
		return ""
	}
}

func mapAuditIcon(action string) string {
	switch action {
	case "create":
		return "add_circle"
	case "update":
		return "edit"
	case "clone":
		return "content_copy"
	case "discard":
		return "delete"
	case "recover":
		return "restore"
	default:
		return "info"
	}
}

func mapHistoryAction(action, fieldName string) string {
	switch action {
	case "version_bump":
		return "版本升级"
	default:
		if fieldName != "" {
			return "更新了 " + fieldName
		}
		return action
	}
}

func mapHistoryDetail(action, fieldName, oldValue, newValue string) string {
	if action == "version_bump" && newValue != "" {
		return newValue
	}
	return ""
}

func mapHistoryIcon(action string) string {
	switch action {
	case "version_bump":
		return "history"
	default:
		return "edit"
	}
}

// ========== 鐢ㄤ緥鍏宠仈 ==========

func (a *API) listCaseRelations(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	tcID, ok := parseUintParam(c, "testcaseID")
	if !ok {
		return
	}
	relations, err := a.caseRelationRepo.ListByCaseID(tcID)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, relations)
}

func (a *API) createCaseRelation(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	tcID, ok := parseUintParam(c, "testcaseID")
	if !ok {
		return
	}
	var req createRelationRequest
	if !bindJSON(c, &req) {
		return
	}
	rel := &model.CaseRelation{
		SourceCaseID: tcID,
		TargetCaseID: req.TargetCaseID,
		RelationType: req.RelationType,
		CreatedBy:    user.ID,
	}
	if err := a.caseRelationRepo.Create(rel); err != nil {
		response.HandleError(c, err)
		return
	}
	response.Created(c, rel)
}

func (a *API) deleteCaseRelation(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	relID, err := strconv.ParseUint(c.Param("relationID"), 10, 64)
	if err != nil {
		response.Error(c, 400, service.CodeParamsError, "invalid relation ID")
		return
	}
	if err := a.caseRelationRepo.Delete(uint(relID)); err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, gin.H{"deleted": true})
}

// ========== 状态流转(废弃/恢复) ==========

// discardTestCase 搴熷純鐢ㄤ緥
func (a *API) discardTestCase(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	// 鏉冮檺锛氫粎闄愰」鐩鐞嗗憳鎴栫郴缁熺鐞嗗憳
	if !requireRole(c, user, model.GlobalRoleAdmin, model.GlobalRoleManager) {
		return
	}
	tcID, ok := parseUintParam(c, "testcaseID")
	if !ok {
		return
	}

	if err := a.testCaseSvc.Discard(c.Request.Context(), projectID, tcID, user.ID); err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, "discarded")
}

// recoverTestCase 鎭㈠鐢ㄤ緥
func (a *API) recoverTestCase(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	// 鏉冮檺锛氫粎闄愰」鐩鐞嗗憳鎴栫郴缁熺鐞嗗憳
	if !requireRole(c, user, model.GlobalRoleAdmin, model.GlobalRoleManager) {
		return
	}
	tcID, ok := parseUintParam(c, "testcaseID")
	if !ok {
		return
	}

	if err := a.testCaseSvc.Recover(c.Request.Context(), projectID, tcID, user.ID); err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, "recovered")
}

// analyzeTestCase 代理到 Executor 的 AI 用例质量分析接口
func (a *API) analyzeTestCase(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	tcID, ok := parseUintParam(c, "testcaseID")
	if !ok {
		return
	}

	// 查询用例获取内容
	tc, err := a.testCaseSvc.FindByID(c.Request.Context(), tcID, projectID)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	if tc == nil {
		response.Error(c, 404, 404000, "用例不存在")
		return
	}

	// 构建 Executor 请求
	payload := map[string]string{
		"title":         tc.Title,
		"precondition":  tc.Precondition,
		"postcondition": tc.Postcondition,
		"steps":         tc.Steps,
	}
	body, _ := json.Marshal(payload)

	executorURL := strings.TrimRight(a.executorURL, "/") + "/api/testcase/analyze"
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, executorURL, bytes.NewReader(body))
	if err != nil {
		response.Error(c, 500, 500000, fmt.Sprintf("构建请求失败: %v", err))
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if a.executorAPIKey != "" {
		req.Header.Set("X-API-Key", a.executorAPIKey)
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		response.Error(c, 502, 502000, fmt.Sprintf("AI 服务请求失败: %v", err))
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		response.Error(c, resp.StatusCode, 502000, fmt.Sprintf("AI 服务返回错误: %s", string(respBody)))
		return
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		response.Error(c, 500, 500000, "解析 AI 响应失败")
		return
	}
	response.OK(c, result)
}

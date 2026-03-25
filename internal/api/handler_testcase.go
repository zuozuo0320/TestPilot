// handler_testcase.go — 用例管理 Handler（含批量操作、克隆、历史、关联）
package api

import (
	"strconv"
	"strings"

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
		ReviewResult: req.ReviewResult,
		ExecResult:   req.ExecResult,
		ModuleID:     req.ModuleID,
		ModulePath:   req.ModulePath,
		Tags:         req.Tags,
		Precondition: req.Precondition,
		Steps:        req.Steps,
		Remark:       req.Remark,
		Priority:     req.Priority,
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
	items, total, err := a.testCaseSvc.ListPaged(c.Request.Context(), projectID, filter)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.Page(c, items, total, filter.Page, filter.PageSize)
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
	updated, err := a.testCaseSvc.Update(c.Request.Context(), projectID, tcID, service.UpdateTestCaseInput{
		Title:        req.Title,
		Level:        req.Level,
		ReviewResult: req.ReviewResult,
		ExecResult:   req.ExecResult,
		ModuleID:     req.ModuleID,
		ModulePath:   req.ModulePath,
		Tags:         req.Tags,
		Precondition: req.Precondition,
		Steps:        req.Steps,
		Remark:       req.Remark,
		Priority:     req.Priority,
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

// ========== 批量操作 ==========

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
	affected, err := a.testCaseSvc.BatchDelete(c.Request.Context(), projectID, req.IDs)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, gin.H{"affected": affected})
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

// ========== 用例克隆 ==========

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

// ========== 编辑历史 ==========

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

// ========== 用例关联 ==========

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
		response.Error(c, 400, "invalid relation ID")
		return
	}
	if err := a.caseRelationRepo.Delete(uint(relID)); err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, gin.H{"deleted": true})
}

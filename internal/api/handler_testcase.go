// handler_testcase.go — 用例管理 Handler
package api

import (
	"strings"

	"github.com/gin-gonic/gin"

	"testpilot/internal/dto/response"
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
		ModulePath:   req.ModulePath,
		Tags:         req.Tags,
		Steps:        req.Steps,
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
		Page:         parsePositiveIntWithDefault(c.Query("page"), 1),
		PageSize:     parsePositiveIntWithDefault(c.Query("pageSize"), 20),
		Keyword:      strings.TrimSpace(c.Query("keyword")),
		Level:        c.Query("level"),
		ReviewResult: c.Query("review_result"),
		ExecResult:   c.Query("exec_result"),
		SortBy:       c.Query("sortBy"),
		SortOrder:    c.Query("sortOrder"),
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
		ModulePath:   req.ModulePath,
		Tags:         req.Tags,
		Steps:        req.Steps,
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

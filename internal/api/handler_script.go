// handler_script.go — 脚本管理与关联 Handler
package api

import (
	"github.com/gin-gonic/gin"

	"testpilot/internal/dto/response"
)

func (a *API) createScript(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	var req createScriptRequest
	if !bindJSON(c, &req) {
		return
	}
	created, err := a.scriptSvc.Create(c.Request.Context(), projectID, req.Name, req.Path, req.Type)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.Created(c, created)
}

func (a *API) listScripts(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	scripts, err := a.scriptSvc.List(c.Request.Context(), projectID)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, scripts)
}

func (a *API) linkRequirementAndTestCase(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	requirementID, ok := parseUintParam(c, "requirementID")
	if !ok {
		return
	}
	testCaseID, ok := parseUintParam(c, "testcaseID")
	if !ok {
		return
	}
	if err := a.requirementSvc.LinkTestCase(c.Request.Context(), projectID, requirementID, testCaseID); err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, gin.H{"linked": true})
}

func (a *API) linkTestCaseAndScript(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	testCaseID, ok := parseUintParam(c, "testcaseID")
	if !ok {
		return
	}
	scriptID, ok := parseUintParam(c, "scriptID")
	if !ok {
		return
	}
	if err := a.scriptSvc.LinkTestCase(c.Request.Context(), projectID, testCaseID, scriptID); err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, gin.H{"linked": true})
}

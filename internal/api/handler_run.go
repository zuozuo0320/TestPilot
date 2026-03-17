// handler_run.go — 执行管理 Handler
package api

import (
	"strings"

	"github.com/gin-gonic/gin"

	"testpilot/internal/dto/response"
)

func (a *API) createRun(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	var req createRunRequest
	if !bindJSON(c, &req) {
		return
	}
	result, err := a.executionSvc.CreateRun(c.Request.Context(), projectID, user.ID, strings.TrimSpace(req.Mode), req.ScriptID, req.ScriptIDs)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.Created(c, result)
}

func (a *API) listRunResults(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	runID, ok := parseUintParam(c, "runID")
	if !ok {
		return
	}
	run, results, err := a.executionSvc.ListResults(c.Request.Context(), runID, projectID)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, gin.H{"run": run, "results": results})
}

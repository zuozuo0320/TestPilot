// handler_defect.go — 缺陷管理 Handler
package api

import (
	"github.com/gin-gonic/gin"

	"testpilot/internal/dto/response"
)

func (a *API) createDefect(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	var req createDefectRequest
	if !bindJSON(c, &req) {
		return
	}
	created, err := a.defectSvc.Create(c.Request.Context(), projectID, user.ID, req.RunResultID, req.Title, req.Description, req.Severity)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.Created(c, created)
}

func (a *API) listDefects(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	defects, err := a.defectSvc.List(c.Request.Context(), projectID)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, defects)
}

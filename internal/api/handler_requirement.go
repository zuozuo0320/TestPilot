// handler_requirement.go — 需求管理 Handler
package api

import (
	"github.com/gin-gonic/gin"

	"testpilot/internal/dto/response"
)

func (a *API) createRequirement(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	var req createRequirementRequest
	if !bindJSON(c, &req) {
		return
	}
	created, err := a.requirementSvc.Create(c.Request.Context(), projectID, req.Title, req.Content)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.Created(c, created)
}

func (a *API) listRequirements(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	reqs, err := a.requirementSvc.List(c.Request.Context(), projectID)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, reqs)
}

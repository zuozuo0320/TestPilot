// handler_overview.go — 项目概览与 WebHook Handler
package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"testpilot/internal/dto/response"
)

func (a *API) projectDemoOverview(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	overview, err := a.overviewSvc.GetOverview(c.Request.Context(), projectID)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, overview)
}

func (a *API) mockGitLabWebhook(c *gin.Context) {
	var payload map[string]any
	if err := c.ShouldBindJSON(&payload); err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	response.OK(c, gin.H{"received": true, "payload": payload})
}

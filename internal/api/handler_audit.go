// handler_audit.go — 审计日志 Handler
package api

import (
	"github.com/gin-gonic/gin"

	"testpilot/internal/dto/response"
	"testpilot/internal/model"
)

func (a *API) listAuditLogs(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleAdmin) {
		return
	}
	logs, err := a.auditSvc.ListRecent(c.Request.Context(), 100)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, gin.H{"logs": logs})
}

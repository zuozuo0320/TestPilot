// handler_role.go — 角色管理 Handler
package api

import (
	"github.com/gin-gonic/gin"

	"testpilot/internal/dto/response"
	"testpilot/internal/model"
)

func (a *API) listRoles(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleAdmin, model.GlobalRoleManager) {
		return
	}
	roles, err := a.roleSvc.List(c.Request.Context())
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, roles)
}

func (a *API) createRole(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleAdmin) {
		return
	}
	var req createRoleRequest
	if !bindJSON(c, &req) {
		return
	}
	role, err := a.roleSvc.Create(c.Request.Context(), user.ID, req.Name, req.DisplayName, req.Description)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.Created(c, role)
}

func (a *API) updateRole(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleAdmin) {
		return
	}
	roleID, ok := parseUintParam(c, "roleID")
	if !ok {
		return
	}
	var req updateRoleRequest
	if !bindJSON(c, &req) {
		return
	}
	updated, err := a.roleSvc.Update(c.Request.Context(), user.ID, roleID, req.Name, req.DisplayName, req.Description)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, updated)
}

func (a *API) deleteRole(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleAdmin) {
		return
	}
	roleID, ok := parseUintParam(c, "roleID")
	if !ok {
		return
	}
	if err := a.roleSvc.Delete(c.Request.Context(), user.ID, roleID); err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, gin.H{"deleted": true})
}

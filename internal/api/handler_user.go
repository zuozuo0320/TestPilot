// handler_user.go — 用户管理 Handler
package api

import (
	"strings"

	"github.com/gin-gonic/gin"

	"testpilot/internal/dto/response"
	"testpilot/internal/model"
	"testpilot/internal/service"
)

func (a *API) listUsers(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleAdmin, model.GlobalRoleManager) {
		return
	}
	users, err := a.userSvc.List(c.Request.Context())
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, users)
}

func (a *API) createUser(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleAdmin) {
		return
	}
	var req createUserRequest
	if !bindJSON(c, &req) {
		return
	}
	created, err := a.userSvc.Create(c.Request.Context(), user.ID, service.CreateUserInput{
		Name:       strings.TrimSpace(req.Name),
		Email:      strings.ToLower(strings.TrimSpace(req.Email)),
		Phone:      strings.TrimSpace(req.Phone),
		Role:       strings.TrimSpace(req.Role),
		RoleIDs:    req.RoleIDs,
		ProjectIDs: req.ProjectIDs,
	})
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.Created(c, created)
}

func (a *API) updateUser(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleAdmin) {
		return
	}
	userID, ok := parseUintParam(c, "userID")
	if !ok {
		return
	}
	var req updateUserRequest
	if !bindJSON(c, &req) {
		return
	}
	updated, err := a.userSvc.Update(c.Request.Context(), user.ID, userID, service.UpdateUserInput{
		Name:       req.Name,
		Email:      req.Email,
		Phone:      req.Phone,
		Avatar:     req.Avatar,
		Active:     req.Active,
		RoleIDs:    req.RoleIDs,
		ProjectIDs: req.ProjectIDs,
	})
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, updated)
}

func (a *API) deleteUser(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleAdmin) {
		return
	}
	userID, ok := parseUintParam(c, "userID")
	if !ok {
		return
	}
	if err := a.userSvc.Delete(c.Request.Context(), user.ID, userID); err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, gin.H{"deleted": true})
}

func (a *API) assignUserRoles(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleAdmin) {
		return
	}
	userID, ok := parseUintParam(c, "userID")
	if !ok {
		return
	}
	var req assignUserRolesRequest
	if !bindJSON(c, &req) {
		return
	}
	if err := a.userSvc.AssignRoles(c.Request.Context(), user.ID, userID, req.RoleIDs); err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, gin.H{"assigned": true})
}

func (a *API) assignUserProjects(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleAdmin) {
		return
	}
	userID, ok := parseUintParam(c, "userID")
	if !ok {
		return
	}
	var req assignUserProjectsRequest
	if !bindJSON(c, &req) {
		return
	}
	if err := a.userSvc.AssignProjects(c.Request.Context(), user.ID, userID, req.ProjectIDs); err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, gin.H{"assigned": true})
}

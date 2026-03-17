// handler_project.go — 项目管理 Handler
package api

import (
	"github.com/gin-gonic/gin"

	"testpilot/internal/dto/response"
	"testpilot/internal/model"
)

func (a *API) createProject(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleAdmin, model.GlobalRoleManager) {
		return
	}
	var req createProjectRequest
	if !bindJSON(c, &req) {
		return
	}
	project, err := a.projectSvc.Create(c.Request.Context(), user.ID, req.Name, req.Description)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.Created(c, project)
}

func (a *API) listProjects(c *gin.Context) {
	user := currentUser(c)
	projects, err := a.projectSvc.List(c.Request.Context(), user)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, projects)
}

func (a *API) listProjectMembers(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	members, err := a.projectSvc.ListMembers(c.Request.Context(), projectID)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, members)
}

func (a *API) addProjectMember(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleAdmin, model.GlobalRoleManager) {
		return
	}
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	var req addMemberRequest
	if !bindJSON(c, &req) {
		return
	}
	member, err := a.projectSvc.AddMember(c.Request.Context(), projectID, req.UserID, req.Role)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.Created(c, member)
}

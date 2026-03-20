// handler_module.go — 用例目录模块 Handler
package api

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"testpilot/internal/dto/response"
)

func (a *API) listModules(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	tree, err := a.moduleSvc.GetTree(projectID)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, tree)
}

func (a *API) createModule(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	var req struct {
		ParentID uint   `json:"parent_id"`
		Name     string `json:"name"`
	}
	if !bindJSON(c, &req) {
		return
	}
	m, err := a.moduleSvc.Create(projectID, req.ParentID, strings.TrimSpace(req.Name))
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.Created(c, m)
}

func (a *API) renameModule(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	moduleID, err := strconv.ParseUint(c.Param("moduleID"), 10, 64)
	if err != nil {
		response.Error(c, 400, "invalid module ID")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !bindJSON(c, &req) {
		return
	}
	m, err2 := a.moduleSvc.Rename(uint(moduleID), strings.TrimSpace(req.Name))
	if err2 != nil {
		response.HandleError(c, err2)
		return
	}
	response.OK(c, m)
}

func (a *API) moveModule(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	moduleID, err := strconv.ParseUint(c.Param("moduleID"), 10, 64)
	if err != nil {
		response.Error(c, 400, "invalid module ID")
		return
	}
	var req struct {
		ParentID  uint `json:"parent_id"`
		SortOrder int  `json:"sort_order"`
	}
	if !bindJSON(c, &req) {
		return
	}
	if err := a.moduleSvc.Move(uint(moduleID), req.ParentID, req.SortOrder); err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, gin.H{"moved": true})
}

func (a *API) deleteModule(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	moduleID, err := strconv.ParseUint(c.Param("moduleID"), 10, 64)
	if err != nil {
		response.Error(c, 400, "invalid module ID")
		return
	}
	if err := a.moduleSvc.Delete(uint(moduleID)); err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, gin.H{"deleted": true})
}

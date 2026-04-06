// handler_project.go — 项目管理 Handler
package api

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"testpilot/internal/dto/response"
	"testpilot/internal/model"
	"testpilot/internal/service"
)

// createProject 创建项目（admin/manager 可操作）
func (a *API) createProject(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleAdmin, model.GlobalRoleManager) {
		return
	}
	var req createProjectRequest
	if !bindJSON(c, &req) {
		return
	}
	project, err := a.projectSvc.Create(c.Request.Context(), user.ID, service.CreateProjectInput{
		Name:        req.Name,
		Description: req.Description,
		Avatar:      req.Avatar,
		OwnerID:     req.OwnerID,
	})
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.Created(c, project)
}

// updateProject 更新项目（admin/manager 可操作，归档项目不可编辑）
func (a *API) updateProject(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleAdmin, model.GlobalRoleManager) {
		return
	}
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	var req updateProjectRequest
	if !bindJSON(c, &req) {
		return
	}
	updated, err := a.projectSvc.Update(c.Request.Context(), user.ID, projectID, service.UpdateProjectInput{
		Name:        req.Name,
		Description: req.Description,
		Avatar:      req.Avatar,
		OwnerID:     req.OwnerID,
	})
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, updated)
}

// listProjects 获取项目列表（admin 看全部，其他看自己的）
func (a *API) listProjects(c *gin.Context) {
	user := currentUser(c)
	projects, err := a.projectSvc.List(c.Request.Context(), user)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, projects)
}

// archiveProject 归档项目（admin/manager 可操作，种子项目不可归档）
func (a *API) archiveProject(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleAdmin, model.GlobalRoleManager) {
		return
	}
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if err := a.projectSvc.Archive(c.Request.Context(), user.ID, projectID); err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, gin.H{"archived": true})
}

// restoreProject 恢复已归档项目（仅 admin 可操作）
func (a *API) restoreProject(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleAdmin) {
		return
	}
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if err := a.projectSvc.Restore(c.Request.Context(), user.ID, projectID); err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, gin.H{"restored": true})
}

// deleteProject 删除项目（仅 admin，须已归档+无数据+非种子项目）
func (a *API) deleteProject(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleAdmin) {
		return
	}
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if err := a.projectSvc.Delete(c.Request.Context(), user.ID, projectID); err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, gin.H{"deleted": true})
}

// listProjectMembers 获取项目成员列表
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

// addProjectMember 添加项目成员（admin/manager 可操作）
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

// removeProjectMember 移除项目成员（admin/manager 可操作）
func (a *API) removeProjectMember(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleAdmin, model.GlobalRoleManager) {
		return
	}
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	memberUserID, ok := parseUintParam(c, "userID")
	if !ok {
		return
	}
	if err := a.projectSvc.RemoveMember(c.Request.Context(), projectID, memberUserID); err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, gin.H{"removed": true})
}

// uploadProjectAvatar 上传项目头像（admin/manager 可操作）
func (a *API) uploadProjectAvatar(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleAdmin, model.GlobalRoleManager) {
		return
	}
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	file, header, err := c.Request.FormFile("avatar")
	if err != nil {
		response.Error(c, http.StatusBadRequest, service.CodeParamsError, "avatar file is required")
		return
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext != ".png" && ext != ".jpg" && ext != ".jpeg" && ext != ".gif" && ext != ".webp" {
		response.Error(c, http.StatusBadRequest, service.CodeParamsError, "unsupported image format")
		return
	}
	if header.Size > 2*1024*1024 {
		response.Error(c, http.StatusBadRequest, service.CodeParamsError, "file too large (max 2MB)")
		return
	}

	dir := "uploads/projects"
	os.MkdirAll(dir, 0o755)
	filename := fmt.Sprintf("project_%d_%d%s", projectID, time.Now().UnixMilli(), ext)
	dst, err := os.Create(filepath.Join(dir, filename))
	if err != nil {
		response.Error(c, http.StatusInternalServerError, service.CodeInternal, "failed to save file")
		return
	}
	defer dst.Close()
	if _, err := io.Copy(dst, file); err != nil {
		response.Error(c, http.StatusInternalServerError, service.CodeInternal, "failed to save file")
		return
	}

	avatarURL := "/" + dir + "/" + filename
	avatarPtr := &avatarURL
	if _, err := a.projectSvc.Update(c.Request.Context(), user.ID, projectID, service.UpdateProjectInput{
		Avatar: avatarPtr,
	}); err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, gin.H{"avatar": avatarURL})
}

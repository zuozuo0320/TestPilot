// handler_user.go — 用户管理 Handler
package api

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"testpilot/internal/dto/response"
	"testpilot/internal/model"
	"testpilot/internal/repository"
	"testpilot/internal/service"
)

// userDetailResp 用户列表返回结构体（含关联角色/项目 ID 列表和角色名称）
type userDetailResp struct {
	model.User
	RoleIDs    []uint   `json:"role_ids"`
	ProjectIDs []uint   `json:"project_ids"`
	RoleNames  []string `json:"role_names"`
}

// listUsers 获取用户列表（admin/manager 可访问）
// 支持 query 参数：keyword（姓名/邮箱模糊搜索）、role_id（角色筛选）、status（active/disabled）
// 返回结果包含 role_ids、project_ids 和 role_names，供前端展示和编辑弹窗使用
func (a *API) listUsers(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleAdmin, model.GlobalRoleManager) {
		return
	}

	// 解析筛选参数
	filter := repository.UserListFilter{
		Keyword: c.Query("keyword"),
		Status:  c.Query("status"),
	}
	if roleIDStr := c.Query("role_id"); roleIDStr != "" {
		if rid, err := strconv.ParseUint(roleIDStr, 10, 64); err == nil {
			filter.RoleID = uint(rid)
		}
	}

	// 判断是否有筛选条件，有则走筛选查询，无则走全量查询
	ctx := c.Request.Context()
	var users []model.User
	var err error
	if filter.Keyword != "" || filter.RoleID > 0 || filter.Status != "" {
		users, err = a.userSvc.ListFiltered(ctx, filter)
	} else {
		users, err = a.userSvc.List(ctx)
	}
	if err != nil {
		response.HandleError(c, err)
		return
	}

	// 预加载所有角色列表，构建 ID→显示名映射
	allRoles, _ := a.roleSvc.List(ctx)
	roleDisplayMap := make(map[uint]string, len(allRoles))
	for _, r := range allRoles {
		displayName := r.DisplayName
		if displayName == "" {
			displayName = r.Name
		}
		roleDisplayMap[r.ID] = displayName
	}

	// 逐用户填充 role_ids、project_ids 和 role_names
	result := make([]userDetailResp, 0, len(users))
	for _, u := range users {
		roleIDs, _ := a.userSvc.GetRoleIDs(ctx, u.ID)
		projectIDs, _ := a.userSvc.GetProjectIDs(ctx, u.ID)
		if roleIDs == nil {
			roleIDs = []uint{}
		}
		if projectIDs == nil {
			projectIDs = []uint{}
		}
		// 将角色 ID 映射为显示名
		roleNames := make([]string, 0, len(roleIDs))
		for _, rid := range roleIDs {
			if name, ok := roleDisplayMap[rid]; ok {
				roleNames = append(roleNames, name)
			}
		}
		result = append(result, userDetailResp{
			User:       u,
			RoleIDs:    roleIDs,
			ProjectIDs: projectIDs,
			RoleNames:  roleNames,
		})
	}
	response.OK(c, result)
}

// createUser 创建用户（仅 admin）
// 请求体需包含初始密码，密码须满足复杂度规则
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
		Password:   req.Password,
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

// updateUser 更新用户（仅 admin，邮箱不可修改）
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

// deleteUser 逻辑删除用户（仅 admin，admin 用户不可删除）
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

// resetPassword 管理员重置用户密码（FR-02-14）
func (a *API) resetPassword(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleAdmin) {
		return
	}
	userID, ok := parseUintParam(c, "userID")
	if !ok {
		return
	}
	var req resetPasswordRequest
	if !bindJSON(c, &req) {
		return
	}
	if err := a.userSvc.ResetPassword(c.Request.Context(), user.ID, userID, req.NewPassword); err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, gin.H{"reset": true})
}

// changePassword 用户修改自身密码（FR-02-13）
func (a *API) changePassword(c *gin.Context) {
	user := currentUser(c)
	var req changePasswordRequest
	if !bindJSON(c, &req) {
		return
	}
	if err := a.userSvc.ChangePassword(c.Request.Context(), user.ID, req.OldPassword, req.NewPassword); err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, gin.H{"changed": true})
}

// toggleActive 启用/禁用用户（FR-02-15）
func (a *API) toggleActive(c *gin.Context) {
	user := currentUser(c)
	if !requireRole(c, user, model.GlobalRoleAdmin) {
		return
	}
	userID, ok := parseUintParam(c, "userID")
	if !ok {
		return
	}
	var body struct {
		Active bool `json:"active"`
	}
	if !bindJSON(c, &body) {
		return
	}
	if err := a.userSvc.ToggleActive(c.Request.Context(), user.ID, userID, body.Active); err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, gin.H{"active": body.Active})
}

// assignUserRoles 分配用户角色
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

// assignUserProjects 分配用户项目
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

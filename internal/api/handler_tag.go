// handler_tag.go — 标签管理 Handler
//
// 提供标签模块的 RESTful 接口，包括：
//   - GET    /projects/:projectID/tags          分页列表
//   - GET    /projects/:projectID/tags/options   候选列表（轻量）
//   - POST   /projects/:projectID/tags          创建标签
//   - PUT    /projects/:projectID/tags/:tagID   更新标签
//   - DELETE /projects/:projectID/tags/:tagID   删除标签
//
// 每个 Handler 遵循统一流程：鉴权 → 参数解析 → 权限校验 → 调用 Service → 统一响应
package api

import (
	"github.com/gin-gonic/gin"

	"testpilot/internal/dto/response"
	"testpilot/internal/model"
	"testpilot/internal/repository"
	"testpilot/internal/service"
)

// ═══════ 标签列表 ═══════

// @Summary 标签列表
// @Description 获取项目下的标签列表，支持分页和关键词搜索
// @Tags Tag
// @Produce json
// @Param projectID path int true "项目 ID"
// @Param keyword query string false "名称模糊搜索"
// @Param page query int false "页码"
// @Param pageSize query int false "每页数量"
// @Param sortBy query string false "排序字段: case_count | name | created_at（默认 case_count）"
// @Success 200 {object} response.PageResult
// @Router /projects/{projectID}/tags [get]
func (a *API) listTags(c *gin.Context) {
	// 获取当前登录用户信息（由 JWT 中间件注入）
	user := currentUser(c)
	// 解析路径参数中的项目 ID
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	// 校验用户是否有该项目的访问权限
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}

	// 构建查询过滤器：关键词、分页、排序
	f := repository.TagFilter{
		Keyword:  c.Query("keyword"),
		Page:     parsePositiveIntWithDefault(c.Query("page"), 1),
		PageSize: parsePositiveIntWithDefault(c.Query("pageSize"), 20),
		SortBy:   c.Query("sortBy"),
	}

	// 调用 Service 层获取分页数据
	tags, total, err := a.tagSvc.ListPaged(c.Request.Context(), projectID, f)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	// 返回标准分页响应
	response.Page(c, tags, total, f.Page, f.PageSize)
}

// ═══════ 标签候选列表 ═══════

// @Summary 标签候选列表（轻量）
// @Description 用于标签选择器和筛选下拉，返回 id/name/color，不分页
// @Tags Tag
// @Produce json
// @Param projectID path int true "项目 ID"
// @Param keyword query string false "名称模糊搜索"
// @Success 200 {object} response.Result
// @Router /projects/{projectID}/tags/options [get]
func (a *API) listTagOptions(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}

	// 查询轻量标签列表（仅 id/name/color，不分页）
	options, err := a.tagSvc.ListOptions(c.Request.Context(), projectID, c.Query("keyword"))
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, options)
}

// ═══════ 创建标签 ═══════

// @Summary 创建标签
// @Description 在项目下创建新标签
// @Tags Tag
// @Accept json
// @Produce json
// @Param projectID path int true "项目 ID"
// @Param body body createTagRequest true "标签信息"
// @Success 201 {object} response.Result{data=model.Tag}
// @Failure 400 {object} response.Result
// @Failure 409 {object} response.Result
// @Router /projects/{projectID}/tags [post]
func (a *API) createTag(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	// 创建标签需要 manager 或 tester 角色
	if !requireRole(c, user, model.GlobalRoleManager, model.GlobalRoleTester) {
		return
	}

	// 解析请求体：名称、颜色、描述
	var req createTagRequest
	if !bindJSON(c, &req) {
		return
	}

	// 调用 Service 层创建标签（含参数校验、限额检查、重复名检查）
	tag, err := a.tagSvc.Create(c.Request.Context(), projectID, user.ID, service.CreateTagInput{
		Name:        req.Name,
		Color:       req.Color,
		Description: req.Description,
	})
	if err != nil {
		response.HandleError(c, err)
		return
	}
	// 返回 201 Created + 标签完整信息
	response.Created(c, tag)
}

// ═══════ 更新标签 ═══════

// @Summary 更新标签
// @Description 修改标签名称、颜色或描述
// @Tags Tag
// @Accept json
// @Produce json
// @Param projectID path int true "项目 ID"
// @Param tagID path int true "标签 ID"
// @Param body body updateTagRequest true "更新字段"
// @Success 200 {object} response.Result
// @Router /projects/{projectID}/tags/{tagID} [put]
func (a *API) updateTag(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	// 更新标签需要 manager 或 tester 角色
	if !requireRole(c, user, model.GlobalRoleManager, model.GlobalRoleTester) {
		return
	}
	tagID, ok := parseUintParam(c, "tagID")
	if !ok {
		return
	}

	// 解析请求体：名称、颜色、描述（均为指针类型，仅传递需要修改的字段）
	var req updateTagRequest
	if !bindJSON(c, &req) {
		return
	}

	// 调用 Service 层更新（含归属校验、参数校验、重复名检查）
	if err := a.tagSvc.Update(c.Request.Context(), projectID, tagID, user.ID, service.UpdateTagInput{
		Name:        req.Name,
		Color:       req.Color,
		Description: req.Description,
	}); err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, gin.H{"message": "ok"})
}

// ═══════ 删除标签 ═══════

// @Summary 删除标签
// @Description 删除标签并级联解除所有用例关联
// @Tags Tag
// @Produce json
// @Param projectID path int true "项目 ID"
// @Param tagID path int true "标签 ID"
// @Success 200 {object} response.Result
// @Router /projects/{projectID}/tags/{tagID} [delete]
func (a *API) deleteTag(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	// 删除标签需要 manager 或 tester 角色
	if !requireRole(c, user, model.GlobalRoleManager, model.GlobalRoleTester) {
		return
	}
	tagID, ok := parseUintParam(c, "tagID")
	if !ok {
		return
	}

	// Service 层在事务内解除关联 + 删除标签，返回被解绑的用例数
	unlinked, err := a.tagSvc.Delete(c.Request.Context(), projectID, tagID)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, gin.H{"unlinked_case_count": unlinked})
}

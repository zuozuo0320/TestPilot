// handler_xlsx.go — 用例导入导出 Handler
package api

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"testpilot/internal/dto/response"
	"testpilot/internal/repository"
	"testpilot/internal/service"
)

func (a *API) exportTestCasesXlsx(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}

	fileName := fmt.Sprintf("testcases_%d_%s.xlsx", projectID, time.Now().Format("20060102_150405"))
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fileName))

	if err := a.xlsxSvc.ExportToXlsx(c.Request.Context(), projectID, c.Writer); err != nil {
		response.Error(c, 500, service.CodeInternal, "export failed: "+err.Error())
		return
	}
}

func (a *API) exportReportXlsx(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}

	// 构建筛选条件（与 listTestCases 保持一致）
	filter := repository.TestCaseFilter{
		Keyword:       strings.TrimSpace(c.Query("keyword")),
		Level:         c.Query("level"),
		ReviewResult:  c.Query("review_result"),
		ExecResult:    c.Query("exec_result"),
		Tags:          c.Query("tags"),
		ModulePath:    strings.TrimSpace(c.Query("module_path")),
		CreatedAfter:  c.Query("created_after"),
		CreatedBefore: c.Query("created_before"),
		UpdatedAfter:  c.Query("updated_after"),
		UpdatedBefore: c.Query("updated_before"),
		SortBy:        c.Query("sortBy"),
		SortOrder:     c.Query("sortOrder"),
	}
	if mid := c.Query("module_id"); mid != "" {
		if v, err := strconv.ParseUint(mid, 10, 64); err == nil {
			moduleID := uint(v)
			filter.ModuleID = &moduleID
		}
	}
	if cid := c.Query("created_by"); cid != "" {
		if v, err := strconv.ParseUint(cid, 10, 64); err == nil {
			createdBy := uint(v)
			filter.CreatedByID = &createdBy
		}
	}
	if uid := c.Query("updated_by"); uid != "" {
		if v, err := strconv.ParseUint(uid, 10, 64); err == nil {
			updatedBy := uint(v)
			filter.UpdatedByID = &updatedBy
		}
	}
	if tagIDsStr := c.Query("tag_ids"); tagIDsStr != "" {
		for _, s := range strings.Split(tagIDsStr, ",") {
			if v, err := strconv.ParseUint(strings.TrimSpace(s), 10, 64); err == nil && v > 0 {
				filter.TagIDs = append(filter.TagIDs, uint(v))
			}
		}
	}

	data, err := a.xlsxSvc.ExportReportToXlsx(c.Request.Context(), projectID, filter)
	if err != nil {
		response.Error(c, 500, service.CodeInternal, "export report failed: "+err.Error())
		return
	}

	// 获取项目名用于文件名
	projectName := fmt.Sprintf("%d", projectID)
	if p, pErr := a.projectSvc.GetByID(c.Request.Context(), projectID); pErr == nil && p != nil {
		projectName = p.Name
	}
	fileName := fmt.Sprintf("用例报表_%s_%s.xlsx", projectName, time.Now().Format("20060102_150405"))

	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename*=UTF-8''%s`, url.PathEscape(fileName)))
	c.Data(200, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", data)
}

func (a *API) importTestCasesXlsx(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}

	file, _, err := c.Request.FormFile("file")
	if err != nil {
		response.Error(c, 400, service.CodeParamsError, "file is required")
		return
	}
	defer file.Close()

	created, skipped, err := a.xlsxSvc.ImportFromXlsx(c.Request.Context(), projectID, user.ID, file)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, gin.H{
		"created": created,
		"skipped": skipped,
	})
}

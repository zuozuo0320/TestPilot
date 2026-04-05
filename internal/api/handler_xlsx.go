// handler_xlsx.go — 用例导入导出 Handler
package api

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"

	"testpilot/internal/dto/response"
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

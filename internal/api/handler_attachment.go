// handler_attachment.go — 附件管理 Handler
package api

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"testpilot/internal/dto/response"
)

func (a *API) uploadAttachment(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	testcaseID, ok := parseUintParam(c, "testcaseID")
	if !ok {
		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		response.Error(c, 400, "no file provided")
		return
	}
	defer file.Close()

	att, err := a.attachmentSvc.Upload(
		testcaseID, user.ID,
		header.Filename, header.Size, header.Header.Get("Content-Type"),
		file,
	)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.Created(c, att)
}

func (a *API) listAttachments(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	testcaseID, ok := parseUintParam(c, "testcaseID")
	if !ok {
		return
	}
	list, err := a.attachmentSvc.ListByCaseID(testcaseID)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, list)
}

func (a *API) deleteAttachment(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	attID, err := strconv.ParseUint(c.Param("attachmentID"), 10, 64)
	if err != nil {
		response.Error(c, 400, "invalid attachment ID")
		return
	}
	if err := a.attachmentSvc.Delete(uint(attID)); err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, gin.H{"deleted": true})
}

func (a *API) downloadAttachment(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	attID, err := strconv.ParseUint(c.Param("attachmentID"), 10, 64)
	if err != nil {
		response.Error(c, 400, "invalid attachment ID")
		return
	}
	fullPath, fileName, err := a.attachmentSvc.GetFilePath(uint(attID))
	if err != nil {
		response.HandleError(c, err)
		return
	}
	c.FileAttachment(fullPath, fileName)
}

// handler_case_review_attachment.go — 评审附件管理 Handler
package api

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"testpilot/internal/dto/response"
	"testpilot/internal/model"
	"testpilot/internal/service"
)

// @Summary 上传评审附件
// @Description 给某个评审项上传证据附件，独立于用例正式附件
// @Tags CaseReview
// @Accept multipart/form-data
// @Produce json
// @Param projectID path int true "项目 ID"
// @Param reviewID path int true "评审计划 ID"
// @Param itemID path int true "评审项 ID"
// @Param file formData file true "文件"
// @Success 201 {object} response.Response{data=model.CaseReviewAttachment}
// @Router /projects/{projectID}/case-reviews/{reviewID}/items/{itemID}/attachments [post]
func (a *API) uploadReviewAttachment(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	if !requireRole(c, user, model.GlobalRoleManager, model.GlobalRoleReviewer, model.GlobalRoleTester) {
		return
	}
	reviewID, ok := parseUintParam(c, "reviewID")
	if !ok {
		return
	}
	itemID, ok := parseUintParam(c, "itemID")
	if !ok {
		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		response.Error(c, 400, service.CodeParamsError, "no file provided")
		return
	}
	defer file.Close()

	att, err := a.caseReviewAttachmentSvc.Upload(c.Request.Context(), service.UploadReviewAttachmentInput{
		ProjectID:    projectID,
		ReviewID:     reviewID,
		ReviewItemID: itemID,
		UploaderID:   user.ID,
		FileName:     header.Filename,
		FileSize:     header.Size,
		MimeType:     header.Header.Get("Content-Type"),
		Reader:       file,
	})
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.Created(c, att)
}

// @Summary 查询评审项附件列表
// @Tags CaseReview
// @Produce json
// @Param projectID path int true "项目 ID"
// @Param reviewID path int true "评审计划 ID"
// @Param itemID path int true "评审项 ID"
// @Success 200 {object} response.Response{data=[]model.CaseReviewAttachment}
// @Router /projects/{projectID}/case-reviews/{reviewID}/items/{itemID}/attachments [get]
func (a *API) listReviewAttachments(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	reviewID, ok := parseUintParam(c, "reviewID")
	if !ok {
		return
	}
	itemID, ok := parseUintParam(c, "itemID")
	if !ok {
		return
	}
	list, err := a.caseReviewAttachmentSvc.ListByItem(c.Request.Context(), projectID, reviewID, itemID)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, list)
}

// @Summary 查询用例维度的评审附件（只读镜像）
// @Description 聚合该用例在所有评审计划中的历史证据
// @Tags CaseReview
// @Produce json
// @Param projectID path int true "项目 ID"
// @Param testcaseID path int true "用例 ID"
// @Success 200 {object} response.Response{data=[]model.CaseReviewAttachment}
// @Router /projects/{projectID}/testcases/{testcaseID}/review-attachments [get]
func (a *API) listReviewAttachmentsByTestCase(c *gin.Context) {
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
	list, err := a.caseReviewAttachmentSvc.ListByTestCase(c.Request.Context(), projectID, testcaseID)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, list)
}

// @Summary 删除评审附件
// @Tags CaseReview
// @Produce json
// @Param projectID path int true "项目 ID"
// @Param attachmentID path int true "附件 ID"
// @Success 200 {object} response.Response
// @Router /projects/{projectID}/review-attachments/{attachmentID} [delete]
func (a *API) deleteReviewAttachment(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	if !requireRole(c, user, model.GlobalRoleManager, model.GlobalRoleReviewer, model.GlobalRoleTester) {
		return
	}
	attID, err := strconv.ParseUint(c.Param("attachmentID"), 10, 64)
	if err != nil {
		response.Error(c, 400, service.CodeParamsError, "invalid attachment ID")
		return
	}
	if err := a.caseReviewAttachmentSvc.Delete(c.Request.Context(), projectID, uint(attID)); err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, gin.H{"deleted": true})
}

// @Summary 下载评审附件
// @Tags CaseReview
// @Param projectID path int true "项目 ID"
// @Param attachmentID path int true "附件 ID"
// @Router /projects/{projectID}/review-attachments/{attachmentID}/download [get]
func (a *API) downloadReviewAttachment(c *gin.Context) {
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
		response.Error(c, 400, service.CodeParamsError, "invalid attachment ID")
		return
	}
	fullPath, fileName, err := a.caseReviewAttachmentSvc.GetFilePath(c.Request.Context(), projectID, uint(attID))
	if err != nil {
		response.HandleError(c, err)
		return
	}
	c.FileAttachment(fullPath, fileName)
}

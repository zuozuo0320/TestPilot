// handler_requirement_doc.go — 需求文档 HTTP Handler
//
// 外部 API（前端调用）：
//
//	POST   /projects/:projectID/requirement-docs/upload     上传文件
//	POST   /projects/:projectID/requirement-docs/paste      粘贴文本
//	GET    /projects/:projectID/requirement-docs            文档列表
//	GET    /projects/:projectID/requirement-docs/:docID     文档详情
//	DELETE /projects/:projectID/requirement-docs/:docID     删除文档
//
// 内部 API（Executor 回调）：
//
//	POST   /internal/requirement-docs/:docID/parse-callback 解析回调
package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"testpilot/internal/dto/response"
	"testpilot/internal/repository"
	"testpilot/internal/service"
)

// ========== 请求结构体 ==========

// pasteDocRequest 粘贴文本创建文档
type pasteDocRequest struct {
	Title      string `json:"title" binding:"required,min=1,max=200"`
	RawContent string `json:"raw_content" binding:"required,min=1"`
	Remark     string `json:"remark" binding:"max=500"`
}

// parseCallbackRequest 解析回调请求
type parseCallbackRequest struct {
	Status  string `json:"status" binding:"required,oneof=parsed parse_failed"`
	Content string `json:"content"` // status=parsed 时必填
	Error   string `json:"error"`   // status=parse_failed 时必填
}

// ========== Handler 方法 ==========

// uploadRequirementDoc 上传文件创建需求文档
// @Summary 上传需求文档
// @Tags 需求智生-文档
// @Accept multipart/form-data
// @Produce json
// @Param projectID path int true "项目ID"
// @Param file formData file true "需求文档文件(docx/pdf/md/txt)"
// @Param title formData string true "文档标题"
// @Param remark formData string false "备注"
// @Success 201 {object} response.Result
// @Failure 400 {object} response.Result
// @Router /api/v1/projects/{projectID}/requirement-docs/upload [post]
func (a *API) uploadRequirementDoc(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}

	// 解析 multipart form
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		response.Error(c, http.StatusBadRequest, service.CodeParamsError, "请选择要上传的文件")
		return
	}
	defer file.Close()

	title := c.PostForm("title")
	if title == "" {
		title = header.Filename
	}
	remark := c.PostForm("remark")

	// 获取文件扩展名
	ext := service.GetFileExtension(header.Filename)
	if ext == "" {
		response.Error(c, http.StatusBadRequest, service.CodeReqDocFormatInvalid, "无法识别文件格式")
		return
	}

	// 保存文件到磁盘
	safeFilename := sanitizeUploadFilename(header.Filename, ext)
	saveName := fmt.Sprintf("%d_%d_%s", projectID, time.Now().UnixMilli(), safeFilename)
	saveDir := filepath.Join("uploads", "requirement-docs", fmt.Sprintf("%d", projectID))
	if err := os.MkdirAll(saveDir, 0o755); err != nil {
		a.logger.Error("创建上传目录失败", "error", err, "dir", saveDir)
		response.Error(c, http.StatusInternalServerError, service.CodeInternal, "文件保存失败")
		return
	}
	savePath := filepath.Join(saveDir, saveName)
	dst, err := os.Create(savePath)
	if err != nil {
		a.logger.Error("创建上传文件失败", "error", err, "path", savePath)
		response.Error(c, http.StatusInternalServerError, service.CodeInternal, "文件保存失败")
		return
	}
	if _, err := io.Copy(dst, file); err != nil {
		_ = dst.Close()
		_ = os.Remove(savePath)
		a.logger.Error("写入上传文件失败", "error", err, "path", savePath)
		response.Error(c, http.StatusInternalServerError, service.CodeInternal, "文件保存失败")
		return
	}
	if err := dst.Close(); err != nil {
		_ = os.Remove(savePath)
		a.logger.Error("关闭上传文件失败", "error", err, "path", savePath)
		response.Error(c, http.StatusInternalServerError, service.CodeInternal, "文件保存失败")
		return
	}

	doc, svcErr := a.reqDocSvc.CreateByUpload(c.Request.Context(), service.CreateDocByUploadInput{
		ProjectID:  projectID,
		Title:      title,
		FilePath:   savePath,
		FileFormat: ext,
		FileSize:   header.Size,
		Remark:     remark,
		CreatedBy:  user.ID,
	})
	if svcErr != nil {
		response.HandleError(c, svcErr)
		return
	}

	// 异步派发文档解析（非阻塞，失败不影响创建响应）
	// 注意：必须使用 context.Background()，handler 返回后 request context 会被取消
	go func() {
		if err := a.reqDocSvc.DispatchParse(context.Background(), doc); err != nil {
			a.logger.Error("异步派发文档解析失败", "error", err, "doc_id", doc.ID)
		}
	}()

	response.Created(c, doc)
}

// pasteRequirementDoc 粘贴文本创建需求文档
// @Summary 粘贴文本创建需求文档
// @Tags 需求智生-文档
// @Accept json
// @Produce json
// @Param projectID path int true "项目ID"
// @Param body body pasteDocRequest true "请求体"
// @Success 201 {object} response.Result
// @Failure 400 {object} response.Result
// @Router /api/v1/projects/{projectID}/requirement-docs/paste [post]
func (a *API) pasteRequirementDoc(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}

	var req pasteDocRequest
	if !bindJSON(c, &req) {
		return
	}

	doc, err := a.reqDocSvc.CreateByPaste(c.Request.Context(), service.CreateDocByPasteInput{
		ProjectID:  projectID,
		Title:      req.Title,
		RawContent: req.RawContent,
		Remark:     req.Remark,
		CreatedBy:  user.ID,
	})
	if err != nil {
		response.HandleError(c, err)
		return
	}

	response.Created(c, doc)
}

// listRequirementDocs 需求文档列表（分页）
// @Summary 需求文档列表
// @Tags 需求智生-文档
// @Produce json
// @Param projectID path int true "项目ID"
// @Param keyword query string false "标题关键字"
// @Param parse_status query string false "解析状态"
// @Param source_type query string false "来源类型"
// @Param page query int false "页码" default(1)
// @Param page_size query int false "每页条数" default(20)
// @Success 200 {object} response.PageResult
// @Router /api/v1/projects/{projectID}/requirement-docs [get]
func (a *API) listRequirementDocs(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}

	page := parsePositiveIntWithDefault(c.Query("page"), 1)
	pageSize := parsePositiveIntWithDefault(c.Query("page_size"), 20)

	docs, total, err := a.reqDocSvc.ListPaged(c.Request.Context(), projectID, repository.RequirementDocFilter{
		Keyword:     c.Query("keyword"),
		ParseStatus: c.Query("parse_status"),
		SourceType:  c.Query("source_type"),
		Page:        page,
		PageSize:    pageSize,
	})
	if err != nil {
		response.HandleError(c, err)
		return
	}

	response.Page(c, docs, total, page, pageSize)
}

// getRequirementDoc 需求文档详情
// @Summary 需求文档详情
// @Tags 需求智生-文档
// @Produce json
// @Param projectID path int true "项目ID"
// @Param docID path int true "文档ID"
// @Success 200 {object} response.Result
// @Router /api/v1/projects/{projectID}/requirement-docs/{docID} [get]
func (a *API) getRequirementDoc(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	docID, ok := parseUintParam(c, "docID")
	if !ok {
		return
	}

	doc, err := a.reqDocSvc.GetByID(c.Request.Context(), projectID, docID)
	if err != nil {
		response.HandleError(c, err)
		return
	}

	response.OK(c, doc)
}

// deleteRequirementDoc 删除需求文档
// @Summary 删除需求文档
// @Tags 需求智生-文档
// @Produce json
// @Param projectID path int true "项目ID"
// @Param docID path int true "文档ID"
// @Success 200 {object} response.Result
// @Router /api/v1/projects/{projectID}/requirement-docs/{docID} [delete]
func (a *API) deleteRequirementDoc(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}
	docID, ok := parseUintParam(c, "docID")
	if !ok {
		return
	}

	if err := a.reqDocSvc.Delete(c.Request.Context(), projectID, docID); err != nil {
		response.HandleError(c, err)
		return
	}

	response.OK(c, nil)
}

// retryParseRequirementDocs 重试所有未解析文档
// @Summary 重试未解析文档
// @Tags 需求智生-文档
// @Produce json
// @Param projectID path int true "项目ID"
// @Success 200 {object} response.Result
// @Router /api/v1/projects/{projectID}/requirement-docs/retry-parse [post]
func (a *API) retryParseRequirementDocs(c *gin.Context) {
	user := currentUser(c)
	projectID, ok := parseUintParam(c, "projectID")
	if !ok {
		return
	}
	if !a.requireProjectAccess(c, user, projectID) {
		return
	}

	dispatched, err := a.reqDocSvc.RetryUnparsedDocs(c.Request.Context(), projectID)
	if err != nil {
		response.HandleError(c, err)
		return
	}

	response.OK(c, gin.H{"dispatched": dispatched})
}

// parseCallbackRequirementDoc Executor 解析回调（内部接口）
// @Summary 文档解析回调
// @Tags 需求智生-内部
// @Accept json
// @Produce json
// @Param docID path int true "文档ID"
// @Param body body parseCallbackRequest true "回调数据"
// @Success 200 {object} response.Result
// @Router /internal/requirement-docs/{docID}/parse-callback [post]
func (a *API) parseCallbackRequirementDoc(c *gin.Context) {
	docID, ok := parseUintParam(c, "docID")
	if !ok {
		return
	}

	var req parseCallbackRequest
	if !bindJSON(c, &req) {
		return
	}

	ctx := c.Request.Context()
	var err error
	switch req.Status {
	case "parsed":
		err = a.reqDocSvc.MarkParsed(ctx, docID, req.Content)
	case "parse_failed":
		err = a.reqDocSvc.MarkParseFailed(ctx, docID, req.Error)
	}

	if err != nil {
		response.HandleError(c, err)
		return
	}

	response.OK(c, nil)
}

func sanitizeUploadFilename(filename string, ext string) string {
	base := filepath.Base(strings.TrimSpace(filename))
	replacer := strings.NewReplacer(
		"\\", "_",
		"/", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
	)
	base = strings.Trim(replacer.Replace(base), ". ")
	if base == "" {
		base = "requirement_doc." + ext
	}
	return base
}

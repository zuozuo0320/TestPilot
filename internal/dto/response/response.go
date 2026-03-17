// response.go — 统一 API 响应格式
package response

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"testpilot/internal/service"
)

// Result 统一响应结构
type Result struct {
	Code      int    `json:"code"`               // HTTP 状态码
	Message   string `json:"message"`             // 提示消息
	Data      any    `json:"data,omitempty"`      // 业务数据
	RequestID string `json:"request_id,omitempty"` // 请求追踪 ID
}

// PageResult 分页响应结构
type PageResult struct {
	Code      int    `json:"code"`
	Message   string `json:"message"`
	Data      any    `json:"data,omitempty"`
	Total     int64  `json:"total"`
	Page      int    `json:"page"`
	PageSize  int    `json:"page_size"`
	RequestID string `json:"request_id,omitempty"`
}

// ========== 成功响应 ==========

// OK 200 成功
func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Result{
		Code: 200, Message: "success", Data: data,
		RequestID: c.GetString("request_id"),
	})
}

// Created 201 创建成功
func Created(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, Result{
		Code: 201, Message: "created", Data: data,
		RequestID: c.GetString("request_id"),
	})
}

// Page 分页成功
func Page(c *gin.Context, data any, total int64, page, pageSize int) {
	c.JSON(http.StatusOK, PageResult{
		Code: 200, Message: "success", Data: data,
		Total: total, Page: page, PageSize: pageSize,
		RequestID: c.GetString("request_id"),
	})
}

// ========== 错误响应 ==========

// Error 通用错误响应
func Error(c *gin.Context, status int, message string) {
	c.JSON(status, Result{
		Code: status, Message: message,
		RequestID: c.GetString("request_id"),
	})
}

// HandleError 将 Service 层 BizError 映射为标准 HTTP 错误响应
func HandleError(c *gin.Context, err error) {
	if bizErr, ok := err.(*service.BizError); ok {
		Error(c, bizErr.Status, bizErr.Message)
		return
	}
	Error(c, http.StatusInternalServerError, "internal server error")
}

// response.go — 统一 API 响应格式
package response

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"testpilot/internal/service"
)

// Result 统一响应结构
// Code 字段为 6 位数字业务错误码（如 200000=成功, 100101=评审计划不存在）
type Result struct {
	Code      int    `json:"code"`                 // 6 位业务错误码
	Message   string `json:"message"`              // 提示消息
	Data      any    `json:"data,omitempty"`       // 业务数据
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

// OK 200 成功（业务码 200000）
func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Result{
		Code: service.CodeSuccess, Message: "success", Data: data,
		RequestID: c.GetString("request_id"),
	})
}

// Created 201 创建成功（业务码 200000）
func Created(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, Result{
		Code: service.CodeSuccess, Message: "created", Data: data,
		RequestID: c.GetString("request_id"),
	})
}

// Page 分页成功（业务码 200000）
func Page(c *gin.Context, data any, total int64, page, pageSize int) {
	c.JSON(http.StatusOK, PageResult{
		Code: service.CodeSuccess, Message: "success", Data: data,
		Total: total, Page: page, PageSize: pageSize,
		RequestID: c.GetString("request_id"),
	})
}

// ========== 错误响应 ==========

// Error 通用错误响应（传入 6 位业务错误码）
func Error(c *gin.Context, httpStatus int, bizCode int, message string) {
	c.JSON(httpStatus, Result{
		Code: bizCode, Message: message,
		RequestID: c.GetString("request_id"),
	})
}

// HandleError 将 Service 层 BizError 映射为标准错误响应
// 关键：Code 使用 BizError 中的 6 位数字业务码，而非 HTTP 状态码
func HandleError(c *gin.Context, err error) {
	if bizErr, ok := err.(*service.BizError); ok {
		c.JSON(bizErr.Status, Result{
			Code:      bizErr.NumericCode(),
			Message:   bizErr.Message,
			RequestID: c.GetString("request_id"),
		})
		return
	}
	c.JSON(http.StatusInternalServerError, Result{
		Code:      service.CodeInternal,
		Message:   "internal server error",
		RequestID: c.GetString("request_id"),
	})
}

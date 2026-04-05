// helpers.go — API 层公共工具函数（P7: 含校验错误格式化）
package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"

	"testpilot/internal/dto/response"
	"testpilot/internal/model"
	"testpilot/internal/service"
)

// bindJSON 绑定 JSON 并格式化校验错误
func bindJSON(c *gin.Context, obj any) bool {
	if err := c.ShouldBindJSON(obj); err != nil {
		var ve validator.ValidationErrors
		if errors.As(err, &ve) {
			msgs := make([]string, 0, len(ve))
			for _, fe := range ve {
				field := strings.ToLower(fe.Field())
				switch fe.Tag() {
				case "required":
					msgs = append(msgs, fmt.Sprintf("%s is required", field))
				case "email":
					msgs = append(msgs, fmt.Sprintf("%s must be a valid email", field))
				case "min":
					msgs = append(msgs, fmt.Sprintf("%s: min=%s", field, fe.Param()))
				case "max":
					msgs = append(msgs, fmt.Sprintf("%s: max=%s", field, fe.Param()))
				case "oneof":
					msgs = append(msgs, fmt.Sprintf("%s must be one of [%s]", field, fe.Param()))
				case "url":
					msgs = append(msgs, fmt.Sprintf("%s must be a valid URL", field))
				default:
					msgs = append(msgs, fmt.Sprintf("%s: %s=%s", field, fe.Tag(), fe.Param()))
				}
			}
			response.Error(c, http.StatusBadRequest, service.CodeParamsError, strings.Join(msgs, "; "))
		} else {
			response.Error(c, http.StatusBadRequest, service.CodeParamsError, err.Error())
		}
		return false
	}
	return true
}

// ========== 常量 ==========

const currentUserKey = "current-user"

var defaultAllowedOrigins = []string{
	"http://localhost:5173",
	"http://127.0.0.1:5173",
	"http://localhost:3000",
	"http://127.0.0.1:3000",
}

// ========== 上下文辅助 ==========

// currentUser 从 gin.Context 中获取当前登录用户
func currentUser(c *gin.Context) model.User {
	value, ok := c.Get(currentUserKey)
	if !ok {
		return model.User{}
	}
	user, ok := value.(model.User)
	if !ok {
		return model.User{}
	}
	return user
}

// ========== 参数解析 ==========

func parseUintParam(c *gin.Context, key string) (uint, bool) {
	text := c.Param(key)
	value, err := strconv.ParseUint(text, 10, 64)
	if err != nil || value == 0 {
		response.Error(c, http.StatusBadRequest, service.CodeParamsError, fmt.Sprintf("invalid path param: %s", key))
		return 0, false
	}
	return uint(value), true
}

func parsePositiveIntWithDefault(raw string, defaultValue int) int {
	if strings.TrimSpace(raw) == "" {
		return defaultValue
	}
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || v <= 0 {
		return defaultValue
	}
	return v
}

// ========== 权限校验 ==========

// requireRole 校验用户角色
func requireRole(c *gin.Context, user model.User, roles ...string) bool {
	if user.Role == model.GlobalRoleAdmin {
		return true
	}
	for _, role := range roles {
		if user.Role == role {
			return true
		}
	}
	response.Error(c, http.StatusForbidden, service.CodeForbidden, "insufficient role")
	return false
}

// requireProjectAccess 通过 ProjectService 校验项目权限
func (a *API) requireProjectAccess(c *gin.Context, user model.User, projectID uint) bool {
	if err := a.projectSvc.RequireAccess(c.Request.Context(), user, projectID); err != nil {
		response.HandleError(c, err)
		return false
	}
	return true
}

// ========== CORS ==========

func parseAllowedOrigins(raw string) []string {
	parts := strings.Split(raw, ",")
	origins := make([]string, 0, len(parts))
	for _, p := range parts {
		item := strings.TrimSpace(p)
		if item == "" {
			continue
		}
		origins = append(origins, item)
	}
	if len(origins) == 0 {
		return append([]string(nil), defaultAllowedOrigins...)
	}
	return origins
}

func (a *API) isAllowedOrigin(origin string) bool {
	if origin == "" {
		return false
	}
	for _, allowed := range a.allowedOrigins {
		if allowed == "*" || strings.EqualFold(allowed, origin) {
			return true
		}
	}
	return false
}

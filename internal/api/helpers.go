// helpers.go — API 层公共工具函数（P7: 含校验错误格式化）
package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"

	"testpilot/internal/dto/response"
	"testpilot/internal/model"
	"testpilot/internal/service"
)

const uploadDirPerm = 0o750

func saveUploadedFileUnderRoot(dir, filename string, reader io.Reader) error {
	if err := os.MkdirAll(dir, uploadDirPerm); err != nil {
		return fmt.Errorf("创建上传目录失败: %w", err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return fmt.Errorf("打开上传目录失败: %w", err)
	}
	defer func() { _ = root.Close() }()

	dst, err := root.Create(filename)
	if err != nil {
		return fmt.Errorf("创建上传文件失败: %w", err)
	}
	if _, err := io.Copy(dst, reader); err != nil {
		_ = dst.Close()
		_ = root.Remove(filename)
		return fmt.Errorf("写入上传文件失败: %w", err)
	}
	if err := dst.Close(); err != nil {
		_ = root.Remove(filename)
		return fmt.Errorf("关闭上传文件失败: %w", err)
	}
	return nil
}

// bindJSON 绑定 JSON 并格式化校验错误
func bindJSON(c *gin.Context, obj any) bool {
	if err := c.ShouldBindJSON(obj); err != nil {
		var ve validator.ValidationErrors
		if errors.As(err, &ve) {
			fieldErrors := make([]response.FieldError, 0, len(ve))
			for _, fe := range ve {
				field := strings.ToLower(fe.Field())
				message := ""
				switch fe.Tag() {
				case "required":
					message = "不能为空"
				case "email":
					message = "邮箱格式不正确"
				case "min":
					message = fmt.Sprintf("最小值或最小长度为 %s", fe.Param())
				case "max":
					message = fmt.Sprintf("最大值或最大长度为 %s", fe.Param())
				case "oneof":
					message = fmt.Sprintf("必须是以下值之一：%s", fe.Param())
				case "url":
					message = "URL 格式不正确"
				default:
					message = fmt.Sprintf("校验失败：%s=%s", fe.Tag(), fe.Param())
				}
				fieldErrors = append(fieldErrors, response.FieldError{Field: field, Message: message})
			}
			response.ValidationError(c, fieldErrors)
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

// internalAPIKeyAuth 内部接口 API Key 鉴权中间件。
// Executor 回调时在 Authorization header 携带 Bearer <API_KEY>，
// 与配置的 ExecutorAPIKey 进行比对。
func (a *API) internalAPIKeyAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if header == "" {
			response.Error(c, http.StatusUnauthorized, service.CodeReqInternalTokenInvalid, "missing authorization header")
			c.Abort()
			return
		}
		token := strings.TrimPrefix(header, "Bearer ")
		if token == header || token != a.executorAPIKey {
			response.Error(c, http.StatusUnauthorized, service.CodeReqInternalTokenInvalid, "invalid api key")
			c.Abort()
			return
		}
		c.Next()
	}
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

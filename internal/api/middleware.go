// middleware.go — HTTP 中间件（CORS / 认证 / 请求日志 / Request ID / Recovery）
package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"testpilot/internal/dto/response"
	pkgauth "testpilot/internal/pkg/auth"
	"testpilot/internal/service"
)

// requestIDMiddleware 为每个请求分配唯一 Request-ID
func (a *API) requestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		rid := c.GetHeader("X-Request-ID")
		if rid == "" {
			rid = uuid.New().String()
		}
		c.Set("request_id", rid)
		c.Header("X-Request-ID", rid)
		c.Next()
	}
}

// recoveryMiddleware 捕获 panic，返回统一 500 错误
func (a *API) recoveryMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				a.logger.Error("panic_recovered",
					"request_id", c.GetString("request_id"),
					"path", c.Request.URL.Path,
					"panic", r,
				)
				response.Error(c, http.StatusInternalServerError, service.CodeInternal, "internal server error")
				c.Abort()
			}
		}()
		c.Next()
	}
}

// requestLogger 请求日志中间件（含 request_id）
func (a *API) requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}
		a.logger.Info("http_request",
			"request_id", c.GetString("request_id"),
			"method", c.Request.Method,
			"path", path,
			"status", c.Writer.Status(),
			"latency_ms", time.Since(start).Milliseconds(),
			"client_ip", c.ClientIP(),
		)
	}
}

// corsMiddleware 跨域资源共享中间件
func (a *API) corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" && a.isAllowedOrigin(origin) {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-User-ID, X-Request-ID")
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			c.Header("Access-Control-Expose-Headers", "Content-Disposition")
			c.Header("Vary", "Origin")
		}
		if c.Request.Method == http.MethodOptions {
			c.Status(http.StatusNoContent)
			c.Abort()
			return
		}
		c.Next()
	}
}

// authMiddleware 认证中间件
// 优先解析 Authorization: Bearer <jwt>
// 兼容旧方式 X-User-ID（用于测试和过渡期）
func (a *API) authMiddleware() gin.HandlerFunc {
	jwtCfg := a.authSvc.JWTConfig()

	return func(c *gin.Context) {
		var userID uint

		// 1. 优先尝试 JWT Bearer token
		authHeader := c.GetHeader("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
			claims, err := pkgauth.ParseToken(jwtCfg.Secret, tokenStr)
			if err != nil {
				response.Error(c, http.StatusUnauthorized, service.CodeUnauthorized, err.Error())
				c.Abort()
				return
			}
			userID = claims.UserID
		} else {
			// 2. 兼容旧 X-User-ID header（过渡期）
			userIDText := c.GetHeader("X-User-ID")
			if userIDText == "" {
				response.Error(c, http.StatusUnauthorized, service.CodeUnauthorized, "missing Authorization header")
				c.Abort()
				return
			}
			userID64, err := strconv.ParseUint(userIDText, 10, 64)
			if err != nil || userID64 == 0 {
				response.Error(c, http.StatusUnauthorized, service.CodeUnauthorized, "invalid X-User-ID header")
				c.Abort()
				return
			}
			userID = uint(userID64)
		}

		// 通过 AuthService 查找用户并验证状态
		user, bizErr := a.authSvc.FindUserForAuth(c.Request.Context(), userID)
		if bizErr != nil {
			response.HandleError(c, bizErr)
			c.Abort()
			return
		}

		c.Set(currentUserKey, *user)
		c.Next()
	}
}

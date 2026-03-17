// handler_auth.go — 登录 / 刷新 Token Handler
package api

import (
	"strings"

	"github.com/gin-gonic/gin"

	"testpilot/internal/dto/response"
)

// login 用户登录
func (a *API) login(c *gin.Context) {
	var req loginRequest
	if !bindJSON(c, &req) {
		return
	}
	result, err := a.authSvc.Login(c.Request.Context(),
		strings.ToLower(strings.TrimSpace(req.Email)),
		strings.TrimSpace(req.Password),
	)
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, result)
}

// refreshToken 刷新 Token
func (a *API) refreshToken(c *gin.Context) {
	var req refreshTokenRequest
	if !bindJSON(c, &req) {
		return
	}
	result, err := a.authSvc.RefreshToken(c.Request.Context(), strings.TrimSpace(req.RefreshToken))
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, result)
}

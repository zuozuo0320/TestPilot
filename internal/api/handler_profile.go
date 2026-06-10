// handler_profile.go — 个人中心 Handler
package api

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"testpilot/internal/dto/response"
	"testpilot/internal/service"
)

func (a *API) updateProfile(c *gin.Context) {
	user := currentUser(c)
	var req updateProfileRequest
	if !bindJSON(c, &req) {
		return
	}
	updated, err := a.profileSvc.Update(c.Request.Context(), user, service.UpdateProfileInput{
		Name:   req.Name,
		Email:  req.Email,
		Phone:  req.Phone,
		Avatar: req.Avatar,
	})
	if err != nil {
		response.HandleError(c, err)
		return
	}
	response.OK(c, updated)
}

func (a *API) uploadMyAvatar(c *gin.Context) {
	user := currentUser(c)
	file, header, err := c.Request.FormFile("avatar")
	if err != nil {
		response.Error(c, http.StatusBadRequest, service.CodeParamsError, "avatar file is required")
		return
	}
	defer func() { _ = file.Close() }()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext != ".png" && ext != ".jpg" && ext != ".jpeg" && ext != ".gif" && ext != ".webp" {
		response.Error(c, http.StatusBadRequest, service.CodeParamsError, "unsupported image format")
		return
	}
	if header.Size > 2*1024*1024 {
		response.Error(c, http.StatusBadRequest, service.CodeParamsError, "file too large (max 2MB)")
		return
	}

	dir := "uploads/avatars"
	filename := fmt.Sprintf("%d_%d%s", user.ID, time.Now().UnixMilli(), ext)
	if err := saveUploadedFileUnderRoot(dir, filename, file); err != nil {
		a.logger.Error("保存头像失败", "error", err, "dir", dir)
		response.Error(c, http.StatusInternalServerError, service.CodeInternal, "failed to save file")
		return
	}

	avatarURL := "/" + dir + "/" + filename
	if svcErr := a.profileSvc.UpdateAvatar(c.Request.Context(), user, avatarURL); svcErr != nil {
		response.HandleError(c, svcErr)
		return
	}
	response.OK(c, gin.H{"avatar": avatarURL})
}

func (a *API) getProfile(c *gin.Context) {
	user := currentUser(c)
	response.OK(c, user)
}

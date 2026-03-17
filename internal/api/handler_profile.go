// handler_profile.go — 个人中心 Handler
package api

import (
	"fmt"
	"io"
	"net/http"
	"os"
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
		response.Error(c, http.StatusBadRequest, "avatar file is required")
		return
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext != ".png" && ext != ".jpg" && ext != ".jpeg" && ext != ".gif" && ext != ".webp" {
		response.Error(c, http.StatusBadRequest, "unsupported image format")
		return
	}
	if header.Size > 2*1024*1024 {
		response.Error(c, http.StatusBadRequest, "file too large (max 2MB)")
		return
	}

	dir := "uploads/avatars"
	os.MkdirAll(dir, 0o755)
	filename := fmt.Sprintf("%d_%d%s", user.ID, time.Now().UnixMilli(), ext)
	dst, err := os.Create(filepath.Join(dir, filename))
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to save file")
		return
	}
	defer dst.Close()
	if _, err := io.Copy(dst, file); err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to save file")
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

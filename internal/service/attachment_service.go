// attachment_service.go — 附件管理服务
package service

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"testpilot/internal/model"
	"testpilot/internal/repository"
)

const MaxAttachmentSize = 10 * 1024 * 1024 // 10MB

// AttachmentService 附件管理服务
type AttachmentService struct {
	attachRepo *repository.AttachmentRepo
	uploadDir  string
}

// NewAttachmentService 创建附件服务
func NewAttachmentService(repo *repository.AttachmentRepo, uploadDir string) *AttachmentService {
	os.MkdirAll(uploadDir, 0755)
	return &AttachmentService{attachRepo: repo, uploadDir: uploadDir}
}

// Upload 上传附件
func (s *AttachmentService) Upload(testCaseID uint, userID uint, fileName string, fileSize int64, mimeType string, reader io.Reader) (*model.CaseAttachment, error) {
	if fileSize > MaxAttachmentSize {
		return nil, ErrBadRequest(CodeParamsError, "file size exceeds 10MB limit")
	}

	// Generate unique file path
	ext := filepath.Ext(fileName)
	storedName := fmt.Sprintf("%d_%d_%d%s", testCaseID, userID, time.Now().UnixMilli(), ext)
	fullPath := filepath.Join(s.uploadDir, storedName)

	dst, err := os.Create(fullPath)
	if err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, reader); err != nil {
		os.Remove(fullPath)
		return nil, ErrInternal(CodeInternal, err)
	}

	attachment := &model.CaseAttachment{
		TestCaseID: testCaseID,
		FileName:   fileName,
		FilePath:   storedName,
		FileSize:   fileSize,
		MimeType:   mimeType,
		CreatedBy:  userID,
	}
	if err := s.attachRepo.Create(attachment); err != nil {
		os.Remove(fullPath)
		return nil, ErrInternal(CodeInternal, err)
	}
	return attachment, nil
}

// ListByCaseID 获取用例附件列表
func (s *AttachmentService) ListByCaseID(testCaseID uint) ([]model.CaseAttachment, error) {
	return s.attachRepo.ListByCaseID(testCaseID)
}

// Delete 删除附件
func (s *AttachmentService) Delete(id uint) error {
	att, err := s.attachRepo.GetByID(id)
	if err != nil {
		return ErrNotFound(CodeNotFound, "attachment not found")
	}
	// Remove file
	fullPath := filepath.Join(s.uploadDir, att.FilePath)
	os.Remove(fullPath)

	return s.attachRepo.Delete(id)
}

// GetFilePath 获取文件完整路径
func (s *AttachmentService) GetFilePath(id uint) (string, string, error) {
	att, err := s.attachRepo.GetByID(id)
	if err != nil {
		return "", "", ErrNotFound(CodeNotFound, "attachment not found")
	}
	return filepath.Join(s.uploadDir, att.FilePath), att.FileName, nil
}

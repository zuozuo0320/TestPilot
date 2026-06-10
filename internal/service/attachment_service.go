// attachment_service.go — 附件管理服务
package service

import (
	"fmt"
	"io"
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
	if err := saveReaderUnderRoot(s.uploadDir, storedName, reader); err != nil {
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
		_ = removeUnderRoot(s.uploadDir, storedName)
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
	if err := removeUnderRoot(s.uploadDir, att.FilePath); err != nil {
		return ErrInternal(CodeInternal, err)
	}

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

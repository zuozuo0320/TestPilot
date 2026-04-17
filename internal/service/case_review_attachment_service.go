// case_review_attachment_service.go — 评审项附件服务（独立于用例正式附件）
package service

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"testpilot/internal/model"
	"testpilot/internal/repository"
)

// CaseReviewAttachmentService 评审附件服务
type CaseReviewAttachmentService struct {
	attRepo    repository.CaseReviewAttachmentRepository
	reviewRepo repository.CaseReviewRepository
	uploadDir  string
}

// NewCaseReviewAttachmentService 创建评审附件服务
func NewCaseReviewAttachmentService(
	attRepo repository.CaseReviewAttachmentRepository,
	reviewRepo repository.CaseReviewRepository,
	uploadDir string,
) *CaseReviewAttachmentService {
	os.MkdirAll(uploadDir, 0755)
	return &CaseReviewAttachmentService{
		attRepo:    attRepo,
		reviewRepo: reviewRepo,
		uploadDir:  uploadDir,
	}
}

// UploadInput 上传输入
type UploadReviewAttachmentInput struct {
	ProjectID    uint
	ReviewID     uint
	ReviewItemID uint
	UploaderID   uint
	FileName     string
	FileSize     int64
	MimeType     string
	Reader       io.Reader
}

// Upload 上传评审附件，校验 item 归属当前 review + project
func (s *CaseReviewAttachmentService) Upload(ctx context.Context, input UploadReviewAttachmentInput) (*model.CaseReviewAttachment, error) {
	if input.FileSize > MaxAttachmentSize {
		return nil, ErrBadRequest(CodeParamsError, "file size exceeds 10MB limit")
	}

	// 1. 取 item 并校验归属
	item, err := s.reviewRepo.GetItemByID(ctx, nil, input.ReviewItemID)
	if err != nil {
		return nil, ErrNotFound(CodeReviewItemNotFound, "评审项不存在")
	}
	if item.ReviewID != input.ReviewID || item.ProjectID != input.ProjectID {
		return nil, ErrBadRequest(CodeReviewItemMismatch, "评审项不属于当前评审计划")
	}

	// 2. 写文件
	ext := filepath.Ext(input.FileName)
	storedName := fmt.Sprintf("review_%d_%d_%d_%d%s", input.ReviewID, input.ReviewItemID, input.UploaderID, time.Now().UnixMilli(), ext)
	fullPath := filepath.Join(s.uploadDir, storedName)

	dst, err := os.Create(fullPath)
	if err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, input.Reader); err != nil {
		os.Remove(fullPath)
		return nil, ErrInternal(CodeInternal, err)
	}

	// 3. 落库
	att := &model.CaseReviewAttachment{
		ReviewID:     input.ReviewID,
		ReviewItemID: input.ReviewItemID,
		ProjectID:    input.ProjectID,
		TestCaseID:   item.TestCaseID,
		RoundNo:      item.CurrentRoundNo,
		FileName:     input.FileName,
		FilePath:     storedName,
		FileSize:     input.FileSize,
		MimeType:     input.MimeType,
		CreatedBy:    input.UploaderID,
	}
	if err := s.attRepo.Create(ctx, nil, att); err != nil {
		os.Remove(fullPath)
		return nil, ErrInternal(CodeInternal, err)
	}
	return att, nil
}

// ListByItem 列出评审项的附件
func (s *CaseReviewAttachmentService) ListByItem(ctx context.Context, projectID, reviewID, reviewItemID uint) ([]model.CaseReviewAttachment, error) {
	item, err := s.reviewRepo.GetItemByID(ctx, nil, reviewItemID)
	if err != nil {
		return nil, ErrNotFound(CodeReviewItemNotFound, "评审项不存在")
	}
	if item.ReviewID != reviewID || item.ProjectID != projectID {
		return nil, ErrBadRequest(CodeReviewItemMismatch, "评审项不属于当前评审计划")
	}
	return s.attRepo.ListByItemID(ctx, reviewItemID)
}

// ListByTestCase 用例维度只读镜像，聚合该用例在所有评审计划中的证据
func (s *CaseReviewAttachmentService) ListByTestCase(ctx context.Context, projectID, testCaseID uint) ([]model.CaseReviewAttachment, error) {
	return s.attRepo.ListByTestCaseID(ctx, projectID, testCaseID)
}

// Delete 删除评审附件（带项目归属校验）
func (s *CaseReviewAttachmentService) Delete(ctx context.Context, projectID, id uint) error {
	att, err := s.attRepo.GetByID(ctx, id)
	if err != nil {
		return ErrNotFound(CodeNotFound, "attachment not found")
	}
	if att.ProjectID != projectID {
		return ErrBadRequest(CodeReviewItemMismatch, "附件不属于当前项目")
	}
	// 删除文件，忽略不存在错误
	os.Remove(filepath.Join(s.uploadDir, att.FilePath))
	return s.attRepo.Delete(ctx, nil, id)
}

// GetFilePath 下载时取磁盘路径（带项目归属校验）
func (s *CaseReviewAttachmentService) GetFilePath(ctx context.Context, projectID, id uint) (string, string, error) {
	att, err := s.attRepo.GetByID(ctx, id)
	if err != nil {
		return "", "", ErrNotFound(CodeNotFound, "attachment not found")
	}
	if att.ProjectID != projectID {
		return "", "", ErrBadRequest(CodeReviewItemMismatch, "附件不属于当前项目")
	}
	return filepath.Join(s.uploadDir, att.FilePath), att.FileName, nil
}

// Package service — 需求文档业务逻辑层
//
// RequirementDocService 管理需求文档的全生命周期：
//   - 文件上传（保存本地 + 创建记录 + 触发异步解析）
//   - 文本粘贴（直接入库）
//   - 分页列表（带创建人信息）
//   - 文档详情
//   - 软删除
//   - 解析状态回调（Executor 调用）
package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"testpilot/internal/model"
	"testpilot/internal/repository"
)

// RequirementDocService 需求文档业务逻辑层
type RequirementDocService struct {
	logger         *slog.Logger
	docRepo        repository.RequirementDocRepository
	sourceRepo     repository.RequirementDocSourceRepository
	txMgr          *repository.TxManager
	executorURL    string
	executorAPIKey string
	httpClient     *http.Client
}

// NewRequirementDocService 创建需求文档 Service
func NewRequirementDocService(
	logger *slog.Logger,
	docRepo repository.RequirementDocRepository,
	sourceRepo repository.RequirementDocSourceRepository,
	txMgr *repository.TxManager,
	executorURL string,
	executorAPIKey string,
) *RequirementDocService {
	return &RequirementDocService{
		logger:         logger.With("module", "requirement_doc"),
		docRepo:        docRepo,
		sourceRepo:     sourceRepo,
		txMgr:          txMgr,
		executorURL:    strings.TrimRight(executorURL, "/"),
		executorAPIKey: executorAPIKey,
		httpClient:     &http.Client{Timeout: 30 * time.Second},
	}
}

// ========== 输入结构体 ==========

// CreateDocByUploadInput 文件上传创建文档的参数
type CreateDocByUploadInput struct {
	ProjectID  uint
	Title      string
	FilePath   string // 已保存的文件相对路径
	FileFormat string // docx / pdf / md / txt
	FileSize   int64
	Remark     string
	CreatedBy  uint
}

// CreateDocByPasteInput 文本粘贴创建文档的参数
type CreateDocByPasteInput struct {
	ProjectID  uint
	Title      string
	RawContent string
	Remark     string
	CreatedBy  uint
}

// ========== 常量 ==========

const (
	maxDocContentChars = 50000 // 文档正文截断阈值
)

// 支持的文件格式
var supportedFileFormats = map[string]bool{
	"docx": true,
	"pdf":  true,
	"md":   true,
	"txt":  true,
}

// ========== 业务方法 ==========

// CreateByUpload 通过文件上传创建需求文档。
// 文件已由 Handler 保存至磁盘，此处仅创建数据库记录并标记为 not_parsed。
// 后续由 Executor 异步解析为纯文本。
func (s *RequirementDocService) CreateByUpload(ctx context.Context, input CreateDocByUploadInput) (*model.RequirementDoc, error) {
	// 1. 校验文件格式
	format := strings.ToLower(input.FileFormat)
	if !supportedFileFormats[format] {
		return nil, ErrBadRequest(CodeReqDocFormatInvalid, fmt.Sprintf("不支持的文件格式: %s，仅支持 docx/pdf/md/txt", format))
	}

	// 2. 校验文件大小（10MB）
	if input.FileSize > 10*1024*1024 {
		return nil, ErrBadRequest(CodeReqDocTooLarge, "文件大小不得超过 10MB")
	}

	// 3. 创建文档记录
	doc := &model.RequirementDoc{
		ProjectID:   input.ProjectID,
		Title:       input.Title,
		SourceType:  model.DocSourceTypeUpload,
		FileFormat:  format,
		FilePath:    input.FilePath,
		FileSize:    input.FileSize,
		ParseStatus: model.DocParseStatusNotParsed,
		Remark:      input.Remark,
		CreatedBy:   input.CreatedBy,
	}

	if err := s.docRepo.Create(ctx, doc); err != nil {
		s.logger.Error("创建需求文档失败", "error", err, "project_id", input.ProjectID)
		return nil, ErrInternal(CodeInternal, err)
	}

	s.logger.Info("需求文档上传成功",
		"doc_id", doc.ID, "project_id", input.ProjectID,
		"format", format, "file_size", input.FileSize,
	)

	return doc, nil
}

// CreateByPaste 通过粘贴文本创建需求文档。
// 直接将文本入库，标记为 parsed 状态。超过阈值会截断。
func (s *RequirementDocService) CreateByPaste(ctx context.Context, input CreateDocByPasteInput) (*model.RequirementDoc, error) {
	// 1. 校验文本内容非空
	content := strings.TrimSpace(input.RawContent)
	if content == "" {
		return nil, ErrBadRequest(CodeParamsError, "需求文本内容不能为空")
	}

	// 2. 统计字数并截断
	originalCount := utf8.RuneCountInString(content)
	truncated := false
	wordCount := originalCount
	if originalCount > maxDocContentChars {
		// 按 rune 截断到阈值
		runes := []rune(content)
		content = string(runes[:maxDocContentChars])
		wordCount = maxDocContentChars
		truncated = true
	}

	// 3. 创建文档记录（粘贴文本直接为 parsed 状态）
	doc := &model.RequirementDoc{
		ProjectID:         input.ProjectID,
		Title:             input.Title,
		SourceType:        model.DocSourceTypePaste,
		FileFormat:        "text",
		RawContent:        &content,
		WordCount:         wordCount,
		OriginalWordCount: originalCount,
		Truncated:         truncated,
		ParseStatus:       model.DocParseStatusParsed,
		Remark:            input.Remark,
		CreatedBy:         input.CreatedBy,
	}

	if err := s.docRepo.Create(ctx, doc); err != nil {
		s.logger.Error("创建粘贴文档失败", "error", err, "project_id", input.ProjectID)
		return nil, ErrInternal(CodeInternal, err)
	}

	s.logger.Info("粘贴文档创建成功",
		"doc_id", doc.ID, "project_id", input.ProjectID,
		"word_count", wordCount, "truncated", truncated,
	)

	return doc, nil
}

// GetByID 查询文档详情
func (s *RequirementDocService) GetByID(ctx context.Context, projectID, docID uint) (*model.RequirementDoc, error) {
	doc, err := s.docRepo.FindByID(ctx, docID)
	if err != nil {
		return nil, ErrNotFound(CodeReqDocNotFound, "需求文档不存在")
	}
	// 校验项目归属
	if doc.ProjectID != projectID {
		return nil, ErrNotFound(CodeReqDocNotFound, "需求文档不存在")
	}
	s.fillExternalSources(ctx, []model.RequirementDoc{*doc}, func(filled model.RequirementDoc) {
		doc.SourceURL = filled.SourceURL
		doc.SyncStatus = filled.SyncStatus
	})
	return doc, nil
}

// ListPaged 分页查询需求文档列表
func (s *RequirementDocService) ListPaged(ctx context.Context, projectID uint, f repository.RequirementDocFilter) ([]model.RequirementDoc, int64, error) {
	docs, total, err := s.docRepo.ListPaged(ctx, projectID, f)
	if err != nil {
		return nil, 0, err
	}
	s.fillExternalSources(ctx, docs, func(filled model.RequirementDoc) {
		for i := range docs {
			if docs[i].ID == filled.ID {
				docs[i].SourceURL = filled.SourceURL
				docs[i].SyncStatus = filled.SyncStatus
				break
			}
		}
	})
	return docs, total, nil
}

func (s *RequirementDocService) fillExternalSources(ctx context.Context, docs []model.RequirementDoc, apply func(model.RequirementDoc)) {
	if s.sourceRepo == nil || len(docs) == 0 {
		return
	}
	docIDs := make([]uint, 0, len(docs))
	for _, doc := range docs {
		if doc.ID > 0 {
			docIDs = append(docIDs, doc.ID)
		}
	}
	sources, err := s.sourceRepo.ListByDocIDs(ctx, docIDs)
	if err != nil {
		s.logger.Warn("回填需求文档来源失败", "error", err)
		return
	}
	sourceMap := make(map[uint]model.RequirementDocSource, len(sources))
	for _, source := range sources {
		sourceMap[source.RequirementDocID] = source
	}
	for _, doc := range docs {
		source, ok := sourceMap[doc.ID]
		if !ok {
			continue
		}
		doc.SourceURL = source.SourceURL
		doc.SyncStatus = source.SyncStatus
		apply(doc)
	}
}

// Delete 软删除需求文档
func (s *RequirementDocService) Delete(ctx context.Context, projectID, docID uint) error {
	doc, err := s.docRepo.FindByID(ctx, docID)
	if err != nil {
		return ErrNotFound(CodeReqDocNotFound, "需求文档不存在")
	}
	if doc.ProjectID != projectID {
		return ErrNotFound(CodeReqDocNotFound, "需求文档不存在")
	}

	if err := s.docRepo.SoftDelete(ctx, docID); err != nil {
		s.logger.Error("删除需求文档失败", "error", err, "doc_id", docID)
		return ErrInternal(CodeInternal, err)
	}

	s.logger.Info("需求文档已删除", "doc_id", docID, "project_id", projectID)
	return nil
}

// ========== 解析回调（Executor 调用） ==========

// MarkParsingStarted 标记文档开始解析（CAS: not_parsed → parsing）
func (s *RequirementDocService) MarkParsingStarted(ctx context.Context, docID uint) error {
	now := time.Now()
	affected, err := s.docRepo.CASParseStatus(ctx, docID,
		[]string{model.DocParseStatusNotParsed, model.DocParseStatusParseFailed},
		model.DocParseStatusParsing,
		map[string]interface{}{"parse_started_at": now},
	)
	if err != nil {
		return ErrInternal(CodeInternal, err)
	}
	if affected == 0 {
		return ErrConflict(CodeReqDocParsing, "文档当前状态不允许开始解析")
	}
	return nil
}

// MarkParsed 标记文档解析完成（CAS: parsing → parsed）
func (s *RequirementDocService) MarkParsed(ctx context.Context, docID uint, content string) error {
	// 截断处理
	originalCount := utf8.RuneCountInString(content)
	truncated := false
	wordCount := originalCount
	if originalCount > maxDocContentChars {
		runes := []rune(content)
		content = string(runes[:maxDocContentChars])
		wordCount = maxDocContentChars
		truncated = true
	}

	affected, err := s.docRepo.CASParseStatus(ctx, docID,
		[]string{model.DocParseStatusParsing},
		model.DocParseStatusParsed,
		map[string]interface{}{
			"raw_content":         content,
			"word_count":          wordCount,
			"original_word_count": originalCount,
			"truncated":           truncated,
		},
	)
	if err != nil {
		return ErrInternal(CodeInternal, err)
	}
	if affected == 0 {
		return ErrConflict(CodeReqDocParsing, "文档当前状态不允许标记解析完成")
	}

	s.logger.Info("文档解析完成", "doc_id", docID, "word_count", wordCount, "truncated", truncated)
	return nil
}

// MarkParseFailed 标记文档解析失败（CAS: parsing → parse_failed）
func (s *RequirementDocService) MarkParseFailed(ctx context.Context, docID uint, reason string) error {
	affected, err := s.docRepo.CASParseStatus(ctx, docID,
		[]string{model.DocParseStatusParsing},
		model.DocParseStatusParseFailed,
		map[string]interface{}{"parse_error": reason},
	)
	if err != nil {
		return ErrInternal(CodeInternal, err)
	}
	if affected == 0 {
		return ErrConflict(CodeReqDocParsing, "文档当前状态不允许标记解析失败")
	}

	s.logger.Warn("文档解析失败", "doc_id", docID, "reason", reason)
	return nil
}

// DispatchParse 将文件上传的文档派发给 Executor 异步解析
func (s *RequirementDocService) DispatchParse(ctx context.Context, doc *model.RequirementDoc) error {
	if s.executorURL == "" {
		s.logger.Warn("executor URL 未配置，跳过文档解析派发", "doc_id", doc.ID)
		return fmt.Errorf("executor url is empty")
	}

	// CAS: not_parsed → parsing
	if err := s.MarkParsingStarted(ctx, doc.ID); err != nil {
		return fmt.Errorf("mark parsing started: %w", err)
	}

	payload := map[string]interface{}{
		"doc_id":      doc.ID,
		"file_path":   doc.FilePath,
		"file_format": doc.FileFormat,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.executorURL+"/requirement-gen/parse-doc", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create dispatch request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", s.executorAPIKey)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		s.logger.Error("派发文档解析失败", "error", err, "doc_id", doc.ID)
		return fmt.Errorf("dispatch parse: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("executor dispatch status: %d", resp.StatusCode)
	}

	s.logger.Info("文档解析已派发", "doc_id", doc.ID, "status", resp.StatusCode)
	return nil
}

// RetryUnparsedDocs 重新派发所有 not_parsed 状态的文档解析
func (s *RequirementDocService) RetryUnparsedDocs(ctx context.Context, projectID uint) (int, error) {
	docs, _, err := s.docRepo.ListPaged(ctx, projectID, repository.RequirementDocFilter{
		ParseStatus: model.DocParseStatusNotParsed,
		Page:        1,
		PageSize:    100,
	})
	if err != nil {
		return 0, ErrInternal(CodeInternal, err)
	}

	dispatched := 0
	for i := range docs {
		if err := s.DispatchParse(ctx, &docs[i]); err != nil {
			s.logger.Warn("重试派发文档解析失败", "doc_id", docs[i].ID, "error", err)
			continue
		}
		dispatched++
	}

	s.logger.Info("批量重试文档解析完成", "project_id", projectID, "total", len(docs), "dispatched", dispatched)
	return dispatched, nil
}

// GetFileExtension 从文件名获取扩展名（不含点号，小写）
func GetFileExtension(filename string) string {
	ext := filepath.Ext(filename)
	if ext == "" {
		return ""
	}
	return strings.ToLower(ext[1:])
}

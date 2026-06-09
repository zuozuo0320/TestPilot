// gitlab_integration_service.go — GitLab Issue 接入业务服务
package service

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	gitlabapi "testpilot/internal/integration/gitlab"
	"testpilot/internal/model"
	"testpilot/internal/repository"
)

// GitLabIntegrationService 管理项目 GitLab 配置与 Issue 导入。
type GitLabIntegrationService struct {
	logger          *slog.Logger
	integrationRepo repository.ProjectIntegrationRepository
	sourceRepo      repository.RequirementDocSourceRepository
	docRepo         repository.RequirementDocRepository
	auditRepo       repository.AuditRepository
	txMgr           *repository.TxManager
	secret          string
	executorURL     string
	executorAPIKey  string
	httpClient      *http.Client
	executorClient  *http.Client
}

// NewGitLabIntegrationService 创建 GitLab 集成服务。
func NewGitLabIntegrationService(
	logger *slog.Logger,
	integrationRepo repository.ProjectIntegrationRepository,
	sourceRepo repository.RequirementDocSourceRepository,
	docRepo repository.RequirementDocRepository,
	auditRepo repository.AuditRepository,
	txMgr *repository.TxManager,
	secret string,
	executorURL string,
	executorAPIKey string,
) *GitLabIntegrationService {
	return &GitLabIntegrationService{
		logger:          logger.With("module", "gitlab_integration"),
		integrationRepo: integrationRepo,
		sourceRepo:      sourceRepo,
		docRepo:         docRepo,
		auditRepo:       auditRepo,
		txMgr:           txMgr,
		secret:          secret,
		executorURL:     strings.TrimRight(strings.TrimSpace(executorURL), "/"),
		executorAPIKey:  executorAPIKey,
		httpClient:      &http.Client{Timeout: 15 * time.Second},
		executorClient:  &http.Client{Timeout: 95 * time.Second},
	}
}

// GitLabConfigDTO 前端可见的 GitLab 配置。
type GitLabConfigDTO struct {
	Enabled         bool   `json:"enabled"`
	BaseURL         string `json:"base_url"`
	ProjectPath     string `json:"project_path"`
	TokenConfigured bool   `json:"token_configured"`
	TokenMask       string `json:"token_mask"`
}

// SaveGitLabConfigInput 保存 GitLab 配置输入。
type SaveGitLabConfigInput struct {
	ProjectID   uint
	BaseURL     string
	ProjectPath string
	Token       string // 为空时复用既有 Token，仅首次配置要求填写。
	Enabled     bool
	ActorID     uint
}

// ImportGitLabIssueInput 导入 GitLab Issue 输入。
type ImportGitLabIssueInput struct {
	ProjectID       uint
	IssueURL        string
	IncludeComments bool
	AnalyzeImages   bool
	ActorID         uint
}

type gitLabImageRef struct {
	Source string
	Alt    string
	URL    string
}

type gitLabImagePayload struct {
	Source      string `json:"source"`
	Alt         string `json:"alt"`
	URL         string `json:"url"`
	ContentType string `json:"content_type"`
	Base64Data  string `json:"base64_data"`
}

type gitLabImageAnalysisItem struct {
	Source  string `json:"source"`
	Alt     string `json:"alt"`
	URL     string `json:"url"`
	Summary string `json:"summary"`
	Error   string `json:"error"`
}

type gitLabImageAnalysisResult struct {
	Enabled    bool
	Images     []gitLabImageAnalysisItem
	ErrorCount int
}

const (
	maxGitLabIssueImages     = 6
	maxGitLabIssueImageBytes = 4 * 1024 * 1024
	gitLabImageAnalysisTitle = "## 图片附件视觉分析"
)

var (
	markdownImagePattern = regexp.MustCompile(`!\[([^\]]*)\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)
	htmlImagePattern     = regexp.MustCompile(`(?is)<img\b[^>]*>`)
	htmlSrcAttrPattern   = regexp.MustCompile(`(?is)\bsrc\s*=\s*["']([^"']+)["']`)
	htmlAltAttrPattern   = regexp.MustCompile(`(?is)\balt\s*=\s*["']([^"']*)["']`)
	uploadPathPattern    = regexp.MustCompile(`/uploads/([^/]+)/([^?#]+)$`)
)

// GetConfig 查询项目 GitLab 配置。
func (s *GitLabIntegrationService) GetConfig(ctx context.Context, projectID uint) (*GitLabConfigDTO, error) {
	integration, err := s.integrationRepo.FindByProvider(ctx, projectID, model.IntegrationProviderGitLab)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &GitLabConfigDTO{}, nil
		}
		return nil, ErrInternal(CodeInternal, err)
	}
	return toGitLabConfigDTO(integration), nil
}

// SaveConfig 保存或更新项目 GitLab 配置。
func (s *GitLabIntegrationService) SaveConfig(ctx context.Context, input SaveGitLabConfigInput) (*GitLabConfigDTO, error) {
	baseURL, err := normalizeGitLabBaseURL(input.BaseURL)
	if err != nil {
		return nil, ErrBadRequest(CodeReqGitLabConfigInvalid, "GitLab 地址无效")
	}
	projectPath := normalizeGitLabProjectPath(input.ProjectPath)
	if projectPath == "" {
		return nil, ErrBadRequest(CodeReqGitLabConfigInvalid, "GitLab 项目路径不能为空")
	}
	token := strings.TrimSpace(input.Token)
	tokenChanged := token != ""
	encryptedToken := ""
	tokenMask := ""
	if tokenChanged {
		encryptedToken, err = encryptSecret(s.secret, token)
		if err != nil {
			return nil, ErrInternal(CodeInternal, err)
		}
		tokenMask = maskToken(token)
	} else {
		existing, findErr := s.integrationRepo.FindByProvider(ctx, input.ProjectID, model.IntegrationProviderGitLab)
		if findErr != nil {
			if errors.Is(findErr, gorm.ErrRecordNotFound) {
				return nil, ErrBadRequest(CodeReqGitLabConfigInvalid, "首次配置 GitLab Token 不能为空")
			}
			return nil, ErrInternal(CodeInternal, findErr)
		}
		if existing.EncryptedToken == "" {
			return nil, ErrBadRequest(CodeReqGitLabConfigInvalid, "GitLab Token 不能为空")
		}
		encryptedToken = existing.EncryptedToken
		tokenMask = existing.TokenMask
	}
	integration := &model.ProjectIntegration{
		ProjectID:      input.ProjectID,
		Provider:       model.IntegrationProviderGitLab,
		BaseURL:        baseURL,
		ProjectPath:    projectPath,
		EncryptedToken: encryptedToken,
		TokenMask:      tokenMask,
		Enabled:        input.Enabled,
		CreatedBy:      input.ActorID,
		UpdatedBy:      input.ActorID,
	}
	if err := s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		if err := s.integrationRepo.Upsert(ctx, tx, integration); err != nil {
			return err
		}
		return s.auditRepo.WriteLogTx(tx, input.ActorID, "gitlab.config.save", "project", input.ProjectID, nil, map[string]any{
			"base_url":      baseURL,
			"project_path":  projectPath,
			"enabled":       input.Enabled,
			"token_mask":    integration.TokenMask,
			"token_changed": tokenChanged,
		})
	}); err != nil {
		s.logger.Error("保存 GitLab 配置失败", "project_id", input.ProjectID, "actor_id", input.ActorID, "error", err)
		return nil, ErrInternal(CodeInternal, err)
	}
	return toGitLabConfigDTO(integration), nil
}

// TestConfig 测试项目 GitLab 配置是否可用。
func (s *GitLabIntegrationService) TestConfig(ctx context.Context, projectID uint) error {
	client, _, err := s.clientForProject(ctx, projectID)
	if err != nil {
		return err
	}
	if err := client.TestConnection(ctx); err != nil {
		return s.mapGitLabError(err)
	}
	return nil
}

// ImportIssue 从 GitLab Issue 导入需求文档。
func (s *GitLabIntegrationService) ImportIssue(ctx context.Context, input ImportGitLabIssueInput) (*model.RequirementDoc, error) {
	client, integration, err := s.clientForProject(ctx, input.ProjectID)
	if err != nil {
		return nil, err
	}
	parsed, err := parseGitLabIssueURL(input.IssueURL)
	if err != nil {
		return nil, ErrBadRequest(CodeReqGitLabIssueURLInvalid, "请输入有效的 GitLab Issue URL")
	}
	if !sameGitLabHost(integration.BaseURL, parsed.BaseURL) {
		return nil, ErrBadRequest(CodeReqGitLabIssueURLInvalid, "Issue URL 必须属于当前项目配置的 GitLab 地址")
	}
	if parsed.ProjectPath != integration.ProjectPath {
		return nil, ErrBadRequest(CodeReqGitLabIssueURLInvalid, "Issue URL 必须属于当前配置的 GitLab 项目")
	}

	bundle, err := client.FetchIssue(ctx, integration.ProjectPath, parsed.IssueIID, input.IncludeComments)
	if err != nil {
		return nil, s.mapGitLabError(err)
	}
	imageAnalysis := s.analyzeIssueImages(ctx, integration, bundle, input.IncludeComments, input.AnalyzeImages)
	content, originalCount, wordCount, truncated := normalizeIssueContent(bundle, integration.ProjectPath, input.IncludeComments, imageAnalysis)
	externalKey := buildGitLabExternalKey(integration.BaseURL, integration.ProjectPath, parsed.IssueIID)
	versionNo := 1
	if latest, err := s.sourceRepo.FindLatestByExternalKey(ctx, input.ProjectID, model.DocSourceTypeGitLabIssue, externalKey); err == nil {
		versionNo = latest.VersionNo
		if sameExternalUpdate(latest.ExternalUpdatedAt, bundle.Issue.UpdatedAt) {
			doc, docErr := s.docRepo.FindByID(ctx, latest.RequirementDocID)
			if docErr == nil && doc.ProjectID == input.ProjectID {
				if !input.AnalyzeImages || docContainsImageAnalysis(doc) {
					fillDocSource(doc, latest)
					return doc, nil
				}
			}
		}
		versionNo = latest.VersionNo + 1
	}

	now := time.Now()
	doc := &model.RequirementDoc{
		ProjectID:         input.ProjectID,
		Title:             "GitLab Issue #" + strconv.Itoa(parsed.IssueIID) + " " + strings.TrimSpace(bundle.Issue.Title),
		SourceType:        model.DocSourceTypeGitLabIssue,
		FileFormat:        "markdown",
		RawContent:        &content,
		WordCount:         wordCount,
		OriginalWordCount: originalCount,
		Truncated:         truncated,
		ParseStatus:       model.DocParseStatusParsed,
		Remark:            "由 GitLab Issue 导入",
		CreatedBy:         input.ActorID,
	}
	source := &model.RequirementDocSource{
		ProjectID:           input.ProjectID,
		SourceType:          model.DocSourceTypeGitLabIssue,
		ExternalSystem:      model.IntegrationProviderGitLab,
		SourceURL:           firstNonEmpty(bundle.Issue.WebURL, strings.TrimSpace(input.IssueURL)),
		ExternalProjectID:   strconv.Itoa(bundle.Issue.ProjectID),
		ExternalProjectPath: integration.ProjectPath,
		ExternalIssueIID:    parsed.IssueIID,
		ExternalKey:         externalKey,
		VersionNo:           versionNo,
		ExternalUpdatedAt:   bundle.Issue.UpdatedAt,
		LastSyncedAt:        &now,
		SyncStatus:          model.DocSourceSyncStatusSynced,
		CreatedBy:           input.ActorID,
	}
	if err := s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		if err := s.docRepo.CreateTx(ctx, tx, doc); err != nil {
			return err
		}
		source.RequirementDocID = doc.ID
		if err := s.sourceRepo.Create(ctx, tx, source); err != nil {
			return err
		}
		return s.auditRepo.WriteLogTx(tx, input.ActorID, "gitlab.issue.import", "requirement_doc", doc.ID, nil, map[string]any{
			"source_url":       source.SourceURL,
			"issue_iid":        parsed.IssueIID,
			"project_path":     integration.ProjectPath,
			"include_comments": input.IncludeComments,
			"analyze_images":   input.AnalyzeImages,
			"image_count":      len(imageAnalysis.Images),
			"image_errors":     imageAnalysis.ErrorCount,
			"version_no":       versionNo,
		})
	}); err != nil {
		s.logger.Error("导入 GitLab Issue 失败", "project_id", input.ProjectID, "actor_id", input.ActorID, "issue_iid", parsed.IssueIID, "error", err)
		return nil, ErrInternal(CodeReqGitLabImportFailed, err)
	}
	fillDocSource(doc, source)
	s.logger.Info("导入 GitLab Issue 成功", "project_id", input.ProjectID, "doc_id", doc.ID, "issue_iid", parsed.IssueIID, "version_no", versionNo)
	return doc, nil
}

func (s *GitLabIntegrationService) clientForProject(ctx context.Context, projectID uint) (*gitlabapi.Client, *model.ProjectIntegration, error) {
	integration, err := s.integrationRepo.FindByProvider(ctx, projectID, model.IntegrationProviderGitLab)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrPreconditionFailed(CodeReqGitLabConfigMissing, "请先配置 GitLab 集成")
		}
		return nil, nil, ErrInternal(CodeInternal, err)
	}
	if !integration.Enabled {
		return nil, nil, ErrPreconditionFailed(CodeReqGitLabConfigMissing, "GitLab 集成未启用")
	}
	token, err := decryptSecret(s.secret, integration.EncryptedToken)
	if err != nil || token == "" {
		return nil, nil, ErrPreconditionFailed(CodeReqGitLabConfigInvalid, "GitLab Token 无法解密，请重新保存配置")
	}
	client, err := gitlabapi.NewClient(integration.BaseURL, token, s.httpClient)
	if err != nil {
		return nil, nil, ErrBadRequest(CodeReqGitLabConfigInvalid, "GitLab 配置无效")
	}
	return client, integration, nil
}

func (s *GitLabIntegrationService) mapGitLabError(err error) error {
	var apiErr *gitlabapi.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return ErrPreconditionFailed(CodeReqGitLabTokenInvalid, "GitLab Token 无效或权限不足")
		case http.StatusNotFound:
			return ErrNotFound(CodeReqGitLabIssueNotFound, "GitLab Issue 不存在")
		default:
			return ErrServiceUnavailable(CodeReqGitLabUnavailable, "GitLab 服务暂不可用")
		}
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return ErrServiceUnavailable(CodeReqGitLabUnavailable, "GitLab 请求超时")
	}
	return ErrServiceUnavailable(CodeReqGitLabUnavailable, "GitLab 服务暂不可用")
}

func toGitLabConfigDTO(integration *model.ProjectIntegration) *GitLabConfigDTO {
	if integration == nil {
		return &GitLabConfigDTO{}
	}
	return &GitLabConfigDTO{
		Enabled:         integration.Enabled,
		BaseURL:         integration.BaseURL,
		ProjectPath:     integration.ProjectPath,
		TokenConfigured: integration.EncryptedToken != "",
		TokenMask:       integration.TokenMask,
	}
}

type parsedGitLabIssueURL struct {
	BaseURL     string
	ProjectPath string
	IssueIID    int
}

var gitLabIssuePathPattern = regexp.MustCompile(`^/(.+)/-/issues/([1-9][0-9]*)/?$`)

func parseGitLabIssueURL(raw string) (*parsedGitLabIssueURL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid issue url")
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return nil, fmt.Errorf("invalid issue url scheme")
	}
	matches := gitLabIssuePathPattern.FindStringSubmatch(parsed.Path)
	if len(matches) != 3 {
		return nil, fmt.Errorf("invalid issue url path")
	}
	iid, err := strconv.Atoi(matches[2])
	if err != nil || iid <= 0 {
		return nil, fmt.Errorf("invalid issue iid")
	}
	projectPath, err := url.PathUnescape(matches[1])
	if err != nil || projectPath == "" {
		return nil, fmt.Errorf("invalid project path")
	}
	return &parsedGitLabIssueURL{
		BaseURL:     parsed.Scheme + "://" + parsed.Host,
		ProjectPath: normalizeGitLabProjectPath(projectPath),
		IssueIID:    iid,
	}, nil
}

func normalizeGitLabBaseURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(raw), "/"))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid gitlab base url")
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return "", fmt.Errorf("invalid gitlab base url scheme")
	}
	return parsed.Scheme + "://" + parsed.Host, nil
}

func normalizeGitLabProjectPath(raw string) string {
	value := strings.TrimSpace(raw)
	value = strings.Trim(value, "/")
	value = strings.TrimSuffix(value, ".git")
	return value
}

func sameGitLabHost(configBaseURL, issueBaseURL string) bool {
	configParsed, err1 := url.Parse(strings.TrimSpace(configBaseURL))
	issueParsed, err2 := url.Parse(strings.TrimSpace(issueBaseURL))
	if err1 != nil || err2 != nil {
		return false
	}
	return strings.EqualFold(configParsed.Host, issueParsed.Host) && strings.EqualFold(configParsed.Scheme, issueParsed.Scheme)
}

func normalizeIssueContent(bundle *gitlabapi.IssueBundle, projectPath string, includeComments bool, imageAnalysis gitLabImageAnalysisResult) (string, int, int, bool) {
	var b strings.Builder
	issue := bundle.Issue
	b.WriteString("# GitLab Issue: ")
	b.WriteString(strings.TrimSpace(issue.Title))
	b.WriteString("\n\n")
	writeLine(&b, "来源", issue.WebURL)
	writeLine(&b, "项目", projectPath)
	writeLine(&b, "Issue IID", strconv.Itoa(issue.IID))
	writeLine(&b, "状态", issue.State)
	writeLine(&b, "Labels", strings.Join(issue.Labels, ", "))
	if issue.Milestone != nil {
		writeLine(&b, "Milestone", issue.Milestone.Title)
	}
	assignees := make([]string, 0, len(issue.Assignees))
	for _, assignee := range issue.Assignees {
		assignees = append(assignees, formatGitLabUser(assignee.Name, assignee.Username))
	}
	sort.Strings(assignees)
	writeLine(&b, "Assignee", strings.Join(assignees, ", "))
	if issue.UpdatedAt != nil {
		writeLine(&b, "更新时间", issue.UpdatedAt.Format(time.RFC3339))
	}
	b.WriteString("\n## 描述\n\n")
	if strings.TrimSpace(issue.Description) == "" {
		b.WriteString("（无描述）\n")
	} else {
		b.WriteString(strings.TrimSpace(issue.Description))
		b.WriteString("\n")
	}
	if includeComments {
		b.WriteString("\n## 评论/补充说明\n\n")
		wrote := false
		for _, note := range bundle.Notes {
			if note.System || strings.TrimSpace(note.Body) == "" {
				continue
			}
			wrote = true
			b.WriteString("### ")
			b.WriteString(formatGitLabUser(note.Author.Name, note.Author.Username))
			if note.UpdatedAt != nil {
				b.WriteString(" · ")
				b.WriteString(note.UpdatedAt.Format(time.RFC3339))
			}
			b.WriteString("\n\n")
			b.WriteString(strings.TrimSpace(note.Body))
			b.WriteString("\n\n")
		}
		if !wrote {
			b.WriteString("（无用户评论）\n")
		}
	}
	appendImageAnalysisSection(&b, imageAnalysis)
	content := strings.TrimSpace(b.String())
	originalCount := utf8.RuneCountInString(content)
	wordCount := originalCount
	truncated := false
	if originalCount > maxDocContentChars {
		runes := []rune(content)
		content = string(runes[:maxDocContentChars])
		wordCount = maxDocContentChars
		truncated = true
	}
	return content, originalCount, wordCount, truncated
}

func appendImageAnalysisSection(b *strings.Builder, imageAnalysis gitLabImageAnalysisResult) {
	if !imageAnalysis.Enabled {
		return
	}
	b.WriteString("\n")
	b.WriteString(gitLabImageAnalysisTitle)
	b.WriteString("\n\n")
	if len(imageAnalysis.Images) == 0 {
		b.WriteString("未识别到可分析的图片附件，或图片附件均未通过安全校验。\n")
		return
	}
	for index, item := range imageAnalysis.Images {
		b.WriteString("### 图片 ")
		b.WriteString(strconv.Itoa(index + 1))
		if strings.TrimSpace(item.Alt) != "" {
			b.WriteString("：")
			b.WriteString(strings.TrimSpace(item.Alt))
		}
		b.WriteString("\n\n")
		writeLine(b, "来源位置", item.Source)
		writeLine(b, "图片链接", item.URL)
		if strings.TrimSpace(item.Error) != "" {
			writeLine(b, "分析状态", "失败")
			writeLine(b, "失败原因", item.Error)
			b.WriteString("\n")
			continue
		}
		b.WriteString(strings.TrimSpace(item.Summary))
		b.WriteString("\n\n")
	}
	if imageAnalysis.ErrorCount > 0 {
		writeLine(b, "图片分析失败数量", strconv.Itoa(imageAnalysis.ErrorCount))
	}
}

func docContainsImageAnalysis(doc *model.RequirementDoc) bool {
	if doc == nil || doc.RawContent == nil {
		return false
	}
	return strings.Contains(*doc.RawContent, gitLabImageAnalysisTitle)
}

func (s *GitLabIntegrationService) analyzeIssueImages(ctx context.Context, integration *model.ProjectIntegration, bundle *gitlabapi.IssueBundle, includeComments, enabled bool) gitLabImageAnalysisResult {
	result := gitLabImageAnalysisResult{Enabled: enabled}
	if !enabled {
		return result
	}
	refs := collectGitLabImageRefs(integration.BaseURL, integration.ProjectPath, bundle, includeComments)
	if len(refs) == 0 {
		return result
	}
	if s.executorURL == "" {
		result.ErrorCount = len(refs)
		for _, ref := range refs {
			result.Images = append(result.Images, gitLabImageAnalysisItem{
				Source: ref.Source,
				Alt:    ref.Alt,
				URL:    ref.URL,
				Error:  "Executor 未配置，无法进行图片视觉分析",
			})
		}
		return result
	}
	token, err := decryptSecret(s.secret, integration.EncryptedToken)
	if err != nil || token == "" {
		result.ErrorCount = len(refs)
		for _, ref := range refs {
			result.Images = append(result.Images, gitLabImageAnalysisItem{
				Source: ref.Source,
				Alt:    ref.Alt,
				URL:    ref.URL,
				Error:  "GitLab Token 无法解密，无法下载图片",
			})
		}
		return result
	}

	payloads := make([]gitLabImagePayload, 0, len(refs))
	for _, ref := range refs {
		payload, downloadErr := s.downloadGitLabImage(ctx, integration, token, ref)
		if downloadErr != nil {
			result.ErrorCount += 1
			result.Images = append(result.Images, gitLabImageAnalysisItem{
				Source: ref.Source,
				Alt:    ref.Alt,
				URL:    ref.URL,
				Error:  downloadErr.Error(),
			})
			continue
		}
		payloads = append(payloads, *payload)
	}
	if len(payloads) == 0 {
		return result
	}

	analysisItems, err := s.callExecutorAnalyzeImages(ctx, payloads)
	if err != nil {
		s.logger.Warn("GitLab 图片视觉分析失败，降级为文本导入", "error", err, "image_count", len(payloads))
		result.ErrorCount += len(payloads)
		for _, payload := range payloads {
			result.Images = append(result.Images, gitLabImageAnalysisItem{
				Source: payload.Source,
				Alt:    payload.Alt,
				URL:    payload.URL,
				Error:  "Executor 图片分析失败：" + err.Error(),
			})
		}
		return result
	}
	for _, item := range analysisItems {
		if strings.TrimSpace(item.Error) != "" {
			result.ErrorCount += 1
		}
		result.Images = append(result.Images, item)
	}
	return result
}

func collectGitLabImageRefs(baseURL, projectPath string, bundle *gitlabapi.IssueBundle, includeComments bool) []gitLabImageRef {
	if bundle == nil {
		return nil
	}
	refs := make([]gitLabImageRef, 0)
	seen := make(map[string]struct{})
	addFromText := func(source, text string) {
		for _, ref := range extractGitLabImageRefs(baseURL, projectPath, source, text) {
			key := strings.ToLower(ref.URL)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			refs = append(refs, ref)
			if len(refs) >= maxGitLabIssueImages {
				return
			}
		}
	}
	addFromText("Issue 描述", bundle.Issue.Description)
	if includeComments && len(refs) < maxGitLabIssueImages {
		for _, note := range bundle.Notes {
			if note.System || strings.TrimSpace(note.Body) == "" {
				continue
			}
			addFromText("评论/"+formatGitLabUser(note.Author.Name, note.Author.Username), note.Body)
			if len(refs) >= maxGitLabIssueImages {
				break
			}
		}
	}
	return refs
}

func extractGitLabImageRefs(baseURL, projectPath, source, text string) []gitLabImageRef {
	refs := make([]gitLabImageRef, 0)
	for _, match := range markdownImagePattern.FindAllStringSubmatch(text, -1) {
		if len(match) < 3 {
			continue
		}
		if ref, ok := buildGitLabImageRef(baseURL, projectPath, source, match[1], match[2]); ok {
			refs = append(refs, ref)
		}
	}
	for _, match := range htmlImagePattern.FindAllString(text, -1) {
		srcMatch := htmlSrcAttrPattern.FindStringSubmatch(match)
		if len(srcMatch) < 2 {
			continue
		}
		alt := ""
		if altMatch := htmlAltAttrPattern.FindStringSubmatch(match); len(altMatch) >= 2 {
			alt = altMatch[1]
		}
		if ref, ok := buildGitLabImageRef(baseURL, projectPath, source, alt, srcMatch[1]); ok {
			refs = append(refs, ref)
		}
	}
	return refs
}

func buildGitLabImageRef(baseURL, projectPath, source, alt, rawURL string) (gitLabImageRef, bool) {
	resolved, err := resolveGitLabImageURL(baseURL, projectPath, rawURL)
	if err != nil || resolved == "" {
		return gitLabImageRef{}, false
	}
	return gitLabImageRef{
		Source: source,
		Alt:    strings.TrimSpace(alt),
		URL:    resolved,
	}, true
}

func resolveGitLabImageURL(baseURL, projectPath, raw string) (string, error) {
	value := strings.TrimSpace(raw)
	value = strings.Trim(value, "<>")
	if value == "" || strings.HasPrefix(strings.ToLower(value), "data:") {
		return "", fmt.Errorf("unsupported image url")
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return "", err
	}
	if parsed.Scheme != "" && parsed.Host != "" {
		return parsed.String(), nil
	}
	baseParsed, err := url.Parse(strings.TrimRight(baseURL, "/") + "/")
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(value, "/") {
		return baseParsed.ResolveReference(parsed).String(), nil
	}
	if strings.HasPrefix(value, "../") {
		return "", fmt.Errorf("unsupported relative image url")
	}
	value = strings.TrimPrefix(value, "./")
	projectBase, err := url.Parse(strings.TrimRight(baseURL, "/") + "/" + strings.Trim(projectPath, "/") + "/")
	if err != nil {
		return "", err
	}
	relative, err := url.Parse(value)
	if err != nil {
		return "", err
	}
	return projectBase.ResolveReference(relative).String(), nil
}

func (s *GitLabIntegrationService) downloadGitLabImage(ctx context.Context, integration *model.ProjectIntegration, token string, ref gitLabImageRef) (*gitLabImagePayload, error) {
	target, err := buildGitLabImageDownloadURL(integration, ref.URL)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("PRIVATE-TOKEN", token)
	req.Header.Set("Accept", "image/*")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("下载图片失败")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("下载图片返回 %d", resp.StatusCode)
	}
	if resp.ContentLength > maxGitLabIssueImageBytes {
		return nil, fmt.Errorf("图片超过 %dMB 限制", maxGitLabIssueImageBytes/1024/1024)
	}
	limited := io.LimitReader(resp.Body, maxGitLabIssueImageBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("读取图片失败")
	}
	if len(body) > maxGitLabIssueImageBytes {
		return nil, fmt.Errorf("图片超过 %dMB 限制", maxGitLabIssueImageBytes/1024/1024)
	}
	contentType := normalizeImageContentType(resp.Header.Get("Content-Type"), body)
	if contentType == "" {
		return nil, fmt.Errorf("仅支持 PNG、JPEG、WebP、GIF 图片")
	}
	return &gitLabImagePayload{
		Source:      ref.Source,
		Alt:         ref.Alt,
		URL:         ref.URL,
		ContentType: contentType,
		Base64Data:  base64.StdEncoding.EncodeToString(body),
	}, nil
}

func buildGitLabImageDownloadURL(integration *model.ProjectIntegration, rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("图片 URL 无效")
	}
	if !sameGitLabHost(integration.BaseURL, parsed.Scheme+"://"+parsed.Host) {
		return "", fmt.Errorf("图片 URL 必须属于当前 GitLab 地址")
	}
	if uploadMatch := uploadPathPattern.FindStringSubmatch(parsed.Path); len(uploadMatch) == 3 {
		if !isGitLabUploadPathInProject(parsed.Path, integration.ProjectPath) {
			return "", fmt.Errorf("图片 URL 必须属于当前 GitLab 项目")
		}
		return strings.TrimRight(integration.BaseURL, "/") +
			"/api/v4/projects/" +
			encodeGitLabProjectPath(integration.ProjectPath) +
			"/uploads/" +
			url.PathEscape(uploadMatch[1]) +
			"/" +
			url.PathEscape(uploadMatch[2]), nil
	}
	if !strings.Contains(parsed.Path, "/"+strings.Trim(integration.ProjectPath, "/")+"/") &&
		!strings.HasPrefix(parsed.Path, "/"+strings.Trim(integration.ProjectPath, "/")+"/") {
		return "", fmt.Errorf("图片 URL 必须属于当前 GitLab 项目")
	}
	return parsed.String(), nil
}

func isGitLabUploadPathInProject(path, projectPath string) bool {
	path = "/" + strings.Trim(strings.TrimSpace(path), "/")
	if strings.HasPrefix(path, "/uploads/") {
		return true
	}
	projectPrefix := "/" + strings.Trim(normalizeGitLabProjectPath(projectPath), "/") + "/"
	return strings.HasPrefix(path, projectPrefix)
}

func normalizeImageContentType(header string, body []byte) string {
	contentType := strings.TrimSpace(header)
	if contentType != "" {
		if parsed, _, err := mime.ParseMediaType(contentType); err == nil {
			contentType = parsed
		}
	}
	if contentType == "" || contentType == "application/octet-stream" {
		contentType = http.DetectContentType(body)
	}
	switch strings.ToLower(contentType) {
	case "image/png", "image/jpeg", "image/webp", "image/gif":
		return strings.ToLower(contentType)
	default:
		return ""
	}
}

func (s *GitLabIntegrationService) callExecutorAnalyzeImages(ctx context.Context, images []gitLabImagePayload) ([]gitLabImageAnalysisItem, error) {
	body, err := json.Marshal(map[string]any{"images": images})
	if err != nil {
		return nil, err
	}
	reqCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, s.executorURL+"/requirement-gen/analyze-images", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.executorAPIKey != "" {
		req.Header.Set("X-API-Key", s.executorAPIKey)
	}
	resp, err := s.executorClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Executor 不可用")
	}
	defer resp.Body.Close()
	respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if readErr != nil {
		return nil, fmt.Errorf("读取 Executor 响应失败")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Executor 返回 %d", resp.StatusCode)
	}
	var decoded struct {
		Status string                    `json:"status"`
		Images []gitLabImageAnalysisItem `json:"images"`
		Error  string                    `json:"error"`
	}
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("解析 Executor 响应失败")
	}
	if decoded.Status != "ok" {
		if decoded.Error != "" {
			return nil, fmt.Errorf("%s", decoded.Error)
		}
		return nil, fmt.Errorf("Executor 图片分析失败")
	}
	return decoded.Images, nil
}

func encodeGitLabProjectPath(projectPath string) string {
	return strings.ReplaceAll(url.PathEscape(projectPath), "/", "%2F")
}

func writeLine(b *strings.Builder, label, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	b.WriteString("- ")
	b.WriteString(label)
	b.WriteString("：")
	b.WriteString(value)
	b.WriteString("\n")
}

func formatGitLabUser(name, username string) string {
	name = strings.TrimSpace(name)
	username = strings.TrimSpace(username)
	if name == "" {
		if username == "" {
			return "未知用户"
		}
		return "@" + username
	}
	if username == "" {
		return name
	}
	return name + "(@" + username + ")"
}

func buildGitLabExternalKey(baseURL, projectPath string, issueIID int) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimRight(baseURL, "/")) + "|" + projectPath + "|" + strconv.Itoa(issueIID)))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func sameExternalUpdate(left, right *time.Time) bool {
	if left == nil && right == nil {
		return true
	}
	if left == nil || right == nil {
		return false
	}
	return left.Equal(*right)
}

func fillDocSource(doc *model.RequirementDoc, source *model.RequirementDocSource) {
	if doc == nil || source == nil {
		return
	}
	doc.SourceURL = source.SourceURL
	doc.SyncStatus = source.SyncStatus
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func maskToken(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	runes := []rune(token)
	if len(runes) <= 8 {
		return "****"
	}
	return string(runes[:5]) + "****" + string(runes[len(runes)-4:])
}

func encryptSecret(secret, plaintext string) (string, error) {
	block, err := aes.NewCipher(deriveSecretKey(secret))
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	payload := append(nonce, ciphertext...)
	return base64.StdEncoding.EncodeToString(payload), nil
}

func decryptSecret(secret, encrypted string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encrypted))
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(deriveSecretKey(secret))
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", fmt.Errorf("invalid encrypted secret")
	}
	nonce := raw[:gcm.NonceSize()]
	ciphertext := raw[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func deriveSecretKey(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

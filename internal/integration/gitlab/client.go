// Package gitlab 封装 GitLab API 访问。
package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// APIError 表示 GitLab API 返回的非 2xx 错误。
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("gitlab api status=%d message=%s", e.StatusCode, e.Message)
}

// Client GitLab API 客户端。
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// Issue GitLab Issue 关键信息。
type Issue struct {
	ID          int        `json:"id"`
	IID         int        `json:"iid"`
	ProjectID   int        `json:"project_id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	State       string     `json:"state"`
	Labels      []string   `json:"labels"`
	WebURL      string     `json:"web_url"`
	UpdatedAt   *time.Time `json:"updated_at"`
	Milestone   *struct {
		Title string `json:"title"`
	} `json:"milestone"`
	Assignees []struct {
		Name     string `json:"name"`
		Username string `json:"username"`
	} `json:"assignees"`
}

// Note GitLab Issue 评论。
type Note struct {
	ID        int        `json:"id"`
	Body      string     `json:"body"`
	System    bool       `json:"system"`
	UpdatedAt *time.Time `json:"updated_at"`
	Author    struct {
		Name     string `json:"name"`
		Username string `json:"username"`
	} `json:"author"`
}

// IssueBundle Issue 与评论聚合结果。
type IssueBundle struct {
	Issue Issue
	Notes []Note
}

// NewClient 创建 GitLab API 客户端。
func NewClient(baseURL, token string, httpClient *http.Client) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	token = strings.TrimSpace(token)
	if baseURL == "" || token == "" {
		return nil, fmt.Errorf("gitlab base_url and token are required")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid gitlab base_url")
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return nil, fmt.Errorf("unsupported gitlab base_url scheme")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{baseURL: baseURL, token: token, httpClient: httpClient}, nil
}

// FetchIssue 获取单个 Issue；includeComments 为 true 时附带用户评论。
func (c *Client) FetchIssue(ctx context.Context, projectPath string, issueIID int, includeComments bool) (*IssueBundle, error) {
	var issue Issue
	if err := c.get(ctx, c.issuePath(projectPath, issueIID), nil, &issue); err != nil {
		return nil, err
	}
	bundle := &IssueBundle{Issue: issue}
	if includeComments {
		notes, err := c.fetchIssueNotes(ctx, projectPath, issueIID)
		if err != nil {
			return nil, err
		}
		bundle.Notes = notes
	}
	return bundle, nil
}

// TestConnection 通过轻量用户接口校验 Token 是否可用。
func (c *Client) TestConnection(ctx context.Context) error {
	var result struct {
		ID       int    `json:"id"`
		Username string `json:"username"`
	}
	return c.get(ctx, "/api/v4/user", nil, &result)
}

func (c *Client) fetchIssueNotes(ctx context.Context, projectPath string, issueIID int) ([]Note, error) {
	notes := make([]Note, 0)
	for page := 1; page <= 3; page += 1 {
		query := url.Values{}
		query.Set("activity_filter", "only_comments")
		query.Set("sort", "asc")
		query.Set("per_page", "100")
		query.Set("page", strconv.Itoa(page))

		var pageNotes []Note
		if err := c.get(ctx, c.issuePath(projectPath, issueIID)+"/notes", query, &pageNotes); err != nil {
			return nil, err
		}
		if len(pageNotes) == 0 {
			break
		}
		notes = append(notes, pageNotes...)
		if len(pageNotes) < 100 {
			break
		}
	}
	return notes, nil
}

func (c *Client) issuePath(projectPath string, issueIID int) string {
	return fmt.Sprintf("/api/v4/projects/%s/issues/%d", encodeProjectPath(projectPath), issueIID)
}

func encodeProjectPath(projectPath string) string {
	// GitLab 要求命名空间项目路径整体 URL 编码，例如 group/sub/project -> group%2Fsub%2Fproject。
	return strings.ReplaceAll(url.PathEscape(projectPath), "/", "%2F")
}

func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	target := c.baseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	req.Header.Set("PRIVATE-TOKEN", c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var body struct {
			Message any `json:"message"`
			Error   any `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		msg := fmt.Sprint(body.Message)
		if msg == "<nil>" || msg == "" {
			msg = fmt.Sprint(body.Error)
		}
		return &APIError{StatusCode: resp.StatusCode, Message: msg}
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

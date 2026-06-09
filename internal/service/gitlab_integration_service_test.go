package service

import (
	"strings"
	"testing"

	gitlabapi "testpilot/internal/integration/gitlab"
	"testpilot/internal/model"
)

// TestBuildGitLabImageDownloadURLForProjectUpload 验证项目内上传图片会转为 GitLab API 下载地址。
func TestBuildGitLabImageDownloadURLForProjectUpload(t *testing.T) {
	integration := &model.ProjectIntegration{
		BaseURL:     "https://gitlab.example.com",
		ProjectPath: "group/sub/project",
	}

	target, err := buildGitLabImageDownloadURL(
		integration,
		"https://gitlab.example.com/group/sub/project/uploads/abc123/screen.png",
	)
	if err != nil {
		t.Fatalf("build download url failed: %v", err)
	}

	want := "https://gitlab.example.com/api/v4/projects/group%2Fsub%2Fproject/uploads/abc123/screen.png"
	if target != want {
		t.Fatalf("unexpected target:\nwant=%s\ngot =%s", want, target)
	}
}

// TestBuildGitLabImageDownloadURLRejectsForeignProject 验证同域其他项目的上传图片不会被下载。
func TestBuildGitLabImageDownloadURLRejectsForeignProject(t *testing.T) {
	integration := &model.ProjectIntegration{
		BaseURL:     "https://gitlab.example.com",
		ProjectPath: "group/sub/project",
	}

	_, err := buildGitLabImageDownloadURL(
		integration,
		"https://gitlab.example.com/other/project/uploads/abc123/screen.png",
	)
	if err == nil {
		t.Fatal("expected foreign project image to be rejected")
	}
	if !strings.Contains(err.Error(), "当前 GitLab 项目") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestCollectGitLabImageRefs 验证描述和评论中的 Markdown/HTML 图片会被提取并去重。
func TestCollectGitLabImageRefs(t *testing.T) {
	var note gitlabapi.Note
	note.Body = `<img alt="流程图" src="/group/sub/project/uploads/def456/flow.png">`
	note.Author.Name = "测试"
	note.Author.Username = "tester"

	bundle := &gitlabapi.IssueBundle{
		Issue: gitlabapi.Issue{
			Description: "![截图](/uploads/abc123/screen.png)\n![重复](/uploads/abc123/screen.png)",
		},
		Notes: []gitlabapi.Note{note},
	}

	refs := collectGitLabImageRefs("https://gitlab.example.com", "group/sub/project", bundle, true)
	if len(refs) != 2 {
		t.Fatalf("expected 2 refs, got %d: %+v", len(refs), refs)
	}
	if refs[0].Alt != "截图" || refs[0].Source != "Issue 描述" {
		t.Fatalf("unexpected first ref: %+v", refs[0])
	}
	if refs[1].Alt != "流程图" || !strings.Contains(refs[1].Source, "评论/") {
		t.Fatalf("unexpected second ref: %+v", refs[1])
	}
}

// TestNormalizeIssueContentAppendsImageAnalysis 验证图片视觉分析会进入导入后的 Markdown 正文。
func TestNormalizeIssueContentAppendsImageAnalysis(t *testing.T) {
	content, _, _, truncated := normalizeIssueContent(
		&gitlabapi.IssueBundle{
			Issue: gitlabapi.Issue{
				Title:       "资产台账删除 IP",
				IID:         12,
				State:       "opened",
				Description: "删除 IP 时需要校验关联设备台账。",
			},
		},
		"group/sub/project",
		false,
		gitLabImageAnalysisResult{
			Enabled: true,
			Images: []gitLabImageAnalysisItem{
				{
					Source:  "Issue 描述",
					Alt:     "弹窗截图",
					URL:     "https://gitlab.example.com/group/sub/project/uploads/abc123/screen.png",
					Summary: "截图展示删除确认弹窗，需要校验关联设备台账被并发修改时的错误提示。",
				},
			},
		},
	)

	if truncated {
		t.Fatal("content should not be truncated")
	}
	if !strings.Contains(content, gitLabImageAnalysisTitle) {
		t.Fatalf("image analysis title missing:\n%s", content)
	}
	if !strings.Contains(content, "弹窗截图") || !strings.Contains(content, "并发修改") {
		t.Fatalf("image analysis content missing:\n%s", content)
	}
}

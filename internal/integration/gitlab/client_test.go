package gitlab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchIssueEncodesNamespaceProjectPath(t *testing.T) {
	t.Helper()

	var seenIssuePath string
	var seenNotesPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PRIVATE-TOKEN") != "test-token" {
			t.Fatalf("expected private token header, got %q", r.Header.Get("PRIVATE-TOKEN"))
		}
		switch r.URL.EscapedPath() {
		case "/api/v4/projects/group%2Fsub%2Fproject/issues/12":
			seenIssuePath = r.URL.EscapedPath()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":          1001,
				"iid":         12,
				"project_id":  99,
				"title":       "需求标题",
				"description": "需求描述",
				"state":       "opened",
				"web_url":     "https://gitlab.example.com/group/sub/project/-/issues/12",
			})
		case "/api/v4/projects/group%2Fsub%2Fproject/issues/12/notes":
			seenNotesPath = r.URL.EscapedPath()
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"id":     1,
					"body":   "补充验收标准",
					"system": false,
					"author": map[string]any{"name": "Tester", "username": "tester"},
				},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.EscapedPath())
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "test-token", server.Client())
	if err != nil {
		t.Fatalf("new client failed: %v", err)
	}

	bundle, err := client.FetchIssue(context.Background(), "group/sub/project", 12, true)
	if err != nil {
		t.Fatalf("fetch issue failed: %v", err)
	}
	if seenIssuePath != "/api/v4/projects/group%2Fsub%2Fproject/issues/12" {
		t.Fatalf("issue path not encoded correctly: %s", seenIssuePath)
	}
	if seenNotesPath != "/api/v4/projects/group%2Fsub%2Fproject/issues/12/notes" {
		t.Fatalf("notes path not encoded correctly: %s", seenNotesPath)
	}
	if bundle.Issue.IID != 12 || bundle.Issue.Title != "需求标题" {
		t.Fatalf("unexpected issue: %+v", bundle.Issue)
	}
	if len(bundle.Notes) != 1 || bundle.Notes[0].Body != "补充验收标准" {
		t.Fatalf("unexpected notes: %+v", bundle.Notes)
	}
}

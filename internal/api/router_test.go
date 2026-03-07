package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"testpilot/internal/execution"
	"testpilot/internal/model"
)

func setupTestRouter(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite failed: %v", err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := NewRouter(Dependencies{
		DB:       db,
		Logger:   logger,
		Executor: execution.NewMockExecutor(logger, 0),
	})

	seedTestData(t, db)
	return router, db
}

func seedTestData(t *testing.T, db *gorm.DB) {
	t.Helper()
	users := []model.User{
		{ID: 1, Name: "Admin", Email: "admin@test.local", Role: model.GlobalRoleAdmin},
		{ID: 2, Name: "Tester", Email: "tester@test.local", Role: model.GlobalRoleTester},
		{ID: 3, Name: "Outsider", Email: "outsider@test.local", Role: model.GlobalRoleTester},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatalf("seed users failed: %v", err)
	}

	project := model.Project{ID: 1, Name: "Demo", Description: "demo"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	members := []model.ProjectMember{
		{ProjectID: 1, UserID: 1, Role: model.MemberRoleOwner},
		{ProjectID: 1, UserID: 2, Role: model.MemberRoleMember},
	}
	if err := db.Create(&members).Error; err != nil {
		t.Fatalf("seed members failed: %v", err)
	}

	script := model.Script{ID: 1, ProjectID: 1, Name: "login.cy.ts", Path: "cypress/e2e/login.cy.ts", Type: "cypress"}
	if err := db.Create(&script).Error; err != nil {
		t.Fatalf("seed script failed: %v", err)
	}
}

func TestAuthRequired(t *testing.T) {
	router, _ := setupTestRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.Code)
	}
}

func TestRunAndDefectFlow(t *testing.T) {
	router, _ := setupTestRouter(t)

	runReqBody := map[string]any{
		"mode":      "one",
		"script_id": 1,
	}
	payload, _ := json.Marshal(runReqBody)
	runReq := httptest.NewRequest(http.MethodPost, "/api/v1/projects/1/runs", bytes.NewReader(payload))
	runReq.Header.Set("Content-Type", "application/json")
	runReq.Header.Set("X-User-ID", "2")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, runReq)

	if resp.Code != http.StatusCreated {
		t.Fatalf("expected run 201, got %d, body=%s", resp.Code, resp.Body.String())
	}

	var runResp struct {
		Results []model.RunResult `json:"results"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &runResp); err != nil {
		t.Fatalf("parse run response failed: %v", err)
	}
	if len(runResp.Results) != 1 {
		t.Fatalf("expected 1 run result, got %d", len(runResp.Results))
	}

	defectReqBody := map[string]any{
		"run_result_id": runResp.Results[0].ID,
		"title":         "Login failure",
		"description":   "mock failure from cypress",
		"severity":      "high",
	}
	defectPayload, _ := json.Marshal(defectReqBody)
	defectReq := httptest.NewRequest(http.MethodPost, "/api/v1/projects/1/defects", bytes.NewReader(defectPayload))
	defectReq.Header.Set("Content-Type", "application/json")
	defectReq.Header.Set("X-User-ID", "2")
	defectResp := httptest.NewRecorder()
	router.ServeHTTP(defectResp, defectReq)

	if defectResp.Code != http.StatusCreated {
		t.Fatalf("expected defect 201, got %d, body=%s", defectResp.Code, defectResp.Body.String())
	}
}

func TestProjectACL(t *testing.T) {
	router, _ := setupTestRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/1/scripts", nil)
	req.Header.Set("X-User-ID", "3")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.Code)
	}
}

func TestProjectDemoOverview(t *testing.T) {
	router, _ := setupTestRouter(t)

	runReqBody := map[string]any{
		"mode":      "one",
		"script_id": 1,
	}
	payload, _ := json.Marshal(runReqBody)
	runReq := httptest.NewRequest(http.MethodPost, "/api/v1/projects/1/runs", bytes.NewReader(payload))
	runReq.Header.Set("Content-Type", "application/json")
	runReq.Header.Set("X-User-ID", "2")
	runResp := httptest.NewRecorder()
	router.ServeHTTP(runResp, runReq)
	if runResp.Code != http.StatusCreated {
		t.Fatalf("expected run 201, got %d, body=%s", runResp.Code, runResp.Body.String())
	}

	overviewReq := httptest.NewRequest(http.MethodGet, "/api/v1/projects/1/demo-overview", nil)
	overviewReq.Header.Set("X-User-ID", "2")
	overviewResp := httptest.NewRecorder()
	router.ServeHTTP(overviewResp, overviewReq)

	if overviewResp.Code != http.StatusOK {
		t.Fatalf("expected overview 200, got %d, body=%s", overviewResp.Code, overviewResp.Body.String())
	}

	var overview map[string]any
	if err := json.Unmarshal(overviewResp.Body.Bytes(), &overview); err != nil {
		t.Fatalf("parse overview response failed: %v", err)
	}

	counts, ok := overview["counts"].(map[string]any)
	if !ok {
		t.Fatalf("missing counts in overview")
	}
	if counts["scripts"] == nil {
		t.Fatalf("missing scripts count")
	}

	qualityGate, ok := overview["quality_gate"].(map[string]any)
	if !ok {
		t.Fatalf("missing quality_gate in overview")
	}
	if qualityGate["status"] == nil {
		t.Fatalf("missing quality gate status")
	}
}

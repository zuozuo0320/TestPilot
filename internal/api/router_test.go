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
		{ID: 1, Name: "Admin", Email: "admin@test.local", Role: model.GlobalRoleAdmin, Active: true},
		{ID: 2, Name: "Tester", Email: "tester@test.local", Role: model.GlobalRoleTester, Active: true},
		{ID: 3, Name: "Outsider", Email: "outsider@test.local", Role: model.GlobalRoleTester, Active: true},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatalf("seed users failed: %v", err)
	}

	roles := []model.Role{
		{ID: 1, Name: "admin", Description: "admin role"},
		{ID: 2, Name: "tester", Description: "tester role"},
	}
	if err := db.Create(&roles).Error; err != nil {
		t.Fatalf("seed roles failed: %v", err)
	}

	userRoles := []model.UserRole{
		{UserID: 1, RoleID: 1},
		{UserID: 2, RoleID: 2},
		{UserID: 3, RoleID: 2},
	}
	if err := db.Create(&userRoles).Error; err != nil {
		t.Fatalf("seed user roles failed: %v", err)
	}

	userProjects := []model.UserProject{
		{UserID: 1, ProjectID: 1},
		{UserID: 2, ProjectID: 1},
	}
	if err := db.Create(&userProjects).Error; err != nil {
		t.Fatalf("seed user projects failed: %v", err)
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

func TestLogin(t *testing.T) {
	router, _ := setupTestRouter(t)

	body := map[string]any{"email": "tester@test.local", "password": "TestPilot@2026"}
	payload, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected login 200, got %d, body=%s", resp.Code, resp.Body.String())
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

func TestTestCaseCRUDAndListPaging(t *testing.T) {
	router, _ := setupTestRouter(t)

	createPayload := map[string]any{
		"title":         "Login success case",
		"level":         "P0",
		"review_result": "未评审",
		"exec_result":   "未执行",
		"module_path":   "/登录",
		"tags":          "smoke,auth",
		"steps":         "open page -> input -> submit",
		"priority":      "high",
	}
	createBody, _ := json.Marshal(createPayload)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/projects/1/testcases", bytes.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("X-User-ID", "1")
	createResp := httptest.NewRecorder()
	router.ServeHTTP(createResp, createReq)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("expected create 201, got %d, body=%s", createResp.Code, createResp.Body.String())
	}

	var created model.TestCase
	if err := json.Unmarshal(createResp.Body.Bytes(), &created); err != nil {
		t.Fatalf("parse create response failed: %v", err)
	}
	if created.ID == 0 {
		t.Fatalf("created testcase id should not be zero")
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/projects/1/testcases?page=1&pageSize=10&keyword=Login", nil)
	listReq.Header.Set("X-User-ID", "2")
	listResp := httptest.NewRecorder()
	router.ServeHTTP(listResp, listReq)
	if listResp.Code != http.StatusOK {
		t.Fatalf("expected list 200, got %d, body=%s", listResp.Code, listResp.Body.String())
	}

	var listBody struct {
		Items []struct {
			ID            uint   `json:"id"`
			Title         string `json:"title"`
			Level         string `json:"level"`
			ReviewResult  string `json:"review_result"`
			ExecResult    string `json:"exec_result"`
			ModulePath    string `json:"module_path"`
			Tags          string `json:"tags"`
			CreatedByName string `json:"created_by_name"`
			UpdatedByName string `json:"updated_by_name"`
		} `json:"items"`
		Total    int64 `json:"total"`
		Page     int   `json:"page"`
		PageSize int   `json:"pageSize"`
	}
	if err := json.Unmarshal(listResp.Body.Bytes(), &listBody); err != nil {
		t.Fatalf("parse list response failed: %v", err)
	}
	if listBody.Page != 1 || listBody.PageSize != 10 {
		t.Fatalf("unexpected paging response: page=%d pageSize=%d", listBody.Page, listBody.PageSize)
	}
	if listBody.Total < 1 {
		t.Fatalf("expected total >= 1, got %d", listBody.Total)
	}
	if len(listBody.Items) == 0 {
		t.Fatalf("expected at least one item")
	}
	if listBody.Items[0].Title == "" {
		t.Fatalf("expected title field")
	}
	if listBody.Items[0].Level == "" {
		t.Fatalf("expected level field")
	}
	if listBody.Items[0].ReviewResult == "" {
		t.Fatalf("expected review_result field")
	}
	if listBody.Items[0].ExecResult == "" {
		t.Fatalf("expected exec_result field")
	}
	if listBody.Items[0].ModulePath == "" {
		t.Fatalf("expected module_path field")
	}
	if listBody.Items[0].Tags == "" {
		t.Fatalf("expected tags field")
	}
	if listBody.Items[0].CreatedByName == "" {
		t.Fatalf("expected created_by_name field")
	}
	if listBody.Items[0].UpdatedByName == "" {
		t.Fatalf("expected updated_by_name field")
	}

	listSortReq := httptest.NewRequest(http.MethodGet, "/api/v1/projects/1/testcases?page=1&pageSize=10&sortBy=id&sortOrder=asc", nil)
	listSortReq.Header.Set("X-User-ID", "2")
	listSortResp := httptest.NewRecorder()
	router.ServeHTTP(listSortResp, listSortReq)
	if listSortResp.Code != http.StatusOK {
		t.Fatalf("expected sorted list 200, got %d, body=%s", listSortResp.Code, listSortResp.Body.String())
	}
	var sortedBody struct {
		Items []struct {
			ID uint `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(listSortResp.Body.Bytes(), &sortedBody); err != nil {
		t.Fatalf("parse sorted list response failed: %v", err)
	}
	if len(sortedBody.Items) >= 2 && sortedBody.Items[0].ID > sortedBody.Items[1].ID {
		t.Fatalf("expected id asc sort")
	}

	listFilterReq := httptest.NewRequest(http.MethodGet, "/api/v1/projects/1/testcases?page=1&pageSize=10&level=P0&review_result=未评审&exec_result=未执行", nil)
	listFilterReq.Header.Set("X-User-ID", "2")
	listFilterResp := httptest.NewRecorder()
	router.ServeHTTP(listFilterResp, listFilterReq)
	if listFilterResp.Code != http.StatusOK {
		t.Fatalf("expected filtered list 200, got %d, body=%s", listFilterResp.Code, listFilterResp.Body.String())
	}
	var filteredBody struct {
		Items []struct {
			Level        string `json:"level"`
			ReviewResult string `json:"review_result"`
			ExecResult   string `json:"exec_result"`
		} `json:"items"`
	}
	if err := json.Unmarshal(listFilterResp.Body.Bytes(), &filteredBody); err != nil {
		t.Fatalf("parse filtered list response failed: %v", err)
	}
	if len(filteredBody.Items) == 0 {
		t.Fatalf("expected filtered items")
	}
	if filteredBody.Items[0].Level != "P0" || filteredBody.Items[0].ReviewResult != "未评审" || filteredBody.Items[0].ExecResult != "未执行" {
		t.Fatalf("unexpected filtered item")
	}

	updatePayload := map[string]any{
		"title":         "Login success case updated",
		"level":         "P1",
		"review_result": "已通过",
		"exec_result":   "成功",
		"module_path":   "/登录/主流程",
		"tags":          "smoke",
		"priority":      "medium",
	}
	updateBody, _ := json.Marshal(updatePayload)
	updateReq := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/v1/projects/1/testcases/%d", created.ID), bytes.NewReader(updateBody))
	updateReq.Header.Set("Content-Type", "application/json")
	updateReq.Header.Set("X-User-ID", "1")
	updateResp := httptest.NewRecorder()
	router.ServeHTTP(updateResp, updateReq)
	if updateResp.Code != http.StatusOK {
		t.Fatalf("expected update 200, got %d, body=%s", updateResp.Code, updateResp.Body.String())
	}

	var updated model.TestCase
	if err := json.Unmarshal(updateResp.Body.Bytes(), &updated); err != nil {
		t.Fatalf("parse update response failed: %v", err)
	}
	if updated.Title != "Login success case updated" {
		t.Fatalf("unexpected updated title: %s", updated.Title)
	}
	if updated.Level != "P1" {
		t.Fatalf("unexpected updated level: %s", updated.Level)
	}
	if updated.ReviewResult != "已通过" {
		t.Fatalf("unexpected updated review_result: %s", updated.ReviewResult)
	}
	if updated.ExecResult != "成功" {
		t.Fatalf("unexpected updated exec_result: %s", updated.ExecResult)
	}
	if updated.ModulePath != "/登录/主流程" {
		t.Fatalf("unexpected updated module_path: %s", updated.ModulePath)
	}
	if updated.Tags != "smoke" {
		t.Fatalf("unexpected updated tags: %s", updated.Tags)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/v1/projects/1/testcases/%d", created.ID), nil)
	deleteReq.Header.Set("X-User-ID", "1")
	deleteResp := httptest.NewRecorder()
	router.ServeHTTP(deleteResp, deleteReq)
	if deleteResp.Code != http.StatusOK {
		t.Fatalf("expected delete 200, got %d, body=%s", deleteResp.Code, deleteResp.Body.String())
	}
}

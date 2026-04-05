package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"testpilot/internal/execution"
	"testpilot/internal/model"
	pkgauth "testpilot/internal/pkg/auth"
	"testpilot/internal/repository"
	"testpilot/internal/service"
)

func setupTestRouter(t *testing.T) (http.Handler, *gorm.DB) {
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

	// Repository 层
	txMgr := repository.NewTxManager(db)
	userRepo := repository.NewUserRepo(db)
	roleRepo := repository.NewRoleRepo(db)
	projectRepo := repository.NewProjectRepo(db)
	testCaseRepo := repository.NewTestCaseRepo(db)
	caseHistoryRepo := repository.NewCaseHistoryRepo(db)
	auditRepo := repository.NewAuditRepo(db)
	executionRepo := repository.NewExecutionRepo(db)
	defectRepo := repository.NewDefectRepo(db)
	requirementRepo := repository.NewRequirementRepo(db)
	scriptRepo := repository.NewScriptRepo(db)
	aiScriptRepo := repository.NewAIScriptRepo(db)

	// Service 层
	mockExecutor := execution.NewMockExecutor(logger, 0)
	jwtCfg := pkgauth.DefaultConfig("test-secret")

	router := NewRouter(Dependencies{
		Logger:             logger,
		AuthService:        service.NewAuthService(userRepo, jwtCfg),
		UserService:        service.NewUserService(userRepo, roleRepo, projectRepo, auditRepo, txMgr),
		RoleService:        service.NewRoleService(roleRepo, auditRepo, txMgr),
		ProjectService:     service.NewProjectService(projectRepo, userRepo, auditRepo, txMgr),
		TestCaseService:    service.NewTestCaseService(testCaseRepo, caseHistoryRepo, auditRepo),
		ProfileService:     service.NewProfileService(userRepo, auditRepo, txMgr),
		ExecutionService:   service.NewExecutionService(executionRepo, txMgr, mockExecutor, nil, logger),
		DefectService:      service.NewDefectService(defectRepo, executionRepo),
		RequirementService: service.NewRequirementService(requirementRepo, testCaseRepo),
		ScriptService:      service.NewScriptService(scriptRepo, testCaseRepo),
		OverviewService:    service.NewOverviewService(projectRepo, requirementRepo, testCaseRepo, scriptRepo, executionRepo, defectRepo),
		AuditService:       service.NewAuditService(auditRepo),
		AIScriptService:    service.NewAIScriptService(aiScriptRepo, projectRepo, userRepo, txMgr, "http://127.0.0.1:8100", "http://localhost:8100", "", logger),
	}, "")

	seedTestData(t, db)
	return router, db
}

// getResultData 从统一响应 {code, message, data} 中提取 data 字段的原始 JSON
func getResultData(t *testing.T, body []byte) json.RawMessage {
	t.Helper()
	var envelope struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("parse result envelope failed: %v, body=%s", err, string(body))
	}
	return envelope.Data
}

// getPageResultData 从分页响应中提取 data 字段
func getPageResultData(t *testing.T, body []byte) (json.RawMessage, int64, int, int) {
	t.Helper()
	var envelope struct {
		Code     int             `json:"code"`
		Data     json.RawMessage `json:"data"`
		Total    int64           `json:"total"`
		Page     int             `json:"page"`
		PageSize int             `json:"page_size"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("parse page result envelope failed: %v, body=%s", err, string(body))
	}
	return envelope.Data, envelope.Total, envelope.Page, envelope.PageSize
}

func seedTestData(t *testing.T, db *gorm.DB) {
	t.Helper()
	passwordHash, err := pkgauth.HashPassword("TestPilot@2026")
	if err != nil {
		t.Fatalf("hash test password failed: %v", err)
	}
	users := []model.User{
		{ID: 1, Name: "Admin", Email: "admin@test.local", Role: model.GlobalRoleAdmin, Active: true, PasswordHash: passwordHash},
		{ID: 2, Name: "Tester", Email: "tester@test.local", Role: model.GlobalRoleTester, Active: true, PasswordHash: passwordHash},
		{ID: 3, Name: "Outsider", Email: "outsider@test.local", Role: model.GlobalRoleTester, Active: true, PasswordHash: passwordHash},
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

	runData := getResultData(t, resp.Body.Bytes())
	var runResp struct {
		Results []model.RunResult `json:"results"`
	}
	if err := json.Unmarshal(runData, &runResp); err != nil {
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
		"title":       "Login success case",
		"level":       "P0",
		"exec_result": "未执行",
		"module_path": "/登录",
		"tags":        "smoke,auth",
		"steps":       "open page -> input -> submit",
		"priority":    "high",
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

	createData := getResultData(t, createResp.Body.Bytes())
	var created model.TestCase
	if err := json.Unmarshal(createData, &created); err != nil {
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

	listRawData, listTotal, listPage, listPageSize := getPageResultData(t, listResp.Body.Bytes())
	var listItems []struct {
		ID                 uint   `json:"id"`
		Title              string `json:"title"`
		Level              string `json:"level"`
		ReviewResult       string `json:"review_result"`
		ExecResult         string `json:"exec_result"`
		ModulePath         string `json:"module_path"`
		Tags               string `json:"tags"`
		CreatedByName      string `json:"created_by_name"`
		UpdatedByName      string `json:"updated_by_name"`
		InReview           bool   `json:"in_review"`
		CurrentReviewID    uint   `json:"current_review_id"`
		CurrentReviewName  string `json:"current_review_name"`
		RelatedReviewCount int64  `json:"related_review_count"`
	}
	if err := json.Unmarshal(listRawData, &listItems); err != nil {
		t.Fatalf("parse list items failed: %v", err)
	}
	if listPage != 1 || listPageSize != 10 {
		t.Fatalf("unexpected paging response: page=%d pageSize=%d", listPage, listPageSize)
	}
	if listTotal < 1 {
		t.Fatalf("expected total >= 1, got %d", listTotal)
	}
	if len(listItems) == 0 {
		t.Fatalf("expected at least one item")
	}
	if listItems[0].Title == "" {
		t.Fatalf("expected title field")
	}
	if listItems[0].Level == "" {
		t.Fatalf("expected level field")
	}
	if listItems[0].ReviewResult == "" {
		t.Fatalf("expected review_result field")
	}
	if listItems[0].ExecResult == "" {
		t.Fatalf("expected exec_result field")
	}
	if listItems[0].ModulePath == "" {
		t.Fatalf("expected module_path field")
	}
	if listItems[0].Tags == "" {
		t.Fatalf("expected tags field")
	}
	if listItems[0].CreatedByName == "" {
		t.Fatalf("expected created_by_name field")
	}
	if listItems[0].UpdatedByName == "" {
		t.Fatalf("expected updated_by_name field")
	}
	if listItems[0].InReview {
		t.Fatalf("expected in_review default false")
	}
	if listItems[0].CurrentReviewID != 0 {
		t.Fatalf("expected current_review_id default zero")
	}
	if listItems[0].CurrentReviewName != "" {
		t.Fatalf("expected current_review_name default empty")
	}
	if listItems[0].RelatedReviewCount != 0 {
		t.Fatalf("expected related_review_count default zero")
	}

	listSortReq := httptest.NewRequest(http.MethodGet, "/api/v1/projects/1/testcases?page=1&pageSize=10&sortBy=id&sortOrder=asc", nil)
	listSortReq.Header.Set("X-User-ID", "2")
	listSortResp := httptest.NewRecorder()
	router.ServeHTTP(listSortResp, listSortReq)
	if listSortResp.Code != http.StatusOK {
		t.Fatalf("expected sorted list 200, got %d, body=%s", listSortResp.Code, listSortResp.Body.String())
	}
	sortedData, _, _, _ := getPageResultData(t, listSortResp.Body.Bytes())
	var sortedItems []struct {
		ID uint `json:"id"`
	}
	if err := json.Unmarshal(sortedData, &sortedItems); err != nil {
		t.Fatalf("parse sorted list response failed: %v", err)
	}
	if len(sortedItems) >= 2 && sortedItems[0].ID > sortedItems[1].ID {
		t.Fatalf("expected id asc sort")
	}

	listFilterReq := httptest.NewRequest(http.MethodGet, "/api/v1/projects/1/testcases?page=1&pageSize=10&level=P0&review_result=未评审&exec_result=未执行", nil)
	listFilterReq.Header.Set("X-User-ID", "2")
	listFilterResp := httptest.NewRecorder()
	router.ServeHTTP(listFilterResp, listFilterReq)
	if listFilterResp.Code != http.StatusOK {
		t.Fatalf("expected filtered list 200, got %d, body=%s", listFilterResp.Code, listFilterResp.Body.String())
	}
	filteredData, _, _, _ := getPageResultData(t, listFilterResp.Body.Bytes())
	var filteredItems []struct {
		Level        string `json:"level"`
		ReviewResult string `json:"review_result"`
		ExecResult   string `json:"exec_result"`
	}
	if err := json.Unmarshal(filteredData, &filteredItems); err != nil {
		t.Fatalf("parse filtered list response failed: %v", err)
	}
	if len(filteredItems) == 0 {
		t.Fatalf("expected filtered items")
	}
	if filteredItems[0].Level != "P0" || filteredItems[0].ReviewResult != "未评审" || filteredItems[0].ExecResult != "未执行" {
		t.Fatalf("unexpected filtered item")
	}

	updatePayload := map[string]any{
		"title":       "Login success case updated",
		"level":       "P1",
		"exec_result": "成功",
		"module_path": "/登录/主流程",
		"tags":        "smoke",
		"priority":    "medium",
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

	updateData := getResultData(t, updateResp.Body.Bytes())
	var updated model.TestCase
	if err := json.Unmarshal(updateData, &updated); err != nil {
		t.Fatalf("parse update response failed: %v", err)
	}
	if updated.Title != "Login success case updated" {
		t.Fatalf("unexpected updated title: %s", updated.Title)
	}
	if updated.Level != "P1" {
		t.Fatalf("unexpected updated level: %s", updated.Level)
	}
	if updated.ReviewResult != "未评审" {
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

func TestIAM_UserRoleProjectAndProfileFlow(t *testing.T) {
	router, db := setupTestRouter(t)

	invalidCreate := map[string]any{
		"name":        "NoBind",
		"email":       "nobind@test.local",
		"password":    "TestPilot@2026",
		"role_ids":    []uint{2},
		"project_ids": []uint{},
	}
	invalidBody, _ := json.Marshal(invalidCreate)
	invalidReq := httptest.NewRequest(http.MethodPost, "/api/v1/users", bytes.NewReader(invalidBody))
	invalidReq.Header.Set("Content-Type", "application/json")
	invalidReq.Header.Set("X-User-ID", "1")
	invalidResp := httptest.NewRecorder()
	router.ServeHTTP(invalidResp, invalidReq)
	if invalidResp.Code != http.StatusBadRequest {
		t.Fatalf("expected create user without project 400, got %d, body=%s", invalidResp.Code, invalidResp.Body.String())
	}

	project2 := model.Project{ID: 2, Name: "Demo-2", Description: "demo2"}
	if err := db.Create(&project2).Error; err != nil {
		t.Fatalf("seed project2 failed: %v", err)
	}
	role3 := model.Role{ID: 3, Name: "reviewer", Description: "review role"}
	if err := db.Create(&role3).Error; err != nil {
		t.Fatalf("seed role3 failed: %v", err)
	}

	createPayload := map[string]any{
		"name":        "IAM User",
		"email":       "iam.user@test.local",
		"phone":       "13800001234",
		"avatar":      "https://example.com/a.png",
		"password":    "TestPilot@2026",
		"role":        "tester",
		"role_ids":    []uint{2, 3},
		"project_ids": []uint{1, 2},
	}
	createBody, _ := json.Marshal(createPayload)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/users", bytes.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("X-User-ID", "1")
	createResp := httptest.NewRecorder()
	router.ServeHTTP(createResp, createReq)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("expected create user 201, got %d, body=%s", createResp.Code, createResp.Body.String())
	}
	createdUserData := getResultData(t, createResp.Body.Bytes())
	var created model.User
	if err := json.Unmarshal(createdUserData, &created); err != nil {
		t.Fatalf("parse created user failed: %v", err)
	}

	profilePayload := map[string]any{
		"name":   "Tester Updated",
		"phone":  "13900005678",
		"avatar": "https://example.com/new.png",
	}
	profileBody, _ := json.Marshal(profilePayload)
	profileReq := httptest.NewRequest(http.MethodPut, "/api/v1/users/me/profile", bytes.NewReader(profileBody))
	profileReq.Header.Set("Content-Type", "application/json")
	profileReq.Header.Set("X-User-ID", "2")
	profileResp := httptest.NewRecorder()
	router.ServeHTTP(profileResp, profileReq)
	if profileResp.Code != http.StatusOK {
		t.Fatalf("expected profile update 200, got %d, body=%s", profileResp.Code, profileResp.Body.String())
	}

	freezePayload := map[string]any{"active": false}
	freezeBody, _ := json.Marshal(freezePayload)
	freezeReq := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/v1/users/%d", created.ID), bytes.NewReader(freezeBody))
	freezeReq.Header.Set("Content-Type", "application/json")
	freezeReq.Header.Set("X-User-ID", "1")
	freezeResp := httptest.NewRecorder()
	router.ServeHTTP(freezeResp, freezeReq)
	if freezeResp.Code != http.StatusOK {
		t.Fatalf("expected freeze user 200, got %d, body=%s", freezeResp.Code, freezeResp.Body.String())
	}

	loginBody, _ := json.Marshal(map[string]any{"email": "iam.user@test.local", "password": "TestPilot@2026"})
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginResp := httptest.NewRecorder()
	router.ServeHTTP(loginResp, loginReq)
	if loginResp.Code != http.StatusForbidden {
		t.Fatalf("expected frozen login 403, got %d, body=%s", loginResp.Code, loginResp.Body.String())
	}

	delReq := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/v1/users/%d", created.ID), nil)
	delReq.Header.Set("X-User-ID", "1")
	delResp := httptest.NewRecorder()
	router.ServeHTTP(delResp, delReq)
	if delResp.Code != http.StatusOK {
		t.Fatalf("expected delete user 200, got %d, body=%s", delResp.Code, delResp.Body.String())
	}

	reusePayload := map[string]any{
		"name":        "IAM User 2",
		"email":       "iam.user@test.local",
		"phone":       "13800001234",
		"password":    "TestPilot@2026",
		"role":        "tester",
		"role_ids":    []uint{2},
		"project_ids": []uint{1},
	}
	reuseBody, _ := json.Marshal(reusePayload)
	reuseReq := httptest.NewRequest(http.MethodPost, "/api/v1/users", bytes.NewReader(reuseBody))
	reuseReq.Header.Set("Content-Type", "application/json")
	reuseReq.Header.Set("X-User-ID", "1")
	reuseResp := httptest.NewRecorder()
	router.ServeHTTP(reuseResp, reuseReq)
	// 当前实现是软删除和数据库唯一索引并存，所以被删除用户的邮箱/手机号仍然不能直接复用。
	if reuseResp.Code != http.StatusConflict {
		t.Fatalf("expected reuse email/phone create 409, got %d, body=%s", reuseResp.Code, reuseResp.Body.String())
	}
	if !strings.Contains(reuseResp.Body.String(), "user already exists") {
		t.Fatalf("expected reuse create conflict body, got body=%s", reuseResp.Body.String())
	}

	roleCreateBody, _ := json.Marshal(map[string]any{"name": "ops", "description": "ops role"})
	roleCreateReq := httptest.NewRequest(http.MethodPost, "/api/v1/roles", bytes.NewReader(roleCreateBody))
	roleCreateReq.Header.Set("Content-Type", "application/json")
	roleCreateReq.Header.Set("X-User-ID", "1")
	roleCreateResp := httptest.NewRecorder()
	router.ServeHTTP(roleCreateResp, roleCreateReq)
	if roleCreateResp.Code != http.StatusCreated {
		t.Fatalf("expected create role 201, got %d, body=%s", roleCreateResp.Code, roleCreateResp.Body.String())
	}
	createdRoleData := getResultData(t, roleCreateResp.Body.Bytes())
	var createdRole model.Role
	if err := json.Unmarshal(createdRoleData, &createdRole); err != nil {
		t.Fatalf("parse role create failed: %v", err)
	}

	roleUpdateBody, _ := json.Marshal(map[string]any{"description": "ops role updated"})
	roleUpdateReq := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/v1/roles/%d", createdRole.ID), bytes.NewReader(roleUpdateBody))
	roleUpdateReq.Header.Set("Content-Type", "application/json")
	roleUpdateReq.Header.Set("X-User-ID", "1")
	roleUpdateResp := httptest.NewRecorder()
	router.ServeHTTP(roleUpdateResp, roleUpdateReq)
	if roleUpdateResp.Code != http.StatusOK {
		t.Fatalf("expected update role 200, got %d, body=%s", roleUpdateResp.Code, roleUpdateResp.Body.String())
	}

	inUseDeleteReq := httptest.NewRequest(http.MethodDelete, "/api/v1/roles/2", nil)
	inUseDeleteReq.Header.Set("X-User-ID", "1")
	inUseDeleteResp := httptest.NewRecorder()
	router.ServeHTTP(inUseDeleteResp, inUseDeleteReq)
	if inUseDeleteResp.Code != http.StatusConflict {
		t.Fatalf("expected delete in-use role 409, got %d, body=%s", inUseDeleteResp.Code, inUseDeleteResp.Body.String())
	}

	presetDeleteReq := httptest.NewRequest(http.MethodDelete, "/api/v1/roles/1", nil)
	presetDeleteReq.Header.Set("X-User-ID", "1")
	presetDeleteResp := httptest.NewRecorder()
	router.ServeHTTP(presetDeleteResp, presetDeleteReq)
	if presetDeleteResp.Code != http.StatusConflict {
		t.Fatalf("expected delete preset role 409, got %d, body=%s", presetDeleteResp.Code, presetDeleteResp.Body.String())
	}
	if !strings.Contains(presetDeleteResp.Body.String(), "preset system role cannot be deleted") {
		t.Fatalf("expected preset role protected message, got body=%s", presetDeleteResp.Body.String())
	}

	reviewerDeleteReq := httptest.NewRequest(http.MethodDelete, "/api/v1/roles/3", nil)
	reviewerDeleteReq.Header.Set("X-User-ID", "1")
	reviewerDeleteResp := httptest.NewRecorder()
	router.ServeHTTP(reviewerDeleteResp, reviewerDeleteReq)
	if reviewerDeleteResp.Code != http.StatusConflict {
		t.Fatalf("expected delete reviewer preset role 409, got %d, body=%s", reviewerDeleteResp.Code, reviewerDeleteResp.Body.String())
	}
	if !strings.Contains(reviewerDeleteResp.Body.String(), "preset system role cannot be deleted") {
		t.Fatalf("expected reviewer preset role protected message, got body=%s", reviewerDeleteResp.Body.String())
	}

	deleteRoleReq := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/v1/roles/%d", createdRole.ID), nil)
	deleteRoleReq.Header.Set("X-User-ID", "1")
	deleteRoleResp := httptest.NewRecorder()
	router.ServeHTTP(deleteRoleResp, deleteRoleReq)
	if deleteRoleResp.Code != http.StatusOK {
		t.Fatalf("expected delete role 200, got %d, body=%s", deleteRoleResp.Code, deleteRoleResp.Body.String())
	}

	var auditCount int64
	if err := db.Model(&model.AuditLog{}).Count(&auditCount).Error; err != nil {
		t.Fatalf("query audit log failed: %v", err)
	}
	if auditCount == 0 {
		t.Fatalf("expected audit logs > 0")
	}
}

func TestLegacyTestCaseReviewRoutesRemoved(t *testing.T) {
	router, _ := setupTestRouter(t)

	legacyPaths := []string{
		"/api/v1/projects/1/testcase/1/submit-review",
		"/api/v1/projects/1/testcase/1/approve-review",
		"/api/v1/projects/1/testcase/1/reject-review",
	}

	for _, path := range legacyPaths {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.Header.Set("X-User-ID", "1")
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)
		if resp.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for removed legacy route %s, got %d, body=%s", path, resp.Code, resp.Body.String())
		}
	}
}

package service

import (
	"errors"
	"testing"

	"gorm.io/gorm"

	"testpilot/internal/model"
	pkgauth "testpilot/internal/pkg/auth"
	"testpilot/internal/repository"
)

// seedManager 种入 manager 角色用户，供任务治理能力测试使用。
func seedManager(t *testing.T, db *gorm.DB) model.User {
	t.Helper()
	hash, _ := pkgauth.HashPassword("TestPilot@2026")
	manager := model.User{
		ID:           3,
		Name:         "Manager",
		Email:        "manager@test.local",
		Role:         model.GlobalRoleManager,
		Active:       true,
		PasswordHash: hash,
	}
	if err := db.Create(&manager).Error; err != nil {
		t.Fatalf("seed manager: %v", err)
	}
	return manager
}

// newTestAIScriptService 创建测试用 AIScriptService。
func newTestAIScriptService(t *testing.T) (*AIScriptService, *gorm.DB, model.User, model.Project) {
	t.Helper()

	db := testDB(t)
	seedRoles(t, db)
	manager := seedManager(t, db)
	project := seedProject(t, db)
	_, _, projectRepo, _, txMgr := testRepos(db)

	svc := NewAIScriptService(
		repository.NewAIScriptRepo(db),
		projectRepo,
		repository.NewUserRepo(db),
		txMgr,
		"http://127.0.0.1:8100",
		"http://127.0.0.1:8100",
		"",
		testLogger(),
	)

	return svc, db, manager, project
}

// createTaskRecord 创建指定状态的任务记录，便于测试批量治理逻辑。
func createTaskRecord(t *testing.T, db *gorm.DB, projectID, creatorID, taskID uint, status string) model.AIScriptTask {
	t.Helper()

	task := model.AIScriptTask{
		ID:             taskID,
		ProjectID:      projectID,
		ProjectKey:     "project_1",
		TaskName:       "task",
		GenerationMode: model.AIGenerationModeRecordingEnhanced,
		ScenarioDesc:   "scenario",
		StartURL:       "https://example.com",
		TaskStatus:     status,
		FrameworkType:  "Playwright",
		CreatedBy:      creatorID,
		UpdatedBy:      creatorID,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}
	return task
}

// TestAIScriptService_DiscardTaskRejectsRunningStatus 验证运行中的任务不能被废弃。
func TestAIScriptService_DiscardTaskRejectsRunningStatus(t *testing.T) {
	svc, db, manager, project := newTestAIScriptService(t)
	task := createTaskRecord(t, db, project.ID, manager.ID, 1001, model.AITaskStatusRunning)

	err := svc.DiscardTask(t.Context(), manager.ID, task.ID, "运行中不应被废弃")
	if err == nil {
		t.Fatalf("expected discard task to fail for RUNNING status")
	}

	var bizErr *BizError
	if !errors.As(err, &bizErr) {
		t.Fatalf("expected BizError, got %T", err)
	}
	if bizErr.Code != "AI_SCRIPT_4005" {
		t.Fatalf("unexpected error code: %s", bizErr.Code)
	}

	stored, queryErr := repository.NewAIScriptRepo(db).GetTask(t.Context(), task.ID)
	if queryErr != nil {
		t.Fatalf("query task: %v", queryErr)
	}
	if stored.TaskStatus != model.AITaskStatusRunning {
		t.Fatalf("expected task status to remain RUNNING, got %s", stored.TaskStatus)
	}
}

// TestAIScriptService_BatchDiscardTasksByIDs 验证按显式 ID 批量废弃时的成功、跳过和失败统计。
func TestAIScriptService_BatchDiscardTasksByIDs(t *testing.T) {
	svc, db, manager, project := newTestAIScriptService(t)
	taskDraft := createTaskRecord(t, db, project.ID, manager.ID, 2001, model.AITaskStatusDraft)
	taskRunning := createTaskRecord(t, db, project.ID, manager.ID, 2002, model.AITaskStatusRunning)
	taskDiscarded := createTaskRecord(t, db, project.ID, manager.ID, 2003, model.AITaskStatusDiscarded)

	result, err := svc.BatchDiscardTasks(t.Context(), manager.ID, BatchDiscardTasksInput{
		BatchTaskSelectionInput: BatchTaskSelectionInput{
			SelectionMode: BatchTaskSelectionModeIDs,
			TaskIDs:       []uint{taskDraft.ID, taskRunning.ID, taskDiscarded.ID, 9999},
		},
		Reason: "批量清理旧任务",
	})
	if err != nil {
		t.Fatalf("batch discard tasks: %v", err)
	}

	if result.Matched != 4 || result.Success != 1 || result.Skipped != 2 || result.Failed != 1 {
		t.Fatalf("unexpected batch result: %+v", result)
	}

	storedDraft, err := repository.NewAIScriptRepo(db).GetTask(t.Context(), taskDraft.ID)
	if err != nil {
		t.Fatalf("query draft task: %v", err)
	}
	if storedDraft.TaskStatus != model.AITaskStatusDiscarded {
		t.Fatalf("expected draft task discarded, got %s", storedDraft.TaskStatus)
	}
	if storedDraft.DiscardReason != "批量清理旧任务" {
		t.Fatalf("unexpected discard reason: %s", storedDraft.DiscardReason)
	}
}

// TestAIScriptService_BatchDeleteTasksByFilterAll 验证按筛选结果全选删除时会正确处理排除项与状态校验。
func TestAIScriptService_BatchDeleteTasksByFilterAll(t *testing.T) {
	svc, db, manager, project := newTestAIScriptService(t)
	taskDeleted := createTaskRecord(t, db, project.ID, manager.ID, 3001, model.AITaskStatusDiscarded)
	taskExcluded := createTaskRecord(t, db, project.ID, manager.ID, 3002, model.AITaskStatusDiscarded)
	taskSkipped := createTaskRecord(t, db, project.ID, manager.ID, 3003, model.AITaskStatusGenerateSuccess)

	result, err := svc.BatchDeleteTasks(t.Context(), manager.ID, BatchTaskSelectionInput{
		SelectionMode:   BatchTaskSelectionModeFilterAll,
		ExcludedTaskIDs: []uint{taskExcluded.ID},
		FilterSnapshot: &TaskFilterSnapshot{
			ProjectID: project.ID,
		},
	})
	if err != nil {
		t.Fatalf("batch delete tasks: %v", err)
	}

	if result.Matched != 2 || result.Success != 1 || result.Skipped != 1 || result.Failed != 0 {
		t.Fatalf("unexpected batch delete result: %+v", result)
	}

	if _, err := repository.NewAIScriptRepo(db).GetTask(t.Context(), taskDeleted.ID); err == nil {
		t.Fatalf("expected discarded task to be deleted")
	}
	if _, err := repository.NewAIScriptRepo(db).GetTask(t.Context(), taskExcluded.ID); err != nil {
		t.Fatalf("expected excluded task to remain, got error: %v", err)
	}
	storedSkipped, err := repository.NewAIScriptRepo(db).GetTask(t.Context(), taskSkipped.ID)
	if err != nil {
		t.Fatalf("query skipped task: %v", err)
	}
	if storedSkipped.TaskStatus != model.AITaskStatusGenerateSuccess {
		t.Fatalf("expected non-discarded task to remain unchanged, got %s", storedSkipped.TaskStatus)
	}
}

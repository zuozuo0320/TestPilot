// ai_script_repo.go — 测试智编模块数据访问层
package repository

import (
	"context"

	"gorm.io/gorm"

	"testpilot/internal/model"
)

// AIScriptRepo 测试智编 Repository
type AIScriptRepo struct {
	db *gorm.DB
}

// NewAIScriptRepo 创建测试智编 Repository
func NewAIScriptRepo(db *gorm.DB) *AIScriptRepo {
	return &AIScriptRepo{db: db}
}

// buildTaskQuery 构建任务列表查询条件，供分页查询与批量操作复用。
func (r *AIScriptRepo) buildTaskQuery(ctx context.Context, projectID uint, keyword string, taskStatus string) *gorm.DB {
	q := r.db.WithContext(ctx).Model(&model.AIScriptTask{})

	if projectID > 0 {
		q = q.Where("project_id = ?", projectID)
	}
	if keyword != "" {
		q = q.Where("task_name LIKE ?", "%"+keyword+"%")
	}
	if taskStatus != "" {
		q = q.Where("task_status = ?", taskStatus)
	}

	return q
}

// ── 任务 CRUD ──

// CreateTask 创建任务
func (r *AIScriptRepo) CreateTask(ctx context.Context, task *model.AIScriptTask) error {
	return r.db.WithContext(ctx).Create(task).Error
}

// GetTask 获取任务详情
func (r *AIScriptRepo) GetTask(ctx context.Context, id uint) (*model.AIScriptTask, error) {
	var task model.AIScriptTask
	if err := r.db.WithContext(ctx).First(&task, id).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

// ListTasks 分页查询任务列表
func (r *AIScriptRepo) ListTasks(ctx context.Context, projectID uint, keyword string, taskStatus string, page, pageSize int) ([]model.AIScriptTask, int64, error) {
	q := r.buildTaskQuery(ctx, projectID, keyword, taskStatus)

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var tasks []model.AIScriptTask
	offset := (page - 1) * pageSize
	if err := q.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&tasks).Error; err != nil {
		return nil, 0, err
	}
	return tasks, total, nil
}

// ListTasksByIDs 按任务 ID 集合查询任务，用于批量操作前解析真实任务集。
func (r *AIScriptRepo) ListTasksByIDs(ctx context.Context, ids []uint) ([]model.AIScriptTask, error) {
	if len(ids) == 0 {
		return []model.AIScriptTask{}, nil
	}

	var tasks []model.AIScriptTask
	err := r.db.WithContext(ctx).
		Where("id IN ?", ids).
		Order("created_at DESC").
		Find(&tasks).Error
	return tasks, err
}

// ListTasksByFilter 按筛选快照查询全部命中任务，支持批量操作的筛选结果全选。
func (r *AIScriptRepo) ListTasksByFilter(ctx context.Context, projectID uint, keyword string, taskStatus string, excludedIDs []uint) ([]model.AIScriptTask, error) {
	q := r.buildTaskQuery(ctx, projectID, keyword, taskStatus)
	if len(excludedIDs) > 0 {
		q = q.Where("id NOT IN ?", excludedIDs)
	}

	var tasks []model.AIScriptTask
	err := q.Order("created_at DESC").Find(&tasks).Error
	return tasks, err
}

// UpdateTaskStatus 更新任务状态
func (r *AIScriptRepo) UpdateTaskStatus(ctx context.Context, id uint, status string) error {
	return r.db.WithContext(ctx).Model(&model.AIScriptTask{}).Where("id = ?", id).Update("task_status", status).Error
}

// UpdateTaskFields 更新任务多个字段
func (r *AIScriptRepo) UpdateTaskFields(ctx context.Context, id uint, fields map[string]interface{}) error {
	return r.db.WithContext(ctx).Model(&model.AIScriptTask{}).Where("id = ?", id).Updates(fields).Error
}

// ── 任务-用例关联 ──

// CreateTaskCaseRels 批量创建任务-用例关联
func (r *AIScriptRepo) CreateTaskCaseRels(ctx context.Context, rels []model.AIScriptTaskCaseRel) error {
	return r.db.WithContext(ctx).Create(&rels).Error
}

// GetTaskCaseIDs 获取任务关联的用例ID列表
func (r *AIScriptRepo) GetTaskCaseIDs(ctx context.Context, taskID uint) ([]uint, error) {
	var ids []uint
	err := r.db.WithContext(ctx).Model(&model.AIScriptTaskCaseRel{}).
		Where("task_id = ?", taskID).Pluck("case_id", &ids).Error
	return ids, err
}

// CountTaskCases 获取任务关联的用例数
func (r *AIScriptRepo) CountTaskCases(ctx context.Context, taskID uint) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.AIScriptTaskCaseRel{}).
		Where("task_id = ?", taskID).Count(&count).Error
	return count, err
}

// ── 脚本版本 ──

// CreateScriptVersion 创建脚本版本
func (r *AIScriptRepo) CreateScriptVersion(ctx context.Context, version *model.AIScriptVersion) error {
	return r.db.WithContext(ctx).Create(version).Error
}

// GetScriptVersion 获取脚本版本详情
func (r *AIScriptRepo) GetScriptVersion(ctx context.Context, id uint) (*model.AIScriptVersion, error) {
	var version model.AIScriptVersion
	if err := r.db.WithContext(ctx).First(&version, id).Error; err != nil {
		return nil, err
	}
	return &version, nil
}

// GetCurrentScriptVersion 获取任务的当前脚本版本
func (r *AIScriptRepo) GetCurrentScriptVersion(ctx context.Context, taskID uint) (*model.AIScriptVersion, error) {
	var version model.AIScriptVersion
	err := r.db.WithContext(ctx).
		Where("task_id = ? AND is_current_flag = ?", taskID, true).
		First(&version).Error
	if err != nil {
		return nil, err
	}
	return &version, nil
}

// ListScriptVersions 获取任务的所有脚本版本
func (r *AIScriptRepo) ListScriptVersions(ctx context.Context, taskID uint) ([]model.AIScriptVersion, error) {
	var versions []model.AIScriptVersion
	err := r.db.WithContext(ctx).
		Where("task_id = ?", taskID).
		Order("version_no DESC").
		Find(&versions).Error
	return versions, err
}

// GetMaxVersionNo 获取任务下最大的版本号
func (r *AIScriptRepo) GetMaxVersionNo(ctx context.Context, taskID uint) (int, error) {
	var maxNo *int
	err := r.db.WithContext(ctx).Model(&model.AIScriptVersion{}).
		Where("task_id = ?", taskID).
		Select("MAX(version_no)").Scan(&maxNo).Error
	if err != nil {
		return 0, err
	}
	if maxNo == nil {
		return 0, nil
	}
	return *maxNo, nil
}

// ClearCurrentFlag 清除任务下所有版本的当前标记
func (r *AIScriptRepo) ClearCurrentFlag(ctx context.Context, taskID uint) error {
	return r.db.WithContext(ctx).Model(&model.AIScriptVersion{}).
		Where("task_id = ? AND is_current_flag = ?", taskID, true).
		Update("is_current_flag", false).Error
}

// UpdateScriptVersionFields 更新脚本版本多个字段
func (r *AIScriptRepo) UpdateScriptVersionFields(ctx context.Context, id uint, fields map[string]interface{}) error {
	return r.db.WithContext(ctx).Model(&model.AIScriptVersion{}).Where("id = ?", id).Updates(fields).Error
}

// ── 回放验证 ──

// CreateValidation 创建验证记录
func (r *AIScriptRepo) CreateValidation(ctx context.Context, v *model.AIScriptValidation) error {
	return r.db.WithContext(ctx).Create(v).Error
}

// GetValidation 获取验证记录
func (r *AIScriptRepo) GetValidation(ctx context.Context, id uint) (*model.AIScriptValidation, error) {
	var v model.AIScriptValidation
	if err := r.db.WithContext(ctx).First(&v, id).Error; err != nil {
		return nil, err
	}
	return &v, nil
}

// GetLatestValidation 获取脚本版本的最近一次验证结果
func (r *AIScriptRepo) GetLatestValidation(ctx context.Context, scriptVersionID uint) (*model.AIScriptValidation, error) {
	var v model.AIScriptValidation
	err := r.db.WithContext(ctx).
		Where("script_version_id = ?", scriptVersionID).
		Order("created_at DESC").
		First(&v).Error
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// HasActiveValidation 检查脚本版本是否有正在进行的验证
func (r *AIScriptRepo) HasActiveValidation(ctx context.Context, scriptVersionID uint) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.AIScriptValidation{}).
		Where("script_version_id = ? AND validation_status = ?", scriptVersionID, model.AIValidationStatusValidating).
		Count(&count).Error
	return count > 0, err
}

// UpdateValidationFields 更新验证记录字段
func (r *AIScriptRepo) UpdateValidationFields(ctx context.Context, id uint, fields map[string]interface{}) error {
	return r.db.WithContext(ctx).Model(&model.AIScriptValidation{}).Where("id = ?", id).Updates(fields).Error
}

// ── 轨迹与证据 ──

// BatchCreateTraces 批量创建轨迹
func (r *AIScriptRepo) BatchCreateTraces(ctx context.Context, traces []model.AIScriptTrace) error {
	if len(traces) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Create(&traces).Error
}

// ListTraces 获取任务的轨迹列表
func (r *AIScriptRepo) ListTraces(ctx context.Context, taskID uint) ([]model.AIScriptTrace, error) {
	var traces []model.AIScriptTrace
	err := r.db.WithContext(ctx).
		Where("task_id = ?", taskID).
		Order("trace_no ASC").
		Find(&traces).Error
	return traces, err
}

// BatchCreateEvidences 批量创建证据
func (r *AIScriptRepo) BatchCreateEvidences(ctx context.Context, evidences []model.AIScriptEvidence) error {
	if len(evidences) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Create(&evidences).Error
}

// ListEvidences 获取任务的证据列表
func (r *AIScriptRepo) ListEvidences(ctx context.Context, taskID uint) ([]model.AIScriptEvidence, error) {
	var evidences []model.AIScriptEvidence
	err := r.db.WithContext(ctx).
		Where("task_id = ?", taskID).
		Order("created_at ASC").
		Find(&evidences).Error
	return evidences, err
}

// ListEvidencesByValidation 获取某次验证的证据
func (r *AIScriptRepo) ListEvidencesByValidation(ctx context.Context, validationID uint) ([]model.AIScriptEvidence, error) {
	var evidences []model.AIScriptEvidence
	err := r.db.WithContext(ctx).
		Where("validation_id = ?", validationID).
		Order("created_at ASC").
		Find(&evidences).Error
	return evidences, err
}

// ── 操作日志 ──

// CreateOperationLog 创建操作日志
func (r *AIScriptRepo) CreateOperationLog(ctx context.Context, log *model.AIScriptOperationLog) error {
	return r.db.WithContext(ctx).Create(log).Error
}

// ── 录制会话 ──

// CreateRecordingSession 创建录制会话
func (r *AIScriptRepo) CreateRecordingSession(ctx context.Context, session *model.AIScriptRecordingSession) error {
	return r.db.WithContext(ctx).Create(session).Error
}

// UpdateRecordingSessionFields 更新录制会话字段
func (r *AIScriptRepo) UpdateRecordingSessionFields(ctx context.Context, id uint, fields map[string]interface{}) error {
	return r.db.WithContext(ctx).Model(&model.AIScriptRecordingSession{}).Where("id = ?", id).Updates(fields).Error
}

// FindLatestRecordingByTaskID 获取任务最近一次录制会话
func (r *AIScriptRepo) FindLatestRecordingByTaskID(ctx context.Context, taskID uint) (*model.AIScriptRecordingSession, error) {
	var session model.AIScriptRecordingSession
	err := r.db.WithContext(ctx).
		Where("task_id = ?", taskID).
		Order("created_at DESC").
		First(&session).Error
	if err != nil {
		return nil, err
	}
	return &session, nil
}

// ── 扩展查询 ──

// ListValidationsByScriptID 获取脚本版本的所有验证记录
func (r *AIScriptRepo) ListValidationsByScriptID(ctx context.Context, scriptVersionID uint) ([]model.AIScriptValidation, error) {
	var validations []model.AIScriptValidation
	err := r.db.WithContext(ctx).
		Where("script_version_id = ?", scriptVersionID).
		Order("created_at DESC").
		Find(&validations).Error
	return validations, err
}

// MaxVersionNo GetMaxVersionNo 的别名
func (r *AIScriptRepo) MaxVersionNo(ctx context.Context, taskID uint) (int, error) {
	return r.GetMaxVersionNo(ctx, taskID)
}

// DeleteTask 物理删除任务及所有关联数据（事务保证原子性）
func (r *AIScriptRepo) DeleteTask(ctx context.Context, taskID uint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. 删除操作日志
		if err := tx.Where("task_id = ?", taskID).Delete(&model.AIScriptOperationLog{}).Error; err != nil {
			return err
		}
		// 2. 删除证据
		if err := tx.Where("task_id = ?", taskID).Delete(&model.AIScriptEvidence{}).Error; err != nil {
			return err
		}
		// 3. 删除轨迹
		if err := tx.Where("task_id = ?", taskID).Delete(&model.AIScriptTrace{}).Error; err != nil {
			return err
		}
		// 4. 删除验证记录
		if err := tx.Where("task_id = ?", taskID).Delete(&model.AIScriptValidation{}).Error; err != nil {
			return err
		}
		// 5. 删除生成文件明细
		if err := tx.Where("task_id = ?", taskID).Delete(&model.AIScriptFile{}).Error; err != nil {
			return err
		}
		// 6. 删除脚本版本
		if err := tx.Where("task_id = ?", taskID).Delete(&model.AIScriptVersion{}).Error; err != nil {
			return err
		}
		// 6. 删除录制会话
		if err := tx.Where("task_id = ?", taskID).Delete(&model.AIScriptRecordingSession{}).Error; err != nil {
			return err
		}
		// 7. 删除用例关联
		if err := tx.Where("task_id = ?", taskID).Delete(&model.AIScriptTaskCaseRel{}).Error; err != nil {
			return err
		}
		// 8. 删除任务本体
		if err := tx.Delete(&model.AIScriptTask{}, taskID).Error; err != nil {
			return err
		}
		return nil
	})
}

// ── V1 多项目工程化：文件明细 CRUD ──

// CreateScriptFile 创建生成文件记录
func (r *AIScriptRepo) CreateScriptFile(ctx context.Context, file *model.AIScriptFile) error {
	return r.db.WithContext(ctx).Create(file).Error
}

// BatchCreateScriptFiles 批量创建生成文件记录
func (r *AIScriptRepo) BatchCreateScriptFiles(ctx context.Context, files []model.AIScriptFile) error {
	if len(files) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Create(&files).Error
}

// ListScriptFiles 获取版本的所有文件
func (r *AIScriptRepo) ListScriptFiles(ctx context.Context, versionID uint) ([]model.AIScriptFile, error) {
	var files []model.AIScriptFile
	err := r.db.WithContext(ctx).
		Where("version_id = ?", versionID).
		Order("file_type ASC, relative_path ASC").
		Find(&files).Error
	return files, err
}

// GetScriptFileByPath 通过版本ID和相对路径查找文件
func (r *AIScriptRepo) GetScriptFileByPath(ctx context.Context, versionID uint, relativePath string) (*model.AIScriptFile, error) {
	var file model.AIScriptFile
	err := r.db.WithContext(ctx).
		Where("version_id = ? AND relative_path = ?", versionID, relativePath).
		First(&file).Error
	if err != nil {
		return nil, err
	}
	return &file, nil
}

// ListScriptFilesByProject 获取项目的所有文件（最新版本）
func (r *AIScriptRepo) ListScriptFilesByProject(ctx context.Context, projectID uint) ([]model.AIScriptFile, error) {
	var files []model.AIScriptFile
	err := r.db.WithContext(ctx).
		Where("project_id = ?", projectID).
		Order("relative_path ASC").
		Find(&files).Error
	return files, err
}

// DeleteScriptFilesByVersion 删除版本的所有文件
func (r *AIScriptRepo) DeleteScriptFilesByVersion(ctx context.Context, versionID uint) error {
	return r.db.WithContext(ctx).Where("version_id = ?", versionID).Delete(&model.AIScriptFile{}).Error
}

// ── V1 多项目工程化：工作区锁操作 ──

// AcquireWorkspaceLockAtomic 原子获取项目工作区锁（INSERT ... ON DUPLICATE KEY UPDATE）
// 仅当锁不存在或已过期/已释放时才能获取成功，避免先查后写的竞态条件。
// 返回 true 表示获取成功，false 表示锁被其他任务持有。
func (r *AIScriptRepo) AcquireWorkspaceLockAtomic(ctx context.Context, lock *model.AIScriptWorkspaceLock) (bool, error) {
	// 使用原生 SQL 实现原子 upsert：
	// - 如果 project_id 不存在 → INSERT 成功
	// - 如果 project_id 已存在但锁已过期或已释放 → UPDATE 成功（affected=2 for ON DUPLICATE KEY UPDATE）
	// - 如果 project_id 已存在且锁仍活跃 → UPDATE 不满足条件，affected=0
	sql := `INSERT INTO ai_script_workspace_locks
		(project_id, lock_key, lock_type, owner_task_id, owner_version_id, owner_request_id, heartbeat_at, expires_at, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'active', NOW())
		ON DUPLICATE KEY UPDATE
			lock_key = IF(status != 'active' OR heartbeat_at < DATE_SUB(NOW(), INTERVAL 10 MINUTE), VALUES(lock_key), lock_key),
			lock_type = IF(status != 'active' OR heartbeat_at < DATE_SUB(NOW(), INTERVAL 10 MINUTE), VALUES(lock_type), lock_type),
			owner_task_id = IF(status != 'active' OR heartbeat_at < DATE_SUB(NOW(), INTERVAL 10 MINUTE), VALUES(owner_task_id), owner_task_id),
			owner_version_id = IF(status != 'active' OR heartbeat_at < DATE_SUB(NOW(), INTERVAL 10 MINUTE), VALUES(owner_version_id), owner_version_id),
			owner_request_id = IF(status != 'active' OR heartbeat_at < DATE_SUB(NOW(), INTERVAL 10 MINUTE), VALUES(owner_request_id), owner_request_id),
			heartbeat_at = IF(status != 'active' OR heartbeat_at < DATE_SUB(NOW(), INTERVAL 10 MINUTE), VALUES(heartbeat_at), heartbeat_at),
			expires_at = IF(status != 'active' OR heartbeat_at < DATE_SUB(NOW(), INTERVAL 10 MINUTE), VALUES(expires_at), expires_at),
			status = IF(status != 'active' OR heartbeat_at < DATE_SUB(NOW(), INTERVAL 10 MINUTE), 'active', status)`

	result := r.db.WithContext(ctx).Exec(sql,
		lock.ProjectID, lock.LockKey, lock.LockType,
		lock.OwnerTaskID, lock.OwnerVersionID, lock.OwnerRequestID,
		lock.HeartbeatAt, lock.ExpiresAt,
	)
	if result.Error != nil {
		return false, result.Error
	}
	// RowsAffected: 1=新插入, 2=ON DUPLICATE KEY UPDATE 实际更新了字段, 0=条件不满足（锁被持有）
	return result.RowsAffected > 0, nil
}

// ReleaseWorkspaceLock 释放项目工作区锁
func (r *AIScriptRepo) ReleaseWorkspaceLock(ctx context.Context, projectID uint) error {
	return r.db.WithContext(ctx).
		Model(&model.AIScriptWorkspaceLock{}).
		Where("project_id = ? AND status = ?", projectID, "active").
		Updates(map[string]interface{}{"status": "released"}).Error
}

// HeartbeatLock 续约工作区锁
func (r *AIScriptRepo) HeartbeatLock(ctx context.Context, projectID uint) error {
	return r.db.WithContext(ctx).
		Model(&model.AIScriptWorkspaceLock{}).
		Where("project_id = ? AND status = ?", projectID, "active").
		Update("heartbeat_at", r.db.NowFunc()).Error
}

// GetActiveWorkspaceLock 获取项目的活跃锁
func (r *AIScriptRepo) GetActiveWorkspaceLock(ctx context.Context, projectID uint) (*model.AIScriptWorkspaceLock, error) {
	var lock model.AIScriptWorkspaceLock
	err := r.db.WithContext(ctx).
		Where("project_id = ? AND status = ?", projectID, "active").
		First(&lock).Error
	if err != nil {
		return nil, err
	}
	return &lock, nil
}

// DB 暴露底层 DB 实例（用于跨表查询等特殊场景）
func (r *AIScriptRepo) DB() *gorm.DB {
	return r.db
}

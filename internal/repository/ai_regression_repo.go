// ai_regression_repo.go — 回归计划/执行记录/修复建议/计划指标记录数据访问层
package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"testpilot/internal/model"
)

// AIRegressionRepo 回归与修复闭环 Repository。
type AIRegressionRepo struct {
	db *gorm.DB
}

// NewAIRegressionRepo 创建回归与修复闭环 Repository。
func NewAIRegressionRepo(db *gorm.DB) *AIRegressionRepo {
	return &AIRegressionRepo{db: db}
}

// ── 回归计划 ──

// CreatePlan 创建回归计划。
func (r *AIRegressionRepo) CreatePlan(ctx context.Context, plan *model.AIRegressionPlan) error {
	return r.db.WithContext(ctx).Create(plan).Error
}

// GetPlanByID 按 ID 查询回归计划。
func (r *AIRegressionRepo) GetPlanByID(ctx context.Context, id uint) (*model.AIRegressionPlan, error) {
	var plan model.AIRegressionPlan
	if err := r.db.WithContext(ctx).First(&plan, id).Error; err != nil {
		return nil, err
	}
	return &plan, nil
}

// GetPlanByComposition 按编排查询回归计划。
func (r *AIRegressionRepo) GetPlanByComposition(ctx context.Context, projectID, compositionID uint) (*model.AIRegressionPlan, error) {
	var plan model.AIRegressionPlan
	err := r.db.WithContext(ctx).
		Where("project_id = ? AND composition_id = ?", projectID, compositionID).
		First(&plan).Error
	if err != nil {
		return nil, err
	}
	return &plan, nil
}

// ListPlans 按项目查询回归计划列表。
func (r *AIRegressionRepo) ListPlans(ctx context.Context, projectID uint) ([]model.AIRegressionPlan, error) {
	var plans []model.AIRegressionPlan
	err := r.db.WithContext(ctx).
		Where("project_id = ?", projectID).
		Order("id DESC").
		Find(&plans).Error
	return plans, err
}

// UpdatePlanFields 更新回归计划字段。
func (r *AIRegressionRepo) UpdatePlanFields(ctx context.Context, id uint, fields map[string]interface{}) error {
	return r.db.WithContext(ctx).
		Model(&model.AIRegressionPlan{}).
		Where("id = ?", id).
		Updates(fields).Error
}

// DeletePlan 删除回归计划。
func (r *AIRegressionRepo) DeletePlan(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&model.AIRegressionPlan{}, id).Error
}

// FindDuePlans 查询到期且启用的回归计划。
func (r *AIRegressionRepo) FindDuePlans(ctx context.Context, now time.Time, limit int) ([]model.AIRegressionPlan, error) {
	var plans []model.AIRegressionPlan
	err := r.db.WithContext(ctx).
		Where("enabled = ? AND next_run_at IS NOT NULL AND next_run_at <= ?", true, now).
		Order("next_run_at ASC").
		Limit(limit).
		Find(&plans).Error
	return plans, err
}

// ClaimDuePlan 以 CAS 方式占用到期计划（推进 next_run_at），返回是否占用成功。
func (r *AIRegressionRepo) ClaimDuePlan(ctx context.Context, plan *model.AIRegressionPlan, nextRunAt time.Time) (bool, error) {
	result := r.db.WithContext(ctx).
		Model(&model.AIRegressionPlan{}).
		Where("id = ? AND enabled = ? AND next_run_at = ?", plan.ID, true, plan.NextRunAt).
		Updates(map[string]interface{}{"next_run_at": nextRunAt})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// ── 回归执行记录 ──

// CreateExecution 创建回归执行记录。
func (r *AIRegressionRepo) CreateExecution(ctx context.Context, execution *model.AIRegressionExecution) error {
	return r.db.WithContext(ctx).Create(execution).Error
}

// UpdateExecutionFields 更新回归执行记录字段。
func (r *AIRegressionRepo) UpdateExecutionFields(ctx context.Context, id uint, fields map[string]interface{}) error {
	return r.db.WithContext(ctx).
		Model(&model.AIRegressionExecution{}).
		Where("id = ?", id).
		Updates(fields).Error
}

// AIRegressionExecutionFilter 回归执行记录筛选条件。
type AIRegressionExecutionFilter struct {
	ProjectID     uint
	CompositionID uint
	Status        string
	Page          int
	PageSize      int
}

// ListExecutions 分页查询回归执行记录。
func (r *AIRegressionRepo) ListExecutions(ctx context.Context, filter AIRegressionExecutionFilter) ([]model.AIRegressionExecution, int64, error) {
	q := r.db.WithContext(ctx).Model(&model.AIRegressionExecution{}).
		Where("project_id = ?", filter.ProjectID)
	if filter.CompositionID > 0 {
		q = q.Where("composition_id = ?", filter.CompositionID)
	}
	if filter.Status != "" {
		q = q.Where("status = ?", filter.Status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var executions []model.AIRegressionExecution
	offset := (filter.Page - 1) * filter.PageSize
	err := q.Order("id DESC").Offset(offset).Limit(filter.PageSize).Find(&executions).Error
	return executions, total, err
}

// GetValidationByID 按 ID 查询编排验证记录（用于修复建议拼装失败日志）。
func (r *AIRegressionRepo) GetValidationByID(ctx context.Context, id uint) (*model.AICompositionValidation, error) {
	var validation model.AICompositionValidation
	if err := r.db.WithContext(ctx).First(&validation, id).Error; err != nil {
		return nil, err
	}
	return &validation, nil
}

// ── 修复建议 ──

// CreateSuggestion 创建修复建议。
func (r *AIRegressionRepo) CreateSuggestion(ctx context.Context, suggestion *model.AIRepairSuggestion) error {
	return r.db.WithContext(ctx).Create(suggestion).Error
}

// GetSuggestionByID 按 ID 查询修复建议。
func (r *AIRegressionRepo) GetSuggestionByID(ctx context.Context, id uint) (*model.AIRepairSuggestion, error) {
	var suggestion model.AIRepairSuggestion
	if err := r.db.WithContext(ctx).First(&suggestion, id).Error; err != nil {
		return nil, err
	}
	return &suggestion, nil
}

// UpdateSuggestionStatusCAS 以 CAS 方式更新修复建议状态，返回是否更新成功。
func (r *AIRegressionRepo) UpdateSuggestionStatusCAS(ctx context.Context, id uint, fromStatus string, fields map[string]interface{}) (bool, error) {
	result := r.db.WithContext(ctx).
		Model(&model.AIRepairSuggestion{}).
		Where("id = ? AND status = ?", id, fromStatus).
		Updates(fields)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// AIRepairSuggestionFilter 修复建议筛选条件。
type AIRepairSuggestionFilter struct {
	ProjectID     uint
	CompositionID uint
	Status        string
	Page          int
	PageSize      int
}

// ListSuggestions 分页查询修复建议。
func (r *AIRegressionRepo) ListSuggestions(ctx context.Context, filter AIRepairSuggestionFilter) ([]model.AIRepairSuggestion, int64, error) {
	q := r.db.WithContext(ctx).Model(&model.AIRepairSuggestion{}).
		Where("project_id = ?", filter.ProjectID)
	if filter.CompositionID > 0 {
		q = q.Where("composition_id = ?", filter.CompositionID)
	}
	if filter.Status != "" {
		q = q.Where("status = ?", filter.Status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var suggestions []model.AIRepairSuggestion
	offset := (filter.Page - 1) * filter.PageSize
	err := q.Order("id DESC").Offset(offset).Limit(filter.PageSize).Find(&suggestions).Error
	return suggestions, total, err
}

// CountSuggestionsByStatus 按状态统计修复建议数量。
func (r *AIRegressionRepo) CountSuggestionsByStatus(ctx context.Context, projectID uint, status string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.AIRepairSuggestion{}).
		Where("project_id = ? AND status = ?", projectID, status).
		Count(&count).Error
	return count, err
}

// ── 计划指标记录 ──

// CreatePlanRecord 创建计划指标记录。
func (r *AIRegressionRepo) CreatePlanRecord(ctx context.Context, record *model.AIPlanRecord) error {
	return r.db.WithContext(ctx).Create(record).Error
}

// GetPlanRecordByPlanID 按计划 ID 查询指标记录。
func (r *AIRegressionRepo) GetPlanRecordByPlanID(ctx context.Context, projectID uint, planID string) (*model.AIPlanRecord, error) {
	var record model.AIPlanRecord
	err := r.db.WithContext(ctx).
		Where("project_id = ? AND plan_id = ?", projectID, planID).
		First(&record).Error
	if err != nil {
		return nil, err
	}
	return &record, nil
}

// UpdatePlanRecordFields 更新计划指标记录字段。
func (r *AIRegressionRepo) UpdatePlanRecordFields(ctx context.Context, id uint, fields map[string]interface{}) error {
	return r.db.WithContext(ctx).
		Model(&model.AIPlanRecord{}).
		Where("id = ?", id).
		Updates(fields).Error
}

// FindPlanRecordForFirstValidation 查询编排关联且尚未记录首次验证结果的计划记录。
func (r *AIRegressionRepo) FindPlanRecordForFirstValidation(ctx context.Context, compositionID uint) (*model.AIPlanRecord, error) {
	var record model.AIPlanRecord
	err := r.db.WithContext(ctx).
		Where("composition_id = ? AND first_validation_status = ''", compositionID).
		Order("id DESC").
		First(&record).Error
	if err != nil {
		return nil, err
	}
	return &record, nil
}

// AIPlanRecordAggregate 计划指标聚合结果。
type AIPlanRecordAggregate struct {
	PlanCount          int64
	TotalSteps         int64
	FlowCallSteps      int64
	AdoptedPlanCount   int64
	AdoptedSteps       int64
	ModifiedSteps      int64
	ManualConfirmSteps int64
	FirstValidated     int64
	FirstPassed        int64
}

// AggregatePlanRecords 聚合项目内计划指标记录。
func (r *AIRegressionRepo) AggregatePlanRecords(ctx context.Context, projectID uint, since *time.Time) (*AIPlanRecordAggregate, error) {
	q := r.db.WithContext(ctx).Model(&model.AIPlanRecord{}).Where("project_id = ?", projectID)
	if since != nil {
		q = q.Where("created_at >= ?", *since)
	}
	var row struct {
		PlanCount          int64
		TotalSteps         int64
		FlowCallSteps      int64
		AdoptedPlanCount   int64
		AdoptedSteps       int64
		ModifiedSteps      int64
		ManualConfirmSteps int64
		FirstValidated     int64
		FirstPassed        int64
	}
	err := q.Select(
		"COUNT(*) AS plan_count, " +
			"COALESCE(SUM(total_steps), 0) AS total_steps, " +
			"COALESCE(SUM(flow_call_steps), 0) AS flow_call_steps, " +
			"COALESCE(SUM(CASE WHEN composition_id IS NOT NULL THEN 1 ELSE 0 END), 0) AS adopted_plan_count, " +
			"COALESCE(SUM(adopted_steps), 0) AS adopted_steps, " +
			"COALESCE(SUM(modified_steps), 0) AS modified_steps, " +
			"COALESCE(SUM(manual_confirm_steps), 0) AS manual_confirm_steps, " +
			"COALESCE(SUM(CASE WHEN first_validation_status <> '' THEN 1 ELSE 0 END), 0) AS first_validated, " +
			"COALESCE(SUM(CASE WHEN first_validation_status = 'PASSED' THEN 1 ELSE 0 END), 0) AS first_passed",
	).Scan(&row).Error
	if err != nil {
		return nil, err
	}
	return &AIPlanRecordAggregate{
		PlanCount:          row.PlanCount,
		TotalSteps:         row.TotalSteps,
		FlowCallSteps:      row.FlowCallSteps,
		AdoptedPlanCount:   row.AdoptedPlanCount,
		AdoptedSteps:       row.AdoptedSteps,
		ModifiedSteps:      row.ModifiedSteps,
		ManualConfirmSteps: row.ManualConfirmSteps,
		FirstValidated:     row.FirstValidated,
		FirstPassed:        row.FirstPassed,
	}, nil
}

// CountExecutionsByStatus 按状态统计回归执行数量。
func (r *AIRegressionRepo) CountExecutionsByStatus(ctx context.Context, projectID uint, since *time.Time) (total, passed, failed int64, err error) {
	base := func() *gorm.DB {
		q := r.db.WithContext(ctx).Model(&model.AIRegressionExecution{}).Where("project_id = ?", projectID)
		if since != nil {
			q = q.Where("created_at >= ?", *since)
		}
		return q
	}
	if err = base().Count(&total).Error; err != nil {
		return 0, 0, 0, err
	}
	if err = base().Where("status = ?", model.AIRegressionExecutionStatusPassed).Count(&passed).Error; err != nil {
		return 0, 0, 0, err
	}
	if err = base().Where("status = ?", model.AIRegressionExecutionStatusFailed).Count(&failed).Error; err != nil {
		return 0, 0, 0, err
	}
	return total, passed, failed, nil
}

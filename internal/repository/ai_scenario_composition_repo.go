// ai_scenario_composition_repo.go — 测试智编场景编排数据访问层
package repository

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"testpilot/internal/model"
)

// AIScenarioCompositionFilter 场景编排列表筛选条件。
type AIScenarioCompositionFilter struct {
	ProjectID        uint
	Keyword          string
	Status           string
	ValidationStatus string
	Page             int
	PageSize         int
}

// AIScenarioCompositionRepo 场景编排 Repository。
type AIScenarioCompositionRepo struct {
	db *gorm.DB
}

// NewAIScenarioCompositionRepo 创建场景编排 Repository。
func NewAIScenarioCompositionRepo(db *gorm.DB) *AIScenarioCompositionRepo {
	return &AIScenarioCompositionRepo{db: db}
}

func (r *AIScenarioCompositionRepo) getDB(tx *gorm.DB) *gorm.DB {
	if tx != nil {
		return tx
	}
	return r.db
}

func (r *AIScenarioCompositionRepo) buildListQuery(ctx context.Context, filter AIScenarioCompositionFilter) *gorm.DB {
	q := r.db.WithContext(ctx).Model(&model.AIScenarioComposition{})
	if filter.ProjectID > 0 {
		q = q.Where("project_id = ?", filter.ProjectID)
	}
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		like := "%" + keyword + "%"
		q = q.Where("scenario_name LIKE ? OR scenario_key LIKE ? OR description LIKE ?", like, like, like)
	}
	if status := strings.TrimSpace(filter.Status); status != "" {
		q = q.Where("status = ?", status)
	}
	if validationStatus := strings.TrimSpace(filter.ValidationStatus); validationStatus != "" {
		q = q.Where("latest_validation_status = ?", validationStatus)
	}
	return q
}

// List 分页查询场景编排。
func (r *AIScenarioCompositionRepo) List(ctx context.Context, filter AIScenarioCompositionFilter) ([]model.AIScenarioComposition, int64, error) {
	q := r.buildListQuery(ctx, filter)

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var compositions []model.AIScenarioComposition
	offset := (filter.Page - 1) * filter.PageSize
	err := q.Order("updated_at DESC, id DESC").
		Offset(offset).
		Limit(filter.PageSize).
		Find(&compositions).Error
	return compositions, total, err
}

// GetByID 按 ID 查询场景编排。
func (r *AIScenarioCompositionRepo) GetByID(ctx context.Context, id uint) (*model.AIScenarioComposition, error) {
	var composition model.AIScenarioComposition
	if err := r.db.WithContext(ctx).First(&composition, id).Error; err != nil {
		return nil, err
	}
	return &composition, nil
}

// GetByProjectAndKey 按项目和稳定 Key 查询场景编排。
func (r *AIScenarioCompositionRepo) GetByProjectAndKey(ctx context.Context, projectID uint, scenarioKey string) (*model.AIScenarioComposition, error) {
	var composition model.AIScenarioComposition
	err := r.db.WithContext(ctx).
		Where("project_id = ? AND scenario_key = ?", projectID, scenarioKey).
		First(&composition).Error
	if err != nil {
		return nil, err
	}
	return &composition, nil
}

// Create 在事务中创建场景编排。
func (r *AIScenarioCompositionRepo) Create(ctx context.Context, tx *gorm.DB, composition *model.AIScenarioComposition) error {
	return r.getDB(tx).WithContext(ctx).Create(composition).Error
}

// UpdateFields 在事务中更新场景编排字段。
func (r *AIScenarioCompositionRepo) UpdateFields(ctx context.Context, tx *gorm.DB, id uint, fields map[string]interface{}) error {
	return r.getDB(tx).WithContext(ctx).
		Model(&model.AIScenarioComposition{}).
		Where("id = ?", id).
		Updates(fields).Error
}

// Delete 在事务中物理删除场景编排草稿。
func (r *AIScenarioCompositionRepo) Delete(ctx context.Context, tx *gorm.DB, id uint) error {
	return r.getDB(tx).WithContext(ctx).Delete(&model.AIScenarioComposition{}, id).Error
}

// ListSteps 查询场景编排步骤。
func (r *AIScenarioCompositionRepo) ListSteps(ctx context.Context, scenarioID uint) ([]model.AIScenarioStep, error) {
	var steps []model.AIScenarioStep
	err := r.db.WithContext(ctx).
		Where("scenario_id = ?", scenarioID).
		Order("step_no ASC, id ASC").
		Find(&steps).Error
	return steps, err
}

// ListStepsTx 在事务中查询场景编排步骤。
func (r *AIScenarioCompositionRepo) ListStepsTx(ctx context.Context, tx *gorm.DB, scenarioID uint) ([]model.AIScenarioStep, error) {
	var steps []model.AIScenarioStep
	err := r.getDB(tx).WithContext(ctx).
		Where("scenario_id = ?", scenarioID).
		Order("step_no ASC, id ASC").
		Find(&steps).Error
	return steps, err
}

// GetStepByID 查询单个编排步骤。
func (r *AIScenarioCompositionRepo) GetStepByID(ctx context.Context, stepID uint) (*model.AIScenarioStep, error) {
	var step model.AIScenarioStep
	if err := r.db.WithContext(ctx).First(&step, stepID).Error; err != nil {
		return nil, err
	}
	return &step, nil
}

// CreateStep 在事务中创建编排步骤。
func (r *AIScenarioCompositionRepo) CreateStep(ctx context.Context, tx *gorm.DB, step *model.AIScenarioStep) error {
	return r.getDB(tx).WithContext(ctx).Create(step).Error
}

// UpdateStepFields 在事务中更新编排步骤字段。
func (r *AIScenarioCompositionRepo) UpdateStepFields(ctx context.Context, tx *gorm.DB, stepID uint, fields map[string]interface{}) error {
	return r.getDB(tx).WithContext(ctx).
		Model(&model.AIScenarioStep{}).
		Where("id = ?", stepID).
		Updates(fields).Error
}

// DeleteStep 在事务中删除编排步骤。
func (r *AIScenarioCompositionRepo) DeleteStep(ctx context.Context, tx *gorm.DB, stepID uint) error {
	return r.getDB(tx).WithContext(ctx).Delete(&model.AIScenarioStep{}, stepID).Error
}

// DeleteStepsByScenario 在事务中删除某个编排的全部步骤。
func (r *AIScenarioCompositionRepo) DeleteStepsByScenario(ctx context.Context, tx *gorm.DB, scenarioID uint) error {
	return r.getDB(tx).WithContext(ctx).
		Where("scenario_id = ?", scenarioID).
		Delete(&model.AIScenarioStep{}).Error
}

// CountSteps 统计场景步骤数量。
func (r *AIScenarioCompositionRepo) CountSteps(ctx context.Context, scenarioID uint) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.AIScenarioStep{}).
		Where("scenario_id = ?", scenarioID).
		Count(&count).Error
	return count, err
}

// UpdateStepNo 在事务中更新步骤序号。
func (r *AIScenarioCompositionRepo) UpdateStepNo(ctx context.Context, tx *gorm.DB, stepID uint, stepNo int) error {
	return r.getDB(tx).WithContext(ctx).
		Model(&model.AIScenarioStep{}).
		Where("id = ?", stepID).
		Update("step_no", stepNo).Error
}

// CreateVersion 在事务中创建编排版本。
func (r *AIScenarioCompositionRepo) CreateVersion(ctx context.Context, tx *gorm.DB, version *model.AIScenarioCompositionVersion) error {
	return r.getDB(tx).WithContext(ctx).Create(version).Error
}

// MaxVersionNo 查询编排最大版本号。
func (r *AIScenarioCompositionRepo) MaxVersionNo(ctx context.Context, compositionID uint) (int, error) {
	var maxNo *int
	err := r.db.WithContext(ctx).Model(&model.AIScenarioCompositionVersion{}).
		Where("composition_id = ?", compositionID).
		Select("MAX(version_no)").Scan(&maxNo).Error
	if err != nil {
		return 0, err
	}
	if maxNo == nil {
		return 0, nil
	}
	return *maxNo, nil
}

// ListVersions 查询编排版本列表。
func (r *AIScenarioCompositionRepo) ListVersions(ctx context.Context, compositionID uint) ([]model.AIScenarioCompositionVersion, error) {
	var versions []model.AIScenarioCompositionVersion
	err := r.db.WithContext(ctx).
		Where("composition_id = ?", compositionID).
		Order("version_no DESC").
		Find(&versions).Error
	return versions, err
}

// GetVersionByID 按 ID 查询编排版本快照。
func (r *AIScenarioCompositionRepo) GetVersionByID(ctx context.Context, versionID uint) (*model.AIScenarioCompositionVersion, error) {
	var version model.AIScenarioCompositionVersion
	if err := r.db.WithContext(ctx).First(&version, versionID).Error; err != nil {
		return nil, err
	}
	return &version, nil
}

// CreateValidation 在事务中创建编排验证记录。
func (r *AIScenarioCompositionRepo) CreateValidation(ctx context.Context, tx *gorm.DB, validation *model.AICompositionValidation) error {
	return r.getDB(tx).WithContext(ctx).Create(validation).Error
}

// GetValidationByIdempotencyKey 按幂等键查询已创建的验证记录。
func (r *AIScenarioCompositionRepo) GetValidationByIdempotencyKey(ctx context.Context, projectID, compositionID uint, key string) (*model.AICompositionValidation, error) {
	var validation model.AICompositionValidation
	err := r.db.WithContext(ctx).
		Where("project_id = ? AND composition_id = ? AND idempotency_key = ?", projectID, compositionID, key).
		First(&validation).Error
	if err != nil {
		return nil, err
	}
	return &validation, nil
}

// ListValidations 查询编排验证历史。
func (r *AIScenarioCompositionRepo) ListValidations(ctx context.Context, compositionID uint) ([]model.AICompositionValidation, error) {
	var validations []model.AICompositionValidation
	err := r.db.WithContext(ctx).
		Where("composition_id = ?", compositionID).
		Order("created_at DESC, id DESC").
		Find(&validations).Error
	return validations, err
}

// CountValidations 统计编排验证记录数量，用于草稿物理删除保护。
func (r *AIScenarioCompositionRepo) CountValidations(ctx context.Context, compositionID uint) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.AICompositionValidation{}).
		Where("composition_id = ?", compositionID).
		Count(&count).Error
	return count, err
}

// ListAssertionResults 查询单次验证中的断言结果。
func (r *AIScenarioCompositionRepo) ListAssertionResults(ctx context.Context, validationID uint) ([]model.AICompositionAssertionResult, error) {
	var results []model.AICompositionAssertionResult
	err := r.db.WithContext(ctx).
		Where("validation_id = ?", validationID).
		Order("id ASC").
		Find(&results).Error
	return results, err
}

// CreateAssertionResults 在事务中批量创建断言结果。
func (r *AIScenarioCompositionRepo) CreateAssertionResults(ctx context.Context, tx *gorm.DB, results []model.AICompositionAssertionResult) error {
	if len(results) == 0 {
		return nil
	}
	return r.getDB(tx).WithContext(ctx).Create(&results).Error
}

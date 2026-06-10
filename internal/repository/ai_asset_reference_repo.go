// ai_asset_reference_repo.go — 测试智编资产引用关系数据访问层
package repository

import (
	"context"

	"gorm.io/gorm"

	"testpilot/internal/model"
)

// AIAssetReferenceRepo 管理固定场景、断言和编排之间的引用关系。
type AIAssetReferenceRepo struct {
	db *gorm.DB
}

// NewAIAssetReferenceRepo 创建资产引用关系 Repository。
func NewAIAssetReferenceRepo(db *gorm.DB) *AIAssetReferenceRepo {
	return &AIAssetReferenceRepo{db: db}
}

func (r *AIAssetReferenceRepo) getDB(tx *gorm.DB) *gorm.DB {
	if tx != nil {
		return tx
	}
	return r.db
}

// ReplaceForSource 替换指定来源资产的全部引用关系。
func (r *AIAssetReferenceRepo) ReplaceForSource(ctx context.Context, tx *gorm.DB, sourceType string, sourceID uint, refs []model.AIAssetReference) error {
	db := r.getDB(tx).WithContext(ctx)
	if err := db.Where("source_type = ? AND source_id = ?", sourceType, sourceID).
		Delete(&model.AIAssetReference{}).Error; err != nil {
		return err
	}
	if len(refs) == 0 {
		return nil
	}
	return db.Create(&refs).Error
}

// ListBySource 查询来源资产引用的目标资产列表。
func (r *AIAssetReferenceRepo) ListBySource(ctx context.Context, sourceType string, sourceID uint) ([]model.AIAssetReference, error) {
	var refs []model.AIAssetReference
	err := r.db.WithContext(ctx).
		Where("source_type = ? AND source_id = ?", sourceType, sourceID).
		Order("target_type ASC, target_id ASC").
		Find(&refs).Error
	return refs, err
}

// ListByTarget 查询引用某个目标资产的来源资产列表。
func (r *AIAssetReferenceRepo) ListByTarget(ctx context.Context, targetType string, targetID uint) ([]model.AIAssetReference, error) {
	var refs []model.AIAssetReference
	err := r.db.WithContext(ctx).
		Where("target_type = ? AND target_id = ?", targetType, targetID).
		Order("created_at DESC, id DESC").
		Find(&refs).Error
	return refs, err
}

// ListByTargets 批量查询引用指定目标资产集合的来源资产。
func (r *AIAssetReferenceRepo) ListByTargets(ctx context.Context, targetType string, targetIDs []uint) ([]model.AIAssetReference, error) {
	var refs []model.AIAssetReference
	if len(targetIDs) == 0 {
		return refs, nil
	}
	err := r.db.WithContext(ctx).
		Where("target_type = ? AND target_id IN ?", targetType, targetIDs).
		Order("created_at DESC, id DESC").
		Find(&refs).Error
	return refs, err
}

// CountByTarget 统计某个资产被引用次数。
func (r *AIAssetReferenceRepo) CountByTarget(ctx context.Context, targetType string, targetID uint) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.AIAssetReference{}).
		Where("target_type = ? AND target_id = ?", targetType, targetID).
		Count(&count).Error
	return count, err
}

// CountSourceTargets 按来源资产统计引用数量。
func (r *AIAssetReferenceRepo) CountSourceTargets(ctx context.Context, sourceType string, sourceID uint, targetType string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&model.AIAssetReference{}).
		Where("source_type = ? AND source_id = ? AND target_type = ?", sourceType, sourceID, targetType).
		Count(&count).Error
	return count, err
}

// audit_repo.go — 审计日志数据访问层
package repository

import (
	"context"
	"encoding/json"

	"gorm.io/gorm"

	"testpilot/internal/model"
)

// AuditRepository 审计日志数据访问接口
type AuditRepository interface {
	// WriteLogTx 在事务中写入审计日志
	WriteLogTx(tx *gorm.DB, actorID uint, action, targetType string, targetID uint, before, after any) error
	// ListRecent 获取最近的审计日志
	ListRecent(ctx context.Context, limit int) ([]model.AuditLog, error)
	// ListByTarget 按目标对象查询审计日志
	ListByTarget(ctx context.Context, targetType string, targetID uint, limit int) ([]model.AuditLog, error)
}

// auditRepo AuditRepository 的 GORM 实现
type auditRepo struct {
	db *gorm.DB
}

// NewAuditRepo 创建审计仓库
func NewAuditRepo(db *gorm.DB) AuditRepository {
	return &auditRepo{db: db}
}

func (r *auditRepo) WriteLogTx(tx *gorm.DB, actorID uint, action, targetType string, targetID uint, before, after any) error {
	beforeJSON, err := toJSON(before)
	if err != nil {
		return err
	}
	afterJSON, err := toJSON(after)
	if err != nil {
		return err
	}
	log := model.AuditLog{
		ActorID:    actorID,
		Action:     action,
		TargetType: targetType,
		TargetID:   targetID,
		BeforeData: beforeJSON,
		AfterData:  afterJSON,
	}
	return tx.Create(&log).Error
}

func (r *auditRepo) ListRecent(ctx context.Context, limit int) ([]model.AuditLog, error) {
	var logs []model.AuditLog
	if err := r.db.WithContext(ctx).Order("id desc").Limit(limit).Find(&logs).Error; err != nil {
		return nil, err
	}
	return logs, nil
}

func (r *auditRepo) ListByTarget(ctx context.Context, targetType string, targetID uint, limit int) ([]model.AuditLog, error) {
	var logs []model.AuditLog
	if err := r.db.WithContext(ctx).
		Where("target_type = ? AND target_id = ?", targetType, targetID).
		Order("id desc").Limit(limit).Find(&logs).Error; err != nil {
		return nil, err
	}
	return logs, nil
}

// toJSON 将任意值序列化为 JSON 字符串
func toJSON(v any) (string, error) {
	if v == nil {
		return "", nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

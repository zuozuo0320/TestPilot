// audit_service.go — 审计日志服务
package service

import (
	"context"

	"testpilot/internal/model"
	"testpilot/internal/repository"
)

// AuditService 审计日志服务
type AuditService struct {
	auditRepo repository.AuditRepository
}

// NewAuditService 创建审计服务
func NewAuditService(repo repository.AuditRepository) *AuditService {
	return &AuditService{auditRepo: repo}
}

// ListRecent 获取最近审计日志
func (s *AuditService) ListRecent(ctx context.Context, limit int) ([]model.AuditLog, error) {
	return s.auditRepo.ListRecent(ctx, limit)
}

// ListByTarget 按目标对象查询审计日志
func (s *AuditService) ListByTarget(ctx context.Context, targetType string, targetID uint, limit int) ([]model.AuditLog, error) {
	return s.auditRepo.ListByTarget(ctx, targetType, targetID, limit)
}

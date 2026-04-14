// tag_service.go — 标签管理业务逻辑
package service

import (
	"context"
	"log/slog"
	"regexp"
	"strings"

	"gorm.io/gorm"

	"testpilot/internal/model"
	"testpilot/internal/repository"
)

var colorRegex = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

const (
	tagLimitPerProject  = 100
	tagLimitPerTestCase = 10
)

// TagService 标签管理服务
type TagService struct {
	tagRepo   repository.TagRepository
	auditRepo repository.AuditRepository
	txMgr     *repository.TxManager
	logger    *slog.Logger
}

// NewTagService 创建标签服务
func NewTagService(
	tagRepo repository.TagRepository,
	auditRepo repository.AuditRepository,
	txMgr *repository.TxManager,
	logger *slog.Logger,
) *TagService {
	return &TagService{
		tagRepo:   tagRepo,
		auditRepo: auditRepo,
		txMgr:     txMgr,
		logger:    logger.With("module", "tag"),
	}
}

// ── Input 结构 ──

type CreateTagInput struct {
	Name        string
	Color       string
	Description string
}

type UpdateTagInput struct {
	Name        *string
	Color       *string
	Description *string
}

// ── CRUD ──

// Create 创建标签
func (s *TagService) Create(ctx context.Context, projectID, userID uint, input CreateTagInput) (*model.Tag, error) {
	s.logger.Info("create tag start", "project_id", projectID, "user_id", userID, "name", input.Name)

	name := strings.TrimSpace(input.Name)
	if name == "" || len(name) < 2 || len(name) > 50 {
		return nil, ErrBadRequest(CodeParamsError, "标签名称须为 2-50 字符")
	}

	color := strings.TrimSpace(input.Color)
	if color == "" {
		color = "#3B82F6"
	}
	if !colorRegex.MatchString(color) {
		return nil, ErrBadRequest(CodeTagColorInvalid, "颜色格式不符合 #RRGGBB 规范")
	}

	// 项目限额
	count, err := s.tagRepo.CountByProject(ctx, projectID)
	if err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}
	if count >= tagLimitPerProject {
		return nil, ErrBadRequest(CodeTagLimitExceeded, "当前项目标签数量已达上限（100）")
	}

	tag := model.Tag{
		ProjectID:   projectID,
		Name:        name,
		Color:       color,
		Description: strings.TrimSpace(input.Description),
		CreatedBy:   userID,
	}
	if err := s.tagRepo.Create(ctx, &tag); err != nil {
		if isDuplicateError(err) {
			return nil, ErrConflict(CodeTagNameDuplicate, "同项目下标签名已存在")
		}
		return nil, ErrInternal(CodeInternal, err)
	}

	s.logger.Info("create tag success", "tag_id", tag.ID, "project_id", projectID)
	return &tag, nil
}

// Update 更新标签
func (s *TagService) Update(ctx context.Context, projectID, tagID, userID uint, input UpdateTagInput) error {
	s.logger.Info("update tag start", "project_id", projectID, "tag_id", tagID, "user_id", userID)

	tag, err := s.tagRepo.FindByID(ctx, tagID)
	if err != nil {
		return ErrNotFound(CodeTagNotFound, "标签不存在")
	}
	if tag.ProjectID != projectID {
		return ErrNotFound(CodeTagNotFound, "标签不存在")
	}

	fields := map[string]any{}
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" || len(name) < 2 || len(name) > 50 {
			return ErrBadRequest(CodeParamsError, "标签名称须为 2-50 字符")
		}
		fields["name"] = name
	}
	if input.Color != nil {
		color := strings.TrimSpace(*input.Color)
		if !colorRegex.MatchString(color) {
			return ErrBadRequest(CodeTagColorInvalid, "颜色格式不符合 #RRGGBB 规范")
		}
		fields["color"] = color
	}
	if input.Description != nil {
		fields["description"] = strings.TrimSpace(*input.Description)
	}

	if len(fields) == 0 {
		return ErrBadRequest(CodeParamsError, "no fields to update")
	}

	if err := s.tagRepo.Update(ctx, tag, fields); err != nil {
		if isDuplicateError(err) {
			return ErrConflict(CodeTagNameDuplicate, "同项目下标签名已存在")
		}
		return ErrInternal(CodeInternal, err)
	}

	s.logger.Info("update tag success", "tag_id", tagID, "project_id", projectID)
	return nil
}

// Delete 删除标签（事务内级联清除关联）
func (s *TagService) Delete(ctx context.Context, projectID, tagID uint) (int64, error) {
	s.logger.Warn("delete tag request", "project_id", projectID, "tag_id", tagID)

	tag, err := s.tagRepo.FindByID(ctx, tagID)
	if err != nil {
		return 0, ErrNotFound(CodeTagNotFound, "标签不存在")
	}
	if tag.ProjectID != projectID {
		return 0, ErrNotFound(CodeTagNotFound, "标签不存在")
	}

	var unlinked int64
	err = s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		var txErr error
		unlinked, txErr = s.tagRepo.DeleteRelsByTagID(ctx, tx, tagID)
		if txErr != nil {
			return txErr
		}
		return s.tagRepo.Delete(ctx, tagID)
	})
	if err != nil {
		s.logger.Error("delete tag failed", "tag_id", tagID, "error", err)
		return 0, ErrInternal(CodeInternal, err)
	}

	s.logger.Info("delete tag success", "tag_id", tagID, "unlinked_cases", unlinked)
	return unlinked, nil
}

// ListPaged 分页查询标签
func (s *TagService) ListPaged(ctx context.Context, projectID uint, f repository.TagFilter) ([]model.Tag, int64, error) {
	return s.tagRepo.ListPaged(ctx, projectID, f)
}

// ListOptions 标签候选列表（轻量，不分页）
func (s *TagService) ListOptions(ctx context.Context, projectID uint, keyword string) ([]repository.TagBrief, error) {
	return s.tagRepo.ListOptions(ctx, projectID, keyword)
}

// TagRepo 暴露 repo（供 testcase service 调用）
func (s *TagService) TagRepo() repository.TagRepository {
	return s.tagRepo
}

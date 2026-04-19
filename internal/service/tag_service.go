// tag_service.go — 标签管理业务逻辑
//
// 封装标签模块的核心业务规则，包括：
//   - 创建：参数校验 + 项目限额检查 + 重复名检查
//   - 更新：归属校验 + 部分字段更新 + 重复名检查
//   - 删除：归属校验 + 事务内解除关联 + 删除标签
//   - 查询：分页列表 + 候选列表
//
// 所有业务错误通过 BizError 统一抛出，由 Handler 层统一处理。
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

// colorRegex 校验颜色格式：必须为 #RRGGBB 十六进制格式
var colorRegex = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

const (
	tagLimitPerProject  = 100 // 每个项目最多创建 100 个标签
	tagLimitPerTestCase = 10  // 每个用例最多关联 10 个标签
)

// TagService 标签管理服务，封装标签模块的全部业务规则。
// 通过构造函数注入 Repository、事务管理器和日志器。
type TagService struct {
	tagRepo   repository.TagRepository   // 标签数据访问层
	auditRepo repository.AuditRepository // 审计日志数据访问层
	txMgr     *repository.TxManager       // 数据库事务管理器
	logger    *slog.Logger                // 结构化日志（带 module=tag 前缀）
}

// NewTagService 创建标签服务实例，通过依赖注入初始化所有依赖。
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
		logger:    logger.With("module", "tag"), // 日志自动添加 module=tag 前缀
	}
}

// ── Input 结构（Handler → Service 的参数传递对象） ──

// CreateTagInput 创建标签的输入参数
type CreateTagInput struct {
	Name        string // 标签名称（2-50 字符）
	Color       string // 颜色，#RRGGBB 格式，空则使用默认蓝色
	Description string // 描述（可选）
}

// UpdateTagInput 更新标签的输入参数（指针类型，nil 表示不更新该字段）
type UpdateTagInput struct {
	Name        *string // 新名称（nil 表示不修改）
	Color       *string // 新颜色（nil 表示不修改）
	Description *string // 新描述（nil 表示不修改）
}

// ── CRUD ──

// Create 创建标签。
// 流程：参数校验 → 项目限额检查 → 写入数据库 → 重复名检查（数据库唯一索引）
//
// 参数：
//   - ctx:       请求上下文（携带 trace_id）
//   - projectID: 所属项目 ID
//   - userID:    创建人用户 ID
//   - input:     创建参数（名称、颜色、描述）
func (s *TagService) Create(ctx context.Context, projectID, userID uint, input CreateTagInput) (*model.Tag, error) {
	s.logger.Info("开始创建标签", "project_id", projectID, "user_id", userID, "name", input.Name)

	// 1. 名称校验：去首尾空格后必须为 2-50 字符
	name := strings.TrimSpace(input.Name)
	if name == "" || len(name) < 2 || len(name) > 50 {
		return nil, ErrBadRequest(CodeParamsError, "标签名称须为 2-50 字符")
	}

	// 2. 颜色校验：空值使用默认蓝色，非空必须符合 #RRGGBB 格式
	color := strings.TrimSpace(input.Color)
	if color == "" {
		color = "#3B82F6" // 默认蓝色
	}
	if !colorRegex.MatchString(color) {
		return nil, ErrBadRequest(CodeTagColorInvalid, "颜色格式不符合 #RRGGBB 规范")
	}

	// 3. 项目标签限额检查：每个项目最多 100 个标签
	count, err := s.tagRepo.CountByProject(ctx, projectID)
	if err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}
	if count >= tagLimitPerProject {
		return nil, ErrBadRequest(CodeTagLimitExceeded, "当前项目标签数量已达上限（100）")
	}

	// 4. 构建标签实体并写入数据库
	tag := model.Tag{
		ProjectID:   projectID,
		Name:        name,
		Color:       color,
		Description: strings.TrimSpace(input.Description),
		CreatedBy:   userID,
	}
	if err := s.tagRepo.Create(ctx, &tag); err != nil {
		// 数据库唯一索引抦截重复名称
		if isDuplicateError(err) {
			return nil, ErrConflict(CodeTagNameDuplicate, "同项目下标签名已存在")
		}
		return nil, ErrInternal(CodeInternal, err)
	}

	s.logger.Info("标签创建成功", "tag_id", tag.ID, "project_id", projectID)
	return &tag, nil
}

// Update 更新标签（部分更新，仅修改传入的字段）。
// 流程：查询标签 → 归属校验 → 参数校验 → 更新数据库
func (s *TagService) Update(ctx context.Context, projectID, tagID, userID uint, input UpdateTagInput) error {
	s.logger.Info("开始更新标签", "project_id", projectID, "tag_id", tagID, "user_id", userID)

	// 1. 查询标签是否存在
	tag, err := s.tagRepo.FindByID(ctx, tagID)
	if err != nil {
		return ErrNotFound(CodeTagNotFound, "标签不存在")
	}
	// 2. 校验标签归属项目（防止跨项目操作）
	if tag.ProjectID != projectID {
		return ErrNotFound(CodeTagNotFound, "标签不存在")
	}

	// 3. 收集需要更新的字段（仅处理非 nil 的指针字段）
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

	// 必须至少有一个字段需要更新
	if len(fields) == 0 {
		return ErrBadRequest(CodeParamsError, "no fields to update")
	}

	// 4. 执行更新
	if err := s.tagRepo.Update(ctx, tag, fields); err != nil {
		// 数据库唯一索引抦截重复名称
		if isDuplicateError(err) {
			return ErrConflict(CodeTagNameDuplicate, "同项目下标签名已存在")
		}
		return ErrInternal(CodeInternal, err)
	}

	s.logger.Info("标签更新成功", "tag_id", tagID, "project_id", projectID)
	return nil
}

// Delete 删除标签，同时解除所有用例关联。
// 在同一事务内先解绑关联关系，再删除标签本体，保证数据一致性。
//
// 返回：
//   - unlinked: 被解绑的用例数量
//   - err:      BizError（标签不存在）或系统错误
func (s *TagService) Delete(ctx context.Context, projectID, tagID uint) (int64, error) {
	s.logger.Warn("收到删除标签请求", "project_id", projectID, "tag_id", tagID)

	// 1. 查询标签是否存在
	tag, err := s.tagRepo.FindByID(ctx, tagID)
	if err != nil {
		return 0, ErrNotFound(CodeTagNotFound, "标签不存在")
	}
	// 2. 校验标签归属项目（防止跨项目误删）
	if tag.ProjectID != projectID {
		return 0, ErrNotFound(CodeTagNotFound, "标签不存在")
	}

	// 3. 事务内执行：先解绑关联，再删除标签
	// ❗ 所有 repo 方法必须使用 tx 参数（参见后端规范 §2.2 事务规则）
	var unlinked int64
	err = s.txMgr.WithTx(ctx, func(tx *gorm.DB) error {
		var txErr error
		// 解除该标签与所有用例的关联关系
		unlinked, txErr = s.tagRepo.DeleteRelsByTagID(ctx, tx, tagID)
		if txErr != nil {
			return txErr
		}
		// 删除标签本体
		return s.tagRepo.Delete(ctx, tx, tagID)
	})
	if err != nil {
		s.logger.Error("标签删除失败", "tag_id", tagID, "error", err)
		return 0, ErrInternal(CodeInternal, err)
	}

	s.logger.Info("标签删除成功", "tag_id", tagID, "unlinked_cases", unlinked)
	return unlinked, nil
}

// ListPaged 分页查询标签（包含关联用例数、创建人信息，由 Repository 层 JOIN 查询）
func (s *TagService) ListPaged(ctx context.Context, projectID uint, f repository.TagFilter) ([]model.Tag, int64, error) {
	return s.tagRepo.ListPaged(ctx, projectID, f)
}

// ListOptions 标签候选列表（轻量版，仅返回 id/name/color，不分页）
func (s *TagService) ListOptions(ctx context.Context, projectID uint, keyword string) ([]repository.TagBrief, error) {
	return s.tagRepo.ListOptions(ctx, projectID, keyword)
}

// TagRepo 暴露 Repository 实例，供用例 Service 在事务中操作标签关联表
func (s *TagService) TagRepo() repository.TagRepository {
	return s.tagRepo
}

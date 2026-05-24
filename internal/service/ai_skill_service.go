// Package service — 需求智生-Skill 模板业务逻辑层
//
// AISkillService 管理 Skill 模板的全生命周期：
//   - 列出项目可用 Skill（系统 + 项目合并）
//   - 创建项目级 Skill
//   - 编辑 Skill（CAS 防并发）
//   - 删除 Skill（禁止删除系统内置）
//   - 启用 / 禁用
package service

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"testpilot/internal/model"
	"testpilot/internal/repository"
)

// AISkillService Skill 模板业务逻辑层
type AISkillService struct {
	logger    *slog.Logger
	skillRepo repository.AISkillRepository
	txMgr     *repository.TxManager
}

// NewAISkillService 创建 Skill Service
func NewAISkillService(
	logger *slog.Logger,
	skillRepo repository.AISkillRepository,
	txMgr *repository.TxManager,
) *AISkillService {
	return &AISkillService{
		logger:    logger.With("module", "ai_skill"),
		skillRepo: skillRepo,
		txMgr:     txMgr,
	}
}

// ========== 输入结构体 ==========

// CreateSkillInput 创建 Skill 参数
type CreateSkillInput struct {
	ProjectID      uint
	SkillKey       string
	Name           string
	Scope          string
	Description    string
	PromptTemplate string
	OutputSchema   string
	CreatedBy      uint
}

// UpdateSkillInput 更新 Skill 参数
type UpdateSkillInput struct {
	ID             uint
	ProjectID      uint
	Name           string
	Scope          string
	Description    string
	PromptTemplate string
	OutputSchema   string
	LockVersion    int
}

// ========== 校验规则 ==========

// 合法作用域
var validScopes = map[string]bool{
	model.SkillScopeFunctional: true,
	model.SkillScopeAPI:        true,
	model.SkillScopeCompat:     true,
	model.SkillScopeSecurity:   true,
	model.SkillScopeCustom:     true,
}

// skill_key 校验：仅允许小写字母、数字、下划线，2-50 字符
var skillKeyRegex = regexp.MustCompile(`^[a-z][a-z0-9_]{1,49}$`)

// 必须包含的占位符
var requiredPlaceholders = []string{"{{requirement_text}}"}

// ========== 业务方法 ==========

// ListForProject 列出项目可用的 Skill 列表。
// 合并逻辑：系统 Skill 全局可用；如果项目有同 skill_key 的覆写版本，使用项目版本。
func (s *AISkillService) ListForProject(ctx context.Context, projectID uint) ([]model.AISkill, error) {
	skills, err := s.skillRepo.ListProjectSkills(ctx, projectID)
	if err != nil {
		s.logger.Error("查询项目 Skill 列表失败", "error", err, "project_id", projectID)
		return nil, ErrInternal(CodeInternal, err)
	}

	// 合并：项目级 Skill 覆写同 key 的系统 Skill
	keyMap := make(map[string]model.AISkill)
	for _, sk := range skills {
		existing, exists := keyMap[sk.SkillKey]
		if !exists {
			sk.EffectiveSource = "system"
			if sk.ProjectID > 0 {
				sk.EffectiveSource = "project_override"
			}
			keyMap[sk.SkillKey] = sk
		} else {
			// 项目级覆写优先
			if sk.ProjectID > 0 && existing.ProjectID == 0 {
				sk.EffectiveSource = "project_override"
				keyMap[sk.SkillKey] = sk
			}
		}
	}

	// 输出为排序列表
	result := make([]model.AISkill, 0, len(keyMap))
	for _, sk := range keyMap {
		result = append(result, sk)
	}
	return result, nil
}

// GetByID 查询 Skill 详情
func (s *AISkillService) GetByID(ctx context.Context, projectID, skillID uint) (*model.AISkill, error) {
	skill, err := s.skillRepo.FindByID(ctx, skillID)
	if err != nil {
		return nil, ErrNotFound(CodeReqSkillNotFound, "Skill 不存在")
	}
	// 系统 Skill 全局可见；项目 Skill 需校验归属
	if skill.ProjectID != 0 && skill.ProjectID != projectID {
		return nil, ErrNotFound(CodeReqSkillNotFound, "Skill 不存在")
	}
	return skill, nil
}

// Create 创建项目级 Skill
func (s *AISkillService) Create(ctx context.Context, input CreateSkillInput) (*model.AISkill, error) {
	// 1. 校验 skill_key 格式
	if !skillKeyRegex.MatchString(input.SkillKey) {
		return nil, ErrBadRequest(CodeParamsError, "skill_key 仅允许小写字母开头、小写字母/数字/下划线组合，2-50 字符")
	}

	// 2. 校验作用域
	if !validScopes[input.Scope] {
		return nil, ErrBadRequest(CodeParamsError, fmt.Sprintf("不支持的作用域: %s", input.Scope))
	}

	// 3. 校验 prompt_template 包含必需占位符
	if err := s.validatePromptTemplate(input.PromptTemplate); err != nil {
		return nil, err
	}

	// 4. 校验 skill_key 在项目下唯一
	existing, _ := s.skillRepo.FindByProjectAndKey(ctx, input.ProjectID, input.SkillKey)
	if existing != nil {
		return nil, ErrConflict(CodeReqSkillKeyExists, fmt.Sprintf("项目下已存在相同 skill_key: %s", input.SkillKey))
	}

	// 5. 规范化 output_schema
	outputSchema := input.OutputSchema
	if outputSchema == "" {
		outputSchema = "standard_case_v1"
	}

	// 6. 创建
	skill := &model.AISkill{
		ProjectID:      input.ProjectID,
		SkillKey:       input.SkillKey,
		Name:           input.Name,
		Scope:          input.Scope,
		Description:    input.Description,
		PromptTemplate: input.PromptTemplate,
		OutputSchema:   outputSchema,
		IsSystem:       false,
		IsActive:       true,
		CreatedBy:      input.CreatedBy,
	}

	if err := s.skillRepo.Create(ctx, skill); err != nil {
		s.logger.Error("创建 Skill 失败", "error", err, "project_id", input.ProjectID, "skill_key", input.SkillKey)
		return nil, ErrInternal(CodeInternal, err)
	}

	s.logger.Info("Skill 创建成功", "skill_id", skill.ID, "project_id", input.ProjectID, "skill_key", input.SkillKey)
	return skill, nil
}

// Update 更新 Skill（CAS 保护）
func (s *AISkillService) Update(ctx context.Context, input UpdateSkillInput) (*model.AISkill, error) {
	skill, err := s.skillRepo.FindByID(ctx, input.ID)
	if err != nil {
		return nil, ErrNotFound(CodeReqSkillNotFound, "Skill 不存在")
	}
	// 系统 Skill 不允许通过此接口修改
	if skill.IsSystem {
		return nil, ErrForbidden(CodeReqSkillSystemNoDelete, "系统内置 Skill 不可编辑")
	}
	// 项目归属校验
	if skill.ProjectID != input.ProjectID {
		return nil, ErrNotFound(CodeReqSkillNotFound, "Skill 不存在")
	}

	// 校验 prompt_template
	if input.PromptTemplate != "" {
		if err := s.validatePromptTemplate(input.PromptTemplate); err != nil {
			return nil, err
		}
	}

	// 校验作用域
	if input.Scope != "" && !validScopes[input.Scope] {
		return nil, ErrBadRequest(CodeParamsError, fmt.Sprintf("不支持的作用域: %s", input.Scope))
	}

	// CAS 更新
	fields := map[string]interface{}{}
	if input.Name != "" {
		fields["name"] = input.Name
	}
	if input.Scope != "" {
		fields["scope"] = input.Scope
	}
	if input.Description != "" {
		fields["description"] = input.Description
	}
	if input.PromptTemplate != "" {
		fields["prompt_template"] = input.PromptTemplate
	}
	if input.OutputSchema != "" {
		fields["output_schema"] = input.OutputSchema
	}

	if len(fields) == 0 {
		return skill, nil
	}

	affected, err := s.skillRepo.CASUpdate(ctx, input.ID, input.LockVersion, fields)
	if err != nil {
		s.logger.Error("更新 Skill 失败", "error", err, "skill_id", input.ID)
		return nil, ErrInternal(CodeInternal, err)
	}
	if affected == 0 {
		return nil, ErrConflict(CodeReqSkillVersionConflict, "Skill 已被他人修改，请刷新后重试")
	}

	// 重新查询最新版本
	updated, _ := s.skillRepo.FindByID(ctx, input.ID)
	s.logger.Info("Skill 更新成功", "skill_id", input.ID)
	return updated, nil
}

// Delete 删除 Skill（软删除，禁止删除系统内置）
func (s *AISkillService) Delete(ctx context.Context, projectID, skillID uint) error {
	skill, err := s.skillRepo.FindByID(ctx, skillID)
	if err != nil {
		return ErrNotFound(CodeReqSkillNotFound, "Skill 不存在")
	}
	if skill.IsSystem {
		return ErrForbidden(CodeReqSkillSystemNoDelete, "系统内置 Skill 不可删除")
	}
	if skill.ProjectID != projectID {
		return ErrNotFound(CodeReqSkillNotFound, "Skill 不存在")
	}

	if err := s.skillRepo.SoftDelete(ctx, skillID); err != nil {
		s.logger.Error("删除 Skill 失败", "error", err, "skill_id", skillID)
		return ErrInternal(CodeInternal, err)
	}

	s.logger.Info("Skill 已删除", "skill_id", skillID, "project_id", projectID)
	return nil
}

// ToggleActive 启用/禁用 Skill
func (s *AISkillService) ToggleActive(ctx context.Context, projectID, skillID uint, active bool) error {
	skill, err := s.skillRepo.FindByID(ctx, skillID)
	if err != nil {
		return ErrNotFound(CodeReqSkillNotFound, "Skill 不存在")
	}
	if skill.IsSystem {
		return ErrForbidden(CodeReqSkillSystemNoDelete, "系统内置 Skill 不可禁用")
	}
	if skill.ProjectID != projectID {
		return ErrNotFound(CodeReqSkillNotFound, "Skill 不存在")
	}

	_, err = s.skillRepo.CASUpdate(ctx, skillID, skill.LockVersion, map[string]interface{}{
		"is_active": active,
	})
	if err != nil {
		return ErrInternal(CodeInternal, err)
	}

	s.logger.Info("Skill 状态变更", "skill_id", skillID, "is_active", active)
	return nil
}

// ========== 内部方法 ==========

// validatePromptTemplate 校验提示词模板是否符合规范
func (s *AISkillService) validatePromptTemplate(template string) *BizError {
	if strings.TrimSpace(template) == "" {
		return ErrBadRequest(CodeReqSkillPromptInvalid, "提示词模板不能为空")
	}

	// 检查必须包含的占位符
	for _, ph := range requiredPlaceholders {
		if !strings.Contains(template, ph) {
			return ErrBadRequest(CodeReqSkillPromptInvalid,
				fmt.Sprintf("提示词模板缺少必需占位符: %s", ph))
		}
	}

	return nil
}

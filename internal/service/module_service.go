// module_service.go — 用例目录模块业务逻辑
package service

import (
	"context"
	"testpilot/internal/model"
	"testpilot/internal/repository"
)

// ModuleService 目录管理服务
type ModuleService struct {
	moduleRepo *repository.ModuleRepo
	testCaseRepo repository.TestCaseRepository
}

// NewModuleService 创建目录服务
func NewModuleService(repo *repository.ModuleRepo, tcRepo repository.TestCaseRepository) *ModuleService {
	return &ModuleService{
		moduleRepo: repo,
		testCaseRepo: tcRepo,
	}
}

// ModuleTreeNode 树形节点（含子节点和用例计数）
type ModuleTreeNode struct {
	ID        uint              `json:"id"`
	ParentID  uint              `json:"parent_id"`
	Name      string            `json:"name"`
	SortOrder int               `json:"sort_order"`
	CaseCount int64             `json:"case_count"`
	Children  []*ModuleTreeNode `json:"children"`
}

// ModuleTreeData 包含树结构及额外的统计信息
type ModuleTreeData struct {
	Tree           []*ModuleTreeNode `json:"tree"`
	Counts         map[uint]int64    `json:"counts"`
	UnplannedCount int64             `json:"unplannedCount"`
}

// GetTree 获取项目的模块树（含用例计数）
func (s *ModuleService) GetTree(ctx context.Context, projectID uint) (*ModuleTreeData, error) {
	modules, err := s.moduleRepo.ListByProject(projectID)
	if err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}
	counts, err := s.moduleRepo.CountCasesByModuleIDs(projectID)
	if err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}

	// 计算未规划用例数（module_id = 0）
	unplannedCount, err := s.testCaseRepo.CountByModule(ctx, 0)
	if err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}

	// Build node map
	nodeMap := make(map[uint]*ModuleTreeNode)
	for _, m := range modules {
		nodeMap[m.ID] = &ModuleTreeNode{
			ID:        m.ID,
			ParentID:  m.ParentID,
			Name:      m.Name,
			SortOrder: m.SortOrder,
			CaseCount: counts[m.ID],
			Children:  []*ModuleTreeNode{},
		}
	}

	// Build tree
	var roots []*ModuleTreeNode
	for _, node := range nodeMap {
		if node.ParentID == 0 {
			roots = append(roots, node)
		} else if parent, ok := nodeMap[node.ParentID]; ok {
			parent.Children = append(parent.Children, node)
		} else {
			roots = append(roots, node)
		}
	}
	return &ModuleTreeData{
		Tree:           roots,
		Counts:         counts,
		UnplannedCount: unplannedCount,
	}, nil
}

// Create 创建模块
func (s *ModuleService) Create(ctx context.Context, projectID uint, parentID uint, name string) (*model.Module, error) {
	if name == "" {
		return nil, ErrBadRequest(CodeParamsError, "module name is required")
	}

	// Check depth limit (max 5 levels)
	if parentID != 0 {
		depth, err := s.moduleRepo.GetDepth(parentID)
		if err != nil {
			return nil, ErrInternal(CodeInternal, err)
		}
		if depth >= 5 {
			return nil, ErrBadRequest(CodeParamsError, "module tree max depth is 5")
		}
	}

	m := &model.Module{
		ProjectID: projectID,
		ParentID:  parentID,
		Name:      name,
	}
	if err := s.moduleRepo.Create(m); err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}
	return m, nil
}

// Rename 重命名模块
func (s *ModuleService) Rename(ctx context.Context, id uint, name string) (*model.Module, error) {
	if name == "" {
		return nil, ErrBadRequest(CodeParamsError, "module name is required")
	}
	m, err := s.moduleRepo.GetByID(id)
	if err != nil {
		return nil, ErrNotFound(CodeNotFound, "module not found")
	}

	// 1. 获取旧全路径（用于级联更新用例）
	oldPath, err := s.moduleRepo.GetFullPath(id)
	if err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}

	// 2. 更新名称
	m.Name = name
	if err := s.moduleRepo.Update(m); err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}

	// 3. 获取新全路径并级联更新用例
	newPath, err := s.moduleRepo.GetFullPath(id)
	if err != nil {
		return nil, ErrInternal(CodeInternal, err)
	}

	if oldPath != newPath {
		if err := s.testCaseRepo.UpdateModulePathsByPrefix(ctx, m.ProjectID, oldPath, newPath); err != nil {
			return nil, ErrInternal(CodeInternal, err)
		}
	}

	return m, nil
}

// Move 移动模块
func (s *ModuleService) Move(ctx context.Context, id uint, newParentID uint, sortOrder int) error {
	m, err := s.moduleRepo.GetByID(id)
	if err != nil {
		return ErrNotFound(CodeNotFound, "module not found")
	}

	// 1. 获取旧路径
	oldPath, err := s.moduleRepo.GetFullPath(id)
	if err != nil {
		return ErrInternal(CodeInternal, err)
	}

	// 2. 深度校验
	if newParentID != 0 {
		depth, err := s.moduleRepo.GetDepth(newParentID)
		if err != nil {
			return ErrInternal(CodeInternal, err)
		}
		if depth >= 5 {
			return ErrBadRequest(CodeParamsError, "module tree max depth is 5")
		}
	}

	// 3. 执行移动
	if err := s.moduleRepo.MoveModule(id, newParentID, sortOrder); err != nil {
		return ErrInternal(CodeInternal, err)
	}

	// 4. 获取新路径并级联更新
	newPath, err := s.moduleRepo.GetFullPath(id)
	if err != nil {
		return ErrInternal(CodeInternal, err)
	}

	if oldPath != newPath {
		if err := s.testCaseRepo.UpdateModulePathsByPrefix(ctx, m.ProjectID, oldPath, newPath); err != nil {
			return ErrInternal(CodeInternal, err)
		}
	}
	return nil
}

// Delete 删除模块（仅空目录可删）
func (s *ModuleService) Delete(ctx context.Context, id uint) error {
	// 校验子模块
	children, err := s.moduleRepo.CountChildren(id)
	if err != nil {
		return ErrInternal(CodeInternal, err)
	}
	if children > 0 {
		return ErrBadRequest(CodeParamsError, "cannot delete module with sub-modules")
	}

	// 校验用例
	count, err := s.testCaseRepo.CountByModule(ctx, id)
	if err != nil {
		return ErrInternal(CodeInternal, err)
	}
	if count > 0 {
		return ErrBadRequest(CodeParamsError, "cannot delete module with test cases")
	}

	return s.moduleRepo.Delete(id)
}

// ListFlat 返回平铺列表
func (s *ModuleService) ListFlat(projectID uint) ([]model.Module, error) {
	return s.moduleRepo.ListByProject(projectID)
}

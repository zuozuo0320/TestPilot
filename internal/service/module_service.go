// module_service.go — 用例目录模块业务逻辑
package service

import (
	"testpilot/internal/model"
	"testpilot/internal/repository"
)

// ModuleService 目录管理服务
type ModuleService struct {
	moduleRepo *repository.ModuleRepo
}

// NewModuleService 创建目录服务
func NewModuleService(repo *repository.ModuleRepo) *ModuleService {
	return &ModuleService{moduleRepo: repo}
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

// GetTree 获取项目的模块树（含用例计数）
func (s *ModuleService) GetTree(projectID uint) ([]*ModuleTreeNode, error) {
	modules, err := s.moduleRepo.ListByProject(projectID)
	if err != nil {
		return nil, ErrInternal("DB_ERROR", err)
	}
	counts, err := s.moduleRepo.CountCasesByModuleIDs(projectID)
	if err != nil {
		return nil, ErrInternal("DB_ERROR", err)
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
	return roots, nil
}

// Create 创建模块
func (s *ModuleService) Create(projectID uint, parentID uint, name string) (*model.Module, error) {
	if name == "" {
		return nil, ErrBadRequest("MISSING_NAME", "module name is required")
	}

	// Check depth limit (max 5 levels)
	if parentID != 0 {
		depth, err := s.moduleRepo.GetDepth(parentID)
		if err != nil {
			return nil, ErrInternal("DB_ERROR", err)
		}
		if depth >= 5 {
			return nil, ErrBadRequest("MAX_DEPTH", "module tree max depth is 5")
		}
	}

	m := &model.Module{
		ProjectID: projectID,
		ParentID:  parentID,
		Name:      name,
	}
	if err := s.moduleRepo.Create(m); err != nil {
		return nil, ErrInternal("DB_ERROR", err)
	}
	return m, nil
}

// Rename 重命名模块
func (s *ModuleService) Rename(id uint, name string) (*model.Module, error) {
	if name == "" {
		return nil, ErrBadRequest("MISSING_NAME", "module name is required")
	}
	m, err := s.moduleRepo.GetByID(id)
	if err != nil {
		return nil, ErrNotFound("MODULE_NOT_FOUND", "module not found")
	}
	m.Name = name
	if err := s.moduleRepo.Update(m); err != nil {
		return nil, ErrInternal("DB_ERROR", err)
	}
	return m, nil
}

// Move 移动模块
func (s *ModuleService) Move(id uint, newParentID uint, sortOrder int) error {
	// Check depth
	if newParentID != 0 {
		depth, err := s.moduleRepo.GetDepth(newParentID)
		if err != nil {
			return ErrInternal("DB_ERROR", err)
		}
		if depth >= 5 {
			return ErrBadRequest("MAX_DEPTH", "module tree max depth is 5")
		}
	}
	return s.moduleRepo.MoveModule(id, newParentID, sortOrder)
}

// Delete 删除模块（仅叶子节点可删）
func (s *ModuleService) Delete(id uint) error {
	children, err := s.moduleRepo.CountChildren(id)
	if err != nil {
		return ErrInternal("DB_ERROR", err)
	}
	if children > 0 {
		return ErrBadRequest("HAS_CHILDREN", "cannot delete module with children, delete children first")
	}
	return s.moduleRepo.Delete(id)
}

// ListFlat 返回平铺列表
func (s *ModuleService) ListFlat(projectID uint) ([]model.Module, error) {
	return s.moduleRepo.ListByProject(projectID)
}

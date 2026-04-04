package repository

import (
	"testpilot/internal/model"

	"gorm.io/gorm"
)

type ModuleRepo struct {
	db *gorm.DB
}

func NewModuleRepo(db *gorm.DB) *ModuleRepo {
	return &ModuleRepo{db: db}
}

// ListByProject returns all modules for a project, ordered by sort_order.
func (r *ModuleRepo) ListByProject(projectID uint) ([]model.Module, error) {
	var modules []model.Module
	err := r.db.Where("project_id = ?", projectID).Order("sort_order ASC, id ASC").Find(&modules).Error
	return modules, err
}

// Create inserts a new module.
func (r *ModuleRepo) Create(m *model.Module) error {
	return r.db.Create(m).Error
}

// GetByID fetches a single module.
func (r *ModuleRepo) GetByID(id uint) (*model.Module, error) {
	var m model.Module
	err := r.db.First(&m, id).Error
	return &m, err
}

// Update saves changes to a module.
func (r *ModuleRepo) Update(m *model.Module) error {
	return r.db.Save(m).Error
}

// Delete removes a module by ID.
func (r *ModuleRepo) Delete(id uint) error {
	return r.db.Delete(&model.Module{}, id).Error
}

// CountChildren returns the number of direct child modules.
func (r *ModuleRepo) CountChildren(id uint) (int64, error) {
	var count int64
	err := r.db.Model(&model.Module{}).Where("parent_id = ?", id).Count(&count).Error
	return count, err
}

// CountCasesByModuleIDs returns case counts grouped by module_id for a project.
func (r *ModuleRepo) CountCasesByModuleIDs(projectID uint) (map[uint]int64, error) {
	type result struct {
		ModuleID uint  `json:"module_id"`
		Count    int64 `json:"count"`
	}
	var results []result
	err := r.db.Model(&model.TestCase{}).
		Select("module_id, COUNT(*) as count").
		Where("project_id = ?", projectID).
		Group("module_id").
		Scan(&results).Error
	if err != nil {
		return nil, err
	}
	m := make(map[uint]int64, len(results))
	for _, r := range results {
		m[r.ModuleID] = r.Count
	}
	return m, nil
}

// GetDepth returns the depth of a module (1 = root child, max 5).
func (r *ModuleRepo) GetDepth(moduleID uint) (int, error) {
	depth := 0
	currentID := moduleID
	for currentID != 0 && depth < 10 {
		var m model.Module
		if err := r.db.Select("id, parent_id").First(&m, currentID).Error; err != nil {
			return depth, err
		}
		depth++
		currentID = m.ParentID
	}
	return depth, nil
}

// MoveModule updates parent_id and sort_order for a module.
func (r *ModuleRepo) MoveModule(id uint, newParentID uint, sortOrder int) error {
	return r.db.Model(&model.Module{}).Where("id = ?", id).
		Updates(map[string]interface{}{"parent_id": newParentID, "sort_order": sortOrder}).Error
}

// GetFullPath returns the slash-separated path for a module (e.g. "/Level1/Level2").
func (r *ModuleRepo) GetFullPath(id uint) (string, error) {
	if id == 0 {
		return "", nil
	}
	var path []string
	currentID := id
	for currentID != 0 {
		var m model.Module
		if err := r.db.Select("id, parent_id, name").First(&m, currentID).Error; err != nil {
			return "", err
		}
		path = append([]string{m.Name}, path...)
		currentID = m.ParentID
	}
	return "/" + joinStrings(path, "/"), nil
}

func joinStrings(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	res := parts[0]
	for i := 1; i < len(parts); i++ {
		res += sep + parts[i]
	}
	return res
}

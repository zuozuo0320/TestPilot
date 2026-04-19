// request.go — 所有 HTTP 请求/响应结构体定义（含 binding 校验标签）
package api

// createUserRequest 创建用户请求
type createUserRequest struct {
	Name       string `json:"name" binding:"required,min=2,max=80"`
	Email      string `json:"email" binding:"required,email,max=120"`
	Phone      string `json:"phone" binding:"omitempty,min=5,max=30"`
	Password   string `json:"password" binding:"required,min=8,max=128"`
	Role       string `json:"role" binding:"omitempty"`
	RoleIDs    []uint `json:"role_ids" binding:"required,min=1"`
	ProjectIDs []uint `json:"project_ids" binding:"required,min=1"`
}

// updateUserRequest 更新用户请求（字段可选，邮箱不可改）
type updateUserRequest struct {
	Name       *string `json:"name" binding:"omitempty,min=2,max=80"`
	Phone      *string `json:"phone" binding:"omitempty,max=30"`
	Avatar     *string `json:"avatar" binding:"omitempty,max=500"`
	Active     *bool   `json:"active"`
	RoleIDs    []uint  `json:"role_ids"`
	ProjectIDs []uint  `json:"project_ids"`
}

// createRoleRequest 创建角色请求
type createRoleRequest struct {
	Name        string `json:"name" binding:"required,min=2,max=80"`
	DisplayName string `json:"display_name" binding:"max=80"`
	Description string `json:"description" binding:"max=500"`
}

// updateRoleRequest 更新角色请求（字段可选）
type updateRoleRequest struct {
	Name        *string `json:"name" binding:"omitempty,min=2,max=80"`
	DisplayName *string `json:"display_name" binding:"omitempty,max=80"`
	Description *string `json:"description" binding:"omitempty,max=500"`
}

// resetPasswordRequest 管理员重置密码请求
type resetPasswordRequest struct {
	NewPassword string `json:"new_password" binding:"required,min=8,max=128"`
}

// changePasswordRequest 用户修改自身密码请求
type changePasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required,min=8,max=128"`
}

// updateProjectRequest 更新项目请求
type updateProjectRequest struct {
	Name        *string `json:"name" binding:"omitempty,min=2,max=120"`
	Description *string `json:"description" binding:"omitempty,max=500"`
	Avatar      *string `json:"avatar" binding:"omitempty,max=500"`
	OwnerID     *uint   `json:"owner_id" binding:"omitempty,min=1"`
}

// updateProfileRequest 个人资料更新请求
type updateProfileRequest struct {
	Name   *string `json:"name" binding:"omitempty,min=2,max=80"`
	Email  *string `json:"email" binding:"omitempty,email"`
	Phone  *string `json:"phone" binding:"omitempty,max=30"`
	Avatar *string `json:"avatar" binding:"omitempty,max=500"`
}

// assignUserRolesRequest 分配用户角色请求
type assignUserRolesRequest struct {
	RoleIDs []uint `json:"role_ids" binding:"required,min=1"`
}

// assignUserProjectsRequest 分配用户项目请求
type assignUserProjectsRequest struct {
	ProjectIDs []uint `json:"project_ids" binding:"required,min=1"`
}

// createProjectRequest 创建项目请求
type createProjectRequest struct {
	Name        string `json:"name" binding:"required,min=2,max=120"`
	Description string `json:"description" binding:"max=500"`
	Avatar      string `json:"avatar" binding:"max=500"`
	OwnerID     *uint  `json:"owner_id" binding:"omitempty,min=1"`
}

// addMemberRequest 添加项目成员请求
type addMemberRequest struct {
	UserID uint   `json:"user_id" binding:"required,min=1"`
	Role   string `json:"role" binding:"required,oneof=owner member"`
}

// createRequirementRequest 创建需求请求
type createRequirementRequest struct {
	Title   string `json:"title" binding:"required,min=1,max=200"`
	Content string `json:"content"`
}

// createTestCaseRequest 创建用例请求
type createTestCaseRequest struct {
	Title        string `json:"title" binding:"required,min=1,max=200"`
	Level        string `json:"level" binding:"omitempty,oneof=P0 P1 P2 P3"`
	ExecResult   string `json:"exec_result"`
	ModuleID     uint   `json:"module_id"`
	ModulePath   string `json:"module_path" binding:"omitempty,max=255"`
	Tags         string `json:"tags" binding:"omitempty,max=500"`
	TagIDs       []uint `json:"tag_ids" binding:"omitempty,max=10"`
	Precondition string `json:"precondition"`
	Steps        string `json:"steps"`
	Remark       string `json:"remark"`
	Priority     string `json:"priority" binding:"omitempty,oneof=high medium low"`
}

// updateTestCaseRequest 更新用例请求（字段可选）
type updateTestCaseRequest struct {
	Title        *string `json:"title" binding:"omitempty,min=1,max=200"`
	Level        *string `json:"level" binding:"omitempty,oneof=P0 P1 P2 P3"`
	ExecResult   *string `json:"exec_result"`
	ModuleID     *uint   `json:"module_id"`
	ModulePath   *string `json:"module_path" binding:"omitempty,max=255"`
	Tags         *string `json:"tags" binding:"omitempty,max=500"`
	TagIDs       []uint  `json:"tag_ids" binding:"omitempty,max=10"`
	Precondition *string `json:"precondition"`
	Steps        *string `json:"steps"`
	Remark       *string `json:"remark"`
	Priority     *string `json:"priority" binding:"omitempty,oneof=high medium low"`
}

// batchDeleteRequest 批量删除请求
type batchDeleteRequest struct {
	IDs []uint `json:"ids" binding:"required,min=1"`
}

// batchUpdateLevelRequest 批量修改等级请求
type batchUpdateLevelRequest struct {
	IDs   []uint `json:"ids" binding:"required,min=1"`
	Level string `json:"level" binding:"required,oneof=P0 P1 P2 P3"`
}

// batchMoveRequest 批量移动请求
type batchMoveRequest struct {
	IDs        []uint `json:"ids" binding:"required,min=1"`
	ModuleID   uint   `json:"module_id"`
	ModulePath string `json:"module_path"`
}

// batchTagRequest 批量打标签请求
type batchTagRequest struct {
	IDs    []uint `json:"ids" binding:"required,min=1"`
	TagIDs []uint `json:"tag_ids" binding:"required,min=1,max=10"`
}

// createRelationRequest 创建用例关联
type createRelationRequest struct {
	TargetCaseID uint   `json:"target_case_id" binding:"required,min=1"`
	RelationType string `json:"relation_type" binding:"required,oneof=precondition related"`
}

// createScriptRequest 创建脚本请求
type createScriptRequest struct {
	Name string `json:"name" binding:"required,min=1,max=200"`
	Path string `json:"path" binding:"required,min=1,max=255"`
	Type string `json:"type" binding:"required,oneof=cypress playwright selenium"`
}

// createRunRequest 创建执行请求
type createRunRequest struct {
	Mode      string `json:"mode" binding:"required,oneof=one batch"`
	ScriptID  uint   `json:"script_id"`
	ScriptIDs []uint `json:"script_ids"`
}

// createDefectRequest 创建缺陷请求
type createDefectRequest struct {
	RunResultID uint   `json:"run_result_id" binding:"required,min=1"`
	Title       string `json:"title" binding:"required,min=1,max=200"`
	Description string `json:"description"`
	Severity    string `json:"severity" binding:"omitempty,oneof=critical high medium low"`
}

// loginRequest 登录请求
type loginRequest struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// refreshTokenRequest 刷新 Token 请求
type refreshTokenRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

// createTagRequest 创建标签请求
type createTagRequest struct {
	Name        string `json:"name" binding:"required,min=2,max=50"`
	Color       string `json:"color" binding:"required,len=7"`
	Description string `json:"description" binding:"omitempty,max=200"`
}

// updateTagRequest 更新标签请求（字段可选）
type updateTagRequest struct {
	Name        *string `json:"name" binding:"omitempty,min=2,max=50"`
	Color       *string `json:"color" binding:"omitempty,len=7"`
	Description *string `json:"description" binding:"omitempty,max=200"`
}

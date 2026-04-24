// errors.go — 业务错误类型定义
// BizError 统一携带 6 位数字业务错误码，Handler 层根据 Status 映射 HTTP 状态码
package service

import "fmt"

// BizError 业务错误，携带 HTTP 状态码和 6 位数字业务码
type BizError struct {
	Status  int    // HTTP 状态码（200/400/401/403/404/500）
	Code    int    // 6 位数字业务错误码（如 100101）
	Message string // 用户可见消息
}

func (e *BizError) Error() string {
	return fmt.Sprintf("[%d] %s", e.Code, e.Message)
}

// NumericCode 返回 6 位数字业务码（供 response.HandleError 使用）
func (e *BizError) NumericCode() int {
	return e.Code
}

// ========== 构造函数 ==========

// ErrBadRequest 400 参数错误
func ErrBadRequest(code int, message string) *BizError {
	return &BizError{Status: 400, Code: code, Message: message}
}

// ErrUnauthorized 401 未认证
func ErrUnauthorized(code int, message string) *BizError {
	return &BizError{Status: 401, Code: code, Message: message}
}

// ErrForbidden 403 权限不足
func ErrForbidden(code int, message string) *BizError {
	return &BizError{Status: 403, Code: code, Message: message}
}

// ErrNotFound 404 资源不存在
func ErrNotFound(code int, message string) *BizError {
	return &BizError{Status: 404, Code: code, Message: message}
}

// ErrConflict 409 资源冲突
func ErrConflict(code int, message string) *BizError {
	return &BizError{Status: 409, Code: code, Message: message}
}

// ErrInternal 500 内部错误（不暴露细节给客户端）
func ErrInternal(code int, err error) *BizError {
	_ = err // TODO: 接入结构化日志后记录原始错误
	return &BizError{Status: 500, Code: code, Message: "internal server error"}
}

// ========== 统一 6 位数字错误码定义 ==========
// 格式: [服务2位][模块2位][序号2位]
// 通用码: 2xxxxx=成功, 4xxxxx=客户端错误, 5xxxxx=服务端错误

const (
	CodeSuccess      = 200000 // 成功
	CodeParamsError  = 400000 // 通用参数错误
	CodeUnauthorized = 401000 // 未认证
	CodeForbidden    = 403000 // 无权限
	CodeNotFound     = 404000 // 资源不存在
	CodeConflict     = 409000 // 资源冲突
	CodeInternal     = 500000 // 内部错误

	// 10: 用户/认证模块
	CodeInvalidCredentials = 100001
	CodeUserFrozen         = 100002
	CodeUserDisabled       = 100003
	CodeInsufficientRole   = 100004
	CodeEmailExists        = 100005
	CodePhoneExists        = 100006
	CodeUserNotFound       = 100007
	CodeRoleNotFound       = 100008
	CodePasswordTooWeak    = 100009
	CodeOldPasswordWrong   = 100010
	CodeEmailImmutable     = 100011

	// 11: 项目模块
	CodeProjectNotFound      = 110001
	CodeNoProjectAccess      = 110002
	CodeProjectArchived      = 110003
	CodeProjectNotArchived   = 110004
	CodeProjectNotEmpty      = 110005
	CodeSeedProjectProtected = 110006
	CodeAdminProtected       = 110007
	CodePresetRoleProtected  = 110008
	CodeRoleInUse            = 110009
	CodeTestCaseNotFound     = 110010
	CodeProjectOwnerInvalid  = 110011
	CodeProjectOwnerLocked   = 110012
	CodeProjectOwnerByMember = 110013

	// 12: 用例评审模块
	CodeReviewNotFound      = 120101
	CodeReviewStatusInvalid = 120102
	CodeReviewOwnerInvalid  = 120103
	CodeReviewItemNotFound  = 120104
	CodeReviewForbidden     = 120105
	CodeReviewMissingName   = 120106
	CodeReviewEmptyReviewer = 120107
	CodeReviewItemMismatch  = 120108

	// 12-v0.2: 用例评审 v0.2 角色 / AI 门禁 / 缺陷
	CodeReviewSelfReviewForbidden = 120201 // 禁止自审 Author 不能评审自己的用例
	CodeReviewPrimaryRequired     = 120202 // 必须指定唯一 Primary
	CodeReviewPrimaryDuplicate    = 120203 // 同一评审项存在多个 Primary
	CodeReviewRoleInvalid         = 120204 // review_role 枚举非法
	CodeReviewSeverityInvalid     = 120205 // severity 枚举非法
	CodeReviewModeratorInvalid    = 120206 // Moderator 不在项目成员列表或不存在
	CodeReviewAIGateStatusInvalid = 120207 // ai_gate_status 非法
	CodeReviewDefectNotFound      = 120208 // Action Item 不存在
	CodeReviewDefectStatusInvalid = 120209 // Action Item 状态不允许本次变更
	CodeReviewCriticalOpen        = 120210 // 存在未 resolve 的 critical 缺陷，禁止重提
	CodeReviewRerunForbidden      = 120211 // 评审项当前状态不允许触发 AI 门禁 rerun
	CodeReviewFeedbackInvalid     = 120212 // AI feedback 格式非法

	// 13: 标签管理模块
	CodeTagNotFound          = 130001
	CodeTagNameDuplicate     = 130002
	CodeTagLimitExceeded     = 130003
	CodeTagColorInvalid      = 130004
	CodeTagCaseLimitExceeded = 130005
)

// ========== 预定义错误（向后兼容） ==========

var (
	ErrInvalidCredentials   = ErrUnauthorized(CodeInvalidCredentials, "invalid credentials")
	ErrUserFrozen           = ErrForbidden(CodeUserFrozen, "user is frozen")
	ErrUserDisabled         = ErrForbidden(CodeUserDisabled, "账号已被禁用")
	ErrInsufficientRole     = ErrForbidden(CodeInsufficientRole, "insufficient role")
	ErrEmailExists          = ErrConflict(CodeEmailExists, "email already exists")
	ErrPhoneExists          = ErrConflict(CodePhoneExists, "phone already exists")
	ErrUserNotFound         = ErrNotFound(CodeUserNotFound, "user not found")
	ErrRoleNotFound         = ErrNotFound(CodeRoleNotFound, "role not found")
	ErrProjectNotFound      = ErrNotFound(CodeProjectNotFound, "project not found")
	ErrTestCaseNotFound     = ErrNotFound(CodeTestCaseNotFound, "testcase not found")
	ErrAdminCannotBeDeleted = ErrConflict(CodeAdminProtected, "admin user cannot be deleted")
	ErrPresetRoleProtected  = ErrConflict(CodePresetRoleProtected, "preset system role cannot be deleted")
	ErrRoleInUse            = ErrConflict(CodeRoleInUse, "role is in use")
	ErrNoProjectAccess      = ErrForbidden(CodeNoProjectAccess, "no project access")
	ErrProjectArchived      = ErrConflict(CodeProjectArchived, "项目已归档，不可操作")
	ErrProjectNotArchived   = ErrConflict(CodeProjectNotArchived, "项目未归档，无需恢复")
	ErrProjectNotEmpty      = ErrConflict(CodeProjectNotEmpty, "项目下仍有数据，无法删除")
	ErrSeedProjectProtected = ErrConflict(CodeSeedProjectProtected, "种子项目「快速开始」不允许删除或归档")
	ErrPasswordTooWeak      = ErrBadRequest(CodePasswordTooWeak, "密码至少8位，须包含大写字母、小写字母和数字")
	ErrOldPasswordWrong     = ErrBadRequest(CodeOldPasswordWrong, "旧密码不正确")
	ErrEmailImmutable       = ErrBadRequest(CodeEmailImmutable, "邮箱不可修改")
)

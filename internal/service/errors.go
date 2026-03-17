// errors.go — 业务错误类型定义，Service 层统一返回 BizError，Handler 层根据 Code 映射 HTTP 状态码
package service

import "fmt"

// BizError 业务错误，携带 HTTP 状态码和业务消息
type BizError struct {
	Status  int    // HTTP 状态码
	Code    string // 业务错误码
	Message string // 用户可见消息
}

func (e *BizError) Error() string {
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// ========== 构造函数 ==========

// ErrBadRequest 400 参数错误
func ErrBadRequest(code, message string) *BizError {
	return &BizError{Status: 400, Code: code, Message: message}
}

// ErrUnauthorized 401 未认证
func ErrUnauthorized(code, message string) *BizError {
	return &BizError{Status: 401, Code: code, Message: message}
}

// ErrForbidden 403 权限不足
func ErrForbidden(code, message string) *BizError {
	return &BizError{Status: 403, Code: code, Message: message}
}

// ErrNotFound 404 资源不存在
func ErrNotFound(code, message string) *BizError {
	return &BizError{Status: 404, Code: code, Message: message}
}

// ErrConflict 409 资源冲突
func ErrConflict(code, message string) *BizError {
	return &BizError{Status: 409, Code: code, Message: message}
}

// ErrInternal 500 内部错误（不暴露细节）
func ErrInternal(code string, err error) *BizError {
	return &BizError{Status: 500, Code: code, Message: err.Error()}
}

// ========== 预定义错误 ==========

var (
	ErrInvalidCredentials   = ErrUnauthorized("INVALID_CREDENTIALS", "invalid credentials")
	ErrUserFrozen           = ErrForbidden("USER_FROZEN", "user is frozen")
	ErrInsufficientRole     = ErrForbidden("INSUFFICIENT_ROLE", "insufficient role")
	ErrEmailExists          = ErrConflict("EMAIL_EXISTS", "email already exists")
	ErrPhoneExists          = ErrConflict("PHONE_EXISTS", "phone already exists")
	ErrUserNotFound         = ErrNotFound("USER_NOT_FOUND", "user not found")
	ErrRoleNotFound         = ErrNotFound("ROLE_NOT_FOUND", "role not found")
	ErrProjectNotFound      = ErrNotFound("PROJECT_NOT_FOUND", "project not found")
	ErrTestCaseNotFound     = ErrNotFound("TESTCASE_NOT_FOUND", "testcase not found")
	ErrAdminCannotBeDeleted = ErrConflict("ADMIN_PROTECTED", "admin user cannot be deleted")
	ErrPresetRoleProtected  = ErrConflict("PRESET_ROLE_PROTECTED", "preset system role cannot be deleted")
	ErrRoleInUse            = ErrConflict("ROLE_IN_USE", "role is in use")
	ErrNoProjectAccess      = ErrForbidden("NO_PROJECT_ACCESS", "no project access")
)

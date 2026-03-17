// helpers.go — Service 层共用的校验和工具函数
package service

import (
	"regexp"
	"strings"

	"testpilot/internal/model"
)

var phonePattern = regexp.MustCompile(`^1\d{10}$`)

// isValidPersonName 校验姓名
func isValidPersonName(name string) bool {
	name = strings.TrimSpace(name)
	if len(name) < 2 || len(name) > 40 {
		return false
	}
	for _, r := range name {
		if r == ' ' || r == '·' || r == '-' || r == '_' {
			continue
		}
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= 0x4e00 && r <= 0x9fa5) {
			continue
		}
		return false
	}
	return true
}

// isValidEmail 校验邮箱
func isValidEmail(email string) bool {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" || len(email) > 120 || strings.Count(email, "@") != 1 {
		return false
	}
	parts := strings.Split(email, "@")
	local, domain := parts[0], parts[1]
	if len(local) < 1 || len(domain) < 3 || !strings.Contains(domain, ".") {
		return false
	}
	return !strings.HasPrefix(domain, ".") && !strings.HasSuffix(domain, ".")
}

// isValidPhone 校验手机号
func isValidPhone(phone string) bool {
	return phonePattern.MatchString(strings.TrimSpace(phone))
}

// isDuplicateError 判断是否为唯一索引冲突
func isDuplicateError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "duplicate") || strings.Contains(text, "unique")
}

// uniqueUint 去重去零
func uniqueUint(values []uint) []uint {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[uint]struct{}, len(values))
	result := make([]uint, 0, len(values))
	for _, v := range values {
		if v == 0 {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		result = append(result, v)
	}
	return result
}

// containsRoleName 判断角色列表中是否包含指定角色名
func containsRoleName(roles []model.Role, roleName string) bool {
	for _, item := range roles {
		if strings.EqualFold(strings.TrimSpace(item.Name), strings.TrimSpace(roleName)) {
			return true
		}
	}
	return false
}

// isValidSeverity 校验缺陷严重程度
func isValidSeverity(severity string) bool {
	switch severity {
	case "low", "medium", "high", "critical":
		return true
	default:
		return false
	}
}

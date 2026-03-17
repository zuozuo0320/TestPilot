// password.go — 密码哈希与验证（bcrypt）
package auth

import (
	"golang.org/x/crypto/bcrypt"
)

const defaultCost = bcrypt.DefaultCost // 10

// HashPassword 将明文密码哈希为 bcrypt 字符串
func HashPassword(plaintext string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(plaintext), defaultCost)
	return string(bytes), err
}

// CheckPassword 比对明文密码与 bcrypt 哈希
func CheckPassword(plaintext, hashed string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hashed), []byte(plaintext)) == nil
}

// Package credentials centralizes username and password rules shared by the
// HTTP API, configuration loading, and startup bootstrap.
package credentials

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

const (
	// BcryptCost is the work factor used for every stored password hash.
	BcryptCost = 12
	// MinPasswordRunes mirrors the public password policy.
	MinPasswordRunes = 10
	// MaxPasswordBytes is the bcrypt input limit.
	MaxPasswordBytes = 72
)

// NormalizeUsername trims and validates a username, returning a field-level
// message suitable for the API error payload.
func NormalizeUsername(raw string) (string, string) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", "请填写用户名。"
	}
	if strings.ContainsRune(value, '\x00') {
		return "", "用户名包含非法字符。"
	}
	if utf8.RuneCountInString(value) > 150 {
		return "", "用户名不能超过 150 个字符。"
	}
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("@.+-_", r) {
			continue
		}
		return "", "用户名只能包含字母、数字和 @ . + - _ 字符。"
	}
	return value, ""
}

// PasswordValidationError returns a field-level message for an unacceptable
// password, or "" when the password follows the public policy.
func PasswordValidationError(password string) string {
	if utf8.RuneCountInString(password) < MinPasswordRunes {
		return "密码至少需要 10 个字符。"
	}
	if len(password) > MaxPasswordBytes {
		return "密码不能超过 72 个字节。"
	}
	return ""
}

// HashPassword applies the shared bcrypt work factor. Callers must validate
// the password first; the returned string is safe to persist but never log.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), BcryptCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// PasswordMatches compares a plaintext password with a stored bcrypt hash.
func PasswordMatches(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

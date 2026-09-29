package auth

// 登录凭证缝（openspec/changes/login-credential-seam，ADR-0020）：邮箱+密码。
// 镜像运营端密码模式（ADR-0010），但绑定不换 ID——凭证只是匿名身份的可选
// 升级凭据，不是新的身份主体。

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

var (
	// ErrCredentialExists 表示 identifier 已被绑定（Login 据此切到验证路径）。
	ErrCredentialExists = errors.New("credential already exists")
	ErrInvalidPassword  = errors.New("invalid password")
	ErrInvalidLogin     = errors.New("invalid identifier or password")
	// ErrInvalidLoginFields 表单形状非法（缺 @/长度越界）——HTTP 语义 400，
	// 与密码错误的 401 分开。
	ErrInvalidLoginFields = errors.New("malformed identifier or password")
)

// argon2id 参数：RFC 9106 第二推荐档的低交互折中（64MiB 对小型部署偏重，
// 取 32MiB/t=2/p=1）。参数编码进 PHC 串，将来可无损调参。
const (
	argon2Memory  uint32 = 32 * 1024
	argon2Time    uint32 = 2
	argon2Threads uint8  = 1
	argon2KeyLen  uint32 = 32
	argon2SaltLen        = 16
)

// HashPassword 生成 argon2id PHC 串：$argon2id$v=19$m=...,t=...,p=...$salt$hash。
func HashPassword(password string) (string, error) {
	salt := make([]byte, argon2SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("password salt: %w", err)
	}
	digest := argon2.IDKey([]byte(password), salt, argon2Time, argon2Memory, argon2Threads, argon2KeyLen)
	encoded := fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argon2Memory, argon2Time, argon2Threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(digest))
	return encoded, nil
}

// VerifyPassword 恒定时间校验密码与 PHC 串。串里携带的参数优先——哈希可
// 无损升级。
func VerifyPassword(password, encoded string) bool {
	parts := strings.Split(strings.TrimSpace(encoded), "$")
	// 空首段：$argon2id$v=...$salt$hash → ["", "argon2id", "v=19", "m=...", "salt", "hash"]
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false
	}
	var memory, timeCost uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &timeCost, &threads); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}
	digest := argon2.IDKey([]byte(password), salt, timeCost, memory, threads, uint32(len(expected)))
	return subtle.ConstantTimeCompare(digest, expected) == 1
}

// NormalizeIdentifier 归一登录标识：trim + 小写（邮箱域大小写不敏感）。
func NormalizeIdentifier(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// minPasswordLength 是登录密码下限（ADR-0020 修订：8→7，容纳运营指定的
// 演示账号 test/5055365；上限 128 不变）。
const minPasswordLength = 7

// validUsernameIdentifier 报告值是否为合法用户名形标识：3–32 位字母数字
// （ADR-0020 修订：identifier 放宽为「邮箱或用户名」双形态——客户端演示
// 账号 test 按字面登录）。
func validUsernameIdentifier(value string) bool {
	if len(value) < 3 || len(value) > 32 {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '_' || character == '.' {
			continue
		}
		return false
	}
	return true
}

// ValidateLoginRequest 校验登录表单：identifier 双形态——邮箱形状（含 @、
// 长度 254）或用户名形（3–32 位字母数字），密码 7–128 字符。不发信、不
// 验证归属（ADR-0020 决定 4 + 2026-09 修订）。
func ValidateLoginRequest(identifier, password string) error {
	identifier = NormalizeIdentifier(identifier)
	if identifier == "" || len(identifier) > 254 {
		return ErrInvalidLoginFields
	}
	if !strings.Contains(identifier, "@") {
		if !validUsernameIdentifier(identifier) {
			return ErrInvalidLoginFields
		}
	} else {
		if at := strings.LastIndex(identifier, "@"); at <= 0 || at == len(identifier)-1 {
			return ErrInvalidLoginFields
		}
	}
	if len(password) < minPasswordLength || len(password) > 128 {
		return ErrInvalidLoginFields
	}
	return nil
}

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

// ValidateLoginRequest 校验登录表单：identifier 按邮箱形状（含 @、长度
// 254），密码 8–128 字符。不发信、不验证归属（ADR-0020 决定 4）。
func ValidateLoginRequest(identifier, password string) error {
	identifier = NormalizeIdentifier(identifier)
if identifier == "" || len(identifier) > 254 || !strings.Contains(identifier, "@") {
		return ErrInvalidLoginFields
	}
	if at := strings.LastIndex(identifier, "@"); at <= 0 || at == len(identifier)-1 {
		return ErrInvalidLoginFields
	}
	if len(password) < 8 || len(password) > 128 {
		return ErrInvalidLoginFields
	}
	return nil
}

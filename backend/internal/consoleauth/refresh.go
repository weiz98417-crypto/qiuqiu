package consoleauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"time"
)

// RefreshTokenTTL is the 30-day refresh lifetime (locked decision 3).
const RefreshTokenTTL = 30 * 24 * time.Hour

// NewRefreshToken mints a 30-day refresh token: 32 random bytes, base64url.
// Only HashRefreshToken(token) is ever stored.
func NewRefreshToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// HashRefreshToken is the only representation the store keeps (SHA-256 hex,
// same discipline as personal tokens).
func HashRefreshToken(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

// RefreshExpiryAt returns the absolute expiry for a refresh issued now.
func RefreshExpiryAt(now time.Time) time.Time { return now.Add(RefreshTokenTTL) }

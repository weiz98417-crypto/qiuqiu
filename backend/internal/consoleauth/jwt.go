package consoleauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Minimal hand-rolled HS256 JWT (ADR-0010 locked decision 2): the header is
// always {"alg":"HS256","typ":"JWT"} and Verify rejects any other alg BEFORE
// the signature check — the algorithm-confusion class is structurally
// absent. Scope: exactly the console claims shape; no other feature.

const (
	// AccessTokenTTL is the 15-minute access token lifetime; revocation
	// latency is bounded by it (refresh revocation kills the session within
	// 15 minutes).
	AccessTokenTTL = 15 * time.Minute
	jwtHeader      = `{"alg":"HS256","typ":"JWT"}`
)

var (
	ErrJWTSecretRequired = errors.New("QIUQIU_JWT_SECRET is required when any password account exists")
	ErrJWTMalformed      = errors.New("token is not a valid JWT")
	ErrJWTBadAlgorithm   = errors.New("token algorithm is not HS256")
	ErrJWTBadSignature   = errors.New("token signature is invalid")
	ErrJWTExpired        = errors.New("token is expired")
)

// Claims is the console JWT payload (design.md routes section): operator
// name, role, scopes and the standard timestamps.
type Claims struct {
	Sub         string   `json:"sub"`
	Role        string   `json:"role"`
	Scopes      []string `json:"scopes"`
	Exp         int64    `json:"exp"`
	Iat         int64    `json:"iat"`
	PasswordSet bool     `json:"passwordSet"`
}

func base64URL(data []byte) string { return base64.RawURLEncoding.EncodeToString(data) }

// SignJWT issues an HS256 access token for the claims. exp/iat are filled
// from now + ttl when Exp is zero.
func SignJWT(claims Claims, secret string, ttl time.Duration) (string, error) {
	if strings.TrimSpace(secret) == "" {
		return "", ErrJWTSecretRequired
	}
	now := time.Now()
	if claims.Exp == 0 {
		claims.Exp = now.Add(ttl).Unix()
	}
	if claims.Iat == 0 {
		claims.Iat = now.Unix()
	}
	header := base64URL([]byte(jwtHeader))
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("jwt payload: %w", err)
	}
	signingInput := header + "." + base64URL(payload)
	signature := signHS256(signingInput, secret)
	return signingInput + "." + base64URL(signature), nil
}

// VerifyJWT validates an HS256 token: shape, alg (rejected before the
// signature), signature (constant-time), then expiry.
func VerifyJWT(token, secret string) (Claims, error) {
	var claims Claims
	if strings.TrimSpace(secret) == "" {
		return claims, ErrJWTSecretRequired
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return claims, ErrJWTMalformed
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return claims, ErrJWTMalformed
	}
	var header struct {
		Alg string `json:"alg"`
		Typ string `json:"typ"`
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return claims, ErrJWTMalformed
	}
	if header.Alg != "HS256" {
		// SECURITY: no algorithm negotiation — anything ≠ HS256 dies here,
		// before any signature material is touched.
		return claims, ErrJWTBadAlgorithm
	}
	expected := signHS256(parts[0]+"."+parts[1], secret)
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(signature, expected) {
		return claims, ErrJWTBadSignature
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return claims, ErrJWTMalformed
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return claims, ErrJWTMalformed
	}
	if claims.Exp > 0 && time.Now().Unix() > claims.Exp {
		return claims, ErrJWTExpired
	}
	return claims, nil
}

// DecodeJWTClaims extracts the payload without verification — client-side
// display only (header operator name), never an authorization decision.
func DecodeJWTClaims(token string) (Claims, error) {
	var claims Claims
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return claims, ErrJWTMalformed
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return claims, ErrJWTMalformed
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return claims, ErrJWTMalformed
	}
	return claims, nil
}

func signHS256(signingInput, secret string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(signingInput))
	return mac.Sum(nil)
}

// LooksLikeJWT is the cheap structural check the middleware uses to route a
// bearer token to JWT verification (personal tokens never contain dots).
func LooksLikeJWT(token string) bool {
	return strings.Count(token, ".") == 2
}

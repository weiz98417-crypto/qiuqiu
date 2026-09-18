package consoleauth

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

const testSecret = "test-jwt-secret-with-some-length"

func base64URLRaw(t *testing.T, data []byte) string {
	t.Helper()
	return base64.RawURLEncoding.EncodeToString(data)
}

func mustSign(t *testing.T, claims Claims, ttl time.Duration) string {
	t.Helper()
	token, err := SignJWT(claims, testSecret, ttl)
	if err != nil {
		t.Fatalf("SignJWT error: %v", err)
	}
	return token
}

func TestJWTRoundTrip(t *testing.T) {
	token := mustSign(t, Claims{Sub: "值班导演", Role: "director", Scopes: []string{"trace_read", "operator_match_write"}}, AccessTokenTTL)
	claims, err := VerifyJWT(token, testSecret)
	if err != nil {
		t.Fatalf("VerifyJWT error: %v", err)
	}
	if claims.Sub != "值班导演" || claims.Role != "director" || len(claims.Scopes) != 2 {
		t.Fatalf("claims = %+v", claims)
	}
	if claims.Exp <= time.Now().Unix() {
		t.Fatalf("exp not in the future: %d", claims.Exp)
	}
}

func TestJWTExpiredRejected(t *testing.T) {
	token := mustSign(t, Claims{Sub: "op", Exp: time.Now().Add(-time.Minute).Unix()}, 0)
	if _, err := VerifyJWT(token, testSecret); err != ErrJWTExpired {
		t.Fatalf("error = %v, want ErrJWTExpired", err)
	}
}

func TestJWTTamperedPayloadRejected(t *testing.T) {
	token := mustSign(t, Claims{Sub: "op", Role: "auditor", Scopes: []string{"trace_read"}}, AccessTokenTTL)
	parts := strings.Split(token, ".")
	// 提权尝试：把 role 改成 director 后原签名必然失效。
	forged := base64URLRaw(t, []byte(`{"sub":"op","role":"director","scopes":["trace_read","operator_match_write"],"exp":9999999999}`))
	tampered := parts[0] + "." + forged + "." + parts[2]
	if _, err := VerifyJWT(tampered, testSecret); err == nil {
		t.Fatalf("tampered token accepted")
	}
	// 同一载荷、错误密钥的签名也必须被拒。
	otherKey := mustSign(t, Claims{Sub: "op"}, AccessTokenTTL)
	if _, err := VerifyJWT(strings.Replace(otherKey, parts[0], parts[0], 1), "another-secret-entirely"); err == nil {
		t.Fatalf("wrong-secret token accepted")
	}
}

func TestJWTWrongAlgRejected(t *testing.T) {
	// alg=none 与 alg=HS512 都必须在签名校验前被拒（locked decision 2）。
	for _, alg := range []string{"none", "HS512", "RS256"} {
		header := base64URLRaw(t, []byte(`{"alg":"`+alg+`","typ":"JWT"}`))
		payload := base64URLRaw(t, []byte(`{"sub":"op","exp":9999999999}`))
		token := header + "." + payload + "." + base64URLRaw(t, []byte("forged-signature"))
		if _, err := VerifyJWT(token, testSecret); err != ErrJWTBadAlgorithm {
			t.Fatalf("alg %s: error = %v, want ErrJWTBadAlgorithm", alg, err)
		}
	}
}

func TestJWTMalformedRejected(t *testing.T) {
	for _, token := range []string{"", "not-a-jwt", "a.b", "a.b.c.d"} {
		if _, err := VerifyJWT(token, testSecret); err == nil {
			t.Fatalf("malformed token %q accepted", token)
		}
	}
}

func TestJWTSecretRequired(t *testing.T) {
	if _, err := SignJWT(Claims{Sub: "op"}, "", AccessTokenTTL); err != ErrJWTSecretRequired {
		t.Fatalf("sign without secret error = %v, want ErrJWTSecretRequired", err)
	}
	if _, err := VerifyJWT("a.b.c", ""); err != ErrJWTSecretRequired {
		t.Fatalf("verify without secret error = %v, want ErrJWTSecretRequired", err)
	}
}

func TestDecodeJWTClaims(t *testing.T) {
	token := mustSign(t, Claims{Sub: "运营员", Role: "auditor", Scopes: []string{"trace_read"}}, AccessTokenTTL)
	claims, err := DecodeJWTClaims(token)
	if err != nil {
		t.Fatalf("DecodeJWTClaims error: %v", err)
	}
	if claims.Sub != "运营员" || claims.Role != "auditor" {
		t.Fatalf("claims = %+v", claims)
	}
}

func TestLooksLikeJWT(t *testing.T) {
	if !LooksLikeJWT("a.b.c") {
		t.Fatalf("a.b.c should look like a JWT")
	}
	if LooksLikeJWT("qiuqiu-dev-token") || LooksLikeJWT("a.b") {
		t.Fatalf("personal tokens must not look like JWTs")
	}
}

func TestRefreshTokenPrimitives(t *testing.T) {
	first, err := NewRefreshToken()
	if err != nil {
		t.Fatalf("NewRefreshToken error: %v", err)
	}
	second, err := NewRefreshToken()
	if err != nil {
		t.Fatalf("NewRefreshToken error: %v", err)
	}
	if first == second {
		t.Fatalf("refresh tokens must be unique")
	}
	if HashRefreshToken(first) == HashRefreshToken(second) {
		t.Fatalf("hashes must differ")
	}
	if HashRefreshToken(first) != HashRefreshToken(first) {
		t.Fatalf("hashing must be deterministic")
	}
	expiry := RefreshExpiryAt(time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC))
	if expiry.Sub(time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)) != 30*24*time.Hour {
		t.Fatalf("refresh ttl = %s", expiry)
	}
}

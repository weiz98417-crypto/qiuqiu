package main

// Console auth tests (ADR-0010): login → access/refresh pair, refresh
// rotation (single use), logout revocation, self-service password change,
// the force-change lifecycle, and dual-channel compat — a JWT and a personal
// token both authorize the same routes; the legacy eval env (no operator
// rows) refuses login but keeps its APP_TOKEN behavior verbatim.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"qiuqiu/internal/consoleauth"
	"qiuqiu/internal/operatorwrite"
)

const authTestSecret = "console-auth-test-secret-0123456789"

// newAuthHarness rebuilds the console handler with the JWT secret enabled
// (the shared harness constructor keeps the machine channel only).
func newAuthHarness(t *testing.T) *consoleHarness {
	t.Helper()
	harness := newConsoleHarness(t)
	harness.cfg.JWTSecret = authTestSecret
	authz := newOperatorAuthz(harness.cfg, harness.operators).withJWTSecret(harness.cfg.JWTSecret)
	harness.console = handleConsoleAPI(consoleAPI{
		cfg: harness.cfg, authz: authz, matches: harness.store, traces: harness.traces,
		sessions: harness.sessions, memories: harness.queue, operators: harness.operators,
		writes: operatorwrite.NewMemoryService(), interruptions: harness.ring, jwtSecret: harness.cfg.JWTSecret,
	})
	return harness
}

// seedPasswordOperator creates an operator with password credentials.
// setAt zero keeps the first-login force-change flag.
func seedPasswordOperator(t *testing.T, harness *consoleHarness, name, password string, setAt time.Time) {
	t.Helper()
	if _, err := harness.operators.Seed(context.Background(), name, "machine-token-"+name, "director"); err != nil {
		t.Fatalf("seed operator: %v", err)
	}
	hash, err := consoleauth.HashPassword(password)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if err := harness.operators.SetPasswordCredentials(context.Background(), name, hash, setAt); err != nil {
		t.Fatalf("set password: %v", err)
	}
}

func postJSON(t *testing.T, handler http.HandlerFunc, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	recorder := httptest.NewRecorder()
	handler(recorder, request)
	return recorder
}

func decodeBody(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response %q: %v", recorder.Body.String(), err)
	}
	return body
}

func (h *consoleHarness) login(t *testing.T, username, password string) *httptest.ResponseRecorder {
	t.Helper()
	return postJSON(t, h.console, "/api/console/auth/login",
		`{"username":`+jsonString(username)+`,"password":`+jsonString(password)+`}`)
}

func jsonString(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func TestLoginIssuesTokenPair(t *testing.T) {
	harness := newAuthHarness(t)
	seedPasswordOperator(t, harness, "阿琴", "director-pass-01", time.Now().UTC())

	recorder := harness.login(t, "阿琴", "director-pass-01")
	if recorder.Code != http.StatusOK {
		t.Fatalf("login status = %d: %s", recorder.Code, recorder.Body.String())
	}
	body := decodeBody(t, recorder)
	accessToken, _ := body["accessToken"].(string)
	refreshToken, _ := body["refreshToken"].(string)
	if accessToken == "" || refreshToken == "" {
		t.Fatalf("token pair missing: %v", body)
	}
	if _, err := consoleauth.VerifyJWT(accessToken, authTestSecret); err != nil {
		t.Fatalf("access token does not verify: %v", err)
	}
	if body["passwordChangeRequired"] != false {
		t.Fatalf("passwordChangeRequired = %v, want false", body["passwordChangeRequired"])
	}
	operator, _ := body["operator"].(map[string]any)
	if operator["name"] != "阿琴" || operator["role"] != "director" {
		t.Fatalf("operator shape = %v", operator)
	}
}

func TestLoginBadCredentialsAuditAndReject(t *testing.T) {
	harness := newAuthHarness(t)
	seedPasswordOperator(t, harness, "阿琴", "director-pass-01", time.Now().UTC())

	for _, attempt := range []struct{ username, password string }{
		{"阿琴", "wrong-password"},
		{"ghost", "whatever-pass"},
	} {
		if recorder := harness.login(t, attempt.username, attempt.password); recorder.Code != http.StatusUnauthorized {
			t.Fatalf("login(%q) status = %d, want 401", attempt.username, recorder.Code)
		}
	}
	audits, err := harness.operators.RecentAudit(context.Background(), 10)
	if err != nil {
		t.Fatalf("RecentAudit: %v", err)
	}
	failed := 0
	for _, entry := range audits {
		if entry.Action == "auth.login_failed" {
			failed++
		}
	}
	if failed != 2 {
		t.Fatalf("auth.login_failed audit rows = %d, want 2", failed)
	}
}

func TestLoginFirstLoginForceChangeFlag(t *testing.T) {
	harness := newAuthHarness(t)
	seedPasswordOperator(t, harness, "小阅", "temp-pass-123456", time.Time{}) // zero = director temp

	body := decodeBody(t, harness.login(t, "小阅", "temp-pass-123456"))
	if body["passwordChangeRequired"] != true {
		t.Fatalf("passwordChangeRequired = %v, want true for a temp password", body["passwordChangeRequired"])
	}
}

func TestRefreshRotationSingleUse(t *testing.T) {
	harness := newAuthHarness(t)
	seedPasswordOperator(t, harness, "阿琴", "director-pass-01", time.Now().UTC())
	first := decodeBody(t, harness.login(t, "阿琴", "director-pass-01"))
	original := first["refreshToken"].(string)

	recorder := postJSON(t, harness.console, "/api/console/auth/refresh",
		`{"refreshToken":`+jsonString(original)+`}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("refresh status = %d: %s", recorder.Code, recorder.Body.String())
	}
	rotated := decodeBody(t, recorder)
	if rotated["refreshToken"] == original {
		t.Fatalf("rotation must mint a new refresh token")
	}

	// 单次使用：旧刷新令牌立即失效。
	if again := postJSON(t, harness.console, "/api/console/auth/refresh",
		`{"refreshToken":`+jsonString(original)+`}`); again.Code != http.StatusUnauthorized {
		t.Fatalf("replayed refresh status = %d, want 401", again.Code)
	}
	// 新令牌继续可用。
	if next := postJSON(t, harness.console, "/api/console/auth/refresh",
		`{"refreshToken":`+jsonString(rotated["refreshToken"].(string))+`}`); next.Code != http.StatusOK {
		t.Fatalf("rotated refresh status = %d: %s", next.Code, next.Body.String())
	}
}

func TestLogoutRevokesRefresh(t *testing.T) {
	harness := newAuthHarness(t)
	seedPasswordOperator(t, harness, "阿琴", "director-pass-01", time.Now().UTC())
	pair := decodeBody(t, harness.login(t, "阿琴", "director-pass-01"))
	refreshToken := pair["refreshToken"].(string)

	if recorder := postJSON(t, harness.console, "/api/console/auth/logout",
		`{"refreshToken":`+jsonString(refreshToken)+`}`); recorder.Code != http.StatusOK {
		t.Fatalf("logout status = %d", recorder.Code)
	}
	if recorder := postJSON(t, harness.console, "/api/console/auth/refresh",
		`{"refreshToken":`+jsonString(refreshToken)+`}`); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("refresh after logout status = %d, want 401", recorder.Code)
	}
}

func TestJWTAndPersonalTokenBothAuthorize(t *testing.T) {
	harness := newAuthHarness(t)
	seedPasswordOperator(t, harness, "阿琴", "director-pass-01", time.Now().UTC())
	pair := decodeBody(t, harness.login(t, "阿琴", "director-pass-01"))
	accessToken := pair["accessToken"].(string)

	for _, label := range []struct {
		name, token string
	}{
		{"jwt", accessToken},
		{"personal", "machine-token-阿琴"},
	} {
		request := httptest.NewRequest(http.MethodGet, "/api/console/whoami", nil)
		request.Header.Set("Authorization", "Bearer "+label.token)
		recorder := httptest.NewRecorder()
		harness.console(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s channel whoami status = %d: %s", label.name, recorder.Code, recorder.Body.String())
		}
		body := decodeBody(t, recorder)
		if body["name"] != "阿琴" {
			t.Fatalf("%s channel whoami name = %v", label.name, body["name"])
		}
	}
}

func TestMePasswordChangeFlow(t *testing.T) {
	harness := newAuthHarness(t)
	seedPasswordOperator(t, harness, "小阅", "temp-pass-123456", time.Time{})
	pair := decodeBody(t, harness.login(t, "小阅", "temp-pass-123456"))
	accessToken := pair["accessToken"].(string)

	patch := func(body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPatch, "/api/console/me/password", bytes.NewBufferString(body))
		request.Header.Set("Authorization", "Bearer "+accessToken)
		recorder := httptest.NewRecorder()
		harness.console(recorder, request)
		return recorder
	}
	// 旧密码错误拒绝。
	if recorder := patch(`{"oldPassword":"nope-wrong","newPassword":"brand-new-pass-10"}`); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("wrong old password status = %d, want 401", recorder.Code)
	}
	// 新密码过短拒绝。
	if recorder := patch(`{"oldPassword":"temp-pass-123456","newPassword":"short"}`); recorder.Code != http.StatusBadRequest {
		t.Fatalf("short new password status = %d, want 400", recorder.Code)
	}
	// 正常改密成功。
	if recorder := patch(`{"oldPassword":"temp-pass-123456","newPassword":"brand-new-pass-10"}`); recorder.Code != http.StatusOK {
		t.Fatalf("password change status = %d: %s", recorder.Code, recorder.Body.String())
	}
	// 旧密码不再可用，新密码可登录。
	if recorder := harness.login(t, "小阅", "temp-pass-123456"); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("old password login status = %d, want 401", recorder.Code)
	}
	if recorder := harness.login(t, "小阅", "brand-new-pass-10"); recorder.Code != http.StatusOK {
		t.Fatalf("new password login status = %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestCreateOperatorIssuesTemporaryPassword(t *testing.T) {
	harness := newAuthHarness(t)
	seedPasswordOperator(t, harness, "阿琴", "director-pass-01", time.Now().UTC())

	request := httptest.NewRequest(http.MethodPost, "/api/console/operators",
		bytes.NewBufferString(`{"name":"新审计","role":"auditor"}`))
	request.Header.Set("Authorization", "Bearer machine-token-阿琴")
	recorder := httptest.NewRecorder()
	harness.console(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("create operator status = %d: %s", recorder.Code, recorder.Body.String())
	}
	body := decodeBody(t, recorder)
	temporary, _ := body["temporaryPassword"].(string)
	if temporary == "" {
		t.Fatalf("temporaryPassword missing from create response: %v", body)
	}
	// 临时密码可登录，且标记为首登强制改密。
	login := decodeBody(t, harness.login(t, "新审计", temporary))
	if login["passwordChangeRequired"] != true {
		t.Fatalf("passwordChangeRequired = %v, want true", login["passwordChangeRequired"])
	}
}

func TestLoginLegacyEvalModeRefused(t *testing.T) {
	// 无运营员行 = ADR-0008 legacy eval 模式：没有密码账号可登录，
	// APP_TOKEN 机器通道行为原样保留。
	harness := newAuthHarness(t)
	if recorder := harness.login(t, "anyone", "whatever-pass"); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("legacy-mode login status = %d, want 401", recorder.Code)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/console/whoami", nil)
	request.Header.Set("Authorization", "Bearer qiuqiu-dev-token")
	recorder := httptest.NewRecorder()
	harness.console(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("legacy APP_TOKEN whoami status = %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestLoginWithoutJWTSecretRefused(t *testing.T) {
	harness := newConsoleHarness(t) // 无 jwtSecret
	seedPasswordOperator(t, harness, "阿琴", "director-pass-01", time.Now().UTC())
	if recorder := harness.login(t, "阿琴", "director-pass-01"); recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("login without secret status = %d, want 503", recorder.Code)
	}
}

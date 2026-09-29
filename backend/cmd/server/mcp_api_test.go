package main

// mcp_api.go 鉴权装配测试（mcp-registry-serve 5.2）：真实 consoleauth
// JWT 与 operatorauth 个人令牌两条通道过 mcpserve.Handler——匿名 401、
// 无效/错签/过期 JWT 401、临时密码 JWT（无 scope）403、auditor/director
// 任一角色放行；legacy 开发旁路对 /mcp 不生效。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"qiuqiu/internal/auth"
	"qiuqiu/internal/config"
	"qiuqiu/internal/consoleauth"
	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/mcpserve"
	"qiuqiu/internal/operatorauth"
)

const (
	testMCPJWTSecret = "test-mcp-jwt-secret"
	testMCPToken     = "test-auditor-personal-token"
)

func testMCPResolver(t *testing.T) mcpIdentityResolver {
	t.Helper()
	store := operatorauth.NewMemoryStore()
	if _, err := store.Seed(context.Background(), "auditor", testMCPToken, operatorauth.RoleAuditor); err != nil {
		t.Fatalf("seed auditor: %v", err)
	}
	return mcpIdentityResolver{operators: store, jwtSecret: testMCPJWTSecret}
}

func testMCPSignJWT(t *testing.T, secret string, claims consoleauth.Claims) string {
	t.Helper()
	token, err := consoleauth.SignJWT(claims, secret, consoleauth.AccessTokenTTL)
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}
	return token
}

func testMCPPost(t *testing.T, handler http.Handler, bearer string) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	// The streamable transport requires the client to accept both response
	// media types before it will even look at the body.
	request.Header.Set("Accept", "application/json, text/event-stream")
	if bearer != "" {
		request.Header.Set("Authorization", "Bearer "+bearer)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func testMCPHandler(t *testing.T, resolver mcpIdentityResolver) http.Handler {
	t.Helper()
	return mcpserve.Handler(mcpserve.Deps{Matches: readOnlyStubStore{}}, resolver)
}

// readOnlyStubStore 是 matchstate.Repository 只读半边的空实现，够鉴权
// 测试打到 MCP handler 为止。
type readOnlyStubStore struct{}

func (readOnlyStubStore) PublicSnapshot(string) matchstate.Snapshot { return matchstate.Snapshot{} }
func (readOnlyStubStore) PublicEvents(string) []matchstate.MatchEvent {
	return nil
}

func TestMCPNoBearerIs401(t *testing.T) {
	recorder := testMCPPost(t, testMCPHandler(t, testMCPResolver(t)), "")
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}

func TestMCPInvalidBearerIs401(t *testing.T) {
	recorder := testMCPPost(t, testMCPHandler(t, testMCPResolver(t)), "garbage-token")
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}

func TestMCPWrongSignatureJWTIs401(t *testing.T) {
	token := testMCPSignJWT(t, "some-other-secret", consoleauth.Claims{
		Sub: "auditor", Role: "auditor", Scopes: []string{auth.ScopeOperatorTraceRead}, PasswordSet: true,
	})
	recorder := testMCPPost(t, testMCPHandler(t, testMCPResolver(t)), token)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}

func TestMCPExpiredJWTIs401(t *testing.T) {
	token, err := consoleauth.SignJWT(consoleauth.Claims{
		Sub: "auditor", Role: "auditor", Scopes: []string{auth.ScopeOperatorTraceRead}, PasswordSet: true,
		Exp: time.Now().Add(-time.Minute).Unix(), Iat: time.Now().Add(-2 * time.Minute).Unix(),
	}, testMCPJWTSecret, time.Hour)
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}
	recorder := testMCPPost(t, testMCPHandler(t, testMCPResolver(t)), token)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}

func TestMCPTempPasswordJWTHasNoScopesIs403(t *testing.T) {
	// ADR-0010 locked decision 1: first-login JWT (PasswordSet=false) carries
	// no scopes — the MCP read scope check must 403 it.
	token := testMCPSignJWT(t, testMCPJWTSecret, consoleauth.Claims{
		Sub: "rookie", Role: "director", Scopes: operatorauth.ScopesFor(operatorauth.RoleDirector), PasswordSet: false,
	})
	recorder := testMCPPost(t, testMCPHandler(t, testMCPResolver(t)), token)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", recorder.Code)
	}
}

func TestMCPJWTAuditorPasses(t *testing.T) {
	token := testMCPSignJWT(t, testMCPJWTSecret, consoleauth.Claims{
		Sub: "auditor", Role: "auditor", Scopes: operatorauth.ScopesFor(operatorauth.RoleAuditor), PasswordSet: true,
	})
	recorder := testMCPPost(t, testMCPHandler(t, testMCPResolver(t)), token)
	if recorder.Code == http.StatusUnauthorized || recorder.Code == http.StatusForbidden {
		t.Fatalf("status = %d, want the auditor JWT to reach the MCP handler", recorder.Code)
	}
	if recorder.Header().Get("Mcp-Session-Id") == "" {
		t.Errorf("initialize did not create an MCP session, status %d", recorder.Code)
	}
}

func TestMCPPersonalTokenAuditorPasses(t *testing.T) {
	recorder := testMCPPost(t, testMCPHandler(t, testMCPResolver(t)), testMCPToken)
	if recorder.Code == http.StatusUnauthorized || recorder.Code == http.StatusForbidden {
		t.Fatalf("status = %d, want the personal-token auditor to reach the MCP handler", recorder.Code)
	}
	if recorder.Header().Get("Mcp-Session-Id") == "" {
		t.Errorf("initialize did not create an MCP session, status %d", recorder.Code)
	}
}

func TestMCPLegacyDevBypassIsDead(t *testing.T) {
	// In legacy mode (no operator rows, empty APP_TOKEN, non-production) the
	// rest of the operator API grants anonymous director scopes — the MCP
	// endpoint must not: 匿名一律 401.
	emptyResolver := mcpIdentityResolver{operators: operatorauth.NewMemoryStore(), jwtSecret: testMCPJWTSecret}
	recorder := testMCPPost(t, testMCPHandler(t, emptyResolver), "")
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 even in legacy mode", recorder.Code)
	}
}

// 装配烟测：mountMCPServer 挂上 /mcp 且路径经只读门（匿名 401）。
func TestMountMCPServerMountsOnMux(t *testing.T) {
	mux := http.NewServeMux()
	mountMCPServer(mux, &config.Config{JWTSecret: testMCPJWTSecret}, readOnlyStubStore{}, nil, operatorauth.NewMemoryStore())
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}")))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("mounted /mcp status = %d, want 401 for anonymous", recorder.Code)
	}
}

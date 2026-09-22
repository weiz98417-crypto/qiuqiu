package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"qiuqiu/internal/auth"
	"qiuqiu/internal/config"
)

func TestSessionAPIIssuesRefreshesAndRevokesAnonymousSession(t *testing.T) {
	manager, err := auth.NewManager(auth.NewMemoryStore(), "test-session-signing-key-0123456789")
	if err != nil {
		t.Fatal(err)
	}
	handler := handleSessionAPI(manager, &config.Config{Environment: "development"})

	created := doSessionRequest(t, handler, http.MethodPost, "/api/sessions/anonymous", `{"deviceId":"device_123"}`, "")
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	if created.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("session response cache-control = %q", created.Header().Get("Cache-Control"))
	}
	var first map[string]interface{}
	if err := json.Unmarshal(created.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	accessToken := first["accessToken"].(string)
	refreshToken := first["refreshToken"].(string)
	userID := first["userId"].(string)
	if userID == "" || accessToken == "" || refreshToken == "" {
		t.Fatalf("missing session fields: %+v", first)
	}

	refreshed := doSessionRequest(t, handler, http.MethodPost, "/api/sessions/refresh", `{"refreshToken":"`+refreshToken+`"}`, "")
	if refreshed.Code != http.StatusOK {
		t.Fatalf("refresh status=%d body=%s", refreshed.Code, refreshed.Body.String())
	}
	var second map[string]interface{}
	if err := json.Unmarshal(refreshed.Body.Bytes(), &second); err != nil {
		t.Fatal(err)
	}
	if second["userId"] != userID || second["accessToken"] == accessToken {
		t.Fatalf("refresh did not preserve identity and rotate access token: %+v", second)
	}

	revoked := doSessionRequest(t, handler, http.MethodPost, "/api/sessions/revoke", "{}", "Bearer "+second["accessToken"].(string))
	if revoked.Code != http.StatusOK {
		t.Fatalf("revoke status=%d body=%s", revoked.Code, revoked.Body.String())
	}
	if _, err := manager.Authenticate(context.Background(), second["accessToken"].(string)); err != auth.ErrRevoked {
		t.Fatalf("revoked session authenticate error=%v", err)
	}
}

func TestSessionAPIRejectsMalformedRefreshRequest(t *testing.T) {
	manager, err := auth.NewManager(auth.NewMemoryStore(), "test-session-signing-key-0123456789")
	if err != nil {
		t.Fatal(err)
	}
	handler := handleSessionAPI(manager, &config.Config{Environment: "development"})
	response := doSessionRequest(t, handler, http.MethodPost, "/api/sessions/refresh", `{}`, "")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("malformed refresh status=%d", response.Code)
	}
}

// 登录凭证缝端点（ADR-0020）：Bearer 缺失 401、表单非法 400、成功 200。
func TestSessionAPILoginEndpoint(t *testing.T) {
	manager, err := auth.NewManager(auth.NewMemoryStore(), "test-session-signing-key-0123456789")
	if err != nil {
		t.Fatal(err)
	}
	handler := handleSessionAPI(manager, &config.Config{Environment: "development"})

	created := doSessionRequest(t, handler, http.MethodPost, "/api/sessions/anonymous", `{"deviceId":"device_123"}`, "")
	var anonymous map[string]interface{}
	if err := json.Unmarshal(created.Body.Bytes(), &anonymous); err != nil {
		t.Fatal(err)
	}
	bearer := "Bearer " + anonymous["accessToken"].(string)

	// Bearer 缺失 → 401。
	missing := doSessionRequest(t, handler, http.MethodPost, "/api/sessions/login", `{"identifier":"fan@example.com","password":"password123"}`, "")
	if missing.Code != http.StatusUnauthorized {
		t.Fatalf("missing bearer status=%d", missing.Code)
	}

	// 表单非法 → 400。
	bad := doSessionRequest(t, handler, http.MethodPost, "/api/sessions/login", `{"identifier":"not-an-email","password":"password123"}`, bearer)
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("bad form status=%d", bad.Code)
	}
	short := doSessionRequest(t, handler, http.MethodPost, "/api/sessions/login", `{"identifier":"fan@example.com","password":"short"}`, bearer)
	if short.Code != http.StatusBadRequest {
		t.Fatalf("short password status=%d", short.Code)
	}

	// 注册式登录成功 → 200，userId 与匿名会话一致。
	ok := doSessionRequest(t, handler, http.MethodPost, "/api/sessions/login", `{"identifier":"Fan@Example.com","password":"password123"}`, bearer)
	if ok.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", ok.Code, ok.Body.String())
	}
	var loggedIn map[string]interface{}
	if err := json.Unmarshal(ok.Body.Bytes(), &loggedIn); err != nil {
		t.Fatal(err)
	}
	if loggedIn["userId"] != anonymous["userId"] {
		t.Fatalf("login changed userId: %v → %v", anonymous["userId"], loggedIn["userId"])
	}

	// 切换路径：新设备新会话，用已绑定 identifier + 正确密码 → 200 且 userId 指向该账号。
	switched := doSessionRequest(t, handler, http.MethodPost, "/api/sessions/anonymous", `{"deviceId":"device_new"}`, "")
	var other map[string]interface{}
	if err := json.Unmarshal(switched.Body.Bytes(), &other); err != nil {
		t.Fatal(err)
	}
	relogin := doSessionRequest(t, handler, http.MethodPost, "/api/sessions/login", `{"identifier":"fan@example.com","password":"password123"}`, "Bearer "+other["accessToken"].(string))
	if relogin.Code != http.StatusOK {
		t.Fatalf("relogin status=%d body=%s", relogin.Code, relogin.Body.String())
	}
	var switchedIn map[string]interface{}
	if err := json.Unmarshal(relogin.Body.Bytes(), &switchedIn); err != nil {
		t.Fatal(err)
	}
	if switchedIn["userId"] != anonymous["userId"] {
		t.Fatalf("switch userId = %v, want %v", switchedIn["userId"], anonymous["userId"])
	}

	// 密码错误 → 401。
	wrong := doSessionRequest(t, handler, http.MethodPost, "/api/sessions/login", `{"identifier":"fan@example.com","password":"wrong-password"}`, "Bearer "+other["accessToken"].(string))
	if wrong.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password status=%d", wrong.Code)
	}
}

func doSessionRequest(t *testing.T, handler http.Handler, method, path, body, authorization string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

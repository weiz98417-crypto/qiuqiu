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

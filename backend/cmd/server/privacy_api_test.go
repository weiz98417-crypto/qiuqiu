package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"qiuqiu/internal/auth"
	"qiuqiu/internal/config"
	"qiuqiu/internal/privacy"
)

func TestPrivacyAPIExportsAndDeletesAuthenticatedUserData(t *testing.T) {
	manager, err := auth.NewManager(auth.NewMemoryStore(), "test-session-signing-key-0123456789")
	if err != nil {
		t.Fatal(err)
	}
	session, err := manager.IssueAnonymous(context.Background(), "device_privacy")
	if err != nil {
		t.Fatal(err)
	}
	privacyStore := privacy.NewMemoryStore()
	service := privacy.NewService(privacyStore)
	handler := handlePrivacyAPI(manager, &config.Config{Environment: "development"}, service)
	authorization := "Bearer " + session.AccessToken

	status := doPrivacyRequest(t, handler, http.MethodGet, "/api/me/privacy", "", authorization)
	if status.Code != http.StatusOK {
		t.Fatalf("status code=%d body=%s", status.Code, status.Body.String())
	}
	var active privacy.Status
	if err := json.Unmarshal(status.Body.Bytes(), &active); err != nil {
		t.Fatal(err)
	}
	if active.UserID != session.Claims.Subject || active.Status != "active" {
		t.Fatalf("active response = %+v", active)
	}

	deleted := doPrivacyRequest(t, handler, http.MethodDelete, "/api/me/data", `{"reason":"user_request"}`, authorization)
	if deleted.Code != http.StatusAccepted {
		t.Fatalf("delete code=%d body=%s", deleted.Code, deleted.Body.String())
	}
	var deletion struct {
		JobID string `json:"jobId"`
	}
	if err := json.Unmarshal(deleted.Body.Bytes(), &deletion); err != nil || deletion.JobID == "" {
		t.Fatalf("delete response = %s", deleted.Body.String())
	}

	deadline := time.Now().Add(time.Second)
	for {
		status = doPrivacyRequest(t, handler, http.MethodGet, "/api/me/privacy?jobId="+deletion.JobID, "", "")
		var current privacy.Status
		if err := json.Unmarshal(status.Body.Bytes(), &current); err != nil {
			t.Fatal(err)
		}
		if current.Status == "completed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("deletion did not complete: %+v", current)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := service.Export(context.Background(), session.Claims.Subject); err != privacy.ErrDataDeleted {
		t.Fatalf("export after deletion error = %v", err)
	}
}

func doPrivacyRequest(t *testing.T, handler http.Handler, method, path, body, authorization string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", authorization)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

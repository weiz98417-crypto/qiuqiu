package main

// memory-surfacing 1.7:未完话题端点——鉴权(401/403)、只开放话题、账本
// 降级=空列表静默。

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
	"qiuqiu/internal/memory"
)

func newThreadsTestHarness(t *testing.T) (http.HandlerFunc, string, string, *memory.Fake) {
	t.Helper()
	manager, err := auth.NewManager(auth.NewMemoryStore(), "test-session-signing-key-0123456789")
	if err != nil {
		t.Fatal(err)
	}
	session, err := manager.IssueAnonymous(context.Background(), "device_threads")
	if err != nil {
		t.Fatal(err)
	}
	fakeThreads := memory.NewFake()
	queue := memory.NewQueue(memory.NewMemobase(memory.MemobaseConfig{BaseURL: "http://127.0.0.1:1", Token: "t"}), nil, nil,
		memory.WithThreads(fakeThreads))
	handler := handleThreadsAPI(manager, &config.Config{Environment: "development"}, queue)
	return handler, session.AccessToken, session.Claims.Subject, fakeThreads
}

func doThreadsRequest(t *testing.T, handler http.Handler, method, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(""))
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestThreadsAPIAuthMatrix(t *testing.T) {
	handler, token, _, _ := newThreadsTestHarness(t)
	if got := doThreadsRequest(t, handler, http.MethodGet, "/api/me/threads", ""); got.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GET = %d, want 401", got.Code)
	}
	if got := doThreadsRequest(t, handler, http.MethodGet, "/api/me/threads", token); got.Code != http.StatusOK {
		t.Fatalf("GET = %d", got.Code)
	}
	if got := doThreadsRequest(t, handler, http.MethodPost, "/api/me/threads", token); got.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST = %d, want 405", got.Code)
	}
}

func TestThreadsAPIReturnsOpenThreadsOnly(t *testing.T) {
	handler, token, userID, fakeThreads := newThreadsTestHarness(t)
	ctx := context.Background()
	if _, err := fakeThreads.AppendThread(ctx, memory.Thread{
		UserID: userID, Kind: memory.ThreadPromise,
		Content: "待跟进的约定：下半场再聊换人", SourceTurn: "sig-1", CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("append open: %v", err)
	}
	if _, err := fakeThreads.AppendThread(ctx, memory.Thread{
		UserID: userID, Kind: memory.ThreadUnansweredQuestion,
		Content: "越位怎么判", SourceTurn: "sig-2", CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("append second: %v", err)
	}
	// 关闭一条(Fake 的 AppendThread 强制 open,走 MarkThreadAddressed)& 别人的话头。
	all, _ := fakeThreads.OpenThreads(ctx, userID)
	if err := fakeThreads.MarkThreadAddressed(ctx, all[0].ID); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := fakeThreads.AppendThread(ctx, memory.Thread{
		UserID: "someone-else", Kind: memory.ThreadPromise,
		Content: "别人的话头", CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("append foreign: %v", err)
	}

	response := doThreadsRequest(t, handler, http.MethodGet, "/api/me/threads", token)
	if response.Code != http.StatusOK {
		t.Fatalf("GET = %d", response.Code)
	}
	var payload struct {
		Threads []threadEntryResponse `json:"threads"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Threads) != 1 {
		t.Fatalf("threads = %+v, want only the open own thread", payload.Threads)
	}
	if !strings.Contains(payload.Threads[0].Content, "越位怎么判") {
		t.Fatalf("thread content = %+v", payload.Threads)
	}
}

func TestThreadsAPIDegradedLedgerIsEmptyList(t *testing.T) {
	manager, err := auth.NewManager(auth.NewMemoryStore(), "test-session-signing-key-0123456789")
	if err != nil {
		t.Fatal(err)
	}
	session, err := manager.IssueAnonymous(context.Background(), "device_deg")
	if err != nil {
		t.Fatal(err)
	}
	// 无 WithThreads:账本缺席 = 空列表(话题条静默隐藏),不是 5xx。
	queue := memory.NewQueue(memory.NewMemobase(memory.MemobaseConfig{}), nil, nil)
	handler := handleThreadsAPI(manager, &config.Config{Environment: "development"}, queue)
	response := doThreadsRequest(t, handler, http.MethodGet, "/api/me/threads", session.AccessToken)
	if response.Code != http.StatusOK {
		t.Fatalf("degraded GET = %d body=%s, want 200 empty", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"threads":[]`) {
		t.Fatalf("degraded body = %s, want empty list", response.Body.String())
	}
}

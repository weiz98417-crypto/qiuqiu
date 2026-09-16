package memory

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type recordedRequest struct {
	method string
	path   string
	query  url.Values
	body   []byte
	auth   string
}

func memobaseOK(data string) []byte {
	return []byte(`{"errno":0,"errmsg":"success","data":` + data + `}`)
}

// stubMemobaseServer records every request and answers through respond.
func stubMemobaseServer(respond func(recordedRequest) (int, []byte)) (*httptest.Server, func() []recordedRequest) {
	var mu sync.Mutex
	var requests []recordedRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		recorded := recordedRequest{method: r.Method, path: r.URL.Path, query: r.URL.Query(), body: body, auth: r.Header.Get("Authorization")}
		mu.Lock()
		requests = append(requests, recorded)
		mu.Unlock()
		status, payload := respond(recorded)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(payload)
	}))
	return server, func() []recordedRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]recordedRequest(nil), requests...)
	}
}

func TestMemobaseObserveCreatesUserInsertsBlobAndFlushSucceeds(t *testing.T) {
	server, requestsFn := stubMemobaseServer(func(r recordedRequest) (int, []byte) {
		switch {
		case r.method == http.MethodGet && r.path == "/api/v1/users/qiuqiu-user-1":
			return http.StatusNotFound, []byte(`{"errno":2003,"errmsg":"user not found"}`)
		case r.method == http.MethodPost && r.path == "/api/v1/users":
			return http.StatusOK, memobaseOK(`"user-1"`)
		case r.method == http.MethodPost && strings.HasPrefix(r.path, "/api/v1/blobs/insert/qiuqiu-user-1"):
			return http.StatusOK, memobaseOK(`"blob-1"`)
		case r.method == http.MethodPost && r.path == "/api/v1/users/buffer/qiuqiu-user-1/chat":
			return http.StatusOK, memobaseOK(`null`)
		default:
			return http.StatusOK, memobaseOK(`null`)
		}
	})
	defer server.Close()
	adapter := NewMemobase(MemobaseConfig{BaseURL: server.URL, Token: "test-token"})
	if !adapter.Configured() {
		t.Fatal("adapter should be configured with base URL and token")
	}
	moment := Moment{
		UserID:         "user-1",
		Kind:           MomentUserFact,
		Content:        "用户明确表达偏好或边界：我喜欢皇马",
		Importance:     0.8,
		LedgerSequence: 7,
		OccurredAt:     time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC),
	}
	if err := adapter.Observe(context.Background(), moment); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if err := adapter.Flush(context.Background(), "user-1"); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	requests := requestsFn()
	if len(requests) != 4 {
		t.Fatalf("requests = %d, want 4 (get user, create user, insert blob, flush)", len(requests))
	}
	if requests[0].auth != "Bearer test-token" {
		t.Fatalf("auth header = %q, want Bearer token", requests[0].auth)
	}
	var createBody map[string]any
	if err := json.Unmarshal(requests[1].body, &createBody); err != nil {
		t.Fatalf("decode create body: %v", err)
	}
	if createBody["id"] != "qiuqiu-user-1" {
		t.Fatalf("create user id = %v, want namespaced user id", createBody["id"])
	}
	if requests[2].query.Get("wait_process") != "false" {
		t.Fatalf("insert wait_process = %q, want false", requests[2].query.Get("wait_process"))
	}
	var insertBody struct {
		BlobType string `json:"blob_type"`
		BlobData struct {
			Messages []chatMessage `json:"messages"`
		} `json:"blob_data"`
		Fields map[string]any `json:"fields"`
	}
	if err := json.Unmarshal(requests[2].body, &insertBody); err != nil {
		t.Fatalf("decode insert body: %v", err)
	}
	if insertBody.BlobType != "chat" || len(insertBody.BlobData.Messages) != 1 {
		t.Fatalf("insert payload = %+v, want one chat message", insertBody)
	}
	if insertBody.BlobData.Messages[0].Content != moment.Content {
		t.Fatalf("blob content = %q, want %q", insertBody.BlobData.Messages[0].Content, moment.Content)
	}
	if insertBody.Fields["kind"] != string(MomentUserFact) {
		t.Fatalf("fields.kind = %v, want %s", insertBody.Fields["kind"], MomentUserFact)
	}
	if requests[3].path != "/api/v1/users/buffer/qiuqiu-user-1/chat" || requests[3].query.Get("wait_process") != "true" {
		t.Fatalf("flush request = %s%s, want buffered chat flush with wait_process", requests[3].path, requests[3].query.Encode())
	}
}

func TestMemobaseRecallRendersProvenanceAndSkipsEmptyEntries(t *testing.T) {
	server, _ := stubMemobaseServer(func(r recordedRequest) (int, []byte) {
		switch {
		case r.method == http.MethodGet && strings.HasPrefix(r.path, "/api/v1/users/profile/"):
			return http.StatusOK, memobaseOK(`{"profiles":[` +
				`{"content":"皇马","attributes":{"topic":"basic_info","sub_topic":"favorite_team"},"updated_at":"2026-01-02T03:04:05Z"},` +
				`{"content":"","attributes":{"topic":"preferences","sub_topic":"reply_style"},"updated_at":"2026-01-01T00:00:00Z"}]}`)
		case r.method == http.MethodGet && strings.HasPrefix(r.path, "/api/v1/users/"):
			return http.StatusOK, memobaseOK(`{}`)
		default:
			return http.StatusOK, memobaseOK(`null`)
		}
	})
	defer server.Close()
	adapter := NewMemobase(MemobaseConfig{BaseURL: server.URL, Token: "test-token"})
	recalls := adapter.Recall(context.Background(), Query{UserID: "user-1", Focus: "皇马", Limit: 5})
	if len(recalls) != 1 {
		t.Fatalf("recalls = %+v, want the single non-empty profile entry", recalls)
	}
	recall := recalls[0]
	if recall.Content != "皇马" {
		t.Fatalf("content = %q, want 皇马", recall.Content)
	}
	if recall.Source != "memobase://profile/basic_info/favorite_team" {
		t.Fatalf("source = %q, want memobase profile provenance", recall.Source)
	}
	if !almostEqual(recall.Importance, 0.8) {
		t.Fatalf("importance = %v, want 0.8 (0.5 base + 0.3 focus match)", recall.Importance)
	}
	if recall.OccurredAt.IsZero() {
		t.Fatal("recalled memory should carry the profile updated_at timestamp")
	}
	if adapter.Degraded() {
		t.Fatal("successful recall must clear the degraded flag")
	}
}

func TestMemobasePortraitRendersBoundedChineseBlock(t *testing.T) {
	server, _ := stubMemobaseServer(func(r recordedRequest) (int, []byte) {
		switch {
		case r.method == http.MethodGet && strings.HasPrefix(r.path, "/api/v1/users/profile/"):
			return http.StatusOK, memobaseOK(`{"profiles":[` +
				`{"content":"皇马","attributes":{"topic":"basic_info","sub_topic":"favorite_team"},"updated_at":"2026-01-02T03:04:05Z"},` +
				`{"content":"简短","attributes":{"topic":"preferences","sub_topic":"reply_style"},"updated_at":"2026-01-03T03:04:05Z"}]}`)
		case r.method == http.MethodGet && strings.HasPrefix(r.path, "/api/v1/users/"):
			return http.StatusOK, memobaseOK(`{}`)
		default:
			return http.StatusOK, memobaseOK(`null`)
		}
	})
	defer server.Close()
	adapter := NewMemobase(MemobaseConfig{BaseURL: server.URL, Token: "test-token"})
	portrait, err := adapter.Portrait(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("Portrait: %v", err)
	}
	if !strings.Contains(portrait.Block, "用户画像") || !strings.Contains(portrait.Block, "favorite_team：皇马") {
		t.Fatalf("portrait block = %q, want labelled Chinese entries", portrait.Block)
	}
	if len([]rune(portrait.Block)) > maxPortraitRunes {
		t.Fatalf("portrait block is %d runes, bounded at %d", len([]rune(portrait.Block)), maxPortraitRunes)
	}
	if portrait.UpdatedAt.IsZero() {
		t.Fatal("portrait should carry the latest profile timestamp")
	}
}

func TestMemobaseDegradedOn5xxRecoversOnSuccess(t *testing.T) {
	var fail atomic.Bool
	server, _ := stubMemobaseServer(func(r recordedRequest) (int, []byte) {
		switch {
		case fail.Load() && strings.HasPrefix(r.path, "/api/v1/users/profile/"):
			return http.StatusInternalServerError, []byte("boom")
		case r.method == http.MethodGet && strings.HasPrefix(r.path, "/api/v1/users/profile/"):
			return http.StatusOK, memobaseOK(`{"profiles":[{"content":"皇马","attributes":{"topic":"basic_info","sub_topic":"favorite_team"},"updated_at":"2026-01-02T03:04:05Z"}]}`)
		case r.method == http.MethodGet && strings.HasPrefix(r.path, "/api/v1/users/"):
			return http.StatusOK, memobaseOK(`{}`)
		default:
			return http.StatusOK, memobaseOK(`null`)
		}
	})
	defer server.Close()
	adapter := NewMemobase(MemobaseConfig{BaseURL: server.URL, Token: "test-token"})
	fail.Store(true)
	if recalls := adapter.Recall(context.Background(), Query{UserID: "user-1", Focus: "皇马", Limit: 5}); len(recalls) != 0 {
		t.Fatalf("degraded recall = %+v, want empty", recalls)
	}
	if !adapter.Degraded() {
		t.Fatal("5xx responses must mark the adapter degraded")
	}
	if _, err := adapter.Portrait(context.Background(), "user-1"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("degraded portrait error = %v, want ErrUnavailable", err)
	}
	fail.Store(false)
	recalls := adapter.Recall(context.Background(), Query{UserID: "user-1", Focus: "皇马", Limit: 5})
	if len(recalls) != 1 || adapter.Degraded() {
		t.Fatalf("recall after recovery = %+v degraded=%t, want one recall and cleared flag", recalls, adapter.Degraded())
	}
}

func TestMemobaseUnconfiguredAdapterDegradesImmediately(t *testing.T) {
	adapter := NewMemobase(MemobaseConfig{})
	if adapter.Configured() {
		t.Fatal("adapter without token must report unconfigured")
	}
	if err := adapter.Observe(context.Background(), Moment{UserID: "user-1", Content: "x"}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unconfigured Observe = %v, want ErrUnavailable", err)
	}
	if recalls := adapter.Recall(context.Background(), Query{UserID: "user-1"}); len(recalls) != 0 {
		t.Fatalf("unconfigured recall = %+v, want empty", recalls)
	}
	if _, err := adapter.Portrait(context.Background(), "user-1"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unconfigured Portrait = %v, want ErrUnavailable", err)
	}
	if _, err := adapter.Threads(context.Background(), "user-1"); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("Threads = %v, want ErrNotSupported until the C2 store lands", err)
	}
}

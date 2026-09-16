package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"qiuqiu/internal/auth"
	"qiuqiu/internal/config"
	"qiuqiu/internal/memory"
)

type portraitStubServer struct {
	mu       sync.Mutex
	requests []string
	server   *httptest.Server
}

func newPortraitStubServer(t *testing.T, profileJSON string) *portraitStubServer {
	t.Helper()
	stub := &portraitStubServer{}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		stub.mu.Lock()
		stub.requests = append(stub.requests, r.Method+" "+r.URL.Path+" "+string(body))
		stub.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/users/profile/"):
			_, _ = w.Write([]byte(`{"errno":0,"errmsg":"success","data":{"profiles":` + profileJSON + `}}`))
		default:
			_, _ = w.Write([]byte(`{"errno":0,"errmsg":"success","data":null}`))
		}
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

func (stub *portraitStubServer) requestsContaining(needle string) []string {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	matched := make([]string, 0, 2)
	for _, request := range stub.requests {
		if strings.Contains(request, needle) {
			matched = append(matched, request)
		}
	}
	return matched
}

func newPortraitTestHarness(t *testing.T, profileJSON string) (http.HandlerFunc, string, *portraitStubServer) {
	t.Helper()
	manager, err := auth.NewManager(auth.NewMemoryStore(), "test-session-signing-key-0123456789")
	if err != nil {
		t.Fatal(err)
	}
	session, err := manager.IssueAnonymous(context.Background(), "device_portrait")
	if err != nil {
		t.Fatal(err)
	}
	stub := newPortraitStubServer(t, profileJSON)
	queue := memory.NewQueue(memory.NewMemobase(memory.MemobaseConfig{BaseURL: stub.server.URL, Token: "test-token"}), nil, nil,
		memory.WithPortraitOverlays(memory.NewMemoryPortraitOverlays()))
	handler := handlePortraitAPI(manager, &config.Config{Environment: "development"}, queue)
	return handler, session.AccessToken, stub
}

func doPortraitRequest(t *testing.T, handler http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func decodePortraitResponse(t *testing.T, response *httptest.ResponseRecorder) portraitResponse {
	t.Helper()
	var decoded portraitResponse
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode portrait response %s: %v", response.Body.String(), err)
	}
	return decoded
}

func TestPortraitAPIRequiresSession(t *testing.T) {
	handler, _, _ := newPortraitTestHarness(t, `[]`)
	if got := doPortraitRequest(t, handler, http.MethodGet, "/api/me/portrait", "", ""); got.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GET = %d, want 401", got.Code)
	}
	if got := doPortraitRequest(t, handler, http.MethodDelete, "/api/me/portrait", `{}`, ""); got.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated DELETE = %d, want 401", got.Code)
	}
}

func TestPortraitAPIGetEditAndForgetRoundTrip(t *testing.T) {
	handler, token, _ := newPortraitTestHarness(t, `[]`)

	// Empty portrait renders as an empty list, never an error (degradation).
	empty := doPortraitRequest(t, handler, http.MethodGet, "/api/me/portrait", "", token)
	if empty.Code != http.StatusOK {
		t.Fatalf("GET = %d body=%s", empty.Code, empty.Body.String())
	}
	if entries := decodePortraitResponse(t, empty).Entries; len(entries) != 0 {
		t.Fatalf("initial entries = %+v, want none", entries)
	}

	// PATCH writes a real user-authored slot into the local overlay.
	edited := doPortraitRequest(t, handler, http.MethodPatch, "/api/me/portrait",
		`{"topic":"basic_info","subTopic":"favorite_player","content":"佩德里"}`, token)
	if edited.Code != http.StatusOK {
		t.Fatalf("PATCH = %d body=%s", edited.Code, edited.Body.String())
	}
	entries := decodePortraitResponse(t, edited).Entries
	if len(entries) != 1 || entries[0].Content != "佩德里" || entries[0].Source != "user" {
		t.Fatalf("entries after PATCH = %+v, want one user-authored slot", entries)
	}
	if entries[0].Label == "" || entries[0].TopicLabel == "" {
		t.Fatalf("entries after PATCH = %+v, want Chinese labels for the page", entries)
	}

	// GET after PATCH must show the same stored state.
	reread := decodePortraitResponse(t, doPortraitRequest(t, handler, http.MethodGet, "/api/me/portrait", "", token))
	if len(reread.Entries) != 1 || reread.Entries[0].Content != "佩德里" {
		t.Fatalf("entries after GET = %+v, want the persisted edit", reread.Entries)
	}

	// DELETE tombstones the slot: the stored state disappears immediately.
	forgotten := doPortraitRequest(t, handler, http.MethodDelete, "/api/me/portrait",
		`{"topic":"basic_info","subTopic":"favorite_player"}`, token)
	if forgotten.Code != http.StatusOK {
		t.Fatalf("DELETE = %d body=%s", forgotten.Code, forgotten.Body.String())
	}
	if entries := decodePortraitResponse(t, forgotten).Entries; len(entries) != 0 {
		t.Fatalf("entries after DELETE = %+v, want none", entries)
	}

	// Invalid edits are rejected before any write.
	if got := doPortraitRequest(t, handler, http.MethodPatch, "/api/me/portrait", `{"topic":"","subTopic":"x","content":"y"}`, token); got.Code != http.StatusBadRequest {
		t.Fatalf("PATCH without topic = %d, want 400", got.Code)
	}
	if got := doPortraitRequest(t, handler, http.MethodPatch, "/api/me/portrait", `{"topic":"basic_info","subTopic":"favorite_team","content":"`+strings.Repeat("长", 200)+`"}`, token); got.Code != http.StatusBadRequest {
		t.Fatalf("oversized PATCH = %d, want 400", got.Code)
	}
}

// TestPortraitAPIDeleteIsHonoredOnNextTurn asserts the page's delete flows
// into the stored state: after the DELETE, the portrait read (shared by this
// handler and the realization prompt assembly) no longer contains the fact.
func TestPortraitAPIDeleteIsHonoredOnNextTurn(t *testing.T) {
	handler, token, _ := newPortraitTestHarness(t, `[{"id":"prof-team","content":"皇马","attributes":{"topic":"basic_info","sub_topic":"favorite_team"},"updated_at":"2026-09-01T03:04:05Z"}]`)

	edited := doPortraitRequest(t, handler, http.MethodPatch, "/api/me/portrait",
		`{"topic":"basic_info","subTopic":"favorite_player","content":"佩德里"}`, token)
	if edited.Code != http.StatusOK {
		t.Fatalf("PATCH = %d body=%s", edited.Code, edited.Body.String())
	}
	seeded := decodePortraitResponse(t, doPortraitRequest(t, handler, http.MethodGet, "/api/me/portrait", "", token))
	if len(seeded.Entries) != 2 {
		t.Fatalf("entries = %+v, want synthesis plus the user edit", seeded.Entries)
	}

	// Forget the synthesis slot by sub-topic; the handler resolves the remote
	// id from the current portrait.
	forgotten := doPortraitRequest(t, handler, http.MethodDelete, "/api/me/portrait",
		`{"topic":"basic_info","subTopic":"favorite_team"}`, token)
	if forgotten.Code != http.StatusOK {
		t.Fatalf("DELETE = %d body=%s", forgotten.Code, forgotten.Body.String())
	}
	remaining := decodePortraitResponse(t, doPortraitRequest(t, handler, http.MethodGet, "/api/me/portrait", "", token))
	if len(remaining.Entries) != 1 || remaining.Entries[0].SubTopic != "favorite_player" {
		t.Fatalf("entries after DELETE = %+v, want only the user edit", remaining.Entries)
	}
}

func TestPortraitAPIForgetEverythingClearsAllSlots(t *testing.T) {
	handler, token, _ := newPortraitTestHarness(t, `[{"id":"prof-a","content":"皇马","attributes":{"topic":"basic_info","sub_topic":"favorite_team"},"updated_at":"2026-09-01T03:04:05Z"},{"id":"prof-b","content":"佩德里","attributes":{"topic":"basic_info","sub_topic":"favorite_player"},"updated_at":"2026-09-01T03:04:05Z"}]`)
	cleared := doPortraitRequest(t, handler, http.MethodDelete, "/api/me/portrait", `{}`, token)
	if cleared.Code != http.StatusOK {
		t.Fatalf("DELETE all = %d body=%s", cleared.Code, cleared.Body.String())
	}
	if entries := decodePortraitResponse(t, cleared).Entries; len(entries) != 0 {
		t.Fatalf("entries after DELETE all = %+v, want none", entries)
	}
}

func TestPortraitAPIForwardsMutationsToMemobase(t *testing.T) {
	handler, token, stub := newPortraitTestHarness(t, `[{"id":"prof-team","content":"皇马","attributes":{"topic":"basic_info","sub_topic":"favorite_team"},"updated_at":"2026-09-01T03:04:05Z"}]`)

	if got := doPortraitRequest(t, handler, http.MethodPatch, "/api/me/portrait",
		`{"topic":"basic_info","subTopic":"favorite_team","content":"巴萨","entryId":"prof-team"}`, token); got.Code != http.StatusOK {
		t.Fatalf("PATCH = %d body=%s", got.Code, got.Body.String())
	}
	puts := stub.requestsContaining("PUT /api/v1/users/profile/qiuqiu-")
	if len(puts) == 0 {
		t.Fatalf("requests = %v, want the forwarded profile PUT", stub.requests)
	}
	if !strings.Contains(puts[0], "巴萨") {
		t.Fatalf("forwarded PUT = %q, want the edited content", puts[0])
	}

	if got := doPortraitRequest(t, handler, http.MethodDelete, "/api/me/portrait",
		`{"topic":"basic_info","subTopic":"favorite_team","entryId":"prof-team"}`, token); got.Code != http.StatusOK {
		t.Fatalf("DELETE = %d body=%s", got.Code, got.Body.String())
	}
	deletes := stub.requestsContaining("DELETE /api/v1/users/profile/")
	if len(deletes) == 0 {
		t.Fatalf("requests = %v, want the forwarded profile DELETE", stub.requests)
	}
	if !strings.Contains(deletes[0], "/prof-team") {
		t.Fatalf("forwarded DELETE = %q, want the profile id", deletes[0])
	}
}

package main

// 球友手记端点（teammate-journal 10.2/10.3）：鉴权（401/403）、列表/点赞/
// 删除/赛季册、无库降级=空列表静默。走 handler 直调（路由注册行由
// journal_routes_locked 锁 grep 面防 node 静默 no-op 吞注册）。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"qiuqiu/internal/auth"
	"qiuqiu/internal/config"
	"qiuqiu/internal/journal"
	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/memory"
)

func newJournalTestHarness(t *testing.T) (http.Handler, string, string, journal.Store) {
	t.Helper()
	manager, err := auth.NewManager(auth.NewMemoryStore(), "test-session-signing-key-0123456789")
	if err != nil {
		t.Fatal(err)
	}
	session, err := manager.IssueAnonymous(context.Background(), "device_journal")
	if err != nil {
		t.Fatal(err)
	}
	store := &journal.MemoryStore{}
	inner := handleJournalAPI(manager, &config.Config{Environment: "development"}, store, nil)
	// 与 main.go 注册形态一致：PathValue 只在 ServeMux 分发下有值。
	mux := http.NewServeMux()
	mux.HandleFunc("/api/me/journal", inner)
	mux.HandleFunc("/api/me/journal/album", inner)
	mux.HandleFunc("/api/me/journal/{entryId}", inner)
	mux.HandleFunc("/api/me/journal/{entryId}/like", inner)
	return mux, session.AccessToken, session.Claims.Subject, store
}

func doJournalRequest(t *testing.T, handler http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

// 路由注册锁（CRLF 教训：main.go 的 mux.HandleFunc 注册曾被 node 静默
// no-op 吞掉，测试直调 handler 没暴露）：journal 三条注册行必须在场。
func TestJournalRoutesLocked(t *testing.T) {
	mainSourceBytes, readErr := os.ReadFile("main.go")
	if readErr != nil {
		t.Fatalf("read main.go: %v", readErr)
	}
	mainSource := string(mainSourceBytes)
	for _, pattern := range []string{
		`mux.HandleFunc("/api/me/journal", handleJournalAPI(`,
		`mux.HandleFunc("/api/me/journal/{entryId}", handleJournalAPI(`,
		`mux.HandleFunc("/api/me/journal/{entryId}/like", handleJournalAPI(`,
		`mux.HandleFunc("/api/me/journal/album", handleJournalAPI(`,
	} {
		if !strings.Contains(mainSource, pattern) {
			t.Fatalf("journal route registration missing: %s", pattern)
		}
	}
}

func TestJournalAPIAuthMatrix(t *testing.T) {
	handler, token, _, _ := newJournalTestHarness(t)
	if got := doJournalRequest(t, handler, http.MethodGet, "/api/me/journal", "", ""); got.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GET = %d, want 401", got.Code)
	}
	if got := doJournalRequest(t, handler, http.MethodGet, "/api/me/journal", token, ""); got.Code != http.StatusOK {
		t.Fatalf("GET = %d", got.Code)
	}
	if got := doJournalRequest(t, handler, http.MethodPost, "/api/me/journal", token, ""); got.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST = %d, want 405", got.Code)
	}
}

// 端到端面：列表 → 点赞 → 赛季册 → 删除（隐私生命周期，物理删）。
func TestJournalAPIListLikeAlbumDelete(t *testing.T) {
	handler, token, subject, store := newJournalTestHarness(t)
	facts := journal.MatchFacts{
		MatchID: "m1", HomeTeam: "西班牙", AwayTeam: "德国", Score: "1-0",
		Goals: []string{"佩德里 25:00"}, Season: "2026",
	}
	if _, err := journal.Generate(context.Background(), store, nil, subject, facts, nil); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	list := doJournalRequest(t, handler, http.MethodGet, "/api/me/journal", token, "")
	if list.Code != http.StatusOK {
		t.Fatalf("list = %d", list.Code)
	}
	var decoded struct {
		Entries []struct {
			ID    string `json:"id"`
			Score string `json:"score"`
			Liked bool   `json:"liked"`
		} `json:"entries"`
	}
	if err := json.NewDecoder(list.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode list: %v", list.Body.String())
	}
	if len(decoded.Entries) != 1 || decoded.Entries[0].Score != "1-0" {
		t.Fatalf("entries = %+v", decoded.Entries)
	}
	entryID := decoded.Entries[0].ID

	liked := true
	likeBody, _ := json.Marshal(map[string]bool{"liked": liked})
	if got := doJournalRequest(t, handler, http.MethodPut, "/api/me/journal/"+entryID+"/like", token, string(likeBody)); got.Code != http.StatusOK {
		t.Fatalf("like = %d: %s", got.Code, got.Body.String())
	}
	again, _ := json.Marshal(map[string]bool{"liked": false})
	if got := doJournalRequest(t, handler, http.MethodPut, "/api/me/journal/"+entryID+"/like", token, string(again)); got.Code != http.StatusOK {
		t.Fatalf("unlike = %d", got.Code)
	}
	if got := doJournalRequest(t, handler, http.MethodPut, "/api/me/journal/unknown/like", token, string(likeBody)); got.Code != http.StatusNotFound {
		t.Fatalf("like unknown = %d, want 404", got.Code)
	}

	album := doJournalRequest(t, handler, http.MethodGet, "/api/me/journal/album?season=2026", token, "")
	if album.Code != http.StatusOK {
		t.Fatalf("album = %d", album.Code)
	}
	var albumDecoded struct {
		Album []journal.AlbumPage `json:"album"`
	}
	if err := json.NewDecoder(album.Body).Decode(&albumDecoded); err != nil {
		t.Fatalf("decode album: %v", album.Body.String())
	}
	if len(albumDecoded.Album) != 1 || albumDecoded.Album[0].MatchID != "m1" {
		t.Fatalf("album = %+v", albumDecoded.Album)
	}

	if got := doJournalRequest(t, handler, http.MethodDelete, "/api/me/journal/"+entryID, token, ""); got.Code != http.StatusOK {
		t.Fatalf("delete = %d", got.Code)
	}
	final := doJournalRequest(t, handler, http.MethodGet, "/api/me/journal", token, "")
	if strings.Contains(final.Body.String(), entryID) {
		t.Fatal("deleted entry must be gone (物理删)")
	}
}

// 无库部署降级：store nil = 空列表 200（页面静默隐藏）。
func TestJournalAPINilStoreDegradesToEmpty(t *testing.T) {
	manager, err := auth.NewManager(auth.NewMemoryStore(), "test-session-signing-key-0123456789")
	if err != nil {
		t.Fatal(err)
	}
	session, err := manager.IssueAnonymous(context.Background(), "device_journal_nil")
	if err != nil {
		t.Fatal(err)
	}
	handler := handleJournalAPI(manager, &config.Config{Environment: "development"}, nil, nil)
	got := doJournalRequest(t, handler, http.MethodGet, "/api/me/journal", session.AccessToken, "")
	if got.Code != http.StatusOK || !strings.Contains(got.Body.String(), `"entries":[]`) {
		t.Fatalf("nil store GET = %d %s, want 200 empty", got.Code, got.Body.String())
	}
}

// 手记生成挂点的素材门：0-0 且无事件 = 无素材不生成（防把空账本写成闷平）。
func TestJournalServiceMaterialGate(t *testing.T) {
	store := matchstate.NewStore()
	if _, _, err := store.SetConfig("m-empty", matchstate.MatchConfig{HomeTeam: "西班牙", AwayTeam: "德国"}); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	service := newJournalService(store, memory.NewQueue(memory.NewMemobase(memory.MemobaseConfig{}), nil, nil), &journal.MemoryStore{}, nil)
	if facts, _ := service.material(context.Background(), "user-1", "m-empty"); facts != nil {
		t.Fatalf("empty ledger must not produce facts, got %+v", facts)
	}
	if _, _, err := store.Create("m-empty", matchstate.MatchEvent{
		EventType: "goal", Period: "first_half", Clock: "25:00", TeamID: "home",
		TeamName: "西班牙", PlayerName: "佩德里", Score: matchstate.Score{Home: 1, Away: 0},
		Description: "佩德里推射破门。",
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	facts, portrait := service.material(context.Background(), "user-1", "m-empty")
	if facts == nil || facts.Score != "1-0" || len(facts.Goals) != 1 {
		t.Fatalf("facts = %+v, want ledger snapshot with goal", facts)
	}
	_ = portrait
}

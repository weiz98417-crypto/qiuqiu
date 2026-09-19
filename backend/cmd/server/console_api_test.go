package main

// Console API tests (ADR-0008): the React console is built against these
// exact JSON shapes, the operator scope matrix (auditor read-only), the
// operator-attributed audit rows, and the ADR-0008 dual-mode auth (operators
// rows present → table lookup only; no rows → legacy APP_TOKEN keeps director
// behavior so the pre-console operator evals stay green).

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"qiuqiu/internal/companion"
	"qiuqiu/internal/config"
	"qiuqiu/internal/conversation"
	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/memory"
	"qiuqiu/internal/operatorauth"
	"qiuqiu/internal/operatorwrite"
	"qiuqiu/internal/relationship"
)

const (
	consoleDirectorToken = "director-token-qiuqiu"
	consoleAuditorToken  = "auditor-token-qiuqiu"
	consoleDirectorName  = "阿琴"
	consoleAuditorName   = "小阅"
)

type consoleHarness struct {
	cfg         *config.Config
	store       *matchstate.Store
	traces      *companion.StoreMemoryTools
	sessions    *conversation.WatchSessionRegistry
	fakeThreads *memory.Fake
	overlays    *memory.MemoryPortraitOverlays
	queue       *memory.Queue
	operators   *operatorauth.MemoryStore
	ring        *interruptionRing
	console     http.HandlerFunc
	matchAPI    http.HandlerFunc
}

func newConsoleHarness(t *testing.T) *consoleHarness {
	t.Helper()
	store := matchstate.NewStore()
	harness := &consoleHarness{
		cfg:         &config.Config{Environment: "development", AppToken: "qiuqiu-dev-token"},
		store:       store,
		traces:      companion.NewStoreMemoryTools(store),
		sessions:    conversation.NewWatchSessionRegistry(context.Background(), conversation.Config{}),
		fakeThreads: memory.NewFake(),
		overlays:    memory.NewMemoryPortraitOverlays(),
		operators:   operatorauth.NewMemoryStore(),
		ring:        newInterruptionRing(),
	}
	harness.queue = memory.NewQueue(memory.NewMemobase(memory.MemobaseConfig{}), nil, nil,
		memory.WithThreads(harness.fakeThreads), memory.WithPortraitOverlays(harness.overlays))
	authz := newOperatorAuthz(harness.cfg, harness.operators)
	writes := operatorwrite.NewMemoryService()
	harness.console = handleConsoleAPI(consoleAPI{
		cfg: harness.cfg, authz: authz, matches: harness.store, traces: harness.traces,
		sessions: harness.sessions, memories: harness.queue, operators: harness.operators,
		writes: writes, interruptions: harness.ring,
	})
	harness.matchAPI = handleMatchAPIWithOperatorAuth(harness.store, harness.traces, harness.traces, harness.cfg, nil,
		nil, nil, nil, authz, writes)
	return harness
}

// seedOperators switches the harness into ADR-0008 operators mode: rows
// exist, so only table lookup applies and roles decide the scopes.
func (h *consoleHarness) seedOperators(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	if _, err := h.operators.Seed(ctx, consoleDirectorName, consoleDirectorToken, operatorauth.RoleDirector); err != nil {
		t.Fatalf("seed director: %v", err)
	}
	if _, err := h.operators.Seed(ctx, consoleAuditorName, consoleAuditorToken, operatorauth.RoleAuditor); err != nil {
		t.Fatalf("seed auditor: %v", err)
	}
}

func (h *consoleHarness) seedLiveMatch(t *testing.T) {
	t.Helper()
	if _, _, err := h.store.SetConfig("m1", matchstate.MatchConfig{
		HomeTeam: "西班牙", AwayTeam: "德国", Lifecycle: matchstate.LifecycleLive,
	}); err != nil {
		t.Fatalf("seed match: %v", err)
	}
}

type consoleRequest struct {
	method        string
	path          string
	body          string
	token         string
	idempotencyKey string
	handler       http.Handler
}

func doConsoleRequest(t *testing.T, request consoleRequest) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if request.body != "" {
		reader = strings.NewReader(request.body)
	}
	httpRequest := httptest.NewRequest(request.method, request.path, reader)
	if request.token != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+request.token)
	}
	if request.idempotencyKey != "" {
		httpRequest.Header.Set("Idempotency-Key", request.idempotencyKey)
	}
	recorder := httptest.NewRecorder()
	request.handler.ServeHTTP(recorder, httpRequest)
	return recorder
}

func decodeConsoleJSON(t *testing.T, recorder *httptest.ResponseRecorder, destination any) {
	t.Helper()
	if err := json.Unmarshal(recorder.Body.Bytes(), destination); err != nil {
		t.Fatalf("decode %s: %v", recorder.Body.String(), err)
	}
}

func TestConsoleOverviewShape(t *testing.T) {
	harness := newConsoleHarness(t)
	harness.seedOperators(t)
	harness.seedLiveMatch(t)
	harness.sessions.Acquire("user-1", "m1")

	now := time.Now().UTC()
	_, _ = harness.fakeThreads.AppendThread(context.Background(), memory.Thread{UserID: "user-1", Kind: memory.ThreadUnansweredQuestion, Content: "谁助攻的？"})
	_, _ = harness.fakeThreads.AppendThread(context.Background(), memory.Thread{UserID: "user-2", Kind: memory.ThreadPromise, Content: "两天前的问题", CreatedAt: now.Add(-48 * time.Hour)})
	_, _ = harness.fakeThreads.AppendThread(context.Background(), memory.Thread{UserID: "user-3", Kind: memory.ThreadPrediction, Content: "上周的预测", CreatedAt: now.Add(-120 * time.Hour)})

	if err := harness.traces.WriteTrace(context.Background(), companion.Trace{
		ID: "trace-proactive", MatchID: "m1", UserID: "user-1", CreatedAt: now,
		RelationshipDecision: &relationship.Decision{ReasonCodes: []string{"relationship_decision", "proactive_citation:open_thread:7"}},
	}); err != nil {
		t.Fatalf("write proactive trace: %v", err)
	}
	if err := harness.traces.WriteTrace(context.Background(), companion.Trace{
		ID: "trace-plain", MatchID: "m1", UserID: "user-1", CreatedAt: now.Add(time.Second),
		RelationshipDecision: &relationship.Decision{ReasonCodes: []string{"relationship_decision"}},
	}); err != nil {
		t.Fatalf("write plain trace: %v", err)
	}
	if err := harness.operators.AppendAudit(context.Background(), consoleDirectorName, "thread.address", "thread:1"); err != nil {
		t.Fatalf("append audit: %v", err)
	}

	recorder := doConsoleRequest(t, consoleRequest{method: http.MethodGet, path: "/api/console/overview", token: consoleDirectorToken, handler: harness.console})
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET overview = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Matches         []consoleMatch              `json:"matches"`
		OnlineSessions  int                         `json:"onlineSessions"`
		Memory          consoleMemoryHealth         `json:"memory"`
		ThreadAging     consoleThreadAging          `json:"threadAging"`
		RecentProactive []consoleProactiveCitation  `json:"recentProactive"`
	}
	decodeConsoleJSON(t, recorder, &payload)

	if len(payload.Matches) != 1 {
		t.Fatalf("matches = %+v, want the seeded live match", payload.Matches)
	}
	if payload.Matches[0].MatchID != "m1" || payload.Matches[0].State != "live" || payload.Matches[0].OnlineUsers != 1 {
		t.Fatalf("match cell = %+v, want m1/live/1 online user", payload.Matches[0])
	}
	if payload.OnlineSessions != 1 {
		t.Fatalf("onlineSessions = %d, want 1", payload.OnlineSessions)
	}
	if !payload.Memory.Degraded {
		t.Fatal("memory cell should report degraded while the Memobase adapter is unconfigured")
	}
	if payload.Memory.BacklogDepth != 0 {
		t.Fatalf("backlogDepth = %d, want 0", payload.Memory.BacklogDepth)
	}
	if len(payload.Memory.RecentAudit) != 1 {
		t.Fatalf("recentAudit = %+v, want the operator audit tail", payload.Memory.RecentAudit)
	}
	audit := payload.Memory.RecentAudit[0]
	if audit.OperatorName != consoleDirectorName || audit.Action != "thread.address" || audit.Object != "thread:1" || audit.CreatedAt == "" {
		t.Fatalf("audit row = %+v, want operator-attributed thread.address row", audit)
	}
	if payload.ThreadAging.Today != 1 || payload.ThreadAging.D1to3 != 1 || payload.ThreadAging.D3plus != 1 {
		t.Fatalf("threadAging = %+v, want 1/1/1 buckets", payload.ThreadAging)
	}
	if len(payload.RecentProactive) != 1 {
		t.Fatalf("recentProactive = %+v, want only the cited trace", payload.RecentProactive)
	}
	proactive := payload.RecentProactive[0]
	if proactive.TraceID != "trace-proactive" || proactive.MatchID != "m1" || proactive.Citation != "open_thread:7" || proactive.CreatedAt == "" {
		t.Fatalf("proactive row = %+v", proactive)
	}

	if got := doConsoleRequest(t, consoleRequest{method: http.MethodGet, path: "/api/console/overview", handler: harness.console}); got.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated overview = %d, want 401", got.Code)
	}
	if got := doConsoleRequest(t, consoleRequest{method: http.MethodGet, path: "/api/console/overview", token: "wrong-token", handler: harness.console}); got.Code != http.StatusUnauthorized {
		t.Fatalf("invalid token overview = %d, want 401", got.Code)
	}
}

func TestConsoleMatchUsersAggregate(t *testing.T) {
	harness := newConsoleHarness(t)
	harness.seedOperators(t)
	harness.seedLiveMatch(t)
	harness.sessions.Acquire("user-1", "m1")
	harness.sessions.Acquire("user-2", "m1")
	harness.sessions.Release("user-2", "m1") // detached: visible but offline

	ctx := context.Background()
	_, _ = harness.fakeThreads.AppendThread(ctx, memory.Thread{UserID: "user-1", Kind: memory.ThreadUnansweredQuestion, Content: "问题一"})
	_, _ = harness.fakeThreads.AppendThread(ctx, memory.Thread{UserID: "user-1", Kind: memory.ThreadPromise, Content: "问题二"})
	if _, err := harness.overlays.Put(ctx, "user-1", "basic_info", "favorite_team", "皇马"); err != nil {
		t.Fatalf("put overlay: %v", err)
	}

	recorder := doConsoleRequest(t, consoleRequest{method: http.MethodGet, path: "/api/console/matches/m1/users", token: consoleDirectorToken, handler: harness.console})
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET users = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Users []consoleUser `json:"users"`
	}
	decodeConsoleJSON(t, recorder, &payload)
	if len(payload.Users) != 2 {
		t.Fatalf("users = %+v, want both registry users", payload.Users)
	}
	first := payload.Users[0]
	if first.UserID != "user-1" || !first.Online || first.OpenThreads != 2 || first.Talkativeness != "normal" || first.PortraitUpdatedAt == "" {
		t.Fatalf("user row = %+v, want online user-1 with 2 open threads, normal tier and portrait stamp", first)
	}
	second := payload.Users[1]
	if second.UserID != "user-2" || second.Online {
		t.Fatalf("user row = %+v, want offline user-2", second)
	}

	if got := doConsoleRequest(t, consoleRequest{method: http.MethodGet, path: "/api/console/matches/m1/users", token: consoleAuditorToken, handler: harness.console}); got.Code != http.StatusOK {
		t.Fatalf("auditor read = %d, want 200", got.Code)
	}
	if got := doConsoleRequest(t, consoleRequest{method: http.MethodGet, path: "/api/console/matches/m1/users", handler: harness.console}); got.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated users = %d, want 401", got.Code)
	}
}

func TestConsoleThreadsListAddressExpireAndAudit(t *testing.T) {
	harness := newConsoleHarness(t)
	harness.seedOperators(t)
	ctx := context.Background()
	first, err := harness.queue.AppendThread(ctx, memory.Thread{UserID: "user-1", Kind: memory.ThreadUnansweredQuestion, Content: "谁助攻的？"})
	if err != nil {
		t.Fatalf("append first: %v", err)
	}
	second, err := harness.queue.AppendThread(ctx, memory.Thread{UserID: "user-2", Kind: memory.ThreadPrediction, Content: "预测下半场"})
	if err != nil {
		t.Fatalf("append second: %v", err)
	}

	list := doConsoleRequest(t, consoleRequest{method: http.MethodGet, path: "/api/console/threads?state=open", token: consoleDirectorToken, handler: harness.console})
	if list.Code != http.StatusOK {
		t.Fatalf("GET threads = %d body=%s", list.Code, list.Body.String())
	}
	var listed struct {
		Threads []consoleThread `json:"threads"`
	}
	decodeConsoleJSON(t, list, &listed)
	if len(listed.Threads) != 2 || listed.Threads[0].ID != first.ID || listed.Threads[1].UserID != "user-2" {
		t.Fatalf("threads = %+v, want both rows oldest first", listed.Threads)
	}
	if listed.Threads[0].LedgerSequence != 0 || listed.Threads[0].CreatedAt == "" || listed.Threads[0].Kind != "unanswered_question" {
		t.Fatalf("thread row shape = %+v", listed.Threads[0])
	}

	filtered := doConsoleRequest(t, consoleRequest{method: http.MethodGet, path: "/api/console/threads?userId=user-1&state=open", token: consoleAuditorToken, handler: harness.console})
	var filteredList struct {
		Threads []consoleThread `json:"threads"`
	}
	decodeConsoleJSON(t, filtered, &filteredList)
	if len(filteredList.Threads) != 1 || filteredList.Threads[0].UserID != "user-1" {
		t.Fatalf("filtered threads = %+v, want only user-1", filteredList.Threads)
	}

	addressed := doConsoleRequest(t, consoleRequest{
		method: http.MethodPatch, path: "/api/console/threads/" + first.ID, body: `{"action":"address"}`,
		token: consoleDirectorToken, idempotencyKey: "console-address-1", handler: harness.console,
	})
	if addressed.Code != http.StatusOK {
		t.Fatalf("PATCH address = %d body=%s", addressed.Code, addressed.Body.String())
	}
	var addressPayload struct {
		Thread consoleThread `json:"thread"`
	}
	decodeConsoleJSON(t, addressed, &addressPayload)
	if addressPayload.Thread.ID != first.ID || addressPayload.Thread.State != "addressed" {
		t.Fatalf("addressed thread = %+v, want state addressed", addressPayload.Thread)
	}

	expired := doConsoleRequest(t, consoleRequest{
		method: http.MethodPatch, path: "/api/console/threads/" + second.ID, body: `{"action":"expire"}`,
		token: consoleDirectorToken, idempotencyKey: "console-expire-1", handler: harness.console,
	})
	if expired.Code != http.StatusOK {
		t.Fatalf("PATCH expire = %d body=%s", expired.Code, expired.Body.String())
	}
	var expirePayload struct {
		Thread consoleThread `json:"thread"`
	}
	decodeConsoleJSON(t, expired, &expirePayload)
	if expirePayload.Thread.State != "expired" {
		t.Fatalf("expired thread = %+v", expirePayload.Thread)
	}

	// Every console write is attributable: audit rows carry the operator name.
	audit, err := harness.operators.RecentAudit(ctx, 10)
	if err != nil {
		t.Fatalf("recent audit: %v", err)
	}
	if len(audit) != 2 {
		t.Fatalf("audit rows = %+v, want one per thread write", audit)
	}
	if audit[1].OperatorName != consoleDirectorName || audit[1].Action != "thread.address" || audit[1].Object != "thread:"+first.ID {
		t.Fatalf("address audit row = %+v", audit[1])
	}
	if audit[0].OperatorName != consoleDirectorName || audit[0].Action != "thread.expire" || audit[0].Object != "thread:"+second.ID {
		t.Fatalf("expire audit row = %+v", audit[0])
	}

	if got := doConsoleRequest(t, consoleRequest{
		method: http.MethodPatch, path: "/api/console/threads/" + second.ID, body: `{"action":"reopen"}`,
		token: consoleDirectorToken, idempotencyKey: "console-bad-1", handler: harness.console,
	}); got.Code != http.StatusBadRequest {
		t.Fatalf("PATCH unknown action = %d, want 400", got.Code)
	}
	if got := doConsoleRequest(t, consoleRequest{
		method: http.MethodPatch, path: "/api/console/threads/9999", body: `{"action":"address"}`,
		token: consoleDirectorToken, idempotencyKey: "console-missing-1", handler: harness.console,
	}); got.Code != http.StatusNotFound {
		t.Fatalf("PATCH unknown thread = %d, want 404", got.Code)
	}
	if got := doConsoleRequest(t, consoleRequest{
		method: http.MethodPatch, path: "/api/console/threads/" + second.ID, body: `{"action":"address"}`,
		token: consoleAuditorToken, idempotencyKey: "console-auditor-1", handler: harness.console,
	}); got.Code != http.StatusForbidden {
		t.Fatalf("auditor PATCH thread = %d, want 403", got.Code)
	}
}

func TestConsolePortraitOnBehalfDeleteHonoredOnNextTurn(t *testing.T) {
	harness := newConsoleHarness(t)
	harness.seedOperators(t)
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.ReadAll(r.Body)
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/users/") {
			_, _ = w.Write([]byte(`{"errno":0,"errmsg":"success","data":{"profiles":[{"id":"prof-team","content":"皇马","attributes":{"topic":"basic_info","sub_topic":"favorite_team"},"updated_at":"2026-09-01T03:04:05Z"}]}}`))
			return
		}
		_, _ = w.Write([]byte(`{"errno":0,"errmsg":"success","data":null}`))
	}))
	defer stub.Close()
	queue := memory.NewQueue(memory.NewMemobase(memory.MemobaseConfig{BaseURL: stub.URL, Token: "console-test"}), nil, nil,
		memory.WithThreads(memory.NewFake()), memory.WithPortraitOverlays(memory.NewMemoryPortraitOverlays()))
	console := handleConsoleAPI(consoleAPI{
		cfg: harness.cfg, authz: newOperatorAuthz(harness.cfg, harness.operators), matches: harness.store,
		traces: harness.traces, sessions: harness.sessions, memories: queue, operators: harness.operators,
		writes: operatorwrite.NewMemoryService(), interruptions: harness.ring,
	})

	// The synthesis slot the next turn would still see before the delete.
	before := doConsoleRequest(t, consoleRequest{method: http.MethodGet, path: "/api/console/users/u-portrait/portrait", token: consoleDirectorToken, handler: console})
	if before.Code != http.StatusOK {
		t.Fatalf("GET portrait = %d body=%s", before.Code, before.Body.String())
	}
	var portraitBefore consolePortrait
	decodeConsoleJSON(t, before, &portraitBefore)
	if len(portraitBefore.Entries) != 1 || portraitBefore.Entries[0].SubTopic != "favorite_team" || portraitBefore.Entries[0].Content != "皇马" {
		t.Fatalf("portrait before delete = %+v", portraitBefore)
	}

	deleted := doConsoleRequest(t, consoleRequest{
		method: http.MethodDelete, path: "/api/console/users/u-portrait/portrait?topic=basic_info&subTopic=favorite_team",
		token: consoleDirectorToken, handler: console,
	})
	if deleted.Code != http.StatusOK {
		t.Fatalf("DELETE portrait = %d body=%s", deleted.Code, deleted.Body.String())
	}
	var empty map[string]any
	decodeConsoleJSON(t, deleted, &empty)
	if len(empty) != 0 {
		t.Fatalf("DELETE payload = %+v, want {}", empty)
	}

	// The tombstone is honored on the very next turn: the assembled portrait
	// (the same read the realization prompt receives) no longer contains the
	// deleted fact.
	after, err := queue.Portrait(context.Background(), "u-portrait")
	if err == nil && strings.Contains(after.Block, "皇马") {
		t.Fatalf("portrait block after delete = %q, want the slot forgotten", after.Block)
	}
	entries, _ := queue.PortraitEntries(context.Background(), "u-portrait")
	if len(entries) != 0 {
		t.Fatalf("portrait entries after delete = %+v, want none", entries)
	}

	// On-behalf privacy-ops are attributable (ADR-0008 red line).
	audit, err := harness.operators.RecentAudit(context.Background(), 5)
	if err != nil {
		t.Fatalf("recent audit: %v", err)
	}
	if len(audit) != 1 || audit[0].OperatorName != consoleDirectorName || audit[0].Action != "portrait.delete" ||
		audit[0].Object != "user:u-portrait slot basic_info/favorite_team" {
		t.Fatalf("portrait audit rows = %+v", audit)
	}

	if got := doConsoleRequest(t, consoleRequest{
		method: http.MethodDelete, path: "/api/console/users/u-portrait/portrait?topic=basic_info&subTopic=favorite_team",
		token: consoleAuditorToken, handler: console,
	}); got.Code != http.StatusForbidden {
		t.Fatalf("auditor DELETE portrait = %d, want 403", got.Code)
	}
	if got := doConsoleRequest(t, consoleRequest{
		method: http.MethodDelete, path: "/api/console/users/u-portrait/portrait?topic=basic_info",
		token: consoleDirectorToken, handler: console,
	}); got.Code != http.StatusBadRequest {
		t.Fatalf("DELETE without subTopic = %d, want 400", got.Code)
	}
}

func TestConsoleDeliveryInterruptionsRing(t *testing.T) {
	harness := newConsoleHarness(t)
	harness.seedOperators(t)

	guard := &interruptedReactionGuard{emitted: make(map[string]struct{}), ring: harness.ring}
	guard.emit(fakePresentationSink{}, companion.Trace{ID: "trace-int", MatchID: "m1"}, relationship.AffectState{})
	// Once per interruption: a second emission must not duplicate the row.
	guard.emit(fakePresentationSink{}, companion.Trace{ID: "trace-int", MatchID: "m1"}, relationship.AffectState{})

	recorder := doConsoleRequest(t, consoleRequest{method: http.MethodGet, path: "/api/console/delivery-interruptions", token: consoleDirectorToken, handler: harness.console})
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET interruptions = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Recent []struct {
			TraceID string `json:"traceId"`
			MatchID string `json:"matchId"`
			At      string `json:"at"`
		} `json:"recent"`
	}
	decodeConsoleJSON(t, recorder, &payload)
	if len(payload.Recent) != 1 || payload.Recent[0].TraceID != "trace-int" || payload.Recent[0].MatchID != "m1" || payload.Recent[0].At == "" {
		t.Fatalf("recent = %+v, want the single interrupted reaction", payload.Recent)
	}

	for i := 0; i < 25; i++ {
		harness.ring.Record("trace-ring-"+string(rune('a'+i)), "m1", time.Now().UTC())
	}
	capped := doConsoleRequest(t, consoleRequest{method: http.MethodGet, path: "/api/console/delivery-interruptions", token: consoleDirectorToken, handler: harness.console})
	decodeConsoleJSON(t, capped, &payload)
	if len(payload.Recent) != 20 {
		t.Fatalf("recent after flood = %d rows, want the last 20", len(payload.Recent))
	}
	if got := doConsoleRequest(t, consoleRequest{method: http.MethodGet, path: "/api/console/delivery-interruptions", token: consoleAuditorToken, handler: harness.console}); got.Code != http.StatusOK {
		t.Fatalf("auditor interruptions = %d, want 200", got.Code)
	}
	if got := doConsoleRequest(t, consoleRequest{method: http.MethodGet, path: "/api/console/delivery-interruptions", handler: harness.console}); got.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated interruptions = %d, want 401", got.Code)
	}
}

type fakePresentationSink struct{}

func (fakePresentationSink) SendJSON(msg interface{}) error { return nil }

func TestConsoleScopeMatrixOnMatchRoutes(t *testing.T) {
	harness := newConsoleHarness(t)
	harness.seedOperators(t)
	harness.seedLiveMatch(t)

	// Auditor: every read stays 200, every write class 403s.
	for _, path := range []string{"/api/matches/m1/traces", "/api/matches/m1/events"} {
		if got := doConsoleRequest(t, consoleRequest{method: http.MethodGet, path: path, token: consoleAuditorToken, handler: harness.matchAPI}); got.Code != http.StatusOK {
			t.Fatalf("auditor GET %s = %d body=%s", path, got.Code, got.Body.String())
		}
	}
	for _, request := range []consoleRequest{
		{method: http.MethodPost, path: "/api/matches/m1/automation", body: `{"mode":"active","cooldownSeconds":30}`},
		{method: http.MethodPost, path: "/api/matches/m1/config", body: `{"homeTeam":"西班牙","awayTeam":"德国"}`},
		{method: http.MethodPost, path: "/api/matches/m1/facts/fact-1/confirm"},
		{method: http.MethodPost, path: "/api/matches/m1/conflicts/c-1/resolve", body: `{"chosenFactId":"fact-1"}`},
		{method: http.MethodPost, path: "/api/matches/m1/events/evt-1/correct", body: `{"evidence":{"correctionReason":"更正"},"eventType":"goal"}`},
		{method: http.MethodPost, path: "/api/matches/m1/lifecycle", body: `{"lifecycle":"live"}`},
	} {
		request.token = consoleAuditorToken
		request.handler = harness.matchAPI
		if got := doConsoleRequest(t, request); got.Code != http.StatusForbidden {
			t.Fatalf("auditor %s %s = %d body=%s, want 403", request.method, request.path, got.Code, got.Body.String())
		}
	}

	// Director (all scopes) passes the same write gate.
	if got := doConsoleRequest(t, consoleRequest{
		method: http.MethodPost, path: "/api/matches/m1/automation", body: `{"mode":"paused","cooldownSeconds":60}`,
		token: consoleDirectorToken, idempotencyKey: "scope-automation-1", handler: harness.matchAPI,
	}); got.Code != http.StatusOK {
		t.Fatalf("director POST automation = %d body=%s, want 200", got.Code, got.Body.String())
	}
	if got := doConsoleRequest(t, consoleRequest{
		method: http.MethodPost, path: "/api/matches/m1/facts/fact-1/confirm",
		token: consoleDirectorToken, idempotencyKey: "scope-confirm-1", handler: harness.matchAPI,
	}); got.Code == http.StatusForbidden || got.Code == http.StatusUnauthorized {
		t.Fatalf("director POST facts confirm = %d, want past the scope gate", got.Code)
	}
	if got := doConsoleRequest(t, consoleRequest{
		method: http.MethodPost, path: "/api/matches/m1/events/evt-1/correct", body: `{"evidence":{"correctionReason":"更正"},"eventType":"goal"}`,
		token: consoleDirectorToken, idempotencyKey: "scope-correct-1", handler: harness.matchAPI,
	}); got.Code == http.StatusForbidden || got.Code == http.StatusUnauthorized {
		t.Fatalf("director POST events correct = %d, want past the scope gate", got.Code)
	}

	// Unauthenticated protected reads 401; invalid tokens 401 in operators mode.
	if got := doConsoleRequest(t, consoleRequest{method: http.MethodGet, path: "/api/matches/m1/traces", handler: harness.matchAPI}); got.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated traces = %d, want 401", got.Code)
	}
	if got := doConsoleRequest(t, consoleRequest{method: http.MethodGet, path: "/api/matches/m1/traces", token: "wrong", handler: harness.matchAPI}); got.Code != http.StatusUnauthorized {
		t.Fatalf("invalid token traces = %d, want 401", got.Code)
	}

	// Public-degradable reads stay public for anonymous clients (the C-end
	// app shares them) while a scoped operator gets the enriched view.
	if got := doConsoleRequest(t, consoleRequest{method: http.MethodGet, path: "/api/matches/m1/events", handler: harness.matchAPI}); got.Code != http.StatusOK {
		t.Fatalf("anonymous events = %d, want 200 public view", got.Code)
	}
}

func TestConsoleLegacyTokenModeKeepsDirector(t *testing.T) {
	harness := newConsoleHarness(t) // no operators rows: ADR-0008 legacy mode
	harness.seedLiveMatch(t)
	ctx := context.Background()
	thread, err := harness.queue.AppendThread(ctx, memory.Thread{UserID: "user-1", Kind: memory.ThreadUnansweredQuestion, Content: "谁助攻的？"})
	if err != nil {
		t.Fatalf("append thread: %v", err)
	}

	// APP_TOKEN keeps working with full director scopes.
	if got := doConsoleRequest(t, consoleRequest{method: http.MethodGet, path: "/api/console/overview", token: harness.cfg.AppToken, handler: harness.console}); got.Code != http.StatusOK {
		t.Fatalf("legacy overview with APP_TOKEN = %d body=%s", got.Code, got.Body.String())
	}
	if got := doConsoleRequest(t, consoleRequest{
		method: http.MethodPatch, path: "/api/console/threads/" + thread.ID, body: `{"action":"address"}`,
		token: harness.cfg.AppToken, idempotencyKey: "legacy-address-1", handler: harness.console,
	}); got.Code != http.StatusOK {
		t.Fatalf("legacy PATCH thread with APP_TOKEN = %d body=%s, want director write", got.Code, got.Body.String())
	}
	audit, err := harness.operators.RecentAudit(ctx, 5)
	if err != nil || len(audit) != 1 || audit[0].OperatorName != "default" || audit[0].Action != "thread.address" {
		t.Fatalf("legacy audit rows = %+v err=%v, want operator name default", audit, err)
	}
	if got := doConsoleRequest(t, consoleRequest{
		method: http.MethodPost, path: "/api/matches/m1/automation", body: `{"mode":"active","cooldownSeconds":30}`,
		token: harness.cfg.AppToken, idempotencyKey: "legacy-automation-1", handler: harness.matchAPI,
	}); got.Code != http.StatusOK {
		t.Fatalf("legacy POST automation with APP_TOKEN = %d body=%s, want 200", got.Code, got.Body.String())
	}

	// Missing/invalid tokens are 401 exactly as before.
	if got := doConsoleRequest(t, consoleRequest{method: http.MethodGet, path: "/api/console/overview", handler: harness.console}); got.Code != http.StatusUnauthorized {
		t.Fatalf("legacy overview without token = %d, want 401", got.Code)
	}
	if got := doConsoleRequest(t, consoleRequest{method: http.MethodGet, path: "/api/console/overview", token: "not-the-token", handler: harness.console}); got.Code != http.StatusUnauthorized {
		t.Fatalf("legacy overview wrong token = %d, want 401", got.Code)
	}

	// Non-production dev bypass (empty APP_TOKEN) keeps local dev alive.
	devCfg := &config.Config{Environment: "development"}
	devAuthz := newOperatorAuthz(devCfg, operatorauth.NewMemoryStore())
	devConsole := handleConsoleAPI(consoleAPI{
		cfg: devCfg, authz: devAuthz, matches: harness.store, traces: harness.traces, sessions: harness.sessions,
		memories: harness.queue, operators: harness.operators, writes: operatorwrite.NewMemoryService(),
		interruptions: harness.ring,
	})
	if got := doConsoleRequest(t, consoleRequest{method: http.MethodGet, path: "/api/console/overview", handler: devConsole}); got.Code != http.StatusOK {
		t.Fatalf("dev bypass overview = %d body=%s, want 200", got.Code, got.Body.String())
	}

	// Production with an empty APP_TOKEN has no bypass.
	prodCfg := &config.Config{Environment: "production"}
	prodAuthz := newOperatorAuthz(prodCfg, operatorauth.NewMemoryStore())
	prodConsole := handleConsoleAPI(consoleAPI{
		cfg: prodCfg, authz: prodAuthz, matches: harness.store, traces: harness.traces, sessions: harness.sessions,
		memories: harness.queue, operators: harness.operators, writes: operatorwrite.NewMemoryService(),
		interruptions: harness.ring,
	})
	if got := doConsoleRequest(t, consoleRequest{method: http.MethodGet, path: "/api/console/overview", handler: prodConsole}); got.Code != http.StatusUnauthorized {
		t.Fatalf("production no-bypass overview = %d, want 401", got.Code)
	}
}

func TestConsoleTracesCitationFilter(t *testing.T) {
	harness := newConsoleHarness(t)
	harness.seedOperators(t)
	harness.seedLiveMatch(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := harness.traces.WriteTrace(ctx, companion.Trace{
		ID: "trace-cite-open", MatchID: "m1", UserID: "user-1", CreatedAt: now,
		RelationshipDecision: &relationship.Decision{ReasonCodes: []string{"proactive_citation:open_thread:7"}},
	}); err != nil {
		t.Fatalf("write cited trace: %v", err)
	}
	if err := harness.traces.WriteTrace(ctx, companion.Trace{
		ID: "trace-cite-goal", MatchID: "m1", UserID: "user-1", CreatedAt: now.Add(time.Second),
		RelationshipDecision: &relationship.Decision{ReasonCodes: []string{"proactive_citation:event:goal-1"}},
	}); err != nil {
		t.Fatalf("write goal-cited trace: %v", err)
	}
	if err := harness.traces.WriteTrace(ctx, companion.Trace{
		ID: "trace-plain", MatchID: "m1", UserID: "user-1", CreatedAt: now.Add(2 * time.Second),
	}); err != nil {
		t.Fatalf("write plain trace: %v", err)
	}

	unfiltered := doConsoleRequest(t, consoleRequest{method: http.MethodGet, path: "/api/matches/m1/traces", token: consoleDirectorToken, handler: harness.matchAPI})
	var all struct {
		Traces []companion.Trace `json:"traces"`
	}
	decodeConsoleJSON(t, unfiltered, &all)
	if len(all.Traces) != 3 {
		t.Fatalf("traces = %d, want 3 before filtering", len(all.Traces))
	}

	filtered := doConsoleRequest(t, consoleRequest{method: http.MethodGet, path: "/api/matches/m1/traces?citation=open_thread", token: consoleDirectorToken, handler: harness.matchAPI})
	decodeConsoleJSON(t, filtered, &all)
	if len(all.Traces) != 1 || all.Traces[0].ID != "trace-cite-open" {
		t.Fatalf("open_thread-filtered traces = %+v", all.Traces)
	}

	eventFiltered := doConsoleRequest(t, consoleRequest{method: http.MethodGet, path: "/api/matches/m1/traces?citation=event", token: consoleDirectorToken, handler: harness.matchAPI})
	decodeConsoleJSON(t, eventFiltered, &all)
	if len(all.Traces) != 1 || all.Traces[0].ID != "trace-cite-goal" {
		t.Fatalf("event-filtered traces = %+v", all.Traces)
	}

	none := doConsoleRequest(t, consoleRequest{method: http.MethodGet, path: "/api/matches/m1/traces?citation=no-match", token: consoleDirectorToken, handler: harness.matchAPI})
	decodeConsoleJSON(t, none, &all)
	if len(all.Traces) != 0 {
		t.Fatalf("no-match filter = %+v, want empty", all.Traces)
	}
}

func TestConsoleOperatorsManagement(t *testing.T) {
	h := newConsoleHarness(t)
	h.seedOperators(t)

	// Auditor cannot manage operators.
	rec := doConsoleRequest(t, consoleRequest{method: http.MethodPost, path: "/api/console/operators", body: `{"name":"新人","role":"director"}`, token: consoleAuditorToken, handler: h.console})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("auditor create = %d, want 403", rec.Code)
	}

	// Director creates an operator; the plaintext token is returned exactly once.
	rec = doConsoleRequest(t, consoleRequest{method: http.MethodPost, path: "/api/console/operators", body: `{"name":"新人","role":"auditor"}`, token: consoleDirectorToken, handler: h.console})
	if rec.Code != http.StatusOK {
		t.Fatalf("create = %d %s, want 200", rec.Code, rec.Body.String())
	}
	var created struct {
		Operator operatorauth.Operator `json:"operator"`
		Token    string                `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.Token == "" || created.Operator.Name != "新人" || created.Operator.Role != operatorauth.RoleAuditor {
		t.Fatalf("created = %+v token=%q, want operator 新人/auditor with a token", created.Operator, created.Token)
	}
	if _, ok := h.operators.Lookup(context.Background(), created.Token); !ok {
		t.Fatal("created token does not authenticate")
	}

	// Duplicate name fails with 400.
	rec = doConsoleRequest(t, consoleRequest{method: http.MethodPost, path: "/api/console/operators", body: `{"name":"新人","role":"auditor"}`, token: consoleDirectorToken, handler: h.console})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("duplicate create = %d, want 400", rec.Code)
	}

	// List contains every operator and never the plaintext token.
	rec = doConsoleRequest(t, consoleRequest{method: http.MethodGet, path: "/api/console/operators", token: consoleDirectorToken, handler: h.console})
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), created.Token) {
		t.Fatal("list leaks the plaintext token")
	}

	// The created token authenticates reads (auditor scope) ...
	rec = doConsoleRequest(t, consoleRequest{method: http.MethodGet, path: "/api/console/overview", token: created.Token, handler: h.console})
	if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
		t.Fatalf("created auditor overview = %d, want 200", rec.Code)
	}

	// Director revokes; the token fails immediately and the audit trail
	// carries both actions attributed to the director.
	rec = doConsoleRequest(t, consoleRequest{method: http.MethodDelete, path: "/api/console/operators/" + created.Operator.Name, token: consoleDirectorToken, handler: h.console})
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke = %d", rec.Code)
	}
	if _, ok := h.operators.Lookup(context.Background(), created.Token); ok {
		t.Fatal("revoked token still authenticates")
	}
	audit, err := h.operators.RecentAudit(context.Background(), 10)
	if err != nil {
		t.Fatalf("RecentAudit: %v", err)
	}
	joined := make([]string, 0, len(audit))
	for _, entry := range audit {
		joined = append(joined, entry.OperatorName+":"+entry.Action)
	}
	joinedAll := strings.Join(joined, ",")
	if !strings.Contains(joinedAll, consoleDirectorName+":operator.create") || !strings.Contains(joinedAll, consoleDirectorName+":operator.revoke") {
		t.Fatalf("audit = %v, want director-attributed create and revoke", joined)
	}
}

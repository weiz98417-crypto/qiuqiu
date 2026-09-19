package main

// Console↔前端契约锁（openspec/changes/console-contract-goldens）：
// console 消费的每个端点的 JSON wire 形状以 golden 快照锁定 —— handler 即
// oracle。后端改名/改结构在这里先红，前端手抄的 TS 类型有唯一真源可对；
// operator.html 退役（ADR-0011）不带走这把锁。
//
// 时间戳一律归一化为 <TS>（形状归形状，值归值）。再生成：
//
//	UPDATE_GOLDEN=1 go test ./cmd/server -run TestConsoleGolden

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
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

var goldenTimestampRe = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?Z`)

// normalizeGolden 把所有 RFC3339 时间戳归一成 <TS>，golden 只锁形状。
func normalizeGolden(body []byte) []byte {
	return goldenTimestampRe.ReplaceAll(body, []byte("<TS>"))
}

// assertGolden 对比（或以 UPDATE_GOLDEN=1 再生成）一份归一化后的响应快照。
func assertGolden(t *testing.T, name string, body []byte) {
	t.Helper()
	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("golden %s: response is not JSON: %v\n%s", name, err, body)
	}
	pretty, err := json.MarshalIndent(decoded, "", "  ")
	if err != nil {
		t.Fatalf("golden %s: re-marshal: %v", name, err)
	}
	normalized := normalizeGolden(pretty)

	path := filepath.Join("testdata", "goldens", name+".json")
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("golden %s: mkdir: %v", name, err)
		}
		if err := os.WriteFile(path, append(normalized, '\n'), 0o644); err != nil {
			t.Fatalf("golden %s: write: %v", name, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden %s missing — run UPDATE_GOLDEN=1 go test ./cmd/server -run TestConsoleGolden to seed it: %v", name, err)
	}
	if !bytes.Equal(bytes.TrimRight(want, "\n"), normalized) {
		t.Fatalf("golden %s drifted — wire shape changed; update the console TS types (console/src/api/client.ts) and re-lock with UPDATE_GOLDEN=1 if intended:\n--- want ---\n%s\n--- got ---\n%s", name, want, normalized)
	}
}

// newGoldenHarness 与 newConsoleHarness 同构，但画像走固定 Memobase stub
// （固定 updated_at → portrait golden 确定性），并富种子比赛/事件/时钟。
func newGoldenHarness(t *testing.T) *consoleHarness {
	t.Helper()
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.ReadAll(r.Body)
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/users/") {
			_, _ = w.Write([]byte(`{"errno":0,"errmsg":"success","data":{"profiles":[{"id":"prof-team","content":"皇马","attributes":{"topic":"basic_info","sub_topic":"favorite_team"},"updated_at":"2026-09-01T03:04:05Z"},{"id":"prof-mood","content":"输球后需要安抚","attributes":{"topic":"emotional","sub_topic":"after_loss"},"updated_at":"2026-09-05T03:04:05Z"}]}}`))
			return
		}
		_, _ = w.Write([]byte(`{"errno":0,"errmsg":"success","data":null}`))
	}))
	t.Cleanup(stub.Close)

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
	harness.queue = memory.NewQueue(memory.NewMemobase(memory.MemobaseConfig{BaseURL: stub.URL, Token: "golden-test"}), nil, nil,
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

// seedGoldenMatch 布阵 → 设钟 → 开表 → 发两条事件（一条确认进球 + 一条
// 比分更正候选），覆盖 console/director 读到的所有事件字段。
func (h *consoleHarness) seedGoldenMatch(t *testing.T) {
	t.Helper()
	configBody := `{"homeTeam":"西班牙","awayTeam":"德国","lifecycle":"live","homePlayers":[{"number":"10","name":"佩德里","position":"CM"},{"number":"19","name":"亚马尔","position":"RW"}],"awayPlayers":[{"number":"10","name":"穆西亚拉","position":"AM"}]}`
	if got := doConsoleRequest(t, consoleRequest{method: http.MethodPost, path: "/api/matches/m1/config", token: consoleDirectorToken, body: configBody, idempotencyKey: "golden-config", handler: h.matchAPI}); got.Code != http.StatusOK {
		t.Fatalf("POST config = %d body=%s", got.Code, got.Body.String())
	}
	if got := doConsoleRequest(t, consoleRequest{method: http.MethodPatch, path: "/api/matches/m1/clock", token: consoleDirectorToken, body: `{"action":"set","expectedVersion":0,"period":"first_half","elapsedSeconds":735}`, idempotencyKey: "golden-clock-set", handler: h.matchAPI}); got.Code != http.StatusOK {
		t.Fatalf("PATCH clock set = %d body=%s", got.Code, got.Body.String())
	}
	if got := doConsoleRequest(t, consoleRequest{method: http.MethodPatch, path: "/api/matches/m1/clock", token: consoleDirectorToken, body: `{"action":"start","expectedVersion":1}`, idempotencyKey: "golden-clock-start", handler: h.matchAPI}); got.Code != http.StatusOK {
		t.Fatalf("PATCH clock start = %d body=%s", got.Code, got.Body.String())
	}
	goalBody := `{"source":"operator","providerName":"director-console","eventType":"goal","period":"first_half","clock":"12:15","teamId":"home","teamName":"西班牙","playerName":"佩德里","participants":[{"role":"scorer","name":"佩德里","teamId":"home","teamName":"西班牙"}],"score":{"home":1,"away":0},"intensity":5,"confirmed":true,"factStatus":"confirmed","description":"禁区抢点破门，西班牙1比0领先。","recommendedAction":"celebrate","tags":["clockVersion=4","input=operator"],"proactiveText":"佩德里进了！1比0！","revisionOf":"","evidence":{}}`
	if got := doConsoleRequest(t, consoleRequest{method: http.MethodPost, path: "/api/matches/m1/events", token: consoleDirectorToken, body: goalBody, idempotencyKey: "golden-goal", handler: h.matchAPI}); got.Code != http.StatusCreated {
		t.Fatalf("POST goal = %d body=%s", got.Code, got.Body.String())
	}
	correctionBody := `{"source":"operator","providerName":"director-console","eventType":"score_correction","period":"second_half","clock":"80:00","teamId":"home","teamName":"西班牙","score":{"home":2,"away":1},"intensity":3,"confirmed":false,"factStatus":"provisional","description":"人工核对后更正当前比分。","recommendedAction":"analysis","tags":["clockVersion=9","input=operator"],"proactiveText":"__quiet__","revisionOf":"","evidence":{"correctionReason":"记分牌核对"}}`
	if got := doConsoleRequest(t, consoleRequest{method: http.MethodPost, path: "/api/matches/m1/events", token: consoleDirectorToken, body: correctionBody, idempotencyKey: "golden-correction", handler: h.matchAPI}); got.Code != http.StatusCreated {
		t.Fatalf("POST correction = %d body=%s", got.Code, got.Body.String())
	}
}

func TestConsoleGoldenPayloads(t *testing.T) {
	harness := newGoldenHarness(t)
	harness.seedOperators(t)
	harness.seedGoldenMatch(t)
	harness.sessions.Acquire("user-1", "m1")

	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	_, _ = harness.fakeThreads.AppendThread(context.Background(), memory.Thread{UserID: "user-1", Kind: memory.ThreadUnansweredQuestion, Content: "谁助攻的？"})
	_, _ = harness.fakeThreads.AppendThread(context.Background(), memory.Thread{UserID: "user-2", Kind: memory.ThreadPromise, Content: "两天前的问题", CreatedAt: now.Add(-48 * time.Hour)})
	_, _ = harness.fakeThreads.AppendThread(context.Background(), memory.Thread{UserID: "user-3", Kind: memory.ThreadPrediction, Content: "上周的预测", CreatedAt: now.Add(-120 * time.Hour)})

	// 被路由回合的 trace（ADR-0009 审计列形状）+ 关系决策 trace。
	if err := harness.traces.WriteTrace(context.Background(), companion.Trace{
		ID: "trace-routed", MatchID: "m1", UserID: "user-1", Input: "明明进了，裁判瞎了吗", Output: "我看到了，进球有效。",
		Reason: "router_reply_realized", CreatedAt: now,
		Router: &companion.RouterTrace{Intent: "fact_claim", Confidence: 0.85, Player: "佩德里", Score: "1-0", ReplyUsed: true},
	}); err != nil {
		t.Fatalf("write routed trace: %v", err)
	}
	if err := harness.traces.WriteTrace(context.Background(), companion.Trace{
		ID: "trace-proactive", MatchID: "m1", UserID: "user-1", CreatedAt: now.Add(time.Second),
		RelationshipDecision: &relationship.Decision{ReasonCodes: []string{"relationship_decision", "proactive_citation:open_thread:7"}},
	}); err != nil {
		t.Fatalf("write proactive trace: %v", err)
	}

	if err := harness.operators.AppendAudit(context.Background(), consoleDirectorName, "thread.address", "thread:1"); err != nil {
		t.Fatalf("append audit: %v", err)
	}
	harness.ring.Record("trace-routed", "m1", now.Add(2*time.Second))

	// console 消费的全部读形状 + 导演写响应形状。
	getConsole := func(path, name string) {
		t.Helper()
		got := doConsoleRequest(t, consoleRequest{method: http.MethodGet, path: path, token: consoleDirectorToken, handler: harness.console})
		if got.Code != http.StatusOK {
			t.Fatalf("GET %s = %d body=%s", path, got.Code, got.Body.String())
		}
		assertGolden(t, name, got.Body.Bytes())
	}
	getMatch := func(path, name string) {
		t.Helper()
		got := doConsoleRequest(t, consoleRequest{method: http.MethodGet, path: path, token: consoleDirectorToken, handler: harness.matchAPI})
		if got.Code != http.StatusOK {
			t.Fatalf("GET %s = %d body=%s", path, got.Code, got.Body.String())
		}
		assertGolden(t, name, got.Body.Bytes())
	}

	getConsole("/api/console/overview", "console-overview")
	getConsole("/api/console/matches/m1/users", "console-match-users")
	getConsole("/api/console/threads", "console-threads")
	getConsole("/api/console/users/user-1/portrait", "console-portrait")
	getConsole("/api/console/delivery-interruptions", "console-delivery-interruptions")
	getConsole("/api/console/whoami", "console-whoami")
	getConsole("/api/console/operators", "console-operators")
	getMatch("/api/matches/m1/events", "match-events")
	getMatch("/api/matches/m1/clock", "match-clock")
	getMatch("/api/matches/m1/config", "match-config")
	getMatch("/api/matches/m1/state", "match-state")
	getMatch("/api/matches/m1/traces?limit=50", "match-traces")

	// 导演写响应（发布事件的返回形状：event + snapshot）。
	createResponse := doConsoleRequest(t, consoleRequest{
		method: http.MethodPost, path: "/api/matches/m1/events", token: consoleDirectorToken,
		body:           `{"source":"operator","providerName":"director-console","eventType":"yellow_card","period":"second_half","clock":"61:00","teamId":"away","teamName":"德国","playerName":"穆西亚拉","participants":[{"role":"offender","name":"穆西亚拉","teamId":"away","teamName":"德国"}],"score":{"home":1,"away":0},"intensity":3,"confirmed":true,"factStatus":"confirmed","description":"穆西亚拉战术犯规染黄。","recommendedAction":"complain","tags":["clockVersion=9","input=operator"],"proactiveText":"","revisionOf":"","evidence":{}}`,
		idempotencyKey: "golden-yellow",
		handler:        harness.matchAPI,
	})
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("POST yellow = %d body=%s", createResponse.Code, createResponse.Body.String())
	}
	assertGolden(t, "match-event-create-response", createResponse.Body.Bytes())
}

package mcpserve

// 5.2 测试：四只读工具经真实 MCP 往返（in-memory client→server，官方
// go-sdk 自带传输）各验一例返回含预期字段；HTTP 层 httptest 验鉴权
//（匿名 401 / 无效凭证 401 / 缺 scope 403 / 有 scope 放行）。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"qiuqiu/internal/auth"
	"qiuqiu/internal/companion"
	"qiuqiu/internal/matchstate"
)

// ---- fakes ----

type fakeMatchStore struct {
	snapshot matchstate.Snapshot
	events   []matchstate.MatchEvent
}

func (f *fakeMatchStore) PublicSnapshot(matchID string) matchstate.Snapshot {
	snapshot := f.snapshot
	snapshot.MatchID = matchID
	return snapshot
}

func (f *fakeMatchStore) PublicEvents(matchID string) []matchstate.MatchEvent {
	return f.events
}

type fakeScheduleReader struct {
	fixtures []companion.ScheduleMatch
	err      error
}

func (f *fakeScheduleReader) TodayFixtures(context.Context) ([]companion.ScheduleMatch, error) {
	return f.fixtures, f.err
}

func testDeps() Deps {
	store := &fakeMatchStore{
		snapshot: matchstate.Snapshot{
			HomeTeam:      "Bayern",
			AwayTeam:      "Dortmund",
			Competition:   "Bundesliga",
			Score:         matchstate.Score{Home: 2, Away: 1},
			Period:        "second_half",
			Clock:         "78:12",
			LastUpdatedAt: "2026-09-30T12:00:00Z",
			RecentEvents: []matchstate.MatchEvent{
				{ID: "ev-1", EventType: "goal", Clock: "60:00", TeamName: "Bayern", PlayerName: "Musiala", Description: "goal", Status: "active"},
			},
		},
		events: []matchstate.MatchEvent{
			{ID: "ev-1", EventType: "goal", PlayerName: "Musiala", Status: "active", Description: "goal"},
			{ID: "ev-2", EventType: "yellow_card", PlayerName: "Can", Status: "active", Description: "booking"},
			{ID: "ev-3", EventType: "goal", PlayerName: "musiala", Status: "retracted", Description: "retired fact"},
		},
	}
	return Deps{
		Matches:   store,
		Schedules: &fakeScheduleReader{fixtures: []companion.ScheduleMatch{{HomeTeam: "Bayern", AwayTeam: "Leverkusen", Status: "scheduled"}}},
	}
}

// ---- in-memory client→server round trip helper ----

func connectTestSession(t *testing.T, deps Deps) *mcp.ClientSession {
	t.Helper()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	server := NewServer(deps)
	ctx := context.Background()
	go func() { _ = server.Run(ctx, serverTransport) }()
	client := mcp.NewClient(&mcp.Implementation{Name: "mcpserve-test-client", Version: "0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// structured decodes the structured content of a tool result into target.
func structured(t *testing.T, result *mcp.CallToolResult, target any) {
	t.Helper()
	if result == nil {
		t.Fatal("nil tool result")
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", contentText(result))
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("unmarshal structured content %s: %v", data, err)
	}
}

func contentText(result *mcp.CallToolResult) string {
	var parts []string
	for _, content := range result.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			parts = append(parts, text.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// ---- tools/list: exactly the four read-only tools ----

func TestToolsListContainsExactlyFourReadOnlyTools(t *testing.T) {
	session := connectTestSession(t, testDeps())
	listing, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	got := make(map[string]bool, len(listing.Tools))
	for _, tool := range listing.Tools {
		got[tool.Name] = true
	}
	want := servedToolNames()
	if len(listing.Tools) != len(want) {
		t.Fatalf("tools/list = %v, want exactly %v", got, want)
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("tools/list missing %q", name)
		}
	}
}

// ---- one round-trip case per tool ----

func TestMatchReadSnapshotRoundTrip(t *testing.T) {
	session := connectTestSession(t, testDeps())
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      companion.ToolCallMatchReadSnapshot,
		Arguments: map[string]any{"matchId": "m-1"},
	})
	if err != nil {
		t.Fatalf("tools/call: %v", err)
	}
	var payload snapshotSummary
	structured(t, result, &payload)
	if payload.HomeTeam != "Bayern" || payload.AwayTeam != "Dortmund" {
		t.Errorf("teams = %q/%q, want Bayern/Dortmund", payload.HomeTeam, payload.AwayTeam)
	}
	if payload.Score.Home != 2 || payload.Score.Away != 1 {
		t.Errorf("score = %+v, want 2-1", payload.Score)
	}
	if payload.Period != "second_half" || payload.Clock != "78:12" {
		t.Errorf("period/clock = %q/%q", payload.Period, payload.Clock)
	}
	if len(payload.RecentEvents) != 1 || payload.RecentEvents[0].EventType != "goal" || payload.RecentEvents[0].PlayerName != "Musiala" {
		t.Errorf("recent events = %+v", payload.RecentEvents)
	}
	if !strings.Contains(contentText(result), `"homeTeam": "Bayern"`) {
		t.Errorf("text content is not the JSON serialization: %s", contentText(result))
	}
}

func TestMatchSearchEventsRoundTrip(t *testing.T) {
	session := connectTestSession(t, testDeps())
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      companion.ToolCallMatchSearchEvents,
		Arguments: map[string]any{"matchId": "m-1", "limit": 50},
	})
	if err != nil {
		t.Fatalf("tools/call: %v", err)
	}
	var payload eventsResult
	structured(t, result, &payload)
	if payload.MatchID != "m-1" || payload.Limit != 50 {
		t.Errorf("matchId/limit = %q/%d", payload.MatchID, payload.Limit)
	}
	if len(payload.Events) != 2 {
		t.Fatalf("events = %d, want 2 active only", len(payload.Events))
	}
	if payload.Events[0].ID != "ev-1" || payload.Events[1].ID != "ev-2" {
		t.Errorf("event ids = %q,%q", payload.Events[0].ID, payload.Events[1].ID)
	}
}

func TestMatchSearchEventsClampsLimit(t *testing.T) {
	session := connectTestSession(t, testDeps())
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      companion.ToolCallMatchSearchEvents,
		Arguments: map[string]any{"matchId": "m-1", "limit": 5000},
	})
	if err != nil {
		t.Fatalf("tools/call: %v", err)
	}
	var payload eventsResult
	structured(t, result, &payload)
	if payload.Limit != maxEventLimit {
		t.Errorf("limit = %d, want clamped to %d", payload.Limit, maxEventLimit)
	}
}

func TestMatchGetPlayerTimelineRoundTrip(t *testing.T) {
	session := connectTestSession(t, testDeps())
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      companion.ToolCallMatchGetPlayerTimeline,
		Arguments: map[string]any{"matchId": "m-1", "playerName": "MUSIALA", "limit": 10},
	})
	if err != nil {
		t.Fatalf("tools/call: %v", err)
	}
	var payload playerTimelineResult
	structured(t, result, &payload)
	if payload.PlayerName != "MUSIALA" {
		t.Errorf("playerName = %q", payload.PlayerName)
	}
	if len(payload.Events) != 1 || payload.Events[0].ID != "ev-1" {
		t.Fatalf("events = %+v, want only the active ev-1 (retracted fact must not surface)", payload.Events)
	}
}

func TestScheduleReadTodayRoundTrip(t *testing.T) {
	session := connectTestSession(t, testDeps())
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      companion.ToolCallScheduleReadToday,
		Arguments: map[string]any{"scope": "today"},
	})
	if err != nil {
		t.Fatalf("tools/call: %v", err)
	}
	var payload todayResult
	structured(t, result, &payload)
	if payload.Count != 1 || len(payload.Fixtures) != 1 {
		t.Fatalf("fixtures = %+v, want one", payload.Fixtures)
	}
	if payload.Fixtures[0].HomeTeam != "Bayern" || payload.Fixtures[0].AwayTeam != "Leverkusen" {
		t.Errorf("fixture teams = %q/%q", payload.Fixtures[0].HomeTeam, payload.Fixtures[0].AwayTeam)
	}
}

func TestScheduleReadTodayWithoutSourceIsToolError(t *testing.T) {
	deps := testDeps()
	deps.Schedules = nil
	session := connectTestSession(t, deps)
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name: companion.ToolCallScheduleReadToday,
	})
	if err != nil {
		t.Fatalf("tools/call: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected tool error when schedule source is unconfigured")
	}
}

// ---- HTTP auth middleware (httptest) ----

const testReadToken = "good-read-token"

func testAuthHandler() http.Handler {
	return Handler(testDeps(), AuthenticatorFunc(func(r *http.Request) (Principal, bool) {
		switch auth.BearerToken(r.Header.Get("Authorization")) {
		case testReadToken:
			return Principal{Operator: "auditor", Scopes: []string{ReadScope}}, true
		case "scopeless-token":
			return Principal{Operator: "restricted"}, true
		default:
			return Principal{}, false
		}
	}))
}

func postMCP(t *testing.T, handler http.Handler, token string) *http.Response {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`
	request := httptest.NewRequest(http.MethodPost, MountPath, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	// The streamable transport requires the client to accept both response
	// media types before it will even look at the body.
	request.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder.Result()
}

func TestMCPAnonymousIs401(t *testing.T) {
	response := postMCP(t, testAuthHandler(), "")
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d, want 401", response.StatusCode)
	}
	if got := response.Header.Get("WWW-Authenticate"); !strings.Contains(got, "Bearer") {
		t.Errorf("WWW-Authenticate = %q, want a Bearer challenge", got)
	}
}

func TestMCPInvalidCredentialIs401(t *testing.T) {
	response := postMCP(t, testAuthHandler(), "not-a-credential")
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("invalid credential status = %d, want 401", response.StatusCode)
	}
}

func TestMCPMissingReadScopeIs403(t *testing.T) {
	response := postMCP(t, testAuthHandler(), "scopeless-token")
	defer response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("scopeless status = %d, want 403", response.StatusCode)
	}
}

func TestMCPWithReadScopePassesAuth(t *testing.T) {
	response := postMCP(t, testAuthHandler(), testReadToken)
	defer response.Body.Close()
	// Auth passed: the request reached the MCP handler, which answered the
	// initialize by creating a streamable session. The session id response
	// header is the transport-independent proof; 401/403 would mean the
	// credential gate ate the request.
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		t.Fatalf("scoped operator got %d, want the request to reach the MCP handler", response.StatusCode)
	}
	if response.Header.Get("Mcp-Session-Id") == "" {
		t.Errorf("initialize did not create an MCP session (no Mcp-Session-Id header), status %d", response.StatusCode)
	}
}

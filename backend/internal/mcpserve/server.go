package mcpserve

// 只读 MCP server（mcp-registry-serve 5.2）：四个只读工具直调 matchstate
// 公共读与 companion.ScheduleReader 既有读函数，官方 go-sdk Streamable
// HTTP 形态挂 /mcp。工具名与描述从 companion.ToolRegistry 单一源取，
// 本包不定义第二份工具描述。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"qiuqiu/internal/companion"
	"qiuqiu/internal/matchstate"
)

const (
	// ServerName / ServerVersion identify the MCP implementation payload.
	ServerName    = "qiuqiu-ledger"
	ServerVersion = "0.1.0"

	// MountPath is the ServeMux path the streamable-HTTP endpoint mounts on.
	MountPath = "/mcp"

	// defaultEventLimit mirrors the companion agent's read window (8).
	defaultEventLimit = 8
	// maxEventLimit clamps client-supplied limits for the two event tools.
	maxEventLimit = 50
)

// MatchReader narrows this package to the public read half of the match
// fact store. matchstate.Repository satisfies it; the repository's write
// half is deliberately unreachable from here (constitution red line,
// redline_test.go).
type MatchReader interface {
	PublicSnapshot(matchID string) matchstate.Snapshot
	PublicEvents(matchID string) []matchstate.MatchEvent
}

// Deps 收窄 MCP 层能看到的依赖（accept interfaces）：比赛账本只读半边 +
// 既有 schedule 读取接口。cmd/server 装配时传 matchstate.Repository 与
// companion.ScheduleReader，二者天然满足。
type Deps struct {
	Matches   MatchReader
	Schedules companion.ScheduleReader
}

// servedToolNames 是本 server 暴露的四个只读工具名（companion 注册表常量，
// 与 agent 侧同源）。一致性测试断言这四个名字经 IsUserAgentToolAllowed
// 为 true 且都在 CompanionToolSchemas 里。
func servedToolNames() []string {
	return []string{
		companion.ToolCallMatchReadSnapshot,
		companion.ToolCallMatchSearchEvents,
		companion.ToolCallMatchGetPlayerTimeline,
		companion.ToolCallScheduleReadToday,
	}
}

// servedToolSchemas 从 companion 注册表取四工具的描述；缺任何一条都是
// 装配期错误（注册表被改而本包未跟）——fail-fast 与 MustRegister 同规格。
func servedToolSchemas() []companion.ToolSchema {
	wanted := make(map[string]bool, 4)
	for _, name := range servedToolNames() {
		wanted[name] = true
	}
	var out []companion.ToolSchema
	for _, schema := range companion.CompanionToolSchemas() {
		if wanted[schema.Name] {
			out = append(out, schema)
			delete(wanted, schema.Name)
		}
	}
	if len(wanted) > 0 {
		missing := make([]string, 0, len(wanted))
		for name := range wanted {
			missing = append(missing, name)
		}
		panic(fmt.Sprintf("mcpserve: companion tool registry lacks served tools %v", missing))
	}
	return out
}

// NewServer builds the read-only MCP server over deps. Nil Matches is an
// assembly error (panic at construction, never at call time).
func NewServer(deps Deps) *mcp.Server {
	if deps.Matches == nil {
		panic("mcpserve: nil Matches reader")
	}
	server := mcp.NewServer(&mcp.Implementation{Name: ServerName, Version: ServerVersion}, nil)
	for _, schema := range servedToolSchemas() {
		name := schema.Name
		tool := &mcp.Tool{
			Name:        schema.Name,
			Description: schema.Description,
			InputSchema: inputSchemaFor(schema),
		}
		server.AddTool(tool, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return dispatch(deps, name, ctx, req)
		})
	}
	return server
}

// jsonSchemaType maps the companion registry's descriptive input type names
// onto JSON Schema types.
func jsonSchemaType(descriptor string) string {
	switch descriptor {
	case "int", "int64", "integer":
		return "integer"
	case "float64", "float", "number":
		return "number"
	case "bool", "boolean":
		return "boolean"
	default:
		return "string"
	}
}

// inputSchemaFor advertises the tool's JSON Schema straight from the
// companion registry description's Input map — the registry stays the single
// source for names, descriptions and parameter shapes alike. The raw
// ToolHandler path does not validate input server-side (the SDK validates
// only for the generic typed handler), so this schema is the advertised
// contract; the read handlers stay lenient by design.
func inputSchemaFor(schema companion.ToolSchema) map[string]any {
	properties := make(map[string]any, len(schema.Input))
	required := make([]string, 0, len(schema.Input))
	for name, descriptor := range schema.Input {
		properties[name] = map[string]any{"type": jsonSchemaType(descriptor)}
		required = append(required, name)
	}
	sort.Strings(required)
	return map[string]any{
		"type":       "object",
		"properties": properties,
		"required":   required,
	}
}

// dispatch routes a tools/call to the read implementation. Expected domain
// failures (bad arguments, unconfigured source) come back as tool errors
// (IsError content), not protocol errors.
func dispatch(deps Deps, name string, ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	switch name {
	case companion.ToolCallMatchReadSnapshot:
		return deps.readSnapshot(req)
	case companion.ToolCallMatchSearchEvents:
		return deps.searchEvents(req)
	case companion.ToolCallMatchGetPlayerTimeline:
		return deps.playerTimeline(req)
	case companion.ToolCallScheduleReadToday:
		return deps.readToday(ctx, req)
	default:
		// Unreachable for registered tools; the SDK already rejects unknown
		// names at the protocol layer (redline_test.go pins that).
		return nil, fmt.Errorf("mcpserve: unknown tool %q", name)
	}
}

// ---- tool argument payloads ----

type matchArgs struct {
	MatchID string `json:"matchId"`
	Limit   int    `json:"limit,omitempty"`
}

type playerTimelineArgs struct {
	MatchID    string `json:"matchId"`
	PlayerName string `json:"playerName"`
	Limit      int    `json:"limit,omitempty"`
}

type todayArgs struct {
	Scope string `json:"scope,omitempty"`
}

func decodeArgs(req *mcp.CallToolRequest, target any) error {
	if len(req.Params.Arguments) == 0 {
		return nil
	}
	if err := json.Unmarshal(req.Params.Arguments, target); err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}
	return nil
}

// toolError reports an expected failure as a tool error result (MCP spec:
// the LLM sees it and can self-correct), never as a protocol error.
func toolError(format string, args ...any) (*mcp.CallToolResult, error) {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(format, args...)}},
	}, nil
}

// textResult serializes the payload as JSON text plus structured content.
func textResult(payload any) (*mcp.CallToolResult, error) {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("mcpserve: marshal result: %w", err)
	}
	return &mcp.CallToolResult{
		Content:           []mcp.Content{&mcp.TextContent{Text: string(data)}},
		StructuredContent: payload,
	}, nil
}

// clampLimit applies the shared limit semantics: 0/missing → default window,
// negative → default window, above the cap → capped.
func clampLimit(limit int) int {
	if limit <= 0 {
		return defaultEventLimit
	}
	if limit > maxEventLimit {
		return maxEventLimit
	}
	return limit
}

// ---- tool implementations (直调既有读函数，零新事实逻辑) ----

// eventSummary is the compact per-event shape inside snapshot summaries.
type eventSummary struct {
	ID          string `json:"id"`
	EventType   string `json:"eventType"`
	Clock       string `json:"clock,omitempty"`
	TeamName    string `json:"teamName,omitempty"`
	PlayerName  string `json:"playerName,omitempty"`
	Description string `json:"description,omitempty"`
	FactStatus  string `json:"factStatus,omitempty"`
}

type snapshotSummary struct {
	MatchID       string           `json:"matchId"`
	HomeTeam      string           `json:"homeTeam"`
	AwayTeam      string           `json:"awayTeam"`
	Competition   string           `json:"competition,omitempty"`
	Score         matchstate.Score `json:"score"`
	Period        string           `json:"period"`
	Clock         string           `json:"clock"`
	RecentEvents  []eventSummary   `json:"recentEvents"`
	LastUpdatedAt string           `json:"lastUpdatedAt,omitempty"`
}

func (deps Deps) readSnapshot(req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args matchArgs
	if err := decodeArgs(req, &args); err != nil {
		return toolError("%v", err)
	}
	args.MatchID = strings.TrimSpace(args.MatchID)
	if args.MatchID == "" {
		return toolError("matchId is required")
	}
	snapshot := deps.Matches.PublicSnapshot(args.MatchID)
	summary := snapshotSummary{
		MatchID:       snapshot.MatchID,
		HomeTeam:      snapshot.HomeTeam,
		AwayTeam:      snapshot.AwayTeam,
		Competition:   snapshot.Competition,
		Score:         snapshot.Score,
		Period:        snapshot.Period,
		Clock:         snapshot.Clock,
		LastUpdatedAt: snapshot.LastUpdatedAt,
		RecentEvents:  []eventSummary{},
	}
	for _, event := range snapshot.RecentEvents {
		if len(summary.RecentEvents) >= defaultEventLimit {
			break
		}
		summary.RecentEvents = append(summary.RecentEvents, eventSummary{
			ID:          event.ID,
			EventType:   event.EventType,
			Clock:       event.Clock,
			TeamName:    event.TeamName,
			PlayerName:  event.PlayerName,
			Description: event.Description,
			FactStatus:  string(event.FactStatus),
		})
	}
	return textResult(summary)
}

type eventsResult struct {
	MatchID string                  `json:"matchId"`
	Limit   int                     `json:"limit"`
	Events  []matchstate.MatchEvent `json:"events"`
}

func (deps Deps) searchEvents(req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args matchArgs
	if err := decodeArgs(req, &args); err != nil {
		return toolError("%v", err)
	}
	args.MatchID = strings.TrimSpace(args.MatchID)
	if args.MatchID == "" {
		return toolError("matchId is required")
	}
	limit := clampLimit(args.Limit)
	events := activeOnly(deps.Matches.PublicEvents(args.MatchID))
	if len(events) > limit {
		events = events[:limit]
	}
	return textResult(eventsResult{MatchID: args.MatchID, Limit: limit, Events: events})
}

type playerTimelineResult struct {
	MatchID    string                  `json:"matchId"`
	PlayerName string                  `json:"playerName"`
	Limit      int                     `json:"limit"`
	Events     []matchstate.MatchEvent `json:"events"`
}

func (deps Deps) playerTimeline(req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args playerTimelineArgs
	if err := decodeArgs(req, &args); err != nil {
		return toolError("%v", err)
	}
	args.MatchID = strings.TrimSpace(args.MatchID)
	args.PlayerName = strings.TrimSpace(args.PlayerName)
	if args.MatchID == "" {
		return toolError("matchId is required")
	}
	if args.PlayerName == "" {
		return toolError("playerName is required")
	}
	limit := clampLimit(args.Limit)
	events := make([]matchstate.MatchEvent, 0, limit)
	for _, event := range activeOnly(deps.Matches.PublicEvents(args.MatchID)) {
		if eventHasPlayer(event, args.PlayerName) {
			events = append(events, event)
		}
		if len(events) >= limit {
			break
		}
	}
	return textResult(playerTimelineResult{MatchID: args.MatchID, PlayerName: args.PlayerName, Limit: limit, Events: events})
}

type todayResult struct {
	Scope    string                    `json:"scope"`
	Fixtures []companion.ScheduleMatch `json:"fixtures"`
	Count    int                       `json:"count"`
}

func (deps Deps) readToday(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args todayArgs
	if err := decodeArgs(req, &args); err != nil {
		return toolError("%v", err)
	}
	if deps.Schedules == nil {
		return toolError("schedule source is not configured")
	}
	fixtures, err := deps.Schedules.TodayFixtures(ctx)
	if err != nil {
		return toolError("schedule source unavailable: %v", err)
	}
	if fixtures == nil {
		fixtures = []companion.ScheduleMatch{}
	}
	return textResult(todayResult{Scope: strings.TrimSpace(args.Scope), Fixtures: fixtures, Count: len(fixtures)})
}

// ---- read helpers mirrored from the companion agent's established 口径 ----

// activeOnly keeps Status=="active" events, exactly like the agent-side
// event reads (internal/companion/memory.go).
func activeOnly(events []matchstate.MatchEvent) []matchstate.MatchEvent {
	out := make([]matchstate.MatchEvent, 0, len(events))
	for _, event := range events {
		if event.Status == "active" {
			out = append(out, event)
		}
	}
	return out
}

// eventHasPlayer matches PlayerName or any participant name, case-folded,
// exactly like the agent-side player timeline read.
func eventHasPlayer(event matchstate.MatchEvent, playerName string) bool {
	if playerName == "" {
		return false
	}
	if strings.EqualFold(event.PlayerName, playerName) {
		return true
	}
	for _, participant := range event.Participants {
		if strings.EqualFold(participant.Name, playerName) {
			return true
		}
	}
	return false
}

// ---- mounting ----

// Mount registers the read-only MCP streamable-HTTP endpoint on mux at
// MountPath behind authenticator.
func Mount(mux *http.ServeMux, deps Deps, authenticator Authenticator) {
	mux.Handle(MountPath, Handler(deps, authenticator))
}

// Handler wraps the streamable-HTTP MCP handler with the operations-console
// credential check. Anonymous and invalid credentials always get 401 — the
// MCP endpoint never inherits the legacy shared-token dev bypass; an
// authenticated principal without the read scope gets 403.
func Handler(deps Deps, authenticator Authenticator) http.Handler {
	if authenticator == nil {
		panic("mcpserve: nil authenticator")
	}
	server := NewServer(deps)
	inner := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := authenticator.Authenticate(r)
		if !ok {
			w.Header().Set("WWW-Authenticate", `Bearer realm="`+ServerName+`"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if !HasReadScope(principal) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		inner.ServeHTTP(w, r)
	})
}

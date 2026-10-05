package companion

import (
	"context"
	"strings"
	"time"

	"qiuqiu/internal/router"

	"qiuqiu/internal/relationship"
)

// AgentBoundaryRequest is the stable in-process DTO that can later cross a
// process boundary if the companion agent moves out of the Go backend.
type AgentBoundaryRequest struct {
	SignalID            string                            `json:"signalId"`
	FactRefresh         string                            `json:"factRefresh,omitempty"`
	MatchID             string                            `json:"matchId"`
	UserID              string                            `json:"userId"`
	Text                string                            `json:"text"`
	Timezone            string                            `json:"timezone,omitempty"`
	Talkativeness       string                            `json:"talkativeness,omitempty"`
	Settings            *relationship.PreferenceOverrides `json:"settings,omitempty"`
	ProgressiveSchedule bool                              `json:"progressiveSchedule,omitempty"`
	Now                 time.Time                         `json:"now"`
	Voice               *VoiceTraceMetadata               `json:"voice,omitempty"`
}

func sanitizeVoiceMetadata(meta *VoiceTraceMetadata) *VoiceTraceMetadata {
	if meta == nil {
		return nil
	}
	clean := *meta
	if clean.TTSByteCount < 0 {
		clean.TTSByteCount = 0
	}
	return &clean
}

type ToolSchema struct {
	Name              string `json:"name"`
	Description       string `json:"description"`
	MutatesMatchFacts bool   `json:"mutatesMatchFacts"`
	// ServedOverMCP 标记该工具经 /mcp 只读 server 对外供给（ADR-0022）——
	// 供给清单从注册表派生，不再手工列表。
	ServedOverMCP bool              `json:"servedOverMCP"`
	Input         map[string]string `json:"input"`
	Output        map[string]string `json:"output"`
}

func CompanionToolSchemas() []ToolSchema {
	return userAgentTools.Schemas()
}

func IsUserAgentToolAllowed(name string) bool {
	return userAgentTools.Allowed(name)
}

// ToolRegistry 是用户侧工具描述的单一源（mcp-registry-serve 5.1）：描述、
// 运营只读门（IsUserAgentToolAllowed）与后续 MCP 供给层都从这里出发——
// 加一个工具 = tracefields.go 常量 + userAgentToolSchemas() 一条描述，
// 注册表去重守卫挡住双注册。
type ToolRegistry struct {
	byName map[string]ToolSchema
	order  []string
}

func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{byName: make(map[string]ToolSchema)}
}

// MustRegister 登记一条工具描述：空名或重名直接 panic——装配期错误不允许
// 拖到运行时。
func (registry *ToolRegistry) MustRegister(schema ToolSchema) {
	if schema.Name == "" {
		panic("companion: tool schema with empty name")
	}
	if _, exists := registry.byName[schema.Name]; exists {
		panic("companion: duplicate tool schema " + schema.Name)
	}
	registry.byName[schema.Name] = schema
	registry.order = append(registry.order, schema.Name)
}

// Schemas 按登记序返回描述副本。
func (registry *ToolRegistry) Schemas() []ToolSchema {
	schemas := make([]ToolSchema, 0, len(registry.order))
	for _, name := range registry.order {
		schemas = append(schemas, registry.byName[name])
	}
	return schemas
}

// Allowed 报告一个工具名是否登记且不改比赛事实（运营只读门语义不变：
// 未登记 = 不允许）。
func (registry *ToolRegistry) Allowed(name string) bool {
	schema, ok := registry.byName[name]
	return ok && !schema.MutatesMatchFacts
}

// ServedMCPNames 按登记序返回声明 ServedOverMCP 且不改比赛事实的工具名
// ——/mcp 只读 server 的供给清单单一源（ADR-0022），不再手工列表。
func (registry *ToolRegistry) ServedMCPNames() []string {
	names := make([]string, 0, 4)
	for _, name := range registry.order {
		if schema := registry.byName[name]; schema.ServedOverMCP && !schema.MutatesMatchFacts {
			names = append(names, name)
		}
	}
	return names
}

// ServedMCPToolNames 是注册表级供给清单。
func ServedMCPToolNames() []string {
	return userAgentTools.ServedMCPNames()
}

var userAgentTools = func() *ToolRegistry {
	registry := NewToolRegistry()
	for _, schema := range userAgentToolSchemas() {
		registry.MustRegister(schema)
	}
	return registry
}()

// userAgentToolSchemas 是工具描述数据：注册表(userAgentTools)的唯一输入。
func userAgentToolSchemas() []ToolSchema {
	return []ToolSchema{
		{
			Name:              ToolCallMatchReadSnapshot,
			Description:       "Read the current score, clock, period, teams, and compact recent-event snapshot.",
			MutatesMatchFacts: false,
			ServedOverMCP:     true,
			Input:             map[string]string{"matchId": "string"},
			Output:            map[string]string{"snapshot": "matchstate.Snapshot"},
		},
		{
			Name:              ToolCallIntentRoute,
			Description:       "Classify a keyword-miss user turn with the LLM intent router (intent, slots, confidence); never writes match facts.",
			MutatesMatchFacts: false,
			Input:             map[string]string{"text": "string", "context": "string"},
			Output:            map[string]string{"intent": "string", "confidence": "float64"},
		},
		{
			Name:              ToolCallObservationRecord,
			Description:       "Record an unverified user claim as a pending observation for source coordination; never mutates match facts.",
			MutatesMatchFacts: false,
			Input:             map[string]string{"claim": "companion.FactClaim"},
			Output:            map[string]string{"observationId": "string"},
		},
		{
			Name:              ToolCallScheduleReadToday,
			Description:       "Read today's fixture list from the schedule reader; never mutates match facts.",
			MutatesMatchFacts: false,
			ServedOverMCP:     true,
			Input:             map[string]string{"scope": "string"},
			Output:            map[string]string{"fixtures": "[]schedule.ScheduleMatch"},
		},
		{
			Name:              ToolCallScheduleSearch,
			Description:       "Search fixtures in a time window from the schedule reader; never mutates match facts.",
			MutatesMatchFacts: false,
			Input:             map[string]string{"from": "string", "to": "string", "timezone": "string"},
			Output:            map[string]string{"fixtures": "[]schedule.ScheduleMatch"},
		},
		{
			Name:              ToolCallMatchSearchEvents,
			Description:       "Read recent active match events, optionally filtered by intent in the agent policy.",
			MutatesMatchFacts: false,
			ServedOverMCP:     true,
			Input:             map[string]string{"matchId": "string", "limit": "int"},
			Output:            map[string]string{"events": "[]matchstate.MatchEvent"},
		},
		{
			Name:              ToolCallMatchGetPlayerTimeline,
			Description:       "Read active events involving a named player.",
			MutatesMatchFacts: false,
			ServedOverMCP:     true,
			Input:             map[string]string{"matchId": "string", "playerName": "string", "limit": "int"},
			Output:            map[string]string{"events": "[]matchstate.MatchEvent"},
		},
		{
			Name:              ToolCallMatchVerifyUserClaim,
			Description:       "Compare a user-provided score or event claim with the current match snapshot and active events.",
			MutatesMatchFacts: false,
			Input:             map[string]string{"kind": "string", "status": "string"},
			Output:            map[string]string{"claim": "companion.FactClaim"},
		},
		{
			Name:              ToolCallKnowledgeAnswer,
			Description:       "Answer a knowledge_question verbatim from the curated knowledge library (ADR-0017); never mutates match facts.",
			MutatesMatchFacts: false,
			Input:             map[string]string{"text": "string"},
			Output:            map[string]string{"entryId": "string", "answer": "string"},
		},
		{
			Name:              ToolCallKnowledgeTrigger,
			Description:       "Look up a curated knowledge entry triggered by a judgment-class match event (knowledge-event-triggers); never mutates match facts.",
			MutatesMatchFacts: false,
			Input:             map[string]string{"eventType": "string"},
			Output:            map[string]string{"entryId": "string", "quote": "string"},
		},
		{
			Name:              ToolCallKnowledgeTriggerFallback,
			Description:       "Deterministic appendix fallback when the realized reply dropped the quote anchor; never mutates match facts.",
			MutatesMatchFacts: false,
			Input:             map[string]string{"entryId": "string"},
			Output:            map[string]string{"appended": "string"},
		},
		{
			Name:              ToolCallRelationshipGoalComfort,
			Description:       "Record that a goal-comfort prefix was emitted for the subscribed team (proactive-match-nodes); never mutates match facts.",
			MutatesMatchFacts: false,
			Input:             map[string]string{"team": "string"},
			Output:            map[string]string{"ok": "bool"},
		},
		{
			Name:              ToolCallConversationReadRecent,
			Description:       "Read the last few user and companion turns for follow-up resolution.",
			MutatesMatchFacts: false,
			Input:             map[string]string{"matchId": "string", "userId": "string", "limit": "int"},
			Output:            map[string]string{"turns": "[]companion.ConversationTurn"},
		},
		{
			Name:              ToolCallConversationAppendTurn,
			Description:       "Store user and companion turns for short-term conversation memory without changing match facts.",
			MutatesMatchFacts: false,
			Input:             map[string]string{"matchId": "string", "userId": "string", "roles": "user,qiuqiu"},
			Output:            map[string]string{"ok": "bool"},
		},
		{
			Name:              ToolCallRelationshipApply,
			Description:       "Apply an idempotent relationship decision before language realization.",
			MutatesMatchFacts: false,
			Input:             map[string]string{"signalId": "string", "userId": "string", "matchId": "string"},
			Output:            map[string]string{"decision": "relationship.Decision"},
		},
		{
			Name:              ToolCallTraceWriteDecision,
			Description:       "Store the intent, tools, retrieved event IDs, output, reason, latency, and any error.",
			MutatesMatchFacts: false,
			Input:             map[string]string{"trace": "companion.Trace"},
			Output:            map[string]string{"ok": "bool"},
		},
		{
			Name:              ToolCallMemoryRecall,
			Description:       "Read the top-k relevant long-term memories with provenance citations for natural callback.",
			MutatesMatchFacts: false,
			Input:             map[string]string{"userId": "string", "focus": "string", "limit": "int"},
			Output:            map[string]string{"recalls": "[]memory.Recall"},
		},
		{
			Name:              ToolCallMemoryPortrait,
			Description:       "Read the synthesized user portrait (bounded Chinese block) for natural callback; never a source of match facts.",
			MutatesMatchFacts: false,
			Input:             map[string]string{"userId": "string"},
			Output:            map[string]string{"entries": "int"},
		},
		{
			Name:              ToolCallMemoryRecoverThread,
			Description:       "Read the open-thread ledger and answer a previously unanswered question from recorded match facts.",
			MutatesMatchFacts: false,
			Input:             map[string]string{"threadId": "string", "kind": "string"},
			Output:            map[string]string{"reply": "string"},
		},
		{
			Name:              ToolCallMemoryAppendThread,
			Description:       "Record an open-thread candidate (unanswered question, promise, emotional moment, prediction) without changing match facts.",
			MutatesMatchFacts: false,
			Input:             map[string]string{"kind": "string", "content": "string"},
			Output:            map[string]string{"threadId": "string"},
		},
		{
			Name:              ToolCallResponseEmitCompanionReply,
			Description:       "Deliver the final companion reply back to the realtime backend.",
			MutatesMatchFacts: false,
			Input:             map[string]string{"reply": "string", "action": "string"},
			Output:            map[string]string{"ok": "bool"},
		},
	}
}

// classifyAndRoute 是话轮管线的分类与路由阶段(agent-internals 3.3):关键词
// 分类 → LLM 路由(keyword-miss 单次)→ 置信门降级 → 路由建议采纳资格。
// 从 HandleBoundaryRequest 抽出的命名阶段,行为与内联时期逐字节一致。
func (a *Agent) classifyAndRoute(ctx context.Context, req AgentBoundaryRequest) (Intent, Trace, *router.Result, bool, string) {
	intent := Classify(req.Text)
	requestTraceID := traceID(req.Now)
	if signalID := strings.TrimSpace(req.SignalID); signalID != "" && len(signalID) <= 256 {
		traceSignalID := signalID
		if refresh := strings.TrimSpace(req.FactRefresh); refresh != "" {
			traceSignalID += "\x00fact-refresh:" + refresh
		}
		requestTraceID = stableTraceID(req.UserID, req.MatchID, traceSignalID)
	}
	trace := a.newUserTurnTrace(req, intent, requestTraceID)
	if intent == IntentSchedule {
		scheduleIntent := ClassifyScheduleIntent(req.Text)
		trace.Schedule = &scheduleIntent
	}

	// ADR-0009: a keyword-miss turn goes to the LLM router once (single
	// attempt, client-enforced 6s timeout). Any routing failure leaves the
	// turn exactly where it was — the legacy keyword-miss path is also the
	// degradation path (locked decision 6).
	routed := a.routeKeywordMiss(ctx, req, intent, &trace)
	routedCasual := false
	if routed != nil {
		switch mapped := routedTurnIntent(routed.Intent); {
		case mapped == IntentUnknown:
			routedCasual = true
		case confidenceGatedIntent(mapped) && routed.Confidence < routerConfidenceThreshold:
			// Locked decision 5: below 0.7 a fact-class route is not trusted
			// with the deterministic fact path; the turn degrades to the C1
			// casual realization with the evidence preserved in the funnel.
			// A low-confidence control command is never acted on at all.
			if mapped == IntentControlCommand {
				routed = nil
			} else {
				routedCasual = true
			}
		default:
			intent = mapped
			if intent == IntentSchedule && trace.Schedule == nil {
				scheduleIntent := ClassifyScheduleIntent(req.Text)
				trace.Schedule = &scheduleIntent
			}
		}
		trace.Intent = intent
	}
	// Design decision 4: the router's reply suggestion is only consumed for
	// the chat-class intents (and the degraded-casual unknown); deterministic
	// fact paths own their wording and ignore it.
	routerChatReply := ""
	if routed != nil && routerReplyEligibleIntent(intent) {
		routerChatReply = strings.TrimSpace(routed.Reply)
	}
	return intent, trace, routed, routedCasual, routerChatReply
}

// newUserTurnTrace 构造用户回合的初始 trace(管线第二阶段的命名化)。
func (a *Agent) newUserTurnTrace(req AgentBoundaryRequest, intent Intent, requestTraceID string) Trace {
	trace := Trace{
		ID:        requestTraceID,
		MatchID:   req.MatchID,
		UserID:    req.UserID,
		Input:     req.Text,
		Intent:    intent,
		Voice:     sanitizeVoiceMetadata(req.Voice),
		CreatedAt: req.Now,
	}
	if trace.CreatedAt.IsZero() {
		trace.CreatedAt = time.Now()
	}
	return trace
}

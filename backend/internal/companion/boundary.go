package companion

import "time"

// AgentBoundaryRequest is the stable in-process DTO that can later cross a
// process boundary if the companion agent moves out of the Go backend.
type AgentBoundaryRequest struct {
	SignalID            string              `json:"signalId"`
	FactRefresh         string              `json:"factRefresh,omitempty"`
	MatchID             string              `json:"matchId"`
	UserID              string              `json:"userId"`
	Text                string              `json:"text"`
	Timezone            string              `json:"timezone,omitempty"`
	Talkativeness       string              `json:"talkativeness,omitempty"`
	ProgressiveSchedule bool                `json:"progressiveSchedule,omitempty"`
	Now                 time.Time           `json:"now"`
	Voice               *VoiceTraceMetadata `json:"voice,omitempty"`
}

type AgentBoundaryResponse struct {
	Intent            Intent     `json:"intent"`
	Reply             string     `json:"reply"`
	TraceID           string     `json:"traceId"`
	ToolCalls         []ToolCall `json:"toolCalls"`
	RetrievedEventIDs []string   `json:"retrievedEventIds"`
	Reason            string     `json:"reason"`
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
	Name              string            `json:"name"`
	Description       string            `json:"description"`
	MutatesMatchFacts bool              `json:"mutatesMatchFacts"`
	Input             map[string]string `json:"input"`
	Output            map[string]string `json:"output"`
}

func CompanionToolSchemas() []ToolSchema {
	return []ToolSchema{
		{
			Name:              "match.read_snapshot",
			Description:       "Read the current score, clock, period, teams, and compact recent-event snapshot.",
			MutatesMatchFacts: false,
			Input:             map[string]string{"matchId": "string"},
			Output:            map[string]string{"snapshot": "matchstate.Snapshot"},
		},
		{
			Name:              "intent.route",
			Description:       "Classify a keyword-miss user turn with the LLM intent router (intent, slots, confidence); never writes match facts.",
			MutatesMatchFacts: false,
			Input:             map[string]string{"text": "string", "context": "string"},
			Output:            map[string]string{"intent": "string", "confidence": "float64"},
		},
		{
			Name:              "observation.record",
			Description:       "Record an unverified user claim as a pending observation for source coordination; never mutates match facts.",
			MutatesMatchFacts: false,
			Input:             map[string]string{"claim": "companion.FactClaim"},
			Output:            map[string]string{"observationId": "string"},
		},
		{
			Name:              "match.search_events",
			Description:       "Read recent active match events, optionally filtered by intent in the agent policy.",
			MutatesMatchFacts: false,
			Input:             map[string]string{"matchId": "string", "limit": "int"},
			Output:            map[string]string{"events": "[]matchstate.MatchEvent"},
		},
		{
			Name:              "match.get_player_timeline",
			Description:       "Read active events involving a named player.",
			MutatesMatchFacts: false,
			Input:             map[string]string{"matchId": "string", "playerName": "string", "limit": "int"},
			Output:            map[string]string{"events": "[]matchstate.MatchEvent"},
		},
		{
			Name:              "match.verify_user_claim",
			Description:       "Compare a user-provided score or event claim with the current match snapshot and active events.",
			MutatesMatchFacts: false,
			Input:             map[string]string{"kind": "string", "status": "string"},
			Output:            map[string]string{"claim": "companion.FactClaim"},
		},
		{
			Name:              "conversation.read_recent",
			Description:       "Read the last few user and companion turns for follow-up resolution.",
			MutatesMatchFacts: false,
			Input:             map[string]string{"matchId": "string", "userId": "string", "limit": "int"},
			Output:            map[string]string{"turns": "[]companion.ConversationTurn"},
		},
		{
			Name:              "conversation.append_turn",
			Description:       "Store user and companion turns for short-term conversation memory without changing match facts.",
			MutatesMatchFacts: false,
			Input:             map[string]string{"matchId": "string", "userId": "string", "roles": "user,qiuqiu"},
			Output:            map[string]string{"ok": "bool"},
		},
		{
			Name:              "relationship.apply",
			Description:       "Apply an idempotent relationship decision before language realization.",
			MutatesMatchFacts: false,
			Input:             map[string]string{"signalId": "string", "userId": "string", "matchId": "string"},
			Output:            map[string]string{"decision": "relationship.Decision"},
		},
		{
			Name:              "trace.write_decision",
			Description:       "Store the intent, tools, retrieved event IDs, output, reason, latency, and any error.",
			MutatesMatchFacts: false,
			Input:             map[string]string{"trace": "companion.Trace"},
			Output:            map[string]string{"ok": "bool"},
		},
		{
			Name:              "memory.recall",
			Description:       "Read the top-k relevant long-term memories with provenance citations for natural callback.",
			MutatesMatchFacts: false,
			Input:             map[string]string{"userId": "string", "focus": "string", "limit": "int"},
			Output:            map[string]string{"recalls": "[]memory.Recall"},
		},
		{
			Name:              "memory.portrait",
			Description:       "Read the synthesized user portrait (bounded Chinese block) for natural callback; never a source of match facts.",
			MutatesMatchFacts: false,
			Input:             map[string]string{"userId": "string"},
			Output:            map[string]string{"entries": "int"},
		},
		{
			Name:              "memory.recover_thread",
			Description:       "Read the open-thread ledger and answer a previously unanswered question from recorded match facts.",
			MutatesMatchFacts: false,
			Input:             map[string]string{"threadId": "string", "kind": "string"},
			Output:            map[string]string{"reply": "string"},
		},
		{
			Name:              "memory.append_thread",
			Description:       "Record an open-thread candidate (unanswered question, promise, emotional moment, prediction) without changing match facts.",
			MutatesMatchFacts: false,
			Input:             map[string]string{"kind": "string", "content": "string"},
			Output:            map[string]string{"threadId": "string"},
		},
		{
			Name:              "response.emit_companion_reply",
			Description:       "Deliver the final companion reply back to the realtime backend.",
			MutatesMatchFacts: false,
			Input:             map[string]string{"reply": "string", "action": "string"},
			Output:            map[string]string{"ok": "bool"},
		},
	}
}

func IsUserAgentToolAllowed(name string) bool {
	for _, schema := range CompanionToolSchemas() {
		if schema.Name == name {
			return !schema.MutatesMatchFacts
		}
	}
	return false
}

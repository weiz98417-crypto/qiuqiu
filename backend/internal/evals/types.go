package evals

import (
	"time"

	"qiuqiu/internal/companion"
	"qiuqiu/internal/matchstate"
)

const SchemaVersion = "2026-07"

type Case struct {
	Version  string                 `json:"version"`
	ID       string                 `json:"id"`
	Suite    string                 `json:"suite"`
	Tags     []string               `json:"tags,omitempty"`
	Summary  string                 `json:"summary"`
	Config   matchstate.MatchConfig `json:"config"`
	Realizer *RealizerFixture       `json:"realizer,omitempty"`
	// Knowledge 夹具（knowledge-event-triggers / ADR-0017）：runner 把条目
	// 写进临时目录再 Load，评测里知识域不再是"没接上"。
	Knowledge []KnowledgeEntryFixture `json:"knowledge,omitempty"`
	Portrait  *PortraitSeed           `json:"portrait,omitempty"`
	Router    *RouterFixture          `json:"router,omitempty"`
	Events    []EventStep             `json:"events,omitempty"`
	Turns     []TurnStep              `json:"turns,omitempty"`
	Final     FinalExpectation        `json:"final,omitempty"`
}

// KnowledgeEntryFixture 是一条策展知识条目的评测夹具（YAML 字段同型）。
type KnowledgeEntryFixture struct {
	ID         string   `json:"id"`
	Topics     []string `json:"topics"`
	Triggers   []string `json:"triggers,omitempty"`
	Quote      string   `json:"quote,omitempty"`
	Answer     string   `json:"answer"`
	Source     string   `json:"source,omitempty"`
	Confidence float64  `json:"confidence"`
}

type RealizerFixture struct {
	Reply string `json:"reply,omitempty"`
	Error string `json:"error,omitempty"`
}

// RouterFixture scripts the ADR-0009 intent router for one case: each route
// answers the user text it matches. Without a fixture the case runs with the
// router disabled — identical to the no-key production/CI posture.
type RouterFixture struct {
	Routes []RouterRoute `json:"routes"`
}

type RouterRoute struct {
	MatchText  string  `json:"matchText"`
	Intent     string  `json:"intent"`
	Confidence float64 `json:"confidence"`
	Player     string  `json:"player,omitempty"`
	Team       string  `json:"team,omitempty"`
	Score      string  `json:"score,omitempty"`
	Reply      string  `json:"reply,omitempty"`
	// Error simulates a router failure (transport/5xx): the turn must
	// degrade to the legacy keyword-miss behavior.
	Error bool `json:"error,omitempty"`
}

// PortraitSeed plants a synthesized portrait for one user before the turns
// run (C3). The seam treats it exactly like a Memobase-synthesized profile,
// so cases can assert the fact reaches the realization context — and stops
// reaching it after a forget step.
type PortraitSeed struct {
	UserID  string              `json:"userId"`
	Entries []PortraitSeedEntry `json:"entries"`
}

type PortraitSeedEntry struct {
	Topic    string `json:"topic"`
	SubTopic string `json:"subTopic"`
	Content  string `json:"content"`
}

type EventStep struct {
	Key string `json:"key"`
	// AfterTurn defers the event until the turn with this id has executed,
	// so a case can express facts landing between two user turns (e.g. the
	// C2 recovered-answer journey). Empty means the event runs up front.
	Corrects    string                `json:"corrects,omitempty"`
	AfterTurn   string                `json:"afterTurn,omitempty"`
	ExpectError bool                  `json:"expectError,omitempty"`
	Event       matchstate.MatchEvent `json:"event"`
	Proactive   *ProactiveExpectation `json:"proactive,omitempty"`
}

type ProactiveExpectation struct {
	UserID             string   `json:"userId,omitempty"`
	Exact              string   `json:"exact,omitempty"`
	MustMention        []string `json:"mustMention,omitempty"`
	MustNotMention     []string `json:"mustNotMention,omitempty"`
	Reason             string   `json:"reason,omitempty"`
	RequiredTools      []string `json:"requiredTools,omitempty"`
	ForbiddenTools     []string `json:"forbiddenTools,omitempty"`
	RetrievedEventKeys []string `json:"retrievedEventKeys,omitempty"`
}

type TurnStep struct {
	ID     string                        `json:"id"`
	UserID string                        `json:"userId"`
	Text   string                        `json:"text"`
	Voice  *companion.VoiceTraceMetadata `json:"voice,omitempty"`
	// ForgetPortrait removes the named sub-topics from this user's portrait
	// AFTER the turn executes (the C3 page's delete flow), so the next turn
	// can assert the fact is gone from the realization context.
	ForgetPortrait []string `json:"forgetPortrait,omitempty"`
	// PortraitClaim merges one new user claim into the local authoritative
	// overlay layer AFTER the turn executes (portrait-maintenance 阶段一，
	// Reflection 写路径的评测替身)：decision 是脚本化判定（pr tier 脚本
	// LLM，先例=scriptedRouter），替代真判定器。
	PortraitClaim *PortraitClaimStep `json:"portraitClaim,omitempty"`
	Expect        TurnExpectation    `json:"expect"`
}

// PortraitClaimStep mirrors memory.PortraitClaim plus its scripted
// memory.OpDecision；TargetID 指 MemoryPortraitOverlays 里此前主张的行 id
// （id 从 1 起按插入序分配，用例内确定）。
type PortraitClaimStep struct {
	Topic    string            `json:"topic"`
	SubTopic string            `json:"subTopic"`
	Content  string            `json:"content"`
	Decision OpDecisionFixture `json:"decision"`
}

type OpDecisionFixture struct {
	Op       string `json:"op"`
	TargetID int64  `json:"targetId,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

type TurnExpectation struct {
	Intent             companion.Intent              `json:"intent,omitempty"`
	Exact              string                        `json:"exact,omitempty"`
	MustMention        []string                      `json:"mustMention,omitempty"`
	MustNotMention     []string                      `json:"mustNotMention,omitempty"`
	Reason             string                        `json:"reason,omitempty"`
	RequiredTools      []string                      `json:"requiredTools,omitempty"`
	ForbiddenTools     []string                      `json:"forbiddenTools,omitempty"`
	RetrievedEventKeys []string                      `json:"retrievedEventKeys,omitempty"`
	Voice              *companion.VoiceTraceMetadata `json:"voice,omitempty"`
	ClaimKind          string                        `json:"claimKind,omitempty"`
	ClaimStatus        companion.ClaimStatus         `json:"claimStatus,omitempty"`
	ClaimCertainty     string                        `json:"claimCertainty,omitempty"`
	ForbidClaim        bool                          `json:"forbidClaim,omitempty"`
	// MemoryMustMention / MemoryMustNotMention grade the memory context the
	// realization request actually carried (recall + portrait blocks). They
	// need a realizer fixture, which is what captures the request.
	MemoryMustMention    []string `json:"memoryMustMention,omitempty"`
	MemoryMustNotMention []string `json:"memoryMustNotMention,omitempty"`
	// Intent-router assertions (ADR-0009): the raw router verdict recorded on
	// the trace, whether the suggested reply survived validation, and whether
	// the C3 funnel opened an unroutable thread for this turn.
	RouterIntent        string  `json:"routerIntent,omitempty"`
	RouterMinConfidence float64 `json:"routerMinConfidence,omitempty"`
	RouterReplyUsed     *bool   `json:"routerReplyUsed,omitempty"`
	UnroutableThread    bool    `json:"unroutableThread,omitempty"`
}

type FinalExpectation struct {
	Score         *matchstate.Score `json:"score,omitempty"`
	MinimumTraces int               `json:"minimumTraces,omitempty"`
}

type Failure struct {
	Category string `json:"category"`
	Message  string `json:"message"`
}

type StepResult struct {
	ID        string           `json:"id"`
	Kind      string           `json:"kind"`
	Reply     string           `json:"reply,omitempty"`
	TraceID   string           `json:"traceId,omitempty"`
	LatencyMS int              `json:"latencyMs"`
	Failures  []Failure        `json:"failures,omitempty"`
	Trace     *companion.Trace `json:"trace,omitempty"`
}

type CaseResult struct {
	ID        string             `json:"id"`
	Suite     string             `json:"suite"`
	Tags      []string           `json:"tags,omitempty"`
	Passed    bool               `json:"passed"`
	LatencyMS int                `json:"latencyMs"`
	Failures  []Failure          `json:"failures,omitempty"`
	Steps     []StepResult       `json:"steps"`
	Scores    map[string]float64 `json:"scores"`
}

type Scorecard struct {
	TotalCases        int                `json:"totalCases"`
	PassedCases       int                `json:"passedCases"`
	FailedCases       int                `json:"failedCases"`
	PassRate          float64            `json:"passRate"`
	FactSafetyRate    float64            `json:"factSafetyRate"`
	TrajectoryRate    float64            `json:"trajectoryRate"`
	TraceCompleteRate float64            `json:"traceCompleteRate"`
	Suites            map[string]float64 `json:"suites"`
}

type Report struct {
	SchemaVersion string       `json:"schemaVersion"`
	StartedAt     time.Time    `json:"startedAt"`
	FinishedAt    time.Time    `json:"finishedAt"`
	GitRevision   string       `json:"gitRevision,omitempty"`
	Scorecard     Scorecard    `json:"scorecard"`
	Cases         []CaseResult `json:"cases"`
}

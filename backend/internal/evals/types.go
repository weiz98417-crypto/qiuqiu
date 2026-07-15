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
	Events   []EventStep            `json:"events,omitempty"`
	Turns    []TurnStep             `json:"turns,omitempty"`
	Final    FinalExpectation       `json:"final,omitempty"`
}

type RealizerFixture struct {
	Reply string `json:"reply,omitempty"`
	Error string `json:"error,omitempty"`
}

type EventStep struct {
	Key         string                `json:"key"`
	Corrects    string                `json:"corrects,omitempty"`
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
	Expect TurnExpectation               `json:"expect"`
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

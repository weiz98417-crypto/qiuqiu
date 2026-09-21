package companion

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/observation"
	"qiuqiu/internal/relationship"
)

type Intent string

var traceSequence atomic.Uint64

const (
	IntentSmalltalk       Intent = "smalltalk"
	IntentSchedule        Intent = "schedule_question"
	IntentMatchStatus     Intent = "match_status_question"
	IntentRecentEvent     Intent = "recent_event_question"
	IntentMatchReaction   Intent = "match_reaction"
	IntentFollowUp        Intent = "follow_up_question"
	IntentPlayerQuestion  Intent = "player_question"
	IntentEmotionReaction Intent = "emotion_reaction"
	IntentPersonalShare   Intent = "personal_share"
	IntentControlCommand  Intent = "control_command"
	IntentMatchClaim      Intent = "match_fact_claim"
	IntentReminderRequest Intent = "reminder_request"
	IntentKnowledge       Intent = "knowledge_question"
	IntentUnknown         Intent = "unknown"
)

type ClaimStatus string

const (
	ClaimStatusConfirmed    ClaimStatus = "confirmed"
	ClaimStatusContradicted ClaimStatus = "contradicted"
	ClaimStatusUnverified   ClaimStatus = "unverified"
)

type FactClaim struct {
	Kind          string            `json:"kind"`
	Certainty     string            `json:"certainty"`
	Status        ClaimStatus       `json:"status"`
	ClaimedScore  *matchstate.Score `json:"claimedScore,omitempty"`
	ActualScore   *matchstate.Score `json:"actualScore,omitempty"`
	EventType     string            `json:"eventType,omitempty"`
	ClaimedPlayer string            `json:"claimedPlayer,omitempty"`
	ActualPlayer  string            `json:"actualPlayer,omitempty"`
	ClaimedTeam   string            `json:"claimedTeam,omitempty"`
	ActualTeam    string            `json:"actualTeam,omitempty"`
	Reason        string            `json:"reason,omitempty"`
}

type MessageRequest struct {
	SignalID            string
	FactRefresh         string
	MatchID             string
	UserID              string
	Text                string
	Timezone            string
	Talkativeness       string
	ProgressiveSchedule bool
	Now                 time.Time
	Voice               *VoiceTraceMetadata
}

type Response struct {
	Intent         Intent
	Reply          string
	Trace          Trace
	Presentation   relationship.PresentationPlan
	ScheduleLookup *ScheduleLookup
}

type TurnKind string

const (
	TurnKindUser         TurnKind = "user"
	TurnKindMatchEvent   TurnKind = "match_event"
	TurnKindFirstMeeting TurnKind = "first_meeting"
	TurnKindObservation  TurnKind = "observation_resolution"
	TurnKindDelivery     TurnKind = "delivery"
)

type DeliveryInput struct {
	SignalID      string
	TraceID       string
	UserID        string
	MatchID       string
	DecisionID    string
	State         string
	Purpose       string
	UsedMemoryIDs []string
	Now           time.Time
}

type TurnInput struct {
	Kind         TurnKind
	Message      *MessageRequest
	MatchEvent   *MatchEventRequest
	FirstMeeting *FirstMeetingRequest
	Observation  *ObservationInput
	Delivery     *DeliveryInput
}

type ObservationInput struct {
	Resolution observation.Resolution
	EventID    string
	Now        time.Time
}

type TurnPlan struct {
	Kind           TurnKind                      `json:"kind"`
	Intent         Intent                        `json:"intent,omitempty"`
	Reply          string                        `json:"reply,omitempty"`
	Trace          Trace                         `json:"trace"`
	Decision       *relationship.Decision        `json:"decision,omitempty"`
	Presentation   relationship.PresentationPlan `json:"presentation"`
	ScheduleLookup *ScheduleLookup               `json:"scheduleLookup,omitempty"`
}

type TurnPlanner interface {
	Plan(context.Context, TurnInput) (TurnPlan, error)
}

type ProactiveResponse struct {
	Reply        string
	Trace        Trace
	Decision     relationship.Decision
	Presentation relationship.PresentationPlan
}

type ObservationResponse struct {
	Resolution   observation.Resolution
	Reply        string
	Trace        Trace
	Presentation relationship.PresentationPlan
}

type MatchEventRequest struct {
	UserID                string
	Event                 matchstate.MatchEvent
	Snapshot              matchstate.Snapshot
	OutputAllowed         bool
	Critical              bool
	UserSpeaking          bool
	NormalCooldownSeconds int
	Talkativeness         string
	// CitationReason is the proactive citation code (C2), e.g.
	// "open_thread:123" or "shared_moment:<eventId>"; carried on the trace.
	CitationReason string
	Now            time.Time
}

type FirstMeetingRequest struct {
	SignalID     string
	MatchID      string
	UserID       string
	Nickname     string
	FavoriteTeam string
	Now          time.Time
}

type Trace struct {
	ID                    string                          `json:"id"`
	MatchID               string                          `json:"matchId"`
	UserID                string                          `json:"userId"`
	Input                 string                          `json:"input"`
	Intent                Intent                          `json:"intent"`
	ToolCalls             []ToolCall                      `json:"toolCalls"`
	RetrievedEvent        []string                        `json:"retrievedEventIds"`
	Output                string                          `json:"output"`
	Reason                string                          `json:"reason"`
	LatencyMS             int                             `json:"latencyMs"`
	Error                 string                          `json:"error"`
	Voice                 *VoiceTraceMetadata             `json:"voice,omitempty"`
	Schedule              *ScheduleIntent                 `json:"schedule,omitempty"`
	LookupID              string                          `json:"lookupId,omitempty"`
	ParentTraceID         string                          `json:"parentTraceId,omitempty"`
	Claim                 *FactClaim                      `json:"claim,omitempty"`
	Observation           *observation.PendingObservation `json:"observation,omitempty"`
	ObservationResolution *observation.Resolution         `json:"observationResolution,omitempty"`
	RelationshipDecision  *relationship.Decision          `json:"relationshipDecision,omitempty"`
	Router                *RouterTrace                    `json:"router,omitempty"`
	CreatedAt             time.Time                       `json:"createdAt"`
}

// RouterTrace carries the ADR-0009 routing observability for a turn: the raw
// router verdict (intent/confidence/slots) plus whether the suggested reply
// survived guard validation. The reason code "router:<intent>:<confidence>"
// rides on the relationship decision's reason codes.
type RouterTrace struct {
	Intent     string  `json:"intent"`
	Confidence float64 `json:"confidence"`
	Player     string  `json:"player,omitempty"`
	Team       string  `json:"team,omitempty"`
	Score      string  `json:"score,omitempty"`
	ReplyUsed  bool    `json:"replyUsed,omitempty"`
	// RejectReason 非空 = 建议回复存在但被护栏拦截（policy）或为空（empty）：
	// ReplyUsed=false 的两种去向从此可分（ADR-0009 审计承诺补全）。
	RejectReason string `json:"rejectReason,omitempty"`
}

type VoiceTraceMetadata struct {
	ASRStatus      string `json:"asrStatus,omitempty"`
	ASRText        string `json:"asrText,omitempty"`
	ASRError       string `json:"asrError,omitempty"`
	ASRProvider    string `json:"asrProvider,omitempty"`
	TTSStatus      string `json:"ttsStatus,omitempty"`
	TTSError       string `json:"ttsError,omitempty"`
	TTSMime        string `json:"ttsMime,omitempty"`
	TTSByteCount   int    `json:"ttsByteCount,omitempty"`
	PlaybackStatus string `json:"playbackStatus,omitempty"`
}

type ToolCall struct {
	Name string            `json:"name"`
	Args map[string]string `json:"args"`
}

type ConversationTurn struct {
	TraceID   string    `json:"traceId,omitempty"`
	MatchID   string    `json:"matchId"`
	UserID    string    `json:"userId"`
	Role      string    `json:"role"`
	Text      string    `json:"text"`
	EventID   string    `json:"eventId"`
	CreatedAt time.Time `json:"createdAt"`
}

type ScheduleMatch struct {
	FixtureID   string    `json:"fixtureId,omitempty"`
	HomeTeam    string    `json:"homeTeam"`
	AwayTeam    string    `json:"awayTeam"`
	Competition string    `json:"competition,omitempty"`
	KickoffAt   time.Time `json:"kickoffAt,omitempty"`
	Status      string    `json:"status,omitempty"`
	HomeScore   *int      `json:"homeScore,omitempty"`
	AwayScore   *int      `json:"awayScore,omitempty"`
	Source      string    `json:"source,omitempty"`
	SourceURL   string    `json:"sourceUrl,omitempty"`
	Freshness   string    `json:"freshness,omitempty"`
}

type ScheduleReader interface {
	TodayFixtures(context.Context) ([]ScheduleMatch, error)
}

type ScheduleScope string

const (
	ScheduleScopeCurrent  ScheduleScope = "current"
	ScheduleScopeToday    ScheduleScope = "today"
	ScheduleScopeTomorrow ScheduleScope = "tomorrow"
	ScheduleScopeNearby   ScheduleScope = "nearby"
)

type ScheduleIntent struct {
	Topic       string        `json:"topic"`
	Action      string        `json:"action"`
	Scope       ScheduleScope `json:"scope"`
	Competition string        `json:"competition,omitempty"`
	Confidence  float64       `json:"confidence"`
}

type ScheduleSearchRequest struct {
	From        time.Time `json:"from"`
	To          time.Time `json:"to"`
	Timezone    string    `json:"timezone"`
	Competition string    `json:"competition,omitempty"`
	Query       string    `json:"query,omitempty"`
}

type ScheduleSearchResult struct {
	Fixtures  []ScheduleMatch `json:"fixtures"`
	Source    string          `json:"source"`
	FetchedAt time.Time       `json:"fetchedAt"`
	Freshness string          `json:"freshness"`
}

type ScheduleSearchReader interface {
	Search(context.Context, ScheduleSearchRequest) (ScheduleSearchResult, error)
}

type ScheduleLookup struct {
	ID            string                `json:"lookupId"`
	ParentTraceID string                `json:"parentTraceId"`
	MatchID       string                `json:"matchId"`
	UserID        string                `json:"userId"`
	Query         string                `json:"query"`
	Intent        ScheduleIntent        `json:"intent"`
	Search        ScheduleSearchRequest `json:"search"`
	ExpiresAt     time.Time             `json:"expiresAt"`
}

var ErrScheduleLookupExpired = errors.New("schedule lookup expired")

type RealizationRequest struct {
	UserInput string
	Intent    Intent
	Grounding relationship.GroundedContent
	Decision  relationship.Decision
	// MemoryContext is the bounded provenance-cited recall block (ADR-0006);
	// empty when the memory seam is absent or degraded.
	MemoryContext string
	// PortraitContext is the bounded synthesized user-model block (ADR-0006,
	// C3); user edits and deletion tombstones already applied in the seam.
	// Empty renders as 无.
	PortraitContext string
	ReliableText    string
}

type RealizedTurn struct {
	Text string
}

type ReplyRealizer interface {
	Realize(ctx context.Context, req RealizationRequest) (RealizedTurn, error)
}

type MemoryTools interface {
	Snapshot(ctx context.Context, matchID string) (matchstate.Snapshot, error)
	RecentEvents(ctx context.Context, matchID string, limit int) ([]matchstate.MatchEvent, error)
	EventsByPlayer(ctx context.Context, matchID, playerName string, limit int) ([]matchstate.MatchEvent, error)
	RecentTurns(ctx context.Context, matchID, userID string, limit int) ([]ConversationTurn, error)
	WriteTrace(ctx context.Context, trace Trace) error
	UpdateTrace(ctx context.Context, trace Trace) error
}

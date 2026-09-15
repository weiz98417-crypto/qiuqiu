package companion

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	"qiuqiu/internal/interaction"
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
	Now                   time.Time
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
	CreatedAt             time.Time                       `json:"createdAt"`
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
	UserInput    string
	Intent       Intent
	Grounding    relationship.GroundedContent
	Decision     relationship.Decision
	ReliableText string
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

type Agent struct {
	tools                      MemoryTools
	realizer                   ReplyRealizer
	scheduleReader             ScheduleReader
	director                   relationship.CompanionDirector
	observations               observation.Coordinator
	observationReconcileWindow func(string, string) time.Duration
	realizeTimeout             time.Duration
	interactions               interaction.Ledger
}

func NewAgent(tools MemoryTools) *Agent {
	return &Agent{tools: tools, realizeTimeout: 800 * time.Millisecond, interactions: interaction.NewMemoryLedger()}
}

func (a *Agent) WithInteractionLedger(ledger interaction.Ledger) *Agent {
	if ledger != nil {
		a.interactions = ledger
	}
	return a
}

func (a *Agent) RecordMediaDelivery(ctx context.Context, event interaction.Event) error {
	return a.recordInteraction(ctx, event)
}

func (a *Agent) WithRealizer(realizer ReplyRealizer, timeout time.Duration) *Agent {
	a.realizer = realizer
	if timeout > 0 {
		a.realizeTimeout = timeout
	}
	return a
}

func (a *Agent) WithScheduleReader(reader ScheduleReader) *Agent {
	a.scheduleReader = reader
	return a
}

func (a *Agent) WithDirector(director relationship.CompanionDirector) *Agent {
	a.director = director
	return a
}

func (a *Agent) WithObservationCoordinator(coordinator observation.Coordinator) *Agent {
	a.observations = coordinator
	return a
}

func (a *Agent) WithObservationReconcileWindow(provider func(string, string) time.Duration) *Agent {
	a.observationReconcileWindow = provider
	return a
}

func (a *Agent) UpdateTrace(ctx context.Context, trace Trace) error {
	return a.tools.UpdateTrace(ctx, trace)
}

func (a *Agent) recordInteraction(ctx context.Context, event interaction.Event) error {
	if a == nil || a.interactions == nil || event.ID == "" || event.UserID == "" || event.MatchID == "" {
		return nil
	}
	_, err := a.interactions.Append(ctx, event)
	return err
}

func (a *Agent) recordTurn(ctx context.Context, event interaction.Event, trace Trace) error {
	payload, err := json.Marshal(trace)
	if err != nil {
		return err
	}
	event.TracePayload = payload
	return a.recordInteraction(ctx, event)
}

func (a *Agent) recordScheduleLookupTurn(ctx context.Context, lookup ScheduleLookup, trace Trace) error {
	return a.recordTurn(ctx, interaction.Event{
		ID:         trace.ID,
		Kind:       interaction.KindTurnPlanned,
		SignalID:   lookup.ID,
		UserID:     lookup.UserID,
		MatchID:    lookup.MatchID,
		TraceID:    trace.ID,
		InputText:  lookup.Query,
		OutputText: trace.Output,
		Source:     "schedule_lookup",
		CreatedAt:  trace.CreatedAt,
	}, trace)
}

func decisionID(decision *relationship.Decision) string {
	if decision == nil {
		return ""
	}
	return decision.ID
}

func presentationPointer(presentation relationship.PresentationPlan) *relationship.PresentationPlan {
	if presentation == (relationship.PresentationPlan{}) {
		return nil
	}
	value := presentation
	return &value
}

func (a *Agent) HandleObservationFactChanged(ctx context.Context, event matchstate.MatchEvent, now time.Time) ([]ObservationResponse, error) {
	if a == nil || a.observations == nil {
		return nil, nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	resolutions, err := a.observations.OnFactChanged(ctx, event)
	if err != nil {
		return nil, err
	}
	return a.observationResponses(ctx, resolutions, now, event.ID)
}

func (a *Agent) RecoverObservationFollowUps(ctx context.Context, userID, matchID string, now time.Time) ([]ObservationResponse, error) {
	recovery, ok := a.observations.(observation.ResolutionRecovery)
	if !ok {
		return nil, nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	resolutions, err := recovery.PendingResolutions(ctx, userID, matchID, now)
	if err != nil {
		return nil, err
	}
	return a.observationResponses(ctx, resolutions, now, "")
}

func (a *Agent) MarkObservationResolutionDelivered(ctx context.Context, deliveryKey string, deliveredAt time.Time) error {
	recovery, ok := a.observations.(observation.ResolutionRecovery)
	if !ok {
		return nil
	}
	return recovery.MarkResolutionDelivered(ctx, deliveryKey, deliveredAt)
}

func (a *Agent) SuppressObservationFollowUp(ctx context.Context, observationID string, suppressedAt time.Time) error {
	suppressor, ok := a.observations.(observation.ResolutionSuppressor)
	if !ok {
		return nil
	}
	return suppressor.SuppressFollowUp(ctx, observationID, suppressedAt)
}

func (a *Agent) observationResponses(ctx context.Context, resolutions []observation.Resolution, now time.Time, eventID string) ([]ObservationResponse, error) {
	responses := make([]ObservationResponse, 0, len(resolutions))
	for index := range resolutions {
		resolution := resolutions[index]
		if strings.TrimSpace(resolution.ReliableText) == "" {
			continue
		}
		anchor := strings.TrimSpace(resolution.FactID)
		if anchor == "" {
			anchor = eventID
		}
		plan, err := a.Plan(ctx, TurnInput{Kind: TurnKindObservation, Observation: &ObservationInput{
			Resolution: resolution,
			EventID:    anchor,
			Now:        now,
		}})
		if err != nil {
			return responses, err
		}
		responses = append(responses, ObservationResponse{
			Resolution:   resolution,
			Reply:        plan.Reply,
			Trace:        plan.Trace,
			Presentation: plan.Presentation,
		})
	}
	return responses, nil
}

func observationPresentation(status observation.Status) relationship.PresentationPlan {
	switch status {
	case observation.StatusConfirmed:
		return relationship.PresentationPlan{Expression: "excited", Motion: "cheer", VoiceStyle: "excited", VoiceEnergy: 0.9, VoiceSpeed: 1.05, HoldMS: 1800, ReturnMode: "watching"}
	case observation.StatusContradicted:
		return relationship.PresentationPlan{Expression: "deflated", Motion: "slump", VoiceStyle: "soft", VoiceEnergy: 0.45, VoiceSpeed: 0.95, HoldMS: 1600, ReturnMode: "watching"}
	default:
		return relationship.PresentationPlan{Expression: "focus", Motion: "speak", VoiceStyle: "calm", VoiceEnergy: 0.55, VoiceSpeed: 1, HoldMS: 1200, ReturnMode: "watching"}
	}
}

func (a *Agent) Plan(ctx context.Context, input TurnInput) (TurnPlan, error) {
	if event, ok := interactionEventForInput(input); ok {
		if err := a.recordInteraction(ctx, event); err != nil {
			if errors.Is(err, interaction.ErrConflict) {
				return TurnPlan{}, ErrTraceConflict
			}
			return TurnPlan{}, err
		}
	}
	switch input.Kind {
	case TurnKindUser:
		if input.Message == nil {
			return TurnPlan{}, errors.New("user turn input is required")
		}
		response, err := a.handleMessage(ctx, *input.Message)
		if err != nil {
			return TurnPlan{}, err
		}
		plan := TurnPlan{Kind: input.Kind, Intent: response.Intent, Reply: response.Reply, Trace: response.Trace, Presentation: response.Presentation, ScheduleLookup: response.ScheduleLookup, Decision: response.Trace.RelationshipDecision}
		factRevision := a.factRevisionFor(ctx, input.Message.MatchID, response.Trace.RetrievedEvent)
		if input.Message.FactRefresh != "" {
			if err := a.markPriorFactTurnsStale(ctx, input.Message.UserID, input.Message.MatchID, input.Message.SignalID, response.Trace.ID, response.Trace.CreatedAt); err != nil {
				return TurnPlan{}, err
			}
		}
		if err := a.recordTurn(ctx, interaction.Event{ID: response.Trace.ID, Kind: interaction.KindTurnPlanned, SignalID: input.Message.SignalID, UserID: input.Message.UserID, MatchID: input.Message.MatchID, TraceID: response.Trace.ID, DecisionID: decisionID(response.Trace.RelationshipDecision), Decision: response.Trace.RelationshipDecision, Presentation: presentationPointer(response.Presentation), InputText: input.Message.Text, OutputText: response.Reply, FactIDs: response.Trace.RetrievedEvent, FactRevision: factRevision, Source: string(input.Kind), CreatedAt: response.Trace.CreatedAt}, response.Trace); err != nil {
			return TurnPlan{}, err
		}
		if err := a.recordChosenSilence(ctx, plan, input.Message.UserID, input.Message.MatchID, input.Message.SignalID); err != nil {
			return TurnPlan{}, err
		}
		return plan, nil
	case TurnKindMatchEvent:
		if input.MatchEvent == nil {
			return TurnPlan{}, errors.New("match event turn input is required")
		}
		response, err := a.handleMatchEvent(ctx, *input.MatchEvent)
		if err != nil {
			return TurnPlan{}, err
		}
		plan := TurnPlan{Kind: input.Kind, Intent: IntentMatchReaction, Reply: response.Reply, Trace: response.Trace, Decision: &response.Decision, Presentation: response.Presentation}
		factRevision := matchstate.DeliveryKey(input.MatchEvent.Event)
		stale := input.MatchEvent.Critical && response.Decision.ID != "" && response.Decision.FactRevision != factRevision
		if !stale {
			if err := a.markPriorFactTurnsStale(ctx, input.MatchEvent.UserID, input.MatchEvent.Event.MatchID, input.MatchEvent.Event.ID, response.Trace.ID, response.Trace.CreatedAt); err != nil {
				return TurnPlan{}, err
			}
		}
		if err := a.recordTurn(ctx, interaction.Event{ID: response.Trace.ID, Kind: interaction.KindTurnPlanned, SignalID: input.MatchEvent.Event.ID, UserID: input.MatchEvent.UserID, MatchID: input.MatchEvent.Event.MatchID, TraceID: response.Trace.ID, DecisionID: response.Decision.ID, Decision: &response.Decision, Presentation: presentationPointer(response.Presentation), OutputText: response.Reply, FactIDs: []string{input.MatchEvent.Event.ID}, FactRevision: fmt.Sprintf("%d:%s", input.MatchEvent.Event.FactRevision, input.MatchEvent.Event.FactStatus), Stale: stale, Source: string(input.Kind), CreatedAt: response.Trace.CreatedAt}, response.Trace); err != nil {
			return TurnPlan{}, err
		}
		if err := a.recordChosenSilence(ctx, plan, input.MatchEvent.UserID, input.MatchEvent.Event.MatchID, input.MatchEvent.Event.ID); err != nil {
			return TurnPlan{}, err
		}
		return plan, nil
	case TurnKindFirstMeeting:
		if input.FirstMeeting == nil {
			return TurnPlan{}, errors.New("first meeting turn input is required")
		}
		response, err := a.handleFirstMeeting(ctx, *input.FirstMeeting)
		if err != nil {
			return TurnPlan{}, err
		}
		plan := TurnPlan{Kind: input.Kind, Reply: response.Reply, Trace: response.Trace, Decision: &response.Decision, Presentation: response.Presentation}
		if err := a.recordTurn(ctx, interaction.Event{ID: response.Trace.ID, Kind: interaction.KindTurnPlanned, SignalID: input.FirstMeeting.SignalID, UserID: input.FirstMeeting.UserID, MatchID: input.FirstMeeting.MatchID, TraceID: response.Trace.ID, DecisionID: response.Decision.ID, Decision: &response.Decision, Presentation: presentationPointer(response.Presentation), OutputText: response.Reply, Source: string(input.Kind), CreatedAt: response.Trace.CreatedAt}, response.Trace); err != nil {
			return TurnPlan{}, err
		}
		if err := a.recordChosenSilence(ctx, plan, input.FirstMeeting.UserID, input.FirstMeeting.MatchID, input.FirstMeeting.SignalID); err != nil {
			return TurnPlan{}, err
		}
		return plan, nil
	case TurnKindObservation:
		if input.Observation == nil {
			return TurnPlan{}, errors.New("observation resolution input is required")
		}
		observationInput := input.Observation
		resolution := observationInput.Resolution
		if strings.TrimSpace(resolution.ReliableText) == "" {
			return TurnPlan{}, errors.New("observation resolution reliable text is required")
		}
		now := observationInput.Now
		if now.IsZero() {
			now = time.Now().UTC()
		}
		if !resolution.FollowUpDeadline.IsZero() {
			now = resolution.FollowUpDeadline.Add(-time.Hour).UTC()
		}
		trace := Trace{
			ID:                    stableTraceID(resolution.UserID, resolution.MatchID, "observation:"+resolution.DeliveryKey),
			MatchID:               resolution.MatchID,
			UserID:                resolution.UserID,
			Intent:                IntentRecentEvent,
			RetrievedEvent:        compactAnchors(observationInput.EventID),
			Output:                resolution.ReliableText,
			Reason:                "observation_" + string(resolution.Status),
			ObservationResolution: &resolution,
			CreatedAt:             now.UTC(),
			ToolCalls: []ToolCall{
				{Name: "observation.reconcile", Args: map[string]string{"observationId": resolution.ObservationID, "status": string(resolution.Status)}},
				{Name: "response.emit_companion_reply", Args: map[string]string{"mode": "deterministic", "source": "observation_resolution"}},
				{Name: "trace.write_decision", Args: map[string]string{"matchId": resolution.MatchID}},
			},
		}
		if err := a.tools.WriteTrace(ctx, trace); err != nil {
			return TurnPlan{}, err
		}
		plan := TurnPlan{
			Kind:         input.Kind,
			Intent:       IntentRecentEvent,
			Reply:        resolution.ReliableText,
			Trace:        trace,
			Presentation: observationPresentation(resolution.Status),
		}
		if err := a.recordTurn(ctx, interaction.Event{
			ID:            trace.ID,
			Kind:          interaction.KindTurnPlanned,
			SignalID:      resolution.DeliveryKey,
			UserID:        resolution.UserID,
			MatchID:       resolution.MatchID,
			TraceID:       trace.ID,
			FactIDs:       compactAnchors(observationInput.EventID),
			DeliveryKey:   resolution.DeliveryKey,
			DeliveryState: "planned",
			OutputText:    resolution.ReliableText,
			Presentation:  presentationPointer(plan.Presentation),
			Source:        string(input.Kind),
			CreatedAt:     trace.CreatedAt,
		}, trace); err != nil {
			return TurnPlan{}, err
		}
		return plan, nil
	case TurnKindDelivery:
		if input.Delivery == nil {
			return TurnPlan{}, errors.New("delivery input is required")
		}
		delivery := input.Delivery
		decision, err := a.ObserveDelivery(ctx, delivery.SignalID, delivery.UserID, delivery.MatchID, delivery.DecisionID, delivery.State, delivery.Purpose, delivery.UsedMemoryIDs, delivery.Now)
		if err != nil {
			return TurnPlan{}, err
		}
		if err := a.recordInteraction(ctx, interaction.Event{ID: delivery.SignalID, Kind: interaction.KindDelivery, SignalID: delivery.SignalID, UserID: delivery.UserID, MatchID: delivery.MatchID, TraceID: delivery.TraceID, DecisionID: delivery.DecisionID, DeliveryKey: delivery.TraceID, DeliveryState: delivery.State, Source: delivery.Purpose, CreatedAt: delivery.Now}); err != nil {
			return TurnPlan{}, err
		}
		return TurnPlan{Kind: input.Kind, Decision: &decision, Presentation: decision.Presentation}, nil
	default:
		return TurnPlan{}, fmt.Errorf("unsupported turn kind %q", input.Kind)
	}
}

func (a *Agent) recordChosenSilence(ctx context.Context, plan TurnPlan, userID, matchID, signalID string) error {
	if strings.TrimSpace(plan.Reply) != "" || plan.Trace.ID == "" {
		return nil
	}
	return a.recordInteraction(ctx, interaction.Event{
		ID: interactionEventID("delivery", userID, matchID, plan.Trace.ID+":skipped"), Kind: interaction.KindDelivery,
		SignalID: signalID, UserID: userID, MatchID: matchID, TraceID: plan.Trace.ID,
		DecisionID: decisionID(plan.Decision), DeliveryKey: plan.Trace.ID, DeliveryState: "skipped",
		Source: "chosen_silence", CreatedAt: plan.Trace.CreatedAt,
	})
}

func (a *Agent) factRevisionFor(ctx context.Context, matchID string, factIDs []string) string {
	if len(factIDs) == 0 || a == nil || a.tools == nil {
		return ""
	}
	events, err := a.tools.RecentEvents(ctx, matchID, 100)
	if err != nil {
		return ""
	}
	for _, event := range events {
		if containsString(factIDs, event.ID) || containsString(factIDs, event.FactID) {
			return fmt.Sprintf("%d:%s", event.FactRevision, event.FactStatus)
		}
	}
	return ""
}

func (a *Agent) markPriorFactTurnsStale(ctx context.Context, userID, matchID, signalID, currentTraceID string, at time.Time) error {
	if a == nil || a.interactions == nil {
		return nil
	}
	events, err := a.interactions.List(ctx, userID, matchID, 500)
	if err != nil {
		return err
	}
	for _, event := range events {
		if event.Kind != interaction.KindTurnPlanned || event.SignalID != signalID || event.TraceID == "" || event.TraceID == currentTraceID || event.Stale {
			continue
		}
		marker := interaction.Event{
			ID:         interactionEventID("stale", userID, matchID, event.TraceID),
			Kind:       interaction.KindTurnStale,
			SignalID:   signalID,
			UserID:     userID,
			MatchID:    matchID,
			TraceID:    event.TraceID,
			DecisionID: event.DecisionID,
			Stale:      true,
			Source:     "fact_revision",
			CreatedAt:  at,
		}
		if err := a.recordInteraction(ctx, marker); err != nil {
			return err
		}
	}
	return nil
}

func interactionEventForInput(input TurnInput) (interaction.Event, bool) {
	switch input.Kind {
	case TurnKindUser:
		if input.Message == nil || input.Message.SignalID == "" || input.Message.FactRefresh != "" {
			return interaction.Event{}, false
		}
		return interaction.Event{ID: interactionEventID("signal", input.Message.UserID, input.Message.MatchID, input.Message.SignalID), Kind: interaction.KindSignal, SignalID: input.Message.SignalID, UserID: input.Message.UserID, MatchID: input.Message.MatchID, InputText: input.Message.Text, Source: string(input.Kind), CreatedAt: input.Message.Now}, true
	case TurnKindMatchEvent:
		if input.MatchEvent == nil {
			return interaction.Event{}, false
		}
		event := input.MatchEvent.Event
		revision := fmt.Sprintf("%d:%s", event.FactRevision, event.FactStatus)
		return interaction.Event{ID: interactionEventID("fact", input.MatchEvent.UserID, event.MatchID, event.ID+":"+revision), Kind: interaction.KindFactRevision, SignalID: event.ID, UserID: input.MatchEvent.UserID, MatchID: event.MatchID, FactIDs: []string{event.ID}, FactRevision: revision, DeliveryKey: matchstate.DeliveryKey(event), Source: string(input.Kind), CreatedAt: input.MatchEvent.Now}, true
	case TurnKindFirstMeeting:
		if input.FirstMeeting == nil || input.FirstMeeting.SignalID == "" {
			return interaction.Event{}, false
		}
		return interaction.Event{ID: interactionEventID("signal", input.FirstMeeting.UserID, input.FirstMeeting.MatchID, input.FirstMeeting.SignalID), Kind: interaction.KindSignal, SignalID: input.FirstMeeting.SignalID, UserID: input.FirstMeeting.UserID, MatchID: input.FirstMeeting.MatchID, Source: string(input.Kind), CreatedAt: input.FirstMeeting.Now}, true
	case TurnKindObservation:
		if input.Observation == nil {
			return interaction.Event{}, false
		}
		resolution := input.Observation.Resolution
		if resolution.DeliveryKey == "" || resolution.UserID == "" || resolution.MatchID == "" {
			return interaction.Event{}, false
		}
		createdAt := input.Observation.Now
		if !resolution.FollowUpDeadline.IsZero() {
			createdAt = resolution.FollowUpDeadline.Add(-time.Hour).UTC()
		}
		return interaction.Event{
			ID:          interactionEventID("signal", resolution.UserID, resolution.MatchID, resolution.DeliveryKey),
			Kind:        interaction.KindSignal,
			SignalID:    resolution.DeliveryKey,
			UserID:      resolution.UserID,
			MatchID:     resolution.MatchID,
			FactIDs:     compactAnchors(input.Observation.EventID),
			DeliveryKey: resolution.DeliveryKey,
			Source:      string(input.Kind),
			CreatedAt:   createdAt,
		}, true
	case TurnKindDelivery:
		return interaction.Event{}, false
	default:
		return interaction.Event{}, false
	}
}

func interactionEventID(kind, userID, matchID, suffix string) string {
	return strings.Join([]string{kind, userID, matchID, suffix}, ":")
}

func (a *Agent) HandleMessage(ctx context.Context, req MessageRequest) (Response, error) {
	plan, err := a.Plan(ctx, TurnInput{Kind: TurnKindUser, Message: &req})
	if err != nil {
		return Response{}, err
	}
	return Response{Intent: plan.Intent, Reply: plan.Reply, Trace: plan.Trace, Presentation: plan.Presentation, ScheduleLookup: plan.ScheduleLookup}, nil
}

func (a *Agent) handleMessage(ctx context.Context, req MessageRequest) (Response, error) {
	return a.HandleBoundaryRequest(ctx, AgentBoundaryRequest{
		SignalID:            req.SignalID,
		FactRefresh:         req.FactRefresh,
		MatchID:             req.MatchID,
		UserID:              req.UserID,
		Text:                req.Text,
		Timezone:            req.Timezone,
		ProgressiveSchedule: req.ProgressiveSchedule,
		Now:                 req.Now,
		Voice:               req.Voice,
	})
}

func (a *Agent) HandleBoundaryRequest(ctx context.Context, req AgentBoundaryRequest) (Response, error) {
	start := time.Now()
	intent := Classify(req.Text)
	requestTraceID := traceID(req.Now)
	if signalID := strings.TrimSpace(req.SignalID); signalID != "" && len(signalID) <= 256 {
		traceSignalID := signalID
		if refresh := strings.TrimSpace(req.FactRefresh); refresh != "" {
			traceSignalID += "\x00fact-refresh:" + refresh
		}
		requestTraceID = stableTraceID(req.UserID, req.MatchID, traceSignalID)
	}
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
	if intent == IntentSchedule {
		scheduleIntent := ClassifyScheduleIntent(req.Text)
		trace.Schedule = &scheduleIntent
	}

	var reply string
	var requiredAnchors []string
	var scheduleLookup *ScheduleLookup
	allowRealize := true
	deterministicReason := "policy"
	switch intent {
	case IntentMatchClaim:
		allowRealize = false
		deterministicReason = "claim_policy"
		snapshot, err := a.tools.Snapshot(ctx, req.MatchID)
		if err != nil {
			return Response{}, err
		}
		trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "match.read_snapshot", Args: map[string]string{"matchId": req.MatchID}})
		events, err := a.tools.RecentEvents(ctx, req.MatchID, 8)
		if err != nil {
			return Response{}, err
		}
		trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "match.search_events", Args: map[string]string{"matchId": req.MatchID, "limit": "8"}})
		claim, eventIDs, ok := assessMatchClaim(req.Text, snapshot, events)
		if !ok {
			claim = FactClaim{Kind: "match_fact", Status: ClaimStatusUnverified, Reason: "claim could not be parsed"}
		}
		trace.Claim = &claim
		trace.RetrievedEvent = eventIDs
		if snapshot.Period == "pre_match" && claim.EventType == "goal" {
			claim.Status = ClaimStatusContradicted
			claim.Reason = "match has not started"
		}
		trace.Reason = "user_match_claim_" + string(claim.Status)
		trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "match.verify_user_claim", Args: map[string]string{"kind": claim.Kind, "status": string(claim.Status)}})
		if claim.Status == ClaimStatusUnverified {
			a.recordObservation(ctx, req, requestTraceID, claim, &trace)
		}
		if claim.Kind == "score" {
			score := fmt.Sprintf("%d-%d", snapshot.Score.Home, snapshot.Score.Away)
			switch claim.Status {
			case ClaimStatusConfirmed:
				reply = fmt.Sprintf("对，场上现在是%s %s %s。", snapshot.HomeTeam, score, snapshot.AwayTeam)
			case ClaimStatusContradicted:
				reply = fmt.Sprintf("还没，场上现在是%s %s %s。", snapshot.HomeTeam, score, snapshot.AwayTeam)
			default:
				reply = "这条赛况我还不能确定，先别急着算。"
			}
			requiredAnchors = compactAnchors(snapshot.HomeTeam, score, snapshot.AwayTeam)
		} else {
			if claim.Reason == "match has not started" {
				reply = "比赛还没开始，这条不能算。"
				break
			}
			switch claim.Status {
			case ClaimStatusConfirmed:
				if claim.ClaimedPlayer != "" {
					reply = fmt.Sprintf("对，刚才这球是%s进的。", claim.ActualPlayer)
				} else {
					reply = fmt.Sprintf("对，刚才是%s进的，进球的是%s。", claim.ActualTeam, claim.ActualPlayer)
				}
			case ClaimStatusContradicted:
				if claim.ClaimedPlayer != "" {
					reply = fmt.Sprintf("不是%s，刚才这球是%s进的。", claim.ClaimedPlayer, claim.ActualPlayer)
				} else {
					reply = fmt.Sprintf("不是%s，刚才是%s的%s进了。", claim.ClaimedTeam, claim.ActualTeam, claim.ActualPlayer)
				}
			default:
				reply = "我这边还没跟上这粒进球，先等一下看结果。"
			}
			requiredAnchors = compactAnchors(claim.ActualPlayer, claim.ActualTeam)
		}
	case IntentMatchStatus:
		allowRealize = false
		deterministicReason = "fact_policy"
		snapshot, err := a.tools.Snapshot(ctx, req.MatchID)
		if err != nil {
			return Response{}, err
		}
		trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "match.read_snapshot", Args: map[string]string{"matchId": req.MatchID}})
		if issue := snapshotIntegrityIssue(snapshot); issue != "" {
			allowRealize = false
			deterministicReason = "snapshot_integrity"
			trace.Reason = "match_snapshot_inconsistent"
			reply = "这会儿赛况有点对不上，我先不报死，等一下再看。"
			if claim, ok := assessScoreClaim(req.Text, snapshot); ok {
				claim.Status = ClaimStatusUnverified
				claim.Reason = issue
				trace.Claim = &claim
				trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "match.verify_user_claim", Args: map[string]string{"kind": claim.Kind, "status": string(claim.Status)}})
			}
			break
		}
		if claim, ok := assessScoreClaim(req.Text, snapshot); ok {
			allowRealize = false
			deterministicReason = "claim_policy"
			trace.Claim = &claim
			trace.Reason = "user_match_claim_" + string(claim.Status)
			trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "match.verify_user_claim", Args: map[string]string{"kind": claim.Kind, "status": string(claim.Status)}})
		}
		reply = fmt.Sprintf("现在是%s %d-%d %s，时间在%s %s。", snapshot.HomeTeam, snapshot.Score.Home, snapshot.Score.Away, snapshot.AwayTeam, displayPeriod(snapshot.Period), snapshot.Clock)
		requiredAnchors = compactAnchors(snapshot.HomeTeam, snapshot.AwayTeam, fmt.Sprintf("%d-%d", snapshot.Score.Home, snapshot.Score.Away), snapshot.Clock)
	case IntentRecentEvent:
		allowRealize = false
		deterministicReason = "fact_policy"
		events, err := a.tools.RecentEvents(ctx, req.MatchID, 8)
		if err != nil {
			return Response{}, err
		}
		trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "match.search_events", Args: map[string]string{"matchId": req.MatchID, "limit": "8"}})
		reply, trace.RetrievedEvent = answerRecentEvent(req.Text, events)
		requiredAnchors = anchorsForEvents(events, trace.RetrievedEvent, reply)
	case IntentFollowUp:
		allowRealize = false
		deterministicReason = "fact_policy"
		turns, err := a.tools.RecentTurns(ctx, req.MatchID, req.UserID, 8)
		if err != nil {
			return Response{}, err
		}
		trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "conversation.read_recent", Args: map[string]string{"matchId": req.MatchID, "userId": req.UserID, "limit": "8"}})
		events, err := a.tools.RecentEvents(ctx, req.MatchID, 8)
		if err != nil {
			return Response{}, err
		}
		trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "match.search_events", Args: map[string]string{"matchId": req.MatchID, "limit": "8"}})
		reply, trace.RetrievedEvent = answerFollowUp(req.Text, turns, events)
		requiredAnchors = anchorsForEvents(events, trace.RetrievedEvent, reply)
	case IntentPlayerQuestion:
		allowRealize = false
		deterministicReason = "fact_policy"
		player := inferPlayer(req.Text)
		events, err := a.tools.EventsByPlayer(ctx, req.MatchID, player, 8)
		if err != nil {
			return Response{}, err
		}
		trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "match.get_player_timeline", Args: map[string]string{"matchId": req.MatchID, "playerName": player, "limit": "8"}})
		reply, trace.RetrievedEvent = answerPlayerQuestion(req.Text, player, events)
		requiredAnchors = compactAnchors(player)
		requiredAnchors = append(requiredAnchors, anchorsForEvents(events, trace.RetrievedEvent, reply)...)
	case IntentControlCommand:
		reply = "收到，我会少说一点，关键变化再提醒你。"
	case IntentSchedule:
		allowRealize = false
		deterministicReason = "schedule_policy"
		scheduleIntent := *trace.Schedule
		if scheduleIntent.Scope == ScheduleScopeCurrent || scheduleIntent.Scope == ScheduleScopeNearby {
			if snapshot, err := a.tools.Snapshot(ctx, req.MatchID); err == nil {
				trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "match.read_snapshot", Args: map[string]string{"matchId": req.MatchID}})
				if currentReply, ok := activeMatchScheduleReply(snapshot); ok {
					deterministicReason = "active_match_context"
					trace.Reason = "active_match_context"
					reply = currentReply
					break
				}
			}
		}
		searchRequest := BuildScheduleSearchRequest(scheduleIntent, req.Now, req.Timezone)
		searchRequest.Competition = scheduleIntent.Competition
		searchRequest.Query = req.Text
		if searchReader, ok := a.scheduleReader.(ScheduleSearchReader); ok {
			if req.ProgressiveSchedule {
				lookupID := "schedule:" + trace.ID
				trace.LookupID = lookupID
				scheduleLookup = &ScheduleLookup{
					ID:            lookupID,
					ParentTraceID: trace.ID,
					MatchID:       req.MatchID,
					UserID:        req.UserID,
					Query:         req.Text,
					Intent:        scheduleIntent,
					Search:        searchRequest,
					ExpiresAt:     trace.CreatedAt.Add(20 * time.Second),
				}
				trace.ToolCalls = append(trace.ToolCalls, ToolCall{
					Name: "schedule.lookup_pending",
					Args: map[string]string{"lookupId": lookupID, "scope": string(scheduleIntent.Scope)},
				})
				trace.Reason = "schedule_lookup_acknowledgement"
				reply = scheduleLookupAcknowledgement(scheduleIntent.Scope)
				break
			}
			trace.ToolCalls = append(trace.ToolCalls, ToolCall{
				Name: "schedule.search",
				Args: scheduleSearchToolArgs(searchRequest),
			})
			result, err := searchReader.Search(ctx, searchRequest)
			if err != nil {
				trace.Error = strings.TrimSpace(err.Error())
				trace.Reason = "schedule_unavailable"
				reply = "赛程源这次没接上，我不先乱报。"
				break
			}
			reply = formatScheduleSearchResult(result, scheduleIntent.Scope)
			break
		}
		trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "schedule.read_today", Args: map[string]string{"scope": "today"}})
		if a.scheduleReader == nil {
			reply = "今天的赛程我还没拿到，你想查哪个联赛？"
			break
		}
		fixtures, err := a.scheduleReader.TodayFixtures(ctx)
		if err != nil {
			trace.Error = strings.TrimSpace(err.Error())
			trace.Reason = "schedule_unavailable"
			reply = "今天的赛程查询没接上，你想查哪个联赛？"
			break
		}
		reply = formatTodaySchedule(fixtures)
	case IntentEmotionReaction:
		if isGroundedMatchReaction(req.Text) {
			allowRealize = false
			deterministicReason = "deictic_event_grounding"
			events, err := a.tools.RecentEvents(ctx, req.MatchID, 8)
			if err != nil {
				return Response{}, err
			}
			trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "match.search_events", Args: map[string]string{"matchId": req.MatchID, "limit": "8"}})
			var claim FactClaim
			reply, claim, trace.RetrievedEvent = answerDeicticMatchReaction(req.Text, events)
			trace.Claim = &claim
			trace.Reason = "user_event_reference_" + string(claim.Status)
			trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "match.verify_user_claim", Args: map[string]string{"kind": claim.Kind, "status": string(claim.Status)}})
			if claim.Status == ClaimStatusUnverified {
				a.recordObservation(ctx, req, requestTraceID, claim, &trace)
			}
			requiredAnchors = anchorsForEvents(events, trace.RetrievedEvent, reply)
		} else {
			reply = emotionReactionReply(req.Text)
		}
	case IntentPersonalShare:
		reply = personalShareReply(req.Text)
	case IntentSmalltalk:
		reply = smalltalkFallbackReply(req.Text)
	default:
		reply = "这句我没接明白，你换个说法？"
	}
	if intent != IntentPersonalShare && containsMatchFactLanguage(req.Text) && len(requiredAnchors) == 0 {
		allowRealize = false
		if deterministicReason == "policy" {
			deterministicReason = "fact_language_policy"
		}
	}

	recentPhraseHashes := a.recentPhraseHashes(ctx, req.MatchID, req.UserID, allowRealize, &trace)
	if err := a.tools.WriteTrace(ctx, trace); err != nil {
		return Response{}, err
	}
	decision := a.applyDecision(ctx, req, intent, reply, requiredAnchors, recentPhraseHashes, &trace)
	if allowRealize && decision != nil && decision.Speech == nil {
		reply = ""
		trace.Reason = "relationship_chosen_silence"
		trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "response.emit_companion_reply", Args: map[string]string{"mode": "silence"}})
	} else if allowRealize && decision != nil && shouldRealizeUserTurn(intent, *decision) {
		reply = reliableFallbackForDecision(req.Text, intent, reply, *decision)
		if a.realizer != nil && decision.Speech != nil {
			reply = a.realizeReply(ctx, req, intent, reply, requiredAnchors, *decision, &trace)
		} else {
			trace.Reason = "realize_fallback_unavailable"
			trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "response.emit_companion_reply", Args: map[string]string{"mode": "deterministic", "fallback": "realizer_unavailable"}})
		}
	} else {
		trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "response.emit_companion_reply", Args: map[string]string{"mode": "deterministic", "reason": deterministicReason}})
	}
	if decision != nil {
		decision.UsedMemoryIDs = relationshipMemoryIDsUsedByReply(reply, *decision)
		trace.RelationshipDecision = decision
	}
	trace.Output = reply
	trace.LatencyMS = int(time.Since(start).Milliseconds())
	if trace.Reason == "" {
		if !allowRealize && deterministicReason != "policy" {
			trace.Reason = "deterministic_" + deterministicReason
		} else {
			trace.Reason = "deterministic_companion_policy"
		}
	}
	trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "conversation.append_turn", Args: map[string]string{"matchId": req.MatchID, "userId": req.UserID, "roles": "user,qiuqiu"}})
	trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "trace.write_decision", Args: map[string]string{"matchId": req.MatchID, "traceId": trace.ID}})
	if err := a.tools.WriteTrace(ctx, trace); err != nil {
		return Response{}, err
	}
	presentation := relationship.PresentationPlan{}
	if decision != nil {
		presentation = decision.Presentation
	}
	return Response{Intent: intent, Reply: reply, Trace: trace, Presentation: presentation, ScheduleLookup: scheduleLookup}, nil
}

func (a *Agent) ResolveScheduleLookup(ctx context.Context, lookup ScheduleLookup) (Response, error) {
	if !lookup.ExpiresAt.IsZero() && !lookup.ExpiresAt.After(time.Now()) {
		return Response{}, ErrScheduleLookupExpired
	}
	searchReader, ok := a.scheduleReader.(ScheduleSearchReader)
	if !ok {
		return Response{}, fmt.Errorf("schedule search reader is unavailable")
	}
	startedAt := time.Now()
	createdAt := startedAt.UTC()
	intent := lookup.Intent
	trace := Trace{
		ID:            stableTraceID(lookup.UserID, lookup.MatchID, "schedule-result:"+lookup.ID),
		LookupID:      lookup.ID,
		ParentTraceID: lookup.ParentTraceID,
		MatchID:       lookup.MatchID,
		UserID:        lookup.UserID,
		Input:         lookup.Query,
		Intent:        IntentSchedule,
		Schedule:      &intent,
		CreatedAt:     createdAt,
	}
	if intent.Scope == ScheduleScopeCurrent || intent.Scope == ScheduleScopeNearby {
		if snapshot, err := a.tools.Snapshot(ctx, lookup.MatchID); err == nil {
			trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "match.read_snapshot", Args: map[string]string{"matchId": lookup.MatchID}})
			if reply, active := activeMatchScheduleReply(snapshot); active {
				trace.Output = reply
				trace.Reason = "schedule_lookup_context_updated"
				trace.LatencyMS = int(time.Since(startedAt).Milliseconds())
				trace.ToolCalls = append(trace.ToolCalls,
					ToolCall{Name: "response.emit_companion_reply", Args: map[string]string{"mode": "deterministic", "source": "schedule_lookup"}},
					ToolCall{Name: "trace.write_decision", Args: map[string]string{"matchId": lookup.MatchID, "traceId": trace.ID}},
				)
				if err := a.tools.WriteTrace(ctx, trace); err != nil {
					return Response{}, err
				}
				if err := a.recordScheduleLookupTurn(ctx, lookup, trace); err != nil {
					return Response{}, err
				}
				return Response{Intent: IntentSchedule, Reply: reply, Trace: trace, Presentation: scheduleLookupPresentation()}, nil
			}
		}
	}

	searchArgs := scheduleSearchToolArgs(lookup.Search)
	result, err := searchReader.Search(ctx, lookup.Search)
	if ctx.Err() != nil {
		return Response{}, ctx.Err()
	}
	if intent.Scope == ScheduleScopeCurrent || intent.Scope == ScheduleScopeNearby {
		if snapshot, snapshotErr := a.tools.Snapshot(ctx, lookup.MatchID); snapshotErr == nil {
			trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "match.read_snapshot", Args: map[string]string{"matchId": lookup.MatchID}})
			if currentReply, active := activeMatchScheduleReply(snapshot); active {
				searchArgs["state"] = "superseded"
				searchArgs["source"] = strings.TrimSpace(result.Source)
				searchArgs["durationMs"] = strconv.Itoa(int(time.Since(startedAt).Milliseconds()))
				trace.ToolCalls = append(trace.ToolCalls,
					ToolCall{Name: "schedule.search", Args: searchArgs},
					ToolCall{Name: "response.emit_companion_reply", Args: map[string]string{"mode": "deterministic", "source": "schedule_lookup"}},
					ToolCall{Name: "trace.write_decision", Args: map[string]string{"matchId": lookup.MatchID, "traceId": trace.ID}},
				)
				trace.Output = currentReply
				trace.Reason = "schedule_lookup_context_updated"
				trace.LatencyMS = int(time.Since(startedAt).Milliseconds())
				if err := a.tools.WriteTrace(ctx, trace); err != nil {
					return Response{}, err
				}
				if err := a.recordScheduleLookupTurn(ctx, lookup, trace); err != nil {
					return Response{}, err
				}
				return Response{Intent: IntentSchedule, Reply: currentReply, Trace: trace, Presentation: scheduleLookupPresentation()}, nil
			}
		}
	}
	reply := ""
	if err != nil {
		trace.Error = strings.TrimSpace(err.Error())
		trace.Reason = "schedule_lookup_unavailable"
		reply = "赛程源这次没接上，我不先乱报。"
		searchArgs["state"] = "failed"
	} else {
		reply = formatScheduleSearchResult(result, intent.Scope)
		trace.Reason = "schedule_lookup_result"
		searchArgs["state"] = "completed"
		searchArgs["source"] = strings.TrimSpace(result.Source)
		searchArgs["freshness"] = strings.TrimSpace(result.Freshness)
		searchArgs["fixtureCount"] = strconv.Itoa(len(result.Fixtures))
		if !result.FetchedAt.IsZero() {
			searchArgs["fetchedAt"] = result.FetchedAt.UTC().Format(time.RFC3339)
		}
	}
	searchArgs["durationMs"] = strconv.Itoa(int(time.Since(startedAt).Milliseconds()))
	trace.ToolCalls = append(trace.ToolCalls,
		ToolCall{Name: "schedule.search", Args: searchArgs},
		ToolCall{Name: "response.emit_companion_reply", Args: map[string]string{"mode": "deterministic", "source": "schedule_lookup"}},
		ToolCall{Name: "trace.write_decision", Args: map[string]string{"matchId": lookup.MatchID, "traceId": trace.ID}},
	)
	trace.Output = reply
	trace.LatencyMS = int(time.Since(startedAt).Milliseconds())
	if err := a.tools.WriteTrace(ctx, trace); err != nil {
		return Response{}, err
	}
	if err := a.recordScheduleLookupTurn(ctx, lookup, trace); err != nil {
		return Response{}, err
	}
	return Response{Intent: IntentSchedule, Reply: reply, Trace: trace, Presentation: scheduleLookupPresentation()}, nil
}

func scheduleLookupAcknowledgement(scope ScheduleScope) string {
	switch scope {
	case ScheduleScopeToday:
		return "我去找找今天的比赛，找到后告诉你。"
	case ScheduleScopeTomorrow:
		return "我去找找明天的比赛，找到后告诉你。"
	default:
		return "我去找找今天和明天的比赛，找到后告诉你。"
	}
}

func scheduleLookupPresentation() relationship.PresentationPlan {
	return relationship.PresentationPlan{
		Expression:  "focus",
		Motion:      "speak",
		VoiceStyle:  "calm",
		VoiceEnergy: 0.55,
		VoiceSpeed:  1,
		HoldMS:      1400,
		ReturnMode:  "watching",
	}
}

func (a *Agent) recordObservation(ctx context.Context, req AgentBoundaryRequest, traceID string, claim FactClaim, trace *Trace) {
	if a.observations == nil || trace == nil {
		return
	}
	signalID := strings.TrimSpace(req.SignalID)
	if signalID == "" {
		signalID = traceID
	}
	reconcileWindow := time.Duration(0)
	if a.observationReconcileWindow != nil {
		reconcileWindow = a.observationReconcileWindow(req.MatchID, claim.EventType)
	}
	recorded, err := a.observations.Record(ctx, observation.Input{
		SignalID:        signalID,
		TraceID:         traceID,
		UserID:          req.UserID,
		MatchID:         req.MatchID,
		Kind:            claim.Kind,
		EventType:       claim.EventType,
		ClaimedTeam:     claim.ClaimedTeam,
		ClaimedPlayer:   claim.ClaimedPlayer,
		ClaimedScore:    claim.ClaimedScore,
		Certainty:       claim.Certainty,
		ReceivedAt:      trace.CreatedAt,
		ReconcileWindow: reconcileWindow,
	})
	if err != nil {
		trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "observation.record", Args: map[string]string{"status": "error"}})
		return
	}
	trace.Observation = &recorded
	trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "observation.record", Args: map[string]string{"status": string(recorded.Status), "observationId": recorded.ID}})
}

func reliableFallbackForDecision(input string, intent Intent, original string, decision relationship.Decision) string {
	if hasCommunicationAct(decision.Actions, relationship.ActRecall) {
		if topic := recalledOpenThreadTopic(decision.Memories); topic != "" {
			return "上次说到" + topic + "，接着聊。"
		}
	}
	if hasCommunicationAct(decision.Actions, relationship.ActRepair) {
		switch decision.Relationship.RepairCategory {
		case "over_analysis":
			return "行，收住。刚才确实说多了。"
		case "repetition":
			return "对，这句我又说顺嘴了。收掉。"
		case "banter_boundary":
			return "行，这个不拿你开玩笑了。"
		default:
			return "行，刚才那下没接好。我收住。"
		}
	}
	if hasCommunicationAct(decision.Actions, relationship.ActAsk) {
		return "这点我记住。你更吃哪一点？"
	}
	if intent == IntentEmotionReaction {
		return emotionReactionReply(input)
	}
	if intent == IntentPersonalShare {
		return personalShareReply(input)
	}
	if intent == IntentSmalltalk {
		return smalltalkFallbackReply(input)
	}
	return original
}

func smalltalkFallbackReply(input string) string {
	switch {
	case isAudioConnectionCheck(input):
		return "听得到。"
	case isPresenceCheck(input):
		return "在，听着呢。"
	case isCompanionStateCompliment(input):
		return "被你看出来了，今天状态确实不错。"
	case isCompanionDirectedSmalltalk(input) && containsAny(input, "有点意思", "挺有意思", "真有意思"):
		return "那我就当你是在夸我了。"
	case isCompanionDirectedSmalltalk(input):
		return "这句我收下了。"
	case isNonLiteralMatchAside(input):
		return "知道，你是在逗我。"
	case isGreeting(input):
		return greetingReply(input)
	case containsAny(input, "陪我看", "一起看", "陪我聊"):
		return "行，陪你看着。"
	case containsAny(input, "继续", "接着"):
		return "嗯，接着看。"
	case containsAny(input, "先看", "看看比赛", "看球"):
		return "行，先看着。"
	case containsAny(input, "谢谢", "谢了", "多谢"):
		return "客气什么。"
	case containsAny(input, "累", "困", "难受", "烦", "不舒服"):
		return "听着就不太顺，先缓口气。"
	case containsAny(input, "开心", "高兴", "爽", "兴奋"):
		return "听出来了，你这会儿心情是真不错。"
	default:
		return "嗯，我听着呢。"
	}
}

func personalShareReply(input string) string {
	switch {
	case containsAny(input, "打进", "踢进", "进球", "赢了", "赢啦", "拿下", "成功", "做到了"):
		return "可以啊，这下够你得意一阵了。"
	case containsAny(input, "累", "困", "难受", "烦", "输了", "不舒服"):
		return "听着就不太顺，先缓口气。"
	case containsAny(input, "开心", "高兴", "爽", "兴奋"):
		return "听出来了，你这会儿心情是真不错。"
	case containsAny(input, "喜欢", "支持", "更看好"):
		return "行，这个立场我记住了。"
	default:
		return "这话我听进去了。"
	}
}

func emotionReactionReply(input string) string {
	switch {
	case isDisbeliefReaction(input):
		return "真的假的？你是说刚刚那一下吗？"
	case containsAny(input, "紧张", "悬", "绷"):
		return "这一下是真绷着。先看这波。"
	case containsAny(input, "好球", "精彩", "厉害", "太棒", "神了", "绝了"):
		return "这一下有点东西。"
	case containsAny(input, "漂亮", "舒服"):
		return "嗯，这一下真漂亮。"
	case containsAny(input, "牛", "太激动", "上头"):
		return "这下确实顶。"
	default:
		return "嗯，这一下有感觉。"
	}
}

func isGroundedMatchReaction(input string) bool {
	lower := strings.ToLower(strings.TrimSpace(input))
	if !containsMatchReactionCue(lower) {
		return false
	}
	return containsMatchFactLanguage(lower) || containsAny(lower,
		"这球", "这个球", "这一球", "那球", "那个球", "那一球", "这一下", "那一下", "这脚", "那脚", "这一脚", "那一脚",
	)
}

func containsMatchReactionCue(input string) bool {
	return containsAny(strings.ToLower(strings.TrimSpace(input)),
		"漂亮", "舒服", "精彩", "好球", "牛", "厉害", "关键",
		"太棒", "神了", "绝了", "可惜", "离谱",
	)
}

func answerDeicticMatchReaction(text string, events []matchstate.MatchEvent) (string, FactClaim, []string) {
	claim := FactClaim{
		Kind:      "event_reference",
		EventType: "play",
		Certainty: claimCertainty(text),
		Status:    ClaimStatusUnverified,
	}
	var event *matchstate.MatchEvent
	for index := range events {
		if isReferencableMatchEvent(events[index].EventType) {
			event = &events[index]
			break
		}
	}
	if event == nil {
		claim.Reason = "no recent confirmed match event"
		return "我这边还没看到你说的那一下，先不跟着瞎认。", claim, nil
	}
	claim.EventType = event.EventType
	claim.ActualPlayer = strings.TrimSpace(event.PlayerName)
	claim.ActualTeam = strings.TrimSpace(event.TeamName)
	claim.Status = ClaimStatusConfirmed
	claim.Reason = "matched latest confirmed match event"
	return fmt.Sprintf("这下我能接，刚才%s这一下确实漂亮：%s", event.Clock, event.Description), claim, []string{event.ID}
}

func isReferencableMatchEvent(eventType string) bool {
	switch strings.TrimSpace(eventType) {
	case "", "kickoff", "match_start", "half_time", "halftime", "fulltime", "match_end":
		return false
	default:
		return true
	}
}

func isDisbeliefReaction(input string) bool {
	return containsAny(input, "真的假的", "真的吗", "认真的吗", "不会吧", "不是吧", "开玩笑吧")
}

func shouldRealizeUserTurn(intent Intent, decision relationship.Decision) bool {
	if decision.Speech == nil {
		return false
	}
	return intent == IntentSmalltalk || intent == IntentEmotionReaction || intent == IntentPersonalShare || hasCommunicationAct(decision.Actions, relationship.ActRecall) || hasCommunicationAct(decision.Actions, relationship.ActRepair) || hasCommunicationAct(decision.Actions, relationship.ActAsk)
}

func recalledOpenThreadTopic(memories []relationship.RelationshipMemory) string {
	for _, memory := range memories {
		if memory.Kind != relationship.MemoryKindOpenThread {
			continue
		}
		var payload relationship.OpenThreadMemoryPayload
		if json.Unmarshal(memory.Payload, &payload) == nil && strings.TrimSpace(payload.Topic) != "" {
			return strings.TrimSpace(payload.Topic)
		}
	}
	return ""
}

func relationshipMemoryIDsUsedByReply(reply string, decision relationship.Decision) []string {
	if strings.TrimSpace(reply) == "" || !hasCommunicationAct(decision.Actions, relationship.ActRecall) {
		return nil
	}
	used := make([]string, 0, len(decision.Memories))
	for _, memory := range decision.Memories {
		if memory.Kind != relationship.MemoryKindOpenThread {
			continue
		}
		used = append(used, memory.ID)
	}
	return used
}

func (a *Agent) applyDecision(ctx context.Context, req AgentBoundaryRequest, intent Intent, reply string, requiredAnchors []string, recentPhraseHashes []uint64, trace *Trace) *relationship.Decision {
	if a.director == nil || trace == nil {
		return nil
	}
	signalID := strings.TrimSpace(req.SignalID)
	if signalID == "" {
		signalID = "turn:" + trace.ID
	}
	factMode := relationship.FactModeNone
	if len(requiredAnchors) > 0 {
		factMode = relationship.FactModeAnchored
	}
	if trace.Claim != nil && trace.Claim.Status == ClaimStatusUnverified {
		factMode = relationship.FactModeUnverified
	} else if trace.Claim != nil || isFactIntent(intent) {
		factMode = relationship.FactModeDeterministic
	}
	decision, err := a.director.Apply(ctx, relationship.Signal{
		ID:           signalID,
		TraceID:      trace.ID,
		Kind:         relationship.SignalUserTurn,
		UserID:       req.UserID,
		MatchID:      req.MatchID,
		OccurredAt:   trace.CreatedAt,
		ReceivedAt:   time.Now().UTC(),
		FactRevision: strings.Join(trace.RetrievedEvent, ","),
		User:         &relationship.UserSignal{Text: req.Text},
		Grounding: relationship.GroundedContent{
			Intent:             string(intent),
			ReliableText:       reply,
			RequiredAnchors:    append([]string(nil), requiredAnchors...),
			FactMode:           factMode,
			SourceEventIDs:     append([]string(nil), trace.RetrievedEvent...),
			RecentPhraseHashes: append([]uint64(nil), recentPhraseHashes...),
		},
	})
	if err != nil {
		trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "relationship.apply", Args: map[string]string{"status": "error", "reason": err.Error()}})
		return nil
	}
	trace.RelationshipDecision = &decision
	trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "relationship.apply", Args: map[string]string{"status": "ok", "decisionId": decision.ID}})
	return &decision
}

func isFactIntent(intent Intent) bool {
	switch intent {
	case IntentMatchClaim, IntentMatchStatus, IntentRecentEvent, IntentFollowUp, IntentPlayerQuestion:
		return true
	default:
		return false
	}
}

func (a *Agent) HandleMatchEvent(ctx context.Context, req MatchEventRequest) (ProactiveResponse, error) {
	plan, err := a.Plan(ctx, TurnInput{Kind: TurnKindMatchEvent, MatchEvent: &req})
	if err != nil {
		return ProactiveResponse{}, err
	}
	decision := relationship.Decision{}
	if plan.Decision != nil {
		decision = *plan.Decision
	}
	return ProactiveResponse{Reply: plan.Reply, Trace: plan.Trace, Decision: decision, Presentation: plan.Presentation}, nil
}

func (a *Agent) handleMatchEvent(ctx context.Context, req MatchEventRequest) (ProactiveResponse, error) {
	if req.Now.IsZero() {
		req.Now = time.Now().UTC()
	}
	start := time.Now()
	deliveryKey := matchstate.DeliveryKey(req.Event)
	trace := Trace{
		ID:             stableTraceID(req.UserID, req.Event.MatchID, "match:"+deliveryKey),
		MatchID:        req.Event.MatchID,
		UserID:         req.UserID,
		Input:          req.Event.Description,
		Intent:         IntentMatchReaction,
		CreatedAt:      req.Now,
		RetrievedEvent: []string{req.Event.ID},
		Reason:         "relationship_match_observation_pending",
	}
	if err := a.tools.WriteTrace(ctx, trace); err != nil {
		return ProactiveResponse{}, err
	}
	decision, err := a.observeMatchEvent(
		ctx,
		req.UserID,
		req.Event,
		req.OutputAllowed,
		req.Critical,
		req.UserSpeaking,
		req.NormalCooldownSeconds,
		req.Now,
	)
	if err != nil {
		return ProactiveResponse{}, err
	}
	trace.Reason = "relationship_match_reaction"
	if req.Critical && decision.ID != "" && decision.FactRevision != deliveryKey {
		req.OutputAllowed = false
		trace.Reason = "critical_fact_refresh_limit"
	}
	if decision.ID != "" {
		trace.RelationshipDecision = &decision
		trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "relationship.apply", Args: map[string]string{"status": "ok", "decisionId": decision.ID}})
	} else {
		trace.Reason = "operator_event_proactive_line"
	}
	reply := ""
	if req.OutputAllowed && (decision.ID == "" || decision.Speech != nil) {
		trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "response.emit_companion_reply", Args: map[string]string{
			"eventId": req.Event.ID, "deliveryKey": deliveryKey, "eventType": req.Event.EventType, "clock": req.Event.Clock,
		}})
		reply = req.Event.ProactiveText
		if strings.TrimSpace(reply) == "" {
			reply = fallbackProactive(req.Event, req.Snapshot)
		}
	} else if trace.Reason != "critical_fact_refresh_limit" {
		trace.Reason = "relationship_match_observed_silent"
		trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "response.emit_companion_reply", Args: map[string]string{
			"eventId": req.Event.ID, "deliveryKey": deliveryKey, "eventType": req.Event.EventType, "mode": "silence",
		}})
	}
	trace.Output = reply
	trace.LatencyMS = int(time.Since(start).Milliseconds())
	trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "trace.write_decision", Args: map[string]string{
		"matchId": req.Event.MatchID,
		"traceId": trace.ID,
	}})
	if err := a.tools.WriteTrace(ctx, trace); err != nil {
		return ProactiveResponse{}, err
	}
	return ProactiveResponse{
		Reply:        reply,
		Trace:        trace,
		Decision:     decision,
		Presentation: decision.Presentation,
	}, nil
}

func (a *Agent) observeMatchEvent(ctx context.Context, userID string, ev matchstate.MatchEvent, outputAllowed, critical, userSpeaking bool, normalCooldownSeconds int, now time.Time) (relationship.Decision, error) {
	if a == nil || a.director == nil {
		return relationship.Decision{}, nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	deliveryKey := matchstate.DeliveryKey(ev)
	signalID := "match:" + userID + ":" + ev.ID
	return a.director.Apply(ctx, relationship.Signal{
		ID:           signalID,
		TraceID:      stableTraceID(userID, ev.MatchID, "match:"+deliveryKey),
		Kind:         relationship.SignalMatchEvent,
		UserID:       userID,
		MatchID:      ev.MatchID,
		OccurredAt:   now,
		ReceivedAt:   time.Now().UTC(),
		FactRevision: deliveryKey,
		Match: &relationship.MatchSignal{
			EventID:               ev.ID,
			EventType:             ev.EventType,
			Intensity:             ev.Intensity,
			Confirmed:             ev.Confirmed,
			OutputAllowed:         outputAllowed,
			Critical:              critical,
			UserSpeaking:          userSpeaking,
			NormalCooldownSeconds: normalCooldownSeconds,
			Description:           ev.Description,
			TeamName:              ev.TeamName,
			PlayerName:            ev.PlayerName,
			RevisionOf:            ev.RevisionOf,
		},
		Grounding: relationship.GroundedContent{
			Intent:         string(IntentRecentEvent),
			ReliableText:   ev.ProactiveText,
			FactMode:       relationship.FactModeDeterministic,
			SourceEventIDs: []string{ev.ID},
		},
	})
}

func (a *Agent) HandleFirstMeeting(ctx context.Context, req FirstMeetingRequest) (ProactiveResponse, error) {
	plan, err := a.Plan(ctx, TurnInput{Kind: TurnKindFirstMeeting, FirstMeeting: &req})
	if err != nil {
		return ProactiveResponse{}, err
	}
	decision := relationship.Decision{}
	if plan.Decision != nil {
		decision = *plan.Decision
	}
	return ProactiveResponse{Reply: plan.Reply, Trace: plan.Trace, Decision: decision, Presentation: plan.Presentation}, nil
}

func (a *Agent) handleFirstMeeting(ctx context.Context, req FirstMeetingRequest) (ProactiveResponse, error) {
	startedAt := time.Now()
	if req.Now.IsZero() {
		req.Now = startedAt
	}
	signalID := strings.TrimSpace(req.SignalID)
	if signalID == "" {
		signalID = "session:" + req.UserID + ":" + req.MatchID
	}
	reply := firstMeetingGreeting(req.Nickname, req.FavoriteTeam)
	trace := Trace{
		ID:        stableTraceID(req.UserID, req.MatchID, signalID),
		MatchID:   req.MatchID,
		UserID:    req.UserID,
		Input:     "first_meeting",
		Intent:    IntentSmalltalk,
		Output:    reply,
		Reason:    "first_meeting_welcome",
		CreatedAt: req.Now,
		ToolCalls: []ToolCall{
			{Name: "response.emit_companion_reply", Args: map[string]string{"source": "first_meeting"}},
			{Name: "trace.write_decision", Args: map[string]string{"matchId": req.MatchID}},
		},
	}
	if a.director != nil {
		decision, err := a.ObserveSession(ctx, signalID, req.UserID, req.MatchID, req.Now)
		if err == nil {
			trace.RelationshipDecision = &decision
			trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "relationship.apply", Args: map[string]string{"status": "ok", "decisionId": decision.ID}})
			if decision.Relationship.GreetingDelivered {
				reply = ""
				trace.Output = ""
				trace.Reason = "first_meeting_already_delivered"
				trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "response.emit_companion_reply", Args: map[string]string{
					"mode": "silence", "reason": "already_delivered",
				}})
			} else if a.realizer != nil && decision.Speech != nil {
				reply = a.realizeReply(ctx, AgentBoundaryRequest{
					MatchID: req.MatchID,
					UserID:  req.UserID,
					Text:    "first_meeting",
				}, IntentSmalltalk, reply, nil, decision, &trace)
				trace.Output = reply
			}
		} else {
			trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "relationship.apply", Args: map[string]string{"status": "error", "reason": err.Error()}})
		}
	}
	trace.LatencyMS = int(time.Since(startedAt).Milliseconds())
	if err := a.tools.WriteTrace(ctx, trace); err != nil {
		return ProactiveResponse{}, err
	}
	response := ProactiveResponse{Reply: reply, Trace: trace}
	if trace.RelationshipDecision != nil {
		response.Decision = *trace.RelationshipDecision
		response.Presentation = trace.RelationshipDecision.Presentation
	}
	return response, nil
}

func (a *Agent) ObserveSession(ctx context.Context, signalID, userID, matchID string, now time.Time) (relationship.Decision, error) {
	if a == nil || a.director == nil {
		return relationship.Decision{}, nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return a.director.Apply(ctx, relationship.Signal{
		ID:         signalID,
		Kind:       relationship.SignalSessionOpened,
		UserID:     userID,
		MatchID:    matchID,
		OccurredAt: now,
		ReceivedAt: time.Now().UTC(),
		Grounding: relationship.GroundedContent{
			Intent:   string(IntentSmalltalk),
			FactMode: relationship.FactModeNone,
		},
	})
}

func (a *Agent) ObserveDelivery(ctx context.Context, signalID, userID, matchID, decisionID, state, purpose string, usedMemoryIDs []string, now time.Time) (relationship.Decision, error) {
	if a == nil || a.director == nil {
		return relationship.Decision{}, nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return a.director.Apply(ctx, relationship.Signal{
		ID:         signalID,
		Kind:       relationship.SignalDeliveryResult,
		UserID:     userID,
		MatchID:    matchID,
		OccurredAt: now,
		ReceivedAt: time.Now().UTC(),
		Delivery: &relationship.DeliverySignal{
			DecisionID:    decisionID,
			State:         state,
			Purpose:       purpose,
			UsedMemoryIDs: append([]string(nil), usedMemoryIDs...),
		},
	})
}

func isCriticalMatchEvent(eventType string) bool {
	switch eventType {
	case "goal", "penalty", "penalty_awarded", "red_card", "var_result", "goal_cancelled", "match_end":
		return true
	default:
		return false
	}
}

func firstMeetingGreeting(nickname, favoriteTeam string) string {
	nickname = shortLabel(nickname, 20)
	favoriteTeam = shortLabel(favoriteTeam, 24)
	if nickname != "" && favoriteTeam != "" {
		return fmt.Sprintf("嗨，%s，我叫球球。你看%s，那这场应该有得聊。", nickname, favoriteTeam)
	}
	if nickname != "" {
		return fmt.Sprintf("嗨，%s，我叫球球。第一次一起看球，先看着。", nickname)
	}
	return "嗨，我叫球球。第一次一起看球，先看着。"
}

func shortLabel(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) > limit {
		runes = runes[:limit]
	}
	return string(runes)
}

func Classify(text string) Intent {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return IntentUnknown
	}
	lower := strings.ToLower(trimmed)
	if containsAny(lower, "把比分改成", "比分改成", "修改比分", "记录进球", "记一条进球") {
		return IntentUnknown
	}
	if isPresenceCheck(lower) {
		return IntentSmalltalk
	}
	if isNonLiteralMatchAside(lower) {
		return IntentSmalltalk
	}
	if isScoreClaim(lower) || isEventClaim(lower) {
		return IntentMatchClaim
	}
	if scoreClaimPattern.FindStringSubmatch(lower) != nil && containsAny(lower, "吗", "么", "是不是", "?", "？") {
		return IntentMatchStatus
	}
	if containsAny(lower, "别说", "少说", "闭嘴", "安静", "别播报") {
		return IntentControlCommand
	}
	if isMatchStatusQuestion(lower) {
		return IntentMatchStatus
	}
	if isScheduleQuestion(lower) {
		return IntentSchedule
	}
	if isCompanionDirectedSmalltalk(lower) {
		return IntentSmalltalk
	}
	if isDisbeliefReaction(lower) {
		return IntentEmotionReaction
	}
	if containsAny(lower, "哈哈", "太激动", "紧张", "上头") || containsMatchReactionCue(lower) {
		return IntentEmotionReaction
	}
	if isRecentGoalScorerQuestion(lower) {
		return IntentRecentEvent
	}
	if isPersonalShare(lower) {
		return IntentPersonalShare
	}
	if containsAny(lower, "进球了吗", "表现", "有没有进球", "有进球") {
		return IntentPlayerQuestion
	}
	if containsAny(lower, "谁助攻", "谁主攻", "助攻", "刚才谁", "上一个", "刚刚") {
		return IntentRecentEvent
	}
	if containsAny(lower, "最新", "刚才", "上一球") && containsAny(lower, "进球", "破门", "比赛") {
		return IntentRecentEvent
	}
	if containsAny(lower, "谁策动", "策动", "谁传的", "谁参与", "那球呢", "然后呢") {
		return IntentFollowUp
	}
	if isGreeting(lower) {
		return IntentSmalltalk
	}
	if isSimpleSocialTurn(lower) {
		return IntentSmalltalk
	}
	return IntentUnknown
}

func isScheduleQuestion(text string) bool {
	normalized := normalizeConversationText(text)
	if containsAny(normalized, "比分", "进球", "分钟", "赛况", "球员", "比赛怎么样", "比赛什么情况", "比赛现在什么情况", "现在什么情况") {
		return false
	}
	footballTopic := containsAny(normalized, "比赛", "球赛", "赛程", "对阵", "足球", "有球", "什么球", "啥球", "哪些球")
	questionAct := containsAny(normalized, "有", "什么", "哪些", "哪场", "哪几场", "安排", "踢", "开赛")
	return footballTopic && questionAct
}

func ClassifyScheduleIntent(text string) ScheduleIntent {
	normalized := normalizeConversationText(text)
	scope := ScheduleScopeNearby
	switch {
	case containsAny(normalized, "明天", "明日"):
		scope = ScheduleScopeTomorrow
	case containsAny(normalized, "今天", "今日", "今晚"):
		scope = ScheduleScopeToday
	case containsAny(normalized, "现在", "当前", "正在"):
		scope = ScheduleScopeCurrent
	}
	return ScheduleIntent{
		Topic:      "football_schedule",
		Action:     "query",
		Scope:      scope,
		Confidence: 0.9,
	}
}

func BuildScheduleSearchRequest(intent ScheduleIntent, now time.Time, timezone string) ScheduleSearchRequest {
	location := scheduleLocation(now, timezone)
	if now.IsZero() {
		now = time.Now()
	}
	localNow := now.In(location)
	start := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, location)
	switch intent.Scope {
	case ScheduleScopeTomorrow:
		start = start.AddDate(0, 0, 1)
	case ScheduleScopeNearby:
		return ScheduleSearchRequest{
			From:        start,
			To:          start.AddDate(0, 0, 2),
			Timezone:    location.String(),
			Competition: intent.Competition,
		}
	}
	return ScheduleSearchRequest{
		From:        start,
		To:          start.AddDate(0, 0, 1),
		Timezone:    location.String(),
		Competition: intent.Competition,
	}
}

func scheduleLocation(now time.Time, timezone string) *time.Location {
	if name := strings.TrimSpace(timezone); name != "" {
		if location, err := time.LoadLocation(name); err == nil {
			return location
		}
		normalized := strings.ToUpper(name)
		if strings.HasPrefix(normalized, "UTC") {
			if parsed, err := time.Parse("Z07:00", strings.TrimPrefix(normalized, "UTC")); err == nil {
				_, offsetSeconds := parsed.Zone()
				return time.FixedZone(normalized, offsetSeconds)
			}
		}
	}
	if now.Location() != nil {
		return now.Location()
	}
	return time.UTC
}

func scheduleSearchToolArgs(request ScheduleSearchRequest) map[string]string {
	return map[string]string{
		"from":     request.From.Format(time.RFC3339),
		"to":       request.To.Format(time.RFC3339),
		"timezone": request.Timezone,
	}
}

func isSimpleSocialTurn(text string) bool {
	normalized := normalizeConversationText(text)
	if normalized == "" || containsAny(strings.ToLower(strings.TrimSpace(text)),
		"吗", "么", "是不是", "为什么", "怎么", "什么", "哪", "多少", "?", "？",
	) {
		return false
	}
	for _, exact := range []string{
		"好", "好的", "好呀", "好啊", "好吧", "行", "行吧", "嗯", "嗯嗯", "收到", "谢谢", "谢了", "多谢",
		"继续", "接着", "你说", "看球", "累", "困", "难受", "烦", "不舒服", "开心", "高兴", "爽", "兴奋",
	} {
		if normalized == exact {
			return true
		}
	}
	return hasAnyPrefix(normalized,
		"谢谢", "谢了", "多谢", "继续", "接着", "先看", "看球", "收到", "辛苦", "你说",
		"陪我看", "陪我聊", "一起看", "接着看", "继续看", "累", "困", "难受", "烦", "不舒服",
		"开心", "高兴", "爽", "兴奋",
	)
}

func isNonLiteralMatchAside(text string) bool {
	return isNonLiteralMatchTalk(text) && (scoreClaimPattern.FindStringSubmatch(text) != nil || containsAny(text,
		"进球了", "破门了", "得分了", "进啦", "进咯", "进喽", "球进了",
	))
}

func formatTodaySchedule(fixtures []ScheduleMatch) string {
	if len(fixtures) == 0 {
		return "我这边查到今天暂时没有赛程。"
	}
	pairs := make([]string, 0, minInt(len(fixtures), 5))
	for _, fixture := range fixtures {
		home := strings.TrimSpace(fixture.HomeTeam)
		away := strings.TrimSpace(fixture.AwayTeam)
		if home == "" || away == "" {
			continue
		}
		pairs = append(pairs, home+"对"+away)
		if len(pairs) >= 5 {
			break
		}
	}
	if len(pairs) == 0 {
		return "我这边查到今天暂时没有赛程。"
	}
	return "今天有：" + strings.Join(pairs, "、") + "。"
}

func formatScheduleSearchResult(result ScheduleSearchResult, scope ScheduleScope) string {
	if scheduleFreshnessUnreliable(result.Freshness) {
		return "赛程数据现在有延迟或冲突，我先不拿它当准确信息报给你。"
	}
	if len(result.Fixtures) == 0 {
		if scope == ScheduleScopeNearby {
			return "我查到今天和明天暂时没有可靠的赛程。"
		}
		return "我查到这个时间段暂时没有可靠的赛程。"
	}
	pairs := make([]string, 0, minInt(len(result.Fixtures), 5))
	for _, fixture := range result.Fixtures {
		if scheduleFreshnessUnreliable(fixture.Freshness) {
			continue
		}
		home := strings.TrimSpace(fixture.HomeTeam)
		away := strings.TrimSpace(fixture.AwayTeam)
		if home == "" || away == "" {
			continue
		}
		label := home + "对" + away
		details := make([]string, 0, 3)
		if competition := strings.TrimSpace(fixture.Competition); competition != "" {
			details = append(details, competition)
		}
		if !fixture.KickoffAt.IsZero() {
			details = append(details, fixture.KickoffAt.Format("15:04")+"开球")
		}
		if status := scheduleStatusLabel(fixture.Status); status != "" {
			details = append(details, status)
		}
		if len(details) > 0 {
			label += "（" + strings.Join(details, "，") + "）"
		}
		if fixture.HomeScore != nil && fixture.AwayScore != nil {
			label += fmt.Sprintf(" %d-%d", *fixture.HomeScore, *fixture.AwayScore)
		}
		pairs = append(pairs, label)
		if len(pairs) >= 5 {
			break
		}
	}
	if len(pairs) == 0 {
		return "我查到这个时间段暂时没有可靠的赛程。"
	}
	label := "这个时间段"
	switch scope {
	case ScheduleScopeToday:
		label = "今天"
	case ScheduleScopeTomorrow:
		label = "明天"
	case ScheduleScopeNearby:
		label = "今天和明天"
	}
	reply := label + "有：" + strings.Join(pairs, "、") + "。"
	source := strings.TrimSpace(result.Source)
	if source == "" {
		for _, fixture := range result.Fixtures {
			if source = strings.TrimSpace(fixture.Source); source != "" {
				break
			}
		}
	}
	metadata := make([]string, 0, 2)
	if source != "" {
		metadata = append(metadata, "来源："+source)
	}
	if freshness := scheduleFreshnessLabel(result.Freshness); freshness != "" {
		metadata = append(metadata, freshness)
	}
	if len(metadata) > 0 {
		reply += strings.Join(metadata, "，") + "。"
	}
	return reply
}

func scheduleFreshnessUnreliable(freshness string) bool {
	switch strings.ToLower(strings.TrimSpace(freshness)) {
	case "stale", "conflict", "conflicted", "unreliable", "error":
		return true
	default:
		return false
	}
}

func scheduleFreshnessLabel(freshness string) string {
	switch strings.ToLower(strings.TrimSpace(freshness)) {
	case "fresh":
		return "数据刚刚更新"
	case "cached":
		return "缓存数据"
	default:
		return ""
	}
}

func scheduleStatusLabel(status string) string {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "", "NS", "TBD", "SCHEDULED":
		return ""
	case "FT", "AET", "PEN", "FINISHED":
		return "已结束"
	case "CANC", "PST", "CANCELED", "POSTPONED":
		return "已调整"
	default:
		return "进行中"
	}
}

func activeMatchScheduleReply(snapshot matchstate.Snapshot) (string, bool) {
	integrity := strings.ToLower(strings.TrimSpace(snapshot.Integrity.Status))
	if integrity != "" && integrity != "ok" {
		return "", false
	}
	homeTeam := strings.TrimSpace(snapshot.HomeTeam)
	awayTeam := strings.TrimSpace(snapshot.AwayTeam)
	if homeTeam == "" || awayTeam == "" {
		return "", false
	}
	period := strings.TrimSpace(snapshot.Period)
	if period == "" {
		period = strings.TrimSpace(snapshot.MatchClock.Period)
	}
	switch strings.ToLower(period) {
	case "", "pre_match", "fulltime", "full_time", "finished":
		return "", false
	}
	matchLabel := homeTeam + "对" + awayTeam
	if competition := strings.TrimSpace(snapshot.Competition); competition != "" {
		matchLabel += "的" + competition
	}
	return fmt.Sprintf("现在正在看%s，%s %d-%d %s，时间在%s %s。",
		matchLabel,
		homeTeam,
		snapshot.Score.Home,
		snapshot.Score.Away,
		awayTeam,
		displayPeriod(period),
		snapshot.Clock,
	), true
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func isMatchStatusQuestion(text string) bool {
	return containsAny(text,
		"几比几", "比分", "现在多少", "现在几",
		"比赛什么情况", "比赛现在什么情况", "比赛怎么样", "赛况", "比赛时间",
		"踢到第几分钟", "进行到第几分钟", "第几分钟了", "多少分钟了",
	)
}

func isRecentGoalScorerQuestion(text string) bool {
	normalized := strings.ToLower(strings.TrimSpace(text))
	hasScorerQuestion := containsAny(normalized,
		"谁", "哪个球员", "哪位球员", "哪一个球员", "进球者",
	)
	hasGoalAction := containsAny(normalized,
		"进了", "进啦", "进咯", "进喽", "进的", "进球", "破门", "打进", "得分",
	)
	return hasScorerQuestion && hasGoalAction
}

func isPersonalShare(text string) bool {
	normalized := strings.ToLower(strings.TrimSpace(text))
	if containsAny(normalized,
		"吗", "么", "是不是", "为什么", "怎么", "谁", "什么", "哪", "多少", "几", "?", "？",
	) {
		return false
	}
	return isFirstPersonGoalAchievement(normalized) || containsAny(normalized,
		"我刚", "我今天", "我昨天", "我也", "我赢", "我输", "我累", "我困", "我开心", "我高兴",
		"我喜欢", "我支持", "我更看好", "我感觉", "我觉得", "我状态", "我们刚", "我们今天", "我们赢", "我们输",
	) || isAffectShare(normalized)
}

func isAffectShare(text string) bool {
	if containsAny(text, "你", "球球", "比赛", "球员", "球队", "场上") {
		return false
	}
	return containsAny(text, "我", "今天", "最近", "这会儿", "现在", "刚刚") && containsAny(text,
		"累", "困", "难受", "烦", "不舒服", "开心", "高兴", "爽", "兴奋",
	)
}

func isFirstPersonGoalAchievement(text string) bool {
	normalized := strings.ToLower(strings.TrimSpace(text))
	return containsAny(normalized,
		"我打进", "我踢进", "我进球", "我刚进", "我也进", "我们打进", "我们踢进", "我们进球",
	)
}

var scoreClaimPattern = regexp.MustCompile(`(\d{1,2})\s*(?:比|:|：|-)\s*(\d{1,2})`)

func isScoreClaim(text string) bool {
	if scoreClaimPattern.FindStringSubmatch(text) == nil {
		return false
	}
	if isNonLiteralMatchTalk(text) {
		return false
	}
	return !containsAny(text, "吗", "么", "是不是", "多少", "几比几", "?", "？")
}

func isEventClaim(text string) bool {
	if isFirstPersonGoalAchievement(text) {
		return false
	}
	if !containsAny(text, "进球了", "破门了", "得分了", "进啦", "进咯", "进喽", "球进了") {
		return false
	}
	if isNonLiteralMatchTalk(text) {
		return false
	}
	return !containsAny(text, "谁", "吗", "么", "是不是", "有没有", "?", "？")
}

func isNonLiteralMatchTalk(text string) bool {
	return containsAny(text, "要是", "如果", "假如", "假设", "希望", "但愿", "梦里", "梦到", "做梦", "开玩笑", "逗你的", "说着玩", "比如说")
}

func containsMatchFactLanguage(text string) bool {
	return scoreClaimPattern.FindStringSubmatch(text) != nil || containsAny(text,
		"比分", "进球", "进啦", "进咯", "进喽", "球进了", "破门", "得分", "领先", "扳平", "反超", "红牌", "黄牌", "VAR", "var",
		"分钟", "换人", "换下", "换上", "上场", "下场", "首发", "替补", "点球", "判罚", "越位", "半场", "终场", "开场",
		"梅开二度", "帽子戏法", "伤退", "受伤", "停赛", "绝杀", "绝平", "助攻", "扑救", "扑出", "射门", "射正", "犯规",
	)
}

func assessMatchClaim(text string, snapshot matchstate.Snapshot, events []matchstate.MatchEvent) (FactClaim, []string, bool) {
	if isScoreClaim(text) {
		claim, ok := assessScoreClaim(text, snapshot)
		if issue := snapshotIntegrityIssue(snapshot); ok && issue != "" {
			claim.Status = ClaimStatusUnverified
			claim.Reason = issue
		}
		return claim, nil, ok
	}
	if !isEventClaim(text) {
		return FactClaim{}, nil, false
	}
	claimedPlayer := inferClaimedPlayer(text, snapshot)
	claimedTeam := inferClaimedTeam(text, snapshot)
	claim := FactClaim{
		Kind:          "event",
		EventType:     "goal",
		Certainty:     claimCertainty(text),
		ClaimedPlayer: claimedPlayer,
		ClaimedTeam:   claimedTeam,
		Status:        ClaimStatusUnverified,
	}
	if issue := snapshotIntegrityIssue(snapshot); issue != "" {
		claim.Reason = issue
		return claim, nil, true
	}
	for _, event := range events {
		if event.EventType != "goal" {
			continue
		}
		actualPlayer := strings.TrimSpace(event.PlayerName)
		if scorers := participantNames(event.Participants, "scorer"); len(scorers) > 0 {
			actualPlayer = scorers[0]
		}
		claim.ActualPlayer = actualPlayer
		claim.ActualTeam = strings.TrimSpace(event.TeamName)
		if claim.ActualTeam == "" {
			switch event.TeamID {
			case "home":
				claim.ActualTeam = snapshot.HomeTeam
			case "away":
				claim.ActualTeam = snapshot.AwayTeam
			}
		}
		if claimedPlayer != "" && strings.EqualFold(claimedPlayer, actualPlayer) {
			claim.Status = ClaimStatusConfirmed
		} else if claimedPlayer != "" && actualPlayer != "" {
			claim.Status = ClaimStatusContradicted
		} else if claimedTeam != "" && strings.EqualFold(claimedTeam, claim.ActualTeam) {
			claim.Status = ClaimStatusConfirmed
		} else if claimedTeam != "" && claim.ActualTeam != "" {
			claim.Status = ClaimStatusContradicted
		}
		return claim, []string{event.ID}, true
	}
	return claim, nil, true
}

func inferClaimedTeam(text string, snapshot matchstate.Snapshot) string {
	if snapshot.HomeTeam != "" && strings.Contains(text, snapshot.HomeTeam) {
		return snapshot.HomeTeam
	}
	if snapshot.AwayTeam != "" && strings.Contains(text, snapshot.AwayTeam) {
		return snapshot.AwayTeam
	}
	return ""
}

func inferClaimedPlayer(text string, snapshot matchstate.Snapshot) string {
	if known := inferPlayer(text); known != "" {
		return known
	}
	prefix := text
	for _, marker := range []string{"进球了", "破门了", "得分了", "进啦", "进咯", "进喽", "球进了"} {
		if index := strings.Index(prefix, marker); index >= 0 {
			prefix = prefix[:index]
			break
		}
	}
	prefix = strings.ReplaceAll(prefix, snapshot.HomeTeam, "")
	prefix = strings.ReplaceAll(prefix, snapshot.AwayTeam, "")
	prefix = strings.Trim(prefix, " \t\r\n，。！？!?：:、的")
	for _, lead := range []string{"刚刚", "刚才", "好像", "似乎", "可能", "应该", "大概", "听说", "我看", "我觉得", "这球", "那个球"} {
		prefix = strings.TrimPrefix(prefix, lead)
		prefix = strings.Trim(prefix, " \t\r\n，。！？!?：:、的")
	}
	if fields := strings.Fields(prefix); len(fields) > 0 {
		prefix = fields[len(fields)-1]
	}
	if prefix == "" || containsAny(prefix, "主队", "客队", "他们", "他", "她", "有人") {
		return ""
	}
	runes := []rune(prefix)
	if len(runes) > 24 {
		return ""
	}
	return prefix
}

func snapshotIntegrityIssue(snapshot matchstate.Snapshot) string {
	if snapshot.Integrity.Status == "conflict" {
		if snapshot.Integrity.Reason != "" {
			return snapshot.Integrity.Reason
		}
		return "match sources conflict"
	}
	if snapshot.Score.Home < 0 || snapshot.Score.Away < 0 {
		return "match score is invalid"
	}
	if len(snapshot.RecentEvents) == 0 {
		return ""
	}
	if snapshot.RecentEvents[0].Score != snapshot.Score {
		return "latest event score does not match snapshot"
	}
	events := snapshot.RecentEvents
	oldest := events[len(events)-1]
	current := oldest.Score
	if oldest.EventType == "goal" {
		if oldest.Period == "pre_match" {
			return "goal occurred before kickoff"
		}
		switch oldest.TeamID {
		case "home":
			current.Home--
		case "away":
			current.Away--
		default:
			return "goal has no valid team"
		}
		if current.Home < 0 || current.Away < 0 {
			return "goal did not increase score"
		}
	}
	for index := len(events) - 1; index >= 0; index-- {
		event := events[index]
		expected := current
		switch event.EventType {
		case "goal":
			if event.Period == "pre_match" {
				return "goal occurred before kickoff"
			}
			switch event.TeamID {
			case "home":
				expected.Home++
			case "away":
				expected.Away++
			default:
				return "goal has no valid team"
			}
			if event.Score != expected {
				return "goal score transition is inconsistent"
			}
		case "var_check":
			if scoreDistance(current, event.Score) > 1 {
				return "VAR score transition is inconsistent"
			}
		default:
			if event.Score != expected {
				return "non-goal event changed score"
			}
		}
		current = event.Score
	}
	return ""
}

func scoreDistance(left, right matchstate.Score) int {
	home := left.Home - right.Home
	if home < 0 {
		home = -home
	}
	away := left.Away - right.Away
	if away < 0 {
		away = -away
	}
	return home + away
}

func assessScoreClaim(text string, snapshot matchstate.Snapshot) (FactClaim, bool) {
	parts := scoreClaimPattern.FindStringSubmatch(text)
	if len(parts) != 3 {
		return FactClaim{}, false
	}
	left, leftErr := strconv.Atoi(parts[1])
	right, rightErr := strconv.Atoi(parts[2])
	if leftErr != nil || rightErr != nil {
		return FactClaim{}, false
	}
	claimed := matchstate.Score{Home: left, Away: right}
	mentionsAway := snapshot.AwayTeam != "" && strings.Contains(text, snapshot.AwayTeam)
	mentionsHome := snapshot.HomeTeam != "" && strings.Contains(text, snapshot.HomeTeam)
	scoreRange := scoreClaimPattern.FindStringIndex(text)
	awayBeforeScore := len(scoreRange) == 2 && snapshot.AwayTeam != "" && strings.Contains(text[:scoreRange[0]], snapshot.AwayTeam)
	homeAfterScore := len(scoreRange) == 2 && snapshot.HomeTeam != "" && strings.Contains(text[scoreRange[1]:], snapshot.HomeTeam)
	if (awayBeforeScore && homeAfterScore) || (mentionsAway && !mentionsHome) {
		claimed = matchstate.Score{Home: right, Away: left}
	}
	claim := FactClaim{
		Kind:         "score",
		Certainty:    claimCertainty(text),
		ClaimedScore: &claimed,
		ActualScore:  &snapshot.Score,
		Status:       ClaimStatusContradicted,
	}
	if claimed == snapshot.Score {
		claim.Status = ClaimStatusConfirmed
	}
	return claim, true
}

func claimCertainty(text string) string {
	if containsAny(text, "好像", "似乎", "可能", "应该", "大概", "听说", "吧") {
		return "uncertain"
	}
	return "asserted"
}

func answerRecentEvent(text string, events []matchstate.MatchEvent) (string, []string) {
	if isRecentGoalScorerQuestion(text) {
		for _, ev := range events {
			if ev.EventType != "goal" {
				continue
			}
			scorers := participantNames(ev.Participants, "scorer")
			if len(scorers) == 0 && strings.TrimSpace(ev.PlayerName) != "" {
				scorers = []string{strings.TrimSpace(ev.PlayerName)}
			}
			if len(scorers) == 0 {
				return fmt.Sprintf("刚才%s有进球，但进球球员还没有确认。", ev.Clock), []string{ev.ID}
			}
			return fmt.Sprintf("刚才%s这球是%s打进的。", ev.Clock, strings.Join(scorers, "、")), []string{ev.ID}
		}
		return "我这边目前还没有收到进球记录。", nil
	}
	if containsAny(text, "助攻", "谁主攻") {
		for _, ev := range events {
			if ev.EventType != "goal" {
				continue
			}
			assist := participantNames(ev.Participants, "assist")
			preAssist := participantNames(ev.Participants, "pre_assist")
			var parts []string
			if len(assist) > 0 {
				parts = append(parts, strings.Join(assist, "、")+"助攻")
			}
			if len(preAssist) > 0 {
				parts = append(parts, strings.Join(preAssist, "、")+"参与策动")
			}
			if len(parts) == 0 {
				return "我这边只看到刚才有进球记录，但没有看到明确助攻人。", []string{ev.ID}
			}
			return "刚才这球是" + strings.Join(parts, "，") + "。", []string{ev.ID}
		}
		return "我这边目前还没看到进球助攻记录。", nil
	}
	if len(events) == 0 {
		return "我这边目前还没有收到新的比赛动态。", nil
	}
	ev := events[0]
	return fmt.Sprintf("刚才是%s %s：%s", ev.Clock, eventLabel(ev.EventType), ev.Description), []string{ev.ID}
}

func answerFollowUp(text string, turns []ConversationTurn, events []matchstate.MatchEvent) (string, []string) {
	event := followUpEvent(turns, events)
	if event == nil {
		return "这个追问我需要基于前面那条事件来答，但我这边暂时没有可用的上下文记录。", nil
	}
	if containsAny(text, "策动", "谁参与") {
		preAssist := participantNames(event.Participants, "pre_assist")
		if len(preAssist) == 0 {
			return "这球我只看到进球或助攻记录，暂时没有明确策动者。", []string{event.ID}
		}
		return "这球策动的是" + strings.Join(preAssist, "、") + "。", []string{event.ID}
	}
	if containsAny(text, "传", "传的") {
		assist := participantNames(event.Participants, "assist")
		if len(assist) == 0 {
			return "这球我这边暂时没有明确传球助攻记录。", []string{event.ID}
		}
		return "最后一传是" + strings.Join(assist, "、") + "。", []string{event.ID}
	}
	return answerRecentEvent(text, []matchstate.MatchEvent{*event})
}

func followUpEvent(turns []ConversationTurn, events []matchstate.MatchEvent) *matchstate.MatchEvent {
	referenced := lastReferencedEventID(turns)
	if referenced != "" {
		for i := range events {
			if events[i].ID == referenced {
				return &events[i]
			}
		}
	}
	for i := range events {
		if events[i].EventType == "goal" {
			return &events[i]
		}
	}
	if len(events) == 0 {
		return nil
	}
	return &events[0]
}

func lastReferencedEventID(turns []ConversationTurn) string {
	for i := len(turns) - 1; i >= 0; i-- {
		if strings.TrimSpace(turns[i].EventID) != "" {
			return turns[i].EventID
		}
	}
	return ""
}

func answerPlayerQuestion(text, player string, events []matchstate.MatchEvent) (string, []string) {
	if player == "" {
		return "你说的是哪位球员？我帮你翻一下刚才的比赛记录。", nil
	}
	var ids []string
	for _, ev := range events {
		ids = append(ids, ev.ID)
		if ev.EventType == "goal" && containsAny(text, "进球") {
			return fmt.Sprintf("有，我这边看到%s在%s有进球记录：%s。", player, ev.Clock, ev.Description), ids
		}
	}
	if containsAny(text, "进球") {
		return fmt.Sprintf("我这边目前没有看到%s的进球记录。", player), ids
	}
	if len(events) == 0 {
		return fmt.Sprintf("我这边目前还没有%s的实时事件记录。", player), nil
	}
	return fmt.Sprintf("我这边看到%s最近参与了%d条事件，最新一条是：%s。", player, len(events), events[0].Description), ids
}

func (a *Agent) realizeReply(ctx context.Context, req AgentBoundaryRequest, intent Intent, reliable string, anchors []string, decision relationship.Decision, trace *Trace) string {
	if a.realizer == nil {
		return reliable
	}
	realizeCtx, cancel := context.WithTimeout(ctx, a.realizeTimeout)
	defer cancel()
	factMode := relationship.FactModeNone
	if trace != nil && trace.Claim != nil {
		if trace.Claim.Status == ClaimStatusUnverified {
			factMode = relationship.FactModeUnverified
		} else {
			factMode = relationship.FactModeDeterministic
		}
	} else if len(anchors) > 0 {
		factMode = relationship.FactModeAnchored
	}
	grounding := relationship.GroundedContent{
		Intent:          string(intent),
		ReliableText:    reliable,
		RequiredAnchors: append([]string(nil), anchors...),
		FactMode:        factMode,
	}
	realized, err := a.realizer.Realize(realizeCtx, RealizationRequest{
		UserInput:    req.Text,
		Intent:       intent,
		Grounding:    grounding,
		Decision:     decision,
		ReliableText: reliable,
	})
	if err != nil || strings.TrimSpace(realized.Text) == "" {
		if err != nil {
			trace.Error = strings.TrimSpace(err.Error())
		}
		trace.Reason = "realize_fallback_error"
		trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "response.emit_companion_reply", Args: map[string]string{"mode": "deterministic", "fallback": "realizer_error"}})
		return reliable
	}
	allowedSource := strings.Join(compactAnchors(req.Text, reliable, strings.Join(anchors, " ")), " ")
	if err := validateRealizedText(realized.Text, allowedSource, decision); err != nil {
		trace.Reason = "realize_fallback_policy"
		trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "response.emit_companion_reply", Args: map[string]string{"mode": "deterministic", "fallback": "policy"}})
		return reliable
	}
	if err := validateRealizedConversationTurn(req.Text, intent, realized.Text); err != nil {
		trace.Reason = "realize_fallback_policy"
		trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "response.emit_companion_reply", Args: map[string]string{"mode": "deterministic", "fallback": "policy"}})
		return reliable
	}
	trace.Reason = "relationship_plan_realized"
	trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "response.emit_companion_reply", Args: map[string]string{"mode": "realized"}})
	return strings.TrimSpace(realized.Text)
}

func validateRealizedConversationTurn(input string, intent Intent, text string) error {
	if isPresenceOnlyReply(text) && !isPresenceCheck(input) {
		return fmt.Errorf("realized natural turn became a presence acknowledgement")
	}
	if intent != IntentEmotionReaction || !isDisbeliefReaction(input) {
		return nil
	}
	if !containsAny(text, "真的假的", "真的吗", "认真的吗", "不会吧", "不是吧", "开玩笑吧", "刚刚", "那一下", "这一下", "这球") {
		return fmt.Errorf("realized disbelief reaction dropped its conversational context")
	}
	return nil
}

func isPresenceOnlyReply(text string) bool {
	normalized := strings.NewReplacer("，", "", "。", "", "！", "", "？", "", "!", "", "?", "", " ", "").Replace(strings.TrimSpace(text))
	return len([]rune(normalized)) <= 8 && containsAny(normalized, "我在", "在呢", "在呀", "在的", "看着呢")
}

func isPresenceCheck(input string) bool {
	normalized := strings.NewReplacer("，", "", "。", "", "！", "", "？", "", "!", "", "?", "", " ", "").Replace(strings.ToLower(strings.TrimSpace(input)))
	if normalized == "喂" || normalized == "球球" || normalized == "人呢" {
		return true
	}
	return containsAny(normalized, "在吗", "在不在", "还在吗") || isAudioConnectionCheck(normalized)
}

func isAudioConnectionCheck(input string) bool {
	normalized := normalizeConversationText(input)
	if !containsAny(normalized, "听到", "听见", "听得见", "听得到") {
		return false
	}
	return containsAny(normalized, "我说话", "我讲话", "到我", "见我", "到吗", "见吗", "得到吗", "得见吗")
}

func isGreeting(input string) bool {
	normalized := normalizeConversationText(input)
	if !containsAny(normalized,
		"你好", "您好", "嗨", "哈喽", "hello", "hi", "早安", "早上好", "上午好", "中午好", "下午好", "晚上好", "晚安",
	) {
		return false
	}
	return !containsAny(normalized,
		"分析", "解释", "为什么", "怎么", "帮我", "请问", "请你", "能不能", "可以吗", "比赛怎么样", "比分", "进球", "分钟",
	)
}

func greetingReply(input string) string {
	normalized := normalizeConversationText(input)
	switch {
	case containsAny(normalized, "下午好"):
		return "下午好，来了。"
	case containsAny(normalized, "早安", "早上好"):
		return "早上好，来了。"
	case containsAny(normalized, "上午好"):
		return "上午好，来了。"
	case containsAny(normalized, "中午好"):
		return "中午好，来了。"
	case containsAny(normalized, "晚上好"):
		return "晚上好，来了。"
	case containsAny(normalized, "晚安"):
		return "晚安，先休息。"
	default:
		return "嗨，来了。先看球。"
	}
}

func normalizeConversationText(input string) string {
	return strings.NewReplacer(
		"，", "", "。", "", "！", "", "？", "", "!", "", "?", "", " ", "", "\t", "", "\n", "",
	).Replace(strings.ToLower(strings.TrimSpace(input)))
}

func isCompanionStateCompliment(input string) bool {
	normalized := strings.ToLower(strings.TrimSpace(input))
	return containsAny(normalized, "你", "球球") && containsAny(normalized, "精神不错", "状态不错", "看起来不错", "气色不错")
}

func isCompanionDirectedSmalltalk(input string) bool {
	normalized := strings.ToLower(strings.TrimSpace(input))
	if !containsAny(normalized, "你", "球球") {
		return false
	}
	return isCompanionStateCompliment(normalized) || containsAny(normalized,
		"有点意思", "挺有意思", "真有意思", "说你呢", "我是说你", "我说的是你", "我觉得你",
		"你很", "你真", "你挺", "你有点", "你也是", "你才", "夸你",
	)
}

func validateRealizedText(text, allowedSource string, decision relationship.Decision) error {
	text = strings.TrimSpace(text)
	policy := decision.Speech.Content
	if policy.MaxCharacters > 0 && len([]rune(text)) > policy.MaxCharacters {
		return fmt.Errorf("realized reply exceeds character limit")
	}
	if policy.MaxSentences > 0 && realizedSentenceCount(text) > policy.MaxSentences {
		return fmt.Errorf("realized reply exceeds sentence limit")
	}
	if !policy.QuestionAllowed && strings.ContainsAny(text, "?？") {
		return fmt.Errorf("realized reply contains an unplanned question")
	}
	if policy.QuestionAllowed && strings.Count(text, "?")+strings.Count(text, "？") > 1 {
		return fmt.Errorf("realized reply contains too many questions")
	}
	if !replyPreservesAnchors(text, policy.RequiredAnchors) {
		return fmt.Errorf("realized reply dropped required anchors")
	}
	if forbidsNewMatchClaims(policy.ForbiddenClaims) && containsMatchFactLanguage(text) {
		return fmt.Errorf("realized reply introduced a forbidden match claim")
	}
	if forbidsClaim(policy.ForbiddenClaims, "new_player") && containsNewKnownPlayer(text, allowedSource) {
		return fmt.Errorf("realized reply introduced a new player")
	}
	for _, topic := range policy.ForbiddenTopics {
		if topic != "" && strings.Contains(text, topic) {
			return fmt.Errorf("realized reply crossed a user topic boundary")
		}
	}
	if policy.BanterScope == "" && containsAny(text, "毒奶", "逗你", "开你玩笑", "又被我说中", "就你这") {
		return fmt.Errorf("realized reply used banter without permission")
	}
	for _, addressing := range []string{"老伙计", "老球友", "兄弟", "哥们", "搭子"} {
		if strings.Contains(text, addressing) && policy.Addressing != addressing {
			return fmt.Errorf("realized reply used unpermitted addressing")
		}
	}
	if containsAny(text, "老公", "老婆", "主人", "奴才") {
		return fmt.Errorf("realized reply used forbidden relationship addressing")
	}
	if containsPersonalInsultLanguage(text) {
		return fmt.Errorf("realized reply contains a personal insult")
	}
	if containsAny(text, "我操", "我艹", "妈的", "他妈的") {
		return fmt.Errorf("realized reply contains directed or excessive profanity")
	}
	if strings.Contains(text, "卧槽") && policy.ProfanityLevel != "strong_non_directed" {
		return fmt.Errorf("realized reply exceeds permitted profanity level")
	}
	if (containsDelimitedPhrase(text, "我去") || containsDelimitedPhrase(text, "靠") || strings.Contains(text, "真离谱")) &&
		policy.ProfanityLevel != "mild_non_directed" && policy.ProfanityLevel != "strong_non_directed" {
		return fmt.Errorf("realized reply exceeds permitted profanity level")
	}
	currentHash := phraseHash(text)
	for _, recentHash := range policy.RecentPhraseHashes {
		if currentHash == recentHash {
			return fmt.Errorf("realized reply repeated a recent phrase")
		}
	}
	for _, forbidden := range []string{
		"无论如何我都会陪着你",
		"永远陪着你",
		"一直等你",
		"你是我唯一",
		"只有我懂你",
		"不要离开我",
		"终于来了",
		"怎么才来",
		"离不开你",
		"属于我",
		"我最懂你",
		"宝贝",
		"亲爱的",
		"老公",
		"老婆",
		"主人",
		"我能理解你的感受",
		"如果你愿意的话",
		"需要我帮你",
		"你的感受很重要",
		"根据已确认的比赛信息",
		"作为一个AI",
		"作为 AI",
		"导播台",
		"后台",
		"Trace",
		"历史记录",
		"内部记录",
		"关系记忆",
		"记忆库",
	} {
		if strings.Contains(text, forbidden) {
			return fmt.Errorf("realized reply contains forbidden language")
		}
	}
	return nil
}

func (a *Agent) recentPhraseHashes(ctx context.Context, matchID, userID string, enabled bool, trace *Trace) []uint64 {
	if !enabled || a == nil || a.tools == nil {
		return nil
	}
	turns, err := a.tools.RecentTurns(ctx, matchID, userID, 12)
	if err != nil {
		return nil
	}
	trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "conversation.read_recent", Args: map[string]string{"matchId": matchID, "userId": userID, "limit": "12", "purpose": "phrase_cooldown"}})
	hashes := make([]uint64, 0, len(turns))
	for _, turn := range turns {
		if turn.Role == "qiuqiu" && strings.TrimSpace(turn.Text) != "" {
			hashes = append(hashes, phraseHash(turn.Text))
		}
	}
	return hashes
}

func phraseHash(text string) uint64 {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(text)), ""))))
	return hash.Sum64()
}

func realizedSentenceCount(text string) int {
	count := 0
	for _, character := range text {
		switch character {
		case '。', '！', '？', '!', '?':
			count++
		}
	}
	if count == 0 && strings.TrimSpace(text) != "" {
		return 1
	}
	return count
}

func hasCommunicationAct(actions []relationship.CommunicationAct, wanted relationship.CommunicationAct) bool {
	for _, action := range actions {
		if action == wanted {
			return true
		}
	}
	return false
}

func replyPreservesAnchors(reply string, anchors []string) bool {
	reply = strings.TrimSpace(reply)
	if reply == "" {
		return false
	}
	for _, anchor := range anchors {
		if strings.TrimSpace(anchor) == "" {
			continue
		}
		if !strings.Contains(reply, anchor) {
			return false
		}
	}
	return true
}

func compactAnchors(values ...string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func anchorsForEvents(events []matchstate.MatchEvent, ids []string, deterministicReply string) []string {
	if len(events) == 0 {
		return nil
	}
	var anchors []string
	for _, ev := range events {
		if len(ids) > 0 && !containsString(ids, ev.ID) {
			continue
		}
		if strings.Contains(deterministicReply, ev.Description) {
			anchors = append(anchors, ev.Description)
		}
		if strings.Contains(deterministicReply, ev.PlayerName) {
			anchors = append(anchors, ev.PlayerName)
		}
		for _, p := range ev.Participants {
			if strings.Contains(deterministicReply, p.Name) {
				anchors = append(anchors, p.Name)
			}
		}
	}
	return compactAnchors(anchors...)
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func fallbackProactive(ev matchstate.MatchEvent, snapshot matchstate.Snapshot) string {
	switch ev.EventType {
	case "goal":
		if ev.PlayerName != "" {
			return fmt.Sprintf("%s进了！现在%s %d-%d %s。", ev.PlayerName, snapshot.HomeTeam, snapshot.Score.Home, snapshot.Score.Away, snapshot.AwayTeam)
		}
		return fmt.Sprintf("进球了！现在%s %d-%d %s。", snapshot.HomeTeam, snapshot.Score.Home, snapshot.Score.Away, snapshot.AwayTeam)
	case "penalty", "var_check", "big_chance":
		return "这一下很关键，我们先看裁判和双方球员怎么反应。"
	default:
		if ev.Description != "" {
			return ev.Description
		}
		return "场上有新变化，我先陪你盯着。"
	}
}

func participantNames(participants []matchstate.Participant, role string) []string {
	var names []string
	for _, p := range participants {
		if p.Role == role && p.Name != "" {
			names = append(names, p.Name)
		}
	}
	return names
}

var knownPlayerNames = []string{"佩德里", "法比安", "亚马尔", "穆西亚拉", "莫拉塔", "哈弗茨", "菲尔克鲁格", "萨拉赫", "努涅斯", "Pedri", "Musiala", "Salah", "Nunez", "Núñez"}

func inferPlayer(text string) string {
	for _, name := range knownPlayerNames {
		if strings.Contains(text, name) {
			return name
		}
	}
	return ""
}

func eventLabel(eventType string) string {
	switch eventType {
	case "goal":
		return "进球"
	case "shot":
		return "射门"
	case "save":
		return "扑救"
	case "penalty":
		return "点球"
	case "var_check":
		return "VAR"
	default:
		return eventType
	}
}

func displayPeriod(period string) string {
	period = strings.TrimSpace(period)
	switch strings.ToLower(period) {
	case "pre_match":
		return "赛前"
	case "first_half":
		return "上半场"
	case "second_half":
		return "下半场"
	case "halftime", "half_time":
		return "中场"
	case "fulltime", "full_time", "finished":
		return "完场"
	default:
		if period == "" || strings.Contains(period, "_") {
			return "比赛进行中"
		}
		return period
	}
}

func containsAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func hasAnyPrefix(text string, prefixes ...string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(text, prefix) {
			return true
		}
	}
	return false
}

func forbidsNewMatchClaims(claims []string) bool {
	for _, claim := range claims {
		if strings.HasPrefix(claim, "new_") {
			return true
		}
	}
	return false
}

func forbidsClaim(claims []string, wanted string) bool {
	for _, claim := range claims {
		if claim == wanted {
			return true
		}
	}
	return false
}

func containsNewKnownPlayer(text, allowedSource string) bool {
	for _, playerName := range knownPlayerNames {
		if strings.Contains(text, playerName) && !strings.Contains(allowedSource, playerName) {
			return true
		}
	}
	return false
}

func containsPersonalInsultLanguage(text string) bool {
	if containsAny(text, "废物", "傻逼", "蠢货") {
		return true
	}
	return containsDelimitedPhrase(text, "垃圾") || containsAny(text, "是个垃圾", "就是垃圾", "这个垃圾")
}

func containsDelimitedPhrase(text, phrase string) bool {
	textRunes := []rune(text)
	phraseRunes := []rune(phrase)
	if len(phraseRunes) == 0 || len(phraseRunes) > len(textRunes) {
		return false
	}
	for start := 0; start <= len(textRunes)-len(phraseRunes); start++ {
		matched := true
		for offset := range phraseRunes {
			if textRunes[start+offset] != phraseRunes[offset] {
				matched = false
				break
			}
		}
		if !matched {
			continue
		}
		end := start + len(phraseRunes)
		beforeBoundary := start == 0 || isPhraseBoundary(textRunes[start-1])
		afterBoundary := end == len(textRunes) || isPhraseBoundary(textRunes[end])
		if beforeBoundary && afterBoundary {
			return true
		}
	}
	return false
}

func isPhraseBoundary(character rune) bool {
	return unicode.IsSpace(character) || strings.ContainsRune("，。！？、；：,.!?;:（）()“”‘’\"'…—-", character)
}

func traceID(now time.Time) string {
	if now.IsZero() {
		now = time.Now()
	}
	return fmt.Sprintf("trace_%d_%d", now.UnixNano(), traceSequence.Add(1))
}

func stableTraceID(userID, matchID, signalID string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(userID) + "\x00" + strings.TrimSpace(matchID) + "\x00" + strings.TrimSpace(signalID)))
	return "trace_" + hex.EncodeToString(digest[:16])
}

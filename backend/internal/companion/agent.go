package companion

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"
	"time"
	"unicode"

	"qiuqiu/internal/interaction"
	"qiuqiu/internal/knowledge"
	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/memory"
	"qiuqiu/internal/observation"
	"qiuqiu/internal/proactive"
	"qiuqiu/internal/relationship"
)

type Agent struct {
	tools                      MemoryTools
	realizer                   ReplyRealizer
	scheduleReader             ScheduleReader
	director                   relationship.CompanionDirector
	observations               observation.Coordinator
	observationReconcileWindow func(string, string) time.Duration
	realizeTimeout             time.Duration
	router                     TurnRouter
	interactions               interaction.Ledger
	memories                   memory.Memories
	reminders                  proactive.Store
	characterSettings          *relationship.CharacterSettings
	subscriptions              proactive.SubscriptionStore
	knowledge                  *knowledge.Library
	triggerStates              *knowledgeTriggerStates
}

func NewAgent(tools MemoryTools) *Agent {
	return &Agent{tools: tools, realizeTimeout: 800 * time.Millisecond, interactions: interaction.NewMemoryLedger(), triggerStates: newKnowledgeTriggerStates()}
}

// WithMemories attaches the ADR-0006 memory seam (async observations, recall,
// portrait). Nil keeps the agent Ledger-only.
func (a *Agent) WithMemories(memories memory.Memories) *Agent {
	if memories != nil {
		a.memories = memories
	}
	return a
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

// WithRouter attaches the ADR-0009 LLM intent router. Nil or a client whose
// key is unset keeps the legacy keyword-miss behavior verbatim.
func (a *Agent) WithRouter(routerClient TurnRouter) *Agent {
	if routerClient != nil && routerClient.Enabled() {
		a.router = routerClient
	}
	return a
}

// WithReminders attaches the proactive reminder book (ADR-0015): the
// reminder_request intent schedules user-requested pre-match nudges into it.
// Nil keeps the intent replying that the book is not wired yet.
func (a *Agent) WithReminders(store proactive.Store) *Agent {
	if store != nil {
		a.reminders = store
	}
	return a
}

// WithCharacterSettings attaches the character-settings store
// (polish-round-3): cue-driven preference changes are mirrored into it so
// the three entrances share one state. Nil keeps cue words on the legacy
// relationship-state path only.
func (a *Agent) WithCharacterSettings(store *relationship.CharacterSettings) *Agent {
	if store != nil {
		a.characterSettings = store
	}
	return a
}

// WithKnowledge attaches the curated knowledge library (ADR-0017): the
// knowledge_question intent answers from it verbatim. Nil keeps the intent
// replying that the library is not wired yet.
func (a *Agent) WithKnowledge(library *knowledge.Library) *Agent {
	if library != nil {
		a.knowledge = library
	}
	return a
}

// WithSubscriptions attaches the subscription book (season-subscription):
// the subscription_manage intent reads and mutates it. Nil degrades honestly.
func (a *Agent) WithSubscriptions(store proactive.SubscriptionStore) *Agent {
	if store != nil {
		a.subscriptions = store
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
		// Trigger: observation resolved to confirmed — the called shot landed,
		// so the body celebrates (client accepts 'cheer' as a legacy alias).
		return relationship.PresentationPlan{Expression: "excited", Motion: "celebrate", VoiceStyle: "excited", VoiceEnergy: 0.9, VoiceSpeed: 1.05, HoldMS: 1800, ReturnMode: "watching"}
	case observation.StatusContradicted:
		// Trigger: observation resolved to contradicted — the called shot was
		// wrong, so the body plays the near-miss gesture.
		return relationship.PresentationPlan{Expression: "deflated", Motion: "miss", VoiceStyle: "soft", VoiceEnergy: 0.45, VoiceSpeed: 0.95, HoldMS: 1600, ReturnMode: "watching"}
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
		a.observeTurnMemory(ctx, *input.Message, response)
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
		a.observeMatchEventMemory(ctx, *input.MatchEvent, response.Decision)
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
		Talkativeness:       req.Talkativeness,
		Settings:            req.Settings,
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

	handling, err := intentRegistry.handle(a, intent, &userTurn{
		ctx:            ctx,
		req:            req,
		requestTraceID: requestTraceID,
		trace:          &trace,
	})
	if err != nil {
		return Response{}, err
	}
	reply := handling.reply
	requiredAnchors := handling.requiredAnchors
	scheduleLookup := handling.scheduleLookup
	allowRealize := handling.allowRealize
	deterministicReason := handling.deterministicReason
	claimPersisted := handling.claimPersisted
	if intent != IntentPersonalShare && containsMatchFactLanguage(req.Text) && len(requiredAnchors) == 0 {
		allowRealize = false
		if deterministicReason == "policy" {
			deterministicReason = "fact_language_policy"
		}
	}

	// 记忆进措辞层：事实应答补充语——recall 材料非空时补一句记忆衔接，
	// 失败/拒绝即整句丢弃，事实本体措辞不变（design decision 4）。
	reply = a.appendFactMemoryCallback(ctx, req, intent, reply, requiredAnchors, &trace)

	recentPhraseHashes := a.recentPhraseHashes(ctx, req.MatchID, req.UserID, allowRealize, &trace)
	if err := a.tools.WriteTrace(ctx, trace); err != nil {
		return Response{}, err
	}
	decision := a.applyDecision(ctx, req, intent, reply, requiredAnchors, recentPhraseHashes, routedCasual, claimPersisted, &trace)
	// T2-cue 收敛（polish-round-3）：cue 驱动的偏好变化镜像进
	// user_character_settings，三入口共用一份状态；Ledger 记账（source=cue）。
	if a.characterSettings != nil && decision != nil {
		a.mirrorCharacterSettings(ctx, req.UserID, req.MatchID, *decision, &trace)
	}
	routerReplyUsed := false
	if allowRealize && decision != nil && decision.Speech == nil {
		reply = ""
		trace.Reason = ReasonRelationshipChosenSilence
		trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "response.emit_companion_reply", Args: map[string]string{"mode": "silence"}})
	} else if allowRealize && decision != nil && (shouldRealizeUserTurn(intent, *decision) || (intent == IntentUnknown && routerChatReply != "")) {
		reply = reliableFallbackForDecision(req.Text, intent, reply, *decision)
		// Design decision 4: when the router already suggested a natural
		// reply for a chat-class turn, it is realized directly through the
		// guard validation — no second LLM call. Otherwise the normal
		// realizer (C1 for the degraded-casual unknown) takes over.
		if validated, reject := guardValidateReply(req.Text, intent, routerChatReply, requiredAnchors, reply, *decision); validated != "" {
			reply = validated
			routerReplyUsed = true
			trace.Reason = ReasonRouterReplyRealized
			trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "response.emit_companion_reply", Args: map[string]string{"mode": "realized", "source": "router"}})
		} else {
			// ADR-0009 审计承诺补全：有建议但被拒/为空不再是静默的
			// ReplyUsed=false——拒绝原因落 trace.Router，随后照旧走
			// realizer / deterministic 兜底。
			if trace.Router != nil {
				trace.Router.RejectReason = reject
			}
			if a.realizer != nil && decision.Speech != nil {
				reply = a.realizeReply(ctx, req, intent, reply, requiredAnchors, *decision, &trace)
			} else {
				trace.Reason = ReasonRealizeFallbackUnavail
				trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "response.emit_companion_reply", Args: map[string]string{"mode": "deterministic", "fallback": "realizer_unavailable"}})
			}
		}
	} else {
		trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "response.emit_companion_reply", Args: map[string]string{"mode": "deterministic", "reason": deterministicReason}})
	}
	reply = a.appendThreadRecovery(ctx, req, intent, reply, &trace)
	// C2 writer hook: open-thread candidates ride on the same trace as the
	// turn (memory.append_thread) so every loop the ledger opens is auditable.
	a.observeTurnThreads(ctx, req.UserID, req.SignalID, req.Text, req.FactRefresh, intent, reply, req.Now, &trace)
	if decision != nil {
		decision.UsedMemoryIDs = relationshipMemoryIDsUsedByReply(reply, *decision)
		trace.RelationshipDecision = decision
	}
	trace.Output = reply
	trace.LatencyMS = int(time.Since(start).Milliseconds())
	if trace.Reason == "" {
		if !allowRealize && deterministicReason != "policy" {
			trace.Reason = ReasonDeterministicPrefix + deterministicReason
		} else {
			trace.Reason = ReasonDeterministicCompanion
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
	// ADR-0009 observability: the routed turn stamps the raw router verdict
	// as a reason code ("router:<intent>:<confidence>") and marks whether the
	// suggested reply survived validation.
	if routed != nil && trace.Router != nil {
		trace.Router.ReplyUsed = routerReplyUsed
		code := fmt.Sprintf("router:%s:%s", routed.Intent, strconv.FormatFloat(routed.Confidence, 'f', 2, 64))
		if decision != nil {
			decision.ReasonCodes = append(decision.ReasonCodes, code)
		}
	}
	// presentation-map.json delivery.interrupted: a turn we could not parse
	// rides a one-shot confused/listening reaction alongside the
	// deterministic clarification — the reply text itself is never replaced.
	// A routed turn that naturalized into a validated casual reply was parsed
	// and must not play the interrupted reaction.
	routerNaturalized := routerReplyUsed || (routedCasual && trace.Reason == ReasonRelationshipPlanRealized)
	if intent == IntentUnknown && !routerNaturalized {
		presentation = relationship.InterruptedDeliveryPresentation(presentation.Affect)
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
				trace.Reason = ReasonScheduleLookupCtxUpdated
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
				trace.Reason = ReasonScheduleLookupCtxUpdated
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
		trace.Reason = ReasonScheduleLookupUnavail
		reply = "赛程源这次没接上，我不先乱报。"
		searchArgs["state"] = "failed"
	} else {
		reply = formatScheduleSearchResult(result, intent.Scope)
		trace.Reason = ReasonScheduleLookupResult
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
		claim.Reason = ReasonNoRecentConfirmedEvent
		return "我这边还没看到你说的那一下，先不跟着瞎认。", claim, nil
	}
	claim.EventType = event.EventType
	claim.ActualPlayer = strings.TrimSpace(event.PlayerName)
	claim.ActualTeam = strings.TrimSpace(event.TeamName)
	claim.Status = ClaimStatusConfirmed
	claim.Reason = ReasonMatchedLatestEvent
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
	if hasCommunicationAct(decision.Actions, relationship.ActRecall) || hasCommunicationAct(decision.Actions, relationship.ActRepair) || hasCommunicationAct(decision.Actions, relationship.ActAsk) {
		return true
	}
	if intent == IntentUnknown {
		// intent-router 1.3: only a routed casual unknown (policy ActChat)
		// realizes; the legacy canned reply stays deterministic.
		return hasCommunicationAct(decision.Actions, relationship.ActChat)
	}
	return intent == IntentSmalltalk || intent == IntentEmotionReaction || intent == IntentPersonalShare
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

func (a *Agent) applyDecision(ctx context.Context, req AgentBoundaryRequest, intent Intent, reply string, requiredAnchors []string, recentPhraseHashes []uint64, casualChat bool, claimPersisted bool, trace *Trace) *relationship.Decision {
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
	var userCues []relationship.UserCue
	if claimPersisted {
		// intent-router C2: the persisted-claim cue keeps the policy away
		// from the fresh-unverified pushback — the hold stays warm.
		userCues = append(userCues, relationship.UserCue{Kind: relationship.CueClaimPersisted})
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
		User:         &relationship.UserSignal{Text: req.Text, Talkativeness: req.Talkativeness, Cues: userCues, Settings: req.Settings},
		Grounding: relationship.GroundedContent{
			Intent:             string(intent),
			ReliableText:       reply,
			RequiredAnchors:    append([]string(nil), requiredAnchors...),
			FactMode:           factMode,
			SourceEventIDs:     append([]string(nil), trace.RetrievedEvent...),
			RecentPhraseHashes: append([]uint64(nil), recentPhraseHashes...),
			CasualChat:         casualChat,
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
	return intentRegistry.specFor(intent).IsFact
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
		req.Talkativeness,
		req.Now,
	)
	if err != nil {
		return ProactiveResponse{}, err
	}
	trace.Reason = ReasonRelationshipMatchReaction
	if req.Critical && decision.ID != "" && decision.FactRevision != deliveryKey {
		req.OutputAllowed = false
		trace.Reason = ReasonCriticalFactRefreshLimit
	}
	if decision.ID != "" {
		trace.RelationshipDecision = &decision
		trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "relationship.apply", Args: map[string]string{"status": "ok", "decisionId": decision.ID}})
	} else {
		trace.Reason = ReasonOperatorEventProactive
	}
	reply := ""
	if req.OutputAllowed && (decision.ID == "" || decision.Speech != nil) {
		citation := strings.TrimSpace(req.CitationReason)
		trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "response.emit_companion_reply", Args: map[string]string{
			"eventId": req.Event.ID, "deliveryKey": deliveryKey, "eventType": req.Event.EventType, "clock": req.Event.Clock, "citation": citation,
		}})
		if citation != "" && decision.ID != "" {
			decision.ReasonCodes = append(decision.ReasonCodes, "proactive_citation:"+citation)
		}
		reply = req.Event.ProactiveText
		if strings.TrimSpace(reply) == "" {
			reply = FallbackProactiveText(req.Event)
		}
		// 判罚时刻知识附句（knowledge-event-triggers）：过门命中条目——
		// quote 作为织写锚进 RequiredAnchors（硬指令原样携带），织写没带住
		// 或无 realizer 路径时降级确定性附句 verbatim answer。
		knowledgeEntry := a.knowledgeTrigger(req, &trace)
		// 记忆进措辞层：运营 ProactiveText 是锚点（Q3）。仅 recall 材料
		// 非空且 director 给出 Speech 决策时带记忆重措辞，guard 不过回原文。
		// recall 检索词取事件实体（球员优先、球队兜底）——contains 语义下
		// 整段描述永远匹配不上。
		if decision.Speech != nil {
			focus := strings.TrimSpace(req.Event.PlayerName)
			if focus == "" {
				focus = strings.TrimSpace(req.Event.TeamName)
			}
			anchors := decision.Speech.Content.RequiredAnchors
			if knowledgeEntry != nil {
				anchors = append(append([]string(nil), anchors...), knowledgeEntry.Quote)
			}
			if realized, done := a.realizeWithMemory(ctx, IntentMatchReaction, req.UserID, req.Event.Description, focus, reply, anchors, decision, &trace); done {
				reply = realized
				trace.Reason = ReasonProactiveMemoryRealized
				trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "response.emit_companion_reply", Args: map[string]string{"mode": "realized", "source": "match_event_memory"}})
			}
		}
		if knowledgeEntry != nil && !knowledgeQuoteCarried(reply, knowledgeEntry) {
			reply += knowledgeDeterministicAppendix(knowledgeEntry)
			trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "knowledge.trigger_fallback", Args: map[string]string{"id": knowledgeEntry.ID, "mode": "deterministic_appendix"}})
		}
	} else if trace.Reason != ReasonCriticalFactRefreshLimit {
		trace.Reason = ReasonMatchObservedSilent
		trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "response.emit_companion_reply", Args: map[string]string{
			"eventId": req.Event.ID, "deliveryKey": deliveryKey, "eventType": req.Event.EventType, "mode": "silence",
		}})
	}
	trace.Output = reply
	trace.LatencyMS = int(time.Since(start).Milliseconds())
	// C2 writer hook: prediction threads open from the match event itself and
	// stay on the event trace (memory.append_thread).
	a.observeMatchEventThread(ctx, req, decision, &trace)
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

func (a *Agent) observeMatchEvent(ctx context.Context, userID string, ev matchstate.MatchEvent, outputAllowed, critical, userSpeaking bool, normalCooldownSeconds int, talkativeness string, now time.Time) (relationship.Decision, error) {
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
			Talkativeness:         talkativeness,
			Description:           ev.Description,
			TeamName:              ev.TeamName,
			PlayerName:            ev.PlayerName,
			RevisionOf:            ev.RevisionOf,
			Memory:                a.memorySignals(ctx, userID),
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
				trace.Reason = ReasonFirstMeetingDelivered
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

var insistenceAdverbs = []string{"明明", "真的", "确实", "千真万确", "就是"}

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
	memoryContext := a.recallMemoryBlock(ctx, req.UserID, req.Text, trace)
	portraitContext := a.portraitMemoryBlock(ctx, req.UserID, trace)
	realized, err := a.realizer.Realize(realizeCtx, RealizationRequest{
		UserInput:       req.Text,
		Intent:          intent,
		Grounding:       grounding,
		Decision:        decision,
		MemoryContext:   memoryContext,
		PortraitContext: portraitContext,
		ReliableText:    reliable,
	})
	if err != nil || strings.TrimSpace(realized.Text) == "" {
		if err != nil {
			trace.Error = strings.TrimSpace(err.Error())
		}
		trace.Reason = ReasonRealizeFallbackError
		trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "response.emit_companion_reply", Args: map[string]string{"mode": "deterministic", "fallback": "realizer_error"}})
		return reliable
	}
	// 与路由建议共用同一个 guard：拒绝原因、锚源纪律一处定义。
	validated, _ := guardValidateReply(req.Text, intent, realized.Text, anchors, reliable, decision)
	if validated == "" {
		trace.Reason = ReasonRealizeFallbackPolicy
		trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "response.emit_companion_reply", Args: map[string]string{"mode": "deterministic", "fallback": "policy"}})
		return reliable
	}
	trace.Reason = ReasonRelationshipPlanRealized
	trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "response.emit_companion_reply", Args: map[string]string{"mode": "realized"}})
	return validated
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

// RecordBackchannel 落微反应审计（ADR-0016）：trace + Interaction Ledger
// 各一条——伴随反应不过 C2 引用码门、不占回合槽，但全程可审计。
// traceID 由调用方传入（下发信封与落库同键，可关联）。
func (a *Agent) RecordBackchannel(ctx context.Context, userID, matchID, traceID, eventType, phrase string, now time.Time) error {
	if a == nil || a.tools == nil {
		return nil
	}
	trace := Trace{
		ID:      traceID,
		MatchID: matchID, UserID: userID,
		Input: eventType, Intent: IntentMatchReaction,
		Reason: "backchannel", CreatedAt: now, Output: phrase,
		ToolCalls: []ToolCall{{Name: "backchannel.emit", Args: map[string]string{"eventType": eventType}}},
	}
	if err := a.tools.WriteTrace(ctx, trace); err != nil {
		return err
	}
	if a.interactions != nil {
		_, _ = a.interactions.Append(ctx, interaction.Event{
			Kind: interaction.KindBackchannel, UserID: userID, MatchID: matchID,
			TraceID: trace.ID, Phrase: phrase, CreatedAt: now,
		})
	}
	return nil
}

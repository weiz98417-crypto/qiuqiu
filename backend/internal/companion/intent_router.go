package companion

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"qiuqiu/internal/observation"
	"qiuqiu/internal/router"
)

// routerConfidenceThreshold is locked decision 5: a routed fact-class intent
// only takes the deterministic fact path at confidence >= 0.7; below, the
// turn degrades to the casual realization with the evidence funnelled (C3).
const routerConfidenceThreshold = 0.7

// TurnRouter is the routing seam the agent consumes. *router.Client is the
// production implementation; the eval harness scripts its own so the golden
// journeys run without a key or network.
type TurnRouter interface {
	Route(ctx context.Context, req router.Request) (router.Result, error)
	Enabled() bool
}

// routeKeywordMiss sends a keyword-miss turn to the LLM router (ADR-0009).
// Exactly one attempt; the router client enforces the 6s timeout. Any
// failure returns nil and the turn keeps today's keyword-miss behavior —
// the legacy path is the degradation path (locked decision 6), never a
// console error.
func (a *Agent) routeKeywordMiss(ctx context.Context, req AgentBoundaryRequest, keywordIntent Intent, trace *Trace) *router.Result {
	if a.router == nil || !a.router.Enabled() || keywordIntent != IntentUnknown || strings.TrimSpace(req.Text) == "" {
		return nil
	}
	result, err := a.router.Route(ctx, router.Request{
		Text:    req.Text,
		Context: a.routerContextSummary(ctx, req, trace),
	})
	if err != nil {
		if trace != nil {
			trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "intent.route", Args: map[string]string{"status": "degraded", "error": shortLabel(err.Error(), 120)}})
		}
		return nil
	}
	if trace != nil {
		trace.Router = &RouterTrace{
			Intent:     result.Intent,
			Confidence: result.Confidence,
			Player:     result.Player,
			Team:       result.Team,
			Score:      result.Score,
		}
		trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "intent.route", Args: map[string]string{
			"intent":     result.Intent,
			"confidence": strconv.FormatFloat(result.Confidence, 'f', 2, 64),
		}})
	}
	return &result
}

// routerContextSummary builds the third message of the routing call: the
// bounded match snapshot the design's request shape carries. A snapshot read
// failure never blocks routing — the summary simply degrades.
func (a *Agent) routerContextSummary(ctx context.Context, req AgentBoundaryRequest, trace *Trace) string {
	var builder strings.Builder
	builder.WriteString("当前比赛上下文：")
	if a.tools != nil {
		if snapshot, err := a.tools.Snapshot(ctx, req.MatchID); err == nil && snapshot.HomeTeam != "" {
			builder.WriteString(fmt.Sprintf("%s 对 %s，%s，比分 %d-%d，比赛时间 %s。",
				snapshot.HomeTeam, snapshot.AwayTeam, displayPeriod(snapshot.Period),
				snapshot.Score.Home, snapshot.Score.Away, snapshot.Clock))
			if trace != nil {
				trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "match.read_snapshot", Args: map[string]string{"matchId": req.MatchID, "source": "intent.route"}})
			}
		} else {
			builder.WriteString("赛况暂时不可用。")
		}
	}
	builder.WriteString("请只分类用户这句话，不得依据上下文编造任何新事实。")
	return builder.String()
}

// routedTurnIntent maps the raw router intent onto the backend intents 1:1
// (locked decision 4). match_reaction is proactive-only downstream, so a
// routed match_reaction lands on the user-turn emotion path.
func routedTurnIntent(raw string) Intent {
	switch strings.TrimSpace(raw) {
	case "smalltalk":
		return IntentSmalltalk
	case "schedule_question":
		return IntentSchedule
	case "match_status_question":
		return IntentMatchStatus
	case "recent_event_question":
		return IntentRecentEvent
	case "follow_up_question":
		return IntentFollowUp
	case "player_question":
		return IntentPlayerQuestion
	case "match_fact_claim":
		return IntentMatchClaim
	case "match_reaction", "emotion_reaction":
		return IntentEmotionReaction
	case "personal_share":
		return IntentPersonalShare
	case "control_command":
		return IntentControlCommand
	default:
		return IntentUnknown
	}
}

// confidenceGatedIntent: intents whose deterministic path only fires at
// confidence >= 0.7 (locked decisions 4/5) — the fact class plus the two
// other deterministic non-chat paths (schedule lookup, control command).
func confidenceGatedIntent(intent Intent) bool {
	return isFactIntent(intent) || intent == IntentSchedule || intent == IntentControlCommand
}

// routerReplyEligibleIntent: design decision 4 — only chat-class turns (and
// the degraded-casual unknown) consume the router's reply suggestion.
func routerReplyEligibleIntent(intent Intent) bool {
	switch intent {
	case IntentSmalltalk, IntentEmotionReaction, IntentPersonalShare, IntentUnknown:
		return true
	default:
		return false
	}
}

// claimPersistedHoldReply is the C2 warm deterministic hold dialogue: the
// user insisting on a claim we already hold must feel heard — the fact
// posture (wait for a second source) does not change.
func claimPersistedHoldReply() string {
	return "我知道你看到了，我也记下了你这条。我要等慢一点的源确认，一有结果我立刻喊你。"
}

// ActiveObservationReader is the optional coordinator capability the C2
// persisted-claim detection reads. Both coordinators implement it; a
// coordinator without it simply never warm-holds.
type observationReader interface {
	ActiveObservations(ctx context.Context, userID, matchID string) ([]observation.PendingObservation, error)
}

// activeMatchingObservation reports whether the coordinator holds an active
// (pending/corroborating/conflict) observation from this same user+match
// whose content matches the current claim — the user repeating themselves is
// not a second source, so the hold stays warm instead of restarting.
func (a *Agent) activeMatchingObservation(ctx context.Context, req AgentBoundaryRequest, claim FactClaim) (observation.PendingObservation, bool) {
	if a.observations == nil || strings.TrimSpace(req.UserID) == "" || strings.TrimSpace(req.MatchID) == "" {
		return observation.PendingObservation{}, false
	}
	reader, ok := a.observations.(observationReader)
	if !ok {
		return observation.PendingObservation{}, false
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	observations, err := reader.ActiveObservations(lookupCtx, req.UserID, req.MatchID)
	if err != nil {
		return observation.PendingObservation{}, false
	}
	// The staleness check uses the turn's declared clock (the same time the
	// observation's ReceivedAt rides on), never the wall clock.
	now := req.Now
	if now.IsZero() {
		now = time.Now()
	}
	for _, pending := range observations {
		if claimMatchesObservation(claim, pending, now) {
			return pending, true
		}
	}
	return observation.PendingObservation{}, false
}

// claimMatchesObservation matches on claim semantics (event type, claimed
// player/team, claimed score), not verbatim text: "球进了" followed by
// "明明进了" rides the same hold, while a contradicted content (different
// claimed player) does not.
func claimMatchesObservation(claim FactClaim, pending observation.PendingObservation, now time.Time) bool {
	if pending.Status != observation.StatusPendingSync && pending.Status != observation.StatusCorroborating && pending.Status != observation.StatusConflict {
		return false
	}
	if !pending.ReconcileUntil.IsZero() && !now.Before(pending.ReconcileUntil) {
		return false
	}
	if pending.ClaimedPlayer != "" && claim.ClaimedPlayer != "" && !strings.EqualFold(pending.ClaimedPlayer, claim.ClaimedPlayer) {
		return false
	}
	if pending.ClaimedTeam != "" && claim.ClaimedTeam != "" && !strings.EqualFold(pending.ClaimedTeam, claim.ClaimedTeam) {
		return false
	}
	if claim.Kind == "score" {
		if pending.ClaimedScore == nil || claim.ClaimedScore == nil {
			return false
		}
		return *pending.ClaimedScore == *claim.ClaimedScore
	}
	if claim.ClaimedScore != nil && pending.ClaimedScore != nil {
		return *pending.ClaimedScore == *claim.ClaimedScore
	}
	if pending.EventType != "" && claim.EventType != "" && pending.EventType != claim.EventType {
		return false
	}
	return true
}

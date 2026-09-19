package companion

import (
	"context"
	"fmt"
	"strconv"
	"strings"

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

package companion

// 观察协调（C2 persisted-claim hold）的内聚家：用户坚持同一主张时的
// 温热保持判定。原散在 intent_router.go——路由文件只该有路由。

import (
	"context"
	"strings"
	"time"

	"qiuqiu/internal/observation"
)

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

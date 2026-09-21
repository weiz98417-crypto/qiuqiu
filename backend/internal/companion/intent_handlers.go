package companion

// 意图 handler（openspec/changes/intent-registry）：原 agent.go 意图大
// switch 的 11 个 case，逐字迁为注册表 handler 方法。行为不变式：trace
// 副作用顺序、reply/锚点/reason 取值、`break` 早退语义（= 携带已产出值
// 的提前 return）全部原样保留；共享局部变量收进 intentHandling。

import (
	"fmt"
	"strings"
	"time"
)

func (a *Agent) handleMatchClaim(t *userTurn) (intentHandling, error) {
	h := newIntentHandling()
	ctx, req, trace := t.ctx, t.req, t.trace
	h.allowRealize = false
	h.deterministicReason = "claim_policy"
	snapshot, err := a.tools.Snapshot(ctx, req.MatchID)
	if err != nil {
		return h, err
	}
	trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "match.read_snapshot", Args: map[string]string{"matchId": req.MatchID}})
	events, err := a.tools.RecentEvents(ctx, req.MatchID, 8)
	if err != nil {
		return h, err
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
		claim.Reason = ReasonMatchNotStarted
	}
	trace.Reason = ReasonUserMatchClaimPrefix + string(claim.Status)
	trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "match.verify_user_claim", Args: map[string]string{"kind": claim.Kind, "status": string(claim.Status)}})
	// intent-router C2: an insistence repeat of a claim this same user
	// already raised gets the warm deterministic hold, not the generic
	// one. Fact status is unchanged — the coordinator dedupe still owns
	// the observation, a repeat is not a second source.
	if claim.Status == ClaimStatusUnverified {
		if _, persisted := a.activeMatchingObservation(ctx, req, claim); persisted {
			h.claimPersisted = true
			trace.Reason = ReasonClaimPersistedHold
		}
		a.recordObservation(ctx, req, t.requestTraceID, claim, trace)
	}
	if claim.Kind == "score" {
		score := fmt.Sprintf("%d-%d", snapshot.Score.Home, snapshot.Score.Away)
		switch claim.Status {
		case ClaimStatusConfirmed:
			h.reply = fmt.Sprintf("对，场上现在是%s %s %s。", snapshot.HomeTeam, score, snapshot.AwayTeam)
		case ClaimStatusContradicted:
			h.reply = fmt.Sprintf("还没，场上现在是%s %s %s。", snapshot.HomeTeam, score, snapshot.AwayTeam)
		default:
			h.reply = "这条赛况我还不能确定，先别急着算。"
		}
		h.requiredAnchors = compactAnchors(snapshot.HomeTeam, score, snapshot.AwayTeam)
		return h, nil
	}
	if claim.Reason == ReasonMatchNotStarted {
		h.reply = "比赛还没开始，这条不能算。"
		return h, nil
	}
	switch claim.Status {
	case ClaimStatusConfirmed:
		if claim.ClaimedPlayer != "" {
			h.reply = fmt.Sprintf("对，刚才这球是%s进的。", claim.ActualPlayer)
		} else {
			h.reply = fmt.Sprintf("对，刚才是%s进的，进球的是%s。", claim.ActualTeam, claim.ActualPlayer)
		}
	case ClaimStatusContradicted:
		if claim.ClaimedPlayer != "" {
			h.reply = fmt.Sprintf("不是%s，刚才这球是%s进的。", claim.ClaimedPlayer, claim.ActualPlayer)
		} else {
			h.reply = fmt.Sprintf("不是%s，刚才是%s的%s进了。", claim.ClaimedTeam, claim.ActualTeam, claim.ActualPlayer)
		}
	default:
		if h.claimPersisted {
			h.reply = claimPersistedHoldReply()
		} else {
			h.reply = "我这边还没跟上这粒进球，先等一下看结果。"
		}
	}
	h.requiredAnchors = compactAnchors(claim.ActualPlayer, claim.ActualTeam)
	return h, nil
}

func (a *Agent) handleMatchStatus(t *userTurn) (intentHandling, error) {
	h := newIntentHandling()
	ctx, req, trace := t.ctx, t.req, t.trace
	h.allowRealize = false
	h.deterministicReason = "fact_policy"
	snapshot, err := a.tools.Snapshot(ctx, req.MatchID)
	if err != nil {
		return h, err
	}
	trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "match.read_snapshot", Args: map[string]string{"matchId": req.MatchID}})
	if issue := snapshotIntegrityIssue(snapshot); issue != "" {
		h.allowRealize = false
		h.deterministicReason = "snapshot_integrity"
		trace.Reason = ReasonMatchSnapshotInc
		h.reply = "这会儿赛况有点对不上，我先不报死，等一下再看。"
		if claim, ok := assessScoreClaim(req.Text, snapshot); ok {
			claim.Status = ClaimStatusUnverified
			claim.Reason = issue
			trace.Claim = &claim
			trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "match.verify_user_claim", Args: map[string]string{"kind": claim.Kind, "status": string(claim.Status)}})
		}
		return h, nil
	}
	if claim, ok := assessScoreClaim(req.Text, snapshot); ok {
		h.allowRealize = false
		h.deterministicReason = "claim_policy"
		trace.Claim = &claim
		trace.Reason = ReasonUserMatchClaimPrefix + string(claim.Status)
		trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "match.verify_user_claim", Args: map[string]string{"kind": claim.Kind, "status": string(claim.Status)}})
	}
	h.reply = fmt.Sprintf("现在是%s %d-%d %s，时间在%s %s。", snapshot.HomeTeam, snapshot.Score.Home, snapshot.Score.Away, snapshot.AwayTeam, displayPeriod(snapshot.Period), snapshot.Clock)
	h.requiredAnchors = compactAnchors(snapshot.HomeTeam, snapshot.AwayTeam, fmt.Sprintf("%d-%d", snapshot.Score.Home, snapshot.Score.Away), snapshot.Clock)
	return h, nil
}

func (a *Agent) handleRecentEvent(t *userTurn) (intentHandling, error) {
	h := newIntentHandling()
	ctx, req, trace := t.ctx, t.req, t.trace
	h.allowRealize = false
	h.deterministicReason = "fact_policy"
	events, err := a.tools.RecentEvents(ctx, req.MatchID, 8)
	if err != nil {
		return h, err
	}
	trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "match.search_events", Args: map[string]string{"matchId": req.MatchID, "limit": "8"}})
	h.reply, trace.RetrievedEvent = answerRecentEvent(req.Text, events)
	h.requiredAnchors = anchorsForEvents(events, trace.RetrievedEvent, h.reply)
	return h, nil
}

func (a *Agent) handleFollowUp(t *userTurn) (intentHandling, error) {
	h := newIntentHandling()
	ctx, req, trace := t.ctx, t.req, t.trace
	h.allowRealize = false
	h.deterministicReason = "fact_policy"
	turns, err := a.tools.RecentTurns(ctx, req.MatchID, req.UserID, 8)
	if err != nil {
		return h, err
	}
	trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "conversation.read_recent", Args: map[string]string{"matchId": req.MatchID, "userId": req.UserID, "limit": "8"}})
	events, err := a.tools.RecentEvents(ctx, req.MatchID, 8)
	if err != nil {
		return h, err
	}
	trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "match.search_events", Args: map[string]string{"matchId": req.MatchID, "limit": "8"}})
	h.reply, trace.RetrievedEvent = answerFollowUp(req.Text, turns, events)
	h.requiredAnchors = anchorsForEvents(events, trace.RetrievedEvent, h.reply)
	return h, nil
}

func (a *Agent) handlePlayerQuestion(t *userTurn) (intentHandling, error) {
	h := newIntentHandling()
	ctx, req, trace := t.ctx, t.req, t.trace
	h.allowRealize = false
	h.deterministicReason = "fact_policy"
	player := inferPlayer(req.Text)
	events, err := a.tools.EventsByPlayer(ctx, req.MatchID, player, 8)
	if err != nil {
		return h, err
	}
	trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "match.get_player_timeline", Args: map[string]string{"matchId": req.MatchID, "playerName": player, "limit": "8"}})
	h.reply, trace.RetrievedEvent = answerPlayerQuestion(req.Text, player, events)
	h.requiredAnchors = compactAnchors(player)
	h.requiredAnchors = append(h.requiredAnchors, anchorsForEvents(events, trace.RetrievedEvent, h.reply)...)
	return h, nil
}

func (a *Agent) handleControlCommand(t *userTurn) (intentHandling, error) {
	h := newIntentHandling()
	h.reply = "收到，我会少说一点，关键变化再提醒你。"
	return h, nil
}

func (a *Agent) handleSchedule(t *userTurn) (intentHandling, error) {
	h := newIntentHandling()
	ctx, req, trace := t.ctx, t.req, t.trace
	h.allowRealize = false
	h.deterministicReason = "schedule_policy"
	scheduleIntent := *trace.Schedule
	if scheduleIntent.Scope == ScheduleScopeCurrent || scheduleIntent.Scope == ScheduleScopeNearby {
		if snapshot, err := a.tools.Snapshot(ctx, req.MatchID); err == nil {
			trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "match.read_snapshot", Args: map[string]string{"matchId": req.MatchID}})
			if currentReply, ok := activeMatchScheduleReply(snapshot); ok {
				h.deterministicReason = ReasonActiveMatchContext
				trace.Reason = ReasonActiveMatchContext
				h.reply = currentReply
				return h, nil
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
			h.scheduleLookup = &ScheduleLookup{
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
			trace.Reason = ReasonScheduleLookupAck
			h.reply = scheduleLookupAcknowledgement(scheduleIntent.Scope)
			return h, nil
		}
		trace.ToolCalls = append(trace.ToolCalls, ToolCall{
			Name: "schedule.search",
			Args: scheduleSearchToolArgs(searchRequest),
		})
		result, err := searchReader.Search(ctx, searchRequest)
		if err != nil {
			trace.Error = strings.TrimSpace(err.Error())
			trace.Reason = ReasonScheduleUnavailable
			h.reply = "赛程源这次没接上，我不先乱报。"
			return h, nil
		}
		h.reply = formatScheduleSearchResult(result, scheduleIntent.Scope)
		return h, nil
	}
	trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "schedule.read_today", Args: map[string]string{"scope": "today"}})
	if a.scheduleReader == nil {
		h.reply = "今天的赛程我还没拿到，你想查哪个联赛？"
		return h, nil
	}
	fixtures, err := a.scheduleReader.TodayFixtures(ctx)
	if err != nil {
		trace.Error = strings.TrimSpace(err.Error())
		trace.Reason = ReasonScheduleUnavailable
		h.reply = "今天的赛程查询没接上，你想查哪个联赛？"
		return h, nil
	}
	h.reply = formatTodaySchedule(fixtures)
	return h, nil
}

func (a *Agent) handleEmotionReaction(t *userTurn) (intentHandling, error) {
	h := newIntentHandling()
	ctx, req, trace := t.ctx, t.req, t.trace
	if isGroundedMatchReaction(req.Text) {
		h.allowRealize = false
		h.deterministicReason = "deictic_event_grounding"
		events, err := a.tools.RecentEvents(ctx, req.MatchID, 8)
		if err != nil {
			return h, err
		}
		trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "match.search_events", Args: map[string]string{"matchId": req.MatchID, "limit": "8"}})
		var claim FactClaim
		h.reply, claim, trace.RetrievedEvent = answerDeicticMatchReaction(req.Text, events)
		trace.Claim = &claim
		trace.Reason = ReasonUserEventReferencePrfx + string(claim.Status)
		trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "match.verify_user_claim", Args: map[string]string{"kind": claim.Kind, "status": string(claim.Status)}})
		if claim.Status == ClaimStatusUnverified {
			a.recordObservation(ctx, req, t.requestTraceID, claim, trace)
		}
		h.requiredAnchors = anchorsForEvents(events, trace.RetrievedEvent, h.reply)
		return h, nil
	}
	h.reply = emotionReactionReply(req.Text)
	return h, nil
}

func (a *Agent) handlePersonalShare(t *userTurn) (intentHandling, error) {
	h := newIntentHandling()
	h.reply = personalShareReply(t.req.Text)
	return h, nil
}

func (a *Agent) handleSmalltalk(t *userTurn) (intentHandling, error) {
	h := newIntentHandling()
	h.reply = smalltalkFallbackReply(t.req.Text)
	return h, nil
}

func (a *Agent) handleUnknownTurn(t *userTurn) (intentHandling, error) {
	h := newIntentHandling()
	h.reply = "这句我没接明白，你换个说法？"
	return h, nil
}

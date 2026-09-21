package companion

// 意图 handler（openspec/changes/intent-registry）：原 agent.go 意图大
// switch 的 11 个 case，逐字迁为注册表 handler 方法。行为不变式：trace
// 副作用顺序、reply/锚点/reason 取值、`break` 早退语义（= 携带已产出值
// 的提前 return）全部原样保留；共享局部变量收进 intentHandling。

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/proactive"
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

// handleReminderRequest 落 ADR-0015 的提醒簿：pre_match 且赛程源给了
// 开球时间才记；其余情况如实回话，不假装记上了。
func (a *Agent) handleReminderRequest(t *userTurn) (intentHandling, error) {
	h := newIntentHandling()
	ctx, req, trace := t.ctx, t.req, t.trace
	h.allowRealize = false
	h.deterministicReason = "reminder_policy"
	if a.reminders == nil {
		h.reply = "提醒簿还没接上，等我能记事的时候你再叫我一声。"
		return h, nil
	}
	snapshot, err := a.tools.Snapshot(ctx, req.MatchID)
	if err != nil {
		return h, err
	}
	trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "match.read_snapshot", Args: map[string]string{"matchId": req.MatchID}})
	if snapshot.Period != "pre_match" {
		h.reply = "这场已经开赛了，下次开球前提前叫我。"
		return h, nil
	}
	kickoff, ok := a.reminderKickoff(ctx, req, snapshot)
	if !ok {
		h.reply = "赛程源还没给我这场几点开球，等我知道了，你再叫我一声。"
		return h, nil
	}
	// 查重：同一场比赛已有在途提醒就不重复落簿（重复投递 = 重复打扰）。
	if pendings, err := a.reminders.PendingForUser(ctx, req.UserID); err == nil {
		for _, existing := range pendings {
			if existing.MatchID == req.MatchID && teamNamesAlign(existing.HomeTeam, existing.AwayTeam, snapshot.HomeTeam, snapshot.AwayTeam) {
				trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "reminder.duplicate", Args: map[string]string{"reminderId": existing.ID}})
				h.reply = "记着呢，开球前我会叫你，放心。"
				return h, nil
			}
		}
	}
	reminder, err := a.reminders.Append(ctx, proactive.Reminder{
		UserID:    req.UserID,
		MatchID:   req.MatchID,
		HomeTeam:  snapshot.HomeTeam,
		AwayTeam:  snapshot.AwayTeam,
		KickoffAt: kickoff,
		Timezone:  req.Timezone,
		CreatedAt: req.Now,
	})
	if err != nil {
		trace.Error = strings.TrimSpace(err.Error())
		h.reply = "提醒没记上，我这边出了点小状况，再试一次？"
		return h, nil
	}
	trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "reminder.create", Args: map[string]string{"reminderId": reminder.ID, "kickoffAt": kickoff.UTC().Format(time.RFC3339)}})
	trace.Reason = ReasonReminderScheduled
	h.reply = fmt.Sprintf("好，%s 对 %s 开球前%d分钟我叫你。", snapshot.HomeTeam, snapshot.AwayTeam, reminder.LeadMinutes)
	return h, nil
}

// handleKnowledgeQuestion 落 ADR-0017 的知识域：策展条目检索命中即逐字
// 回答（确定性拼装，realizer 不碰），无命中如实说不知道——绝不生成知识。
func (a *Agent) handleKnowledgeQuestion(t *userTurn) (intentHandling, error) {
	h := newIntentHandling()
	ctx, req, trace := t.ctx, t.req, t.trace
	h.allowRealize = false
	h.deterministicReason = "knowledge_policy"
	if a.knowledge == nil {
		h.reply = "知识库还没接上，这块我先不敢乱说。"
		return h, nil
	}
	entry, ok := a.knowledge.Search(ctx, req.Text)
	if !ok {
		h.reply = "这个我还真不敢乱说，等我把功课补上再答你。"
		return h, nil
	}
	trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "knowledge.answer", Args: map[string]string{
		"entryId":    entry.ID,
		"confidence": strconv.FormatFloat(entry.Confidence, 'f', 2, 64),
	}})
	trace.Reason = ReasonKnowledgeAnswered
	h.reply = entry.Answer
	return h, nil
}

// reminderKickoff 从赛程源找本场的开球时间：优先搜索窗口（昨天到后天），
// 降级今日赛程；队伍名对上（不分主客）即采用。
func (a *Agent) reminderKickoff(ctx context.Context, req AgentBoundaryRequest, snapshot matchstate.Snapshot) (time.Time, bool) {
	if a.scheduleReader == nil {
		return time.Time{}, false
	}
	now := req.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var fixtures []ScheduleMatch
	if searchReader, ok := a.scheduleReader.(ScheduleSearchReader); ok {
		if result, err := searchReader.Search(ctx, ScheduleSearchRequest{
			From: now.AddDate(0, 0, -1),
			To:   now.AddDate(0, 0, 2),
		}); err == nil {
			fixtures = result.Fixtures
		}
	} else {
		if today, err := a.scheduleReader.TodayFixtures(ctx); err == nil {
			fixtures = today
		}
	}
	for _, fixture := range fixtures {
		if fixture.KickoffAt.IsZero() {
			continue
		}
		if teamNamesAlign(fixture.HomeTeam, fixture.AwayTeam, snapshot.HomeTeam, snapshot.AwayTeam) ||
			teamNamesAlign(fixture.AwayTeam, fixture.HomeTeam, snapshot.HomeTeam, snapshot.AwayTeam) {
			return fixture.KickoffAt, true
		}
	}
	return time.Time{}, false
}

// teamNamesAlign 要求两队名非空且各自对上（别名感知 + 预备队排除），
// 防止空串全匹配与"曼联 U21"误配"曼联"（season-subscription 还债）。
func teamNamesAlign(leftHome, leftAway, rightHome, rightAway string) bool {
	if leftHome == "" || leftAway == "" || rightHome == "" || rightAway == "" {
		return false
	}
	if proactive.IsReserveOrYouthTeam(leftHome) || proactive.IsReserveOrYouthTeam(leftAway) {
		return false
	}
	return proactive.TeamNameAligns(leftHome, rightHome) && proactive.TeamNameAligns(leftAway, rightAway)
}

// handleSubscriptionManage 落订阅簿（Q8/Q9/Q18）：订阅、列出、取消三动作
// 全走对话即接口；队名从 14 天赛程窗口的参赛队里按字符对齐提取。
func (a *Agent) handleSubscriptionManage(t *userTurn) (intentHandling, error) {
	h := newIntentHandling()
	ctx, req, trace := t.ctx, t.req, t.trace
	h.allowRealize = false
	h.deterministicReason = "subscription_policy"
	if a.subscriptions == nil {
		h.reply = "订阅簿还没接上，等我能记长事的时候再说。"
		return h, nil
	}
	text := strings.TrimSpace(req.Text)

	// 列表。
	if containsAny(text, "列出", "我的订阅") {
		subs, err := a.subscriptions.ActiveForUser(ctx, req.UserID)
		if err != nil {
			return h, err
		}
		if len(subs) == 0 {
			h.reply = "你还没有订阅任何球队，说「以后XX的比赛都叫我」就行。"
			return h, nil
		}
		names := make([]string, 0, len(subs))
		for _, sub := range subs {
			names = append(names, sub.TeamName)
		}
		h.reply = "你现在订了：" + strings.Join(names, "、") + "。想取消就说「别叫我XX的了」。"
		return h, nil
	}

	// 取消：在活跃订阅里找被点名的队。
	if containsAny(text, "取消", "别叫", "别提醒") {
		subs, err := a.subscriptions.ActiveForUser(ctx, req.UserID)
		if err != nil {
			return h, err
		}
		for _, sub := range subs {
			if mentionsRunes(text, sub.TeamName) >= 2 {
				if _, done, err := a.subscriptions.CancelTeam(ctx, req.UserID, sub.TeamName); err == nil && done {
					_ = a.remindersSuppressSubscription(ctx, req.UserID, sub.ID)
					trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "subscription.cancel", Args: map[string]string{"subscriptionId": sub.ID}})
					trace.Reason = ReasonSubscriptionCancelled
					h.reply = "好，以后" + sub.TeamName + "的比赛不叫你了，想恢复随时说。"
					return h, nil
				}
			}
		}
		h.reply = "想取消哪支球队？说个队名，比如「别叫我皇马的了」。"
		return h, nil
	}

	// 订阅：从赛程窗口的参赛队里对齐用户提到的队名。
	team, ok := a.extractFixtureTeam(ctx, text, req)
	if !ok {
		h.reply = "想订阅哪支球队？说个队名，比如「以后皇马的比赛都叫我」。"
		return h, nil
	}
	subs, err := a.subscriptions.ActiveForUser(ctx, req.UserID)
	if err != nil {
		return h, err
	}
	if len(subs) >= proactive.MaxSubscriptionsPerUser {
		h.reply = "最多订三支球队，先取消一支再订吧。"
		return h, nil
	}
	sub, err := a.subscriptions.Append(ctx, proactive.Subscription{UserID: req.UserID, TeamName: team, CreatedAt: req.Now})
	if err != nil {
		trace.Error = strings.TrimSpace(err.Error())
		h.reply = "订阅没记上，再试一次？"
		return h, nil
	}
	trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "subscription.create", Args: map[string]string{"subscriptionId": sub.ID, "team": sub.TeamName}})
	trace.Reason = ReasonSubscriptionScheduled
	h.reply = "好，以后" + sub.TeamName + "的比赛，开球前我都叫你。"
	return h, nil
}

// extractFixtureTeam 从 14 天赛程窗口的参赛队里找用户文本提到的队名
//（字符对齐：文本里出现队名 ≥2 个不同字符即候选，取最多者；预备队/
// 青年队/女足排除）。搜索读不到时降级今日赛程。
func (a *Agent) extractFixtureTeam(ctx context.Context, text string, req AgentBoundaryRequest) (string, bool) {
	if a.scheduleReader == nil {
		return "", false
	}
	now := req.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var fixtures []ScheduleMatch
	if searchReader, ok := a.scheduleReader.(ScheduleSearchReader); ok {
		if result, err := searchReader.Search(ctx, ScheduleSearchRequest{
			From: now,
			To:   now.Add(14 * 24 * time.Hour),
		}); err == nil {
			fixtures = result.Fixtures
		}
	} else {
		if today, err := a.scheduleReader.TodayFixtures(ctx); err == nil {
			fixtures = today
		}
	}
	seen := map[string]bool{}
	bestTeam := ""
	bestScore := 0
	for _, fixture := range fixtures {
		for _, team := range []string{fixture.HomeTeam, fixture.AwayTeam} {
			team = strings.TrimSpace(team)
			if team == "" || seen[team] || proactive.IsReserveOrYouthTeam(team) {
				continue
			}
			seen[team] = true
			if score := mentionsRunes(text, team); score >= 2 && score > bestScore {
				bestScore = score
				bestTeam = team
			}
		}
	}
	return bestTeam, bestTeam != ""
}

// mentionsRunes 统计队名（别名展开后）里有多少个不同字符出现在文本中
//——"皇马"对"以后皇马都叫我"得 2。
func mentionsRunes(text, team string) int {
	seen := map[rune]bool{}
	count := 0
	for _, r := range proactive.AlignKey(team) {
		if r == ' ' || seen[r] {
			continue
		}
		seen[r] = true
		if strings.ContainsRune(text, r) {
			count++
		}
	}
	return count
}

// remindersSuppressSubscription 是可选依赖的守门包装：提醒簿在场才静默。
func (a *Agent) remindersSuppressSubscription(ctx context.Context, userID, subscriptionID string) error {
	if a.reminders == nil {
		return nil
	}
	return a.reminders.SuppressPendingSubscription(ctx, userID, subscriptionID)
}

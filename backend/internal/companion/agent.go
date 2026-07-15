package companion

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/relationship"
)

type Intent string

var traceSequence atomic.Uint64

const (
	IntentSmalltalk       Intent = "smalltalk"
	IntentMatchStatus     Intent = "match_status_question"
	IntentRecentEvent     Intent = "recent_event_question"
	IntentMatchReaction   Intent = "match_reaction"
	IntentFollowUp        Intent = "follow_up_question"
	IntentPlayerQuestion  Intent = "player_question"
	IntentEmotionReaction Intent = "emotion_reaction"
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
	SignalID string
	MatchID  string
	UserID   string
	Text     string
	Now      time.Time
	Voice    *VoiceTraceMetadata
}

type Response struct {
	Intent       Intent
	Reply        string
	Trace        Trace
	Presentation relationship.PresentationPlan
}

type ProactiveResponse struct {
	Reply        string
	Trace        Trace
	Decision     relationship.Decision
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
	ID                   string                 `json:"id"`
	MatchID              string                 `json:"matchId"`
	UserID               string                 `json:"userId"`
	Input                string                 `json:"input"`
	Intent               Intent                 `json:"intent"`
	ToolCalls            []ToolCall             `json:"toolCalls"`
	RetrievedEvent       []string               `json:"retrievedEventIds"`
	Output               string                 `json:"output"`
	Reason               string                 `json:"reason"`
	LatencyMS            int                    `json:"latencyMs"`
	Error                string                 `json:"error"`
	Voice                *VoiceTraceMetadata    `json:"voice,omitempty"`
	Claim                *FactClaim             `json:"claim,omitempty"`
	RelationshipDecision *relationship.Decision `json:"relationshipDecision,omitempty"`
	CreatedAt            time.Time              `json:"createdAt"`
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
	tools          MemoryTools
	realizer       ReplyRealizer
	director       relationship.CompanionDirector
	realizeTimeout time.Duration
}

func NewAgent(tools MemoryTools) *Agent {
	return &Agent{tools: tools, realizeTimeout: 800 * time.Millisecond}
}

func (a *Agent) WithRealizer(realizer ReplyRealizer, timeout time.Duration) *Agent {
	a.realizer = realizer
	if timeout > 0 {
		a.realizeTimeout = timeout
	}
	return a
}

func (a *Agent) WithDirector(director relationship.CompanionDirector) *Agent {
	a.director = director
	return a
}

func (a *Agent) UpdateTrace(ctx context.Context, trace Trace) error {
	return a.tools.UpdateTrace(ctx, trace)
}

func (a *Agent) HandleMessage(ctx context.Context, req MessageRequest) (Response, error) {
	return a.HandleBoundaryRequest(ctx, AgentBoundaryRequest{
		SignalID: req.SignalID,
		MatchID:  req.MatchID,
		UserID:   req.UserID,
		Text:     req.Text,
		Now:      req.Now,
		Voice:    req.Voice,
	})
}

func (a *Agent) HandleBoundaryRequest(ctx context.Context, req AgentBoundaryRequest) (Response, error) {
	start := time.Now()
	intent := Classify(req.Text)
	requestTraceID := traceID(req.Now)
	if signalID := strings.TrimSpace(req.SignalID); signalID != "" && len(signalID) <= 256 {
		requestTraceID = stableTraceID(req.UserID, req.MatchID, signalID)
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

	var reply string
	var requiredAnchors []string
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
		trace.Reason = "user_match_claim_" + string(claim.Status)
		trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "match.verify_user_claim", Args: map[string]string{"kind": claim.Kind, "status": string(claim.Status)}})
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
	case IntentEmotionReaction:
		reply = "哈哈我也有点上头，但我会按已经确认的比赛信息来，不乱说。"
	case IntentSmalltalk:
		reply = "我在，陪你看。你想聊比赛我就跟着场上节奏走，想闲聊也行。"
	default:
		reply = "这个我先按陪看来理解。要是你问这场比赛，我会根据已经确认的比赛信息回答。"
	}
	if containsMatchFactLanguage(req.Text) && len(requiredAnchors) == 0 {
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
	return Response{Intent: intent, Reply: reply, Trace: trace, Presentation: presentation}, nil
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
		switch {
		case containsAny(input, "紧张", "悬", "绷"):
			return "这一下是真绷着。先看这波。"
		case containsAny(input, "漂亮", "舒服"):
			return "嗯，这一下真漂亮。"
		case containsAny(input, "牛", "太激动", "上头"):
			return "这下确实顶。"
		default:
			return "嗯，这一下有感觉。"
		}
	}
	if intent == IntentSmalltalk {
		switch {
		case containsAny(input, "在吗", "在不在"):
			return "在，看着呢。"
		case containsAny(input, "你好", "嗨", "哈喽"):
			return "嗨，来了。先看球。"
		case containsAny(input, "继续", "接着"):
			return "嗯，接着看。"
		default:
			return "嗯，我在。"
		}
	}
	return original
}

func shouldRealizeUserTurn(intent Intent, decision relationship.Decision) bool {
	if decision.Speech == nil {
		return false
	}
	return intent == IntentSmalltalk || intent == IntentEmotionReaction || hasCommunicationAct(decision.Actions, relationship.ActRecall) || hasCommunicationAct(decision.Actions, relationship.ActRepair) || hasCommunicationAct(decision.Actions, relationship.ActAsk)
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
	if req.Now.IsZero() {
		req.Now = time.Now().UTC()
	}
	start := time.Now()
	trace := Trace{
		ID:             stableTraceID(req.UserID, req.Event.MatchID, "match:"+req.Event.ID),
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
	if decision.ID != "" {
		trace.RelationshipDecision = &decision
		trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "relationship.apply", Args: map[string]string{"status": "ok", "decisionId": decision.ID}})
	} else {
		trace.Reason = "operator_event_proactive_line"
	}
	reply := ""
	if req.OutputAllowed && (decision.ID == "" || decision.Speech != nil) {
		trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "response.emit_companion_reply", Args: map[string]string{
			"eventId": req.Event.ID, "eventType": req.Event.EventType, "clock": req.Event.Clock,
		}})
		reply = req.Event.ProactiveText
		if strings.TrimSpace(reply) == "" {
			reply = fallbackProactive(req.Event, req.Snapshot)
		}
	} else {
		trace.Reason = "relationship_match_observed_silent"
		trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "response.emit_companion_reply", Args: map[string]string{
			"eventId": req.Event.ID, "eventType": req.Event.EventType, "mode": "silence",
		}})
	}
	trace.Output = reply
	trace.LatencyMS = int(time.Since(start).Milliseconds())
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
	return a.director.Apply(ctx, relationship.Signal{
		ID:           "match:" + userID + ":" + ev.ID,
		TraceID:      stableTraceID(userID, ev.MatchID, "match:"+ev.ID),
		Kind:         relationship.SignalMatchEvent,
		UserID:       userID,
		MatchID:      ev.MatchID,
		OccurredAt:   now,
		ReceivedAt:   time.Now().UTC(),
		FactRevision: ev.ID,
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
	if isScoreClaim(lower) || isEventClaim(lower) {
		return IntentMatchClaim
	}
	if scoreClaimPattern.FindStringSubmatch(lower) != nil && containsAny(lower, "吗", "么", "是不是", "?", "？") {
		return IntentMatchStatus
	}
	if containsAny(lower, "别说", "少说", "闭嘴", "安静", "别播报") {
		return IntentControlCommand
	}
	if containsAny(lower, "哈哈", "太激动", "紧张", "舒服", "漂亮", "牛") {
		return IntentEmotionReaction
	}
	if containsAny(lower, "几比几", "比分", "现在多少", "现在几") {
		return IntentMatchStatus
	}
	if containsAny(lower, "进球了吗", "表现", "有没有进球", "有进球") {
		return IntentPlayerQuestion
	}
	if containsAny(lower, "谁进的球", "谁进的", "谁破门", "进球是谁", "谁打进", "谁得分") {
		return IntentRecentEvent
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
	if len([]rune(trimmed)) <= 12 {
		return IntentSmalltalk
	}
	return IntentUnknown
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
	if !containsAny(text, "进球了", "破门了", "得分了") {
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
		"比分", "进球", "破门", "得分", "领先", "扳平", "反超", "红牌", "黄牌", "VAR", "var",
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
	for _, marker := range []string{"进球了", "破门了", "得分了"} {
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
	if containsAny(text, "谁进的球", "谁进的", "谁破门", "进球是谁", "谁打进", "谁得分") {
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
	grounding := relationship.GroundedContent{
		Intent:          string(intent),
		ReliableText:    reliable,
		RequiredAnchors: append([]string(nil), anchors...),
		FactMode:        relationship.FactModeNone,
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
	trace.Reason = "relationship_plan_realized"
	trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "response.emit_companion_reply", Args: map[string]string{"mode": "realized"}})
	return strings.TrimSpace(realized.Text)
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

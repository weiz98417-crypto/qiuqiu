package companion

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"qiuqiu/internal/memory"
	"qiuqiu/internal/relationship"
)

// C2 open-thread bounds: candidates are bounded, deduped and written off the
// turn path — a thread write must never block or fail a turn.
const (
	maxThreadContentRunes      = 120
	maxThreadRecoveriesPerTurn = 1
	maxThreadRecoveriesPerBeat = 2
)

// threadStore exposes the optional C2 write capability of the memory seam
// (Queue fronts the local open_threads store; the Fake mirrors it).
func (a *Agent) threadStore() memory.ThreadStore {
	if a == nil || a.memories == nil {
		return nil
	}
	store, _ := a.memories.(memory.ThreadStore)
	return store
}

// observeTurnThreads appends the open-thread candidates derived from a
// finished user turn and records them on the trace; failures are swallowed
// because memory must never fail a turn. Fact-refresh replays of a prior
// turn never open threads.
func (a *Agent) observeTurnThreads(ctx context.Context, userID, signalID, text, factRefresh string, intent Intent, reply string, now time.Time, trace *Trace) {
	store := a.threadStore()
	if store == nil || strings.TrimSpace(factRefresh) != "" {
		return
	}
	for _, thread := range userTurnThreadCandidates(userID, signalID, text, intent, reply, now) {
		a.appendThreadCandidate(ctx, store, thread, trace)
	}
}

// observeMatchEventThread opens a prediction thread from score-changing match
// drama when the user's policy table has the prediction banter domain open
// (RelationshipView.PredictionBanter); the thread cites the fact ledger
// sequence for provenance.
func (a *Agent) observeMatchEventThread(ctx context.Context, req MatchEventRequest, decision relationship.Decision, trace *Trace) {
	store := a.threadStore()
	if store == nil {
		return
	}
	if thread, ok := matchEventThreadCandidate(req, decision); ok {
		a.appendThreadCandidate(ctx, store, thread, trace)
	}
}

func (a *Agent) appendThreadCandidate(ctx context.Context, store memory.ThreadStore, thread memory.Thread, trace *Trace) {
	appendCtx, cancel := context.WithTimeout(ctx, memoryObserveTimeout)
	defer cancel()
	if open, err := store.OpenThreads(appendCtx, thread.UserID); err == nil {
		for _, existing := range open {
			if existing.Kind == thread.Kind && existing.Content == thread.Content {
				return
			}
		}
	}
	appended, err := store.AppendThread(appendCtx, thread)
	if err != nil {
		if !errors.Is(err, memory.ErrNotSupported) {
			log.Printf("memory: append open thread: %v", err)
		}
		return
	}
	if trace != nil {
		trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "memory.append_thread", Args: map[string]string{
			"threadId": appended.ID, "kind": string(appended.Kind), "state": appended.State,
		}})
	}
}

// userTurnThreadCandidates mirrors the moment writers: unanswered questions
// (question text whose reply had no definitive answer), promises
// (待会儿告诉你/稍后 wording) and strong emotional exchanges reuse the same
// deterministic markers as the memory heuristic.
func userTurnThreadCandidates(userID, signalID, text string, intent Intent, reply string, now time.Time) []memory.Thread {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	occurred := now
	if occurred.IsZero() {
		occurred = time.Now().UTC()
	}
	var threads []memory.Thread
	// intent-router C3: every unknown turn feeds the vocabulary funnel —
	// not just questions. Same bounds/dedupe/TTL as the other candidates.
	if intent == IntentUnknown {
		threads = append(threads, memory.Thread{
			UserID: userID, Kind: memory.ThreadUnroutable,
			Content: clampThreadContent(text), SourceTurn: signalID, CreatedAt: occurred,
		})
	}
	if isUnansweredQuestion(text, intent, reply) {
		threads = append(threads, memory.Thread{
			UserID: userID, Kind: memory.ThreadUnansweredQuestion,
			Content: clampThreadContent(text), SourceTurn: signalID, CreatedAt: occurred,
		})
	}
	if containsAny(text, memory.PromiseMarkers...) {
		threads = append(threads, memory.Thread{
			UserID: userID, Kind: memory.ThreadPromise,
			Content: clampThreadContent("待跟进的约定：" + text), SourceTurn: signalID, CreatedAt: occurred,
		})
	}
	if intent == IntentEmotionReaction || containsAny(text, memory.EmotionMarkers...) {
		threads = append(threads, memory.Thread{
			UserID: userID, Kind: memory.ThreadEmotionalMoment,
			Content: clampThreadContent("情绪交流：" + text), SourceTurn: signalID, CreatedAt: occurred,
		})
	}
	return threads
}

// isUnansweredQuestion: the user asked something and the companion could not
// answer definitively (unknown intent, or the deterministic reply said no
// record was found) — exactly the loop the 隔轮补答 beat closes later.
func isUnansweredQuestion(text string, intent Intent, reply string) bool {
	if !containsAny(text, "？", "?", "吗", "谁", "什么", "为什么", "哪里", "哪", "几") {
		return false
	}
	if intent == IntentUnknown {
		return true
	}
	return replyLacksDefinitiveAnswer(reply)
}

func replyLacksDefinitiveAnswer(reply string) bool {
	return containsAny(reply, "没有看到", "没看到", "还没有", "没接明白", "不能确定", "不能算", "暂时没有", "没有明确")
}

// matchEventThreadCandidate snapshots score-changing drama as prediction
// material once prediction banter is allowed; routine events never open
// threads.
func matchEventThreadCandidate(req MatchEventRequest, decision relationship.Decision) (memory.Thread, bool) {
	event := req.Event
	if strings.TrimSpace(event.ID) == "" {
		return memory.Thread{}, false
	}
	if !decision.Relationship.PredictionBanter {
		return memory.Thread{}, false
	}
	label, ok := memoryEventTypeLabel(event.EventType)
	if !ok {
		return memory.Thread{}, false
	}
	occurred := req.Now
	if occurred.IsZero() {
		occurred = time.Now().UTC()
	}
	content := clampThreadContent("预测素材：" + strings.TrimSpace(event.Clock) + " " + label + "：" + strings.TrimSpace(event.Description))
	return memory.Thread{
		UserID:         req.UserID,
		Kind:           memory.ThreadPrediction,
		Content:        content,
		SourceTurn:     event.ID,
		LedgerSequence: event.RecordedSequence,
		CreatedAt:      occurred,
	}, true
}

func clampThreadContent(content string) string {
	runes := []rune(strings.TrimSpace(content))
	if len(runes) > maxThreadContentRunes {
		return string(runes[:maxThreadContentRunes])
	}
	return string(runes)
}

// appendThreadRecovery closes open unanswered-question loops in-turn (隔轮补答):
// when the fact the user asked about has since been recorded, the answer is
// appended to the current reply and the thread is marked addressed. One
// recovery per turn; failure-safe — any error leaves the reply untouched.
func (a *Agent) appendThreadRecovery(ctx context.Context, req AgentBoundaryRequest, intent Intent, reply string, trace *Trace) string {
	store := a.threadStore()
	if store == nil || reply == "" || trace == nil {
		return reply
	}
	switch intent {
	case IntentPlayerQuestion, IntentRecentEvent, IntentFollowUp, IntentMatchClaim, IntentMatchStatus:
		// Fact-answering intents carry their own anchored answer; a recovery
		// append would only clutter it.
		return reply
	}
	recoveryCtx, cancel := context.WithTimeout(ctx, memoryRecallTimeout)
	defer cancel()
	open, err := store.OpenThreads(recoveryCtx, req.UserID)
	if err != nil {
		return reply
	}
	for _, thread := range open {
		if thread.Kind != memory.ThreadUnansweredQuestion || thread.Content == clampThreadContent(req.Text) {
			continue
		}
		player := inferPlayer(thread.Content)
		if player == "" {
			continue
		}
		events, err := a.tools.EventsByPlayer(ctx, req.MatchID, player, 8)
		if err != nil {
			return reply
		}
		answer, eventIDs := answerPlayerQuestion(thread.Content, player, events)
		if len(eventIDs) == 0 || replyLacksDefinitiveAnswer(answer) {
			continue
		}
		if err := store.MarkThreadAddressed(recoveryCtx, thread.ID); err != nil && !errors.Is(err, memory.ErrNotSupported) {
			log.Printf("memory: mark thread %s addressed: %v", thread.ID, err)
			return reply
		}
		trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "memory.recover_thread", Args: map[string]string{
			"threadId": thread.ID, "kind": string(thread.Kind), "player": player,
		}})
		trace.RetrievedEvent = append(trace.RetrievedEvent, eventIDs...)
		return reply + "对了，补上你刚才问的：" + answer
	}
	return reply
}

// OpenThreadRecovery pairs a planned recovery turn with the thread it
// closes, so the caller can address the thread after the delivery lands.
type OpenThreadRecovery struct {
	ThreadID string
	Response ProactiveResponse
}

// RecoverOpenThreads is the post-match recovery beat: it scans the user's
// open threads and plans at most maxThreadRecoveriesPerBeat proactive turns
// ("上一场你问谁助攻的——是法比安"). Only deterministic, ledger-grounded answers
// are emitted; threads without a grounded answer stay open for a later beat.
func (a *Agent) RecoverOpenThreads(ctx context.Context, userID, matchID string, now time.Time) ([]OpenThreadRecovery, error) {
	if a == nil || a.memories == nil {
		return nil, nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	threads, err := a.memories.Threads(ctx, userID)
	if err != nil {
		if !errors.Is(err, memory.ErrNotSupported) {
			return nil, err
		}
		return nil, nil
	}
	recoveries := make([]OpenThreadRecovery, 0, maxThreadRecoveriesPerBeat)
	for _, thread := range threads {
		if len(recoveries) >= maxThreadRecoveriesPerBeat {
			break
		}
		reply, eventIDs, ok := a.openThreadRecoveryReply(ctx, matchID, thread)
		if !ok {
			continue
		}
		traceID := stableTraceID(userID, matchID, "thread-recovery:"+thread.ID)
		trace := Trace{
			ID:             traceID,
			MatchID:        matchID,
			UserID:         userID,
			Input:          thread.Content,
			Intent:         IntentRecentEvent,
			RetrievedEvent: eventIDs,
			Reason:         "open_thread_recovery",
			CreatedAt:      now,
			ToolCalls: []ToolCall{
				{Name: "memory.recover_thread", Args: map[string]string{"threadId": thread.ID, "kind": string(thread.Kind)}},
			},
		}
		// 记忆进措辞层：回访文本仅在 recall 材料非空时带记忆重措辞，
		// guard 不过回罐头原文（evals 无记忆种子，走原文路径）。recall
		// 检索词取线程里的实体词（球员名优先）——contains 语义下整段
		// 提问几乎永远匹配不上。
		emitMode := "deterministic"
		focus := inferPlayer(thread.Content)
		if focus == "" {
			focus = thread.Content
		}
		if realized, done := a.realizeWithMemory(ctx, IntentRecentEvent, userID, thread.Content, focus, reply, nil, threadRecoveryDecision(), &trace); done {
			reply = realized
			emitMode = "realized"
		}
		trace.ToolCalls = append(trace.ToolCalls,
			ToolCall{Name: "response.emit_companion_reply", Args: map[string]string{"mode": emitMode, "source": "open_thread_recovery", "threadId": thread.ID}},
			ToolCall{Name: "trace.write_decision", Args: map[string]string{"matchId": matchID, "traceId": traceID}},
		)
		trace.Output = reply
		if err := a.tools.WriteTrace(ctx, trace); err != nil {
			return recoveries, err
		}
		recoveries = append(recoveries, OpenThreadRecovery{
			ThreadID: thread.ID,
			Response: ProactiveResponse{
				Reply:        reply,
				Trace:        trace,
				Presentation: threadRecoveryPresentation(thread.Kind),
			},
		})
	}
	return recoveries, nil
}

// openThreadRecoveryReply builds the deterministic recovery line for one
// thread kind. ok=false leaves the thread open (no reflexive chatter).
func (a *Agent) openThreadRecoveryReply(ctx context.Context, matchID string, thread memory.Thread) (string, []string, bool) {
	switch thread.Kind {
	case memory.ThreadUnansweredQuestion:
		if player := inferPlayer(thread.Content); player != "" {
			events, err := a.tools.EventsByPlayer(ctx, matchID, player, 8)
			if err != nil {
				return "", nil, false
			}
			answer, eventIDs := answerPlayerQuestion(thread.Content, player, events)
			if len(eventIDs) == 0 || replyLacksDefinitiveAnswer(answer) {
				return "", nil, false
			}
			return "上一场你问：" + thread.Content + "——现在补上：" + answer, eventIDs, true
		}
		// Generic event questions (谁助攻/进了没) answer from the recent-event
		// stream; anything else has no deterministic ledger answer and stays
		// open rather than getting reflexive chatter.
		if !containsAny(thread.Content, "助攻", "进球", "破门", "策动", "谁进", "谁主攻") {
			return "", nil, false
		}
		events, err := a.tools.RecentEvents(ctx, matchID, 8)
		if err != nil {
			return "", nil, false
		}
		answer, eventIDs := answerRecentEvent(thread.Content, events)
		if len(eventIDs) == 0 || replyLacksDefinitiveAnswer(answer) {
			return "", nil, false
		}
		return "上一场你问：" + thread.Content + "——现在补上：" + answer, eventIDs, true
	case memory.ThreadPromise:
		return "上次你说待会儿聊的，我记着呢——这会儿方便接着说吗？", nil, true
	case memory.ThreadPrediction:
		return "上回聊的那句「" + thread.Content + "」我还记着，结果一出我就跟你对答案。", nil, true
	case memory.ThreadEmotionalMoment:
		return "上次那会儿你情绪挺激动的，这会儿缓过来一些没？", nil, true
	default:
		return "", nil, false
	}
}

// threadRecoveryPresentation keeps recovery turns visibly calm; no match
// affect is fabricated for them.
func threadRecoveryPresentation(kind memory.ThreadKind) relationship.PresentationPlan {
	plan := relationship.PresentationPlan{Expression: "focus", Motion: "speak", VoiceStyle: "calm", VoiceEnergy: 0.55, VoiceSpeed: 1, HoldMS: 1200, ReturnMode: "watching"}
	if kind == memory.ThreadEmotionalMoment {
		plan.VoiceStyle = "soft"
		plan.VoiceEnergy = 0.45
	}
	return plan
}

// AddressOpenThread closes one open thread as answered; called after the
// recovery delivery actually landed so failed deliveries leave the loop open.
func (a *Agent) AddressOpenThread(ctx context.Context, threadID string) error {
	store := a.threadStore()
	if store == nil {
		return nil
	}
	addressCtx, cancel := context.WithTimeout(ctx, memoryObserveTimeout)
	defer cancel()
	return store.MarkThreadAddressed(addressCtx, threadID)
}

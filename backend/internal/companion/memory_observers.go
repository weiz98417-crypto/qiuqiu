package companion

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"qiuqiu/internal/memory"
	"qiuqiu/internal/relationship"
)

// Bounds for the memory seam: Observe is an in-memory enqueue and Recall is a
// profile fetch — neither may eat into the turn or realize budget.
const (
	memoryObserveTimeout = 200 * time.Millisecond
	memoryRecallTimeout  = 300 * time.Millisecond
	memoryRecallLimit    = 5
)

// observeTurnMemory runs after the turn has been appended to the Interaction
// Ledger; failures are swallowed because memory must never fail a turn.
// (Open-thread candidates are written inside the turn handler itself so the
// append stays on the trace — see thread_observers.go.)
func (a *Agent) observeTurnMemory(ctx context.Context, msg MessageRequest, response Response) {
	if a == nil || a.memories == nil {
		return
	}
	for _, moment := range userTurnMemoryMoments(msg, response) {
		a.observeMemory(ctx, moment)
	}
}

func (a *Agent) observeMatchEventMemory(ctx context.Context, req MatchEventRequest, decision relationship.Decision) {
	if a == nil || a.memories == nil {
		return
	}
	if moment, ok := matchEventMemoryMoment(req); ok {
		a.observeMemory(ctx, moment)
	}
}

func (a *Agent) observeMemory(ctx context.Context, moment memory.Moment) {
	observeCtx, cancel := context.WithTimeout(ctx, memoryObserveTimeout)
	defer cancel()
	_ = a.memories.Observe(observeCtx, moment)
}

// recallMemoryBlock fetches the Recall top-k as a bounded provenance-cited
// block for realization context. The deterministic conversation.read_recent
// sites (IntentFollowUp answering, phrase cooldown) stay untouched and act as
// the fallback whenever this returns "" — no memories wired, degraded adapter,
// or empty recall.
func (a *Agent) recallMemoryBlock(ctx context.Context, userID, focus string, trace *Trace) string {
	if a == nil || a.memories == nil {
		return ""
	}
	recallCtx, cancel := context.WithTimeout(ctx, memoryRecallTimeout)
	defer cancel()
	recalls := a.memories.Recall(recallCtx, memory.Query{UserID: userID, Focus: focus, Limit: memoryRecallLimit})
	if trace != nil {
		trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "memory.recall", Args: map[string]string{
			"userId": userID, "limit": strconv.Itoa(memoryRecallLimit), "recalled": strconv.Itoa(len(recalls)),
		}})
	}
	return memory.RenderRecallBlock(recalls)
}

// userTurnMemoryMoments derives observations from a finished turn with the
// policy-table cue vocabulary (relationship.InferUserCues), so the memory seam
// classifies user text with the exact phrases the decision kernel acted on.
func userTurnMemoryMoments(msg MessageRequest, response Response) []memory.Moment {
	if msg.FactRefresh != "" {
		return nil
	}
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return nil
	}
	stage := memoryStageFor(response)
	occurred := msg.Now
	if occurred.IsZero() {
		occurred = time.Now().UTC()
	}
	var moments []memory.Moment
	for _, cue := range relationship.InferUserCues(text) {
		if cue.Kind == relationship.CueStablePreference || cue.Kind == relationship.CueBanterDenied {
			moments = append(moments, memory.Moment{
				UserID:     msg.UserID,
				Kind:       memory.MomentUserFact,
				Content:    "用户明确表达偏好或边界：" + text,
				Importance: memory.ScoreImportance(memory.MomentUserFact, text, stage),
				OccurredAt: occurred,
			})
			break
		}
	}
	if containsAny(text, memory.PromiseMarkers...) {
		moments = append(moments, memory.Moment{
			UserID:     msg.UserID,
			Kind:       memory.MomentPromise,
			Content:    "用户留下了待跟进的约定：" + text,
			Importance: memory.ScoreImportance(memory.MomentPromise, text, stage),
			OccurredAt: occurred,
		})
	}
	if response.Intent == IntentEmotionReaction || containsAny(text, memory.EmotionMarkers...) {
		moments = append(moments, memory.Moment{
			UserID:     msg.UserID,
			Kind:       memory.MomentEmotionalExchange,
			Content:    "用户情绪交流：" + text,
			Importance: memory.ScoreImportance(memory.MomentEmotionalExchange, text, stage),
			OccurredAt: occurred,
		})
	}
	return moments
}

// matchEventMemoryMoment observes critical match drama (goal / red card / VAR
// / yellow card) involving the user's match; RecordedSequence cites the fact
// ledger for provenance.
func matchEventMemoryMoment(req MatchEventRequest) (memory.Moment, bool) {
	event := req.Event
	if strings.TrimSpace(event.ID) == "" {
		return memory.Moment{}, false
	}
	label, ok := memoryEventTypeLabel(event.EventType)
	if !ok {
		return memory.Moment{}, false
	}
	occurred := req.Now
	if occurred.IsZero() {
		occurred = time.Now().UTC()
	}
	content := fmt.Sprintf("比赛事件[%s]：%s", label, strings.TrimSpace(event.Description))
	if event.TeamName != "" && req.Snapshot.HomeTeam != "" {
		content = fmt.Sprintf("比赛事件[%s]：%s，%s（%s 对阵 %s）", label, event.TeamName, strings.TrimSpace(event.Description), req.Snapshot.HomeTeam, req.Snapshot.AwayTeam)
	}
	return memory.Moment{
		UserID:         req.UserID,
		Kind:           memory.MomentMatchEvent,
		Content:        content,
		Importance:     memory.ScoreImportance(memory.MomentMatchEvent, content, memory.StageNone),
		LedgerSequence: event.RecordedSequence,
		OccurredAt:     occurred,
	}, true
}

func memoryEventTypeLabel(eventType string) (string, bool) {
	switch eventType {
	case "goal", "penalty", "penalty_awarded":
		return "进球", true
	case "red_card":
		return "红牌", true
	case "var_check", "var_result", "goal_cancelled":
		return "VAR", true
	case "yellow_card":
		return "黄牌", true
	default:
		return "", false
	}
}

func memoryStageFor(response Response) memory.Stage {
	if response.Trace.RelationshipDecision == nil {
		return memory.StageNone
	}
	switch response.Trace.RelationshipDecision.Relationship.Stage {
	case relationship.StageFirstMeeting:
		return memory.StageFirstMeeting
	case relationship.StageFamiliar:
		return memory.StageFamiliar
	case relationship.StageWatchBuddy:
		return memory.StageWatchBuddy
	case relationship.StageOldBallmate:
		return memory.StageOldBallmate
	default:
		return memory.StageNone
	}
}

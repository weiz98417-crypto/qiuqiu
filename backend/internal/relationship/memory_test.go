package relationship

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestExplicitTasteMemoryIsAvailableAcrossMatches(t *testing.T) {
	director := NewDirector(NewMemoryRepository())
	ctx := context.Background()
	now := time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC)

	if _, err := director.Apply(ctx, Signal{
		ID: "taste-match-1", Kind: SignalUserTurn, UserID: "user-1", MatchID: "match-1", OccurredAt: now,
		User: &UserSignal{Text: "我更吃高位压迫这一套"},
	}); err != nil {
		t.Fatalf("record preference: %v", err)
	}

	decision, err := director.Apply(ctx, Signal{
		ID: "taste-match-2", Kind: SignalUserTurn, UserID: "user-1", MatchID: "match-2", OccurredAt: now.Add(7 * 24 * time.Hour),
		User: &UserSignal{Text: "这场的高位压迫你怎么看？"},
	})
	if err != nil {
		t.Fatalf("recall preference: %v", err)
	}
	if len(decision.Memories) != 1 {
		t.Fatalf("memories = %+v, want one relevant cross-match memory", decision.Memories)
	}
	memory := decision.Memories[0]
	if memory.Kind != MemoryKindTasteEvidence || memory.MatchID != "match-1" {
		t.Fatalf("memory = %+v", memory)
	}
	var payload TasteMemoryPayload
	if err := json.Unmarshal(memory.Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.Subject != "高位压迫" || payload.Direction != "like" {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestBanterBoundaryAndRepairMemoryChangeLaterMatches(t *testing.T) {
	repository := NewMemoryRepository()
	director := NewDirector(repository)
	ctx := context.Background()
	now := time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC)

	feedback, err := director.Apply(ctx, Signal{
		ID: "banter-feedback", Kind: SignalUserTurn, UserID: "user-1", MatchID: "match-1", OccurredAt: now,
		User: &UserSignal{Text: "别拿这个开我玩笑了，烦", Cues: []UserCue{{Kind: CueBanterDenied, Scope: "prediction"}}},
	})
	if err != nil {
		t.Fatalf("record feedback: %v", err)
	}
	if !feedback.Relationship.RepairActive || feedback.Relationship.BoundaryCount != 1 {
		t.Fatalf("feedback relationship = %+v", feedback.Relationship)
	}

	state, err := repository.Load(ctx, "user-1", "match-2")
	if err != nil {
		t.Fatalf("load relationship memory: %v", err)
	}
	if !hasMemoryKind(state.Memories, MemoryKindBoundary) || !hasMemoryKind(state.Memories, MemoryKindProcedure) {
		t.Fatalf("memories = %+v, want boundary and procedure evidence", state.Memories)
	}

	followThrough, err := director.Apply(ctx, Signal{
		ID: "repair-match-2", Kind: SignalUserTurn, UserID: "user-1", MatchID: "match-2", OccurredAt: now.Add(7 * 24 * time.Hour),
		User: &UserSignal{Text: "他们为什么右路一直被打穿？"},
	})
	if err != nil {
		t.Fatalf("follow through: %v", err)
	}
	if !followThrough.Relationship.RepairActive {
		t.Fatal("repair must remain active across matches")
	}
	if followThrough.Speech == nil || followThrough.Speech.Content.AnalysisDepth != "none" || followThrough.Speech.Content.QuestionAllowed {
		t.Fatalf("follow-through speech = %+v", followThrough.Speech)
	}
	if hasAction(followThrough.Actions, ActAnalyze) || hasAction(followThrough.Actions, ActTease) || hasAction(followThrough.Actions, ActAsk) {
		t.Fatalf("follow-through actions = %v", followThrough.Actions)
	}
}

func TestOpenThreadRemainsActiveUntilRecalledReplyIsDelivered(t *testing.T) {
	repository := NewMemoryRepository()
	director := NewDirector(repository)
	ctx := context.Background()
	now := time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC)

	if _, err := director.Apply(ctx, Signal{
		ID: "thread-create", Kind: SignalUserTurn, UserID: "user-1", MatchID: "match-1", OccurredAt: now,
		User: &UserSignal{Text: "高位压迫这个问题，下场接着聊"},
	}); err != nil {
		t.Fatalf("create open thread: %v", err)
	}

	decision, err := director.Apply(ctx, Signal{
		ID: "thread-recall", Kind: SignalUserTurn, UserID: "user-1", MatchID: "match-2", OccurredAt: now.Add(7 * 24 * time.Hour),
		User: &UserSignal{Text: "接着上次说高位压迫"},
	})
	if err != nil {
		t.Fatalf("recall open thread: %v", err)
	}
	if !hasAction(decision.Actions, ActRecall) || len(decision.Memories) != 1 || decision.Memories[0].Kind != MemoryKindOpenThread {
		t.Fatalf("decision = %+v", decision)
	}
	var payload OpenThreadMemoryPayload
	if err := json.Unmarshal(decision.Memories[0].Payload, &payload); err != nil {
		t.Fatalf("decode open thread: %v", err)
	}
	if payload.Topic != "高位压迫这个问题" {
		t.Fatalf("topic = %q", payload.Topic)
	}

	state, err := repository.Load(ctx, "user-1", "match-3")
	if err != nil {
		t.Fatalf("load recalled thread: %v", err)
	}
	if memoryStatus(state.Memories, decision.Memories[0].ID) != "active" {
		t.Fatalf("memories = %+v, want recalled thread active before delivery", state.Memories)
	}
	if !containsString(state.Memories[0].PendingDecisionIDs, decision.ID) {
		t.Fatalf("memories = %+v, want pending decision %q", state.Memories, decision.ID)
	}
}

func TestDeliveredReplyResolvesTheOpenThreadItActuallyUsed(t *testing.T) {
	repository := NewMemoryRepository()
	director := NewDirector(repository)
	ctx := context.Background()
	now := time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC)

	if _, err := director.Apply(ctx, Signal{
		ID: "thread-create", Kind: SignalUserTurn, UserID: "user-1", MatchID: "match-1", OccurredAt: now,
		User: &UserSignal{Text: "高位压迫这个问题，下场接着聊"},
	}); err != nil {
		t.Fatalf("create open thread: %v", err)
	}
	recall, err := director.Apply(ctx, Signal{
		ID: "thread-recall", Kind: SignalUserTurn, UserID: "user-1", MatchID: "match-2", OccurredAt: now.Add(7 * 24 * time.Hour),
		User: &UserSignal{Text: "接着上次说高位压迫"},
	})
	if err != nil {
		t.Fatalf("recall open thread: %v", err)
	}
	memoryID := recall.Memories[0].ID
	if _, err := director.Apply(ctx, Signal{
		ID: "delivery-thread-recall", Kind: SignalDeliveryResult, UserID: "user-1", MatchID: "match-2", OccurredAt: now.Add(7*24*time.Hour + time.Second),
		Delivery: &DeliverySignal{DecisionID: recall.ID, State: "text_delivered", UsedMemoryIDs: []string{memoryID}},
	}); err != nil {
		t.Fatalf("record delivery: %v", err)
	}

	state, err := repository.Load(ctx, "user-1", "match-3")
	if err != nil {
		t.Fatalf("load delivered thread: %v", err)
	}
	if memoryStatus(state.Memories, memoryID) != "resolved" {
		t.Fatalf("memories = %+v, want delivered thread resolved", state.Memories)
	}
	if len(state.Memories[0].PendingDecisionIDs) != 0 {
		t.Fatalf("memories = %+v, want no pending decision after delivery", state.Memories)
	}
}

func TestIncompleteDeliveryKeepsUsedOpenThreadActive(t *testing.T) {
	for _, deliveryState := range []string{"failed", "interrupted", "skipped"} {
		t.Run(deliveryState, func(t *testing.T) {
			repository := NewMemoryRepository()
			director := NewDirector(repository)
			ctx := context.Background()
			now := time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC)

			if _, err := director.Apply(ctx, Signal{
				ID: "thread-create", Kind: SignalUserTurn, UserID: "user-1", MatchID: "match-1", OccurredAt: now,
				User: &UserSignal{Text: "高位压迫这个问题，下场接着聊"},
			}); err != nil {
				t.Fatalf("create open thread: %v", err)
			}
			recall, err := director.Apply(ctx, Signal{
				ID: "thread-recall", Kind: SignalUserTurn, UserID: "user-1", MatchID: "match-2", OccurredAt: now.Add(7 * 24 * time.Hour),
				User: &UserSignal{Text: "接着上次说高位压迫"},
			})
			if err != nil {
				t.Fatalf("recall open thread: %v", err)
			}
			memoryID := recall.Memories[0].ID
			if _, err := director.Apply(ctx, Signal{
				ID: "delivery-" + deliveryState, Kind: SignalDeliveryResult, UserID: "user-1", MatchID: "match-2", OccurredAt: now.Add(7*24*time.Hour + time.Second),
				Delivery: &DeliverySignal{DecisionID: recall.ID, State: deliveryState, UsedMemoryIDs: []string{memoryID}},
			}); err != nil {
				t.Fatalf("record %s delivery: %v", deliveryState, err)
			}

			state, err := repository.Load(ctx, "user-1", "match-3")
			if err != nil {
				t.Fatalf("load thread: %v", err)
			}
			if memoryStatus(state.Memories, memoryID) != "active" {
				t.Fatalf("memories = %+v, want thread active after %s delivery", state.Memories, deliveryState)
			}
			if len(state.Memories[0].PendingDecisionIDs) != 0 {
				t.Fatalf("memories = %+v, want pending decision cleared after %s delivery", state.Memories, deliveryState)
			}
		})
	}
}

func TestDeliveredReplyThatDidNotUseOpenThreadKeepsItActive(t *testing.T) {
	repository := NewMemoryRepository()
	director := NewDirector(repository)
	ctx := context.Background()
	now := time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC)

	if _, err := director.Apply(ctx, Signal{
		ID: "thread-create", Kind: SignalUserTurn, UserID: "user-1", MatchID: "match-1", OccurredAt: now,
		User: &UserSignal{Text: "高位压迫这个问题，下场接着聊"},
	}); err != nil {
		t.Fatalf("create open thread: %v", err)
	}
	recall, err := director.Apply(ctx, Signal{
		ID: "thread-recall", Kind: SignalUserTurn, UserID: "user-1", MatchID: "match-2", OccurredAt: now.Add(7 * 24 * time.Hour),
		User: &UserSignal{Text: "接着上次说高位压迫"},
	})
	if err != nil {
		t.Fatalf("recall open thread: %v", err)
	}
	memoryID := recall.Memories[0].ID
	if _, err := director.Apply(ctx, Signal{
		ID: "delivery-without-memory", Kind: SignalDeliveryResult, UserID: "user-1", MatchID: "match-2", OccurredAt: now.Add(7*24*time.Hour + time.Second),
		Delivery: &DeliverySignal{DecisionID: recall.ID, State: "text_delivered"},
	}); err != nil {
		t.Fatalf("record delivery: %v", err)
	}

	state, err := repository.Load(ctx, "user-1", "match-3")
	if err != nil {
		t.Fatalf("load thread: %v", err)
	}
	if memoryStatus(state.Memories, memoryID) != "active" || len(state.Memories[0].PendingDecisionIDs) != 0 {
		t.Fatalf("memories = %+v, want active thread without pending decision", state.Memories)
	}
}

func TestCancelledGoalRevokesOldSharedMomentBeforeCrossMatchRecall(t *testing.T) {
	repository := NewMemoryRepository()
	director := NewDirector(repository)
	ctx := context.Background()
	now := time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC)

	if _, err := director.Apply(ctx, Signal{
		ID: "moment-goal", Kind: SignalMatchEvent, UserID: "user-1", MatchID: "match-1", OccurredAt: now,
		Match: &MatchSignal{
			EventID: "goal-1", EventType: "goal", Intensity: 5, Confirmed: true, Critical: true, OutputAllowed: true,
			Description: "萨拉赫绝杀破门", PlayerName: "萨拉赫",
		},
	}); err != nil {
		t.Fatalf("record goal moment: %v", err)
	}
	if _, err := director.Apply(ctx, Signal{
		ID: "moment-cancelled", Kind: SignalMatchEvent, UserID: "user-1", MatchID: "match-1", OccurredAt: now.Add(time.Minute),
		Match: &MatchSignal{
			EventID: "cancel-1", EventType: "goal_cancelled", Intensity: 5, Confirmed: true, Critical: true, OutputAllowed: true,
			Description: "VAR确认越位，进球取消", RevisionOf: "goal-1",
		},
	}); err != nil {
		t.Fatalf("record cancellation moment: %v", err)
	}

	decision, err := director.Apply(ctx, Signal{
		ID: "moment-recall", Kind: SignalUserTurn, UserID: "user-1", MatchID: "match-2", OccurredAt: now.Add(14 * 24 * time.Hour),
		User: &UserSignal{Text: "又想起上次那个被吹掉的绝杀", Cues: []UserCue{{Kind: CueSharedMomentRecalled}}},
	})
	if err != nil {
		t.Fatalf("recall shared moment: %v", err)
	}
	if !hasAction(decision.Actions, ActRecall) || len(decision.Memories) != 1 {
		t.Fatalf("decision = %+v", decision)
	}
	var payload SharedMomentMemoryPayload
	if err := json.Unmarshal(decision.Memories[0].Payload, &payload); err != nil {
		t.Fatalf("decode shared moment: %v", err)
	}
	if payload.EventType != "goal_cancelled" || payload.RevisionOf != "goal-1" {
		t.Fatalf("payload = %+v", payload)
	}
	state, err := repository.Load(ctx, "user-1", "match-3")
	if err != nil {
		t.Fatalf("load shared moments: %v", err)
	}
	if sharedMomentStatus(state.Memories, "goal-1") != "revoked" || sharedMomentStatus(state.Memories, "cancel-1") != "active" {
		t.Fatalf("memories = %+v", state.Memories)
	}
}

func TestUnconfirmedCriticalEventDoesNotBecomeSharedMoment(t *testing.T) {
	repository := NewMemoryRepository()
	director := NewDirector(repository)
	ctx := context.Background()
	now := time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC)

	if _, err := director.Apply(ctx, Signal{
		ID: "goal-pending-var", Kind: SignalMatchEvent, UserID: "user-1", MatchID: "match-1", OccurredAt: now,
		Match: &MatchSignal{
			EventID: "goal-1", EventType: "goal", Intensity: 5, Critical: true, OutputAllowed: true,
			Description: "萨拉赫破门，等待VAR确认", PlayerName: "萨拉赫", Confirmed: false,
		},
	}); err != nil {
		t.Fatalf("observe pending goal: %v", err)
	}
	state, err := repository.Load(ctx, "user-1", "match-2")
	if err != nil {
		t.Fatalf("load memories: %v", err)
	}
	if hasMemoryKind(state.Memories, MemoryKindSharedMoment) {
		t.Fatalf("memories = %+v, unconfirmed event must not become a shared moment", state.Memories)
	}
}

func TestPreferenceAndBoundaryAcrossTwoMatchesReachFamiliarStage(t *testing.T) {
	director := NewDirector(NewMemoryRepository())
	ctx := context.Background()
	now := time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC)
	for _, signal := range []Signal{
		{ID: "evidence-open-1", Kind: SignalSessionOpened, UserID: "user-1", MatchID: "match-1", OccurredAt: now},
		{ID: "evidence-taste", Kind: SignalUserTurn, UserID: "user-1", MatchID: "match-1", OccurredAt: now.Add(time.Minute), User: &UserSignal{Text: "我更吃高位压迫这一套"}},
		{ID: "evidence-open-2", Kind: SignalSessionOpened, UserID: "user-1", MatchID: "match-2", OccurredAt: now.Add(7 * 24 * time.Hour)},
	} {
		if _, err := director.Apply(ctx, signal); err != nil {
			t.Fatalf("setup %s: %v", signal.ID, err)
		}
	}
	decision, err := director.Apply(ctx, Signal{
		ID: "evidence-boundary", Kind: SignalUserTurn, UserID: "user-1", MatchID: "match-2", OccurredAt: now.Add(7*24*time.Hour + time.Minute),
		User: &UserSignal{Text: "别问工作细节"},
	})
	if err != nil {
		t.Fatalf("record boundary: %v", err)
	}
	if decision.Relationship.Stage != StageFamiliar {
		t.Fatalf("stage = %q, want %q", decision.Relationship.Stage, StageFamiliar)
	}
}

func hasMemoryKind(memories []RelationshipMemory, kind string) bool {
	for _, memory := range memories {
		if memory.Kind == kind && memory.Status == "active" {
			return true
		}
	}
	return false
}

func memoryStatus(memories []RelationshipMemory, id string) string {
	for _, memory := range memories {
		if memory.ID == id {
			return memory.Status
		}
	}
	return ""
}

func sharedMomentStatus(memories []RelationshipMemory, eventID string) string {
	for _, memory := range memories {
		if memory.Kind != MemoryKindSharedMoment {
			continue
		}
		var payload SharedMomentMemoryPayload
		if json.Unmarshal(memory.Payload, &payload) == nil && payload.EventID == eventID {
			return memory.Status
		}
	}
	return ""
}

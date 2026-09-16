package companion

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/memory"
)

type failingMemories struct{}

func (failingMemories) Observe(context.Context, memory.Moment) error {
	return errors.New("memobase unreachable")
}

func (failingMemories) Recall(context.Context, memory.Query) []memory.Recall {
	return nil
}

func (failingMemories) Portrait(context.Context, string) (memory.Portrait, error) {
	return memory.Portrait{}, errors.New("memobase unreachable")
}

func (failingMemories) Threads(context.Context, string) ([]memory.Thread, error) {
	return nil, memory.ErrNotSupported
}

func TestUserTurnMemoryMomentsDerivation(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	msg := MessageRequest{SignalID: "signal-1", MatchID: "match-1", UserID: "user-1", Text: "我喜欢皇马，待会儿告诉你为什么", Now: now}
	moments := userTurnMemoryMoments(msg, Response{})
	if len(moments) != 2 {
		t.Fatalf("moments = %+v, want one user fact and one promise", moments)
	}
	if moments[0].Kind != memory.MomentUserFact || moments[1].Kind != memory.MomentPromise {
		t.Fatalf("moment kinds = %s, %s; want user_fact then promise", moments[0].Kind, moments[1].Kind)
	}
	if !moments[0].OccurredAt.Equal(now) || moments[0].UserID != "user-1" {
		t.Fatalf("moment provenance = %+v, want user and occurrence time preserved", moments[0])
	}
	if moments[0].Importance < 0.7 || moments[1].Importance < 0.8 {
		t.Fatalf("importance scores = %v, %v; want fact and promise above their class floors", moments[0].Importance, moments[1].Importance)
	}
	if refreshed := userTurnMemoryMoments(MessageRequest{Text: "我喜欢皇马", FactRefresh: "goal-1:1:confirmed", UserID: "user-1"}, Response{}); refreshed != nil {
		t.Fatalf("fact refresh turns must not be re-observed: %+v", refreshed)
	}
	if routine := userTurnMemoryMoments(MessageRequest{Text: "在吗", UserID: "user-1"}, Response{}); routine != nil {
		t.Fatalf("routine turns must not produce moments: %+v", routine)
	}
}

func TestMatchEventMemoryMomentCitesFactLedgerSequence(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	req := MatchEventRequest{
		UserID: "user-1",
		Event: matchstate.MatchEvent{
			ID:              "goal-1",
			EventType:       "goal",
			TeamName:        "皇马",
			Description:     "维尼修斯破门",
			RecordedSequence: 42,
		},
		Snapshot: matchstate.Snapshot{HomeTeam: "皇马", AwayTeam: "巴萨"},
		Now:      now,
	}
	moment, ok := matchEventMemoryMoment(req)
	if !ok {
		t.Fatal("goal events must produce a match moment")
	}
	if moment.Kind != memory.MomentMatchEvent || moment.LedgerSequence != 42 {
		t.Fatalf("moment = %+v, want match_event citing the fact ledger sequence", moment)
	}
	if !strings.Contains(moment.Content, "进球") {
		t.Fatalf("moment content = %q, want the Chinese event class token for the heuristic", moment.Content)
	}
	if moment.Importance < 0.85 {
		t.Fatalf("goal moment importance = %v, want the goal class weight", moment.Importance)
	}
	if _, ok := matchEventMemoryMoment(MatchEventRequest{UserID: "user-1", Event: matchstate.MatchEvent{ID: "throw-1", EventType: "throw_in"}}); ok {
		t.Fatal("routine match events must not produce moments")
	}
}

func TestAgentPlanSurvivesMemoryFailure(t *testing.T) {
	agent := NewAgent(NewRepositoryMemoryTools(matchstate.NewStore())).WithMemories(failingMemories{})
	plan, err := agent.Plan(context.Background(), TurnInput{
		Kind: TurnKindUser,
		Message: &MessageRequest{SignalID: "signal-mem-down", MatchID: "match-1", UserID: "user-1", Text: "在吗", Now: time.Now().UTC()},
	})
	if err != nil {
		t.Fatalf("Plan with failing memory seam = %v; memory errors must never fail the turn", err)
	}
	if plan.Trace.ID == "" {
		t.Fatalf("plan = %+v, want a normal turn", plan)
	}
}

func TestAgentPlanEnqueuesTurnMoments(t *testing.T) {
	fake := memory.NewFake()
	agent := NewAgent(NewRepositoryMemoryTools(matchstate.NewStore())).WithMemories(fake)
	if _, err := agent.Plan(context.Background(), TurnInput{
		Kind: TurnKindUser,
		Message: &MessageRequest{SignalID: "signal-mem", MatchID: "match-1", UserID: "user-1", Text: "我喜欢皇马，待会儿告诉你为什么", Now: time.Now().UTC()},
	}); err != nil {
		t.Fatalf("Plan: %v", err)
	}
	observed := fake.Moments()
	if len(observed) == 0 {
		t.Fatal("turn pipeline should enqueue memory moments after the ledger append")
	}
	hasFact := false
	for _, moment := range observed {
		if moment.Kind == memory.MomentUserFact && strings.Contains(moment.Content, "我喜欢皇马") {
			hasFact = true
		}
	}
	if !hasFact {
		t.Fatalf("observed moments = %+v, want the user fact moment", observed)
	}
}

func TestRecallMemoryBlockIsProvenanceCitedAndFallsBackEmpty(t *testing.T) {
	ctx := context.Background()
	fake := memory.NewFake()
	if err := fake.Observe(ctx, memory.Moment{UserID: "user-1", Kind: memory.MomentUserFact, Content: "我喜欢皇马", Importance: 0.8, OccurredAt: time.Now().UTC()}); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	agent := NewAgent(NewRepositoryMemoryTools(matchstate.NewStore())).WithMemories(fake)
	block := agent.recallMemoryBlock(ctx, "user-1", "皇马", nil)
	if !strings.Contains(block, "我喜欢皇马") {
		t.Fatalf("recall block = %q, want the recalled memory", block)
	}
	if !strings.Contains(block, "fake://moment/user-1/user_fact") {
		t.Fatalf("recall block = %q, want per-line provenance citations", block)
	}
	if len([]rune(block)) > 480 {
		t.Fatalf("recall block is %d runes, want it bounded", len([]rune(block)))
	}

	fallback := NewAgent(NewRepositoryMemoryTools(matchstate.NewStore()))
	if block := fallback.recallMemoryBlock(ctx, "user-1", "皇马", nil); block != "" {
		t.Fatalf("recall block without a memory seam = %q, want empty (read_recent stays the fallback)", block)
	}
}

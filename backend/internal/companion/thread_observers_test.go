package companion

import (
	"context"
	"strings"
	"testing"
	"time"

	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/memory"
	"qiuqiu/internal/relationship"
)

func TestUserTurnThreadCandidatesDerivation(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	unanswered := userTurnThreadCandidates("user-1", "signal-1", "穆西亚拉进球了吗？", IntentPlayerQuestion, "我这边目前没有看到穆西亚拉的进球记录。", now)
	if len(unanswered) != 1 || unanswered[0].Kind != memory.ThreadUnansweredQuestion {
		t.Fatalf("candidates = %+v, want the unanswered question", unanswered)
	}
	if unanswered[0].Content != "穆西亚拉进球了吗？" || unanswered[0].SourceTurn != "signal-1" {
		t.Fatalf("candidate = %+v, want raw question content and the source turn", unanswered[0])
	}
	if answered := userTurnThreadCandidates("user-1", "signal-2", "刚才谁助攻？", IntentRecentEvent, "刚才这球是法比安、助攻，亚马尔、参与策动。", now); len(answered) != 0 {
		t.Fatalf("candidates = %+v, definitively answered questions must not open threads", answered)
	}
	if unknown := userTurnThreadCandidates("user-1", "signal-3", "帮我分析一下草皮对节奏的影响？", IntentUnknown, "这句我没接明白，你换个说法？", now); len(unknown) != 1 || unknown[0].Kind != memory.ThreadUnansweredQuestion {
		t.Fatalf("candidates = %+v, unknown intent with a question must open a thread", unknown)
	}
	if promises := userTurnThreadCandidates("user-1", "signal-4", "待会儿告诉你为什么", IntentSmalltalk, "好。", now); len(promises) != 1 || promises[0].Kind != memory.ThreadPromise {
		t.Fatalf("candidates = %+v, want the promise thread", promises)
	}
	if emotional := userTurnThreadCandidates("user-1", "signal-5", "绝了，破防了", IntentEmotionReaction, "这一下有点东西。", now); len(emotional) != 1 || emotional[0].Kind != memory.ThreadEmotionalMoment {
		t.Fatalf("candidates = %+v, want the emotional moment thread", emotional)
	}
	if blank := userTurnThreadCandidates("user-1", "signal-6", "  ", IntentUnknown, "", now); blank != nil {
		t.Fatalf("candidates = %+v, blank turns never open threads", blank)
	}
}

func TestMatchEventThreadCandidateRequiresPredictionBanter(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	req := MatchEventRequest{
		UserID: "user-1",
		Event: matchstate.MatchEvent{
			ID:               "goal-1",
			EventType:        "goal",
			Clock:            "24:10",
			Description:      "佩德里禁区前沿推射破门。",
			RecordedSequence: 42,
		},
		Now: now,
	}
	if _, ok := matchEventThreadCandidate(req, relationship.Decision{}); ok {
		t.Fatal("score drama without the prediction banter domain must not open a thread")
	}
	allowed := relationship.Decision{Relationship: relationship.RelationshipView{PredictionBanter: true}}
	thread, ok := matchEventThreadCandidate(req, allowed)
	if !ok {
		t.Fatal("prediction banter domain open must link the prediction thread")
	}
	if thread.Kind != memory.ThreadPrediction || thread.LedgerSequence != 42 || thread.SourceTurn != "goal-1" {
		t.Fatalf("thread = %+v, want prediction kind with ledger provenance", thread)
	}
	if !strings.Contains(thread.Content, "24:10") || !strings.Contains(thread.Content, "佩德里") {
		t.Fatalf("thread content = %q, want the prediction snapshot", thread.Content)
	}
	if _, ok := matchEventThreadCandidate(MatchEventRequest{Event: matchstate.MatchEvent{ID: "throw-1", EventType: "throw_in"}}, allowed); ok {
		t.Fatal("routine events must not open threads")
	}
}

func TestTurnPipelineAppendsThreadsWithTraceAudit(t *testing.T) {
	ctx := context.Background()
	fake := memory.NewFake()
	agent := NewAgent(NewRepositoryMemoryTools(matchstate.NewStore())).WithMemories(fake)
	plan, err := agent.Plan(ctx, TurnInput{
		Kind: TurnKindUser,
		Message: &MessageRequest{SignalID: "signal-thread", MatchID: "match-1", UserID: "user-1", Text: "穆西亚拉进球了吗？", Now: time.Now().UTC()},
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	open, err := fake.OpenThreads(ctx, "user-1")
	if err != nil || len(open) != 1 || open[0].Kind != memory.ThreadUnansweredQuestion {
		t.Fatalf("open threads = %+v err=%v, want the unanswered question on the ledger", open, err)
	}
	called := false
	for _, call := range plan.Trace.ToolCalls {
		if call.Name == "memory.append_thread" && call.Args["kind"] == string(memory.ThreadUnansweredQuestion) && call.Args["threadId"] == open[0].ID {
			called = true
		}
	}
	if !called {
		t.Fatalf("trace tool calls = %+v, want the memory.append_thread audit", plan.Trace.ToolCalls)
	}
}

func TestInTurnRecoveryClosesUnansweredQuestion(t *testing.T) {
	ctx := context.Background()
	store := matchstate.NewStore()
	tools := NewStoreMemoryTools(store)
	fake := memory.NewFake()
	agent := NewAgent(tools).WithMemories(fake)
	matchID := "thread-recovery-in-turn"
	if _, _, err := store.SetConfig(matchID, matchstate.MatchConfig{HomeTeam: "西班牙", AwayTeam: "德国"}); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	if _, _, err := store.Create(matchID, matchstate.MatchEvent{
		EventType: "goal", Period: "first_half", Clock: "24:10", TeamID: "away", TeamName: "德国",
		PlayerName: "穆西亚拉", Score: matchstate.Score{Home: 0, Away: 1},
		Description: "穆西亚拉禁区前沿推射破门。",
		Participants: []matchstate.Participant{{Role: "scorer", Name: "穆西亚拉", TeamName: "德国"}},
	}); err != nil {
		t.Fatalf("Create goal: %v", err)
	}
	if _, err := fake.AppendThread(ctx, memory.Thread{UserID: "user-1", Kind: memory.ThreadUnansweredQuestion, Content: "穆西亚拉进球了吗？"}); err != nil {
		t.Fatalf("AppendThread: %v", err)
	}

	response, err := agent.HandleMessage(ctx, MessageRequest{MatchID: matchID, UserID: "user-1", Text: "哈哈，太牛了", Now: fixedTime()})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if !strings.Contains(response.Reply, "补上") || !strings.Contains(response.Reply, "穆西亚拉") || !strings.Contains(response.Reply, "进球记录") {
		t.Fatalf("reply = %q, want the recovered answer appended", response.Reply)
	}
	assertToolCalled(t, response.Trace, "memory.recover_thread")
	open, err := fake.OpenThreads(ctx, "user-1")
	if err != nil {
		t.Fatalf("OpenThreads after recovery: %v", err)
	}
	for _, thread := range open {
		if thread.Kind == memory.ThreadUnansweredQuestion {
			t.Fatalf("open threads = %+v, want the unanswered question addressed", open)
		}
	}
}

func TestRecoverOpenThreadsAnswersFromLedgerAndLeavesUnanswerableOpen(t *testing.T) {
	ctx := context.Background()
	store := matchstate.NewStore()
	tools := NewStoreMemoryTools(store)
	fake := memory.NewFake()
	agent := NewAgent(tools).WithMemories(fake)
	matchID := "thread-recovery-beat"
	if _, _, err := store.SetConfig(matchID, matchstate.MatchConfig{HomeTeam: "西班牙", AwayTeam: "德国"}); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	if _, _, err := store.Create(matchID, matchstate.MatchEvent{
		EventType: "goal", Period: "first_half", Clock: "23:41", TeamID: "home", TeamName: "西班牙",
		PlayerName: "佩德里", Score: matchstate.Score{Home: 1, Away: 0},
		Description: "佩德里禁区前沿推射破门。",
		Participants: []matchstate.Participant{
			{Role: "scorer", Name: "佩德里", TeamName: "西班牙"},
			{Role: "assist", Name: "法比安", TeamName: "西班牙"},
		},
	}); err != nil {
		t.Fatalf("Create goal: %v", err)
	}
	answerable, err := fake.AppendThread(ctx, memory.Thread{UserID: "user-1", Kind: memory.ThreadUnansweredQuestion, Content: "刚才谁助攻？"})
	if err != nil {
		t.Fatalf("AppendThread answerable: %v", err)
	}
	if _, err := fake.AppendThread(ctx, memory.Thread{UserID: "user-1", Kind: memory.ThreadUnansweredQuestion, Content: "裁判的判罚依据是什么？"}); err != nil {
		t.Fatalf("AppendThread unanswerable: %v", err)
	}

	recoveries, err := agent.RecoverOpenThreads(ctx, "user-1", matchID, time.Now().UTC())
	if err != nil {
		t.Fatalf("RecoverOpenThreads: %v", err)
	}
	if len(recoveries) != 1 || recoveries[0].ThreadID != answerable.ID {
		t.Fatalf("recoveries = %+v, want only the ledger-grounded question", recoveries)
	}
	recovery := recoveries[0].Response
	if !strings.Contains(recovery.Reply, "法比安") || !strings.Contains(recovery.Reply, "助攻") {
		t.Fatalf("recovery reply = %q, want the deterministic assist answer", recovery.Reply)
	}
	if recovery.Trace.Reason != "open_thread_recovery" {
		t.Fatalf("recovery reason = %q, want open_thread_recovery", recovery.Trace.Reason)
	}
	// main.go marks the thread addressed only after the recovery reply lands
	// (post-delivery); the beat itself returns the recovery for delivery.
	if err := fake.MarkThreadAddressed(ctx, recoveries[0].ThreadID); err != nil {
		t.Fatalf("MarkThreadAddressed: %v", err)
	}
	// Threads without a grounded answer stay open for a later beat.
	open, err := fake.OpenThreads(ctx, "user-1")
	if err != nil || len(open) != 1 || open[0].Content != "裁判的判罚依据是什么？" {
		t.Fatalf("open threads = %+v err=%v, want the unanswerable thread left open", open, err)
	}
}

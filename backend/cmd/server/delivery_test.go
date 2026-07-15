package main

import (
	"context"
	"testing"
	"time"

	"qiuqiu/internal/companion"
	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/relationship"
)

func TestInterruptedReplyClearsPendingOpenThreadDecision(t *testing.T) {
	repository := relationship.NewMemoryRepository()
	agent := companion.NewAgent(companion.NewStoreMemoryTools(matchstate.NewStore())).WithDirector(
		relationship.NewDirector(repository),
	)
	ctx := context.Background()
	now := time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC)

	if _, err := agent.HandleMessage(ctx, companion.MessageRequest{
		SignalID: "thread-create", MatchID: "match-1", UserID: "user-1",
		Text: "高位压迫这个问题，下场接着聊", Now: now,
	}); err != nil {
		t.Fatalf("create open thread: %v", err)
	}
	recall, err := agent.HandleMessage(ctx, companion.MessageRequest{
		SignalID: "thread-recall", MatchID: "match-2", UserID: "user-1",
		Text: "接着上次说高位压迫", Now: now.Add(7 * 24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("recall open thread: %v", err)
	}
	if err := observeReplyDelivery(ctx, agent, recall.Trace, "user-1", "match-2", "interrupted", now.Add(7*24*time.Hour+time.Second)); err != nil {
		t.Fatalf("observe interrupted delivery: %v", err)
	}

	state, err := repository.Load(ctx, "user-1", "match-3")
	if err != nil {
		t.Fatalf("load relationship state: %v", err)
	}
	if len(state.Memories) != 1 || state.Memories[0].Status != "active" || len(state.Memories[0].PendingDecisionIDs) != 0 {
		t.Fatalf("memories = %+v, want active open thread without pending decisions", state.Memories)
	}
}

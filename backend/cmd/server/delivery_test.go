package main

import (
	"context"
	"testing"
	"time"

	"qiuqiu/internal/companion"
	"qiuqiu/internal/conversation"
	"qiuqiu/internal/interaction"
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

func TestDisplayedReplyDoesNotCompletePendingAudio(t *testing.T) {
	now := time.Date(2026, 9, 8, 20, 0, 0, 0, time.UTC)
	tracker := newReplyDeliveryTracker()
	trace := companion.Trace{ID: "trace-1", UserID: "user-1", MatchID: "match-1"}
	tracker.TrackWithPolicy(trace, trace.UserID, trace.MatchID, "goal:1", true, time.Minute)

	tracker.Transition(trace.ID, conversation.DeliveryTextDelivered, now)
	tracker.Transition(trace.ID, conversation.DeliveryTextDelivered, now.Add(time.Millisecond))
	tracker.Transition(trace.ID, conversation.DeliveryAudioStarted, now.Add(2*time.Millisecond))

	record, ok := tracker.Ledger().Get(trace.ID)
	if !ok || record.State != conversation.DeliveryAudioStarted {
		t.Fatalf("record = %+v, ok=%v", record, ok)
	}
}

func TestTerminalPlaybackStates(t *testing.T) {
	for _, state := range []string{"ended", "completed", "interrupted", "skipped", "blocked"} {
		if !terminalPlaybackState(state) {
			t.Fatalf("%q should be terminal", state)
		}
	}
	if terminalPlaybackState("started") {
		t.Fatal("started should not be terminal")
	}
}

func TestPlaybackResultClientReportRoutesDelivery(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 25, 20, 0, 0, 0, time.UTC)
	deliveryLedger := conversation.NewMemoryDeliveryLedger()
	interactionLedger := interaction.NewMemoryLedger()
	agent := companion.NewAgent(companion.NewStoreMemoryTools(matchstate.NewStore())).WithInteractionLedger(interactionLedger)
	tracker := newReplyDeliveryTrackerWithLedger(deliveryLedger)
	trace := companion.Trace{ID: "trace-1", UserID: "user-1", MatchID: "match-1"}
	if err := tracker.TrackWithPolicy(trace, trace.UserID, trace.MatchID, "goal:1:confirmed", false, time.Minute); err != nil {
		t.Fatalf("track delivery: %v", err)
	}
	if err := tracker.Transition(trace.ID, conversation.DeliveryTextDelivered, now); err != nil {
		t.Fatalf("transition text delivered: %v", err)
	}

	reports := newClientPlaybackReportSet()
	source, err := recordPlaybackResult(ctx, deliveryLedger, agent, reports, "user-1", "match-1", "goal:1:confirmed", "completed", "", now.Add(time.Second))
	if err != nil {
		t.Fatalf("record playback result: %v", err)
	}
	if source != playbackSourceClient {
		t.Fatalf("source = %q, want %q", source, playbackSourceClient)
	}
	record, ok := deliveryLedger.Get(trace.ID)
	if !ok || record.State != conversation.DeliveryCompleted {
		t.Fatalf("record = %+v, want completed", record)
	}
	if !reports.has("goal:1:confirmed") || !reports.has(trace.ID) {
		t.Fatal("client report should be registered under deliveryKey and trace id")
	}
	events, err := interactionLedger.List(ctx, "user-1", "match-1", 100)
	if err != nil || len(events) != 1 {
		t.Fatalf("ledger events = %+v err=%v, want exactly the client report", events, err)
	}
	event := events[0]
	if event.Kind != interaction.KindPlaybackResult || event.PlaybackState != "completed" || event.Source != playbackSourceClient {
		t.Fatalf("event = %+v, want playback_result completed from client", event)
	}
	if event.DeliveryKey != "goal:1:confirmed" || event.TraceID != trace.ID {
		t.Fatalf("event correlation = %q/%q, want goal:1:confirmed/%s", event.DeliveryKey, event.TraceID, trace.ID)
	}
}

func TestPlaybackResultLateReportDoesNotOverwriteTerminal(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 25, 20, 0, 0, 0, time.UTC)
	deliveryLedger := conversation.NewMemoryDeliveryLedger()
	interactionLedger := interaction.NewMemoryLedger()
	agent := companion.NewAgent(companion.NewStoreMemoryTools(matchstate.NewStore())).WithInteractionLedger(interactionLedger)
	tracker := newReplyDeliveryTrackerWithLedger(deliveryLedger)
	trace := companion.Trace{ID: "trace-late", UserID: "user-1", MatchID: "match-1"}
	if err := tracker.TrackWithPolicy(trace, trace.UserID, trace.MatchID, "goal:2:confirmed", false, time.Minute); err != nil {
		t.Fatalf("track delivery: %v", err)
	}
	if err := tracker.Transition(trace.ID, conversation.DeliveryTextDelivered, now); err != nil {
		t.Fatalf("transition text delivered: %v", err)
	}
	if err := tracker.Transition(trace.ID, conversation.DeliveryAudioStarted, now); err != nil {
		t.Fatalf("transition audio started: %v", err)
	}
	reports := newClientPlaybackReportSet()
	if _, err := recordPlaybackResult(ctx, deliveryLedger, agent, reports, "user-1", "match-1", "goal:2:confirmed", "completed", "", now); err != nil {
		t.Fatalf("record playback result: %v", err)
	}

	source, err := recordPlaybackResult(ctx, deliveryLedger, agent, reports, "user-1", "match-1", "goal:2:confirmed", "interrupted", "late", now.Add(time.Second))
	if err != nil {
		t.Fatalf("record late playback result: %v", err)
	}
	if source != playbackSourceClientLate {
		t.Fatalf("source = %q, want %q", source, playbackSourceClientLate)
	}
	record, ok := deliveryLedger.Get(trace.ID)
	if !ok || record.State != conversation.DeliveryCompleted {
		t.Fatalf("record = %+v, want completed kept", record)
	}
	events, err := interactionLedger.List(ctx, "user-1", "match-1", 100)
	if err != nil || len(events) != 2 {
		t.Fatalf("ledger events = %+v err=%v, want both reports kept", events, err)
	}
	sources := map[string]string{}
	for _, event := range events {
		sources[event.Source] = event.PlaybackState
	}
	if sources[playbackSourceClient] != "completed" || sources[playbackSourceClientLate] != "interrupted" {
		t.Fatalf("sources = %+v, want client=completed and client_late=interrupted", sources)
	}
}

func TestPlaybackResultUnknownDeliveryKeyRecordsLate(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 25, 20, 0, 0, 0, time.UTC)
	deliveryLedger := conversation.NewMemoryDeliveryLedger()
	interactionLedger := interaction.NewMemoryLedger()
	agent := companion.NewAgent(companion.NewStoreMemoryTools(matchstate.NewStore())).WithInteractionLedger(interactionLedger)

	reports := newClientPlaybackReportSet()
	source, err := recordPlaybackResult(ctx, deliveryLedger, agent, reports, "user-1", "match-1", "stale-key", "skipped", "muted", now)
	if err != nil {
		t.Fatalf("record playback result: %v", err)
	}
	if source != playbackSourceClientLate {
		t.Fatalf("source = %q, want %q", source, playbackSourceClientLate)
	}
	events, err := interactionLedger.List(ctx, "user-1", "match-1", 100)
	if err != nil || len(events) != 1 {
		t.Fatalf("ledger events = %+v err=%v, want the late report only", events, err)
	}
	event := events[0]
	if event.Source != playbackSourceClientLate || event.DeliveryReason != "muted" || event.TraceID != "" {
		t.Fatalf("event = %+v, want late report without trace correlation", event)
	}
	if validPlaybackResultState("started") || validPlaybackResultState("") {
		t.Fatal("non-terminal playback states must not be reportable")
	}
}

func TestInferredOutcomeSkippedAfterClientReport(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 25, 20, 0, 0, 0, time.UTC)
	interactionLedger := interaction.NewMemoryLedger()
	agent := companion.NewAgent(companion.NewStoreMemoryTools(matchstate.NewStore())).WithInteractionLedger(interactionLedger)
	connection := &watchConnection{
		deps:                  watchDeps{agent: agent},
		deliveryTracker:       newReplyDeliveryTracker(),
		interruptedReactions:  newInterruptedReactionGuard(nil),
		clientPlaybackReports: newClientPlaybackReportSet(),
		identity:              newConnectionIdentity("user-1"),
	}
	trace := companion.Trace{ID: "trace-1", UserID: "user-1", MatchID: "match-1", RelationshipDecision: &relationship.Decision{ID: "decision-1"}}

	// 无实报：推断照旧写入，且账本标 server_inferred（failed 不触发打断
	// 表演，连接级 writer 缺省无碍）。
	if err := connection.observeInferredOutcome(ctx, trace, "user-1", "match-1", "failed", now); err != nil {
		t.Fatalf("observe inferred outcome: %v", err)
	}
	events, err := interactionLedger.List(ctx, "user-1", "match-1", 100)
	if err != nil || len(events) != 1 {
		t.Fatalf("ledger events = %+v err=%v, want the inferred delivery", events, err)
	}
	if events[0].Kind != interaction.KindDelivery || events[0].Source != deliverySourceInferred {
		t.Fatalf("event = %+v, want delivery from %q", events[0], deliverySourceInferred)
	}

	// 实报在先：推断与其连带的打断反应都不再发生。
	connection.clientPlaybackReports.note(trace.ID)
	if err := connection.observeInferredOutcome(ctx, trace, "user-1", "match-1", "interrupted", now.Add(time.Second)); err != nil {
		t.Fatalf("observe inferred outcome after report: %v", err)
	}
	events, err = interactionLedger.List(ctx, "user-1", "match-1", 100)
	if err != nil || len(events) != 1 {
		t.Fatalf("ledger events = %+v err=%v, want the client report to suppress inference", events, err)
	}
}

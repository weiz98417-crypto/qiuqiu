package companion

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/relationship"
)

func TestAgentUsesRelationshipDecisionFallbackWithoutRealizer(t *testing.T) {
	store := matchstate.NewStore()
	agent := NewAgent(NewStoreMemoryTools(store)).WithDirector(
		relationship.NewDirector(relationship.NewMemoryRepository()),
	)
	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "shadow-turn-1",
		MatchID:  "match-1",
		UserID:   "user-1",
		Text:     "先看看比赛",
		Now:      time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	wantReply := "嗯，我在。"
	if response.Reply != wantReply {
		t.Fatalf("reply = %q, want relationship fallback %q", response.Reply, wantReply)
	}
	if response.Trace.RelationshipDecision == nil {
		t.Fatal("trace is missing relationship decision")
	}
	if response.Trace.RelationshipDecision.SignalID != "shadow-turn-1" {
		t.Fatalf("signal id = %q", response.Trace.RelationshipDecision.SignalID)
	}
	if len(response.Trace.RelationshipDecision.Actions) != 1 || response.Trace.RelationshipDecision.Actions[0] != relationship.ActAcknowledge {
		t.Fatalf("actions = %v", response.Trace.RelationshipDecision.Actions)
	}
}

func TestAgentRecordsShadowDecisionForFirstMeeting(t *testing.T) {
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore())).WithDirector(
		relationship.NewDirector(relationship.NewMemoryRepository()),
	)
	response, err := agent.HandleFirstMeeting(context.Background(), FirstMeetingRequest{
		SignalID: "session-user-1-match-1",
		MatchID:  "match-1",
		UserID:   "user-1",
		Now:      time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("HandleFirstMeeting: %v", err)
	}
	if response.Trace.RelationshipDecision == nil {
		t.Fatal("first meeting trace is missing relationship decision")
	}
	if response.Trace.RelationshipDecision.Relationship.Stage != relationship.StageFirstMeeting {
		t.Fatalf("stage = %q", response.Trace.RelationshipDecision.Relationship.Stage)
	}
}

func TestAgentHandlesMutedMatchEventWithoutProducingSpeech(t *testing.T) {
	tools := NewStoreMemoryTools(matchstate.NewStore())
	repository := relationship.NewMemoryRepository()
	agent := NewAgent(tools).WithDirector(
		relationship.NewDirector(repository),
	)
	response, err := agent.HandleMatchEvent(context.Background(), MatchEventRequest{
		UserID: "user-1",
		Event: matchstate.MatchEvent{
			ID:        "goal-1",
			MatchID:   "match-1",
			EventType: "goal",
			Intensity: 5,
			Confirmed: true,
		},
		OutputAllowed: false,
		Critical:      true,
		Now:           time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("HandleMatchEvent: %v", err)
	}
	if response.Reply != "" || response.Trace.ID == "" {
		t.Fatalf("muted event produced output: %+v", response)
	}
	assertToolCalled(t, response.Trace, "trace.write_decision")
	silenceRecorded := false
	for _, call := range response.Trace.ToolCalls {
		if call.Name == "response.emit_companion_reply" && call.Args["mode"] == "silence" {
			silenceRecorded = true
			break
		}
	}
	if !silenceRecorded {
		t.Fatalf("muted event trace omitted explicit silence: %+v", response.Trace.ToolCalls)
	}
	if len(tools.Traces()) != 1 {
		t.Fatalf("trace count = %d, want one silent observation trace", len(tools.Traces()))
	}
	state, err := repository.Load(context.Background(), "user-1", "match-2")
	if err != nil {
		t.Fatalf("load relationship memory: %v", err)
	}
	if len(state.Memories) != 1 || state.Memories[0].SourceTraceID != response.Trace.ID {
		t.Fatalf("memories = %+v, want source trace %q", state.Memories, response.Trace.ID)
	}
	if response.Decision.Speech != nil {
		t.Fatalf("speech = %+v, want nil", response.Decision.Speech)
	}
	if response.Presentation.Expression != "excited" || response.Presentation.Motion != "cheer" {
		t.Fatalf("presentation = %+v", response.Presentation)
	}
}

func TestAgentDoesNotCreateRelationshipMemoryBeforeSourceTraceExists(t *testing.T) {
	tools := NewStoreMemoryTools(matchstate.NewStore()).WithTraceWriter(failingTraceWriter{})
	repository := relationship.NewMemoryRepository()
	agent := NewAgent(tools).WithDirector(relationship.NewDirector(repository))

	_, err := agent.HandleMatchEvent(context.Background(), MatchEventRequest{
		UserID: "user-1",
		Event: matchstate.MatchEvent{
			ID: "goal-without-trace", MatchID: "match-1", EventType: "goal", Intensity: 5,
			Confirmed: true, Description: "已确认的关键进球",
		},
		OutputAllowed: false,
		Critical:      true,
		Now:           time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC),
	})
	if err == nil {
		t.Fatal("HandleMatchEvent succeeded without a persisted source trace")
	}
	state, loadErr := repository.Load(context.Background(), "user-1", "match-2")
	if loadErr != nil {
		t.Fatalf("load relationship state: %v", loadErr)
	}
	if len(state.Memories) != 0 {
		t.Fatalf("memories = %+v, want none when source trace write fails", state.Memories)
	}
}

func TestAgentHandlesAllowedMatchEventAsOnePlannedTurn(t *testing.T) {
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore())).WithDirector(
		relationship.NewDirector(relationship.NewMemoryRepository()),
	)
	event := matchstate.MatchEvent{
		ID:            "goal-planned-1",
		MatchID:       "match-1",
		EventType:     "goal",
		Intensity:     5,
		ProactiveText: "萨拉赫进球了！这一下太关键了。",
	}
	response, err := agent.HandleMatchEvent(context.Background(), MatchEventRequest{
		UserID:        "user-1",
		Event:         event,
		OutputAllowed: true,
		Critical:      true,
		Now:           time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("HandleMatchEvent: %v", err)
	}
	if response.Reply != event.ProactiveText {
		t.Fatalf("reply = %q", response.Reply)
	}
	if response.Decision.ID == "" || response.Trace.RelationshipDecision == nil || response.Trace.RelationshipDecision.ID != response.Decision.ID {
		t.Fatalf("decision mismatch: response=%+v trace=%+v", response.Decision, response.Trace.RelationshipDecision)
	}
	if response.Presentation.Expression != "excited" || response.Presentation.Motion != "cheer" {
		t.Fatalf("presentation = %+v", response.Presentation)
	}
}

func TestAgentTreatsFactRevisionsAsDistinctMatchSignals(t *testing.T) {
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore())).WithDirector(
		relationship.NewDirector(relationship.NewMemoryRepository()),
	)
	event := matchstate.MatchEvent{
		ID:           "goal-revision-1",
		MatchID:      "match-1",
		EventType:    "goal",
		Intensity:    5,
		FactRevision: 1,
		FactStatus:   matchstate.FactStatusConfirmed,
	}
	first, err := agent.HandleMatchEvent(context.Background(), MatchEventRequest{
		UserID: "user-1", Event: event, OutputAllowed: true, Critical: true,
		Now: time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	event.FactRevision = 2
	event.FactStatus = matchstate.FactStatusReconciled
	second, err := agent.HandleMatchEvent(context.Background(), MatchEventRequest{
		UserID: "user-1", Event: event, OutputAllowed: true, Critical: true,
		Now: time.Date(2026, 7, 15, 20, 0, 1, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Trace.ID == second.Trace.ID || first.Decision.ID == second.Decision.ID {
		t.Fatalf("fact revisions reused trace or decision: first=%+v second=%+v", first.Trace, second.Trace)
	}
	if second.Decision.SignalID != "match:user-1:goal-revision-1:2:reconciled" {
		t.Fatalf("second signal id = %q", second.Decision.SignalID)
	}
}

func TestAgentSignalRetryDoesNotDuplicateTraceOrConversationTurns(t *testing.T) {
	store := matchstate.NewStore()
	tools := NewStoreMemoryTools(store)
	agent := NewAgent(tools).WithDirector(
		relationship.NewDirector(relationship.NewMemoryRepository()),
	)
	request := MessageRequest{
		SignalID: "retry-turn-1",
		MatchID:  "match-1",
		UserID:   "user-1",
		Text:     "现在几比几？",
		Now:      time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC),
	}
	first, err := agent.HandleMessage(context.Background(), request)
	if err != nil {
		t.Fatalf("first HandleMessage: %v", err)
	}
	retried, err := agent.HandleMessage(context.Background(), request)
	if err != nil {
		t.Fatalf("retried HandleMessage: %v", err)
	}
	if retried.Trace.ID != first.Trace.ID {
		t.Fatalf("retried trace id = %q, want %q", retried.Trace.ID, first.Trace.ID)
	}
	if got := len(tools.Traces()); got != 1 {
		t.Fatalf("trace count = %d, want 1", got)
	}
	turns, err := tools.RecentTurns(context.Background(), request.MatchID, request.UserID, 10)
	if err != nil {
		t.Fatalf("RecentTurns: %v", err)
	}
	if got := len(turns); got != 2 {
		t.Fatalf("turn count = %d, want 2", got)
	}
}

func TestAgentRejectsSignalReuseWithDifferentPayload(t *testing.T) {
	tools := NewStoreMemoryTools(matchstate.NewStore())
	agent := NewAgent(tools).WithDirector(relationship.NewDirector(relationship.NewMemoryRepository()))
	request := MessageRequest{
		SignalID: "reused-signal",
		MatchID:  "match-1",
		UserID:   "user-1",
		Text:     "第一句话",
		Now:      time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC),
	}
	if _, err := agent.HandleMessage(context.Background(), request); err != nil {
		t.Fatalf("first HandleMessage: %v", err)
	}
	request.Text = "完全不同的第二句话"
	if _, err := agent.HandleMessage(context.Background(), request); err != ErrTraceConflict {
		t.Fatalf("reused signal error = %v, want ErrTraceConflict", err)
	}
}

func TestAgentScopesStableTraceByMatch(t *testing.T) {
	tools := NewStoreMemoryTools(matchstate.NewStore())
	agent := NewAgent(tools).WithDirector(relationship.NewDirector(relationship.NewMemoryRepository()))
	request := MessageRequest{
		SignalID: "shared-signal", MatchID: "match-1", UserID: "user-1", Text: "继续",
		Now: time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC),
	}
	first, err := agent.HandleMessage(context.Background(), request)
	if err != nil {
		t.Fatalf("first HandleMessage: %v", err)
	}
	request.MatchID = "match-2"
	second, err := agent.HandleMessage(context.Background(), request)
	if err != nil {
		t.Fatalf("second HandleMessage: %v", err)
	}
	if first.Trace.ID == second.Trace.ID {
		t.Fatalf("cross-match requests share trace id %q", first.Trace.ID)
	}
}

func TestAgentAdvancesRelationshipFromNaturalTrustLanguageAcrossMatches(t *testing.T) {
	repository := relationship.NewMemoryRepository()
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore())).WithDirector(
		relationship.NewDirector(repository),
	)
	ctx := context.Background()
	start := time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC)

	for index, matchID := range []string{"match-1", "match-2", "match-3"} {
		if _, err := agent.HandleFirstMeeting(ctx, FirstMeetingRequest{
			SignalID: "open-" + matchID,
			MatchID:  matchID,
			UserID:   "user-1",
			Now:      start.Add(time.Duration(index) * 7 * 24 * time.Hour),
		}); err != nil {
			t.Fatalf("open %s: %v", matchID, err)
		}
	}

	turns := []MessageRequest{
		{SignalID: "taste", MatchID: "match-1", UserID: "user-1", Text: "我喜欢高位压迫", Now: start.Add(time.Minute)},
		{SignalID: "callback", MatchID: "match-2", UserID: "user-1", Text: "接着上次说高位压迫", Now: start.Add(7*24*time.Hour + time.Minute)},
		{SignalID: "trust", MatchID: "match-3", UserID: "user-1", Text: "你上次那个判断挺准，接着来", Now: start.Add(14*24*time.Hour + time.Minute)},
	}
	var final Response
	for _, turn := range turns {
		response, err := agent.HandleMessage(ctx, turn)
		if err != nil {
			t.Fatalf("HandleMessage %s: %v", turn.SignalID, err)
		}
		final = response
	}
	if final.Trace.RelationshipDecision == nil {
		t.Fatal("final turn is missing relationship decision")
	}
	if stage := final.Trace.RelationshipDecision.Relationship.Stage; stage != relationship.StageWatchBuddy {
		t.Fatalf("stage = %q, want %q", stage, relationship.StageWatchBuddy)
	}
}

func TestAgentRecallsSharedMomentFromNaturalCrossMatchLanguage(t *testing.T) {
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore())).WithDirector(
		relationship.NewDirector(relationship.NewMemoryRepository()),
	)
	ctx := context.Background()
	start := time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC)

	if _, err := agent.HandleMatchEvent(ctx, MatchEventRequest{
		UserID: "user-1",
		Event: matchstate.MatchEvent{
			ID: "goal-1", MatchID: "match-1", EventType: "goal", Intensity: 5,
			Description: "萨拉赫补时绝杀", PlayerName: "萨拉赫", Confirmed: true,
		},
		OutputAllowed: true,
		Critical:      true,
		Now:           start,
	}); err != nil {
		t.Fatalf("record shared moment: %v", err)
	}

	response, err := agent.HandleMessage(ctx, MessageRequest{
		SignalID: "recall-moment", MatchID: "match-2", UserID: "user-1",
		Text: "这一下又想起上次那场了", Now: start.Add(7 * 24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("recall shared moment: %v", err)
	}
	decision := response.Trace.RelationshipDecision
	if decision == nil || !hasCommunicationAct(decision.Actions, relationship.ActRecall) {
		t.Fatalf("decision = %+v, want recall action", decision)
	}
	if len(decision.Memories) != 1 || decision.Memories[0].Kind != relationship.MemoryKindSharedMoment {
		t.Fatalf("memories = %+v, want shared moment", decision.Memories)
	}
}

func TestAgentResolvesOpenThreadOnlyAfterItsReplyIsDisplayed(t *testing.T) {
	repository := relationship.NewMemoryRepository()
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore())).WithDirector(
		relationship.NewDirector(repository),
	).WithRealizer(fakeRealizer{text: "那套压迫思路，接着聊。"}, time.Second)
	ctx := context.Background()
	start := time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC)

	if _, err := agent.HandleMessage(ctx, MessageRequest{
		SignalID: "thread-create", MatchID: "match-1", UserID: "user-1",
		Text: "高位压迫这个问题，下场接着聊", Now: start,
	}); err != nil {
		t.Fatalf("create open thread: %v", err)
	}
	recall, err := agent.HandleMessage(ctx, MessageRequest{
		SignalID: "thread-recall", MatchID: "match-2", UserID: "user-1",
		Text: "接着上次说高位压迫", Now: start.Add(7 * 24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("recall open thread: %v", err)
	}
	decision := recall.Trace.RelationshipDecision
	if decision == nil || len(decision.UsedMemoryIDs) != 1 {
		t.Fatalf("decision = %+v, want one used relationship memory", decision)
	}
	if strings.Contains(recall.Reply, "高位压迫这个问题") {
		t.Fatalf("reply = %q, want a natural paraphrase", recall.Reply)
	}
	if _, err := agent.ObserveDelivery(
		ctx, "delivery:thread-recall:text", "user-1", "match-2", decision.ID,
		"text_delivered", "user_reply", decision.UsedMemoryIDs, start.Add(7*24*time.Hour+time.Second),
	); err != nil {
		t.Fatalf("ObserveDelivery: %v", err)
	}

	state, err := repository.Load(ctx, "user-1", "match-3")
	if err != nil {
		t.Fatalf("load delivered thread: %v", err)
	}
	for _, memory := range state.Memories {
		if memory.ID == decision.UsedMemoryIDs[0] && memory.Status == "resolved" {
			return
		}
	}
	t.Fatalf("memories = %+v, want used open thread resolved", state.Memories)
}

func TestFirstMeetingRetryAfterDeliveryProducesNoSecondGreeting(t *testing.T) {
	tools := NewStoreMemoryTools(matchstate.NewStore())
	agent := NewAgent(tools).WithDirector(relationship.NewDirector(relationship.NewMemoryRepository()))
	ctx := context.Background()
	now := time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC)
	first, err := agent.HandleFirstMeeting(ctx, FirstMeetingRequest{
		SignalID: "first-meeting-attempt-1", MatchID: "match-1", UserID: "user-1", Now: now,
	})
	if err != nil {
		t.Fatalf("first HandleFirstMeeting: %v", err)
	}
	if first.Reply == "" {
		t.Fatal("first greeting is empty")
	}
	if _, err := agent.ObserveDelivery(ctx, "delivery:first-meeting:text", "user-1", "match-1", first.Trace.RelationshipDecision.ID, "text_delivered", "first_meeting", nil, now.Add(time.Second)); err != nil {
		t.Fatalf("ObserveDelivery: %v", err)
	}
	retried, err := agent.HandleFirstMeeting(ctx, FirstMeetingRequest{
		SignalID: "first-meeting-attempt-2", MatchID: "match-1", UserID: "user-1", Now: now.Add(2 * time.Second),
	})
	if err != nil {
		t.Fatalf("retried HandleFirstMeeting: %v", err)
	}
	if retried.Reply != "" || retried.Trace.Reason != "first_meeting_already_delivered" {
		t.Fatalf("retried greeting = %+v, want silent already-delivered result", retried)
	}
}

type failingTraceWriter struct{}

func (failingTraceWriter) WriteTrace(context.Context, Trace) error {
	return errors.New("trace store unavailable")
}

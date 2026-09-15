package companion

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"qiuqiu/internal/matchstate"
)

func TestEvalProactiveLineThenUserAsksAboutRecordedMatchMemory(t *testing.T) {
	ctx := context.Background()
	store := matchstate.NewStore()
	tools := NewStoreMemoryTools(store)
	agent := NewAgent(tools)
	matchID := "product-eval-baseline"

	if _, _, err := store.SetConfig(matchID, matchstate.MatchConfig{
		HomeTeam: "西班牙",
		AwayTeam: "德国",
		HomePlayers: []matchstate.Player{
			{Number: "10", Name: "佩德里", Position: "CM"},
			{Number: "8", Name: "法比安", Position: "CM"},
			{Number: "19", Name: "亚马尔", Position: "RW"},
		},
		AwayPlayers: []matchstate.Player{{Number: "10", Name: "穆西亚拉", Position: "AM"}},
	}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}

	goal, snapshot, err := store.Create(matchID, matchstate.MatchEvent{
		EventType:     "goal",
		Period:        "first_half",
		Clock:         "23:41",
		TeamID:        "home",
		TeamName:      "西班牙",
		Score:         matchstate.Score{Home: 1, Away: 0},
		Intensity:     5,
		Description:   "佩德里禁区前沿推射破门。",
		ProactiveText: "佩德里进球了！这一下西班牙先打开局面。",
		Participants: []matchstate.Participant{
			{Role: "scorer", Name: "佩德里", TeamID: "home", TeamName: "西班牙"},
			{Role: "assist", Name: "法比安", TeamID: "home", TeamName: "西班牙"},
			{Role: "pre_assist", Name: "亚马尔", TeamID: "home", TeamName: "西班牙"},
		},
	})
	if err != nil {
		t.Fatalf("Create goal error: %v", err)
	}

	proactive, err := agent.HandleMatchEvent(ctx, MatchEventRequest{UserID: "user-1", Event: goal, Snapshot: snapshot, OutputAllowed: true, Critical: true})
	if err != nil {
		t.Fatalf("HandleMatchEvent error: %v", err)
	}
	assertContains(t, proactive.Reply, "佩德里")
	assertContains(t, proactive.Reply, "进球")

	assistReply, err := agent.HandleMessage(ctx, MessageRequest{
		MatchID: matchID,
		UserID:  "user-1",
		Text:    "刚才谁助攻？",
		Now:     fixedTime(),
	})
	if err != nil {
		t.Fatalf("HandleMessage assist error: %v", err)
	}
	if assistReply.Intent != IntentRecentEvent {
		t.Fatalf("wrong intent: %s", assistReply.Intent)
	}
	assertContains(t, assistReply.Reply, "法比安")
	assertContains(t, assistReply.Reply, "亚马尔")
	assertContains(t, strings.Join(assistReply.Trace.RetrievedEvent, ","), goal.ID)

	scoreReply, err := agent.HandleMessage(ctx, MessageRequest{
		MatchID: matchID,
		UserID:  "user-1",
		Text:    "现在几比几？",
		Now:     fixedTime(),
	})
	if err != nil {
		t.Fatalf("HandleMessage score error: %v", err)
	}
	assertContains(t, scoreReply.Reply, "西班牙 1-0 德国")
	assertToolCalled(t, scoreReply.Trace, "match.read_snapshot")

	missingReply, err := agent.HandleMessage(ctx, MessageRequest{
		MatchID: matchID,
		UserID:  "user-1",
		Text:    "穆西亚拉刚才进球了吗？",
		Now:     fixedTime(),
	})
	if err != nil {
		t.Fatalf("HandleMessage missing player goal error: %v", err)
	}
	assertContains(t, missingReply.Reply, "没有看到穆西亚拉的进球记录")

	traces := tools.Traces()
	if len(traces) != 4 {
		t.Fatalf("expected proactive + 3 user traces, got %d", len(traces))
	}
	for _, trace := range traces {
		if trace.Output == "" || trace.Reason == "" {
			t.Fatalf("trace missing output/reason: %+v", trace)
		}
	}
}

func TestEvalDirectorProactiveTraceAnchorsFollowUpMemory(t *testing.T) {
	ctx := context.Background()
	store := matchstate.NewStore()
	tools := NewStoreMemoryTools(store)
	agent := NewAgent(tools)
	matchID := "product-eval-director-proactive"
	if _, _, err := store.SetConfig(matchID, matchstate.MatchConfig{HomeTeam: "西班牙", AwayTeam: "德国"}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}
	goal, snapshot, err := store.Create(matchID, matchstate.MatchEvent{
		EventType:     "goal",
		Period:        "first_half",
		Clock:         "24:10",
		TeamID:        "home",
		TeamName:      "西班牙",
		PlayerName:    "佩德里",
		Score:         matchstate.Score{Home: 1, Away: 0},
		Description:   "佩德里：禁区内抢点破门。",
		ProactiveText: "佩德里这一下太关键了，法比安的助攻也很漂亮。",
		Participants: []matchstate.Participant{
			{Role: "scorer", Name: "佩德里", TeamID: "home", TeamName: "西班牙"},
			{Role: "assist", Name: "法比安", TeamID: "home", TeamName: "西班牙"},
			{Role: "pre_assist", Name: "亚马尔", TeamID: "home", TeamName: "西班牙"},
		},
	})
	if err != nil {
		t.Fatalf("Create goal error: %v", err)
	}
	proactive, err := agent.HandleMatchEvent(ctx, MatchEventRequest{UserID: "demo-user", Event: goal, Snapshot: snapshot, OutputAllowed: true, Critical: true})
	if err != nil {
		t.Fatalf("HandleMatchEvent error: %v", err)
	}
	if proactive.Reply != goal.ProactiveText {
		t.Fatalf("manual proactive line changed: got %q want %q", proactive.Reply, goal.ProactiveText)
	}
	if proactive.Trace.Reason != "operator_event_proactive_line" {
		t.Fatalf("unexpected proactive reason: %+v", proactive.Trace)
	}
	assertContains(t, strings.Join(proactive.Trace.RetrievedEvent, ","), goal.ID)

	followUp, err := agent.HandleMessage(ctx, MessageRequest{
		MatchID: matchID,
		UserID:  "demo-user",
		Text:    "谁策动的？",
		Now:     fixedTime().Add(time.Second),
	})
	if err != nil {
		t.Fatalf("HandleMessage follow-up error: %v", err)
	}
	assertContains(t, followUp.Reply, "亚马尔")
	assertContains(t, strings.Join(followUp.Trace.RetrievedEvent, ","), goal.ID)
}

func TestEvalCompanionBoundariesDoNotInventUnrecordedFacts(t *testing.T) {
	ctx := context.Background()
	store := matchstate.NewStore()
	tools := NewStoreMemoryTools(store)
	agent := NewAgent(tools)
	matchID := "product-eval-boundary"

	if _, _, err := store.SetConfig(matchID, matchstate.MatchConfig{HomeTeam: "西班牙", AwayTeam: "德国"}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}

	reply, err := agent.HandleMessage(ctx, MessageRequest{
		MatchID: matchID,
		UserID:  "user-1",
		Text:    "穆西亚拉进球了吗？",
		Now:     fixedTime(),
	})
	if err != nil {
		t.Fatalf("HandleMessage error: %v", err)
	}
	assertContains(t, reply.Reply, "没有看到穆西亚拉的进球记录")
	assertToolCalled(t, reply.Trace, "match.get_player_timeline")

	control, err := agent.HandleMessage(ctx, MessageRequest{
		MatchID: matchID,
		UserID:  "user-1",
		Text:    "你先少说一点",
		Now:     fixedTime(),
	})
	if err != nil {
		t.Fatalf("HandleMessage control error: %v", err)
	}
	if control.Intent != IntentControlCommand {
		t.Fatalf("expected control intent, got %s", control.Intent)
	}
	assertContains(t, control.Reply, "少说")
}

func TestEvalCompanionFollowUpUsesShortTermConversationMemory(t *testing.T) {
	ctx := context.Background()
	store := matchstate.NewStore()
	tools := NewStoreMemoryTools(store)
	agent := NewAgent(tools)
	matchID := "product-eval-follow-up"

	if _, _, err := store.SetConfig(matchID, matchstate.MatchConfig{HomeTeam: "西班牙", AwayTeam: "德国"}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}
	goal, _, err := store.Create(matchID, matchstate.MatchEvent{
		EventType:   "goal",
		Period:      "first_half",
		Clock:       "24:10",
		TeamID:      "home",
		TeamName:    "西班牙",
		PlayerName:  "佩德里",
		Score:       matchstate.Score{Home: 1, Away: 0},
		Description: "佩德里禁区内抢点破门。",
		Participants: []matchstate.Participant{
			{Role: "scorer", Name: "佩德里", TeamID: "home", TeamName: "西班牙"},
			{Role: "assist", Name: "法比安", TeamID: "home", TeamName: "西班牙"},
			{Role: "pre_assist", Name: "亚马尔", TeamID: "home", TeamName: "西班牙"},
		},
	})
	if err != nil {
		t.Fatalf("Create goal error: %v", err)
	}

	assistReply, err := agent.HandleMessage(ctx, MessageRequest{
		MatchID: matchID,
		UserID:  "user-1",
		Text:    "刚才谁助攻？",
		Now:     fixedTime(),
	})
	if err != nil {
		t.Fatalf("HandleMessage assist error: %v", err)
	}
	assertContains(t, assistReply.Reply, "法比安")
	assertContains(t, strings.Join(assistReply.Trace.RetrievedEvent, ","), goal.ID)

	followUp, err := agent.HandleMessage(ctx, MessageRequest{
		MatchID: matchID,
		UserID:  "user-1",
		Text:    "谁策动的？",
		Now:     fixedTime().Add(time.Second),
	})
	if err != nil {
		t.Fatalf("HandleMessage follow-up error: %v", err)
	}
	if followUp.Intent != IntentFollowUp {
		t.Fatalf("expected follow-up intent, got %s", followUp.Intent)
	}
	assertContains(t, followUp.Reply, "亚马尔")
	assertContains(t, strings.Join(followUp.Trace.RetrievedEvent, ","), goal.ID)
	assertToolCalled(t, followUp.Trace, "conversation.read_recent")
	assertToolCalled(t, followUp.Trace, "match.search_events")
	assertToolCalled(t, followUp.Trace, "conversation.append_turn")
}

func TestEvalUserAgentToolBoundaryForbidsOperatorMutations(t *testing.T) {
	for _, allowed := range []string{
		"match.read_snapshot",
		"match.search_events",
		"match.get_player_timeline",
		"match.verify_user_claim",
		"conversation.append_turn",
		"trace.write_decision",
		"response.emit_companion_reply",
	} {
		if !IsUserAgentToolAllowed(allowed) {
			t.Fatalf("expected user agent tool %q to be allowed", allowed)
		}
	}
	for _, forbidden := range []string{
		"operator.create_event",
		"operator.correct_event",
		"database.exec_match_mutation",
	} {
		if IsUserAgentToolAllowed(forbidden) {
			t.Fatalf("expected mutation tool %q to be forbidden", forbidden)
		}
	}
	for _, schema := range CompanionToolSchemas() {
		if schema.MutatesMatchFacts {
			t.Fatalf("companion tool schema must not mutate match facts: %+v", schema)
		}
	}
}

func TestEvalRealizerCannotOverrideGroundedFacts(t *testing.T) {
	cases := []struct {
		name       string
		realizer   ReplyRealizer
		wantReply  string
		wantReason string
		wantError  string
	}{
		{
			name:       "success",
			realizer:   fakeRealizer{text: "刚才这球确实是法比安助攻，亚马尔参与策动，信息很清楚。"},
			wantReply:  "刚才这球是法比安助攻，亚马尔参与策动。",
			wantReason: "deterministic_fact_policy",
		},
		{
			name:       "empty",
			realizer:   fakeRealizer{text: ""},
			wantReply:  "刚才这球是法比安助攻，亚马尔参与策动。",
			wantReason: "deterministic_fact_policy",
		},
		{
			name:       "error",
			realizer:   fakeRealizer{err: fmt.Errorf("provider timeout")},
			wantReply:  "刚才这球是法比安助攻，亚马尔参与策动。",
			wantReason: "deterministic_fact_policy",
		},
		{
			name:       "anchor mismatch",
			realizer:   fakeRealizer{text: "刚才这球是莫拉塔助攻，节奏很好。"},
			wantReply:  "刚才这球是法比安助攻，亚马尔参与策动。",
			wantReason: "deterministic_fact_policy",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			store := matchstate.NewStore()
			tools := NewStoreMemoryTools(store)
			agent := NewAgent(tools).WithRealizer(tc.realizer, time.Second)
			matchID := "product-eval-realizer-" + strings.ReplaceAll(tc.name, " ", "-")
			seedRealizerGoal(t, store, matchID)

			reply, err := agent.HandleMessage(ctx, MessageRequest{
				MatchID: matchID,
				UserID:  "user-1",
				Text:    "刚才谁助攻？",
				Now:     fixedTime(),
			})
			if err != nil {
				t.Fatalf("HandleMessage error: %v", err)
			}
			if reply.Reply != tc.wantReply {
				t.Fatalf("wrong reply: got %q want %q", reply.Reply, tc.wantReply)
			}
			if reply.Trace.Reason != tc.wantReason {
				t.Fatalf("wrong reason: got %q want %q", reply.Trace.Reason, tc.wantReason)
			}
			if tc.wantError != "" {
				assertContains(t, reply.Trace.Error, tc.wantError)
			}
			assertContains(t, reply.Reply, "法比安")
			assertContains(t, reply.Reply, "亚马尔")
			assertToolCalled(t, reply.Trace, "response.emit_companion_reply")
		})
	}
}

func TestEvalCompanionCorrectionAwareMemoryAndUnknownFallback(t *testing.T) {
	ctx := context.Background()
	store := matchstate.NewStore()
	tools := NewStoreMemoryTools(store)
	agent := NewAgent(tools)
	matchID := "product-eval-correction"

	if _, _, err := store.SetConfig(matchID, matchstate.MatchConfig{HomeTeam: "西班牙", AwayTeam: "德国"}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}

	original, _, err := store.Create(matchID, matchstate.MatchEvent{
		EventType:   "goal",
		Period:      "first_half",
		Clock:       "12:00",
		TeamID:      "home",
		TeamName:    "西班牙",
		Score:       matchstate.Score{Home: 1, Away: 0},
		Description: "佩德里破门。",
		Participants: []matchstate.Participant{
			{Role: "scorer", Name: "佩德里"},
			{Role: "assist", Name: "法比安"},
		},
	})
	if err != nil {
		t.Fatalf("Create original goal error: %v", err)
	}
	replacement, _, err := store.Correct(matchID, original.ID, matchstate.MatchEvent{
		EventType:   "var_check",
		Period:      "first_half",
		Clock:       "13:10",
		TeamID:      "home",
		TeamName:    "西班牙",
		PlayerName:  "佩德里",
		Score:       matchstate.Score{Home: 0, Away: 0},
		Description: "VAR 取消这粒进球。",
	})
	if err != nil {
		t.Fatalf("Correct goal error: %v", err)
	}

	scoreReply, err := agent.HandleMessage(ctx, MessageRequest{
		MatchID: matchID,
		UserID:  "user-1",
		Text:    "现在几比几？",
		Now:     fixedTime(),
	})
	if err != nil {
		t.Fatalf("HandleMessage score error: %v", err)
	}
	assertContains(t, scoreReply.Reply, "0-0")
	assertNotContains(t, scoreReply.Reply, "1-0")

	assistReply, err := agent.HandleMessage(ctx, MessageRequest{
		MatchID: matchID,
		UserID:  "user-1",
		Text:    "刚才谁助攻？",
		Now:     fixedTime(),
	})
	if err != nil {
		t.Fatalf("HandleMessage assist error: %v", err)
	}
	assertNotContains(t, strings.Join(assistReply.Trace.RetrievedEvent, ","), original.ID)
	assertNotContains(t, assistReply.Trace.Output, "法比安")
	assertContains(t, assistReply.Trace.Output, "没看到")
	_ = replacement

	unknownReply, err := agent.HandleMessage(ctx, MessageRequest{
		MatchID: matchID,
		UserID:  "user-1",
		Text:    "请你分析一下今天球场草皮对传控节奏的隐藏影响",
		Now:     fixedTime(),
	})
	if err != nil {
		t.Fatalf("HandleMessage unknown error: %v", err)
	}
	if unknownReply.Intent != IntentUnknown {
		t.Fatalf("expected unknown intent, got %s", unknownReply.Intent)
	}
	assertContains(t, unknownReply.Reply, "没接明白")
	assertNotContains(t, unknownReply.Reply, "按陪看")
	assertToolNotCalled(t, unknownReply.Trace, "match.read_snapshot")
	assertToolNotCalled(t, unknownReply.Trace, "match.search_events")
	assertToolNotCalled(t, unknownReply.Trace, "match.get_player_timeline")
	assertToolCalled(t, unknownReply.Trace, "conversation.append_turn")
	assertToolCalled(t, unknownReply.Trace, "trace.write_decision")
}

func BenchmarkEvalCompanionRecentEventAnswer(b *testing.B) {
	ctx := context.Background()
	store := matchstate.NewStore()
	tools := NewStoreMemoryTools(store)
	agent := NewAgent(tools)
	matchID := "product-eval-bench"
	if _, _, err := store.SetConfig(matchID, matchstate.MatchConfig{HomeTeam: "西班牙", AwayTeam: "德国"}); err != nil {
		b.Fatalf("SetConfig error: %v", err)
	}
	for i := 0; i < 30; i++ {
		if _, _, err := store.Create(matchID, matchstate.MatchEvent{
			EventType:   "shot",
			Clock:       "10:00",
			TeamID:      "home",
			TeamName:    "西班牙",
			Score:       matchstate.Score{Home: 0, Away: 0},
			Description: "一次射门。",
		}); err != nil {
			b.Fatalf("Create history error: %v", err)
		}
	}
	if _, _, err := store.Create(matchID, matchstate.MatchEvent{
		EventType:   "goal",
		Clock:       "23:41",
		TeamID:      "home",
		TeamName:    "西班牙",
		Score:       matchstate.Score{Home: 1, Away: 0},
		Description: "佩德里破门。",
		Participants: []matchstate.Participant{
			{Role: "scorer", Name: "佩德里"},
			{Role: "assist", Name: "法比安"},
		},
	}); err != nil {
		b.Fatalf("Create goal error: %v", err)
	}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := agent.HandleMessage(ctx, MessageRequest{MatchID: matchID, UserID: "bench", Text: "刚才谁助攻？", Now: fixedTime()}); err != nil {
			b.Fatalf("HandleMessage error: %v", err)
		}
	}
}

func assertContains(t *testing.T, text, want string) {
	t.Helper()
	if !strings.Contains(text, want) {
		t.Fatalf("expected %q to contain %q", text, want)
	}
}

func assertNotContains(t *testing.T, text, want string) {
	t.Helper()
	if strings.Contains(text, want) {
		t.Fatalf("expected %q not to contain %q", text, want)
	}
}

func assertToolCalled(t *testing.T, trace Trace, name string) {
	t.Helper()
	for _, call := range trace.ToolCalls {
		if call.Name == name {
			return
		}
	}
	t.Fatalf("expected tool %q in trace %+v", name, trace.ToolCalls)
}

func assertToolNotCalled(t *testing.T, trace Trace, name string) {
	t.Helper()
	for _, call := range trace.ToolCalls {
		if call.Name == name {
			t.Fatalf("expected tool %q not to be called in trace %+v", name, trace.ToolCalls)
		}
	}
}

func seedRealizerGoal(t *testing.T, store *matchstate.Store, matchID string) {
	t.Helper()
	if _, _, err := store.SetConfig(matchID, matchstate.MatchConfig{HomeTeam: "西班牙", AwayTeam: "德国"}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}
	if _, _, err := store.Create(matchID, matchstate.MatchEvent{
		EventType:   "goal",
		Period:      "first_half",
		Clock:       "24:10",
		TeamID:      "home",
		TeamName:    "西班牙",
		PlayerName:  "佩德里",
		Score:       matchstate.Score{Home: 1, Away: 0},
		Description: "佩德里禁区内抢点破门。",
		Participants: []matchstate.Participant{
			{Role: "scorer", Name: "佩德里", TeamID: "home", TeamName: "西班牙"},
			{Role: "assist", Name: "法比安", TeamID: "home", TeamName: "西班牙"},
			{Role: "pre_assist", Name: "亚马尔", TeamID: "home", TeamName: "西班牙"},
		},
	}); err != nil {
		t.Fatalf("Create goal error: %v", err)
	}
}

func fixedTime() time.Time {
	return time.Date(2026, 6, 1, 20, 0, 0, 0, time.UTC)
}

package companion

import (
	"context"
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

	proactive, err := agent.HandleProactiveEvent(ctx, "user-1", goal, snapshot)
	if err != nil {
		t.Fatalf("HandleProactiveEvent error: %v", err)
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

func assertToolCalled(t *testing.T, trace Trace, name string) {
	t.Helper()
	for _, call := range trace.ToolCalls {
		if call.Name == name {
			return
		}
	}
	t.Fatalf("expected tool %q in trace %+v", name, trace.ToolCalls)
}

func fixedTime() time.Time {
	return time.Date(2026, 6, 1, 20, 0, 0, 0, time.UTC)
}

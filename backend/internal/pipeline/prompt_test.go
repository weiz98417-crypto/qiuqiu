package pipeline

import (
	"strings"
	"testing"

	"qiuqiu/internal/matchstate"
)

func TestEvalProactiveEventPackagePromptIncludesContext(t *testing.T) {
	ev := matchstate.MatchEvent{
		EventType:         "goal",
		Period:            "first_half",
		Clock:             "23:41",
		TeamID:            "home",
		TeamName:          "Spain",
		PlayerName:        "Pedri",
		Score:             matchstate.Score{Home: 2, Away: 1},
		Intensity:         5,
		RecommendedAction: "celebrate",
		Description:       "Spain: Pedri scores after a cutback.",
		Participants: []matchstate.Participant{
			{Role: "scorer", Name: "Pedri"},
			{Role: "assist", Name: "Olmo"},
			{Role: "pre_assist", Name: "Yamal"},
		},
	}
	snapshot := matchstate.Snapshot{
		MatchID:              "eval-prompt",
		HomeTeam:             "Spain",
		AwayTeam:             "Germany",
		Score:                matchstate.Score{Home: 1, Away: 1},
		Period:               "first_half",
		Clock:                "23:00",
		EmotionalTemperature: 4,
		RecentEvents: []matchstate.MatchEvent{{
			EventType:   "shot",
			Clock:       "22:10",
			TeamName:    "Spain",
			Description: "A warning shot.",
		}},
	}

	messages := BuildProactiveEventMessages("system persona", ev, snapshot)
	if len(messages) != 3 {
		t.Fatalf("expected system, snapshot, user messages; got %d", len(messages))
	}
	user := messages[2].Content
	for _, want := range []string{
		"23:41",
		"2-1",
		"goal",
		"Pedri",
		"scorer=Pedri",
		"assist=Olmo",
		"pre_assist=Yamal",
		"celebrate",
		"Pedri scores after a cutback.",
	} {
		if !strings.Contains(user, want) {
			t.Fatalf("proactive prompt missing %q:\n%s", want, user)
		}
	}
	if strings.Contains(user, "Spain: Spain:") {
		t.Fatalf("prompt should trim repeated team prefix:\n%s", user)
	}
}

func TestEvalReplyPromptCarriesMatchMemory(t *testing.T) {
	snapshot := matchstate.Snapshot{
		MatchID:              "eval-reply",
		HomeTeam:             "Spain",
		AwayTeam:             "Germany",
		Score:                matchstate.Score{Home: 1, Away: 0},
		Period:               "second_half",
		Clock:                "61:20",
		EmotionalTemperature: 5,
		RecentEvents: []matchstate.MatchEvent{
			{
				EventType:   "goal",
				Clock:       "60:55",
				TeamName:    "Spain",
				Description: "Pedri scores.",
				Participants: []matchstate.Participant{
					{Role: "scorer", Name: "Pedri"},
					{Role: "assist", Name: "Olmo"},
				},
			},
		},
	}

	messages := BuildReplyMessagesWithMatch("system persona", "刚刚谁进球了？", "question", snapshot)
	if len(messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(messages))
	}
	snapshotMsg := messages[1].Content
	for _, want := range []string{"Spain", "Germany", "1-0", "61:20", "Pedri scores.", "scorer=Pedri", "assist=Olmo"} {
		if !strings.Contains(snapshotMsg, want) {
			t.Fatalf("reply snapshot missing %q:\n%s", want, snapshotMsg)
		}
	}
}

func TestEvalPromptBoundaryNoEventsSnapshot(t *testing.T) {
	messages := BuildReplyMessagesWithMatch("system persona", "现在什么情况？", "question", matchstate.Snapshot{})
	if len(messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(messages))
	}
	if !strings.Contains(messages[1].Content, "暂无") {
		t.Fatalf("empty snapshot should explicitly say no recorded events:\n%s", messages[1].Content)
	}
}

func BenchmarkEvalProactiveEventPackagePrompt(b *testing.B) {
	ev := matchstate.MatchEvent{
		EventType:         "goal",
		Period:            "first_half",
		Clock:             "23:41",
		TeamName:          "Spain",
		PlayerName:        "Pedri",
		Score:             matchstate.Score{Home: 2, Away: 1},
		Intensity:         5,
		RecommendedAction: "celebrate",
		Description:       "Pedri scores after a cutback.",
		Participants: []matchstate.Participant{
			{Role: "scorer", Name: "Pedri"},
			{Role: "assist", Name: "Olmo"},
			{Role: "pre_assist", Name: "Yamal"},
		},
	}
	snapshot := matchstate.Snapshot{
		MatchID:              "bench-prompt",
		HomeTeam:             "Spain",
		AwayTeam:             "Germany",
		Score:                matchstate.Score{Home: 1, Away: 1},
		Period:               "first_half",
		Clock:                "23:00",
		EmotionalTemperature: 4,
		RecentEvents: []matchstate.MatchEvent{
			{EventType: "shot", Clock: "22:10", TeamName: "Spain", Description: "A warning shot."},
			{EventType: "foul", Clock: "21:00", TeamName: "Germany", Description: "A tactical foul."},
		},
	}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = BuildProactiveEventMessages("system persona", ev, snapshot)
	}
}

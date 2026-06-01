package matchstate

import (
	"errors"
	"testing"
	"time"
)

func TestEvalBaselineFullMatchFlow(t *testing.T) {
	store := NewStore()
	matchID := "eval-baseline"

	config, snap, err := store.SetConfig(matchID, MatchConfig{
		HomeTeam: "Spain",
		AwayTeam: "Germany",
		HomePlayers: []Player{
			{Number: " 10 ", Name: " Pedri ", Position: " CM "},
			{Name: "  "},
		},
		AwayPlayers: []Player{{Number: "10", Name: "Musiala", Position: "AM"}},
	})
	if err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}
	if config.MatchID != matchID || snap.HomeTeam != "Spain" || snap.AwayTeam != "Germany" {
		t.Fatalf("config/snapshot mismatch: config=%+v snapshot=%+v", config, snap)
	}
	normalized := store.Config(matchID)
	if got := len(normalized.HomePlayers); got != 1 {
		t.Fatalf("expected empty players to be trimmed in normalized view, got %d", got)
	}

	events, unsubscribe := store.Subscribe(matchID)
	defer unsubscribe()

	created, snap, err := store.Create(matchID, MatchEvent{
		EventType:   "goal",
		Period:      "first_half",
		Clock:       "23:41",
		TeamID:      "home",
		TeamName:    "Spain",
		Score:       Score{Home: 1, Away: 0},
		Intensity:   5,
		Description: "Pedri scores from the edge of the box.",
		Participants: []Participant{
			{Role: "scorer", Name: "Pedri", TeamID: "home", TeamName: "Spain"},
			{Role: "assist", Name: "Olmo", TeamID: "home", TeamName: "Spain"},
			{Role: "pre_assist", Name: "Yamal", TeamID: "home", TeamName: "Spain"},
		},
	})
	if err != nil {
		t.Fatalf("Create goal error: %v", err)
	}
	if created.ID == "" || created.Status != "active" || created.RecommendedAction != "celebrate" || created.Sentiment != "celebratory" {
		t.Fatalf("created event not normalized as expected: %+v", created)
	}
	if created.PlayerName != "Pedri" {
		t.Fatalf("primary player should default from first participant, got %q", created.PlayerName)
	}
	if len(created.Participants) != 3 {
		t.Fatalf("participants not preserved: %+v", created.Participants)
	}
	if snap.Score != (Score{Home: 1, Away: 0}) || snap.Clock != "23:41" || snap.Momentum != "home_pressure" {
		t.Fatalf("snapshot did not absorb goal state: %+v", snap)
	}
	if len(snap.KeyEvents) != 1 || snap.KeyEvents[0].ID != created.ID {
		t.Fatalf("goal should be a key event: %+v", snap.KeyEvents)
	}

	select {
	case pushed := <-events:
		if pushed.ID != created.ID {
			t.Fatalf("subscriber received wrong event: %+v", pushed)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber did not receive created event")
	}
}

func TestEvalBoundariesNormalizeAndReject(t *testing.T) {
	store := NewStore()

	if _, _, err := store.SetConfig(" ", MatchConfig{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("blank match id should be invalid, got %v", err)
	}

	tests := []struct {
		name string
		ev   MatchEvent
	}{
		{name: "missing event type", ev: MatchEvent{Clock: "01:00", Description: "x"}},
		{name: "missing clock", ev: MatchEvent{EventType: "goal", Description: "x"}},
		{name: "missing description", ev: MatchEvent{EventType: "goal", Clock: "01:00"}},
		{name: "unsupported event", ev: MatchEvent{EventType: "alien_invasion", Clock: "01:00", Description: "x"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := store.Create("eval-boundary", tt.ev); !errors.Is(err, ErrInvalid) {
				t.Fatalf("expected ErrInvalid, got %v", err)
			}
		})
	}

	created, _, err := store.Create("eval-boundary", MatchEvent{
		EventType:   "shot",
		Clock:       "09:15",
		PlayerName:  "Pedri",
		TeamID:      "home",
		TeamName:    "Spain",
		Intensity:   99,
		Description: "Shot from distance.",
		Participants: []Participant{
			{Name: "   "},
		},
	})
	if err != nil {
		t.Fatalf("Create shot error: %v", err)
	}
	if created.Intensity != 5 {
		t.Fatalf("intensity should clamp to 5, got %d", created.Intensity)
	}
	if len(created.Participants) != 1 || created.Participants[0].Role != "shooter" || created.Participants[0].Name != "Pedri" {
		t.Fatalf("playerName fallback participant not created: %+v", created.Participants)
	}
}

func TestEvalCorrectionRevisesActiveSnapshot(t *testing.T) {
	store := NewStore()
	original, _, err := store.Create("eval-correction", MatchEvent{
		EventType:   "goal",
		Clock:       "12:00",
		TeamID:      "home",
		TeamName:    "Spain",
		PlayerName:  "Pedri",
		Score:       Score{Home: 1, Away: 0},
		Description: "Goal initially awarded.",
	})
	if err != nil {
		t.Fatalf("Create original error: %v", err)
	}

	replacement, snap, err := store.Correct("eval-correction", original.ID, MatchEvent{
		EventType:   "var_check",
		Clock:       "13:10",
		TeamID:      "home",
		TeamName:    "Spain",
		PlayerName:  "Pedri",
		Score:       Score{Home: 0, Away: 0},
		Description: "VAR overturns the goal.",
	})
	if err != nil {
		t.Fatalf("Correct error: %v", err)
	}
	if replacement.RevisionOf != original.ID || replacement.Status != "active" {
		t.Fatalf("replacement metadata wrong: %+v", replacement)
	}
	if snap.Score != (Score{Home: 0, Away: 0}) || snap.LastPublicDescription != "VAR overturns the goal." {
		t.Fatalf("snapshot should follow replacement only: %+v", snap)
	}
	events := store.Events("eval-correction")
	if len(events) != 2 {
		t.Fatalf("expected original + replacement events, got %d", len(events))
	}
	var correctedFound bool
	for _, ev := range events {
		if ev.ID == original.ID && ev.Status == "corrected" {
			correctedFound = true
		}
	}
	if !correctedFound {
		t.Fatalf("original event was not marked corrected: %+v", events)
	}
}

func BenchmarkEvalStoreCreateGoalEvent(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		store := NewStore()
		if _, _, err := store.SetConfig("bench-store", MatchConfig{HomeTeam: "Spain", AwayTeam: "Germany"}); err != nil {
			b.Fatalf("SetConfig error: %v", err)
		}
		for j := 0; j < 20; j++ {
			if _, _, err := store.Create("bench-store", MatchEvent{
				EventType:   "shot",
				Period:      "first_half",
				Clock:       "10:00",
				TeamID:      "home",
				TeamName:    "Spain",
				Score:       Score{Home: 0, Away: 0},
				Description: "Baseline history event.",
			}); err != nil {
				b.Fatalf("Create history error: %v", err)
			}
		}
		b.StartTimer()
		_, _, err := store.Create("bench-store", MatchEvent{
			EventType:   "goal",
			Period:      "first_half",
			Clock:       "23:41",
			TeamID:      "home",
			TeamName:    "Spain",
			Score:       Score{Home: 1 + i, Away: 0},
			Intensity:   5,
			Description: "Pedri scores from the edge of the box.",
			Participants: []Participant{
				{Role: "scorer", Name: "Pedri", TeamID: "home", TeamName: "Spain"},
				{Role: "assist", Name: "Olmo", TeamID: "home", TeamName: "Spain"},
				{Role: "pre_assist", Name: "Yamal", TeamID: "home", TeamName: "Spain"},
			},
		})
		if err != nil {
			b.Fatalf("Create error: %v", err)
		}
		b.StopTimer()
	}
}

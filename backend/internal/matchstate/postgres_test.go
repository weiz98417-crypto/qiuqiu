package matchstate

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestPostgresStoreIntegration(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set")
	}

	store, err := OpenPostgresStore(context.Background(), databaseURL, "../../migrations")
	if err != nil {
		t.Fatalf("OpenPostgresStore error: %v", err)
	}
	defer store.Close()

	matchID := "pg-eval-" + time.Now().UTC().Format("20060102150405")
	config, snapshot, err := store.SetConfig(matchID, MatchConfig{
		HomeTeam: "西班牙",
		AwayTeam: "德国",
		HomePlayers: []Player{
			{Number: "10", Name: "佩德里", Position: "CM"},
			{Number: "8", Name: "法比安", Position: "CM"},
		},
	})
	if err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}
	if config.MatchID != matchID || snapshot.HomeTeam != "西班牙" {
		t.Fatalf("config/snapshot mismatch: config=%+v snapshot=%+v", config, snapshot)
	}

	events, unsubscribe := store.Subscribe(matchID)
	defer unsubscribe()

	created, snapshot, err := store.Create(matchID, MatchEvent{
		EventType:   "goal",
		Period:      "first_half",
		Clock:       "23:41",
		TeamID:      "home",
		TeamName:    "西班牙",
		Score:       Score{Home: 1, Away: 0},
		Intensity:   5,
		Description: "佩德里破门。",
		Participants: []Participant{
			{Role: "scorer", Name: "佩德里", TeamID: "home", TeamName: "西班牙"},
			{Role: "assist", Name: "法比安", TeamID: "home", TeamName: "西班牙"},
		},
	})
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}
	if snapshot.Score != (Score{Home: 1, Away: 0}) || len(snapshot.KeyEvents) != 1 {
		t.Fatalf("snapshot did not include goal: %+v", snapshot)
	}
	if got := store.Events(matchID); len(got) != 1 || got[0].ID != created.ID || len(got[0].Participants) != 2 {
		t.Fatalf("stored event mismatch: %+v", got)
	}

	select {
	case pushed := <-events:
		if pushed.ID != created.ID {
			t.Fatalf("subscriber got wrong event: %+v", pushed)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber did not receive created event")
	}
}

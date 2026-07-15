package matchstate

import (
	"context"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestPostgresStoreIntegration(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set")
	}

	ctx := context.Background()
	store, err := OpenPostgresStore(ctx, databaseURL, "../../migrations")
	if err != nil {
		t.Fatalf("OpenPostgresStore error: %v", err)
	}
	defer store.Close()

	matchID := "pg-store-eval-" + time.Now().UTC().Format("20060102150405")
	if err := store.Reset(matchID); err != nil {
		t.Fatalf("Reset match error: %v", err)
	}
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
	if len(store.Config(matchID).HomePlayers) != 2 {
		t.Fatalf("players did not round-trip: %+v", store.Config(matchID).HomePlayers)
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
		Confirmed:   true,
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
	if got := store.Events(matchID); len(got) != 1 || got[0].ID != created.ID || !got[0].Confirmed || len(got[0].Participants) != 2 {
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

func TestPostgresSerializesConcurrentGoalTransitions(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set")
	}

	ctx := context.Background()
	store, err := OpenPostgresStore(ctx, databaseURL, "../../migrations")
	if err != nil {
		t.Fatalf("OpenPostgresStore error: %v", err)
	}
	defer store.Close()
	matchID := "pg-concurrent-goal-" + time.Now().UTC().Format("20060102150405.000000000")
	if err := store.Reset(matchID); err != nil {
		t.Fatalf("Reset match error: %v", err)
	}
	if _, _, err := store.SetConfig(matchID, MatchConfig{HomeTeam: "西班牙", AwayTeam: "德国"}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	for index := 0; index < 2; index++ {
		go func(index int) {
			<-start
			_, _, createErr := store.Create(matchID, MatchEvent{
				Source:          "api-sports",
				ProviderEventID: "concurrent-goal-" + strconv.Itoa(index),
				EventType:       "goal",
				Period:          "first_half",
				Clock:           "12:00",
				TeamID:          "home",
				TeamName:        "西班牙",
				PlayerName:      "佩德里",
				Score:           Score{Home: 1, Away: 0},
				Description:     "并发进球。",
			})
			results <- createErr
		}(index)
	}
	close(start)

	successes := 0
	invalid := 0
	for index := 0; index < 2; index++ {
		result := <-results
		switch {
		case result == nil:
			successes++
		case errors.Is(result, ErrInvalid):
			invalid++
		default:
			t.Fatalf("unexpected concurrent Create error: %v", result)
		}
	}
	if successes != 1 || invalid != 1 {
		t.Fatalf("concurrent results: successes=%d invalid=%d", successes, invalid)
	}
	if events := store.Events(matchID); len(events) != 1 {
		t.Fatalf("stored concurrent goals = %d, want 1", len(events))
	}
}

func TestPostgresPersistsCrossSourceConflictIntegrity(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	store, err := OpenPostgresStore(ctx, databaseURL, "../../migrations")
	if err != nil {
		t.Fatalf("OpenPostgresStore error: %v", err)
	}
	defer store.Close()
	matchID := "pg-source-conflict-" + time.Now().UTC().Format("20060102150405.000000000")
	if _, _, err := store.SetConfig(matchID, MatchConfig{HomeTeam: "西班牙", AwayTeam: "德国"}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}
	manual, _, err := store.Create(matchID, MatchEvent{
		Source: "operator", EventType: "goal", Period: "first_half", Clock: "23:41", TeamID: "home", TeamName: "西班牙", PlayerName: "佩德里",
		Score: Score{Home: 1, Away: 0}, Description: "佩德里进球。",
	})
	if err != nil {
		t.Fatalf("Create manual goal error: %v", err)
	}
	_, _, err = store.Create(matchID, MatchEvent{
		Source: "api-sports", ProviderEventID: "fixture-conflict", EventType: "goal", Period: "first_half", Clock: "24:00", TeamID: "away", TeamName: "德国", PlayerName: "穆西亚拉",
		Score: Score{Home: 1, Away: 1}, Description: "穆西亚拉进球。",
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("Create conflicting goal error = %v, want ErrConflict", err)
	}
	if snapshot := store.Snapshot(matchID); snapshot.Integrity.Status != "conflict" || snapshot.Score != (Score{Home: 1, Away: 0}) {
		t.Fatalf("conflict snapshot mismatch: %+v", snapshot)
	}
	if _, _, err := store.Correct(matchID, manual.ID, MatchEvent{
		EventType: "goal", Period: "first_half", Clock: "25:00", TeamID: "home", TeamName: "西班牙", PlayerName: "佩德里",
		Score: Score{Home: 1, Away: 0}, Description: "确认佩德里进球。",
	}); err != nil {
		t.Fatalf("Correct manual goal error: %v", err)
	}
	if snapshot := store.Snapshot(matchID); snapshot.Integrity.Status != "conflict" {
		t.Fatalf("unrelated correction cleared conflict: %+v", snapshot)
	}

	reopened, err := OpenPostgresStore(ctx, databaseURL, "../../migrations")
	if err != nil {
		t.Fatalf("reopen store error: %v", err)
	}
	defer reopened.Close()
	if snapshot := reopened.Snapshot(matchID); snapshot.Integrity.Status != "conflict" {
		t.Fatalf("conflict integrity did not survive reopen: %+v", snapshot)
	}
}

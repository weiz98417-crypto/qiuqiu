package matchstate

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestEventObserverRunsWithoutMatchSubscriber(t *testing.T) {
	store := NewStore()
	observed := make(chan MatchEvent, 1)
	store.SetEventObserver(func(event MatchEvent) error {
		observed <- event
		return nil
	})
	created, _, err := store.Create("observer-without-client", MatchEvent{
		EventType: "kickoff", Period: "first_half", Clock: "00:01", Description: "match started",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	select {
	case event := <-observed:
		if event.ID != created.ID || event.MatchID != "observer-without-client" {
			t.Fatalf("observed event = %+v, want created event", event)
		}
	case <-time.After(time.Second):
		t.Fatal("event observer was not called without a match subscriber")
	}
}

func TestUnsubscribeIsSafeDuringPublish(t *testing.T) {
	store := NewStore()
	const matchID = "subscription-race"
	var wait sync.WaitGroup
	for index := 0; index < 100; index++ {
		_, unsubscribe := store.Subscribe(matchID)
		wait.Add(1)
		go func(clock string) {
			defer wait.Done()
			if _, _, err := store.Create(matchID, MatchEvent{
				EventType:   "shot",
				Clock:       clock,
				Description: "shot",
			}); err != nil {
				t.Errorf("Create error: %v", err)
			}
		}(time.Now().Add(time.Duration(index) * time.Millisecond).Format("04:05"))
		unsubscribe()
		unsubscribe()
	}
	wait.Wait()
}

func TestPublicFactViewHidesProvisionalEvents(t *testing.T) {
	store := NewStore()
	if _, _, err := store.SetConfig("public-facts", MatchConfig{HomeTeam: "Arsenal", AwayTeam: "Liverpool"}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}
	created, _, err := store.Create("public-facts", MatchEvent{
		Source:      "api-sports",
		Period:      "first_half",
		Clock:       "12:00",
		EventType:   "goal",
		TeamID:      "home",
		TeamName:    "Arsenal",
		PlayerName:  "Saka",
		Score:       Score{Home: 1},
		Description: "Saka scored",
		Visibility:  "public",
	})
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}
	if created.FactStatus != FactStatusProvisional {
		t.Fatalf("fact status = %q, want %q", created.FactStatus, FactStatusProvisional)
	}
	if snapshot := store.PublicSnapshot("public-facts"); snapshot.Score != (Score{}) || len(snapshot.RecentEvents) != 0 {
		t.Fatalf("public snapshot exposed provisional fact: %+v", snapshot)
	}
	if events := store.PublicEvents("public-facts"); len(events) != 0 {
		t.Fatalf("public events = %+v, want none", events)
	}
}

func TestConfirmFactPublishesProvisionalEvent(t *testing.T) {
	store := NewStore()
	created, _, err := store.Create("confirm-fact", MatchEvent{
		Source:      "api-sports",
		Period:      "first_half",
		Clock:       "12:00",
		EventType:   "goal",
		TeamID:      "home",
		PlayerName:  "Saka",
		Score:       Score{Home: 1},
		Description: "Saka scored",
		Visibility:  "public",
	})
	if err != nil {
		t.Fatalf("Create provisional event: %v", err)
	}
	confirmed, snapshot, err := store.ConfirmFact("confirm-fact", created.FactID, "operator-1")
	if err != nil {
		t.Fatalf("ConfirmFact: %v", err)
	}
	if confirmed.FactStatus != FactStatusConfirmed || !confirmed.Confirmed || confirmed.ConfirmedBy != "operator-1" || confirmed.PublicAt == "" {
		t.Fatalf("confirmed fact = %+v", confirmed)
	}
	if snapshot.Score != (Score{Home: 1}) || len(snapshot.RecentEvents) != 1 {
		t.Fatalf("public snapshot after confirmation = %+v", snapshot)
	}
	revisions := store.FactRevisions("confirm-fact", created.FactID)
	if len(revisions) != 2 || revisions[0].Status != FactStatusProvisional || revisions[1].Status != FactStatusConfirmed {
		t.Fatalf("fact revisions = %+v", revisions)
	}
}

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

func TestGoalVARCancellationRevisesTheAuthoritativeScore(t *testing.T) {
	store := NewStore()
	matchID := "goal-var-cancel"
	if _, _, err := store.SetConfig(matchID, MatchConfig{HomeTeam: "利物浦", AwayTeam: "切尔西"}); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	goal, _, err := store.Create(matchID, MatchEvent{
		EventType: "goal", Period: "second_half", Clock: "78:10", TeamID: "home", Score: Score{Home: 1}, Description: "萨拉赫进球。",
	})
	if err != nil {
		t.Fatalf("Create goal: %v", err)
	}
	if _, _, err := store.Create(matchID, MatchEvent{
		EventType: "var_result", Period: "second_half", Clock: "78:40", Score: Score{Home: 1}, Description: "VAR确认越位。", RevisionOf: goal.ID,
	}); err != nil {
		t.Fatalf("Create VAR result: %v", err)
	}
	_, snapshot, err := store.Create(matchID, MatchEvent{
		EventType: "goal_cancelled", Period: "second_half", Clock: "78:45", TeamID: "home", Score: Score{}, Description: "进球取消。", RevisionOf: goal.ID,
	})
	if err != nil {
		t.Fatalf("Create goal cancellation: %v", err)
	}
	if snapshot.Score != (Score{}) {
		t.Fatalf("score after cancellation = %+v, want 0-0", snapshot.Score)
	}
	if len(snapshot.KeyEvents) < 3 || snapshot.KeyEvents[0].EventType != "goal_cancelled" {
		t.Fatalf("key events = %+v", snapshot.KeyEvents)
	}
}

func TestExternalProviderEventIsStoredOnce(t *testing.T) {
	store := NewStore()
	event := MatchEvent{
		Source:          "api-sports",
		ProviderName:    "api-sports",
		ProviderEventID: "fixture-42:shot:18:7",
		EventType:       "shot",
		Clock:           "18:00",
		Description:     "External goal event.",
	}
	if _, _, err := store.Create("provider-dedupe", event); err != nil {
		t.Fatalf("first Create error: %v", err)
	}
	if _, _, err := store.Create("provider-dedupe", event); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate Create error = %v, want ErrDuplicate", err)
	}
	if got := store.Events("provider-dedupe"); len(got) != 1 {
		t.Fatalf("stored events = %d, want 1", len(got))
	}
}

func TestCrossSourceGoalDoesNotDoubleCountOrHideConflict(t *testing.T) {
	store := NewStore()
	matchID := "cross-source-goal"
	if _, _, err := store.SetConfig(matchID, MatchConfig{HomeTeam: "西班牙", AwayTeam: "德国"}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}
	if _, _, err := store.Create(matchID, MatchEvent{
		Source:      "operator",
		FactID:      "manual-fact",
		EventType:   "goal",
		Period:      "first_half",
		Clock:       "23:41",
		TeamID:      "home",
		TeamName:    "西班牙",
		PlayerName:  "佩德里",
		Score:       Score{Home: 1, Away: 0},
		Description: "佩德里进球。",
	}); err != nil {
		t.Fatalf("Create manual goal error: %v", err)
	}

	_, _, err := store.Create(matchID, MatchEvent{
		Source:          "api-sports",
		ProviderEventID: "fixture-1:goal-1",
		EventType:       "goal",
		Period:          "first_half",
		Clock:           "24:00",
		TeamID:          "home",
		TeamName:        "西班牙",
		PlayerName:      "佩德里",
		Score:           Score{Home: 2, Away: 0},
		Description:     "佩德里 goal。",
	})
	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("same cross-source goal error = %v, want ErrDuplicate", err)
	}

	_, _, err = store.Create(matchID, MatchEvent{
		Source:          "api-sports",
		ProviderEventID: "fixture-1:goal-2",
		EventType:       "goal",
		Period:          "first_half",
		Clock:           "23:00",
		TeamID:          "away",
		TeamName:        "德国",
		PlayerName:      "穆西亚拉",
		Score:           Score{Home: 1, Away: 1},
		Description:     "穆西亚拉 goal。",
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting cross-source goal error = %v, want ErrConflict", err)
	}
	if snapshot := store.Snapshot(matchID); snapshot.Score != (Score{}) || len(snapshot.KeyEvents) != 0 || snapshot.Integrity.Status != "conflict" {
		t.Fatalf("cross-source conflict should leave no authoritative score: %+v", snapshot)
	}
	events := store.Events(matchID)
	if len(events) != 2 || events[0].FactStatus != FactStatusConflict || events[1].FactStatus != FactStatusConflict {
		t.Fatalf("conflict candidates = %+v", events)
	}
	for _, event := range events {
		if event.Source == "operator" && event.FactID != "manual-fact" {
			t.Fatalf("manual fact id changed on conflict: %+v", event)
		}
	}
}

func TestSubscriberReceivesBurstWithoutDroppingEvents(t *testing.T) {
	store := NewStore()
	events, unsubscribe := store.Subscribe("burst")
	defer unsubscribe()

	const eventCount = 64
	for index := 0; index < eventCount; index++ {
		if _, _, err := store.Create("burst", MatchEvent{
			EventType:   "shot",
			Clock:       time.Date(2026, 1, 1, 0, index, 0, 0, time.UTC).Format("04:05"),
			Description: "Burst event",
		}); err != nil {
			t.Fatalf("Create event %d: %v", index, err)
		}
	}

	for index := 0; index < eventCount; index++ {
		select {
		case event := <-events:
			if event.Description != "Burst event" {
				t.Fatalf("event %d = %+v", index, event)
			}
		case <-time.After(time.Second):
			t.Fatalf("subscriber received only %d/%d burst events", index, eventCount)
		}
	}
}

func TestAutomationPolicyPersistsAcrossMatchConfigUpdates(t *testing.T) {
	store := NewStore()
	matchID := "automation-policy"

	policy, err := store.SetAutomation(matchID, AutomationPolicy{
		Mode:            AutomationModePaused,
		EventTypes:      []string{"goal", "red_card"},
		CooldownSeconds: 12,
	})
	if err != nil {
		t.Fatalf("SetAutomation error: %v", err)
	}
	if policy.Mode != AutomationModePaused {
		t.Fatalf("automation mode = %q, want paused", policy.Mode)
	}

	if _, _, err := store.SetConfig(matchID, MatchConfig{HomeTeam: "Spain", AwayTeam: "Germany"}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}
	got := store.Config(matchID).Automation
	if got.Mode != AutomationModePaused || got.CooldownSeconds != 12 {
		t.Fatalf("automation policy changed after config update: %+v", got)
	}
	if len(got.EventTypes) != 2 || got.EventTypes[0] != "goal" || got.EventTypes[1] != "red_card" {
		t.Fatalf("automation event types changed: %+v", got.EventTypes)
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
		{name: "unsupported fact status", ev: MatchEvent{EventType: "shot", Clock: "01:00", Description: "x", FactStatus: "unknown"}},
		{name: "confidence out of range", ev: MatchEvent{EventType: "shot", Clock: "01:00", Description: "x", Confidence: 1.1}},
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

func TestRejectsGoalBeforeKickoff(t *testing.T) {
	store := NewStore()
	matchID := "fact-integrity-pre-match-goal"
	if _, _, err := store.SetConfig(matchID, MatchConfig{HomeTeam: "西班牙", AwayTeam: "德国"}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}

	for _, period := range []string{"pre_match", "PRE-MATCH"} {
		_, _, err := store.Create(matchID, MatchEvent{
			EventType:   "goal",
			Period:      period,
			Clock:       "00:00",
			TeamID:      "home",
			TeamName:    "西班牙",
			PlayerName:  "佩德里",
			Score:       Score{Home: 1, Away: 0},
			Description: "佩德里进球。",
		})
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("pre-match period %q goal error = %v, want ErrInvalid", period, err)
		}
	}
}

func TestRejectsGoalWithoutSingleTeamScoreIncrement(t *testing.T) {
	store := NewStore()
	matchID := "fact-integrity-goal-score"
	if _, _, err := store.SetConfig(matchID, MatchConfig{HomeTeam: "西班牙", AwayTeam: "德国"}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}

	_, _, err := store.Create(matchID, MatchEvent{
		EventType:   "goal",
		Period:      "first_half",
		Clock:       "12:00",
		TeamID:      "home",
		TeamName:    "西班牙",
		PlayerName:  "佩德里",
		Score:       Score{Home: 0, Away: 0},
		Description: "佩德里进球。",
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("unchanged goal score error = %v, want ErrInvalid", err)
	}
}

func TestRejectsScoreChangeWithoutGoal(t *testing.T) {
	store := NewStore()
	matchID := "fact-integrity-non-goal-score"
	if _, _, err := store.SetConfig(matchID, MatchConfig{HomeTeam: "西班牙", AwayTeam: "德国"}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}

	_, _, err := store.Create(matchID, MatchEvent{
		EventType:   "shot",
		Period:      "first_half",
		Clock:       "08:00",
		TeamID:      "home",
		TeamName:    "西班牙",
		PlayerName:  "佩德里",
		Score:       Score{Home: 1, Away: 0},
		Description: "佩德里远射。",
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("non-goal score change error = %v, want ErrInvalid", err)
	}
}

func TestRejectsScorerAssignedToWrongConfiguredTeam(t *testing.T) {
	store := NewStore()
	matchID := "fact-integrity-player-team"
	if _, _, err := store.SetConfig(matchID, MatchConfig{
		HomeTeam:    "西班牙",
		AwayTeam:    "德国",
		HomePlayers: []Player{{Name: "佩德里"}},
		AwayPlayers: []Player{{Name: "穆西亚拉"}},
	}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}

	_, _, err := store.Create(matchID, MatchEvent{
		EventType:   "goal",
		Period:      "first_half",
		Clock:       "19:00",
		TeamID:      "away",
		TeamName:    "德国",
		PlayerName:  "佩德里",
		Score:       Score{Home: 0, Away: 1},
		Description: "佩德里进球。",
		Participants: []Participant{
			{Role: "scorer", Name: "佩德里", TeamID: "away", TeamName: "德国"},
		},
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("wrong-team scorer error = %v, want ErrInvalid", err)
	}
}

func TestRejectsGoalWhosePlayerNameBypassesScorerParticipants(t *testing.T) {
	store := NewStore()
	matchID := "fact-integrity-player-name-team"
	if _, _, err := store.SetConfig(matchID, MatchConfig{
		HomeTeam:    "西班牙",
		AwayTeam:    "德国",
		HomePlayers: []Player{{Name: "佩德里"}},
		AwayPlayers: []Player{{Name: "穆西亚拉"}},
	}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}

	_, _, err := store.Create(matchID, MatchEvent{
		EventType:   "goal",
		Period:      "first_half",
		Clock:       "24:00",
		TeamID:      "away",
		TeamName:    "德国",
		PlayerName:  "佩德里",
		Score:       Score{Home: 0, Away: 1},
		Description: "进球。",
		Participants: []Participant{
			{Role: "assist", Name: "穆西亚拉", TeamID: "away", TeamName: "德国"},
		},
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("wrong-team playerName error = %v, want ErrInvalid", err)
	}
}

func TestAllowsGoalBeforeScorerIsKnown(t *testing.T) {
	store := NewStore()
	matchID := "fact-integrity-unknown-scorer"
	if _, _, err := store.SetConfig(matchID, MatchConfig{HomeTeam: "西班牙", AwayTeam: "德国"}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}
	created, snapshot, err := store.Create(matchID, MatchEvent{
		EventType:   "goal",
		Period:      "first_half",
		Clock:       "24:00",
		TeamID:      "home",
		TeamName:    "西班牙",
		Score:       Score{Home: 1, Away: 0},
		Description: "主队进球，球员待确认。",
	})
	if err != nil {
		t.Fatalf("Create anonymous goal error: %v", err)
	}
	if created.PlayerName != "" || len(created.Participants) != 0 || snapshot.Score != (Score{Home: 1, Away: 0}) {
		t.Fatalf("anonymous goal changed unexpectedly: event=%+v snapshot=%+v", created, snapshot)
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

func TestRejectedCorrectionKeepsOriginalFactActive(t *testing.T) {
	store := NewStore()
	matchID := "fact-integrity-rejected-correction"
	if _, _, err := store.SetConfig(matchID, MatchConfig{HomeTeam: "西班牙", AwayTeam: "德国"}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}
	original, _, err := store.Create(matchID, MatchEvent{
		EventType:   "goal",
		Period:      "first_half",
		Clock:       "21:00",
		TeamID:      "home",
		TeamName:    "西班牙",
		PlayerName:  "佩德里",
		Score:       Score{Home: 1, Away: 0},
		Description: "佩德里进球。",
	})
	if err != nil {
		t.Fatalf("Create original error: %v", err)
	}

	_, _, err = store.Correct(matchID, original.ID, MatchEvent{
		EventType: "var_check",
		Period:    "first_half",
		Clock:     "22:00",
		Score:     Score{Home: 0, Away: 0},
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid correction error = %v, want ErrInvalid", err)
	}
	if snapshot := store.Snapshot(matchID); snapshot.Score != (Score{Home: 1, Away: 0}) {
		t.Fatalf("snapshot changed after rejected correction: %+v", snapshot)
	}
	events := store.Events(matchID)
	if len(events) != 1 || events[0].Status != "active" {
		t.Fatalf("original fact should remain active: %+v", events)
	}
}

func TestCorrectionCannotRewriteScoreArbitrarily(t *testing.T) {
	store := NewStore()
	matchID := "fact-integrity-correction-score"
	if _, _, err := store.SetConfig(matchID, MatchConfig{HomeTeam: "西班牙", AwayTeam: "德国"}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}
	original, _, err := store.Create(matchID, MatchEvent{
		EventType:   "goal",
		Period:      "first_half",
		Clock:       "21:00",
		TeamID:      "home",
		TeamName:    "西班牙",
		PlayerName:  "佩德里",
		Score:       Score{Home: 1, Away: 0},
		Description: "佩德里进球。",
	})
	if err != nil {
		t.Fatalf("Create original error: %v", err)
	}

	_, _, err = store.Correct(matchID, original.ID, MatchEvent{
		EventType:   "operator_note",
		Period:      "first_half",
		Clock:       "22:00",
		Score:       Score{Home: 9, Away: 0},
		Description: "更正进球说明。",
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("arbitrary score correction error = %v, want ErrInvalid", err)
	}
	if snapshot := store.Snapshot(matchID); snapshot.Score != (Score{Home: 1, Away: 0}) {
		t.Fatalf("snapshot changed after arbitrary correction: %+v", snapshot)
	}
}

func TestCorrectionRejectsScoreChangeAfterLaterGoal(t *testing.T) {
	store := NewStore()
	matchID := "fact-integrity-correction-downstream"
	if _, _, err := store.SetConfig(matchID, MatchConfig{HomeTeam: "西班牙", AwayTeam: "德国"}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}
	first, _, err := store.Create(matchID, MatchEvent{
		EventType:   "goal",
		Period:      "first_half",
		Clock:       "10:00",
		TeamID:      "home",
		TeamName:    "西班牙",
		PlayerName:  "佩德里",
		Score:       Score{Home: 1, Away: 0},
		Description: "第一球。",
	})
	if err != nil {
		t.Fatalf("Create first goal error: %v", err)
	}
	if _, _, err := store.Create(matchID, MatchEvent{
		EventType:   "goal",
		Period:      "first_half",
		Clock:       "20:00",
		TeamID:      "home",
		TeamName:    "西班牙",
		PlayerName:  "亚马尔",
		Score:       Score{Home: 2, Away: 0},
		Description: "第二球。",
	}); err != nil {
		t.Fatalf("Create second goal error: %v", err)
	}

	_, _, err = store.Correct(matchID, first.ID, MatchEvent{
		EventType:   "goal",
		Period:      "first_half",
		Clock:       "21:00",
		TeamID:      "away",
		TeamName:    "德国",
		PlayerName:  "穆西亚拉",
		Score:       Score{Home: 1, Away: 1},
		Description: "第一球应归客队。",
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("downstream score correction error = %v, want ErrInvalid", err)
	}
	if snapshot := store.Snapshot(matchID); snapshot.Score != (Score{Home: 2, Away: 0}) {
		t.Fatalf("downstream correction changed snapshot: %+v", snapshot)
	}
}

func TestCorrectionRejectsScoreChangeAfterLaterNonGoal(t *testing.T) {
	store := NewStore()
	matchID := "fact-integrity-correction-later-shot"
	if _, _, err := store.SetConfig(matchID, MatchConfig{HomeTeam: "西班牙", AwayTeam: "德国"}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}
	goal, _, err := store.Create(matchID, MatchEvent{
		EventType: "goal", Period: "first_half", Clock: "10:00", TeamID: "home", TeamName: "西班牙", PlayerName: "佩德里",
		Score: Score{Home: 1, Away: 0}, Description: "进球。",
	})
	if err != nil {
		t.Fatalf("Create goal error: %v", err)
	}
	if _, _, err := store.Create(matchID, MatchEvent{
		EventType: "shot", Period: "first_half", Clock: "11:00", TeamID: "home", TeamName: "西班牙",
		Score: Score{Home: 1, Away: 0}, Description: "随后射门。",
	}); err != nil {
		t.Fatalf("Create shot error: %v", err)
	}
	_, _, err = store.Correct(matchID, goal.ID, MatchEvent{
		EventType: "var_check", Period: "first_half", Clock: "12:00", TeamID: "home", TeamName: "西班牙",
		Score: Score{Home: 0, Away: 0}, Description: "VAR取消进球。",
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("correction after later shot error = %v, want ErrInvalid", err)
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
			b.Fatalf("Create error: %v", err)
		}
		b.StopTimer()
	}
}

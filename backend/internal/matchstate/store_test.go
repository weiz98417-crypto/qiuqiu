package matchstate

import (
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestSetConfigPreservesTechnicalStatistics(t *testing.T) {
	store := NewStore()
	want := []MatchStatistic{{Key: "possessionPct", Label: "控球率", Home: 61.2, Away: 38.8, Unit: "%"}}
	if _, _, err := store.SetConfig("stats-match", MatchConfig{HomeTeam: "阿森纳", AwayTeam: "考文垂城", Stats: want}); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	if got := store.Config("stats-match").Stats; !reflect.DeepEqual(got, want) {
		t.Fatalf("Stats = %#v, want %#v", got, want)
	}
}

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

func TestCreateDiscardsDerivedAuditScores(t *testing.T) {
	store := NewStore()
	forgedReported := Score{Home: 7, Away: 6}
	forgedEffective := Score{Home: 9, Away: 8}
	created, _, err := store.Create("derived-score-input", MatchEvent{
		EventType:           "goal",
		Period:              "first_half",
		Clock:               "12:00",
		TeamID:              "home",
		Score:               Score{Home: 1},
		ReportedScore:       &forgedReported,
		EffectiveScoreAfter: &forgedEffective,
		Description:         "home goal",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ReportedScore != nil || created.EffectiveScoreAfter != nil {
		t.Fatalf("derived audit scores survived create: %+v", created)
	}
	events := store.Events("derived-score-input")
	if len(events) != 1 || events[0].ReportedScore != nil || events[0].EffectiveScoreAfter != nil {
		t.Fatalf("stored event retained derived audit scores: %+v", events)
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
	if _, err := store.SetClock(matchID, ClockCommand{
		Action: ClockActionSet, Period: "first_half", ElapsedSeconds: intPointer(23*60 + 41), ExpectedVersion: 0,
	}); err != nil {
		t.Fatalf("SetClock error: %v", err)
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
		EventType: "var_result", Period: "second_half", Clock: "78:35", Score: Score{Home: 1}, Description: "VAR确认越位。",
	}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unreferenced VAR result error = %v, want ErrInvalid", err)
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

func TestGoalCancellationMustReferenceTheActiveGoalItRemoves(t *testing.T) {
	store := NewStore()
	matchID := "goal-cancellation-reference"
	if _, _, err := store.SetConfig(matchID, MatchConfig{HomeTeam: "利物浦", AwayTeam: "切尔西"}); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	if _, _, err := store.Create(matchID, MatchEvent{
		EventType: "goal", Period: "second_half", Clock: "78:00", TeamID: "home", Score: Score{Home: 1}, Description: "萨拉赫进球。",
	}); err != nil {
		t.Fatalf("Create goal: %v", err)
	}
	note, _, err := store.Create(matchID, MatchEvent{
		EventType: "operator_note", Period: "second_half", Clock: "78:10", Score: Score{Home: 1}, Description: "VAR开始检查。",
	})
	if err != nil {
		t.Fatalf("Create note: %v", err)
	}
	_, _, err = store.Create(matchID, MatchEvent{
		EventType: "goal_cancelled", Period: "second_half", Clock: "78:45", TeamID: "home", Score: Score{}, Description: "进球取消。", RevisionOf: note.ID,
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("goal cancellation reference error = %v, want ErrInvalid", err)
	}
	if snapshot := store.PublicSnapshot(matchID); snapshot.Score != (Score{Home: 1}) || len(snapshot.RecentEvents) != 2 {
		t.Fatalf("invalid cancellation changed public state: %+v", snapshot)
	}
}

func TestScoreCorrectionRequiresReasonAndOverridesThePublicScore(t *testing.T) {
	store := NewStore()
	matchID := "audited-score-correction"
	if _, _, err := store.SetConfig(matchID, MatchConfig{HomeTeam: "利物浦", AwayTeam: "切尔西"}); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	if _, _, err := store.Create(matchID, MatchEvent{
		EventType: "goal", Period: "first_half", Clock: "12:00", TeamID: "home", Score: Score{Home: 1}, Description: "主队进球。",
	}); err != nil {
		t.Fatalf("Create goal: %v", err)
	}
	_, _, err := store.Create(matchID, MatchEvent{
		EventType: "score_correction", Period: "first_half", Clock: "12:20", Score: Score{}, Description: "比分更正为0比0。",
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing score correction reason error = %v, want ErrInvalid", err)
	}
	corrected, snapshot, err := store.Create(matchID, MatchEvent{
		EventType: "score_correction", Period: "first_half", Clock: "12:20", Score: Score{}, Description: "比分更正为0比0。",
		Evidence: map[string]any{"correctionReason": "现场记分牌回退，原进球无效。"}, ProactiveText: "__quiet__",
	})
	if err != nil {
		t.Fatalf("Create score correction: %v", err)
	}
	if corrected.EventType != "score_correction" || snapshot.Score != (Score{}) || snapshot.LastPublicDescription != "比分更正为0比0。" {
		t.Fatalf("score correction result = %+v / %+v", corrected, snapshot)
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
	if snapshot := store.Snapshot(matchID); snapshot.Score != (Score{Home: 1}) || len(snapshot.KeyEvents) != 1 || snapshot.Integrity.Status != "conflict" {
		t.Fatalf("cross-source conflict should preserve the accepted score: %+v", snapshot)
	}
	events := store.Events(matchID)
	if len(events) != 2 || events[0].FactStatus != FactStatusConflict || events[1].FactStatus != FactStatusConfirmed {
		t.Fatalf("conflict candidates = %+v", events)
	}
	if public := store.PublicEvents(matchID); len(public) != 1 || public[0].FactID != "manual-fact" {
		t.Fatalf("public facts changed during open conflict: %+v", public)
	}
	if _, _, err := store.Correct(matchID, events[1].ID, MatchEvent{
		Source: "operator", EventType: "goal", Period: "first_half", Clock: "25:00", TeamID: "home",
		TeamName: "西班牙", PlayerName: "佩德里", Score: Score{Home: 1}, Description: "试图脱离冲突关系。",
	}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("detaching conflict correction error = %v, want ErrInvalid", err)
	}
	for _, event := range events {
		if event.Source == "operator" && event.FactID != "manual-fact" {
			t.Fatalf("manual fact id changed on conflict: %+v", event)
		}
	}
	reconciled, snapshot, err := store.ReconcileFact(matchID, events[0].FactID, "operator-1")
	if err != nil {
		t.Fatalf("ReconcileFact: %v", err)
	}
	if reconciled.FactStatus != FactStatusReconciled || snapshot.Score != (Score{Away: 1}) || snapshot.Integrity.Status != "ok" {
		t.Fatalf("reconciled event/snapshot = %+v / %+v", reconciled, snapshot)
	}
	if public := store.PublicEvents(matchID); len(public) != 1 || public[0].FactID != reconciled.FactID {
		t.Fatalf("public facts after reconciliation = %+v", public)
	}
	if conflicts := store.FactConflicts(matchID); len(conflicts) != 1 || conflicts[0].Status != ConflictStatusResolved || conflicts[0].ChosenFactID != reconciled.FactID {
		t.Fatalf("legacy reconciliation did not resolve formal conflict: %+v", conflicts)
	}
}

func TestRevokingLastConflictCandidateRestoresIntegrity(t *testing.T) {
	store := NewStore()
	matchID := "reject-conflict-candidate"
	if _, _, err := store.Create(matchID, MatchEvent{
		Source: "operator", EventType: "goal", Period: "first_half", Clock: "20:00", TeamID: "home",
		Score: Score{Home: 1}, Description: "主队进球。",
	}); err != nil {
		t.Fatalf("Create accepted goal: %v", err)
	}
	_, _, err := store.Create(matchID, MatchEvent{
		Source: "provider", EventType: "goal", Period: "first_half", Clock: "20:10", TeamID: "away",
		Score: Score{Home: 1, Away: 1}, Description: "冲突候选。",
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("Create conflict candidate error = %v", err)
	}
	candidate := store.Events(matchID)[0]
	_, snapshot, err := store.RevokeFact(matchID, candidate.FactID, "operator-1")
	if err != nil {
		t.Fatalf("Revoke conflict candidate: %v", err)
	}
	if snapshot.Score != (Score{Home: 1}) || snapshot.Integrity.Status != "ok" {
		t.Fatalf("snapshot after rejecting conflict = %+v", snapshot)
	}
	if conflicts := store.FactConflicts(matchID); len(conflicts) != 1 || conflicts[0].Status != ConflictStatusResolved {
		t.Fatalf("legacy candidate rejection did not resolve formal conflict: %+v", conflicts)
	}
}

func TestConflictSetRequiresExplicitResolution(t *testing.T) {
	store := NewStore()
	matchID := "explicit-conflict-resolution"
	accepted, _, err := store.Create(matchID, MatchEvent{
		Source: "operator", EventType: "goal", Period: "first_half", Clock: "20:00", TeamID: "home",
		Score: Score{Home: 1}, Description: "主队进球。",
	})
	if err != nil {
		t.Fatalf("Create accepted goal: %v", err)
	}
	_, _, err = store.Create(matchID, MatchEvent{
		Source: "provider", EventType: "goal", Period: "first_half", Clock: "20:10", TeamID: "away",
		Score: Score{Away: 1}, Description: "客队冲突候选。",
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("Create conflict candidate error = %v, want ErrConflict", err)
	}

	conflicts := store.FactConflicts(matchID)
	if len(conflicts) != 1 || conflicts[0].Status != ConflictStatusOpen {
		t.Fatalf("open conflicts = %+v", conflicts)
	}
	conflict := conflicts[0]
	if len(conflict.Members) != 2 {
		t.Fatalf("conflict members = %+v", conflict.Members)
	}
	roles := map[string]ConflictMemberRole{}
	for _, member := range conflict.Members {
		roles[member.FactID] = member.Role
	}
	if roles[accepted.FactID] != ConflictMemberAccepted {
		t.Fatalf("accepted member role = %q", roles[accepted.FactID])
	}
	var candidateFactID string
	for factID, role := range roles {
		if role == ConflictMemberCandidate {
			candidateFactID = factID
		}
	}
	if candidateFactID == "" {
		t.Fatalf("candidate member missing: %+v", conflict.Members)
	}

	resolved, chosen, snapshot, err := store.ResolveFactConflict(matchID, conflict.ID, accepted.FactID, "operator-1", "保留现场人工记录")
	if err != nil {
		t.Fatalf("ResolveFactConflict: %v", err)
	}
	if resolved.Status != ConflictStatusResolved || resolved.ChosenFactID != accepted.FactID || resolved.ResolvedBy != "operator-1" {
		t.Fatalf("resolved conflict = %+v", resolved)
	}
	if chosen.FactID != accepted.FactID || chosen.FactStatus != FactStatusConfirmed {
		t.Fatalf("chosen fact = %+v", chosen)
	}
	if snapshot.Score != (Score{Home: 1}) || snapshot.Integrity.Status != "ok" {
		t.Fatalf("resolved snapshot = %+v", snapshot)
	}
	for _, event := range store.Events(matchID) {
		if event.FactID == candidateFactID && event.FactStatus != FactStatusRevoked {
			t.Fatalf("candidate event = %+v", event)
		}
	}
}

func TestResolvingConflictPublishesRetractionsAndProjectedFacts(t *testing.T) {
	t.Run("adopting candidate publishes effective score", func(t *testing.T) {
		store := NewStore()
		matchID := "projected-conflict-signal"
		if _, _, err := store.Create(matchID, MatchEvent{
			Source: "operator", EventType: "goal", Period: "first_half", Clock: "20:00", TeamID: "home",
			Score: Score{Home: 1}, Description: "accepted home goal",
		}); err != nil {
			t.Fatalf("Create accepted goal: %v", err)
		}
		updates, unsubscribe := store.Subscribe(matchID)
		defer unsubscribe()
		_, _, err := store.Create(matchID, MatchEvent{
			Source: "provider", EventType: "goal", Period: "first_half", Clock: "20:10", TeamID: "away",
			Score: Score{Home: 7, Away: 4}, Description: "candidate with an untrusted reported score",
		})
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("Create conflict candidate error = %v, want ErrConflict", err)
		}
		<-updates // The candidate notification is not the resolution signal under test.
		conflict := store.FactConflicts(matchID)[0]
		candidateFactID := conflictCandidateFact(t, conflict)
		if _, _, _, err := store.ResolveFactConflict(matchID, conflict.ID, candidateFactID, "operator-1", "adopt verified candidate"); err != nil {
			t.Fatalf("ResolveFactConflict: %v", err)
		}
		first := waitForMatchEvent(t, updates)
		second := waitForMatchEvent(t, updates)
		if first.FactStatus != FactStatusRevoked {
			t.Fatalf("first resolution event = %+v, want retraction", first)
		}
		if second.FactID != candidateFactID || second.FactStatus != FactStatusReconciled || second.Score != (Score{Away: 1}) {
			t.Fatalf("published resolution event = %+v", second)
		}
	})

	t.Run("keeping accepted fact publishes candidate retraction", func(t *testing.T) {
		store := NewStore()
		matchID := "quiet-conflict-rejection"
		accepted, _, err := store.Create(matchID, MatchEvent{
			Source: "operator", EventType: "goal", Period: "first_half", Clock: "20:00", TeamID: "home",
			Score: Score{Home: 1}, Description: "accepted home goal",
		})
		if err != nil {
			t.Fatalf("Create accepted goal: %v", err)
		}
		updates, unsubscribe := store.Subscribe(matchID)
		defer unsubscribe()
		_, _, err = store.Create(matchID, MatchEvent{
			Source: "provider", EventType: "goal", Period: "first_half", Clock: "20:10", TeamID: "away",
			Score: Score{Away: 1}, Description: "conflicting candidate",
		})
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("Create conflict candidate error = %v, want ErrConflict", err)
		}
		<-updates
		conflict := store.FactConflicts(matchID)[0]
		if _, _, _, err := store.ResolveFactConflict(matchID, conflict.ID, accepted.FactID, "operator-1", "keep accepted fact"); err != nil {
			t.Fatalf("ResolveFactConflict: %v", err)
		}
		select {
		case event := <-updates:
			if event.FactStatus != FactStatusRevoked {
				t.Fatalf("keeping accepted fact published %+v, want retraction", event)
			}
		case <-time.After(time.Second):
			t.Fatal("keeping accepted fact did not publish candidate retraction")
		}
	})
}

func waitForMatchEvent(t *testing.T, updates <-chan MatchEvent) MatchEvent {
	t.Helper()
	select {
	case event := <-updates:
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for match event")
		return MatchEvent{}
	}
}

func TestBridgeCandidateMergesIntersectingOpenConflictSets(t *testing.T) {
	store := NewStore()
	matchID := "bridge-conflict-sets"
	store.factConflicts[matchID] = []FactConflict{
		{
			ID: "conflict-a", MatchID: matchID, Status: ConflictStatusOpen, DetectedAt: "2026-07-20T10:00:00Z",
			Members: []FactConflictMember{{FactID: "accepted-a", Role: ConflictMemberAccepted}, {FactID: "candidate-a", Role: ConflictMemberCandidate}},
		},
		{
			ID: "conflict-b", MatchID: matchID, Status: ConflictStatusOpen, DetectedAt: "2026-07-20T10:01:00Z",
			Members: []FactConflictMember{{FactID: "accepted-b", Role: ConflictMemberAccepted}, {FactID: "candidate-b", Role: ConflictMemberCandidate}},
		},
	}
	events := []MatchEvent{
		{FactID: "accepted-a", Status: "active", Visibility: "public", FactStatus: FactStatusConfirmed},
		{FactID: "accepted-b", Status: "active", Visibility: "public", FactStatus: FactStatusConfirmed},
	}
	store.recordFactConflictLocked(matchID, events, []int{0, 1}, MatchEvent{FactID: "bridge-candidate"}, "2026-07-20T10:02:00Z")

	conflicts := store.FactConflicts(matchID)
	if len(conflicts) != 1 || conflicts[0].ID != "conflict-a" || conflicts[0].Status != ConflictStatusOpen {
		t.Fatalf("merged conflicts = %+v", conflicts)
	}
	roles := make(map[string]ConflictMemberRole)
	for _, member := range conflicts[0].Members {
		roles[member.FactID] = member.Role
	}
	if len(roles) != 5 || roles["accepted-a"] != ConflictMemberAccepted || roles["accepted-b"] != ConflictMemberAccepted || roles["bridge-candidate"] != ConflictMemberCandidate {
		t.Fatalf("merged conflict members = %+v", conflicts[0].Members)
	}
}

func TestBridgeConflictResolutionPreservesCompatibleAcceptedFacts(t *testing.T) {
	store := NewStore()
	matchID := "bridge-resolution-compatible-facts"
	first, _, err := store.Create(matchID, MatchEvent{
		Source: "operator", EventType: "goal", Period: "first_half", Clock: "10:00", TeamID: "home",
		Score: Score{Home: 1}, Description: "first accepted goal",
	})
	if err != nil {
		t.Fatalf("Create first accepted goal: %v", err)
	}
	second, _, err := store.Create(matchID, MatchEvent{
		Source: "operator", EventType: "goal", Period: "first_half", Clock: "10:30", TeamID: "home",
		Score: Score{Home: 2}, Description: "second accepted goal",
	})
	if err != nil {
		t.Fatalf("Create second accepted goal: %v", err)
	}
	_, _, err = store.Create(matchID, MatchEvent{
		Source: "provider", EventType: "goal", Period: "first_half", Clock: "10:15", TeamID: "away",
		Score: Score{Home: 1, Away: 1}, Description: "bridge candidate",
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("Create bridge candidate error = %v, want ErrConflict", err)
	}
	conflicts := store.FactConflicts(matchID)
	if len(conflicts) != 1 || len(conflicts[0].Edges) != 2 {
		t.Fatalf("bridge conflict = %+v, want two direct conflict edges", conflicts)
	}
	resolved, changed, snapshot, err := store.ResolveFactConflictSelection(matchID, conflicts[0].ID, []string{first.FactID, second.FactID}, "operator-1", "保留两次已确认进球")
	if err != nil {
		t.Fatalf("ResolveFactConflictSelection: %v", err)
	}
	if resolved.Status != ConflictStatusResolved || len(resolved.SelectedFactIDs) != 2 || len(changed) != 0 {
		t.Fatalf("resolution = %+v, changed = %+v", resolved, changed)
	}
	if snapshot.Score != (Score{Home: 2}) {
		t.Fatalf("snapshot after keeping compatible facts = %+v", snapshot)
	}
	for _, event := range store.Events(matchID) {
		if event.FactID == first.FactID || event.FactID == second.FactID {
			if event.FactStatus != FactStatusConfirmed {
				t.Fatalf("accepted fact was revoked: %+v", event)
			}
		}
	}
}

func TestConflictSelectionLeavesIndependentEdgesOpen(t *testing.T) {
	store := NewStore()
	matchID := "partial-conflict-graph-resolution"
	now := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	store.events[matchID] = []MatchEvent{
		{ID: "event-a", MatchID: matchID, Source: "operator", Period: "first_half", Clock: "10:00", EventType: "goal", TeamID: "home", Score: Score{Home: 1}, Description: "accepted A", Status: "active", Visibility: "public", FactID: "accepted-a", FactRevision: 1, FactStatus: FactStatusConfirmed, Confirmed: true, CreatedAt: now, RecordedSequence: 1},
		{ID: "event-x", MatchID: matchID, Source: "provider-x", Period: "first_half", Clock: "10:10", EventType: "goal", TeamID: "away", Score: Score{Away: 1}, Description: "candidate X", Status: "active", Visibility: "public", FactID: "candidate-x", FactRevision: 1, FactStatus: FactStatusConflict, CreatedAt: now, RecordedSequence: 2},
		{ID: "event-b", MatchID: matchID, Source: "operator", Period: "first_half", Clock: "20:00", EventType: "goal", TeamID: "home", Score: Score{Home: 2}, Description: "accepted B", Status: "active", Visibility: "public", FactID: "accepted-b", FactRevision: 1, FactStatus: FactStatusConfirmed, Confirmed: true, CreatedAt: now, RecordedSequence: 3},
		{ID: "event-y", MatchID: matchID, Source: "provider-y", Period: "first_half", Clock: "20:10", EventType: "goal", TeamID: "away", Score: Score{Home: 1, Away: 1}, Description: "candidate Y", Status: "active", Visibility: "public", FactID: "candidate-y", FactRevision: 1, FactStatus: FactStatusConflict, CreatedAt: now, RecordedSequence: 4},
	}
	store.factConflicts[matchID] = []FactConflict{{
		ID: "conflict-graph", MatchID: matchID, Status: ConflictStatusOpen, DetectedAt: now,
		Members: []FactConflictMember{
			{FactID: "accepted-a", Role: ConflictMemberAccepted},
			{FactID: "candidate-x", Role: ConflictMemberCandidate},
			{FactID: "accepted-b", Role: ConflictMemberAccepted},
			{FactID: "candidate-y", Role: ConflictMemberCandidate},
		},
		Edges: []FactConflictEdge{
			{LeftFactID: "accepted-a", RightFactID: "candidate-x"},
			{LeftFactID: "accepted-b", RightFactID: "candidate-y"},
		},
	}}
	store.configs[matchID] = MatchConfig{MatchID: matchID, Integrity: MatchIntegrity{Status: "conflict"}}

	conflict, changed, snapshot, err := store.ResolveFactConflictSelection(matchID, "conflict-graph", []string{"candidate-x"}, "operator-1", "采用候选 X")
	if err != nil {
		t.Fatalf("ResolveFactConflictSelection: %v", err)
	}
	if conflict.Status != ConflictStatusOpen || len(conflict.Edges) != 1 || conflict.Edges[0].LeftFactID != "accepted-b" || conflict.Edges[0].RightFactID != "candidate-y" {
		t.Fatalf("remaining conflict = %+v", conflict)
	}
	if len(changed) != 1 || changed[0].FactID != "candidate-x" || changed[0].Score != (Score{Away: 1}) {
		t.Fatalf("published events = %+v", changed)
	}
	if snapshot.Score != (Score{Home: 1, Away: 1}) || snapshot.Integrity.Status != "conflict" {
		t.Fatalf("snapshot after partial resolution = %+v", snapshot)
	}
	statuses := make(map[string]FactStatus)
	for _, event := range store.Events(matchID) {
		statuses[event.FactID] = event.FactStatus
	}
	if statuses["accepted-a"] != FactStatusRevoked || statuses["candidate-x"] != FactStatusReconciled || statuses["accepted-b"] != FactStatusConfirmed || statuses["candidate-y"] != FactStatusConflict {
		t.Fatalf("fact statuses after partial resolution = %+v", statuses)
	}

	conflict, changed, snapshot, err = store.ResolveFactConflictSelection(matchID, "conflict-graph", []string{"accepted-b"}, "operator-2", "保留已确认事实 B")
	if err != nil {
		t.Fatalf("final ResolveFactConflictSelection: %v", err)
	}
	if conflict.Status != ConflictStatusResolved || !reflect.DeepEqual(conflict.SelectedFactIDs, []string{"accepted-b", "candidate-x"}) {
		t.Fatalf("final conflict selection history = %+v", conflict)
	}
	if len(changed) != 0 || snapshot.Score != (Score{Home: 1, Away: 1}) || snapshot.Integrity.Status != "ok" {
		t.Fatalf("final changed = %+v, snapshot = %+v", changed, snapshot)
	}
}

func TestConflictSelectionReleasesUnselectedCandidateWithoutRemainingEdges(t *testing.T) {
	store := NewStore()
	matchID := "conflict-graph-orphan-release"
	now := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	store.events[matchID] = []MatchEvent{
		{ID: "event-a", MatchID: matchID, Source: "operator", Period: "first_half", Clock: "10:00", EventType: "goal", TeamID: "home", Score: Score{Home: 1}, Description: "accepted A", Status: "active", Visibility: "public", FactID: "accepted-a", FactRevision: 1, FactStatus: FactStatusConfirmed, Confirmed: true, CreatedAt: now, RecordedSequence: 1},
		{ID: "event-b", MatchID: matchID, Source: "provider-b", Period: "first_half", Clock: "10:05", EventType: "goal", TeamID: "away", Score: Score{Away: 1}, Description: "candidate B", Status: "active", Visibility: "public", FactID: "candidate-b", FactRevision: 1, FactStatus: FactStatusConflict, CreatedAt: now, RecordedSequence: 2},
		{ID: "event-c", MatchID: matchID, Source: "provider-c", Period: "first_half", Clock: "10:10", EventType: "goal", TeamID: "home", Score: Score{Home: 2}, Description: "candidate C", Status: "active", Visibility: "public", FactID: "candidate-c", FactRevision: 1, FactStatus: FactStatusConflict, CreatedAt: now, RecordedSequence: 3},
	}
	store.factConflicts[matchID] = []FactConflict{{
		ID: "chain-conflict", MatchID: matchID, Status: ConflictStatusOpen, DetectedAt: now,
		Members: []FactConflictMember{
			{FactID: "accepted-a", Role: ConflictMemberAccepted},
			{FactID: "candidate-b", Role: ConflictMemberCandidate},
			{FactID: "candidate-c", Role: ConflictMemberCandidate},
		},
		Edges: []FactConflictEdge{
			{LeftFactID: "accepted-a", RightFactID: "candidate-b"},
			{LeftFactID: "candidate-b", RightFactID: "candidate-c"},
		},
	}}
	store.configs[matchID] = MatchConfig{MatchID: matchID, Integrity: MatchIntegrity{Status: "conflict"}}

	conflict, changed, snapshot, err := store.ResolveFactConflictSelection(matchID, "chain-conflict", []string{"accepted-a"}, "operator-1", "保留已确认事实")
	if err != nil {
		t.Fatalf("ResolveFactConflictSelection: %v", err)
	}
	if conflict.Status != ConflictStatusResolved || len(changed) != 0 || snapshot.Score != (Score{Home: 1}) {
		t.Fatalf("conflict = %+v, changed = %+v, snapshot = %+v", conflict, changed, snapshot)
	}
	statuses := make(map[string]FactStatus)
	for _, event := range store.Events(matchID) {
		statuses[event.FactID] = event.FactStatus
	}
	if statuses["accepted-a"] != FactStatusConfirmed || statuses["candidate-b"] != FactStatusRevoked || statuses["candidate-c"] != FactStatusProvisional {
		t.Fatalf("fact statuses after chain resolution = %+v", statuses)
	}
}

func TestBridgeConflictMergePreservesPartialSelectionHistory(t *testing.T) {
	store := NewStore()
	matchID := "bridge-conflict-selection-history"
	store.factConflicts[matchID] = []FactConflict{
		{
			ID: "conflict-a", MatchID: matchID, Status: ConflictStatusOpen,
			Members:         []FactConflictMember{{FactID: "accepted-a", Role: ConflictMemberAccepted}, {FactID: "candidate-a", Role: ConflictMemberCandidate}},
			Edges:           []FactConflictEdge{{LeftFactID: "accepted-a", RightFactID: "candidate-a"}},
			SelectedFactIDs: []string{"accepted-a"},
			Reason:          "partial A", ResolvedBy: "operator-a",
		},
		{
			ID: "conflict-b", MatchID: matchID, Status: ConflictStatusOpen,
			Members:         []FactConflictMember{{FactID: "accepted-b", Role: ConflictMemberAccepted}, {FactID: "candidate-b", Role: ConflictMemberCandidate}},
			Edges:           []FactConflictEdge{{LeftFactID: "accepted-b", RightFactID: "candidate-b"}},
			SelectedFactIDs: []string{"accepted-b"},
			Reason:          "partial B", ResolvedBy: "operator-b",
		},
	}
	events := []MatchEvent{
		{FactID: "accepted-a", Status: "active", Visibility: "public", FactStatus: FactStatusConfirmed},
		{FactID: "accepted-b", Status: "active", Visibility: "public", FactStatus: FactStatusConfirmed},
	}
	store.recordFactConflictLocked(matchID, events, []int{0, 1}, MatchEvent{FactID: "bridge-candidate"}, time.Now().UTC().Format(time.RFC3339Nano))
	conflicts := store.FactConflicts(matchID)
	if len(conflicts) != 1 || !reflect.DeepEqual(conflicts[0].SelectedFactIDs, []string{"accepted-a", "accepted-b"}) || conflicts[0].Reason != "partial A" || conflicts[0].ResolvedBy != "operator-a" {
		t.Fatalf("merged selection history = %+v", conflicts)
	}
}

func TestResolvingOneConflictDoesNotChangeAnotherOpenConflict(t *testing.T) {
	store := NewStore()
	matchID := "scoped-conflict-resolution"
	firstAccepted, _, err := store.Create(matchID, MatchEvent{
		Source: "operator", EventType: "goal", Period: "first_half", Clock: "10:00", TeamID: "home",
		Score: Score{Home: 1}, Description: "第一组原事实。",
	})
	if err != nil {
		t.Fatalf("Create first accepted goal: %v", err)
	}
	_, _, err = store.Create(matchID, MatchEvent{
		Source: "provider-a", EventType: "goal", Period: "first_half", Clock: "10:10", TeamID: "away",
		Score: Score{Away: 1}, Description: "第一组候选。",
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("Create first candidate error = %v, want ErrConflict", err)
	}
	secondAccepted, _, err := store.Create(matchID, MatchEvent{
		Source: "operator", EventType: "goal", Period: "first_half", Clock: "30:00", TeamID: "home",
		Score: Score{Home: 2}, Description: "第二组原事实。",
	})
	if err != nil {
		t.Fatalf("Create second accepted goal: %v", err)
	}
	_, _, err = store.Create(matchID, MatchEvent{
		Source: "provider-b", EventType: "goal", Period: "first_half", Clock: "30:10", TeamID: "away",
		Score: Score{Home: 1, Away: 1}, Description: "第二组候选。",
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("Create second candidate error = %v, want ErrConflict", err)
	}

	conflicts := store.FactConflicts(matchID)
	if len(conflicts) != 2 {
		t.Fatalf("open conflicts = %+v", conflicts)
	}
	firstConflict := conflictContainingFact(t, conflicts, firstAccepted.FactID)
	secondConflict := conflictContainingFact(t, conflicts, secondAccepted.FactID)
	firstCandidate := conflictCandidateFact(t, firstConflict)
	secondCandidate := conflictCandidateFact(t, secondConflict)

	resolved, _, snapshot, err := store.ResolveFactConflict(matchID, firstConflict.ID, firstCandidate, "operator-1", "采用第一组外部源")
	if err != nil {
		t.Fatalf("Resolve first conflict: %v", err)
	}
	if resolved.ChosenFactID != firstCandidate || snapshot.Score != (Score{Home: 1, Away: 1}) || snapshot.Integrity.Status != "conflict" {
		t.Fatalf("first resolution result = %+v / %+v", resolved, snapshot)
	}

	after := store.FactConflicts(matchID)
	stillOpen := conflictContainingFact(t, after, secondAccepted.FactID)
	if stillOpen.Status != ConflictStatusOpen {
		t.Fatalf("second conflict = %+v", stillOpen)
	}
	for _, event := range store.Events(matchID) {
		if event.FactID == secondAccepted.FactID && event.FactStatus != FactStatusConfirmed {
			t.Fatalf("second accepted fact changed: %+v", event)
		}
		if event.FactID == secondCandidate && event.FactStatus != FactStatusConflict {
			t.Fatalf("second candidate changed: %+v", event)
		}
	}
}

func conflictContainingFact(t *testing.T, conflicts []FactConflict, factID string) FactConflict {
	t.Helper()
	for _, conflict := range conflicts {
		for _, member := range conflict.Members {
			if member.FactID == factID {
				return conflict
			}
		}
	}
	t.Fatalf("conflict containing fact %q not found: %+v", factID, conflicts)
	return FactConflict{}
}

func conflictCandidateFact(t *testing.T, conflict FactConflict) string {
	t.Helper()
	for _, member := range conflict.Members {
		if member.Role == ConflictMemberCandidate {
			return member.FactID
		}
	}
	t.Fatalf("candidate missing from conflict: %+v", conflict)
	return ""
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

func TestRejectsSubstitutionAcrossConfiguredTeams(t *testing.T) {
	store := NewStore()
	matchID := "fact-integrity-substitution-team"
	if _, _, err := store.SetConfig(matchID, MatchConfig{
		HomeTeam:    "西班牙",
		AwayTeam:    "德国",
		HomePlayers: []Player{{Name: "佩德里"}},
		AwayPlayers: []Player{{Name: "穆西亚拉"}},
	}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}

	_, _, err := store.Create(matchID, MatchEvent{
		EventType:   "substitution",
		Period:      "second_half",
		Clock:       "62:00",
		TeamID:      "home",
		TeamName:    "西班牙",
		Score:       Score{},
		Description: "西班牙换人。",
		Participants: []Participant{
			{Role: "sub_on", Name: "佩德里", TeamID: "home", TeamName: "西班牙"},
			{Role: "sub_off", Name: "穆西亚拉", TeamID: "away", TeamName: "德国"},
		},
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("cross-team substitution error = %v, want ErrInvalid", err)
	}
}

func TestSubstitutionRequiresBenchPlayerAndReplaysConfirmedLineup(t *testing.T) {
	store := NewStore()
	const matchID = "lineup-substitution"
	if _, _, err := store.SetConfig(matchID, MatchConfig{
		HomeTeam: "Spain",
		AwayTeam: "Germany",
		HomePlayers: []Player{
			{Name: "Starter One", Lineup: "starter"},
			{Name: "Starter Two", Lineup: "starter"},
			{Name: "Bench One", Lineup: "bench"},
		},
	}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}

	newSubstitution := func(clock, subOn, subOff string) MatchEvent {
		return MatchEvent{
			EventType: "substitution", Period: "second_half", Clock: clock,
			TeamID: "home", TeamName: "Spain", Score: Score{}, Description: "Spain substitution",
			Participants: []Participant{
				{Role: "sub_on", Name: subOn, TeamID: "home", TeamName: "Spain"},
				{Role: "sub_off", Name: subOff, TeamID: "home", TeamName: "Spain"},
			},
		}
	}

	if _, _, err := store.Create(matchID, newSubstitution("60:00", "Starter Two", "Starter One")); !errors.Is(err, ErrInvalid) {
		t.Fatalf("starter cannot be sub_on: %v", err)
	}
	if _, _, err := store.Create(matchID, newSubstitution("61:00", "Bench One", "Starter One")); err != nil {
		t.Fatalf("valid substitution error: %v", err)
	}
	if _, _, err := store.Create(matchID, newSubstitution("62:00", "Starter One", "Starter One")); !errors.Is(err, ErrInvalid) {
		t.Fatalf("same player substitution should be rejected: %v", err)
	}
	if _, _, err := store.Create(matchID, newSubstitution("63:00", "Starter One", "Bench One")); err != nil {
		t.Fatalf("confirmed lineup should allow the reverse change: %v", err)
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

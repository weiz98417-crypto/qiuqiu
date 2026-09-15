package matchstate

import (
	"reflect"
	"slices"
	"testing"
	"time"
)

func TestFactLedgerReplayAtSequenceIsDeterministic(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	matchID := "replay-at-sequence"
	events := []MatchEvent{
		{ID: "evt-1", MatchID: matchID, Source: "operator", EventType: "goal", TeamID: "home", Score: Score{Home: 1}, Description: "主队进球", Visibility: "public", Status: "active", FactID: "fact-1", FactRevision: 1, FactStatus: FactStatusConfirmed, RecordedSequence: 1, UpdatedAt: now.Format(time.RFC3339Nano)},
		{ID: "evt-2", MatchID: matchID, Source: "operator", EventType: "shot", TeamID: "away", Description: "客队射门", Visibility: "public", Status: "active", FactID: "fact-2", FactRevision: 1, FactStatus: FactStatusConfirmed, RecordedSequence: 2, UpdatedAt: now.Add(time.Second).Format(time.RFC3339Nano)},
	}
	engine := FactLedgerEngine{}
	first, err := engine.Replay(FactLedgerProjectInput{MatchID: matchID, Events: events, Now: now}, 1)
	if err != nil {
		t.Fatalf("replay at sequence: %v", err)
	}
	second, err := engine.Replay(FactLedgerProjectInput{MatchID: matchID, Events: events}, 1)
	if err != nil {
		t.Fatalf("repeat replay at sequence: %v", err)
	}
	if first.ProjectorVersion != FactLedgerProjectorVersion || first.LastSequence != 1 {
		t.Fatalf("replay metadata = %+v", first)
	}
	if first.Snapshot.ProjectedSequence != 1 || first.Snapshot.ProjectionVersion != FactLedgerProjectorVersion {
		t.Fatalf("snapshot metadata = %+v", first.Snapshot)
	}
	if first.Snapshot.Score != (Score{Home: 1}) || len(first.PublicEvents) != 1 {
		t.Fatalf("replay result = %+v", first)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("replay is not deterministic:\nfirst=%+v\nsecond=%+v", first, second)
	}
}

func TestFactLedgerReplayRejectsIncompleteSequenceHistory(t *testing.T) {
	engine := FactLedgerEngine{}
	_, err := engine.Replay(FactLedgerProjectInput{
		MatchID: "incomplete-sequence",
		Events: []MatchEvent{
			{ID: "evt-1", MatchID: "incomplete-sequence", EventType: "shot", RecordedSequence: 1},
			{ID: "evt-2", MatchID: "incomplete-sequence", EventType: "shot"},
		},
	}, 1)
	if err == nil {
		t.Fatal("expected incomplete sequence error")
	}
}

func TestFactLedgerProjectsScoreAfterEarlierGoalIsRevoked(t *testing.T) {
	now := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)
	events := []MatchEvent{
		{
			ID: "goal-1", FactID: "goal-1", MatchID: "replay", EventType: "goal", TeamID: "home",
			Score: Score{Home: 1}, Description: "主队进球。", Status: "active", Visibility: "public",
			FactStatus: FactStatusRevoked, CreatedAt: now.Add(-2 * time.Minute).Format(time.RFC3339Nano), UpdatedAt: now.Add(-time.Minute).Format(time.RFC3339Nano),
		},
		{
			ID: "shot-1", FactID: "shot-1", MatchID: "replay", EventType: "shot", TeamID: "home",
			Score: Score{Home: 1}, Description: "随后完成一次射门。", Status: "active", Visibility: "public",
			FactStatus: FactStatusConfirmed, CreatedAt: now.Add(-time.Minute).Format(time.RFC3339Nano), UpdatedAt: now.Add(-time.Minute).Format(time.RFC3339Nano),
		},
	}

	projection, err := (FactLedgerEngine{}).Project(FactLedgerProjectInput{
		MatchID: "replay",
		Events:  events,
		Config:  MatchConfig{HomeTeam: "西班牙", AwayTeam: "德国"},
		Clock:   MatchClock{MatchID: "replay", Period: "first_half"},
		Now:     now,
	})
	if err != nil {
		t.Fatalf("Project error: %v", err)
	}
	if projection.Snapshot.Score != (Score{}) {
		t.Fatalf("projected score = %+v, want 0-0", projection.Snapshot.Score)
	}
	if len(projection.Snapshot.RecentEvents) != 1 || projection.Snapshot.RecentEvents[0].ID != "shot-1" {
		t.Fatalf("projected recent events = %+v", projection.Snapshot.RecentEvents)
	}
	if projection.Snapshot.RecentEvents[0].Score != (Score{}) {
		t.Fatalf("shot effective score = %+v, want 0-0", projection.Snapshot.RecentEvents[0].Score)
	}
}

func TestMemoryPublicReadsReplayRevokedGoal(t *testing.T) {
	store := NewStore()
	goal, _, err := store.Create("public-replay", MatchEvent{
		EventType: "goal", Period: "first_half", Clock: "10:00", TeamID: "home", Score: Score{Home: 1}, Description: "主队进球。",
	})
	if err != nil {
		t.Fatalf("Create goal: %v", err)
	}
	if _, _, err := store.Create("public-replay", MatchEvent{
		EventType: "shot", Period: "first_half", Clock: "11:00", TeamID: "home", Score: Score{Home: 1}, Description: "随后射门。",
	}); err != nil {
		t.Fatalf("Create shot: %v", err)
	}
	_, revokedSnapshot, err := store.RevokeFact("public-replay", goal.FactID, "operator-1")
	if err != nil {
		t.Fatalf("Revoke goal: %v", err)
	}
	if revokedSnapshot.Score != (Score{}) || len(revokedSnapshot.RecentEvents) != 1 || revokedSnapshot.RecentEvents[0].Score != (Score{}) {
		t.Fatalf("revoke response did not replay public state: %+v", revokedSnapshot)
	}

	snapshot := store.PublicSnapshot("public-replay")
	if snapshot.Score != (Score{}) {
		t.Fatalf("public score = %+v, want 0-0", snapshot.Score)
	}
	if len(snapshot.RecentEvents) != 1 || snapshot.RecentEvents[0].Score != (Score{}) {
		t.Fatalf("public recent events = %+v", snapshot.RecentEvents)
	}
	events := store.PublicEvents("public-replay")
	if len(events) != 1 || events[0].ID != snapshot.RecentEvents[0].ID || events[0].Score != (Score{}) {
		t.Fatalf("public events = %+v", events)
	}
}

func TestCreateValidatesAgainstPublicProjection(t *testing.T) {
	store := NewStore()
	if _, _, err := store.Create("public-validation", MatchEvent{
		Source: "provider", FactStatus: FactStatusProvisional, Visibility: "private",
		EventType: "goal", Period: "first_half", Clock: "10:00", TeamID: "home", Score: Score{Home: 1}, Description: "待确认进球。",
	}); err != nil {
		t.Fatalf("Create candidate goal: %v", err)
	}
	confirmed, snapshot, err := store.Create("public-validation", MatchEvent{
		Source: "provider", FactStatus: FactStatusConfirmed, Visibility: "public",
		EventType: "goal", Period: "first_half", Clock: "10:10", TeamID: "home", Score: Score{Home: 1}, Description: "正式确认进球。",
	})
	if err != nil {
		t.Fatalf("Create confirmed goal: %v", err)
	}
	if confirmed.Score != (Score{Home: 1}) || snapshot.Score != (Score{Home: 1}) {
		t.Fatalf("confirmed event/snapshot = %+v / %+v", confirmed, snapshot)
	}
	if len(snapshot.RecentEvents) != 1 || snapshot.RecentEvents[0].ID != confirmed.ID {
		t.Fatalf("mutation snapshot leaked candidate facts: %+v", snapshot.RecentEvents)
	}
}

func TestSnapshotUsesPublicProjectionWhenEnabled(t *testing.T) {
	store := NewStore()
	if _, _, err := store.Create("snapshot-public-projection", MatchEvent{
		Source: "provider", FactStatus: FactStatusProvisional, Visibility: "private",
		EventType: "goal", Period: "first_half", Clock: "10:00", TeamID: "home", Score: Score{Home: 1}, Description: "待确认进球。",
	}); err != nil {
		t.Fatalf("Create candidate goal: %v", err)
	}
	snapshot := store.Snapshot("snapshot-public-projection")
	if snapshot.Score != (Score{}) || len(snapshot.RecentEvents) != 0 {
		t.Fatalf("snapshot leaked candidate facts: %+v", snapshot)
	}
}

func TestConfirmFactValidatesAgainstPublicProjection(t *testing.T) {
	store := NewStore()
	oldGoal, _, err := store.Create("confirm-public-validation", MatchEvent{
		EventType: "goal", Period: "first_half", Clock: "10:00", TeamID: "home", Score: Score{Home: 1}, Description: "旧进球。",
	})
	if err != nil {
		t.Fatalf("Create old goal: %v", err)
	}
	if _, _, err := store.Create("confirm-public-validation", MatchEvent{
		EventType: "shot", Period: "first_half", Clock: "11:00", TeamID: "home", Score: Score{Home: 1}, Description: "随后射门。",
	}); err != nil {
		t.Fatalf("Create shot: %v", err)
	}
	if _, _, err := store.RevokeFact("confirm-public-validation", oldGoal.FactID, "operator-1"); err != nil {
		t.Fatalf("Revoke old goal: %v", err)
	}
	candidate, _, err := store.Create("confirm-public-validation", MatchEvent{
		Source: "provider", FactStatus: FactStatusProvisional, Visibility: "public",
		EventType: "goal", Period: "first_half", Clock: "12:00", TeamID: "home", Score: Score{Home: 1}, Description: "待确认新进球。",
	})
	if err != nil {
		t.Fatalf("Create candidate: %v", err)
	}
	confirmed, snapshot, err := store.ConfirmFact("confirm-public-validation", candidate.FactID, "operator-2")
	if err != nil {
		t.Fatalf("Confirm candidate: %v", err)
	}
	if confirmed.FactStatus != FactStatusConfirmed || snapshot.Score != (Score{Home: 1}) {
		t.Fatalf("confirmed event/snapshot = %+v / %+v", confirmed, snapshot)
	}
	if len(snapshot.RecentEvents) != 2 || snapshot.RecentEvents[1].EventType != "shot" || snapshot.RecentEvents[1].Score != (Score{}) {
		t.Fatalf("confirmed snapshot did not replay historical scores: %+v", snapshot.RecentEvents)
	}
}

func TestCorrectValidatesAgainstPublicProjection(t *testing.T) {
	store := NewStore()
	if _, _, err := store.Create("correct-public-validation", MatchEvent{
		EventType: "goal", Period: "first_half", Clock: "10:00", TeamID: "home", Score: Score{Home: 1}, Description: "公开进球。",
	}); err != nil {
		t.Fatalf("Create public goal: %v", err)
	}
	shot, _, err := store.Create("correct-public-validation", MatchEvent{
		EventType: "shot", Period: "first_half", Clock: "11:00", TeamID: "home", Score: Score{Home: 1}, Description: "公开射门。",
	})
	if err != nil {
		t.Fatalf("Create public shot: %v", err)
	}
	if _, _, err := store.Create("correct-public-validation", MatchEvent{
		Source: "provider", FactStatus: FactStatusProvisional, Visibility: "private",
		EventType: "goal", Period: "first_half", Clock: "12:00", TeamID: "home", Score: Score{Home: 2}, Description: "待确认进球。",
	}); err != nil {
		t.Fatalf("Create candidate goal: %v", err)
	}
	corrected, snapshot, err := store.Correct("correct-public-validation", shot.ID, MatchEvent{
		EventType: "shot", Period: "first_half", Clock: "11:00", TeamID: "home", Score: Score{Home: 1}, Description: "修正后的射门说明。",
	})
	if err != nil {
		t.Fatalf("Correct public shot: %v", err)
	}
	if corrected.Score != (Score{Home: 1}) || snapshot.Score != (Score{Home: 1}) {
		t.Fatalf("corrected event/snapshot = %+v / %+v", corrected, snapshot)
	}
}

func TestMemoryStoreReportsProjectionMismatchWithoutChangingPublicSnapshot(t *testing.T) {
	store := NewStore(WithFactLedgerPublicReads(false))
	var audits []FactProjectionAudit
	store.SetFactProjectionAuditObserver(func(audit FactProjectionAudit) {
		audits = append(audits, audit)
	})
	goal, _, err := store.Create("shadow-replay", MatchEvent{
		EventType: "goal", Period: "first_half", Clock: "10:00", TeamID: "home", TeamName: "西班牙",
		Score: Score{Home: 1}, Description: "主队进球。",
	})
	if err != nil {
		t.Fatalf("Create goal: %v", err)
	}
	if _, _, err := store.Create("shadow-replay", MatchEvent{
		EventType: "shot", Period: "first_half", Clock: "11:00", TeamID: "home", TeamName: "西班牙",
		Score: Score{Home: 1}, Description: "随后完成一次射门。",
	}); err != nil {
		t.Fatalf("Create shot: %v", err)
	}
	if _, _, err := store.RevokeFact("shadow-replay", goal.FactID, "operator-1"); err != nil {
		t.Fatalf("Revoke goal: %v", err)
	}

	legacy := store.PublicSnapshot("shadow-replay")
	if legacy.Score != (Score{Home: 1}) {
		t.Fatalf("stage A changed the online snapshot: %+v", legacy.Score)
	}
	if len(audits) != 1 {
		t.Fatalf("projection audits = %d, want 1", len(audits))
	}
	if audits[0].LegacyScore != (Score{Home: 1}) || audits[0].ProjectedScore != (Score{}) {
		t.Fatalf("projection audit = %+v", audits[0])
	}
	if !slices.Contains(audits[0].Mismatches, "public_events") || len(audits[0].Differences) == 0 {
		t.Fatalf("projection audit did not include complete event differences: %+v", audits[0])
	}
}

func TestFactLedgerRejectsDanglingGoalCancellationReference(t *testing.T) {
	_, err := (FactLedgerEngine{}).Project(FactLedgerProjectInput{
		MatchID: "dangling-cancellation",
		Events: []MatchEvent{
			{
				ID: "cancel-1", FactID: "cancel-1", MatchID: "dangling-cancellation",
				EventType: "goal_cancelled", TeamID: "home", RevisionOf: "missing-goal",
				Status: "active", Visibility: "public", FactStatus: FactStatusConfirmed,
			},
		},
		Config: MatchConfig{HomeTeam: "西班牙", AwayTeam: "德国"},
		Clock:  MatchClock{MatchID: "dangling-cancellation", Period: "first_half"},
		Now:    time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC),
	})
	if err == nil {
		t.Fatal("dangling goal cancellation should fail projection")
	}
}

func TestFactLedgerProjectionInvariants(t *testing.T) {
	now := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		events []MatchEvent
		want   Score
	}{
		{
			name: "candidate facts do not affect the accepted score",
			events: []MatchEvent{
				{ID: "candidate-goal", FactID: "candidate-goal", EventType: "goal", TeamID: "home", Score: Score{Home: 1}, Status: "active", Visibility: "private", FactStatus: FactStatusProvisional},
				{ID: "confirmed-shot", FactID: "confirmed-shot", EventType: "shot", TeamID: "home", Score: Score{}, Status: "active", Visibility: "public", FactStatus: FactStatusConfirmed},
			},
			want: Score{},
		},
		{
			name: "goal cancellation neutralizes the referenced goal",
			events: []MatchEvent{
				{ID: "goal-1", FactID: "goal-1", EventType: "goal", TeamID: "home", Score: Score{Home: 1}, Status: "active", Visibility: "public", FactStatus: FactStatusConfirmed},
				{ID: "cancel-1", FactID: "cancel-1", EventType: "goal_cancelled", TeamID: "home", RevisionOf: "goal-1", Score: Score{}, Status: "active", Visibility: "public", FactStatus: FactStatusConfirmed},
			},
			want: Score{},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			projection, err := (FactLedgerEngine{}).Project(FactLedgerProjectInput{
				MatchID: "projection-invariants", Events: testCase.events,
				Config: MatchConfig{HomeTeam: "西班牙", AwayTeam: "德国"},
				Clock:  MatchClock{MatchID: "projection-invariants", Period: "first_half"}, Now: now,
			})
			if err != nil {
				t.Fatalf("Project error: %v", err)
			}
			if projection.Snapshot.Score != testCase.want {
				t.Fatalf("projected score = %+v, want %+v", projection.Snapshot.Score, testCase.want)
			}
		})
	}
}

func TestFactLedgerUsesRecordedSequenceInsteadOfInputOrder(t *testing.T) {
	now := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)
	projection, err := (FactLedgerEngine{}).Project(FactLedgerProjectInput{
		MatchID: "recorded-order",
		Events: []MatchEvent{
			{ID: "away-goal", FactID: "away-goal", EventType: "goal", TeamID: "away", RecordedSequence: 3, Status: "active", Visibility: "public", FactStatus: FactStatusConfirmed},
			{ID: "home-goal", FactID: "home-goal", EventType: "goal", TeamID: "home", RecordedSequence: 1, Status: "active", Visibility: "public", FactStatus: FactStatusConfirmed},
			{ID: "checkpoint", FactID: "checkpoint", EventType: "score_correction", Score: Score{}, RecordedSequence: 2, Status: "active", Visibility: "public", FactStatus: FactStatusConfirmed},
		},
		Clock: MatchClock{MatchID: "recorded-order", Period: "first_half"},
		Now:   now,
	})
	if err != nil {
		t.Fatalf("Project error: %v", err)
	}
	if projection.Snapshot.Score != (Score{Away: 1}) {
		t.Fatalf("projected score = %+v, want 0-1", projection.Snapshot.Score)
	}
	if len(projection.PublicEvents) != 3 || projection.PublicEvents[0].ID != "away-goal" {
		t.Fatalf("projected public events = %+v", projection.PublicEvents)
	}
}

func TestFactLedgerRejectsIncompleteRecordedSequence(t *testing.T) {
	_, err := (FactLedgerEngine{}).Project(FactLedgerProjectInput{
		MatchID: "incomplete-order",
		Events: []MatchEvent{
			{ID: "goal-1", FactID: "goal-1", EventType: "goal", TeamID: "home", RecordedSequence: 1, Status: "active", Visibility: "public", FactStatus: FactStatusConfirmed},
			{ID: "shot-1", FactID: "shot-1", EventType: "shot", Status: "active", Visibility: "public", FactStatus: FactStatusConfirmed},
		},
	})
	if err == nil {
		t.Fatal("mixed sequenced and unsequenced history should fail projection")
	}
}

func TestFactLedgerRejectsInvalidGoalCancellationOrder(t *testing.T) {
	tests := []struct {
		name   string
		events []MatchEvent
	}{
		{
			name: "cancellation before goal",
			events: []MatchEvent{
				{ID: "cancel-1", FactID: "cancel-1", EventType: "goal_cancelled", RevisionOf: "goal-1", RecordedSequence: 1, Status: "active", Visibility: "public", FactStatus: FactStatusConfirmed},
				{ID: "goal-1", FactID: "goal-1", EventType: "goal", TeamID: "home", RecordedSequence: 2, Status: "active", Visibility: "public", FactStatus: FactStatusConfirmed},
			},
		},
		{
			name: "duplicate cancellation",
			events: []MatchEvent{
				{ID: "goal-1", FactID: "goal-1", EventType: "goal", TeamID: "home", RecordedSequence: 1, Status: "active", Visibility: "public", FactStatus: FactStatusConfirmed},
				{ID: "cancel-1", FactID: "cancel-1", EventType: "goal_cancelled", RevisionOf: "goal-1", RecordedSequence: 2, Status: "active", Visibility: "public", FactStatus: FactStatusConfirmed},
				{ID: "cancel-2", FactID: "cancel-2", EventType: "goal_cancelled", RevisionOf: "goal-1", RecordedSequence: 3, Status: "active", Visibility: "public", FactStatus: FactStatusConfirmed},
			},
		},
		{
			name: "cancellation after checkpoint",
			events: []MatchEvent{
				{ID: "goal-1", FactID: "goal-1", EventType: "goal", TeamID: "home", RecordedSequence: 1, Status: "active", Visibility: "public", FactStatus: FactStatusConfirmed},
				{ID: "checkpoint", FactID: "checkpoint", EventType: "score_correction", Score: Score{}, RecordedSequence: 2, Status: "active", Visibility: "public", FactStatus: FactStatusConfirmed},
				{ID: "cancel-1", FactID: "cancel-1", EventType: "goal_cancelled", RevisionOf: "goal-1", RecordedSequence: 3, Status: "active", Visibility: "public", FactStatus: FactStatusConfirmed},
			},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := (FactLedgerEngine{}).Project(FactLedgerProjectInput{
				MatchID: "invalid-cancellation", Events: testCase.events,
			})
			if err == nil {
				t.Fatal("invalid cancellation history should fail projection")
			}
		})
	}
}

func TestFactLedgerKeepsCancellationWhenReferencedGoalIsRevoked(t *testing.T) {
	projection, err := (FactLedgerEngine{}).Project(FactLedgerProjectInput{
		MatchID: "revoked-goal-cancellation",
		Events: []MatchEvent{
			{ID: "goal-1", FactID: "goal-1", EventType: "goal", TeamID: "home", RecordedSequence: 1, Status: "active", Visibility: "public", FactStatus: FactStatusRevoked},
			{ID: "cancel-1", FactID: "cancel-1", EventType: "goal_cancelled", RevisionOf: "goal-1", RecordedSequence: 2, Status: "active", Visibility: "public", FactStatus: FactStatusConfirmed},
		},
	})
	if err != nil {
		t.Fatalf("Project error: %v", err)
	}
	if projection.Snapshot.Score != (Score{}) {
		t.Fatalf("projected score = %+v, want 0-0", projection.Snapshot.Score)
	}
	if len(projection.PublicEvents) != 1 || projection.PublicEvents[0].ID != "cancel-1" {
		t.Fatalf("public events = %+v", projection.PublicEvents)
	}
}

func TestFactLedgerRejectsPublicCancellationOfPrivateGoal(t *testing.T) {
	_, err := (FactLedgerEngine{}).Project(FactLedgerProjectInput{
		MatchID: "private-goal-cancellation",
		Events: []MatchEvent{
			{ID: "goal-1", FactID: "goal-1", EventType: "goal", TeamID: "home", RecordedSequence: 1, Status: "active", Visibility: "private", FactStatus: FactStatusConfirmed},
			{ID: "cancel-1", FactID: "cancel-1", EventType: "goal_cancelled", RevisionOf: "goal-1", RecordedSequence: 2, Status: "active", Visibility: "public", FactStatus: FactStatusConfirmed},
		},
	})
	if err == nil {
		t.Fatal("public cancellation of a private goal should fail projection")
	}
}

func TestStoreRejectsPublicCancellationOfPrivateGoal(t *testing.T) {
	store := NewStore()
	goal, _, err := store.Create("private-goal-cancellation", MatchEvent{
		EventType: "goal", Period: "first_half", Clock: "10:00", TeamID: "home", Score: Score{Home: 1},
		Description: "内部进球候选。", Visibility: "private", FactStatus: FactStatusConfirmed, Confirmed: true,
	})
	if err != nil {
		t.Fatalf("Create private goal: %v", err)
	}
	if _, _, err := store.Create("private-goal-cancellation", MatchEvent{
		EventType: "goal_cancelled", Period: "first_half", Clock: "11:00", TeamID: "home", Score: Score{},
		Description: "不应公开的取消。", RevisionOf: goal.ID,
	}); err == nil {
		t.Fatal("public cancellation of a private goal should be rejected")
	}
}

func TestStoreRejectsRelationshipEventsThroughCorrection(t *testing.T) {
	store := NewStore()
	goal, _, err := store.Create("relationship-correction", MatchEvent{
		EventType: "goal", Period: "first_half", Clock: "10:00", TeamID: "home", Score: Score{Home: 1}, Description: "主队进球。",
	})
	if err != nil {
		t.Fatalf("Create goal: %v", err)
	}
	if _, _, err := store.Correct("relationship-correction", goal.ID, MatchEvent{
		EventType: "var_result", Period: "first_half", Clock: "10:30", TeamID: "home", Score: Score{}, Description: "试图改成VAR结果。",
	}); err == nil {
		t.Fatal("ordinary fact should not be corrected into a relationship event")
	}
	cancellation, _, err := store.Create("relationship-correction", MatchEvent{
		EventType: "goal_cancelled", Period: "first_half", Clock: "11:00", TeamID: "home", Score: Score{},
		Description: "进球取消。", RevisionOf: goal.ID,
	})
	if err != nil {
		t.Fatalf("Create cancellation: %v", err)
	}
	if _, _, err := store.Correct("relationship-correction", cancellation.ID, MatchEvent{
		EventType: "goal_cancelled", Period: "first_half", Clock: "11:10", TeamID: "home", Score: Score{}, Description: "修改取消说明。",
	}); err == nil {
		t.Fatal("relationship event should be revoked and recreated instead of corrected")
	}
}

func TestStoreRejectsCancellationAfterScoreCheckpoint(t *testing.T) {
	store := NewStore()
	goal, _, err := store.Create("checkpoint-cancellation", MatchEvent{
		EventType: "goal", Period: "first_half", Clock: "10:00", TeamID: "home", Score: Score{Home: 1}, Description: "主队进球。",
	})
	if err != nil {
		t.Fatalf("Create goal: %v", err)
	}
	if _, _, err := store.Create("checkpoint-cancellation", MatchEvent{
		EventType: "score_correction", Period: "first_half", Clock: "11:00", Score: Score{Home: 2}, Description: "比分更正为二比零。",
		Evidence: map[string]any{"correctionReason": "人工核对"},
	}); err != nil {
		t.Fatalf("Create score correction: %v", err)
	}
	if _, _, err := store.Create("checkpoint-cancellation", MatchEvent{
		EventType: "goal_cancelled", Period: "first_half", Clock: "12:00", TeamID: "home", Score: Score{Home: 1},
		Description: "取消检查点前的进球。", RevisionOf: goal.ID,
	}); err == nil {
		t.Fatal("cancellation after score checkpoint should be rejected")
	}
}

func TestStoreAllowsCancellationAfterEarlierCancellationIsRevoked(t *testing.T) {
	store := NewStore()
	goal, _, err := store.Create("replacement-cancellation", MatchEvent{
		EventType: "goal", Period: "first_half", Clock: "10:00", TeamID: "home", Score: Score{Home: 1}, Description: "主队进球。",
	})
	if err != nil {
		t.Fatalf("Create goal: %v", err)
	}
	cancellation, _, err := store.Create("replacement-cancellation", MatchEvent{
		EventType: "goal_cancelled", Period: "first_half", Clock: "11:00", TeamID: "home", Score: Score{},
		Description: "进球取消。", RevisionOf: goal.ID,
	})
	if err != nil {
		t.Fatalf("Create cancellation: %v", err)
	}
	if _, _, err := store.RevokeFact("replacement-cancellation", cancellation.FactID, "operator-1"); err != nil {
		t.Fatalf("Revoke cancellation: %v", err)
	}
	if _, _, err := store.Create("replacement-cancellation", MatchEvent{
		EventType: "goal_cancelled", Period: "first_half", Clock: "12:00", TeamID: "home", Score: Score{},
		Description: "重新确认进球取消。", RevisionOf: goal.ID,
	}); err != nil {
		t.Fatalf("Create replacement cancellation: %v", err)
	}
}

func TestFactLedgerReturnsCompletePublicEvents(t *testing.T) {
	events := make([]MatchEvent, 0, 7)
	for sequence := int64(1); sequence <= 7; sequence++ {
		events = append(events, MatchEvent{
			ID: "shot-" + time.Unix(sequence, 0).UTC().Format("05"), FactID: "shot-" + time.Unix(sequence, 0).UTC().Format("05"),
			EventType: "shot", RecordedSequence: sequence, Status: "active", Visibility: "public", FactStatus: FactStatusConfirmed,
		})
	}
	projection, err := (FactLedgerEngine{}).Project(FactLedgerProjectInput{MatchID: "complete-public-events", Events: events})
	if err != nil {
		t.Fatalf("Project error: %v", err)
	}
	if len(projection.PublicEvents) != 7 {
		t.Fatalf("public events = %d, want 7", len(projection.PublicEvents))
	}
	if len(projection.Snapshot.RecentEvents) != 5 {
		t.Fatalf("recent events = %d, want 5", len(projection.Snapshot.RecentEvents))
	}
}

func TestFactLedgerProjectionIsDeterministicWithoutNow(t *testing.T) {
	input := FactLedgerProjectInput{MatchID: "deterministic-projection"}
	first, err := (FactLedgerEngine{}).Project(input)
	if err != nil {
		t.Fatalf("first Project error: %v", err)
	}
	second, err := (FactLedgerEngine{}).Project(input)
	if err != nil {
		t.Fatalf("second Project error: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("same input produced different projections: first=%+v second=%+v", first, second)
	}
}

func TestEnabledPublicProjectionDoesNotFallbackToLegacyOnError(t *testing.T) {
	input := FactLedgerProjectInput{
		MatchID: "projection-failure",
		Events: []MatchEvent{
			{
				ID: "cancel-1", FactID: "cancel-1", EventType: "goal_cancelled", RevisionOf: "missing-goal",
				Score: Score{Home: 7}, Status: "active", Visibility: "public", FactStatus: FactStatusConfirmed,
			},
		},
	}
	enabled := resolvePublicProjection(input, true, nil)
	if enabled.Snapshot.Score != (Score{}) || len(enabled.Events) != 0 || enabled.Snapshot.Integrity.Status != "conflict" {
		t.Fatalf("enabled projection fell back to legacy state: %+v", enabled)
	}
	disabled := resolvePublicProjection(input, false, nil)
	if disabled.Snapshot.Score != (Score{Home: 7}) || len(disabled.Events) != 1 {
		t.Fatalf("explicit rollback did not return legacy state: %+v", disabled)
	}
}

func TestMemoryStoreDoesNotReportMatchingProjection(t *testing.T) {
	store := NewStore()
	var audits []FactProjectionAudit
	store.SetFactProjectionAuditObserver(func(audit FactProjectionAudit) {
		audits = append(audits, audit)
	})
	if _, _, err := store.Create("matching-shadow", MatchEvent{
		EventType: "goal", Period: "first_half", Clock: "10:00", TeamID: "home", TeamName: "西班牙",
		Score: Score{Home: 1}, Description: "主队进球。",
	}); err != nil {
		t.Fatalf("Create goal: %v", err)
	}
	if _, _, err := store.Create("matching-shadow", MatchEvent{
		EventType: "shot", Period: "first_half", Clock: "11:00", TeamID: "home", TeamName: "西班牙",
		Score: Score{Home: 1}, Description: "随后完成一次射门。",
	}); err != nil {
		t.Fatalf("Create shot: %v", err)
	}

	if snapshot := store.PublicSnapshot("matching-shadow"); snapshot.Score != (Score{Home: 1}) {
		t.Fatalf("public score = %+v, want 1-0", snapshot.Score)
	}
	if len(audits) != 0 {
		t.Fatalf("matching projection produced audits: %+v", audits)
	}
}

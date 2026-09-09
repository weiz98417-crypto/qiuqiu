package matchstate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	"qiuqiu/internal/operatorwrite"
)

func TestEventIDsRemainUniqueUnderConcurrentBurst(t *testing.T) {
	const count = 4096
	ids := make(chan string, count)
	var wait sync.WaitGroup
	wait.Add(count)
	for index := 0; index < count; index++ {
		go func() {
			defer wait.Done()
			ids <- newEventID()
		}()
	}
	wait.Wait()
	close(ids)
	seen := make(map[string]struct{}, count)
	for id := range ids {
		if _, duplicate := seen[id]; duplicate {
			t.Fatalf("duplicate event id %q", id)
		}
		seen[id] = struct{}{}
	}
}

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

func TestPostgresStorePersistsAndValidatesSubstitutionLineup(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	store, err := OpenPostgresStore(context.Background(), databaseURL, "../../migrations")
	if err != nil {
		t.Fatalf("OpenPostgresStore error: %v", err)
	}
	defer store.Close()
	matchID := "pg-lineup-" + time.Now().UTC().Format("20060102150405.000000000")
	defer store.Reset(matchID)
	config, _, err := store.SetConfig(matchID, MatchConfig{
		HomeTeam: "Spain",
		AwayTeam: "Germany",
		HomePlayers: []Player{
			{Name: "Starter One", Lineup: "starter"},
			{Name: "Starter Two", Lineup: "starter"},
			{Name: "Bench One", Lineup: "bench"},
		},
	})
	if err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}
	if got := config.HomePlayers[2].Lineup; got != "bench" {
		t.Fatalf("bench lineup did not round-trip: %+v", config.HomePlayers)
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
	if _, _, err := store.Create(matchID, newSubstitution("62:00", "Starter One", "Bench One")); err != nil {
		t.Fatalf("confirmed lineup should allow the reverse change: %v", err)
	}
}

func TestPostgresStoreReportsProjectionMismatchWithoutChangingPublicSnapshot(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	store, err := OpenPostgresStore(context.Background(), databaseURL, "../../migrations", WithFactLedgerPublicReads(false))
	if err != nil {
		t.Fatalf("OpenPostgresStore error: %v", err)
	}
	defer store.Close()
	matchID := "pg-shadow-replay-" + time.Now().UTC().Format("20060102150405.000000000")
	defer store.Reset(matchID)
	var audits []FactProjectionAudit
	store.SetFactProjectionAuditObserver(func(audit FactProjectionAudit) {
		audits = append(audits, audit)
	})
	goal, _, err := store.Create(matchID, MatchEvent{
		EventType: "goal", Period: "first_half", Clock: "10:00", TeamID: "home", TeamName: "西班牙",
		Score: Score{Home: 1}, Description: "主队进球。",
	})
	if err != nil {
		t.Fatalf("Create goal: %v", err)
	}
	if _, _, err := store.Create(matchID, MatchEvent{
		EventType: "shot", Period: "first_half", Clock: "11:00", TeamID: "home", TeamName: "西班牙",
		Score: Score{Home: 1}, Description: "随后完成一次射门。",
	}); err != nil {
		t.Fatalf("Create shot: %v", err)
	}
	if _, _, err := store.RevokeFact(matchID, goal.FactID, "operator-1"); err != nil {
		t.Fatalf("Revoke goal: %v", err)
	}

	legacy := store.PublicSnapshot(matchID)
	if legacy.Score != (Score{Home: 1}) {
		t.Fatalf("stage A changed the online snapshot: %+v", legacy.Score)
	}
	if len(audits) != 1 || audits[0].ProjectedScore != (Score{}) {
		t.Fatalf("projection audits = %+v", audits)
	}
}

func TestPostgresPublicReadsReplayRevokedGoal(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	store, err := OpenPostgresStore(context.Background(), databaseURL, "../../migrations")
	if err != nil {
		t.Fatalf("OpenPostgresStore error: %v", err)
	}
	defer store.Close()
	matchID := "pg-public-replay-" + time.Now().UTC().Format("20060102150405.000000000")
	defer store.Reset(matchID)
	goal, _, err := store.Create(matchID, MatchEvent{
		EventType: "goal", Period: "first_half", Clock: "10:00", TeamID: "home", Score: Score{Home: 1}, Description: "主队进球。",
	})
	if err != nil {
		t.Fatalf("Create goal: %v", err)
	}
	if _, _, err := store.Create(matchID, MatchEvent{
		EventType: "shot", Period: "first_half", Clock: "11:00", TeamID: "home", Score: Score{Home: 1}, Description: "随后射门。",
	}); err != nil {
		t.Fatalf("Create shot: %v", err)
	}
	_, revokedSnapshot, err := store.RevokeFact(matchID, goal.FactID, "operator-1")
	if err != nil {
		t.Fatalf("Revoke goal: %v", err)
	}
	if revokedSnapshot.Score != (Score{}) || len(revokedSnapshot.RecentEvents) != 1 || revokedSnapshot.RecentEvents[0].Score != (Score{}) {
		t.Fatalf("revoke response = %+v", revokedSnapshot)
	}
	if snapshot := store.PublicSnapshot(matchID); !reflect.DeepEqual(snapshot, revokedSnapshot) {
		t.Fatalf("public snapshot = %+v, revoke snapshot = %+v", snapshot, revokedSnapshot)
	}
	if events := store.PublicEvents(matchID); len(events) != 1 || events[0].Score != (Score{}) {
		t.Fatalf("public events = %+v", events)
	}
}

func TestFactLedgerProjectionMatchesMemoryAndPostgresAdapters(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	postgresStore, err := OpenPostgresStore(context.Background(), databaseURL, "../../migrations")
	if err != nil {
		t.Fatalf("OpenPostgresStore error: %v", err)
	}
	defer postgresStore.Close()
	matchID := "projection-adapter-parity-" + time.Now().UTC().Format("20060102150405.000000000")
	defer postgresStore.Reset(matchID)
	memoryStore := NewStore()
	now := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)

	memoryProjection := buildAdapterProjection(t, memoryStore, matchID, now)
	postgresProjection := buildAdapterProjection(t, postgresStore, matchID, now)
	canonicalizeProjection(&memoryProjection)
	canonicalizeProjection(&postgresProjection)
	if !reflect.DeepEqual(memoryProjection, postgresProjection) {
		t.Fatalf("adapter projections differ:\nmemory=%+v\npostgres=%+v", memoryProjection, postgresProjection)
	}
}

func TestFactLedgerMutationSnapshotsMatchMemoryAndPostgresAdapters(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	postgresStore, err := OpenPostgresStore(context.Background(), databaseURL, "../../migrations")
	if err != nil {
		t.Fatalf("OpenPostgresStore error: %v", err)
	}
	defer postgresStore.Close()
	matchID := "mutation-adapter-parity-" + time.Now().UTC().Format("20060102150405.000000000")
	defer postgresStore.Reset(matchID)
	memoryStore := NewStore()
	candidate := MatchEvent{
		Source: "provider", FactStatus: FactStatusProvisional, Visibility: "private",
		EventType: "goal", Period: "first_half", Clock: "10:00", TeamID: "home", Score: Score{Home: 1}, Description: "待确认进球。",
	}
	_, memorySnapshot, err := memoryStore.Create(matchID, candidate)
	if err != nil {
		t.Fatalf("memory Create: %v", err)
	}
	_, postgresSnapshot, err := postgresStore.Create(matchID, candidate)
	if err != nil {
		t.Fatalf("postgres Create: %v", err)
	}
	if memorySnapshot.Score != postgresSnapshot.Score || len(memorySnapshot.RecentEvents) != len(postgresSnapshot.RecentEvents) {
		t.Fatalf("mutation snapshots differ: memory=%+v postgres=%+v", memorySnapshot, postgresSnapshot)
	}
}

func buildAdapterProjection(t *testing.T, store Repository, matchID string, now time.Time) FactLedgerProjection {
	t.Helper()
	config, _, err := store.SetConfig(matchID, MatchConfig{HomeTeam: "西班牙", AwayTeam: "德国"})
	if err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	firstGoal, _, err := store.Create(matchID, MatchEvent{
		EventType: "goal", Period: "first_half", Clock: "10:00", TeamID: "home", TeamName: "西班牙",
		Score: Score{Home: 1}, Description: "主队第一次进球。",
	})
	if err != nil {
		t.Fatalf("Create first goal: %v", err)
	}
	if _, _, err := store.Correct(matchID, firstGoal.ID, MatchEvent{
		EventType: "var_result", Period: "first_half", Clock: "10:10", TeamID: "home", Score: Score{}, Description: "试图改成VAR结果。",
	}); err == nil {
		t.Fatal("ordinary fact should not be corrected into a relationship event")
	}
	firstCancellation, _, err := store.Create(matchID, MatchEvent{
		EventType: "goal_cancelled", Period: "first_half", Clock: "10:30", TeamID: "home", TeamName: "西班牙",
		Score: Score{}, Description: "第一次进球取消。", RevisionOf: firstGoal.ID,
	})
	if err != nil {
		t.Fatalf("Create cancellation: %v", err)
	}
	if _, _, err := store.RevokeFact(matchID, firstCancellation.FactID, "operator-1"); err != nil {
		t.Fatalf("Revoke first cancellation: %v", err)
	}
	if _, _, err := store.Create(matchID, MatchEvent{
		EventType: "goal_cancelled", Period: "first_half", Clock: "10:40", TeamID: "home", TeamName: "西班牙",
		Score: Score{}, Description: "重新确认第一次进球取消。", RevisionOf: firstGoal.ID,
	}); err != nil {
		t.Fatalf("Create replacement cancellation: %v", err)
	}
	if _, _, err := store.Create(matchID, MatchEvent{
		Source: "provider", FactStatus: FactStatusProvisional, Visibility: "private",
		EventType: "goal", Period: "first_half", Clock: "20:00", TeamID: "home", TeamName: "西班牙",
		Score: Score{Home: 1}, Description: "待确认进球。",
	}); err != nil {
		t.Fatalf("Create candidate goal: %v", err)
	}
	secondGoal, _, err := store.Create(matchID, MatchEvent{
		Source: "provider", FactStatus: FactStatusConfirmed, Visibility: "public",
		EventType: "goal", Period: "first_half", Clock: "20:10", TeamID: "home", TeamName: "西班牙",
		Score: Score{Home: 1}, Description: "主队再次进球。",
	})
	if err != nil {
		t.Fatalf("Create second goal: %v", err)
	}
	if _, _, err := store.Create(matchID, MatchEvent{
		EventType: "score_correction", Period: "first_half", Clock: "20:30", Score: Score{}, Description: "比分更正为零比零。",
		Evidence: map[string]any{"correctionReason": "人工核对"},
	}); err != nil {
		t.Fatalf("Create score correction: %v", err)
	}
	if _, _, err := store.Create(matchID, MatchEvent{
		EventType: "goal_cancelled", Period: "first_half", Clock: "20:40", TeamID: "home", TeamName: "西班牙",
		Score: Score{}, Description: "尝试取消检查点前的进球。", RevisionOf: secondGoal.ID,
	}); err == nil {
		t.Fatal("cancellation after score checkpoint should be rejected")
	}
	if _, _, err := store.Create(matchID, MatchEvent{
		EventType: "goal", Period: "first_half", Clock: "21:00", TeamID: "away", TeamName: "德国",
		Score: Score{Away: 1}, Description: "客队进球。",
	}); err != nil {
		t.Fatalf("Create away goal: %v", err)
	}
	projection, err := (FactLedgerEngine{}).Project(FactLedgerProjectInput{
		MatchID: matchID, Events: store.Events(matchID), Config: config,
		Clock: MatchClock{MatchID: matchID, Period: "first_half"}, Now: now,
	})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	return projection
}

func canonicalizeProjection(projection *FactLedgerProjection) {
	aliases := make(map[string]string, len(projection.PublicEvents)*2)
	for index, event := range projection.PublicEvents {
		canonicalID := "event-" + strconv.Itoa(index)
		aliases[event.ID] = canonicalID
		aliases[event.FactID] = canonicalID
	}
	normalizeEvents := func(events []MatchEvent) {
		for index := range events {
			event := &events[index]
			event.ID = aliases[event.ID]
			event.FactID = event.ID
			if event.RevisionOf != "" {
				event.RevisionOf = aliases[event.RevisionOf]
			}
			event.RecordedSequence = int64(len(projection.PublicEvents) - index)
			event.CreatedAt = ""
			event.UpdatedAt = ""
			event.PublicAt = ""
			if len(event.Participants) == 0 {
				event.Participants = nil
			}
			if len(event.Tags) == 0 {
				event.Tags = nil
			}
			if len(event.Evidence) == 0 {
				event.Evidence = nil
			}
		}
	}
	normalizeEvents(projection.PublicEvents)
	normalizeEvents(projection.Snapshot.RecentEvents)
	normalizeEvents(projection.Snapshot.KeyEvents)
	projection.Snapshot.LastUpdatedAt = ""
}

func TestPostgresOutboxRetriesFailedMatchEventPublication(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	store, err := OpenPostgresStore(ctx, databaseURL, "../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	matchID := "pg-outbox-" + time.Now().UTC().Format("20060102150405.000000000")
	defer store.Reset(matchID)
	var attempts int
	published := make(chan MatchEvent, 1)
	store.outboxPublisher = func(event MatchEvent) error {
		attempts++
		if attempts == 1 {
			return errors.New("temporary publish failure")
		}
		published <- event
		return nil
	}
	created, _, err := store.Create(matchID, MatchEvent{
		EventType:   "shot",
		Period:      "first_half",
		Clock:       "10:00",
		Score:       Score{},
		Description: "shot",
		Confirmed:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var status string
	var storedAttempts int
	if err := store.pool.QueryRow(ctx, `
		SELECT status, attempts FROM outbox_messages
		WHERE aggregate_type = 'match_event' AND aggregate_id = $1
	`, created.ID+":1:confirmed").Scan(&status, &storedAttempts); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || storedAttempts != 1 {
		t.Fatalf("outbox after failure = status %s attempts %d", status, storedAttempts)
	}
	if _, err := store.pool.Exec(ctx, `
		UPDATE outbox_messages SET next_attempt_at = now()
		WHERE aggregate_type = 'match_event' AND aggregate_id = $1
	`, created.ID+":1:confirmed"); err != nil {
		t.Fatal(err)
	}
	processed, err := store.publishOutboxAggregate(ctx, created)
	if err != nil || !processed {
		t.Fatalf("outbox retry processed=%v err=%v", processed, err)
	}
	select {
	case event := <-published:
		if event.ID != created.ID {
			t.Fatalf("published event = %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("retried outbox event was not published")
	}
	if err := store.pool.QueryRow(ctx, `
		SELECT status, attempts FROM outbox_messages
		WHERE aggregate_type = 'match_event' AND aggregate_id = $1
	`, created.ID+":1:confirmed").Scan(&status, &storedAttempts); err != nil {
		t.Fatal(err)
	}
	if status != "published" || storedAttempts != 2 {
		t.Fatalf("outbox after retry = status %s attempts %d", status, storedAttempts)
	}
}

func TestPostgresOperatorTransactionAtomicallyCommitsEventIdempotencyAndOutbox(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	store, err := OpenPostgresStore(ctx, databaseURL, "../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service, err := operatorwrite.OpenPostgresService(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	matchID := "pg-operator-atomic-" + time.Now().UTC().Format("20060102150405.000000000")
	defer store.Reset(matchID)
	request := operatorwrite.Request{MatchID: matchID, Key: "atomic-key", PayloadHash: "atomic-hash", Operation: "events.create", Atomic: true}
	event := MatchEvent{EventType: "shot", Period: "first_half", Clock: "12:00", Score: Score{}, Description: "atomic shot", Confirmed: true}
	_, _, err = service.Execute(ctx, request, func(operationCtx context.Context) (operatorwrite.Response, error) {
		if _, _, err := store.CreateOperator(operationCtx, matchID, event); err != nil {
			return operatorwrite.Response{}, err
		}
		return operatorwrite.Response{}, errors.New("fail after event mutation")
	})
	if err == nil {
		t.Fatal("expected transaction failure")
	}
	if events := store.Events(matchID); len(events) != 0 {
		t.Fatalf("rolled back events = %+v", events)
	}
	var count int
	if err := store.pool.QueryRow(ctx, `SELECT COUNT(*) FROM idempotency_records WHERE match_id = $1`, matchID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rolled back idempotency count=%d err=%v", count, err)
	}
	if err := store.pool.QueryRow(ctx, `SELECT COUNT(*) FROM outbox_messages WHERE payload->>'matchId' = $1`, matchID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rolled back outbox count=%d err=%v", count, err)
	}

	response, replayed, err := service.Execute(ctx, request, func(operationCtx context.Context) (operatorwrite.Response, error) {
		created, snapshot, err := store.CreateOperator(operationCtx, matchID, event)
		if err != nil {
			return operatorwrite.Response{}, err
		}
		transactionSnapshot, err := store.PublicSnapshotOperator(operationCtx, matchID)
		if err != nil {
			return operatorwrite.Response{}, err
		}
		if transactionSnapshot.Score != snapshot.Score {
			return operatorwrite.Response{}, errors.New("operator transaction snapshot did not include its own mutation")
		}
		return operatorwrite.JSONResponse(201, map[string]any{"event": created, "snapshot": snapshot})
	})
	if err != nil || replayed || response.StatusCode != 201 {
		t.Fatalf("committed response status=%d replayed=%v err=%v", response.StatusCode, replayed, err)
	}
	if events := store.Events(matchID); len(events) != 1 {
		t.Fatalf("committed events = %+v", events)
	}
	if err := store.pool.QueryRow(ctx, `SELECT COUNT(*) FROM idempotency_records WHERE match_id = $1 AND status = 'completed'`, matchID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("committed idempotency count=%d err=%v", count, err)
	}
	if err := store.pool.QueryRow(ctx, `SELECT COUNT(*) FROM outbox_messages WHERE aggregate_type = 'match_event' AND payload->>'matchId' = $1`, matchID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("committed outbox count=%d err=%v", count, err)
	}
}

func TestPostgresOperatorConflictResolutionResponseUsesCommittedIntegrity(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	store, err := OpenPostgresStore(ctx, databaseURL, "../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service, err := operatorwrite.OpenPostgresService(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	matchID := "pg-operator-reconcile-" + time.Now().UTC().Format("20060102150405.000000000")
	defer store.Reset(matchID)
	if _, _, err := store.Create(matchID, MatchEvent{
		Source: "operator", EventType: "goal", Period: "first_half", Clock: "20:00", TeamID: "home",
		Score: Score{Home: 1}, Description: "主队进球。",
	}); err != nil {
		t.Fatalf("Create accepted goal: %v", err)
	}
	_, _, err = store.Create(matchID, MatchEvent{
		Source: "provider", EventType: "goal", Period: "first_half", Clock: "20:10", TeamID: "away",
		Score: Score{Home: 1, Away: 1}, Description: "冲突候选。",
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("Create conflict candidate error = %v", err)
	}
	candidate := store.Events(matchID)[0]
	conflicts := store.FactConflicts(matchID)
	if len(conflicts) != 1 {
		t.Fatalf("open conflicts = %+v", conflicts)
	}
	request := operatorwrite.Request{
		MatchID: matchID, Key: "conflict-resolution-key", PayloadHash: "conflict-resolution-hash", Operation: "conflicts.resolve", Atomic: true,
	}
	operation := func(operationCtx context.Context) (operatorwrite.Response, error) {
		conflict, reconciled, snapshot, err := store.ResolveFactConflictOperator(
			operationCtx, matchID, conflicts[0].ID, candidate.FactID, "operator-1", "采用官方来源",
		)
		if err != nil {
			return operatorwrite.Response{}, err
		}
		return operatorwrite.JSONResponse(200, map[string]any{"conflict": conflict, "event": reconciled, "snapshot": snapshot})
	}
	response, replayed, err := service.Execute(ctx, request, operation)
	if err != nil || replayed {
		t.Fatalf("first reconcile replayed=%v err=%v", replayed, err)
	}
	assertReconciledResponse := func(response operatorwrite.Response) {
		t.Helper()
		var envelope struct {
			Conflict FactConflict `json:"conflict"`
			Snapshot Snapshot     `json:"snapshot"`
		}
		if err := json.Unmarshal(response.Body, &envelope); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if envelope.Snapshot.Score != (Score{Away: 1}) || envelope.Snapshot.Integrity.Status != "ok" {
			t.Fatalf("reconcile response snapshot = %+v", envelope.Snapshot)
		}
		if envelope.Conflict.Status != ConflictStatusResolved || envelope.Conflict.ChosenFactID != candidate.FactID {
			t.Fatalf("resolution response conflict = %+v", envelope.Conflict)
		}
	}
	assertReconciledResponse(response)
	replayedResponse, replayed, err := service.Execute(ctx, request, func(context.Context) (operatorwrite.Response, error) {
		t.Fatal("idempotent replay executed mutation")
		return operatorwrite.Response{}, nil
	})
	if err != nil || !replayed {
		t.Fatalf("reconcile replayed=%v err=%v", replayed, err)
	}
	assertReconciledResponse(replayedResponse)
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
				Source:          "operator",
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
	if snapshot := store.Snapshot(matchID); snapshot.Integrity.Status != "conflict" || snapshot.Score != (Score{Home: 1}) {
		t.Fatalf("conflict snapshot mismatch: %+v", snapshot)
	}
	conflictEvents := store.Events(matchID)
	if len(conflictEvents) != 2 || conflictEvents[0].FactStatus != FactStatusConflict || conflictEvents[1].FactStatus != FactStatusConfirmed {
		t.Fatalf("conflict candidates = %+v", conflictEvents)
	}
	manualRevisionFound := false
	providerRevisionFound := false
	for _, event := range conflictEvents {
		revisions := store.FactRevisions(matchID, event.FactID)
		switch event.Source {
		case "operator":
			if len(revisions) != 1 || revisions[0].Status != FactStatusConfirmed {
				t.Fatalf("manual fact revisions = %+v", revisions)
			}
			manualRevisionFound = true
		case "api-sports":
			if len(revisions) != 1 || revisions[0].Status != FactStatusConflict {
				t.Fatalf("provider fact revisions = %+v", revisions)
			}
			providerRevisionFound = true
		default:
			t.Fatalf("unexpected source %q", event.Source)
		}
	}
	if !manualRevisionFound || !providerRevisionFound {
		t.Fatalf("fact revision counts: manual=%v provider=%v", manualRevisionFound, providerRevisionFound)
	}
	if _, _, err := store.Correct(matchID, manual.ID, MatchEvent{
		EventType: "goal", Period: "first_half", Clock: "25:00", TeamID: "home", TeamName: "西班牙", PlayerName: "佩德里",
		Score: Score{Home: 1, Away: 0}, Description: "确认佩德里进球。",
	}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("detaching conflict correction error = %v, want ErrInvalid", err)
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

func TestPostgresPersistsAndAtomicallyResolvesFactConflict(t *testing.T) {
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
	matchID := "pg-formal-conflict-" + time.Now().UTC().Format("20060102150405.000000000")
	if _, _, err := store.SetConfig(matchID, MatchConfig{HomeTeam: "Spain", AwayTeam: "Germany"}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}
	accepted, _, err := store.Create(matchID, MatchEvent{
		Source: "operator", EventType: "goal", Period: "first_half", Clock: "23:41", TeamID: "home",
		Score: Score{Home: 1}, Description: "Spain goal",
	})
	if err != nil {
		t.Fatalf("Create accepted goal: %v", err)
	}
	_, _, err = store.Create(matchID, MatchEvent{
		Source: "api-sports", ProviderEventID: "formal-conflict", EventType: "goal", Period: "first_half", Clock: "24:00", TeamID: "away",
		Score: Score{Home: 7, Away: 4}, Description: "Germany candidate goal",
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("Create conflict candidate error = %v, want ErrConflict", err)
	}

	conflicts := store.FactConflicts(matchID)
	if len(conflicts) != 1 || conflicts[0].Status != ConflictStatusOpen {
		t.Fatalf("open conflicts = %+v", conflicts)
	}
	conflict := conflicts[0]
	candidateFactID := conflictCandidateFact(t, conflict)
	var candidate MatchEvent
	for _, event := range store.Events(matchID) {
		if event.FactID == candidateFactID && event.Status == "active" {
			candidate = event
			break
		}
	}
	if candidate.ID == "" {
		t.Fatalf("candidate event missing for fact %q", candidateFactID)
	}
	resolved, chosen, snapshot, err := store.ResolveFactConflict(matchID, conflict.ID, candidateFactID, "operator-3", "official source confirmed")
	if err != nil {
		t.Fatalf("ResolveFactConflict error: %v", err)
	}
	if resolved.Status != ConflictStatusResolved || resolved.ChosenFactID != candidateFactID {
		t.Fatalf("resolved conflict = %+v", resolved)
	}
	if chosen.FactStatus != FactStatusReconciled || snapshot.Score != (Score{Away: 1}) || snapshot.Integrity.Status != "ok" {
		t.Fatalf("chosen event/snapshot = %+v / %+v", chosen, snapshot)
	}
	var resolutionPayload []byte
	if err := store.pool.QueryRow(ctx, `
		SELECT payload FROM outbox_messages
		WHERE aggregate_type = 'match_event' AND aggregate_id = $1
	`, candidate.ID+":2:reconciled").Scan(&resolutionPayload); err != nil {
		t.Fatalf("read resolution outbox payload: %v", err)
	}
	var published MatchEvent
	if err := json.Unmarshal(resolutionPayload, &published); err != nil {
		t.Fatalf("decode resolution outbox payload: %v", err)
	}
	if published.Score != (Score{Away: 1}) {
		t.Fatalf("resolution outbox published reported score: %+v", published)
	}
	for _, event := range store.Events(matchID) {
		if event.FactID == accepted.FactID && event.FactStatus != FactStatusRevoked {
			t.Fatalf("accepted fact was not revoked: %+v", event)
		}
	}

	reopened, err := OpenPostgresStore(ctx, databaseURL, "../../migrations")
	if err != nil {
		t.Fatalf("reopen store error: %v", err)
	}
	defer reopened.Close()
	persisted := reopened.FactConflicts(matchID)
	if len(persisted) != 1 || persisted[0].Status != ConflictStatusResolved || persisted[0].ChosenFactID != candidateFactID || persisted[0].Reason != "official source confirmed" {
		t.Fatalf("persisted conflict = %+v", persisted)
	}
	if reopened.Snapshot(matchID).Score != (Score{Away: 1}) {
		t.Fatalf("reopened snapshot = %+v", reopened.Snapshot(matchID))
	}
}

func TestPostgresBridgeCandidateMergesIntersectingOpenConflictSets(t *testing.T) {
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
	matchID := "pg-bridge-conflicts-" + time.Now().UTC().Format("20060102150405.000000000")
	defer store.Reset(matchID)
	if _, _, err := store.SetConfig(matchID, MatchConfig{HomeTeam: "Spain", AwayTeam: "Germany"}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	defer tx.Rollback(ctx)
	acceptedA := MatchEvent{FactID: "accepted-a", Status: "active", Visibility: "public", FactStatus: FactStatusConfirmed}
	acceptedB := MatchEvent{FactID: "accepted-b", Status: "active", Visibility: "public", FactStatus: FactStatusConfirmed}
	if err := createOrExtendFactConflict(ctx, tx, matchID, []MatchEvent{acceptedA}, []int{0}, MatchEvent{FactID: "candidate-a"}, time.Now().UTC()); err != nil {
		t.Fatalf("create first conflict: %v", err)
	}
	if err := createOrExtendFactConflict(ctx, tx, matchID, []MatchEvent{acceptedB}, []int{0}, MatchEvent{FactID: "candidate-b"}, time.Now().UTC().Add(time.Second)); err != nil {
		t.Fatalf("create second conflict: %v", err)
	}
	for _, item := range []struct {
		factID   string
		reason   string
		operator string
	}{
		{factID: "accepted-a", reason: "partial A", operator: "operator-a"},
		{factID: "accepted-b", reason: "partial B", operator: "operator-b"},
	} {
		var conflictID string
		if err := tx.QueryRow(ctx, `SELECT conflict_id FROM fact_conflict_members WHERE fact_id = $1`, item.factID).Scan(&conflictID); err != nil {
			t.Fatalf("find conflict for %s: %v", item.factID, err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO fact_conflict_resolution_facts (conflict_id, fact_id, selected_at, reason, resolved_by)
			VALUES ($1, $2, now(), $3, $4)
		`, conflictID, item.factID, item.reason, item.operator); err != nil {
			t.Fatalf("insert resolution audit for %s: %v", item.factID, err)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE fact_conflicts SET reason = $2, resolved_by = $3 WHERE id = $1
		`, conflictID, item.reason, item.operator); err != nil {
			t.Fatalf("update top-level audit for %s: %v", item.factID, err)
		}
	}
	if err := createOrExtendFactConflict(ctx, tx, matchID, []MatchEvent{acceptedA, acceptedB}, []int{0, 1}, MatchEvent{FactID: "bridge-candidate"}, time.Now().UTC().Add(2*time.Second)); err != nil {
		t.Fatalf("bridge conflicts: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit bridge conflicts: %v", err)
	}
	conflicts := store.FactConflicts(matchID)
	if len(conflicts) != 1 || conflicts[0].Status != ConflictStatusOpen || len(conflicts[0].Members) != 5 || !reflect.DeepEqual(conflicts[0].SelectedFactIDs, []string{"accepted-a", "accepted-b"}) || conflicts[0].Reason != "partial A" || conflicts[0].ResolvedBy != "operator-a" {
		t.Fatalf("merged postgres conflicts = %+v", conflicts)
	}
	var reason, operator string
	if err := store.pool.QueryRow(ctx, `
		SELECT reason, resolved_by
		FROM fact_conflict_resolution_facts
		WHERE conflict_id = $1 AND fact_id = 'accepted-b'
	`, conflicts[0].ID).Scan(&reason, &operator); err != nil {
		t.Fatalf("read merged audit: %v", err)
	}
	if reason != "partial B" || operator != "operator-b" {
		t.Fatalf("merged audit = %q/%q", reason, operator)
	}
}

func TestPostgresBridgeConflictResolutionPreservesCompatibleAcceptedFacts(t *testing.T) {
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
	matchID := "pg-bridge-selection-" + time.Now().UTC().Format("20060102150405.000000000")
	defer store.Reset(matchID)
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
		Source: "provider", ProviderEventID: matchID, EventType: "goal", Period: "first_half", Clock: "10:15", TeamID: "away",
		Score: Score{Home: 1, Away: 1}, Description: "bridge candidate",
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("Create bridge candidate error = %v, want ErrConflict", err)
	}
	conflicts := store.FactConflicts(matchID)
	if len(conflicts) != 1 || len(conflicts[0].Edges) != 2 {
		t.Fatalf("bridge conflict = %+v, want two direct conflict edges", conflicts)
	}
	resolved, changed, snapshot, err := store.ResolveFactConflictSelection(matchID, conflicts[0].ID, []string{first.FactID, second.FactID}, "operator-1", "keep both accepted goals")
	if err != nil {
		t.Fatalf("ResolveFactConflictSelection: %v", err)
	}
	if resolved.Status != ConflictStatusResolved || len(resolved.SelectedFactIDs) != 2 || len(changed) != 0 || snapshot.Score != (Score{Home: 2}) {
		t.Fatalf("resolution = %+v, changed = %+v, snapshot = %+v", resolved, changed, snapshot)
	}

	reopened, err := OpenPostgresStore(ctx, databaseURL, "../../migrations")
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer reopened.Close()
	persisted := reopened.FactConflicts(matchID)
	if len(persisted) != 1 || len(persisted[0].Edges) != 2 || len(persisted[0].SelectedFactIDs) != 2 {
		t.Fatalf("persisted conflict graph = %+v", persisted)
	}
}

func TestPostgresPartialConflictResolutionAccumulatesSelectionAndAudit(t *testing.T) {
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
	matchID := "pg-partial-conflict-selection-" + time.Now().UTC().Format("20060102150405.000000000")
	defer store.Reset(matchID)
	if _, _, err := store.SetConfig(matchID, MatchConfig{HomeTeam: "Spain", AwayTeam: "Germany"}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}
	firstAccepted, _, err := store.Create(matchID, MatchEvent{
		Source: "operator", EventType: "goal", Period: "first_half", Clock: "10:00", TeamID: "home",
		Score: Score{Home: 1}, Description: "first accepted",
	})
	if err != nil {
		t.Fatalf("Create first accepted: %v", err)
	}
	_, _, err = store.Create(matchID, MatchEvent{
		Source: "provider-a", ProviderEventID: matchID + "-a", EventType: "goal", Period: "first_half", Clock: "10:10", TeamID: "away",
		Score: Score{Away: 1}, Description: "first candidate",
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("Create first candidate error = %v, want ErrConflict", err)
	}
	secondAccepted, _, err := store.Create(matchID, MatchEvent{
		Source: "operator", EventType: "goal", Period: "first_half", Clock: "30:00", TeamID: "home",
		Score: Score{Home: 2}, Description: "second accepted",
	})
	if err != nil {
		t.Fatalf("Create second accepted: %v", err)
	}
	_, _, err = store.Create(matchID, MatchEvent{
		Source: "provider-b", ProviderEventID: matchID + "-b", EventType: "goal", Period: "first_half", Clock: "30:10", TeamID: "away",
		Score: Score{Home: 1, Away: 1}, Description: "second candidate",
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("Create second candidate error = %v, want ErrConflict", err)
	}

	conflicts := store.FactConflicts(matchID)
	if len(conflicts) != 2 {
		t.Fatalf("conflicts before merge = %+v", conflicts)
	}
	primary := conflictContainingFact(t, conflicts, firstAccepted.FactID)
	secondary := conflictContainingFact(t, conflicts, secondAccepted.FactID)
	firstCandidate := conflictCandidateFact(t, primary)
	if _, err := store.pool.Exec(ctx, `
		INSERT INTO fact_conflict_members (conflict_id, fact_id, role)
		SELECT $1, fact_id, role FROM fact_conflict_members WHERE conflict_id = $2
		ON CONFLICT (conflict_id, fact_id) DO UPDATE SET role = EXCLUDED.role
	`, primary.ID, secondary.ID); err != nil {
		t.Fatalf("merge members: %v", err)
	}
	if _, err := store.pool.Exec(ctx, `
		INSERT INTO fact_conflict_edges (conflict_id, left_fact_id, right_fact_id, reason, detected_at)
		SELECT $1, left_fact_id, right_fact_id, reason, detected_at FROM fact_conflict_edges WHERE conflict_id = $2
		ON CONFLICT (conflict_id, left_fact_id, right_fact_id) DO NOTHING
	`, primary.ID, secondary.ID); err != nil {
		t.Fatalf("merge edges: %v", err)
	}
	if _, err := store.pool.Exec(ctx, `DELETE FROM fact_conflicts WHERE id = $1`, secondary.ID); err != nil {
		t.Fatalf("remove merged conflict: %v", err)
	}

	partial, changed, _, err := store.ResolveFactConflictSelection(matchID, primary.ID, []string{firstCandidate}, "operator-partial", "adopt first candidate")
	if err != nil {
		t.Fatalf("partial ResolveFactConflictSelection: %v", err)
	}
	if partial.Status != ConflictStatusOpen || !reflect.DeepEqual(partial.SelectedFactIDs, []string{firstCandidate}) || len(changed) != 1 {
		t.Fatalf("partial conflict = %+v, changed = %+v", partial, changed)
	}
	var auditReason, auditOperator string
	if err := store.pool.QueryRow(ctx, `
		SELECT reason, resolved_by
		FROM fact_conflict_resolution_facts
		WHERE conflict_id = $1 AND fact_id = $2
	`, primary.ID, firstCandidate).Scan(&auditReason, &auditOperator); err != nil {
		t.Fatalf("read partial resolution audit: %v", err)
	}
	if auditReason != "adopt first candidate" || auditOperator != "operator-partial" {
		t.Fatalf("partial audit = %q/%q", auditReason, auditOperator)
	}

	resolved, changed, snapshot, err := store.ResolveFactConflictSelection(matchID, primary.ID, []string{secondAccepted.FactID}, "operator-final", "keep second accepted")
	if err != nil {
		t.Fatalf("final ResolveFactConflictSelection: %v", err)
	}
	expectedSelection := uniqueFactIDs([]string{firstCandidate, secondAccepted.FactID})
	if resolved.Status != ConflictStatusResolved || !reflect.DeepEqual(resolved.SelectedFactIDs, expectedSelection) || len(changed) != 0 {
		t.Fatalf("resolved conflict = %+v, changed = %+v", resolved, changed)
	}
	if snapshot.Score != (Score{Home: 1, Away: 1}) || snapshot.Integrity.Status != "ok" {
		t.Fatalf("resolved snapshot = %+v", snapshot)
	}
	persisted := store.FactConflicts(matchID)
	if len(persisted) != 1 || !reflect.DeepEqual(persisted[0].SelectedFactIDs, expectedSelection) {
		t.Fatalf("persisted selection history = %+v", persisted)
	}
}

func TestPostgresResolvesMultipleFactConflictsIndependently(t *testing.T) {
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
	matchID := "pg-independent-conflicts-" + time.Now().UTC().Format("20060102150405.000000000")
	defer store.Reset(matchID)
	if _, _, err := store.SetConfig(matchID, MatchConfig{HomeTeam: "Spain", AwayTeam: "Germany"}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}
	firstAccepted, _, err := store.Create(matchID, MatchEvent{
		Source: "operator", EventType: "goal", Period: "first_half", Clock: "10:00", TeamID: "home",
		Score: Score{Home: 1}, Description: "first accepted",
	})
	if err != nil {
		t.Fatalf("Create first accepted goal: %v", err)
	}
	_, _, err = store.Create(matchID, MatchEvent{
		Source: "provider-a", ProviderEventID: "independent-a", EventType: "goal", Period: "first_half", Clock: "10:10", TeamID: "away",
		Score: Score{Away: 1}, Description: "first candidate",
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("Create first candidate error = %v", err)
	}
	secondAccepted, _, err := store.Create(matchID, MatchEvent{
		Source: "operator", EventType: "goal", Period: "first_half", Clock: "30:00", TeamID: "home",
		Score: Score{Home: 2}, Description: "second accepted",
	})
	if err != nil {
		t.Fatalf("Create second accepted goal: %v", err)
	}
	_, _, err = store.Create(matchID, MatchEvent{
		Source: "provider-b", ProviderEventID: "independent-b", EventType: "goal", Period: "first_half", Clock: "30:10", TeamID: "away",
		Score: Score{Home: 1, Away: 1}, Description: "second candidate",
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("Create second candidate error = %v", err)
	}

	conflicts := store.FactConflicts(matchID)
	if len(conflicts) != 2 {
		t.Fatalf("open conflicts = %+v", conflicts)
	}
	firstConflict := conflictContainingFact(t, conflicts, firstAccepted.FactID)
	secondConflict := conflictContainingFact(t, conflicts, secondAccepted.FactID)
	firstCandidate := conflictCandidateFact(t, firstConflict)
	secondCandidate := conflictCandidateFact(t, secondConflict)
	var secondCandidateEvent MatchEvent
	for _, event := range store.Events(matchID) {
		if event.FactID == secondCandidate && event.Status == "active" {
			secondCandidateEvent = event
			break
		}
	}
	if secondCandidateEvent.ID == "" {
		t.Fatalf("second candidate event missing for fact %q", secondCandidate)
	}
	if _, _, snapshot, err := store.ResolveFactConflict(matchID, firstConflict.ID, firstCandidate, "operator-1", "adopt first candidate"); err != nil {
		t.Fatalf("Resolve first conflict: %v", err)
	} else if snapshot.Score != (Score{Home: 1, Away: 1}) || snapshot.Integrity.Status != "conflict" {
		t.Fatalf("first resolution snapshot = %+v", snapshot)
	}
	remaining := conflictContainingFact(t, store.FactConflicts(matchID), secondAccepted.FactID)
	if remaining.Status != ConflictStatusOpen {
		t.Fatalf("second conflict changed: %+v", remaining)
	}
	if _, _, snapshot, err := store.ResolveFactConflict(matchID, secondConflict.ID, secondAccepted.FactID, "operator-2", "keep second accepted"); err != nil {
		t.Fatalf("Resolve second conflict: %v", err)
	} else if snapshot.Score != (Score{Home: 1, Away: 1}) || snapshot.Integrity.Status != "ok" {
		t.Fatalf("second resolution snapshot = %+v", snapshot)
	}
	var rejectedUpdateCount int
	if err := store.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM outbox_messages
		WHERE aggregate_type = 'match_event' AND aggregate_id = $1
	`, secondCandidateEvent.ID+":2:revoked").Scan(&rejectedUpdateCount); err != nil {
		t.Fatalf("count rejected candidate outbox updates: %v", err)
	}
	if rejectedUpdateCount != 1 {
		t.Fatalf("keeping accepted fact enqueued %d retractions, want 1", rejectedUpdateCount)
	}
	for _, conflict := range store.FactConflicts(matchID) {
		if conflict.Status != ConflictStatusResolved {
			t.Fatalf("unresolved conflict after both choices: %+v", conflict)
		}
	}
}

func TestPostgresFactLifecycleAndRevisions(t *testing.T) {
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

	matchID := "pg-fact-lifecycle-" + time.Now().UTC().Format("20060102150405.000000000")
	if _, _, err := store.SetConfig(matchID, MatchConfig{HomeTeam: "Spain", AwayTeam: "Germany"}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}
	if err := store.SetSourceCursor(matchID, "api-sports", "fixture-42", 123); err != nil {
		t.Fatalf("SetSourceCursor error: %v", err)
	}
	if cursor, err := store.SourceCursor(matchID, "api-sports", "fixture-42"); err != nil || cursor != 123 {
		t.Fatalf("source cursor = %d, err=%v", cursor, err)
	}
	provisional, _, err := store.Create(matchID, MatchEvent{
		Source:          "api-sports",
		ProviderEventID: "fixture-lifecycle:goal-1",
		EventType:       "goal",
		Period:          "first_half",
		Clock:           "12:00",
		TeamID:          "home",
		TeamName:        "Spain",
		Score:           Score{Home: 1},
		Description:     "Spain goal",
	})
	if err != nil {
		t.Fatalf("Create provisional event error: %v", err)
	}
	if provisional.FactStatus != FactStatusProvisional || provisional.FactRevision != 1 {
		t.Fatalf("provisional event = %+v", provisional)
	}
	confirmed, snapshot, err := store.ConfirmFact(matchID, provisional.FactID, "operator-1")
	if err != nil {
		t.Fatalf("ConfirmFact error: %v", err)
	}
	if confirmed.FactStatus != FactStatusConfirmed || confirmed.FactRevision != 2 || snapshot.Score != (Score{Home: 1}) {
		t.Fatalf("confirmed event/snapshot = %+v / %+v", confirmed, snapshot)
	}
	revoked, snapshot, err := store.RevokeFact(matchID, provisional.FactID, "operator-2")
	if err != nil {
		t.Fatalf("RevokeFact error: %v", err)
	}
	if revoked.FactStatus != FactStatusRevoked || revoked.FactRevision != 3 || snapshot.Score != (Score{}) {
		t.Fatalf("revoked event/snapshot = %+v / %+v", revoked, snapshot)
	}
	revisions := store.FactRevisions(matchID, provisional.FactID)
	if len(revisions) != 3 || revisions[0].Status != FactStatusProvisional || revisions[1].Status != FactStatusConfirmed || revisions[2].Status != FactStatusRevoked {
		t.Fatalf("lifecycle revisions = %+v", revisions)
	}

	conflictMatchID := matchID + "-conflict"
	if _, _, err := store.SetConfig(conflictMatchID, MatchConfig{HomeTeam: "Spain", AwayTeam: "Germany"}); err != nil {
		t.Fatalf("SetConfig conflict match error: %v", err)
	}
	manual, _, err := store.Create(conflictMatchID, MatchEvent{
		Source: "operator", EventType: "goal", Period: "first_half", Clock: "20:00", TeamID: "home", TeamName: "Spain",
		Score: Score{Home: 1}, Description: "Spain manual goal",
	})
	if err != nil {
		t.Fatalf("Create manual conflict event error: %v", err)
	}
	_, _, err = store.Create(conflictMatchID, MatchEvent{
		Source: "api-sports", ProviderEventID: "fixture-lifecycle:goal-2", EventType: "goal", Period: "first_half", Clock: "20:10", TeamID: "away", TeamName: "Germany",
		Score: Score{Away: 1}, Description: "Germany provider goal",
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("Create conflict event error = %v, want ErrConflict", err)
	}
	provider := store.Events(conflictMatchID)[0]
	if provider.Source != "api-sports" {
		t.Fatalf("provider conflict event = %+v", provider)
	}
	reconciled, snapshot, err := store.ReconcileFact(conflictMatchID, provider.FactID, "operator-3")
	if err != nil {
		t.Fatalf("ReconcileFact error: %v", err)
	}
	if reconciled.FactStatus != FactStatusReconciled || reconciled.FactRevision != 2 || snapshot.Score != (Score{Away: 1}) {
		t.Fatalf("reconciled event/snapshot = %+v / %+v", reconciled, snapshot)
	}
	if snapshot.Integrity.Status != "ok" {
		t.Fatalf("reconciled integrity = %+v", snapshot.Integrity)
	}
	if got := store.Events(conflictMatchID); len(got) != 2 || got[1].FactStatus != FactStatusRevoked {
		t.Fatalf("reconciled conflict candidates = %+v", got)
	}
	if got := store.PublicEvents(conflictMatchID); len(got) != 1 || got[0].FactID != provider.FactID {
		t.Fatalf("public reconciled events = %+v", got)
	}
	if got := store.FactRevisions(conflictMatchID, manual.FactID); len(got) != 2 || got[1].Status != FactStatusRevoked {
		t.Fatalf("manual conflict revisions = %+v", got)
	}
	if conflicts := store.FactConflicts(conflictMatchID); len(conflicts) != 1 || conflicts[0].Status != ConflictStatusResolved || conflicts[0].ChosenFactID != provider.FactID {
		t.Fatalf("legacy reconciliation did not resolve formal conflict: %+v", conflicts)
	}
	reopened, err := OpenPostgresStore(ctx, databaseURL, "../../migrations")
	if err != nil {
		t.Fatalf("reopen lifecycle store error: %v", err)
	}
	defer reopened.Close()
	if cursor, err := reopened.SourceCursor(matchID, "api-sports", "fixture-42"); err != nil || cursor != 123 {
		t.Fatalf("reopened source cursor = %d, err=%v", cursor, err)
	}
}

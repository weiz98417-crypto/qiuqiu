package matchstate

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strconv"
	"testing"
	"time"

	"qiuqiu/internal/operatorwrite"
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

func TestPostgresStoreReportsProjectionMismatchWithoutChangingPublicSnapshot(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	store, err := OpenPostgresStore(context.Background(), databaseURL, "../../migrations")
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
	secondGoal, _, err := store.Create(matchID, MatchEvent{
		EventType: "goal", Period: "first_half", Clock: "20:00", TeamID: "home", TeamName: "西班牙",
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
	processed, err := store.publishOutboxOnce(ctx)
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
	if snapshot := store.Snapshot(matchID); snapshot.Integrity.Status != "conflict" || snapshot.Score != (Score{}) {
		t.Fatalf("conflict snapshot mismatch: %+v", snapshot)
	}
	conflictEvents := store.Events(matchID)
	if len(conflictEvents) != 2 || conflictEvents[0].FactStatus != FactStatusConflict || conflictEvents[1].FactStatus != FactStatusConflict {
		t.Fatalf("conflict candidates = %+v", conflictEvents)
	}
	manualRevisionFound := false
	providerRevisionFound := false
	for _, event := range conflictEvents {
		revisions := store.FactRevisions(matchID, event.FactID)
		switch len(revisions) {
		case 2:
			if revisions[0].Status != FactStatusConfirmed || revisions[1].Status != FactStatusConflict {
				t.Fatalf("manual fact revisions = %+v", revisions)
			}
			manualRevisionFound = true
		case 1:
			if revisions[0].Status != FactStatusConflict {
				t.Fatalf("provider fact revisions = %+v", revisions)
			}
			providerRevisionFound = true
		default:
			t.Fatalf("unexpected fact revisions for %s = %+v", event.FactID, revisions)
		}
	}
	if !manualRevisionFound || !providerRevisionFound {
		t.Fatalf("fact revision counts: manual=%v provider=%v", manualRevisionFound, providerRevisionFound)
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
	if got := store.FactRevisions(conflictMatchID, manual.FactID); len(got) != 3 || got[2].Status != FactStatusRevoked {
		t.Fatalf("manual conflict revisions = %+v", got)
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

package companion

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/privacy"
	"qiuqiu/internal/relationship"
)

func TestPostgresUserWritesAreBlockedDuringDeletion(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	migrations, err := matchstate.OpenPostgresStore(ctx, databaseURL, "../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	defer migrations.Close()
	privacyStore, err := privacy.OpenPostgresStore(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer privacyStore.Close()
	traces, err := OpenPostgresTraceWriter(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer traces.Close()
	repository, err := relationship.OpenPostgresRepository(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()

	userID := "privacy-write-block-" + time.Now().UTC().Format("20060102150405.000000000")
	status, err := privacyStore.RequestDeletion(ctx, userID, "test")
	if err != nil || status.Status != "pending" {
		t.Fatalf("deletion status = %+v, err=%v", status, err)
	}
	if err := traces.WriteTrace(ctx, Trace{
		ID:      userID + "-trace",
		MatchID: userID + "-match",
		UserID:  userID,
		Input:   "blocked",
		Intent:  IntentSmalltalk,
		Output:  "blocked",
	}); !errors.Is(err, privacy.ErrDeletionInProgress) {
		t.Fatalf("trace write error = %v", err)
	}
	if err := traces.UpdateTrace(ctx, Trace{ID: userID + "-trace", MatchID: userID + "-match", UserID: userID}); !errors.Is(err, privacy.ErrDeletionInProgress) {
		t.Fatalf("trace update error = %v", err)
	}
	if _, err := repository.Load(ctx, userID, userID+"-match"); !errors.Is(err, privacy.ErrDeletionInProgress) {
		t.Fatalf("relationship load error = %v", err)
	}
	if err := repository.CompareAndSwap(ctx, relationship.ExpectedVersions{}, relationship.StateUpdate{
		Relationship: relationship.RelationshipState{UserID: userID},
		Match:        relationship.MatchCompanionState{UserID: userID, MatchID: userID + "-match"},
		Decision:     relationship.Decision{ID: userID + "-decision", SignalID: userID + "-signal"},
	}); !errors.Is(err, privacy.ErrDeletionInProgress) {
		t.Fatalf("relationship write error = %v", err)
	}
	if err := privacyStore.ProcessDeletion(ctx, userID); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresDeletionWaitsForInFlightUserWriteAndRemovesIt(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	migrations, err := matchstate.OpenPostgresStore(ctx, databaseURL, "../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	defer migrations.Close()
	privacyStore, err := privacy.OpenPostgresStore(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer privacyStore.Close()
	traces, err := OpenPostgresTraceWriter(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer traces.Close()

	userID := "privacy-race-" + time.Now().UTC().Format("20060102150405.000000000")
	matchID := userID + "-match"
	traceID := userID + "-trace"
	defer func() {
		_, _ = traces.pool.Exec(context.Background(), `
			DELETE FROM privacy_tombstones WHERE user_id = $1;
			DELETE FROM conversation_turns WHERE user_id = $1;
			DELETE FROM agent_traces WHERE user_id = $1;
			DELETE FROM matches WHERE id = $2
		`, userID, matchID)
	}()

	writeTx, err := traces.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer writeTx.Rollback(ctx)
	if err := privacy.LockUserTx(ctx, writeTx, userID); err != nil {
		t.Fatal(err)
	}
	if err := privacy.CheckDeletionTx(ctx, writeTx, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := writeTx.Exec(ctx, `
		INSERT INTO matches (id, home_team, away_team, updated_at)
		VALUES ($1, 'Home', 'Away', now())
		ON CONFLICT (id) DO NOTHING
	`, matchID); err != nil {
		t.Fatal(err)
	}
	if _, err := writeTx.Exec(ctx, `
		INSERT INTO agent_traces (id, match_id, user_id, input, intent, output, reason, created_at, expires_at)
		VALUES ($1, $2, $3, 'in flight', 'smalltalk', 'stored', 'race test', now(), now() + interval '1 day')
	`, traceID, matchID, userID); err != nil {
		t.Fatal(err)
	}

	type deletionResult struct {
		status privacy.Status
		err    error
	}
	deletionDone := make(chan deletionResult, 1)
	go func() {
		status, requestErr := privacyStore.RequestDeletion(ctx, userID, "concurrent_test")
		deletionDone <- deletionResult{status: status, err: requestErr}
	}()

	blockedDeadline := time.Now().Add(3 * time.Second)
	blocked := false
	for !blocked {
		select {
		case result := <-deletionDone:
			t.Fatalf("deletion bypassed in-flight user write: status=%+v err=%v", result.status, result.err)
		default:
		}
		if err := traces.pool.QueryRow(ctx, `
			WITH privacy_key AS (
				SELECT hashtextextended('privacy:' || $1, 0) AS value
			)
			SELECT EXISTS (
				SELECT 1
				FROM pg_locks, privacy_key
				WHERE locktype = 'advisory'
					AND NOT granted
					AND classid::text::numeric * 4294967296 + objid::text::numeric =
						CASE WHEN privacy_key.value < 0
							THEN privacy_key.value::numeric + 18446744073709551616
							ELSE privacy_key.value::numeric
						END
			)
		`, userID).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if !blocked && time.Now().After(blockedDeadline) {
			t.Fatal("deletion request did not reach the user advisory lock")
		}
		if !blocked {
			time.Sleep(10 * time.Millisecond)
		}
	}

	if err := writeTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var requested deletionResult
	select {
	case requested = <-deletionDone:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if requested.err != nil || requested.status.Status != "pending" {
		t.Fatalf("deletion request status=%+v err=%v", requested.status, requested.err)
	}
	if err := privacyStore.ProcessDeletion(ctx, userID); err != nil {
		t.Fatal(err)
	}

	var remaining int
	if err := traces.pool.QueryRow(ctx, `SELECT COUNT(*) FROM agent_traces WHERE user_id = $1`, userID).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("in-flight trace survived deletion: %d", remaining)
	}
	if err := traces.WriteTrace(ctx, Trace{
		ID:      traceID + "-late",
		MatchID: matchID,
		UserID:  userID,
		Input:   "late write",
		Intent:  IntentSmalltalk,
		Output:  "must be rejected",
	}); !errors.Is(err, privacy.ErrDataDeleted) {
		t.Fatalf("late trace write error = %v", err)
	}
}

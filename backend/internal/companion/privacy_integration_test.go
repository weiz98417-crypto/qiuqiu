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

package conversation

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"qiuqiu/internal/matchstate"
)

func TestPostgresDeliveryLedgerMatchesStateMachineContract(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	migrations, err := matchstate.OpenPostgresStore(ctx, databaseURL, "../../migrations")
	if err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	defer migrations.Close()
	store, err := OpenPostgresDeliveryStore(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open delivery store: %v", err)
	}
	defer store.Close()

	now := time.Now().UTC()
	suffix := now.Format("20060102150405.000000000")
	userID := "delivery-user-" + suffix
	matchID := "delivery-match-" + suffix
	ledger := store.For(userID, matchID)
	record := DeliveryRecord{Key: "delivery-" + suffix, DeliveryKey: "client-" + suffix, TraceID: "trace-" + suffix, UserID: userID, MatchID: matchID, Critical: true, ExpiresAt: now.Add(time.Minute), UpdatedAt: now}
	if _, err := ledger.Plan(record); err != nil {
		t.Fatalf("plan: %v", err)
	}
	if found, ok := ledger.FindByDeliveryKey(record.DeliveryKey); !ok || found.Key != record.Key {
		t.Fatalf("find by delivery key = %+v, %v", found, ok)
	}
	duplicate := record
	duplicate.Key = record.Key + "-duplicate"
	duplicate.TraceID = record.TraceID + "-duplicate"
	if _, err := ledger.Plan(duplicate); !errors.Is(err, ErrDeliveryConflict) {
		t.Fatalf("duplicate client delivery key error = %v", err)
	}
	if _, err := ledger.Transition(record.Key, DeliveryTextDelivered, now.Add(time.Millisecond)); err != nil {
		t.Fatalf("text delivery: %v", err)
	}
	if _, err := ledger.Transition(record.Key, DeliveryCompleted, now.Add(2*time.Millisecond)); err != nil {
		t.Fatalf("completion: %v", err)
	}
	if _, err := ledger.Transition(record.Key, DeliveryAudioStarted, now.Add(3*time.Millisecond)); !errors.Is(err, ErrDeliveryConflict) {
		t.Fatalf("terminal transition error = %v", err)
	}
	if pending := ledger.Pending(now); len(pending) != 0 {
		t.Fatalf("completed delivery remained pending: %+v", pending)
	}
}

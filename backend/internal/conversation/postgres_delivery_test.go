package conversation

import (
	"context"
	"testing"
)

func TestPostgresDeliveryStoreForContextKeepsCancellationContext(t *testing.T) {
	store := &PostgresDeliveryStore{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ledger, ok := store.ForContext(ctx, "user-1", "match-1").(*postgresDeliveryLedger)
	if !ok {
		t.Fatal("expected postgres delivery ledger")
	}
	if ledger.context() != ctx {
		t.Fatal("delivery ledger did not retain caller context")
	}
}

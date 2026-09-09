package interaction

import (
	"context"
	"testing"
	"time"
)

func TestMemoryLedgerListPageUsesStableSemanticCursor(t *testing.T) {
	ledger := NewMemoryLedger()
	now := time.Date(2026, 9, 8, 20, 0, 0, 0, time.UTC)
	for _, event := range []Event{
		{ID: "ended", Kind: KindPlaybackResult, PlaybackState: "ended", UserID: "u1", MatchID: "m1", CreatedAt: now},
		{ID: "turn", Kind: KindTurnPlanned, UserID: "u1", MatchID: "m1", CreatedAt: now},
		{ID: "started", Kind: KindPlaybackResult, PlaybackState: "started", UserID: "u1", MatchID: "m1", CreatedAt: now},
		{ID: "other", Kind: KindSignal, UserID: "u1", MatchID: "m2", CreatedAt: now},
	} {
		if _, err := ledger.Append(context.Background(), event); err != nil {
			t.Fatalf("append %s: %v", event.ID, err)
		}
	}
	first, err := ledger.ListPage(context.Background(), PageQuery{UserID: "u1", MatchID: "m1", Limit: 2})
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(first.Events) != 2 || first.Events[0].ID != "turn" || first.Events[1].ID != "started" || first.NextCursor == "" {
		t.Fatalf("unexpected first page: %+v", first)
	}
	second, err := ledger.ListPage(context.Background(), PageQuery{UserID: "u1", MatchID: "m1", Limit: 2, Cursor: first.NextCursor})
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(second.Events) != 1 || second.Events[0].ID != "ended" || second.NextCursor != "" {
		t.Fatalf("unexpected second page: %+v", second)
	}
}

func TestMemoryLedgerListPageRejectsInvalidCursor(t *testing.T) {
	ledger := NewMemoryLedger()
	if _, err := ledger.ListPage(context.Background(), PageQuery{UserID: "u1", Cursor: "not-a-cursor"}); err != ErrInvalidCursor {
		t.Fatalf("error=%v, want ErrInvalidCursor", err)
	}
}

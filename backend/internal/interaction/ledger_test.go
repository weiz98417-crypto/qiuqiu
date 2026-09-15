package interaction

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"qiuqiu/internal/privacy"
)

func TestMemoryLedgerListReturnsLatestLimitInChronologicalOrder(t *testing.T) {
	ledger := NewMemoryLedger()
	now := time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC)
	for index := 1; index <= 4; index++ {
		event := Event{
			ID: "event-" + strconv.Itoa(index), Kind: KindSignal, UserID: "u1", MatchID: "m1",
			CreatedAt: now.Add(time.Duration(index) * time.Second),
		}
		if _, err := ledger.Append(context.Background(), event); err != nil {
			t.Fatalf("append event %d: %v", index, err)
		}
	}
	events, err := ledger.List(context.Background(), "u1", "m1", 2)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(events) != 2 || events[0].ID != "event-3" || events[1].ID != "event-4" {
		t.Fatalf("latest events = %+v", events)
	}
}

func TestMemoryLedgerUsesConfiguredPrivacyRetention(t *testing.T) {
	privacy.SetRetentionDays(7)
	t.Cleanup(func() { privacy.SetRetentionDays(30) })
	createdAt := time.Date(2026, time.September, 7, 12, 0, 0, 0, time.UTC)
	ledger := NewMemoryLedger()

	event, err := ledger.Append(context.Background(), Event{ID: "retention", Kind: KindSignal, UserID: "u1", MatchID: "m1", CreatedAt: createdAt})
	if err != nil {
		t.Fatalf("Append error: %v", err)
	}
	if want := createdAt.Add(7 * 24 * time.Hour); !event.ExpiresAt.Equal(want) {
		t.Fatalf("ExpiresAt = %s, want %s", event.ExpiresAt, want)
	}
}

func TestMemoryLedgerIsAppendOnlyAndIdempotent(t *testing.T) {
	ledger := NewMemoryLedger()
	event := Event{ID: "turn-1", Kind: KindTurnPlanned, UserID: "user-1", MatchID: "match-1", TraceID: "trace-1", CreatedAt: time.Now()}
	first, err := ledger.Append(context.Background(), event)
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	second, err := ledger.Append(context.Background(), event)
	if err != nil || second.ID != first.ID {
		t.Fatalf("duplicate append = %+v err=%v", second, err)
	}
	_, err = ledger.Append(context.Background(), Event{ID: "turn-1", Kind: KindDelivery, UserID: "user-1", MatchID: "match-1"})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("conflict = %v", err)
	}
	got, err := ledger.List(context.Background(), "user-1", "match-1", 10)
	if err != nil || len(got) != 1 || got[0].TraceID != "trace-1" {
		t.Fatalf("events = %+v", got)
	}
}

func TestMemoryLedgerSupportsMediaAndPlaybackCorrelation(t *testing.T) {
	ledger := NewMemoryLedger()
	for _, event := range []Event{
		{ID: "signal-1", Kind: KindSignal, UserID: "u1", MatchID: "m1", SignalID: "s1"},
		{ID: "fact-1", Kind: KindFactRevision, UserID: "u1", MatchID: "m1", FactRevision: "2:confirmed", FactIDs: []string{"f1"}},
		{ID: "media-1", Kind: KindMediaDelivery, UserID: "u1", MatchID: "m1", TraceID: "t1", DeliveryKey: "d1", MediaType: "audio/mpeg"},
		{ID: "playback-1", Kind: KindPlaybackResult, UserID: "u1", MatchID: "m1", TraceID: "t1", PlaybackState: "completed"},
	} {
		if _, err := ledger.Append(context.Background(), event); err != nil {
			t.Fatalf("append %s: %v", event.ID, err)
		}
	}
	events, err := ledger.List(context.Background(), "u1", "m1", 10)
	if err != nil || len(events) != 4 {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	journey := ProjectJourney(events)
	if journey.TurnCount != 0 || journey.DeliveryCount != 2 {
		t.Fatalf("derived journey counts = %+v", journey)
	}
}

func TestMemoryLedgerListsLongitudinalUserJourney(t *testing.T) {
	ledger := NewMemoryLedger()
	now := time.Now().UTC()
	for index, matchID := range []string{"m1", "m2"} {
		if _, err := ledger.Append(context.Background(), Event{ID: matchID, Kind: KindSignal, UserID: "u1", MatchID: matchID, CreatedAt: now.Add(time.Duration(index) * time.Second)}); err != nil {
			t.Fatal(err)
		}
	}
	events, err := ledger.ListUser(context.Background(), "u1", 10)
	if err != nil || len(events) != 2 || events[1].MatchID != "m2" {
		t.Fatalf("longitudinal events = %+v, %v", events, err)
	}
}

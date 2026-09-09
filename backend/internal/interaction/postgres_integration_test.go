package interaction

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"qiuqiu/internal/matchstate"
)

func TestPostgresLedgerMatchesAppendOnlyContract(t *testing.T) {
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
	ledger, err := OpenPostgresLedger(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer ledger.Close()

	now := time.Now().UTC()
	suffix := now.Format("20060102150405.000000000")
	event := Event{ID: "interaction-" + suffix, Kind: KindTurnPlanned, UserID: "interaction-user-" + suffix, MatchID: "interaction-match-" + suffix, TraceID: "trace-" + suffix, DeliveryReason: "provider unavailable", TracePayload: json.RawMessage(`{"id":"trace-payload"}`), CreatedAt: now}
	first, err := ledger.Append(ctx, event)
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	retried, err := ledger.Append(ctx, event)
	if err != nil || retried.ID != first.ID {
		t.Fatalf("idempotent append = %+v, %v", retried, err)
	}
	conflict := event
	conflict.TraceID = "different"
	if _, err := ledger.Append(ctx, conflict); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting append error = %v", err)
	}
	events, err := ledger.List(ctx, event.UserID, event.MatchID, 10)
	if err != nil || len(events) != 1 || events[0].TraceID != event.TraceID || events[0].DeliveryReason != event.DeliveryReason || !sameTracePayload(events[0].TracePayload, event.TracePayload) {
		t.Fatalf("list = %+v, %v", events, err)
	}
	for index := 1; index <= 2; index++ {
		additional := event
		additional.ID = event.ID + "-page-" + time.Duration(index).String()
		additional.TraceID = event.TraceID + "-page-" + time.Duration(index).String()
		additional.CreatedAt = now.Add(time.Duration(index) * time.Second)
		if _, err := ledger.Append(ctx, additional); err != nil {
			t.Fatalf("append page event: %v", err)
		}
	}
	firstPage, err := ledger.ListPage(ctx, PageQuery{UserID: event.UserID, MatchID: event.MatchID, Limit: 2})
	if err != nil || len(firstPage.Events) != 2 || firstPage.NextCursor == "" {
		t.Fatalf("first page = %+v, %v", firstPage, err)
	}
	secondPage, err := ledger.ListPage(ctx, PageQuery{UserID: event.UserID, MatchID: event.MatchID, Limit: 2, Cursor: firstPage.NextCursor})
	if err != nil || len(secondPage.Events) != 1 || secondPage.NextCursor != "" {
		t.Fatalf("second page = %+v, %v", secondPage, err)
	}
	latest, err := ledger.List(ctx, event.UserID, event.MatchID, 2)
	if err != nil || len(latest) != 2 || latest[0].ID != event.ID+"-page-1ns" || latest[1].ID != event.ID+"-page-2ns" {
		t.Fatalf("latest list = %+v, %v", latest, err)
	}
	snapshot, err := ledger.ListSnapshot(ctx, event.UserID, event.MatchID)
	if err != nil || len(snapshot) != 3 || snapshot[0].DeliveryReason != event.DeliveryReason {
		t.Fatalf("snapshot = %+v, %v", snapshot, err)
	}
	otherUser := event
	otherUser.ID += "-other-user"
	otherUser.UserID += "-other"
	otherUser.TraceID += "-other"
	otherUser.CreatedAt = now.Add(3 * time.Second)
	if _, err := ledger.Append(ctx, otherUser); err != nil {
		t.Fatalf("append other user: %v", err)
	}
	matchSnapshot, err := ledger.ListMatchSnapshot(ctx, event.MatchID)
	if err != nil || len(matchSnapshot) != 4 || matchSnapshot[3].UserID != otherUser.UserID {
		t.Fatalf("match snapshot = %+v, %v", matchSnapshot, err)
	}
}

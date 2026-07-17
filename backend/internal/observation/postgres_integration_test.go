package observation

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"qiuqiu/internal/matchstate"
)

func TestPostgresCoordinatorRecoversPendingAndStableResolution(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	migrations, err := matchstate.OpenPostgresStore(ctx, databaseURL, "../../migrations")
	if err != nil {
		t.Fatalf("migrations: %v", err)
	}
	defer migrations.Close()

	userID := fmt.Sprintf("observation-pg-%d", time.Now().UnixNano())
	matchID := userID + "-match"
	now := time.Now().UTC()
	first, err := OpenPostgresCoordinator(ctx, databaseURL)
	if err != nil {
		t.Fatalf("OpenPostgresCoordinator: %v", err)
	}
	pending, err := first.Record(ctx, Input{
		SignalID: "turn-1", TraceID: "trace-1", UserID: userID, MatchID: matchID,
		Kind: "event", EventType: "goal", ClaimedPlayer: "萨拉赫", ReceivedAt: now,
		ReconcileWindow: 2 * time.Minute,
	})
	if err != nil {
		first.Close()
		t.Fatalf("Record: %v", err)
	}
	first.Close()

	reopened, err := OpenPostgresCoordinator(ctx, databaseURL)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	restored, ok, err := reopened.Get(ctx, pending.ID)
	if err != nil || !ok || restored.Status != StatusPendingSync {
		t.Fatalf("restored = %+v ok=%v err=%v", restored, ok, err)
	}
	if restored.ReconcileUntil.Before(now.Add(119 * time.Second)) {
		t.Fatalf("reconcile window was not persisted: %s", restored.ReconcileUntil)
	}
	event := matchstate.MatchEvent{
		MatchID: matchID, ID: "goal-1", FactID: "fact-1", FactRevision: 2,
		FactStatus: matchstate.FactStatusConfirmed, EventType: "goal", PlayerName: "萨拉赫",
		CreatedAt: now.Add(5 * time.Second).Format(time.RFC3339Nano),
	}
	resolved, err := reopened.OnFactChanged(ctx, event)
	if err != nil || len(resolved) != 1 || resolved[0].Status != StatusConfirmed {
		t.Fatalf("resolved = %+v err=%v", resolved, err)
	}
	wantKey := pending.ID + ":2:confirmed"
	if resolved[0].DeliveryKey != wantKey {
		t.Fatalf("delivery key = %q, want %q", resolved[0].DeliveryKey, wantKey)
	}

	reopened.Close()
	again, err := OpenPostgresCoordinator(ctx, databaseURL)
	if err != nil {
		t.Fatalf("reopen after resolution: %v", err)
	}
	defer again.Close()
	defer again.DeleteUser(context.Background(), userID)
	repeated, err := again.OnFactChanged(ctx, event)
	if err != nil || len(repeated) != 1 || repeated[0].DeliveryKey != wantKey {
		t.Fatalf("repeated = %+v err=%v", repeated, err)
	}
	recovered, err := again.PendingResolutions(ctx, userID, matchID, now.Add(6*time.Second))
	if err != nil || len(recovered) != 1 || recovered[0].DeliveryKey != wantKey {
		t.Fatalf("recovered outbox = %+v err=%v", recovered, err)
	}
	if err := again.MarkResolutionDelivered(ctx, wantKey, now.Add(7*time.Second)); err != nil {
		t.Fatalf("MarkResolutionDelivered: %v", err)
	}
	recovered, err = again.PendingResolutions(ctx, userID, matchID, now.Add(8*time.Second))
	if err != nil || len(recovered) != 0 {
		t.Fatalf("delivered resolution recovered again: %+v err=%v", recovered, err)
	}
}

func TestPostgresCoordinatorSuppressesInBandFollowUpBeforeOrAfterResolution(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	migrations, err := matchstate.OpenPostgresStore(ctx, databaseURL, "../../migrations")
	if err != nil {
		t.Fatalf("migrations: %v", err)
	}
	defer migrations.Close()
	coordinator, err := OpenPostgresCoordinator(ctx, databaseURL)
	if err != nil {
		t.Fatalf("OpenPostgresCoordinator: %v", err)
	}
	defer coordinator.Close()
	now := time.Now().UTC()

	for index, suppressBeforeResolution := range []bool{true, false} {
		userID := fmt.Sprintf("observation-suppress-%d-%d", time.Now().UnixNano(), index)
		matchID := userID + "-match"
		pending, err := coordinator.Record(ctx, Input{
			SignalID: "turn-1", TraceID: "trace-1", UserID: userID, MatchID: matchID,
			Kind: "event", EventType: "goal", ClaimedPlayer: "萨拉赫", ReceivedAt: now,
		})
		if err != nil {
			t.Fatalf("Record: %v", err)
		}
		if suppressBeforeResolution {
			if err := coordinator.SuppressFollowUp(ctx, pending.ID, now.Add(time.Second)); err != nil {
				t.Fatalf("SuppressFollowUp before resolution: %v", err)
			}
		}
		if _, err := coordinator.OnFactChanged(ctx, matchstate.MatchEvent{
			MatchID: matchID, ID: "goal-1", FactID: "fact-1", FactRevision: 2,
			FactStatus: matchstate.FactStatusConfirmed, EventType: "goal", PlayerName: "萨拉赫",
			CreatedAt: now.Add(5 * time.Second).Format(time.RFC3339Nano),
		}); err != nil {
			t.Fatalf("OnFactChanged: %v", err)
		}
		if !suppressBeforeResolution {
			if err := coordinator.SuppressFollowUp(ctx, pending.ID, now.Add(6*time.Second)); err != nil {
				t.Fatalf("SuppressFollowUp after resolution: %v", err)
			}
		}
		recovered, err := coordinator.PendingResolutions(ctx, userID, matchID, now.Add(7*time.Second))
		if err != nil || len(recovered) != 0 {
			t.Fatalf("suppressed resolution recovered (before=%v): %+v err=%v", suppressBeforeResolution, recovered, err)
		}
		if err := coordinator.DeleteUser(ctx, userID); err != nil {
			t.Fatalf("DeleteUser: %v", err)
		}
	}
}

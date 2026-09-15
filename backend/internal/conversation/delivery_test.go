package conversation

import (
	"errors"
	"testing"
	"time"
)

func TestDeliveryLedgerEnforcesMonotonicLifecycle(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	ledger := NewMemoryDeliveryLedger()
	if _, err := ledger.Plan(DeliveryRecord{Key: "delivery-1", TraceID: "trace-1", UpdatedAt: now}); err != nil {
		t.Fatalf("plan: %v", err)
	}
	for _, state := range []DeliveryState{DeliveryTextDelivered, DeliveryAudioStarted, DeliveryCompleted} {
		if _, err := ledger.Transition("delivery-1", state, now); err != nil {
			t.Fatalf("transition to %q: %v", state, err)
		}
	}
	if _, err := ledger.Transition("delivery-1", DeliveryInterrupted, now); !errors.Is(err, ErrDeliveryConflict) {
		t.Fatalf("terminal transition error = %v", err)
	}
	if record, ok := ledger.Get("delivery-1"); !ok || record.State != DeliveryCompleted {
		t.Fatalf("record = %+v, ok=%v", record, ok)
	}
}

func TestDeliveryLedgerIsIdempotentAndFiltersExpiredPending(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	ledger := NewMemoryDeliveryLedger()
	record := DeliveryRecord{Key: "delivery-2", TraceID: "trace-2", UpdatedAt: now, ExpiresAt: now.Add(time.Minute)}
	first, err := ledger.Plan(record)
	if err != nil {
		t.Fatalf("first plan: %v", err)
	}
	second, err := ledger.Plan(record)
	if err != nil || first != second {
		t.Fatalf("duplicate plan = %+v err=%v", second, err)
	}
	if _, err := ledger.Transition(record.Key, DeliveryTextDelivered, now); err != nil {
		t.Fatalf("text transition: %v", err)
	}
	if _, err := ledger.Transition(record.Key, DeliveryTextDelivered, now); err != nil {
		t.Fatalf("duplicate transition: %v", err)
	}
	if pending := ledger.Pending(now.Add(2 * time.Minute)); len(pending) != 0 {
		t.Fatalf("expired pending = %+v", pending)
	}
}

func TestDeliveryLedgerRejectsDeliveryKeyReuseForAnotherEnvelope(t *testing.T) {
	ledger := NewMemoryDeliveryLedger()
	record := DeliveryRecord{Key: "trace-1", DeliveryKey: "match:event:1", TraceID: "trace-1", UserID: "u1", MatchID: "m1"}
	if _, err := ledger.Plan(record); err != nil {
		t.Fatal(err)
	}
	record.DeliveryKey = "match:event:2"
	if _, err := ledger.Plan(record); !errors.Is(err, ErrDeliveryConflict) {
		t.Fatalf("delivery key conflict = %v", err)
	}
}

func TestDeliveryLedgerTracksTextAcknowledgementWithoutCompletingAudio(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	ledger := NewMemoryDeliveryLedger()
	if _, err := ledger.Plan(DeliveryRecord{Key: "delivery-ack", TraceID: "trace-ack", Critical: true, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.Transition("delivery-ack", DeliveryTextDelivered, now); err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.AcknowledgeText("delivery-ack", now.Add(time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	record, ok := ledger.Get("delivery-ack")
	if !ok || !record.TextAcknowledged || record.State != DeliveryTextDelivered {
		t.Fatalf("record = %+v, ok=%v", record, ok)
	}
	if pending := ledger.Pending(now); len(pending) != 1 {
		t.Fatalf("pending should retain media lifecycle: %+v", pending)
	}
}

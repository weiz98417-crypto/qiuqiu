package conversation

import (
	"testing"
	"time"
)

func TestDeliveryTrackerOwnsLifecycleAndPendingMetadata(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	tracker := NewDeliveryTracker(nil)
	if err := tracker.Plan(DeliveryRecord{Key: "trace-1", TraceID: "trace-1", State: DeliveryPlanned, UpdatedAt: now}, "metadata"); err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if err := tracker.Transition("trace-1", DeliveryTextDelivered, now.Add(time.Millisecond)); err != nil {
		t.Fatalf("Transition: %v", err)
	}
	record, ok := tracker.Ledger().Get("trace-1")
	if !ok || record.State != DeliveryTextDelivered {
		t.Fatalf("record = %+v, ok=%v", record, ok)
	}
	pending := tracker.Drain()
	if len(pending) != 1 || pending[0] != "metadata" {
		t.Fatalf("pending = %+v", pending)
	}
}

func TestDeliveryTrackerDrainSkipsTerminalDeliveries(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	tracker := NewDeliveryTracker(nil)
	for _, key := range []string{"active", "complete"} {
		if err := tracker.Plan(DeliveryRecord{Key: key, TraceID: key, State: DeliveryPlanned, UpdatedAt: now}, key); err != nil {
			t.Fatalf("Plan(%s): %v", key, err)
		}
	}
	if err := tracker.Transition("active", DeliveryTextDelivered, now.Add(time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if err := tracker.Transition("complete", DeliveryTextDelivered, now.Add(time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if err := tracker.Transition("complete", DeliveryCompleted, now.Add(2*time.Millisecond)); err != nil {
		t.Fatal(err)
	}

	pending := tracker.Drain()
	if len(pending) != 1 || pending[0] != "active" {
		t.Fatalf("pending = %+v", pending)
	}
}

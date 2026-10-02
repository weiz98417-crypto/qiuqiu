package observation

import (
	"testing"
	"time"
)

func TestClientHealthLedgerRecordAndSnapshot(t *testing.T) {
	ledger := NewClientHealthLedger()
	base := time.Date(2026, 10, 3, 20, 0, 0, 0, time.UTC)
	ledger.Record("self_interrupt_suspected", "m1", base)
	ledger.Record("self_interrupt_suspected", "m1", base.Add(time.Second))
	ledger.Record("duplex_degraded", "m2", base.Add(2*time.Second))

	counters, recent := ledger.Snapshot()
	if counters["self_interrupt_suspected"] != 2 || counters["duplex_degraded"] != 1 {
		t.Fatalf("counters = %v, want suspected=2 degraded=1", counters)
	}
	if len(recent) != 3 || recent[0].Kind != "duplex_degraded" || recent[2].Kind != "self_interrupt_suspected" {
		t.Fatalf("recent = %+v, want newest first", recent)
	}
	if recent[0].MatchID != "m2" {
		t.Fatalf("matchId = %q, want m2", recent[0].MatchID)
	}
}

func TestClientHealthLedgerRecentRingEvictsOldest(t *testing.T) {
	ledger := NewClientHealthLedger()
	base := time.Date(2026, 10, 3, 20, 0, 0, 0, time.UTC)
	for i := 0; i < clientHealthRecentCapacity+10; i++ {
		ledger.Record("k", "m", base.Add(time.Duration(i)*time.Second))
	}
	counters, recent := ledger.Snapshot()
	if counters["k"] != int64(clientHealthRecentCapacity+10) {
		t.Fatalf("counter = %d, want %d (计数不随环形淘汰)", counters["k"], clientHealthRecentCapacity+10)
	}
	if len(recent) != clientHealthRecentCapacity {
		t.Fatalf("recent len = %d, want %d", len(recent), clientHealthRecentCapacity)
	}
	if recent[0].At != base.Add(time.Duration(clientHealthRecentCapacity+9)*time.Second) {
		t.Fatalf("newest = %v, want the last recorded instant", recent[0].At)
	}
}

func TestClientHealthLedgerNilSafeAndEmptyKindIgnored(t *testing.T) {
	var ledger *ClientHealthLedger
	ledger.Record("k", "m", time.Now()) // nil 接收者不炸
	counters, recent := ledger.Snapshot()
	if len(counters) != 0 || len(recent) != 0 {
		t.Fatalf("nil ledger snapshot = %v/%v, want empty", counters, recent)
	}
	real := NewClientHealthLedger()
	real.Record("", "m", time.Now())
	if counters, recent := real.Snapshot(); len(counters) != 0 || len(recent) != 0 {
		t.Fatalf("empty kind recorded: %v/%v", counters, recent)
	}
}

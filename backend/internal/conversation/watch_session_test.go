package conversation

import (
	"context"
	"errors"
	"testing"
	"time"

	"qiuqiu/internal/relationship"
)

type recoverySourceStub struct {
	payloads map[string]RecoveryPayload
	errors   map[string]error
}

func (source recoverySourceStub) ResolveRecovery(_ context.Context, matchID, traceID string) (RecoveryPayload, error) {
	if err := source.errors[traceID]; err != nil {
		return RecoveryPayload{}, err
	}
	payload, ok := source.payloads[traceID]
	if !ok || payload.MatchID != matchID {
		return RecoveryPayload{}, errors.New("recovery payload not found")
	}
	return payload, nil
}

func TestWatchSessionUsesStableLogicalKeyAndTracksAttachments(t *testing.T) {
	session := NewWatchSession(context.Background(), "user-1", "match-1", Config{}, nil)
	defer session.Close()
	if session.Key != "user-1:match-1" {
		t.Fatalf("key = %q", session.Key)
	}
	if session.Attached() {
		t.Fatal("new session should not be attached")
	}
	session.Attach()
	session.Attach()
	if !session.Attached() {
		t.Fatal("session should be attached")
	}
	session.Detach()
	session.Detach()
	session.Detach()
	if session.Attached() {
		t.Fatal("session should be detached")
	}
	if session.LastSeen().IsZero() {
		t.Fatal("last seen should be recorded")
	}

	planned, err := session.Ledger().Plan(DeliveryRecord{Key: "trace-1", UserID: "user-1", MatchID: "match-1", ExpiresAt: time.Now().Add(time.Minute)})
	if err != nil || planned.State != DeliveryPlanned {
		t.Fatalf("plan = %+v err=%v", planned, err)
	}
	if pending := session.PendingDeliveries(time.Now()); len(pending) != 1 {
		t.Fatalf("pending = %+v", pending)
	}
}

func TestWatchSessionRegistryRecoversPendingDeliveryAcrossAttachments(t *testing.T) {
	registry := NewWatchSessionRegistry(context.Background(), Config{})
	first := registry.Acquire("user-1", "match-1")
	_, err := first.Ledger().Plan(DeliveryRecord{Key: "critical-1", UserID: "user-1", MatchID: "match-1", Critical: true, ExpiresAt: time.Now().Add(time.Minute)})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	first.Detach()
	first.Close()

	second := registry.Acquire("user-1", "match-1")
	defer second.Close()
	if pending := second.PendingDeliveries(time.Now()); len(pending) != 1 || pending[0].Key != "critical-1" {
		t.Fatalf("recovered pending = %+v", pending)
	}
}

func TestWatchSessionRegistryDoesNotMergeAnonymousConnections(t *testing.T) {
	registry := NewWatchSessionRegistry(context.Background(), Config{})
	defer registry.Close()
	first := registry.Acquire("connection:1", "match-1")
	second := registry.Acquire("connection:2", "match-1")
	if first == second || first.Scheduler() == second.Scheduler() || first.Ledger() == second.Ledger() {
		t.Fatal("anonymous connections shared logical session state")
	}
}

func TestWatchSessionRegistryBindReusesExistingIdentitySession(t *testing.T) {
	registry := NewWatchSessionRegistry(context.Background(), Config{})
	defer registry.Close()
	existing := registry.Acquire("user-1", "match-1")
	anonymous := registry.Acquire("connection:1", "match-1")
	bound := registry.Bind(anonymous, "user-1", "match-1")
	if bound != existing {
		t.Fatal("bind did not return the existing logical session")
	}
	reacquiredAnonymous := registry.Acquire("connection:1", "match-1")
	defer reacquiredAnonymous.Close()
	if reacquiredAnonymous == anonymous {
		t.Fatal("bind left the closed anonymous session addressable")
	}
}

func TestWatchSessionRecoversOnlyLiveCriticalDeliveries(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	ledger := NewMemoryDeliveryLedger()
	for _, record := range []DeliveryRecord{
		{Key: "critical", DeliveryKey: "goal:1", TraceID: "trace-critical", UserID: "user-1", MatchID: "match-1", Critical: true, UpdatedAt: now, ExpiresAt: now.Add(time.Minute)},
		{Key: "normal", DeliveryKey: "comment:1", TraceID: "trace-normal", UserID: "user-1", MatchID: "match-1", UpdatedAt: now, ExpiresAt: now.Add(time.Minute)},
		{Key: "expired", DeliveryKey: "goal:old", TraceID: "trace-expired", UserID: "user-1", MatchID: "match-1", Critical: true, UpdatedAt: now, ExpiresAt: now.Add(-time.Second)},
		{Key: "other-user", DeliveryKey: "goal:other", TraceID: "trace-other", UserID: "user-2", MatchID: "match-1", Critical: true, UpdatedAt: now, ExpiresAt: now.Add(time.Minute)},
	} {
		if _, err := ledger.Plan(record); err != nil {
			t.Fatalf("plan %s: %v", record.Key, err)
		}
	}
	session := NewWatchSession(context.Background(), "user-1", "match-1", Config{}, ledger)
	source := recoverySourceStub{payloads: map[string]RecoveryPayload{
		"trace-critical": {TraceID: "trace-critical", UserID: "user-1", MatchID: "match-1", Text: "进球了", Presentation: relationship.PresentationPlan{Expression: "excited"}},
		"trace-other":    {TraceID: "trace-other", UserID: "user-2", MatchID: "match-1", Text: "别人的回复"},
	}}
	first, err := session.Recoveries(context.Background(), source, now)
	if err != nil {
		t.Fatalf("Recoveries: %v", err)
	}
	second, err := session.Recoveries(context.Background(), source, now)
	if err != nil {
		t.Fatalf("retry Recoveries: %v", err)
	}
	if len(first) != 1 || first[0].TraceID != "trace-critical" || first[0].DeliveryKey != "goal:1" || first[0].Text != "进球了" || first[0].Presentation.Expression != "excited" {
		t.Fatalf("recoveries = %+v", first)
	}
	if len(second) != 1 || second[0].TraceID != first[0].TraceID {
		t.Fatalf("reconnect recovery was not idempotent: first=%+v second=%+v", first, second)
	}
}

func TestWatchSessionRecoveryStopsAfterTerminalAcknowledgement(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	ledger := NewMemoryDeliveryLedger()
	record := DeliveryRecord{Key: "critical", DeliveryKey: "goal:1", TraceID: "trace-critical", UserID: "user-1", MatchID: "match-1", Critical: true, UpdatedAt: now, ExpiresAt: now.Add(time.Minute)}
	if _, err := ledger.Plan(record); err != nil {
		t.Fatal(err)
	}
	session := NewWatchSession(context.Background(), "user-1", "match-1", Config{}, ledger)
	source := recoverySourceStub{payloads: map[string]RecoveryPayload{"trace-critical": {TraceID: "trace-critical", UserID: "user-1", MatchID: "match-1", Text: "进球了"}}}
	if recovered, err := session.Recoveries(context.Background(), source, now); err != nil || len(recovered) != 1 {
		t.Fatalf("initial recovery = %+v err=%v", recovered, err)
	}
	if _, err := ledger.Transition(record.Key, DeliveryTextDelivered, now); err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.Transition(record.Key, DeliveryCompleted, now); err != nil {
		t.Fatal(err)
	}
	if recovered, err := session.Recoveries(context.Background(), source, now); err != nil || len(recovered) != 0 {
		t.Fatalf("terminal recovery = %+v err=%v", recovered, err)
	}
}

func TestWatchSessionRecoveryStopsAfterTextAcknowledgement(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	ledger := NewMemoryDeliveryLedger()
	record := DeliveryRecord{Key: "critical", TraceID: "trace-critical", UserID: "user-1", MatchID: "match-1", Critical: true, UpdatedAt: now, ExpiresAt: now.Add(time.Minute)}
	if _, err := ledger.Plan(record); err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.AcknowledgeText(record.Key, now.Add(time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	session := NewWatchSession(context.Background(), "user-1", "match-1", Config{}, ledger)
	source := recoverySourceStub{payloads: map[string]RecoveryPayload{"trace-critical": {TraceID: "trace-critical", UserID: "user-1", MatchID: "match-1", Text: "进球了"}}}
	recovered, err := session.Recoveries(context.Background(), source, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(recovered) != 0 {
		t.Fatalf("text-acknowledged delivery was recovered: %+v", recovered)
	}
}

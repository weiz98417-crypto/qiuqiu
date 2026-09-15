package conversation

import (
	"errors"
	"sync"
	"time"
)

// DeliveryTracker owns delivery lifecycle state that must survive transport
// reconnects. Protocol adapters may attach domain metadata to Pending values.
type DeliveryTracker struct {
	mu      sync.Mutex
	pending map[string]any
	ledger  DeliveryLedger
}

func NewDeliveryTracker(ledger DeliveryLedger) *DeliveryTracker {
	if ledger == nil {
		ledger = NewMemoryDeliveryLedger()
	}
	return &DeliveryTracker{pending: make(map[string]any), ledger: ledger}
}

func (tracker *DeliveryTracker) Ledger() DeliveryLedger {
	if tracker == nil {
		return nil
	}
	return tracker.ledger
}

func (tracker *DeliveryTracker) BindLedger(ledger DeliveryLedger) {
	if tracker != nil && ledger != nil {
		tracker.ledger = ledger
	}
}

func (tracker *DeliveryTracker) Plan(record DeliveryRecord, pending any) error {
	if tracker == nil || tracker.ledger == nil {
		return ErrDeliveryConflict
	}
	if _, err := tracker.ledger.Plan(record); err != nil {
		return err
	}
	if pending != nil {
		tracker.mu.Lock()
		tracker.pending[record.Key] = pending
		tracker.mu.Unlock()
	}
	return nil
}

func (tracker *DeliveryTracker) Transition(key string, state DeliveryState, at time.Time) error {
	if tracker == nil || tracker.ledger == nil || key == "" {
		return ErrDeliveryConflict
	}
	_, err := tracker.ledger.Transition(key, state, at)
	if errors.Is(err, ErrDeliveryNotFound) {
		return nil
	}
	return err
}

func (tracker *DeliveryTracker) Remove(key string) {
	if tracker == nil || key == "" {
		return
	}
	tracker.mu.Lock()
	delete(tracker.pending, key)
	tracker.mu.Unlock()
}

func (tracker *DeliveryTracker) Drain() []any {
	if tracker == nil {
		return nil
	}
	tracker.mu.Lock()
	values := make([]any, 0, len(tracker.pending))
	for key, value := range tracker.pending {
		if record, ok := tracker.ledger.Get(key); ok && isTerminalDeliveryState(record.State) {
			delete(tracker.pending, key)
			continue
		}
		values = append(values, value)
	}
	tracker.pending = make(map[string]any)
	tracker.mu.Unlock()
	return values
}

package conversation

import (
	"errors"
	"sort"
	"sync"
	"time"
)

type DeliveryState string

const (
	DeliveryPlanned       DeliveryState = "planned"
	DeliveryTextDelivered DeliveryState = "text_delivered"
	DeliveryAudioStarted  DeliveryState = "audio_started"
	DeliveryCompleted     DeliveryState = "completed"
	DeliveryInterrupted   DeliveryState = "interrupted"
	DeliverySkipped       DeliveryState = "skipped"
	DeliveryFailed        DeliveryState = "failed"
)

var (
	ErrDeliveryNotFound = errors.New("delivery not found")
	ErrDeliveryConflict = errors.New("delivery state transition is invalid")
)

type DeliveryRecord struct {
	Key              string        `json:"key"`
	DeliveryKey      string        `json:"deliveryKey,omitempty"`
	TraceID          string        `json:"traceId,omitempty"`
	MatchID          string        `json:"matchId,omitempty"`
	UserID           string        `json:"userId,omitempty"`
	State            DeliveryState `json:"state"`
	TextAcknowledged bool          `json:"textAcknowledged,omitempty"`
	Critical         bool          `json:"critical"`
	ExpiresAt        time.Time     `json:"expiresAt,omitempty"`
	UpdatedAt        time.Time     `json:"updatedAt"`
}

type DeliveryLedger interface {
	Plan(DeliveryRecord) (DeliveryRecord, error)
	Transition(string, DeliveryState, time.Time) (DeliveryRecord, error)
	AcknowledgeText(string, time.Time) (DeliveryRecord, error)
	Get(string) (DeliveryRecord, bool)
	FindByDeliveryKey(string) (DeliveryRecord, bool)
	Pending(time.Time) []DeliveryRecord
}

type MemoryDeliveryLedger struct {
	mu      sync.Mutex
	records map[string]DeliveryRecord
}

func NewMemoryDeliveryLedger() *MemoryDeliveryLedger {
	return &MemoryDeliveryLedger{records: make(map[string]DeliveryRecord)}
}

func (l *MemoryDeliveryLedger) Plan(record DeliveryRecord) (DeliveryRecord, error) {
	if l == nil || record.Key == "" {
		return DeliveryRecord{}, ErrDeliveryConflict
	}
	if record.State == "" {
		record.State = DeliveryPlanned
	}
	if record.State != DeliveryPlanned {
		return DeliveryRecord{}, ErrDeliveryConflict
	}
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = time.Now().UTC()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if existing, ok := l.records[record.Key]; ok {
		if existing.TraceID != record.TraceID || existing.DeliveryKey != record.DeliveryKey || existing.MatchID != record.MatchID || existing.UserID != record.UserID {
			return DeliveryRecord{}, ErrDeliveryConflict
		}
		return existing, nil
	}
	if record.DeliveryKey != "" {
		for _, existing := range l.records {
			if existing.DeliveryKey == record.DeliveryKey {
				return existing, ErrDeliveryConflict
			}
		}
	}
	l.records[record.Key] = record
	return record, nil
}

func (l *MemoryDeliveryLedger) Transition(key string, target DeliveryState, at time.Time) (DeliveryRecord, error) {
	if l == nil || key == "" || !validDeliveryState(target) {
		return DeliveryRecord{}, ErrDeliveryConflict
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	record, ok := l.records[key]
	if !ok {
		return DeliveryRecord{}, ErrDeliveryNotFound
	}
	if record.State == target {
		return record, nil
	}
	if isTerminalDeliveryState(record.State) {
		return DeliveryRecord{}, ErrDeliveryConflict
	}
	if !allowedDeliveryTransition(record.State, target) {
		return DeliveryRecord{}, ErrDeliveryConflict
	}
	record.State = target
	record.UpdatedAt = at.UTC()
	l.records[key] = record
	return record, nil
}

func (l *MemoryDeliveryLedger) AcknowledgeText(key string, at time.Time) (DeliveryRecord, error) {
	if l == nil || key == "" {
		return DeliveryRecord{}, ErrDeliveryConflict
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	record, ok := l.records[key]
	if !ok {
		return DeliveryRecord{}, ErrDeliveryNotFound
	}
	if record.TextAcknowledged {
		return record, nil
	}
	record.TextAcknowledged = true
	record.UpdatedAt = at.UTC()
	l.records[key] = record
	return record, nil
}

func (l *MemoryDeliveryLedger) Get(key string) (DeliveryRecord, bool) {
	if l == nil {
		return DeliveryRecord{}, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	record, ok := l.records[key]
	return record, ok
}

func (l *MemoryDeliveryLedger) FindByDeliveryKey(deliveryKey string) (DeliveryRecord, bool) {
	if l == nil || deliveryKey == "" {
		return DeliveryRecord{}, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, record := range l.records {
		if record.DeliveryKey == deliveryKey {
			return record, true
		}
	}
	return DeliveryRecord{}, false
}

func (l *MemoryDeliveryLedger) Pending(now time.Time) []DeliveryRecord {
	if l == nil {
		return nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	pending := make([]DeliveryRecord, 0, len(l.records))
	for _, record := range l.records {
		if isTerminalDeliveryState(record.State) {
			continue
		}
		if !record.ExpiresAt.IsZero() && !record.ExpiresAt.After(now) {
			continue
		}
		pending = append(pending, record)
	}
	sort.SliceStable(pending, func(left, right int) bool {
		return pending[left].UpdatedAt.Before(pending[right].UpdatedAt)
	})
	return pending
}

func validDeliveryState(state DeliveryState) bool {
	switch state {
	case DeliveryPlanned, DeliveryTextDelivered, DeliveryAudioStarted, DeliveryCompleted, DeliveryInterrupted, DeliverySkipped, DeliveryFailed:
		return true
	default:
		return false
	}
}

func isTerminalDeliveryState(state DeliveryState) bool {
	switch state {
	case DeliveryCompleted, DeliveryInterrupted, DeliverySkipped, DeliveryFailed:
		return true
	default:
		return false
	}
}

func allowedDeliveryTransition(current, target DeliveryState) bool {
	switch current {
	case DeliveryPlanned:
		return target == DeliveryTextDelivered || target == DeliveryInterrupted || target == DeliverySkipped || target == DeliveryFailed
	case DeliveryTextDelivered:
		return target == DeliveryAudioStarted || target == DeliveryCompleted || target == DeliveryInterrupted || target == DeliverySkipped || target == DeliveryFailed
	case DeliveryAudioStarted:
		return target == DeliveryCompleted || target == DeliveryInterrupted || target == DeliverySkipped || target == DeliveryFailed
	default:
		return false
	}
}

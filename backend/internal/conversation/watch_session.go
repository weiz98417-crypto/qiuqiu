package conversation

import (
	"context"
	"errors"
	"sync"
	"time"

	"qiuqiu/internal/relationship"
)

var ErrRecoverySource = errors.New("recovery source is not configured")

type RecoveryPayload struct {
	TraceID      string
	UserID       string
	MatchID      string
	Text         string
	DeliveryKey  string
	Presentation relationship.PresentationPlan
}

type RecoverySource interface {
	ResolveRecovery(context.Context, string, string) (RecoveryPayload, error)
}

// WatchSession is the logical conversation for one user watching one match.
// Transport attachments are intentionally separate from the session identity.
type WatchSession struct {
	UserID  string
	MatchID string
	Key     string

	mu        sync.Mutex
	attached  int
	lastSeen  time.Time
	ledger    DeliveryLedger
	scheduler *Scheduler
}

func (s *WatchSession) Recoveries(ctx context.Context, source RecoverySource, now time.Time) ([]RecoveryPayload, error) {
	if s == nil || source == nil {
		return nil, ErrRecoverySource
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	records := s.PendingDeliveries(now)
	recoveries := make([]RecoveryPayload, 0, len(records))
	for _, record := range records {
		if !record.Critical || record.TraceID == "" || record.TextAcknowledged {
			continue
		}
		payload, err := source.ResolveRecovery(ctx, s.MatchID, record.TraceID)
		if err != nil {
			continue
		}
		if payload.TraceID == "" {
			payload.TraceID = record.TraceID
		}
		if payload.UserID != s.UserID || payload.MatchID != s.MatchID || payload.Text == "" {
			continue
		}
		if payload.DeliveryKey == "" {
			payload.DeliveryKey = record.DeliveryKey
		}
		recoveries = append(recoveries, payload)
	}
	return recoveries, nil
}

type DeliveryLedgerRegistry struct {
	mu      sync.Mutex
	ledgers map[string]*MemoryDeliveryLedger
}

type WatchSessionRegistry struct {
	mu       sync.Mutex
	parent   context.Context
	config   Config
	ledgers  map[string]*MemoryDeliveryLedger
	sessions map[string]*WatchSession
	store    interface {
		For(string, string) DeliveryLedger
	}
}

func NewWatchSessionRegistry(parent context.Context, config Config) *WatchSessionRegistry {
	if parent == nil {
		parent = context.Background()
	}
	return &WatchSessionRegistry{parent: parent, config: config, ledgers: make(map[string]*MemoryDeliveryLedger), sessions: make(map[string]*WatchSession)}
}

func (r *WatchSessionRegistry) WithStore(store interface {
	For(string, string) DeliveryLedger
}) *WatchSessionRegistry {
	r.store = store
	return r
}

func (r *WatchSessionRegistry) Acquire(userID, matchID string) *WatchSession {
	if r == nil {
		return NewWatchSession(context.Background(), userID, matchID, Config{}, nil)
	}
	key := userID + ":" + matchID
	r.mu.Lock()
	defer r.mu.Unlock()
	if session := r.sessions[key]; session != nil {
		session.Attach()
		return session
	}
	if r.store != nil {
		var ledger DeliveryLedger
		if contextual, ok := r.store.(interface {
			ForContext(context.Context, string, string) DeliveryLedger
		}); ok {
			ledger = contextual.ForContext(r.parent, userID, matchID)
		} else {
			ledger = r.store.For(userID, matchID)
		}
		session := NewWatchSession(r.parent, userID, matchID, r.config, ledger)
		session.Attach()
		r.sessions[key] = session
		return session
	}
	ledger := r.ledgers[key]
	if ledger == nil {
		ledger = NewMemoryDeliveryLedger()
		r.ledgers[key] = ledger
	}
	session := NewWatchSession(r.parent, userID, matchID, r.config, ledger)
	session.Attach()
	r.sessions[key] = session
	return session
}

func (r *WatchSessionRegistry) Close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	sessions := make([]*WatchSession, 0, len(r.sessions))
	for _, session := range r.sessions {
		sessions = append(sessions, session)
	}
	r.sessions = make(map[string]*WatchSession)
	r.mu.Unlock()
	for _, session := range sessions {
		session.Close()
	}
}

func (r *WatchSessionRegistry) Bind(session *WatchSession, userID, matchID string) *WatchSession {
	if r == nil || session == nil || userID == "" || matchID == "" {
		return session
	}
	key := userID + ":" + matchID
	oldKey := session.Key
	r.mu.Lock()
	if existing := r.sessions[key]; existing != nil && existing != session {
		existing.Attach()
		if oldKey != key {
			delete(r.sessions, oldKey)
		}
		r.mu.Unlock()
		session.Close()
		return existing
	}
	if r.store != nil {
		if oldKey != key {
			delete(r.sessions, oldKey)
			r.sessions[key] = session
		}
		r.mu.Unlock()
		session.mu.Lock()
		session.UserID, session.MatchID, session.Key = userID, matchID, key
		if contextual, ok := r.store.(interface {
			ForContext(context.Context, string, string) DeliveryLedger
		}); ok {
			session.ledger = contextual.ForContext(r.parent, userID, matchID)
		} else {
			session.ledger = r.store.For(userID, matchID)
		}
		session.lastSeen = time.Now().UTC()
		session.mu.Unlock()
		return session
	}
	if oldKey != key {
		delete(r.sessions, oldKey)
		r.sessions[key] = session
	}
	ledger := r.ledgers[key]
	if ledger == nil {
		ledger = NewMemoryDeliveryLedger()
		r.ledgers[key] = ledger
	}
	r.mu.Unlock()
	session.mu.Lock()
	session.UserID = userID
	session.MatchID = matchID
	session.Key = key
	session.ledger = ledger
	session.lastSeen = time.Now().UTC()
	session.mu.Unlock()
	return session
}

func (r *WatchSessionRegistry) Release(userID, matchID string) {
	if r == nil {
		return
	}
	key := userID + ":" + matchID
	r.mu.Lock()
	session := r.sessions[key]
	r.mu.Unlock()
	if session != nil {
		session.Detach()
		if !session.Attached() {
			session.Interrupt()
		}
	}
}

func NewDeliveryLedgerRegistry() *DeliveryLedgerRegistry {
	return &DeliveryLedgerRegistry{ledgers: make(map[string]*MemoryDeliveryLedger)}
}

func (r *DeliveryLedgerRegistry) For(userID, matchID string) *MemoryDeliveryLedger {
	if r == nil {
		return NewMemoryDeliveryLedger()
	}
	key := userID + ":" + matchID
	r.mu.Lock()
	defer r.mu.Unlock()
	if ledger := r.ledgers[key]; ledger != nil {
		return ledger
	}
	ledger := NewMemoryDeliveryLedger()
	r.ledgers[key] = ledger
	return ledger
}

func NewWatchSession(parent context.Context, userID, matchID string, config Config, ledger DeliveryLedger) *WatchSession {
	if ledger == nil {
		ledger = NewMemoryDeliveryLedger()
	}
	now := time.Now().UTC()
	return &WatchSession{
		UserID:    userID,
		MatchID:   matchID,
		Key:       userID + ":" + matchID,
		lastSeen:  now,
		ledger:    ledger,
		scheduler: NewScheduler(parent, config),
	}
}

func (s *WatchSession) Scheduler() *Scheduler {
	if s == nil {
		return nil
	}
	return s.scheduler
}

func (s *WatchSession) Ledger() DeliveryLedger {
	if s == nil {
		return nil
	}
	return s.ledger
}

func (s *WatchSession) Attach() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.attached++
	s.lastSeen = time.Now().UTC()
	s.mu.Unlock()
}

func (s *WatchSession) Detach() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.attached > 0 {
		s.attached--
	}
	s.lastSeen = time.Now().UTC()
	s.mu.Unlock()
}

func (s *WatchSession) Attached() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.attached > 0
}

func (s *WatchSession) LastSeen() time.Time {
	if s == nil {
		return time.Time{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastSeen
}

func (s *WatchSession) PendingDeliveries(now time.Time) []DeliveryRecord {
	if s == nil || s.ledger == nil {
		return nil
	}
	return s.ledger.Pending(now)
}

func (s *WatchSession) Interrupt() {
	if s != nil && s.scheduler != nil {
		s.scheduler.Interrupt()
	}
}

func (s *WatchSession) Close() {
	if s != nil && s.scheduler != nil {
		s.scheduler.Close()
	}
}

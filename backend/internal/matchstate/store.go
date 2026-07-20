package matchstate

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	ErrNotFound             = errors.New("not found")
	ErrInvalid              = errors.New("invalid match event")
	ErrDuplicate            = errors.New("duplicate match event")
	ErrConflict             = errors.New("conflicting match event")
	ErrClockVersionConflict = errors.New("match clock version conflict")
)

type Score struct {
	Home int `json:"home"`
	Away int `json:"away"`
}

type FactStatus string

const (
	FactStatusProvisional FactStatus = "provisional"
	FactStatusConfirmed   FactStatus = "confirmed"
	FactStatusRevoked     FactStatus = "revoked"
	FactStatusConflict    FactStatus = "conflict"
	FactStatusReconciled  FactStatus = "reconciled"
)

type FactRevision struct {
	FactID        string         `json:"factId"`
	Revision      int            `json:"revision"`
	MatchID       string         `json:"matchId"`
	EventID       string         `json:"eventId,omitempty"`
	Status        FactStatus     `json:"status"`
	SourceType    string         `json:"sourceType"`
	SourceEventID string         `json:"sourceEventId,omitempty"`
	Confidence    float64        `json:"confidence"`
	Evidence      map[string]any `json:"evidence,omitempty"`
	ConfirmedBy   string         `json:"confirmedBy,omitempty"`
	RevisionOf    string         `json:"revisionOf,omitempty"`
	OccurredAt    string         `json:"occurredAt,omitempty"`
	RecordedAt    string         `json:"recordedAt"`
	PublicAt      string         `json:"publicAt,omitempty"`
}

type ConflictStatus string

const (
	ConflictStatusOpen     ConflictStatus = "open"
	ConflictStatusResolved ConflictStatus = "resolved"
)

type ConflictMemberRole string

const (
	ConflictMemberAccepted  ConflictMemberRole = "accepted"
	ConflictMemberCandidate ConflictMemberRole = "candidate"
)

type FactConflictMember struct {
	FactID string             `json:"factId"`
	Role   ConflictMemberRole `json:"role"`
}

type FactConflict struct {
	ID           string               `json:"id"`
	MatchID      string               `json:"matchId"`
	Status       ConflictStatus       `json:"status"`
	ChosenFactID string               `json:"chosenFactId,omitempty"`
	Reason       string               `json:"reason,omitempty"`
	DetectedAt   string               `json:"detectedAt"`
	ResolvedAt   string               `json:"resolvedAt,omitempty"`
	ResolvedBy   string               `json:"resolvedBy,omitempty"`
	Members      []FactConflictMember `json:"members"`
}

const (
	AutomationModeActive = "active"
	AutomationModePaused = "paused"
)

type AutomationPolicy struct {
	Mode            string   `json:"mode"`
	EventTypes      []string `json:"eventTypes"`
	CooldownSeconds int      `json:"cooldownSeconds"`
}

type MatchIntegrity struct {
	Status     string `json:"status"`
	Reason     string `json:"reason,omitempty"`
	DetectedAt string `json:"detectedAt,omitempty"`
}

type MatchConfig struct {
	MatchID     string           `json:"matchId"`
	HomeTeam    string           `json:"homeTeam"`
	AwayTeam    string           `json:"awayTeam"`
	Competition string           `json:"competition,omitempty"`
	Kickoff     string           `json:"kickoff,omitempty"`
	HomePlayers []Player         `json:"homePlayers,omitempty"`
	AwayPlayers []Player         `json:"awayPlayers,omitempty"`
	Automation  AutomationPolicy `json:"automation"`
	Integrity   MatchIntegrity   `json:"integrity"`
	UpdatedAt   string           `json:"updatedAt"`
}

type Player struct {
	Name     string `json:"name"`
	Number   string `json:"number,omitempty"`
	Position string `json:"position,omitempty"`
}

type Participant struct {
	Role     string `json:"role"`
	Name     string `json:"name"`
	TeamID   string `json:"teamId,omitempty"`
	TeamName string `json:"teamName,omitempty"`
}

type MatchEvent struct {
	ID                string         `json:"id"`
	MatchID           string         `json:"matchId"`
	Source            string         `json:"source"`
	ProviderName      string         `json:"providerName,omitempty"`
	ProviderEventID   string         `json:"providerEventId,omitempty"`
	OperatorID        string         `json:"operatorId,omitempty"`
	Period            string         `json:"period"`
	Clock             string         `json:"clock"`
	EventType         string         `json:"eventType"`
	TeamID            string         `json:"teamId,omitempty"`
	TeamName          string         `json:"teamName,omitempty"`
	PlayerName        string         `json:"playerName,omitempty"`
	Participants      []Participant  `json:"participants,omitempty"`
	Score             Score          `json:"score"`
	Intensity         int            `json:"intensity"`
	Confirmed         bool           `json:"confirmed"`
	Sentiment         string         `json:"sentiment,omitempty"`
	Description       string         `json:"description"`
	ProactiveText     string         `json:"proactiveText,omitempty"`
	Tags              []string       `json:"tags,omitempty"`
	RecommendedAction string         `json:"recommendedAction,omitempty"`
	Visibility        string         `json:"visibility"`
	CreatedAt         string         `json:"createdAt"`
	UpdatedAt         string         `json:"updatedAt"`
	RevisionOf        string         `json:"revisionOf,omitempty"`
	Status            string         `json:"status"`
	FactID            string         `json:"factId"`
	FactRevision      int            `json:"factRevision"`
	FactStatus        FactStatus     `json:"factStatus"`
	Confidence        float64        `json:"confidence"`
	Evidence          map[string]any `json:"evidence,omitempty"`
	ConfirmedBy       string         `json:"confirmedBy,omitempty"`
	PublicAt          string         `json:"publicAt,omitempty"`
	RecordedSequence  int64          `json:"recordedSequence,omitempty"`
}

func DeliveryKey(event MatchEvent) string {
	return fmt.Sprintf("%s:%d:%s", event.ID, event.FactRevision, event.FactStatus)
}

type Snapshot struct {
	MatchID               string         `json:"matchId"`
	HomeTeam              string         `json:"homeTeam"`
	AwayTeam              string         `json:"awayTeam"`
	Score                 Score          `json:"score"`
	Period                string         `json:"period"`
	Clock                 string         `json:"clock"`
	MatchClock            MatchClock     `json:"matchClock"`
	Momentum              string         `json:"momentum"`
	EmotionalTemperature  int            `json:"emotionalTemperature"`
	RecentEvents          []MatchEvent   `json:"recentEvents"`
	KeyEvents             []MatchEvent   `json:"keyEvents"`
	LastUpdatedAt         string         `json:"lastUpdatedAt"`
	LastRecommendedAction string         `json:"lastRecommendedAction,omitempty"`
	LastPublicDescription string         `json:"lastPublicDescription,omitempty"`
	Integrity             MatchIntegrity `json:"integrity"`
}

type Repository interface {
	SetConfig(matchID string, config MatchConfig) (MatchConfig, Snapshot, error)
	SetAutomation(matchID string, policy AutomationPolicy) (AutomationPolicy, error)
	Config(matchID string) MatchConfig
	Reset(matchID string) error
	Create(matchID string, ev MatchEvent) (MatchEvent, Snapshot, error)
	Correct(matchID, eventID string, replacement MatchEvent) (MatchEvent, Snapshot, error)
	Events(matchID string) []MatchEvent
	Snapshot(matchID string) Snapshot
	PublicEvents(matchID string) []MatchEvent
	PublicSnapshot(matchID string) Snapshot
	ConfirmFact(matchID, factID, operatorID string) (MatchEvent, Snapshot, error)
	RevokeFact(matchID, factID, operatorID string) (MatchEvent, Snapshot, error)
	ReconcileFact(matchID, factID, operatorID string) (MatchEvent, Snapshot, error)
	FactRevisions(matchID, factID string) []FactRevision
	Subscribe(matchID string) (<-chan MatchEvent, func())
}

type OperatorTransactionRepository interface {
	CreateOperator(context.Context, string, MatchEvent) (MatchEvent, Snapshot, error)
	CorrectOperator(context.Context, string, string, MatchEvent) (MatchEvent, Snapshot, error)
	ConfirmFactOperator(context.Context, string, string, string) (MatchEvent, Snapshot, error)
	RevokeFactOperator(context.Context, string, string, string) (MatchEvent, Snapshot, error)
	ReconcileFactOperator(context.Context, string, string, string) (MatchEvent, Snapshot, error)
	PublicSnapshotOperator(context.Context, string) (Snapshot, error)
}

type FactConflictRepository interface {
	FactConflicts(matchID string) []FactConflict
	ResolveFactConflict(matchID, conflictID, chosenFactID, operatorID, reason string) (FactConflict, MatchEvent, Snapshot, error)
}

type FactConflictTransactionRepository interface {
	ResolveFactConflictOperator(context.Context, string, string, string, string, string) (FactConflict, MatchEvent, Snapshot, error)
}

type OutboxRunner interface {
	RunOutbox(context.Context)
}

type EventObserverRegistrar interface {
	SetEventObserver(func(MatchEvent) error)
}

type FactProjectionAuditRegistrar interface {
	SetFactProjectionAuditObserver(func(FactProjectionAudit))
}

type StoreOption func(*storeOptions)

type storeOptions struct {
	projectedReads bool
}

func WithFactLedgerPublicReads(enabled bool) StoreOption {
	return func(options *storeOptions) {
		options.projectedReads = enabled
	}
}

func resolveStoreOptions(options []StoreOption) storeOptions {
	resolved := storeOptions{projectedReads: true}
	for _, option := range options {
		option(&resolved)
	}
	return resolved
}

type Store struct {
	mu               sync.RWMutex
	events           map[string][]MatchEvent
	configs          map[string]MatchConfig
	factHistory      map[string][]FactRevision
	factConflicts    map[string][]FactConflict
	sourceCursor     map[string]int64
	clocks           map[string]MatchClock
	clockSubscribers map[string]map[chan MatchClock]struct{}
	subscribers      map[string]map[*eventSubscription]struct{}
	eventObserver    func(MatchEvent) error
	projectionAudit  func(FactProjectionAudit)
	projectedReads   bool
	nextID           int64
	nextConflictID   int64
	now              func() time.Time
}

type eventSubscription struct {
	events  chan MatchEvent
	wake    chan struct{}
	stop    chan struct{}
	done    chan struct{}
	mu      sync.Mutex
	queue   []MatchEvent
	stopped bool
}

func newEventSubscription() *eventSubscription {
	subscription := &eventSubscription{
		events: make(chan MatchEvent),
		wake:   make(chan struct{}, 1),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	go subscription.run()
	return subscription
}

func (s *eventSubscription) enqueue(event MatchEvent) {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.queue = append(s.queue, event)
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *eventSubscription) close() {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		<-s.done
		return
	}
	s.stopped = true
	close(s.stop)
	s.mu.Unlock()
	<-s.done
}

func (s *eventSubscription) run() {
	defer close(s.done)
	for {
		s.mu.Lock()
		var next MatchEvent
		hasEvent := len(s.queue) > 0
		if hasEvent {
			next = s.queue[0]
		}
		s.mu.Unlock()

		if !hasEvent {
			select {
			case <-s.wake:
				continue
			case <-s.stop:
				return
			}
		}

		select {
		case s.events <- next:
			s.mu.Lock()
			s.queue = s.queue[1:]
			s.mu.Unlock()
		case <-s.stop:
			return
		}
	}
}

func NewStore(options ...StoreOption) *Store {
	resolved := resolveStoreOptions(options)
	return &Store{
		events:           make(map[string][]MatchEvent),
		configs:          make(map[string]MatchConfig),
		factHistory:      make(map[string][]FactRevision),
		factConflicts:    make(map[string][]FactConflict),
		sourceCursor:     make(map[string]int64),
		clocks:           make(map[string]MatchClock),
		clockSubscribers: make(map[string]map[chan MatchClock]struct{}),
		subscribers:      make(map[string]map[*eventSubscription]struct{}),
		projectedReads:   resolved.projectedReads,
		now:              time.Now,
	}
}

func (s *Store) Clock(matchID string) MatchClock {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return normalizeMatchClock(matchID, s.clocks[matchID])
}

func (s *Store) SetClock(matchID string, command ClockCommand) (MatchClock, error) {
	matchID = strings.TrimSpace(matchID)
	if matchID == "" {
		return MatchClock{}, fmt.Errorf("%w: matchId is required", ErrInvalid)
	}
	s.mu.Lock()
	current := normalizeMatchClock(matchID, s.clocks[matchID])
	next, err := applyClockCommand(current, command, s.now())
	if err != nil {
		s.mu.Unlock()
		return MatchClock{}, err
	}
	if next.Version == current.Version {
		s.mu.Unlock()
		return next, nil
	}
	s.clocks[matchID] = next
	subscribers := make([]chan MatchClock, 0, len(s.clockSubscribers[matchID]))
	for subscriber := range s.clockSubscribers[matchID] {
		subscribers = append(subscribers, subscriber)
	}
	s.mu.Unlock()
	for _, subscriber := range subscribers {
		select {
		case subscriber <- next:
		default:
			select {
			case <-subscriber:
			default:
			}
			select {
			case subscriber <- next:
			default:
			}
		}
	}
	return next, nil
}

func (s *Store) SubscribeClock(matchID string) (<-chan MatchClock, func()) {
	matchID = strings.TrimSpace(matchID)
	updates := make(chan MatchClock, 1)
	s.mu.Lock()
	if s.clockSubscribers[matchID] == nil {
		s.clockSubscribers[matchID] = make(map[chan MatchClock]struct{})
	}
	s.clockSubscribers[matchID][updates] = struct{}{}
	s.mu.Unlock()
	var once sync.Once
	return updates, func() {
		once.Do(func() {
			s.mu.Lock()
			delete(s.clockSubscribers[matchID], updates)
			s.mu.Unlock()
		})
	}
}

func (s *Store) SetConfig(matchID string, config MatchConfig) (MatchConfig, Snapshot, error) {
	matchID = strings.TrimSpace(matchID)
	if matchID == "" {
		return MatchConfig{}, Snapshot{}, fmt.Errorf("%w: matchId is required", ErrInvalid)
	}
	automationProvided := strings.TrimSpace(config.Automation.Mode) != ""
	config.MatchID = matchID
	config.HomeTeam = defaultString(config.HomeTeam, "主队")
	config.AwayTeam = defaultString(config.AwayTeam, "客队")
	config.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)

	s.mu.Lock()
	if !automationProvided {
		config.Automation = normalizeConfig(matchID, s.configs[matchID]).Automation
	}
	config.Integrity = normalizeConfig(matchID, s.configs[matchID]).Integrity
	config.Automation = normalizeAutomationPolicy(config.Automation)
	s.configs[matchID] = config
	snapshot := resolvePublicProjection(
		FactLedgerProjectInput{
			MatchID: matchID, Events: s.events[matchID], Config: config, Clock: s.clocks[matchID], Now: s.now(),
		},
		s.projectedReads,
		nil,
	).Snapshot
	s.mu.Unlock()

	return config, snapshot, nil
}

func (s *Store) SetAutomation(matchID string, policy AutomationPolicy) (AutomationPolicy, error) {
	matchID = strings.TrimSpace(matchID)
	if matchID == "" {
		return AutomationPolicy{}, fmt.Errorf("%w: matchId is required", ErrInvalid)
	}
	if err := validateAutomationPolicy(policy); err != nil {
		return AutomationPolicy{}, err
	}
	policy = normalizeAutomationPolicy(policy)

	s.mu.Lock()
	config := normalizeConfig(matchID, s.configs[matchID])
	config.Automation = policy
	config.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	s.configs[matchID] = config
	s.mu.Unlock()
	return policy, nil
}

func (s *Store) Config(matchID string) MatchConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return normalizeConfig(matchID, s.configs[matchID])
}

func (s *Store) Reset(matchID string) error {
	matchID = strings.TrimSpace(matchID)
	if matchID == "" {
		return fmt.Errorf("%w: matchId is required", ErrInvalid)
	}
	s.mu.Lock()
	delete(s.events, matchID)
	delete(s.configs, matchID)
	delete(s.clocks, matchID)
	delete(s.factConflicts, matchID)
	for key := range s.factHistory {
		if strings.HasPrefix(key, matchID+"\x00") {
			delete(s.factHistory, key)
		}
	}
	for key := range s.sourceCursor {
		if strings.HasPrefix(key, matchID+"\x00") {
			delete(s.sourceCursor, key)
		}
	}
	s.mu.Unlock()
	return nil
}

func (s *Store) SourceCursor(matchID, sourceType, sourceKey string) (int64, error) {
	key, err := sourceCursorKey(matchID, sourceType, sourceKey)
	if err != nil {
		return 0, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sourceCursor[key], nil
}

func (s *Store) SetSourceCursor(matchID, sourceType, sourceKey string, cursor int64) error {
	key, err := sourceCursorKey(matchID, sourceType, sourceKey)
	if err != nil {
		return err
	}
	if cursor < 0 {
		return fmt.Errorf("%w: source cursor cannot be negative", ErrInvalid)
	}
	s.mu.Lock()
	if cursor > s.sourceCursor[key] {
		s.sourceCursor[key] = cursor
	}
	s.mu.Unlock()
	return nil
}

func (s *Store) Create(matchID string, ev MatchEvent) (MatchEvent, Snapshot, error) {
	matchID = strings.TrimSpace(matchID)
	if matchID == "" {
		return MatchEvent{}, Snapshot{}, fmt.Errorf("%w: matchId is required", ErrInvalid)
	}
	ev.MatchID = matchID
	normalize(&ev)
	if err := validate(ev); err != nil {
		return MatchEvent{}, Snapshot{}, err
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	s.mu.Lock()
	if ev.ProviderEventID != "" {
		for _, existing := range s.events[matchID] {
			if existing.Source == ev.Source && existing.ProviderEventID == ev.ProviderEventID {
				s.mu.Unlock()
				return MatchEvent{}, Snapshot{}, ErrDuplicate
			}
		}
	}
	if err := crossSourceEventError(s.events[matchID], ev); err != nil {
		if errors.Is(err, ErrConflict) {
			conflictIndices := crossSourceConflictIndices(s.events[matchID], ev)
			config := normalizeConfig(matchID, s.configs[matchID])
			config.Integrity = conflictIntegrity(ev)
			config.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
			s.configs[matchID] = config
			s.nextID++
			ev.ID = fmt.Sprintf("evt_%d", s.nextID)
			ev.RecordedSequence = s.nextID
			if ev.FactID == "" {
				ev.FactID = ev.ID
			}
			ev.FactStatus = FactStatusConflict
			ev.Confirmed = false
			ev.CreatedAt = now
			ev.UpdatedAt = now
			s.events[matchID] = append(s.events[matchID], ev)
			s.recordFactRevisionLocked(matchID, ev)
			s.recordFactConflictLocked(matchID, s.events[matchID], conflictIndices, ev, now)
			subs := s.subscriberListLocked(matchID)
			s.mu.Unlock()
			s.publish(subs, ev)
			return MatchEvent{}, Snapshot{}, ErrConflict
		}
		s.mu.Unlock()
		return MatchEvent{}, Snapshot{}, err
	}
	config := normalizeConfig(matchID, s.configs[matchID])
	projection, err := (FactLedgerEngine{}).Project(FactLedgerProjectInput{
		MatchID: matchID, Events: s.events[matchID], Config: config, Clock: s.clocks[matchID], Now: s.now(),
	})
	if err != nil {
		s.mu.Unlock()
		return MatchEvent{}, Snapshot{}, err
	}
	current := projection.Snapshot
	if err := validateEventRelations(s.events[matchID], ev); err != nil {
		s.mu.Unlock()
		return MatchEvent{}, Snapshot{}, err
	}
	if err := validateAgainstSnapshot(ev, current, config); err != nil {
		s.mu.Unlock()
		return MatchEvent{}, Snapshot{}, err
	}
	s.nextID++
	ev.ID = fmt.Sprintf("evt_%d", s.nextID)
	ev.RecordedSequence = s.nextID
	if ev.FactID == "" {
		ev.FactID = ev.ID
	}
	ev.CreatedAt = now
	ev.UpdatedAt = now
	s.events[matchID] = append(s.events[matchID], ev)
	s.recordFactRevisionLocked(matchID, ev)
	snapshot := resolvePublicProjection(
		FactLedgerProjectInput{
			MatchID: matchID, Events: s.events[matchID], Config: s.configs[matchID], Clock: s.clocks[matchID], Now: s.now(),
		},
		s.projectedReads,
		nil,
	).Snapshot
	subs := s.subscriberListLocked(matchID)
	s.mu.Unlock()

	s.publish(subs, ev)
	return ev, snapshot, nil
}

func (s *Store) Correct(matchID, eventID string, replacement MatchEvent) (MatchEvent, Snapshot, error) {
	matchID = strings.TrimSpace(matchID)
	eventID = strings.TrimSpace(eventID)
	if matchID == "" || eventID == "" {
		return MatchEvent{}, Snapshot{}, fmt.Errorf("%w: matchId and event id are required", ErrInvalid)
	}

	s.mu.Lock()
	events := s.events[matchID]
	found := -1
	for i := range events {
		if events[i].ID == eventID && events[i].Status == "active" {
			found = i
			break
		}
	}
	if found == -1 {
		s.mu.Unlock()
		return MatchEvent{}, Snapshot{}, ErrNotFound
	}

	replacement.MatchID = matchID
	replacement.RevisionOf = eventID
	requestedFactStatus := replacement.FactStatus
	normalize(&replacement)
	if requestedFactStatus == "" {
		replacement.FactStatus = events[found].FactStatus
		replacement.Confirmed = replacement.FactStatus == FactStatusConfirmed || replacement.FactStatus == FactStatusReconciled
	}
	if err := validate(replacement); err != nil {
		s.mu.Unlock()
		return MatchEvent{}, Snapshot{}, err
	}
	projection, err := (FactLedgerEngine{}).Project(FactLedgerProjectInput{
		MatchID: matchID, Events: events, Config: s.configs[matchID], Clock: s.clocks[matchID], Now: s.now(),
	})
	if err != nil {
		s.mu.Unlock()
		return MatchEvent{}, Snapshot{}, err
	}
	current := projection.Snapshot
	if err := validateCorrectionTimeline(events, events[found], replacement); err != nil {
		s.mu.Unlock()
		return MatchEvent{}, Snapshot{}, err
	}
	if err := validateCorrection(events[found], replacement, current, normalizeConfig(matchID, s.configs[matchID])); err != nil {
		s.mu.Unlock()
		return MatchEvent{}, Snapshot{}, err
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	events[found].Status = "corrected"
	events[found].UpdatedAt = now
	s.nextID++
	replacement.ID = fmt.Sprintf("evt_%d", s.nextID)
	replacement.RecordedSequence = s.nextID
	if replacement.FactID == "" {
		replacement.FactID = events[found].FactID
	}
	replacement.FactRevision = events[found].FactRevision + 1
	replacement.CreatedAt = now
	replacement.UpdatedAt = now
	events = append(events, replacement)
	s.recordFactRevisionLocked(matchID, replacement)
	s.events[matchID] = events
	snapshot := resolvePublicProjection(
		FactLedgerProjectInput{
			MatchID: matchID, Events: events, Config: s.configs[matchID], Clock: s.clocks[matchID], Now: s.now(),
		},
		s.projectedReads,
		nil,
	).Snapshot
	subs := s.subscriberListLocked(matchID)
	s.mu.Unlock()

	s.publish(subs, replacement)
	return replacement, snapshot, nil
}

func (s *Store) Events(matchID string) []MatchEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	events := append([]MatchEvent(nil), s.events[matchID]...)
	sort.SliceStable(events, func(i, j int) bool {
		return events[i].RecordedSequence > events[j].RecordedSequence
	})
	return events
}

func (s *Store) Snapshot(matchID string) Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return resolvePublicProjection(
		FactLedgerProjectInput{
			MatchID: matchID, Events: s.events[matchID], Config: s.configs[matchID], Clock: s.clocks[matchID], Now: s.now(),
		},
		s.projectedReads,
		nil,
	).Snapshot
}

func (s *Store) PublicEvents(matchID string) []MatchEvent {
	s.mu.RLock()
	events := append([]MatchEvent(nil), s.events[matchID]...)
	input := FactLedgerProjectInput{
		MatchID: matchID, Events: events, Config: s.configs[matchID], Clock: s.clocks[matchID], Now: s.now(),
	}
	observer := s.projectionAudit
	enabled := s.projectedReads
	s.mu.RUnlock()
	return resolvePublicProjection(input, enabled, observer).Events
}

func (s *Store) PublicSnapshot(matchID string) Snapshot {
	s.mu.RLock()
	events := append([]MatchEvent(nil), s.events[matchID]...)
	config := s.configs[matchID]
	clock := s.clocks[matchID]
	now := s.now()
	observer := s.projectionAudit
	enabled := s.projectedReads
	s.mu.RUnlock()
	return resolvePublicProjection(
		FactLedgerProjectInput{MatchID: matchID, Events: events, Config: config, Clock: clock, Now: now},
		enabled,
		observer,
	).Snapshot
}

func (s *Store) ConfirmFact(matchID, factID, operatorID string) (MatchEvent, Snapshot, error) {
	matchID = strings.TrimSpace(matchID)
	factID = strings.TrimSpace(factID)
	operatorID = strings.TrimSpace(operatorID)
	if matchID == "" || factID == "" || operatorID == "" {
		return MatchEvent{}, Snapshot{}, fmt.Errorf("%w: matchId, factId and operatorId are required", ErrInvalid)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	events := s.events[matchID]
	found := -1
	for index := range events {
		if events[index].FactID == factID && events[index].Status == "active" {
			found = index
			break
		}
	}
	if found == -1 {
		return MatchEvent{}, Snapshot{}, ErrNotFound
	}
	if events[found].FactStatus != FactStatusProvisional {
		return MatchEvent{}, Snapshot{}, fmt.Errorf("%w: fact is not provisional", ErrInvalid)
	}
	projection, err := (FactLedgerEngine{}).Project(FactLedgerProjectInput{
		MatchID: matchID, Events: events, Config: s.configs[matchID], Clock: s.clocks[matchID], Now: s.now(),
	})
	if err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	if err := validateAgainstSnapshot(events[found], projection.Snapshot, normalizeConfig(matchID, s.configs[matchID])); err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	events[found].FactStatus = FactStatusConfirmed
	events[found].Confirmed = true
	events[found].ConfirmedBy = operatorID
	events[found].PublicAt = now
	events[found].FactRevision++
	events[found].UpdatedAt = now
	s.events[matchID] = events
	s.recordFactRevisionLocked(matchID, events[found])
	confirmed := events[found]
	snapshot := resolvePublicProjection(
		FactLedgerProjectInput{
			MatchID: matchID, Events: events, Config: s.configs[matchID], Clock: s.clocks[matchID], Now: s.now(),
		},
		s.projectedReads,
		nil,
	).Snapshot
	subs := s.subscriberListLocked(matchID)
	s.mu.Unlock()
	s.publish(subs, confirmed)
	s.mu.Lock()
	return confirmed, snapshot, nil
}

func (s *Store) RevokeFact(matchID, factID, operatorID string) (MatchEvent, Snapshot, error) {
	return s.transitionFact(matchID, factID, operatorID, FactStatusRevoked)
}

func (s *Store) ReconcileFact(matchID, factID, operatorID string) (MatchEvent, Snapshot, error) {
	return s.transitionFact(matchID, factID, operatorID, FactStatusReconciled)
}

func (s *Store) FactRevisions(matchID, factID string) []FactRevision {
	s.mu.RLock()
	defer s.mu.RUnlock()
	key := factHistoryKey(matchID, factID)
	return append([]FactRevision(nil), s.factHistory[key]...)
}

func (s *Store) FactConflicts(matchID string) []FactConflict {
	s.mu.RLock()
	defer s.mu.RUnlock()
	conflicts := s.factConflicts[strings.TrimSpace(matchID)]
	cloned := make([]FactConflict, len(conflicts))
	for index := range conflicts {
		cloned[index] = cloneFactConflict(conflicts[index])
	}
	return cloned
}

func (s *Store) ResolveFactConflict(matchID, conflictID, chosenFactID, operatorID, reason string) (FactConflict, MatchEvent, Snapshot, error) {
	matchID = strings.TrimSpace(matchID)
	conflictID = strings.TrimSpace(conflictID)
	chosenFactID = strings.TrimSpace(chosenFactID)
	operatorID = strings.TrimSpace(operatorID)
	reason = strings.TrimSpace(reason)
	if matchID == "" || conflictID == "" || chosenFactID == "" || operatorID == "" || reason == "" {
		return FactConflict{}, MatchEvent{}, Snapshot{}, fmt.Errorf("%w: matchId, conflictId, chosenFactId, operatorId and reason are required", ErrInvalid)
	}

	s.mu.Lock()
	conflicts := s.factConflicts[matchID]
	conflictIndex := -1
	for index := range conflicts {
		if conflicts[index].ID == conflictID && conflicts[index].Status == ConflictStatusOpen {
			conflictIndex = index
			break
		}
	}
	if conflictIndex == -1 {
		s.mu.Unlock()
		return FactConflict{}, MatchEvent{}, Snapshot{}, ErrNotFound
	}
	memberFactIDs := make(map[string]struct{}, len(conflicts[conflictIndex].Members))
	for _, member := range conflicts[conflictIndex].Members {
		memberFactIDs[member.FactID] = struct{}{}
	}
	if _, member := memberFactIDs[chosenFactID]; !member {
		s.mu.Unlock()
		return FactConflict{}, MatchEvent{}, Snapshot{}, fmt.Errorf("%w: chosen fact is not a member of the conflict", ErrInvalid)
	}

	events := s.events[matchID]
	chosenIndex := -1
	changedSignalIndex := -1
	now := s.now().UTC().Format(time.RFC3339Nano)
	for index := range events {
		if events[index].Status != "active" {
			continue
		}
		if _, member := memberFactIDs[events[index].FactID]; !member {
			continue
		}
		if events[index].FactID == chosenFactID {
			chosenIndex = index
			if events[index].FactStatus == FactStatusConflict || events[index].FactStatus == FactStatusProvisional {
				events[index].FactStatus = FactStatusReconciled
				events[index].Confirmed = true
				events[index].ConfirmedBy = operatorID
				events[index].PublicAt = now
				events[index].FactRevision++
				events[index].UpdatedAt = now
				s.recordFactRevisionLocked(matchID, events[index])
				changedSignalIndex = index
			}
			continue
		}
		if events[index].FactStatus == FactStatusRevoked {
			continue
		}
		events[index].FactStatus = FactStatusRevoked
		events[index].Confirmed = false
		events[index].ConfirmedBy = operatorID
		events[index].PublicAt = ""
		events[index].FactRevision++
		events[index].UpdatedAt = now
		s.recordFactRevisionLocked(matchID, events[index])
		if changedSignalIndex == -1 {
			changedSignalIndex = index
		}
	}
	if chosenIndex == -1 {
		s.mu.Unlock()
		return FactConflict{}, MatchEvent{}, Snapshot{}, ErrNotFound
	}

	conflicts[conflictIndex].Status = ConflictStatusResolved
	conflicts[conflictIndex].ChosenFactID = chosenFactID
	conflicts[conflictIndex].Reason = reason
	conflicts[conflictIndex].ResolvedAt = now
	conflicts[conflictIndex].ResolvedBy = operatorID
	s.factConflicts[matchID] = conflicts
	s.events[matchID] = events
	config := normalizeConfig(matchID, s.configs[matchID])
	if !hasOpenFactConflict(conflicts) {
		config.Integrity = MatchIntegrity{Status: "ok"}
		config.UpdatedAt = now
		s.configs[matchID] = config
	}
	snapshot := resolvePublicProjection(
		FactLedgerProjectInput{MatchID: matchID, Events: events, Config: config, Clock: s.clocks[matchID], Now: s.now()},
		s.projectedReads,
		nil,
	).Snapshot
	chosen := events[chosenIndex]
	resolved := cloneFactConflict(conflicts[conflictIndex])
	subs := s.subscriberListLocked(matchID)
	var signal MatchEvent
	if changedSignalIndex >= 0 {
		signal = events[changedSignalIndex]
	}
	s.mu.Unlock()
	if signal.ID != "" {
		s.publish(subs, signal)
	}
	return resolved, chosen, snapshot, nil
}

func (s *Store) recordFactConflictLocked(matchID string, events []MatchEvent, conflictingIndices []int, candidate MatchEvent, detectedAt string) {
	conflicts := s.factConflicts[matchID]
	conflictIndex := -1
	conflictingFactIDs := make(map[string]struct{}, len(conflictingIndices))
	for _, index := range conflictingIndices {
		if index >= 0 && index < len(events) {
			conflictingFactIDs[events[index].FactID] = struct{}{}
		}
	}
	for index := range conflicts {
		if conflicts[index].Status != ConflictStatusOpen {
			continue
		}
		for _, member := range conflicts[index].Members {
			if _, conflictsWithExisting := conflictingFactIDs[member.FactID]; conflictsWithExisting {
				conflictIndex = index
				break
			}
		}
		if conflictIndex >= 0 {
			break
		}
	}
	if conflictIndex == -1 {
		s.nextConflictID++
		conflicts = append(conflicts, FactConflict{
			ID:         fmt.Sprintf("conflict_%d", s.nextConflictID),
			MatchID:    matchID,
			Status:     ConflictStatusOpen,
			DetectedAt: detectedAt,
			Members:    []FactConflictMember{},
		})
		conflictIndex = len(conflicts) - 1
	}
	for _, index := range conflictingIndices {
		if index < 0 || index >= len(events) {
			continue
		}
		role := ConflictMemberCandidate
		if IsPublicFact(events[index]) {
			role = ConflictMemberAccepted
		}
		conflicts[conflictIndex].Members = appendConflictMember(conflicts[conflictIndex].Members, events[index].FactID, role)
	}
	conflicts[conflictIndex].Members = appendConflictMember(conflicts[conflictIndex].Members, candidate.FactID, ConflictMemberCandidate)
	s.factConflicts[matchID] = conflicts
}

func appendConflictMember(members []FactConflictMember, factID string, role ConflictMemberRole) []FactConflictMember {
	for index := range members {
		if members[index].FactID == factID {
			if role == ConflictMemberAccepted {
				members[index].Role = role
			}
			return members
		}
	}
	return append(members, FactConflictMember{FactID: factID, Role: role})
}

func cloneFactConflict(conflict FactConflict) FactConflict {
	conflict.Members = append([]FactConflictMember(nil), conflict.Members...)
	return conflict
}

func hasOpenFactConflict(conflicts []FactConflict) bool {
	for _, conflict := range conflicts {
		if conflict.Status == ConflictStatusOpen {
			return true
		}
	}
	return false
}

func (s *Store) transitionFact(matchID, factID, operatorID string, status FactStatus) (MatchEvent, Snapshot, error) {
	matchID = strings.TrimSpace(matchID)
	factID = strings.TrimSpace(factID)
	operatorID = strings.TrimSpace(operatorID)
	if matchID == "" || factID == "" || operatorID == "" {
		return MatchEvent{}, Snapshot{}, fmt.Errorf("%w: matchId, factId and operatorId are required", ErrInvalid)
	}
	if conflictID, chosenFactID, reason, found := s.legacyConflictResolution(matchID, factID, status); found {
		if chosenFactID == "" {
			return MatchEvent{}, Snapshot{}, fmt.Errorf("%w: resolve the open conflict explicitly", ErrInvalid)
		}
		_, changed, snapshot, err := s.ResolveFactConflict(matchID, conflictID, chosenFactID, operatorID, reason)
		if err != nil {
			return MatchEvent{}, Snapshot{}, err
		}
		if chosenFactID == factID {
			return changed, snapshot, nil
		}
		for _, event := range s.Events(matchID) {
			if event.FactID == factID && event.Status == "active" {
				return event, snapshot, nil
			}
		}
		return MatchEvent{}, Snapshot{}, ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	events := s.events[matchID]
	found := -1
	for index := range events {
		if events[index].FactID == factID && events[index].Status == "active" {
			found = index
			break
		}
	}
	if found == -1 {
		return MatchEvent{}, Snapshot{}, ErrNotFound
	}
	if err := validateFactTransition(events[found].FactStatus, status); err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	previousStatus := events[found].FactStatus
	now := time.Now().UTC().Format(time.RFC3339Nano)
	events[found].FactStatus = status
	events[found].Confirmed = status == FactStatusReconciled
	events[found].ConfirmedBy = operatorID
	events[found].PublicAt = ""
	if events[found].Confirmed {
		events[found].PublicAt = now
	}
	events[found].FactRevision++
	events[found].UpdatedAt = now
	if status == FactStatusReconciled {
		conflictingIndices := reconciliationConflictIndices(events, found)
		for index := range events {
			if _, conflicting := conflictingIndices[index]; !conflicting {
				continue
			}
			events[index].FactStatus = FactStatusRevoked
			events[index].Confirmed = false
			events[index].ConfirmedBy = operatorID
			events[index].PublicAt = ""
			events[index].FactRevision++
			events[index].UpdatedAt = now
			s.recordFactRevisionLocked(matchID, events[index])
		}
	}
	if (status == FactStatusReconciled || previousStatus == FactStatusConflict) && !hasActiveFactConflict(events) {
		config := normalizeConfig(matchID, s.configs[matchID])
		config.Integrity = MatchIntegrity{Status: "ok"}
		config.UpdatedAt = now
		s.configs[matchID] = config
	}
	s.events[matchID] = events
	s.recordFactRevisionLocked(matchID, events[found])
	changed := events[found]
	snapshot := resolvePublicProjection(
		FactLedgerProjectInput{
			MatchID: matchID, Events: events, Config: s.configs[matchID], Clock: s.clocks[matchID], Now: s.now(),
		},
		s.projectedReads,
		nil,
	).Snapshot
	subs := s.subscriberListLocked(matchID)
	s.mu.Unlock()
	s.publish(subs, changed)
	s.mu.Lock()
	return changed, snapshot, nil
}

func (s *Store) legacyConflictResolution(matchID, factID string, status FactStatus) (string, string, string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, conflict := range s.factConflicts[matchID] {
		if conflict.Status != ConflictStatusOpen {
			continue
		}
		var targetRole ConflictMemberRole
		acceptedFactID := ""
		for _, member := range conflict.Members {
			if member.FactID == factID {
				targetRole = member.Role
			}
			if member.Role == ConflictMemberAccepted && acceptedFactID == "" {
				acceptedFactID = member.FactID
			}
		}
		if targetRole == "" {
			continue
		}
		switch status {
		case FactStatusReconciled:
			return conflict.ID, factID, "兼容接口采用冲突候选", true
		case FactStatusRevoked:
			if targetRole == ConflictMemberCandidate && acceptedFactID != "" {
				return conflict.ID, acceptedFactID, "兼容接口保留原事实", true
			}
			return conflict.ID, "", "", true
		}
	}
	return "", "", "", false
}

func reconciliationConflictIndices(events []MatchEvent, chosenIndex int) map[int]struct{} {
	conflicts := make(map[int]struct{})
	if chosenIndex < 0 || chosenIndex >= len(events) {
		return conflicts
	}
	chosen := events[chosenIndex]
	for _, index := range crossSourceConflictIndices(events, chosen) {
		if index != chosenIndex && events[index].FactStatus != FactStatusRevoked {
			conflicts[index] = struct{}{}
		}
	}
	for index, event := range events {
		if index == chosenIndex || event.Status != "active" || event.FactStatus != FactStatusConflict {
			continue
		}
		if event.EventType == chosen.EventType && event.Period == chosen.Period && clocksNear(event.Clock, chosen.Clock, 45) {
			conflicts[index] = struct{}{}
		}
	}
	return conflicts
}

func hasActiveFactConflict(events []MatchEvent) bool {
	for _, event := range events {
		if event.Status == "active" && event.FactStatus == FactStatusConflict {
			return true
		}
	}
	return false
}

func (s *Store) recordFactRevisionLocked(matchID string, event MatchEvent) {
	key := factHistoryKey(matchID, event.FactID)
	revision := FactRevision{
		FactID:        event.FactID,
		Revision:      event.FactRevision,
		MatchID:       matchID,
		EventID:       event.ID,
		Status:        event.FactStatus,
		SourceType:    factSourceType(event.Source),
		SourceEventID: event.ProviderEventID,
		Confidence:    event.Confidence,
		Evidence:      cloneEvidence(event.Evidence),
		ConfirmedBy:   event.ConfirmedBy,
		RevisionOf:    event.RevisionOf,
		OccurredAt:    event.CreatedAt,
		RecordedAt:    event.UpdatedAt,
		PublicAt:      event.PublicAt,
	}
	s.factHistory[key] = append(s.factHistory[key], revision)
}

func factHistoryKey(matchID, factID string) string {
	return strings.TrimSpace(matchID) + "\x00" + strings.TrimSpace(factID)
}

func sourceCursorKey(matchID, sourceType, sourceKey string) (string, error) {
	matchID = strings.TrimSpace(matchID)
	sourceType = strings.TrimSpace(sourceType)
	sourceKey = strings.TrimSpace(sourceKey)
	if matchID == "" || sourceType == "" || sourceKey == "" {
		return "", fmt.Errorf("%w: matchId, sourceType and sourceKey are required", ErrInvalid)
	}
	return matchID + "\x00" + sourceType + "\x00" + sourceKey, nil
}

func factSourceType(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "api-sports", "provider", "replay":
		return "provider"
	case "system":
		return "system"
	default:
		return "operator"
	}
}

func cloneEvidence(evidence map[string]any) map[string]any {
	if evidence == nil {
		return map[string]any{}
	}
	cloned := make(map[string]any, len(evidence))
	for key, value := range evidence {
		cloned[key] = value
	}
	return cloned
}

func (s *Store) Subscribe(matchID string) (<-chan MatchEvent, func()) {
	subscription := newEventSubscription()
	s.mu.Lock()
	if s.subscribers[matchID] == nil {
		s.subscribers[matchID] = make(map[*eventSubscription]struct{})
	}
	s.subscribers[matchID][subscription] = struct{}{}
	s.mu.Unlock()

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			s.mu.Lock()
			if subs := s.subscribers[matchID]; subs != nil {
				delete(subs, subscription)
				if len(subs) == 0 {
					delete(s.subscribers, matchID)
				}
			}
			s.mu.Unlock()
			subscription.close()
		})
	}
	return subscription.events, unsubscribe
}

func (s *Store) SetEventObserver(observer func(MatchEvent) error) {
	s.mu.Lock()
	s.eventObserver = observer
	s.mu.Unlock()
}

func (s *Store) SetFactProjectionAuditObserver(observer func(FactProjectionAudit)) {
	s.mu.Lock()
	s.projectionAudit = observer
	s.mu.Unlock()
}

func (s *Store) subscriberListLocked(matchID string) []*eventSubscription {
	var subs []*eventSubscription
	for subscription := range s.subscribers[matchID] {
		subs = append(subs, subscription)
	}
	return subs
}

func (s *Store) publish(subs []*eventSubscription, ev MatchEvent) error {
	s.mu.RLock()
	observer := s.eventObserver
	s.mu.RUnlock()
	var observerErr error
	if observer != nil {
		observerErr = observer(ev)
	}
	for _, subscription := range subs {
		subscription.enqueue(ev)
	}
	return observerErr
}

func normalize(ev *MatchEvent) {
	ev.Source = defaultString(ev.Source, "operator")
	ev.ProviderName = strings.TrimSpace(ev.ProviderName)
	ev.ProviderEventID = strings.TrimSpace(ev.ProviderEventID)
	ev.Period = normalizePeriod(ev.Period)
	ev.Visibility = defaultString(ev.Visibility, "public")
	ev.Status = defaultString(ev.Status, "active")
	normalizeFactMetadata(ev)
	ev.EventType = strings.TrimSpace(ev.EventType)
	ev.Clock = strings.TrimSpace(ev.Clock)
	ev.Description = strings.TrimSpace(ev.Description)
	ev.PlayerName = strings.TrimSpace(ev.PlayerName)
	if ev.Tags == nil {
		ev.Tags = []string{}
	}
	ev.Participants = normalizeParticipants(ev.Participants)
	if ev.EventType == "goal" && ev.PlayerName != "" && len(participantNamesByRole(ev.Participants, "scorer")) == 0 {
		ev.Participants = append([]Participant{{
			Role:     "scorer",
			Name:     ev.PlayerName,
			TeamID:   ev.TeamID,
			TeamName: ev.TeamName,
		}}, ev.Participants...)
	}
	if len(ev.Participants) == 0 && ev.PlayerName != "" {
		ev.Participants = []Participant{{
			Role:     DefaultParticipantRole(ev.EventType),
			Name:     ev.PlayerName,
			TeamID:   ev.TeamID,
			TeamName: ev.TeamName,
		}}
	}
	if ev.PlayerName == "" && len(ev.Participants) > 0 {
		ev.PlayerName = ev.Participants[0].Name
	}
	if ev.Intensity < 1 {
		ev.Intensity = 3
	}
	if ev.Intensity > 5 {
		ev.Intensity = 5
	}
	if ev.RecommendedAction == "" {
		ev.RecommendedAction = DefaultAction(ev.EventType)
	}
	if ev.Sentiment == "" {
		ev.Sentiment = DefaultSentiment(ev.EventType)
	}
}

func normalizeFactMetadata(event *MatchEvent) {
	if event.FactStatus == "" {
		source := strings.ToLower(strings.TrimSpace(event.Source))
		operatorSource := source == "" || source == "operator" || source == "manual" || source == "system"
		if event.Confirmed || operatorSource {
			event.FactStatus = FactStatusConfirmed
		} else {
			event.FactStatus = FactStatusProvisional
		}
	}
	event.Confirmed = event.FactStatus == FactStatusConfirmed || event.FactStatus == FactStatusReconciled
	if event.FactRevision < 1 {
		event.FactRevision = 1
	}
	if event.Confidence == 0 && event.Confirmed {
		event.Confidence = 1
	}
	if event.Evidence == nil {
		event.Evidence = map[string]any{}
	}
}

func IsPublicFact(event MatchEvent) bool {
	if event.Status != "active" || event.Visibility != "public" {
		return false
	}
	return event.FactStatus == FactStatusConfirmed || event.FactStatus == FactStatusReconciled
}

func filterPublicFacts(events []MatchEvent) []MatchEvent {
	public := make([]MatchEvent, 0, len(events))
	for _, event := range events {
		if IsPublicFact(event) {
			public = append(public, event)
		}
	}
	return public
}

func normalizePeriod(period string) string {
	period = strings.ToLower(strings.TrimSpace(period))
	period = strings.ReplaceAll(period, "-", "_")
	period = strings.ReplaceAll(period, " ", "_")
	switch period {
	case "", "firsthalf":
		return "first_half"
	case "prematch":
		return "pre_match"
	case "halftime", "half_time":
		return "halftime"
	case "secondhalf":
		return "second_half"
	case "extratime":
		return "extra_time"
	case "full_time", "finished":
		return "fulltime"
	default:
		return period
	}
}

func normalizeParticipants(participants []Participant) []Participant {
	out := make([]Participant, 0, len(participants))
	for _, participant := range participants {
		participant.Role = strings.TrimSpace(participant.Role)
		participant.Name = strings.TrimSpace(participant.Name)
		participant.TeamID = strings.TrimSpace(participant.TeamID)
		participant.TeamName = strings.TrimSpace(participant.TeamName)
		if participant.Name == "" {
			continue
		}
		if participant.Role == "" {
			participant.Role = "player"
		}
		out = append(out, participant)
	}
	return out
}

func validate(ev MatchEvent) error {
	if ev.EventType == "" {
		return fmt.Errorf("%w: eventType is required", ErrInvalid)
	}
	if ev.Clock == "" {
		return fmt.Errorf("%w: clock is required", ErrInvalid)
	}
	if ev.Description == "" {
		return fmt.Errorf("%w: description is required", ErrInvalid)
	}
	if !allowedEventTypes[ev.EventType] {
		return fmt.Errorf("%w: unsupported eventType %q", ErrInvalid, ev.EventType)
	}
	if !allowedPeriods[ev.Period] {
		return fmt.Errorf("%w: unsupported period %q", ErrInvalid, ev.Period)
	}
	if ev.Score.Home < 0 || ev.Score.Away < 0 {
		return fmt.Errorf("%w: score cannot be negative", ErrInvalid)
	}
	if !validFactStatus(ev.FactStatus) {
		return fmt.Errorf("%w: unsupported factStatus %q", ErrInvalid, ev.FactStatus)
	}
	if ev.Confidence < 0 || ev.Confidence > 1 {
		return fmt.Errorf("%w: confidence must be between 0 and 1", ErrInvalid)
	}
	if ev.EventType == "goal" && ev.Period == "pre_match" {
		return fmt.Errorf("%w: goal cannot occur before kickoff", ErrInvalid)
	}
	if ev.EventType == "goal_cancelled" && ev.RevisionOf == "" {
		return fmt.Errorf("%w: goal cancellation requires revisionOf", ErrInvalid)
	}
	if ev.EventType == "var_result" && ev.RevisionOf == "" {
		return fmt.Errorf("%w: VAR result requires revisionOf", ErrInvalid)
	}
	if ev.EventType == "score_correction" {
		reason, _ := ev.Evidence["correctionReason"].(string)
		if strings.TrimSpace(reason) == "" {
			return fmt.Errorf("%w: score correction requires correctionReason evidence", ErrInvalid)
		}
	}
	return nil
}

func validFactStatus(status FactStatus) bool {
	switch status {
	case FactStatusProvisional, FactStatusConfirmed, FactStatusRevoked, FactStatusConflict, FactStatusReconciled:
		return true
	default:
		return false
	}
}

func validateFactTransition(current, target FactStatus) error {
	switch target {
	case FactStatusRevoked:
		if current == FactStatusProvisional || current == FactStatusConfirmed || current == FactStatusConflict || current == FactStatusReconciled {
			return nil
		}
	case FactStatusReconciled:
		if current == FactStatusConflict {
			return nil
		}
	}
	return fmt.Errorf("%w: fact cannot transition from %s to %s", ErrInvalid, current, target)
}

func validateAgainstSnapshot(ev MatchEvent, current Snapshot, config MatchConfig) error {
	if err := validateEventTeam(ev, config); err != nil {
		return err
	}
	if ev.EventType == "goal" {
		expected := current.Score
		switch ev.TeamID {
		case "home":
			expected.Home++
		case "away":
			expected.Away++
		default:
			return fmt.Errorf("%w: goal requires home or away teamId", ErrInvalid)
		}
		if ev.Score != expected {
			return fmt.Errorf("%w: goal score must move from %d-%d to %d-%d", ErrInvalid, current.Score.Home, current.Score.Away, expected.Home, expected.Away)
		}
		return nil
	}
	if ev.EventType == "goal_cancelled" {
		expected := current.Score
		switch ev.TeamID {
		case "home":
			expected.Home--
		case "away":
			expected.Away--
		default:
			return fmt.Errorf("%w: goal cancellation requires home or away teamId", ErrInvalid)
		}
		if expected.Home < 0 || expected.Away < 0 || ev.Score != expected {
			return fmt.Errorf("%w: goal cancellation score must move from %d-%d to %d-%d", ErrInvalid, current.Score.Home, current.Score.Away, expected.Home, expected.Away)
		}
		return nil
	}
	if ev.EventType == "score_correction" {
		if ev.Score == current.Score {
			return fmt.Errorf("%w: score correction must change the public score", ErrInvalid)
		}
		return nil
	}
	if ev.Score != current.Score {
		return fmt.Errorf("%w: %s cannot change score from %d-%d to %d-%d", ErrInvalid, ev.EventType, current.Score.Home, current.Score.Away, ev.Score.Home, ev.Score.Away)
	}
	return nil
}

func crossSourceEventError(events []MatchEvent, candidate MatchEvent) error {
	if len(crossSourceConflictIndices(events, candidate)) > 0 {
		return ErrConflict
	}
	if !crossSourceComparable(candidate.EventType) {
		return nil
	}
	for _, existing := range events {
		if existing.Status != "active" || existing.EventType != candidate.EventType || existing.Source == candidate.Source {
			continue
		}
		if existing.Period != candidate.Period || !clocksNear(existing.Clock, candidate.Clock, 45) {
			continue
		}
		playersCompatible := existing.PlayerName == "" || candidate.PlayerName == "" || strings.EqualFold(existing.PlayerName, candidate.PlayerName)
		if existing.TeamID == candidate.TeamID && playersCompatible {
			return ErrDuplicate
		}
	}
	return nil
}

func crossSourceConflictIndices(events []MatchEvent, candidate MatchEvent) []int {
	if !crossSourceComparable(candidate.EventType) {
		return nil
	}
	var conflicts []int
	for index, existing := range events {
		if crossSourceFactsConflict(existing, candidate) {
			conflicts = append(conflicts, index)
		}
	}
	return conflicts
}

func crossSourceFactsConflict(existing, candidate MatchEvent) bool {
	if existing.Status != "active" || existing.EventType != candidate.EventType || existing.Source == candidate.Source {
		return false
	}
	if existing.Period != candidate.Period || !clocksNear(existing.Clock, candidate.Clock, 45) {
		return false
	}
	playersCompatible := existing.PlayerName == "" || candidate.PlayerName == "" || strings.EqualFold(existing.PlayerName, candidate.PlayerName)
	return existing.TeamID != candidate.TeamID || !playersCompatible
}

func crossSourceComparable(eventType string) bool {
	switch eventType {
	case "goal", "red_card", "var_check", "penalty":
		return true
	default:
		return false
	}
}

func clocksNear(left, right string, toleranceSeconds int) bool {
	leftSeconds, leftOK := clockSeconds(left)
	rightSeconds, rightOK := clockSeconds(right)
	if !leftOK || !rightOK {
		return strings.TrimSpace(left) == strings.TrimSpace(right)
	}
	difference := leftSeconds - rightSeconds
	if difference < 0 {
		difference = -difference
	}
	return difference <= toleranceSeconds
}

func clockSeconds(clock string) (int, bool) {
	minuteText, secondText, found := strings.Cut(strings.TrimSpace(clock), ":")
	if !found {
		return 0, false
	}
	minute, minuteErr := strconv.Atoi(minuteText)
	second, secondErr := strconv.Atoi(secondText)
	if minuteErr != nil || secondErr != nil || minute < 0 || second < 0 || second > 59 {
		return 0, false
	}
	return minute*60 + second, true
}

func validateEventTeam(ev MatchEvent, config MatchConfig) error {
	expectedTeamName := ""
	switch ev.TeamID {
	case "":
		if ev.EventType == "goal" || ev.EventType == "substitution" {
			return fmt.Errorf("%w: %s requires home or away teamId", ErrInvalid, ev.EventType)
		}
		return nil
	case "home":
		expectedTeamName = config.HomeTeam
	case "away":
		expectedTeamName = config.AwayTeam
	default:
		return fmt.Errorf("%w: teamId must be home or away", ErrInvalid)
	}
	configuredTeamName := expectedTeamName != "" && expectedTeamName != "主队" && expectedTeamName != "客队"
	if configuredTeamName && ev.TeamName != "" && !strings.EqualFold(ev.TeamName, expectedTeamName) {
		return fmt.Errorf("%w: teamName %q does not match %s team %q", ErrInvalid, ev.TeamName, ev.TeamID, expectedTeamName)
	}
	if ev.EventType == "substitution" {
		return validateSubstitutionParticipants(ev, config)
	}
	if ev.EventType != "goal" {
		return nil
	}
	scorers := participantNamesByRole(ev.Participants, "scorer")
	if len(scorers) > 1 {
		return fmt.Errorf("%w: goal cannot have multiple scorers", ErrInvalid)
	}
	if ev.PlayerName != "" && len(scorers) != 1 {
		return fmt.Errorf("%w: known goal player requires one scorer", ErrInvalid)
	}
	if ev.PlayerName != "" && !strings.EqualFold(ev.PlayerName, scorers[0]) {
		return fmt.Errorf("%w: playerName must match the scorer", ErrInvalid)
	}
	if teamID := configuredPlayerTeam(config, ev.PlayerName); teamID != "" && teamID != ev.TeamID {
		return fmt.Errorf("%w: scorer %q belongs to %s team", ErrInvalid, ev.PlayerName, teamID)
	}
	for _, participant := range ev.Participants {
		if participant.Role != "scorer" && participant.Role != "assist" && participant.Role != "pre_assist" {
			continue
		}
		if participant.TeamID != "" && participant.TeamID != ev.TeamID {
			return fmt.Errorf("%w: %s %q must belong to the scoring team", ErrInvalid, participant.Role, participant.Name)
		}
		if teamID := configuredPlayerTeam(config, participant.Name); teamID != "" && teamID != ev.TeamID {
			return fmt.Errorf("%w: %s %q belongs to %s team", ErrInvalid, participant.Role, participant.Name, teamID)
		}
	}
	return nil
}

func validateSubstitutionParticipants(ev MatchEvent, config MatchConfig) error {
	var subOn, subOff *Participant
	for index := range ev.Participants {
		participant := &ev.Participants[index]
		switch participant.Role {
		case "sub_on":
			if subOn != nil {
				return fmt.Errorf("%w: substitution requires exactly one sub_on player", ErrInvalid)
			}
			subOn = participant
		case "sub_off":
			if subOff != nil {
				return fmt.Errorf("%w: substitution requires exactly one sub_off player", ErrInvalid)
			}
			subOff = participant
		}
	}
	if subOn == nil || subOff == nil {
		return fmt.Errorf("%w: substitution requires sub_on and sub_off players", ErrInvalid)
	}
	if strings.EqualFold(subOn.Name, subOff.Name) {
		return fmt.Errorf("%w: substitution players must be different", ErrInvalid)
	}
	for _, participant := range []*Participant{subOn, subOff} {
		if participant.TeamID != ev.TeamID {
			return fmt.Errorf("%w: %s %q must belong to the substituted team", ErrInvalid, participant.Role, participant.Name)
		}
		if teamID := configuredPlayerTeam(config, participant.Name); teamID != "" && teamID != ev.TeamID {
			return fmt.Errorf("%w: %s %q belongs to %s team", ErrInvalid, participant.Role, participant.Name, teamID)
		}
	}
	return nil
}

func validateEventRelations(events []MatchEvent, candidate MatchEvent) error {
	if candidate.EventType != "var_result" && candidate.EventType != "goal_cancelled" {
		return nil
	}
	orderedEvents, err := orderFactEvents(events)
	if err != nil {
		return err
	}
	var referenced *MatchEvent
	referencedIndex := -1
	for index := range orderedEvents {
		event := &orderedEvents[index]
		if event.Status == "active" && (event.ID == candidate.RevisionOf || event.FactID == candidate.RevisionOf) {
			referenced = event
			referencedIndex = index
		}
	}
	if referenced == nil ||
		(referenced.FactStatus != FactStatusConfirmed && referenced.FactStatus != FactStatusReconciled) {
		return fmt.Errorf("%w: %s must reference an active confirmed fact", ErrInvalid, candidate.EventType)
	}
	if IsPublicFact(candidate) && !IsPublicFact(*referenced) {
		return fmt.Errorf("%w: public %s must reference a public fact", ErrInvalid, candidate.EventType)
	}
	if candidate.EventType == "var_result" {
		return nil
	}
	if referenced.EventType != "goal" {
		return fmt.Errorf("%w: goal cancellation must reference an active confirmed goal", ErrInvalid)
	}
	if referenced.TeamID != candidate.TeamID {
		return fmt.Errorf("%w: goal cancellation team must match the referenced goal", ErrInvalid)
	}
	for _, event := range orderedEvents[referencedIndex+1:] {
		if IsPublicFact(event) && event.EventType == "score_correction" {
			return fmt.Errorf("%w: goal cancellation cannot target a goal before the latest score correction", ErrInvalid)
		}
	}
	for _, event := range orderedEvents {
		if IsPublicFact(event) && event.EventType == "goal_cancelled" &&
			(event.RevisionOf == referenced.ID || event.RevisionOf == referenced.FactID) {
			return fmt.Errorf("%w: referenced goal is already cancelled", ErrInvalid)
		}
	}
	return nil
}

func participantNamesByRole(participants []Participant, role string) []string {
	var names []string
	for _, participant := range participants {
		if participant.Role == role && participant.Name != "" {
			names = append(names, participant.Name)
		}
	}
	return names
}

func validateCorrection(original, replacement MatchEvent, current Snapshot, config MatchConfig) error {
	if isRelationshipEventType(original.EventType) || isRelationshipEventType(replacement.EventType) {
		return fmt.Errorf("%w: relationship events must be revoked and recreated instead of corrected", ErrInvalid)
	}
	if err := validateEventTeam(replacement, config); err != nil {
		return err
	}
	expected := current.Score
	if original.EventType == "goal" && original.FactStatus != FactStatusConflict && original.FactStatus != FactStatusRevoked {
		if err := changeTeamScore(&expected, original.TeamID, -1); err != nil {
			return err
		}
	}
	if replacement.EventType == "goal" {
		if err := changeTeamScore(&expected, replacement.TeamID, 1); err != nil {
			return err
		}
	}
	if expected.Home < 0 || expected.Away < 0 {
		return fmt.Errorf("%w: correction produces a negative score", ErrInvalid)
	}
	if replacement.Score != expected {
		return fmt.Errorf("%w: correction score must be %d-%d", ErrInvalid, expected.Home, expected.Away)
	}
	return nil
}

func isRelationshipEventType(eventType string) bool {
	return eventType == "goal_cancelled" || eventType == "var_result"
}

func validateCorrectionTimeline(events []MatchEvent, original, replacement MatchEvent) error {
	for _, event := range events {
		if event.FactStatus != FactStatusConflict || !crossSourceFactsConflict(event, original) {
			continue
		}
		if !crossSourceFactsConflict(event, replacement) {
			return fmt.Errorf("%w: resolve the open conflict before changing its matching fields", ErrInvalid)
		}
	}
	changesGoalContribution := original.EventType != replacement.EventType || (original.EventType == "goal" && original.TeamID != replacement.TeamID)
	if !changesGoalContribution {
		return nil
	}
	for _, event := range events {
		if event.ID == original.ID || event.Status != "active" {
			continue
		}
		if event.CreatedAt >= original.CreatedAt {
			return fmt.Errorf("%w: later events must be corrected before changing this event's score contribution", ErrInvalid)
		}
	}
	return nil
}

func changeTeamScore(score *Score, teamID string, delta int) error {
	switch teamID {
	case "home":
		score.Home += delta
	case "away":
		score.Away += delta
	default:
		return fmt.Errorf("%w: score-changing event requires home or away teamId", ErrInvalid)
	}
	return nil
}

func configuredPlayerTeam(config MatchConfig, name string) string {
	for _, player := range config.HomePlayers {
		if strings.EqualFold(player.Name, name) {
			return "home"
		}
	}
	for _, player := range config.AwayPlayers {
		if strings.EqualFold(player.Name, name) {
			return "away"
		}
	}
	return ""
}

func buildLegacySnapshot(matchID string, events []MatchEvent, config MatchConfig, clock MatchClock, now time.Time) Snapshot {
	config = normalizeConfig(matchID, config)
	clock = normalizeMatchClock(matchID, clock)
	snap := Snapshot{
		MatchID:              matchID,
		HomeTeam:             config.HomeTeam,
		AwayTeam:             config.AwayTeam,
		Score:                Score{},
		Period:               clock.Period,
		Clock:                clock.displayAt(now),
		MatchClock:           clock,
		Momentum:             "neutral",
		EmotionalTemperature: 1,
		LastUpdatedAt:        time.Now().UTC().Format(time.RFC3339Nano),
		Integrity:            config.Integrity,
	}

	for _, ev := range events {
		if ev.Status != "active" || ev.FactStatus == FactStatusConflict || ev.FactStatus == FactStatusRevoked {
			continue
		}
		if ev.TeamName != "" {
			switch ev.TeamID {
			case "home":
				snap.HomeTeam = ev.TeamName
			case "away":
				snap.AwayTeam = ev.TeamName
			}
		}
		snap.Score = ev.Score
		snap.EmotionalTemperature = ev.Intensity
		snap.LastRecommendedAction = ev.RecommendedAction
		snap.LastPublicDescription = ev.Description
		snap.LastUpdatedAt = ev.UpdatedAt
		snap.RecentEvents = append([]MatchEvent{ev}, snap.RecentEvents...)
		if isKeyEvent(ev.EventType) {
			snap.KeyEvents = append([]MatchEvent{ev}, snap.KeyEvents...)
		}
		snap.Momentum = momentumFor(ev)
	}
	if !clock.UpdatedAt.IsZero() && clock.UpdatedAt.Format(time.RFC3339Nano) > snap.LastUpdatedAt {
		snap.LastUpdatedAt = clock.UpdatedAt.Format(time.RFC3339Nano)
	}

	if len(snap.RecentEvents) > 5 {
		snap.RecentEvents = snap.RecentEvents[:5]
	}
	if len(snap.KeyEvents) > 8 {
		snap.KeyEvents = snap.KeyEvents[:8]
	}
	return snap
}

func normalizeConfig(matchID string, config MatchConfig) MatchConfig {
	config.MatchID = defaultString(config.MatchID, matchID)
	config.HomeTeam = defaultString(config.HomeTeam, "主队")
	config.AwayTeam = defaultString(config.AwayTeam, "客队")
	config.HomePlayers = normalizePlayers(config.HomePlayers)
	config.AwayPlayers = normalizePlayers(config.AwayPlayers)
	config.Automation = normalizeAutomationPolicy(config.Automation)
	if strings.TrimSpace(config.Integrity.Status) == "" {
		config.Integrity = MatchIntegrity{Status: "ok"}
	}
	return config
}

func conflictIntegrity(event MatchEvent) MatchIntegrity {
	return MatchIntegrity{
		Status:     "conflict",
		Reason:     fmt.Sprintf("conflicting %s evidence from %s at %s", event.EventType, event.Source, event.Clock),
		DetectedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func DefaultAutomationPolicy() AutomationPolicy {
	return AutomationPolicy{
		Mode: AutomationModeActive,
		EventTypes: []string{
			"kickoff", "goal", "shot", "big_chance", "save", "miss", "foul",
			"yellow_card", "red_card", "var_check", "var_result", "goal_cancelled",
			"penalty", "penalty_awarded", "substitution", "injury", "tactical_shift",
			"pressure", "halftime", "fulltime", "match_end",
		},
		CooldownSeconds: 90,
	}
}

func normalizeAutomationPolicy(policy AutomationPolicy) AutomationPolicy {
	if strings.TrimSpace(policy.Mode) == "" {
		return DefaultAutomationPolicy()
	}
	policy.Mode = strings.TrimSpace(policy.Mode)
	seen := make(map[string]struct{}, len(policy.EventTypes))
	eventTypes := make([]string, 0, len(policy.EventTypes))
	for _, eventType := range policy.EventTypes {
		eventType = strings.TrimSpace(eventType)
		if eventType == "" || !allowedEventTypes[eventType] {
			continue
		}
		if _, ok := seen[eventType]; ok {
			continue
		}
		seen[eventType] = struct{}{}
		eventTypes = append(eventTypes, eventType)
	}
	policy.EventTypes = eventTypes
	return policy
}

func validateAutomationPolicy(policy AutomationPolicy) error {
	mode := strings.TrimSpace(policy.Mode)
	if mode != AutomationModeActive && mode != AutomationModePaused {
		return fmt.Errorf("%w: automation mode must be active or paused", ErrInvalid)
	}
	if policy.CooldownSeconds < 0 || policy.CooldownSeconds > 300 {
		return fmt.Errorf("%w: cooldownSeconds must be between 0 and 300", ErrInvalid)
	}
	for _, eventType := range policy.EventTypes {
		eventType = strings.TrimSpace(eventType)
		if !allowedEventTypes[eventType] || eventType == "operator_note" {
			return fmt.Errorf("%w: unsupported automation eventType %q", ErrInvalid, eventType)
		}
	}
	return nil
}

func normalizePlayers(players []Player) []Player {
	out := make([]Player, 0, len(players))
	for _, player := range players {
		player.Name = strings.TrimSpace(player.Name)
		player.Number = strings.TrimSpace(player.Number)
		player.Position = strings.TrimSpace(player.Position)
		if player.Name == "" {
			continue
		}
		out = append(out, player)
	}
	return out
}

func isKeyEvent(t string) bool {
	switch t {
	case "goal", "red_card", "penalty", "penalty_awarded", "var_check", "var_result", "goal_cancelled", "score_correction", "halftime", "fulltime", "match_end":
		return true
	default:
		return false
	}
}

func momentumFor(ev MatchEvent) string {
	switch ev.EventType {
	case "goal", "big_chance", "shot", "pressure":
		if ev.TeamID != "" {
			return ev.TeamID + "_pressure"
		}
		return "pressure"
	case "halftime", "fulltime":
		return "settled"
	default:
		return "neutral"
	}
}

func DefaultAction(eventType string) string {
	switch eventType {
	case "goal":
		return "celebrate"
	case "big_chance", "penalty", "penalty_awarded", "var_check":
		return "tense"
	case "var_result", "goal_cancelled":
		return "settle"
	case "save":
		return "surprise"
	case "miss":
		return "miss"
	case "foul", "yellow_card":
		return "complain"
	case "red_card":
		return "angry"
	case "tactical_shift", "score_correction", "halftime":
		return "analysis"
	case "pressure":
		return "focus"
	case "fulltime", "match_end":
		return "comfort"
	case "kickoff":
		return "wave"
	default:
		return "listen"
	}
}

func DefaultParticipantRole(eventType string) string {
	switch eventType {
	case "goal":
		return "scorer"
	case "shot", "miss":
		return "shooter"
	case "save":
		return "keeper"
	case "foul", "yellow_card", "red_card":
		return "offender"
	case "penalty":
		return "taker"
	case "substitution":
		return "sub_on"
	case "injury":
		return "injured"
	default:
		return "player"
	}
}

func DefaultSentiment(eventType string) string {
	switch eventType {
	case "goal":
		return "celebratory"
	case "big_chance", "penalty", "penalty_awarded", "var_check", "pressure":
		return "tense"
	case "var_result", "goal_cancelled":
		return "disappointed"
	case "miss":
		return "regret"
	case "foul", "yellow_card", "red_card":
		return "complaint"
	case "tactical_shift", "score_correction", "halftime":
		return "analytical"
	default:
		return "neutral"
	}
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

var allowedEventTypes = map[string]bool{
	"kickoff":          true,
	"goal":             true,
	"shot":             true,
	"big_chance":       true,
	"save":             true,
	"miss":             true,
	"foul":             true,
	"yellow_card":      true,
	"red_card":         true,
	"var_check":        true,
	"var_result":       true,
	"goal_cancelled":   true,
	"score_correction": true,
	"penalty":          true,
	"penalty_awarded":  true,
	"substitution":     true,
	"injury":           true,
	"tactical_shift":   true,
	"pressure":         true,
	"halftime":         true,
	"fulltime":         true,
	"match_end":        true,
	"operator_note":    true,
}

var allowedPeriods = map[string]bool{
	"pre_match":   true,
	"first_half":  true,
	"halftime":    true,
	"second_half": true,
	"extra_time":  true,
	"fulltime":    true,
}

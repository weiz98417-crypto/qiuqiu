package interaction

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"sync"
	"time"

	"qiuqiu/internal/privacy"
	"qiuqiu/internal/relationship"
)

type Kind string

const (
	KindTurnPlanned    Kind = "turn_planned"
	KindDelivery       Kind = "delivery"
	KindSignal         Kind = "signal"
	KindFactRevision   Kind = "fact_revision"
	KindMediaDelivery  Kind = "media_delivery"
	KindPlaybackResult Kind = "playback_result"
	KindTurnStale      Kind = "turn_stale"
)

var (
	ErrInvalidEvent = errors.New("invalid interaction event")
	ErrConflict     = errors.New("interaction event conflict")
)

// Event is the append-only correlation envelope for signals, plans and outcomes.
// Payloads intentionally contain identifiers and classifications, not raw audio.
type Event struct {
	ID             string                         `json:"id"`
	Kind           Kind                           `json:"kind"`
	SignalID       string                         `json:"signalId,omitempty"`
	UserID         string                         `json:"userId"`
	MatchID        string                         `json:"matchId"`
	TraceID        string                         `json:"traceId,omitempty"`
	DecisionID     string                         `json:"decisionId,omitempty"`
	FactIDs        []string                       `json:"factIds,omitempty"`
	FactRevision   string                         `json:"factRevision,omitempty"`
	DeliveryKey    string                         `json:"deliveryKey,omitempty"`
	DeliveryState  string                         `json:"deliveryState,omitempty"`
	DeliveryReason string                         `json:"deliveryReason,omitempty"`
	MediaType      string                         `json:"mediaType,omitempty"`
	PlaybackState  string                         `json:"playbackState,omitempty"`
	InputText      string                         `json:"inputText,omitempty"`
	OutputText     string                         `json:"outputText,omitempty"`
	Decision       *relationship.Decision         `json:"decision,omitempty"`
	Presentation   *relationship.PresentationPlan `json:"presentation,omitempty"`
	TracePayload   json.RawMessage                `json:"trace,omitempty"`
	Stale          bool                           `json:"stale,omitempty"`
	Source         string                         `json:"source,omitempty"`
	CreatedAt      time.Time                      `json:"createdAt"`
	ExpiresAt      time.Time                      `json:"expiresAt,omitempty"`
}

type Ledger interface {
	Append(context.Context, Event) (Event, error)
	List(context.Context, string, string, int) ([]Event, error)
	ListUser(context.Context, string, int) ([]Event, error)
}

type MemoryLedger struct {
	mu     sync.Mutex
	byID   map[string]Event
	byPair map[string][]string
}

func NewMemoryLedger() *MemoryLedger {
	return &MemoryLedger{byID: make(map[string]Event), byPair: make(map[string][]string)}
}

func (l *MemoryLedger) Append(_ context.Context, event Event) (Event, error) {
	if err := validateEvent(event); err != nil {
		return Event{}, err
	}
	event = canonicalEvent(event)
	if l == nil {
		return Event{}, ErrInvalidEvent
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	event.CreatedAt = event.CreatedAt.UTC()
	if event.ExpiresAt.IsZero() {
		event.ExpiresAt = privacy.RetentionExpiry(event.CreatedAt)
	}
	event.FactIDs = append([]string(nil), event.FactIDs...)
	l.mu.Lock()
	defer l.mu.Unlock()
	if existing, ok := l.byID[event.ID]; ok {
		if !sameEvent(existing, event) {
			return Event{}, ErrConflict
		}
		return existing, nil
	}
	l.byID[event.ID] = event
	pair := event.UserID + ":" + event.MatchID
	l.byPair[pair] = append(l.byPair[pair], event.ID)
	return event, nil
}

func (l *MemoryLedger) List(_ context.Context, userID, matchID string, limit int) ([]Event, error) {
	return l.list(userID, matchID, limit)
}

func (l *MemoryLedger) ListUser(_ context.Context, userID string, limit int) ([]Event, error) {
	return l.list(userID, "", limit)
}

func (l *MemoryLedger) list(userID, matchID string, limit int) ([]Event, error) {
	if l == nil {
		return nil, nil
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	items := make([]Event, 0)
	for _, event := range l.byID {
		if event.UserID != userID || (matchID != "" && event.MatchID != matchID) {
			continue
		}
		if !event.ExpiresAt.IsZero() && !event.ExpiresAt.After(time.Now().UTC()) {
			continue
		}
		items = append(items, event)
	}
	sort.SliceStable(items, func(left, right int) bool {
		if items[left].CreatedAt.Equal(items[right].CreatedAt) {
			leftRank := interactionEventRank(items[left])
			rightRank := interactionEventRank(items[right])
			if leftRank != rightRank {
				return leftRank < rightRank
			}
			return items[left].ID < items[right].ID
		}
		return items[left].CreatedAt.Before(items[right].CreatedAt)
	})
	if len(items) > limit {
		items = items[len(items)-limit:]
	}
	return append([]Event(nil), items...), nil
}

func interactionEventRank(event Event) int {
	if event.Kind != KindPlaybackResult {
		return 0
	}
	switch event.PlaybackState {
	case "started":
		return 1
	case "ended", "completed", "interrupted", "skipped", "blocked":
		return 2
	default:
		return 3
	}
}

func validateEvent(event Event) error {
	if event.ID == "" || event.Kind == "" || event.UserID == "" || event.MatchID == "" {
		return ErrInvalidEvent
	}
	return nil
}

func sameEvent(left, right Event) bool {
	if left.ID != right.ID || left.Kind != right.Kind || left.SignalID != right.SignalID || left.UserID != right.UserID || left.MatchID != right.MatchID || left.TraceID != right.TraceID || left.DecisionID != right.DecisionID || left.FactRevision != right.FactRevision || left.DeliveryKey != right.DeliveryKey || left.DeliveryState != right.DeliveryState || left.DeliveryReason != right.DeliveryReason || left.MediaType != right.MediaType || left.PlaybackState != right.PlaybackState || left.InputText != right.InputText || left.OutputText != right.OutputText || !reflect.DeepEqual(left.Decision, right.Decision) || !reflect.DeepEqual(left.Presentation, right.Presentation) || !sameTracePayload(left.TracePayload, right.TracePayload) || left.Stale != right.Stale || left.Source != right.Source || !left.ExpiresAt.Equal(right.ExpiresAt) {
		return false
	}
	if len(left.FactIDs) != len(right.FactIDs) {
		return false
	}
	for index := range left.FactIDs {
		if left.FactIDs[index] != right.FactIDs[index] {
			return false
		}
	}
	return true
}

func sameTracePayload(left, right json.RawMessage) bool {
	if len(left) == 0 || string(left) == "null" {
		return len(right) == 0 || string(right) == "null"
	}
	if len(right) == 0 || string(right) == "null" {
		return false
	}
	var leftTrace, rightTrace map[string]any
	if json.Unmarshal(left, &leftTrace) != nil || json.Unmarshal(right, &rightTrace) != nil {
		return reflect.DeepEqual(left, right)
	}
	delete(leftTrace, "latencyMs")
	delete(rightTrace, "latencyMs")
	return reflect.DeepEqual(leftTrace, rightTrace)
}

func canonicalEvent(event Event) Event {
	if !event.CreatedAt.IsZero() {
		event.CreatedAt = event.CreatedAt.UTC().Truncate(time.Microsecond)
	}
	if !event.ExpiresAt.IsZero() {
		event.ExpiresAt = event.ExpiresAt.UTC().Truncate(time.Microsecond)
	}
	event.FactIDs = append([]string(nil), event.FactIDs...)
	event.TracePayload = append(json.RawMessage(nil), event.TracePayload...)
	if event.Decision != nil {
		decision := *event.Decision
		decision.Actions = append([]relationship.CommunicationAct(nil), decision.Actions...)
		decision.ReasonCodes = append([]string(nil), decision.ReasonCodes...)
		decision.UsedMemoryIDs = append([]string(nil), decision.UsedMemoryIDs...)
		if len(decision.Memories) == 0 {
			decision.Memories = nil
		} else {
			decision.Memories = append([]relationship.RelationshipMemory(nil), decision.Memories...)
			for index := range decision.Memories {
				decision.Memories[index].Payload = append([]byte(nil), decision.Memories[index].Payload...)
				decision.Memories[index].PendingDecisionIDs = append([]string(nil), decision.Memories[index].PendingDecisionIDs...)
			}
		}
		if decision.Speech != nil {
			speech := *decision.Speech
			speech.Actions = append([]relationship.CommunicationAct(nil), speech.Actions...)
			speech.Content.RequiredAnchors = append([]string(nil), speech.Content.RequiredAnchors...)
			speech.Content.ForbiddenClaims = append([]string(nil), speech.Content.ForbiddenClaims...)
			speech.Content.ForbiddenTopics = append([]string(nil), speech.Content.ForbiddenTopics...)
			speech.Content.RecentPhraseHashes = append([]uint64(nil), speech.Content.RecentPhraseHashes...)
			decision.Speech = &speech
		}
		event.Decision = &decision
	}
	if event.Presentation != nil {
		presentation := *event.Presentation
		event.Presentation = &presentation
	}
	return event
}

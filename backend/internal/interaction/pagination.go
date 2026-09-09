package interaction

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sort"
	"time"
)

var ErrInvalidCursor = errors.New("invalid interaction cursor")

type PageQuery struct {
	UserID  string
	MatchID string
	Limit   int
	Cursor  string
}

type Page struct {
	Events     []Event `json:"events"`
	NextCursor string  `json:"nextCursor,omitempty"`
}

func (page Page) HasMore() bool { return page.NextCursor != "" }

type PageableLedger interface {
	ListPage(context.Context, PageQuery) (Page, error)
}

type SnapshotLedger interface {
	ListSnapshot(context.Context, string, string) ([]Event, error)
}

type MatchSnapshotLedger interface {
	ListMatchSnapshot(context.Context, string) ([]Event, error)
}

type interactionCursor struct {
	CreatedAt time.Time `json:"createdAt"`
	Rank      int       `json:"rank"`
	ID        string    `json:"id"`
}

func (l *MemoryLedger) ListPage(_ context.Context, query PageQuery) (Page, error) {
	if l == nil {
		return Page{Events: []Event{}}, nil
	}
	cursor, err := decodeInteractionCursor(query.Cursor)
	if err != nil {
		return Page{}, err
	}
	limit := normalizePageLimit(query.Limit)
	now := time.Now().UTC()
	l.mu.Lock()
	items := make([]Event, 0, len(l.byID))
	for _, event := range l.byID {
		if event.UserID != query.UserID || (query.MatchID != "" && event.MatchID != query.MatchID) {
			continue
		}
		if !event.ExpiresAt.IsZero() && !event.ExpiresAt.After(now) {
			continue
		}
		if cursor != nil && compareEventCursor(event, *cursor) <= 0 {
			continue
		}
		items = append(items, event)
	}
	l.mu.Unlock()
	sortInteractionEvents(items)
	return interactionPage(items, limit)
}

func (l *MemoryLedger) ListSnapshot(_ context.Context, userID, matchID string) ([]Event, error) {
	if l == nil {
		return []Event{}, nil
	}
	now := time.Now().UTC()
	l.mu.Lock()
	items := make([]Event, 0, len(l.byID))
	for _, event := range l.byID {
		if event.UserID != userID || (matchID != "" && event.MatchID != matchID) {
			continue
		}
		if !event.ExpiresAt.IsZero() && !event.ExpiresAt.After(now) {
			continue
		}
		items = append(items, event)
	}
	l.mu.Unlock()
	sortInteractionEvents(items)
	return items, nil
}

func (l *MemoryLedger) ListMatchSnapshot(_ context.Context, matchID string) ([]Event, error) {
	if l == nil {
		return []Event{}, nil
	}
	now := time.Now().UTC()
	l.mu.Lock()
	items := make([]Event, 0, len(l.byID))
	for _, event := range l.byID {
		if event.MatchID != matchID {
			continue
		}
		if !event.ExpiresAt.IsZero() && !event.ExpiresAt.After(now) {
			continue
		}
		items = append(items, canonicalEvent(event))
	}
	l.mu.Unlock()
	sortInteractionEvents(items)
	return items, nil
}

func normalizePageLimit(limit int) int {
	if limit <= 0 {
		return 100
	}
	if limit > 500 {
		return 500
	}
	return limit
}

func sortInteractionEvents(events []Event) {
	sort.SliceStable(events, func(left, right int) bool {
		return compareInteractionEvents(events[left], events[right]) < 0
	})
}

func compareInteractionEvents(left, right Event) int {
	if left.CreatedAt.Before(right.CreatedAt) {
		return -1
	}
	if left.CreatedAt.After(right.CreatedAt) {
		return 1
	}
	leftRank := interactionEventRank(left)
	rightRank := interactionEventRank(right)
	if leftRank < rightRank {
		return -1
	}
	if leftRank > rightRank {
		return 1
	}
	if left.ID < right.ID {
		return -1
	}
	if left.ID > right.ID {
		return 1
	}
	return 0
}

func compareEventCursor(event Event, cursor interactionCursor) int {
	if event.CreatedAt.Before(cursor.CreatedAt) {
		return -1
	}
	if event.CreatedAt.After(cursor.CreatedAt) {
		return 1
	}
	rank := interactionEventRank(event)
	if rank < cursor.Rank {
		return -1
	}
	if rank > cursor.Rank {
		return 1
	}
	if event.ID < cursor.ID {
		return -1
	}
	if event.ID > cursor.ID {
		return 1
	}
	return 0
}

func interactionPage(events []Event, limit int) (Page, error) {
	hasMore := len(events) > limit
	if hasMore {
		events = events[:limit]
	}
	page := Page{Events: append([]Event(nil), events...)}
	if hasMore && len(events) > 0 {
		cursor, err := encodeInteractionCursor(events[len(events)-1])
		if err != nil {
			return Page{}, err
		}
		page.NextCursor = cursor
	}
	return page, nil
}

func encodeInteractionCursor(event Event) (string, error) {
	payload, err := json.Marshal(interactionCursor{
		CreatedAt: event.CreatedAt.UTC(), Rank: interactionEventRank(event), ID: event.ID,
	})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeInteractionCursor(value string) (*interactionCursor, error) {
	if value == "" {
		return nil, nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, ErrInvalidCursor
	}
	var cursor interactionCursor
	if err := json.Unmarshal(payload, &cursor); err != nil || cursor.CreatedAt.IsZero() || cursor.ID == "" || cursor.Rank < 0 || cursor.Rank > 3 {
		return nil, ErrInvalidCursor
	}
	cursor.CreatedAt = cursor.CreatedAt.UTC()
	return &cursor, nil
}

var _ PageableLedger = (*MemoryLedger)(nil)
var _ SnapshotLedger = (*MemoryLedger)(nil)
var _ MatchSnapshotLedger = (*MemoryLedger)(nil)

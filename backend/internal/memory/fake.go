package memory

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Fake is the second adapter (ADR-0006): a deterministic in-process
// Memories for tests and no-database dev mode, so the seam always has at
// least two implementations.
type Fake struct {
	mu        sync.Mutex
	moments   []Moment
	portraits map[string]Portrait
	nextID    int
}

func NewFake() *Fake {
	return &Fake{portraits: make(map[string]Portrait)}
}

func (f *Fake) Observe(_ context.Context, moment Moment) error {
	if f == nil {
		return nil
	}
	if moment.OccurredAt.IsZero() {
		moment.OccurredAt = time.Now().UTC()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	f.moments = append(f.moments, moment)
	return nil
}

func (f *Fake) Recall(_ context.Context, query Query) []Recall {
	if f == nil {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	limit := query.Limit
	if limit <= 0 {
		limit = defaultRecallLimit
	}
	matched := make([]Moment, 0, len(f.moments))
	for _, moment := range f.moments {
		if moment.UserID != query.UserID {
			continue
		}
		if focus := strings.TrimSpace(query.Focus); focus != "" &&
			!strings.Contains(moment.Content, focus) && !strings.Contains(focus, moment.Content) {
			continue
		}
		matched = append(matched, moment)
	}
	sort.SliceStable(matched, func(left, right int) bool {
		if matched[left].Importance != matched[right].Importance {
			return matched[left].Importance > matched[right].Importance
		}
		return matched[left].OccurredAt.After(matched[right].OccurredAt)
	})
	if len(matched) > limit {
		matched = matched[:limit]
	}
	recalls := make([]Recall, 0, len(matched))
	for _, moment := range matched {
		recalls = append(recalls, Recall{
			Content:    moment.Content,
			Importance: moment.Importance,
			OccurredAt: moment.OccurredAt,
			Source:     fmt.Sprintf("fake://moment/%s/%s", moment.UserID, moment.Kind),
		})
	}
	return recalls
}

func (f *Fake) Portrait(_ context.Context, userID string) (Portrait, error) {
	if f == nil {
		return Portrait{}, ErrUnavailable
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if portrait, ok := f.portraits[userID]; ok {
		return portrait, nil
	}
	entries := make([]profileEntry, 0, 4)
	for _, moment := range f.moments {
		if moment.UserID != userID {
			continue
		}
		if moment.Kind != MomentUserFact && moment.Kind != MomentPromise {
			continue
		}
		var entry profileEntry
		entry.Content = moment.Content
		entry.Attributes.Topic = "basic_info"
		entry.Attributes.SubTopic = string(moment.Kind)
		entry.UpdatedAt = moment.OccurredAt.UTC().Format(time.RFC3339)
		entries = append(entries, entry)
	}
	return Portrait{Block: RenderPortraitBlock(entries), UpdatedAt: latestMomentAt(f.moments, userID)}, nil
}

// Threads derives open promises from observed moments; the durable local
// store replaces this in Phase C2.
func (f *Fake) Threads(_ context.Context, userID string) ([]Thread, error) {
	if f == nil {
		return nil, ErrNotSupported
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	threads := make([]Thread, 0)
	for _, moment := range f.moments {
		if moment.UserID != userID {
			continue
		}
		if moment.Kind != MomentPromise {
			continue
		}
		f.nextID++
		threads = append(threads, Thread{
			ID:        fmt.Sprintf("fake-thread-%d", f.nextID),
			UserID:    userID,
			Kind:      ThreadPromise,
			Content:   moment.Content,
			State:     "open",
			CreatedAt: moment.OccurredAt,
		})
	}
	return threads, nil
}

// Moments returns a copy of everything observed (test accessor).
func (f *Fake) Moments() []Moment {
	if f == nil {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Moment(nil), f.moments...)
}

// SetPortrait overrides the synthesized portrait (tests, C3 page writes).
func (f *Fake) SetPortrait(userID string, portrait Portrait) {
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.portraits[userID] = portrait
}

func latestMomentAt(moments []Moment, userID string) time.Time {
	var latest time.Time
	for _, moment := range moments {
		if moment.UserID == userID && moment.OccurredAt.After(latest) {
			latest = moment.OccurredAt
		}
	}
	return latest
}

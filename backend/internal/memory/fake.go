package memory

import (
	"context"
	"fmt"
	"sort"
	"strconv"
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
	threads   []Thread
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
	entries := make([]PortraitEntry, 0, 4)
	for _, moment := range f.moments {
		if moment.UserID != userID {
			continue
		}
		if moment.Kind != MomentUserFact && moment.Kind != MomentPromise {
			continue
		}
		entries = append(entries, PortraitEntry{
			Topic:     "basic_info",
			SubTopic:  string(moment.Kind),
			Content:   moment.Content,
			UpdatedAt: moment.OccurredAt.UTC(),
			Source:    PortraitSourceSynthesis,
		})
	}
	updatedAt := latestMomentAt(f.moments, userID)
	if len(entries) == 0 {
		return Portrait{}, nil
	}
	return Portrait{Block: RenderPortraitBlock(entries, updatedAt), Entries: entries, UpdatedAt: updatedAt}, nil
}

// Threads lists the user's slice-backed open threads; the durable local
// store (threads.go) is the production counterpart (Phase C2).
func (f *Fake) Threads(_ context.Context, userID string) ([]Thread, error) {
	return f.OpenThreads(context.Background(), userID)
}

// AppendThread inserts one open-thread candidate into the slice and fills in
// the ledger id and timestamps (mirrors PostgresThreads.AppendThread).
func (f *Fake) AppendThread(_ context.Context, thread Thread) (Thread, error) {
	if f == nil {
		return Thread{}, ErrUnavailable
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	thread.ID = strconv.Itoa(f.nextID)
	thread.State = "open"
	if thread.CreatedAt.IsZero() {
		thread.CreatedAt = time.Now().UTC()
	}
	f.threads = append(f.threads, thread)
	return thread, nil
}

// OpenThreads returns the user's open threads, oldest first.
func (f *Fake) OpenThreads(_ context.Context, userID string) ([]Thread, error) {
	if f == nil {
		return nil, ErrUnavailable
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	open := make([]Thread, 0, 4)
	for _, thread := range f.threads {
		if thread.UserID == userID && thread.State == "open" {
			open = append(open, thread)
		}
	}
	return open, nil
}

// MarkThreadAddressed flips one open thread to 'addressed'; no-op when the
// thread is unknown or already closed (mirrors the SQL guard).
func (f *Fake) MarkThreadAddressed(_ context.Context, threadID string) error {
	if f == nil {
		return ErrUnavailable
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for index := range f.threads {
		if f.threads[index].ID == threadID && f.threads[index].State == "open" {
			f.threads[index].State = "addressed"
			return nil
		}
	}
	return nil
}

// ExpireStaleThreads flips open threads older than the TTL to 'expired' and
// returns them so callers can audit the transition.
func (f *Fake) ExpireStaleThreads(_ context.Context, now time.Time, ttl time.Duration) ([]Thread, error) {
	if f == nil {
		return nil, ErrUnavailable
	}
	if ttl <= 0 {
		ttl = DefaultThreadTTL
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	expired := make([]Thread, 0, 4)
	for index := range f.threads {
		if f.threads[index].State != "open" {
			continue
		}
		if !f.threads[index].CreatedAt.Add(ttl).Before(now) {
			continue
		}
		f.threads[index].State = "expired"
		expired = append(expired, f.threads[index])
	}
	return expired, nil
}

// ThreadsAll returns a copy of every thread in any state (test accessor for
// expiry/addressed assertions).
func (f *Fake) ThreadsAll() []Thread {
	if f == nil {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Thread(nil), f.threads...)
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

// ForgetPortraitEntries drops the named sub-topics from the stored portrait
// override — the Fake counterpart of Queue.ForgetPortraitEntry for the C3
// delete-on-next-turn tests. Unknown names are ignored.
func (f *Fake) ForgetPortraitEntries(userID string, subTopics ...string) {
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	portrait, ok := f.portraits[userID]
	if !ok {
		return
	}
	dropped := make(map[string]bool, len(subTopics))
	for _, subTopic := range subTopics {
		dropped[subTopic] = true
	}
	entries := make([]PortraitEntry, 0, len(portrait.Entries))
	var updatedAt time.Time
	for _, entry := range portrait.Entries {
		if dropped[entry.SubTopic] {
			continue
		}
		if entry.UpdatedAt.After(updatedAt) {
			updatedAt = entry.UpdatedAt
		}
		entries = append(entries, entry)
	}
	portrait.Entries = entries
	portrait.UpdatedAt = updatedAt
	portrait.Block = RenderPortraitBlock(entries, updatedAt)
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

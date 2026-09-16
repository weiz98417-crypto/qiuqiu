package memory

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"
)

// PortraitOverlay is one local user mutation over the synthesized portrait:
// either an edit (Content set) or a deletion tombstone (Deleted set). The
// overlay is the authority for what the next turn sees — Memobase is only
// synced best-effort, so a forgotten slot stays forgotten even while Memobase
// is unreachable, while its extraction cache repopulates, or after its server
// re-extracts the fact from old blobs.
type PortraitOverlay struct {
	Topic     string
	SubTopic  string
	Content   string
	Deleted   bool
	UpdatedAt time.Time
}

// PortraitOverlayStore persists the local portrait override layer
// (migrations/041 portrait_overlays). Implementations must honor the privacy
// lifecycle: Check returns privacy.ErrDataDeleted / ErrDeletionInProgress
// while a user tombstone exists, and writes must refuse deleted users.
type PortraitOverlayStore interface {
	// Check reports whether the portrait may be shown at all.
	Check(ctx context.Context, userID string) error
	// List returns every overlay row for the user (edits and tombstones).
	List(ctx context.Context, userID string) ([]PortraitOverlay, error)
	// Put records a user edit for one profile slot.
	Put(ctx context.Context, userID, topic, subTopic, content string) (PortraitOverlay, error)
	// Delete tombstones one profile slot.
	Delete(ctx context.Context, userID, topic, subTopic string) error
}

// ResolvePortrait layers the user's overlays over the synthesized entries:
// edits replace content and re-stamp the slot as user-authored, tombstones
// drop slots, and user-created slots (unknown to Memobase) are appended.
// Deterministic (topic, sub_topic) order keeps blocks reproducible in evals.
func ResolvePortrait(entries []PortraitEntry, overlays []PortraitOverlay) []PortraitEntry {
	byKey := make(map[string]PortraitEntry, len(entries)+len(overlays))
	for _, entry := range entries {
		byKey[portraitKey(entry.Topic, entry.SubTopic)] = entry
	}
	for _, overlay := range overlays {
		key := portraitKey(overlay.Topic, overlay.SubTopic)
		if overlay.Deleted {
			delete(byKey, key)
			continue
		}
		byKey[key] = PortraitEntry{
			Topic:     overlay.Topic,
			SubTopic:  overlay.SubTopic,
			Content:   overlay.Content,
			UpdatedAt: overlay.UpdatedAt,
			Source:    PortraitSourceUser,
		}
	}
	merged := make([]PortraitEntry, 0, len(byKey))
	for _, entry := range byKey {
		merged = append(merged, entry)
	}
	sort.Slice(merged, func(left, right int) bool {
		if merged[left].Topic != merged[right].Topic {
			return merged[left].Topic < merged[right].Topic
		}
		return merged[left].SubTopic < merged[right].SubTopic
	})
	return merged
}

func portraitKey(topic, subTopic string) string {
	return topic + "\x00" + subTopic
}

// MemoryPortraitOverlays is the in-process PortraitOverlayStore for tests and
// no-database dev mode, mirroring ThreadStore/Fake (ADR-0006: every local
// store has at least two implementations).
type MemoryPortraitOverlays struct {
	mu   sync.Mutex
	rows map[string]map[string]PortraitOverlay
}

func NewMemoryPortraitOverlays() *MemoryPortraitOverlays {
	return &MemoryPortraitOverlays{rows: make(map[string]map[string]PortraitOverlay)}
}

func (m *MemoryPortraitOverlays) Check(_ context.Context, userID string) error {
	if m == nil {
		return ErrUnavailable
	}
	return nil
}

func (m *MemoryPortraitOverlays) List(_ context.Context, userID string) ([]PortraitOverlay, error) {
	if m == nil {
		return nil, ErrUnavailable
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	overlays := make([]PortraitOverlay, 0, len(m.rows[userID]))
	for _, overlay := range m.rows[userID] {
		overlays = append(overlays, overlay)
	}
	sort.Slice(overlays, func(left, right int) bool {
		return portraitKey(overlays[left].Topic, overlays[left].SubTopic) <
			portraitKey(overlays[right].Topic, overlays[right].SubTopic)
	})
	return overlays, nil
}

func (m *MemoryPortraitOverlays) Put(_ context.Context, userID, topic, subTopic, content string) (PortraitOverlay, error) {
	if m == nil {
		return PortraitOverlay{}, ErrUnavailable
	}
	overlay := PortraitOverlay{
		Topic:     strings.TrimSpace(topic),
		SubTopic:  strings.TrimSpace(subTopic),
		Content:   strings.TrimSpace(content),
		UpdatedAt: time.Now().UTC(),
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	userRows, ok := m.rows[userID]
	if !ok {
		userRows = make(map[string]PortraitOverlay)
		m.rows[userID] = userRows
	}
	userRows[portraitKey(overlay.Topic, overlay.SubTopic)] = overlay
	return overlay, nil
}

func (m *MemoryPortraitOverlays) Delete(_ context.Context, userID, topic, subTopic string) error {
	if m == nil {
		return ErrUnavailable
	}
	overlay := PortraitOverlay{
		Topic:     strings.TrimSpace(topic),
		SubTopic:  strings.TrimSpace(subTopic),
		Deleted:   true,
		UpdatedAt: time.Now().UTC(),
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	userRows, ok := m.rows[userID]
	if !ok {
		userRows = make(map[string]PortraitOverlay)
		m.rows[userID] = userRows
	}
	userRows[portraitKey(overlay.Topic, overlay.SubTopic)] = overlay
	return nil
}

var _ PortraitOverlayStore = (*MemoryPortraitOverlays)(nil)

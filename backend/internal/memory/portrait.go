package memory

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"
)

// PortraitOverlay is one row of the local user portrait authority (migrations
// 041 + 051): either a live slot (Content set), a user deletion tombstone
// (Deleted set), or a historical version closed by the conflict op set
// (ValidTo stamped). The overlay layer is the authority for what the next
// turn sees — Memobase is only synced best-effort, so a forgotten slot stays
// forgotten even while Memobase is unreachable, while its extraction cache
// repopulates, or after its server re-extracts the fact from old blobs.
// ID is the row identity the op set's TargetID points at; ValidFrom/ValidTo
// are the temporal window (NULL/zero = open-ended, existing rows migrated as
// NULL keep their all-time validity).
type PortraitOverlay struct {
	ID        int64
	Topic     string
	SubTopic  string
	Content   string
	Deleted   bool
	ValidFrom time.Time
	ValidTo   time.Time
	UpdatedAt time.Time
}

// inWindow 报告一行在 now 时刻是否处于时间窗内（零值=全域有效的 NULL 语义）。
func (o PortraitOverlay) inWindow(now time.Time) bool {
	if !o.ValidFrom.IsZero() && o.ValidFrom.After(now) {
		return false
	}
	if !o.ValidTo.IsZero() && !o.ValidTo.After(now) {
		return false
	}
	return true
}

// PortraitOverlayStore persists the local portrait override layer
// (migrations/041 + 051 portrait_overlays). Implementations must honor the
// privacy lifecycle: Check returns privacy.ErrDataDeleted / ErrDeletionInProgress
// while a user tombstone exists, and every read/write must refuse deleted
// users before touching rows.
type PortraitOverlayStore interface {
	// Check reports whether the portrait may be shown at all.
	Check(ctx context.Context, userID string) error
	// CurrentPortrait returns the rows inside the temporal window — the
	// "next turn sees this" view. Deletion tombstones stay in the result so
	// the merge layer (ResolvePortrait) keeps masking synthesis slots; the
	// caller filters them exactly like the pre-051 List did.
	CurrentPortrait(ctx context.Context, userID string) ([]PortraitOverlay, error)
	// PortraitHistory returns every row regardless of window or tombstone,
	// oldest version first — the replayable ledger behind "2026-09 之前用户
	// 支持的是 A 队" (explainability/debug; no API surface required).
	PortraitHistory(ctx context.Context, userID string) ([]PortraitOverlay, error)
	// Put records a user edit for one profile slot: live versions of the slot
	// are closed (ValidTo=now) and a fresh row opens, so re-editing a
	// forgotten slot restores it while the old versions stay replayable.
	Put(ctx context.Context, userID, topic, subTopic, content string) (PortraitOverlay, error)
	// Delete tombstones one profile slot: live versions are closed and one
	// open-ended tombstone row is written; nothing is ever hard-deleted.
	Delete(ctx context.Context, userID, topic, subTopic string) error
	// ApplyPortraitOp lands one conflict-op decision deterministically:
	// ADD inserts a fresh row (ValidFrom=now) and requires the slot to be
	// free of open rows; UPDATE closes TargetID and opens the new version in
	// one transaction; DELETE closes TargetID and appends an open tombstone
	// (same semantics as the user-side Delete) in one transaction; NOOP
	// writes nothing.
	ApplyPortraitOp(ctx context.Context, userID string, decision OpDecision, claim PortraitClaim) (PortraitOverlay, error)
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
// store has at least two implementations). Rows are a temporal ledger per
// user, exactly like the Postgres table after migration 051.
type MemoryPortraitOverlays struct {
	mu   sync.Mutex
	rows map[string][]PortraitOverlay
	next int64
	// now 可注入时间源（单测锁时间窗语义）；nil 即真实 UTC 时钟。
	now func() time.Time
}

func NewMemoryPortraitOverlays() *MemoryPortraitOverlays {
	return &MemoryPortraitOverlays{rows: make(map[string][]PortraitOverlay)}
}

func (m *MemoryPortraitOverlays) clock() time.Time {
	if m.now != nil {
		return m.now().UTC()
	}
	return time.Now().UTC()
}

func (m *MemoryPortraitOverlays) Check(_ context.Context, userID string) error {
	if m == nil {
		return ErrUnavailable
	}
	return nil
}

// CurrentPortrait returns the rows inside the temporal window (tombstones
// included, so ResolvePortrait keeps masking synthesis), in stable
// (topic, sub_topic, id) order.
func (m *MemoryPortraitOverlays) CurrentPortrait(_ context.Context, userID string) ([]PortraitOverlay, error) {
	if m == nil {
		return nil, ErrUnavailable
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.clock()
	overlays := make([]PortraitOverlay, 0, len(m.rows[userID]))
	for _, overlay := range m.rows[userID] {
		if overlay.inWindow(now) {
			overlays = append(overlays, overlay)
		}
	}
	sortOverlayRows(overlays)
	return overlays, nil
}

// PortraitHistory returns every row regardless of window or tombstone,
// oldest version first — the replayable local ledger.
func (m *MemoryPortraitOverlays) PortraitHistory(_ context.Context, userID string) ([]PortraitOverlay, error) {
	if m == nil {
		return nil, ErrUnavailable
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	history := append([]PortraitOverlay(nil), m.rows[userID]...)
	sort.Slice(history, func(left, right int) bool {
		return history[left].ID < history[right].ID
	})
	return history, nil
}

// Put records a user edit: live versions of the slot are closed and a fresh
// open-ended row is written, so re-editing a forgotten slot restores it.
func (m *MemoryPortraitOverlays) Put(_ context.Context, userID, topic, subTopic, content string) (PortraitOverlay, error) {
	if m == nil {
		return PortraitOverlay{}, ErrUnavailable
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.clock()
	m.closeLiveRows(userID, topic, subTopic, now)
	overlay := PortraitOverlay{
		ID:        m.nextIDLocked(),
		Topic:     strings.TrimSpace(topic),
		SubTopic:  strings.TrimSpace(subTopic),
		Content:   strings.TrimSpace(content),
		ValidFrom: now,
		UpdatedAt: now,
	}
	m.rows[userID] = append(m.rows[userID], overlay)
	return overlay, nil
}

// Delete tombstones one profile slot: live versions are closed and one
// open-ended tombstone is appended; the rows stay (never hard-deleted) so
// Memobase re-extraction can never resurrect the fact into a prompt.
func (m *MemoryPortraitOverlays) Delete(_ context.Context, userID, topic, subTopic string) error {
	if m == nil {
		return ErrUnavailable
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.clock()
	m.closeLiveRows(userID, topic, subTopic, now)
	m.rows[userID] = append(m.rows[userID], PortraitOverlay{
		ID:        m.nextIDLocked(),
		Topic:     strings.TrimSpace(topic),
		SubTopic:  strings.TrimSpace(subTopic),
		Deleted:   true,
		UpdatedAt: now,
	})
	return nil
}

// ApplyPortraitOp lands one op decision with the same determinism as the SQL
// path: ADD opens a fresh row (the slot must carry no open row — 同槽开放行
// 检查，migration 052 部分唯一索引的内存侧前置), UPDATE closes the target
// and opens the new version, DELETE closes the target and appends an open
// tombstone, NOOP writes nothing.
func (m *MemoryPortraitOverlays) ApplyPortraitOp(_ context.Context, userID string, decision OpDecision, claim PortraitClaim) (PortraitOverlay, error) {
	if m == nil {
		return PortraitOverlay{}, ErrUnavailable
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.clock()
	switch decision.Op {
	case PortraitOpNoop:
		return PortraitOverlay{}, nil
	case PortraitOpAdd:
		// 同槽开放行检查：判定器把 UPDATE 误判成 ADD（或对已遗忘槽位盲目
		// ADD）在这里显式报错——不静默转语义、不并出第二条开放行（Postgres
		// 侧同语义由 migration 052 的部分唯一索引兜底）。
		if m.slotOccupiedLocked(userID, claim.Topic, claim.SubTopic) {
			return PortraitOverlay{}, ErrSlotOccupied
		}
		overlay := claim.overlay(m.nextIDLocked(), now)
		m.rows[userID] = append(m.rows[userID], overlay)
		return overlay, nil
	case PortraitOpUpdate:
		if err := m.closeLocked(userID, decision.TargetID, now); err != nil {
			return PortraitOverlay{}, err
		}
		overlay := claim.overlay(m.nextIDLocked(), now)
		m.rows[userID] = append(m.rows[userID], overlay)
		return overlay, nil
	case PortraitOpDelete:
		// 封口 + 开放墓碑两行（同用户侧 Delete 语义）：目标行封口后追加
		// 一条 deleted 墓碑，ResolvePortrait 靠它遮蔽未来同槽重提取。
		target := m.findLocked(userID, decision.TargetID)
		if target == nil {
			return PortraitOverlay{}, ErrNotFound
		}
		if err := m.closeLocked(userID, decision.TargetID, now); err != nil {
			return PortraitOverlay{}, err
		}
		tombstone := PortraitOverlay{
			ID:        m.nextIDLocked(),
			Topic:     target.Topic,
			SubTopic:  target.SubTopic,
			Deleted:   true,
			UpdatedAt: now,
		}
		m.rows[userID] = append(m.rows[userID], tombstone)
		return tombstone, nil
	default:
		return PortraitOverlay{}, ErrNotFound
	}
}

// findLocked returns a pointer to the live row with the given id, or nil.
func (m *MemoryPortraitOverlays) findLocked(userID string, id int64) *PortraitOverlay {
	for index := range m.rows[userID] {
		row := &m.rows[userID][index]
		if row.ID == id {
			return row
		}
	}
	return nil
}

// slotOccupiedLocked reports whether the slot still carries any open row
// （含墓碑行——遗忘槽位不因盲目 ADD 复活，与部分唯一索引同口径）。
func (m *MemoryPortraitOverlays) slotOccupiedLocked(userID, topic, subTopic string) bool {
	for _, row := range m.rows[userID] {
		if row.Topic == topic && row.SubTopic == subTopic && row.ValidTo.IsZero() {
			return true
		}
	}
	return false
}

// closeLocked stamps ValidTo on one live row (墓碑保留：行永不移除)。Targeting
// an unknown, already-closed, or foreign row is ErrNotFound — the caller must
// notice that the target vanished instead of silently writing a duplicate.
func (m *MemoryPortraitOverlays) closeLocked(userID string, id int64, now time.Time) error {
	for index := range m.rows[userID] {
		row := &m.rows[userID][index]
		if row.ID == id {
			if !row.ValidTo.IsZero() {
				return ErrNotFound
			}
			row.ValidTo = now
			row.UpdatedAt = now
			return nil
		}
	}
	return ErrNotFound
}

// closeLiveRows closes every open-ended version of one slot (Put/Delete 的
// 关旧步）；已封口的历史行不动。
func (m *MemoryPortraitOverlays) closeLiveRows(userID, topic, subTopic string, now time.Time) {
	for index := range m.rows[userID] {
		row := &m.rows[userID][index]
		if row.Topic == topic && row.SubTopic == subTopic && row.ValidTo.IsZero() {
			row.ValidTo = now
			row.UpdatedAt = now
		}
	}
}

func (m *MemoryPortraitOverlays) nextIDLocked() int64 {
	m.next++
	return m.next
}

func (c PortraitClaim) overlay(id int64, now time.Time) PortraitOverlay {
	return PortraitOverlay{
		ID:        id,
		Topic:     c.Topic,
		SubTopic:  c.SubTopic,
		Content:   c.Content,
		ValidFrom: now,
		UpdatedAt: now,
	}
}

// sortOverlayRows keeps the (topic, sub_topic, id) read order deterministic,
// matching the Postgres ORDER BY.
func sortOverlayRows(overlays []PortraitOverlay) {
	sort.Slice(overlays, func(left, right int) bool {
		if overlays[left].Topic != overlays[right].Topic {
			return overlays[left].Topic < overlays[right].Topic
		}
		if overlays[left].SubTopic != overlays[right].SubTopic {
			return overlays[left].SubTopic < overlays[right].SubTopic
		}
		return overlays[left].ID < overlays[right].ID
	})
}

var _ PortraitOverlayStore = (*MemoryPortraitOverlays)(nil)

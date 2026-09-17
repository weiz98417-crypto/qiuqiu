package memory

import (
	"context"
	"time"
)

// healthAuditTail bounds how many recent extraction decisions Health()
// exposes (ADR-0008 design: "audit tail", last 5 rows).
const healthAuditTail = 5

// AuditRow is one memory extraction decision tail row for read-only health
// views. It mirrors ExtractionAudit without the importance/sequence detail
// the console has no use for.
type AuditRow struct {
	MomentID   string
	UserID     string
	Kind       MomentKind
	ReasonCode string
	Detail     string
	CreatedAt  time.Time
}

// Health is the console's read-only memory health snapshot (ADR-0008 overview
// memory cell): degraded reports whether the extraction pipeline is falling
// behind — the Memobase adapter is unconfigured (Ledger-only recall) or
// moments are parked in the outage backlog — backlogDepth is the number of
// moments currently waiting for Memobase, and recentAudit is the last few
// extraction decisions recorded by this process, oldest first.
func (q *Queue) Health() (degraded bool, backlogDepth int, recentAudit []AuditRow) {
	if q == nil {
		return true, 0, nil
	}
	backlogDepth = int(q.backlogPending.Load())
	degraded = !q.adapter.Configured() || backlogDepth > 0
	q.healthMu.Lock()
	recentAudit = append([]AuditRow(nil), q.auditTail...)
	q.healthMu.Unlock()
	return degraded, backlogDepth, recentAudit
}

// ListThreads is the console's cross-user thread read (ADR-0008): userID or
// state may be empty to widen the filter ("all users" / "all states").
// Threads come back oldest first.
func (q *Queue) ListThreads(ctx context.Context, userID, state string) ([]Thread, error) {
	if q == nil || q.threads == nil {
		return nil, ErrNotSupported
	}
	return q.threads.ListThreads(ctx, userID, state)
}

// Thread resolves one thread in any state (ThreadStore seam); ThreadByID is
// the console-facing alias.
func (q *Queue) Thread(ctx context.Context, threadID string) (Thread, bool) {
	return q.ThreadByID(ctx, threadID)
}

// ThreadByID resolves one thread in any state (console PATCH returns the
// updated row).
func (q *Queue) ThreadByID(ctx context.Context, threadID string) (Thread, bool) {
	if q == nil || q.threads == nil {
		return Thread{}, false
	}
	return q.threads.Thread(ctx, threadID)
}

// ExpireThread flips one open thread to 'expired' on behalf of an operator
// (console expire action) and returns the updated row.
func (q *Queue) ExpireThread(ctx context.Context, threadID string) (Thread, error) {
	if q == nil || q.threads == nil {
		return Thread{}, ErrNotSupported
	}
	return q.threads.ExpireThread(ctx, threadID)
}

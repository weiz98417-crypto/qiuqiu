package memory

import "context"

// Memories is the seam from ADR-0006. Implementations: the Memobase adapter
// (production), the in-process queue that fronts it (async write path), and
// the in-memory fake (tests, no-DB dev mode).
type Memories interface {
	// Observe enqueues one observation. Implementations must never block a
	// watch turn for extraction and must never fail the turn because of it.
	Observe(ctx context.Context, moment Moment) error
	// Recall returns the recency x importance x relevance top-k. A degraded
	// adapter returns an empty slice so callers fall back to read_recent.
	Recall(ctx context.Context, query Query) []Recall
	// Portrait returns the synthesized user model for prompt injection.
	Portrait(ctx context.Context, userID string) (Portrait, error)
	// Threads exposes the open-thread ledger; ErrNotSupported is allowed
	// until the local C2 store lands.
	Threads(ctx context.Context, userID string) ([]Thread, error)
}

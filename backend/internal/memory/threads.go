package memory

import (
	"context"
	"errors"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"qiuqiu/internal/privacy"
)

// DefaultThreadTTL bounds how long a thread stays open without being
// addressed (C2 idle beat). Expiry is a distinct terminal state from
// addressed: expired loops aged out unanswered while addressed loops were
// actually closed by a reply, and the difference stays visible in the audit.
const DefaultThreadTTL = 7 * 24 * time.Hour

// Thread lifecycle audit reason codes persisted to memory_extraction_audit
// so every state transition stays auditable (expiry is never silent).
const (
	ReasonThreadOpened    = "thread_opened"
	ReasonThreadAddressed = "thread_addressed"
	ReasonThreadExpired   = "thread_expired"
)

// ThreadStore is the C2 open-thread ledger: local Postgres-backed CRUD that
// the Queue fronts and the Fake mirrors for tests and no-database dev mode.
type ThreadStore interface {
	// AppendThread inserts one open-thread candidate and returns it with its
	// ledger id and timestamps filled in.
	AppendThread(ctx context.Context, thread Thread) (Thread, error)
	// OpenThreads lists the user's threads in state 'open', oldest first.
	OpenThreads(ctx context.Context, userID string) ([]Thread, error)
	// MarkThreadAddressed flips one open thread to 'addressed'; addressed
	// means the loop was actually closed by a reply, not aged out.
	MarkThreadAddressed(ctx context.Context, threadID string) error
	// ExpireStaleThreads flips open threads older than the TTL to 'expired'
	// and returns them so the caller can audit the transition.
	ExpireStaleThreads(ctx context.Context, now time.Time, ttl time.Duration) ([]Thread, error)
}

// PostgresThreads persists the open_threads table from
// migrations/040_open_threads.sql (applied by the store migration runner)
// and writes thread lifecycle rows into memory_extraction_audit.
type PostgresThreads struct {
	pool *pgxpool.Pool
}

// OpenThreadStore connects the thread store on the existing database URL.
func OpenThreadStore(ctx context.Context, databaseURL string) (*PostgresThreads, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &PostgresThreads{pool: pool}, nil
}

func (t *PostgresThreads) Close() {
	if t != nil && t.pool != nil {
		t.pool.Close()
	}
}

func (t *PostgresThreads) AppendThread(ctx context.Context, thread Thread) (Thread, error) {
	if t == nil || t.pool == nil {
		return Thread{}, ErrUnavailable
	}
	if err := privacy.CheckDeletion(ctx, t.pool, thread.UserID); err != nil {
		return Thread{}, err
	}
	var id int64
	var createdAt time.Time
	err := t.pool.QueryRow(ctx, `
		INSERT INTO open_threads (user_id, kind, content, state, source_turn, ledger_sequence)
		VALUES ($1, $2, $3, 'open', $4, $5)
		RETURNING id, created_at
	`, thread.UserID, string(thread.Kind), thread.Content, thread.SourceTurn, thread.LedgerSequence).Scan(&id, &createdAt)
	if err != nil {
		return Thread{}, err
	}
	thread.ID = formatThreadID(id)
	thread.State = "open"
	thread.CreatedAt = createdAt.UTC()
	t.recordLifecycle(ctx, thread, ReasonThreadOpened)
	return thread, nil
}

func (t *PostgresThreads) OpenThreads(ctx context.Context, userID string) ([]Thread, error) {
	if t == nil || t.pool == nil {
		return nil, ErrUnavailable
	}
	if err := privacy.CheckDeletion(ctx, t.pool, userID); err != nil {
		return nil, err
	}
	rows, err := t.pool.Query(ctx, `
		SELECT id, user_id, kind, content, state, source_turn, ledger_sequence, created_at
		FROM open_threads
		WHERE user_id = $1 AND state = 'open'
		ORDER BY id
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	threads := make([]Thread, 0, 8)
	for rows.Next() {
		var thread Thread
		var id int64
		var kind string
		if err := rows.Scan(&id, &thread.UserID, &kind, &thread.Content, &thread.State, &thread.SourceTurn, &thread.LedgerSequence, &thread.CreatedAt); err != nil {
			return nil, err
		}
		thread.ID = formatThreadID(id)
		thread.Kind = ThreadKind(kind)
		threads = append(threads, thread)
	}
	return threads, rows.Err()
}

func (t *PostgresThreads) MarkThreadAddressed(ctx context.Context, threadID string) error {
	if t == nil || t.pool == nil {
		return ErrUnavailable
	}
	id, err := parseThreadID(threadID)
	if err != nil {
		return err
	}
	var userID, kind string
	var ledgerSequence int64
	err = t.pool.QueryRow(ctx, `
		UPDATE open_threads
		SET state = 'addressed', updated_at = now()
		WHERE id = $1 AND state = 'open'
		RETURNING user_id, kind, ledger_sequence
	`, id).Scan(&userID, &kind, &ledgerSequence)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Unknown or already-closed threads stay untouched (mirrors the
			// fake's no-op guard) so callers can address idempotently.
			return nil
		}
		return err
	}
	if err := privacy.CheckDeletion(ctx, t.pool, userID); err != nil {
		return err
	}
	t.recordLifecycle(ctx, Thread{ID: threadID, UserID: userID, Kind: ThreadKind(kind), LedgerSequence: ledgerSequence}, ReasonThreadAddressed)
	return nil
}

func (t *PostgresThreads) ExpireStaleThreads(ctx context.Context, now time.Time, ttl time.Duration) ([]Thread, error) {
	if t == nil || t.pool == nil {
		return nil, ErrUnavailable
	}
	if ttl <= 0 {
		ttl = DefaultThreadTTL
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	rows, err := t.pool.Query(ctx, `
		UPDATE open_threads
		SET state = 'expired', updated_at = $2
		WHERE state = 'open' AND created_at < $1
		RETURNING id, user_id, kind, content, state, source_turn, ledger_sequence, created_at
	`, now.Add(-ttl), now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	expired := make([]Thread, 0, 8)
	for rows.Next() {
		var thread Thread
		var id int64
		var kind string
		if err := rows.Scan(&id, &thread.UserID, &kind, &thread.Content, &thread.State, &thread.SourceTurn, &thread.LedgerSequence, &thread.CreatedAt); err != nil {
			return nil, err
		}
		thread.ID = formatThreadID(id)
		thread.Kind = ThreadKind(kind)
		expired = append(expired, thread)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, thread := range expired {
		t.recordLifecycle(ctx, thread, ReasonThreadExpired)
	}
	return expired, nil
}

// recordLifecycle keeps every thread state transition visible in the
// extraction audit (ADR-0006: the fact-first culture applies to threads too).
func (t *PostgresThreads) recordLifecycle(ctx context.Context, thread Thread, reasonCode string) {
	lifecycleCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, err := t.pool.Exec(lifecycleCtx, `
		INSERT INTO memory_extraction_audit (moment_id, user_id, kind, ledger_sequence, status, reason_code, detail, created_at)
		VALUES ($1, $2, $3, $4, 'recorded', $5, $6, now())
	`, "thread:"+thread.ID, thread.UserID, string(thread.Kind), thread.LedgerSequence, reasonCode, thread.Content)
	if err != nil {
		log.Printf("memory: record thread lifecycle %s for %q: %v", reasonCode, thread.ID, err)
	}
}

func formatThreadID(id int64) string {
	return strconv.FormatInt(id, 10)
}

func parseThreadID(threadID string) (int64, error) {
	return strconv.ParseInt(strings.TrimSpace(threadID), 10, 64)
}

var (
	_ ThreadStore = (*PostgresThreads)(nil)
)

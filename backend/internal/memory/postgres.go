package memory

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"qiuqiu/internal/privacy"
)

// PostgresRecords persists extraction audits, the Memobase outage backlog and
// reflection beats, mirroring the pgxpool idioms of internal/matchstate.
// Tables come from migrations/039_memory_observations.sql (applied by the
// store migration runner).
type PostgresRecords struct{ pool *pgxpool.Pool }

func OpenRecords(ctx context.Context, databaseURL string) (*PostgresRecords, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &PostgresRecords{pool: pool}, nil
}

func (r *PostgresRecords) Close() {
	if r != nil && r.pool != nil {
		r.pool.Close()
	}
}

func (r *PostgresRecords) RecordExtraction(ctx context.Context, entry ExtractionAudit) error {
	if r == nil || r.pool == nil {
		return ErrUnavailable
	}
	if err := privacy.CheckDeletion(ctx, r.pool, entry.UserID); err != nil {
		return err
	}
	createdAt := entry.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO memory_extraction_audit (moment_id, user_id, kind, importance, ledger_sequence, status, reason_code, detail, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, entry.MomentID, entry.UserID, string(entry.Kind), entry.Importance, entry.LedgerSequence, "recorded", entry.ReasonCode, entry.Detail, createdAt)
	return err
}

func (r *PostgresRecords) RecordReflection(ctx context.Context, entry ReflectionAudit) error {
	if r == nil || r.pool == nil {
		return ErrUnavailable
	}
	if err := privacy.CheckDeletion(ctx, r.pool, entry.UserID); err != nil {
		return err
	}
	cited, err := json.Marshal(entry.CitedSequences)
	if err != nil {
		return err
	}
	if cited == nil {
		cited = []byte("[]")
	}
	createdAt := entry.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO memory_reflection_audit (user_id, match_id, trigger_kind, insight, cited_sequences, status, detail, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, entry.UserID, entry.MatchID, entry.Trigger, entry.Insight, cited, entry.Status, entry.Detail, createdAt)
	return err
}

func (r *PostgresRecords) Put(ctx context.Context, entry BacklogEntry) (int64, error) {
	if r == nil || r.pool == nil {
		return 0, ErrUnavailable
	}
	if err := privacy.CheckDeletion(ctx, r.pool, entry.UserID); err != nil {
		return 0, err
	}
	nextAttemptAt := entry.NextAttemptAt
	if nextAttemptAt.IsZero() {
		nextAttemptAt = time.Now().UTC()
	}
	var id int64
	err := r.pool.QueryRow(ctx, `
		INSERT INTO memory_backlog (user_id, payload, attempts, status, next_attempt_at)
		VALUES ($1, $2, $3, 'pending', $4)
		RETURNING id
	`, entry.UserID, entry.Payload, entry.Attempts, nextAttemptAt).Scan(&id)
	return id, err
}

func (r *PostgresRecords) Due(ctx context.Context, limit int) ([]BacklogEntry, error) {
	if r == nil || r.pool == nil {
		return nil, ErrUnavailable
	}
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, user_id, payload, attempts, next_attempt_at
		FROM memory_backlog
		WHERE status = 'pending' AND next_attempt_at <= now()
		ORDER BY id
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := make([]BacklogEntry, 0, limit)
	for rows.Next() {
		var entry BacklogEntry
		if err := rows.Scan(&entry.ID, &entry.UserID, &entry.Payload, &entry.Attempts, &entry.NextAttemptAt); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (r *PostgresRecords) MarkReplayed(ctx context.Context, id int64) error {
	if r == nil || r.pool == nil {
		return ErrUnavailable
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE memory_backlog
		SET status = 'replayed', last_error = '', updated_at = now()
		WHERE id = $1
	`, id)
	return err
}

func (r *PostgresRecords) MarkFailed(ctx context.Context, id int64, attempts int, nextAttemptAt time.Time, lastError string) error {
	if r == nil || r.pool == nil {
		return ErrUnavailable
	}
	status := "pending"
	if attempts >= MaxBacklogAttempts {
		status = "failed"
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE memory_backlog
		SET attempts = $2, next_attempt_at = $3, last_error = $4, status = $5, updated_at = now()
		WHERE id = $1
	`, id, attempts, nextAttemptAt, lastError, status)
	return err
}

var (
	_ AuditSink      = (*PostgresRecords)(nil)
	_ ReflectionSink = (*PostgresRecords)(nil)
	_ BacklogStore   = (*PostgresRecords)(nil)
)

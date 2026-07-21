package privacy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct {
	pool *pgxpool.Pool
}

func OpenPostgresStore(ctx context.Context, databaseURL string) (*PostgresStore, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &PostgresStore{pool: pool}, nil
}

func (s *PostgresStore) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

func (s *PostgresStore) Status(ctx context.Context, userID string) (Status, error) {
	var status Status
	var requestedAt, completedAt *time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT user_id, COALESCE(job_id, ''), status, scope, reason, requested_at, completed_at, error
		FROM privacy_tombstones
		WHERE user_id = $1
	`, userID).Scan(&status.UserID, &status.JobID, &status.Status, &status.Scope, &status.Reason, &requestedAt, &completedAt, &status.Error)
	if errors.Is(err, pgx.ErrNoRows) {
		return Status{UserID: userID, Status: "active", Scope: "all"}, nil
	}
	if err != nil {
		return Status{}, err
	}
	if requestedAt != nil {
		status.RequestedAt = requestedAt.UTC()
	}
	if completedAt != nil {
		status.CompletedAt = completedAt.UTC()
	}
	return status, nil
}

func (s *PostgresStore) StatusByJob(ctx context.Context, jobID string) (Status, error) {
	var status Status
	var requestedAt, completedAt *time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT user_id, COALESCE(job_id, ''), status, scope, reason, requested_at, completed_at, error
		FROM privacy_tombstones
		WHERE job_id = $1
	`, jobID).Scan(&status.UserID, &status.JobID, &status.Status, &status.Scope, &status.Reason, &requestedAt, &completedAt, &status.Error)
	if errors.Is(err, pgx.ErrNoRows) {
		return Status{}, ErrNotFound
	}
	if err != nil {
		return Status{}, err
	}
	if requestedAt != nil {
		status.RequestedAt = requestedAt.UTC()
	}
	if completedAt != nil {
		status.CompletedAt = completedAt.UTC()
	}
	return status, nil
}

func (s *PostgresStore) Export(ctx context.Context, userID string) (Export, error) {
	status, err := s.Status(ctx, userID)
	if err != nil {
		return Export{}, err
	}
	if status.Status == "pending" || status.Status == "failed" {
		return Export{}, ErrDeletionInProgress
	}
	if status.Status == "completed" {
		return Export{}, ErrDataDeleted
	}
	export := Export{UserID: userID, ExportedAt: time.Now().UTC()}
	if export.Sessions, err = queryMaps(ctx, s.pool, `
		SELECT id, user_id, device_id, scopes, created_at, expires_at, revoked_at
		FROM user_sessions WHERE user_id = $1 ORDER BY created_at ASC
	`, userID); err != nil {
		return Export{}, err
	}
	if export.AnonymousIdentities, err = queryMaps(ctx, s.pool, `
		SELECT device_id, user_id, created_at, updated_at, expires_at
		FROM anonymous_device_identities WHERE user_id = $1 ORDER BY created_at ASC
	`, userID); err != nil {
		return Export{}, err
	}
	if export.ConversationTurns, err = queryMaps(ctx, s.pool, `
		SELECT id, trace_id, match_id, user_id, role, text, event_id, created_at
		FROM conversation_turns
		WHERE user_id = $1 AND deleted_at IS NULL AND (expires_at IS NULL OR expires_at > now())
		ORDER BY created_at ASC, id ASC
	`, userID); err != nil {
		return Export{}, err
	}
	if export.AgentTraces, err = queryMaps(ctx, s.pool, `
		SELECT id, match_id, user_id, input, intent, tool_calls, retrieved_event_ids,
			output, reason, latency_ms, error, voice, fact_claim, relationship_decision,
			pending_observation, observation_resolution, created_at
		FROM agent_traces
		WHERE user_id = $1 AND deleted_at IS NULL AND (expires_at IS NULL OR expires_at > now())
		ORDER BY created_at ASC
	`, userID); err != nil {
		return Export{}, err
	}
	if export.RelationshipStates, err = queryMaps(ctx, s.pool, `
		SELECT user_id, state, version, updated_at
		FROM relationship_states WHERE user_id = $1 AND deleted_at IS NULL
	`, userID); err != nil {
		return Export{}, err
	}
	if export.RelationshipMemories, err = queryMaps(ctx, s.pool, `
		SELECT id, user_id, match_id, kind, payload, confidence, source_trace_id, status,
			created_at, last_used_at, expires_at, pending_decision_ids
		FROM relationship_memories
		WHERE user_id = $1 AND deleted_at IS NULL AND (expires_at IS NULL OR expires_at > now())
		ORDER BY created_at ASC
	`, userID); err != nil {
		return Export{}, err
	}
	if export.MatchStates, err = queryMaps(ctx, s.pool, `
		SELECT user_id, match_id, state, version, updated_at
		FROM match_companion_states WHERE user_id = $1 AND deleted_at IS NULL
		ORDER BY match_id ASC
	`, userID); err != nil {
		return Export{}, err
	}
	if export.InteractionDecisions, err = queryMaps(ctx, s.pool, `
		SELECT id, signal_id, user_id, match_id, decision, created_at
		FROM interaction_decisions
		WHERE user_id = $1 AND deleted_at IS NULL AND (expires_at IS NULL OR expires_at > now())
		ORDER BY created_at ASC
	`, userID); err != nil {
		return Export{}, err
	}
	if export.PendingObservations, err = queryMaps(ctx, s.pool, `
		SELECT id, signal_id, trace_id, user_id, match_id, kind, event_type,
			claimed_team, claimed_player, claimed_score_home, claimed_score_away,
			certainty, status, candidate_fact_id, resolved_fact_id, resolved_revision,
			received_at, follow_up_deadline, reconcile_until, resolved_at, resolution_reason
		FROM pending_match_observations
		WHERE user_id = $1
		ORDER BY received_at ASC, id ASC
	`, userID); err != nil {
		return Export{}, err
	}
	return export, nil
}

func (s *PostgresStore) RequestDeletion(ctx context.Context, userID, reason string) (Status, error) {
	if strings.TrimSpace(userID) == "" {
		return Status{}, fmt.Errorf("user id is required")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Status{}, err
	}
	defer tx.Rollback(ctx)
	if err := LockUserTx(ctx, tx, userID); err != nil {
		return Status{}, err
	}
	var existing Status
	var completedAt *time.Time
	err = tx.QueryRow(ctx, `
		SELECT user_id, COALESCE(job_id, ''), status, scope, reason, requested_at, completed_at, error
		FROM privacy_tombstones WHERE user_id = $1
	`, userID).Scan(&existing.UserID, &existing.JobID, &existing.Status, &existing.Scope, &existing.Reason, &existing.RequestedAt, &completedAt, &existing.Error)
	if err == nil && existing.Status == "completed" {
		if completedAt != nil {
			existing.CompletedAt = completedAt.UTC()
		}
		return existing, nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Status{}, err
	}
	now := time.Now().UTC()
	jobID := NewJobID()
	if _, err := tx.Exec(ctx, `
		INSERT INTO privacy_tombstones (user_id, job_id, scope, status, reason, requested_at, updated_at, error)
		VALUES ($1, $2, 'all', 'pending', $3, $4, $4, '')
		ON CONFLICT (user_id) DO UPDATE SET
			job_id = EXCLUDED.job_id, scope = EXCLUDED.scope, status = 'pending', reason = EXCLUDED.reason,
			requested_at = EXCLUDED.requested_at, completed_at = NULL, error = '', updated_at = EXCLUDED.updated_at
	`, userID, jobID, reason, now); err != nil {
		return Status{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Status{}, err
	}
	return Status{UserID: userID, JobID: jobID, Status: "pending", Scope: "all", Reason: reason, RequestedAt: now}, nil
}

func (s *PostgresStore) ProcessDeletion(ctx context.Context, userID string) (err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		originalErr := err
		if rollbackErr := tx.Rollback(ctx); err == nil && rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			err = rollbackErr
		}
		if originalErr != nil {
			failureCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = s.markDeletionFailed(failureCtx, userID, originalErr)
			cancel()
		}
	}()
	if err = LockUserTx(ctx, tx, userID); err != nil {
		return err
	}
	var status string
	if err = tx.QueryRow(ctx, `SELECT status FROM privacy_tombstones WHERE user_id = $1`, userID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if status == "completed" {
		return nil
	}
	for _, query := range []string{
		`DELETE FROM anonymous_device_identities WHERE user_id = $1`,
		`DELETE FROM pending_match_observations WHERE user_id = $1`,
		`DELETE FROM conversation_turns WHERE user_id = $1`,
		`DELETE FROM agent_traces WHERE user_id = $1`,
		`DELETE FROM relationship_memories WHERE user_id = $1`,
		`DELETE FROM relationship_states WHERE user_id = $1`,
		`DELETE FROM match_companion_states WHERE user_id = $1`,
		`DELETE FROM interaction_decisions WHERE user_id = $1`,
		`DELETE FROM user_sessions WHERE user_id = $1`,
	} {
		if _, err = tx.Exec(ctx, query, userID); err != nil {
			return err
		}
	}
	now := time.Now().UTC()
	_, err = tx.Exec(ctx, `
		UPDATE privacy_tombstones
		SET status = 'completed', completed_at = $2, updated_at = $2, error = ''
		WHERE user_id = $1
	`, userID, now)
	if err != nil {
		return err
	}
	err = tx.Commit(ctx)
	return err
}

func (s *PostgresStore) markDeletionFailed(ctx context.Context, userID string, deletionErr error) error {
	errorText := strings.TrimSpace(deletionErr.Error())
	if len(errorText) > 2048 {
		errorText = errorText[:2048]
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE privacy_tombstones
		SET status = 'failed', error = $2, updated_at = now()
		WHERE user_id = $1 AND status = 'pending'
	`, userID, errorText)
	return err
}

func (s *PostgresStore) CleanupExpired(ctx context.Context) error {
	for _, query := range []string{
		`DELETE FROM anonymous_device_identities WHERE expires_at <= now()`,
		`DELETE FROM pending_match_observations WHERE status IN ('confirmed', 'contradicted', 'expired', 'superseded') AND reconcile_until <= now() - interval '10 minutes'`,
		`DELETE FROM conversation_turns WHERE deleted_at IS NOT NULL OR (expires_at IS NOT NULL AND expires_at <= now())`,
		`DELETE FROM agent_traces WHERE deleted_at IS NOT NULL OR (expires_at IS NOT NULL AND expires_at <= now())`,
		`DELETE FROM relationship_memories WHERE deleted_at IS NOT NULL OR (expires_at IS NOT NULL AND expires_at <= now())`,
		`DELETE FROM interaction_decisions WHERE deleted_at IS NOT NULL OR (expires_at IS NOT NULL AND expires_at <= now())`,
		`DELETE FROM user_sessions WHERE expires_at <= now() OR revoked_at IS NOT NULL`,
	} {
		if _, err := s.pool.Exec(ctx, query); err != nil {
			return err
		}
	}
	return nil
}

func LockUserTx(ctx context.Context, tx pgx.Tx, userID string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('privacy:' || $1, 0))`, userID)
	return err
}

func CheckDeletionTx(ctx context.Context, tx pgx.Tx, userID string) error {
	var status string
	err := tx.QueryRow(ctx, `SELECT status FROM privacy_tombstones WHERE user_id = $1`, userID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if status == "completed" {
		return ErrDataDeleted
	}
	if status == "pending" || status == "failed" {
		return ErrDeletionInProgress
	}
	return nil
}

func CheckDeletion(ctx context.Context, pool *pgxpool.Pool, userID string) error {
	var status string
	err := pool.QueryRow(ctx, `SELECT status FROM privacy_tombstones WHERE user_id = $1`, userID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if status == "completed" {
		return ErrDataDeleted
	}
	if status == "pending" || status == "failed" {
		return ErrDeletionInProgress
	}
	return nil
}

func queryMaps(ctx context.Context, pool *pgxpool.Pool, query string, args ...any) ([]map[string]any, error) {
	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	fields := rows.FieldDescriptions()
	result := make([]map[string]any, 0)
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, err
		}
		item := make(map[string]any, len(fields))
		for index, field := range fields {
			item[field.Name] = exportValue(values[index])
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func exportValue(value any) any {
	switch value := value.(type) {
	case []byte:
		var decoded any
		if json.Unmarshal(value, &decoded) == nil {
			return decoded
		}
		return string(value)
	case time.Time:
		return value.UTC().Format(time.RFC3339Nano)
	default:
		return value
	}
}

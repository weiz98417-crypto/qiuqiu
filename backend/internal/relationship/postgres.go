package relationship

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"qiuqiu/internal/privacy"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func OpenPostgresRepository(ctx context.Context, databaseURL string) (*PostgresRepository, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &PostgresRepository{pool: pool}, nil
}

func (r *PostgresRepository) Close() {
	if r != nil && r.pool != nil {
		r.pool.Close()
	}
}

func (r *PostgresRepository) ResetMatch(matchID string) error {
	ctx := context.Background()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM interaction_decisions WHERE match_id = $1`, matchID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM match_companion_states WHERE match_id = $1`, matchID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *PostgresRepository) Load(ctx context.Context, userID, matchID string) (StateBundle, error) {
	if err := privacy.CheckDeletion(ctx, r.pool, userID); err != nil {
		return StateBundle{}, err
	}
	relationshipState := RelationshipState{UserID: userID, Stage: StageFirstMeeting}
	var relationshipJSON []byte
	var relationshipVersion int64
	err := r.pool.QueryRow(ctx, `
		SELECT state, version
		FROM relationship_states
		WHERE user_id = $1
		  AND deleted_at IS NULL
	`, userID).Scan(&relationshipJSON, &relationshipVersion)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return StateBundle{}, err
	}
	if len(relationshipJSON) > 0 {
		if err := json.Unmarshal(relationshipJSON, &relationshipState); err != nil {
			return StateBundle{}, err
		}
	}
	if relationshipState.Stage == "" {
		relationshipState.Stage = StageFirstMeeting
	}
	relationshipState.Version = relationshipVersion

	matchState := MatchCompanionState{UserID: userID, MatchID: matchID}
	var matchJSON []byte
	var matchVersion int64
	err = r.pool.QueryRow(ctx, `
		SELECT state, version
		FROM match_companion_states
		WHERE user_id = $1 AND match_id = $2
		  AND deleted_at IS NULL
	`, userID, matchID).Scan(&matchJSON, &matchVersion)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return StateBundle{}, err
	}
	if len(matchJSON) > 0 {
		if err := json.Unmarshal(matchJSON, &matchState); err != nil {
			return StateBundle{}, err
		}
	}
	matchState.Version = matchVersion

	rows, err := r.pool.Query(ctx, `
		SELECT id, user_id, match_id, kind, payload, confidence, source_trace_id, status,
		       created_at, last_used_at, expires_at, pending_decision_ids
		FROM relationship_memories
		WHERE user_id = $1 AND deleted_at IS NULL
		  AND (expires_at IS NULL OR expires_at > now())
		ORDER BY created_at DESC
		LIMIT 128
	`, userID)
	if err != nil {
		return StateBundle{}, err
	}
	defer rows.Close()
	memories := make([]RelationshipMemory, 0)
	for rows.Next() {
		var memory RelationshipMemory
		if err := rows.Scan(
			&memory.ID, &memory.UserID, &memory.MatchID, &memory.Kind, &memory.Payload,
			&memory.Confidence, &memory.SourceTraceID, &memory.Status, &memory.CreatedAt,
			&memory.LastUsedAt, &memory.ExpiresAt, &memory.PendingDecisionIDs,
		); err != nil {
			return StateBundle{}, err
		}
		memories = append(memories, memory)
	}
	if err := rows.Err(); err != nil {
		return StateBundle{}, err
	}
	return StateBundle{Relationship: relationshipState, Match: matchState, Memories: memories}, nil
}

func (r *PostgresRepository) CompareAndSwap(ctx context.Context, expected ExpectedVersions, update StateUpdate) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, update.Relationship.UserID); err != nil {
		return err
	}
	if err := privacy.LockUserTx(ctx, tx, update.Relationship.UserID); err != nil {
		return err
	}
	if err := privacy.CheckDeletionTx(ctx, tx, update.Relationship.UserID); err != nil {
		return err
	}

	currentRelationship, err := currentVersion(ctx, tx, `SELECT version FROM relationship_states WHERE user_id = $1`, update.Relationship.UserID)
	if err != nil {
		return err
	}
	currentMatch, err := currentVersion(ctx, tx, `SELECT version FROM match_companion_states WHERE user_id = $1 AND match_id = $2`, update.Match.UserID, update.Match.MatchID)
	if err != nil {
		return err
	}
	if currentRelationship != expected.Relationship || currentMatch != expected.Match {
		return ErrConcurrentUpdate
	}

	relationshipJSON, err := json.Marshal(update.Relationship)
	if err != nil {
		return err
	}
	matchJSON, err := json.Marshal(update.Match)
	if err != nil {
		return err
	}
	decisionJSON, err := json.Marshal(update.Decision)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO relationship_states (user_id, state, version, updated_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id) DO UPDATE SET
			state = EXCLUDED.state,
			version = EXCLUDED.version,
			updated_at = EXCLUDED.updated_at
	`, update.Relationship.UserID, relationshipJSON, update.Relationship.Version, update.Relationship.UpdatedAt); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO match_companion_states (user_id, match_id, state, version, updated_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (user_id, match_id) DO UPDATE SET
			state = EXCLUDED.state,
			version = EXCLUDED.version,
			updated_at = EXCLUDED.updated_at
	`, update.Match.UserID, update.Match.MatchID, matchJSON, update.Match.Version, update.Match.UpdatedAt); err != nil {
		return err
	}
	for _, memory := range update.Memories {
		pendingDecisionIDs := memory.PendingDecisionIDs
		if pendingDecisionIDs == nil {
			pendingDecisionIDs = []string{}
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO relationship_memories (
				id, user_id, match_id, kind, payload, confidence, source_trace_id, status,
				created_at, last_used_at, expires_at, pending_decision_ids, updated_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
			ON CONFLICT (id) DO UPDATE SET
				payload = EXCLUDED.payload,
				confidence = EXCLUDED.confidence,
				source_trace_id = EXCLUDED.source_trace_id,
				status = EXCLUDED.status,
				last_used_at = EXCLUDED.last_used_at,
				expires_at = EXCLUDED.expires_at,
				pending_decision_ids = EXCLUDED.pending_decision_ids,
				updated_at = EXCLUDED.updated_at
		`, memory.ID, memory.UserID, memory.MatchID, memory.Kind, memory.Payload, memory.Confidence,
			memory.SourceTraceID, memory.Status, memory.CreatedAt, memory.LastUsedAt, memory.ExpiresAt,
			pendingDecisionIDs, update.Relationship.UpdatedAt); err != nil {
			return err
		}
	}
	tag, err := tx.Exec(ctx, `
		INSERT INTO interaction_decisions (id, signal_id, user_id, match_id, decision, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (user_id, match_id, signal_id) DO NOTHING
	`, update.Decision.ID, update.Decision.SignalID, update.Relationship.UserID, update.Match.MatchID, decisionJSON, update.Decision.CreatedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrConcurrentUpdate
	}
	return tx.Commit(ctx)
}

func (r *PostgresRepository) DecisionBySignal(ctx context.Context, userID, matchID, signalID string) (Decision, bool, error) {
	var payload []byte
	err := r.pool.QueryRow(ctx, `
		SELECT decision
		FROM interaction_decisions
		WHERE user_id = $1 AND match_id = $2 AND signal_id = $3
	`, userID, matchID, signalID).Scan(&payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return Decision{}, false, nil
	}
	if err != nil {
		return Decision{}, false, err
	}
	var decision Decision
	if err := json.Unmarshal(payload, &decision); err != nil {
		return Decision{}, false, err
	}
	return decision, true, nil
}

func currentVersion(ctx context.Context, tx pgx.Tx, query string, args ...any) (int64, error) {
	var version int64
	err := tx.QueryRow(ctx, query, args...).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read relationship state version: %w", err)
	}
	return version, nil
}

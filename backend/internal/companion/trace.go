package companion

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"qiuqiu/internal/observation"
	"qiuqiu/internal/privacy"
	"qiuqiu/internal/relationship"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrTraceNotFound = errors.New("trace not found")
	ErrTraceConflict = errors.New("trace id belongs to a different request")
)

type TraceWriter interface {
	WriteTrace(ctx context.Context, trace Trace) error
}

type TraceUpdater interface {
	UpdateTrace(ctx context.Context, trace Trace) error
}

type TraceReader interface {
	ListTraces(ctx context.Context, matchID string, limit int) ([]Trace, error)
	GetTrace(ctx context.Context, matchID, traceID string) (Trace, error)
}

type ConversationTurnReader interface {
	RecentTurns(ctx context.Context, matchID, userID string, limit int) ([]ConversationTurn, error)
}

type DemoResetter interface {
	Reset(matchID string) error
}

type PostgresTraceWriter struct {
	pool *pgxpool.Pool
}

func OpenPostgresTraceWriter(ctx context.Context, databaseURL string) (*PostgresTraceWriter, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &PostgresTraceWriter{pool: pool}, nil
}

func (w *PostgresTraceWriter) Close() {
	w.pool.Close()
}

func (w *PostgresTraceWriter) Reset(matchID string) error {
	ctx := context.Background()
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM conversation_turns WHERE match_id = $1`, matchID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM agent_traces WHERE match_id = $1`, matchID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (w *PostgresTraceWriter) WriteTrace(ctx context.Context, trace Trace) error {
	toolCalls, err := json.Marshal(trace.ToolCalls)
	if err != nil {
		return err
	}
	voice, err := json.Marshal(trace.Voice)
	if err != nil {
		return err
	}
	schedule, err := json.Marshal(trace.Schedule)
	if err != nil {
		return err
	}
	claim, err := json.Marshal(trace.Claim)
	if err != nil {
		return err
	}
	pendingObservation, err := json.Marshal(trace.Observation)
	if err != nil {
		return err
	}
	observationResolution, err := json.Marshal(trace.ObservationResolution)
	if err != nil {
		return err
	}
	relationshipDecision, err := json.Marshal(trace.RelationshipDecision)
	if err != nil {
		return err
	}
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := privacy.LockUserTx(ctx, tx, trace.UserID); err != nil {
		return err
	}
	if err := privacy.CheckDeletionTx(ctx, tx, trace.UserID); err != nil {
		return err
	}
	createdAt := trace.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	expiresAt := privacy.RetentionExpiry(createdAt)

	retrievedEvents := trace.RetrievedEvent
	if retrievedEvents == nil {
		retrievedEvents = []string{}
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO matches (id, home_team, away_team, updated_at)
		VALUES ($1, '主队', '客队', now())
		ON CONFLICT (id) DO NOTHING
	`, trace.MatchID); err != nil {
		return err
	}

	tag, err := tx.Exec(ctx, `
		INSERT INTO agent_traces (
			id, match_id, user_id, input, intent, tool_calls,
			retrieved_event_ids, output, reason, latency_ms, error, voice, fact_claim,
			pending_observation, observation_resolution, relationship_decision, created_at, expires_at,
			schedule, lookup_id, parent_trace_id
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21)
		ON CONFLICT (id) DO UPDATE SET
			input = EXCLUDED.input,
			intent = EXCLUDED.intent,
			tool_calls = EXCLUDED.tool_calls,
			retrieved_event_ids = EXCLUDED.retrieved_event_ids,
			output = EXCLUDED.output,
			reason = EXCLUDED.reason,
			latency_ms = EXCLUDED.latency_ms,
			error = EXCLUDED.error,
			voice = EXCLUDED.voice,
			fact_claim = EXCLUDED.fact_claim,
			pending_observation = EXCLUDED.pending_observation,
			observation_resolution = EXCLUDED.observation_resolution,
			relationship_decision = EXCLUDED.relationship_decision,
			schedule = EXCLUDED.schedule,
			lookup_id = EXCLUDED.lookup_id,
			parent_trace_id = EXCLUDED.parent_trace_id,
			expires_at = EXCLUDED.expires_at
		WHERE agent_traces.match_id = EXCLUDED.match_id
			AND agent_traces.user_id = EXCLUDED.user_id
			AND agent_traces.input = EXCLUDED.input
	`, trace.ID, trace.MatchID, trace.UserID, trace.Input, string(trace.Intent), toolCalls,
		retrievedEvents, trace.Output, trace.Reason, trace.LatencyMS, trace.Error, voice, claim,
		pendingObservation, observationResolution, relationshipDecision, createdAt, expiresAt,
		schedule, trace.LookupID, trace.ParentTraceID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrTraceConflict
	}
	if trace.Input != "" {
		if _, err := tx.Exec(ctx, `
			INSERT INTO conversation_turns (trace_id, match_id, user_id, role, text, created_at, expires_at)
			VALUES ($1, $2, $3, 'user', $4, $5, $6)
			ON CONFLICT (trace_id, role) WHERE trace_id <> '' DO UPDATE SET
				text = EXCLUDED.text,
				created_at = EXCLUDED.created_at
		`, trace.ID, trace.MatchID, trace.UserID, trace.Input, createdAt, expiresAt); err != nil {
			return err
		}
	}
	if trace.Output != "" {
		eventID := ""
		if len(trace.RetrievedEvent) > 0 {
			eventID = trace.RetrievedEvent[0]
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO conversation_turns (trace_id, match_id, user_id, role, text, event_id, created_at, expires_at)
			VALUES ($1, $2, $3, 'qiuqiu', $4, NULLIF($5, ''), $6, $7)
			ON CONFLICT (trace_id, role) WHERE trace_id <> '' DO UPDATE SET
				text = EXCLUDED.text,
				event_id = EXCLUDED.event_id,
				created_at = EXCLUDED.created_at
		`, trace.ID, trace.MatchID, trace.UserID, trace.Output, eventID, createdAt, expiresAt); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (w *PostgresTraceWriter) UpdateTrace(ctx context.Context, trace Trace) error {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := privacy.LockUserTx(ctx, tx, trace.UserID); err != nil {
		return err
	}
	if err := privacy.CheckDeletionTx(ctx, tx, trace.UserID); err != nil {
		return err
	}
	toolCalls, err := json.Marshal(trace.ToolCalls)
	if err != nil {
		return err
	}
	voice, err := json.Marshal(trace.Voice)
	if err != nil {
		return err
	}
	schedule, err := json.Marshal(trace.Schedule)
	if err != nil {
		return err
	}
	claim, err := json.Marshal(trace.Claim)
	if err != nil {
		return err
	}
	pendingObservation, err := json.Marshal(trace.Observation)
	if err != nil {
		return err
	}
	observationResolution, err := json.Marshal(trace.ObservationResolution)
	if err != nil {
		return err
	}
	relationshipDecision, err := json.Marshal(trace.RelationshipDecision)
	if err != nil {
		return err
	}
	retrievedEvents := trace.RetrievedEvent
	if retrievedEvents == nil {
		retrievedEvents = []string{}
	}
	_, err = tx.Exec(ctx, `
		UPDATE agent_traces
		SET tool_calls = $3,
			retrieved_event_ids = $4,
			output = $5,
			reason = $6,
			latency_ms = $7,
			error = $8,
			voice = $9,
			fact_claim = $10,
			pending_observation = $11,
			observation_resolution = $12,
			relationship_decision = $13,
			schedule = $14,
			lookup_id = $15,
			parent_trace_id = $16
		WHERE match_id = $1 AND id = $2 AND deleted_at IS NULL
	`, trace.MatchID, trace.ID, toolCalls, retrievedEvents, trace.Output, trace.Reason, trace.LatencyMS, trace.Error,
		voice, claim, pendingObservation, observationResolution, relationshipDecision,
		schedule, trace.LookupID, trace.ParentTraceID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (w *PostgresTraceWriter) ListTraces(ctx context.Context, matchID string, limit int) ([]Trace, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := w.pool.Query(ctx, `
		SELECT id, match_id, user_id, input, intent, tool_calls, retrieved_event_ids,
			output, reason, latency_ms, error, voice, fact_claim, pending_observation,
			observation_resolution, relationship_decision, schedule, lookup_id, parent_trace_id, created_at
		FROM agent_traces
		WHERE match_id = $1 AND deleted_at IS NULL
			AND (expires_at IS NULL OR expires_at > now())
		ORDER BY created_at DESC
		LIMIT $2
	`, matchID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var traces []Trace
	for rows.Next() {
		trace, err := scanTrace(rows)
		if err != nil {
			return nil, err
		}
		traces = append(traces, trace)
	}
	return traces, rows.Err()
}

func (w *PostgresTraceWriter) GetTrace(ctx context.Context, matchID, traceID string) (Trace, error) {
	rows, err := w.pool.Query(ctx, `
		SELECT id, match_id, user_id, input, intent, tool_calls, retrieved_event_ids,
			output, reason, latency_ms, error, voice, fact_claim, pending_observation,
			observation_resolution, relationship_decision, schedule, lookup_id, parent_trace_id, created_at
		FROM agent_traces
		WHERE match_id = $1 AND id = $2 AND deleted_at IS NULL
			AND (expires_at IS NULL OR expires_at > now())
		LIMIT 1
	`, matchID, traceID)
	if err != nil {
		return Trace{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return Trace{}, ErrTraceNotFound
	}
	trace, err := scanTrace(rows)
	if err != nil {
		return Trace{}, err
	}
	return trace, rows.Err()
}

func (w *PostgresTraceWriter) RecentTurns(ctx context.Context, matchID, userID string, limit int) ([]ConversationTurn, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	rows, err := w.pool.Query(ctx, `
		SELECT trace_id, match_id, user_id, role, text, COALESCE(event_id, ''), created_at
		FROM conversation_turns
		WHERE match_id = $1 AND user_id = $2 AND deleted_at IS NULL
			AND (expires_at IS NULL OR expires_at > now())
		ORDER BY created_at DESC, id DESC
		LIMIT $3
	`, matchID, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var turns []ConversationTurn
	for rows.Next() {
		var turn ConversationTurn
		if err := rows.Scan(&turn.TraceID, &turn.MatchID, &turn.UserID, &turn.Role, &turn.Text, &turn.EventID, &turn.CreatedAt); err != nil {
			return nil, err
		}
		turns = append(turns, turn)
	}
	for i, j := 0, len(turns)-1; i < j; i, j = i+1, j-1 {
		turns[i], turns[j] = turns[j], turns[i]
	}
	return turns, rows.Err()
}

func scanTrace(rows pgx.Rows) (Trace, error) {
	var trace Trace
	var intent string
	var toolCalls []byte
	var voice []byte
	var claim []byte
	var pendingObservation []byte
	var observationResolution []byte
	var relationshipDecision []byte
	var schedule []byte
	var createdAt time.Time
	err := rows.Scan(
		&trace.ID,
		&trace.MatchID,
		&trace.UserID,
		&trace.Input,
		&intent,
		&toolCalls,
		&trace.RetrievedEvent,
		&trace.Output,
		&trace.Reason,
		&trace.LatencyMS,
		&trace.Error,
		&voice,
		&claim,
		&pendingObservation,
		&observationResolution,
		&relationshipDecision,
		&schedule,
		&trace.LookupID,
		&trace.ParentTraceID,
		&createdAt,
	)
	if err != nil {
		return Trace{}, err
	}
	if len(toolCalls) > 0 {
		if err := json.Unmarshal(toolCalls, &trace.ToolCalls); err != nil {
			return Trace{}, err
		}
	}
	if len(voice) > 0 && string(voice) != "null" {
		var meta VoiceTraceMetadata
		if err := json.Unmarshal(voice, &meta); err != nil {
			return Trace{}, err
		}
		trace.Voice = &meta
	}
	if len(claim) > 0 && string(claim) != "null" {
		var assessment FactClaim
		if err := json.Unmarshal(claim, &assessment); err != nil {
			return Trace{}, err
		}
		trace.Claim = &assessment
	}
	if len(pendingObservation) > 0 && string(pendingObservation) != "null" {
		var pending observation.PendingObservation
		if err := json.Unmarshal(pendingObservation, &pending); err != nil {
			return Trace{}, err
		}
		trace.Observation = &pending
	}
	if len(observationResolution) > 0 && string(observationResolution) != "null" {
		var resolution observation.Resolution
		if err := json.Unmarshal(observationResolution, &resolution); err != nil {
			return Trace{}, err
		}
		trace.ObservationResolution = &resolution
	}
	if len(relationshipDecision) > 0 && string(relationshipDecision) != "null" {
		var decision relationship.Decision
		if err := json.Unmarshal(relationshipDecision, &decision); err != nil {
			return Trace{}, err
		}
		trace.RelationshipDecision = &decision
	}
	if len(schedule) > 0 && string(schedule) != "null" {
		var intent ScheduleIntent
		if err := json.Unmarshal(schedule, &intent); err != nil {
			return Trace{}, err
		}
		trace.Schedule = &intent
	}
	trace.Intent = Intent(intent)
	trace.CreatedAt = createdAt.UTC()
	return trace, nil
}

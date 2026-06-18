package companion

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrTraceNotFound = errors.New("trace not found")

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

type AsyncTraceWriter struct {
	inner TraceWriter
	ch    chan traceOp
}

type traceOp struct {
	update bool
	trace  Trace
}

func NewAsyncTraceWriter(inner TraceWriter, buffer int) *AsyncTraceWriter {
	if buffer <= 0 {
		buffer = 64
	}
	w := &AsyncTraceWriter{
		inner: inner,
		ch:    make(chan traceOp, buffer),
	}
	go w.run()
	return w
}

func (w *AsyncTraceWriter) WriteTrace(ctx context.Context, trace Trace) error {
	_ = ctx
	select {
	case w.ch <- traceOp{trace: trace}:
	default:
		log.Printf("companion trace dropped: buffer full")
	}
	return nil
}

func (w *AsyncTraceWriter) UpdateTrace(ctx context.Context, trace Trace) error {
	_ = ctx
	select {
	case w.ch <- traceOp{update: true, trace: trace}:
	default:
		log.Printf("companion trace update dropped: buffer full")
	}
	return nil
}

func (w *AsyncTraceWriter) run() {
	for op := range w.ch {
		var err error
		if op.update {
			if updater, ok := w.inner.(TraceUpdater); ok {
				err = updater.UpdateTrace(context.Background(), op.trace)
			} else {
				err = w.inner.WriteTrace(context.Background(), op.trace)
			}
		} else {
			err = w.inner.WriteTrace(context.Background(), op.trace)
		}
		if err != nil {
			log.Printf("companion trace write error: %v", err)
		}
	}
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
	_, err := w.pool.Exec(context.Background(), `
		DELETE FROM conversation_turns WHERE match_id = $1;
		DELETE FROM agent_traces WHERE match_id = $1;
	`, matchID)
	return err
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
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		INSERT INTO matches (id, home_team, away_team, updated_at)
		VALUES ($1, '主队', '客队', now())
		ON CONFLICT (id) DO NOTHING
	`, trace.MatchID); err != nil {
		return err
	}

	if trace.Input != "" {
		if _, err := tx.Exec(ctx, `
			INSERT INTO conversation_turns (match_id, user_id, role, text, created_at)
			VALUES ($1, $2, 'user', $3, $4)
		`, trace.MatchID, trace.UserID, trace.Input, trace.CreatedAt); err != nil {
			return err
		}
	}
	if trace.Output != "" {
		eventID := ""
		if len(trace.RetrievedEvent) > 0 {
			eventID = trace.RetrievedEvent[0]
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO conversation_turns (match_id, user_id, role, text, event_id, created_at)
			VALUES ($1, $2, 'qiuqiu', $3, NULLIF($4, ''), $5)
		`, trace.MatchID, trace.UserID, trace.Output, eventID, trace.CreatedAt); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO agent_traces (
			id, match_id, user_id, input, intent, tool_calls,
			retrieved_event_ids, output, reason, latency_ms, error, voice, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (id) DO NOTHING
	`, trace.ID, trace.MatchID, trace.UserID, trace.Input, string(trace.Intent), toolCalls,
		trace.RetrievedEvent, trace.Output, trace.Reason, trace.LatencyMS, trace.Error, voice, trace.CreatedAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (w *PostgresTraceWriter) UpdateTrace(ctx context.Context, trace Trace) error {
	toolCalls, err := json.Marshal(trace.ToolCalls)
	if err != nil {
		return err
	}
	voice, err := json.Marshal(trace.Voice)
	if err != nil {
		return err
	}
	_, err = w.pool.Exec(ctx, `
		UPDATE agent_traces
		SET tool_calls = $3,
			retrieved_event_ids = $4,
			output = $5,
			reason = $6,
			latency_ms = $7,
			error = $8,
			voice = $9
		WHERE match_id = $1 AND id = $2
	`, trace.MatchID, trace.ID, toolCalls, trace.RetrievedEvent, trace.Output, trace.Reason, trace.LatencyMS, trace.Error, voice)
	return err
}

func (w *PostgresTraceWriter) ListTraces(ctx context.Context, matchID string, limit int) ([]Trace, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := w.pool.Query(ctx, `
		SELECT id, match_id, user_id, input, intent, tool_calls, retrieved_event_ids,
			output, reason, latency_ms, error, voice, created_at
		FROM agent_traces
		WHERE match_id = $1
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
			output, reason, latency_ms, error, voice, created_at
		FROM agent_traces
		WHERE match_id = $1 AND id = $2
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
		SELECT match_id, user_id, role, text, COALESCE(event_id, ''), created_at
		FROM conversation_turns
		WHERE match_id = $1 AND user_id = $2
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
		if err := rows.Scan(&turn.MatchID, &turn.UserID, &turn.Role, &turn.Text, &turn.EventID, &turn.CreatedAt); err != nil {
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
	trace.Intent = Intent(intent)
	trace.CreatedAt = createdAt.UTC()
	return trace, nil
}

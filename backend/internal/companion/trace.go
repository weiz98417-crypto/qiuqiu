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

type TraceReader interface {
	ListTraces(ctx context.Context, matchID string, limit int) ([]Trace, error)
	GetTrace(ctx context.Context, matchID, traceID string) (Trace, error)
}

type AsyncTraceWriter struct {
	inner TraceWriter
	ch    chan Trace
}

func NewAsyncTraceWriter(inner TraceWriter, buffer int) *AsyncTraceWriter {
	if buffer <= 0 {
		buffer = 64
	}
	w := &AsyncTraceWriter{
		inner: inner,
		ch:    make(chan Trace, buffer),
	}
	go w.run()
	return w
}

func (w *AsyncTraceWriter) WriteTrace(ctx context.Context, trace Trace) error {
	_ = ctx
	select {
	case w.ch <- trace:
	default:
		log.Printf("companion trace dropped: buffer full")
	}
	return nil
}

func (w *AsyncTraceWriter) run() {
	for trace := range w.ch {
		if err := w.inner.WriteTrace(context.Background(), trace); err != nil {
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

func (w *PostgresTraceWriter) WriteTrace(ctx context.Context, trace Trace) error {
	toolCalls, err := json.Marshal(trace.ToolCalls)
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
		if _, err := tx.Exec(ctx, `
			INSERT INTO conversation_turns (match_id, user_id, role, text, created_at)
			VALUES ($1, $2, 'qiuqiu', $3, $4)
		`, trace.MatchID, trace.UserID, trace.Output, trace.CreatedAt); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO agent_traces (
			id, match_id, user_id, input, intent, tool_calls,
			retrieved_event_ids, output, reason, latency_ms, error, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (id) DO NOTHING
	`, trace.ID, trace.MatchID, trace.UserID, trace.Input, string(trace.Intent), toolCalls,
		trace.RetrievedEvent, trace.Output, trace.Reason, trace.LatencyMS, trace.Error, trace.CreatedAt); err != nil {
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
			output, reason, latency_ms, error, created_at
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
			output, reason, latency_ms, error, created_at
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

func scanTrace(rows pgx.Rows) (Trace, error) {
	var trace Trace
	var intent string
	var toolCalls []byte
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
	trace.Intent = Intent(intent)
	trace.CreatedAt = createdAt.UTC()
	return trace, nil
}

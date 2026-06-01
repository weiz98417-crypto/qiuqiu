package companion

import (
	"context"
	"encoding/json"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
)

type TraceWriter interface {
	WriteTrace(ctx context.Context, trace Trace) error
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
			retrieved_event_ids, output, reason, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (id) DO NOTHING
	`, trace.ID, trace.MatchID, trace.UserID, trace.Input, string(trace.Intent), toolCalls,
		trace.RetrievedEvent, trace.Output, trace.Reason, trace.CreatedAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

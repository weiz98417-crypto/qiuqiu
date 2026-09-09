package interaction

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"qiuqiu/internal/privacy"
	"qiuqiu/internal/relationship"
)

type PostgresLedger struct{ pool *pgxpool.Pool }

const interactionRankSQL = `CASE
	WHEN kind <> 'playback_result' THEN 0
	WHEN playback_state = 'started' THEN 1
	WHEN playback_state IN ('ended','completed','interrupted','skipped','blocked') THEN 2
	ELSE 3
END`

func OpenPostgresLedger(ctx context.Context, databaseURL string) (*PostgresLedger, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &PostgresLedger{pool: pool}, nil
}

func (l *PostgresLedger) Close() {
	if l != nil && l.pool != nil {
		l.pool.Close()
	}
}

func (l *PostgresLedger) Append(ctx context.Context, event Event) (Event, error) {
	if err := validateEvent(event); err != nil {
		return Event{}, err
	}
	event = canonicalEvent(event)
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	event = canonicalEvent(event)
	if event.ExpiresAt.IsZero() {
		event.ExpiresAt = privacy.RetentionExpiry(event.CreatedAt)
	}
	if event.FactIDs == nil {
		event.FactIDs = []string{}
	}
	if err := privacy.CheckDeletion(ctx, l.pool, event.UserID); err != nil {
		return Event{}, err
	}
	factIDs, err := json.Marshal(event.FactIDs)
	if err != nil {
		return Event{}, err
	}
	decision, err := json.Marshal(event.Decision)
	if err != nil {
		return Event{}, err
	}
	presentation, err := json.Marshal(event.Presentation)
	if err != nil {
		return Event{}, err
	}
	_, err = l.pool.Exec(ctx, `
		INSERT INTO interaction_ledger (id, kind, signal_id, user_id, match_id, trace_id, decision_id, fact_ids, fact_revision, delivery_key, delivery_state, delivery_reason, media_type, playback_state, input_text, output_text, decision, presentation, trace_payload, stale, source, created_at, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23)
		ON CONFLICT (id) DO NOTHING
	`, event.ID, event.Kind, event.SignalID, event.UserID, event.MatchID, event.TraceID, event.DecisionID, factIDs, event.FactRevision, event.DeliveryKey, event.DeliveryState, event.DeliveryReason, event.MediaType, event.PlaybackState, event.InputText, event.OutputText, decision, presentation, event.TracePayload, event.Stale, event.Source, event.CreatedAt, event.ExpiresAt)
	if err != nil {
		return Event{}, err
	}
	existing, err := l.get(ctx, event.ID)
	if err != nil {
		return Event{}, err
	}
	if !sameEvent(existing, event) {
		return Event{}, ErrConflict
	}
	return existing, nil
}

func (l *PostgresLedger) get(ctx context.Context, id string) (Event, error) {
	return getInteractionEvent(ctx, l.pool, id)
}

type interactionRowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func getInteractionEvent(ctx context.Context, querier interactionRowQuerier, id string) (Event, error) {
	var event Event
	var kind string
	var factIDs []byte
	var decision, presentation, tracePayload []byte
	err := querier.QueryRow(ctx, `SELECT id, kind, COALESCE(signal_id,''), user_id, match_id, COALESCE(trace_id,''), COALESCE(decision_id,''), fact_ids, COALESCE(fact_revision,''), COALESCE(delivery_key,''), COALESCE(delivery_state,''), COALESCE(delivery_reason,''), COALESCE(media_type,''), COALESCE(playback_state,''), COALESCE(input_text,''), COALESCE(output_text,''), decision, presentation, trace_payload, stale, COALESCE(source,''), created_at, expires_at FROM interaction_ledger WHERE id = $1`, id).
		Scan(&event.ID, &kind, &event.SignalID, &event.UserID, &event.MatchID, &event.TraceID, &event.DecisionID, &factIDs, &event.FactRevision, &event.DeliveryKey, &event.DeliveryState, &event.DeliveryReason, &event.MediaType, &event.PlaybackState, &event.InputText, &event.OutputText, &decision, &presentation, &tracePayload, &event.Stale, &event.Source, &event.CreatedAt, &event.ExpiresAt)
	if err != nil {
		return Event{}, err
	}
	event.Kind = Kind(kind)
	if err := json.Unmarshal(factIDs, &event.FactIDs); err != nil {
		return Event{}, err
	}
	if len(decision) > 0 && string(decision) != "null" {
		var value relationship.Decision
		if err := json.Unmarshal(decision, &value); err != nil {
			return Event{}, err
		}
		event.Decision = &value
	}
	if len(presentation) > 0 && string(presentation) != "null" {
		var value relationship.PresentationPlan
		if err := json.Unmarshal(presentation, &value); err != nil {
			return Event{}, err
		}
		event.Presentation = &value
	}
	if len(tracePayload) > 0 && string(tracePayload) != "null" {
		event.TracePayload = append(json.RawMessage(nil), tracePayload...)
	}
	return event, nil
}

func (l *PostgresLedger) List(ctx context.Context, userID, matchID string, limit int) ([]Event, error) {
	return l.list(ctx, userID, matchID, limit)
}

func (l *PostgresLedger) ListUser(ctx context.Context, userID string, limit int) ([]Event, error) {
	return l.list(ctx, userID, "", limit)
}

func (l *PostgresLedger) ListPage(ctx context.Context, pageQuery PageQuery) (Page, error) {
	cursor, err := decodeInteractionCursor(pageQuery.Cursor)
	if err != nil {
		return Page{}, err
	}
	limit := normalizePageLimit(pageQuery.Limit)
	query := `SELECT id FROM interaction_ledger WHERE user_id=$1 AND (expires_at IS NULL OR expires_at > now())`
	args := []any{pageQuery.UserID}
	if pageQuery.MatchID != "" {
		args = append(args, pageQuery.MatchID)
		query += fmt.Sprintf(" AND match_id=$%d", len(args))
	}
	if cursor != nil {
		args = append(args, cursor.CreatedAt, cursor.Rank, cursor.ID)
		query += fmt.Sprintf(" AND (created_at, %s, id) > ($%d, $%d, $%d)", interactionRankSQL, len(args)-2, len(args)-1, len(args))
	}
	args = append(args, limit+1)
	query += fmt.Sprintf(" ORDER BY created_at, %s, id LIMIT $%d", interactionRankSQL, len(args))
	rows, err := l.pool.Query(ctx, query, args...)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()
	events := make([]Event, 0, limit+1)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return Page{}, err
		}
		event, err := l.get(ctx, id)
		if err != nil {
			return Page{}, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return Page{}, err
	}
	return interactionPage(events, limit)
}

func (l *PostgresLedger) ListSnapshot(ctx context.Context, userID, matchID string) ([]Event, error) {
	tx, err := l.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	query := `SELECT id FROM interaction_ledger WHERE user_id=$1 AND (expires_at IS NULL OR expires_at > now())`
	args := []any{userID}
	if matchID != "" {
		args = append(args, matchID)
		query += ` AND match_id=$2`
	}
	query += fmt.Sprintf(` ORDER BY created_at, %s, id`, interactionRankSQL)
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	events := make([]Event, 0, len(ids))
	for _, id := range ids {
		event, err := getInteractionEvent(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return events, nil
}

func (l *PostgresLedger) ListMatchSnapshot(ctx context.Context, matchID string) ([]Event, error) {
	tx, err := l.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	query := `SELECT id FROM interaction_ledger WHERE match_id=$1 AND (expires_at IS NULL OR expires_at > now())`
	query += fmt.Sprintf(` ORDER BY created_at, %s, id`, interactionRankSQL)
	rows, err := tx.Query(ctx, query, matchID)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	events := make([]Event, 0, len(ids))
	for _, id := range ids {
		event, err := getInteractionEvent(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return events, nil
}

func (l *PostgresLedger) list(ctx context.Context, userID, matchID string, limit int) ([]Event, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := `SELECT id FROM (SELECT id, created_at, ` + interactionRankSQL + ` AS semantic_rank FROM interaction_ledger WHERE user_id=$1 AND (expires_at IS NULL OR expires_at > now())`
	args := []any{userID}
	if matchID != "" {
		query += ` AND match_id=$2 ORDER BY created_at DESC, semantic_rank DESC, id DESC LIMIT $3) AS latest ORDER BY created_at, semantic_rank, id`
		args = append(args, matchID, limit)
	} else {
		query += ` ORDER BY created_at DESC, semantic_rank DESC, id DESC LIMIT $2) AS latest ORDER BY created_at, semantic_rank, id`
		args = append(args, limit)
	}
	rows, err := l.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []Event
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		event, err := l.get(ctx, id)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

var _ Ledger = (*PostgresLedger)(nil)
var _ PageableLedger = (*PostgresLedger)(nil)
var _ SnapshotLedger = (*PostgresLedger)(nil)
var _ MatchSnapshotLedger = (*PostgresLedger)(nil)

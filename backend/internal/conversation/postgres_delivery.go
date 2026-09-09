package conversation

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"qiuqiu/internal/privacy"
)

type PostgresDeliveryStore struct{ pool *pgxpool.Pool }

func OpenPostgresDeliveryStore(ctx context.Context, databaseURL string) (*PostgresDeliveryStore, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &PostgresDeliveryStore{pool: pool}, nil
}

func (s *PostgresDeliveryStore) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

func (s *PostgresDeliveryStore) For(userID, matchID string) DeliveryLedger {
	return s.ForContext(context.Background(), userID, matchID)
}

func (s *PostgresDeliveryStore) ForContext(ctx context.Context, userID, matchID string) DeliveryLedger {
	if ctx == nil {
		ctx = context.Background()
	}
	return &postgresDeliveryLedger{ctx: ctx, pool: s.pool, userID: userID, matchID: matchID}
}

type postgresDeliveryLedger struct {
	ctx             context.Context
	pool            *pgxpool.Pool
	userID, matchID string
}

func (l *postgresDeliveryLedger) context() context.Context {
	if l != nil && l.ctx != nil {
		return l.ctx
	}
	return context.Background()
}

func (l *postgresDeliveryLedger) Plan(record DeliveryRecord) (DeliveryRecord, error) {
	if record.Key == "" || record.UserID != l.userID || record.MatchID != l.matchID {
		return DeliveryRecord{}, ErrDeliveryConflict
	}
	if record.State == "" {
		record.State = DeliveryPlanned
	}
	if record.State != DeliveryPlanned {
		return DeliveryRecord{}, ErrDeliveryConflict
	}
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = time.Now().UTC()
	}
	if err := privacy.CheckDeletion(l.context(), l.pool, record.UserID); err != nil {
		return DeliveryRecord{}, err
	}
	_, err := l.pool.Exec(l.context(), `INSERT INTO delivery_ledger(delivery_key,client_delivery_key,trace_id,match_id,user_id,state,critical,expires_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(delivery_key) DO NOTHING`, record.Key, record.DeliveryKey, record.TraceID, record.MatchID, record.UserID, record.State, record.Critical, nullableTime(record.ExpiresAt), record.UpdatedAt)
	if err != nil {
		if existing, ok := l.FindByDeliveryKey(record.DeliveryKey); ok && existing.Key != record.Key {
			return existing, ErrDeliveryConflict
		}
		return DeliveryRecord{}, err
	}
	existing, ok := l.Get(record.Key)
	if !ok {
		return DeliveryRecord{}, ErrDeliveryNotFound
	}
	if existing.TraceID != record.TraceID || existing.DeliveryKey != record.DeliveryKey || existing.UserID != record.UserID || existing.MatchID != record.MatchID {
		return DeliveryRecord{}, ErrDeliveryConflict
	}
	return existing, nil
}

func (l *postgresDeliveryLedger) Transition(key string, target DeliveryState, at time.Time) (DeliveryRecord, error) {
	record, ok := l.Get(key)
	if !ok {
		return DeliveryRecord{}, ErrDeliveryNotFound
	}
	if record.State == target {
		return record, nil
	}
	if isTerminalDeliveryState(record.State) || !allowedDeliveryTransition(record.State, target) {
		return DeliveryRecord{}, ErrDeliveryConflict
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	tag, err := l.pool.Exec(l.context(), `UPDATE delivery_ledger SET state=$1, updated_at=$2 WHERE delivery_key=$3 AND user_id=$4 AND match_id=$5 AND state=$6`, target, at.UTC(), key, l.userID, l.matchID, record.State)
	if err != nil {
		return DeliveryRecord{}, err
	}
	if tag.RowsAffected() == 0 {
		return DeliveryRecord{}, ErrDeliveryConflict
	}
	return l.GetRequired(key)
}

func (l *postgresDeliveryLedger) AcknowledgeText(key string, at time.Time) (DeliveryRecord, error) {
	if key == "" {
		return DeliveryRecord{}, ErrDeliveryConflict
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	tag, err := l.pool.Exec(l.context(), `UPDATE delivery_ledger SET text_acknowledged=TRUE, updated_at=$1 WHERE delivery_key=$2 AND user_id=$3 AND match_id=$4`, at.UTC(), key, l.userID, l.matchID)
	if err != nil {
		return DeliveryRecord{}, err
	}
	if tag.RowsAffected() == 0 {
		return DeliveryRecord{}, ErrDeliveryNotFound
	}
	return l.GetRequired(key)
}

func (l *postgresDeliveryLedger) Get(key string) (DeliveryRecord, bool) {
	record, err := l.GetRequired(key)
	return record, err == nil
}

func (l *postgresDeliveryLedger) FindByDeliveryKey(deliveryKey string) (DeliveryRecord, bool) {
	if deliveryKey == "" {
		return DeliveryRecord{}, false
	}
	record, err := l.scanRecord(l.pool.QueryRow(l.context(), `SELECT delivery_key,COALESCE(client_delivery_key,''),COALESCE(trace_id,''),match_id,user_id,state,critical,text_acknowledged,expires_at,updated_at FROM delivery_ledger WHERE client_delivery_key=$1 AND user_id=$2 AND match_id=$3`, deliveryKey, l.userID, l.matchID))
	return record, err == nil
}

func (l *postgresDeliveryLedger) GetRequired(key string) (DeliveryRecord, error) {
	return l.scanRecord(l.pool.QueryRow(l.context(), `SELECT delivery_key,COALESCE(client_delivery_key,''),COALESCE(trace_id,''),match_id,user_id,state,critical,text_acknowledged,expires_at,updated_at FROM delivery_ledger WHERE delivery_key=$1 AND user_id=$2 AND match_id=$3`, key, l.userID, l.matchID))
}

type deliveryRecordScanner interface {
	Scan(...any) error
}

func (l *postgresDeliveryLedger) scanRecord(scanner deliveryRecordScanner) (DeliveryRecord, error) {
	var r DeliveryRecord
	var expires *time.Time
	err := scanner.Scan(&r.Key, &r.DeliveryKey, &r.TraceID, &r.MatchID, &r.UserID, &r.State, &r.Critical, &r.TextAcknowledged, &expires, &r.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return DeliveryRecord{}, ErrDeliveryNotFound
		}
		return DeliveryRecord{}, err
	}
	if expires != nil {
		r.ExpiresAt = expires.UTC()
	}
	return r, nil
}

func (l *postgresDeliveryLedger) Pending(now time.Time) []DeliveryRecord {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	rows, err := l.pool.Query(l.context(), `SELECT delivery_key,COALESCE(client_delivery_key,''),COALESCE(trace_id,''),match_id,user_id,state,critical,text_acknowledged,expires_at,updated_at FROM delivery_ledger WHERE user_id=$1 AND match_id=$2 AND state NOT IN ('completed','interrupted','skipped','failed') AND (expires_at IS NULL OR expires_at>$3) ORDER BY updated_at`, l.userID, l.matchID, now)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []DeliveryRecord
	for rows.Next() {
		var r DeliveryRecord
		var expires *time.Time
		if rows.Scan(&r.Key, &r.DeliveryKey, &r.TraceID, &r.MatchID, &r.UserID, &r.State, &r.Critical, &r.TextAcknowledged, &expires, &r.UpdatedAt) == nil {
			if expires != nil {
				r.ExpiresAt = expires.UTC()
			}
			out = append(out, r)
		}
	}
	return out
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

var _ DeliveryLedger = (*postgresDeliveryLedger)(nil)

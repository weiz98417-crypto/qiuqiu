package knowledge

// 知识条目的 Postgres 实现（migration 053 knowledge_entries）。schema 迁移
// 由 main 装配序里先行的 matchstate 迁移器统一执行（同一条 schema_migrations
// 链），本包只连接不迁移——与 privacy/proactive 等后到 store 同一口径。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

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

const entryColumns = `id, topics, answer, source, confidence, effective_at, triggers, quote, created_by, created_at, updated_at`

func scanRecord(row pgx.Row) (Record, error) {
	var record Record
	var topicsJSON, triggersJSON []byte
	err := row.Scan(
		&record.ID, &topicsJSON, &record.Answer, &record.Source, &record.Confidence,
		&record.EffectiveAt, &triggersJSON, &record.Quote, &record.CreatedBy,
		&record.CreatedAt, &record.UpdatedAt,
	)
	if err != nil {
		return Record{}, err
	}
	if err := json.Unmarshal(topicsJSON, &record.Topics); err != nil {
		return Record{}, fmt.Errorf("decode knowledge topics %q: %w", record.ID, err)
	}
	if err := json.Unmarshal(triggersJSON, &record.Triggers); err != nil {
		return Record{}, fmt.Errorf("decode knowledge triggers %q: %w", record.ID, err)
	}
	record.EffectiveAt = record.EffectiveAt.UTC()
	record.CreatedAt = record.CreatedAt.UTC()
	record.UpdatedAt = record.UpdatedAt.UTC()
	return record, nil
}

func (s *PostgresStore) List(ctx context.Context) ([]Record, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+entryColumns+` FROM knowledge_entries ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := make([]Record, 0)
	for rows.Next() {
		record, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (s *PostgresStore) Get(ctx context.Context, id string) (Record, error) {
	record, err := scanRecord(s.pool.QueryRow(ctx,
		`SELECT `+entryColumns+` FROM knowledge_entries WHERE id = $1`, strings.TrimSpace(id)))
	if errors.Is(err, pgx.ErrNoRows) {
		return Record{}, ErrNotFound
	}
	if err != nil {
		return Record{}, err
	}
	return record, nil
}

// Put 是保存即生效的 upsert：新条目记 created_by/created_at，已有条目只
// 更新内容字段并推进 updated_at（首建留痕保留）。幂等 seed 走 SeedDir 的
// 先查后插，不经过本路径覆盖运营编辑。
func (s *PostgresStore) Put(ctx context.Context, entry Entry, operator string) (Record, error) {
	entry, err := preparePut(entry)
	if err != nil {
		return Record{}, err
	}
	topicsJSON, err := json.Marshal(entry.Topics)
	if err != nil {
		return Record{}, err
	}
	triggersJSON, err := json.Marshal(entry.Triggers)
	if err != nil {
		return Record{}, err
	}
	record, err := scanRecord(s.pool.QueryRow(ctx, `
		INSERT INTO knowledge_entries (`+entryColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, now(), now())
		ON CONFLICT (id) DO UPDATE SET
			topics = EXCLUDED.topics,
			answer = EXCLUDED.answer,
			source = EXCLUDED.source,
			confidence = EXCLUDED.confidence,
			effective_at = EXCLUDED.effective_at,
			triggers = EXCLUDED.triggers,
			quote = EXCLUDED.quote,
			updated_at = now()
		RETURNING `+entryColumns+`
	`, entry.ID, topicsJSON, entry.Answer, entry.Source, entry.Confidence,
		entry.EffectiveAt, triggersJSON, entry.Quote, strings.TrimSpace(operator)))
	if err != nil {
		return Record{}, err
	}
	return record, nil
}

var _ Store = (*PostgresStore)(nil)

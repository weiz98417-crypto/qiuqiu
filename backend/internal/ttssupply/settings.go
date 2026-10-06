// Package ttssupply 承载语音供给三态的持久化（tts-supply-switch 7.3）：
// ops_settings 表的单行单值（key='tts_supply'）。设置是部署级事实——
// 重启后保持运营者上次的选择；读取宽容（未知值/缺行回默认 cloud）。
package ttssupply

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"qiuqiu/internal/tts"
)

// SettingKey 是语音供给在 ops_settings 里的行键。
const SettingKey = "tts_supply"

// Store 是三态设置的持久化面。PG 版是生产实现；无库部署（开发）用
// MemoryStore——重启回默认态，与「无库无运营台」的部署形态一致。
type Store interface {
	Load(ctx context.Context) (string, error)
	Save(ctx context.Context, mode, operator string) error
}

// MemoryStore 是进程内实现（测试与无库部署）。
type MemoryStore struct {
	mode     string
	operator string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{mode: tts.SupplyModeCloud}
}

func (s *MemoryStore) Load(ctx context.Context) (string, error) {
	if s == nil {
		return tts.SupplyModeCloud, nil
	}
	return s.mode, nil
}

func (s *MemoryStore) Save(ctx context.Context, mode, operator string) error {
	if s == nil {
		return nil
	}
	s.mode = tts.NormalizeSupplyMode(mode)
	s.operator = operator
	return nil
}

// PostgresStore 是 ops_settings 表实现。schema 由 matchstate 迁移器统一
// 执行（migration 055），本包只连接不迁移——与 knowledge 等后到 store
// 同一口径。
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

// Load 读当前三态；缺行/未知值回默认 cloud（读取宽容）。
func (s *PostgresStore) Load(ctx context.Context) (string, error) {
	if s == nil {
		return tts.SupplyModeCloud, nil
	}
	var value string
	err := s.pool.QueryRow(ctx, `SELECT value FROM ops_settings WHERE key = $1`, SettingKey).Scan(&value)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return tts.SupplyModeCloud, nil
		}
		return "", err
	}
	return tts.NormalizeSupplyMode(value), nil
}

// Save upsert 当前三态与运营者归属（审计行另走 appendAudit，这里存的是
// 「谁最后改的」这一行内事实）。
func (s *PostgresStore) Save(ctx context.Context, mode, operator string) error {
	if s == nil {
		return nil
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO ops_settings (key, value, updated_by, updated_at)
		 VALUES ($1, $2, $3, now())
		 ON CONFLICT (key) DO UPDATE SET value = $2, updated_by = $3, updated_at = now()`,
		SettingKey, tts.NormalizeSupplyMode(mode), strings.TrimSpace(operator))
	return err
}

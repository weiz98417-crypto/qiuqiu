package journal

// Postgres store（migration 056 journal_entries）。schema 由 matchstate
// 迁移器统一执行，本包只连接不迁移——与 knowledge/privacy 等后到 store
// 同一口径。

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore 是 journal_entries 表实现。
type PostgresStore struct {
	pool *pgxpool.Pool
}

// OpenPostgresStore 连接并 ping。
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

// Close 关闭连接池。
func (s *PostgresStore) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

const entryColumns = `id, user_id, match_id, home_team, away_team, score, goals, body, season, liked, sources, created_at`

func scanEntry(row pgx.Row) (Entry, error) {
	var entry Entry
	var goals, sources []byte
	err := row.Scan(
		&entry.ID, &entry.UserID, &entry.MatchID, &entry.HomeTeam, &entry.AwayTeam,
		&entry.Score, &goals, &entry.Body, &entry.Season, &entry.Liked, &sources, &entry.CreatedAt,
	)
	if err != nil {
		return Entry{}, err
	}
	entry.Goals = unmarshalStringList(goals)
	entry.Sources = unmarshalStringList(sources)
	return entry, nil
}

// Put 幂等写入（同 user+match 更新正文/快照，liked 保留用户态）。
func (s *PostgresStore) Put(ctx context.Context, entry Entry) error {
	liked := false
	if previous, err := s.Get(ctx, entry.UserID, entry.ID); err == nil {
		liked = previous.Liked
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO journal_entries (id, user_id, match_id, home_team, away_team, score, goals, body, season, liked, sources, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		 ON CONFLICT (user_id, match_id) DO UPDATE SET
		   home_team=$4, away_team=$5, score=$6, goals=$7, body=$8, season=$9, sources=$11`,
		entry.ID, entry.UserID, entry.MatchID, entry.HomeTeam, entry.AwayTeam,
		entry.Score, marshalStringList(entry.Goals), entry.Body, entry.Season, liked, marshalStringList(entry.Sources), entry.CreatedAt)
	return err
}

// List 按创建时间倒序。
func (s *PostgresStore) List(ctx context.Context, userID string, limit int) ([]Entry, error) {
	query := `SELECT ` + entryColumns + ` FROM journal_entries WHERE user_id = $1 ORDER BY created_at DESC`
	args := []any{userID}
	if limit > 0 {
		query += ` LIMIT $2`
		args = append(args, limit)
	}
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := make([]Entry, 0)
	for rows.Next() {
		entry, scanErr := scanEntryRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

// Get 取单篇（归属校验）。
func (s *PostgresStore) Get(ctx context.Context, userID, entryID string) (Entry, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+entryColumns+` FROM journal_entries WHERE user_id = $1 AND id = $2`, userID, entryID)
	entry, err := scanEntry(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Entry{}, ErrNotFound
	}
	return entry, err
}

// SetLiked 点赞/取消。
func (s *PostgresStore) SetLiked(ctx context.Context, userID, entryID string, liked bool) error {
	tag, err := s.pool.Exec(ctx, `UPDATE journal_entries SET liked = $3 WHERE user_id = $1 AND id = $2`, userID, entryID, liked)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete 物理删（隐私生命周期）。
func (s *PostgresStore) Delete(ctx context.Context, userID, entryID string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM journal_entries WHERE user_id = $1 AND id = $2`, userID, entryID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func scanEntryRow(rows pgx.Rows) (Entry, error) {
	var entry Entry
	var goals, sources []byte
	if err := rows.Scan(&entry.ID, &entry.UserID, &entry.MatchID, &entry.HomeTeam, &entry.AwayTeam,
		&entry.Score, &goals, &entry.Body, &entry.Season, &entry.Liked, &sources, &entry.CreatedAt); err != nil {
		return Entry{}, err
	}
	entry.Goals = unmarshalStringList(goals)
	entry.Sources = unmarshalStringList(sources)
	return entry, nil
}

// marshalStringList/unmarshalStringList 是 JSONB 字符串数组的最小编解码
// （与 knowledge topics 同款）。
func marshalStringList(values []string) []byte {
	if len(values) == 0 {
		return []byte("[]")
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return []byte("[]")
	}
	return encoded
}

func unmarshalStringList(raw []byte) []string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil
	}
	return values
}

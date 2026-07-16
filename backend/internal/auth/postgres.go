package auth

import (
	"context"
	"errors"
	"time"

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

func (store *PostgresStore) Close() {
	if store != nil && store.pool != nil {
		store.pool.Close()
	}
}

func (store *PostgresStore) Create(ctx context.Context, record SessionRecord) error {
	_, err := store.pool.Exec(ctx, `
		INSERT INTO user_sessions (id, user_id, device_id, token_hash, scopes, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, record.SessionID, record.UserID, record.DeviceID, record.RefreshTokenHash, record.Scopes, record.CreatedAt, record.ExpiresAt)
	return err
}

func (store *PostgresStore) Get(ctx context.Context, sessionID string) (SessionRecord, error) {
	var record SessionRecord
	err := store.pool.QueryRow(ctx, `
		SELECT id, user_id, device_id, token_hash, scopes, created_at, expires_at, revoked_at
		FROM user_sessions
		WHERE id = $1
	`, sessionID).Scan(
		&record.SessionID, &record.UserID, &record.DeviceID, &record.RefreshTokenHash,
		&record.Scopes, &record.CreatedAt, &record.ExpiresAt, &record.RevokedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return SessionRecord{}, ErrNotFound
	}
	return record, err
}

func (store *PostgresStore) FindByRefreshHash(ctx context.Context, refreshHash []byte) (SessionRecord, error) {
	var record SessionRecord
	err := store.pool.QueryRow(ctx, `
		SELECT id, user_id, device_id, token_hash, scopes, created_at, expires_at, revoked_at
		FROM user_sessions
		WHERE token_hash = $1
	`, refreshHash).Scan(
		&record.SessionID, &record.UserID, &record.DeviceID, &record.RefreshTokenHash,
		&record.Scopes, &record.CreatedAt, &record.ExpiresAt, &record.RevokedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return SessionRecord{}, ErrNotFound
	}
	return record, err
}

func (store *PostgresStore) Rotate(ctx context.Context, sessionID string, currentHash, refreshHash []byte, expiresAt time.Time) error {
	result, err := store.pool.Exec(ctx, `
		UPDATE user_sessions
		SET token_hash = $3, expires_at = $4
		WHERE id = $1 AND token_hash = $2 AND revoked_at IS NULL
	`, sessionID, currentHash, refreshHash, expiresAt)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrInvalidToken
	}
	return nil
}

func (store *PostgresStore) Revoke(ctx context.Context, sessionID string) error {
	result, err := store.pool.Exec(ctx, `
		UPDATE user_sessions
		SET revoked_at = COALESCE(revoked_at, now())
		WHERE id = $1
	`, sessionID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

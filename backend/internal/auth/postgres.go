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

func (store *PostgresStore) CreateAnonymous(ctx context.Context, record SessionRecord) (SessionRecord, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return SessionRecord{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "anonymous-device:"+record.DeviceID); err != nil {
		return SessionRecord{}, err
	}
	var existingUserID string
	var existingExpiresAt time.Time
	err = tx.QueryRow(ctx, `
		SELECT user_id, expires_at
		FROM anonymous_device_identities
		WHERE device_id = $1
	`, record.DeviceID).Scan(&existingUserID, &existingExpiresAt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return SessionRecord{}, err
	}
	proposedUserID := record.UserID
	selectedUserID := proposedUserID
	if existingUserID != "" {
		status, statusErr := lockAndReadPrivacyStatus(ctx, tx, existingUserID)
		if statusErr != nil {
			return SessionRecord{}, statusErr
		}
		if status == "pending" || status == "failed" {
			return SessionRecord{}, ErrIdentityUnavailable
		}
		if status != "completed" && existingExpiresAt.After(record.CreatedAt) {
			selectedUserID = existingUserID
		}
	}
	if selectedUserID == proposedUserID {
		status, statusErr := lockAndReadPrivacyStatus(ctx, tx, proposedUserID)
		if statusErr != nil {
			return SessionRecord{}, statusErr
		}
		if status != "" {
			return SessionRecord{}, ErrIdentityUnavailable
		}
	}
	record.UserID = selectedUserID
	if _, err := tx.Exec(ctx, `
		INSERT INTO anonymous_device_identities (device_id, user_id, expires_at, updated_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (device_id) DO UPDATE SET
			user_id = EXCLUDED.user_id,
			expires_at = EXCLUDED.expires_at,
			updated_at = EXCLUDED.updated_at
	`, record.DeviceID, record.UserID, record.ExpiresAt, record.CreatedAt); err != nil {
		return SessionRecord{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO user_sessions (id, user_id, device_id, token_hash, scopes, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, record.SessionID, record.UserID, record.DeviceID, record.RefreshTokenHash, record.Scopes, record.CreatedAt, record.ExpiresAt); err != nil {
		return SessionRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SessionRecord{}, err
	}
	return record, nil
}

func lockAndReadPrivacyStatus(ctx context.Context, tx pgx.Tx, userID string) (string, error) {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('privacy:' || $1, 0))`, userID); err != nil {
		return "", err
	}
	var status string
	err := tx.QueryRow(ctx, `SELECT status FROM privacy_tombstones WHERE user_id = $1`, userID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return status, err
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

package operatorauth

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore persists the operators + operator_audit tables from
// migrations/042_operators.sql (applied by the matchstate migration runner).
// Every lookup is a fresh SHA-256 hex hash query — revocation (row deletion)
// is immediate, per ADR-0008.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// OpenPostgresStore connects the operator store on the existing database URL.
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

// Seed inserts one operator row under the SHA-256 hex hash of the plaintext
// token. Bootstrap and future console create/revoke both go through here.
func (s *PostgresStore) Seed(ctx context.Context, name, token string, role Role) (Operator, error) {
	if s == nil || s.pool == nil {
		return Operator{}, ErrAuditNotStored
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return Operator{}, ErrNameRequired
	}
	if strings.TrimSpace(token) == "" {
		return Operator{}, ErrTokenRequired
	}
	if ScopesFor(role) == nil {
		return Operator{}, ErrUnknownRole
	}
	var operator Operator
	err := s.pool.QueryRow(ctx, `
		INSERT INTO operators (name, token_hash, role)
		VALUES ($1, $2, $3)
		ON CONFLICT (name) DO NOTHING
		RETURNING id, created_at
	`, name, HashToken(token), string(role)).Scan(&operator.ID, &operator.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Operator{}, ErrDuplicateName
	}
	if err != nil {
		return Operator{}, err
	}
	operator.Name = name
	operator.Role = role
	return operator, nil
}

// Delete revokes an operator by removing the row; audit rows keep the name.
func (s *PostgresStore) Delete(ctx context.Context, name string) (bool, error) {
	if s == nil || s.pool == nil {
		return false, ErrAuditNotStored
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM operators WHERE name = $1`, strings.TrimSpace(name))
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// List returns every operator row, oldest first (console Operators page).
func (s *PostgresStore) List(ctx context.Context) ([]Operator, error) {
	if s == nil || s.pool == nil {
		return nil, ErrAuditNotStored
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, role, created_at
		FROM operators
		ORDER BY id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	operators := make([]Operator, 0, 8)
	for rows.Next() {
		var operator Operator
		var role string
		if err := rows.Scan(&operator.ID, &operator.Name, &role, &operator.CreatedAt); err != nil {
			return nil, err
		}
		operator.Role = Role(role)
		operators = append(operators, operator)
	}
	return operators, rows.Err()
}

func (s *PostgresStore) Lookup(ctx context.Context, token string) (Operator, bool) {
	if s == nil || s.pool == nil {
		return Operator{}, false
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return Operator{}, false
	}
	var operator Operator
	var role string
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, role, created_at
		FROM operators
		WHERE token_hash = $1
	`, HashToken(token)).Scan(&operator.ID, &operator.Name, &role, &operator.CreatedAt)
	if err != nil {
		return Operator{}, false
	}
	operator.Role = Role(role)
	return operator, true
}

func (s *PostgresStore) Count(ctx context.Context) int64 {
	if s == nil || s.pool == nil {
		return 0
	}
	var count int64
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM operators`).Scan(&count); err != nil {
		return 0
	}
	return count
}

func (s *PostgresStore) AppendAudit(ctx context.Context, operatorName, action, object string) error {
	if s == nil || s.pool == nil {
		return ErrAuditNotStored
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO operator_audit (operator_name, action, object)
		VALUES ($1, $2, $3)
	`, operatorName, action, object)
	return err
}

func (s *PostgresStore) RecentAudit(ctx context.Context, limit int) ([]AuditEntry, error) {
	if s == nil || s.pool == nil {
		return nil, ErrAuditNotStored
	}
	if limit <= 0 {
		limit = 5
	}
	rows, err := s.pool.Query(ctx, `
		SELECT operator_name, action, object, created_at
		FROM operator_audit
		ORDER BY id DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := make([]AuditEntry, 0, limit)
	for rows.Next() {
		var entry AuditEntry
		if err := rows.Scan(&entry.OperatorName, &entry.Action, &entry.Object, &entry.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

var _ Directory = (*PostgresStore)(nil)

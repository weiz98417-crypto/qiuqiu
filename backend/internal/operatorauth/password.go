package operatorauth

// ADR-0010 password + refresh-token storage seam. Both capabilities are
// OPTIONAL interfaces resolved by type assertion at the route layer (the
// same pattern as List/Delete): the no-database MemoryStore implements them
// in-process, PostgresStore persists them in migrations/043.

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Credentials is one operator's password material: the PBKDF2 encoded hash
// (consoleauth layout) and when it was last set. A zero PasswordSetAt means
// director-issued temp password — first login forces a change.
type Credentials struct {
	PasswordHash  string
	PasswordSetAt time.Time
}

// ErrNoCredentials marks an operator without a password account (token-only,
// the ADR-0008 machine channel).
var ErrNoCredentials = errors.New("operator has no password account")

// PasswordAccounts is the password-credential capability of a store.
type PasswordAccounts interface {
	// SetPasswordCredentials stores (or replaces) one operator's encoded
	// password hash. setAt zero keeps the first-login force-change flag.
	SetPasswordCredentials(ctx context.Context, name, passwordHash string, setAt time.Time) error
	// Credentials returns the operator's password material.
	Credentials(ctx context.Context, name string) (Credentials, error)
	// HasPasswordAccounts reports whether ANY operator row carries password
	// credentials — QIUQIU_JWT_SECRET becomes required when it does.
	HasPasswordAccounts(ctx context.Context) bool
}

// RefreshTokens is the refresh-token capability of a store: rows hold only
// SHA-256 hashes (consoleauth.HashRefreshToken), are single-use (rotation
// consumes the row) and die with the operator (FK cascade in Postgres;
// explicit revoke in memory).
type RefreshTokens interface {
	// PutRefresh stores one refresh-token hash for the operator.
	PutRefresh(ctx context.Context, operatorName, tokenHash, device string, expiresAt time.Time) error
	// ConsumeRefresh resolves a refresh hash to its operator and deletes the
	// row (rotation = single use). Missing/revoked/expired → ok=false.
	ConsumeRefresh(ctx context.Context, tokenHash string) (operatorName string, ok bool)
	// RevokeRefresh deletes one refresh row (logout). ok=false when absent.
	RevokeRefresh(ctx context.Context, tokenHash string) bool
}

// MemoryStore password + refresh state.
type memoryCredentials struct {
	passwordHash  string
	passwordSetAt time.Time
}

type memoryRefresh struct {
	operatorName string
	expiresAt    time.Time
}

func (m *MemoryStore) SetPasswordCredentials(_ context.Context, name, passwordHash string, setAt time.Time) error {
	if m == nil {
		return ErrAuditNotStored
	}
	name = strings.TrimSpace(name)
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.byName[name]; !exists {
		return ErrNameRequired
	}
	if m.credentials == nil {
		m.credentials = make(map[string]memoryCredentials)
	}
	m.credentials[name] = memoryCredentials{passwordHash: passwordHash, passwordSetAt: setAt}
	return nil
}

func (m *MemoryStore) Credentials(_ context.Context, name string) (Credentials, error) {
	if m == nil {
		return Credentials{}, ErrAuditNotStored
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	stored, exists := m.credentials[strings.TrimSpace(name)]
	if !exists {
		return Credentials{}, ErrNoCredentials
	}
	return Credentials{PasswordHash: stored.passwordHash, PasswordSetAt: stored.passwordSetAt}, nil
}

func (m *MemoryStore) HasPasswordAccounts(_ context.Context) bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.credentials) > 0
}

func (m *MemoryStore) PutRefresh(_ context.Context, operatorName, tokenHash, _ string, expiresAt time.Time) error {
	if m == nil {
		return ErrAuditNotStored
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.refresh == nil {
		m.refresh = make(map[string]memoryRefresh)
	}
	m.refresh[tokenHash] = memoryRefresh{operatorName: operatorName, expiresAt: expiresAt}
	return nil
}

func (m *MemoryStore) ConsumeRefresh(_ context.Context, tokenHash string) (string, bool) {
	if m == nil {
		return "", false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	row, exists := m.refresh[tokenHash]
	if !exists || time.Now().After(row.expiresAt) {
		return "", false
	}
	delete(m.refresh, tokenHash) // rotation: single use
	return row.operatorName, true
}

func (m *MemoryStore) RevokeRefresh(_ context.Context, tokenHash string) bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.refresh[tokenHash]; !exists {
		return false
	}
	delete(m.refresh, tokenHash)
	return true
}

var (
	_ PasswordAccounts = (*MemoryStore)(nil)
	_ RefreshTokens    = (*MemoryStore)(nil)
)

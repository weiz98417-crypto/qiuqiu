// Package operatorauth is the ADR-0008 operator identity seam: personal
// bearer tokens stored as SHA-256 hex hashes, two roles (director/auditor)
// carrying the four declared operator scopes, and an operator-attributed
// audit trail. Revocation is a row deletion — Lookup hashes the presented
// token on every request (no cached sessions), so a deleted row fails on the
// very next call.
package operatorauth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"qiuqiu/internal/auth"
)

// Role is an operator role. director carries every operator scope; auditor is
// read-only (TraceRead).
type Role string

const (
	RoleDirector Role = "director"
	RoleAuditor  Role = "auditor"
)

// Operator is one identity row (never carries the plaintext token).
type Operator struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Role      Role      `json:"role"`
	CreatedAt time.Time `json:"createdAt"`
}

// AuditEntry is one operator-attributed console write row (operator_audit).
type AuditEntry struct {
	OperatorName string    `json:"operatorName"`
	Action       string    `json:"action"`
	Object       string    `json:"object"`
	CreatedAt    time.Time `json:"createdAt"`
}

// ScopesFor maps a role onto the declared operator scopes. Unknown/empty
// roles yield no scopes (every route then 403s) — the CHECK constraint keeps
// such rows out of Postgres anyway.
func ScopesFor(role Role) []string {
	switch role {
	case RoleDirector:
		return []string{
			auth.ScopeOperatorMatchWrite,
			auth.ScopeOperatorFactConfirm,
			auth.ScopeOperatorFactCorrect,
			auth.ScopeOperatorTraceRead,
		}
	case RoleAuditor:
		return []string{auth.ScopeOperatorTraceRead}
	default:
		return nil
	}
}

// HashToken is the only token representation the stores keep: SHA-256 hex of
// the plaintext bearer token.
func HashToken(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

// Directory is what the server needs from the operator store: per-request
// token resolution, a row count for the ADR-0008 dual-mode decision (legacy
// single-token mode while the table is empty), seeding for bootstrap/create,
// and the audit trail.
type Directory interface {
	// Lookup resolves a bearer token to its operator (SHA-256 hex hash
	// lookup, per request). Revoked (deleted) rows fail immediately.
	Lookup(ctx context.Context, token string) (Operator, bool)
	// Count returns how many operator rows exist.
	Count(ctx context.Context) int64
	// Seed inserts a new operator under the token's hash (bootstrap +
	// console create).
	Seed(ctx context.Context, name, token string, role Role) (Operator, error)
	// AppendAudit records one operator-attributed console write.
	AppendAudit(ctx context.Context, operatorName, action, object string) error
	// RecentAudit returns up to limit audit rows, newest first.
	RecentAudit(ctx context.Context, limit int) ([]AuditEntry, error)
}

// OperatorLister is the optional capability of stores that can list every
// operator row (the console Operators page). Memory (dev) and Postgres both
// provide it; a token-only custom store may not.
type OperatorLister interface {
	List(ctx context.Context) ([]Operator, error)
}

// OperatorRevoker is the optional capability of stores that can revoke an
// operator by row deletion; the bool reports whether the row existed.
type OperatorRevoker interface {
	Delete(ctx context.Context, name string) (bool, error)
}

var (
	ErrNameRequired   = errors.New("operator name is required")
	ErrTokenRequired  = errors.New("operator token is required")
	ErrDuplicateName  = errors.New("operator name already exists")
	ErrUnknownRole    = errors.New("operator role must be director or auditor")
	ErrAuditNotStored = errors.New("operator audit trail is unavailable")
)

// MemoryStore is the in-process Directory for tests and no-database dev mode
// (ADR-0006 convention: every local store has at least two implementations).
type MemoryStore struct {
	mu         sync.Mutex
	byToken    map[string]Operator // token hash → operator
	byName     map[string]string   // name → token hash
	nextID     int64
	audit      []AuditEntry
	auditLimit int
	// ADR-0010 password + refresh state (password.go).
	credentials map[string]memoryCredentials
	refresh     map[string]memoryRefresh
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		byToken:    make(map[string]Operator),
		byName:     make(map[string]string),
		auditLimit: 200,
	}
}

// Seed stores a new operator under the hash of the plaintext token.
func (m *MemoryStore) Seed(_ context.Context, name, token string, role Role) (Operator, error) {
	if m == nil {
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
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.byName[name]; exists {
		return Operator{}, ErrDuplicateName
	}
	hash := HashToken(token)
	if _, exists := m.byToken[hash]; exists {
		return Operator{}, ErrDuplicateName
	}
	m.nextID++
	operator := Operator{ID: m.nextID, Name: name, Role: role, CreatedAt: time.Now().UTC()}
	m.byToken[hash] = operator
	m.byName[name] = hash
	return operator, nil
}

// Delete revokes an operator: the row is removed, so Lookup fails on the
// next request (immediate revocation).
func (m *MemoryStore) Delete(_ context.Context, name string) (bool, error) {
	if m == nil {
		return false, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	hash, exists := m.byName[name]
	if !exists {
		return false, nil
	}
	delete(m.byName, name)
	delete(m.byToken, hash)
	delete(m.credentials, name)
	for tokenHash, row := range m.refresh {
		if row.operatorName == name {
			delete(m.refresh, tokenHash)
		}
	}
	return true, nil
}

func (m *MemoryStore) Lookup(_ context.Context, token string) (Operator, bool) {
	if m == nil {
		return Operator{}, false
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return Operator{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	operator, ok := m.byToken[HashToken(token)]
	return operator, ok
}

func (m *MemoryStore) Count(_ context.Context) int64 {
	if m == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return int64(len(m.byToken))
}

// List returns every operator row (the console Operators page), sorted by
// name. Rows never carry the plaintext token.
func (m *MemoryStore) List(_ context.Context) ([]Operator, error) {
	if m == nil {
		return nil, ErrAuditNotStored
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	operators := make([]Operator, 0, len(m.byName))
	for _, hash := range m.byName {
		if operator, ok := m.byToken[hash]; ok {
			operators = append(operators, operator)
		}
	}
	sort.Slice(operators, func(i, j int) bool { return operators[i].Name < operators[j].Name })
	return operators, nil
}

func (m *MemoryStore) AppendAudit(_ context.Context, operatorName, action, object string) error {
	if m == nil {
		return ErrAuditNotStored
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.audit = append(m.audit, AuditEntry{
		OperatorName: operatorName, Action: action, Object: object, CreatedAt: time.Now().UTC(),
	})
	if len(m.audit) > m.auditLimit {
		m.audit = m.audit[len(m.audit)-m.auditLimit:]
	}
	return nil
}

func (m *MemoryStore) RecentAudit(_ context.Context, limit int) ([]AuditEntry, error) {
	if m == nil {
		return nil, ErrAuditNotStored
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if limit <= 0 || limit > len(m.audit) {
		limit = len(m.audit)
	}
	recent := make([]AuditEntry, 0, limit)
	for index := len(m.audit) - 1; index >= 0 && len(recent) < limit; index-- {
		recent = append(recent, m.audit[index])
	}
	return recent, nil
}

var _ Directory = (*MemoryStore)(nil)

// BootstrapSeed parses the QIUQIU_BOOTSTRAP_OPERATOR value ("name:token").
func BootstrapSeed(spec string) (name, token string, err error) {
	parts := strings.SplitN(strings.TrimSpace(spec), ":", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("QIUQIU_BOOTSTRAP_OPERATOR must be name:token")
	}
	name, token = strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	if name == "" || token == "" {
		return "", "", fmt.Errorf("QIUQIU_BOOTSTRAP_OPERATOR must be name:token")
	}
	return name, token, nil
}

// Bootstrap seeds the first director operator when the table has no rows and
// QIUQIU_BOOTSTRAP_OPERATOR ("name:token") is set. Called once at startup; it
// logs exactly once whether it seeded. An existing operator population is
// never touched.
func Bootstrap(ctx context.Context, store Directory, spec string, logf func(format string, args ...any)) error {
	if store == nil {
		return nil
	}
	name, token, err := BootstrapSeed(spec)
	if err != nil {
		if strings.TrimSpace(spec) == "" {
			return nil
		}
		return err
	}
	if store.Count(ctx) > 0 {
		return nil
	}
	if _, err := store.Seed(ctx, name, token, RoleDirector); err != nil {
		return err
	}
	if logf != nil {
		logf("operator bootstrap: seeded director operator %q from QIUQIU_BOOTSTRAP_OPERATOR (token shown once, stored as sha256)", name)
	}
	return nil
}

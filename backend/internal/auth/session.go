package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	ScopeUserChat            = "user:chat"
	ScopeUserRead            = "user:read"
	ScopeOperatorMatchWrite  = "operator:match:write"
	ScopeOperatorFactConfirm = "operator:fact:confirm"
	ScopeOperatorFactCorrect = "operator:fact:correct"
	ScopeOperatorTraceRead   = "operator:trace:read"

	accessTokenVersion = "qiuqiu-session-v1"
	accessTokenTTL     = 15 * time.Minute
	refreshTokenTTL    = 30 * 24 * time.Hour
)

var (
	ErrInvalidToken = errors.New("invalid session token")
	ErrExpiredToken = errors.New("session token expired")
	ErrRevoked      = errors.New("session revoked")
	ErrNotFound     = errors.New("session not found")
)

type Claims struct {
	Subject   string
	SessionID string
	DeviceID  string
	Scopes    []string
	IssuedAt  time.Time
	ExpiresAt time.Time
}

func (claims Claims) HasScope(scope string) bool {
	for _, candidate := range claims.Scopes {
		if candidate == scope {
			return true
		}
	}
	return false
}

type Session struct {
	Claims           Claims
	AccessToken      string
	RefreshToken     string
	RefreshExpiresAt time.Time
}

type SessionRecord struct {
	SessionID        string
	UserID           string
	DeviceID         string
	RefreshTokenHash []byte
	Scopes           []string
	CreatedAt        time.Time
	ExpiresAt        time.Time
	RevokedAt        *time.Time
}

type Store interface {
	Create(context.Context, SessionRecord) error
	Get(context.Context, string) (SessionRecord, error)
	FindByRefreshHash(context.Context, []byte) (SessionRecord, error)
	Rotate(context.Context, string, []byte, []byte, time.Time) error
	Revoke(context.Context, string) error
}

type Manager struct {
	store Store
	key   []byte
	now   func() time.Time
}

func NewManager(store Store, signingKey string) (*Manager, error) {
	if store == nil {
		return nil, errors.New("session store is required")
	}
	if len(strings.TrimSpace(signingKey)) < 32 {
		return nil, errors.New("session signing key must be at least 32 characters")
	}
	return &Manager{
		store: store,
		key:   []byte(signingKey),
		now:   func() time.Time { return time.Now().UTC() },
	}, nil
}

func (manager *Manager) IssueAnonymous(ctx context.Context, deviceID string) (Session, error) {
	now := manager.now().UTC()
	userID, err := randomIdentifier("usr_")
	if err != nil {
		return Session{}, err
	}
	sessionID, err := randomIdentifier("ses_")
	if err != nil {
		return Session{}, err
	}
	if !validIdentifier(deviceID, 128) {
		deviceID, err = randomIdentifier("dev_")
		if err != nil {
			return Session{}, err
		}
	}
	refreshToken, err := randomToken()
	if err != nil {
		return Session{}, err
	}
	record := SessionRecord{
		SessionID:        sessionID,
		UserID:           userID,
		DeviceID:         deviceID,
		RefreshTokenHash: hashToken(refreshToken),
		Scopes:           []string{ScopeUserChat, ScopeUserRead},
		CreatedAt:        now,
		ExpiresAt:        now.Add(refreshTokenTTL),
	}
	if err := manager.store.Create(ctx, record); err != nil {
		return Session{}, err
	}
	return manager.session(record, refreshToken, now), nil
}

func (manager *Manager) Authenticate(ctx context.Context, token string) (Claims, error) {
	claims, err := manager.parseAccessToken(token)
	if err != nil {
		return Claims{}, err
	}
	record, err := manager.store.Get(ctx, claims.SessionID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Claims{}, ErrInvalidToken
		}
		return Claims{}, err
	}
	if record.RevokedAt != nil {
		return Claims{}, ErrRevoked
	}
	if !record.ExpiresAt.After(manager.now()) {
		return Claims{}, ErrExpiredToken
	}
	if record.UserID != claims.Subject || record.DeviceID != claims.DeviceID {
		return Claims{}, ErrInvalidToken
	}
	return claims, nil
}

func (manager *Manager) ValidateClaims(ctx context.Context, claims Claims) error {
	if claims.Subject == "" || claims.SessionID == "" || claims.DeviceID == "" {
		return ErrInvalidToken
	}
	if !claims.ExpiresAt.After(manager.now()) {
		return ErrExpiredToken
	}
	record, err := manager.store.Get(ctx, claims.SessionID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrInvalidToken
		}
		return err
	}
	if record.RevokedAt != nil {
		return ErrRevoked
	}
	if !record.ExpiresAt.After(manager.now()) || record.UserID != claims.Subject || record.DeviceID != claims.DeviceID {
		return ErrInvalidToken
	}
	return nil
}

func (manager *Manager) Refresh(ctx context.Context, refreshToken string) (Session, error) {
	if strings.TrimSpace(refreshToken) == "" {
		return Session{}, ErrInvalidToken
	}
	record, err := manager.store.FindByRefreshHash(ctx, hashToken(refreshToken))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Session{}, ErrInvalidToken
		}
		return Session{}, err
	}
	now := manager.now().UTC()
	if record.RevokedAt != nil {
		return Session{}, ErrRevoked
	}
	if !record.ExpiresAt.After(now) {
		return Session{}, ErrExpiredToken
	}
	rotated, err := randomToken()
	if err != nil {
		return Session{}, err
	}
	if err := manager.store.Rotate(ctx, record.SessionID, record.RefreshTokenHash, hashToken(rotated), record.ExpiresAt); err != nil {
		return Session{}, err
	}
	record.RefreshTokenHash = hashToken(rotated)
	return manager.session(record, rotated, now), nil
}

func (manager *Manager) RevokeAccess(ctx context.Context, accessToken string) error {
	claims, err := manager.parseAccessToken(accessToken)
	if err != nil {
		return err
	}
	return manager.store.Revoke(ctx, claims.SessionID)
}

func (manager *Manager) RevokeRefresh(ctx context.Context, refreshToken string) error {
	record, err := manager.store.FindByRefreshHash(ctx, hashToken(refreshToken))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrInvalidToken
		}
		return err
	}
	return manager.store.Revoke(ctx, record.SessionID)
}

func (manager *Manager) session(record SessionRecord, refreshToken string, now time.Time) Session {
	claims := Claims{
		Subject:   record.UserID,
		SessionID: record.SessionID,
		DeviceID:  record.DeviceID,
		Scopes:    append([]string(nil), record.Scopes...),
		IssuedAt:  now,
		ExpiresAt: now.Add(accessTokenTTL),
	}
	return Session{
		Claims:           claims,
		AccessToken:      manager.signAccessToken(claims, refreshToken),
		RefreshToken:     refreshToken,
		RefreshExpiresAt: record.ExpiresAt,
	}
}

type tokenClaims struct {
	Subject   string   `json:"sub"`
	SessionID string   `json:"sid"`
	DeviceID  string   `json:"did"`
	Scopes    []string `json:"scp"`
	IssuedAt  int64    `json:"iat"`
	ExpiresAt int64    `json:"exp"`
	TokenID   string   `json:"jti"`
}

func (manager *Manager) signAccessToken(claims Claims, refreshToken string) string {
	tokenIDHash := hashToken(refreshToken)
	payload, _ := json.Marshal(tokenClaims{
		Subject: claims.Subject, SessionID: claims.SessionID, DeviceID: claims.DeviceID,
		Scopes: claims.Scopes, IssuedAt: claims.IssuedAt.Unix(), ExpiresAt: claims.ExpiresAt.Unix(),
		TokenID: base64.RawURLEncoding.EncodeToString(tokenIDHash[:12]),
	})
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	return accessTokenVersion + "." + encoded + "." + base64.RawURLEncoding.EncodeToString(manager.sign(encoded))
}

func (manager *Manager) parseAccessToken(token string) (Claims, error) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 || parts[0] != accessTokenVersion || len(token) > 4096 {
		return Claims{}, ErrInvalidToken
	}
	provided, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || subtle.ConstantTimeCompare(provided, manager.sign(parts[1])) != 1 {
		return Claims{}, ErrInvalidToken
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	var raw tokenClaims
	if json.Unmarshal(payload, &raw) != nil || raw.Subject == "" || raw.SessionID == "" || raw.DeviceID == "" || raw.TokenID == "" || raw.ExpiresAt <= 0 {
		return Claims{}, ErrInvalidToken
	}
	claims := Claims{
		Subject: raw.Subject, SessionID: raw.SessionID, DeviceID: raw.DeviceID,
		Scopes:   append([]string(nil), raw.Scopes...),
		IssuedAt: time.Unix(raw.IssuedAt, 0).UTC(), ExpiresAt: time.Unix(raw.ExpiresAt, 0).UTC(),
	}
	if !claims.ExpiresAt.After(manager.now()) {
		return Claims{}, ErrExpiredToken
	}
	return claims, nil
}

func (manager *Manager) sign(value string) []byte {
	mac := hmac.New(sha256.New, manager.key)
	_, _ = mac.Write([]byte(value))
	return mac.Sum(nil)
}

func hashToken(token string) []byte {
	digest := sha256.Sum256([]byte(token))
	return digest[:]
}

func randomToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func randomIdentifier(prefix string) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	return prefix + token, nil
}

func validIdentifier(value string, maximum int) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maximum {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') {
			continue
		}
		if character != '_' && character != '-' && character != '.' {
			return false
		}
	}
	return true
}

func BearerToken(value string) string {
	value = strings.TrimSpace(value)
	if len(value) < 7 || !strings.EqualFold(value[:7], "Bearer ") {
		return ""
	}
	return strings.TrimSpace(value[7:])
}

func (claims Claims) String() string {
	return fmt.Sprintf("%s/%s", claims.Subject, claims.SessionID)
}

type MemoryStore struct {
	mu       sync.RWMutex
	sessions map[string]SessionRecord
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{sessions: make(map[string]SessionRecord)}
}

func (store *MemoryStore) Create(_ context.Context, record SessionRecord) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, exists := store.sessions[record.SessionID]; exists {
		return fmt.Errorf("session %s already exists", record.SessionID)
	}
	store.sessions[record.SessionID] = cloneRecord(record)
	return nil
}

func (store *MemoryStore) Get(_ context.Context, sessionID string) (SessionRecord, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	record, ok := store.sessions[sessionID]
	if !ok {
		return SessionRecord{}, ErrNotFound
	}
	return cloneRecord(record), nil
}

func (store *MemoryStore) FindByRefreshHash(_ context.Context, refreshHash []byte) (SessionRecord, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	for _, record := range store.sessions {
		if hmac.Equal(record.RefreshTokenHash, refreshHash) {
			return cloneRecord(record), nil
		}
	}
	return SessionRecord{}, ErrNotFound
}

func (store *MemoryStore) Rotate(_ context.Context, sessionID string, currentHash, refreshHash []byte, expiresAt time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	record, ok := store.sessions[sessionID]
	if !ok {
		return ErrNotFound
	}
	if !hmac.Equal(record.RefreshTokenHash, currentHash) {
		return ErrInvalidToken
	}
	record.RefreshTokenHash = append([]byte(nil), refreshHash...)
	record.ExpiresAt = expiresAt
	store.sessions[sessionID] = record
	return nil
}

func (store *MemoryStore) Revoke(_ context.Context, sessionID string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	record, ok := store.sessions[sessionID]
	if !ok {
		return ErrNotFound
	}
	now := time.Now().UTC()
	record.RevokedAt = &now
	store.sessions[sessionID] = record
	return nil
}

func cloneRecord(record SessionRecord) SessionRecord {
	record.RefreshTokenHash = append([]byte(nil), record.RefreshTokenHash...)
	record.Scopes = append([]string(nil), record.Scopes...)
	if record.RevokedAt != nil {
		revoked := *record.RevokedAt
		record.RevokedAt = &revoked
	}
	return record
}

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
	ErrInvalidToken        = errors.New("invalid session token")
	ErrExpiredToken        = errors.New("session token expired")
	ErrRevoked             = errors.New("session revoked")
	ErrNotFound            = errors.New("session not found")
	ErrIdentityUnavailable = errors.New("anonymous identity unavailable")
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
	CreateAnonymous(context.Context, SessionRecord) (SessionRecord, error)
	Get(context.Context, string) (SessionRecord, error)
	Rotate(context.Context, string, []byte, []byte, time.Time) error
	Revoke(context.Context, string) error
	// 登录凭证缝（ADR-0020）：identifier→usr_ 的绑定存取。
	CreateCredential(context.Context, Credential) error
	FindCredential(context.Context, string) (Credential, error)
}

// Credential 是一个正式身份凭证：identifier 唯一，密码只存 argon2id PHC 串。
// user_id 是身份主体——绑定不换 ID，画像/记忆/订阅都挂在原 usr_ 上。
type Credential struct {
	UserID        string
	Identifier    string
	PasswordHash  string
	CreatedAt     time.Time
	UpdatedAt     time.Time
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
	var err error
	if !validIdentifier(deviceID, 128) {
		deviceID, err = randomIdentifier("dev_")
		if err != nil {
			return Session{}, err
		}
	}
	proposedUserID, err := randomIdentifier("usr_")
	if err != nil {
		return Session{}, err
	}
	sessionID, err := randomIdentifier("ses_")
	if err != nil {
		return Session{}, err
	}
	refreshSecret, err := randomToken()
	if err != nil {
		return Session{}, err
	}
	record := SessionRecord{
		SessionID:        sessionID,
		UserID:           proposedUserID,
		DeviceID:         deviceID,
		RefreshTokenHash: hashToken(refreshSecret),
		Scopes:           []string{ScopeUserChat, ScopeUserRead},
		CreatedAt:        now,
		ExpiresAt:        now.Add(refreshTokenTTL),
	}
	record, err = manager.store.CreateAnonymous(ctx, record)
	if err != nil {
		return Session{}, err
	}
	return manager.session(record, formatRefreshToken(record.SessionID, refreshSecret), now), nil
}

// formatRefreshToken 把会话 ID 编进刷新令牌（<sessionID>.<secret>）：
// 重放检测需要定位被滥用的会话才能吊销整条链——纯随机令牌按哈希查找时，
// 旧令牌在库里已不存在，无从归因（ADR-0020 决定 6）。
func formatRefreshToken(sessionID, secret string) string {
	return sessionID + "." + secret
}

func splitRefreshToken(token string) (sessionID, secret string, ok bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// Login 是登录凭证缝的单一入口（ADR-0020），注册与切换双合一：
//   - identifier 未绑定 → 给当前会话的 usr_ 原地注册凭证（升级不换 ID）；
//   - identifier 已绑定 → 验密后为该账号签发会话（切换；本机匿名数据不迁移）；
//   - 密码错误 → ErrInvalidLogin。
//
// DeviceID 沿用当前会话的设备；旧会话不吊销（多设备允许同时在线）。
func (manager *Manager) Login(ctx context.Context, claims Claims, identifier, password string) (Session, error) {
	if claims.Subject == "" || claims.DeviceID == "" {
		return Session{}, ErrInvalidToken
	}
	identifier = NormalizeIdentifier(identifier)
	if err := ValidateLoginRequest(identifier, password); err != nil {
		return Session{}, err
	}
	existing, err := manager.store.FindCredential(ctx, identifier)
	switch {
	case err == nil:
		if !VerifyPassword(password, existing.PasswordHash) {
			return Session{}, ErrInvalidLogin
		}
		return manager.issueForDevice(ctx, existing.UserID, claims.DeviceID)
	case errors.Is(err, ErrNotFound):
		hash, hashErr := HashPassword(password)
		if hashErr != nil {
			return Session{}, hashErr
		}
		now := manager.now().UTC()
		createErr := manager.store.CreateCredential(ctx, Credential{
			UserID:       claims.Subject,
			Identifier:   identifier,
			PasswordHash: hash,
			CreatedAt:    now,
			UpdatedAt:    now,
		})
		if createErr != nil {
			if errors.Is(createErr, ErrCredentialExists) {
				// 注册竞态：另一请求先绑定了同一 identifier——归并到
				// 验证路径，语义与「已绑定」一致。
				raced, raceErr := manager.store.FindCredential(ctx, identifier)
				if raceErr != nil || !VerifyPassword(password, raced.PasswordHash) {
					return Session{}, ErrInvalidLogin
				}
				return manager.issueForDevice(ctx, raced.UserID, claims.DeviceID)
			}
			return Session{}, createErr
		}
		return manager.issueForDevice(ctx, claims.Subject, claims.DeviceID)
	default:
		return Session{}, err
	}
}

// issueForDevice 为 (userID, deviceID) 签发全新会话， scopes 与匿名一致
// （正式身份不追加管理面作用域）。
func (manager *Manager) issueForDevice(ctx context.Context, userID, deviceID string) (Session, error) {
	now := manager.now().UTC()
	sessionID, err := randomIdentifier("ses_")
	if err != nil {
		return Session{}, err
	}
	refreshSecret, err := randomToken()
	if err != nil {
		return Session{}, err
	}
	record := SessionRecord{
		SessionID:        sessionID,
		UserID:           userID,
		DeviceID:         deviceID,
		RefreshTokenHash: hashToken(refreshSecret),
		Scopes:           []string{ScopeUserChat, ScopeUserRead},
		CreatedAt:        now,
		ExpiresAt:        now.Add(refreshTokenTTL),
	}
	if err := manager.store.Create(ctx, record); err != nil {
		return Session{}, err
	}
	return manager.session(record, formatRefreshToken(record.SessionID, refreshSecret), now), nil
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
	sessionID, secret, ok := splitRefreshToken(refreshToken)
	if !ok {
		return Session{}, ErrInvalidToken
	}
	record, err := manager.store.Get(ctx, sessionID)
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
	if !hmac.Equal(record.RefreshTokenHash, hashToken(secret)) {
		// 重用检测（ADR-0020 决定 6）：令牌自报的会话与库中哈希不符=
		// 重放——吊销整条会话，重放者与持有者一同下线。
		_ = manager.store.Revoke(ctx, record.SessionID)
		return Session{}, ErrInvalidToken
	}
	rotated, err := randomToken()
	if err != nil {
		return Session{}, err
	}
	if err := manager.store.Rotate(ctx, record.SessionID, record.RefreshTokenHash, hashToken(rotated), record.ExpiresAt); err != nil {
		return Session{}, err
	}
	record.RefreshTokenHash = hashToken(rotated)
	return manager.session(record, formatRefreshToken(record.SessionID, rotated), now), nil
}

func (manager *Manager) RevokeAccess(ctx context.Context, accessToken string) error {
	claims, err := manager.parseAccessToken(accessToken)
	if err != nil {
		return err
	}
	return manager.store.Revoke(ctx, claims.SessionID)
}

func (manager *Manager) RevokeRefresh(ctx context.Context, refreshToken string) error {
	sessionID, secret, ok := splitRefreshToken(refreshToken)
	if !ok {
		return ErrInvalidToken
	}
	record, err := manager.store.Get(ctx, sessionID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrInvalidToken
		}
		return err
	}
	if !hmac.Equal(record.RefreshTokenHash, hashToken(secret)) {
		return ErrInvalidToken
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
	mu          sync.RWMutex
	sessions    map[string]SessionRecord
	identities  map[string]anonymousIdentity
	credentials map[string]Credential // identifier → credential
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		sessions:    make(map[string]SessionRecord),
		identities:  make(map[string]anonymousIdentity),
		credentials: make(map[string]Credential),
	}
}

type anonymousIdentity struct {
	UserID    string
	ExpiresAt time.Time
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

func (store *MemoryStore) CreateAnonymous(_ context.Context, record SessionRecord) (SessionRecord, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, exists := store.sessions[record.SessionID]; exists {
		return SessionRecord{}, fmt.Errorf("session %s already exists", record.SessionID)
	}
	identity := store.identities[record.DeviceID]
	if identity.UserID == "" || !identity.ExpiresAt.After(record.CreatedAt) {
		identity.UserID = record.UserID
	}
	identity.ExpiresAt = record.ExpiresAt
	store.identities[record.DeviceID] = identity
	record.UserID = identity.UserID
	store.sessions[record.SessionID] = cloneRecord(record)
	return cloneRecord(record), nil
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

func (store *MemoryStore) CreateCredential(_ context.Context, credential Credential) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, exists := store.credentials[credential.Identifier]; exists {
		return ErrCredentialExists
	}
	store.credentials[credential.Identifier] = credential
	return nil
}

func (store *MemoryStore) FindCredential(_ context.Context, identifier string) (Credential, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	credential, ok := store.credentials[identifier]
	if !ok {
		return Credential{}, ErrNotFound
	}
	return credential, nil
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

package main

// Console auth routes (ADR-0010): the human channel — username + password →
// 15-minute HS256 access token + 30-day refresh token — alongside the
// untouched personal-token machine channel. Password material is PBKDF2
// (consoleauth); refresh rows keep only SHA-256 hashes and rotation is
// single-use (the old row is deleted when consumed).

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"qiuqiu/internal/consoleauth"
	"qiuqiu/internal/operatorauth"
)

// temporaryPasswordLength is the director-issued temp password length
// (ambiguous characters excluded).
const temporaryPasswordLength = 12

// handleLogin issues the access/refresh pair for valid credentials.
// POST /api/console/auth/login {username, password} — unauthenticated.
func (deps consoleAPI) handleLogin(w http.ResponseWriter, r *http.Request) {
	passwords, _, ok := deps.passwordStores(w)
	if !ok {
		return
	}
	var request struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Device   string `json:"device"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	request.Username = strings.TrimSpace(request.Username)
	credentials, err := passwords.Credentials(r.Context(), request.Username)
	if err != nil || !consoleauth.VerifyPassword(request.Password, credentials.PasswordHash) {
		// Failed attempts are audited; v1 has no lockout (locked decision 5).
		_ = deps.operators.AppendAudit(r.Context(), request.Username, "auth.login_failed", request.Username)
		http.Error(w, "invalid username or password", http.StatusUnauthorized)
		return
	}
	// Locked decision: the JWT secret is required once any password account
	// exists — a missing secret is a deployment error, not a login error.
	if strings.TrimSpace(deps.jwtSecret) == "" {
		http.Error(w, consoleauth.ErrJWTSecretRequired.Error(), http.StatusServiceUnavailable)
		return
	}
	pair, err := deps.issueAuthPair(w, r, request.Username, credentials, request.Device)
	if err != nil {
		return // response already written
	}
	writeJSON(w, http.StatusOK, pair)
}

// handleRefresh rotates the refresh token (single use) into a fresh pair.
// POST /api/console/auth/refresh {refreshToken} — the refresh token is the
// credential.
func (deps consoleAPI) handleRefresh(w http.ResponseWriter, r *http.Request) {
	passwords, refresh, ok := deps.passwordStores(w)
	if !ok {
		return
	}
	var request struct {
		RefreshToken string `json:"refreshToken"`
		Device       string `json:"device"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	operatorName, valid := refresh.ConsumeRefresh(r.Context(), consoleauth.HashRefreshToken(strings.TrimSpace(request.RefreshToken)))
	if !valid {
		http.Error(w, "refresh token is invalid or expired", http.StatusUnauthorized)
		return
	}
	credentials, err := passwords.Credentials(r.Context(), operatorName)
	if err != nil {
		http.Error(w, "operator no longer has a password account", http.StatusUnauthorized)
		return
	}
	if strings.TrimSpace(deps.jwtSecret) == "" {
		http.Error(w, consoleauth.ErrJWTSecretRequired.Error(), http.StatusServiceUnavailable)
		return
	}
	pair, err := deps.issueAuthPair(w, r, operatorName, credentials, request.Device)
	if err != nil {
		return
	}
	writeJSON(w, http.StatusOK, pair)
}

// handleLogout revokes one device's refresh row. Access tokens die within
// AccessTokenTTL on their own (ADR-0010 accepted revocation latency).
// POST /api/console/auth/logout {refreshToken}.
func (deps consoleAPI) handleLogout(w http.ResponseWriter, r *http.Request) {
	_, refresh, ok := deps.passwordStores(w)
	if !ok {
		return
	}
	var request struct {
		RefreshToken string `json:"refreshToken"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	revoked := refresh.RevokeRefresh(r.Context(), consoleauth.HashRefreshToken(strings.TrimSpace(request.RefreshToken)))
	writeJSON(w, http.StatusOK, map[string]any{"revoked": revoked})
}

// handleMePassword is the self-service change: old password required, new
// password min length 10. PATCH /api/console/me/password — any authenticated
// operator identity (JWT or personal token), self only.
func (deps consoleAPI) handleMePassword(w http.ResponseWriter, r *http.Request) {
	claims, ok := deps.authz.claims(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	passwords, _, ok := deps.passwordStores(w)
	if !ok {
		return
	}
	name := operatorName(claims)
	var request struct {
		OldPassword string `json:"oldPassword"`
		NewPassword string `json:"newPassword"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	credentials, err := passwords.Credentials(r.Context(), name)
	if err != nil {
		http.Error(w, consoleauth.ErrNoPasswordAccount.Error(), http.StatusBadRequest)
		return
	}
	if !consoleauth.VerifyPassword(request.OldPassword, credentials.PasswordHash) {
		http.Error(w, "old password is incorrect", http.StatusUnauthorized)
		return
	}
	if len(request.NewPassword) < 10 {
		http.Error(w, consoleauth.ErrPasswordTooShort.Error(), http.StatusBadRequest)
		return
	}
	hash, err := consoleauth.HashPassword(request.NewPassword)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := passwords.SetPasswordCredentials(r.Context(), name, hash, time.Now().UTC()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = deps.operators.AppendAudit(r.Context(), name, "me.password_change", name)
	writeJSON(w, http.StatusOK, map[string]any{"changed": true})
}

// issueAuthPair signs the access JWT and stores the refresh row, then writes
// the response. The refresh token itself is shown once; only its hash lands
// in the store.
func (deps consoleAPI) issueAuthPair(w http.ResponseWriter, r *http.Request, operatorName string, credentials operatorauth.Credentials, device string) (map[string]any, error) {
	var operator operatorauth.Operator
	if listed, ok := deps.operators.(interface {
		List(ctx context.Context) ([]operatorauth.Operator, error)
	}); ok {
		if rows, err := listed.List(r.Context()); err == nil {
			for _, row := range rows {
				if row.Name == operatorName {
					operator = row
				}
			}
		}
	}
	if operator.Name == "" {
		http.Error(w, "operator not found", http.StatusUnauthorized)
		return nil, errResponseWritten
	}
	accessToken, err := consoleauth.SignJWT(consoleauth.Claims{
		Sub:         operator.Name,
		Role:        string(operator.Role),
		Scopes:      operatorauth.ScopesFor(operator.Role),
		PasswordSet: !credentials.PasswordSetAt.IsZero(),
	}, deps.jwtSecret, consoleauth.AccessTokenTTL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return nil, errResponseWritten
	}
	refreshStore, _ := deps.operators.(operatorauth.RefreshTokens)
	refreshToken, err := consoleauth.NewRefreshToken()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil, errResponseWritten
	}
	if refreshStore != nil {
		if err := refreshStore.PutRefresh(r.Context(), operator.Name, consoleauth.HashRefreshToken(refreshToken), device, consoleauth.RefreshExpiryAt(time.Now().UTC())); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return nil, errResponseWritten
		}
	}
	return map[string]any{
		"accessToken":            accessToken,
		"refreshToken":           refreshToken,
		"passwordChangeRequired": credentials.PasswordSetAt.IsZero(),
		"operator": map[string]any{
			"name":   operator.Name,
			"role":   operator.Role,
			"scopes": operatorauth.ScopesFor(operator.Role),
		},
	}, nil
}

// passwordStores resolves the optional ADR-0010 store capabilities and
// gates the whole human channel on password auth being configured at all:
// the legacy eval mode (no operator rows) has no password accounts, and the
// routes answer 400 instead of pretending to authenticate.
func (deps consoleAPI) passwordStores(w http.ResponseWriter) (operatorauth.PasswordAccounts, operatorauth.RefreshTokens, bool) {
	passwords, ok := deps.operators.(operatorauth.PasswordAccounts)
	if !ok {
		http.Error(w, "password auth requires a persistent operator store", http.StatusBadRequest)
		return nil, nil, false
	}
	refresh, ok := deps.operators.(operatorauth.RefreshTokens)
	if !ok {
		http.Error(w, "password auth requires a persistent operator store", http.StatusBadRequest)
		return nil, nil, false
	}
	return passwords, refresh, true
}

// newTemporaryPassword mints the director-issued one-time password
// (unambiguous alphabet, crypto/rand).
func newTemporaryPassword() (string, error) {
	const alphabet = "abcdefghjkmnpqrstuvwxyzABCDEFGHJKMNPQRSTUVWXYZ23456789"
	raw := make([]byte, temporaryPasswordLength)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	out := make([]byte, temporaryPasswordLength)
	for index, value := range raw {
		out[index] = alphabet[value%byte(len(alphabet))]
	}
	return string(out), nil
}

// issueTemporaryPassword stores a first-login password for a freshly created
// operator and returns the plaintext (shown once, next to the personal
// token). A nil-capability store degrades to token-only creation.
func (deps consoleAPI) issueTemporaryPassword(w http.ResponseWriter, r *http.Request, operator operatorauth.Operator) (string, bool) {
	passwords, ok := deps.operators.(operatorauth.PasswordAccounts)
	if !ok {
		return "", false
	}
	temporary, err := newTemporaryPassword()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return "", false
	}
	hash, err := consoleauth.HashPassword(temporary)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return "", false
	}
	// Zero setAt = first login forces a change (locked decision 5).
	if err := passwords.SetPasswordCredentials(r.Context(), operator.Name, hash, time.Time{}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return "", false
	}
	return temporary, true
}

type errResponseType struct{}

func (errResponseType) Error() string { return "response already written" }

var errResponseWritten = errResponseType{}

package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestAnonymousSessionCarriesServerAssignedIdentity(t *testing.T) {
	manager := newTestManager(t)
	session, err := manager.IssueAnonymous(context.Background(), "device_123")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(session.Claims.Subject, "usr_") || !strings.HasPrefix(session.Claims.SessionID, "ses_") {
		t.Fatalf("unexpected claims: %+v", session.Claims)
	}
	if session.Claims.DeviceID != "device_123" {
		t.Fatalf("device id = %q", session.Claims.DeviceID)
	}
	claims, err := manager.Authenticate(context.Background(), session.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != session.Claims.Subject || !claims.HasScope(ScopeUserChat) {
		t.Fatalf("authenticated claims = %+v", claims)
	}
}

func TestSessionTokenTamperingAndExpiryAreRejected(t *testing.T) {
	manager := newTestManager(t)
	session, err := manager.IssueAnonymous(context.Background(), "device_123")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(session.AccessToken, ".")
	parts[1] = parts[1] + "x"
	if _, err := manager.Authenticate(context.Background(), strings.Join(parts, ".")); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("tampered token error = %v", err)
	}
	manager.now = func() time.Time { return time.Now().UTC().Add(accessTokenTTL + time.Second) }
	if _, err := manager.Authenticate(context.Background(), session.AccessToken); !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("expired token error = %v", err)
	}
}

func TestRefreshTokenRotatesAndCannotBeReused(t *testing.T) {
	manager := newTestManager(t)
	session, err := manager.IssueAnonymous(context.Background(), "device_123")
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := manager.Refresh(context.Background(), session.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	if rotated.RefreshToken == session.RefreshToken || rotated.AccessToken == session.AccessToken {
		t.Fatal("refresh token and access token should rotate")
	}
	if _, err := manager.Refresh(context.Background(), session.RefreshToken); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("reused refresh token error = %v", err)
	}
	if err := manager.RevokeAccess(context.Background(), rotated.AccessToken); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Authenticate(context.Background(), rotated.AccessToken); !errors.Is(err, ErrRevoked) {
		t.Fatalf("revoked access token error = %v", err)
	}
}

func TestBearerTokenParserDoesNotAcceptOtherSchemes(t *testing.T) {
	if got := BearerToken("Basic secret"); got != "" {
		t.Fatalf("basic token = %q", got)
	}
	if got := BearerToken("Bearer session-token"); got != "session-token" {
		t.Fatalf("bearer token = %q", got)
	}
}

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	manager, err := NewManager(NewMemoryStore(), "test-session-signing-key-0123456789")
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

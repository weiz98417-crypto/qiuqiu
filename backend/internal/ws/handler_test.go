package ws

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"qiuqiu/internal/auth"
	"qiuqiu/internal/config"

	"github.com/gorilla/websocket"
)

func TestHubAcceptsProtocolTokenAndRejectsQueryToken(t *testing.T) {
	cfg := &config.Config{Environment: "development", AppToken: "secret"}
	hub := NewHub(cfg)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, release, err := hub.Upgrade(w, r)
		if err != nil {
			return
		}
		defer release()
		defer conn.Close()
	}))
	defer server.Close()

	websocketURL := "ws" + strings.TrimPrefix(server.URL, "http")
	protocol := authProtocolPrefix + base64.RawURLEncoding.EncodeToString([]byte("secret"))
	dialer := websocket.Dialer{Subprotocols: []string{protocol}}
	conn, response, err := dialer.Dial(websocketURL, nil)
	if err != nil {
		t.Fatalf("protocol token should connect: %v response=%v", err, response)
	}
	if conn.Subprotocol() != protocol {
		t.Fatalf("server should negotiate auth protocol, got %q", conn.Subprotocol())
	}
	conn.Close()

	_, response, err = websocket.DefaultDialer.Dial(websocketURL+"?token=secret", nil)
	if err == nil {
		t.Fatal("query-string token should not authenticate")
	}
	if response == nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for query token, got response=%v err=%v", response, err)
	}
}

func TestHubRejectsUntrustedOrigin(t *testing.T) {
	cfg := &config.Config{
		Environment:    "production",
		AuthMode:       "dual",
		AppToken:       "secret",
		AllowedOrigins: []string{"https://app.qiuqiu.example"},
	}
	hub := NewHub(cfg)
	request := httptest.NewRequest(http.MethodGet, "http://server/ws", nil)
	request.Header.Set("Origin", "https://attacker.example")
	request.Header.Set("Authorization", "Bearer secret")
	response := httptest.NewRecorder()

	_, _, err := hub.Upgrade(response, request)
	if err == nil {
		t.Fatal("untrusted origin should be rejected")
	}
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", response.Code)
	}
}

func TestHubRejectsSameOriginUserClientWithoutSessionToken(t *testing.T) {
	cfg := &config.Config{Environment: "production", AuthMode: "session", AppToken: "operator-secret"}
	hub := NewHub(cfg)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, release, err := hub.Upgrade(w, r)
		if err != nil {
			return
		}
		defer release()
		defer conn.Close()
	}))
	defer server.Close()

	websocketURL := "ws" + strings.TrimPrefix(server.URL, "http")
	header := http.Header{"Origin": []string{server.URL}}
	_, response, err := websocket.DefaultDialer.Dial(websocketURL, header)
	if err == nil {
		t.Fatal("same-origin production client should require a session token")
	}
	if response == nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for missing session token, got response=%v err=%v", response, err)
	}
}

func TestHubRejectsOperatorTokenOnSessionOnlyUserWebSocket(t *testing.T) {
	cfg := &config.Config{
		Environment: "production",
		AuthMode:    "session",
		AppToken:    "operator-secret",
	}
	hub := NewHub(cfg)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, release, err := hub.Upgrade(w, r)
		if err != nil {
			return
		}
		defer release()
		defer conn.Close()
	}))
	defer server.Close()

	protocol := authProtocolPrefix + base64.RawURLEncoding.EncodeToString([]byte("operator-secret"))
	dialer := websocket.Dialer{Subprotocols: []string{protocol}}
	_, response, err := dialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err == nil {
		t.Fatal("operator token should not authenticate the user websocket")
	}
	if response == nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for operator token, got response=%v err=%v", response, err)
	}
}

func TestHubBindsProductionConnectionToSessionClaims(t *testing.T) {
	manager, err := auth.NewManager(auth.NewMemoryStore(), "test-session-signing-key-0123456789")
	if err != nil {
		t.Fatal(err)
	}
	session, err := manager.IssueAnonymous(context.Background(), "device_123")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Environment: "production", AuthMode: "session"}
	hub := NewHub(cfg).WithSessionAuthenticator(manager)
	identities := make(chan auth.Claims, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, release, claims, err := hub.UpgradeWithIdentity(w, r)
		if err != nil {
			return
		}
		defer release()
		defer conn.Close()
		identities <- claims
	}))
	defer server.Close()

	protocol := authProtocolPrefix + base64.RawURLEncoding.EncodeToString([]byte(session.AccessToken))
	dialer := websocket.Dialer{Subprotocols: []string{protocol}}
	connection, response, err := dialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("session token should connect: %v response=%v", err, response)
	}
	connection.Close()
	claims := <-identities
	if claims.Subject != session.Claims.Subject || claims.SessionID != session.Claims.SessionID {
		t.Fatalf("connection claims = %+v", claims)
	}
}

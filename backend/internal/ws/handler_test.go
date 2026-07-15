package ws

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

func TestHubAllowsSameOriginUserClientWithoutOperatorToken(t *testing.T) {
	cfg := &config.Config{Environment: "production", AppToken: "operator-secret"}
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
	conn, response, err := websocket.DefaultDialer.Dial(websocketURL, header)
	if err != nil {
		t.Fatalf("same-origin user client should connect: %v response=%v", err, response)
	}
	conn.Close()
}

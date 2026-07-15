package ws

import (
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"qiuqiu/internal/config"

	"github.com/gorilla/websocket"
)

const authProtocolPrefix = "qiuqiu-auth."

type Hub struct {
	cfg      *config.Config
	mu       sync.Mutex
	conns    map[*websocket.Conn]string
	ipCounts map[string]int
}

func NewHub(cfg *config.Config) *Hub {
	return &Hub{
		cfg:      cfg,
		conns:    make(map[*websocket.Conn]string),
		ipCounts: make(map[string]int),
	}
}

func (h *Hub) HandleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (h *Hub) Upgrade(w http.ResponseWriter, r *http.Request) (*websocket.Conn, func(), error) {
	token, protocol := requestToken(r)
	if h.cfg.AppToken != "" && !tokenEqual(token, h.cfg.AppToken) && !sameOriginClient(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return nil, func() {}, fmt.Errorf("unauthorized")
	}
	if !h.cfg.OriginAllowedForHost(r.Header.Get("Origin"), r.Host) {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return nil, func() {}, fmt.Errorf("origin not allowed")
	}

	clientIP := remoteIP(r.RemoteAddr)
	h.mu.Lock()
	if h.ipCounts[clientIP] >= h.cfg.MaxConnsPerIP() {
		h.mu.Unlock()
		http.Error(w, "too many connections", http.StatusTooManyRequests)
		return nil, func() {}, fmt.Errorf("too many connections")
	}
	h.ipCounts[clientIP]++
	h.mu.Unlock()

	releaseCount := func() {
		h.mu.Lock()
		h.ipCounts[clientIP]--
		if h.ipCounts[clientIP] <= 0 {
			delete(h.ipCounts, clientIP)
		}
		h.mu.Unlock()
	}

	responseHeader := http.Header{}
	if protocol != "" {
		responseHeader.Set("Sec-WebSocket-Protocol", protocol)
	}
	upgrader := websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		CheckOrigin: func(request *http.Request) bool {
			return h.cfg.OriginAllowedForHost(request.Header.Get("Origin"), request.Host)
		},
	}
	conn, err := upgrader.Upgrade(w, r, responseHeader)
	if err != nil {
		releaseCount()
		return nil, func() {}, err
	}

	conn.SetReadLimit(h.cfg.WSReadLimit())
	_ = conn.SetReadDeadline(time.Now().Add(time.Duration(h.cfg.WSReadTimeout()) * time.Second))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(time.Duration(h.cfg.WSReadTimeout()) * time.Second))
	})

	h.mu.Lock()
	h.conns[conn] = clientIP
	h.mu.Unlock()

	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() {
			h.mu.Lock()
			delete(h.conns, conn)
			h.mu.Unlock()
			releaseCount()
		})
	}
	return conn, release, nil
}

func sameOriginClient(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return false
	}
	parsed, err := url.Parse(origin)
	return err == nil && parsed.Scheme != "" && strings.EqualFold(parsed.Host, strings.TrimSpace(r.Host))
}

func (h *Hub) HandleWS(w http.ResponseWriter, r *http.Request) {
	conn, release, err := h.Upgrade(w, r)
	if err != nil {
		return
	}
	defer release()
	defer conn.Close()

	if err := conn.WriteJSON(map[string]string{"type": "welcome", "message": "connected"}); err != nil {
		return
	}

	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var request map[string]interface{}
			if json.Unmarshal(message, &request) == nil && request["type"] == "ping" {
				if err := conn.WriteJSON(map[string]string{"type": "pong"}); err != nil {
					return
				}
			}
		}
	}()

	pingTicker := time.NewTicker(30 * time.Second)
	defer pingTicker.Stop()
	for {
		select {
		case <-readDone:
			return
		case <-pingTicker.C:
			_ = conn.SetWriteDeadline(time.Now().Add(time.Duration(h.cfg.WSWriteTimeout()) * time.Second))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.Printf("ws ping error: %v", err)
				return
			}
		}
	}
}

func requestToken(r *http.Request) (string, string) {
	authorization := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(authorization, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer ")), ""
	}
	for _, protocol := range websocket.Subprotocols(r) {
		if !strings.HasPrefix(protocol, authProtocolPrefix) {
			continue
		}
		encoded := strings.TrimPrefix(protocol, authProtocolPrefix)
		decoded, err := base64.RawURLEncoding.DecodeString(encoded)
		if err == nil {
			return string(decoded), protocol
		}
	}
	return "", ""
}

func tokenEqual(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func remoteIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil {
		return host
	}
	return remoteAddr
}

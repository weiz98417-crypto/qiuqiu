package ws

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"qiuqiu/internal/config"

	"github.com/gorilla/websocket"
)

// Upgrade wraps the websocket upgrader with auth and limits.
func Upgrade(w http.ResponseWriter, r *http.Request, cfg *config.Config) (*websocket.Conn, error) {
	token := r.URL.Query().Get("token")
	if token != cfg.AppToken {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return nil, fmt.Errorf("unauthorized")
	}
	return upgrader.Upgrade(w, r, nil)
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

type Hub struct {
	cfg       *config.Config
	mu        sync.Mutex
	conns     map[*websocket.Conn]string // conn -> client IP
	ipCounts  map[string]int
}

func NewHub(cfg *config.Config) *Hub {
	return &Hub{
		cfg:      cfg,
		conns:    make(map[*websocket.Conn]string),
		ipCounts: make(map[string]int),
	}
}

// HandleHealth returns 200 for liveness checks.
func (h *Hub) HandleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

// HandleWS upgrades HTTP to WebSocket for match watching.
func (h *Hub) HandleWS(w http.ResponseWriter, r *http.Request) {
	// Auth check
	token := r.URL.Query().Get("token")
	if token != h.cfg.AppToken {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Per-IP connection limit
	clientIP := r.RemoteAddr
	h.mu.Lock()
	if h.ipCounts[clientIP] >= h.cfg.MaxConnsPerIP() {
		h.mu.Unlock()
		http.Error(w, "too many connections", http.StatusTooManyRequests)
		return
	}
	h.ipCounts[clientIP]++
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		h.ipCounts[clientIP]--
		if h.ipCounts[clientIP] <= 0 {
			delete(h.ipCounts, clientIP)
		}
		h.mu.Unlock()
	}()

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade error: %v", err)
		return
	}

	conn.SetReadLimit(h.cfg.WSReadLimit())
	conn.SetReadDeadline(time.Now().Add(time.Duration(h.cfg.WSReadTimeout()) * time.Second))

	h.mu.Lock()
	h.conns[conn] = clientIP
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		delete(h.conns, conn)
		h.mu.Unlock()
		conn.Close()
	}()

	// Send welcome message
	welcome := map[string]string{"type": "welcome", "message": "connected"}
	data, _ := json.Marshal(welcome)
	conn.WriteMessage(websocket.TextMessage, data)

	// Ping-pong keepalive
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(time.Duration(h.cfg.WSReadTimeout()) * time.Second))
		return nil
	})

	pingTicker := time.NewTicker(30 * time.Second)
	defer pingTicker.Stop()

	// Read loop — handle client messages
	go func() {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var req map[string]interface{}
			if json.Unmarshal(msg, &req) == nil {
				if req["type"] == "ping" {
					resp := map[string]string{"type": "pong"}
					data, _ := json.Marshal(resp)
					conn.WriteMessage(websocket.TextMessage, data)
				}
			}
		}
	}()

	// Write loop — periodic ping
	for {
		select {
		case <-pingTicker.C:
			conn.SetWriteDeadline(time.Now().Add(time.Duration(h.cfg.WSWriteTimeout()) * time.Second))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

package session

import (
	"context"
	"encoding/json"
	"log"
	"qiuqiu/internal/event"
	"qiuqiu/internal/pipeline"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// WatchSession manages a single user's match watching session.
type WatchSession struct {
	ID         string
	MatchID    int64
	Status     string // waiting|watching|ended
	conn       *websocket.Conn
	engine     *pipeline.Engine
	aiPipeline *pipeline.AIPipeline
	mu         sync.Mutex
	writeMu    *sync.Mutex
}

func New(id string, matchID int64, conn *websocket.Conn, engine *pipeline.Engine, aiPipe *pipeline.AIPipeline, writeMu *sync.Mutex) *WatchSession {
	return &WatchSession{
		ID:         id,
		MatchID:    matchID,
		Status:     "watching",
		conn:       conn,
		engine:     engine,
		aiPipeline: aiPipe,
		writeMu:    writeMu,
	}
}

// Run starts the session main loop: consume pipeline output, send to client.
func (s *WatchSession) Run(ctx context.Context) {
	s.mu.Lock()
	s.Status = "watching"
	s.mu.Unlock()

	s.send(map[string]string{"type": "match_status", "status": "live"})

	for {
		select {
		case inst := <-s.engine.Output():
			if inst == nil {
				continue
			}
			// Process through AI pipeline
			result := s.aiPipeline.Process(ctx, inst)
			if result.Text == "" {
				continue
			}

			// Send expression update
			s.send(map[string]interface{}{
				"type":  "expression",
				"state": result.Expression,
			})

			// Send audio if available
			if len(result.AudioData) > 0 {
				if s.writeMu != nil {
					s.writeMu.Lock()
					s.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
					if err := s.conn.WriteMessage(websocket.BinaryMessage, result.AudioData); err != nil {
						log.Printf("session %s: audio write error: %v", s.ID, err)
					}
					s.writeMu.Unlock()
				} else {
					s.conn.WriteMessage(websocket.BinaryMessage, result.AudioData)
				}
			}

			// Send text event
			s.send(map[string]interface{}{
				"type":       "event",
				"event":      inst.Event.Type,
				"expression": result.Expression,
				"data": map[string]interface{}{
					"team":   inst.Event.Team,
					"minute": inst.Event.Minute,
					"player": inst.Event.Player.Name,
					"score":  inst.MatchContext.ScoreAfter,
				},
			})

		case <-ctx.Done():
			s.send(map[string]string{"type": "match_status", "status": "ended"})
			s.mu.Lock()
			s.Status = "ended"
			s.mu.Unlock()
			return
		}
	}
}

func (s *WatchSession) PushEvent(ev *event.StandardEvent) {
	s.engine.Push(ev)
}

func (s *WatchSession) send(msg interface{}) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	if s.writeMu != nil {
		s.writeMu.Lock()
		defer s.writeMu.Unlock()
		s.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if err := s.conn.WriteMessage(websocket.TextMessage, data); err != nil {
			log.Printf("session %s: write error: %v", s.ID, err)
		}
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := s.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		log.Printf("session %s: write error: %v", s.ID, err)
	}
}

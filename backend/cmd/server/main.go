package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"qiuqiu/internal/config"
	"qiuqiu/internal/datasource"
	"qiuqiu/internal/llm"
	"qiuqiu/internal/pipeline"
	"qiuqiu/internal/session"
	"qiuqiu/internal/tts"
	"qiuqiu/internal/ws"

	"github.com/gorilla/websocket"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
)

func main() {
	_ = godotenv.Load()
	cfg := config.Load()

	var rdb *redis.Client
	if cfg.RedisAddr != "" {
		rdb = redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	}

	// Prompt manager
	promptMgr := pipeline.NewPromptManager()
	promptMgr.LoadSystem(readFile("prompts/v1.0/system.txt"))
	promptMgr.LoadTemplate("goal", readFile("prompts/v1.0/goal.txt"))
	promptMgr.LoadTemplate("shot", readFile("prompts/v1.0/shot.txt"))
	promptMgr.LoadTemplate("card", readFile("prompts/v1.0/card.txt"))
	promptMgr.LoadTemplate("match_status", readFile("prompts/v1.0/match_status.txt"))

	// AI clients
	llmClient := llm.NewClient(cfg.DeepseekBaseURL, cfg.DeepseekAPIKey)
	var ttsClient *tts.Client
	if cfg.ElevenLabsKey != "" {
		ttsClient = tts.NewClient(cfg.ElevenLabsKey)
	}
	apiSports := datasource.NewClient(os.Getenv("APISPORTS_API_KEY"))

	var sessionsMu sync.Mutex
	sessions := make(map[string]*session.WatchSession)

	hub := ws.NewHub(cfg)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", hub.HandleHealth)

	mux.HandleFunc("/ws/match/", func(w http.ResponseWriter, r *http.Request) {
		conn, err := ws.Upgrade(w, r, cfg)
		if err != nil {
			return
		}

		matchIDStr := r.URL.Path[len("/ws/match/"):]
		matchID, _ := strconv.ParseInt(matchIDStr, 10, 64)

		deduper := pipeline.NewDeduper(rdb)
		throttler := pipeline.NewThrottler(rdb)
		enricher := pipeline.NewContextEnricher()
		engine := pipeline.NewEngine(deduper, throttler, enricher)
		aiPipe := pipeline.NewAIPipeline(llmClient, ttsClient, promptMgr)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go engine.Run(ctx)

		sess := session.New(matchIDStr, matchID, conn, engine, aiPipe)
		sessionsMu.Lock()
		sessions[matchIDStr] = sess
		sessionsMu.Unlock()

		// Start polling data source
		poller := datasource.NewPoller(apiSports, matchID, engine.EventChan())
		go poller.Run(ctx)

		go sess.Run(ctx)

		// Read loop
		go func() {
			for {
				_, msg, err := conn.ReadMessage()
				if err != nil {
					cancel()
					return
				}
				var req map[string]interface{}
				if json.Unmarshal(msg, &req) == nil && req["type"] == "ping" {
					resp, _ := json.Marshal(map[string]string{"type": "pong"})
					conn.WriteMessage(websocket.TextMessage, resp)
				}
			}
		}()

		// Heartbeat
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	})

	addr := ":" + cfg.Port
	log.Printf("qiuqiu server starting on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func readFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

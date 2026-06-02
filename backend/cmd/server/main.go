package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"qiuqiu/internal/asr"
	"qiuqiu/internal/companion"
	"qiuqiu/internal/config"
	"qiuqiu/internal/datasource"
	"qiuqiu/internal/llm"
	"qiuqiu/internal/matchstate"
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
	llmClient := llm.NewClient(cfg.DeepseekBaseURL, cfg.DeepseekAPIKey, cfg.DeepseekModel)
	var ttsClient *tts.Client
	if cfg.ElevenLabsKey != "" {
		ttsClient = tts.NewClient(cfg.ElevenLabsKey)
	}
	apiSports := datasource.NewClient(os.Getenv("APISPORTS_API_KEY"))
	asrClient := asr.NewClient(os.Getenv("SILICONFLOW_API_KEY"))

	var sessionsMu sync.Mutex
	sessions := make(map[string]*session.WatchSession)

	hub := ws.NewHub(cfg)
	var matchStore matchstate.Repository = matchstate.NewStore()
	if cfg.DatabaseURL != "" {
		postgresStore, err := matchstate.OpenPostgresStore(context.Background(), cfg.DatabaseURL, "migrations")
		if err != nil {
			log.Fatalf("postgres match store: %v", err)
		}
		defer postgresStore.Close()
		matchStore = postgresStore
		log.Printf("match store: postgresql")
	} else {
		log.Printf("match store: memory")
	}
	companionTools := companion.NewRepositoryMemoryTools(matchStore)
	var traceReader companion.TraceReader = companionTools
	if cfg.DatabaseURL != "" {
		traceWriter, err := companion.OpenPostgresTraceWriter(context.Background(), cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("postgres trace writer: %v", err)
		}
		defer traceWriter.Close()
		traceReader = traceWriter
		companionTools.WithTraceWriter(companion.NewAsyncTraceWriter(traceWriter, 256))
	}
	companionAgent := companion.NewAgent(companionTools)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", hub.HandleHealth)
	mux.HandleFunc("/api/matches/", handleMatchAPI(matchStore, traceReader, cfg, llmClient, promptMgr))
	fs := http.StripPrefix("/assets/", http.FileServer(http.Dir("../client/assets/live2d")))
	mux.HandleFunc("/assets/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		if r.Method == "OPTIONS" {
			w.WriteHeader(200)
			return
		}
		fs.ServeHTTP(w, r)
	})
	mux.HandleFunc("/live2d.html", func(w http.ResponseWriter, r *http.Request) {
		noCache(w)
		http.ServeFile(w, r, "../client/assets/live2d/live2d.html")
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/app.html" {
			http.NotFound(w, r)
			return
		}
		noCache(w)
		http.ServeFile(w, r, "../client/assets/live2d/app.html")
	})
	mux.HandleFunc("/test-expressions.html", func(w http.ResponseWriter, r *http.Request) {
		noCache(w)
		http.ServeFile(w, r, "../client/assets/live2d/test-expressions.html")
	})
	mux.HandleFunc("/operator.html", func(w http.ResponseWriter, r *http.Request) {
		noCache(w)
		http.ServeFile(w, r, "../client/assets/live2d/operator.html")
	})
	mux.HandleFunc("/director-prototype.html", func(w http.ResponseWriter, r *http.Request) {
		noCache(w)
		http.ServeFile(w, r, "../client/assets/live2d/director-prototype.html")
	})

	mux.HandleFunc("/ws/match/", func(w http.ResponseWriter, r *http.Request) {
		conn, err := ws.Upgrade(w, r, cfg)
		if err != nil {
			return
		}
		writer := &wsWriter{conn: conn}

		matchIDStr := r.URL.Path[len("/ws/match/"):]
		matchID, _ := strconv.ParseInt(matchIDStr, 10, 64)
		matchEvents, unsubscribe := matchStore.Subscribe(matchIDStr)
		defer unsubscribe()

		deduper := pipeline.NewDeduper(rdb)
		throttler := pipeline.NewThrottler(rdb)
		enricher := pipeline.NewContextEnricher()
		engine := pipeline.NewEngine(deduper, throttler, enricher)
		aiPipe := pipeline.NewAIPipeline(llmClient, ttsClient, promptMgr)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go engine.Run(ctx)

		sess := session.New(matchIDStr, matchID, conn, engine, aiPipe, &writer.mu)
		sessionsMu.Lock()
		sessions[matchIDStr] = sess
		sessionsMu.Unlock()

		// Start polling data source
		poller := datasource.NewPoller(apiSports, matchID, engine.EventChan())
		go poller.Run(ctx)

		go sess.Run(ctx)
		writer.SendJSON(map[string]interface{}{
			"type": "match_snapshot",
			"data": matchStore.Snapshot(matchIDStr),
		})
		go func() {
			for ev := range matchEvents {
				writer.SendJSON(map[string]interface{}{
					"type":     "match_event",
					"data":     ev,
					"snapshot": matchStore.Snapshot(matchIDStr),
				})
			}
		}()

		// Read loop
		go func() {
			defer cancel()
			for {
				_, msg, err := conn.ReadMessage()
				if err != nil {
					return
				}
				var req map[string]interface{}
				if json.Unmarshal(msg, &req) != nil {
					continue
				}
				switch req["type"] {
				case "ping":
					writer.SendJSON(map[string]string{"type": "pong"})

				case "interrupt":
					// Cancel current generation, restart context
					cancel()
					newCtx, newCancel := context.WithCancel(context.Background())
					ctx = newCtx
					cancel = newCancel
					go engine.Run(ctx)
					go sess.Run(ctx)
					go datasource.NewPoller(apiSports, matchID, engine.EventChan()).Run(ctx)

					writer.SendJSON(map[string]interface{}{
						"type":       "interrupt",
						"expression": "listening",
					})

				case "user_speech":
					text := str(req, "text")
					audioB64 := str(req, "audio")
					// Adjust cooldown based on talkativeness
					if t := str(req, "talkativeness"); t != "" {
						switch t {
						case "quiet":
							engine.SetCooldown(15 * time.Second)
						case "active":
							engine.SetCooldown(4 * time.Second)
						default:
							engine.SetCooldown(8 * time.Second)
						}
					}
					go func() {
						// Use independent context - connection ctx may be cancelled
						bgCtx := context.Background()
						var replyText string
						if audioB64 != "" {
							audioBytes, err := base64.StdEncoding.DecodeString(audioB64)
							if err == nil && len(audioBytes) > 0 {
								wav := pcmToWav(audioBytes)
								result, err := asrClient.Transcribe(bgCtx, wav, nil)
								if err == nil {
									text = result.Text
									log.Printf("asr: %q (conf=%.2f)", text, result.Confidence)
								} else {
									log.Printf("asr error: %v", err)
								}
							}
						}

						if text == "" {
							return
						}

						result, err := companionAgent.HandleMessage(bgCtx, companion.MessageRequest{
							MatchID: matchIDStr,
							UserID:  fallbackString(str(req, "userId"), r.RemoteAddr),
							Text:    text,
							Now:     time.Now(),
						})
						if err != nil {
							log.Printf("companion reply error: %v", err)
							return
						}
						replyText = result.Reply

						// Synthesize speech
						var audioData []byte
						if ttsClient != nil {
							ttsResult, err := ttsClient.Synthesize(bgCtx, replyText, "cgSgspJ2msm6clMCkdW9")
							if err == nil {
								audioData = ttsResult.AudioData
							}
						}

						// Send expression
						writer.SendJSON(map[string]interface{}{
							"type":  "expression",
							"state": "chat",
						})

						// Send audio
						if len(audioData) > 0 {
							writer.SendBinary(audioData)
						}

						// Send text
						writer.SendJSON(map[string]interface{}{
							"type":  "event",
							"event": "qiuqiu_reply",
							"data": map[string]interface{}{
								"text": replyText,
							},
						})
					}()
				}
			}
		}()

		// Heartbeat
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if err := writer.Ping(); err != nil {
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

func handleMatchAPI(store matchstate.Repository, traceReader companion.TraceReader, cfg *config.Config, llmClient *llm.Client, promptMgr *pipeline.PromptManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		path := strings.TrimPrefix(r.URL.Path, "/api/matches/")
		parts := strings.Split(strings.Trim(path, "/"), "/")
		if len(parts) < 2 || parts[0] == "" {
			http.NotFound(w, r)
			return
		}

		matchID := parts[0]
		resource := parts[1]
		switch {
		case r.Method == http.MethodGet && resource == "config" && len(parts) == 2:
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"config":   store.Config(matchID),
				"snapshot": store.Snapshot(matchID),
			})
		case r.Method == http.MethodPost && resource == "config" && len(parts) == 2:
			if !validAPIToken(r, cfg) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			var config matchstate.MatchConfig
			if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			saved, snapshot, err := store.SetConfig(matchID, config)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"config":   saved,
				"snapshot": snapshot,
			})
		case r.Method == http.MethodGet && resource == "events" && len(parts) == 2:
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"events": store.Events(matchID),
			})
		case r.Method == http.MethodGet && resource == "state" && len(parts) == 2:
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"snapshot": store.Snapshot(matchID),
			})
		case r.Method == http.MethodGet && resource == "traces" && len(parts) == 2:
			if !validAPIToken(r, cfg) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			limit := 50
			if value := r.URL.Query().Get("limit"); value != "" {
				if parsed, err := strconv.Atoi(value); err == nil {
					limit = parsed
				}
			}
			traces, err := traceReader.ListTraces(r.Context(), matchID, limit)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"traces": traces,
			})
		case r.Method == http.MethodGet && resource == "traces" && len(parts) == 3:
			if !validAPIToken(r, cfg) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			trace, err := traceReader.GetTrace(r.Context(), matchID, parts[2])
			if err != nil {
				status := http.StatusInternalServerError
				if errors.Is(err, companion.ErrTraceNotFound) {
					status = http.StatusNotFound
				}
				http.Error(w, err.Error(), status)
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"trace": trace,
			})
		case r.Method == http.MethodPost && resource == "events" && len(parts) == 2:
			if !validAPIToken(r, cfg) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			var ev matchstate.MatchEvent
			if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			if ev.ProactiveText == "__quiet__" {
				ev.ProactiveText = ""
			} else if strings.TrimSpace(ev.ProactiveText) == "" {
				ev.ProactiveText = generateProactiveText(r.Context(), llmClient, promptMgr, ev, store.Snapshot(matchID))
			}
			created, snapshot, err := store.Create(matchID, ev)
			if err != nil {
				status := http.StatusBadRequest
				if errors.Is(err, matchstate.ErrNotFound) {
					status = http.StatusNotFound
				}
				http.Error(w, err.Error(), status)
				return
			}
			writeJSON(w, http.StatusCreated, map[string]interface{}{
				"event":    created,
				"snapshot": snapshot,
			})
		case r.Method == http.MethodPost && resource == "events" && len(parts) == 4 && parts[3] == "correct":
			if !validAPIToken(r, cfg) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			var ev matchstate.MatchEvent
			if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			if ev.ProactiveText == "__quiet__" {
				ev.ProactiveText = ""
			} else if strings.TrimSpace(ev.ProactiveText) == "" {
				ev.ProactiveText = generateProactiveText(r.Context(), llmClient, promptMgr, ev, store.Snapshot(matchID))
			}
			corrected, snapshot, err := store.Correct(matchID, parts[2], ev)
			if err != nil {
				status := http.StatusBadRequest
				if errors.Is(err, matchstate.ErrNotFound) {
					status = http.StatusNotFound
				}
				http.Error(w, err.Error(), status)
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"event":    corrected,
				"snapshot": snapshot,
			})
		default:
			http.NotFound(w, r)
		}
	}
}

func validAPIToken(r *http.Request, cfg *config.Config) bool {
	if cfg.AppToken == "" {
		return true
	}
	if r.URL.Query().Get("token") == cfg.AppToken {
		return true
	}
	auth := r.Header.Get("Authorization")
	return auth == "Bearer "+cfg.AppToken
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("writeJSON error: %v", err)
	}
}

func noCache(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
}

func generateProactiveText(ctx context.Context, llmClient *llm.Client, promptMgr *pipeline.PromptManager, ev matchstate.MatchEvent, snapshot matchstate.Snapshot) string {
	if llmClient == nil || promptMgr == nil {
		return fallbackProactiveText(ev)
	}
	genCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	messages := pipeline.BuildProactiveEventMessages(promptMgr.System(), ev, snapshot)
	result, err := llmClient.GenerateWithMessages(genCtx, messages, 0.8)
	if err != nil {
		log.Printf("proactive event reply error: %v", err)
		return fallbackProactiveText(ev)
	}
	return strings.TrimSpace(result.Text)
}

func fallbackProactiveText(ev matchstate.MatchEvent) string {
	switch ev.EventType {
	case "goal":
		return "进了！这一下气氛直接被点起来了。"
	case "red_card":
		return "红牌来了，比赛走势一下子变得很微妙。"
	case "penalty":
		return "点球时刻来了，先深呼吸，这球太关键了。"
	case "var_check":
		return "VAR 介入了，这几秒真的很折磨人。"
	case "big_chance", "pressure":
		return "这波很危险，我们盯紧一点。"
	case "miss":
		return "哎呀，就差一点点，这球太可惜了。"
	default:
		if ev.Description != "" {
			return ev.Description
		}
		return "场上有新情况，我们一起看下去。"
	}
}

func readFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func str(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
}

func fallbackString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func sendJSON(conn *websocket.Conn, msg interface{}) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("sendJSON marshal error: %v", err)
		return
	}
	conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		log.Printf("sendJSON write error: %v", err)
	}
}

type wsWriter struct {
	mu   sync.Mutex
	conn *websocket.Conn
}

func (w *wsWriter) SendJSON(msg interface{}) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("ws marshal error: %v", err)
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := w.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		log.Printf("ws write error: %v", err)
	}
}

func (w *wsWriter) SendBinary(data []byte) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := w.conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
		log.Printf("ws binary write error: %v", err)
	}
}

func (w *wsWriter) Ping() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return w.conn.WriteMessage(websocket.PingMessage, nil)
}

// pcmToWav wraps raw PCM 16bit 16kHz mono in a WAV header.
func pcmToWav(pcm []byte) []byte {
	dataSize := len(pcm)
	wav := make([]byte, 44+dataSize)
	// RIFF
	copy(wav[0:4], "RIFF")
	le32(wav[4:8], uint32(36+dataSize))
	copy(wav[8:12], "WAVE")
	// fmt
	copy(wav[12:16], "fmt ")
	le32(wav[16:20], 16)    // chunk size
	le16(wav[20:22], 1)     // PCM
	le16(wav[22:24], 1)     // mono
	le32(wav[24:28], 24000) // sample rate
	le32(wav[28:32], 48000) // byte rate (24000 * 2)
	le16(wav[32:34], 2)     // block align
	le16(wav[34:36], 16)    // bits per sample
	// data
	copy(wav[36:40], "data")
	le32(wav[40:44], uint32(dataSize))
	copy(wav[44:], pcm)
	return wav
}

func le16(b []byte, v uint16) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
}

func le32(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}

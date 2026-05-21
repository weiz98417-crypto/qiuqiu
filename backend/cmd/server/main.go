package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"qiuqiu/internal/asr"
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
	asrClient := asr.NewClient(os.Getenv("SILICONFLOW_API_KEY"))

	var sessionsMu sync.Mutex
	sessions := make(map[string]*session.WatchSession)

	hub := ws.NewHub(cfg)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", hub.HandleHealth)
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
		http.ServeFile(w, r, "../client/assets/live2d/live2d.html")
	})
	mux.HandleFunc("/test-expressions.html", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "../client/assets/live2d/test-expressions.html")
	})

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
					resp, _ := json.Marshal(map[string]string{"type": "pong"})
					conn.WriteMessage(websocket.TextMessage, resp)

				case "interrupt":
					// Cancel current generation, restart context
					cancel()
					newCtx, newCancel := context.WithCancel(context.Background())
					ctx = newCtx
					cancel = newCancel
					go engine.Run(ctx)
					go sess.Run(ctx)
					go datasource.NewPoller(apiSports, matchID, engine.EventChan()).Run(ctx)

					resp, _ := json.Marshal(map[string]interface{}{
						"type":       "interrupt",
						"expression": "listening",
					})
					conn.WriteMessage(websocket.TextMessage, resp)

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

						// Classify intent
						intent := pipeline.ClassifyIntent(text)
						if intent == "ignore" {
							return
						}

						// Build reply prompt
						messages := pipeline.BuildReplyMessages(promptMgr.System(), text, intent)

						// Generate reply
						result, err := llmClient.GenerateWithMessages(bgCtx, messages, 0.8)
						if err != nil {
							log.Printf("llm reply error: %v", err)
							return
						}
						replyText = result.Text

						// Synthesize speech
						var audioData []byte
						if ttsClient != nil {
							ttsResult, err := ttsClient.Synthesize(bgCtx, replyText, "cgSgspJ2msm6clMCkdW9")
							if err == nil {
								audioData = ttsResult.AudioData
							}
						}

						// Send expression
						sendJSON(conn, map[string]interface{}{
							"type":  "expression",
							"state": "chat",
						})

						// Send audio
						if len(audioData) > 0 {
							conn.WriteMessage(websocket.BinaryMessage, audioData)
						}

						// Send text
						sendJSON(conn, map[string]interface{}{
							"type": "event",
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

func str(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
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
	le32(wav[16:20], 16)         // chunk size
	le16(wav[20:22], 1)          // PCM
	le16(wav[22:24], 1)          // mono
	le32(wav[24:28], 24000)      // sample rate
	le32(wav[28:32], 48000)      // byte rate (24000 * 2)
	le16(wav[32:34], 2)          // block align
	le16(wav[34:36], 16)         // bits per sample
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

package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
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

type speechRecognizer interface {
	Transcribe(ctx context.Context, audio []byte, hints []string) (*asr.Result, error)
}

type speechSynthesizer interface {
	Synthesize(ctx context.Context, text, voiceID string) (*tts.SynthesizeResult, error)
}

type voiceSessionResult struct {
	Text      string
	Reply     string
	Trace     companion.Trace
	AudioData []byte
	AudioMIME string
	ASRError  string
	TTSError  string
}

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
	llmClient := newTextLLMClient(cfg)
	var ttsClient *tts.Client
	ttsKey := fallbackString(cfg.MiMoAPIKey, cfg.ElevenLabsKey)
	if ttsKey != "" {
		ttsClient = tts.NewClient(ttsKey).WithBaseURL(cfg.MiMoBaseURL).WithModel("mimo-v2.5-tts").WithVoice(cfg.MiMoVoice)
	}
	asrKey := fallbackString(cfg.MiMoAPIKey, os.Getenv("SILICONFLOW_API_KEY"))
	asrClient := asr.NewClient(asrKey).WithBaseURL(cfg.MiMoBaseURL).WithModel("mimo-v2.5-asr")

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
	var demoResetter companion.DemoResetter = companionTools
	if cfg.DatabaseURL != "" {
		traceWriter, err := companion.OpenPostgresTraceWriter(context.Background(), cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("postgres trace writer: %v", err)
		}
		defer traceWriter.Close()
		traceReader = traceWriter
		companionTools.WithTurnReader(traceWriter)
		demoResetter = traceWriter
		companionTools.WithTraceWriter(companion.NewAsyncTraceWriter(traceWriter, 256))
	}
	companionAgent := companion.NewAgent(companionTools)
	if llmClient != nil {
		companionAgent.WithPolisher(companion.NewLLMReplyPolisher(llmClient), 3*time.Second)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", hub.HandleHealth)
	mux.HandleFunc("/api/matches/", handleMatchAPI(matchStore, traceReader, demoResetter, cfg, llmClient, promptMgr))
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

		go sess.Run(ctx)
		writer.SendJSON(map[string]interface{}{
			"type": "match_snapshot",
			"data": matchStore.Snapshot(matchIDStr),
		})
		go func() {
			for ev := range matchEvents {
				snapshot := matchStore.Snapshot(matchIDStr)
				writer.SendJSON(map[string]interface{}{
					"type":     "match_event",
					"data":     ev,
					"snapshot": snapshot,
				})
				if strings.TrimSpace(ev.ProactiveText) != "" && ev.Visibility == "public" && ev.Status == "active" {
					go emitProactiveEvent(context.Background(), writer, companionAgent, ttsClient, fallbackString(r.RemoteAddr, "broadcast"), ev, snapshot)
				}
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
						bgCtx := context.Background()
						userID := fallbackString(str(req, "userId"), r.RemoteAddr)
						result, err := handleVoiceSession(bgCtx, companionAgent, asrClient, nil, matchIDStr, userID, text, audioB64, time.Now())
						if err != nil {
							log.Printf("companion voice reply error: %v", err)
							if result.ASRError != "" {
								writer.SendJSON(map[string]interface{}{"type": "voice_status", "state": "failed", "reason": result.ASRError})
							}
							return
						}
						if result.ASRError != "" {
							writer.SendJSON(map[string]interface{}{"type": "voice_status", "state": "text_fallback", "reason": result.ASRError})
						}
						writer.SendJSON(map[string]interface{}{
							"type":  "expression",
							"state": "chat",
						})
						writer.SendJSON(map[string]interface{}{
							"type":  "event",
							"event": "qiuqiu_reply",
							"data": map[string]interface{}{
								"text": result.Reply,
							},
						})
						if ttsClient != nil {
							ttsResult, err := ttsClient.Synthesize(bgCtx, result.Reply, "cgSgspJ2msm6clMCkdW9")
							if err != nil {
								result.TTSError = err.Error()
								recordVoiceTTS(bgCtx, companionAgent, result, "", 0, err.Error())
								writer.SendJSON(map[string]interface{}{"type": "voice_status", "state": "tts_fallback", "reason": err.Error()})
								return
							}
							if len(ttsResult.AudioData) > 0 {
								recordVoiceTTS(bgCtx, companionAgent, result, fallbackString(ttsResult.MimeType, "audio/mpeg"), len(ttsResult.AudioData), "")
								writer.SendJSON(map[string]interface{}{"type": "voice_audio", "mime": fallbackString(ttsResult.MimeType, "audio/mpeg"), "traceId": result.Trace.ID})
								writer.SendBinary(ttsResult.AudioData)
							}
						}
					}()
				case "voice_playback":
					traceID := strings.TrimSpace(str(req, "traceId"))
					state := strings.TrimSpace(str(req, "state"))
					if traceID == "" || state == "" {
						continue
					}
					status := "ok"
					if state != "ended" && state != "started" {
						status = "failed:" + state
					}
					if err := recordPlaybackStatus(context.Background(), traceReader, companionAgent, matchIDStr, traceID, status); err != nil {
						log.Printf("voice playback trace update error: %v", err)
					}
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

func handleMatchAPI(store matchstate.Repository, traceReader companion.TraceReader, demoResetter companion.DemoResetter, cfg *config.Config, llmClient *llm.Client, promptMgr *pipeline.PromptManager) http.HandlerFunc {
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
		case r.Method == http.MethodPost && resource == "reset" && len(parts) == 2:
			if !validAPIToken(r, cfg) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if !isDemoMatchID(matchID) {
				http.Error(w, "reset is only available for local demo match ids", http.StatusBadRequest)
				return
			}
			if err := store.Reset(matchID); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if demoResetter != nil {
				if err := demoResetter.Reset(matchID); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"ok":       true,
				"matchId":  matchID,
				"snapshot": store.Snapshot(matchID),
			})
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

func handleVoiceSession(ctx context.Context, agent *companion.Agent, recognizer speechRecognizer, synthesizer speechSynthesizer, matchID, userID, text, audioB64 string, now time.Time) (voiceSessionResult, error) {
	result := voiceSessionResult{Text: strings.TrimSpace(text)}
	voiceMeta := &companion.VoiceTraceMetadata{}
	if audioB64 != "" {
		audioBytes, err := base64.StdEncoding.DecodeString(audioB64)
		if err != nil {
			result.ASRError = "invalid audio payload"
			voiceMeta.ASRStatus = "failed"
			voiceMeta.ASRError = result.ASRError
		} else if len(audioBytes) > 0 {
			if recognizer == nil {
				result.ASRError = asr.ErrNotConfigured.Error()
				voiceMeta.ASRStatus = "failed"
				voiceMeta.ASRError = result.ASRError
			} else {
				asrResult, err := recognizer.Transcribe(ctx, pcmToWav(audioBytes), nil)
				if err != nil {
					result.ASRError = err.Error()
					voiceMeta.ASRStatus = "failed"
					voiceMeta.ASRError = result.ASRError
				} else if strings.TrimSpace(asrResult.Text) != "" {
					result.Text = strings.TrimSpace(asrResult.Text)
					voiceMeta.ASRStatus = "ok"
					voiceMeta.ASRText = result.Text
					voiceMeta.ASRProvider = asrResult.Provider
					log.Printf("asr: %q (conf=%.2f)", result.Text, asrResult.Confidence)
				} else {
					result.ASRError = "empty voice input"
					voiceMeta.ASRStatus = "failed"
					voiceMeta.ASRError = result.ASRError
				}
			}
		}
	}
	if result.Text == "" {
		if result.ASRError == "" {
			result.ASRError = "empty voice input"
		}
		return result, fmt.Errorf("no usable user text")
	}
	response, err := agent.HandleMessage(ctx, companion.MessageRequest{
		MatchID: matchID,
		UserID:  userID,
		Text:    result.Text,
		Now:     now,
		Voice:   nonEmptyVoiceMeta(voiceMeta),
	})
	if err != nil {
		return result, err
	}
	result.Reply = response.Reply
	result.Trace = response.Trace
	if synthesizer != nil {
		ttsResult, err := synthesizer.Synthesize(ctx, result.Reply, "cgSgspJ2msm6clMCkdW9")
		if err != nil {
			result.TTSError = err.Error()
			result.Trace.Voice = ensureVoiceMeta(result.Trace.Voice)
			result.Trace.Voice.TTSStatus = "failed"
			result.Trace.Voice.TTSError = result.TTSError
			_ = agent.UpdateTrace(ctx, result.Trace)
		} else {
			result.AudioData = ttsResult.AudioData
			result.AudioMIME = ttsResult.MimeType
			result.Trace.Voice = ensureVoiceMeta(result.Trace.Voice)
			result.Trace.Voice.TTSStatus = "ok"
			result.Trace.Voice.TTSMime = result.AudioMIME
			result.Trace.Voice.TTSByteCount = len(result.AudioData)
			_ = agent.UpdateTrace(ctx, result.Trace)
		}
	}
	return result, nil
}

func recordVoiceTTS(ctx context.Context, agent *companion.Agent, result voiceSessionResult, mime string, byteCount int, errText string) {
	if result.Trace.ID == "" {
		return
	}
	trace := result.Trace
	trace.Voice = ensureVoiceMeta(trace.Voice)
	if errText != "" {
		trace.Voice.TTSStatus = "failed"
		trace.Voice.TTSError = errText
	} else {
		trace.Voice.TTSStatus = "ok"
		trace.Voice.TTSMime = mime
		trace.Voice.TTSByteCount = byteCount
	}
	if err := agent.UpdateTrace(ctx, trace); err != nil {
		log.Printf("voice trace update error: %v", err)
	}
}

func emitProactiveEvent(ctx context.Context, writer *wsWriter, agent *companion.Agent, ttsClient *tts.Client, userID string, ev matchstate.MatchEvent, snapshot matchstate.Snapshot) {
	response, err := agent.HandleProactiveEvent(ctx, userID, ev, snapshot)
	if err != nil {
		log.Printf("proactive event error: %v", err)
		return
	}
	writer.SendJSON(map[string]interface{}{
		"type":  "event",
		"event": "qiuqiu_reply",
		"data": map[string]interface{}{
			"text":    response.Reply,
			"traceId": response.Trace.ID,
			"eventId": ev.ID,
			"source":  "operator_proactive",
		},
	})
	if ttsClient == nil {
		return
	}
	ttsResult, err := ttsClient.Synthesize(ctx, response.Reply, "")
	if err != nil {
		trace := response.Trace
		trace.Voice = ensureVoiceMeta(trace.Voice)
		trace.Voice.TTSStatus = "failed"
		trace.Voice.TTSError = err.Error()
		_ = agent.UpdateTrace(ctx, trace)
		writer.SendJSON(map[string]interface{}{"type": "voice_status", "state": "tts_fallback", "reason": err.Error()})
		return
	}
	if len(ttsResult.AudioData) == 0 {
		return
	}
	trace := response.Trace
	trace.Voice = ensureVoiceMeta(trace.Voice)
	trace.Voice.TTSStatus = "ok"
	trace.Voice.TTSMime = fallbackString(ttsResult.MimeType, "audio/mpeg")
	trace.Voice.TTSByteCount = len(ttsResult.AudioData)
	_ = agent.UpdateTrace(ctx, trace)
	writer.SendJSON(map[string]interface{}{"type": "voice_audio", "mime": trace.Voice.TTSMime, "traceId": response.Trace.ID})
	writer.SendBinary(ttsResult.AudioData)
}

func recordPlaybackStatus(ctx context.Context, reader companion.TraceReader, agent *companion.Agent, matchID, traceID, status string) error {
	trace, err := reader.GetTrace(ctx, matchID, traceID)
	if err != nil {
		return err
	}
	trace.Voice = ensureVoiceMeta(trace.Voice)
	trace.Voice.PlaybackStatus = status
	return agent.UpdateTrace(ctx, trace)
}

func ensureVoiceMeta(meta *companion.VoiceTraceMetadata) *companion.VoiceTraceMetadata {
	if meta != nil {
		return meta
	}
	return &companion.VoiceTraceMetadata{}
}

func nonEmptyVoiceMeta(meta *companion.VoiceTraceMetadata) *companion.VoiceTraceMetadata {
	if meta == nil {
		return nil
	}
	if meta.ASRStatus == "" && meta.ASRText == "" && meta.ASRError == "" && meta.TTSStatus == "" && meta.TTSError == "" {
		return nil
	}
	return meta
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

func isDemoMatchID(matchID string) bool {
	matchID = strings.TrimSpace(matchID)
	return matchID == "test" || strings.HasPrefix(matchID, "demo-")
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

func newTextLLMClient(cfg *config.Config) *llm.Client {
	if cfg == nil || strings.TrimSpace(cfg.DeepseekAPIKey) == "" {
		return nil
	}
	return llm.NewClient(cfg.DeepseekBaseURL, cfg.DeepseekAPIKey, cfg.DeepseekModel)
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

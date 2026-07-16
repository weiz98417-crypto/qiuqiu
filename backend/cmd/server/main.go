package main

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"qiuqiu/internal/asr"
	"qiuqiu/internal/auth"
	"qiuqiu/internal/companion"
	"qiuqiu/internal/config"
	"qiuqiu/internal/conversation"
	"qiuqiu/internal/datasource"
	"qiuqiu/internal/llm"
	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/pipeline"
	"qiuqiu/internal/relationship"
	"qiuqiu/internal/tts"
	"qiuqiu/internal/ws"

	"github.com/gorilla/websocket"
	"github.com/joho/godotenv"
)

type speechRecognizer interface {
	Transcribe(ctx context.Context, audio []byte, hints []string) (*asr.Result, error)
}

type speechSynthesizer interface {
	Synthesize(ctx context.Context, text, voiceID string) (*tts.SynthesizeResult, error)
}

type voiceSessionResult struct {
	Text         string
	Reply        string
	Trace        companion.Trace
	Presentation relationship.PresentationPlan
	AudioData    []byte
	AudioMIME    string
	ASRError     string
	TTSError     string
}

func qiuqiuReplyData(text, traceID, source, eventID string, presentation relationship.PresentationPlan) map[string]interface{} {
	data := map[string]interface{}{
		"text":    text,
		"traceId": traceID,
		"source":  source,
	}
	if eventID != "" {
		data["eventId"] = eventID
	}
	if presentation.Expression != "" || presentation.Motion != "" || presentation.VoiceStyle != "" {
		data["presentation"] = presentation
	}
	return data
}

type demoStateResetter struct {
	traces        companion.DemoResetter
	relationships relationship.MatchResetter
}

func (resetter demoStateResetter) Reset(matchID string) error {
	if resetter.traces != nil {
		if err := resetter.traces.Reset(matchID); err != nil {
			return err
		}
	}
	if resetter.relationships != nil {
		return resetter.relationships.ResetMatch(matchID)
	}
	return nil
}

func main() {
	_ = godotenv.Load()
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		log.Fatal(err)
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
	if cfg.MiMoAPIKey != "" {
		ttsClient = tts.NewClient(cfg.MiMoAPIKey).WithBaseURL(cfg.MiMoBaseURL).WithModel("mimo-v2.5-tts").WithVoice(cfg.MiMoVoice)
	}
	asrClient := asr.NewClient(cfg.MiMoAPIKey).WithBaseURL(cfg.MiMoBaseURL).WithModel("mimo-v2.5-asr")

	var sessionStore auth.Store = auth.NewMemoryStore()
	var sessionStoreCloser func()
	if cfg.DatabaseURL != "" {
		postgresSessionStore, err := auth.OpenPostgresStore(context.Background(), cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("postgres session store: %v", err)
		}
		sessionStore = postgresSessionStore
		sessionStoreCloser = postgresSessionStore.Close
	}
	sessionManager, err := auth.NewManager(sessionStore, cfg.SessionSigningKey)
	if err != nil {
		log.Fatalf("session manager: %v", err)
	}
	if sessionStoreCloser != nil {
		defer sessionStoreCloser()
	}
	hub := ws.NewHub(cfg).WithSessionAuthenticator(sessionManager)
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
	var sportsClient datasource.EventsClient
	if cfg.APISportsAPIKey != "" {
		sportsClient = datasource.NewClient(cfg.APISportsAPIKey)
	}
	sourceManager := datasource.NewManager(context.Background(), matchStore, sportsClient, datasource.ManagerConfig{})
	defer sourceManager.Close()
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
		companionTools.WithTraceWriter(traceWriter)
	}
	companionAgent := companion.NewAgent(companionTools)
	var relationshipRepository relationship.StateRepository = relationship.NewMemoryRepository()
	if cfg.DatabaseURL != "" {
		postgresRelationshipRepository, err := relationship.OpenPostgresRepository(context.Background(), cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("relationship repository: %v", err)
		}
		defer postgresRelationshipRepository.Close()
		relationshipRepository = postgresRelationshipRepository
	}
	companionAgent.WithDirector(relationship.NewDirector(relationshipRepository))
	if relationshipResetter, ok := relationshipRepository.(relationship.MatchResetter); ok {
		demoResetter = demoStateResetter{traces: demoResetter, relationships: relationshipResetter}
	}
	if llmClient != nil {
		companionAgent.WithRealizer(companion.NewLLMReplyRealizer(llmClient), 3*time.Second)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", hub.HandleHealth)
	mux.HandleFunc("/api/sessions/", handleSessionAPI(sessionManager, cfg))
	mux.HandleFunc("/api/matches/", handleMatchAPIWithSources(matchStore, traceReader, demoResetter, cfg, llmClient, promptMgr, sourceManager))
	fs := http.StripPrefix("/live2d-assets/", http.FileServer(http.Dir("../client/assets/live2d")))
	mux.HandleFunc("/live2d-assets/", func(w http.ResponseWriter, r *http.Request) {
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
	mux.HandleFunc("/operator.html", func(w http.ResponseWriter, r *http.Request) {
		noCache(w)
		http.ServeFile(w, r, "../client/assets/live2d/operator.html")
	})
	registerDevelopmentPages(mux, cfg.Environment, "../client/assets/live2d")
	webApp := http.FileServer(http.Dir(resolveWebAppDir()))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/app.html" {
			http.Redirect(w, r, "/", http.StatusPermanentRedirect)
			return
		}
		if r.URL.Path == "/" || filepath.Ext(r.URL.Path) == ".html" {
			noCache(w)
		}
		webApp.ServeHTTP(w, r)
	})

	mux.HandleFunc("/ws/match/", func(w http.ResponseWriter, r *http.Request) {
		conn, release, claims, err := hub.UpgradeWithIdentity(w, r)
		if err != nil {
			return
		}
		defer release()
		defer conn.Close()
		writer := &wsWriter{conn: conn}
		identity := newConnectionIdentity(claims.Subject)

		matchIDStr := r.URL.Path[len("/ws/match/"):]
		matchEvents, unsubscribe := matchStore.Subscribe(matchIDStr)
		defer unsubscribe()

		connectionCtx, connectionCancel := context.WithCancel(r.Context())
		defer connectionCancel()
		conversationScheduler := conversation.NewScheduler(connectionCtx, conversation.Config{})
		proactiveGate := conversation.NewProactiveGate()
		deliveryTracker := newReplyDeliveryTracker()
		var userSpeaking atomic.Bool
		var userTurnActive atomic.Bool
		defer func() {
			for _, pending := range deliveryTracker.Drain() {
				cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 3*time.Second)
				if err := observeReplyDelivery(cleanupCtx, companionAgent, pending.Trace, pending.UserID, pending.MatchID, "interrupted", time.Now().UTC()); err != nil {
					log.Printf("relationship interrupted delivery cleanup error: %v", err)
				}
				cleanupCancel()
			}
		}()
		defer conversationScheduler.Close()

		writer.SendJSON(map[string]interface{}{
			"type": "match_snapshot",
			"data": matchStore.PublicSnapshot(matchIDStr),
		})
		go func() {
			for {
				select {
				case <-connectionCtx.Done():
					return
				case ev := <-matchEvents:
					snapshot := matchStore.PublicSnapshot(matchIDStr)
					if !matchstate.IsPublicFact(ev) {
						writer.SendJSON(map[string]interface{}{
							"type": "match_snapshot",
							"data": snapshot,
						})
						continue
					}
					writer.SendJSON(map[string]interface{}{
						"type":     "match_event",
						"data":     ev,
						"snapshot": snapshot,
					})
					if ev.Visibility == "public" && ev.Status == "active" {
						userID := identity.Get()
						if userID == "" {
							userID = identity.Wait(connectionCtx)
							if userID == "" {
								return
							}
						}
						policy := matchStore.Config(matchIDStr).Automation
						critical := proactiveUrgency(ev.EventType) == conversation.UrgencyCritical
						now := time.Now()
						allowed := proactiveGate.Allow(policy, ev.EventType, critical, now)
						if hasEventTag(ev, "proactive=manual") {
							allowed = proactiveGate.AllowManual(now)
						}
						response, err := companionAgent.HandleMatchEvent(connectionCtx, companion.MatchEventRequest{
							UserID:                userID,
							Event:                 ev,
							Snapshot:              snapshot,
							OutputAllowed:         allowed,
							Critical:              critical,
							UserSpeaking:          userSpeaking.Load() || userTurnActive.Load(),
							NormalCooldownSeconds: policy.CooldownSeconds,
							Now:                   now,
						})
						if err != nil {
							log.Printf("match turn planning error: %v", err)
							continue
						}
						if response.Presentation.Expression != "" {
							writer.SendJSON(map[string]interface{}{
								"type":    "presentation",
								"data":    response.Presentation,
								"eventId": ev.ID,
								"source":  "match_reaction",
							})
						}
						if strings.TrimSpace(response.Reply) == "" {
							continue
						}
						urgency, ttl := proactiveSchedule(response.Decision, ev.EventType)
						conversationScheduler.SubmitProactive(ev.ID, urgency, ttl, func(replyCtx context.Context, playback conversation.Playback) {
							emitProactiveResponse(replyCtx, writer, companionAgent, ttsClient, playback, response, ev.ID)
						})
					}
				}
			}
		}()

		// Read loop
		go func() {
			defer connectionCancel()
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
				case "identify":
					requested := strings.TrimSpace(str(req, "userId"))
					if cfg.SessionAuthRequired() && requested != "" && requested != identity.Get() {
						writer.SendJSON(map[string]string{"type": "auth_error", "reason": "user identity does not match session"})
						return
					}
					if cfg.LegacyAuthAllowed() {
						identity.Set(str(req, "userId"))
					}
				case "user_activity":
					userSpeaking.Store(str(req, "state") == "speaking")
				case "session_opened":
					userID, identityMatches := connectionUserID(identity, cfg, str(req, "userId"))
					if !identityMatches {
						writer.SendJSON(map[string]string{"type": "auth_error", "reason": "user identity does not match session"})
						return
					}
					if userID == "" {
						continue
					}
					if _, err := companionAgent.ObserveSession(connectionCtx, "session:"+userID+":"+matchIDStr, userID, matchIDStr, time.Now().UTC()); err != nil {
						log.Printf("relationship session observation error: %v", err)
					}

				case "first_meeting":
					userID, identityMatches := connectionUserID(identity, cfg, str(req, "userId"))
					if !identityMatches {
						writer.SendJSON(map[string]string{"type": "auth_error", "reason": "user identity does not match session"})
						return
					}
					if userID == "" {
						continue
					}
					nickname := str(req, "nickname")
					favoriteTeam := str(req, "favoriteTeam")
					firstMeetingSignalID := fmt.Sprintf("first-meeting:%s:%s:%d", userID, matchIDStr, time.Now().UnixNano())
					conversationScheduler.SubmitProactive("first-meeting:"+userID, conversation.UrgencyNormal, 30*time.Second, func(replyCtx context.Context, playback conversation.Playback) {
						emitFirstMeeting(
							replyCtx,
							writer,
							companionAgent,
							ttsClient,
							playback,
							matchIDStr,
							userID,
							nickname,
							favoriteTeam,
							firstMeetingSignalID,
						)
					})

				case "interrupt":
					conversationScheduler.Interrupt()

					writer.SendJSON(map[string]interface{}{
						"type":       "interrupt",
						"expression": "listening",
					})

				case "user_speech":
					text := str(req, "text")
					audioB64 := str(req, "audio")
					userID, identityMatches := connectionUserID(identity, cfg, str(req, "userId"))
					if !identityMatches {
						writer.SendJSON(map[string]string{"type": "auth_error", "reason": "user identity does not match session"})
						return
					}
					if userID == "" {
						writer.SendJSON(map[string]interface{}{"type": "voice_status", "state": "failed", "reason": "identity required"})
						continue
					}
					generatedSignalID := fmt.Sprintf("turn_%s_%d", userID, time.Now().UnixNano())
					turnSignalID := stableSignalID(str(req, "signalId"), generatedSignalID)
					writer.SendJSON(map[string]interface{}{
						"type":       "interrupt",
						"expression": "listening",
					})
					userTurnActive.Store(true)
					conversationScheduler.SubmitUser(func(replyCtx context.Context, playback conversation.Playback) {
						defer userTurnActive.Store(false)
						result, err := handleVoiceTurnWithFactRefresh(
							func() matchstate.Snapshot { return matchStore.PublicSnapshot(matchIDStr) },
							func(turnText, turnAudio string) (voiceSessionResult, error) {
								return handleVoiceSessionWithSignalID(replyCtx, companionAgent, asrClient, nil, matchIDStr, userID, turnText, turnAudio, time.Now(), turnSignalID)
							},
							text,
							audioB64,
						)
						if err != nil {
							state := "failed"
							if errors.Is(err, context.Canceled) {
								state = "interrupted"
								cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 3*time.Second)
								_ = observeReplyDelivery(cleanupCtx, companionAgent, result.Trace, userID, matchIDStr, state, time.Now().UTC())
								cleanupCancel()
								return
							}
							cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 3*time.Second)
							_ = observeReplyDelivery(cleanupCtx, companionAgent, result.Trace, userID, matchIDStr, state, time.Now().UTC())
							cleanupCancel()
							log.Printf("companion voice reply error: %v", err)
							if result.ASRError != "" {
								writer.SendJSON(map[string]interface{}{"type": "voice_status", "state": "failed", "reason": result.ASRError})
							}
							return
						}
						if replyCtx.Err() != nil {
							cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 3*time.Second)
							_ = observeReplyDelivery(cleanupCtx, companionAgent, result.Trace, userID, matchIDStr, "interrupted", time.Now().UTC())
							cleanupCancel()
							return
						}
						if result.ASRError != "" {
							writer.SendJSON(map[string]interface{}{"type": "voice_status", "state": "text_fallback", "reason": result.ASRError})
						}
						if strings.TrimSpace(result.Reply) == "" {
							cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 3*time.Second)
							_ = observeReplyDelivery(cleanupCtx, companionAgent, result.Trace, userID, matchIDStr, "skipped", time.Now().UTC())
							cleanupCancel()
							return
						}
						deliveryTracker.Track(result.Trace, userID, matchIDStr)
						if err := writer.SendJSON(map[string]interface{}{
							"type":  "event",
							"event": "qiuqiu_reply",
							"data":  qiuqiuReplyData(result.Reply, result.Trace.ID, "conversation", "", result.Presentation),
						}); err != nil {
							deliveryTracker.Remove(result.Trace.ID)
							cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 3*time.Second)
							_ = observeReplyDelivery(cleanupCtx, companionAgent, result.Trace, userID, matchIDStr, "failed", time.Now().UTC())
							cleanupCancel()
							return
						}
						if ttsClient != nil {
							ttsResult, err := ttsClient.Synthesize(replyCtx, result.Reply, "cgSgspJ2msm6clMCkdW9")
							if err != nil {
								if errors.Is(err, context.Canceled) {
									return
								}
								log.Printf("voice reply tts error: %v", err)
								result.TTSError = err.Error()
								recordVoiceTTS(replyCtx, companionAgent, result, "", 0, err.Error())
								writer.SendJSON(map[string]interface{}{"type": "voice_status", "state": "tts_fallback", "reason": err.Error()})
								return
							}
							if len(ttsResult.AudioData) > 0 {
								recordVoiceTTS(replyCtx, companionAgent, result, fallbackString(ttsResult.MimeType, "audio/mpeg"), len(ttsResult.AudioData), "")
								playback(result.Trace.ID)
								writer.SendAudio(map[string]interface{}{"type": "voice_audio", "mime": fallbackString(ttsResult.MimeType, "audio/mpeg"), "traceId": result.Trace.ID, "byteLength": len(ttsResult.AudioData)}, ttsResult.AudioData)
							}
						}
					})
				case "voice_playback":
					traceID := strings.TrimSpace(str(req, "traceId"))
					state := strings.TrimSpace(str(req, "state"))
					if traceID == "" || state == "" {
						continue
					}
					status := playbackTraceStatus(state)
					updateCtx, updateCancel := context.WithTimeout(connectionCtx, 3*time.Second)
					userID := identity.Get()
					err := recordPlaybackStatus(updateCtx, traceReader, companionAgent, matchIDStr, traceID, userID, status)
					if err == nil {
						conversationScheduler.PlaybackChanged(traceID, state)
						if _, relationshipErr := companionAgent.ObserveDelivery(updateCtx, "delivery:"+traceID+":"+state, userID, matchIDStr, "", state, "", nil, time.Now().UTC()); relationshipErr != nil && !errors.Is(relationshipErr, context.Canceled) {
							log.Printf("relationship delivery observation error: %v", relationshipErr)
						}
					}
					updateCancel()
					if err != nil && !errors.Is(err, context.Canceled) {
						log.Printf("voice playback trace update error: %v", err)
					}
				case "reply_displayed":
					traceID := strings.TrimSpace(str(req, "traceId"))
					userID := identity.Get()
					if traceID == "" || userID == "" {
						continue
					}
					updateCtx, updateCancel := context.WithTimeout(connectionCtx, 3*time.Second)
					err := recordDisplayedReply(updateCtx, traceReader, companionAgent, matchIDStr, traceID, userID, time.Now().UTC())
					if err == nil {
						deliveryTracker.Remove(traceID)
					}
					updateCancel()
					if err != nil && !errors.Is(err, context.Canceled) {
						log.Printf("reply display observation error: %v", err)
					}
				}
			}
		}()

		// Heartbeat
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-connectionCtx.Done():
				return
			case <-ticker.C:
				if claims.Subject != "" {
					if err := sessionManager.ValidateClaims(connectionCtx, claims); err != nil {
						writer.SendJSON(map[string]string{"type": "auth_error", "reason": "session expired or revoked"})
						connectionCancel()
						return
					}
				}
				if err := writer.Ping(); err != nil {
					connectionCancel()
					return
				}
			}
		}
	})

	addr := ":" + cfg.Port
	log.Printf("qiuqiu server starting on %s", addr)
	if err := newHTTPServer(addr, mux).ListenAndServe(); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func registerDevelopmentPages(mux *http.ServeMux, environment, assetsDir string) {
	if strings.EqualFold(strings.TrimSpace(environment), "production") {
		return
	}
	for _, name := range []string{"test-expressions.html", "director-prototype.html"} {
		path := "/" + name
		file := filepath.Join(assetsDir, name)
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			noCache(w)
			http.ServeFile(w, r, file)
		})
	}
}

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    1 << 20,
	}
}

func handleMatchAPI(store matchstate.Repository, traceReader companion.TraceReader, demoResetter companion.DemoResetter, cfg *config.Config, llmClient *llm.Client, promptMgr *pipeline.PromptManager) http.HandlerFunc {
	return handleMatchAPIWithSources(store, traceReader, demoResetter, cfg, llmClient, promptMgr, nil)
}

func handleMatchAPIWithSources(store matchstate.Repository, traceReader companion.TraceReader, demoResetter companion.DemoResetter, cfg *config.Config, llmClient *llm.Client, promptMgr *pipeline.PromptManager, sources *datasource.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !applyCORS(w, r, cfg) {
			return
		}
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
			if sources != nil {
				sources.Stop(matchID)
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
				"snapshot": store.PublicSnapshot(matchID),
			})
		case r.Method == http.MethodGet && resource == "sources" && len(parts) == 2:
			if !validAPIToken(r, cfg) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if sources == nil {
				http.Error(w, "source manager unavailable", http.StatusServiceUnavailable)
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"status": sources.Status(matchID)})
		case r.Method == http.MethodPost && resource == "sources" && len(parts) == 3 && parts[2] == "start":
			if !validAPIToken(r, cfg) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if sources == nil {
				http.Error(w, "source manager unavailable", http.StatusServiceUnavailable)
				return
			}
			var sourceConfig datasource.SourceConfig
			if err := json.NewDecoder(r.Body).Decode(&sourceConfig); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			status, err := sources.Start(matchID, sourceConfig)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"status": status})
		case r.Method == http.MethodPost && resource == "sources" && len(parts) == 3 && parts[2] == "stop":
			if !validAPIToken(r, cfg) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if sources == nil {
				http.Error(w, "source manager unavailable", http.StatusServiceUnavailable)
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"status": sources.Stop(matchID)})
		case r.Method == http.MethodPost && resource == "takeover" && len(parts) == 2:
			if !validAPIToken(r, cfg) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if sources == nil {
				http.Error(w, "source manager unavailable", http.StatusServiceUnavailable)
				return
			}
			status := sources.Stop(matchID)
			policy := store.Config(matchID).Automation
			policy.Mode = matchstate.AutomationModePaused
			saved, err := store.SetAutomation(matchID, policy)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"policy": saved,
				"status": status,
			})
		case r.Method == http.MethodGet && resource == "automation" && len(parts) == 2:
			if !validAPIToken(r, cfg) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"policy": store.Config(matchID).Automation})
		case r.Method == http.MethodPost && resource == "automation" && len(parts) == 2:
			if !validAPIToken(r, cfg) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			var policy matchstate.AutomationPolicy
			if err := json.NewDecoder(r.Body).Decode(&policy); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			saved, err := store.SetAutomation(matchID, policy)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"policy": saved})
		case r.Method == http.MethodPost && resource == "facts" && len(parts) == 4:
			operator, authorized := operatorClaims(r, cfg)
			if !authorized {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			var changed matchstate.MatchEvent
			var snapshot matchstate.Snapshot
			var err error
			switch parts[3] {
			case "confirm":
				changed, snapshot, err = store.ConfirmFact(matchID, parts[2], operator.Subject)
			case "revoke":
				changed, snapshot, err = store.RevokeFact(matchID, parts[2], operator.Subject)
			case "reconcile":
				changed, snapshot, err = store.ReconcileFact(matchID, parts[2], operator.Subject)
			default:
				http.NotFound(w, r)
				return
			}
			if err != nil {
				status := http.StatusBadRequest
				if errors.Is(err, matchstate.ErrNotFound) {
					status = http.StatusNotFound
				} else if errors.Is(err, matchstate.ErrConflict) {
					status = http.StatusConflict
				}
				http.Error(w, err.Error(), status)
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"event": changed, "snapshot": snapshot})
		case r.Method == http.MethodGet && resource == "facts" && len(parts) == 4 && parts[3] == "revisions":
			if _, authorized := operatorClaims(r, cfg); !authorized {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"revisions": store.FactRevisions(matchID, parts[2]),
			})
		case r.Method == http.MethodGet && resource == "config" && len(parts) == 2:
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"config":   store.Config(matchID),
				"snapshot": store.PublicSnapshot(matchID),
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
			events := store.PublicEvents(matchID)
			if validAPIToken(r, cfg) {
				events = store.Events(matchID)
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"events": events})
		case r.Method == http.MethodGet && resource == "state" && len(parts) == 2:
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"snapshot": store.PublicSnapshot(matchID),
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
			operator, authorized := operatorClaims(r, cfg)
			if !authorized {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			var ev matchstate.MatchEvent
			if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			ev.OperatorID = operator.Subject
			applyRequestedFactStatus(&ev)
			markProactiveMode(&ev)
			var created matchstate.MatchEvent
			var err error
			if sources != nil {
				created, _, err = sources.Ingest(r.Context(), matchID, ev)
			} else {
				created, _, err = store.Create(matchID, ev)
			}
			if err != nil {
				status := http.StatusBadRequest
				if errors.Is(err, matchstate.ErrNotFound) {
					status = http.StatusNotFound
				} else if errors.Is(err, matchstate.ErrConflict) {
					status = http.StatusConflict
				}
				http.Error(w, err.Error(), status)
				return
			}
			writeJSON(w, http.StatusCreated, map[string]interface{}{
				"event":    created,
				"snapshot": store.PublicSnapshot(matchID),
			})
		case r.Method == http.MethodPost && resource == "events" && len(parts) == 4 && parts[3] == "correct":
			operator, authorized := operatorClaims(r, cfg)
			if !authorized {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			var ev matchstate.MatchEvent
			if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			ev.OperatorID = operator.Subject
			applyRequestedFactStatus(&ev)
			markProactiveMode(&ev)
			corrected, _, err := store.Correct(matchID, parts[2], ev)
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
				"snapshot": store.PublicSnapshot(matchID),
			})
		default:
			http.NotFound(w, r)
		}
	}
}

func handleVoiceSession(ctx context.Context, agent *companion.Agent, recognizer speechRecognizer, synthesizer speechSynthesizer, matchID, userID, text, audioB64 string, now time.Time) (voiceSessionResult, error) {
	return handleVoiceSessionWithSignalID(ctx, agent, recognizer, synthesizer, matchID, userID, text, audioB64, now, "")
}

func handleVoiceSessionWithSignalID(ctx context.Context, agent *companion.Agent, recognizer speechRecognizer, synthesizer speechSynthesizer, matchID, userID, text, audioB64 string, now time.Time, signalID string) (voiceSessionResult, error) {
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
		SignalID: signalID,
		MatchID:  matchID,
		UserID:   userID,
		Text:     result.Text,
		Now:      now,
		Voice:    nonEmptyVoiceMeta(voiceMeta),
	})
	if err != nil {
		return result, err
	}
	result.Reply = response.Reply
	result.Trace = response.Trace
	result.Presentation = response.Presentation
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

func handleVoiceTurnWithFactRefresh(snapshot func() matchstate.Snapshot, generate func(text, audio string) (voiceSessionResult, error), text, audio string) (voiceSessionResult, error) {
	before := latestCriticalFactID(snapshot())
	result, err := generate(text, audio)
	if err != nil {
		return result, err
	}
	after := latestCriticalFactID(snapshot())
	if before == after || result.Text == "" {
		return result, nil
	}
	refreshed, refreshErr := generate(result.Text, "")
	if refreshErr != nil {
		return result, nil
	}
	return refreshed, nil
}

func latestCriticalFactID(snapshot matchstate.Snapshot) string {
	for _, event := range snapshot.KeyEvents {
		if proactiveUrgency(event.EventType) != conversation.UrgencyCritical {
			continue
		}
		if event.ID != "" {
			return event.ID
		}
		return event.EventType + ":" + event.Clock + ":" + event.UpdatedAt
	}
	return ""
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

func emitProactiveResponse(ctx context.Context, writer *wsWriter, agent *companion.Agent, ttsClient *tts.Client, playback conversation.Playback, response companion.ProactiveResponse, eventID string) {
	if !replyContextActive(ctx) {
		return
	}
	if err := writer.SendJSON(map[string]interface{}{
		"type":  "event",
		"event": "qiuqiu_reply",
		"data":  qiuqiuReplyData(response.Reply, response.Trace.ID, "match_reaction", eventID, response.Presentation),
	}); err != nil {
		return
	}
	if ttsClient == nil {
		writer.SendJSON(map[string]interface{}{"type": "voice_status", "state": "tts_fallback", "reason": "tts unavailable"})
		return
	}
	ttsResult, err := ttsClient.Synthesize(ctx, response.Reply, "")
	if err != nil {
		if !replyContextActive(ctx) {
			return
		}
		trace := response.Trace
		trace.Voice = ensureVoiceMeta(trace.Voice)
		trace.Voice.TTSStatus = "failed"
		trace.Voice.TTSError = err.Error()
		_ = agent.UpdateTrace(ctx, trace)
		writer.SendJSON(map[string]interface{}{"type": "voice_status", "state": "tts_fallback", "reason": err.Error()})
		return
	}
	if len(ttsResult.AudioData) == 0 {
		writer.SendJSON(map[string]interface{}{"type": "voice_status", "state": "tts_fallback", "reason": "empty audio"})
		return
	}
	if !replyContextActive(ctx) {
		return
	}
	trace := response.Trace
	trace.Voice = ensureVoiceMeta(trace.Voice)
	trace.Voice.TTSStatus = "ok"
	trace.Voice.TTSMime = fallbackString(ttsResult.MimeType, "audio/mpeg")
	trace.Voice.TTSByteCount = len(ttsResult.AudioData)
	_ = agent.UpdateTrace(ctx, trace)
	playback(response.Trace.ID)
	writer.SendAudio(map[string]interface{}{"type": "voice_audio", "mime": trace.Voice.TTSMime, "traceId": response.Trace.ID, "byteLength": len(ttsResult.AudioData)}, ttsResult.AudioData)
}

func emitFirstMeeting(ctx context.Context, writer *wsWriter, agent *companion.Agent, ttsClient *tts.Client, playback conversation.Playback, matchID, userID, nickname, favoriteTeam, signalID string) {
	response, err := agent.HandleFirstMeeting(ctx, companion.FirstMeetingRequest{
		SignalID:     signalID,
		MatchID:      matchID,
		UserID:       userID,
		Nickname:     nickname,
		FavoriteTeam: favoriteTeam,
		Now:          time.Now(),
	})
	if err != nil {
		if !replyContextActive(ctx) {
			return
		}
		log.Printf("first meeting greeting error: %v", err)
		writer.SendJSON(map[string]interface{}{"type": "voice_status", "state": "failed", "reason": "greeting unavailable"})
		return
	}
	if strings.TrimSpace(response.Reply) == "" {
		return
	}
	if !replyContextActive(ctx) {
		return
	}
	if err := writer.SendJSON(map[string]interface{}{
		"type":  "event",
		"event": "qiuqiu_reply",
		"data":  qiuqiuReplyData(response.Reply, response.Trace.ID, "first_meeting", "", response.Presentation),
	}); err != nil {
		return
	}
	if ttsClient == nil {
		writer.SendJSON(map[string]interface{}{"type": "voice_status", "state": "tts_fallback", "reason": "tts unavailable"})
		return
	}
	ttsResult, err := ttsClient.Synthesize(ctx, response.Reply, "")
	if err != nil {
		if !replyContextActive(ctx) {
			return
		}
		trace := response.Trace
		trace.Voice = ensureVoiceMeta(trace.Voice)
		trace.Voice.TTSStatus = "failed"
		trace.Voice.TTSError = err.Error()
		_ = agent.UpdateTrace(ctx, trace)
		writer.SendJSON(map[string]interface{}{"type": "voice_status", "state": "tts_fallback", "reason": err.Error()})
		return
	}
	if len(ttsResult.AudioData) == 0 {
		writer.SendJSON(map[string]interface{}{"type": "voice_status", "state": "tts_fallback", "reason": "empty audio"})
		return
	}
	if !replyContextActive(ctx) {
		return
	}
	trace := response.Trace
	trace.Voice = ensureVoiceMeta(trace.Voice)
	trace.Voice.TTSStatus = "ok"
	trace.Voice.TTSMime = fallbackString(ttsResult.MimeType, "audio/mpeg")
	trace.Voice.TTSByteCount = len(ttsResult.AudioData)
	_ = agent.UpdateTrace(ctx, trace)
	playback(response.Trace.ID)
	writer.SendAudio(map[string]interface{}{"type": "voice_audio", "mime": trace.Voice.TTSMime, "traceId": response.Trace.ID, "byteLength": len(ttsResult.AudioData)}, ttsResult.AudioData)
}

func replyContextActive(ctx context.Context) bool {
	return ctx != nil && ctx.Err() == nil
}

func proactiveUrgency(eventType string) conversation.Urgency {
	switch eventType {
	case "goal", "red_card", "penalty", "penalty_awarded", "var_check", "var_result", "goal_cancelled", "halftime", "fulltime", "match_end":
		return conversation.UrgencyCritical
	default:
		return conversation.UrgencyNormal
	}
}

func proactiveTTL(eventType string) time.Duration {
	if proactiveUrgency(eventType) == conversation.UrgencyCritical {
		return 45 * time.Second
	}
	return 12 * time.Second
}

func proactiveSchedule(decision relationship.Decision, eventType string) (conversation.Urgency, time.Duration) {
	urgency := proactiveUrgency(eventType)
	ttl := proactiveTTL(eventType)
	if decision.Speech == nil {
		return urgency, ttl
	}
	if decision.Speech.Delivery.Urgency == "critical" {
		urgency = conversation.UrgencyCritical
	} else if decision.Speech.Delivery.Urgency == "normal" {
		urgency = conversation.UrgencyNormal
	}
	if decision.Speech.Delivery.TTLSeconds > 0 {
		ttl = time.Duration(decision.Speech.Delivery.TTLSeconds) * time.Second
	}
	return urgency, ttl
}

func recordPlaybackStatus(ctx context.Context, reader companion.TraceReader, agent *companion.Agent, matchID, traceID, userID, status string) error {
	trace, err := reader.GetTrace(ctx, matchID, traceID)
	if err != nil {
		return err
	}
	if userID == "" || trace.UserID != userID {
		return fmt.Errorf("playback trace owner mismatch")
	}
	trace.Voice = ensureVoiceMeta(trace.Voice)
	trace.Voice.PlaybackStatus = status
	return agent.UpdateTrace(ctx, trace)
}

func recordDisplayedReply(ctx context.Context, reader companion.TraceReader, agent *companion.Agent, matchID, traceID, userID string, now time.Time) error {
	trace, err := reader.GetTrace(ctx, matchID, traceID)
	if err != nil {
		return err
	}
	if userID == "" || trace.UserID != userID {
		return fmt.Errorf("displayed reply owner mismatch")
	}
	purpose := "user_reply"
	if trace.Input == "first_meeting" {
		purpose = "first_meeting"
	} else if trace.Reason == "operator_event_proactive_line" || trace.Reason == "relationship_match_reaction" {
		purpose = "match_reaction"
	}
	decisionID := ""
	var usedMemoryIDs []string
	if trace.RelationshipDecision != nil {
		decisionID = trace.RelationshipDecision.ID
		usedMemoryIDs = trace.RelationshipDecision.UsedMemoryIDs
	}
	_, err = agent.ObserveDelivery(ctx, "delivery:"+traceID+":text", userID, matchID, decisionID, "text_delivered", purpose, usedMemoryIDs, now)
	return err
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
	_, ok := operatorClaims(r, cfg)
	return ok
}

func operatorClaims(r *http.Request, cfg *config.Config) (auth.Claims, bool) {
	if cfg == nil {
		return auth.Claims{}, false
	}
	if cfg.AppToken == "" && !strings.EqualFold(cfg.Environment, "production") {
		return auth.Claims{
			Subject: "operator:development",
			Scopes: []string{
				auth.ScopeOperatorMatchWrite,
				auth.ScopeOperatorFactConfirm,
				auth.ScopeOperatorFactCorrect,
				auth.ScopeOperatorTraceRead,
			},
		}, true
	}
	token := auth.BearerToken(r.Header.Get("Authorization"))
	if token == "" || len(token) != len(cfg.AppToken) || subtle.ConstantTimeCompare([]byte(token), []byte(cfg.AppToken)) != 1 {
		return auth.Claims{}, false
	}
	return auth.Claims{
		Subject: "operator:default",
		Scopes: []string{
			auth.ScopeOperatorMatchWrite,
			auth.ScopeOperatorFactConfirm,
			auth.ScopeOperatorFactCorrect,
			auth.ScopeOperatorTraceRead,
		},
	}, true
}

func applyCORS(w http.ResponseWriter, r *http.Request, cfg *config.Config) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin != "" {
		if !cfg.OriginAllowedForHost(origin, r.Host) {
			http.Error(w, "origin not allowed", http.StatusForbidden)
			return false
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
	}
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	return true
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

func resolveWebAppDir() string {
	candidates := []string{
		strings.TrimSpace(os.Getenv("QIUQIU_WEB_DIR")),
		"../client/build/web",
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if info, err := os.Stat(filepath.Join(candidate, "index.html")); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return "../client/build/web"
}

func playbackTraceStatus(state string) string {
	switch state {
	case "started", "ended":
		return "ok"
	case "skipped":
		return "skipped"
	case "blocked":
		return "blocked:autoplay"
	case "interrupted":
		return "interrupted"
	default:
		return "failed:" + state
	}
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
	if cfg == nil || strings.TrimSpace(cfg.MiMoAPIKey) == "" {
		return nil
	}
	return llm.NewClient(cfg.MiMoBaseURL, cfg.MiMoAPIKey, cfg.MiMoModel)
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

func markProactiveMode(event *matchstate.MatchEvent) {
	event.Tags = removeTagPrefix(event.Tags, "proactive=")
	if event.ProactiveText == "__quiet__" {
		event.ProactiveText = ""
		event.Tags = append(event.Tags, "proactive=quiet")
		return
	}
	if strings.TrimSpace(event.ProactiveText) == "" {
		event.ProactiveText = fallbackProactiveText(*event)
		event.Tags = append(event.Tags, "proactive=auto")
		return
	}
	event.Tags = append(event.Tags, "proactive=manual")
}

func applyRequestedFactStatus(event *matchstate.MatchEvent) {
	if event == nil || event.FactStatus != "" {
		return
	}
	if event.Confirmed {
		event.FactStatus = matchstate.FactStatusConfirmed
	}
}

func removeTagPrefix(tags []string, prefix string) []string {
	filtered := make([]string, 0, len(tags))
	for _, tag := range tags {
		if strings.HasPrefix(tag, prefix) {
			continue
		}
		filtered = append(filtered, tag)
	}
	return filtered
}

func hasEventTag(event matchstate.MatchEvent, tag string) bool {
	for _, candidate := range event.Tags {
		if candidate == tag {
			return true
		}
	}
	return false
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

func (w *wsWriter) SendJSON(msg interface{}) error {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("ws marshal error: %v", err)
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := w.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		log.Printf("ws write error: %v", err)
		return err
	}
	return nil
}

func (w *wsWriter) SendBinary(data []byte) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := w.conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
		log.Printf("ws binary write error: %v", err)
	}
}

func (w *wsWriter) SendAudio(meta interface{}, data []byte) {
	encoded, err := json.Marshal(meta)
	if err != nil {
		log.Printf("ws audio metadata marshal error: %v", err)
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := w.conn.WriteMessage(websocket.TextMessage, encoded); err != nil {
		log.Printf("ws audio metadata write error: %v", err)
		return
	}
	w.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := w.conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
		log.Printf("ws audio write error: %v", err)
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
	le32(wav[24:28], 16000) // sample rate
	le32(wav[28:32], 32000) // byte rate (16000 * 2)
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

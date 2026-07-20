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
	"qiuqiu/internal/directordraft"
	"qiuqiu/internal/llm"
	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/observation"
	"qiuqiu/internal/operatorwrite"
	"qiuqiu/internal/pipeline"
	"qiuqiu/internal/privacy"
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

func qiuqiuReplyData(text, traceID, source, eventID, deliveryKey string, presentation relationship.PresentationPlan) map[string]interface{} {
	data := map[string]interface{}{
		"text":    text,
		"traceId": traceID,
		"source":  source,
	}
	if eventID != "" {
		data["eventId"] = eventID
	}
	if deliveryKey != "" {
		data["deliveryKey"] = deliveryKey
	}
	if presentation.Expression != "" || presentation.Motion != "" || presentation.VoiceStyle != "" {
		data["presentation"] = presentation
	}
	return data
}

type demoStateResetter struct {
	traces        companion.DemoResetter
	relationships relationship.MatchResetter
	observations  observation.MatchResetter
}

func (resetter demoStateResetter) Reset(matchID string) error {
	if resetter.traces != nil {
		if err := resetter.traces.Reset(matchID); err != nil {
			return err
		}
	}
	if resetter.relationships != nil {
		if err := resetter.relationships.ResetMatch(matchID); err != nil {
			return err
		}
	}
	if resetter.observations != nil {
		return resetter.observations.ResetMatch(context.Background(), matchID)
	}
	return nil
}

func main() {
	_ = godotenv.Load()
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		log.Fatal(err)
	}
	privacy.SetRetentionDays(cfg.PrivacyRetentionDays)

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
	directorDrafts := directordraft.NewService(asrClient, directordraft.NewLLMExtractor(llmClient))

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
	outboxCtx, outboxCancel := context.WithCancel(context.Background())
	defer outboxCancel()
	outboxRunner, _ := matchStore.(matchstate.OutboxRunner)
	var privacyStore privacy.Store = privacy.NewMemoryStore()
	var privacyStoreCloser func()
	if cfg.DatabaseURL != "" {
		postgresPrivacyStore, err := privacy.OpenPostgresStore(context.Background(), cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("postgres privacy store: %v", err)
		}
		privacyStore = postgresPrivacyStore
		privacyStoreCloser = postgresPrivacyStore.Close
	}
	if privacyStoreCloser != nil {
		defer privacyStoreCloser()
	}
	privacyService := privacy.NewService(privacyStore)
	cleanupCtx, cleanupCancel := context.WithCancel(context.Background())
	defer cleanupCancel()
	go runPrivacyCleanup(cleanupCtx, privacyService)
	operatorWrites := operatorwrite.NewMemoryService()
	if cfg.DatabaseURL != "" {
		postgresOperatorWrites, err := operatorwrite.OpenPostgresService(context.Background(), cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("postgres operator idempotency store: %v", err)
		}
		operatorWrites = postgresOperatorWrites
	}
	defer operatorWrites.Close()
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
	var observationCoordinator observation.Coordinator
	if cfg.PendingObservationCoordination {
		observationCoordinator = observation.NewMemoryCoordinator()
		if cfg.DatabaseURL != "" {
			postgresObservationCoordinator, err := observation.OpenPostgresCoordinator(context.Background(), cfg.DatabaseURL)
			if err != nil {
				log.Fatalf("postgres observation coordinator: %v", err)
			}
			defer postgresObservationCoordinator.Close()
			observationCoordinator = postgresObservationCoordinator
		}
		go runObservationExpiry(cleanupCtx, observationCoordinator)
	}
	companionAgent := companion.NewAgent(companionTools)
	if observationCoordinator != nil {
		companionAgent.WithObservationCoordinator(observationCoordinator)
		companionAgent.WithObservationReconcileWindow(func(matchID, eventType string) time.Duration {
			return sourceManager.ObservationReconcileWindow(matchID, observation.DefaultReconcileWindow(eventType))
		})
	}
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
	relationshipResetter, _ := relationshipRepository.(relationship.MatchResetter)
	observationResetter, _ := observationCoordinator.(observation.MatchResetter)
	demoResetter = demoStateResetter{traces: demoResetter, relationships: relationshipResetter, observations: observationResetter}
	if llmClient != nil {
		companionAgent.WithRealizer(companion.NewLLMReplyRealizer(llmClient), 3*time.Second)
	}
	if registrar, ok := matchStore.(matchstate.EventObserverRegistrar); ok && observationCoordinator != nil {
		registrar.SetEventObserver(func(event matchstate.MatchEvent) error {
			observationCtx, observationCancel := context.WithTimeout(cleanupCtx, 5*time.Second)
			defer observationCancel()
			_, err := companionAgent.HandleObservationFactChanged(observationCtx, event, time.Now().UTC())
			return err
		})
	}
	if outboxRunner != nil {
		go outboxRunner.RunOutbox(outboxCtx)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", hub.HandleHealth)
	mux.HandleFunc("/api/sessions/", handleSessionAPI(sessionManager, cfg))
	mux.HandleFunc("/api/me/", handlePrivacyAPI(sessionManager, cfg, privacyService))
	mux.HandleFunc("/api/matches/", handleMatchAPIWithDirectorDraft(matchStore, traceReader, demoResetter, cfg, llmClient, promptMgr, sourceManager, directorDrafts, operatorWrites))
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
		var clockUpdates <-chan matchstate.MatchClock
		if clockStore, ok := matchStore.(matchstate.ClockRepository); ok {
			var unsubscribeClock func()
			clockUpdates, unsubscribeClock = clockStore.SubscribeClock(matchIDStr)
			defer unsubscribeClock()
		}

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
		scheduleRecoveredObservations := func(userID string) int {
			now := time.Now().UTC()
			responses, err := companionAgent.RecoverObservationFollowUps(connectionCtx, userID, matchIDStr, now)
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					log.Printf("observation recovery error: %v", err)
				}
				return 0
			}
			followUps := observationFollowUpsForUser(responses, userID, now)
			for _, followUp := range followUps {
				followUp := followUp
				ttl := time.Until(followUp.Resolution.FollowUpDeadline)
				conversationScheduler.SubmitProactive(followUp.Resolution.DeliveryKey, conversation.UrgencyCritical, ttl, func(replyCtx context.Context, playback conversation.Playback) {
					emitObservationResponse(replyCtx, writer, companionAgent, ttsClient, playback, followUp, followUp.Resolution.FactID)
				})
			}
			return len(followUps)
		}

		writer.SendJSON(map[string]interface{}{
			"type": "match_snapshot",
			"data": matchStore.PublicSnapshot(matchIDStr),
		})
		if clockStore, ok := matchStore.(matchstate.ClockRepository); ok {
			writer.SendJSON(map[string]interface{}{
				"type": "match_clock",
				"data": clockStore.Clock(matchIDStr),
			})
		}
		if userID := identity.Get(); userID != "" {
			scheduleRecoveredObservations(userID)
		}
		go func() {
			deliveredEventKeys := make(map[string]struct{})
			deliveredEventOrder := make([]string, 0, 512)
			for {
				select {
				case <-connectionCtx.Done():
					return
				case clock := <-clockUpdates:
					writer.SendJSON(map[string]interface{}{
						"type": "match_clock",
						"data": clock,
					})
					writer.SendJSON(map[string]interface{}{
						"type": "match_snapshot",
						"data": matchStore.PublicSnapshot(matchIDStr),
					})
				case ev := <-matchEvents:
					eventKey := matchstate.DeliveryKey(ev)
					if _, duplicate := deliveredEventKeys[eventKey]; duplicate {
						continue
					}
					deliveredEventKeys[eventKey] = struct{}{}
					deliveredEventOrder = append(deliveredEventOrder, eventKey)
					if len(deliveredEventOrder) > 512 {
						delete(deliveredEventKeys, deliveredEventOrder[0])
						deliveredEventOrder = deliveredEventOrder[1:]
					}
					snapshot := matchStore.PublicSnapshot(matchIDStr)
					userID := identity.Get()
					followUpCount := scheduleRecoveredObservations(userID)
					if !matchstate.IsPublicFact(ev) {
						writer.SendJSON(map[string]interface{}{
							"type": "match_snapshot",
							"data": snapshot,
						})
						continue
					}
					writer.SendJSON(map[string]interface{}{
						"type":        "match_event",
						"data":        ev,
						"snapshot":    snapshot,
						"deliveryKey": eventKey,
					})
					if ev.Visibility == "public" && ev.Status == "active" {
						if followUpCount > 0 {
							continue
						}
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
						if hasEventTag(ev, "proactive=quiet") {
							allowed = false
						} else if hasEventTag(ev, "proactive=manual") {
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
								"type":        "presentation",
								"data":        response.Presentation,
								"eventId":     ev.ID,
								"deliveryKey": eventKey,
								"source":      "match_reaction",
							})
						}
						if strings.TrimSpace(response.Reply) == "" {
							continue
						}
						urgency, ttl := proactiveSchedule(response.Decision, ev.EventType)
						conversationScheduler.SubmitProactive(eventKey, urgency, ttl, func(replyCtx context.Context, playback conversation.Playback) {
							emitProactiveResponse(replyCtx, writer, companionAgent, ttsClient, playback, response, ev.ID, eventKey)
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
						scheduleRecoveredObservations(identity.Get())
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
					scheduleRecoveredObservations(userID)

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
							func(observationID string) bool {
								if err := companionAgent.SuppressObservationFollowUp(replyCtx, observationID, time.Now().UTC()); err != nil {
									log.Printf("observation in-band suppression error: %v", err)
									return false
								}
								return true
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
						if result.Trace.Observation != nil {
							_ = companionAgent.UpdateTrace(replyCtx, result.Trace)
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
							"data":  qiuqiuReplyData(result.Reply, result.Trace.ID, "conversation", "", "", result.Presentation),
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

func observationFollowUpsForUser(responses []companion.ObservationResponse, userID string, now time.Time) []companion.ObservationResponse {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil
	}
	selected := make([]companion.ObservationResponse, 0, len(responses))
	for _, response := range responses {
		if response.Resolution.UserID != userID {
			continue
		}
		if !response.Resolution.FollowUpDeadline.IsZero() && now.After(response.Resolution.FollowUpDeadline) {
			continue
		}
		selected = append(selected, response)
	}
	return selected
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

func runPrivacyCleanup(ctx context.Context, service *privacy.Service) {
	cleanup := func() {
		cleanupCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if err := service.CleanupExpired(cleanupCtx); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("privacy cleanup error: %v", err)
		}
	}
	cleanup()
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cleanup()
		}
	}
}

func runObservationExpiry(ctx context.Context, coordinator observation.Coordinator) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if _, err := coordinator.Expire(ctx, now.UTC()); err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("observation expiry error: %v", err)
			}
		}
	}
}

func handleMatchAPI(store matchstate.Repository, traceReader companion.TraceReader, demoResetter companion.DemoResetter, cfg *config.Config, llmClient *llm.Client, promptMgr *pipeline.PromptManager) http.HandlerFunc {
	return handleMatchAPIWithSources(store, traceReader, demoResetter, cfg, llmClient, promptMgr, nil)
}

func handleMatchAPIWithSources(store matchstate.Repository, traceReader companion.TraceReader, demoResetter companion.DemoResetter, cfg *config.Config, llmClient *llm.Client, promptMgr *pipeline.PromptManager, sources *datasource.Manager, writeServices ...*operatorwrite.Service) http.HandlerFunc {
	return handleMatchAPIWithDirectorDraft(store, traceReader, demoResetter, cfg, llmClient, promptMgr, sources, nil, writeServices...)
}

func handleMatchAPIWithDirectorDraft(store matchstate.Repository, traceReader companion.TraceReader, demoResetter companion.DemoResetter, cfg *config.Config, llmClient *llm.Client, promptMgr *pipeline.PromptManager, sources *datasource.Manager, directorDrafts *directordraft.Service, writeServices ...*operatorwrite.Service) http.HandlerFunc {
	operatorWrites := selectedOperatorWriteService(writeServices)
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
			executeOperatorWrite(w, r, operatorWrites, matchID, "match.reset", []byte("{}"), func(_ context.Context) (operatorwrite.Response, error) {
				if sources != nil {
					sources.Stop(matchID)
				}
				if err := store.Reset(matchID); err != nil {
					return operatorwrite.Response{}, operatorError(http.StatusBadRequest, err)
				}
				if demoResetter != nil {
					if err := demoResetter.Reset(matchID); err != nil {
						return operatorwrite.Response{}, operatorError(http.StatusBadRequest, err)
					}
				}
				return operatorwrite.JSONResponse(http.StatusOK, map[string]interface{}{
					"ok":       true,
					"matchId":  matchID,
					"snapshot": store.PublicSnapshot(matchID),
				})
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
			body, err := decodeOperatorJSON(w, r, &sourceConfig)
			if err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			executeOperatorWrite(w, r, operatorWrites, matchID, "sources.start", body, func(_ context.Context) (operatorwrite.Response, error) {
				status, err := sources.Start(matchID, sourceConfig)
				if err != nil {
					return operatorwrite.Response{}, operatorError(http.StatusBadRequest, err)
				}
				return operatorwrite.JSONResponse(http.StatusOK, map[string]interface{}{"status": status})
			})
		case r.Method == http.MethodPost && resource == "sources" && len(parts) == 3 && parts[2] == "stop":
			if !validAPIToken(r, cfg) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if sources == nil {
				http.Error(w, "source manager unavailable", http.StatusServiceUnavailable)
				return
			}
			executeOperatorWrite(w, r, operatorWrites, matchID, "sources.stop", []byte("{}"), func(_ context.Context) (operatorwrite.Response, error) {
				return operatorwrite.JSONResponse(http.StatusOK, map[string]interface{}{"status": sources.Stop(matchID)})
			})
		case r.Method == http.MethodPost && resource == "takeover" && len(parts) == 2:
			if !validAPIToken(r, cfg) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if sources == nil {
				http.Error(w, "source manager unavailable", http.StatusServiceUnavailable)
				return
			}
			executeOperatorWrite(w, r, operatorWrites, matchID, "match.takeover", []byte("{}"), func(_ context.Context) (operatorwrite.Response, error) {
				status := sources.Stop(matchID)
				policy := store.Config(matchID).Automation
				policy.Mode = matchstate.AutomationModePaused
				saved, err := store.SetAutomation(matchID, policy)
				if err != nil {
					return operatorwrite.Response{}, operatorError(http.StatusBadRequest, err)
				}
				return operatorwrite.JSONResponse(http.StatusOK, map[string]interface{}{
					"policy": saved,
					"status": status,
				})
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
			body, err := decodeOperatorJSON(w, r, &policy)
			if err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			executeOperatorWrite(w, r, operatorWrites, matchID, "automation.set", body, func(_ context.Context) (operatorwrite.Response, error) {
				saved, err := store.SetAutomation(matchID, policy)
				if err != nil {
					return operatorwrite.Response{}, operatorError(http.StatusBadRequest, err)
				}
				return operatorwrite.JSONResponse(http.StatusOK, map[string]interface{}{"policy": saved})
			})
		case r.Method == http.MethodGet && resource == "clock" && len(parts) == 2:
			clockStore, ok := store.(matchstate.ClockRepository)
			if !ok {
				http.Error(w, "match clock unavailable", http.StatusNotImplemented)
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"clock":    clockStore.Clock(matchID),
				"snapshot": store.PublicSnapshot(matchID),
			})
		case r.Method == http.MethodPatch && resource == "clock" && len(parts) == 2:
			if _, authorized := operatorClaims(r, cfg); !authorized {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			clockStore, ok := store.(matchstate.ClockRepository)
			if !ok {
				http.Error(w, "match clock unavailable", http.StatusNotImplemented)
				return
			}
			var command matchstate.ClockCommand
			body, err := decodeOperatorJSON(w, r, &command)
			if err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			command.Source = "operator"
			executeOperatorWrite(w, r, operatorWrites, matchID, "match.clock", body, func(_ context.Context) (operatorwrite.Response, error) {
				clock, err := clockStore.SetClock(matchID, command)
				if err != nil {
					status := http.StatusBadRequest
					if errors.Is(err, matchstate.ErrClockVersionConflict) {
						status = http.StatusConflict
					}
					return operatorwrite.Response{}, operatorError(status, err)
				}
				return operatorwrite.JSONResponse(http.StatusOK, map[string]interface{}{
					"clock":    clock,
					"snapshot": store.PublicSnapshot(matchID),
				})
			})
		case r.Method == http.MethodPost && resource == "drafts" && len(parts) == 3 && parts[2] == "voice":
			if _, authorized := operatorClaims(r, cfg); !authorized {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if directorDrafts == nil {
				http.Error(w, "director voice draft unavailable", http.StatusServiceUnavailable)
				return
			}
			clockStore, ok := store.(matchstate.ClockRepository)
			if !ok {
				http.Error(w, "match clock unavailable", http.StatusNotImplemented)
				return
			}
			var request directordraft.Request
			body, err := decodeOperatorJSON(w, r, &request)
			if err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			executeOperatorWrite(w, r, operatorWrites, matchID, "drafts.voice", body, func(operationCtx context.Context) (operatorwrite.Response, error) {
				draftCtx, cancel := context.WithTimeout(operationCtx, 45*time.Second)
				defer cancel()
				result, err := directorDrafts.Build(draftCtx, request, directordraft.MatchContext{
					MatchID: matchID,
					Config:  store.Config(matchID),
					Clock:   clockStore.Clock(matchID),
				})
				if err != nil {
					status := http.StatusBadGateway
					if errors.Is(err, directordraft.ErrNoInput) {
						status = http.StatusBadRequest
					} else if errors.Is(err, directordraft.ErrNotConfigured) || errors.Is(err, asr.ErrNotConfigured) {
						status = http.StatusServiceUnavailable
					}
					return operatorwrite.Response{}, operatorError(status, err)
				}
				return operatorwrite.JSONResponse(http.StatusOK, result)
			})
		case r.Method == http.MethodPost && resource == "facts" && len(parts) == 4:
			operator, authorized := operatorClaims(r, cfg)
			if !authorized {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			action := parts[3]
			if action != "confirm" && action != "revoke" && action != "reconcile" {
				http.NotFound(w, r)
				return
			}
			executeOperatorWrite(w, r, operatorWrites, matchID, "facts."+action, []byte("{}"), func(operationCtx context.Context) (operatorwrite.Response, error) {
				var changed matchstate.MatchEvent
				var snapshot matchstate.Snapshot
				var err error
				transactionalStore, transactional := store.(matchstate.OperatorTransactionRepository)
				switch action {
				case "confirm":
					if transactional {
						changed, snapshot, err = transactionalStore.ConfirmFactOperator(operationCtx, matchID, parts[2], operator.Subject)
					} else {
						changed, snapshot, err = store.ConfirmFact(matchID, parts[2], operator.Subject)
					}
				case "revoke":
					if transactional {
						changed, snapshot, err = transactionalStore.RevokeFactOperator(operationCtx, matchID, parts[2], operator.Subject)
					} else {
						changed, snapshot, err = store.RevokeFact(matchID, parts[2], operator.Subject)
					}
				case "reconcile":
					if transactional {
						changed, snapshot, err = transactionalStore.ReconcileFactOperator(operationCtx, matchID, parts[2], operator.Subject)
					} else {
						changed, snapshot, err = store.ReconcileFact(matchID, parts[2], operator.Subject)
					}
				}
				if err != nil {
					status := http.StatusBadRequest
					if errors.Is(err, matchstate.ErrNotFound) {
						status = http.StatusNotFound
					} else if errors.Is(err, matchstate.ErrConflict) {
						status = http.StatusConflict
					}
					return operatorwrite.Response{}, operatorError(status, err)
				}
				return operatorwrite.JSONResponse(http.StatusOK, map[string]interface{}{"event": changed, "snapshot": snapshot})
			})
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
			body, err := decodeOperatorJSON(w, r, &config)
			if err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			executeOperatorWrite(w, r, operatorWrites, matchID, "config.set", body, func(_ context.Context) (operatorwrite.Response, error) {
				saved, _, err := store.SetConfig(matchID, config)
				if err != nil {
					return operatorwrite.Response{}, operatorError(http.StatusBadRequest, err)
				}
				return operatorwrite.JSONResponse(http.StatusOK, map[string]interface{}{
					"config":   saved,
					"snapshot": store.PublicSnapshot(matchID),
				})
			})
		case r.Method == http.MethodGet && resource == "events" && len(parts) == 2:
			events := store.PublicEvents(matchID)
			if validAPIToken(r, cfg) {
				events = store.Events(matchID)
			}
			if events == nil {
				events = []matchstate.MatchEvent{}
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
			body, err := decodeOperatorJSON(w, r, &ev)
			if err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			ev.OperatorID = operator.Subject
			applyRequestedFactStatus(&ev)
			markProactiveMode(&ev)
			executeOperatorWrite(w, r, operatorWrites, matchID, "events.create", body, func(operationCtx context.Context) (operatorwrite.Response, error) {
				var created matchstate.MatchEvent
				var snapshot matchstate.Snapshot
				var err error
				if sources != nil {
					created, snapshot, err = sources.Ingest(operationCtx, matchID, ev)
				} else {
					if transactionalStore, ok := store.(matchstate.OperatorTransactionRepository); ok {
						created, snapshot, err = transactionalStore.CreateOperator(operationCtx, matchID, ev)
					} else {
						created, snapshot, err = store.Create(matchID, ev)
					}
				}
				if err != nil {
					status := http.StatusBadRequest
					if errors.Is(err, matchstate.ErrNotFound) {
						status = http.StatusNotFound
					} else if errors.Is(err, matchstate.ErrConflict) {
						status = http.StatusConflict
					}
					return operatorwrite.Response{}, operatorError(status, err)
				}
				if transactionalStore, ok := store.(matchstate.OperatorTransactionRepository); ok {
					snapshot, err = transactionalStore.PublicSnapshotOperator(operationCtx, matchID)
				} else {
					snapshot = store.PublicSnapshot(matchID)
				}
				if err != nil {
					return operatorwrite.Response{}, err
				}
				return operatorwrite.JSONResponse(http.StatusCreated, map[string]interface{}{
					"event":    created,
					"snapshot": snapshot,
				})
			})
		case r.Method == http.MethodPost && resource == "events" && len(parts) == 4 && parts[3] == "correct":
			operator, authorized := operatorClaims(r, cfg)
			if !authorized {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			var ev matchstate.MatchEvent
			body, err := decodeOperatorJSON(w, r, &ev)
			if err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			correctionReason, _ := ev.Evidence["correctionReason"].(string)
			if strings.TrimSpace(correctionReason) == "" {
				http.Error(w, "correction reason is required", http.StatusBadRequest)
				return
			}
			ev.OperatorID = operator.Subject
			applyRequestedFactStatus(&ev)
			markProactiveMode(&ev)
			executeOperatorWrite(w, r, operatorWrites, matchID, "events.correct", body, func(operationCtx context.Context) (operatorwrite.Response, error) {
				var corrected matchstate.MatchEvent
				var snapshot matchstate.Snapshot
				var err error
				if transactionalStore, ok := store.(matchstate.OperatorTransactionRepository); ok {
					corrected, snapshot, err = transactionalStore.CorrectOperator(operationCtx, matchID, parts[2], ev)
				} else {
					corrected, snapshot, err = store.Correct(matchID, parts[2], ev)
				}
				if err != nil {
					status := http.StatusBadRequest
					if errors.Is(err, matchstate.ErrNotFound) {
						status = http.StatusNotFound
					}
					return operatorwrite.Response{}, operatorError(status, err)
				}
				if transactionalStore, ok := store.(matchstate.OperatorTransactionRepository); ok {
					snapshot, err = transactionalStore.PublicSnapshotOperator(operationCtx, matchID)
				} else {
					snapshot = store.PublicSnapshot(matchID)
				}
				if err != nil {
					return operatorwrite.Response{}, err
				}
				return operatorwrite.JSONResponse(http.StatusOK, map[string]interface{}{
					"event":    corrected,
					"snapshot": snapshot,
				})
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

func handleVoiceTurnWithFactRefresh(snapshot func() matchstate.Snapshot, generate func(text, audio string) (voiceSessionResult, error), suppressFollowUp func(string) bool, text, audio string) (voiceSessionResult, error) {
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
	if result.Trace.Observation != nil {
		if suppressFollowUp == nil || !suppressFollowUp(result.Trace.Observation.ID) {
			return result, nil
		}
		refreshed.Trace.Observation = result.Trace.Observation
		refreshed.Trace.ToolCalls = append(refreshed.Trace.ToolCalls, companion.ToolCall{
			Name: "observation.follow_up",
			Args: map[string]string{"status": "suppressed_in_band", "observationId": result.Trace.Observation.ID},
		})
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

func emitProactiveResponse(ctx context.Context, writer *wsWriter, agent *companion.Agent, ttsClient *tts.Client, playback conversation.Playback, response companion.ProactiveResponse, eventID, deliveryKey string) {
	emitScheduledResponse(ctx, writer, agent, ttsClient, playback, response.Reply, response.Trace, response.Presentation, "match_reaction", eventID, deliveryKey)
}

func emitObservationResponse(ctx context.Context, writer *wsWriter, agent *companion.Agent, ttsClient *tts.Client, playback conversation.Playback, response companion.ObservationResponse, eventID string) {
	emitScheduledResponse(ctx, writer, agent, ttsClient, playback, response.Reply, response.Trace, response.Presentation, "observation_resolution", eventID, response.Resolution.DeliveryKey)
}

func emitScheduledResponse(ctx context.Context, writer *wsWriter, agent *companion.Agent, ttsClient *tts.Client, playback conversation.Playback, reply string, trace companion.Trace, presentation relationship.PresentationPlan, source, eventID, deliveryKey string) {
	if !replyContextActive(ctx) {
		return
	}
	if err := writer.SendJSON(map[string]interface{}{
		"type":  "event",
		"event": "qiuqiu_reply",
		"data":  qiuqiuReplyData(reply, trace.ID, source, eventID, deliveryKey, presentation),
	}); err != nil {
		return
	}
	if ttsClient == nil {
		writer.SendJSON(map[string]interface{}{"type": "voice_status", "state": "tts_fallback", "reason": "tts unavailable"})
		return
	}
	ttsResult, err := ttsClient.Synthesize(ctx, reply, "")
	if err != nil {
		if !replyContextActive(ctx) {
			return
		}
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
	trace.Voice = ensureVoiceMeta(trace.Voice)
	trace.Voice.TTSStatus = "ok"
	trace.Voice.TTSMime = fallbackString(ttsResult.MimeType, "audio/mpeg")
	trace.Voice.TTSByteCount = len(ttsResult.AudioData)
	_ = agent.UpdateTrace(ctx, trace)
	playback(trace.ID)
	writer.SendAudio(map[string]interface{}{
		"type":        "voice_audio",
		"mime":        trace.Voice.TTSMime,
		"traceId":     trace.ID,
		"byteLength":  len(ttsResult.AudioData),
		"eventId":     eventID,
		"deliveryKey": deliveryKey,
		"source":      source,
	}, ttsResult.AudioData)
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
		"data":  qiuqiuReplyData(response.Reply, response.Trace.ID, "first_meeting", "", "", response.Presentation),
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
	} else if trace.ObservationResolution != nil {
		purpose = "observation_resolution"
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
	if err != nil {
		return err
	}
	if trace.ObservationResolution != nil {
		return agent.MarkObservationResolutionDelivered(ctx, trace.ObservationResolution.DeliveryKey, now)
	}
	return nil
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
	w.Header().Set("Access-Control-Allow-Methods", "DELETE, GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Idempotency-Key")
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
	if event.EventType == "score_correction" {
		event.ProactiveText = ""
		event.Tags = append(event.Tags, "proactive=quiet")
		return
	}
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

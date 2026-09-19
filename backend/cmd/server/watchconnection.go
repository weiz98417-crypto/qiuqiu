package main

// /ws/match/ 实时会话引擎（openspec/changes/server-surface-split）：
// 原 main() 里的 675 行内联闭包——连接生命周期、投递跟踪、15-case 读
// 循环、主动调度、心跳。外部依赖显式收进 watchDeps，main() 只装配。

import (
	"context"
	"encoding/json"
	"errors"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"qiuqiu/internal/asr"
	"qiuqiu/internal/auth"
	"qiuqiu/internal/companion"
	"qiuqiu/internal/config"
	"qiuqiu/internal/conversation"
	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/memory"
	"qiuqiu/internal/relationship"
	"qiuqiu/internal/ws"

)

// watchDeps 收集 /ws/match/ 连接处理器的全部外部依赖（原由闭包隐式捕获）。
type watchDeps struct {
	hub           *ws.Hub
	matchStore    matchstate.Repository
	traceReader   companion.TraceReader
	watchSessions *conversation.WatchSessionRegistry
	agent         *companion.Agent
	tts           speechSynthesizer
	asr           *asr.Client
	cfg           *config.Config
	memories      *memory.Queue
	memoriesPrefs *memory.PostgresRecords
	interruptions *interruptionRing
	sessions      *auth.Manager
}

func handleWatchConnection(deps watchDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, release, claims, err := deps.hub.UpgradeWithIdentity(w, r)
		if err != nil {
			return
		}
		defer release()
		defer conn.Close()
		writer := &wsWriter{conn: conn}
		identity := newConnectionIdentity(claims.Subject)

		matchIDStr := r.URL.Path[len("/ws/match/"):]
		matchEvents, unsubscribe := deps.matchStore.Subscribe(matchIDStr)
		defer unsubscribe()
		var clockUpdates <-chan matchstate.MatchClock
		if clockStore, ok := deps.matchStore.(matchstate.ClockRepository); ok {
			var unsubscribeClock func()
			clockUpdates, unsubscribeClock = clockStore.SubscribeClock(matchIDStr)
			defer unsubscribeClock()
		}

		connectionCtx, connectionCancel := context.WithCancel(r.Context())
		defer connectionCancel()
		sessionUserID := identity.Get()
		if sessionUserID == "" {
			sessionUserID = "connection:" + strconv.FormatInt(time.Now().UnixNano(), 10)
		}
		watchSession := deps.watchSessions.Acquire(sessionUserID, matchIDStr)
		defer func() { deps.watchSessions.Release(watchSession.UserID, watchSession.MatchID) }()
		deliveryTracker := newReplyDeliveryTrackerWithLedger(watchSession.Ledger())
		// ADR-0007 delivery reaction: one-shot confused/listening per
		// scheduler user-preempt during playback (once per interruption).
		interruptedReactions := newInterruptedReactionGuard(deps.interruptions)
		responseDelivery := newResponseDeliveryService(writer, deps.agent, deps.tts, deliveryTracker)
		firstMeetingCoordinator := conversation.NewFirstMeetingCoordinator(deps.agent, responseDelivery)
		conversationScheduler := watchSession.Scheduler()
		var scheduleLookups scheduleLookupLifecycle
		defer scheduleLookups.Cancel()
		proactiveGate := conversation.NewProactiveGate()
		var userSpeaking atomic.Bool
		var userTurnActive atomic.Bool
		// Talkativeness tier (C2 drift fix): the client sends the 话痨程度
		// setting on every user_speech payload; the latest tier on this
		// connection feeds the proactive gate (quiet restricts), the policy
		// cooldown scale and the relationship InitiativeMode.
		var userTalkativeness atomic.Value
		userTalkativeness.Store(relationship.TalkativenessNormal)
		// fetchOpenThreadCitation returns "open_thread:<id>" for the user's
		// oldest open thread, or "" when none exists; callers then cite the
		// match event itself as the shared moment.
		fetchOpenThreadCitation := func(userID string) string {
			if userID == "" || deps.memories == nil {
				return ""
			}
			threadsCtx, cancel := context.WithTimeout(connectionCtx, 2*time.Second)
			defer cancel()
			threads, err := deps.memories.Threads(threadsCtx, userID)
			if err != nil || len(threads) == 0 {
				return ""
			}
			return conversation.ThreadCitation(threads[0].ID)
		}
		defer func() {
			for _, pending := range deliveryTracker.Drain() {
				cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 3*time.Second)
				// Connection teardown, not a preempt during playback: the
				// observation runs but no delivery reaction is emitted (the
				// socket is closing).
				if err := observeReplyDelivery(cleanupCtx, deps.agent, pending.Trace, pending.UserID, pending.MatchID, "interrupted", time.Now().UTC()); err != nil {
					log.Printf("relationship interrupted delivery cleanup error: %v", err)
				}
				cleanupCancel()
			}
		}()
		scheduleRecoveredObservations := func(userID string) int {
			now := time.Now().UTC()
			responses, err := deps.agent.RecoverObservationFollowUps(connectionCtx, userID, matchIDStr, now)
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
					_, err := responseDelivery.Deliver(replyCtx, conversation.ResponseDeliveryRequest{
						Reply: followUp.Reply, Trace: followUp.Trace, Presentation: followUp.Presentation,
						Source: "observation_resolution", EventID: followUp.Resolution.FactID,
						DeliveryKey: followUp.Resolution.DeliveryKey, Critical: true, TTL: ttl,
					}, playback)
					if err != nil && !errors.Is(err, context.Canceled) {
						log.Printf("observation response delivery error: %v", err)
					}
				})
			}
			return len(followUps)
		}

		// scheduleRecoveredThreadTurns is the C2 post-match recovery beat: it
		// scans the user's open threads and enqueues proactive recovery turns
		// through the scheduler's proactive path ("上一场你问谁助攻的——是法
		// 比安"). The thread is addressed only after the delivery landed, so
		// failed deliveries stay open for the next beat.
		scheduleRecoveredThreadTurns := func(userID string) {
			if userID == "" {
				return
			}
			recoveryCtx, cancel := context.WithTimeout(connectionCtx, 10*time.Second)
			defer cancel()
			recoveries, err := deps.agent.RecoverOpenThreads(recoveryCtx, userID, matchIDStr, time.Now().UTC())
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					log.Printf("open thread recovery error: %v", err)
				}
				return
			}
			for _, recovery := range recoveries {
				recovery := recovery
				ttl := 60 * time.Second
				conversationScheduler.SubmitProactive("open-thread-recovery:"+recovery.ThreadID, conversation.UrgencyNormal, ttl, func(replyCtx context.Context, playback conversation.Playback) {
					_, err := responseDelivery.Deliver(replyCtx, conversation.ResponseDeliveryRequest{
						Reply: recovery.Response.Reply, Trace: recovery.Response.Trace, Presentation: recovery.Response.Presentation,
						Source: "open_thread_recovery", DeliveryKey: recovery.Response.Trace.ID,
						Critical: false, TTL: ttl,
					}, playback)
					if err != nil {
						if !errors.Is(err, context.Canceled) {
							log.Printf("open thread recovery delivery error: %v", err)
						}
						return
					}
					addressCtx, addressCancel := context.WithTimeout(context.Background(), 3*time.Second)
					defer addressCancel()
					if err := deps.agent.AddressOpenThread(addressCtx, recovery.ThreadID); err != nil {
						log.Printf("open thread %s address error: %v", recovery.ThreadID, err)
					}
				})
			}
		}

		writer.SendJSON(map[string]interface{}{
			"type": "match_snapshot",
			"data": clientSnapshot(deps.matchStore.PublicSnapshot(matchIDStr)),
		})
		if clockStore, ok := deps.matchStore.(matchstate.ClockRepository); ok {
			writer.SendJSON(map[string]interface{}{
				"type": "match_clock",
				"data": clockStore.Clock(matchIDStr),
			})
		}
		if userID := identity.Get(); userID != "" {
			scheduleRecoveredObservations(userID)
			scheduleRecoveredThreadTurns(userID)
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
						"data": clientSnapshot(deps.matchStore.PublicSnapshot(matchIDStr)),
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
					if ev.EventType == "match_end" || ev.EventType == "fulltime" {
						// C2 post-match beat: recover open threads for the
						// user once the match record is complete.
						scheduleRecoveredThreadTurns(identity.Get())
					}
					snapshot := deps.matchStore.PublicSnapshot(matchIDStr)
					userID := identity.Get()
					followUpCount := scheduleRecoveredObservations(userID)
					if !matchstate.IsPublicFact(ev) {
						writer.SendJSON(map[string]interface{}{
							"type": "match_snapshot",
							"data": clientSnapshot(snapshot),
						})
						if ev.FactStatus == matchstate.FactStatusRevoked {
							writer.SendJSON(matchFactRetractedMessage(ev))
						}
						continue
					}
					writer.SendJSON(map[string]interface{}{
						"type":        "match_event",
						"data":        ev,
						"snapshot":    clientSnapshot(snapshot),
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
						policy := deps.matchStore.Config(matchIDStr).Automation
						critical := proactiveUrgency(ev.EventType) == conversation.UrgencyCritical
						now := time.Now()
						tier, _ := userTalkativeness.Load().(string)
						// C2 gate: the whitelist and the director cooldown stay
						// as preconditions; the turn additionally needs a
						// citation — an open thread when one exists, otherwise
						// the event itself as the shared moment.
						citation := fetchOpenThreadCitation(userID)
						if citation == "" {
							citation = conversation.EventCitation(ev.ID)
						}
						allowed := proactiveGate.Allow(policy, ev.EventType, citation, tier, critical, now)
						if hasEventTag(ev, "proactive=quiet") {
							allowed = false
						} else if hasEventTag(ev, "proactive=manual") {
							allowed = proactiveGate.AllowManual(now)
						}
						response, err := deps.agent.HandleMatchEvent(connectionCtx, companion.MatchEventRequest{
							UserID:                userID,
							Event:                 ev,
							Snapshot:              snapshot,
							OutputAllowed:         allowed,
							Critical:              critical,
							UserSpeaking:          userSpeaking.Load() || userTurnActive.Load(),
							NormalCooldownSeconds: policy.CooldownSeconds,
							Talkativeness:         tier,
							CitationReason:        citation,
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
								"citation":    citation,
							})
						}
						if strings.TrimSpace(response.Reply) == "" {
							continue
						}
						urgency, ttl := proactiveSchedule(response.Decision, ev.EventType)
						conversationScheduler.SubmitProactive(eventKey, urgency, ttl, func(replyCtx context.Context, playback conversation.Playback) {
							_, err := responseDelivery.Deliver(replyCtx, conversation.ResponseDeliveryRequest{
								Reply: response.Reply, Trace: response.Trace, Presentation: response.Presentation,
								Source: "match_reaction", EventID: ev.ID, DeliveryKey: eventKey,
								Critical: urgency == conversation.UrgencyCritical, TTL: ttl,
							}, playback)
							if err != nil && !errors.Is(err, context.Canceled) {
								log.Printf("match response delivery error: %v", err)
							}
						})
					}
				}
			}
		}()

		submitUserTurn := func(userID, text, audioB64, signalID, asrProvider, timezone string) {
			if normalizedSignalID := strings.TrimSpace(signalID); normalizedSignalID != "" {
				signalKey := strings.Join([]string{userID, matchIDStr, normalizedSignalID}, "\x00")
				if !submittedUserSignals.Claim(signalKey, time.Now()) {
					return
				}
			}
			tier, _ := userTalkativeness.Load().(string)
			turnGeneration := scheduleLookups.BeginTurn()
			writer.SendJSON(map[string]interface{}{
				"type":       "interrupt",
				"expression": "listening",
			})
			userTurnActive.Store(true)
			conversationScheduler.SubmitUser(func(replyCtx context.Context, playback conversation.Playback) {
				defer userTurnActive.Store(false)
				result, err := handleVoiceTurnWithFactRefresh(
					func() matchstate.Snapshot { return deps.matchStore.PublicSnapshot(matchIDStr) },
					func(turnText, turnAudio, factRefresh string) (voiceSessionResult, error) {
						if strings.TrimSpace(asrProvider) != "" {
							return handleTranscribedVoiceSessionWithSignalIDOptions(
								replyCtx,
								deps.agent,
								nil,
								matchIDStr,
								userID,
								turnText,
								asrProvider,
								time.Now(),
								signalID,
								voiceSessionOptions{ProgressiveSchedule: true, Timezone: timezone, FactRefresh: factRefresh, Talkativeness: tier},
							)
						}
						return handleVoiceSessionWithSignalIDOptions(replyCtx, deps.agent, deps.asr, nil, matchIDStr, userID, turnText, turnAudio, time.Now(), signalID, voiceSessionOptions{ProgressiveSchedule: true, Timezone: timezone, FactRefresh: factRefresh, Talkativeness: tier})
					},
					func(observationID string) bool {
						if err := deps.agent.SuppressObservationFollowUp(replyCtx, observationID, time.Now().UTC()); err != nil {
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
						_ = observeReplyOutcome(cleanupCtx, deps.agent, writer, interruptedReactions, result.Trace, userID, matchIDStr, state, time.Now().UTC())
						cleanupCancel()
						return
					}
					cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 3*time.Second)
					_ = observeReplyOutcome(cleanupCtx, deps.agent, writer, interruptedReactions, result.Trace, userID, matchIDStr, state, time.Now().UTC())
					cleanupCancel()
					log.Printf("companion voice reply error: %v", err)
					if result.ASRError != "" {
						writer.SendJSON(map[string]interface{}{"type": "voice_status", "state": "failed", "reason": result.ASRError})
					}
					return
				}
				if replyCtx.Err() != nil {
					cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 3*time.Second)
					_ = observeReplyOutcome(cleanupCtx, deps.agent, writer, interruptedReactions, result.Trace, userID, matchIDStr, "interrupted", time.Now().UTC())
					cleanupCancel()
					return
				}
				if result.ASRError != "" {
					writer.SendJSON(map[string]interface{}{"type": "voice_status", "state": "text_fallback", "reason": result.ASRError})
				}
				if strings.TrimSpace(result.Reply) == "" {
					cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 3*time.Second)
					_ = observeReplyDelivery(cleanupCtx, deps.agent, result.Trace, userID, matchIDStr, "skipped", time.Now().UTC())
					cleanupCancel()
					return
				}
				// presentation-mapping 3.2 (ADR-0007): the reply completes into a
				// voice-session wait for the user, so the plan that rides with
				// the reply decays to the listening pose instead of the
				// watching focus.
				_, deliveryErr := responseDelivery.Deliver(replyCtx, conversation.ResponseDeliveryRequest{
					Reply: result.Reply, Trace: result.Trace, Presentation: voiceWaitPresentation(result.Presentation),
					Source: "conversation", DeliveryKey: result.Trace.ID, TTL: 30 * time.Second,
					AfterText: func(context.Context) error {
						if result.ScheduleLookup == nil || replyCtx.Err() != nil {
							return nil
						}
						lookup := *result.ScheduleLookup
						lookupCtx, started := scheduleLookups.Start(connectionCtx, turnGeneration, lookup.ID, lookup.ExpiresAt)
						if !started {
							return nil
						}
						go func() {
							response, resolveErr := deps.agent.ResolveScheduleLookup(lookupCtx, lookup)
							if resolveErr != nil {
								if !errors.Is(resolveErr, context.Canceled) && !errors.Is(resolveErr, context.DeadlineExceeded) && !errors.Is(resolveErr, companion.ErrScheduleLookupExpired) {
									log.Printf("schedule lookup error: %v", resolveErr)
								}
								return
							}
							if lookupCtx.Err() != nil || !scheduleLookups.IsCurrent(lookup.ID) {
								return
							}
							ttl := time.Until(lookup.ExpiresAt)
							if ttl <= 0 {
								scheduleLookups.Complete(lookup.ID)
								return
							}
							conversationScheduler.SubmitProactive(lookup.ID, conversation.UrgencyNormal, ttl, func(resultCtx context.Context, resultPlayback conversation.Playback) {
								if !scheduleLookups.Complete(lookup.ID) {
									return
								}
								_, err := responseDelivery.Deliver(resultCtx, conversation.ResponseDeliveryRequest{
									Reply: response.Reply, Trace: response.Trace, Presentation: response.Presentation,
									Source: "schedule_lookup", DeliveryKey: lookup.ID, TTL: ttl,
								}, resultPlayback)
								if err != nil && !errors.Is(err, context.Canceled) {
									log.Printf("schedule lookup delivery error: %v", err)
								}
							})
						}()
						return nil
					},
				}, playback)
				if deliveryErr != nil {
					state := "failed"
					if errors.Is(deliveryErr, context.Canceled) {
						state = "interrupted"
					}
					cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 3*time.Second)
					_ = observeReplyOutcome(cleanupCtx, deps.agent, writer, interruptedReactions, result.Trace, userID, matchIDStr, state, time.Now().UTC())
					cleanupCancel()
					if state == "failed" {
						log.Printf("conversation response delivery error: %v", deliveryErr)
					}
				}
			})
		}

		transcriptions := newTranscriptionSessions(
			connectionCtx,
			deps.asr,
			asr.StreamOptions{},
			func(message map[string]interface{}) { _ = writer.SendJSON(message) },
			func(completion transcriptionCompletion) {
				submitUserTurn(completion.UserID, completion.Text, "", completion.SignalID, completion.Provider, completion.Timezone)
			},
		)
		defer transcriptions.Close()

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
					if deps.cfg.SessionAuthRequired() && requested != "" && requested != identity.Get() {
						writer.SendJSON(map[string]string{"type": "auth_error", "reason": "user identity does not match session"})
						return
					}
					if deps.cfg.LegacyAuthAllowed() {
						identifiedUserID := identity.Set(str(req, "userId"))
						watchSession = deps.watchSessions.Bind(watchSession, identifiedUserID, matchIDStr)
						deliveryTracker.BindLedger(watchSession.Ledger())
						conversationScheduler = watchSession.Scheduler()
						scheduleRecoveredObservations(identity.Get())
					}
				case "set_talkativeness":
					// C3 drift fix 的显式通道：改档即时持久化并 ack——客户端以 ack 为
					// 真源，不再等下一条 user_speech 顺带生效，也不被第二台设备的
					// 本地默认值静默覆盖。
					setUserID := identity.Get()
					tier := relationship.NormalizeTalkativeness(str(req, "talkativeness"))
					userTalkativeness.Store(tier)
					persisted := false
					if deps.memoriesPrefs != nil {
						persistCtx, persistCancel := context.WithTimeout(connectionCtx, 3*time.Second)
						defer persistCancel()
						if err := deps.memoriesPrefs.RecordTalkativeness(persistCtx, setUserID, tier); err != nil {
							log.Printf("memory: record talkativeness for %q: %v", setUserID, err)
						} else {
							persisted = true
						}
					}
					writer.SendJSON(map[string]interface{}{"type": "talkativeness_ack", "tier": tier, "persisted": persisted})
				case "user_activity":
					speaking := str(req, "state") == "speaking"
					userSpeaking.Store(speaking)
					if speaking {
						scheduleLookups.Cancel()
						conversationScheduler.Interrupt()
					}
				case "session_opened":
					userID, identityMatches := connectionUserID(identity, deps.cfg, str(req, "userId"))
					if !identityMatches {
						writer.SendJSON(map[string]string{"type": "auth_error", "reason": "user identity does not match session"})
						return
					}
					if userID == "" {
						continue
					}
					watchSession = deps.watchSessions.Bind(watchSession, userID, matchIDStr)
					deliveryTracker.BindLedger(watchSession.Ledger())
					conversationScheduler = watchSession.Scheduler()
					// Restore the persisted 话痨程度 tier so the proactive
					// behavior survives reconnects (C2 talkativeness wiring).
					if deps.memoriesPrefs != nil {
						prefCtx, prefCancel := context.WithTimeout(connectionCtx, 2*time.Second)
						if storedTier, err := deps.memoriesPrefs.Talkativeness(prefCtx, userID); err == nil {
							userTalkativeness.Store(storedTier)
						}
						prefCancel()
					}
					decision, observeErr := deps.agent.ObserveSession(connectionCtx, "session:"+userID+":"+matchIDStr, userID, matchIDStr, time.Now().UTC())
					if observeErr != nil {
						log.Printf("relationship session observation error: %v", observeErr)
					} else {
						// presentation-mapping 1.5: the computed hello used to
						// be discarded here (`_, err :=`); deliver it with the
						// same shape the first-meeting coordinator uses.
						deliverSessionOpeningPresentation(writer, decision)
					}
					scheduleRecoveredObservations(userID)
					scheduleRecoveredThreadTurns(userID)
					recoverPendingDeliveries(connectionCtx, writer, watchSession, deps.traceReader, userID, matchIDStr)

				case "session_closed":
					userID, identityMatches := connectionUserID(identity, deps.cfg, str(req, "userId"))
					if !identityMatches {
						writer.SendJSON(map[string]string{"type": "auth_error", "reason": "user identity does not match session"})
						return
					}
					if userID != "" {
						deps.watchSessions.Release(userID, matchIDStr)
					}
					writer.SendJSON(map[string]string{"type": "session_closed", "reason": str(req, "reason")})

				case "first_meeting":
					userID, identityMatches := connectionUserID(identity, deps.cfg, str(req, "userId"))
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
						_, err := firstMeetingCoordinator.Handle(replyCtx, companion.FirstMeetingRequest{
							SignalID: firstMeetingSignalID, MatchID: matchIDStr, UserID: userID,
							Nickname: nickname, FavoriteTeam: favoriteTeam, Now: time.Now(),
						}, playback)
						if err != nil && !errors.Is(err, context.Canceled) {
							log.Printf("first meeting delivery error: %v", err)
						}
					})

				case "interrupt":
					scheduleLookups.Cancel()
					conversationScheduler.Interrupt()

					writer.SendJSON(map[string]interface{}{
						"type":       "interrupt",
						"expression": "listening",
					})

				case "user_speech":
					text := str(req, "text")
					audioB64 := str(req, "audio")
					userID, identityMatches := connectionUserID(identity, deps.cfg, str(req, "userId"))
					if !identityMatches {
						writer.SendJSON(map[string]string{"type": "auth_error", "reason": "user identity does not match session"})
						return
					}
					if userID == "" {
						writer.SendJSON(map[string]interface{}{"type": "voice_status", "state": "failed", "reason": "identity required"})
						continue
					}
					// C2 drift fix: the client has always sent the 话痨程度
					// tier with every user_speech payload; parse it, keep it
					// on the connection and persist it per user.
					tier := relationship.NormalizeTalkativeness(str(req, "talkativeness"))
					userTalkativeness.Store(tier)
					if deps.memoriesPrefs != nil {
						persistCtx, persistCancel := context.WithTimeout(context.Background(), 3*time.Second)
						go func(persistUserID, persistTier string) {
							defer persistCancel()
							if err := deps.memoriesPrefs.RecordTalkativeness(persistCtx, persistUserID, persistTier); err != nil {
								log.Printf("memory: record talkativeness for %q: %v", persistUserID, err)
							}
						}(userID, tier)
					}
					generatedSignalID := fmt.Sprintf("turn_%s_%d", userID, time.Now().UnixNano())
					turnSignalID := stableSignalID(str(req, "signalId"), generatedSignalID)
					submitUserTurn(userID, text, audioB64, turnSignalID, "", strings.TrimSpace(str(req, "timezone")))
				case "asr_start":
					userID, identityMatches := connectionUserID(identity, deps.cfg, str(req, "userId"))
					if !identityMatches {
						writer.SendJSON(map[string]string{"type": "auth_error", "reason": "user identity does not match session"})
						return
					}
					utteranceID := strings.TrimSpace(str(req, "utteranceId"))
					generatedSignalID := fmt.Sprintf("turn_%s_%d", userID, time.Now().UnixNano())
					signalID := stableSignalID(str(req, "signalId"), generatedSignalID)
					if err := validateTranscriptionStart(req); err != nil {
						writer.SendJSON(transcriptErrorMessage(utteranceID, err, false))
						continue
					}
					if err := transcriptions.Start(utteranceID, signalID, userID, strings.TrimSpace(str(req, "timezone")), voiceRecognitionHints(deps.matchStore.Config(matchIDStr))); err != nil {
						writer.SendJSON(transcriptErrorMessage(utteranceID, err, false))
					}
				case "asr_chunk":
					utteranceID := strings.TrimSpace(str(req, "utteranceId"))
					sequence, ok := intField(req, "sequence")
					audio, err := base64.StdEncoding.DecodeString(str(req, "audio"))
					if !ok || err != nil || len(audio) == 0 || len(audio)%2 != 0 {
						_ = transcriptions.Cancel(utteranceID)
						writer.SendJSON(transcriptErrorMessage(utteranceID, errors.New("invalid audio chunk"), false))
						continue
					}
					if err := transcriptions.Append(utteranceID, sequence, audio); err != nil {
						writer.SendJSON(transcriptErrorMessage(utteranceID, err, false))
					}
				case "asr_finish":
					utteranceID := strings.TrimSpace(str(req, "utteranceId"))
					if err := transcriptions.Finish(utteranceID); err != nil {
						writer.SendJSON(transcriptErrorMessage(utteranceID, err, false))
					}
				case "asr_cancel":
					utteranceID := strings.TrimSpace(str(req, "utteranceId"))
					_ = transcriptions.Cancel(utteranceID)
				case "voice_playback":
					traceID := strings.TrimSpace(str(req, "traceId"))
					state := strings.TrimSpace(str(req, "state"))
					if traceID == "" || state == "" {
						continue
					}
					updateCtx, updateCancel := context.WithTimeout(connectionCtx, 3*time.Second)
					userID := identity.Get()
					err := recordPlaybackStatus(updateCtx, deps.traceReader, deps.agent, matchIDStr, traceID, userID, state)
					if err == nil {
						conversationScheduler.PlaybackChanged(traceID, state)
						deliveryTracker.Transition(traceID, deliveryStateForPlayback(state), time.Now().UTC())
						if terminalPlaybackState(state) {
							deliveryTracker.Remove(traceID)
						}
						if _, relationshipErr := deps.agent.Plan(updateCtx, companion.TurnInput{Kind: companion.TurnKindDelivery, Delivery: &companion.DeliveryInput{SignalID: "delivery:" + traceID + ":" + state, TraceID: traceID, UserID: userID, MatchID: matchIDStr, State: state, Purpose: "playback", Now: time.Now().UTC()}}); relationshipErr != nil && !errors.Is(relationshipErr, context.Canceled) {
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
					err := recordDisplayedReply(updateCtx, deps.traceReader, deps.agent, matchIDStr, traceID, userID, time.Now().UTC())
					if err == nil {
						if _, ackErr := deliveryTracker.Ledger().AcknowledgeText(traceID, time.Now().UTC()); ackErr != nil && !errors.Is(ackErr, conversation.ErrDeliveryNotFound) {
							log.Printf("text acknowledgement error: %v", ackErr)
						}
						deliveryTracker.Transition(traceID, conversation.DeliveryTextDelivered, time.Now().UTC())
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
					if err := deps.sessions.ValidateClaims(connectionCtx, claims); err != nil {
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

	}
}

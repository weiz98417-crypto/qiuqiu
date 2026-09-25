package main

// /ws/match/ 实时会话引擎（openspec/changes/server-surface-split，四相拆分见
// openspec/changes/server-residual-polish）：原 main() 里的 675 行内联闭包，
// 打磨轮 #5 按连接生命周期拆成四个相——upgrade / identify-and-subscribe /
// message-pump / teardown。外部依赖显式收进 watchDeps，main() 只装配；
// 每相是 watchConnection 的私有方法，纯搬移，消息处理语义零变化。

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"qiuqiu/internal/asr"
	"qiuqiu/internal/auth"
	"qiuqiu/internal/backchannel"
	"qiuqiu/internal/companion"
	"qiuqiu/internal/config"
	"qiuqiu/internal/conversation"
	"qiuqiu/internal/interaction"
	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/memory"
	"qiuqiu/internal/proactive"
	"qiuqiu/internal/relationship"
	"qiuqiu/internal/ws"

	"github.com/gorilla/websocket"
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
	reminders     proactive.Store
	// characterSettings 是人格互动规范的持久化状态（三入口一状态）。
	characterSettings *relationship.CharacterSettings
	interactionLedger interaction.Ledger
	// backchannelState 是伴随反应的连接内限频计数（ADR-0016）。
	backchannelState backchannel.State
	// submittedSignals 是用户轮次的信号去重器（server-residual-polish 1.3：
	// 此前是包级 global，现由 main() 构造注入）。
	submittedSignals *signalDeduper
	// ambient 是气氛旁路接线器（ambient-audio-observation）：nil 即旁路
	// 停用；ASR 主路不感知 sidecar 存活，旁路失败静默计数。
	ambient *ambientRelay
}

// watchConnection 承载一条 /ws/match/ 连接跨四相的全部状态。字段与拆分前
// 闭包捕获的变量一一对应；读循环里对 watchSession / scheduler 的重绑定通过
// 字段赋值保持原语义（外推协程与心跳同读一处）。
type watchConnection struct {
	deps   watchDeps
	conn   *websocket.Conn
	claims auth.Claims
	writer *wsWriter

	identity *connectionIdentity

	matchID      string
	matchEvents  <-chan matchstate.MatchEvent
	clockUpdates <-chan matchstate.MatchClock

	// backchannelState 是伴随反应的连接内限频计数（ADR-0016）。
	backchannelState backchannel.State

	connectionCtx    context.Context
	connectionCancel context.CancelFunc

	watchSession         *conversation.WatchSession
	deliveryTracker      *replyDeliveryTracker
	responseDelivery     *conversation.ResponseDeliveryService
	firstMeeting         *conversation.FirstMeetingCoordinator
	scheduler            *conversation.Scheduler
	scheduleLookups      scheduleLookupLifecycle
	proactiveGate        *conversation.ProactiveGate
	interruptedReactions *interruptedReactionGuard
	// clientPlaybackReports 登记本连接收到的播放实报，推断路径据此降级。
	clientPlaybackReports *clientPlaybackReportSet

	userSpeaking   atomic.Bool
	userTurnActive atomic.Bool
	// Talkativeness tier (C2 drift fix): the client sends the 话痨程度
	// setting on every user_speech payload; the latest tier on this
	// connection feeds the proactive gate (quiet restricts), the policy
	// cooldown scale and the relationship InitiativeMode.
	userTalkativeness atomic.Value

	// voiceLatencyAnchors 记录每个话轮信号/转写会话的服务端到达时刻，供
	// 延迟分解日志取 elapsed（voice-transport-upgrade 1.1，待真机会话采集）。
	voiceLatencyMu      sync.Mutex
	voiceLatencyAnchors map[string]time.Time

	transcriptions *transcriptionSessions
}

func newWatchConnection(deps watchDeps, conn *websocket.Conn, claims auth.Claims) *watchConnection {
	connection := &watchConnection{deps: deps, conn: conn, claims: claims}
	connection.writer = &wsWriter{conn: conn}
	connection.identity = newConnectionIdentity(claims.Subject)
	connection.userTalkativeness.Store(relationship.TalkativenessNormal)
	connection.clientPlaybackReports = newClientPlaybackReportSet()
	connection.voiceLatencyAnchors = make(map[string]time.Time)
	return connection
}

// ── 语音链路延迟分解日志（voice-transport-upgrade 1.1）──────────────────────
//
// 与 duplex_event 同风格的结构化日志点，不建新系统：speech_received →
// asr_finish → asr_final → turn_decided → audio_delivered 各打一行带
// elapsed 的日志，真机 p90 由后续真机会话按此采集。操作台 HTTP 语音路径的
// TTS 阶段见 completeVoiceSessionWithOptions 的 tts_synthesized 行。

// maxVoiceLatencyAnchors 锚点表上限：满 64 即整表重置（在途话轮锚点随之
// 丢弃，延迟日志行静默缺失——观测性已知边界），只影响迟到消息的日志行，
// 不影响业务。
const maxVoiceLatencyAnchors = 64

// voiceLatencyAnchorKey 归一锚点键：信号直接用 signalID，转写会话加 utt:
// 前缀防跨类型撞键。
func voiceLatencyAnchorKey(kind, id string) string {
	return kind + ":" + id
}

func (c *watchConnection) storeVoiceLatencyAnchor(key string, at time.Time) {
	if key == "" || at.IsZero() {
		return
	}
	c.voiceLatencyMu.Lock()
	defer c.voiceLatencyMu.Unlock()
	if len(c.voiceLatencyAnchors) >= maxVoiceLatencyAnchors {
		c.voiceLatencyAnchors = make(map[string]time.Time)
	}
	c.voiceLatencyAnchors[key] = at
}

func (c *watchConnection) voiceLatencyAnchor(key string) time.Time {
	if key == "" {
		return time.Time{}
	}
	c.voiceLatencyMu.Lock()
	defer c.voiceLatencyMu.Unlock()
	return c.voiceLatencyAnchors[key]
}

// logVoiceLatency 打一行语音链路延迟分解日志；锚点缺失（如非本连接发起的
// 话轮）直接跳过，避免没有 elapsed 基准的噪音行。
func (c *watchConnection) logVoiceLatency(stage, signalID string, startedAt time.Time) {
	if startedAt.IsZero() {
		return
	}
	log.Printf("voice latency event: user=%q match=%q signal=%q stage=%q elapsed_ms=%d",
		c.identity.Get(), c.matchID, signalID, stage, time.Since(startedAt).Milliseconds())
}

func handleWatchConnection(deps watchDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// ── 相 1：upgrade——WS 握手与连接身份。失败即结束（错误响应已写出）。──
		conn, release, claims, err := deps.hub.UpgradeWithIdentity(w, r)
		if err != nil {
			return
		}
		defer release()
		defer conn.Close()
		connection := newWatchConnection(deps, conn, claims)

		// ── 相 2：identify-and-subscribe——定位比赛、订阅事件/时钟流、装配会话。──
		unsubscribeEvents, unsubscribeClock := connection.identifyAndSubscribe(r)
		defer unsubscribeEvents()
		if unsubscribeClock != nil {
			defer unsubscribeClock()
		}
		defer connection.connectionCancel()
		defer connection.releaseWatchSession()
		defer connection.scheduleLookups.Cancel()
		defer connection.drainPendingDeliveries()

		// ── 相 3：message-pump——比赛事件外推、15-case 读循环、语音转写会话。──
		connection.startMessagePumps()
		defer connection.transcriptions.Close()

		// ── 相 4：teardown——心跳看守直到连接关闭；其余清理按上方 defer 栈逆序执行。──
		connection.serveHeartbeat()
	}
}

// ── 相 2：identify-and-subscribe ─────────────────────────────────────────────

// identifyAndSubscribe 解析比赛 ID、订阅事件与时钟流、装配会话级状态（投递
// 跟踪、调度器、主动门控），并发送初始快照与恢复投递。返回事件/时钟流的
// 退订函数（时钟流在不支持时钟仓库时为 nil）。
func (c *watchConnection) identifyAndSubscribe(r *http.Request) (unsubscribeEvents, unsubscribeClock func()) {
	c.matchID = r.URL.Path[len("/ws/match/"):]
	c.matchEvents, unsubscribeEvents = c.deps.matchStore.Subscribe(c.matchID)
	if clockStore, ok := c.deps.matchStore.(matchstate.ClockRepository); ok {
		c.clockUpdates, unsubscribeClock = clockStore.SubscribeClock(c.matchID)
	}

	c.connectionCtx, c.connectionCancel = context.WithCancel(r.Context())
	sessionUserID := c.identity.Get()
	if sessionUserID == "" {
		sessionUserID = "connection:" + strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	c.watchSession = c.deps.watchSessions.Acquire(sessionUserID, c.matchID)
	c.deliveryTracker = newReplyDeliveryTrackerWithLedger(c.watchSession.Ledger())
	// ADR-0007 delivery reaction: one-shot confused/listening per
	// scheduler user-preempt during playback (once per interruption).
	c.interruptedReactions = newInterruptedReactionGuard(c.deps.interruptions)
	c.responseDelivery = newResponseDeliveryService(c.writer, c.deps.agent, c.deps.tts, c.deliveryTracker)
	c.firstMeeting = conversation.NewFirstMeetingCoordinator(c.deps.agent, c.responseDelivery)
	c.scheduler = c.watchSession.Scheduler()
	c.proactiveGate = conversation.NewProactiveGate()

	c.writer.SendJSON(map[string]interface{}{
		"type": "match_snapshot",
		"data": clientSnapshot(c.deps.matchStore.PublicSnapshot(c.matchID)),
	})
	if clockStore, ok := c.deps.matchStore.(matchstate.ClockRepository); ok {
		c.writer.SendJSON(map[string]interface{}{
			"type": "match_clock",
			"data": clockStore.Clock(c.matchID),
		})
	}
	if userID := c.identity.Get(); userID != "" {
		c.scheduleRecoveredObservations(userID)
		c.scheduleRecoveredThreadTurns(userID)
		c.submitDueReminders(userID)
	}
	return unsubscribeEvents, unsubscribeClock
}

// releaseWatchSession is the deferred Release of the logical watch session; it
// reads the session at teardown time so read-loop rebinds (identify /
// session_opened) are honoured, exactly like the pre-split closure capture.
func (c *watchConnection) releaseWatchSession() {
	if c.watchSession == nil {
		return
	}
	c.deps.watchSessions.Release(c.watchSession.UserID, c.watchSession.MatchID)
}

// drainPendingDeliveries flushes the delivery tracker's pending observations at
// teardown. Connection teardown, not a preempt during playback: the observation
// runs but no delivery reaction is emitted (the socket is closing).
func (c *watchConnection) drainPendingDeliveries() {
	for _, pending := range c.deliveryTracker.Drain() {
		// 客户端已实报的回合不再补推断（实报优先，推断仅兜底）。
		if c.clientPlaybackReports.has(pending.Trace.ID) {
			continue
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 3*time.Second)
		if err := observeReplyDelivery(cleanupCtx, c.deps.agent, pending.Trace, pending.UserID, pending.MatchID, "interrupted", time.Now().UTC()); err != nil {
			log.Printf("relationship interrupted delivery cleanup error: %v", err)
		}
		cleanupCancel()
	}
}

// openThreadCitation returns "open_thread:<id>" for the user's oldest open
// thread, or "" when none exists; callers then cite the match event itself as
// the shared moment.
func (c *watchConnection) openThreadCitation(userID string) string {
	if userID == "" || c.deps.memories == nil {
		return ""
	}
	threadsCtx, cancel := context.WithTimeout(c.connectionCtx, 2*time.Second)
	defer cancel()
	threads, err := c.deps.memories.Threads(threadsCtx, userID)
	if err != nil || len(threads) == 0 {
		return ""
	}
	return conversation.ThreadCitation(threads[0].ID)
}

func (c *watchConnection) scheduleRecoveredObservations(userID string) int {
	now := time.Now().UTC()
	responses, err := c.deps.agent.RecoverObservationFollowUps(c.connectionCtx, userID, c.matchID, now)
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
		c.scheduler.SubmitProactive(followUp.Resolution.DeliveryKey, conversation.UrgencyCritical, ttl, func(replyCtx context.Context, playback conversation.Playback) {
			_, err := c.responseDelivery.Deliver(replyCtx, conversation.ResponseDeliveryRequest{
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

// scheduleRecoveredThreadTurns is the C2 post-match recovery beat: it scans the
// user's open threads and enqueues proactive recovery turns through the
// scheduler's proactive path ("上一场你问谁助攻的——是法比安"). The thread is
// addressed only after the delivery landed, so failed deliveries stay open for
// the next beat.
func (c *watchConnection) scheduleRecoveredThreadTurns(userID string) {
	if userID == "" {
		return
	}
	recoveryCtx, cancel := context.WithTimeout(c.connectionCtx, 10*time.Second)
	defer cancel()
	recoveries, err := c.deps.agent.RecoverOpenThreads(recoveryCtx, userID, c.matchID, time.Now().UTC())
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			log.Printf("open thread recovery error: %v", err)
		}
		return
	}
	for _, recovery := range recoveries {
		recovery := recovery
		ttl := 60 * time.Second
		c.scheduler.SubmitProactive("open-thread-recovery:"+recovery.ThreadID, conversation.UrgencyNormal, ttl, func(replyCtx context.Context, playback conversation.Playback) {
			_, err := c.responseDelivery.Deliver(replyCtx, conversation.ResponseDeliveryRequest{
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
			if err := c.deps.agent.AddressOpenThread(addressCtx, recovery.ThreadID); err != nil {
				log.Printf("open thread %s address error: %v", recovery.ThreadID, err)
			}
		})
	}
}

// ── 相 3：message-pump ───────────────────────────────────────────────────────

// submitDueReminders 把该用户已到点的赛前提醒经同一条主动投递路径送出
// （020 outbox 的连接补递腿）；送达才翻 delivered，失败留在簿里下次再试。
func (c *watchConnection) submitDueReminders(userID string) {
	if c.deps.reminders == nil || userID == "" {
		return
	}
	remindersCtx, cancel := context.WithTimeout(c.connectionCtx, 5*time.Second)
	defer cancel()
	pendings, err := c.deps.reminders.PendingForUser(remindersCtx, userID)
	if err != nil {
		log.Printf("reminder pending query error: %v", err)
		return
	}
	now := time.Now().UTC()
	for _, reminder := range proactive.PlanDue(pendings, now) {
		reminder := reminder
		reply := proactive.PreMatchReminderReply(reminder)
		trace := companion.Trace{
			// trace ID 即提醒 ID 的确定性投影：同一条提醒的重试投递共用
			// 一条 trace（投递去重靠 DeliveryKey，不靠 ID）。
			ID:      "reminder-trace-" + reminder.ID,
			MatchID: reminder.MatchID, UserID: userID,
			Input:     reminder.CitationCode(),
			Intent:    companion.IntentMatchReaction,
			Reason:    "reminder_due",
			CreatedAt: now, Output: reply,
			RelationshipDecision: &relationship.Decision{ReasonCodes: []string{"proactive_citation:" + reminder.CitationCode()}},
		}
		ttl := time.Until(reminder.ExpireAt)
		c.scheduler.SubmitProactive("reminder:"+reminder.ID, conversation.UrgencyNormal, ttl, func(replyCtx context.Context, playback conversation.Playback) {
			_, err := c.responseDelivery.Deliver(replyCtx, conversation.ResponseDeliveryRequest{
				Reply: reply, Trace: trace,
				Presentation: relationship.PresentationPlan{Expression: "focus", Motion: "speak", VoiceStyle: "calm", VoiceEnergy: 0.55, VoiceSpeed: 1, HoldMS: 1200, ReturnMode: "watching"},
				Source:       "reminder", DeliveryKey: "reminder:" + reminder.ID, Critical: false, TTL: ttl,
			}, playback)
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					log.Printf("reminder delivery error: %v", err)
				}
				return
			}
			markCtx, markCancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer markCancel()
			if err := c.deps.reminders.MarkDelivered(markCtx, reminder.ID); err != nil {
				log.Printf("reminder mark delivered error: %v", err)
			}
		})
	}
}

// startReminderTicker 让一直挂在 App 里的用户在开球时刻附近也能收到提醒，
// 无需重连；协程随 connectionCtx 收敛。
func (c *watchConnection) startReminderTicker() {
	if c.deps.reminders == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-c.connectionCtx.Done():
				return
			case <-ticker.C:
				if userID := c.identity.Get(); userID != "" {
					c.submitDueReminders(userID)
				}
			}
		}
	}()
}

// maybeBackchannel 是伴随反应通道（openspec/changes/backchannel，ADR-0016）：
// 白名单事件的微反应直发（绕回合调度、不过 C2 门），文字气泡 + 现有表演
// 槽位，失败即弃。审计走 trace + 账本（agent.RecordBackchannel）。
func (c *watchConnection) maybeBackchannel(ev matchstate.MatchEvent) {
	verdict, ok := backchannel.Decide(&c.backchannelState, ev.EventType, ev.Period, backchannelTalkativeness(&c.userTalkativeness), c.userSpeaking.Load() || c.userTurnActive.Load(), time.Now().UTC())
	if !ok {
		return
	}
	now := time.Now().UTC()
	c.writer.SendJSON(map[string]interface{}{
		"type":  "event",
		"event": "qiuqiu_reply",
		"data": map[string]interface{}{
			"text":    verdict.Phrase,
			"traceId": "backchannel-" + ev.ID,
			"source":  "backchannel",
			"presentation": map[string]interface{}{
				"expression": verdict.Expression,
				"motion":     verdict.Motion,
			},
		},
	})
	auditCtx, auditCancel := context.WithTimeout(c.connectionCtx, 3*time.Second)
	defer auditCancel()
	if err := c.deps.agent.RecordBackchannel(auditCtx, c.identity.Get(), c.matchID, "backchannel-"+ev.ID, verdict.EventType, verdict.Phrase, now); err != nil {
		log.Printf("backchannel audit error: %v", err)
	}
}

// backchannelTalkativeness 归一连接内的安静档读取。
func backchannelTalkativeness(store interface{ Load() any }) string {
	tier, _ := store.Load().(string)
	return tier
}

// startMessagePumps 装配语音转写会话并拉起两条协程：比赛事件外推与 15-case
// 读循环。两者都随 connectionCtx 收敛，写出口共用同一条 wsWriter。
func (c *watchConnection) startMessagePumps() {
	c.transcriptions = newTranscriptionSessions(
		c.connectionCtx,
		c.deps.asr,
		asr.StreamOptions{},
		func(message map[string]interface{}) {
			// 延迟分解：asr_final = 转写终稿下行的时刻（voice-transport-
			// upgrade 1.1），锚点取该转写会话 asr_start 的到达时刻。
			if message["type"] == "transcript_final" {
				if utteranceID, ok := message["utteranceId"].(string); ok {
					c.logVoiceLatency("asr_final", utteranceID,
						c.voiceLatencyAnchor(voiceLatencyAnchorKey("utt", utteranceID)))
				}
			}
			_ = c.writer.SendJSON(message)
		},
		func(completion transcriptionCompletion) {
			c.submitUserTurn(completion.UserID, completion.Text, "", completion.SignalID, completion.Provider, completion.Timezone)
		},
	)
	go c.pumpMatchEvents()
	go c.readMessages()
	c.startReminderTicker()
}

// pumpMatchEvents is the outbound event loop: clock ticks and match events are
// mirrored to the client, public facts drive the proactive policy gate and the
// companion's match reaction.
func (c *watchConnection) pumpMatchEvents() {
	deliveredEventKeys := make(map[string]struct{})
	deliveredEventOrder := make([]string, 0, 512)
	for {
		select {
		case <-c.connectionCtx.Done():
			return
		case clock := <-c.clockUpdates:
			c.writer.SendJSON(map[string]interface{}{
				"type": "match_clock",
				"data": clock,
			})
			c.writer.SendJSON(map[string]interface{}{
				"type": "match_snapshot",
				"data": clientSnapshot(c.deps.matchStore.PublicSnapshot(c.matchID)),
			})
		case ev := <-c.matchEvents:
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
				c.scheduleRecoveredThreadTurns(c.identity.Get())
			}
			snapshot := c.deps.matchStore.PublicSnapshot(c.matchID)
			userID := c.identity.Get()
			followUpCount := c.scheduleRecoveredObservations(userID)
			if !matchstate.IsPublicFact(ev) {
				c.writer.SendJSON(map[string]interface{}{
					"type": "match_snapshot",
					"data": clientSnapshot(snapshot),
				})
				if ev.FactStatus == matchstate.FactStatusRevoked {
					c.writer.SendJSON(matchFactRetractedMessage(ev))
				}
				continue
			}
			c.writer.SendJSON(map[string]interface{}{
				"type":        "match_event",
				"data":        ev,
				"snapshot":    clientSnapshot(snapshot),
				"deliveryKey": eventKey,
			})
			if ev.Visibility == "public" && ev.Status == "active" {
				if followUpCount > 0 {
					continue
				}
				userID := c.identity.Get()
				if userID == "" {
					userID = c.identity.Wait(c.connectionCtx)
					if userID == "" {
						return
					}
				}
				policy := c.deps.matchStore.Config(c.matchID).Automation
				critical := proactiveUrgency(ev.EventType) == conversation.UrgencyCritical
				now := time.Now()
				tier, _ := c.userTalkativeness.Load().(string)
				// C2 gate: the whitelist and the director cooldown stay
				// as preconditions; the turn additionally needs a
				// citation — an open thread when one exists, otherwise
				// the event itself as the shared moment.
				citation := c.openThreadCitation(userID)
				if citation == "" {
					citation = conversation.EventCitation(ev.ID)
				}
				allowed := c.proactiveGate.Allow(policy, ev.EventType, citation, tier, critical, now)
				if hasEventTag(ev, "proactive=quiet") {
					allowed = false
				} else if hasEventTag(ev, "proactive=manual") {
					allowed = c.proactiveGate.AllowManual(now)
				} else {
					// ADR-0016：导播手动注入（manual/quiet 标记）不走微反应，
					// 自动事件的微反应在回合判定之后让路触发。
					c.maybeBackchannel(ev)
				}
				response, err := c.deps.agent.HandleMatchEvent(c.connectionCtx, companion.MatchEventRequest{
					UserID:                userID,
					Event:                 ev,
					Snapshot:              snapshot,
					OutputAllowed:         allowed,
					Critical:              critical,
					UserSpeaking:          c.userSpeaking.Load() || c.userTurnActive.Load(),
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
					c.writer.SendJSON(map[string]interface{}{
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
				c.scheduler.SubmitProactive(eventKey, urgency, ttl, func(replyCtx context.Context, playback conversation.Playback) {
					_, err := c.responseDelivery.Deliver(replyCtx, conversation.ResponseDeliveryRequest{
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
}

// submitUserTurn dedupes the client signal and enqueues one user turn on the
// session scheduler; the turn runs the voice session (with in-band fact
// refresh) and delivers the reply through the shared delivery service.
func (c *watchConnection) submitUserTurn(userID, text, audioB64, signalID, asrProvider, timezone string) {
	if normalizedSignalID := strings.TrimSpace(signalID); normalizedSignalID != "" {
		signalKey := strings.Join([]string{userID, c.matchID, normalizedSignalID}, "\x00")
		if !c.deps.submittedSignals.Claim(signalKey, time.Now()) {
			return
		}
	}
	tier, _ := c.userTalkativeness.Load().(string)
	overrides := readPreferenceOverrides(c.connectionCtx, c.deps.characterSettings, userID)
	turnGeneration := c.scheduleLookups.BeginTurn()
	c.writer.SendJSON(map[string]interface{}{
		"type":       "interrupt",
		"expression": "listening",
	})
	c.userTurnActive.Store(true)
	c.scheduler.SubmitUser(func(replyCtx context.Context, playback conversation.Playback) {
		defer c.userTurnActive.Store(false)
		result, err := handleVoiceTurnWithFactRefresh(
			func() matchstate.Snapshot { return c.deps.matchStore.PublicSnapshot(c.matchID) },
			func(turnText, turnAudio, factRefresh string) (voiceSessionResult, error) {
				if strings.TrimSpace(asrProvider) != "" {
					return handleTranscribedVoiceSessionWithSignalIDOptions(
						replyCtx,
						c.deps.agent,
						nil,
						c.matchID,
						userID,
						turnText,
						asrProvider,
						time.Now(),
						signalID,
						voiceSessionOptions{ProgressiveSchedule: true, Timezone: timezone, FactRefresh: factRefresh, Talkativeness: tier, Settings: overrides},
					)
				}
				return handleVoiceSessionWithSignalIDOptions(replyCtx, c.deps.agent, c.deps.asr, nil, c.matchID, userID, turnText, turnAudio, time.Now(), signalID, voiceSessionOptions{ProgressiveSchedule: true, Timezone: timezone, FactRefresh: factRefresh, Talkativeness: tier, Settings: overrides})
			},
			func(observationID string) bool {
				if err := c.deps.agent.SuppressObservationFollowUp(replyCtx, observationID, time.Now().UTC()); err != nil {
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
				_ = c.observeInferredOutcome(cleanupCtx, result.Trace, userID, c.matchID, state, time.Now().UTC())
				cleanupCancel()
				return
			}
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 3*time.Second)
			_ = c.observeInferredOutcome(cleanupCtx, result.Trace, userID, c.matchID, state, time.Now().UTC())
			cleanupCancel()
			log.Printf("companion voice reply error: %v", err)
			if result.ASRError != "" {
				c.writer.SendJSON(map[string]interface{}{"type": "voice_status", "state": "failed", "reason": result.ASRError})
			}
			return
		}
		if replyCtx.Err() != nil {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 3*time.Second)
			_ = c.observeInferredOutcome(cleanupCtx, result.Trace, userID, c.matchID, "interrupted", time.Now().UTC())
			cleanupCancel()
			return
		}
		if result.ASRError != "" {
			c.writer.SendJSON(map[string]interface{}{"type": "voice_status", "state": "text_fallback", "reason": result.ASRError})
		}
		if strings.TrimSpace(result.Reply) == "" {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 3*time.Second)
			_ = observeReplyDelivery(cleanupCtx, c.deps.agent, result.Trace, userID, c.matchID, "skipped", time.Now().UTC())
			cleanupCancel()
			return
		}
		// 延迟分解：turn_decided = 话轮决策（含 ASR/事实刷新）完成的时刻。
		c.logVoiceLatency("turn_decided", signalID, c.voiceLatencyAnchor(signalID))
		// presentation-mapping 3.2 (ADR-0007): the reply completes into a
		// voice-session wait for the user, so the plan that rides with
		// the reply decays to the listening pose instead of the
		// watching focus.
		_, deliveryErr := c.responseDelivery.Deliver(replyCtx, conversation.ResponseDeliveryRequest{
			Reply: result.Reply, Trace: result.Trace, Presentation: voiceWaitPresentation(result.Presentation),
			Source: "conversation", DeliveryKey: result.Trace.ID, TTL: 30 * time.Second,
			AfterText: func(context.Context) error {
				if result.ScheduleLookup == nil || replyCtx.Err() != nil {
					return nil
				}
				lookup := *result.ScheduleLookup
				lookupCtx, started := c.scheduleLookups.Start(c.connectionCtx, turnGeneration, lookup.ID, lookup.ExpiresAt)
				if !started {
					return nil
				}
				go func() {
					response, resolveErr := c.deps.agent.ResolveScheduleLookup(lookupCtx, lookup)
					if resolveErr != nil {
						if !errors.Is(resolveErr, context.Canceled) && !errors.Is(resolveErr, context.DeadlineExceeded) && !errors.Is(resolveErr, companion.ErrScheduleLookupExpired) {
							log.Printf("schedule lookup error: %v", resolveErr)
						}
						return
					}
					if lookupCtx.Err() != nil || !c.scheduleLookups.IsCurrent(lookup.ID) {
						return
					}
					ttl := time.Until(lookup.ExpiresAt)
					if ttl <= 0 {
						c.scheduleLookups.Complete(lookup.ID)
						return
					}
					c.scheduler.SubmitProactive(lookup.ID, conversation.UrgencyNormal, ttl, func(resultCtx context.Context, resultPlayback conversation.Playback) {
						if !c.scheduleLookups.Complete(lookup.ID) {
							return
						}
						_, err := c.responseDelivery.Deliver(resultCtx, conversation.ResponseDeliveryRequest{
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
		// 延迟分解：audio_delivered = 响应投递完成（WS 路径的 TTS 合成与
		// 音频下行都在投递服务内，此行覆盖到下行完成）。
		if deliveryErr == nil {
			c.logVoiceLatency("audio_delivered", signalID, c.voiceLatencyAnchor(signalID))
		}
		if deliveryErr != nil {
			state := "failed"
			if errors.Is(deliveryErr, context.Canceled) {
				state = "interrupted"
			}
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 3*time.Second)
			_ = c.observeInferredOutcome(cleanupCtx, result.Trace, userID, c.matchID, state, time.Now().UTC())
			cleanupCancel()
			if state == "failed" {
				log.Printf("conversation response delivery error: %v", deliveryErr)
			}
		}
	})
}

// readMessages is the inbound 15-case read loop: identity binding, session
// lifecycle, talkativeness, activity/interrupt, ASR streaming, playback and
// display acknowledgements. It closes the connection context on exit.
func (c *watchConnection) readMessages() {
	defer c.connectionCancel()
	for {
		_, msg, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		var req map[string]interface{}
		if json.Unmarshal(msg, &req) != nil {
			continue
		}
		switch req["type"] {
		case "ping":
			c.writer.SendJSON(map[string]string{"type": "pong"})
		case "identify":
			requested := strings.TrimSpace(str(req, "userId"))
			if c.deps.cfg.SessionAuthRequired() && requested != "" && requested != c.identity.Get() {
				c.writer.SendJSON(map[string]string{"type": "auth_error", "reason": "user identity does not match session"})
				return
			}
			if c.deps.cfg.LegacyAuthAllowed() {
				identifiedUserID := c.identity.Set(str(req, "userId"))
				c.watchSession = c.deps.watchSessions.Bind(c.watchSession, identifiedUserID, c.matchID)
				c.deliveryTracker.BindLedger(c.watchSession.Ledger())
				c.scheduler = c.watchSession.Scheduler()
				c.scheduleRecoveredObservations(c.identity.Get())
			}
		case "set_talkativeness":
			// C3 drift fix 的显式通道：改档即时持久化并 ack——客户端以 ack 为
			// 真源，不再等下一条 user_speech 顺带生效，也不被第二台设备的
			// 本地默认值静默覆盖。
			setUserID := c.identity.Get()
			tier := relationship.NormalizeTalkativeness(str(req, "talkativeness"))
			c.userTalkativeness.Store(tier)
			persisted := false
			if c.deps.memoriesPrefs != nil {
				persistCtx, persistCancel := context.WithTimeout(c.connectionCtx, 3*time.Second)
				defer persistCancel()
				if err := c.deps.memoriesPrefs.RecordTalkativeness(persistCtx, setUserID, tier); err != nil {
					log.Printf("memory: record talkativeness for %q: %v", setUserID, err)
				} else {
					persisted = true
				}
			}
			c.writer.SendJSON(map[string]interface{}{"type": "talkativeness_ack", "tier": tier, "persisted": persisted})
		case "set_character":
			// 三入口一状态（openspec/changes/character-settings）：改互动
			// 规范即时持久化并 ack，与 set_talkativeness 同一模式。
			setUserID := c.identity.Get()
			field := strings.TrimSpace(str(req, "field"))
			value := strings.TrimSpace(str(req, "value"))
			persisted := false
			if c.deps.characterSettings != nil {
				settingsCtx, settingsCancel := context.WithTimeout(c.connectionCtx, 3*time.Second)
				if _, err := c.deps.characterSettings.Set(settingsCtx, setUserID, field, value); err != nil {
					log.Printf("character settings set for %q: %v", setUserID, err)
				} else {
					persisted = true
					// T1 审计：入口层落账（polish-round-3）。
					recordCharacterChange(settingsCtx, c.deps.interactionLedger, c.deps.characterSettings, setUserID, field, value, "ws")
				}
				settingsCancel()
			}
			c.writer.SendJSON(map[string]interface{}{"type": "character_ack", "field": field, "value": value, "persisted": persisted})
		case "user_activity":
			speaking := str(req, "state") == "speaking"
			c.userSpeaking.Store(speaking)
			if speaking {
				c.scheduleLookups.Cancel()
				c.scheduler.Interrupt()
			}
		case "session_opened":
			userID, identityMatches := connectionUserID(c.identity, c.deps.cfg, str(req, "userId"))
			if !identityMatches {
				c.writer.SendJSON(map[string]string{"type": "auth_error", "reason": "user identity does not match session"})
				return
			}
			if userID == "" {
				continue
			}
			c.watchSession = c.deps.watchSessions.Bind(c.watchSession, userID, c.matchID)
			c.deliveryTracker.BindLedger(c.watchSession.Ledger())
			c.scheduler = c.watchSession.Scheduler()
			// Restore the persisted 话痨程度 tier so the proactive
			// behavior survives reconnects (C2 talkativeness wiring).
			if c.deps.memoriesPrefs != nil {
				prefCtx, prefCancel := context.WithTimeout(c.connectionCtx, 2*time.Second)
				if storedTier, err := c.deps.memoriesPrefs.Talkativeness(prefCtx, userID); err == nil {
					c.userTalkativeness.Store(storedTier)
				}
				prefCancel()
			}
			decision, observeErr := c.deps.agent.ObserveSession(c.connectionCtx, "session:"+userID+":"+c.matchID, userID, c.matchID, time.Now().UTC())
			if observeErr != nil {
				log.Printf("relationship session observation error: %v", observeErr)
			} else {
				// presentation-mapping 1.5: the computed hello used to
				// be discarded here (`_, err :=`); deliver it with the
				// same shape the first-meeting coordinator uses.
				deliverSessionOpeningPresentation(c.writer, decision)
			}
			c.scheduleRecoveredObservations(userID)
			c.scheduleRecoveredThreadTurns(userID)
			recoverPendingDeliveries(c.connectionCtx, c.writer, c.watchSession, c.deps.traceReader, userID, c.matchID)

		case "session_closed":
			userID, identityMatches := connectionUserID(c.identity, c.deps.cfg, str(req, "userId"))
			if !identityMatches {
				c.writer.SendJSON(map[string]string{"type": "auth_error", "reason": "user identity does not match session"})
				return
			}
			if userID != "" {
				c.deps.watchSessions.Release(userID, c.matchID)
			}
			c.writer.SendJSON(map[string]string{"type": "session_closed", "reason": str(req, "reason")})

		case "first_meeting":
			userID, identityMatches := connectionUserID(c.identity, c.deps.cfg, str(req, "userId"))
			if !identityMatches {
				c.writer.SendJSON(map[string]string{"type": "auth_error", "reason": "user identity does not match session"})
				return
			}
			if userID == "" {
				continue
			}
			nickname := str(req, "nickname")
			favoriteTeam := str(req, "favoriteTeam")
			firstMeetingSignalID := fmt.Sprintf("first-meeting:%s:%s:%d", userID, c.matchID, time.Now().UnixNano())
			c.scheduler.SubmitProactive("first-meeting:"+userID, conversation.UrgencyNormal, 30*time.Second, func(replyCtx context.Context, playback conversation.Playback) {
				_, err := c.firstMeeting.Handle(replyCtx, companion.FirstMeetingRequest{
					SignalID: firstMeetingSignalID, MatchID: c.matchID, UserID: userID,
					Nickname: nickname, FavoriteTeam: favoriteTeam, Now: time.Now(),
				}, playback)
				if err != nil && !errors.Is(err, context.Canceled) {
					log.Printf("first meeting delivery error: %v", err)
				}
			})

		case "interrupt":
			c.scheduleLookups.Cancel()
			c.scheduler.Interrupt()

			c.writer.SendJSON(map[string]interface{}{
				"type":       "interrupt",
				"expression": "listening",
			})

		case "user_speech":
			text := str(req, "text")
			audioB64 := str(req, "audio")
			userID, identityMatches := connectionUserID(c.identity, c.deps.cfg, str(req, "userId"))
			if !identityMatches {
				c.writer.SendJSON(map[string]string{"type": "auth_error", "reason": "user identity does not match session"})
				return
			}
			if userID == "" {
				c.writer.SendJSON(map[string]interface{}{"type": "voice_status", "state": "failed", "reason": "identity required"})
				continue
			}
			// C2 drift fix: the client has always sent the 话痨程度
			// tier with every user_speech payload; parse it, keep it
			// on the connection and persist it per user.
			tier := relationship.NormalizeTalkativeness(str(req, "talkativeness"))
			c.userTalkativeness.Store(tier)
			if c.deps.memoriesPrefs != nil {
				persistCtx, persistCancel := context.WithTimeout(context.Background(), 3*time.Second)
				go func(persistUserID, persistTier string) {
					defer persistCancel()
					if err := c.deps.memoriesPrefs.RecordTalkativeness(persistCtx, persistUserID, persistTier); err != nil {
						log.Printf("memory: record talkativeness for %q: %v", persistUserID, err)
					}
				}(userID, tier)
			}
			generatedSignalID := fmt.Sprintf("turn_%s_%d", userID, time.Now().UnixNano())
			turnSignalID := stableSignalID(str(req, "signalId"), generatedSignalID)
			// 延迟分解：speech_received = user_speech 到达（voice-transport-
			// upgrade 1.1），锚点供后续 turn_decided / audio_delivered 取 elapsed。
			arrivedAt := time.Now()
			c.storeVoiceLatencyAnchor(turnSignalID, arrivedAt)
			c.logVoiceLatency("speech_received", turnSignalID, arrivedAt)
			c.submitUserTurn(userID, text, audioB64, turnSignalID, "", strings.TrimSpace(str(req, "timezone")))
		case "asr_start":
			userID, identityMatches := connectionUserID(c.identity, c.deps.cfg, str(req, "userId"))
			if !identityMatches {
				c.writer.SendJSON(map[string]string{"type": "auth_error", "reason": "user identity does not match session"})
				return
			}
			utteranceID := strings.TrimSpace(str(req, "utteranceId"))
			generatedSignalID := fmt.Sprintf("turn_%s_%d", userID, time.Now().UnixNano())
			signalID := stableSignalID(str(req, "signalId"), generatedSignalID)
			// 延迟分解：speech_received = 流式转写会话开始（等价 user_speech
			// 到达），双锚点分别供话轮链路与 asr_finish/asr_final 取 elapsed。
			arrivedAt := time.Now()
			c.storeVoiceLatencyAnchor(signalID, arrivedAt)
			c.storeVoiceLatencyAnchor(voiceLatencyAnchorKey("utt", utteranceID), arrivedAt)
			c.logVoiceLatency("speech_received", signalID, arrivedAt)
			if err := validateTranscriptionStart(req); err != nil {
				c.writer.SendJSON(transcriptErrorMessage(utteranceID, err, false))
				continue
			}
			if err := c.transcriptions.Start(utteranceID, signalID, userID, strings.TrimSpace(str(req, "timezone")), voiceRecognitionHints(c.deps.matchStore.Config(c.matchID))); err != nil {
				c.writer.SendJSON(transcriptErrorMessage(utteranceID, err, false))
			}
		case "asr_chunk":
			utteranceID := strings.TrimSpace(str(req, "utteranceId"))
			sequence, ok := intField(req, "sequence")
			audio, err := base64.StdEncoding.DecodeString(str(req, "audio"))
			if !ok || err != nil || len(audio) == 0 || len(audio)%2 != 0 {
				_ = c.transcriptions.Cancel(utteranceID)
				c.writer.SendJSON(transcriptErrorMessage(utteranceID, errors.New("invalid audio chunk"), false))
				continue
			}
			// 气氛旁路（ambient-audio-observation）：合法分片原样并行旁送
			// AED sidecar——不阻塞 ASR 主路，旁路失败静默丢弃+计数。
			c.relayAmbientChunk(audio)
			if err := c.transcriptions.Append(utteranceID, sequence, audio); err != nil {
				c.writer.SendJSON(transcriptErrorMessage(utteranceID, err, false))
			}
		case "asr_finish":
			utteranceID := strings.TrimSpace(str(req, "utteranceId"))
			// 延迟分解：asr_finish = 客户端说完信号到达（相对 asr_start 锚点）。
			c.logVoiceLatency("asr_finish", utteranceID,
				c.voiceLatencyAnchor(voiceLatencyAnchorKey("utt", utteranceID)))
			if err := c.transcriptions.Finish(utteranceID); err != nil {
				c.writer.SendJSON(transcriptErrorMessage(utteranceID, err, false))
			}
		case "asr_cancel":
			utteranceID := strings.TrimSpace(str(req, "utteranceId"))
			_ = c.transcriptions.Cancel(utteranceID)
		case "voice_playback":
			traceID := strings.TrimSpace(str(req, "traceId"))
			state := strings.TrimSpace(str(req, "state"))
			if traceID == "" || state == "" {
				continue
			}
			updateCtx, updateCancel := context.WithTimeout(c.connectionCtx, 3*time.Second)
			userID := c.identity.Get()
			err := recordPlaybackStatus(updateCtx, c.deps.traceReader, c.deps.agent, c.matchID, traceID, userID, state)
			if err == nil {
				c.scheduler.PlaybackChanged(traceID, state)
				c.deliveryTracker.Transition(traceID, deliveryStateForPlayback(state), time.Now().UTC())
				if terminalPlaybackState(state) {
					c.deliveryTracker.Remove(traceID)
				}
				if _, relationshipErr := c.deps.agent.Plan(updateCtx, companion.TurnInput{Kind: companion.TurnKindDelivery, Delivery: &companion.DeliveryInput{SignalID: "delivery:" + traceID + ":" + state, TraceID: traceID, UserID: userID, MatchID: c.matchID, State: state, Purpose: "playback", Now: time.Now().UTC()}}); relationshipErr != nil && !errors.Is(relationshipErr, context.Canceled) {
					log.Printf("relationship delivery observation error: %v", relationshipErr)
				}
			}
			updateCancel()
			if err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("voice playback trace update error: %v", err)
			}
		case "reply_displayed":
			traceID := strings.TrimSpace(str(req, "traceId"))
			userID := c.identity.Get()
			if traceID == "" || userID == "" {
				continue
			}
			updateCtx, updateCancel := context.WithTimeout(c.connectionCtx, 3*time.Second)
			err := recordDisplayedReply(updateCtx, c.deps.traceReader, c.deps.agent, c.matchID, traceID, userID, time.Now().UTC())
			if err == nil {
				if _, ackErr := c.deliveryTracker.Ledger().AcknowledgeText(traceID, time.Now().UTC()); ackErr != nil && !errors.Is(ackErr, conversation.ErrDeliveryNotFound) {
					log.Printf("text acknowledgement error: %v", ackErr)
				}
				c.deliveryTracker.Transition(traceID, conversation.DeliveryTextDelivered, time.Now().UTC())
			}
			updateCancel()
			if err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("reply display observation error: %v", err)
			}
		case "playback_result":
			// 播放终态实报（delivery-outcome-uplink）：deliveryKey 定位投递
			// 记录推进七态；迟到回执只记 client_late，不回改既有终态。
			deliveryKey := strings.TrimSpace(str(req, "deliveryKey"))
			playbackState := strings.TrimSpace(str(req, "state"))
			reportUserID := c.identity.Get()
			if deliveryKey == "" || reportUserID == "" || !validPlaybackResultState(playbackState) {
				continue
			}
			updateCtx, updateCancel := context.WithTimeout(c.connectionCtx, 3*time.Second)
			if _, err := recordPlaybackResult(updateCtx, c.deliveryTracker.Ledger(), c.deps.agent, c.clientPlaybackReports, reportUserID, c.matchID, deliveryKey, playbackState, strings.TrimSpace(str(req, "reason")), time.Now().UTC()); err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("playback result error: %v", err)
			}
			updateCancel()
		case "duplex_event":
			// 播放期抢话遥测实报（voice-duplex 1.4）：判定在客户端，服务端
			// 只收事件落结构化日志，不参与判定、不改变话轮调度。
			event := strings.TrimSpace(str(req, "event"))
			if event == "" {
				continue
			}
			streak, _ := intField(req, "streak")
			log.Printf("voice duplex event: user=%q match=%q event=%q transcript=%q streak=%d",
				c.identity.Get(), c.matchID, event, strings.TrimSpace(str(req, "transcript")), streak)
		}
	}
}

// ── 相 4：teardown ───────────────────────────────────────────────────────────

// serveHeartbeat is the connection watchdog: every 30s it revalidates the
// session claims (auth_error + cancel on expiry/revocation) and pings the
// socket; either failure ends the blocking loop so the defer stack above
// performs the teardown.
func (c *watchConnection) serveHeartbeat() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.connectionCtx.Done():
			return
		case <-ticker.C:
			// 无会话管理器的部署形态（如匿名演示启动）跳过会话复查——
			// nil Manager 上调用会 panic（真机 E2E 2026-09-24 实测）。
			if c.deps.sessions != nil && c.claims.Subject != "" {
				if err := c.deps.sessions.ValidateClaims(c.connectionCtx, c.claims); err != nil {
					c.writer.SendJSON(map[string]string{"type": "auth_error", "reason": "session expired or revoked"})
					c.connectionCancel()
					return
				}
			}
			if err := c.writer.Ping(); err != nil {
				c.connectionCancel()
				return
			}
		}
	}
}

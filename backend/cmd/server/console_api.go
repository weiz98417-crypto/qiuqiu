package main

// Console API (ADR-0008 运营管理台): the three-tier IA data surface for the
// React console — global overview → match layer (users) → user layer
// (portrait + threads) — plus the delivery-interruption feed. Every route
// requires an operator bearer token (operatorAuthz): reads → TraceRead,
// writes → MatchWrite (auditor stays read-only). Thread ops go through the
// idempotency service and append operator-attributed audit rows; portrait
// on-behalf operations honor the privacy lifecycle and are audited. No
// portrait export, no talkativeness writes (owner red lines).

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"qiuqiu/internal/auth"
	"qiuqiu/internal/backchannel"
	"qiuqiu/internal/companion"
	"qiuqiu/internal/config"
	"qiuqiu/internal/conversation"
	"qiuqiu/internal/interaction"
	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/memory"
	"qiuqiu/internal/observation"
	"qiuqiu/internal/operatorauth"
	"qiuqiu/internal/operatorwrite"
	"qiuqiu/internal/relationship"
	"qiuqiu/internal/tts"
	"qiuqiu/internal/ttssupply"
)

// Scope mapping for console routes (ADR-0008): reads → TraceRead, writes →
// MatchWrite (auditor stays read-only).
const (
	traceReadScope  = auth.ScopeOperatorTraceRead
	matchWriteScope = auth.ScopeOperatorMatchWrite
)

// talkativenessReader reads the persisted user preference tier; nil (or a
// failing store) degrades to "normal" — talkativeness is read-only here.
type talkativenessReader interface {
	Talkativeness(ctx context.Context, userID string) (string, error)
}

type consoleAPI struct {
	cfg               *config.Config
	authz             operatorAuthz
	matches           matchstate.Repository
	traces            companion.TraceReader
	ledger            interaction.Ledger
	sessions          *conversation.WatchSessionRegistry
	memories          *memory.Queue
	operators         operatorauth.Directory
	preferences       talkativenessReader
	characterSettings *relationship.CharacterSettings
	writes            *operatorwrite.Service
	interruptions     *interruptionRing
	// clientHealth 是客户端语音健康遥测账本（快修 P1 观测面）；nil 时端点
	// 返回空快照——无遥测不报错，面板显示零。
	clientHealth *observation.ClientHealthLedger
	// 语音供给三态（tts-supply-switch）：supply 运行中即时生效；localProbe
	// 是本地腿健康门控（nil=本地腿未配置，选项置灰）；supplySettings 持久
	// 化（nil 等价内存态——重启回 cloud）。
	supply         *tts.SupplySwitch
	localProbe     *tts.LocalProbe
	supplySettings ttssupply.Store
	// ADR-0010 human channel: the HS256 signing secret (QIUQIU_JWT_SECRET).
	jwtSecret string
}

// Response shapes — the React console is built against exactly these.

type consoleMatch struct {
	MatchID     string `json:"matchId"`
	State       string `json:"state"`
	OnlineUsers int    `json:"onlineUsers"`
}

type operatorAuditRow struct {
	OperatorName string `json:"operatorName"`
	Action       string `json:"action"`
	Object       string `json:"object"`
	CreatedAt    string `json:"createdAt"`
}

type consoleMemoryHealth struct {
	Degraded     bool               `json:"degraded"`
	BacklogDepth int                `json:"backlogDepth"`
	RecentAudit  []operatorAuditRow `json:"recentAudit"`
}

type consoleThreadAging struct {
	Today  int `json:"today"`
	D1to3  int `json:"d1to3"`
	D3plus int `json:"d3plus"`
}

type consoleProactiveCitation struct {
	TraceID   string `json:"traceId"`
	MatchID   string `json:"matchId"`
	Citation  string `json:"citation"`
	CreatedAt string `json:"createdAt"`
}

type consoleUser struct {
	UserID            string            `json:"userId"`
	Online            bool              `json:"online"`
	Talkativeness     string            `json:"talkativeness"`
	OpenThreads       int               `json:"openThreads"`
	PortraitUpdatedAt string            `json:"portraitUpdatedAt,omitempty"`
	Preferences       map[string]string `json:"preferences,omitempty"`
}

type consoleThread struct {
	ID             string `json:"id"`
	UserID         string `json:"userId"`
	Kind           string `json:"kind"`
	Content        string `json:"content"`
	State          string `json:"state"`
	LedgerSequence int64  `json:"ledgerSequence"`
	CreatedAt      string `json:"createdAt"`
}

type consolePortraitEntry struct {
	Topic     string `json:"topic"`
	SubTopic  string `json:"subTopic"`
	Content   string `json:"content"`
	UpdatedAt string `json:"updatedAt"`
}

type consolePortrait struct {
	UpdatedAt string                 `json:"updatedAt,omitempty"`
	Entries   []consolePortraitEntry `json:"entries"`
}

func handleConsoleAPI(deps consoleAPI) http.HandlerFunc {
	return mountRoutes(deps.cfg, deps.authz, consoleRoutes(deps))
}

// consoleRoutes 是 console 面（ADR-0008）的声明式路由表：一行一个端点，
// scope 走 withScope（读 → TraceRead，写 → MatchWrite，auditor 只读）。
// scope 为空的端点与迁移前一致：login 是认证入口本身（无鉴权）；whoami 与
// me/password 只解析身份不做 scope 检查；refresh/logout 以 refresh token
// 为凭证。行为差异（405 等）统一记录在 router.go。
func consoleRoutes(deps consoleAPI) []route {
	return []route{
		{method: http.MethodGet, pattern: "/api/console/overview", scope: traceReadScope, handler: deps.handleOverview},
		// 客户端语音健康遥测（快修 P1 观测面）：聚合计数 + 最近事件,
		// Operations Observation 纪律——不见用户身份与正文。
		{method: http.MethodGet, pattern: "/api/console/client-health", scope: traceReadScope, handler: deps.handleClientHealth},
		{method: http.MethodGet, pattern: "/api/console/matches/{matchId}/users", scope: traceReadScope, handler: deps.handleMatchUsers},
		{method: http.MethodGet, pattern: "/api/console/threads", scope: traceReadScope, handler: deps.handleListThreads},
		{method: http.MethodPatch, pattern: "/api/console/threads/{threadId}", scope: matchWriteScope, handler: deps.handlePatchThread},
		{method: http.MethodGet, pattern: "/api/console/users/{userId}/portrait", scope: traceReadScope, handler: deps.handleGetPortrait},
		{method: http.MethodDelete, pattern: "/api/console/users/{userId}/portrait", scope: matchWriteScope, handler: deps.handleDeletePortrait},
		{method: http.MethodGet, pattern: "/api/console/delivery-interruptions", scope: traceReadScope, handler: deps.handleDeliveryInterruptions},
		// 运营观测页运行时配置（operations-metrics-stack）：grafanaUrl 空 =
		// 前端渲染部署指引占位。
		{method: http.MethodGet, pattern: "/api/console/config", scope: traceReadScope, handler: deps.handleConsoleConfig},
		// 语音供给三态（tts-supply-switch）：读=状态+健康门控快照；写=幂等
		// 写+审计+即时生效（写入校验本地腿健康门控，MatchSettings 同款纪律）。
		{method: http.MethodGet, pattern: "/api/console/tts-supply", scope: traceReadScope, handler: deps.handleGetTTSSupply},
		{method: http.MethodPatch, pattern: "/api/console/tts-supply", scope: matchWriteScope, handler: deps.handlePatchTTSSupply},
		{method: http.MethodGet, pattern: "/api/console/operators", scope: matchWriteScope, handler: deps.handleListOperators},
		{method: http.MethodPost, pattern: "/api/console/operators", scope: matchWriteScope, handler: deps.handleCreateOperator},
		{method: http.MethodGet, pattern: "/api/console/whoami", handler: deps.handleWhoami},
		{method: http.MethodDelete, pattern: "/api/console/operators/{operatorName}", scope: matchWriteScope, handler: deps.handleDeleteOperator},
		// ADR-0010 human auth channel: login/refresh/logout + self-service
		// password change. Login is unauthenticated (it IS the auth step).
		{method: http.MethodPost, pattern: "/api/console/auth/login", handler: deps.handleLogin},
		{method: http.MethodPost, pattern: "/api/console/auth/refresh", handler: deps.handleRefresh},
		{method: http.MethodPost, pattern: "/api/console/auth/logout", handler: deps.handleLogout},
		{method: http.MethodPatch, pattern: "/api/console/me/password", handler: deps.handleMePassword},
	}
}

// Operators management (ADR-0008): director-only. The plaintext token is
// returned exactly once at creation; the store keeps only its SHA-256 hash.
// A store without persistent management (no DATABASE_URL) answers 501 — the
// console keeps operating on the legacy APP_TOKEN mode instead.

// handleListOperators lists every operator row (director only).
func (deps consoleAPI) handleListOperators(w http.ResponseWriter, r *http.Request) {
	lister, ok := deps.operators.(operatorauth.OperatorLister)
	if !ok {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "operators management requires a persistent operator store (DATABASE_URL)"})
		return
	}
	operators, err := lister.List(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"operators": operators})
}

// handleCreateOperator mints a personal token for a new operator. The token
// is returned exactly once; only its SHA-256 hash is stored.
func (deps consoleAPI) handleCreateOperator(w http.ResponseWriter, r *http.Request) {
	claims, ok := operatorClaims(deps.authz, w, r)
	if !ok {
		return
	}
	var request struct {
		Name string            `json:"name"`
		Role operatorauth.Role `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	token := newOperatorToken()
	operator, err := deps.operators.Seed(r.Context(), request.Name, token, request.Role)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, operatorauth.ErrDuplicateName) || errors.Is(err, operatorauth.ErrUnknownRole) || errors.Is(err, operatorauth.ErrNameRequired) {
			status = http.StatusBadRequest
		}
		http.Error(w, err.Error(), status)
		return
	}
	deps.appendAudit(claims, "operator.create", request.Name)
	response := map[string]any{"operator": operator, "token": token}
	// ADR-0010 lifecycle: a director-issued temp password rides along (shown
	// once); first login forces a change. Token-only stores skip this.
	if temporary, ok := deps.issueTemporaryPassword(w, r, operator); ok {
		response["temporaryPassword"] = temporary
	}
	writeJSON(w, http.StatusOK, response)
}

// appendAudit 是运营审计三段式的收敛（operations-live-stream 3.5）：3s 独立
// 预算 + 写审计 + 失败仅记日志——业务已落地，审计失败不回滚也不拖响应。
func (deps consoleAPI) appendAudit(operator auth.Claims, intent, object string) {
	if deps.operators == nil {
		return
	}
	auditCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := deps.operators.AppendAudit(auditCtx, operatorName(operator), intent, object); err != nil {
		log.Printf("console: append %s audit for %q: %v", intent, operatorName(operator), err)
	}
} // handleDeleteOperator revokes an operator (row deletion, immediate).
func (deps consoleAPI) handleDeleteOperator(w http.ResponseWriter, r *http.Request) {
	claims, ok := operatorClaims(deps.authz, w, r)
	if !ok {
		return
	}
	name := r.PathValue("operatorName")
	revoker, ok := deps.operators.(operatorauth.OperatorRevoker)
	if !ok {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "operators management requires a persistent operator store (DATABASE_URL)"})
		return
	}
	deleted, err := revoker.Delete(r.Context(), name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !deleted {
		http.Error(w, "operator not found", http.StatusNotFound)
		return
	}
	deps.appendAudit(claims, "operator.revoke", name)
	writeJSON(w, http.StatusOK, map[string]any{})
}

// handleConsoleConfig 返回观测页需要的运行时配置（operations-metrics-stack）：
// grafanaUrl 为空 = 观测页渲染部署指引占位（与 Operators 页 501 降级同模
// 式）。非空时返回**同源前缀** /grafana（后端把该前缀反代到
// QIUQIU_GRAFANA_URL）——同源 iframe 让前端能注入 CSS 隐藏 Grafana 控制
// 条，且不依赖 allow_embedding。
func (deps consoleAPI) handleConsoleConfig(w http.ResponseWriter, r *http.Request) {
	grafanaURL := ""
	if strings.TrimSpace(os.Getenv("QIUQIU_GRAFANA_URL")) != "" {
		grafanaURL = "/grafana"
	}
	writeJSON(w, http.StatusOK, map[string]any{"grafanaUrl": grafanaURL})
}

// handleWhoami reports the authenticated operator identity (name, role via
// subject, scopes) — the console header uses it after token load.
func (deps consoleAPI) handleWhoami(w http.ResponseWriter, r *http.Request) {
	claims, ok := deps.authz.claims(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": operatorName(claims), "subject": claims.Subject, "scopes": claims.Scopes})
}

func newOperatorToken() string {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand failure is unrecoverable; an empty token would be a
		// security hole, so fail loudly instead.
		panic(err)
	}
	return hex.EncodeToString(buf)
}

// consoleClientHealthEvent 是遥测事件的 wire 形状：已裁剪——无用户身份、
// 无正文，只有种类/时间/比赛归属（Operations Observation 纪律）。
type consoleClientHealthEvent struct {
	Kind    string `json:"kind"`
	At      string `json:"at"`
	MatchID string `json:"matchId,omitempty"`
}

// handleClientHealth 返回客户端语音健康遥测快照（快修 P1 观测面）：聚合
// 计数 + 最近事件（新在前）。账本缺席（nil）返回空快照——面板显示零而非报错。
func (deps consoleAPI) handleClientHealth(w http.ResponseWriter, r *http.Request) {
	counters, recent := deps.clientHealth.Snapshot()
	events := make([]consoleClientHealthEvent, len(recent))
	for i, event := range recent {
		events[i] = consoleClientHealthEvent{
			Kind:    event.Kind,
			At:      event.At.Format(time.RFC3339),
			MatchID: event.MatchID,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"counters": counters, "recent": events})
}

func (deps consoleAPI) handleOverview(w http.ResponseWriter, r *http.Request) {
	onlineSessions, usersByMatch := deps.sessions.ConsoleSnapshot()

	matches := make([]consoleMatch, 0, 8)
	for _, summary := range deps.catalog() {
		onlineUsers := 0
		for _, user := range usersByMatch[summary.MatchID] {
			if user.Online {
				onlineUsers++
			}
		}
		matches = append(matches, consoleMatch{
			MatchID:     summary.MatchID,
			State:       summary.Status,
			OnlineUsers: onlineUsers,
		})
	}

	degraded, backlogDepth, _ := deps.memories.Health()
	health := consoleMemoryHealth{Degraded: degraded, BacklogDepth: backlogDepth, RecentAudit: []operatorAuditRow{}}
	if deps.operators != nil {
		if entries, err := deps.operators.RecentAudit(r.Context(), 5); err == nil {
			for _, entry := range entries {
				health.RecentAudit = append(health.RecentAudit, operatorAuditRow{
					OperatorName: entry.OperatorName,
					Action:       entry.Action,
					Object:       entry.Object,
					CreatedAt:    entry.CreatedAt.UTC().Format(time.RFC3339),
				})
			}
		}
	}

	aging := consoleThreadAging{}
	if threads, err := deps.memories.ListThreads(r.Context(), "", "open"); err == nil {
		now := time.Now().UTC()
		for _, thread := range threads {
			age := now.Sub(thread.CreatedAt)
			switch {
			case age < 24*time.Hour:
				aging.Today++
			case age < 72*time.Hour:
				aging.D1to3++
			default:
				aging.D3plus++
			}
		}
	}

	overviewSummaries := deps.catalog()
	writeJSON(w, http.StatusOK, map[string]any{
		"matches":         matches,
		"onlineSessions":  onlineSessions,
		"memory":          health,
		"threadAging":     aging,
		"recentProactive": deps.recentProactive(r.Context(), overviewSummaries),
		"router":          deps.routerFunnel(r.Context()),
		"backchannel":     deps.backchannelPulse(r.Context(), overviewSummaries),
	})
}

// consoleBackchannelPulse 是微反应通道的运营观测（operations-turn-replay）：
// 滚动 24h 窗口的发出数与白名单事件数。Decide 的拒绝路径（quiet/限频/
// 风暴去重）无痕，不承诺限频归因——那是留尾。
type consoleBackchannelPulse struct {
	Emitted         int `json:"emitted"`
	WhitelistEvents int `json:"whitelistEvents"`
	WindowHours     int `json:"windowHours"`
}

// backchannelPulse 扫描全部比赛的微反应账本行与白名单事件；读失败降级为
// 零值，从不让 overview 变红（与 routerFunnel 同纪律）。账本行优先走
// match 快照接口（跨用户全量）——List(userID="") 在内存实现里是双键过滤，
// 查不到任何行。
func (deps consoleAPI) backchannelPulse(ctx context.Context, summaries []matchstate.MatchSummary) consoleBackchannelPulse {
	pulse := consoleBackchannelPulse{WindowHours: 24}
	if deps.ledger == nil || deps.matches == nil {
		return pulse
	}
	cutoff := time.Now().UTC().Add(-24 * time.Hour)
	snapshotLedger, hasSnapshot := deps.ledger.(interaction.MatchSnapshotLedger)
	for _, summary := range summaries {
		if hasSnapshot {
			if events, err := snapshotLedger.ListMatchSnapshot(ctx, summary.MatchID); err == nil {
				for _, event := range events {
					if event.Kind == interaction.KindBackchannel && event.CreatedAt.After(cutoff) {
						pulse.Emitted++
					}
				}
			}
		} else if events, err := deps.ledger.List(ctx, "", summary.MatchID, 100); err == nil {
			for _, event := range events {
				if event.Kind == interaction.KindBackchannel && event.CreatedAt.After(cutoff) {
					pulse.Emitted++
				}
			}
		}
		for _, event := range deps.matches.PublicEvents(summary.MatchID) {
			if !backchannel.Whitelisted(event.EventType) {
				continue
			}
			if created, err := time.Parse(time.RFC3339, event.CreatedAt); err == nil && created.After(cutoff) {
				pulse.WhitelistEvents++
			}
		}
	}
	return pulse
}

// consoleRouterFunnel is the intent-router C3 vocabulary funnel on the
// operator overview: the rolling 没接明白率 (unknown turns / total user turns)
// and the newest unroutable samples worth keyword or prompt work.
type consoleRouterFunnel struct {
	UnknownTurns  int                    `json:"unknownTurns"`
	TotalTurns    int                    `json:"totalTurns"`
	UnknownRate   float64                `json:"unknownRate"`
	TopUnroutable []consoleUnroutableRow `json:"topUnroutable"`
}

type consoleUnroutableRow struct {
	UserID    string `json:"userId"`
	Content   string `json:"content"`
	CreatedAt string `json:"createdAt"`
}

// routerFunnel scans the same rolling window recentProactive uses (20
// matches × latest 100 traces); a read hiccup degrades to zeros, never a
// console error.
func (deps consoleAPI) routerFunnel(ctx context.Context) consoleRouterFunnel {
	const (
		maxMatches  = 20
		perMatch    = 100
		maxSamples  = 5
		rollingLeft = -24 * time.Hour
	)
	funnel := consoleRouterFunnel{TopUnroutable: []consoleUnroutableRow{}}
	since := time.Now().UTC().Add(rollingLeft)
	for index, summary := range deps.catalog() {
		if index >= maxMatches {
			break
		}
		traces, err := deps.listTraces(ctx, summary.MatchID, perMatch)
		if err != nil {
			continue
		}
		for _, trace := range traces {
			if trace.CreatedAt.Before(since) || strings.TrimSpace(trace.Input) == "" {
				continue
			}
			funnel.TotalTurns++
			if trace.Intent == companion.IntentUnknown {
				funnel.UnknownTurns++
			}
		}
	}
	if funnel.TotalTurns > 0 {
		funnel.UnknownRate = math.Round(float64(funnel.UnknownTurns)/float64(funnel.TotalTurns)*1000) / 1000
	}
	if deps.memories != nil {
		if threads, err := deps.memories.ListThreads(ctx, "", "open"); err == nil {
			sort.SliceStable(threads, func(left, right int) bool {
				return threads[left].CreatedAt.After(threads[right].CreatedAt)
			})
			for _, thread := range threads {
				if len(funnel.TopUnroutable) >= maxSamples {
					break
				}
				if thread.Kind != memory.ThreadUnroutable {
					continue
				}
				funnel.TopUnroutable = append(funnel.TopUnroutable, consoleUnroutableRow{
					UserID:    thread.UserID,
					Content:   thread.Content,
					CreatedAt: thread.CreatedAt.UTC().Format(time.RFC3339),
				})
			}
		}
	}
	return funnel
}

func (deps consoleAPI) handleMatchUsers(w http.ResponseWriter, r *http.Request) {
	matchID := r.PathValue("matchId")
	_, usersByMatch := deps.sessions.ConsoleSnapshot()
	users := make([]consoleUser, 0, len(usersByMatch[matchID]))
	for _, entry := range usersByMatch[matchID] {
		users = append(users, deps.consoleUser(r.Context(), entry))
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

func (deps consoleAPI) consoleUser(ctx context.Context, entry conversation.OnlineUser) consoleUser {
	user := consoleUser{
		UserID:        entry.UserID,
		Online:        entry.Online,
		Talkativeness: "normal",
	}
	if deps.preferences != nil {
		prefCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if tier, err := deps.preferences.Talkativeness(prefCtx, entry.UserID); err == nil && tier != "" {
			user.Talkativeness = tier
		}
	}
	if deps.characterSettings != nil {
		settingsCtx, settingsCancel := context.WithTimeout(ctx, 2*time.Second)
		defer settingsCancel()
		if values, err := deps.characterSettings.Get(settingsCtx, entry.UserID); err == nil {
			prefs := map[string]string{}
			for field, value := range values {
				if value != "" {
					prefs[string(field)] = value
				}
			}
			if len(prefs) > 0 {
				user.Preferences = prefs
			}
		}
	}
	if threads, err := deps.memories.Threads(ctx, entry.UserID); err == nil {
		user.OpenThreads = len(threads)
	}
	if _, updatedAt := deps.memories.PortraitEntries(ctx, entry.UserID); !updatedAt.IsZero() {
		user.PortraitUpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	}
	return user
}

func (deps consoleAPI) handleListThreads(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(r.URL.Query().Get("userId"))
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	threads, err := deps.memories.ListThreads(r.Context(), userID, state)
	if err != nil {
		// No thread store (or a store hiccup): the ledger reads degrade to an
		// empty list, never a console error page.
		if !errors.Is(err, memory.ErrNotSupported) && !errors.Is(err, memory.ErrUnavailable) {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		threads = nil
	}
	writeJSON(w, http.StatusOK, map[string]any{"threads": consoleThreads(threads)})
}

func (deps consoleAPI) handlePatchThread(w http.ResponseWriter, r *http.Request) {
	operator, ok := operatorClaims(deps.authz, w, r)
	if !ok {
		return
	}
	threadID := r.PathValue("threadId")
	var request struct {
		Action string `json:"action"`
	}
	body, err := decodeOperatorJSON(w, r, &request)
	if err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	action := strings.TrimSpace(request.Action)
	if action != "address" && action != "expire" {
		http.Error(w, "action must be address or expire", http.StatusBadRequest)
		return
	}
	// Console writes go through the idempotency service like every operator
	// write; the audit row carries the authenticated operator's name and is
	// appended once per distinct operation (replays skip the callback).
	// Threads are account-scoped, not match-scoped, so the idempotency
	// partition key is the literal "console".
	executeOperatorWrite(w, r, deps.writes, "console", "console.thread."+action, body, func(callbackCtx context.Context) (operatorwrite.Response, error) {
		var updated memory.Thread
		var threadErr error
		if action == "address" {
			// MarkThreadAddressed no-ops on unknown/already-closed threads
			// (store-side idempotency); the console still 404s unknown ids by
			// resolving the row afterwards.
			threadErr = deps.memories.MarkThreadAddressed(callbackCtx, threadID)
			if threadErr == nil {
				var found bool
				updated, found = deps.memories.ThreadByID(callbackCtx, threadID)
				if !found {
					threadErr = memory.ErrNotFound
				}
			}
		} else {
			updated, threadErr = deps.memories.ExpireThread(callbackCtx, threadID)
		}
		if threadErr != nil {
			switch {
			case errors.Is(threadErr, memory.ErrNotFound):
				return operatorwrite.Response{}, operatorError(http.StatusNotFound, threadErr)
			case errors.Is(threadErr, memory.ErrNotSupported):
				return operatorwrite.Response{}, operatorError(http.StatusNotImplemented, threadErr)
			default:
				return operatorwrite.Response{}, operatorError(http.StatusInternalServerError, threadErr)
			}
		}
		deps.appendAudit(operator, "thread."+action, "thread:"+threadID)
		return operatorwrite.JSONResponse(http.StatusOK, map[string]any{"thread": consoleThreadRow(updated)})
	})
}

func (deps consoleAPI) handleGetPortrait(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userId")
	// On-behalf view shows exactly what the next turn sees — entries only,
	// no export of anything beyond the portrait slots.
	entries, updatedAt := deps.memories.PortraitEntries(r.Context(), userID)
	portrait := consolePortrait{Entries: []consolePortraitEntry{}}
	if !updatedAt.IsZero() {
		portrait.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	}
	for _, entry := range entries {
		portrait.Entries = append(portrait.Entries, consolePortraitEntry{
			Topic:     entry.Topic,
			SubTopic:  entry.SubTopic,
			Content:   entry.Content,
			UpdatedAt: entry.UpdatedAt.UTC().Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, portrait)
}

func (deps consoleAPI) handleDeletePortrait(w http.ResponseWriter, r *http.Request) {
	operator, ok := operatorClaims(deps.authz, w, r)
	if !ok {
		return
	}
	userID := r.PathValue("userId")
	topic := strings.TrimSpace(r.URL.Query().Get("topic"))
	subTopic := strings.TrimSpace(r.URL.Query().Get("subTopic"))
	// Detached timeout: the tombstone must survive a client disconnect.
	writeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var err error
	object := "user:" + userID
	if topic == "" {
		err = deps.memories.ForgetPortrait(writeCtx, userID)
	} else {
		if subTopic == "" {
			http.Error(w, "subTopic is required to forget one slot", http.StatusBadRequest)
			return
		}
		err = deps.memories.ForgetPortraitEntry(writeCtx, userID, topic, subTopic, "")
		object += " slot " + topic + "/" + subTopic
	}
	if err != nil {
		writePortraitMutationError(w, err, "画像删除失败")
		return
	}
	// On-behalf privacy-ops are always attributable (ADR-0008 red line).
	deps.appendAudit(operator, "portrait.delete", object)
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (deps consoleAPI) handleDeliveryInterruptions(w http.ResponseWriter, r *http.Request) {
	recent := deps.interruptions.Recent()
	payload := make([]map[string]any, 0, len(recent))
	for _, item := range recent {
		payload = append(payload, map[string]any{
			"traceId": item.TraceID,
			"matchId": item.MatchID,
			"at":      item.At.UTC().Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"recent": payload})
}

// catalog lists matches from the store (live first); unavailable catalogs
// degrade to an empty list.
func (deps consoleAPI) catalog() []matchstate.MatchSummary {
	catalogStore, ok := deps.matches.(matchstate.CatalogRepository)
	if !ok {
		return nil
	}
	summaries, err := catalogStore.PublicMatchCatalog()
	if err != nil {
		return nil
	}
	return summaries
}

// recentProactive collects the latest proactive turns across matches: traces
// whose reason codes cite a proactive_citation, newest first, capped at 10.
func (deps consoleAPI) recentProactive(ctx context.Context, summaries []matchstate.MatchSummary) []consoleProactiveCitation {
	const (
		maxMatches = 20
		perMatch   = 50
		maxTotal   = 10
	)
	citations := make([]consoleProactiveCitation, 0, maxTotal)
	for index, summary := range summaries {
		if index >= maxMatches {
			break
		}
		traces, err := deps.listTraces(ctx, summary.MatchID, perMatch)
		if err != nil {
			continue
		}
		for _, trace := range traces {
			citation, ok := firstProactiveCitation(trace)
			if !ok {
				continue
			}
			citations = append(citations, consoleProactiveCitation{
				TraceID:   trace.ID,
				MatchID:   fallbackString(trace.MatchID, summary.MatchID),
				Citation:  citation,
				CreatedAt: trace.CreatedAt.UTC().Format(time.RFC3339),
			})
		}
	}
	sort.SliceStable(citations, func(left, right int) bool {
		return citations[left].CreatedAt > citations[right].CreatedAt
	})
	if len(citations) > maxTotal {
		citations = citations[:maxTotal]
	}
	return citations
}

func (deps consoleAPI) listTraces(ctx context.Context, matchID string, limit int) ([]companion.Trace, error) {
	if deps.ledger != nil {
		return listInteractionTraces(ctx, deps.ledger, matchID, limit)
	}
	if deps.traces == nil {
		return nil, companion.ErrTraceNotFound
	}
	return deps.traces.ListTraces(ctx, matchID, limit)
}

// proactiveCitationPrefix is the reason-code namespace the companion kernel
// stamps on proactive turns that carry a citation (open thread or match
// event): "proactive_citation:<citation>".
const proactiveCitationPrefix = "proactive_citation:"

// firstProactiveCitation extracts the citation value from a trace's reason
// codes; ok=false when the trace did not cite anything.
func firstProactiveCitation(trace companion.Trace) (string, bool) {
	if trace.RelationshipDecision == nil {
		return "", false
	}
	for _, reason := range trace.RelationshipDecision.ReasonCodes {
		if strings.HasPrefix(reason, proactiveCitationPrefix) {
			return strings.TrimPrefix(reason, proactiveCitationPrefix), true
		}
	}
	return "", false
}

// filterTracesByCitationPrefix keeps traces whose proactive_citation citation
// starts with the given prefix (traces API `citation=` filter).
//
// The stored citation value is the part AFTER the "proactive_citation:"
// namespace, so a query prefix that carries the namespace must have it
// stripped before matching — otherwise the UI's default query
// ("proactive_citation:") would match nothing even when citations exist.
func filterTracesByCitationPrefix(traces []companion.Trace, prefix string) []companion.Trace {
	prefix = strings.TrimPrefix(prefix, proactiveCitationPrefix)
	filtered := make([]companion.Trace, 0, len(traces))
	for _, trace := range traces {
		citation, ok := firstProactiveCitation(trace)
		if !ok || !strings.HasPrefix(citation, prefix) {
			continue
		}
		filtered = append(filtered, trace)
	}
	return filtered
}

func consoleThreads(threads []memory.Thread) []consoleThread {
	rows := make([]consoleThread, 0, len(threads))
	for _, thread := range threads {
		rows = append(rows, consoleThreadRow(thread))
	}
	return rows
}

func consoleThreadRow(thread memory.Thread) consoleThread {
	createdAt := ""
	if !thread.CreatedAt.IsZero() {
		createdAt = thread.CreatedAt.UTC().Format(time.RFC3339)
	}
	return consoleThread{
		ID:             thread.ID,
		UserID:         thread.UserID,
		Kind:           string(thread.Kind),
		Content:        thread.Content,
		State:          thread.State,
		LedgerSequence: thread.LedgerSequence,
		CreatedAt:      createdAt,
	}
}

// ttsSupplyState 是运营台「语音供给」卡的状态面（tts-supply-switch）：
// 当前三态、本地腿健康门控快照与回退计数。
type ttsSupplyState struct {
	Mode            string                 `json:"mode"`
	Local           tts.LocalProbeSnapshot `json:"local"`
	LocalSelectable bool                   `json:"localSelectable"`
	LocalFailures   int64                  `json:"localFailures"`
	CloudFallbacks  int64                  `json:"cloudFallbacks"`
}

// handleGetTTSSupply 读当前三态与本地腿健康（门控未过=选项置灰+原因）。
func (deps consoleAPI) handleGetTTSSupply(w http.ResponseWriter, r *http.Request) {
	if deps.supply == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "tts supply switch is not wired"})
		return
	}
	writeJSON(w, http.StatusOK, ttsSupplyState{
		Mode:            deps.supply.Mode(),
		Local:           deps.localProbe.Snapshot(),
		LocalSelectable: deps.localProbe.Available(),
		LocalFailures:   deps.supply.LocalFailures(),
		CloudFallbacks:  deps.supply.CloudFallbacks(),
	})
}

// handlePatchTTSSupply 切换三态：幂等写（Idempotency-Key）+ 写入校验
// （mode 原值；local/local_first 需健康门通过——409 带原因）+ 持久化 +
// 运行中即时生效 + 审计落账。
func (deps consoleAPI) handlePatchTTSSupply(w http.ResponseWriter, r *http.Request) {
	if deps.supply == nil {
		http.Error(w, "tts supply switch is not wired", http.StatusNotImplemented)
		return
	}
	claims, ok := operatorClaims(deps.authz, w, r)
	if !ok {
		return
	}
	body, err := decodeOperatorJSON(w, r, &struct {
		Mode string `json:"mode"`
	}{})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	executeOperatorWrite(w, r, deps.writes, "console", "tts_supply.update", body, func(ctx context.Context) (operatorwrite.Response, error) {
		var request struct {
			Mode string `json:"mode"`
		}
		if err := json.Unmarshal(body, &request); err != nil {
			return operatorwrite.Response{}, operatorError(http.StatusBadRequest, err)
		}
		mode := strings.TrimSpace(request.Mode)
		if !tts.ValidSupplyMode(mode) {
			return operatorwrite.Response{}, operatorError(http.StatusBadRequest, errors.New("mode must be one of cloud/local/local_first"))
		}
		if mode != tts.SupplyModeCloud && !deps.localProbe.Available() {
			return operatorwrite.Response{}, operatorError(http.StatusConflict, fmt.Errorf("local engine is not healthy: %s", deps.localProbe.Snapshot().Reason))
		}
		if err := deps.supplySettings.Save(ctx, mode, operatorName(claims)); err != nil {
			return operatorwrite.Response{}, operatorError(http.StatusInternalServerError, err)
		}
		deps.supply.SetMode(mode)
		deps.appendAudit(claims, "tts_supply.update", mode)
		return operatorwrite.JSONResponse(http.StatusOK, ttsSupplyState{
			Mode:            deps.supply.Mode(),
			Local:           deps.localProbe.Snapshot(),
			LocalSelectable: deps.localProbe.Available(),
			LocalFailures:   deps.supply.LocalFailures(),
			CloudFallbacks:  deps.supply.CloudFallbacks(),
		})
	})
}

package main

// 比赛运营 API（openspec/changes/server-surface-split）：请求形状是
// ADR-0011 冻结基线（28 条 operator-control evals 回归网）。路由由
// matchRoutes 注册表 + Go 1.22+ ServeMux method+wildcard pattern 声明
// （router.go），鉴权走 withScope；原 25-case switch 的内联 case 体原样
// 搬进具名方法，纯搬移零行为变化。

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"qiuqiu/internal/asr"
	"qiuqiu/internal/auth"
	"sort"
	"strconv"
	"strings"
	"time"

	"qiuqiu/internal/companion"
	"qiuqiu/internal/config"
	"qiuqiu/internal/datasource"
	"qiuqiu/internal/directordraft"
	"qiuqiu/internal/evals"
	"qiuqiu/internal/interaction"
	"qiuqiu/internal/llm"
	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/operatorwrite"
)

// matchStateErrorStatus 是比赛运营 API 唯一的领域错误→HTTP 状态映射表
// （server-residual-polish 1.2：此前 ErrNotFound→404 / ErrConflict→409 的
// 判定散在 9 处内联 if/else）。ErrClockVersionConflict 与 ErrConflict 同为
// 冲突语义；其余一律 400。
func matchStateErrorStatus(err error) int {
	switch {
	case errors.Is(err, matchstate.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, matchstate.ErrConflict), errors.Is(err, matchstate.ErrClockVersionConflict):
		return http.StatusConflict
	default:
		return http.StatusBadRequest
	}
}

// writeMatchStateError 把映射结果直接写给客户端（幂等写包裹之外的调用面；
// executeOperatorWrite 回调内走 matchStateWriteError，由幂等信封写响应体）。
func writeMatchStateError(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), matchStateErrorStatus(err))
}

// matchStateWriteError 携带映射后的状态，供 executeOperatorWrite 回调返回。
func matchStateWriteError(err error) error {
	return operatorError(matchStateErrorStatus(err), err)
}

// directorDraftErrorStatus 是语音草稿构建失败的映射变体：输入问题 400、
// 服务未配置 503、上游 LLM/ASR 故障 502。
func directorDraftErrorStatus(err error) int {
	switch {
	case errors.Is(err, directordraft.ErrNoInput), errors.Is(err, directordraft.ErrInvalidTranscript):
		return http.StatusBadRequest
	case errors.Is(err, directordraft.ErrNotConfigured), errors.Is(err, asr.ErrNotConfigured):
		return http.StatusServiceUnavailable
	default:
		return http.StatusBadGateway
	}
}

// directorDraftWriteError 携带草稿映射后的状态，供 executeOperatorWrite 回调返回。
func directorDraftWriteError(err error) error {
	return operatorError(directorDraftErrorStatus(err), err)
}

func handleMatchAPI(store matchstate.Repository, traceReader companion.TraceReader, demoResetter companion.DemoResetter, cfg *config.Config, llmClient *llm.Client) http.HandlerFunc {
	// 塌缩后的唯一入口：此前五层洋葱只在这里汇合。
	return handleMatchAPIWithOperatorAuth(store, traceReader, demoResetter, cfg, llmClient, nil, nil, nil, operatorAuthz{cfg: cfg}, nil)
}

func handleMatchCatalog(store matchstate.Repository, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !applyCORS(w, r, cfg) {
			return
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		catalog, ok := store.(matchstate.CatalogRepository)
		if !ok {
			writeJSON(w, http.StatusOK, map[string]any{"matches": []matchstate.MatchSummary{}})
			return
		}
		matches, err := catalog.PublicMatchCatalog()
		if err != nil {
			http.Error(w, "比赛列表暂时不可用", http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"matches": matches})
	}
}

func validateNewMatchConfig(config matchstate.MatchConfig) error {
	if strings.TrimSpace(config.HomeTeam) == "" || strings.TrimSpace(config.AwayTeam) == "" {
		return errors.New("新比赛必须填写主队和客队名称")
	}
	homeStarters := countStartingPlayers(config.HomePlayers)
	awayStarters := countStartingPlayers(config.AwayPlayers)
	if homeStarters < 11 || awayStarters < 11 {
		return fmt.Errorf("新比赛双方至少需要 11 名首发，当前主队 %d 名、客队 %d 名", homeStarters, awayStarters)
	}
	return nil
}

func countStartingPlayers(players []matchstate.Player) int {
	count := 0
	for _, player := range players {
		if strings.TrimSpace(player.Name) == "" || strings.EqualFold(strings.TrimSpace(player.Lineup), "bench") {
			continue
		}
		count++
	}
	return count
}

func mergeMatchConfigRoster(existing, incoming matchstate.MatchConfig) (matchstate.MatchConfig, error) {
	for _, side := range []struct {
		name     string
		existing []matchstate.Player
		incoming *[]matchstate.Player
	}{
		{name: "主队", existing: existing.HomePlayers, incoming: &incoming.HomePlayers},
		{name: "客队", existing: existing.AwayPlayers, incoming: &incoming.AwayPlayers},
	} {
		if len(*side.incoming) == 0 {
			*side.incoming = side.existing
			continue
		}
		if countStartingPlayers(side.existing) >= 11 && countStartingPlayers(*side.incoming) < 11 {
			return matchstate.MatchConfig{}, fmt.Errorf("%s已有完整首发名单，不能保存为不完整阵容；请保留至少 11 名首发", side.name)
		}
	}
	return incoming, nil
}

func projectInteractionTraces(ctx context.Context, ledger interaction.Ledger, matchID string) ([]companion.Trace, error) {
	snapshotLedger, ok := ledger.(interaction.MatchSnapshotLedger)
	if !ok {
		return nil, errors.New("interaction ledger does not support match snapshots")
	}
	events, err := snapshotLedger.ListMatchSnapshot(ctx, matchID)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]*companion.Trace)
	for _, event := range events {
		if event.Kind != interaction.KindTurnPlanned || len(event.TracePayload) == 0 {
			continue
		}
		var trace companion.Trace
		if err := json.Unmarshal(event.TracePayload, &trace); err != nil {
			return nil, fmt.Errorf("decode interaction trace %q: %w", event.TraceID, err)
		}
		if trace.ID == "" {
			trace.ID = event.TraceID
		}
		if trace.MatchID == "" {
			trace.MatchID = event.MatchID
		}
		if trace.UserID == "" {
			trace.UserID = event.UserID
		}
		if trace.CreatedAt.IsZero() {
			trace.CreatedAt = event.CreatedAt
		}
		if trace.ID != event.TraceID || trace.MatchID != event.MatchID || trace.UserID != event.UserID {
			return nil, fmt.Errorf("interaction trace %q correlation mismatch", event.TraceID)
		}
		byID[trace.ID] = &trace
	}
	for _, event := range events {
		trace := byID[event.TraceID]
		if trace == nil {
			continue
		}
		switch event.Kind {
		case interaction.KindMediaDelivery:
			trace.Voice = ensureVoiceMeta(trace.Voice)
			if event.MediaType != "" {
				trace.Voice.TTSMime = event.MediaType
			}
			switch event.DeliveryState {
			case "audio_ready", "completed":
				trace.Voice.TTSStatus = "ok"
			case "failed":
				trace.Voice.TTSStatus = "failed"
				trace.Voice.TTSError = event.DeliveryReason
			case "skipped":
				trace.Voice.TTSStatus = "skipped"
			}
		case interaction.KindPlaybackResult:
			trace.Voice = ensureVoiceMeta(trace.Voice)
			trace.Voice.PlaybackStatus = event.PlaybackState
		case interaction.KindDelivery:
			if event.Source == "playback" && event.DeliveryState != "" {
				trace.Voice = ensureVoiceMeta(trace.Voice)
				trace.Voice.PlaybackStatus = event.DeliveryState
			}
		}
	}
	traces := make([]companion.Trace, 0, len(byID))
	for _, trace := range byID {
		traces = append(traces, *trace)
	}
	sort.SliceStable(traces, func(left, right int) bool {
		if traces[left].CreatedAt.Equal(traces[right].CreatedAt) {
			return traces[left].ID > traces[right].ID
		}
		return traces[left].CreatedAt.After(traces[right].CreatedAt)
	})
	return traces, nil
}

func listInteractionTraces(ctx context.Context, ledger interaction.Ledger, matchID string, limit int) ([]companion.Trace, error) {
	traces, err := projectInteractionTraces(ctx, ledger, matchID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if len(traces) > limit {
		traces = traces[:limit]
	}
	return traces, nil
}

func getInteractionTrace(ctx context.Context, ledger interaction.Ledger, matchID, traceID string) (companion.Trace, error) {
	traces, err := projectInteractionTraces(ctx, ledger, matchID)
	if err != nil {
		return companion.Trace{}, err
	}
	for _, trace := range traces {
		if trace.ID == traceID {
			return trace, nil
		}
	}
	return companion.Trace{}, companion.ErrTraceNotFound
}

// matchAPI 聚拢 /api/matches/ 运营面的全部依赖；原 25-case switch 的内联
// case 体原样搬进具名方法（matchID/资源 ID 从 r.PathValue 取），路由由
// matchRoutes 注册表声明，鉴权走 withScope。
type matchAPI struct {
	store             matchstate.Repository
	traceReader       companion.TraceReader
	demoResetter      companion.DemoResetter
	sources           *datasource.Manager
	directorDrafts    *directordraft.Service
	interactionLedger interaction.Ledger
	authz             operatorAuthz
	operatorWrites    *operatorwrite.Service
}

func handleMatchAPIWithOperatorAuth(store matchstate.Repository, traceReader companion.TraceReader, demoResetter companion.DemoResetter, cfg *config.Config, llmClient *llm.Client, sources *datasource.Manager, directorDrafts *directordraft.Service, interactionLedger interaction.Ledger, authz operatorAuthz, writeServices ...*operatorwrite.Service) http.HandlerFunc {
	return mountRoutes(cfg, authz, matchRoutes(matchAPI{
		store:             store,
		traceReader:       traceReader,
		demoResetter:      demoResetter,
		sources:           sources,
		directorDrafts:    directorDrafts,
		interactionLedger: interactionLedger,
		authz:             authz,
		operatorWrites:    selectedOperatorWriteService(writeServices),
	}))
}

// matchRoutes 是 /api/matches/ 运营面的声明式路由表：一行一个端点（method
// + pattern + scope + handler）。scope 为空的四个 GET（clock/config/events/
// state）是公开可降级读端点：匿名拿公开视图、持 TraceRead 的运营拿运营视
// 图，判定在方法内用 authz.view 完成——与迁移前一致，故不包 withScope。
// 行为差异（已知路径错方法 → 405 等）统一记录在 router.go。
func matchRoutes(m matchAPI) []route {
	return []route{
		{method: http.MethodGet, pattern: "/api/matches/{matchId}/interaction", scope: auth.ScopeOperatorTraceRead, handler: m.handleInteraction},
		{method: http.MethodPost, pattern: "/api/matches/{matchId}/start", scope: auth.ScopeOperatorMatchWrite, handler: m.handleStart},
		{method: http.MethodPost, pattern: "/api/matches/{matchId}/reset", scope: auth.ScopeOperatorMatchWrite, handler: m.handleReset},
		{method: http.MethodGet, pattern: "/api/matches/{matchId}/sources", scope: auth.ScopeOperatorTraceRead, handler: m.handleSourcesStatus},
		{method: http.MethodPost, pattern: "/api/matches/{matchId}/sources/start", scope: auth.ScopeOperatorMatchWrite, handler: m.handleSourcesStart},
		{method: http.MethodPost, pattern: "/api/matches/{matchId}/sources/stop", scope: auth.ScopeOperatorMatchWrite, handler: m.handleSourcesStop},
		// 一键托管(auto-hosting 2.6):ESPN summary 填充配置 + 开场 + 挂源。
		{method: http.MethodPost, pattern: "/api/matches/{matchId}/host", scope: auth.ScopeOperatorMatchWrite, handler: m.handleHost},
		{method: http.MethodPost, pattern: "/api/matches/{matchId}/takeover", scope: auth.ScopeOperatorMatchWrite, handler: m.handleTakeover},
		{method: http.MethodGet, pattern: "/api/matches/{matchId}/automation", scope: auth.ScopeOperatorTraceRead, handler: m.handleGetAutomation},
		{method: http.MethodPost, pattern: "/api/matches/{matchId}/automation", scope: auth.ScopeOperatorMatchWrite, handler: m.handleSetAutomation},
		{method: http.MethodGet, pattern: "/api/matches/{matchId}/clock", handler: m.handleGetClock},
		{method: http.MethodPatch, pattern: "/api/matches/{matchId}/clock", scope: auth.ScopeOperatorMatchWrite, handler: m.handlePatchClock},
		{method: http.MethodPost, pattern: "/api/matches/{matchId}/drafts/voice/publish", scope: auth.ScopeOperatorMatchWrite, handler: m.handleVoiceDraftPublish},
		{method: http.MethodPost, pattern: "/api/matches/{matchId}/drafts/voice", scope: auth.ScopeOperatorMatchWrite, handler: m.handleVoiceDraft},
		{method: http.MethodPost, pattern: "/api/matches/{matchId}/facts/{factId}/confirm", scope: auth.ScopeOperatorFactConfirm, handler: m.handleFactConfirm},
		{method: http.MethodPost, pattern: "/api/matches/{matchId}/facts/{factId}/revoke", scope: auth.ScopeOperatorFactConfirm, handler: m.handleFactRevoke},
		{method: http.MethodPost, pattern: "/api/matches/{matchId}/facts/{factId}/reconcile", scope: auth.ScopeOperatorFactConfirm, handler: m.handleFactReconcile},
		{method: http.MethodPost, pattern: "/api/matches/{matchId}/conflicts/{conflictId}/resolve", scope: auth.ScopeOperatorFactConfirm, handler: m.handleConflictResolve},
		{method: http.MethodGet, pattern: "/api/matches/{matchId}/facts/{factId}/revisions", scope: auth.ScopeOperatorTraceRead, handler: m.handleFactRevisions},
		{method: http.MethodGet, pattern: "/api/matches/{matchId}/config", handler: m.handleGetConfig},
		{method: http.MethodPost, pattern: "/api/matches/{matchId}/config", scope: auth.ScopeOperatorMatchWrite, handler: m.handleSetConfig},
		{method: http.MethodPost, pattern: "/api/matches/{matchId}/lifecycle", scope: auth.ScopeOperatorMatchWrite, handler: m.handleSetLifecycle},
		{method: http.MethodGet, pattern: "/api/matches/{matchId}/events", handler: m.handleGetEvents},
		{method: http.MethodPost, pattern: "/api/matches/{matchId}/events", scope: auth.ScopeOperatorMatchWrite, handler: m.handleCreateEvent},
		{method: http.MethodGet, pattern: "/api/matches/{matchId}/state", handler: m.handleGetState},
		{method: http.MethodGet, pattern: "/api/matches/{matchId}/traces", scope: auth.ScopeOperatorTraceRead, handler: m.handleListTraces},
		{method: http.MethodGet, pattern: "/api/matches/{matchId}/traces/{traceId}", scope: auth.ScopeOperatorTraceRead, handler: m.handleGetTrace},
		{method: http.MethodPost, pattern: "/api/matches/{matchId}/events/{eventId}/correct", scope: auth.ScopeOperatorFactCorrect, handler: m.handleCorrectEvent},
	}
}

// handleInteraction 列出/翻页某比赛某用户的交互账本行（附 journey 投影与
// 审计报告）。
func (m matchAPI) handleInteraction(w http.ResponseWriter, r *http.Request) {
	matchID := r.PathValue("matchId")
	if m.interactionLedger == nil {
		writeJSON(w, http.StatusOK, map[string]any{"events": []interaction.Event{}, "nextCursor": "", "hasMore": false, "projectionScope": "all", "journey": interaction.Journey{}, "audit": evals.InteractionAuditReport{}})
		return
	}
	limit := 100
	if value := r.URL.Query().Get("limit"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	userID := strings.TrimSpace(r.URL.Query().Get("userId"))
	if userID == "" {
		http.Error(w, "userId is required", http.StatusBadRequest)
		return
	}
	page := interaction.Page{}
	var err error
	if pageable, ok := m.interactionLedger.(interaction.PageableLedger); ok {
		page, err = pageable.ListPage(r.Context(), interaction.PageQuery{
			UserID: userID, MatchID: matchID, Limit: limit, Cursor: strings.TrimSpace(r.URL.Query().Get("cursor")),
		})
	} else {
		page.Events, err = m.interactionLedger.List(r.Context(), userID, matchID, limit)
	}
	if err != nil {
		if errors.Is(err, interaction.ErrInvalidCursor) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	projectionEvents := page.Events
	projectionScope := "page"
	if snapshot, ok := m.interactionLedger.(interaction.SnapshotLedger); ok {
		projectionEvents, err = snapshot.ListSnapshot(r.Context(), userID, matchID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		projectionScope = "all"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"events": page.Events, "nextCursor": page.NextCursor, "hasMore": page.HasMore(),
		"projectionScope": projectionScope,
		"journey":         interaction.ProjectJourney(projectionEvents), "audit": evals.AuditInteractions(projectionEvents),
	})
}
// handleStart 布阵开赛：重置比赛状态后写入新配置（幂等写）。
func (m matchAPI) handleStart(w http.ResponseWriter, r *http.Request) {
	matchID := r.PathValue("matchId")
	var matchConfig matchstate.MatchConfig
	body, err := decodeOperatorJSON(w, r, &matchConfig)
	if err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if err := validateNewMatchConfig(matchConfig); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(matchConfig.Lifecycle) == "" {
		matchConfig.Lifecycle = matchstate.LifecycleScheduled
	}
	executeOperatorWrite(w, r, m.operatorWrites, matchID, "match.start", body, func(_ context.Context) (operatorwrite.Response, error) {
		if m.sources != nil {
			m.sources.Stop(matchID)
		}
		if err := m.store.Reset(matchID); err != nil {
			return operatorwrite.Response{}, operatorError(http.StatusBadRequest, err)
		}
		if m.demoResetter != nil {
			if err := m.demoResetter.Reset(matchID); err != nil {
				return operatorwrite.Response{}, operatorError(http.StatusBadRequest, err)
			}
		}
		savedConfig, snapshot, err := m.store.SetConfig(matchID, matchConfig)
		if err != nil {
			return operatorwrite.Response{}, operatorError(http.StatusBadRequest, err)
		}
		return operatorwrite.JSONResponse(http.StatusOK, map[string]interface{}{
			"ok":       true,
			"matchId":  matchID,
			"config":   savedConfig,
			"snapshot": snapshot,
		})
	})
}

// handleReset 只对本地演示比赛开放的一键重置（幂等写）。
func (m matchAPI) handleReset(w http.ResponseWriter, r *http.Request) {
	matchID := r.PathValue("matchId")
	if !isDemoMatchID(matchID) {
		http.Error(w, "reset is only available for local demo match ids", http.StatusBadRequest)
		return
	}
	executeOperatorWrite(w, r, m.operatorWrites, matchID, "match.reset", []byte("{}"), func(_ context.Context) (operatorwrite.Response, error) {
		if m.sources != nil {
			m.sources.Stop(matchID)
		}
		if err := m.store.Reset(matchID); err != nil {
			return operatorwrite.Response{}, operatorError(http.StatusBadRequest, err)
		}
		if m.demoResetter != nil {
			if err := m.demoResetter.Reset(matchID); err != nil {
				return operatorwrite.Response{}, operatorError(http.StatusBadRequest, err)
			}
		}
		return operatorwrite.JSONResponse(http.StatusOK, map[string]interface{}{
			"ok":       true,
			"matchId":  matchID,
			"snapshot": m.store.PublicSnapshot(matchID),
		})
	})
}
// handleSourcesStatus 报告当前活跃信号源。
func (m matchAPI) handleSourcesStatus(w http.ResponseWriter, r *http.Request) {
	matchID := r.PathValue("matchId")
	if m.sources == nil {
		http.Error(w, "source manager unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"status": m.sources.Status(matchID)})
}

// handleSourcesStart 切换并启动一个信号源（幂等写）。
func (m matchAPI) handleSourcesStart(w http.ResponseWriter, r *http.Request) {
	matchID := r.PathValue("matchId")
	if m.sources == nil {
		http.Error(w, "source manager unavailable", http.StatusServiceUnavailable)
		return
	}
	var sourceConfig datasource.SourceConfig
	body, err := decodeOperatorJSON(w, r, &sourceConfig)
	if err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	executeOperatorWrite(w, r, m.operatorWrites, matchID, "sources.start", body, func(_ context.Context) (operatorwrite.Response, error) {
		status, err := m.sources.Start(matchID, sourceConfig)
		if err != nil {
			return operatorwrite.Response{}, operatorError(http.StatusBadRequest, err)
		}
		return operatorwrite.JSONResponse(http.StatusOK, map[string]interface{}{"status": status})
	})
}

// handleSourcesStop 停掉信号源，回到 manual（幂等写）。
func (m matchAPI) handleSourcesStop(w http.ResponseWriter, r *http.Request) {
	matchID := r.PathValue("matchId")
	if m.sources == nil {
		http.Error(w, "source manager unavailable", http.StatusServiceUnavailable)
		return
	}
	executeOperatorWrite(w, r, m.operatorWrites, matchID, "sources.stop", []byte("{}"), func(_ context.Context) (operatorwrite.Response, error) {
		return operatorwrite.JSONResponse(http.StatusOK, map[string]interface{}{"status": m.sources.Stop(matchID)})
	})
}

// handleTakeover 人工接管：停源并把自动化切到 paused（幂等写）。
func (m matchAPI) handleTakeover(w http.ResponseWriter, r *http.Request) {
	matchID := r.PathValue("matchId")
	if m.sources == nil {
		http.Error(w, "source manager unavailable", http.StatusServiceUnavailable)
		return
	}
	executeOperatorWrite(w, r, m.operatorWrites, matchID, "match.takeover", []byte("{}"), func(_ context.Context) (operatorwrite.Response, error) {
		status := m.sources.Stop(matchID)
		policy := m.store.Config(matchID).Automation
		policy.Mode = matchstate.AutomationModePaused
		saved, err := m.store.SetAutomation(matchID, policy)
		if err != nil {
			return operatorwrite.Response{}, operatorError(http.StatusBadRequest, err)
		}
		return operatorwrite.JSONResponse(http.StatusOK, map[string]interface{}{
			"policy": saved,
			"status": status,
		})
	})
}

// handleGetAutomation 读取自动化策略。
func (m matchAPI) handleGetAutomation(w http.ResponseWriter, r *http.Request) {
	matchID := r.PathValue("matchId")
	writeJSON(w, http.StatusOK, map[string]interface{}{"policy": m.store.Config(matchID).Automation})
}

// handleSetAutomation 保存自动化策略（幂等写）。
func (m matchAPI) handleSetAutomation(w http.ResponseWriter, r *http.Request) {
	matchID := r.PathValue("matchId")
	var policy matchstate.AutomationPolicy
	body, err := decodeOperatorJSON(w, r, &policy)
	if err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	executeOperatorWrite(w, r, m.operatorWrites, matchID, "automation.set", body, func(_ context.Context) (operatorwrite.Response, error) {
		saved, err := m.store.SetAutomation(matchID, policy)
		if err != nil {
			return operatorwrite.Response{}, operatorError(http.StatusBadRequest, err)
		}
		return operatorwrite.JSONResponse(http.StatusOK, map[string]interface{}{"policy": saved})
	})
}
// handleGetClock 公开可降级读：匿名拿公开视图，持 TraceRead 的运营拿完整
// 视图（判定在方法内用 authz.view，与迁移前一致）。
func (m matchAPI) handleGetClock(w http.ResponseWriter, r *http.Request) {
	matchID := r.PathValue("matchId")
	clockStore, ok := m.store.(matchstate.ClockRepository)
	if !ok {
		http.Error(w, "match clock unavailable", http.StatusNotImplemented)
		return
	}
	snapshot := m.store.PublicSnapshot(matchID)
	_, operatorView := m.authz.view(r, auth.ScopeOperatorTraceRead)
	if !operatorView {
		snapshot = clientSnapshot(snapshot)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"clock":    clockStore.Clock(matchID),
		"snapshot": snapshot,
	})
}

// handlePatchClock 运营改钟（set/adjust/start/pause），带乐观并发版本号
// （幂等写）。
func (m matchAPI) handlePatchClock(w http.ResponseWriter, r *http.Request) {
	matchID := r.PathValue("matchId")
	clockStore, ok := m.store.(matchstate.ClockRepository)
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
	executeOperatorWrite(w, r, m.operatorWrites, matchID, "match.clock", body, func(_ context.Context) (operatorwrite.Response, error) {
		clock, err := clockStore.SetClock(matchID, command)
		if err != nil {
			return operatorwrite.Response{}, matchStateWriteError(err)
		}
		return operatorwrite.JSONResponse(http.StatusOK, map[string]interface{}{
			"clock":    clock,
			"snapshot": m.store.PublicSnapshot(matchID),
		})
	})
}
// handleVoiceDraftPublish 把语音草稿直发为确认事实（45s 上游预算，幂等写）。
func (m matchAPI) handleVoiceDraftPublish(w http.ResponseWriter, r *http.Request) {
	matchID := r.PathValue("matchId")
	operator, ok := operatorClaims(m.authz, w, r)
	if !ok {
		return
	}
	if m.directorDrafts == nil {
		http.Error(w, "director voice draft unavailable", http.StatusServiceUnavailable)
		return
	}
	clockStore, ok := m.store.(matchstate.ClockRepository)
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
	executeOperatorWrite(w, r, m.operatorWrites, matchID, "drafts.voice.publish", body, func(operationCtx context.Context) (operatorwrite.Response, error) {
		draftCtx, cancel := context.WithTimeout(operationCtx, 45*time.Second)
		defer cancel()
		result, err := m.directorDrafts.Build(draftCtx, request, directordraft.MatchContext{
			MatchID: matchID,
			Config:  m.store.Config(matchID),
			Clock:   clockStore.Clock(matchID),
		})
		if err != nil {
			return operatorwrite.Response{}, directorDraftWriteError(err)
		}
		event, err := eventFromVoiceDraft(result, m.store.PublicSnapshot(matchID).Score)
		if err != nil {
			return operatorwrite.Response{}, operatorError(http.StatusUnprocessableEntity, err)
		}
		event.OperatorID = operator.Subject
		applyRequestedFactStatus(&event)
		markProactiveMode(&event)
		var created matchstate.MatchEvent
		var snapshot matchstate.Snapshot
		if m.sources != nil {
			created, snapshot, err = m.sources.Ingest(operationCtx, matchID, event)
		} else if transactionalStore, supported := m.store.(matchstate.OperatorTransactionRepository); supported {
			created, snapshot, err = transactionalStore.CreateOperator(operationCtx, matchID, event)
		} else {
			created, snapshot, err = m.store.Create(matchID, event)
		}
		if err != nil {
			return operatorwrite.Response{}, matchStateWriteError(err)
		}
		if transactionalStore, supported := m.store.(matchstate.OperatorTransactionRepository); supported {
			snapshot, err = transactionalStore.PublicSnapshotOperator(operationCtx, matchID)
		} else {
			snapshot = m.store.PublicSnapshot(matchID)
		}
		if err != nil {
			return operatorwrite.Response{}, err
		}
		return operatorwrite.JSONResponse(http.StatusCreated, map[string]interface{}{"event": created, "snapshot": snapshot})
	})
}

// handleVoiceDraft 把语音转写成草稿（不落事件，幂等写）。
func (m matchAPI) handleVoiceDraft(w http.ResponseWriter, r *http.Request) {
	matchID := r.PathValue("matchId")
	if m.directorDrafts == nil {
		http.Error(w, "director voice draft unavailable", http.StatusServiceUnavailable)
		return
	}
	var request directordraft.Request
	body, err := decodeOperatorJSON(w, r, &request)
	if err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	executeOperatorWrite(w, r, m.operatorWrites, matchID, "drafts.voice", body, func(operationCtx context.Context) (operatorwrite.Response, error) {
		clockStore, ok := m.store.(matchstate.ClockRepository)
		if !ok {
			return operatorwrite.Response{}, operatorError(http.StatusNotImplemented, errors.New("match clock unavailable"))
		}
		result, err := m.directorDrafts.Build(operationCtx, request, directordraft.MatchContext{
			MatchID: matchID,
			Config:  m.store.Config(matchID),
			Clock:   clockStore.Clock(matchID),
		})
		if err != nil {
			return operatorwrite.Response{}, directorDraftWriteError(err)
		}
		return operatorwrite.JSONResponse(http.StatusOK, result)
	})
}
// handleFactConfirm/Revoke/Reconcile 是事实状态迁移的三个端点形态：原
// switch 里同一 case 以 parts[3] 分流（未知动作落 404），现在由注册表的
// 三条字面量 pattern 各自接住，语义等价。
func (m matchAPI) handleFactConfirm(w http.ResponseWriter, r *http.Request) {
	m.handleFactTransition(w, r, "confirm")
}

func (m matchAPI) handleFactRevoke(w http.ResponseWriter, r *http.Request) {
	m.handleFactTransition(w, r, "revoke")
}

func (m matchAPI) handleFactReconcile(w http.ResponseWriter, r *http.Request) {
	m.handleFactTransition(w, r, "reconcile")
}

func (m matchAPI) handleFactTransition(w http.ResponseWriter, r *http.Request, action string) {
	matchID := r.PathValue("matchId")
	factID := r.PathValue("factId")
	operator, ok := operatorClaims(m.authz, w, r)
	if !ok {
		return
	}
	executeOperatorWrite(w, r, m.operatorWrites, matchID, "facts."+action, []byte("{}"), func(operationCtx context.Context) (operatorwrite.Response, error) {
		var changed matchstate.MatchEvent
		var snapshot matchstate.Snapshot
		var err error
		transactionalStore, transactional := m.store.(matchstate.OperatorTransactionRepository)
		switch action {
		case "confirm":
			if transactional {
				changed, snapshot, err = transactionalStore.ConfirmFactOperator(operationCtx, matchID, factID, operator.Subject)
			} else {
				changed, snapshot, err = m.store.ConfirmFact(matchID, factID, operator.Subject)
			}
		case "revoke":
			if transactional {
				changed, snapshot, err = transactionalStore.RevokeFactOperator(operationCtx, matchID, factID, operator.Subject)
			} else {
				changed, snapshot, err = m.store.RevokeFact(matchID, factID, operator.Subject)
			}
		case "reconcile":
			if transactional {
				changed, snapshot, err = transactionalStore.ReconcileFactOperator(operationCtx, matchID, factID, operator.Subject)
			} else {
				changed, snapshot, err = m.store.ReconcileFact(matchID, factID, operator.Subject)
			}
		}
		if err != nil {
			return operatorwrite.Response{}, matchStateWriteError(err)
		}
		return operatorwrite.JSONResponse(http.StatusOK, map[string]interface{}{"event": changed, "snapshot": snapshot})
	})
}

// handleConflictResolve 在一条事实冲突里裁决采纳哪个/哪些事实（幂等写）。
func (m matchAPI) handleConflictResolve(w http.ResponseWriter, r *http.Request) {
	matchID := r.PathValue("matchId")
	conflictID := r.PathValue("conflictId")
	operator, ok := operatorClaims(m.authz, w, r)
	if !ok {
		return
	}
	conflictStore, supported := m.store.(matchstate.FactConflictRepository)
	if !supported {
		http.Error(w, "fact conflict resolution unavailable", http.StatusNotImplemented)
		return
	}
	var request struct {
		ChosenFactID    string   `json:"chosenFactId"`
		SelectedFactIDs []string `json:"selectedFactIds"`
		Reason          string   `json:"reason"`
	}
	body, err := decodeOperatorJSON(w, r, &request)
	if err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	selectedFactIDs := append([]string(nil), request.SelectedFactIDs...)
	if len(selectedFactIDs) == 0 && strings.TrimSpace(request.ChosenFactID) != "" {
		var target matchstate.FactConflict
		for _, conflict := range conflictStore.FactConflicts(matchID) {
			if conflict.ID == conflictID && conflict.Status == matchstate.ConflictStatusOpen {
				target = conflict
				break
			}
		}
		selectedFactIDs, err = matchstate.CompatibleSelectionForLegacyChoice(target, request.ChosenFactID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	preferredFactID := strings.TrimSpace(request.ChosenFactID)
	if preferredFactID == "" && len(selectedFactIDs) > 0 {
		preferredFactID = strings.TrimSpace(selectedFactIDs[0])
	}
	existingByFactID := make(map[string]matchstate.MatchEvent)
	for _, event := range m.store.Events(matchID) {
		if event.Status == "active" {
			existingByFactID[event.FactID] = event
		}
	}
	executeOperatorWrite(w, r, m.operatorWrites, matchID, "conflicts.resolve", body, func(operationCtx context.Context) (operatorwrite.Response, error) {
		var conflict matchstate.FactConflict
		var changedEvents []matchstate.MatchEvent
		var snapshot matchstate.Snapshot
		var err error
		if transactionalStore, transactional := m.store.(matchstate.FactConflictSelectionTransactionRepository); transactional {
			conflict, changedEvents, snapshot, err = transactionalStore.ResolveFactConflictSelectionOperator(
				operationCtx, matchID, conflictID, selectedFactIDs, operator.Subject, request.Reason,
			)
		} else if selectionStore, selectable := m.store.(matchstate.FactConflictSelectionRepository); selectable {
			conflict, changedEvents, snapshot, err = selectionStore.ResolveFactConflictSelection(
				matchID, conflictID, selectedFactIDs, operator.Subject, request.Reason,
			)
		} else if len(selectedFactIDs) == 1 {
			var changed matchstate.MatchEvent
			conflict, changed, snapshot, err = conflictStore.ResolveFactConflict(matchID, conflictID, selectedFactIDs[0], operator.Subject, request.Reason)
			if changed.ID != "" {
				changedEvents = []matchstate.MatchEvent{changed}
			}
		} else {
			err = fmt.Errorf("%w: compatible fact selection is unavailable", matchstate.ErrInvalid)
		}
		if err != nil {
			return operatorwrite.Response{}, matchStateWriteError(err)
		}
		primary := existingByFactID[preferredFactID]
		for _, event := range changedEvents {
			if event.FactID == preferredFactID {
				primary = event
				break
			}
		}
		return operatorwrite.JSONResponse(http.StatusOK, map[string]interface{}{
			"conflict": conflict,
			"event":    primary,
			"events":   changedEvents,
			"snapshot": snapshot,
		})
	})
}

// handleFactRevisions 列出一条事实的修订史。
func (m matchAPI) handleFactRevisions(w http.ResponseWriter, r *http.Request) {
	matchID := r.PathValue("matchId")
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"revisions": m.store.FactRevisions(matchID, r.PathValue("factId")),
	})
}
// handleGetConfig 公开可降级读：匿名视图抹掉 integrity 审计细节。
func (m matchAPI) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	matchID := r.PathValue("matchId")
	matchConfig := m.store.Config(matchID)
	snapshot := m.store.PublicSnapshot(matchID)
	_, operatorView := m.authz.view(r, auth.ScopeOperatorTraceRead)
	if !operatorView {
		matchConfig.Integrity = matchstate.MatchIntegrity{}
		snapshot = clientSnapshot(snapshot)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"config":   matchConfig,
		"snapshot": snapshot,
	})
}

// handleSetConfig 保存比赛配置；空阵容继承已配置名单，缩编被拒（幂等写）。
func (m matchAPI) handleSetConfig(w http.ResponseWriter, r *http.Request) {
	matchID := r.PathValue("matchId")
	var config matchstate.MatchConfig
	body, err := decodeOperatorJSON(w, r, &config)
	if err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	config, err = mergeMatchConfigRoster(m.store.Config(matchID), config)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	executeOperatorWrite(w, r, m.operatorWrites, matchID, "config.set", body, func(_ context.Context) (operatorwrite.Response, error) {
		saved, _, err := m.store.SetConfig(matchID, config)
		if err != nil {
			return operatorwrite.Response{}, operatorError(http.StatusBadRequest, err)
		}
		return operatorwrite.JSONResponse(http.StatusOK, map[string]interface{}{
			"config":   saved,
			"snapshot": m.store.PublicSnapshot(matchID),
		})
	})
}

// handleSetLifecycle 迁移比赛生命周期（幂等写），审计行带运营身份。
func (m matchAPI) handleSetLifecycle(w http.ResponseWriter, r *http.Request) {
	matchID := r.PathValue("matchId")
	operator, ok := operatorClaims(m.authz, w, r)
	if !ok {
		return
	}
	var request struct {
		Lifecycle string `json:"lifecycle"`
	}
	body, err := decodeOperatorJSON(w, r, &request)
	if err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	executeOperatorWrite(w, r, m.operatorWrites, matchID, "lifecycle.set", body, func(_ context.Context) (operatorwrite.Response, error) {
		lifecycleStore, ok := m.store.(matchstate.LifecycleRepository)
		if !ok {
			return operatorwrite.Response{}, operatorError(http.StatusNotImplemented, errors.New("match lifecycle unavailable"))
		}
		saved, err := lifecycleStore.SetLifecycle(matchID, request.Lifecycle)
		if err != nil {
			return operatorwrite.Response{}, matchStateWriteError(err)
		}
		return operatorwrite.JSONResponse(http.StatusOK, map[string]interface{}{
			"config": saved, "snapshot": m.store.PublicSnapshot(matchID), "operatorId": operator.Subject,
		})
	})
}
// handleGetEvents 公开可降级读：匿名拿公开事件流，持 TraceRead 的运营额外
// 拿到审计事件与冲突列表。
func (m matchAPI) handleGetEvents(w http.ResponseWriter, r *http.Request) {
	matchID := r.PathValue("matchId")
	events := m.store.PublicEvents(matchID)
	_, operatorView := m.authz.view(r, auth.ScopeOperatorTraceRead)
	if operatorView {
		events = operatorAuditEvents(m.store.Events(matchID), events)
	}
	if events == nil {
		events = []matchstate.MatchEvent{}
	}
	response := map[string]interface{}{"events": events}
	if operatorView {
		if conflictStore, supported := m.store.(matchstate.FactConflictRepository); supported {
			conflicts := conflictStore.FactConflicts(matchID)
			if conflicts == nil {
				conflicts = []matchstate.FactConflict{}
			}
			response["conflicts"] = conflicts
		}
	}
	writeJSON(w, http.StatusOK, response)
}

// handleGetState 公开可降级读：匿名拿公开快照。
func (m matchAPI) handleGetState(w http.ResponseWriter, r *http.Request) {
	matchID := r.PathValue("matchId")
	snapshot := m.store.PublicSnapshot(matchID)
	_, operatorView := m.authz.view(r, auth.ScopeOperatorTraceRead)
	if !operatorView {
		snapshot = clientSnapshot(snapshot)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"snapshot": snapshot,
	})
}
// handleListTraces 列出某比赛的 companion traces（interaction ledger 优先），
// 支持 limit 与 citation=<prefix> 过滤。
func (m matchAPI) handleListTraces(w http.ResponseWriter, r *http.Request) {
	matchID := r.PathValue("matchId")
	limit := 50
	if value := r.URL.Query().Get("limit"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			limit = parsed
		}
	}
	var traces []companion.Trace
	var err error
	if m.interactionLedger != nil {
		traces, err = listInteractionTraces(r.Context(), m.interactionLedger, matchID, limit)
	} else {
		traces, err = m.traceReader.ListTraces(r.Context(), matchID, limit)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// ADR-0008 citation audit: `citation=<prefix>` keeps only traces
	// whose reason codes cite a proactive_citation with that prefix.
	if prefix := strings.TrimSpace(r.URL.Query().Get("citation")); prefix != "" {
		traces = filterTracesByCitationPrefix(traces, prefix)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"traces": traces,
	})
}

// handleGetTrace 取单条 trace 详情（interaction ledger 优先）。
func (m matchAPI) handleGetTrace(w http.ResponseWriter, r *http.Request) {
	matchID := r.PathValue("matchId")
	traceID := r.PathValue("traceId")
	var trace companion.Trace
	var err error
	if m.interactionLedger != nil {
		trace, err = getInteractionTrace(r.Context(), m.interactionLedger, matchID, traceID)
	} else {
		trace, err = m.traceReader.GetTrace(r.Context(), matchID, traceID)
	}
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
}

// handleCreateEvent 发布一条比赛事件（sources.Ingest 或事务写，幂等写）。
func (m matchAPI) handleCreateEvent(w http.ResponseWriter, r *http.Request) {
	matchID := r.PathValue("matchId")
	operator, ok := operatorClaims(m.authz, w, r)
	if !ok {
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
	executeOperatorWrite(w, r, m.operatorWrites, matchID, "events.create", body, func(operationCtx context.Context) (operatorwrite.Response, error) {
		var created matchstate.MatchEvent
		var snapshot matchstate.Snapshot
		var err error
		if m.sources != nil {
			created, snapshot, err = m.sources.Ingest(operationCtx, matchID, ev)
		} else {
			if transactionalStore, ok := m.store.(matchstate.OperatorTransactionRepository); ok {
				created, snapshot, err = transactionalStore.CreateOperator(operationCtx, matchID, ev)
			} else {
				created, snapshot, err = m.store.Create(matchID, ev)
			}
		}
		if err != nil {
			return operatorwrite.Response{}, matchStateWriteError(err)
		}
		if transactionalStore, ok := m.store.(matchstate.OperatorTransactionRepository); ok {
			snapshot, err = transactionalStore.PublicSnapshotOperator(operationCtx, matchID)
		} else {
			snapshot = m.store.PublicSnapshot(matchID)
		}
		if err != nil {
			return operatorwrite.Response{}, err
		}
		return operatorwrite.JSONResponse(http.StatusCreated, map[string]interface{}{
			"event":    created,
			"snapshot": snapshot,
		})
	})
}

// handleCorrectEvent 以 VAR 更正语义替换一条既有事件（须带 correctionReason，
// 幂等写）。
func (m matchAPI) handleCorrectEvent(w http.ResponseWriter, r *http.Request) {
	matchID := r.PathValue("matchId")
	eventID := r.PathValue("eventId")
	operator, ok := operatorClaims(m.authz, w, r)
	if !ok {
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
	executeOperatorWrite(w, r, m.operatorWrites, matchID, "events.correct", body, func(operationCtx context.Context) (operatorwrite.Response, error) {
		var corrected matchstate.MatchEvent
		var snapshot matchstate.Snapshot
		var err error
		if transactionalStore, ok := m.store.(matchstate.OperatorTransactionRepository); ok {
			corrected, snapshot, err = transactionalStore.CorrectOperator(operationCtx, matchID, eventID, ev)
		} else {
			corrected, snapshot, err = m.store.Correct(matchID, eventID, ev)
		}
		if err != nil {
			return operatorwrite.Response{}, matchStateWriteError(err)
		}
		if transactionalStore, ok := m.store.(matchstate.OperatorTransactionRepository); ok {
			snapshot, err = transactionalStore.PublicSnapshotOperator(operationCtx, matchID)
		} else {
			snapshot = m.store.PublicSnapshot(matchID)
		}
		if err != nil {
			return operatorwrite.Response{}, err
		}
		return operatorwrite.JSONResponse(http.StatusOK, map[string]interface{}{
			"event":    corrected,
			"snapshot": snapshot,
		})
	})
}

// 语音 wrapper 链塌缩（server-residual-polish 1.3）：四层里两层纯转发 shim
// 已删，只留无信号版入口与 ...WithSignalIDOptions 实装（转写版同理）。
func handleVoiceSession(ctx context.Context, agent *companion.Agent, recognizer speechRecognizer, synthesizer speechSynthesizer, matchID, userID, text, audioB64 string, now time.Time) (voiceSessionResult, error) {
	return handleVoiceSessionWithSignalIDOptions(ctx, agent, recognizer, synthesizer, matchID, userID, text, audioB64, now, "", voiceSessionOptions{})
}

func handleVoiceSessionWithSignalIDOptions(ctx context.Context, agent *companion.Agent, recognizer speechRecognizer, synthesizer speechSynthesizer, matchID, userID, text, audioB64 string, now time.Time, signalID string, options voiceSessionOptions) (voiceSessionResult, error) {
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
	return completeVoiceSessionWithOptions(ctx, agent, synthesizer, matchID, userID, now, signalID, result, voiceMeta, options)
}

// espnHostRequest 是一键托管(auto-hosting 2.6)请求体:ESPN 联赛 slug +
// event ID;可选覆写阵容外的配置字段。
type espnHostRequest struct {
	League      string `json:"league"`
	EspnEventID string `json:"espnEventId"`
}

// handleHost 一键托管此场:拉 ESPN summary 填充比赛配置(队伍/赛事/开球时间),
// Reset+SetConfig 开场,挂 ESPN 快照源。编排单点:一次幂等写完成建场+开场+
// 挂源;api-sports 二源可由运营随后 sources/start 另挂(key 配置时)。
func (m matchAPI) handleHost(w http.ResponseWriter, r *http.Request) {
	matchID := r.PathValue("matchId")
	if m.sources == nil {
		http.Error(w, "source manager unavailable", http.StatusServiceUnavailable)
		return
	}
	if !m.sources.EspnConfigured() {
		http.Error(w, "espn is not configured", http.StatusServiceUnavailable)
		return
	}
	var request espnHostRequest
	body, err := decodeOperatorJSON(w, r, &request)
	if err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	request.League = strings.TrimSpace(request.League)
	request.EspnEventID = strings.TrimSpace(request.EspnEventID)
	if request.League == "" || request.EspnEventID == "" {
		http.Error(w, "league and espnEventId are required", http.StatusBadRequest)
		return
	}
	summary, err := m.sources.EspnMatchPreview(request.League, request.EspnEventID)
	if err != nil {
		http.Error(w, fmt.Sprintf("espn preview failed: %v", err), http.StatusBadGateway)
		return
	}
	if len(summary.Header.Competitions) == 0 || len(summary.Header.Competitions[0].Competitors) < 2 {
		http.Error(w, "espn summary has no competitors", http.StatusBadGateway)
		return
	}
	competition := summary.Header.Competitions[0]
	var homeTeam, awayTeam string
	for _, competitor := range competition.Competitors {
		switch competitor.HomeAway {
		case "home":
			homeTeam = competitor.Team.DisplayName
		case "away":
			awayTeam = competitor.Team.DisplayName
		}
	}
	if homeTeam == "" || awayTeam == "" {
		http.Error(w, "espn summary has no home/away teams", http.StatusBadGateway)
		return
	}
	matchConfig := matchstate.MatchConfig{
		MatchID:     matchID,
		HomeTeam:    homeTeam,
		AwayTeam:    awayTeam,
		Competition: summary.Header.League.Name,
		Kickoff:     competition.Date,
	}
	executeOperatorWrite(w, r, m.operatorWrites, matchID, "match.host", body, func(_ context.Context) (operatorwrite.Response, error) {
		if err := m.store.Reset(matchID); err != nil {
			return operatorwrite.Response{}, operatorError(http.StatusBadRequest, err)
		}
		if _, _, err := m.store.SetConfig(matchID, matchConfig); err != nil {
			return operatorwrite.Response{}, operatorError(http.StatusBadRequest, err)
		}
		status, err := m.sources.Start(matchID, datasource.SourceConfig{
			Type:        datasource.SourceESPN,
			League:      request.League,
			EspnEventID: request.EspnEventID,
		})
		if err != nil {
			return operatorwrite.Response{}, operatorError(http.StatusBadRequest, err)
		}
		return operatorwrite.JSONResponse(http.StatusOK, map[string]interface{}{
			"ok":      true,
			"matchId": matchID,
			"config":  matchConfig,
			"status":  status,
		})
	})
}

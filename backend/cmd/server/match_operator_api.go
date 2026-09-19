package main

// 比赛运营 API（openspec/changes/server-surface-split）：25-case 路由、
// 五层构造器洋葱与私有辅助。请求形状是 ADR-0011 冻结基线（28 条
// operator-control evals 回归网），本文件为纯搬移零行为变化。

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

func handleMatchAPIWithOperatorAuth(store matchstate.Repository, traceReader companion.TraceReader, demoResetter companion.DemoResetter, cfg *config.Config, llmClient *llm.Client, sources *datasource.Manager, directorDrafts *directordraft.Service, interactionLedger interaction.Ledger, authz operatorAuthz, writeServices ...*operatorwrite.Service) http.HandlerFunc {
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
		case r.Method == http.MethodGet && resource == "interaction" && len(parts) == 2:
			if _, ok := authz.authorize(w, r, auth.ScopeOperatorTraceRead); !ok {
				return
			}
			if interactionLedger == nil {
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
			if pageable, ok := interactionLedger.(interaction.PageableLedger); ok {
				page, err = pageable.ListPage(r.Context(), interaction.PageQuery{
					UserID: userID, MatchID: matchID, Limit: limit, Cursor: strings.TrimSpace(r.URL.Query().Get("cursor")),
				})
			} else {
				page.Events, err = interactionLedger.List(r.Context(), userID, matchID, limit)
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
			if snapshot, ok := interactionLedger.(interaction.SnapshotLedger); ok {
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
		case r.Method == http.MethodPost && resource == "start" && len(parts) == 2:
			if _, ok := authz.authorize(w, r, auth.ScopeOperatorMatchWrite); !ok {
				return
			}
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
			executeOperatorWrite(w, r, operatorWrites, matchID, "match.start", body, func(_ context.Context) (operatorwrite.Response, error) {
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
				savedConfig, snapshot, err := store.SetConfig(matchID, matchConfig)
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
		case r.Method == http.MethodPost && resource == "reset" && len(parts) == 2:
			if _, ok := authz.authorize(w, r, auth.ScopeOperatorMatchWrite); !ok {
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
			if _, ok := authz.authorize(w, r, auth.ScopeOperatorTraceRead); !ok {
				return
			}
			if sources == nil {
				http.Error(w, "source manager unavailable", http.StatusServiceUnavailable)
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"status": sources.Status(matchID)})
		case r.Method == http.MethodPost && resource == "sources" && len(parts) == 3 && parts[2] == "start":
			if _, ok := authz.authorize(w, r, auth.ScopeOperatorMatchWrite); !ok {
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
			if _, ok := authz.authorize(w, r, auth.ScopeOperatorMatchWrite); !ok {
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
			if _, ok := authz.authorize(w, r, auth.ScopeOperatorMatchWrite); !ok {
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
			if _, ok := authz.authorize(w, r, auth.ScopeOperatorTraceRead); !ok {
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"policy": store.Config(matchID).Automation})
		case r.Method == http.MethodPost && resource == "automation" && len(parts) == 2:
			if _, ok := authz.authorize(w, r, auth.ScopeOperatorMatchWrite); !ok {
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
			snapshot := store.PublicSnapshot(matchID)
			_, operatorView := authz.view(r, auth.ScopeOperatorTraceRead)
			if !operatorView {
				snapshot = clientSnapshot(snapshot)
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"clock":    clockStore.Clock(matchID),
				"snapshot": snapshot,
			})
		case r.Method == http.MethodPatch && resource == "clock" && len(parts) == 2:
			if _, authorized := authz.authorize(w, r, auth.ScopeOperatorMatchWrite); !authorized {
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
					return operatorwrite.Response{}, matchStateWriteError(err)
				}
				return operatorwrite.JSONResponse(http.StatusOK, map[string]interface{}{
					"clock":    clock,
					"snapshot": store.PublicSnapshot(matchID),
				})
			})
		case r.Method == http.MethodPost && resource == "drafts" && len(parts) == 4 && parts[2] == "voice" && parts[3] == "publish":
			operator, authorized := authz.authorize(w, r, auth.ScopeOperatorMatchWrite)
			if !authorized {
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
			executeOperatorWrite(w, r, operatorWrites, matchID, "drafts.voice.publish", body, func(operationCtx context.Context) (operatorwrite.Response, error) {
				draftCtx, cancel := context.WithTimeout(operationCtx, 45*time.Second)
				defer cancel()
				result, err := directorDrafts.Build(draftCtx, request, directordraft.MatchContext{
					MatchID: matchID,
					Config:  store.Config(matchID),
					Clock:   clockStore.Clock(matchID),
				})
				if err != nil {
					return operatorwrite.Response{}, directorDraftWriteError(err)
				}
				event, err := eventFromVoiceDraft(result, store.PublicSnapshot(matchID).Score)
				if err != nil {
					return operatorwrite.Response{}, operatorError(http.StatusUnprocessableEntity, err)
				}
				event.OperatorID = operator.Subject
				applyRequestedFactStatus(&event)
				markProactiveMode(&event)
				var created matchstate.MatchEvent
				var snapshot matchstate.Snapshot
				if sources != nil {
					created, snapshot, err = sources.Ingest(operationCtx, matchID, event)
				} else if transactionalStore, supported := store.(matchstate.OperatorTransactionRepository); supported {
					created, snapshot, err = transactionalStore.CreateOperator(operationCtx, matchID, event)
				} else {
					created, snapshot, err = store.Create(matchID, event)
				}
				if err != nil {
					return operatorwrite.Response{}, matchStateWriteError(err)
				}
				if transactionalStore, supported := store.(matchstate.OperatorTransactionRepository); supported {
					snapshot, err = transactionalStore.PublicSnapshotOperator(operationCtx, matchID)
				} else {
					snapshot = store.PublicSnapshot(matchID)
				}
				if err != nil {
					return operatorwrite.Response{}, err
				}
				return operatorwrite.JSONResponse(http.StatusCreated, map[string]interface{}{"event": created, "snapshot": snapshot})
			})
		case r.Method == http.MethodPost && resource == "drafts" && len(parts) == 3 && parts[2] == "voice":
			if _, authorized := authz.authorize(w, r, auth.ScopeOperatorMatchWrite); !authorized {
				return
			}
			if directorDrafts == nil {
				http.Error(w, "director voice draft unavailable", http.StatusServiceUnavailable)
				return
			}
			var request directordraft.Request
			body, err := decodeOperatorJSON(w, r, &request)
			if err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			executeOperatorWrite(w, r, operatorWrites, matchID, "drafts.voice", body, func(operationCtx context.Context) (operatorwrite.Response, error) {
				clockStore, ok := store.(matchstate.ClockRepository)
				if !ok {
					return operatorwrite.Response{}, operatorError(http.StatusNotImplemented, errors.New("match clock unavailable"))
				}
				result, err := directorDrafts.Build(operationCtx, request, directordraft.MatchContext{
					MatchID: matchID,
					Config:  store.Config(matchID),
					Clock:   clockStore.Clock(matchID),
				})
				if err != nil {
					return operatorwrite.Response{}, directorDraftWriteError(err)
				}
				return operatorwrite.JSONResponse(http.StatusOK, result)
			})
		case r.Method == http.MethodPost && resource == "facts" && len(parts) == 4:
			operator, authorized := authz.authorize(w, r, auth.ScopeOperatorFactConfirm)
			if !authorized {
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
					return operatorwrite.Response{}, matchStateWriteError(err)
				}
				return operatorwrite.JSONResponse(http.StatusOK, map[string]interface{}{"event": changed, "snapshot": snapshot})
			})
		case r.Method == http.MethodPost && resource == "conflicts" && len(parts) == 4 && parts[3] == "resolve":
			operator, authorized := authz.authorize(w, r, auth.ScopeOperatorFactConfirm)
			if !authorized {
				return
			}
			conflictStore, supported := store.(matchstate.FactConflictRepository)
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
					if conflict.ID == parts[2] && conflict.Status == matchstate.ConflictStatusOpen {
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
			for _, event := range store.Events(matchID) {
				if event.Status == "active" {
					existingByFactID[event.FactID] = event
				}
			}
			executeOperatorWrite(w, r, operatorWrites, matchID, "conflicts.resolve", body, func(operationCtx context.Context) (operatorwrite.Response, error) {
				var conflict matchstate.FactConflict
				var changedEvents []matchstate.MatchEvent
				var snapshot matchstate.Snapshot
				var err error
				if transactionalStore, transactional := store.(matchstate.FactConflictSelectionTransactionRepository); transactional {
					conflict, changedEvents, snapshot, err = transactionalStore.ResolveFactConflictSelectionOperator(
						operationCtx, matchID, parts[2], selectedFactIDs, operator.Subject, request.Reason,
					)
				} else if selectionStore, selectable := store.(matchstate.FactConflictSelectionRepository); selectable {
					conflict, changedEvents, snapshot, err = selectionStore.ResolveFactConflictSelection(
						matchID, parts[2], selectedFactIDs, operator.Subject, request.Reason,
					)
				} else if len(selectedFactIDs) == 1 {
					var changed matchstate.MatchEvent
					conflict, changed, snapshot, err = conflictStore.ResolveFactConflict(matchID, parts[2], selectedFactIDs[0], operator.Subject, request.Reason)
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
		case r.Method == http.MethodGet && resource == "facts" && len(parts) == 4 && parts[3] == "revisions":
			if _, authorized := authz.authorize(w, r, auth.ScopeOperatorTraceRead); !authorized {
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"revisions": store.FactRevisions(matchID, parts[2]),
			})
		case r.Method == http.MethodGet && resource == "config" && len(parts) == 2:
			matchConfig := store.Config(matchID)
			snapshot := store.PublicSnapshot(matchID)
			_, operatorView := authz.view(r, auth.ScopeOperatorTraceRead)
			if !operatorView {
				matchConfig.Integrity = matchstate.MatchIntegrity{}
				snapshot = clientSnapshot(snapshot)
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"config":   matchConfig,
				"snapshot": snapshot,
			})
		case r.Method == http.MethodPost && resource == "config" && len(parts) == 2:
			if _, ok := authz.authorize(w, r, auth.ScopeOperatorMatchWrite); !ok {
				return
			}
			var config matchstate.MatchConfig
			body, err := decodeOperatorJSON(w, r, &config)
			if err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			config, err = mergeMatchConfigRoster(store.Config(matchID), config)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
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
		case r.Method == http.MethodPost && resource == "lifecycle" && len(parts) == 2:
			operator, authorized := authz.authorize(w, r, auth.ScopeOperatorMatchWrite)
			if !authorized {
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
			executeOperatorWrite(w, r, operatorWrites, matchID, "lifecycle.set", body, func(_ context.Context) (operatorwrite.Response, error) {
				lifecycleStore, ok := store.(matchstate.LifecycleRepository)
				if !ok {
					return operatorwrite.Response{}, operatorError(http.StatusNotImplemented, errors.New("match lifecycle unavailable"))
				}
				saved, err := lifecycleStore.SetLifecycle(matchID, request.Lifecycle)
				if err != nil {
					return operatorwrite.Response{}, matchStateWriteError(err)
				}
				return operatorwrite.JSONResponse(http.StatusOK, map[string]interface{}{
					"config": saved, "snapshot": store.PublicSnapshot(matchID), "operatorId": operator.Subject,
				})
			})
		case r.Method == http.MethodGet && resource == "events" && len(parts) == 2:
			events := store.PublicEvents(matchID)
			_, operatorView := authz.view(r, auth.ScopeOperatorTraceRead)
			if operatorView {
				events = operatorAuditEvents(store.Events(matchID), events)
			}
			if events == nil {
				events = []matchstate.MatchEvent{}
			}
			response := map[string]interface{}{"events": events}
			if operatorView {
				if conflictStore, supported := store.(matchstate.FactConflictRepository); supported {
					conflicts := conflictStore.FactConflicts(matchID)
					if conflicts == nil {
						conflicts = []matchstate.FactConflict{}
					}
					response["conflicts"] = conflicts
				}
			}
			writeJSON(w, http.StatusOK, response)
		case r.Method == http.MethodGet && resource == "state" && len(parts) == 2:
			snapshot := store.PublicSnapshot(matchID)
			_, operatorView := authz.view(r, auth.ScopeOperatorTraceRead)
			if !operatorView {
				snapshot = clientSnapshot(snapshot)
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"snapshot": snapshot,
			})
		case r.Method == http.MethodGet && resource == "traces" && len(parts) == 2:
			if _, ok := authz.authorize(w, r, auth.ScopeOperatorTraceRead); !ok {
				return
			}
			limit := 50
			if value := r.URL.Query().Get("limit"); value != "" {
				if parsed, err := strconv.Atoi(value); err == nil {
					limit = parsed
				}
			}
			var traces []companion.Trace
			var err error
			if interactionLedger != nil {
				traces, err = listInteractionTraces(r.Context(), interactionLedger, matchID, limit)
			} else {
				traces, err = traceReader.ListTraces(r.Context(), matchID, limit)
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
		case r.Method == http.MethodGet && resource == "traces" && len(parts) == 3:
			if _, ok := authz.authorize(w, r, auth.ScopeOperatorTraceRead); !ok {
				return
			}
			var trace companion.Trace
			var err error
			if interactionLedger != nil {
				trace, err = getInteractionTrace(r.Context(), interactionLedger, matchID, parts[2])
			} else {
				trace, err = traceReader.GetTrace(r.Context(), matchID, parts[2])
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
		case r.Method == http.MethodPost && resource == "events" && len(parts) == 2:
			operator, authorized := authz.authorize(w, r, auth.ScopeOperatorMatchWrite)
			if !authorized {
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
					return operatorwrite.Response{}, matchStateWriteError(err)
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
			operator, authorized := authz.authorize(w, r, auth.ScopeOperatorFactCorrect)
			if !authorized {
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
					return operatorwrite.Response{}, matchStateWriteError(err)
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

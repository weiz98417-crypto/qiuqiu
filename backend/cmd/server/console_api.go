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
	"errors"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"qiuqiu/internal/auth"
	"qiuqiu/internal/companion"
	"qiuqiu/internal/config"
	"qiuqiu/internal/conversation"
	"qiuqiu/internal/interaction"
	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/memory"
	"qiuqiu/internal/operatorauth"
	"qiuqiu/internal/operatorwrite"
	"qiuqiu/internal/privacy"
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
	cfg           *config.Config
	authz         operatorAuthz
	matches       matchstate.Repository
	traces        companion.TraceReader
	ledger        interaction.Ledger
	sessions      *conversation.WatchSessionRegistry
	memories      *memory.Queue
	operators     operatorauth.Directory
	preferences   talkativenessReader
	writes        *operatorwrite.Service
	interruptions *interruptionRing
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
	UserID            string `json:"userId"`
	Online            bool   `json:"online"`
	Talkativeness     string `json:"talkativeness"`
	OpenThreads       int    `json:"openThreads"`
	PortraitUpdatedAt string `json:"portraitUpdatedAt,omitempty"`
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
	return func(w http.ResponseWriter, r *http.Request) {
		if !applyCORS(w, r, deps.cfg) {
			return
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/api/console/")
		parts := strings.Split(strings.Trim(path, "/"), "/")
		switch {
		case r.Method == http.MethodGet && path == "overview":
			deps.handleOverview(w, r)
		case r.Method == http.MethodGet && len(parts) == 3 && parts[0] == "matches" && parts[2] == "users":
			deps.handleMatchUsers(w, r, parts[1])
		case r.Method == http.MethodGet && len(parts) == 1 && parts[0] == "threads":
			deps.handleListThreads(w, r)
		case r.Method == http.MethodPatch && len(parts) == 2 && parts[0] == "threads":
			deps.handlePatchThread(w, r, parts[1])
		case r.Method == http.MethodGet && len(parts) == 3 && parts[0] == "users" && parts[2] == "portrait":
			deps.handleGetPortrait(w, r, parts[1])
		case r.Method == http.MethodDelete && len(parts) == 3 && parts[0] == "users" && parts[2] == "portrait":
			deps.handleDeletePortrait(w, r, parts[1])
		case r.Method == http.MethodGet && path == "delivery-interruptions":
			deps.handleDeliveryInterruptions(w, r)
		default:
			http.NotFound(w, r)
		}
	}
}

func (deps consoleAPI) handleOverview(w http.ResponseWriter, r *http.Request) {
	if _, ok := deps.authz.authorize(w, r, traceReadScope); !ok {
		return
	}
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

	writeJSON(w, http.StatusOK, map[string]any{
		"matches":         matches,
		"onlineSessions":  onlineSessions,
		"memory":          health,
		"threadAging":     aging,
		"recentProactive": deps.recentProactive(r.Context(), deps.catalog()),
	})
}

func (deps consoleAPI) handleMatchUsers(w http.ResponseWriter, r *http.Request, matchID string) {
	if _, ok := deps.authz.authorize(w, r, traceReadScope); !ok {
		return
	}
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
	if threads, err := deps.memories.Threads(ctx, entry.UserID); err == nil {
		user.OpenThreads = len(threads)
	}
	if _, updatedAt := deps.memories.PortraitEntries(ctx, entry.UserID); !updatedAt.IsZero() {
		user.PortraitUpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	}
	return user
}

func (deps consoleAPI) handleListThreads(w http.ResponseWriter, r *http.Request) {
	if _, ok := deps.authz.authorize(w, r, traceReadScope); !ok {
		return
	}
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

func (deps consoleAPI) handlePatchThread(w http.ResponseWriter, r *http.Request, threadID string) {
	operator, ok := deps.authz.authorize(w, r, matchWriteScope)
	if !ok {
		return
	}
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
		if deps.operators != nil {
			auditCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if auditErr := deps.operators.AppendAudit(auditCtx, operatorName(operator), "thread."+action, "thread:"+threadID); auditErr != nil {
				// The mutation landed; the audit failure is logged, not fatal.
				log.Printf("console: append thread audit for %q: %v", operatorName(operator), auditErr)
			}
		}
		return operatorwrite.JSONResponse(http.StatusOK, map[string]any{"thread": consoleThreadRow(updated)})
	})
}

func (deps consoleAPI) handleGetPortrait(w http.ResponseWriter, r *http.Request, userID string) {
	if _, ok := deps.authz.authorize(w, r, traceReadScope); !ok {
		return
	}
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

func (deps consoleAPI) handleDeletePortrait(w http.ResponseWriter, r *http.Request, userID string) {
	operator, ok := deps.authz.authorize(w, r, matchWriteScope)
	if !ok {
		return
	}
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
		switch {
		case errors.Is(err, privacy.ErrDataDeleted):
			http.Error(w, "用户数据已删除", http.StatusGone)
		case errors.Is(err, privacy.ErrDeletionInProgress):
			http.Error(w, "用户数据删除进行中", http.StatusConflict)
		case errors.Is(err, memory.ErrUnavailable):
			http.Error(w, "画像存储暂不可用", http.StatusServiceUnavailable)
		case errors.Is(err, memory.ErrNotSupported):
			http.Error(w, "画像存储未启用", http.StatusNotImplemented)
		default:
			http.Error(w, "画像删除失败", http.StatusInternalServerError)
		}
		return
	}
	if deps.operators != nil {
		// On-behalf privacy-ops are always attributable (ADR-0008 red line).
		auditCtx, auditCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer auditCancel()
		if auditErr := deps.operators.AppendAudit(auditCtx, operatorName(operator), "portrait.delete", object); auditErr != nil {
			log.Printf("console: append portrait audit for %q: %v", operatorName(operator), auditErr)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (deps consoleAPI) handleDeliveryInterruptions(w http.ResponseWriter, r *http.Request) {
	if _, ok := deps.authz.authorize(w, r, traceReadScope); !ok {
		return
	}
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
func filterTracesByCitationPrefix(traces []companion.Trace, prefix string) []companion.Trace {
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

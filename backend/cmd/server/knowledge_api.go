package main

// 知识策展 API（openspec/changes/knowledge-curation-console，ADR-0017 修订）：
// 条目 DB 化后的运营面。读走 TraceRead、写走 MatchWrite（auditor 只读，
// 与 console 面同一 scope 矩阵）；写路径走 executeOperatorWrite 幂等包裹 +
// operator 归属审计（appendAudit 三段式纪律），保存即生效——Put 后 Library
// 换血，运行时检索立刻看到新条目。路由在独立注册表声明（router.go 口径），
// 挂 /api/console/knowledge 子树（ServeMux 最长前缀优先，不碰既有 console 表）。

import (
	"context"
	"errors"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"qiuqiu/internal/auth"
	"qiuqiu/internal/config"
	"qiuqiu/internal/knowledge"
	"qiuqiu/internal/operatorauth"
	"qiuqiu/internal/operatorwrite"
)

// errKnowledgeEffectiveAt 是 effectiveAt 形状错误的哨兵（映射 400）。
var errKnowledgeEffectiveAt = errors.New("knowledge effective_at is required")

type knowledgeAPI struct {
	cfg       *config.Config
	authz     operatorAuthz
	library   *knowledge.Library
	store     knowledge.Store
	writes    *operatorwrite.Service
	operators operatorauth.Directory
}

// knowledgeEntryView 是运营台消费的条目投影：ADR-0017 字段 + 生效窗口二态
// （active/pending）+ 转会窗复查标记（effective_at 早于最近一次窗闭）+
// 检索后处理参数学五字段（knowledge-worldinfo，表单随附）。
type knowledgeEntryView struct {
	ID             string   `json:"id"`
	Topics         []string `json:"topics"`
	Answer         string   `json:"answer"`
	Source         string   `json:"source"`
	Confidence     float64  `json:"confidence"`
	EffectiveAt    string   `json:"effectiveAt"`
	Status         string   `json:"status"`
	DueReview      bool     `json:"dueReview"`
	Triggers       []string `json:"triggers,omitempty"`
	Quote          string   `json:"quote,omitempty"`
	Priority       int      `json:"priority"`
	InclusionGroup string   `json:"inclusionGroup"`
	StickyTurns    int      `json:"stickyTurns"`
	CooldownTurns  int      `json:"cooldownTurns"`
	Probability    float64  `json:"probability"`
	CreatedBy      string   `json:"createdBy"`
	CreatedAt      string   `json:"createdAt"`
	UpdatedAt      string   `json:"updatedAt"`
}

func handleKnowledgeAPI(deps knowledgeAPI) http.HandlerFunc {
	return mountRoutes(deps.cfg, deps.authz, knowledgeRoutes(deps))
}

// knowledgeRoutes 是知识策展面的声明式路由表：一行一个端点。store 缺席
// （无 DATABASE_URL）时各 handler 自己降级 501——与 Operators 页同款口径。
func knowledgeRoutes(deps knowledgeAPI) []route {
	return []route{
		{method: http.MethodGet, pattern: "/api/console/knowledge", scope: traceReadScope, handler: deps.handleList},
		{method: http.MethodPost, pattern: "/api/console/knowledge", scope: matchWriteScope, handler: deps.handlePost},
		{method: http.MethodGet, pattern: "/api/console/knowledge/{entryId}", scope: traceReadScope, handler: deps.handleGet},
		{method: http.MethodPut, pattern: "/api/console/knowledge/{entryId}", scope: matchWriteScope, handler: deps.handlePut},
	}
}

func knowledgeUnavailable(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "知识条目管理需要持久化存储（DATABASE_URL）"})
}

func knowledgeEntryViewModel(now time.Time, record knowledge.Record) knowledgeEntryView {
	format := func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		return t.UTC().Format(time.RFC3339)
	}
	return knowledgeEntryView{
		ID:             record.ID,
		Topics:         record.Topics,
		Answer:         record.Answer,
		Source:         record.Source,
		Confidence:     record.Confidence,
		EffectiveAt:    format(record.EffectiveAt),
		Status:         knowledge.EntryStatus(now, record.EffectiveAt),
		DueReview:      knowledge.DueReview(now, record.EffectiveAt),
		Triggers:       record.Triggers,
		Quote:          record.Quote,
		Priority:       record.Priority,
		InclusionGroup: record.InclusionGroup,
		StickyTurns:    record.StickyTurns,
		CooldownTurns:  record.CooldownTurns,
		Probability:    record.Probability,
		CreatedBy:      record.CreatedBy,
		CreatedAt:      format(record.CreatedAt),
		UpdatedAt:      format(record.UpdatedAt),
	}
}

// handleList 列条目：q 检索（id/topics/answer/source 子串）、status 生效
// 二态过滤、due=review 待复查过滤、page/pageSize 分页。条目量级是几十条，
// 内存过滤分页足够，不为它扩 Store 接口。
func (deps knowledgeAPI) handleList(w http.ResponseWriter, r *http.Request) {
	if deps.store == nil {
		knowledgeUnavailable(w)
		return
	}
	records, err := deps.store.List(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	dueReview := r.URL.Query().Get("due") == "review"
	now := time.Now()

	filtered := make([]knowledge.Record, 0, len(records))
	for _, record := range records {
		if query != "" && !knowledgeRecordMatches(record, query) {
			continue
		}
		if status == "active" || status == "pending" {
			if knowledge.EntryStatus(now, record.EffectiveAt) != status {
				continue
			}
		}
		if dueReview && !knowledge.DueReview(now, record.EffectiveAt) {
			continue
		}
		filtered = append(filtered, record)
	}

	page, pageSize := parseKnowledgePage(r)
	start := (page - 1) * pageSize
	if start > len(filtered) {
		start = len(filtered)
	}
	end := start + pageSize
	if end > len(filtered) {
		end = len(filtered)
	}
	views := make([]knowledgeEntryView, 0, end-start)
	for _, record := range filtered[start:end] {
		views = append(views, knowledgeEntryViewModel(now, record))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"entries":  views,
		"total":    len(filtered),
		"page":     page,
		"pageSize": pageSize,
	})
}

func knowledgeRecordMatches(record knowledge.Record, query string) bool {
	if strings.Contains(strings.ToLower(record.ID), query) ||
		strings.Contains(strings.ToLower(record.Answer), query) ||
		strings.Contains(strings.ToLower(record.Source), query) {
		return true
	}
	for _, topic := range record.Topics {
		if strings.Contains(strings.ToLower(topic), query) {
			return true
		}
	}
	return false
}

func parseKnowledgePage(r *http.Request) (page, pageSize int) {
	page, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || page < 1 {
		page = 1
	}
	pageSize, err = strconv.Atoi(r.URL.Query().Get("pageSize"))
	if err != nil || pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

func (deps knowledgeAPI) handleGet(w http.ResponseWriter, r *http.Request) {
	if deps.store == nil {
		knowledgeUnavailable(w)
		return
	}
	record, err := deps.store.Get(r.Context(), r.PathValue("entryId"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entry": knowledgeEntryViewModel(time.Now(), record)})
}

// knowledgeUpdateRequest 是 PUT 请求体：与 ADR-0017 字段一致（topics/
// answer/source/confidence/effective_at）+ 后处理参数学五字段
// （knowledge-worldinfo）；triggers/quote 属事件附句策展，编辑面暂不开放
// （保留 DB 值不被覆盖——见 handlePut 的存量合并）。
type knowledgeUpdateRequest struct {
	Topics         []string `json:"topics"`
	Answer         string   `json:"answer"`
	Source         string   `json:"source"`
	Confidence     float64  `json:"confidence"`
	EffectiveAt    string   `json:"effectiveAt"`
	Priority       int      `json:"priority"`
	InclusionGroup string   `json:"inclusionGroup"`
	StickyTurns    int      `json:"stickyTurns"`
	CooldownTurns  int      `json:"cooldownTurns"`
	Probability    float64  `json:"probability"`
}

// knowledgeCreateRequest 是 POST（新建条目）请求体：id 由策展人命名
// （slug：小写字母/数字/连字符，见 knowledgeIDPattern），其余字段同 PUT。
type knowledgeCreateRequest struct {
	ID string `json:"id"`
	knowledgeUpdateRequest
}

// errKnowledgeDuplicate 是同 id 已存在的哨兵（映射 409）。
var errKnowledgeDuplicate = errors.New("knowledge entry id already exists")

// knowledgeIDPattern 是新建条目的 id 形状：slug——小写字母/数字开头，
// 可含连字符（seed 惯用法如 rule-red-card / format-duration），2-64 字符。
var knowledgeIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,63}$`)

// parseKnowledgeEffectiveAt 接受日期（2026-07-01，repo YAML 惯用法）与
// RFC3339 两种形状。
func parseKnowledgeEffectiveAt(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, errKnowledgeEffectiveAt
	}
	if parsed, err := time.Parse("2006-01-02", raw); err == nil {
		return parsed, nil
	}
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return parsed, nil
	}
	return time.Time{}, errKnowledgeEffectiveAt
}

// handlePut 单条编辑：保存即生效。走 executeOperatorWrite 幂等写 + operator
// 归属审计；库内经 library.Put（store 写 + 快照换血），运行时检索立刻可见。
func (deps knowledgeAPI) handlePut(w http.ResponseWriter, r *http.Request) {
	if deps.store == nil || deps.library == nil {
		knowledgeUnavailable(w)
		return
	}
	claims, ok := operatorClaims(deps.authz, w, r)
	if !ok {
		return
	}
	entryID := r.PathValue("entryId")
	if strings.TrimSpace(entryID) == "" {
		http.Error(w, "knowledge entry id is required", http.StatusBadRequest)
		return
	}
	var request knowledgeUpdateRequest
	body, err := decodeOperatorJSON(w, r, &request)
	if err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	effectiveAt, err := parseKnowledgeEffectiveAt(request.EffectiveAt)
	if err != nil {
		http.Error(w, "effectiveAt must be a date (2026-07-01) or RFC3339 timestamp", http.StatusBadRequest)
		return
	}
	// triggers/quote 是判罚事件附句策展面（ADR-0017 2026-09-23 修订），
	// 本期表单不编辑：存量条目带 triggers 时原样保留，避免表单保存误删织写锚。
	triggers, quote := []string(nil), ""
	if existing, err := deps.store.Get(r.Context(), entryID); err == nil {
		triggers, quote = existing.Triggers, existing.Quote
	}
	entry := knowledge.Entry{
		ID:             entryID,
		Topics:         request.Topics,
		Answer:         request.Answer,
		Source:         request.Source,
		Confidence:     request.Confidence,
		EffectiveAt:    effectiveAt,
		Triggers:       triggers,
		Quote:          quote,
		Priority:       request.Priority,
		InclusionGroup: request.InclusionGroup,
		StickyTurns:    request.StickyTurns,
		CooldownTurns:  request.CooldownTurns,
		Probability:    request.Probability,
	}
	executeOperatorWrite(w, r, deps.writes, "knowledge", "knowledge.update", body, func(ctx context.Context) (operatorwrite.Response, error) {
		record, err := deps.library.Put(ctx, entry, operatorName(claims))
		if err != nil {
			return operatorwrite.Response{}, operatorError(http.StatusBadRequest, err)
		}
		appendKnowledgeAudit(deps.operators, claims, "knowledge.update", entryID)
		return operatorwrite.JSONResponse(http.StatusOK, map[string]any{
			"entry": knowledgeEntryViewModel(time.Now(), record),
		})
	})
}

// handlePost 新建条目：同 id 已存在返回 409（走 PutIfAbsent 原子面，
// 消掉先查后插窗口里 seed/并发写入的竞态）。保存即生效口径同 handlePut；
// triggers/quote 不在新建表单——事件附句策展仍走 seed 与后续编辑。
func (deps knowledgeAPI) handlePost(w http.ResponseWriter, r *http.Request) {
	if deps.store == nil || deps.library == nil {
		knowledgeUnavailable(w)
		return
	}
	claims, ok := operatorClaims(deps.authz, w, r)
	if !ok {
		return
	}
	var request knowledgeCreateRequest
	body, err := decodeOperatorJSON(w, r, &request)
	if err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	entryID := strings.TrimSpace(request.ID)
	if !knowledgeIDPattern.MatchString(entryID) {
		http.Error(w, "id must be a slug: lowercase letters, digits and hyphens (2-64 chars)", http.StatusBadRequest)
		return
	}
	effectiveAt, err := parseKnowledgeEffectiveAt(request.EffectiveAt)
	if err != nil {
		http.Error(w, "effectiveAt must be a date (2026-07-01) or RFC3339 timestamp", http.StatusBadRequest)
		return
	}
	entry := knowledge.Entry{
		ID:             entryID,
		Topics:         request.Topics,
		Answer:         request.Answer,
		Source:         request.Source,
		Confidence:     request.Confidence,
		EffectiveAt:    effectiveAt,
		Priority:       request.Priority,
		InclusionGroup: request.InclusionGroup,
		StickyTurns:    request.StickyTurns,
		CooldownTurns:  request.CooldownTurns,
		Probability:    request.Probability,
	}
	executeOperatorWrite(w, r, deps.writes, "knowledge", "knowledge.create", body, func(ctx context.Context) (operatorwrite.Response, error) {
		record, inserted, err := deps.library.PutIfAbsent(ctx, entry, operatorName(claims))
		if err != nil {
			return operatorwrite.Response{}, operatorError(http.StatusBadRequest, err)
		}
		if !inserted {
			return operatorwrite.Response{}, operatorError(http.StatusConflict, errKnowledgeDuplicate)
		}
		appendKnowledgeAudit(deps.operators, claims, "knowledge.create", entryID)
		return operatorwrite.JSONResponse(http.StatusCreated, map[string]any{
			"entry": knowledgeEntryViewModel(time.Now(), record),
		})
	})
}

// appendKnowledgeAudit 与 consoleAPI.appendAudit 同一口径的三段式收敛：
// 3s 独立预算 + 写审计 + 失败仅记日志——业务已落地，审计失败不回滚也不拖响应。
func appendKnowledgeAudit(operators operatorauth.Directory, operator auth.Claims, intent, object string) {
	if operators == nil {
		return
	}
	auditCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := operators.AppendAudit(auditCtx, operatorName(operator), intent, object); err != nil {
		log.Printf("knowledge api: append %s audit for %q: %v", intent, operatorName(operator), err)
	}
}

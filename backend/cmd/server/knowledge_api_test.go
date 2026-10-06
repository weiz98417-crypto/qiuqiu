package main

// 知识策展 API 测试（knowledge-curation-console 7.2）：鉴权矩阵（匿名 401 /
// auditor 只读 / director 可写）、保存即生效（PUT 后 GET 可见且运行时检索
// 命中新条目）、生效二态与转会窗待复查过滤、审计落账断言、无库 501 降级。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"qiuqiu/internal/config"
	"qiuqiu/internal/knowledge"
	"qiuqiu/internal/operatorauth"
	"qiuqiu/internal/operatorwrite"
)

type knowledgeHarness struct {
	store     *knowledge.MemoryStore
	library   *knowledge.Library
	operators *operatorauth.MemoryStore
	handler   http.HandlerFunc
}

func newKnowledgeHarness(t *testing.T) *knowledgeHarness {
	t.Helper()
	h := &knowledgeHarness{store: knowledge.NewMemoryStore(), operators: operatorauth.NewMemoryStore()}
	library, err := knowledge.NewStoreLibrary(context.Background(), h.store, nil)
	if err != nil {
		t.Fatalf("library: %v", err)
	}
	h.library = library
	cfg := &config.Config{Environment: "development", AppToken: "qiuqiu-dev-token"}
	authz := newOperatorAuthz(cfg, h.operators)
	h.handler = handleKnowledgeAPI(knowledgeAPI{
		cfg: cfg, authz: authz, library: h.library, store: h.store,
		writes: operatorwrite.NewMemoryService(), operators: h.operators,
	})
	return h
}

func (h *knowledgeHarness) seedOperators(t *testing.T) {
	t.Helper()
	if _, err := h.operators.Seed(context.Background(), consoleDirectorName, consoleDirectorToken, operatorauth.RoleDirector); err != nil {
		t.Fatalf("seed director: %v", err)
	}
	if _, err := h.operators.Seed(context.Background(), consoleAuditorName, consoleAuditorToken, operatorauth.RoleAuditor); err != nil {
		t.Fatalf("seed auditor: %v", err)
	}
}

func (h *knowledgeHarness) seedEntry(t *testing.T, id, keyword, answer string, effectiveAt time.Time) {
	t.Helper()
	if _, err := h.store.Put(context.Background(), knowledge.Entry{
		ID: id, Topics: []string{keyword}, Answer: answer, Source: "IFAB",
		Confidence: 0.9, EffectiveAt: effectiveAt,
	}, "seed"); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
	if err := h.library.Reload(context.Background()); err != nil {
		t.Fatalf("reload: %v", err)
	}
}

func doKnowledgeRequest(t *testing.T, handler http.HandlerFunc, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	} else {
		reader = strings.NewReader("")
	}
	request := httptest.NewRequest(method, path, reader)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		// 幂等键按 method+path+body 派生：同一测试内多次不同体重试互不撞键
		//（撞键 = 幂等冲突 409，会遮住真正要断言的校验 400）。
		digest := sha256.Sum256([]byte(method + " " + path + " " + body))
		request.Header.Set("Idempotency-Key", "test-knowledge-"+hex.EncodeToString(digest[:8]))
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestKnowledgeAPIAuthMatrix(t *testing.T) {
	h := newKnowledgeHarness(t)
	h.seedOperators(t)
	h.seedEntry(t, "rule-offside", "越位", "越位答案原文。", time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))

	// 匿名：读与写都 401。
	if got := doKnowledgeRequest(t, h.handler, http.MethodGet, "/api/console/knowledge", "", ""); got.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous list = %d, want 401", got.Code)
	}
	if got := doKnowledgeRequest(t, h.handler, http.MethodPut, "/api/console/knowledge/rule-offside", "", `{}`); got.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous put = %d, want 401", got.Code)
	}
	// auditor 只读：GET 200、PUT 403。
	if got := doKnowledgeRequest(t, h.handler, http.MethodGet, "/api/console/knowledge", consoleAuditorToken, ""); got.Code != http.StatusOK {
		t.Fatalf("auditor list = %d, want 200", got.Code)
	}
	if got := doKnowledgeRequest(t, h.handler, http.MethodPut, "/api/console/knowledge/rule-offside", consoleAuditorToken,
		`{"topics":["越位"],"answer":"x","confidence":0.9,"effectiveAt":"2026-07-01"}`); got.Code != http.StatusForbidden {
		t.Fatalf("auditor put = %d, want 403", got.Code)
	}
	// director 可写。
	if got := doKnowledgeRequest(t, h.handler, http.MethodPut, "/api/console/knowledge/rule-offside", consoleDirectorToken,
		`{"topics":["越位","offside"],"answer":"改写后的越位答案。","source":"IFAB Law 11","confidence":0.95,"effectiveAt":"2026-07-01"}`); got.Code != http.StatusOK {
		t.Fatalf("director put = %d body=%s, want 200", got.Code, got.Body.String())
	}
}

func TestKnowledgeAPIPutEffectiveAndAudited(t *testing.T) {
	h := newKnowledgeHarness(t)
	h.seedOperators(t)
	h.seedEntry(t, "rule-offside", "越位", "越位答案原文。", time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))

	recorder := doKnowledgeRequest(t, h.handler, http.MethodPut, "/api/console/knowledge/rule-offside", consoleDirectorToken,
		`{"topics":["越位","offside"],"answer":"策展台改写后的越位解释。","source":"IFAB Law 11","confidence":0.95,"effectiveAt":"2026-07-01","priority":2,"inclusionGroup":"规则组","stickyTurns":3,"cooldownTurns":5,"probability":0.7}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("put = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Entry knowledgeEntryView `json:"entry"`
	}
	decodeConsoleJSON(t, recorder, &payload)
	if payload.Entry.Answer != "策展台改写后的越位解释。" || payload.Entry.CreatedBy != "seed" {
		t.Fatalf("entry = %+v, want edited answer with first-creator preserved", payload.Entry)
	}
	// 参数学五字段透传（knowledge-worldinfo）：表单保存 → 视图回显。
	if payload.Entry.Priority != 2 || payload.Entry.InclusionGroup != "规则组" ||
		payload.Entry.StickyTurns != 3 || payload.Entry.CooldownTurns != 5 || payload.Entry.Probability != 0.7 {
		t.Fatalf("worldinfo view = %+v, want passthrough echo", payload.Entry)
	}

	// 列表可见新答案（生效中）。
	recorder = doKnowledgeRequest(t, h.handler, http.MethodGet, "/api/console/knowledge?q=越位", consoleDirectorToken, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("list = %d", recorder.Code)
	}
	var list struct {
		Entries []knowledgeEntryView `json:"entries"`
		Total   int                  `json:"total"`
	}
	decodeConsoleJSON(t, recorder, &list)
	if list.Total != 1 || len(list.Entries) != 1 || list.Entries[0].Answer != "策展台改写后的越位解释。" {
		t.Fatalf("list = %+v, want the edited entry", list)
	}
	if list.Entries[0].Status != "active" || list.Entries[0].DueReview {
		t.Fatalf("status/due = %s/%v, want active/false for a post-window entry", list.Entries[0].Status, list.Entries[0].DueReview)
	}

	// 运行时生效：库快照已换血，检索双路（关键词路）命中新条目。
	entry, ok := h.library.Search(context.Background(), "给我讲讲越位")
	if !ok || entry.Answer != "策展台改写后的越位解释。" {
		t.Fatalf("runtime search = %+v ok=%v, want edited entry (save is effective)", entry, ok)
	}

	// 审计落账：operator 归属 + 动作 + 对象。
	audits, err := h.operators.RecentAudit(context.Background(), 10)
	if err != nil {
		t.Fatalf("recent audit: %v", err)
	}
	if len(audits) != 1 || audits[0].OperatorName != consoleDirectorName || audits[0].Action != "knowledge.update" || audits[0].Object != "rule-offside" {
		t.Fatalf("audits = %+v, want one knowledge.update by 阿琴 on rule-offside", audits)
	}
}

func TestKnowledgeAPIPutPreservesTriggersAndQuote(t *testing.T) {
	h := newKnowledgeHarness(t)
	h.seedOperators(t)
	effective := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	h.seedEntry(t, "judgment-var", "var", "VAR 只介入四类情况。", effective)
	// 手工补 triggers/quote（seedEntry 不带）。
	entry, _ := h.store.Get(context.Background(), "judgment-var")
	entry.Triggers = []string{"var_check"}
	entry.Quote = "VAR 只介入进球、点球、直接红牌和认错球员四类情况"
	if _, err := h.store.Put(context.Background(), entry.Entry, "seed"); err != nil {
		t.Fatalf("seed triggers: %v", err)
	}
	if err := h.library.Reload(context.Background()); err != nil {
		t.Fatalf("reload: %v", err)
	}

	// 表单保存不携带 triggers/quote 字段：存量织写锚不被误删。
	if got := doKnowledgeRequest(t, h.handler, http.MethodPut, "/api/console/knowledge/judgment-var", consoleDirectorToken,
		`{"topics":["var","视频助理裁判"],"answer":"VAR 只介入四类情况。","confidence":0.9,"effectiveAt":"2026-07-01"}`); got.Code != http.StatusOK {
		t.Fatalf("put = %d body=%s", got.Code, got.Body.String())
	}
	got, err := h.store.Get(context.Background(), "judgment-var")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got.Triggers) != 1 || got.Triggers[0] != "var_check" || got.Quote == "" {
		t.Fatalf("triggers/quote = %v/%q, want preserved weave anchor", got.Triggers, got.Quote)
	}
	if _, ok := h.library.TriggerLookup("var_check"); !ok {
		t.Fatal("trigger index must survive an operator edit")
	}
}

func TestKnowledgeAPIFiltersAndPagination(t *testing.T) {
	h := newKnowledgeHarness(t)
	h.seedOperators(t)
	h.seedEntry(t, "rule-offside", "越位", "越位答案原文。", time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))
	h.seedEntry(t, "player-haaland", "哈兰德", "哈兰德是曼城中锋。", time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	h.seedEntry(t, "player-stale", "旧档案", "转会窗前的旧档案。", time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC))
	h.seedEntry(t, "rule-future", "新规", "下赛季才生效的新规。", time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC))

	var list struct {
		Entries  []knowledgeEntryView `json:"entries"`
		Total    int                  `json:"total"`
		Page     int                  `json:"page"`
		PageSize int                  `json:"pageSize"`
	}

	// status=pending：只待生效。
	decodeConsoleJSON(t, doKnowledgeRequest(t, h.handler, http.MethodGet, "/api/console/knowledge?status=pending", consoleDirectorToken, ""), &list)
	if list.Total != 1 || list.Entries[0].ID != "rule-future" {
		t.Fatalf("pending = %+v, want only rule-future", list)
	}
	// status=active：三条已生效。
	decodeConsoleJSON(t, doKnowledgeRequest(t, h.handler, http.MethodGet, "/api/console/knowledge?status=active", consoleDirectorToken, ""), &list)
	if list.Total != 3 {
		t.Fatalf("active total = %d, want 3", list.Total)
	}
	// due=review：生效窗口早于最近窗闭（2026-07-01）→ 旧档案。
	decodeConsoleJSON(t, doKnowledgeRequest(t, h.handler, http.MethodGet, "/api/console/knowledge?due=review", consoleDirectorToken, ""), &list)
	if list.Total != 1 || list.Entries[0].ID != "player-stale" || !list.Entries[0].DueReview {
		t.Fatalf("due review = %+v, want only player-stale", list)
	}
	// q 检索：answer 子串。
	decodeConsoleJSON(t, doKnowledgeRequest(t, h.handler, http.MethodGet, "/api/console/knowledge?q=曼城", consoleDirectorToken, ""), &list)
	if list.Total != 1 || list.Entries[0].ID != "player-haaland" {
		t.Fatalf("q search = %+v, want player-haaland", list)
	}
	// 分页：pageSize=2 第 2 页只剩 1 条。
	decodeConsoleJSON(t, doKnowledgeRequest(t, h.handler, http.MethodGet, "/api/console/knowledge?pageSize=2&page=2", consoleDirectorToken, ""), &list)
	if list.Total != 4 || len(list.Entries) != 2 || list.Page != 2 || list.PageSize != 2 {
		t.Fatalf("page 2 = %+v, want 2 of 4 entries", list)
	}
	// 单条读 + 404。
	decodeConsoleJSON(t, doKnowledgeRequest(t, h.handler, http.MethodGet, "/api/console/knowledge/rule-offside", consoleDirectorToken, ""), &struct{}{})
	if got := doKnowledgeRequest(t, h.handler, http.MethodGet, "/api/console/knowledge/missing", consoleDirectorToken, ""); got.Code != http.StatusNotFound {
		t.Fatalf("get missing = %d, want 404", got.Code)
	}
}

func TestKnowledgeAPIPutValidation(t *testing.T) {
	h := newKnowledgeHarness(t)
	h.seedOperators(t)
	cases := []struct {
		name string
		body string
	}{
		{"missing topics", `{"topics":[],"answer":"答案","confidence":0.9,"effectiveAt":"2026-07-01"}`},
		{"missing answer", `{"topics":["越位"],"answer":"  ","confidence":0.9,"effectiveAt":"2026-07-01"}`},
		{"confidence out of range", `{"topics":["越位"],"answer":"答案","confidence":1.5,"effectiveAt":"2026-07-01"}`},
		{"bad effectiveAt", `{"topics":["越位"],"answer":"答案","confidence":0.9,"effectiveAt":"下一个转会窗"}`},
		{"invalid json", `{"topics":`},
	}
	for _, tc := range cases {
		if got := doKnowledgeRequest(t, h.handler, http.MethodPut, "/api/console/knowledge/new-entry", consoleDirectorToken, tc.body); got.Code != http.StatusBadRequest {
			t.Fatalf("%s: put = %d body=%s, want 400", tc.name, got.Code, got.Body.String())
		}
	}
}

func TestKnowledgeAPIUnavailableWithoutStore(t *testing.T) {
	h := newKnowledgeHarness(t)
	h.seedOperators(t)
	cfg := &config.Config{Environment: "development", AppToken: "qiuqiu-dev-token"}
	authz := newOperatorAuthz(cfg, h.operators)
	handler := handleKnowledgeAPI(knowledgeAPI{
		cfg: cfg, authz: authz, library: nil, store: nil,
		writes: operatorwrite.NewMemoryService(), operators: h.operators,
	})
	if got := doKnowledgeRequest(t, handler, http.MethodGet, "/api/console/knowledge", consoleDirectorToken, ""); got.Code != http.StatusNotImplemented {
		t.Fatalf("list without store = %d, want 501", got.Code)
	}
	if got := doKnowledgeRequest(t, handler, http.MethodPut, "/api/console/knowledge/rule-offside", consoleDirectorToken,
		`{"topics":["越位"],"answer":"答案","confidence":0.9,"effectiveAt":"2026-07-01"}`); got.Code != http.StatusNotImplemented {
		t.Fatalf("put without store = %d, want 501", got.Code)
	}
}

func TestKnowledgeAPIIdempotentReplay(t *testing.T) {
	h := newKnowledgeHarness(t)
	h.seedOperators(t)
	h.seedEntry(t, "rule-offside", "越位", "越位答案原文。", time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))
	body := `{"topics":["越位"],"answer":"幂等重放不重复落账。","confidence":0.9,"effectiveAt":"2026-07-01"}`
	first := doKnowledgeRequest(t, h.handler, http.MethodPut, "/api/console/knowledge/rule-offside", consoleDirectorToken, body)
	if first.Code != http.StatusOK {
		t.Fatalf("first put = %d", first.Code)
	}
	auditsBefore, _ := h.operators.RecentAudit(context.Background(), 10)

	// 同键同体重放：命中幂等记录，审计不重复（键与 doKnowledgeRequest 的
	// 派生口径一致）。
	digest := sha256.Sum256([]byte(http.MethodPut + " /api/console/knowledge/rule-offside " + body))
	replayKey := "test-knowledge-" + hex.EncodeToString(digest[:8])
	replay := httptest.NewRequest(http.MethodPut, "/api/console/knowledge/rule-offside", strings.NewReader(body))
	replay.Header.Set("Authorization", "Bearer "+consoleDirectorToken)
	replay.Header.Set("Idempotency-Key", replayKey)
	recorder := httptest.NewRecorder()
	h.handler.ServeHTTP(recorder, replay)
	if recorder.Code != http.StatusOK || recorder.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("replay = %d replayed=%q, want 200 with replay header", recorder.Code, recorder.Header().Get("Idempotency-Replayed"))
	}
	auditsAfter, _ := h.operators.RecentAudit(context.Background(), 10)
	if len(auditsAfter) != len(auditsBefore) {
		t.Fatalf("audit rows = %d after replay, want %d (no duplicate)", len(auditsAfter), len(auditsBefore))
	}
	var decoded map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode replay body: %v", err)
	}
}

// doKnowledgeRequestWithKey 与 doKnowledgeRequest 同形,但幂等键显式给定——
// 「同 id 重复新建」要换新键才会真正打到 PutIfAbsent(同键同体会走幂等重放)。
func doKnowledgeRequestWithKey(t *testing.T, handler http.HandlerFunc, method, path, token, body, key string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		request.Header.Set("Idempotency-Key", key)
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestKnowledgeAPICreateFlow(t *testing.T) {
	h := newKnowledgeHarness(t)
	h.seedOperators(t)

	body := `{"id":"rule-stoppage","topics":["补时","伤停补时"],"answer":"补时答案原文。","source":"IFAB Law 7","confidence":0.9,"effectiveAt":"2026-08-01"}`
	recorder := doKnowledgeRequest(t, h.handler, http.MethodPost, "/api/console/knowledge", consoleDirectorToken, body)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("post = %d body=%s, want 201", recorder.Code, recorder.Body.String())
	}
	var created struct {
		Entry knowledgeEntryView `json:"entry"`
	}
	decodeConsoleJSON(t, recorder, &created)
	if created.Entry.ID != "rule-stoppage" || created.Entry.CreatedBy != consoleDirectorName {
		t.Fatalf("created = %+v, want id=rule-stoppage createdBy=%s", created.Entry, consoleDirectorName)
	}

	// 同 id 换键重复新建:409(PutIfAbsent 原子面),且原条目不被覆盖。
	duplicate := doKnowledgeRequestWithKey(t, h.handler, http.MethodPost, "/api/console/knowledge", consoleDirectorToken,
		`{"id":"rule-stoppage","topics":["补时"],"answer":"覆盖尝试。","confidence":0.5,"effectiveAt":"2026-08-01"}`, "test-knowledge-duplicate-key")
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate post = %d body=%s, want 409", duplicate.Code, duplicate.Body.String())
	}

	// GET 可见,答案仍是首次新建值。
	recorder = doKnowledgeRequest(t, h.handler, http.MethodGet, "/api/console/knowledge/rule-stoppage", consoleDirectorToken, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("get = %d", recorder.Code)
	}
	var fetched struct {
		Entry knowledgeEntryView `json:"entry"`
	}
	decodeConsoleJSON(t, recorder, &fetched)
	if fetched.Entry.Answer != "补时答案原文。" || fetched.Entry.CreatedBy != consoleDirectorName {
		t.Fatalf("fetched = %+v, want first-create answer and creator", fetched.Entry)
	}

	// 运行时生效:库快照已换血,检索命中新条目。
	entry, ok := h.library.Search(context.Background(), "这场补时几分钟")
	if !ok || entry.Answer != "补时答案原文。" {
		t.Fatalf("runtime search = %+v ok=%v, want created entry", entry, ok)
	}

	// 审计落账:knowledge.create 归属 director。
	audits, err := h.operators.RecentAudit(context.Background(), 10)
	if err != nil {
		t.Fatalf("recent audit: %v", err)
	}
	if len(audits) != 1 || audits[0].OperatorName != consoleDirectorName || audits[0].Action != "knowledge.create" || audits[0].Object != "rule-stoppage" {
		t.Fatalf("audits = %+v, want one knowledge.create on rule-stoppage", audits)
	}
}

func TestKnowledgeAPICreateValidation(t *testing.T) {
	h := newKnowledgeHarness(t)
	h.seedOperators(t)
	cases := []struct {
		name string
		body string
	}{
		{"missing id", `{"topics":["越位"],"answer":"答案","confidence":0.9,"effectiveAt":"2026-07-01"}`},
		{"uppercase id", `{"id":"Rule-Offside","topics":["越位"],"answer":"答案","confidence":0.9,"effectiveAt":"2026-07-01"}`},
		{"single char id", `{"id":"a","topics":["越位"],"answer":"答案","confidence":0.9,"effectiveAt":"2026-07-01"}`},
		{"id with underscore", `{"id":"rule_offside","topics":["越位"],"answer":"答案","confidence":0.9,"effectiveAt":"2026-07-01"}`},
		{"missing topics", `{"id":"rule-offside","topics":[],"answer":"答案","confidence":0.9,"effectiveAt":"2026-07-01"}`},
		{"missing answer", `{"id":"rule-offside","topics":["越位"],"answer":"  ","confidence":0.9,"effectiveAt":"2026-07-01"}`},
		{"confidence out of range", `{"id":"rule-offside","topics":["越位"],"answer":"答案","confidence":1.5,"effectiveAt":"2026-07-01"}`},
		{"bad effectiveAt", `{"id":"rule-offside","topics":["越位"],"answer":"答案","confidence":0.9,"effectiveAt":"下一个转会窗"}`},
	}
	for _, tc := range cases {
		if got := doKnowledgeRequest(t, h.handler, http.MethodPost, "/api/console/knowledge", consoleDirectorToken, tc.body); got.Code != http.StatusBadRequest {
			t.Fatalf("%s: post = %d body=%s, want 400", tc.name, got.Code, got.Body.String())
		}
	}
}

func TestKnowledgeAPICreateAuthAndUnavailable(t *testing.T) {
	h := newKnowledgeHarness(t)
	h.seedOperators(t)
	body := `{"id":"rule-stoppage","topics":["补时"],"answer":"答案","confidence":0.9,"effectiveAt":"2026-08-01"}`

	// 匿名 401;auditor 只读 403。
	if got := doKnowledgeRequest(t, h.handler, http.MethodPost, "/api/console/knowledge", "", body); got.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous post = %d, want 401", got.Code)
	}
	if got := doKnowledgeRequest(t, h.handler, http.MethodPost, "/api/console/knowledge", consoleAuditorToken, body); got.Code != http.StatusForbidden {
		t.Fatalf("auditor post = %d, want 403", got.Code)
	}

	// 无库降级 501。
	cfg := &config.Config{Environment: "development", AppToken: "qiuqiu-dev-token"}
	authz := newOperatorAuthz(cfg, h.operators)
	handler := handleKnowledgeAPI(knowledgeAPI{
		cfg: cfg, authz: authz, library: nil, store: nil,
		writes: operatorwrite.NewMemoryService(), operators: h.operators,
	})
	if got := doKnowledgeRequest(t, handler, http.MethodPost, "/api/console/knowledge", consoleDirectorToken, body); got.Code != http.StatusNotImplemented {
		t.Fatalf("post without store = %d, want 501", got.Code)
	}
}

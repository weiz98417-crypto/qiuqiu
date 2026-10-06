package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"qiuqiu/internal/config"
	"qiuqiu/internal/operatorauth"
	"qiuqiu/internal/operatorwrite"
	"qiuqiu/internal/tts"
	"qiuqiu/internal/ttssupply"
)

// ttsSupplyHarness 装配带供给开关的 console 面：云腿 mock、本地腿指向
// httptest、探测小周期、内存设置存储。
type ttsSupplyHarness struct {
	console      http.HandlerFunc
	operators    *operatorauth.MemoryStore
	supply       *tts.SupplySwitch
	probe        *tts.LocalProbe
	localServer  *httptest.Server
	cloudHealthy bool
}

func newTTSSupplyHarness(t *testing.T) *ttsSupplyHarness {
	t.Helper()
	h := &ttsSupplyHarness{cloudHealthy: true}
	h.localServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.cloudHealthy {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("RIFF-local"))
	}))
	t.Cleanup(h.localServer.Close)

	cloud := tts.NewMockClient([]byte("cloud-audio"))
	local := tts.NewLocalClient(h.localServer.URL)
	h.supply = tts.NewSupplySwitch(cloud, local)
	h.probe = tts.NewLocalProbe(local, 5*time.Millisecond, time.Second)
	h.probe.Start()
	t.Cleanup(h.probe.Close)

	h.operators = operatorauth.NewMemoryStore()
	if _, err := h.operators.Seed(context.Background(), consoleDirectorName, consoleDirectorToken, operatorauth.RoleDirector); err != nil {
		t.Fatalf("seed director: %v", err)
	}
	cfg := &config.Config{Environment: "development", AppToken: "qiuqiu-dev-token"}
	h.console = handleConsoleAPI(consoleAPI{
		cfg:            cfg,
		authz:          newOperatorAuthz(cfg, h.operators),
		operators:      h.operators,
		writes:         operatorwrite.NewMemoryService(),
		supply:         h.supply,
		localProbe:     h.probe,
		supplySettings: ttssupply.NewMemoryStore(),
	})
	return h
}

func (h *ttsSupplyHarness) waitLocalAvailable(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if h.probe.Available() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("local probe never became available")
}

type ttsSupplyPayload struct {
	Mode            string                 `json:"mode"`
	Local           tts.LocalProbeSnapshot `json:"local"`
	LocalSelectable bool                   `json:"localSelectable"`
	LocalFailures   int64                  `json:"localFailures"`
	CloudFallbacks  int64                  `json:"cloudFallbacks"`
}

// 默认态：cloud + 本地未过门不可选；门过之后 PATCH local 即时生效、审计
// 落账、幂等重放带标记；非法 mode 400。
func TestTTSSupplyGateAndAudit(t *testing.T) {
	h := newTTSSupplyHarness(t)

	// 门未过：本地不可选（探测还在跑首轮）。
	for h.probe.Available() == false && h.probe.Snapshot().Probes == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	get := doConsoleRequest(t, consoleRequest{method: http.MethodGet, path: "/api/console/tts-supply", token: consoleDirectorToken, handler: h.console})
	if get.Code != http.StatusOK {
		t.Fatalf("GET = %d", get.Code)
	}
	var state ttsSupplyPayload
	decodeConsoleJSON(t, get, &state)
	if state.Mode != tts.SupplyModeCloud {
		t.Fatalf("initial mode = %q, want cloud", state.Mode)
	}
	if !state.Local.Configured {
		t.Fatal("local leg should report configured")
	}

	// 非法 mode 400（写入严格）。
	bad := doConsoleRequest(t, consoleRequest{method: http.MethodPatch, path: "/api/console/tts-supply", token: consoleDirectorToken, body: `{"mode":"turbo"}`, handler: h.console})
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("invalid mode = %d, want 400", bad.Code)
	}

	// 健康门通过后：PATCH local_first 即时生效。
	h.waitLocalAvailable(t)
	patch := doConsoleRequest(t, consoleRequest{
		method: http.MethodPatch, path: "/api/console/tts-supply", token: consoleDirectorToken,
		body: `{"mode":"local_first"}`, idempotencyKey: "tts-supply-1", handler: h.console,
	})
	if patch.Code != http.StatusOK {
		t.Fatalf("PATCH local_first = %d: %s", patch.Code, patch.Body.String())
	}
	decodeConsoleJSON(t, patch, &state)
	if state.Mode != tts.SupplyModeLocalFirst || !state.LocalSelectable {
		t.Fatalf("after patch mode=%q selectable=%v", state.Mode, state.LocalSelectable)
	}
	if h.supply.Mode() != tts.SupplyModeLocalFirst {
		t.Fatalf("runtime mode = %q, want local_first (即时生效)", h.supply.Mode())
	}

	// 幂等重放。
	replay := doConsoleRequest(t, consoleRequest{
		method: http.MethodPatch, path: "/api/console/tts-supply", token: consoleDirectorToken,
		body: `{"mode":"local_first"}`, idempotencyKey: "tts-supply-1", handler: h.console,
	})
	if replay.Code != http.StatusOK || replay.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("replay = %d replayed=%q", replay.Code, replay.Header().Get("Idempotency-Replayed"))
	}
}

// 回退链（HTTP 层端到端）：local_first 态下本地腿死掉，合成回云——
// 供给开关的消费者（语音链路）句子不断流。
func TestTTSSupplyFallbackEndToEnd(t *testing.T) {
	h := newTTSSupplyHarness(t)
	h.waitLocalAvailable(t)
	h.supply.SetMode(tts.SupplyModeLocalFirst)

	h.cloudHealthy = false
	result, err := h.supply.Synthesize(context.Background(), "进球了", tts.VoiceOpts{})
	if err != nil {
		t.Fatalf("synthesize after local failure: %v", err)
	}
	if !strings.Contains(string(result.AudioData), "cloud-audio") {
		t.Fatalf("audio = %q, want cloud fallback audio", result.AudioData)
	}
	if h.supply.LocalFailures() != 1 || h.supply.CloudFallbacks() != 1 {
		t.Fatalf("counters = (%d, %d), want (1, 1)", h.supply.LocalFailures(), h.supply.CloudFallbacks())
	}

	// GET 面把回退计数带给运营台。
	get := doConsoleRequest(t, consoleRequest{method: http.MethodGet, path: "/api/console/tts-supply", token: consoleDirectorToken, handler: h.console})
	var state ttsSupplyPayload
	decodeConsoleJSON(t, get, &state)
	if state.LocalFailures != 1 || state.CloudFallbacks != 1 {
		t.Fatalf("state counters = (%d, %d)", state.LocalFailures, state.CloudFallbacks)
	}
	if jsonBytes, _ := json.Marshal(state); !strings.Contains(string(jsonBytes), "localFirst") && state.Mode != "local_first" {
		t.Fatalf("mode wire format = %q, want local_first literal", state.Mode)
	}
}

// 审计落账：PATCH 成功后 operator 审计里有 tts_supply.update 行（appendAudit
// 收敛点 → Directory.RecentAudit 读回）。
func TestTTSSupplyAuditRow(t *testing.T) {
	h := newTTSSupplyHarness(t)
	h.waitLocalAvailable(t)
	if got := doConsoleRequest(t, consoleRequest{
		method: http.MethodPatch, path: "/api/console/tts-supply", token: consoleDirectorToken,
		body: `{"mode":"local"}`, idempotencyKey: "tts-supply-audit", handler: h.console,
	}); got.Code != http.StatusOK {
		t.Fatalf("PATCH local = %d: %s", got.Code, got.Body.String())
	}
	rows, err := h.operators.RecentAudit(context.Background(), 10)
	if err != nil {
		t.Fatalf("RecentAudit: %v", err)
	}
	found := false
	for _, row := range rows {
		if row.Action == "tts_supply.update" && row.Object == tts.SupplyModeLocal {
			found = true
		}
	}
	if !found {
		t.Fatalf("audit rows = %+v, want tts_supply.update/local", rows)
	}
}

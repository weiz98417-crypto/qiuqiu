package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"qiuqiu/internal/companion"
	"qiuqiu/internal/config"
	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/operatorauth"
	"qiuqiu/internal/interaction"
)

func TestPulsePathReplication(t *testing.T) {
	inner := interaction.NewMemoryLedger()
	ledger := interaction.Ledger(newObservingLedger(inner, func(interaction.Event) {}))
	ctx := context.Background()
	now := time.Now().UTC()
	_, err := ledger.Append(ctx, interaction.Event{
		ID: "bc-1", Kind: interaction.KindBackchannel, UserID: "usr_anon", MatchID: "test",
		Phrase: "神扑！", CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	snapshot, ok := ledger.(interaction.MatchSnapshotLedger)
	if !ok {
		t.Fatal("observingLedger must satisfy MatchSnapshotLedger")
	}
	events, err := snapshot.ListMatchSnapshot(ctx, "test")
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	count := 0
	cutoff := now.Add(-24 * time.Hour)
	for _, event := range events {
		if event.Kind == interaction.KindBackchannel && event.CreatedAt.After(cutoff) {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("pulse count = %d, want 1 (events=%d)", count, len(events))
	}
}

// handler 级复刻：Append 一条微反应 → handleOverview 的 backchannel 脉搏
// 必须数到 1（活服务实测 0 的对账用例）。
func TestOverviewBackchannelPulseHandler(t *testing.T) {
	inner := interaction.NewMemoryLedger()
	ledger := interaction.Ledger(newObservingLedger(inner, func(interaction.Event) {}))
	ctx := context.Background()
	now := time.Now().UTC()
	if _, err := ledger.Append(ctx, interaction.Event{
		ID: "bc-1", Kind: interaction.KindBackchannel, UserID: "usr_anon", MatchID: "test",
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("append: %v", err)
	}

	matchStore := matchstate.NewStore()
	if _, _, err := matchStore.SetConfig("test", matchstate.MatchConfig{HomeTeam: "西班牙", AwayTeam: "德国"}); err != nil {
		t.Fatalf("config: %v", err)
	}
	if _, _, err := matchStore.Create("test", matchstate.MatchEvent{EventType: "goal", TeamID: "home", Score: matchstate.Score{Home: 1}, Period: "first_half", Clock: "24:10", Description: "probe", Status: "active", Visibility: "public", CreatedAt: now.Format(time.RFC3339)}); err != nil {
		t.Fatalf("seed match: %v", err)
	}
	if _, err := matchStore.SetLifecycle("test", "live"); err != nil {
		t.Fatalf("lifecycle: %v", err)
	}
	cfg := &config.Config{Environment: "development", AppToken: "qiuqiu-dev-token"}
	store := operatorauth.NewMemoryStore()
	authz := newOperatorAuthz(cfg, store).withJWTSecret("secret-0123456789abcdef")
	// APP_TOKEN 通道 authorize 需要 cfg 匹配；newOperatorAuthz 内部读 cfg。
	deps := consoleAPI{
		cfg: cfg, authz: authz,
		ledger:   ledger,
		matches:  matchStore,
		traces:   companion.NewStoreMemoryTools(matchstate.NewStore()),
		sessions: nil,
	}
	req := httptest.NewRequest("GET", "/api/console/overview", nil)
	req.Header.Set("Authorization", "Bearer qiuqiu-dev-token")
	rec := httptest.NewRecorder()
	deps.handleOverview(rec, req)
	if rec.Code != 200 {
		t.Fatalf("overview status = %d: %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Backchannel consoleBackchannelPulse `json:"backchannel"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Backchannel.Emitted != 1 {
		t.Fatalf("emitted = %d, want 1: %s", payload.Backchannel.Emitted, rec.Body.String()[:200])
	}
}

package main

// auto-hosting 2.6:一键托管端点——ESPN summary 填充配置 + 开场 + 挂源,
// 幂等写 + 审计同一纪律。

import (
	"context"
	"net/http"
	"testing"
	"time"

	"qiuqiu/internal/config"
	"qiuqiu/internal/datasource"
	"qiuqiu/internal/matchstate"
)

type fakeEspnClient struct {
	summary *datasource.EspnSummary
}

func (c *fakeEspnClient) GetSummary(league, eventID string) (*datasource.EspnSummary, error) {
	return c.summary, nil
}

func espnHostFixtureSummary() *datasource.EspnSummary {
	summary := &datasource.EspnSummary{}
	summary.Header.League.Name = "Premier League"
	summary.Header.Competitions = []struct {
		Date        string                      `json:"date"`
		Status      datasource.EspnStatus       `json:"status"`
		Competitors []datasource.EspnCompetitor `json:"competitors"`
	}{{
		Date:   "2026-10-04T16:30:00Z",
		Status: datasource.EspnStatus{},
		Competitors: []datasource.EspnCompetitor{
			{HomeAway: "home", Team: struct {
				DisplayName string `json:"displayName"`
			}{DisplayName: "Arsenal"}, Score: "0"},
			{HomeAway: "away", Team: struct {
				DisplayName string `json:"displayName"`
			}{DisplayName: "Liverpool"}, Score: "0"},
		},
	}}
	return summary
}

func newHostHarness(t *testing.T, summary *datasource.EspnSummary) (http.HandlerFunc, *matchstate.Store, *datasource.Manager) {
	t.Helper()
	store := matchstate.NewStore()
	sources := datasource.NewManager(context.Background(), store, nil, datasource.ManagerConfig{EspnPollInterval: 10 * time.Millisecond})
	sources = sources.WithEspnClient(&fakeEspnClient{summary: summary})
	t.Cleanup(sources.Close)
	cfg := &config.Config{Environment: "development", AppToken: "eval-token"}
	handler := handleMatchAPIWithOperatorAuth(store, nil, nil, cfg, nil, sources, nil, nil, operatorAuthz{cfg: cfg})
	return handler, store, sources
}

func TestHostEndpointFillsConfigAndStartsEspn(t *testing.T) {
	handler, store, sources := newHostHarness(t, espnHostFixtureSummary())

	response := doJSON(t, handler, http.MethodPost,
		"/api/matches/match-host/host?token=eval-token&idempotency-key=host-1",
		map[string]any{"league": "eng.1", "espnEventId": "701234"})
	if response.Code != http.StatusOK {
		t.Fatalf("host = %d body=%s, want 200", response.Code, response.Body.String())
	}

	saved := store.Config("match-host")
	if saved.HomeTeam != "Arsenal" || saved.AwayTeam != "Liverpool" {
		t.Fatalf("config = %+v, want ESPN-filled teams", saved)
	}
	if saved.Competition != "Premier League" || saved.Kickoff != "2026-10-04T16:30:00Z" {
		t.Fatalf("config competition/kickoff = %q/%q, want ESPN values", saved.Competition, saved.Kickoff)
	}

	// ESPN 源已挂:状态面 running(活源)。
	deadline := time.Now().Add(2 * time.Second)
	for {
		status := sources.Status("match-host")
		if status.ActiveSource == datasource.SourceESPN && status.Sources[datasource.SourceESPN].State == datasource.StateRunning {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("espn source not running in time: %+v", status)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestHostEndpointRequiresLeagueAndEventID(t *testing.T) {
	handler, _, _ := newHostHarness(t, espnHostFixtureSummary())
	response := doJSON(t, handler, http.MethodPost,
		"/api/matches/match-host/host?token=eval-token&idempotency-key=host-2",
		map[string]any{"league": "eng.1"})
	if response.Code != http.StatusBadRequest {
		t.Fatalf("missing espnEventId = %d, want 400", response.Code)
	}
}

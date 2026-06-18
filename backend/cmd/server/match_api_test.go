package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"qiuqiu/internal/companion"
	"qiuqiu/internal/config"
	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/pipeline"
)

func TestEvalMatchAPIConfigQuietManualAndAutoFallback(t *testing.T) {
	store := matchstate.NewStore()
	traces := companion.NewStoreMemoryTools(store)
	cfg := &config.Config{AppToken: "eval-token"}
	promptMgr := pipeline.NewPromptManager()
	handler := handleMatchAPI(store, traces, traces, cfg, nil, promptMgr)

	configResp := doJSON(t, handler, http.MethodPost, "/api/matches/api-eval/config?token=eval-token", matchstate.MatchConfig{
		HomeTeam: "Spain",
		AwayTeam: "Germany",
		HomePlayers: []matchstate.Player{
			{Number: "10", Name: "Pedri", Position: "CM"},
		},
	})
	if configResp.Code != http.StatusOK {
		t.Fatalf("config status=%d body=%s", configResp.Code, configResp.Body.String())
	}

	quietResp := doJSON(t, handler, http.MethodPost, "/api/matches/api-eval/events?token=eval-token", matchstate.MatchEvent{
		EventType:     "shot",
		Clock:         "10:00",
		TeamID:        "home",
		TeamName:      "Spain",
		PlayerName:    "Pedri",
		Score:         matchstate.Score{Home: 0, Away: 0},
		Description:   "A shot from distance.",
		ProactiveText: "__quiet__",
	})
	if quietResp.Code != http.StatusCreated {
		t.Fatalf("quiet status=%d body=%s", quietResp.Code, quietResp.Body.String())
	}
	quietEvent := decodeEvent(t, quietResp)
	if quietEvent.ProactiveText != "" {
		t.Fatalf("quiet mode should clear proactiveText, got %q", quietEvent.ProactiveText)
	}

	manual := "Manual director line."
	manualResp := doJSON(t, handler, http.MethodPost, "/api/matches/api-eval/events?token=eval-token", matchstate.MatchEvent{
		EventType:     "goal",
		Clock:         "23:41",
		TeamID:        "home",
		TeamName:      "Spain",
		PlayerName:    "Pedri",
		Score:         matchstate.Score{Home: 1, Away: 0},
		Intensity:     5,
		Description:   "Pedri scores.",
		ProactiveText: manual,
	})
	if manualResp.Code != http.StatusCreated {
		t.Fatalf("manual status=%d body=%s", manualResp.Code, manualResp.Body.String())
	}
	if got := decodeEvent(t, manualResp).ProactiveText; got != manual {
		t.Fatalf("manual proactiveText changed: %q", got)
	}

	autoResp := doJSON(t, handler, http.MethodPost, "/api/matches/api-eval/events?token=eval-token", matchstate.MatchEvent{
		EventType:   "penalty",
		Clock:       "44:00",
		TeamID:      "away",
		TeamName:    "Germany",
		PlayerName:  "Musiala",
		Score:       matchstate.Score{Home: 1, Away: 0},
		Description: "Penalty awarded.",
	})
	if autoResp.Code != http.StatusCreated {
		t.Fatalf("auto status=%d body=%s", autoResp.Code, autoResp.Body.String())
	}
	autoEvent := decodeEvent(t, autoResp)
	if strings.TrimSpace(autoEvent.ProactiveText) == "" || autoEvent.ProactiveText == "__quiet__" {
		t.Fatalf("auto fallback should produce proactiveText, got %q", autoEvent.ProactiveText)
	}
}

func TestEvalMatchAPIBoundariesAndCorrection(t *testing.T) {
	store := matchstate.NewStore()
	traces := companion.NewStoreMemoryTools(store)
	cfg := &config.Config{AppToken: "eval-token"}
	handler := handleMatchAPI(store, traces, traces, cfg, nil, pipeline.NewPromptManager())

	unauth := doJSON(t, handler, http.MethodPost, "/api/matches/api-boundary/events", matchstate.MatchEvent{
		EventType:   "goal",
		Clock:       "01:00",
		Description: "No token.",
	})
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized, got %d", unauth.Code)
	}

	invalid := doJSON(t, handler, http.MethodPost, "/api/matches/api-boundary/events?token=eval-token", matchstate.MatchEvent{
		EventType:   "unsupported",
		Clock:       "01:00",
		Description: "Bad event type.",
	})
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("expected bad request for unsupported event, got %d body=%s", invalid.Code, invalid.Body.String())
	}

	createdResp := doJSON(t, handler, http.MethodPost, "/api/matches/api-boundary/events?token=eval-token", matchstate.MatchEvent{
		EventType:   "goal",
		Clock:       "12:00",
		TeamID:      "home",
		TeamName:    "Spain",
		PlayerName:  "Pedri",
		Score:       matchstate.Score{Home: 1, Away: 0},
		Description: "Goal awarded.",
	})
	if createdResp.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createdResp.Code, createdResp.Body.String())
	}
	created := decodeEvent(t, createdResp)

	correctResp := doJSON(t, handler, http.MethodPost, "/api/matches/api-boundary/events/"+created.ID+"/correct?token=eval-token", matchstate.MatchEvent{
		EventType:   "var_check",
		Clock:       "13:00",
		TeamID:      "home",
		TeamName:    "Spain",
		PlayerName:  "Pedri",
		Score:       matchstate.Score{Home: 0, Away: 0},
		Description: "VAR overturns the goal.",
	})
	if correctResp.Code != http.StatusOK {
		t.Fatalf("correct status=%d body=%s", correctResp.Code, correctResp.Body.String())
	}
	replacement := decodeEvent(t, correctResp)
	if replacement.RevisionOf != created.ID || replacement.EventType != "var_check" {
		t.Fatalf("wrong replacement event: %+v", replacement)
	}
}

func TestEvalTraceAPIListDetailAndAuth(t *testing.T) {
	store := matchstate.NewStore()
	traces := companion.NewStoreMemoryTools(store)
	agent := companion.NewAgent(traces)
	cfg := &config.Config{AppToken: "eval-token"}
	handler := handleMatchAPI(store, traces, traces, cfg, nil, pipeline.NewPromptManager())

	matchID := "trace-eval"
	if _, _, err := store.SetConfig(matchID, matchstate.MatchConfig{HomeTeam: "Spain", AwayTeam: "Germany"}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}
	goal, _, err := store.Create(matchID, matchstate.MatchEvent{
		EventType:   "goal",
		Clock:       "23:41",
		TeamID:      "home",
		TeamName:    "Spain",
		Score:       matchstate.Score{Home: 1, Away: 0},
		Description: "Pedri scores.",
		Participants: []matchstate.Participant{
			{Role: "scorer", Name: "Pedri"},
			{Role: "assist", Name: "Fabian"},
		},
	})
	if err != nil {
		t.Fatalf("Create goal error: %v", err)
	}
	reply, err := agent.HandleMessage(contextless(), companion.MessageRequest{
		MatchID: matchID,
		UserID:  "user-1",
		Text:    "刚才谁助攻？",
		Now:     time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("HandleMessage error: %v", err)
	}
	if !strings.Contains(strings.Join(reply.Trace.RetrievedEvent, ","), goal.ID) {
		t.Fatalf("trace did not retrieve goal id: %+v", reply.Trace)
	}

	unauth := doJSON(t, handler, http.MethodGet, "/api/matches/"+matchID+"/traces", nil)
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized trace list, got %d", unauth.Code)
	}

	listResp := doJSON(t, handler, http.MethodGet, "/api/matches/"+matchID+"/traces?token=eval-token", nil)
	if listResp.Code != http.StatusOK {
		t.Fatalf("trace list status=%d body=%s", listResp.Code, listResp.Body.String())
	}
	var listEnvelope struct {
		Traces []companion.Trace `json:"traces"`
	}
	if err := json.Unmarshal(listResp.Body.Bytes(), &listEnvelope); err != nil {
		t.Fatalf("decode trace list: %v body=%s", err, listResp.Body.String())
	}
	if len(listEnvelope.Traces) != 1 {
		t.Fatalf("expected one trace, got %+v", listEnvelope.Traces)
	}
	if listEnvelope.Traces[0].Intent != companion.IntentRecentEvent || len(listEnvelope.Traces[0].ToolCalls) == 0 {
		t.Fatalf("trace list missing intent/tool calls: %+v", listEnvelope.Traces[0])
	}

	detailResp := doJSON(t, handler, http.MethodGet, "/api/matches/"+matchID+"/traces/"+reply.Trace.ID+"?token=eval-token", nil)
	if detailResp.Code != http.StatusOK {
		t.Fatalf("trace detail status=%d body=%s", detailResp.Code, detailResp.Body.String())
	}
	var detailEnvelope struct {
		Trace companion.Trace `json:"trace"`
	}
	if err := json.Unmarshal(detailResp.Body.Bytes(), &detailEnvelope); err != nil {
		t.Fatalf("decode trace detail: %v body=%s", err, detailResp.Body.String())
	}
	if detailEnvelope.Trace.ID != reply.Trace.ID || len(detailEnvelope.Trace.RetrievedEvent) == 0 {
		t.Fatalf("trace detail mismatch: %+v", detailEnvelope.Trace)
	}
}

func TestDemoResetEndpointIsTokenGuardedAndLimitedToDemoMatches(t *testing.T) {
	store := matchstate.NewStore()
	traces := companion.NewStoreMemoryTools(store)
	cfg := &config.Config{AppToken: "eval-token"}
	handler := handleMatchAPI(store, traces, traces, cfg, nil, pipeline.NewPromptManager())

	if _, _, err := store.SetConfig("test", matchstate.MatchConfig{HomeTeam: "西班牙", AwayTeam: "德国"}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}
	if _, _, err := store.Create("test", matchstate.MatchEvent{
		EventType:   "goal",
		Clock:       "24:10",
		TeamID:      "home",
		TeamName:    "西班牙",
		PlayerName:  "佩德里",
		Score:       matchstate.Score{Home: 1, Away: 0},
		Description: "佩德里：禁区内抢点破门。",
	}); err != nil {
		t.Fatalf("Create event error: %v", err)
	}
	agent := companion.NewAgent(traces)
	if _, err := agent.HandleMessage(contextless(), companion.MessageRequest{
		MatchID: "test",
		UserID:  "user-1",
		Text:    "刚才谁助攻？",
		Now:     time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("HandleMessage error: %v", err)
	}

	unauth := doJSON(t, handler, http.MethodPost, "/api/matches/test/reset", nil)
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized reset, got %d", unauth.Code)
	}

	disallowed := doJSON(t, handler, http.MethodPost, "/api/matches/real-match/reset?token=eval-token", nil)
	if disallowed.Code != http.StatusBadRequest {
		t.Fatalf("expected reset to reject non-demo match, got %d", disallowed.Code)
	}

	reset := doJSON(t, handler, http.MethodPost, "/api/matches/test/reset?token=eval-token", nil)
	if reset.Code != http.StatusOK {
		t.Fatalf("reset status=%d body=%s", reset.Code, reset.Body.String())
	}
	if got := store.Events("test"); len(got) != 0 {
		t.Fatalf("expected reset to clear events, got %+v", got)
	}
	traceList, err := traces.ListTraces(contextless(), "test", 10)
	if err != nil {
		t.Fatalf("ListTraces error: %v", err)
	}
	if len(traceList) != 0 {
		t.Fatalf("expected reset to clear traces, got %+v", traceList)
	}
	turns, err := traces.RecentTurns(contextless(), "test", "user-1", 10)
	if err != nil {
		t.Fatalf("RecentTurns error: %v", err)
	}
	if len(turns) != 0 {
		t.Fatalf("expected reset to clear turns, got %+v", turns)
	}
	if got := store.Snapshot("test"); got.HomeTeam != "主队" || got.Score.Home != 0 || got.Score.Away != 0 {
		t.Fatalf("expected reset snapshot defaults, got %+v", got)
	}
}

func doJSON(t *testing.T, handler http.HandlerFunc, method, target string, body any) *httptest.ResponseRecorder {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(method, target, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func decodeEvent(t *testing.T, rr *httptest.ResponseRecorder) matchstate.MatchEvent {
	t.Helper()
	var envelope struct {
		Event matchstate.MatchEvent `json:"event"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode event response: %v body=%s", err, rr.Body.String())
	}
	return envelope.Event
}

func contextless() context.Context {
	return context.Background()
}

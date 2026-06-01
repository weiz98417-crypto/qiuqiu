package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"qiuqiu/internal/config"
	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/pipeline"
)

func TestEvalMatchAPIConfigQuietManualAndAutoFallback(t *testing.T) {
	store := matchstate.NewStore()
	cfg := &config.Config{AppToken: "eval-token"}
	promptMgr := pipeline.NewPromptManager()
	handler := handleMatchAPI(store, cfg, nil, promptMgr)

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
	cfg := &config.Config{AppToken: "eval-token"}
	handler := handleMatchAPI(store, cfg, nil, pipeline.NewPromptManager())

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

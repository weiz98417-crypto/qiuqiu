package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"qiuqiu/internal/companion"
	"qiuqiu/internal/config"
	"qiuqiu/internal/datasource"
	"qiuqiu/internal/directordraft"
	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/observation"
	"qiuqiu/internal/pipeline"
)

var testIdempotencyCounter atomic.Uint64

type fixedDirectorExtractor struct {
	extraction directordraft.Extraction
}

func (extractor fixedDirectorExtractor) Extract(context.Context, string, directordraft.MatchContext) (directordraft.Extraction, error) {
	return extractor.extraction, nil
}

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
	if !hasEventTag(quietEvent, "proactive=quiet") {
		t.Fatalf("quiet event missing mode tag: %+v", quietEvent.Tags)
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
	if manualEvent := decodeEvent(t, manualResp); !hasEventTag(manualEvent, "proactive=manual") {
		t.Fatalf("manual event missing mode tag: %+v", manualEvent.Tags)
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
	if !hasEventTag(autoEvent, "proactive=auto") {
		t.Fatalf("auto event missing mode tag: %+v", autoEvent.Tags)
	}
}

func TestAutoGoalProactiveTextAnchorsConfirmedScorerAndScore(t *testing.T) {
	store := matchstate.NewStore()
	traces := companion.NewStoreMemoryTools(store)
	handler := handleMatchAPI(store, traces, traces, &config.Config{AppToken: "eval-token"}, nil, pipeline.NewPromptManager())

	response := doJSON(t, handler, http.MethodPost, "/api/matches/goal-proactive/events?token=eval-token", matchstate.MatchEvent{
		EventType:    "goal",
		Period:       "first_half",
		Clock:        "45:18",
		TeamID:       "home",
		TeamName:     "西班牙",
		PlayerName:   "佩德里",
		Participants: []matchstate.Participant{{Role: "scorer", Name: "佩德里", TeamID: "home", TeamName: "西班牙"}},
		Score:        matchstate.Score{Home: 1, Away: 0},
		Description:  "佩德里进球了。",
		Confirmed:    true,
		FactStatus:   matchstate.FactStatusConfirmed,
	})
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	event := decodeEvent(t, response)
	if !strings.Contains(event.ProactiveText, "佩德里") {
		t.Fatalf("proactiveText = %q, want confirmed scorer", event.ProactiveText)
	}
	if !strings.Contains(event.ProactiveText, "1比0") {
		t.Fatalf("proactiveText = %q, want current score", event.ProactiveText)
	}
}

func TestPublicMatchAPIHidesProvisionalFactsFromUsers(t *testing.T) {
	store := matchstate.NewStore()
	traces := companion.NewStoreMemoryTools(store)
	cfg := &config.Config{AppToken: "eval-token"}
	handler := handleMatchAPI(store, traces, traces, cfg, nil, pipeline.NewPromptManager())

	created := doJSON(t, handler, http.MethodPost, "/api/matches/public-api/events?token=eval-token", matchstate.MatchEvent{
		Source:      "api-sports",
		EventType:   "goal",
		Period:      "first_half",
		Clock:       "12:00",
		TeamID:      "home",
		PlayerName:  "Saka",
		Score:       matchstate.Score{Home: 1},
		Description: "Saka scored",
		Visibility:  "public",
		Confirmed:   false,
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	var createdEnvelope struct {
		Event    matchstate.MatchEvent `json:"event"`
		Snapshot matchstate.Snapshot   `json:"snapshot"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createdEnvelope); err != nil {
		t.Fatalf("decode provisional create response: %v", err)
	}
	if createdEnvelope.Event.FactStatus != matchstate.FactStatusProvisional || createdEnvelope.Snapshot.Score != (matchstate.Score{}) || len(createdEnvelope.Snapshot.RecentEvents) != 0 {
		t.Fatalf("provisional create response exposed private state: %+v", createdEnvelope)
	}

	state := doJSON(t, handler, http.MethodGet, "/api/matches/public-api/state", nil)
	var stateEnvelope struct {
		Snapshot matchstate.Snapshot `json:"snapshot"`
	}
	if err := json.Unmarshal(state.Body.Bytes(), &stateEnvelope); err != nil {
		t.Fatalf("decode public state: %v", err)
	}
	if stateEnvelope.Snapshot.Score != (matchstate.Score{}) || len(stateEnvelope.Snapshot.RecentEvents) != 0 {
		t.Fatalf("public state exposed provisional fact: %+v", stateEnvelope.Snapshot)
	}

	publicEvents := doJSON(t, handler, http.MethodGet, "/api/matches/public-api/events", nil)
	var publicEnvelope struct {
		Events []matchstate.MatchEvent `json:"events"`
	}
	if err := json.Unmarshal(publicEvents.Body.Bytes(), &publicEnvelope); err != nil {
		t.Fatalf("decode public events: %v", err)
	}
	if len(publicEnvelope.Events) != 0 {
		t.Fatalf("public events exposed provisional fact: %+v", publicEnvelope.Events)
	}

	operatorEvents := doJSON(t, handler, http.MethodGet, "/api/matches/public-api/events?token=eval-token", nil)
	var operatorEnvelope struct {
		Events []matchstate.MatchEvent `json:"events"`
	}
	if err := json.Unmarshal(operatorEvents.Body.Bytes(), &operatorEnvelope); err != nil {
		t.Fatalf("decode operator events: %v", err)
	}
	if len(operatorEnvelope.Events) != 1 || operatorEnvelope.Events[0].FactStatus != matchstate.FactStatusProvisional {
		t.Fatalf("operator ledger events = %+v", operatorEnvelope.Events)
	}
}

func TestMatchFactRetractedMessageCarriesFactIdentity(t *testing.T) {
	message := matchFactRetractedMessage(matchstate.MatchEvent{
		ID:     "event-123",
		FactID: "fact-456",
	})
	if got := message["type"]; got != "match_fact_retracted" {
		t.Fatalf("type = %v", got)
	}
	data, ok := message["data"].(map[string]string)
	if !ok || data["eventId"] != "event-123" || data["factId"] != "fact-456" {
		t.Fatalf("retraction data = %#v", message["data"])
	}
}

func TestMatchClockAPIKeepsEventTimeIndependentAndRejectsStaleWrites(t *testing.T) {
	store := matchstate.NewStore()
	traces := companion.NewStoreMemoryTools(store)
	handler := handleMatchAPI(store, traces, traces, &config.Config{AppToken: "eval-token"}, nil, pipeline.NewPromptManager())

	initial := doJSON(t, handler, http.MethodGet, "/api/matches/clock-api/clock", nil)
	if initial.Code != http.StatusOK {
		t.Fatalf("initial clock status=%d body=%s", initial.Code, initial.Body.String())
	}

	unauthorized := doJSON(t, handler, http.MethodPatch, "/api/matches/clock-api/clock", matchstate.ClockCommand{
		Action: matchstate.ClockActionSet, Period: "first_half", ElapsedSeconds: testIntPointer(600), ExpectedVersion: 0,
	})
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d body=%s", unauthorized.Code, unauthorized.Body.String())
	}

	updated := doJSON(t, handler, http.MethodPatch, "/api/matches/clock-api/clock?token=eval-token", matchstate.ClockCommand{
		Action: matchstate.ClockActionSet, Period: "first_half", ElapsedSeconds: testIntPointer(600), ExpectedVersion: 0,
	})
	if updated.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", updated.Code, updated.Body.String())
	}

	event := doJSON(t, handler, http.MethodPost, "/api/matches/clock-api/events?token=eval-token", matchstate.MatchEvent{
		EventType: "shot", Period: "first_half", Clock: "09:30", Description: "delayed report",
	})
	if event.Code != http.StatusCreated {
		t.Fatalf("event status=%d body=%s", event.Code, event.Body.String())
	}
	state := doJSON(t, handler, http.MethodGet, "/api/matches/clock-api/state", nil)
	var envelope struct {
		Snapshot matchstate.Snapshot `json:"snapshot"`
	}
	if err := json.Unmarshal(state.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	if envelope.Snapshot.Clock != "10:00" || envelope.Snapshot.RecentEvents[0].Clock != "09:30" {
		t.Fatalf("clock/event time boundary = %+v", envelope.Snapshot)
	}

	stale := doJSON(t, handler, http.MethodPatch, "/api/matches/clock-api/clock?token=eval-token", matchstate.ClockCommand{
		Action: matchstate.ClockActionAdjust, DeltaSeconds: testIntPointer(10), ExpectedVersion: 0,
	})
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale status=%d body=%s", stale.Code, stale.Body.String())
	}
}

func TestDirectorVoiceDraftAPITranscribesThenPublishesConfirmedFact(t *testing.T) {
	store := matchstate.NewStore()
	if _, _, err := store.SetConfig("voice-draft-api", matchstate.MatchConfig{
		HomeTeam: "西班牙", AwayTeam: "德国",
		AwayPlayers: []matchstate.Player{{Name: "菲尔克鲁格", Lineup: "bench"}, {Name: "哈弗茨", Lineup: "starter"}},
	}); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	if _, err := store.SetClock("voice-draft-api", matchstate.ClockCommand{
		Action: matchstate.ClockActionSet, Period: "second_half", ElapsedSeconds: testIntPointer(67 * 60), ExpectedVersion: 0,
	}); err != nil {
		t.Fatalf("SetClock: %v", err)
	}
	traces := companion.NewStoreMemoryTools(store)
	drafts := directordraft.NewService(nil, fixedDirectorExtractor{extraction: directordraft.Extraction{
		Team: "德国", EventType: "substitution", Description: "德国换人，菲尔克鲁格换下哈弗茨。",
		Participants: []directordraft.ExtractedParticipant{{Role: "sub_on", Name: "菲尔克鲁格"}, {Role: "sub_off", Name: "哈弗茨"}},
	}})
	handler := handleMatchAPIWithDirectorDraft(store, traces, traces, &config.Config{AppToken: "eval-token"}, nil, pipeline.NewPromptManager(), nil, drafts)

	response := doJSON(t, handler, http.MethodPost, "/api/matches/voice-draft-api/drafts/voice?token=eval-token", directordraft.Request{
		Text: "德国换人，菲尔克鲁格换下哈弗茨",
	})
	if response.Code != http.StatusOK {
		t.Fatalf("voice draft status=%d body=%s", response.Code, response.Body.String())
	}
	var result directordraft.Transcription
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode voice transcription: %v", err)
	}
	if result.Transcript != "德国换人，菲尔克鲁格换下哈弗茨" {
		t.Fatalf("voice transcription = %+v", result)
	}
	if events := store.Events("voice-draft-api"); len(events) != 0 {
		t.Fatalf("voice transcription created public facts: %+v", events)
	}

	published := doJSON(t, handler, http.MethodPost, "/api/matches/voice-draft-api/drafts/voice/publish?token=eval-token", directordraft.Request{
		Text: "德国换人，菲尔克鲁格换下哈弗茨", OccurredPeriod: "second_half", OccurredSeconds: testIntPointer(66 * 60), CapturedClockVersion: testInt64Pointer(1),
	})
	if published.Code != http.StatusCreated {
		t.Fatalf("voice publish status=%d body=%s", published.Code, published.Body.String())
	}
	if events := store.Events("voice-draft-api"); len(events) != 1 || events[0].EventType != "substitution" || events[0].Clock != "66:00" {
		t.Fatalf("voice publish events: %+v", events)
	}
}

func TestVoicePublishGoalUsesCapturedTimeAndUpdatesScore(t *testing.T) {
	event, err := eventFromVoiceDraft(directordraft.Result{
		Ready: true,
		Draft: directordraft.Draft{
			EventType: "goal", TeamID: "home", TeamName: "Spain", Description: "Fabian Ruiz scores.",
			OccurredPeriod: "first_half", OccurredSeconds: 45*60 + 18, CapturedClockVersion: 12,
			Participants: []directordraft.DraftParticipant{{Role: "scorer", Name: "Fabian Ruiz", TeamID: "home", TeamName: "Spain", Resolved: true}},
		},
	}, matchstate.Score{Home: 0, Away: 0})
	if err != nil {
		t.Fatalf("eventFromVoiceDraft: %v", err)
	}
	if event.Clock != "45:18" || event.Score != (matchstate.Score{Home: 1, Away: 0}) || event.PlayerName != "Fabian Ruiz" {
		t.Fatalf("voice goal event = %+v", event)
	}
}

func TestOperatorWritesRequireAndReplayIdempotencyKey(t *testing.T) {
	store := matchstate.NewStore()
	traces := companion.NewStoreMemoryTools(store)
	handler := handleMatchAPI(store, traces, traces, &config.Config{AppToken: "eval-token"}, nil, pipeline.NewPromptManager())
	payload := []byte(`{"eventType":"shot","period":"first_half","clock":"10:00","score":{"home":0,"away":0},"description":"shot"}`)

	missing := httptest.NewRequest(http.MethodPost, "/api/matches/idempotent/events", bytes.NewReader(payload))
	missing.Header.Set("Authorization", "Bearer eval-token")
	missing.Header.Set("Content-Type", "application/json")
	missingResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingResponse, missing)
	if missingResponse.Code != http.StatusBadRequest {
		t.Fatalf("missing key status=%d body=%s", missingResponse.Code, missingResponse.Body.String())
	}

	request := func(body []byte) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/matches/idempotent/events", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer eval-token")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "event-key-1")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	first := request(payload)
	second := request(payload)
	if first.Code != http.StatusCreated || second.Code != http.StatusCreated {
		t.Fatalf("replay statuses=%d,%d bodies=%s / %s", first.Code, second.Code, first.Body.String(), second.Body.String())
	}
	if second.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatalf("replay header = %q", second.Header().Get("Idempotency-Replayed"))
	}
	if got := len(store.Events("idempotent")); got != 1 {
		t.Fatalf("stored events = %d, want 1", got)
	}
	conflict := request([]byte(`{"eventType":"shot","period":"first_half","clock":"11:00","score":{"home":0,"away":0},"description":"different"}`))
	if conflict.Code != http.StatusConflict {
		t.Fatalf("conflict status=%d body=%s", conflict.Code, conflict.Body.String())
	}
}

func TestManualMatchAPIEventsDefaultToConfirmedFacts(t *testing.T) {
	store := matchstate.NewStore()
	traces := companion.NewStoreMemoryTools(store)
	cfg := &config.Config{AppToken: "eval-token"}
	handler := handleMatchAPI(store, traces, traces, cfg, nil, pipeline.NewPromptManager())

	created := doJSON(t, handler, http.MethodPost, "/api/matches/manual-default/events?token=eval-token", matchstate.MatchEvent{
		Source:      "operator",
		EventType:   "shot",
		Period:      "first_half",
		Clock:       "05:00",
		Score:       matchstate.Score{},
		Description: "Manual shot note",
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	event := decodeEvent(t, created)
	if event.FactStatus != matchstate.FactStatusConfirmed || !event.Confirmed {
		t.Fatalf("manual event fact status = %+v", event)
	}
	public := doJSON(t, handler, http.MethodGet, "/api/matches/manual-default/events", nil)
	var envelope struct {
		Events []matchstate.MatchEvent `json:"events"`
	}
	if err := json.Unmarshal(public.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode public events: %v", err)
	}
	if len(envelope.Events) != 1 || envelope.Events[0].FactID != event.FactID {
		t.Fatalf("manual public events = %+v", envelope.Events)
	}
}

func TestFactConfirmationAndRevocationAreOperatorBound(t *testing.T) {
	store := matchstate.NewStore()
	traces := companion.NewStoreMemoryTools(store)
	cfg := &config.Config{AppToken: "eval-token"}
	handler := handleMatchAPI(store, traces, traces, cfg, nil, pipeline.NewPromptManager())

	created := doJSON(t, handler, http.MethodPost, "/api/matches/fact-api/events?token=eval-token", matchstate.MatchEvent{
		Source:      "api-sports",
		EventType:   "goal",
		Period:      "first_half",
		Clock:       "12:00",
		TeamID:      "home",
		PlayerName:  "Saka",
		Score:       matchstate.Score{Home: 1},
		Description: "Saka scored",
		Visibility:  "public",
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	event := decodeEvent(t, created)

	unauthorized := doJSON(t, handler, http.MethodPost, "/api/matches/fact-api/facts/"+event.FactID+"/confirm", nil)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized confirmation status=%d", unauthorized.Code)
	}
	confirmed := doJSON(t, handler, http.MethodPost, "/api/matches/fact-api/facts/"+event.FactID+"/confirm?token=eval-token", nil)
	if confirmed.Code != http.StatusOK {
		t.Fatalf("confirm status=%d body=%s", confirmed.Code, confirmed.Body.String())
	}
	confirmedEvent := decodeEvent(t, confirmed)
	if confirmedEvent.FactStatus != matchstate.FactStatusConfirmed || !confirmedEvent.Confirmed {
		t.Fatalf("confirmed event = %+v", confirmedEvent)
	}

	revoked := doJSON(t, handler, http.MethodPost, "/api/matches/fact-api/facts/"+event.FactID+"/revoke?token=eval-token", nil)
	if revoked.Code != http.StatusOK {
		t.Fatalf("revoke status=%d body=%s", revoked.Code, revoked.Body.String())
	}
	revokedEvent := decodeEvent(t, revoked)
	if revokedEvent.FactStatus != matchstate.FactStatusRevoked || revokedEvent.Confirmed {
		t.Fatalf("revoked event = %+v", revokedEvent)
	}
}

func TestFactConflictResolutionIsOperatorBoundAndExplicit(t *testing.T) {
	store := matchstate.NewStore()
	traces := companion.NewStoreMemoryTools(store)
	handler := handleMatchAPI(store, traces, traces, &config.Config{AppToken: "eval-token"}, nil, pipeline.NewPromptManager())
	matchID := "formal-conflict-api"
	forgedReported := matchstate.Score{Home: 7, Away: 6}
	forgedEffective := matchstate.Score{Home: 9, Away: 8}

	acceptedResponse := doJSON(t, handler, http.MethodPost, "/api/matches/"+matchID+"/events?token=eval-token", matchstate.MatchEvent{
		Source: "operator", EventType: "goal", Period: "first_half", Clock: "12:00", TeamID: "home",
		Score: matchstate.Score{Home: 1}, ReportedScore: &forgedReported, EffectiveScoreAfter: &forgedEffective,
		Description: "人工记录主队进球。",
	})
	if acceptedResponse.Code != http.StatusCreated {
		t.Fatalf("accepted status=%d body=%s", acceptedResponse.Code, acceptedResponse.Body.String())
	}
	accepted := decodeEvent(t, acceptedResponse)
	conflictResponse := doJSON(t, handler, http.MethodPost, "/api/matches/"+matchID+"/events?token=eval-token", matchstate.MatchEvent{
		Source: "api-sports", ProviderEventID: "formal-conflict-api", EventType: "goal", Period: "first_half", Clock: "12:10", TeamID: "away",
		Score: matchstate.Score{Away: 1}, ReportedScore: &forgedReported, EffectiveScoreAfter: &forgedEffective,
		Description: "外部源记录客队进球。",
	})
	if conflictResponse.Code != http.StatusConflict {
		t.Fatalf("conflict status=%d body=%s", conflictResponse.Code, conflictResponse.Body.String())
	}

	publicEvents := doJSON(t, handler, http.MethodGet, "/api/matches/"+matchID+"/events", nil)
	var publicPayload map[string]json.RawMessage
	if err := json.Unmarshal(publicEvents.Body.Bytes(), &publicPayload); err != nil {
		t.Fatalf("decode public events: %v", err)
	}
	if _, exposed := publicPayload["conflicts"]; exposed {
		t.Fatalf("public events exposed operator conflicts: %s", publicEvents.Body.String())
	}

	operatorEvents := doJSON(t, handler, http.MethodGet, "/api/matches/"+matchID+"/events?token=eval-token", nil)
	var operatorPayload struct {
		Events    []matchstate.MatchEvent   `json:"events"`
		Conflicts []matchstate.FactConflict `json:"conflicts"`
	}
	if err := json.Unmarshal(operatorEvents.Body.Bytes(), &operatorPayload); err != nil {
		t.Fatalf("decode operator events: %v", err)
	}
	if len(operatorPayload.Conflicts) != 1 || operatorPayload.Conflicts[0].Status != matchstate.ConflictStatusOpen {
		t.Fatalf("operator conflicts = %+v", operatorPayload.Conflicts)
	}
	var operatorEventPayload struct {
		Events []struct {
			FactID              string                `json:"factId"`
			FactStatus          matchstate.FactStatus `json:"factStatus"`
			Score               matchstate.Score      `json:"score"`
			ReportedScore       *matchstate.Score     `json:"reportedScore"`
			EffectiveScoreAfter *matchstate.Score     `json:"effectiveScoreAfter"`
		} `json:"events"`
	}
	if err := json.Unmarshal(operatorEvents.Body.Bytes(), &operatorEventPayload); err != nil {
		t.Fatalf("decode operator event scores: %v", err)
	}
	for _, event := range operatorEventPayload.Events {
		if event.ReportedScore == nil {
			t.Fatalf("operator event omitted reported score: %+v", event)
		}
		if event.FactStatus == matchstate.FactStatusConfirmed && (event.EffectiveScoreAfter == nil || *event.EffectiveScoreAfter != (matchstate.Score{Home: 1})) {
			t.Fatalf("accepted event effective score = %+v", event)
		}
		if event.FactStatus == matchstate.FactStatusConflict && event.EffectiveScoreAfter != nil {
			t.Fatalf("conflict candidate should not have an effective score: %+v", event)
		}
	}

	publicState := doJSON(t, handler, http.MethodGet, "/api/matches/"+matchID+"/state", nil)
	var publicStatePayload struct {
		Snapshot matchstate.Snapshot `json:"snapshot"`
	}
	if err := json.Unmarshal(publicState.Body.Bytes(), &publicStatePayload); err != nil {
		t.Fatalf("decode public state: %v", err)
	}
	if publicStatePayload.Snapshot.Integrity.Status == "conflict" || publicStatePayload.Snapshot.Integrity.Reason != "" || publicStatePayload.Snapshot.Integrity.DetectedAt != "" {
		t.Fatalf("public state exposed internal conflict details: %+v", publicStatePayload.Snapshot.Integrity)
	}
	operatorState := doJSON(t, handler, http.MethodGet, "/api/matches/"+matchID+"/state?token=eval-token", nil)
	var operatorStatePayload struct {
		Snapshot matchstate.Snapshot `json:"snapshot"`
	}
	if err := json.Unmarshal(operatorState.Body.Bytes(), &operatorStatePayload); err != nil {
		t.Fatalf("decode operator state: %v", err)
	}
	if operatorStatePayload.Snapshot.Integrity.Status != "conflict" || operatorStatePayload.Snapshot.Integrity.Reason == "" {
		t.Fatalf("operator state omitted conflict audit details: %+v", operatorStatePayload.Snapshot.Integrity)
	}
	conflict := operatorPayload.Conflicts[0]
	var candidateFactID string
	for _, member := range conflict.Members {
		if member.Role == matchstate.ConflictMemberCandidate {
			candidateFactID = member.FactID
		}
	}
	if candidateFactID == "" {
		t.Fatalf("candidate missing: %+v", conflict)
	}

	unauthorized := doJSON(t, handler, http.MethodPost, "/api/matches/"+matchID+"/conflicts/"+conflict.ID+"/resolve", map[string]string{
		"chosenFactId": accepted.FactID,
		"reason":       "保留人工记录",
	})
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized resolution status=%d body=%s", unauthorized.Code, unauthorized.Body.String())
	}
	resolved := doJSON(t, handler, http.MethodPost, "/api/matches/"+matchID+"/conflicts/"+conflict.ID+"/resolve?token=eval-token", map[string]string{
		"chosenFactId": accepted.FactID,
		"reason":       "保留人工记录",
	})
	if resolved.Code != http.StatusOK {
		t.Fatalf("resolve status=%d body=%s", resolved.Code, resolved.Body.String())
	}
	var resolvedPayload struct {
		Conflict matchstate.FactConflict `json:"conflict"`
		Event    matchstate.MatchEvent   `json:"event"`
		Snapshot matchstate.Snapshot     `json:"snapshot"`
	}
	if err := json.Unmarshal(resolved.Body.Bytes(), &resolvedPayload); err != nil {
		t.Fatalf("decode resolution: %v", err)
	}
	if resolvedPayload.Conflict.Status != matchstate.ConflictStatusResolved || resolvedPayload.Conflict.ChosenFactID != accepted.FactID {
		t.Fatalf("resolved conflict = %+v", resolvedPayload.Conflict)
	}
	if resolvedPayload.Event.FactID != accepted.FactID || resolvedPayload.Snapshot.Score != (matchstate.Score{Home: 1}) || resolvedPayload.Snapshot.Integrity.Status != "ok" {
		t.Fatalf("resolution response = %+v", resolvedPayload)
	}
}

func TestFactConflictResolutionAcceptsCompatibleFactSelection(t *testing.T) {
	store := matchstate.NewStore()
	traces := companion.NewStoreMemoryTools(store)
	handler := handleMatchAPI(store, traces, traces, &config.Config{AppToken: "eval-token"}, nil, pipeline.NewPromptManager())
	matchID := "compatible-conflict-selection-api"

	firstResponse := doJSON(t, handler, http.MethodPost, "/api/matches/"+matchID+"/events?token=eval-token", matchstate.MatchEvent{
		Source: "operator", EventType: "goal", Period: "first_half", Clock: "10:00", TeamID: "home",
		Score: matchstate.Score{Home: 1}, Description: "first accepted goal",
	})
	if firstResponse.Code != http.StatusCreated {
		t.Fatalf("first goal status=%d body=%s", firstResponse.Code, firstResponse.Body.String())
	}
	first := decodeEvent(t, firstResponse)
	secondResponse := doJSON(t, handler, http.MethodPost, "/api/matches/"+matchID+"/events?token=eval-token", matchstate.MatchEvent{
		Source: "operator", EventType: "goal", Period: "first_half", Clock: "10:30", TeamID: "home",
		Score: matchstate.Score{Home: 2}, Description: "second accepted goal",
	})
	if secondResponse.Code != http.StatusCreated {
		t.Fatalf("second goal status=%d body=%s", secondResponse.Code, secondResponse.Body.String())
	}
	second := decodeEvent(t, secondResponse)
	conflictResponse := doJSON(t, handler, http.MethodPost, "/api/matches/"+matchID+"/events?token=eval-token", matchstate.MatchEvent{
		Source: "provider", ProviderEventID: matchID, EventType: "goal", Period: "first_half", Clock: "10:15", TeamID: "away",
		Score: matchstate.Score{Home: 1, Away: 1}, Description: "bridge candidate",
	})
	if conflictResponse.Code != http.StatusConflict {
		t.Fatalf("conflict status=%d body=%s", conflictResponse.Code, conflictResponse.Body.String())
	}
	conflicts := store.FactConflicts(matchID)
	if len(conflicts) != 1 {
		t.Fatalf("conflicts = %+v", conflicts)
	}
	resolveResponse := doJSON(t, handler, http.MethodPost, "/api/matches/"+matchID+"/conflicts/"+conflicts[0].ID+"/resolve?token=eval-token", map[string]interface{}{
		"selectedFactIds": []string{first.FactID, second.FactID},
		"reason":          "keep both confirmed goals",
	})
	if resolveResponse.Code != http.StatusOK {
		t.Fatalf("resolve status=%d body=%s", resolveResponse.Code, resolveResponse.Body.String())
	}
	var payload struct {
		Conflict matchstate.FactConflict `json:"conflict"`
		Events   []matchstate.MatchEvent `json:"events"`
		Snapshot matchstate.Snapshot     `json:"snapshot"`
	}
	if err := json.Unmarshal(resolveResponse.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode resolution: %v", err)
	}
	if payload.Conflict.Status != matchstate.ConflictStatusResolved || len(payload.Conflict.SelectedFactIDs) != 2 || len(payload.Events) != 0 || payload.Snapshot.Score != (matchstate.Score{Home: 2}) {
		t.Fatalf("resolution payload = %+v", payload)
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

	missingReasonResp := doJSON(t, handler, http.MethodPost, "/api/matches/api-boundary/events/"+created.ID+"/correct?token=eval-token", matchstate.MatchEvent{
		EventType: "var_check", Clock: "13:00", TeamID: "home", TeamName: "Spain", PlayerName: "Pedri",
		Score: matchstate.Score{Home: 0, Away: 0}, Description: "VAR overturns the goal.",
	})
	if missingReasonResp.Code != http.StatusBadRequest {
		t.Fatalf("missing correction reason status=%d body=%s", missingReasonResp.Code, missingReasonResp.Body.String())
	}

	correctResp := doJSON(t, handler, http.MethodPost, "/api/matches/api-boundary/events/"+created.ID+"/correct?token=eval-token", matchstate.MatchEvent{
		EventType:   "var_check",
		Clock:       "13:00",
		TeamID:      "home",
		TeamName:    "Spain",
		PlayerName:  "Pedri",
		Score:       matchstate.Score{Home: 0, Away: 0},
		Description: "VAR overturns the goal.",
		Evidence:    map[string]any{"correctionReason": "VAR review overturned the original record."},
	})
	if correctResp.Code != http.StatusOK {
		t.Fatalf("correct status=%d body=%s", correctResp.Code, correctResp.Body.String())
	}
	replacement := decodeEvent(t, correctResp)
	if replacement.RevisionOf != created.ID || replacement.EventType != "var_check" {
		t.Fatalf("wrong replacement event: %+v", replacement)
	}
}

func TestSourceControlAPIReportsAndSwitchesSources(t *testing.T) {
	store := matchstate.NewStore()
	traces := companion.NewStoreMemoryTools(store)
	cfg := &config.Config{AppToken: "eval-token"}
	sources := datasource.NewManager(context.Background(), store, nil, datasource.ManagerConfig{})
	t.Cleanup(sources.Close)
	handler := handleMatchAPIWithSources(store, traces, traces, cfg, nil, pipeline.NewPromptManager(), sources)

	statusResp := doJSON(t, handler, http.MethodGet, "/api/matches/source-api/sources?token=eval-token", nil)
	if statusResp.Code != http.StatusOK {
		t.Fatalf("sources status=%d body=%s", statusResp.Code, statusResp.Body.String())
	}
	var statusEnvelope struct {
		Status datasource.MatchSourceStatus `json:"status"`
	}
	if err := json.Unmarshal(statusResp.Body.Bytes(), &statusEnvelope); err != nil {
		t.Fatalf("decode source status: %v", err)
	}
	if statusEnvelope.Status.ActiveSource != datasource.SourceManual {
		t.Fatalf("active source = %q, want manual", statusEnvelope.Status.ActiveSource)
	}

	replayResp := doJSON(t, handler, http.MethodPost, "/api/matches/source-api/sources/start?token=eval-token", datasource.SourceConfig{Type: datasource.SourceReplay})
	if replayResp.Code != http.StatusOK {
		t.Fatalf("replay start status=%d body=%s", replayResp.Code, replayResp.Body.String())
	}
	if err := json.Unmarshal(replayResp.Body.Bytes(), &statusEnvelope); err != nil {
		t.Fatalf("decode replay status: %v", err)
	}
	if statusEnvelope.Status.ActiveSource != datasource.SourceReplay {
		t.Fatalf("active source = %q, want replay", statusEnvelope.Status.ActiveSource)
	}

	stopResp := doJSON(t, handler, http.MethodPost, "/api/matches/source-api/sources/stop?token=eval-token", nil)
	if stopResp.Code != http.StatusOK {
		t.Fatalf("source stop status=%d body=%s", stopResp.Code, stopResp.Body.String())
	}
	if err := json.Unmarshal(stopResp.Body.Bytes(), &statusEnvelope); err != nil {
		t.Fatalf("decode stopped status: %v", err)
	}
	if statusEnvelope.Status.ActiveSource != datasource.SourceManual {
		t.Fatalf("active source after stop = %q, want manual", statusEnvelope.Status.ActiveSource)
	}
}

func TestAutomationAPIRequiresAuthAndPersistsPolicy(t *testing.T) {
	store := matchstate.NewStore()
	traces := companion.NewStoreMemoryTools(store)
	cfg := &config.Config{AppToken: "eval-token"}
	handler := handleMatchAPI(store, traces, traces, cfg, nil, pipeline.NewPromptManager())
	path := "/api/matches/automation-api/automation"

	unauthorized := doJSON(t, handler, http.MethodGet, path, nil)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d body=%s", unauthorized.Code, unauthorized.Body.String())
	}

	policy := matchstate.AutomationPolicy{
		Mode:            matchstate.AutomationModePaused,
		EventTypes:      []string{"goal", "red_card"},
		CooldownSeconds: 15,
	}
	update := doJSON(t, handler, http.MethodPost, path+"?token=eval-token", policy)
	if update.Code != http.StatusOK {
		t.Fatalf("automation update status=%d body=%s", update.Code, update.Body.String())
	}

	if configResp := doJSON(t, handler, http.MethodPost, "/api/matches/automation-api/config?token=eval-token", matchstate.MatchConfig{
		HomeTeam: "Spain",
		AwayTeam: "Germany",
	}); configResp.Code != http.StatusOK {
		t.Fatalf("config update status=%d body=%s", configResp.Code, configResp.Body.String())
	}

	get := doJSON(t, handler, http.MethodGet, path+"?token=eval-token", nil)
	if get.Code != http.StatusOK {
		t.Fatalf("automation get status=%d body=%s", get.Code, get.Body.String())
	}
	var envelope struct {
		Policy matchstate.AutomationPolicy `json:"policy"`
	}
	if err := json.Unmarshal(get.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode automation policy: %v", err)
	}
	if envelope.Policy.Mode != matchstate.AutomationModePaused || envelope.Policy.CooldownSeconds != 15 {
		t.Fatalf("unexpected automation policy: %+v", envelope.Policy)
	}
}

func TestManualTakeoverSwitchesSourceAndPausesAutomation(t *testing.T) {
	store := matchstate.NewStore()
	traces := companion.NewStoreMemoryTools(store)
	cfg := &config.Config{AppToken: "eval-token"}
	sources := datasource.NewManager(context.Background(), store, nil, datasource.ManagerConfig{})
	t.Cleanup(sources.Close)
	handler := handleMatchAPIWithSources(store, traces, traces, cfg, nil, pipeline.NewPromptManager(), sources)
	matchID := "manual-takeover"

	if _, err := sources.Start(matchID, datasource.SourceConfig{Type: datasource.SourceReplay}); err != nil {
		t.Fatalf("start replay source: %v", err)
	}
	if _, err := store.SetAutomation(matchID, matchstate.AutomationPolicy{
		Mode:            matchstate.AutomationModeActive,
		EventTypes:      []string{"goal"},
		CooldownSeconds: 8,
	}); err != nil {
		t.Fatalf("set automation: %v", err)
	}

	response := doJSON(t, handler, http.MethodPost, "/api/matches/"+matchID+"/takeover?token=eval-token", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("takeover status=%d body=%s", response.Code, response.Body.String())
	}
	if got := sources.Status(matchID).ActiveSource; got != datasource.SourceManual {
		t.Fatalf("active source after takeover = %q, want manual", got)
	}
	if got := store.Config(matchID).Automation.Mode; got != matchstate.AutomationModePaused {
		t.Fatalf("automation mode after takeover = %q, want paused", got)
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
	observations := observation.NewMemoryCoordinator()
	cfg := &config.Config{AppToken: "eval-token"}
	handler := handleMatchAPI(store, traces, demoStateResetter{traces: traces, observations: observations}, cfg, nil, pipeline.NewPromptManager())

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
	pending, err := observations.Record(contextless(), observation.Input{
		SignalID: "reset-observation", UserID: "user-1", MatchID: "test", Kind: "event", EventType: "goal",
		ReceivedAt: time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Record observation error: %v", err)
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
	emptyEvents := doJSON(t, handler, http.MethodGet, "/api/matches/test/events?token=eval-token", nil)
	if !strings.Contains(emptyEvents.Body.String(), `"events":[]`) {
		t.Fatalf("empty event ledger must use a JSON array, body=%s", emptyEvents.Body.String())
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
	if _, ok := observations.Get(pending.ID); ok {
		t.Fatal("expected reset to clear pending observations")
	}
	if got := store.Snapshot("test"); got.HomeTeam != "主队" || got.Score.Home != 0 || got.Score.Away != 0 {
		t.Fatalf("expected reset snapshot defaults, got %+v", got)
	}
}

func TestStartMatchEndpointResetsRunningStateForAnAuthorizedOperator(t *testing.T) {
	store := matchstate.NewStore()
	traces := companion.NewStoreMemoryTools(store)
	observations := observation.NewMemoryCoordinator()
	cfg := &config.Config{AppToken: "eval-token"}
	handler := handleMatchAPI(store, traces, demoStateResetter{traces: traces, observations: observations}, cfg, nil, pipeline.NewPromptManager())

	const matchID = "real-match"
	if _, _, err := store.SetConfig(matchID, matchstate.MatchConfig{HomeTeam: "Old Home", AwayTeam: "Old Away"}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}
	if _, _, err := store.Create(matchID, matchstate.MatchEvent{
		EventType: "goal", Period: "first_half", Clock: "24:10", TeamID: "home", TeamName: "Old Home",
		Score: matchstate.Score{Home: 1, Away: 0}, Description: "Old Home scored.",
	}); err != nil {
		t.Fatalf("Create event error: %v", err)
	}
	elapsed := 1450
	if _, err := store.SetClock(matchID, matchstate.ClockCommand{
		Action: matchstate.ClockActionSet, Period: "first_half", ElapsedSeconds: &elapsed, ExpectedVersion: 0,
	}); err != nil {
		t.Fatalf("SetClock error: %v", err)
	}

	unauthorized := doJSON(t, handler, http.MethodPost, "/api/matches/real-match/start", matchstate.MatchConfig{})
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized start, got %d", unauthorized.Code)
	}

	started := doJSON(t, handler, http.MethodPost, "/api/matches/real-match/start?token=eval-token", matchstate.MatchConfig{
		HomeTeam: "New Home", AwayTeam: "New Away",
		HomePlayers: startingEleven("Home"), AwayPlayers: startingEleven("Away"),
	})
	if started.Code != http.StatusOK {
		t.Fatalf("start status=%d body=%s", started.Code, started.Body.String())
	}
	if got := store.Config(matchID); got.HomeTeam != "New Home" || got.AwayTeam != "New Away" {
		t.Fatalf("start config = %+v", got)
	}
	if got := store.Events(matchID); len(got) != 0 {
		t.Fatalf("start must clear prior events, got %+v", got)
	}
	if got := store.PublicSnapshot(matchID).Score; got != (matchstate.Score{}) {
		t.Fatalf("start score = %+v, want zero", got)
	}
	if got := store.Clock(matchID); got.Period != "pre_match" || got.ElapsedSeconds != 0 || got.Running {
		t.Fatalf("start clock = %+v", got)
	}
}

func TestStartMatchEndpointRejectsIncompleteLineupsWithoutResettingRunningState(t *testing.T) {
	store := matchstate.NewStore()
	traces := companion.NewStoreMemoryTools(store)
	observations := observation.NewMemoryCoordinator()
	cfg := &config.Config{AppToken: "eval-token"}
	handler := handleMatchAPI(store, traces, demoStateResetter{traces: traces, observations: observations}, cfg, nil, pipeline.NewPromptManager())

	const matchID = "real-match"
	if _, _, err := store.SetConfig(matchID, matchstate.MatchConfig{HomeTeam: "Old Home", AwayTeam: "Old Away"}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}
	if _, _, err := store.Create(matchID, matchstate.MatchEvent{
		EventType: "goal", Period: "first_half", Clock: "24:10", TeamID: "home", TeamName: "Old Home",
		Score: matchstate.Score{Home: 1, Away: 0}, Description: "Old Home scored.",
	}); err != nil {
		t.Fatalf("Create event error: %v", err)
	}
	elapsed := 1450
	if _, err := store.SetClock(matchID, matchstate.ClockCommand{
		Action: matchstate.ClockActionSet, Period: "first_half", ElapsedSeconds: &elapsed, ExpectedVersion: 0,
	}); err != nil {
		t.Fatalf("SetClock error: %v", err)
	}

	started := doJSON(t, handler, http.MethodPost, "/api/matches/real-match/start?token=eval-token", matchstate.MatchConfig{
		HomeTeam: "New Home", AwayTeam: "New Away",
		HomePlayers: []matchstate.Player{{Name: "Home 1"}, {Name: "Home 2"}, {Name: "Home 3"}},
		AwayPlayers: []matchstate.Player{{Name: "Away 1"}, {Name: "Away 2"}},
	})
	if started.Code != http.StatusBadRequest {
		t.Fatalf("start status=%d body=%s, want 400", started.Code, started.Body.String())
	}
	if got := store.Config(matchID); got.HomeTeam != "Old Home" || got.AwayTeam != "Old Away" {
		t.Fatalf("invalid start must preserve config, got %+v", got)
	}
	if got := store.Events(matchID); len(got) != 1 {
		t.Fatalf("invalid start must preserve prior events, got %+v", got)
	}
	if got := store.PublicSnapshot(matchID).Score; got != (matchstate.Score{Home: 1, Away: 0}) {
		t.Fatalf("invalid start must preserve score, got %+v", got)
	}
	if got := store.Clock(matchID); got.Period != "first_half" || got.ElapsedSeconds != elapsed {
		t.Fatalf("invalid start must preserve clock, got %+v", got)
	}
}

func startingEleven(prefix string) []matchstate.Player {
	players := make([]matchstate.Player, 11)
	for index := range players {
		players[index] = matchstate.Player{Name: fmt.Sprintf("%s %d", prefix, index+1), Lineup: "starter"}
	}
	return players
}

func doJSON(t *testing.T, handler http.HandlerFunc, method, target string, body any) *httptest.ResponseRecorder {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(method, target, bytes.NewReader(payload))
	query := req.URL.Query()
	if token := query.Get("token"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
		query.Del("token")
		req.URL.RawQuery = query.Encode()
	}
	req.Header.Set("Content-Type", "application/json")
	if method == http.MethodPost || method == http.MethodPatch {
		req.Header.Set("Idempotency-Key", t.Name()+"-"+strconv.FormatUint(testIdempotencyCounter.Add(1), 10))
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func testIntPointer(value int) *int {
	return &value
}

func testInt64Pointer(value int64) *int64 {
	return &value
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

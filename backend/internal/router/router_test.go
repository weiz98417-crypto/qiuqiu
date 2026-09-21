package router

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func stubConfig(baseURL string) Config {
	return Config{BaseURL: baseURL, APIKey: "stub-key", Model: "mimo-v2.5", Timeout: 2 * time.Second}
}

// toolCallStub answers one forced route_turn call with the given arguments.
func toolCallStub(t *testing.T, arguments string, captured *routeRequestPayload) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if captured != nil {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read request body: %v", err)
			}
			if err := json.Unmarshal(body, captured); err != nil {
				t.Errorf("decode request payload: %v", err)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"tool_calls":[{"function":{"name":"route_turn","arguments":` + quoteJSON(arguments) + `}}]}}]}`))
	}
}

func quoteJSON(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func TestClientDisabledWithoutKey(t *testing.T) {
	client := NewClient(Config{BaseURL: "http://127.0.0.1:1", Model: "mimo-v2.5"})
	if client.Enabled() {
		t.Fatalf("client without api key must report disabled")
	}
	if _, err := client.Route(context.Background(), Request{Text: "你在干嘛"}); err == nil {
		t.Fatalf("route on disabled client must error")
	}
}

func TestRoutePayloadShapeAndParse(t *testing.T) {
	var captured routeRequestPayload
	server := httptest.NewServer(toolCallStub(t, `{"intent":"match_fact_claim","confidence":0.85,"reply":""}`, &captured))
	defer server.Close()

	client := NewClient(stubConfig(server.URL))
	result, err := client.Route(context.Background(), Request{Text: "明明进了，裁判瞎了吗", Context: "当前比赛：西班牙 0-0 德国，上半场"})
	if err != nil {
		t.Fatalf("Route error: %v", err)
	}
	if result.Intent != "match_fact_claim" || result.Confidence < 0.84 || result.Confidence > 0.86 {
		t.Fatalf("unexpected result %+v", result)
	}
	if captured.Model != "mimo-v2.5" {
		t.Fatalf("payload model = %q", captured.Model)
	}
	if len(captured.Messages) != 3 {
		t.Fatalf("payload messages = %d, want system+text+context", len(captured.Messages))
	}
	if captured.Messages[0].Role != "system" || !strings.Contains(captured.Messages[0].Content, "只分类不回答") {
		t.Fatalf("system prompt missing fact discipline: %q", captured.Messages[0].Content)
	}
	if captured.Messages[1].Content != "明明进了，裁判瞎了吗" || captured.Messages[2].Content == "" {
		t.Fatalf("user messages not carried: %+v", captured.Messages)
	}
	if len(captured.Tools) != 1 || captured.Tools[0].Function.Name != RouteTurnToolName {
		t.Fatalf("payload tools = %+v", captured.Tools)
	}
	choice, ok := captured.ToolChoice.(map[string]any)
	if !ok {
		t.Fatalf("tool_choice is not a forced function object: %T", captured.ToolChoice)
	}
	forced, ok := choice["function"].(map[string]any)
	if !ok || forced["name"] != RouteTurnToolName {
		t.Fatalf("tool_choice.function = %+v", choice["function"])
	}
	// Forced enum of the routable backend intents (match_reaction is
	// proactive-only and stays out of the user-turn enum).
	properties := captured.Tools[0].Function.Parameters["properties"].(map[string]any)
	intent := properties["intent"].(map[string]any)
	enum := intent["enum"].([]any)
	if len(enum) != 12 {
		t.Fatalf("intent enum size = %d, want 12 routable intents", len(enum))
	}
}

func TestRouteParsesSlotsAndClampsConfidence(t *testing.T) {
	server := httptest.NewServer(toolCallStub(t, `{"intent":"player_question","player":"穆西亚拉","team":"德国","score":"2-1","confidence":1.5,"reply":"我不评价赛况"}`, nil))
	defer server.Close()

	client := NewClient(stubConfig(server.URL))
	result, err := client.Route(context.Background(), Request{Text: "穆西亚拉怎么样"})
	if err != nil {
		t.Fatalf("Route error: %v", err)
	}
	if result.Player != "穆西亚拉" || result.Team != "德国" || result.Score != "2-1" {
		t.Fatalf("slots lost: %+v", result)
	}
	if result.Confidence != 1 {
		t.Fatalf("confidence not clamped: %v", result.Confidence)
	}
}

func TestRouteTimeoutDegraded(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer server.Close()
	defer close(release)

	client := NewClient(Config{BaseURL: server.URL, APIKey: "stub-key", Model: "mimo-v2.5", Timeout: 50 * time.Millisecond})
	startedAt := time.Now()
	_, err := client.Route(context.Background(), Request{Text: "你在干嘛"})
	if err == nil {
		t.Fatalf("slow router must error")
	}
	if elapsed := time.Since(startedAt); elapsed > 3*time.Second {
		t.Fatalf("router call did not respect its own timeout: %s", elapsed)
	}
}

func TestRouteHTTPErrorDegraded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"upstream boom"}`, http.StatusBadGateway)
	}))
	defer server.Close()

	client := NewClient(stubConfig(server.URL))
	_, err := client.Route(context.Background(), Request{Text: "你在干嘛"})
	if err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("http failure must surface: %v", err)
	}
}

func TestRouteMissingToolCallErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"我直接回答你吧"}}]}`))
	}))
	defer server.Close()

	client := NewClient(stubConfig(server.URL))
	if _, err := client.Route(context.Background(), Request{Text: "你在干嘛"}); err == nil {
		t.Fatalf("missing route_turn tool call must error")
	}
}

func TestRouteEmptyTextRejected(t *testing.T) {
	client := NewClient(stubConfig("http://127.0.0.1:1"))
	if _, err := client.Route(context.Background(), Request{Text: "   "}); err == nil {
		t.Fatalf("empty text must be rejected before any HTTP call")
	}
}

func TestNewConfigFromEnv(t *testing.T) {
	env := map[string]string{
		"ROUTER_BASE_URL":   "http://router.example/v1 ",
		"ROUTER_API_KEY":    "",
		"MIMO_API_KEY":      "shared-key",
		"ROUTER_MODEL":      "",
		"ROUTER_TIMEOUT_MS": "2500",
	}
	config := NewConfig(func(key string) string { return env[key] })
	if config.BaseURL != "http://router.example/v1" || config.APIKey != "shared-key" || config.Model != DefaultModel {
		t.Fatalf("config = %+v", config)
	}
	if config.Timeout != 2500*time.Millisecond {
		t.Fatalf("timeout = %s", config.Timeout)
	}
	delete(env, "ROUTER_TIMEOUT_MS")
	config = NewConfig(func(key string) string { return env[key] })
	if config.Timeout != DefaultTimeout {
		t.Fatalf("default timeout = %s", config.Timeout)
	}
}

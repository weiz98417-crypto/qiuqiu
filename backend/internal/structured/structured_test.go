package structured

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type sampleResult struct {
	Team       string  `json:"team"`
	EventType  string  `json:"eventType"`
	Confidence float64 `json:"confidence"`
}

func TestExtractForcesToolCallAndDecodesArguments(t *testing.T) {
	var capturedPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&capturedPayload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{
					"tool_calls": []map[string]any{{
						"function": map[string]any{
							"name":      "extract_match_event",
							"arguments": `{"team":"西班牙","eventType":"goal","confidence":0.9}`,
						},
					}},
				},
			}},
		})
	}))
	defer server.Close()

	result, err := Extract[sampleResult](context.Background(), NewClient(server.URL, "test-key", "test-model"), CallOptions{
		SystemPrompt: "system prompt",
		UserContent:  "user transcript",
		ToolName:     "extract_match_event",
		MaxTokens:    320,
		Temperature:  0.1,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if result.Team != "西班牙" || result.EventType != "goal" || result.Confidence != 0.9 {
		t.Fatalf("result = %+v", result)
	}

	// 强制工具选择与 schema 反射：payload 必须带唯一工具且 schema 描述
	// 结果类型（含 json tag 属性名）。
	tools, ok := capturedPayload["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("payload tools = %+v, want exactly one tool", capturedPayload["tools"])
	}
	tool := tools[0].(map[string]any)["function"].(map[string]any)
	if tool["name"] != "extract_match_event" {
		t.Fatalf("tool name = %+v", tool["name"])
	}
	schema, ok := tool["parameters"].(map[string]any)
	if !ok {
		t.Fatalf("parameters = %+v, want a reflected JSON schema", tool["parameters"])
	}
	properties := schema["$schema"].(string)
	if properties == "" {
		t.Fatalf("schema missing $schema marker: %+v", schema)
	}
	choice := capturedPayload["tool_choice"].(map[string]any)
	if choice["type"] != "function" {
		t.Fatalf("tool_choice = %+v, want forced function", choice)
	}
}

func TestExtractErrorsOnMissingToolCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": "plain text"}}},
		})
	}))
	defer server.Close()

	if _, err := Extract[sampleResult](context.Background(), NewClient(server.URL, "k", "m"), CallOptions{ToolName: "extract_match_event"}); err == nil {
		t.Fatal("Extract must error when the model returns no tool call")
	}
}

func TestExtractNilClient(t *testing.T) {
	if _, err := Extract[sampleResult](context.Background(), nil, CallOptions{ToolName: "x"}); err == nil {
		t.Fatal("nil client must error")
	}
}

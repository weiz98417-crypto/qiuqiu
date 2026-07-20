package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEvalMiMoUsesAPIKeyHeader(t *testing.T) {
	var sawAPIKey bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAPIKey = r.Header.Get("api-key") == "mimo-key" && r.Header.Get("Authorization") == ""
		_ = json.NewEncoder(w).Encode(ChatResponse{
			Choices: []Choice{{Message: Message{Role: "assistant", Content: "我是球球。"}}},
			Usage:   Usage{TotalTokens: 3},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "mimo-key", "mimo-v2.5-pro")
	result, err := client.GenerateWithMessages(context.Background(), []Message{{Role: "user", Content: "你是谁？"}}, 0.2)
	if err != nil {
		t.Fatalf("GenerateWithMessages error: %v", err)
	}
	if result.Text != "我是球球。" || !sawAPIKey {
		t.Fatalf("wrong MiMo result/header: result=%+v sawAPIKey=%v", result, sawAPIKey)
	}
}

func TestEvalBearerHeaderForNonMiMoProviders(t *testing.T) {
	var sawBearer bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawBearer = r.Header.Get("Authorization") == "Bearer deepseek-key" && r.Header.Get("api-key") == ""
		_ = json.NewEncoder(w).Encode(ChatResponse{
			Choices: []Choice{{Message: Message{Role: "assistant", Content: "ok"}}},
			Usage:   Usage{TotalTokens: 1},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "deepseek-key", "deepseek-v4-flash")
	if _, err := client.Generate(context.Background(), "hi"); err != nil {
		t.Fatalf("Generate error: %v", err)
	}
	if !sawBearer {
		t.Fatalf("expected bearer auth for non-MiMo provider")
	}
}

func TestGenerateWithMessagesLimitUsesRequestedTokenBudget(t *testing.T) {
	var request ChatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(ChatResponse{
			Choices: []Choice{{Message: Message{Role: "assistant", Content: `{}`}}},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "mimo-key", "mimo-v2.5-pro")
	if _, err := client.GenerateWithMessagesLimit(context.Background(), []Message{{Role: "user", Content: "extract"}}, 0.1, 320); err != nil {
		t.Fatalf("GenerateWithMessagesLimit error: %v", err)
	}
	if request.MaxTokens != 320 {
		t.Fatalf("max_tokens = %d, want 320", request.MaxTokens)
	}
}

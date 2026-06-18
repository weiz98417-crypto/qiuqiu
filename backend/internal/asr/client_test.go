package asr

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEvalASRMockAndNotConfiguredFallback(t *testing.T) {
	result, err := NewMockClient("刚才谁助攻？").Transcribe(context.Background(), []byte("pcm"), nil)
	if err != nil {
		t.Fatalf("mock Transcribe error: %v", err)
	}
	if result.Text != "刚才谁助攻？" || result.Provider != "mock" || result.Confidence != 1 {
		t.Fatalf("wrong mock result: %+v", result)
	}

	_, err = NewClient("").Transcribe(context.Background(), []byte("pcm"), nil)
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("expected ErrNotConfigured, got %v", err)
	}
}

func TestEvalASRRequestFormatting(t *testing.T) {
	var sawAuth, sawAccept, sawModel, sawPrompt bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		sawAuth = r.Header.Get("api-key") == "test-key"
		sawAccept = r.Header.Get("Accept") == "application/json"
		var payload struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content []struct {
					Type       string `json:"type"`
					InputAudio struct {
						Data string `json:"data"`
					} `json:"input_audio"`
				} `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		sawModel = payload.Model == "mimo-v2.5-asr"
		if len(payload.Messages) != 1 || len(payload.Messages[0].Content) != 1 {
			t.Fatalf("expected one input audio message: %+v", payload.Messages)
		}
		sawPrompt = strings.HasPrefix(payload.Messages[0].Content[0].InputAudio.Data, "data:audio/wav;base64,")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"choices": []map[string]interface{}{{"message": map[string]string{"content": "现在几比几？"}}}})
	}))
	defer server.Close()

	client := NewClient("test-key").WithBaseURL(server.URL).WithHTTPClient(server.Client())
	result, err := client.Transcribe(context.Background(), []byte("wav-bytes"), []string{"Spain vs Germany"})
	if err != nil {
		t.Fatalf("Transcribe error: %v", err)
	}
	if result.Text != "现在几比几？" || result.Provider != "mimo" {
		t.Fatalf("wrong result: %+v", result)
	}
	if !sawAuth || !sawAccept || !sawModel || !sawPrompt {
		t.Fatalf("missing expected request fields auth=%v accept=%v model=%v prompt=%v", sawAuth, sawAccept, sawModel, sawPrompt)
	}
}

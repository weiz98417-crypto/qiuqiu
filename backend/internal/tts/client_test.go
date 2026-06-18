package tts

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEvalTTSMockAndNotConfiguredFallback(t *testing.T) {
	result, err := NewMockClient([]byte("mp3")).Synthesize(context.Background(), "你好", "voice-1")
	if err != nil {
		t.Fatalf("mock Synthesize error: %v", err)
	}
	if string(result.AudioData) != "mp3" || result.MimeType != "audio/mpeg" {
		t.Fatalf("wrong mock result: %+v", result)
	}

	_, err = NewClient("").Synthesize(context.Background(), "你好", "voice-1")
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("expected ErrNotConfigured, got %v", err)
	}
}

func TestEvalTTSRequestFormatting(t *testing.T) {
	var sawKey, sawAccept, sawModel, sawText bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		sawKey = r.Header.Get("api-key") == "tts-key"
		sawAccept = r.Header.Get("Accept") == "application/json"
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		var payload map[string]interface{}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		sawModel = payload["model"] == "mimo-v2.5-tts"
		messages, _ := payload["messages"].([]interface{})
		if len(messages) == 1 {
			message, _ := messages[0].(map[string]interface{})
			sawText = message["role"] == "assistant" && message["content"] == "球球回复"
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"choices": []map[string]interface{}{{"message": map[string]interface{}{"audio": map[string]string{"data": "YXVkaW8tYnl0ZXM="}}}}})
	}))
	defer server.Close()

	client := NewClient("tts-key").WithBaseURL(server.URL).WithHTTPClient(server.Client())
	result, err := client.Synthesize(context.Background(), "球球回复", "voice-1")
	if err != nil {
		t.Fatalf("Synthesize error: %v", err)
	}
	if string(result.AudioData) != "audio-bytes" || result.MimeType != "audio/wav" {
		t.Fatalf("wrong result: %+v", result)
	}
	if !sawKey || !sawAccept || !sawModel || !sawText {
		t.Fatalf("missing expected request fields key=%v accept=%v model=%v text=%v", sawKey, sawAccept, sawModel, sawText)
	}
}

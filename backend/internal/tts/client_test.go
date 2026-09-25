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

func TestMockClientIsDeterministicFake(t *testing.T) {
	client := NewMockClient([]byte("mp3"))
	first, err := client.Synthesize(context.Background(), "你好", VoiceOpts{Instruction: "兴奋一点"})
	if err != nil {
		t.Fatalf("mock Synthesize error: %v", err)
	}
	second, err := client.Synthesize(context.Background(), "你好", VoiceOpts{})
	if err != nil {
		t.Fatalf("mock Synthesize error: %v", err)
	}
	if string(first.AudioData) != "mp3" || first.MimeType != "audio/mpeg" {
		t.Fatalf("wrong mock result: %+v", first)
	}
	if string(first.AudioData) != string(second.AudioData) || first.MimeType != second.MimeType {
		t.Fatalf("fake adapter must be deterministic: %+v vs %+v", first, second)
	}

	_, err = NewClient("").Synthesize(context.Background(), "你好", VoiceOpts{})
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("expected ErrNotConfigured, got %v", err)
	}
}

func TestSynthesizeStreamIsReservedSlot(t *testing.T) {
	for _, client := range []Synthesizer{NewClient("key"), NewMockClient([]byte("mp3"))} {
		ch, err := client.SynthesizeStream(context.Background(), "你好", VoiceOpts{})
		if ch != nil || !errors.Is(err, ErrNotSupported) {
			t.Fatalf("SynthesizeStream = %v, %v; want nil, ErrNotSupported", ch, err)
		}
	}
}

func TestSynthesizeRequestFormatting(t *testing.T) {
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
		audio, _ := payload["audio"].(map[string]interface{})
		messages, _ := payload["messages"].([]interface{})
		if len(messages) == 1 && audio != nil {
			message, _ := messages[0].(map[string]interface{})
			sawText = message["role"] == "assistant" && message["content"] == "球球回复"
			if audio["format"] != "wav" || audio["voice"] != "冰糖" {
				t.Fatalf("empty VoiceOpts must fall back to adapter defaults, got audio=%v", audio)
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"choices": []map[string]interface{}{{"message": map[string]interface{}{"audio": map[string]string{"data": "YXVkaW8tYnl0ZXM="}}}}})
	}))
	defer server.Close()

	client := NewClient("tts-key").WithBaseURL(server.URL).WithHTTPClient(server.Client())
	result, err := client.Synthesize(context.Background(), "球球回复", VoiceOpts{})
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

// 组包形状是 Miimo 唯一的风格通道：指令占 messages 首位（user），待合成
// 文本随后（assistant）；VoiceOpts 的音色/格式覆盖默认值。
func TestSynthesizeCarriesInstructionAndVoiceOpts(t *testing.T) {
	var voice, format string
	var messages []map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Messages []map[string]string `json:"messages"`
			Audio    map[string]string   `json:"audio"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		voice = payload.Audio["voice"]
		format = payload.Audio["format"]
		messages = payload.Messages
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"choices": []map[string]interface{}{{"message": map[string]interface{}{"audio": map[string]string{"data": "YXVkaW8="}}}}})
	}))
	defer server.Close()

	client := NewClient("tts-key").WithBaseURL(server.URL).WithHTTPClient(server.Client())
	opts := VoiceOpts{Instruction: "用自然偏快的语速，带一点兴奋感。", Format: "wav", Voice: "另一音色"}
	if _, err := client.Synthesize(context.Background(), "这球太漂亮了！", opts); err != nil {
		t.Fatalf("Synthesize error: %v", err)
	}
	if voice != "另一音色" || format != "wav" {
		t.Fatalf("voice/format = %q/%q, want overrides honored", voice, format)
	}
	if len(messages) != 2 {
		t.Fatalf("messages = %+v, want instruction plus spoken text", messages)
	}
	if messages[0]["role"] != "user" || messages[0]["content"] != "用自然偏快的语速，带一点兴奋感。" {
		t.Fatalf("instruction message = %+v, want user instruction first", messages[0])
	}
	if messages[1]["role"] != "assistant" || messages[1]["content"] != "这球太漂亮了！" {
		t.Fatalf("spoken text = %+v", messages[1])
	}

	// cgSg 前缀是平台已下线的预制音色 id，必须回落默认音色。
	if _, err := client.Synthesize(context.Background(), "这球太漂亮了！", VoiceOpts{Voice: "cgSgspJ2msm6clMCkdW9"}); err != nil {
		t.Fatalf("Synthesize error: %v", err)
	}
	if voice != "冰糖" {
		t.Fatalf("legacy voice id must fall back to default, got %q", voice)
	}
	if len(messages) != 1 || messages[0]["role"] != "assistant" {
		t.Fatalf("messages without instruction = %+v, want the spoken text only", messages)
	}
}

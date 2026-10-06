package tts

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// OpenAI 兼容请求面：POST {base}/audio/speech，instructions 情感指令透传，
// 音频字节直出（非 base64）。
func TestLocalClientSynthesizeHappyPath(t *testing.T) {
	var gotPath, gotVoice, gotInstructions, gotFormat string
	var gotInput string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		var payload struct {
			Model        string `json:"model"`
			Input        string `json:"input"`
			Voice        string `json:"voice"`
			Format       string `json:"response_format"`
			Instructions string `json:"instructions"`
		}
		_ = decodeJSONBody(r, &payload)
		gotVoice = payload.Voice
		gotInstructions = payload.Instructions
		gotFormat = payload.Format
		gotInput = payload.Input
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write([]byte("RIFF-fake-audio"))
	}))
	defer server.Close()

	client := NewLocalClient(server.URL).WithVoice("默认音色")
	result, err := client.Synthesize(context.Background(), "进球了！", VoiceOpts{Instruction: "兴奋解说"})
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if gotPath != "/audio/speech" {
		t.Fatalf("path = %q, want /audio/speech", gotPath)
	}
	if gotInput != "进球了！" {
		t.Fatalf("input = %q", gotInput)
	}
	if gotInstructions != "兴奋解说" {
		t.Fatalf("instructions = %q, want instruction passthrough", gotInstructions)
	}
	if gotFormat != "wav" {
		t.Fatalf("response_format = %q, want wav", gotFormat)
	}
	if gotVoice != "默认音色" {
		t.Fatalf("voice = %q, want adapter default", gotVoice)
	}
	if string(result.AudioData) != "RIFF-fake-audio" || result.MimeType != "audio/wav" {
		t.Fatalf("result = (%q, %q)", result.AudioData, result.MimeType)
	}
}

// 失败/非 200/超时三态：错误如实上抛，不吞。
func TestLocalClientFailureModes(t *testing.T) {
	t.Run("non_200", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "model loading", http.StatusServiceUnavailable)
		}))
		defer server.Close()
		if _, err := NewLocalClient(server.URL).Synthesize(context.Background(), "x", VoiceOpts{}); err == nil || !strings.Contains(err.Error(), "503") {
			t.Fatalf("err = %v, want 503 surfaced", err)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(300 * time.Millisecond)
			_, _ = w.Write([]byte("late"))
		}))
		defer server.Close()
		client := NewLocalClient(server.URL).WithHTTPClient(&http.Client{Timeout: 50 * time.Millisecond})
		if _, err := client.Synthesize(context.Background(), "x", VoiceOpts{}); err == nil {
			t.Fatal("want timeout error")
		}
	})
	t.Run("empty_audio", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		defer server.Close()
		if _, err := NewLocalClient(server.URL).Synthesize(context.Background(), "x", VoiceOpts{}); err == nil {
			t.Fatal("want empty audio error")
		}
	})
	t.Run("not_configured", func(t *testing.T) {
		if _, err := NewLocalClient("").Synthesize(context.Background(), "x", VoiceOpts{}); err != ErrNotConfigured {
			t.Fatalf("err = %v, want ErrNotConfigured", err)
		}
	})
}

// 流式形态：整段合成单分片投出（本地端点无标准流式），Detailed 非 degraded。
func TestLocalClientStreamShape(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("RIFF-whole-utterance"))
	}))
	defer server.Close()
	client := NewLocalClient(server.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ch, err := client.SynthesizeStream(ctx, "进球了", VoiceOpts{})
	if err != nil {
		t.Fatalf("SynthesizeStream: %v", err)
	}
	audio, ok := <-ch
	if !ok || string(audio) != "RIFF-whole-utterance" {
		t.Fatalf("stream chunk = %q ok=%v", audio, ok)
	}
	if _, open := <-ch; open {
		t.Fatal("channel must close after the single chunk")
	}

	var chunks []StreamChunk
	if err := client.SynthesizeStreamDetailed(ctx, "x", VoiceOpts{}, func(chunk StreamChunk) error {
		chunks = append(chunks, chunk)
		return nil
	}); err != nil {
		t.Fatalf("SynthesizeStreamDetailed: %v", err)
	}
	if len(chunks) != 1 || chunks[0].Degraded {
		t.Fatalf("chunks = %+v, want single non-degraded", chunks)
	}
}

// 探测门控：连续通过才亮灯，一次失败即熄。
func TestLocalProbeConsecutiveGate(t *testing.T) {
	healthy := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !healthy {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("RIFF-ok"))
	}))
	defer server.Close()
	probe := NewLocalProbe(NewLocalClient(server.URL), 10*time.Millisecond, time.Second)
	if probe == nil {
		t.Fatal("probe must construct for a configured client")
	}
	t.Cleanup(probe.Close)

	for i := 0; i < probeConsecutivePasses; i++ {
		probe.probeOnce()
	}
	if !probe.Available() {
		t.Fatalf("snapshot after %d passes = %+v, want available", probeConsecutivePasses, probe.Snapshot())
	}

	healthy = false
	probe.probeOnce()
	if probe.Available() {
		t.Fatal("one failure must extinguish the gate")
	}
	if probe.Snapshot().Reason == "" {
		t.Fatal("failure reason must be surfaced for the console")
	}

	healthy = true
	probe.probeOnce()
	if probe.Available() {
		t.Fatal("single pass after failure must not relight (consecutive gate)")
	}
	probe.probeOnce()
	if !probe.Available() {
		t.Fatal("second consecutive pass must relight")
	}
}

// 未配置客户端 → 探测整体不存在（nil），快照呈现未配置原因。
func TestLocalProbeNilWhenUnconfigured(t *testing.T) {
	if probe := NewLocalProbe(NewLocalClient(""), time.Second, time.Second); probe != nil {
		t.Fatal("unconfigured client must yield nil probe")
	}
	var nilProbe *LocalProbe
	snapshot := nilProbe.Snapshot()
	if snapshot.Configured || snapshot.Reason == "" {
		t.Fatalf("nil probe snapshot = %+v, want configured=false with reason", snapshot)
	}
}

func decodeJSONBody(r *http.Request, target interface{}) error {
	return json.NewDecoder(r.Body).Decode(target)
}

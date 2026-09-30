package tts

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"qiuqiu/internal/resilience"
)

// sseEvent 组一个 openai-compat chat completions 流式事件：delta 携带
// base64 音频（Miimo 流式的 pcm16@24kHz 分片形态）。
func sseEvent(t *testing.T, audio []byte) string {
	t.Helper()
	payload, err := json.Marshal(map[string]interface{}{
		"choices": []map[string]interface{}{
			{"delta": map[string]interface{}{
				"audio": map[string]string{"data": base64.StdEncoding.EncodeToString(audio)},
			}},
		},
	})
	if err != nil {
		t.Fatalf("marshal sse event: %v", err)
	}
	return "data: " + string(payload) + "\n\n"
}

func sseDone() string { return "data: [DONE]\n\n" }

// readRequestPayload 解请求体为通用 map，供组包形状断言。
func readRequestPayload(t *testing.T, r *http.Request) map[string]interface{} {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read request body: %v", err)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode request payload: %v", err)
	}
	return payload
}

// isStreamPayload 报告该请求是否流式（stream:true）。
func isStreamPayload(payload map[string]interface{}) bool {
	stream, _ := payload["stream"].(bool)
	return stream
}

// fullAudioResponse 是整段模式的标准应答（同 Synthesize 既有测试形态）。
func fullAudioResponse(audio string) map[string]interface{} {
	return map[string]interface{}{
		"choices": []map[string]interface{}{
			{"message": map[string]interface{}{"audio": map[string]string{"data": base64.StdEncoding.EncodeToString([]byte(audio))}}},
		},
	}
}

func collectChunks(t *testing.T, client *Client, text string, opts VoiceOpts) ([]StreamChunk, error) {
	t.Helper()
	var chunks []StreamChunk
	err := client.SynthesizeStreamDetailed(context.Background(), text, opts, func(chunk StreamChunk) error {
		chunks = append(chunks, chunk)
		return nil
	})
	return chunks, err
}

// 正常多片流：三个 pcm 分片按序回调，MimeType 是裸 pcm16 标签、零降级；
// 请求组包是流式形态（stream:true、format:pcm）。
func TestSynthesizeStreamDetailedMultiChunk(t *testing.T) {
	var sawStream, sawPCMFormat, sawAccept bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := readRequestPayload(t, r)
		sawStream = isStreamPayload(payload)
		audio, _ := payload["audio"].(map[string]interface{})
		sawPCMFormat = audio != nil && audio["format"] == "pcm"
		sawAccept = r.Header.Get("Accept") == "text/event-stream"
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		for _, piece := range []string{"pcm-one", "pcm-two", "pcm-three"} {
			fmt.Fprint(w, sseEvent(t, []byte(piece)))
			flusher.Flush()
		}
		fmt.Fprint(w, sseDone())
		flusher.Flush()
	}))
	defer server.Close()

	client := NewClient("tts-key").WithBaseURL(server.URL).WithHTTPClient(server.Client())
	chunks, err := collectChunks(t, client, "进球了！还戴着围巾呢。", VoiceOpts{})
	if err != nil {
		t.Fatalf("SynthesizeStreamDetailed error: %v", err)
	}
	if len(chunks) != 3 {
		t.Fatalf("chunks = %d, want 3", len(chunks))
	}
	var joined strings.Builder
	for i, chunk := range chunks {
		if chunk.Degraded {
			t.Fatalf("chunk %d must not be degraded on healthy stream", i)
		}
		if chunk.MimeType != MimeTypeStreamPCM {
			t.Fatalf("chunk %d mime = %q, want %q", i, chunk.MimeType, MimeTypeStreamPCM)
		}
		joined.Write(chunk.Data)
	}
	if joined.String() != "pcm-onepcm-twopcm-three" {
		t.Fatalf("joined pcm = %q", joined.String())
	}
	if !sawStream || !sawPCMFormat || !sawAccept {
		t.Fatalf("stream request shape wrong: stream=%v pcmFormat=%v accept=%v", sawStream, sawPCMFormat, sawAccept)
	}
	if state := client.CircuitState(); state != "closed" {
		t.Fatalf("breaker state after healthy stream = %v", state)
	}
}

// 正常单片流：整段即一片。
func TestSynthesizeStreamDetailedSingleChunk(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseEvent(t, []byte("whole-sentence")), sseDone())
	}))
	defer server.Close()

	client := NewClient("tts-key").WithBaseURL(server.URL).WithHTTPClient(server.Client())
	chunks, err := collectChunks(t, client, "好球！", VoiceOpts{})
	if err != nil {
		t.Fatalf("SynthesizeStreamDetailed error: %v", err)
	}
	if len(chunks) != 1 || string(chunks[0].Data) != "whole-sentence" || chunks[0].Degraded {
		t.Fatalf("chunks = %+v, want single healthy chunk", chunks)
	}
}

// SSE 中断回退：服务端发两片后不发 [DONE] 直接断流。adapter 必须回退整段
// 合成，全量音频以单个 Degraded=true 分片收尾（回退分片替换此前残缺前缀，
// 拼接不重复），且对调用方返回 nil。
func TestSynthesizeStreamFallsBackOnStreamBreak(t *testing.T) {
	var streamHits, fullHits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isStreamPayload(readRequestPayload(t, r)) {
			streamHits++
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, sseEvent(t, []byte("partial-a")), sseEvent(t, []byte("partial-b")))
			// 不发 [DONE]，直接结束响应：客户端视角即断流（EOF）。
			return
		}
		fullHits++
		_ = json.NewEncoder(w).Encode(fullAudioResponse("full-wav"))
	}))
	defer server.Close()

	client := NewClient("tts-key").WithBaseURL(server.URL).WithHTTPClient(server.Client())
	chunks, err := collectChunks(t, client, "这球太漂亮了！", VoiceOpts{})
	if err != nil {
		t.Fatalf("fallback path must be transparent, got error: %v", err)
	}
	if len(chunks) != 3 {
		t.Fatalf("chunks = %d, want 2 partials + 1 fallback", len(chunks))
	}
	for i, chunk := range chunks[:2] {
		if chunk.Degraded || chunk.MimeType != MimeTypeStreamPCM {
			t.Fatalf("partial chunk %d = %+v, want healthy pcm", i, chunk)
		}
	}
	fallback := chunks[2]
	if !fallback.Degraded || fallback.MimeType != "audio/wav" || string(fallback.Data) != "full-wav" {
		t.Fatalf("fallback chunk = %+v, want degraded full wav", fallback)
	}
	if streamHits != 1 || fullHits != 1 {
		t.Fatalf("hits stream=%d full=%d, want 1/1", streamHits, fullHits)
	}
	if state := client.CircuitState(); state != "closed" {
		t.Fatalf("breaker must recover via fallback success, got %v", state)
	}
}

// 超时回退：服务端发一片后停摆，空闲超时触发回退。
func TestSynthesizeStreamFallsBackOnIdleTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isStreamPayload(readRequestPayload(t, r)) {
			_ = json.NewEncoder(w).Encode(fullAudioResponse("recovered"))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseEvent(t, []byte("stalled")))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// 停摆直到客户端断开（空闲超时 cancel 会断开连接）。
		<-r.Context().Done()
	}))
	defer server.Close()

	client := NewClient("tts-key").WithBaseURL(server.URL).WithHTTPClient(server.Client()).WithStreamIdleTimeout(120 * time.Millisecond)
	start := time.Now()
	chunks, err := collectChunks(t, client, "悬念拉满了。", VoiceOpts{})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("idle timeout must fall back transparently, got error: %v", err)
	}
	var degraded *StreamChunk
	for i := range chunks {
		if chunks[i].Degraded {
			degraded = &chunks[i]
		}
	}
	if degraded == nil {
		t.Fatalf("no degraded chunk after idle timeout, chunks = %+v", chunks)
	}
	if string(degraded.Data) != "recovered" {
		t.Fatalf("degraded chunk data = %q, want fallback audio", degraded.Data)
	}
	if elapsed > 10*time.Second {
		t.Fatalf("fallback took %v, idle timeout did not fire", elapsed)
	}
}

// 非 200 回退：流式请求被拒（如供应商政策变化），整段路径兜底。
func TestSynthesizeStreamFallsBackOnNon200(t *testing.T) {
	var streamHits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isStreamPayload(readRequestPayload(t, r)) {
			streamHits++
			http.Error(w, `{"error":"streaming unavailable"}`, http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(fullAudioResponse("nonstream-fallback"))
	}))
	defer server.Close()

	client := NewClient("tts-key").WithBaseURL(server.URL).WithHTTPClient(server.Client())
	chunks, err := collectChunks(t, client, "别急，还有下半场。", VoiceOpts{})
	if err != nil {
		t.Fatalf("non-200 must fall back transparently, got error: %v", err)
	}
	if len(chunks) != 1 || !chunks[0].Degraded || string(chunks[0].Data) != "nonstream-fallback" {
		t.Fatalf("chunks = %+v, want single degraded fallback", chunks)
	}
	if streamHits != 1 {
		t.Fatalf("stream hits = %d, want 1", streamHits)
	}
}

// 零分片「正常流」也回退：服务端只发 [DONE]，供应商静默空转。
func TestSynthesizeStreamFallsBackOnEmptyStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isStreamPayload(readRequestPayload(t, r)) {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, sseDone())
			return
		}
		_ = json.NewEncoder(w).Encode(fullAudioResponse("empty-stream-fallback"))
	}))
	defer server.Close()

	client := NewClient("tts-key").WithBaseURL(server.URL).WithHTTPClient(server.Client())
	chunks, err := collectChunks(t, client, "你说呢？", VoiceOpts{})
	if err != nil {
		t.Fatalf("empty stream must fall back, got error: %v", err)
	}
	if len(chunks) != 1 || !chunks[0].Degraded || string(chunks[0].Data) != "empty-stream-fallback" {
		t.Fatalf("chunks = %+v, want single degraded fallback", chunks)
	}
}

// 回退也失败时如实报错，绝不吞错。
func TestSynthesizeStreamFallsBackAndStillFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "provider down", http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient("tts-key").WithBaseURL(server.URL).WithHTTPClient(server.Client())
	if _, err := collectChunks(t, client, "完了。", VoiceOpts{}); err == nil {
		t.Fatalf("expected error when both stream and fallback fail")
	}
}

// 调用方取消（打断语义）不回退、不重试：剩余音频直接放弃。
func TestSynthesizeStreamCallerCancelDoesNotFallBack(t *testing.T) {
	var hits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseEvent(t, []byte("first")))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := NewClient("tts-key").WithBaseURL(server.URL).WithHTTPClient(server.Client())
	errCh := make(chan error, 1)
	go func() {
		err := client.SynthesizeStreamDetailed(ctx, "先别换台。", VoiceOpts{}, func(StreamChunk) error { return nil })
		errCh <- err
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()
	if err := <-errCh; !errors.Is(err, context.Canceled) {
		t.Fatalf("caller cancel must surface ctx error, got %v", err)
	}
	if hits != 1 {
		t.Fatalf("server hits = %d, want 1 (no fallback after caller cancel)", hits)
	}
}

// onChunk 返回 error 是调用方主动中止：错误如实上抛，不回退、不记熔断。
func TestSynthesizeStreamCallbackAbortDoesNotFallBack(t *testing.T) {
	var hits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseEvent(t, []byte("piece-one")), sseEvent(t, []byte("piece-two")), sseDone())
	}))
	defer server.Close()

	client := NewClient("tts-key").WithBaseURL(server.URL).WithHTTPClient(server.Client())
	abort := errors.New("stop streaming")
	delivered := 0
	err := client.SynthesizeStreamDetailed(context.Background(), "一句话。", VoiceOpts{}, func(StreamChunk) error {
		delivered++
		return abort
	})
	if !errors.Is(err, abort) {
		t.Fatalf("callback abort must propagate, got %v", err)
	}
	if delivered != 1 || hits != 1 {
		t.Fatalf("delivered=%d hits=%d, want 1/1 (no fallback)", delivered, hits)
	}
	if state := client.CircuitState(); state != "closed" {
		t.Fatalf("callback abort must not trip breaker, got %v", state)
	}
}

// instruction 拼法一致性：流式与整段两次请求的 messages/model/voice 必须
// 完全一致（Miimo 唯一风格通道，两种模式共用一份组包），差异只允许
// stream 标志与音频容器（pcm vs wav）。
func TestStreamPayloadMirrorsNonStreamPayload(t *testing.T) {
	var mu = make(chan map[string]interface{}, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := readRequestPayload(t, r)
		mu <- payload
		if isStreamPayload(payload) {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, sseEvent(t, []byte("x")), sseDone())
			return
		}
		_ = json.NewEncoder(w).Encode(fullAudioResponse("y"))
	}))
	defer server.Close()

	client := NewClient("tts-key").WithBaseURL(server.URL).WithHTTPClient(server.Client())
	opts := VoiceOpts{Instruction: "用自然偏快的语速，带一点兴奋感。", Format: "wav", Voice: "另一音色"}
	if _, err := client.Synthesize(context.Background(), "这球太漂亮了！", opts); err != nil {
		t.Fatalf("Synthesize error: %v", err)
	}
	if _, err := collectChunks(t, client, "这球太漂亮了！", opts); err != nil {
		t.Fatalf("SynthesizeStreamDetailed error: %v", err)
	}
	close(mu)
	var payloads []map[string]interface{}
	for payload := range mu {
		payloads = append(payloads, payload)
	}
	if len(payloads) != 2 {
		t.Fatalf("captured %d payloads, want 2", len(payloads))
	}
	var full, stream map[string]interface{}
	for _, p := range payloads {
		if isStreamPayload(p) {
			stream = p
		} else {
			full = p
		}
	}
	if full == nil || stream == nil {
		t.Fatalf("must capture one payload per mode, got %+v", payloads)
	}
	if !reflect.DeepEqual(full["messages"], stream["messages"]) {
		t.Fatalf("messages must be identical across modes: full=%v stream=%v", full["messages"], stream["messages"])
	}
	if full["model"] != stream["model"] {
		t.Fatalf("model mismatch: %v vs %v", full["model"], stream["model"])
	}
	fullAudio, streamAudio := full["audio"].(map[string]interface{}), stream["audio"].(map[string]interface{})
	if !reflect.DeepEqual(fullAudio["voice"], streamAudio["voice"]) {
		t.Fatalf("voice mismatch: %v vs %v", fullAudio["voice"], streamAudio["voice"])
	}
	if streamAudio["format"] != "pcm" || fullAudio["format"] != "wav" {
		t.Fatalf("format mismatch: stream=%v full=%v, want pcm vs wav", streamAudio["format"], fullAudio["format"])
	}
}

// 熔断沿用：连续失败把 breaker 打进 open 后，流式入口直接拒绝，不再发
// 任何请求（含回退）。
func TestSynthesizeStreamHonorsCircuitBreaker(t *testing.T) {
	var hits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.Error(w, "down", http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient("tts-key").WithBaseURL(server.URL).WithHTTPClient(server.Client())
	// 3 次整段失败（阈值 3）把熔断打开。
	for i := 0; i < 3; i++ {
		if _, err := client.Synthesize(context.Background(), "喂？", VoiceOpts{}); err == nil {
			t.Fatalf("synthesize %d must fail", i)
		}
	}
	if state := client.CircuitState(); state != "open" {
		t.Fatalf("breaker state = %v, want open", state)
	}
	before := hits
	if _, err := collectChunks(t, client, "还在吗？", VoiceOpts{}); err == nil || !strings.Contains(err.Error(), "tts unavailable") {
		t.Fatalf("open breaker must refuse stream start, got %v", err)
	}
	if hits != before {
		t.Fatalf("open breaker must not reach provider, hits %d -> %d", before, hits)
	}
}

// channel 形态的 SynthesizeStream：真流式分片按序投出并关闭；mock 路径
// 回放单分片（client_test.go 的 TestSynthesizeStreamChannels 已覆盖 mock
// 与未配置，这里补真实 SSE 路径）。
func TestSynthesizeStreamChannelFormDeliversChunks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseEvent(t, []byte("c1")), sseEvent(t, []byte("c2")), sseDone())
	}))
	defer server.Close()

	client := NewClient("tts-key").WithBaseURL(server.URL).WithHTTPClient(server.Client())
	ch, err := client.SynthesizeStream(context.Background(), "两片即可。", VoiceOpts{})
	if err != nil || ch == nil {
		t.Fatalf("SynthesizeStream = %v, %v", ch, err)
	}
	var chunks []string
	for data := range ch {
		chunks = append(chunks, string(data))
	}
	if !reflect.DeepEqual(chunks, []string{"c1", "c2"}) {
		t.Fatalf("chunks = %v, want [c1 c2]", chunks)
	}
}

// mock 替身走 detailed 路径：单分片回放、零降级。
func TestMockClientStreamDetailed(t *testing.T) {
	client := NewMockClient([]byte("fake-pcm"))
	chunks, err := collectChunks(t, client, "任意文本", VoiceOpts{Instruction: "兴奋一点"})
	if err != nil {
		t.Fatalf("mock detailed error: %v", err)
	}
	if len(chunks) != 1 || string(chunks[0].Data) != "fake-pcm" || chunks[0].Degraded {
		t.Fatalf("mock chunks = %+v, want single deterministic chunk", chunks)
	}
}

// ── 猎虫追加：半开探测被打断后名额泄漏 → 熔断假死 ──

// 打断路径（onChunk 中止 / 调用方取消）不记熔断失败是对的（供应商无辜），
// 但半开态的探测名额也不归还——Allow 发出去的探测没有结论回来，probeTaken
// 永远 true，之后每一次 Allow 都拒绝：语音链路假死到进程重启。
func TestBugInterruptedHalfOpenProbeMustNotStrandBreaker(t *testing.T) {
	var mu sync.Mutex
	healthy := false
	entered := make(chan struct{}, 8)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		h := healthy
		mu.Unlock()
		entered <- struct{}{}
		if !h {
			http.Error(w, "down", http.StatusInternalServerError)
			return
		}
		<-release
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseEvent(t, []byte("recovered")), sseDone())
	}))
	defer server.Close()

	client := NewClient("tts-key").WithBaseURL(server.URL).WithHTTPClient(server.Client()).
		WithCircuitBreaker(resilience.NewCircuitBreaker(3, 30*time.Millisecond))
	// 三次失败 → open。
	for i := 0; i < 3; i++ {
		if _, err := client.Synthesize(context.Background(), "喂？", VoiceOpts{}); err == nil {
			t.Fatalf("synthesize %d must fail", i)
		}
	}
	// 等 openFor 过去，下一次调用成为半开探测。
	time.Sleep(60 * time.Millisecond)
	// 探测请求打到服务端后立即打断（打断语义：不回退、不记熔断）。
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-entered
		cancel()
		close(release)
	}()
	if err := client.SynthesizeStreamDetailed(ctx, "一句话。", VoiceOpts{}, func(StreamChunk) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("interrupted probe must surface ctx error, got %v", err)
	}
	// 服务恢复：下一次调用必须重新获得探测机会并成功——修复前名额未归还，
	// 这里只能拿到 tts unavailable（熔断假死）。
	mu.Lock()
	healthy = true
	mu.Unlock()
	chunks, err := collectChunks(t, client, "还在吗。", VoiceOpts{})
	if err != nil {
		t.Fatalf("interrupted half-open probe stranded the breaker: %v", err)
	}
	if len(chunks) != 1 || chunks[0].Degraded {
		t.Fatalf("chunks = %+v, want one healthy stream chunk", chunks)
	}
}

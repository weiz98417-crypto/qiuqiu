package ambient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"qiuqiu/internal/resilience"
)

func TestClassifyDecodesSidecarEvents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/aevents" || r.Method != http.MethodPost {
			t.Errorf("unexpected call: %s %s", r.Method, r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if got := r.Header.Get("Content-Type"); got != "application/octet-stream" {
			t.Errorf("content type = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"kind":"cheer","confidence":0.9,"ts":"2026-09-25T12:00:00Z"},{"kind":"boo","confidence":1.5}]`))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	events, err := client.Classify(context.Background(), []byte{1, 0, 2, 0})
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %+v", events)
	}
	if events[0].Kind != KindCheer || events[0].Confidence != 0.9 {
		t.Fatalf("first event = %+v", events[0])
	}
	// 第二条缺 ts 补 now、置信度 1.5 夹到 1。
	if events[1].Kind != KindBoo || events[1].Confidence != 1 || events[1].TS.IsZero() {
		t.Fatalf("second event = %+v", events[1])
	}
	if client.Failures() != 0 {
		t.Fatalf("failures = %d, want 0", client.Failures())
	}
}

func TestClassifyDropsUnknownKinds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"kind":"applause","confidence":0.8},{"kind":"","confidence":0.5},{"kind":"volume_spike","confidence":-3}]`))
	}))
	defer server.Close()

	events, err := NewClient(server.URL).Classify(context.Background(), []byte{1, 0})
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if len(events) != 1 || events[0].Kind != KindVolumeSpike || events[0].Confidence != 0 {
		t.Fatalf("events = %+v, want one clamped volume_spike", events)
	}
}

func TestClassifyFailuresAreCountedSilentlyAndTripTheBreaker(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx := context.Background()
	for attempt := 1; attempt <= 4; attempt++ {
		if _, err := client.Classify(ctx, []byte{1, 0}); err == nil {
			t.Fatalf("attempt %d: expected silent failure", attempt)
		}
	}
	if client.Failures() != 4 {
		t.Fatalf("failures = %d, want 4 (three real + one rejected while open)", client.Failures())
	}
	if client.CircuitState() != resilience.StateOpen {
		t.Fatalf("circuit = %q, want open", client.CircuitState())
	}
	if calls != 3 {
		t.Fatalf("server calls = %d, want 3 (breaker rejected the rest)", calls)
	}
}

func TestClassifyMalformedBodyIsASilentFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	if _, err := client.Classify(context.Background(), []byte{1, 0}); err == nil {
		t.Fatal("expected decode failure")
	}
	if client.Failures() != 1 {
		t.Fatalf("failures = %d, want 1", client.Failures())
	}
}

func TestClassifyUnreachableEndpointFailsFastAndCounts(t *testing.T) {
	// 已关闭的本地端口：摘除场景（sidecar 不在）在本机的等价形态。
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := server.URL
	server.Close()

	client := NewClient(url).WithTimeout(300 * time.Millisecond)
	startedAt := time.Now()
	events, err := client.Classify(context.Background(), []byte{1, 0})
	if err == nil || events != nil {
		t.Fatalf("events = %+v err = %v, want silent failure", events, err)
	}
	if elapsed := time.Since(startedAt); elapsed > 2*time.Second {
		t.Fatalf("Classify blocked for %v on unreachable sidecar", elapsed)
	}
	if client.Failures() != 1 {
		t.Fatalf("failures = %d, want 1", client.Failures())
	}
}

func TestUnconfiguredClientIsDisabled(t *testing.T) {
	var disabled *Client
	if disabled.Enabled() {
		t.Fatal("nil client reported enabled")
	}
	if blank := NewClient("   "); blank.Enabled() {
		t.Fatal("blank endpoint reported enabled")
	}
	if _, err := NewClient("").Classify(context.Background(), []byte{1, 0}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("err = %v, want ErrNotConfigured", err)
	}
	// 空 pcm 无需外呼，直接无事件。
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("empty chunk should not reach the sidecar")
	}))
	defer server.Close()
	events, err := NewClient(server.URL).Classify(context.Background(), nil)
	if err != nil || events != nil {
		t.Fatalf("empty chunk produced %+v err=%v", events, err)
	}
}

func TestEventStringStaysSingleLine(t *testing.T) {
	event := Event{Kind: KindCheer, Confidence: 0.9, TS: time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)}
	if text := event.String(); strings.Contains(text, "\n") || !strings.Contains(text, "cheer") {
		t.Fatalf("String = %q", text)
	}
}

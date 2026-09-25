package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"qiuqiu/internal/ambient"
)

// 假 sidecar 测试（ambient-audio-observation 6.1）：确定性是 pr tier 的
// 前提——同一分片字节必须永远得到同一事件序列，且形状与真 sidecar 一致。

func TestClassifyDeterministicIsStableForIdenticalChunks(t *testing.T) {
	pcm := bytes.Repeat([]byte{7, 0, 13, 0}, 32)
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	first := classifyDeterministic(pcm, now)
	second := classifyDeterministic(pcm, now)
	if len(first) != len(second) {
		t.Fatalf("event counts differ: %d vs %d", len(first), len(second))
	}
	for index := range first {
		if first[index] != second[index] {
			t.Fatalf("event %d differs: %+v vs %+v", index, first[index], second[index])
		}
	}
	for _, event := range first {
		if ambient.NormalizeKind(event.Kind) == "" {
			t.Fatalf("event kind %q is outside the sidecar contract", event.Kind)
		}
		if event.Confidence < 0.55 || event.Confidence >= 0.95 {
			t.Fatalf("confidence %v outside the deterministic band", event.Confidence)
		}
	}
}

func TestClassifyDeterministicCoversSilenceAndAllKinds(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	seen := make(map[string]int)
	silent := 0
	for marker := 0; marker < 64; marker++ {
		pcm := []byte{byte(marker), 0, byte(marker + 1), 0}
		events := classifyDeterministic(pcm, now)
		if len(events) == 0 {
			silent++
			continue
		}
		if len(events) != 1 {
			t.Fatalf("fake sidecar should stay single-event per chunk, got %d", len(events))
		}
		seen[events[0].Kind]++
	}
	if silent == 0 {
		t.Fatal("deterministic map never produces a quiet stretch")
	}
	for _, kind := range []string{ambient.KindCheer, ambient.KindBoo, ambient.KindVolumeSpike} {
		if seen[kind] == 0 {
			t.Fatalf("kind %q unreachable in the deterministic map", kind)
		}
	}
	if classifyDeterministic(nil, now) != nil {
		t.Fatal("empty chunk produced events")
	}
}

func TestAEventsHandlerSpeaksSidecarShape(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(handleAEvents))
	defer server.Close()

	response, err := http.Post(server.URL+"/aevents", "application/octet-stream", bytes.NewReader([]byte{9, 0, 9, 0, 9, 0}))
	if err != nil {
		t.Fatalf("POST /aevents: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
	var events []ambient.Event
	if err := json.NewDecoder(response.Body).Decode(&events); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, event := range events {
		if ambient.NormalizeKind(event.Kind) == "" || event.TS.IsZero() {
			t.Fatalf("event outside the sidecar contract: %+v", event)
		}
	}

	// 非 POST 拒绝：与无状态 HTTP 契约一致。
	get, err := http.Get(server.URL + "/aevents")
	if err != nil {
		t.Fatalf("GET /aevents: %v", err)
	}
	defer get.Body.Close()
	if get.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET status = %d, want 405", get.StatusCode)
	}
}

func TestPortFromEnvFallsBackToComposePort(t *testing.T) {
	t.Setenv(fakePortKey, "")
	if portFromEnv() != "8090" {
		t.Fatalf("default port = %q, want 8090", portFromEnv())
	}
	t.Setenv(fakePortKey, "8123")
	if portFromEnv() != "8123" {
		t.Fatalf("configured port = %q, want 8123", portFromEnv())
	}
	t.Setenv(fakePortKey, "not-a-port")
	if portFromEnv() != "8090" {
		t.Fatalf("invalid port fell back to %q, want 8090", portFromEnv())
	}
}

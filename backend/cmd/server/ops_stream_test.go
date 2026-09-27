package main

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"qiuqiu/internal/interaction"
	"qiuqiu/internal/relationship"
)

// 隐私断言（ADR-0021 实时流层）：裁剪后的 wire 事件在序列化层面不存在
// 正文字段——不是「值为空」，是键不存在。
func TestTrimOpsEventCarriesNoUserContent(t *testing.T) {
	event := interaction.Event{
		ID: "evt-1", Kind: interaction.KindTurnPlanned, SignalID: "sig-1",
		UserID: "u1", MatchID: "m1", TraceID: "trace-1",
		DeliveryState: "completed", PlaybackState: "ended",
		Phrase: "神扑！稳住了！", InputText: "用户的原话", OutputText: "球球的原话",
		Decision:     &relationship.Decision{ID: "d1"},
		TracePayload: json.RawMessage(`{"latencyMs": 1234, "input": "用户的原话"}`),
		CreatedAt:    time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC),
	}

	raw, err := json.Marshal(trimOpsEvent(event))
	if err != nil {
		t.Fatalf("marshal wire event: %v", err)
	}
	var wire map[string]interface{}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, forbidden := range []string{"phrase", "inputText", "outputText", "decision", "presentation", "trace", "signalId"} {
		if _, ok := wire[forbidden]; ok {
			t.Fatalf("wire event carries forbidden key %q: %s", forbidden, raw)
		}
	}
	if wire["latencyMs"] != float64(1234) {
		t.Fatalf("latencyMs = %v, want 1234", wire["latencyMs"])
	}
	if wire["type"] != "ops_event" || wire["kind"] != "turn_planned" {
		t.Fatalf("wire shape wrong: %s", raw)
	}
}

// observingLedger：Append 成功才喂观察者；失败不喂。
func TestObservingLedgerFeedsObserverOnlyOnSuccess(t *testing.T) {
	inner := interaction.NewMemoryLedger()
	var mu sync.Mutex
	observed := []interaction.Event{}
	stream := &OpsStream{}
	ledger := newObservingLedger(inner, func(event interaction.Event) {
		mu.Lock()
		observed = append(observed, event)
		mu.Unlock()
	})
	_ = stream

	ctx := context.Background()
	if _, err := ledger.Append(ctx, interaction.Event{ID: "e1", Kind: interaction.KindSignal, UserID: "u1", MatchID: "m1", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("append: %v", err)
	}
	// 同 ID 同事件重放（幂等命中）也算成功；坏事件（缺 MatchID）必须失败且不喂。
	if _, err := ledger.Append(ctx, interaction.Event{ID: "e-bad", Kind: interaction.KindSignal, UserID: ""}); err == nil {
		t.Fatal("invalid event must error")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(observed) != 1 || observed[0].ID != "e1" {
		t.Fatalf("observed = %v, want exactly e1", observed)
	}
}

// 队列饱和丢弃计数；零订阅时泵取出即弃（不广播不崩）。
func TestOpsStreamDropsWhenSaturatedAndPumpDiscardsWithoutSubscribers(t *testing.T) {
	stream := NewOpsStream(1)
	// 不起泵：塞满 1 条后第 2 条必须丢弃计数。
	stream.Observe(interaction.Event{ID: "e1", Kind: interaction.KindSignal, MatchID: "m1"})
	stream.Observe(interaction.Event{ID: "e2", Kind: interaction.KindSignal, MatchID: "m1"})
	if stream.Dropped() != 1 {
		t.Fatalf("dropped = %d, want 1", stream.Dropped())
	}
	// 起泵消费掉队列；零订阅不 panic。
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { stream.Run(ctx); close(done) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done
	if stream.Dropped() != 1 {
		t.Fatalf("dropped drifted = %d", stream.Dropped())
	}
}

// Trim 纪律的另一个切面：所有 kind 的 wire 事件都不该包含正文字段序列化
// （批量断言，防未来加 kind 时漏裁剪）。
func TestTrimOpsEventAllKindsStayLean(t *testing.T) {
	kinds := []interaction.Kind{
		interaction.KindTurnPlanned, interaction.KindDelivery, interaction.KindSignal,
		interaction.KindFactRevision, interaction.KindMediaDelivery, interaction.KindPlaybackResult,
		interaction.KindTurnStale, interaction.KindBackchannel, interaction.KindCharacterSetting,
	}
	for _, kind := range kinds {
		raw, err := json.Marshal(trimOpsEvent(interaction.Event{
			Kind: kind, UserID: "u", MatchID: "m",
			Phrase: "x", InputText: "x", OutputText: "x",
		}))
		if err != nil {
			t.Fatalf("%s marshal: %v", kind, err)
		}
		if strings.Contains(string(raw), "x") {
			t.Fatalf("kind %s wire leaked content: %s", kind, raw)
		}
		if strings.Contains(string(raw), "phrase") || strings.Contains(string(raw), "inputText") {
			t.Fatalf("kind %s wire carries content keys: %s", kind, raw)
		}
	}
}

// 可选接口透传（session-isolation eval 实测回归）：包装器不得把内层的
// 分页/快照能力藏掉——/traces 投影与交互分页都依赖类型断言。
func TestObservingLedgerForwardsOptionalInterfaces(t *testing.T) {
	var ledger interaction.Ledger = interaction.NewMemoryLedger()
	wrapped := newObservingLedger(ledger, func(interaction.Event) {})
	if _, ok := interface{}(wrapped).(interaction.MatchSnapshotLedger); !ok {
		t.Fatal("observingLedger must satisfy MatchSnapshotLedger when inner does")
	}
	if _, ok := interface{}(wrapped).(interaction.SnapshotLedger); !ok {
		t.Fatal("observingLedger must satisfy SnapshotLedger when inner does")
	}
	if _, ok := interface{}(wrapped).(interaction.PageableLedger); !ok {
		t.Fatal("observingLedger must satisfy PageableLedger when inner does")
	}
	events, err := interface{}(wrapped).(interaction.MatchSnapshotLedger).ListMatchSnapshot(context.Background(), "m1")
	if err != nil {
		t.Fatalf("forwarded match snapshot: %v", err)
	}
	if events == nil {
		t.Fatal("forwarded match snapshot returned nil events")
	}
}

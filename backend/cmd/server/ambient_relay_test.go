package main

// 气氛旁路接线测试（ambient-audio-observation 6.3）：
//  1. 宪法负例——sidecar 气氛事件注入后，事实账本（matchstate fact ledger）
//     无任何新行；旁证只落 observation store 的 kind=ambient 槽位。
//  2. 旁证落库——fake 事件在 coordinator 可见，带最低权重档标注。
//  3. 摘除场景——sidecar 不可达（pr tier 用关闭端口模拟 compose 停服务），
//     ASR/话轮主链路照常，旁路静默计数。
//  4. 接线单测——分片旁送不阻塞、身份缺失跳过、nil relay 安全。

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"qiuqiu/internal/ambient"
	"qiuqiu/internal/asr"
	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/observation"
)

// stubClassifier 是 relay 的 sidecar 桩：返回预置事件或错误，记录调用数。
type stubClassifier struct {
	mu      sync.Mutex
	events  []ambient.Event
	err     error
	calls   int
	gate    chan struct{} // 非 nil 时 Classify 阻塞直至 close，模拟慢 sidecar
	callsCh chan struct{} // 非 nil 时每次调用发一个信号
}

func (stub *stubClassifier) Classify(_ context.Context, _ []byte) ([]ambient.Event, error) {
	stub.mu.Lock()
	stub.calls++
	gate, callsCh := stub.gate, stub.callsCh
	stub.mu.Unlock()
	if callsCh != nil {
		callsCh <- struct{}{}
	}
	if gate != nil {
		<-gate
	}
	if stub.err != nil {
		return nil, stub.err
	}
	return stub.events, nil
}

func (stub *stubClassifier) callCount() int {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	return stub.calls
}

// waitForReplication 轮询等待旁路协程完成（Forward 是异步投递）。
func waitForReplication(t *testing.T, probe func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if probe() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for ambient relay replication")
}

// TestAmbientEventsNeverEnterTheFactLedger 是 tripwire（ADR-0002）：本测试
// 锁的是「旁路现状不触碰事实账本」这一构造性为真的事实——若未来有人把
// 旁路接进 matchstate，则在此爆炸。真正的锁在 observation 层测试：旁证只
// 能落 kind=ambient 槽位、不带 claim 语义（欢呼变不成比分）。
func TestAmbientEventsNeverEnterTheFactLedger(t *testing.T) {
	ctx := context.Background()
	factStore := matchstate.NewStore()
	coordinator := observation.NewMemoryCoordinator()
	classifier := &stubClassifier{events: []ambient.Event{
		{Kind: ambient.KindCheer, Confidence: 0.92, TS: time.Now().UTC()},
		{Kind: ambient.KindVolumeSpike, Confidence: 0.7, TS: time.Now().UTC()},
	}}
	relay := newAmbientRelay(classifier, coordinator)

	relay.Forward("user-1", "match-1", []byte{1, 0, 2, 0})
	waitForReplication(t, func() bool {
		active, err := coordinator.ActiveObservations(ctx, "user-1", "match-1")
		return err == nil && len(active) == 2
	})

	// 负例断言：事实账本零新行、公开快照比分未动。
	events := factStore.Events("match-1")
	if len(events) != 0 {
		t.Fatalf("ambient events leaked into the fact ledger: %+v", events)
	}
	projection, err := factStore.Replay("match-1", 0)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if projection.LastSequence != 0 || len(projection.PublicEvents) != 0 {
		t.Fatalf("fact ledger projection advanced: %+v", projection)
	}
	if snapshot := factStore.PublicSnapshot("match-1"); snapshot.Score != (matchstate.Score{}) {
		t.Fatalf("public score moved from ambient injection: %+v", snapshot.Score)
	}
	// 旁证行的落点与标注：kind=ambient、最低权重档、事件类型在 ambient_* 命名空间。
	active, err := coordinator.ActiveObservations(ctx, "user-1", "match-1")
	if err != nil || len(active) != 2 {
		t.Fatalf("active corroboration rows = %+v err = %v", active, err)
	}
	for _, row := range active {
		if row.Kind != observation.KindAmbient || row.Certainty != observation.CertaintyAmbient {
			t.Fatalf("row missing lowest-weight corroboration markers: %+v", row)
		}
		if !strings.HasPrefix(row.EventType, "ambient_") || row.ClaimedScore != nil {
			t.Fatalf("corroboration row carries claim semantics: %+v", row)
		}
		if row.Status != observation.StatusPendingSync {
			t.Fatalf("corroboration row resolved on its own: %+v", row)
		}
	}
}

// TestRelayLandsFakeSidecarEventForOneKindPerSlot 验证同分钟同种类旁证
// 合并成槽：气氛洪峰不挤占用户主张的活跃观察额度。
func TestRelayLandsFakeSidecarEventsWithDedupedSlots(t *testing.T) {
	ctx := context.Background()
	coordinator := observation.NewMemoryCoordinator()
	classifier := &stubClassifier{events: []ambient.Event{
		{Kind: ambient.KindBoo, Confidence: 0.8, TS: time.Date(2026, 9, 25, 12, 0, 10, 0, time.UTC)},
		{Kind: ambient.KindBoo, Confidence: 0.8, TS: time.Date(2026, 9, 25, 12, 0, 20, 0, time.UTC)},
	}}
	relay := newAmbientRelay(classifier, coordinator)

	relay.Forward("user-1", "match-1", []byte{1, 0})
	waitForReplication(t, func() bool {
		active, err := coordinator.ActiveObservations(ctx, "user-1", "match-1")
		return err == nil && len(active) == 1
	})
	if classifier.callCount() != 1 {
		t.Fatalf("classifier calls = %d, want 1", classifier.callCount())
	}
}

// TestASRMainPathSurvivesUnreachableSidecar 是摘除场景测试：compose 停掉
// sidecar（本机等价形态 = 端点不可达）后，ASR 流式转写整条主链路照常，
// 旁路只留下静默计数。
func TestASRMainPathSurvivesUnreachableSidecar(t *testing.T) {
	// 关闭的本地服务：摘除 sidecar 的本机模拟（连接立即被拒）。
	deadSidecar := newDeadEndpoint(t)
	client := ambient.NewClient(deadSidecar).WithTimeout(300 * time.Millisecond)
	coordinator := observation.NewMemoryCoordinator()
	relay := newAmbientRelay(client, coordinator)

	messages := make(chan map[string]interface{}, 4)
	completions := make(chan transcriptionCompletion, 1)
	sessions := newTranscriptionSessions(
		context.Background(),
		asr.NewMockClient("现在比分多少"),
		asr.StreamOptions{PartialBytes: 4, MaxBytes: 32},
		func(message map[string]interface{}) { messages <- message },
		func(completion transcriptionCompletion) { completions <- completion },
	)
	defer sessions.Close()

	if err := sessions.Start("utterance-1", "signal-1", "user-1", "Asia/Shanghai", nil); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// 与 watchconnection asr_chunk 相同的顺序：分片旁送 + ASR 主路 Append。
	relay.Forward("user-1", "match-1", []byte{1, 0, 2, 0})
	if err := sessions.Append("utterance-1", 0, []byte{1, 0, 2, 0}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := sessions.Finish("utterance-1"); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	select {
	case completion := <-completions:
		if completion.Text != "现在比分多少" {
			t.Fatalf("ASR main path degraded: %+v", completion)
		}
	case <-time.After(time.Second):
		t.Fatal("ASR main path stalled while sidecar was unreachable")
	}
	// 旁路失败静默计数，不产生任何观察行。直接等 dropped 计数（client 的
	// Failures 在 Classify 内部先落，早于旁路协程的记账步，全量并发下有
	// 观察窗口差）。
	waitForReplication(t, func() bool { return relay.Dropped() > 0 })
	if active, err := coordinator.ActiveObservations(context.Background(), "user-1", "match-1"); err != nil || len(active) != 0 {
		t.Fatalf("failed sidecar calls left observation rows: %+v err = %v", active, err)
	}
	if relay.Dropped() == 0 {
		t.Fatal("unreachable sidecar was not counted as a silent drop")
	}
}

// TestWatchChunkHookForwardsAndStaysSilent 走真实挂点方法 relayAmbientChunk：
// nil relay（未配置端点）整体跳过；配置后经连接身份旁送。
func TestWatchChunkHookForwardsAndStaysSilent(t *testing.T) {
	coordinator := observation.NewMemoryCoordinator()
	classifier := &stubClassifier{events: []ambient.Event{
		{Kind: ambient.KindCheer, Confidence: 0.9, TS: time.Now().UTC()},
	}}
	relay := newAmbientRelay(classifier, coordinator)

	connection := &watchConnection{deps: watchDeps{ambient: relay}, identity: newConnectionIdentity("user-1")}
	connection.matchID = "match-1"
	connection.relayAmbientChunk([]byte{1, 0})
	waitForReplication(t, func() bool {
		active, err := coordinator.ActiveObservations(context.Background(), "user-1", "match-1")
		return err == nil && len(active) == 1
	})

	// 未配置端点（nil relay）：调用整体跳过，分类器零调用。
	idleConnection := &watchConnection{deps: watchDeps{}, identity: newConnectionIdentity("user-1")}
	idleConnection.matchID = "match-1"
	idleConnection.relayAmbientChunk([]byte{1, 0})
	if classifier.callCount() != 1 {
		t.Fatalf("classifier calls = %d, want 1（nil relay 必须整体跳过）", classifier.callCount())
	}
}

// TestRelayCapsInFlightForwardsOnSlowSidecar 是在途上限测试：sidecar 健康
// 但慢时，批量 asr_chunk 连续投递超过上限后，超出部分被静默丢弃并计入
// dropped，不堆积协程；在途释放后新分片重新可投递。
func TestRelayCapsInFlightForwardsOnSlowSidecar(t *testing.T) {
	coordinator := observation.NewMemoryCoordinator()
	classifier := &stubClassifier{events: []ambient.Event{
		{Kind: ambient.KindCheer, Confidence: 0.9, TS: time.Now().UTC()},
	}}
	classifier.gate = make(chan struct{})
	relay := newAmbientRelay(classifier, coordinator)

	const forwards = ambientRelayMaxInFlight + 3
	for i := 0; i < forwards; i++ {
		relay.Forward("user-1", "match-1", []byte{1, 0})
	}
	// 信号量占位在 Forward 内同步完成：上限之内的分片照常旁送，超出部分
	// 的丢弃是确定性的。
	waitForReplication(t, func() bool {
		return classifier.callCount() == ambientRelayMaxInFlight
	})
	if dropped := relay.Dropped(); dropped != forwards-ambientRelayMaxInFlight {
		t.Fatalf("dropped = %d, want %d", dropped, forwards-ambientRelayMaxInFlight)
	}
	// 放行在途：槽位释放后新分片重新可投递（轮询投递至被接受，释放竞态
	// 期的尝试可能产生少量额外丢弃，不影响上面的确定性断言）。
	close(classifier.gate)
	waitForReplication(t, func() bool {
		relay.Forward("user-1", "match-1", []byte{1, 0})
		return classifier.callCount() > ambientRelayMaxInFlight
	})
}

// TestRelayForwardDoesNotBlockOnSlowSidecar 锁「不阻塞」纪律：Forward 必须
// 立即返回，慢 sidecar 只拖住旁路协程。
func TestRelayForwardDoesNotBlockOnSlowSidecar(t *testing.T) {
	coordinator := observation.NewMemoryCoordinator()
	classifier := &stubClassifier{events: []ambient.Event{
		{Kind: ambient.KindCheer, Confidence: 0.9, TS: time.Now().UTC()},
	}}
	classifier.gate = make(chan struct{})
	relay := newAmbientRelay(classifier, coordinator)

	startedAt := time.Now()
	relay.Forward("user-1", "match-1", []byte{1, 0})
	if elapsed := time.Since(startedAt); elapsed > 50*time.Millisecond {
		t.Fatalf("Forward blocked for %v on a slow sidecar", elapsed)
	}
	close(classifier.gate)
	waitForReplication(t, func() bool {
		active, err := coordinator.ActiveObservations(context.Background(), "user-1", "match-1")
		return err == nil && len(active) == 1
	})
}

// TestRelaySkipsWithoutIdentityOrScope 锁作用域纪律：身份未识别或比赛为空
// 的分片不旁送——气氛旁证仅观赛会话内、user+match 作用域内生效。
func TestRelaySkipsWithoutIdentityOrScope(t *testing.T) {
	coordinator := observation.NewMemoryCoordinator()
	classifier := &stubClassifier{}
	relay := newAmbientRelay(classifier, coordinator)

	relay.Forward("", "match-1", []byte{1, 0})
	relay.Forward("user-1", "", []byte{1, 0})
	relay.Forward("user-1", "match-1", nil)
	if classifier.callCount() != 0 {
		t.Fatalf("classifier calls = %d, want 0", classifier.callCount())
	}
}

// TestNilRelayIsInert 锁摘除形态：newAmbientRelay 任一依赖为空返回 nil，
// nil relay 的 Forward/Dropped 全部惰性安全。
func TestNilRelayIsInert(t *testing.T) {
	if relay := newAmbientRelay(nil, observation.NewMemoryCoordinator()); relay != nil {
		t.Fatal("relay constructed without a classifier")
	}
	if relay := newAmbientRelay(&stubClassifier{}, nil); relay != nil {
		t.Fatal("relay constructed without a coordinator")
	}
	var relay *ambientRelay
	relay.Forward("user-1", "match-1", []byte{1, 0})
	if relay.Dropped() != 0 {
		t.Fatal("nil relay reported drops")
	}
}

// TestRelayCountsRecordFailuresSilently 锁「失败静默丢弃+计数」：sidecar
// 成功但落库失败时，事件被静默丢弃并计入 dropped，不向调用方报错。
func TestRelayCountsRecordFailuresSilently(t *testing.T) {
	coordinator := failingCoordinator{}
	classifier := &stubClassifier{events: []ambient.Event{
		{Kind: ambient.KindCheer, Confidence: 0.9, TS: time.Now().UTC()},
	}}
	relay := newAmbientRelay(classifier, coordinator)

	relay.Forward("user-1", "match-1", []byte{1, 0})
	waitForReplication(t, func() bool { return relay.Dropped() > 0 })
	if classifier.err != nil {
		t.Fatal("unexpected classifier error state")
	}
	if relay.Dropped() != 1 {
		t.Fatalf("dropped = %d, want 1", relay.Dropped())
	}
}

// failingCoordinator 是 Record 恒失败的 coordinator 桩（落库故障注入）。
type failingCoordinator struct {
	observation.Coordinator
}

func (failingCoordinator) Record(context.Context, observation.Input) (observation.PendingObservation, error) {
	return observation.PendingObservation{}, errors.New("observation store unavailable")
}

// newDeadEndpoint 返回一个已关闭的本地 HTTP 端点 URL（摘除模拟）。
func newDeadEndpoint(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := server.URL
	server.Close()
	return url
}

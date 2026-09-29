package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"qiuqiu/internal/companion"
	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/useraffect"
)

type stubAffectClassifier struct {
	signal useraffect.Signal
	err    error
}

func (s *stubAffectClassifier) Classify(ctx context.Context, pcm []byte) (useraffect.Signal, error) {
	return s.signal, s.err
}

// attachRecorder 捕获 relay 的事后合并调用（真实 agent 依赖重，测试用最小替身）。
type attachRecorder struct {
	patches []companion.VoiceObservationPatch
}

func (a *attachRecorder) AttachVoiceStages(ctx context.Context, matchID, traceID string, patch companion.VoiceObservationPatch) error {
	a.patches = append(a.patches, patch)
	return nil
}

func waitForAffect(t *testing.T, probe func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if probe() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for user affect relay")
}

// TestUserAffectNeverEntersTheFactLedger 是 tripwire（ADR-0002，与
// TestAmbientEventsNeverEnterTheFactLedger 同规格）：用户语音情绪只走
// AttachVoiceStages 观测合并通道——本测试锁「旁路现状不触碰事实账本」这
// 一构造性为真的事实，若未来有人把情绪结论接进 matchstate 则在此爆炸。
func TestUserAffectNeverEntersTheFactLedger(t *testing.T) {
	factStore := matchstate.NewStore()
	recorder := &attachRecorder{}
	relay := newUserAffectRelayForTest(&stubAffectClassifier{signal: useraffect.Signal{Label: "excited", Confidence: 0.9}}, recorder, "match-1")

	relay.Forward("user-1", "signal-1", []byte{1, 0, 2, 0})
	waitForAffect(t, func() bool { return len(recorder.patches) == 1 })

	// 负例断言：事实账本零新行、公开快照比分未动。
	if events := factStore.Events("match-1"); len(events) != 0 {
		t.Fatalf("user affect leaked into the fact ledger: %+v", events)
	}
	projection, err := factStore.Replay("match-1", 0)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if projection.LastSequence != 0 || len(projection.PublicEvents) != 0 {
		t.Fatalf("fact ledger projection advanced: %+v", projection)
	}
	if snapshot := factStore.PublicSnapshot("match-1"); snapshot.Score != (matchstate.Score{}) {
		t.Fatalf("public score moved from user affect injection: %+v", snapshot.Score)
	}
	// 唯一落点是观测合并通道，载荷只带情绪信号。
	patch := recorder.patches[0]
	if patch.UserAffect == nil || patch.UserAffect.Label != "excited" {
		t.Fatalf("unexpected patch: %+v", patch)
	}
}

func TestUserAffectRelayBindOrderIndependence(t *testing.T) {
	recorder := &attachRecorder{}

	// 挂点先到：登记 traceID 等结论，结论回来事后合并。
	relay := newUserAffectRelayForTest(&stubAffectClassifier{signal: useraffect.Signal{Label: "low", Confidence: 0.8}}, recorder, "match-1")
	relay.bind("signal-1", "trace-1")
	relay.Forward("user-1", "signal-1", []byte{1, 0})
	waitForAffect(t, func() bool { return len(recorder.patches) == 1 })

	// 结论先到：暂存，挂点即时取走。
	relay2 := newUserAffectRelayForTest(&stubAffectClassifier{signal: useraffect.Signal{Label: "calm", Confidence: 0.7}}, recorder, "match-1")
	relay2.Forward("user-1", "signal-2", []byte{1, 0})
	waitForAffect(t, func() bool { return relay2.Dropped() == 0 && len(relay2.signals) == 1 })
	relay2.bind("signal-2", "trace-2")
	waitForAffect(t, func() bool { return len(recorder.patches) == 2 })

	if recorder.patches[0].UserAffect.Label != "low" || recorder.patches[1].UserAffect.Label != "calm" {
		t.Fatalf("unexpected labels: %+v", recorder.patches)
	}
}

func TestUserAffectRelayConfidenceGate(t *testing.T) {
	recorder := &attachRecorder{}
	relay := newUserAffectRelayForTest(&stubAffectClassifier{signal: useraffect.Signal{Label: "noisy", Confidence: 0.3}}, recorder, "match-1")
	relay.Forward("user-1", "signal-1", []byte{1, 0})
	waitForAffect(t, func() bool { return relay.Dropped() > 0 })
	if len(recorder.patches) != 0 {
		t.Fatalf("below-gate signal must not attach: %+v", recorder.patches)
	}
}

func TestUserAffectRelaySilentOnSidecarFailure(t *testing.T) {
	recorder := &attachRecorder{}
	relay := newUserAffectRelayForTest(&stubAffectClassifier{err: errors.New("sidecar down")}, recorder, "match-1")
	relay.bind("signal-1", "trace-1")
	relay.Forward("user-1", "signal-1", []byte{1, 0})
	waitForAffect(t, func() bool { return relay.Dropped() > 0 })
	if len(recorder.patches) != 0 {
		t.Fatalf("failed classification must not attach: %+v", recorder.patches)
	}
}

// newUserAffectRelayForTest 用 attachRecorder 替代真实 agent 构造 relay。
func newUserAffectRelayForTest(classifier userAffectClassifier, recorder *attachRecorder, matchID string) *useraffectRelay {
	return newUserAffectRelay(classifier, recorder, matchID, 0.55)
}

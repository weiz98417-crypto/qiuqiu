package companion

import (
	"context"
	"testing"
)

// AttachVoiceStages 是延迟分解/轮次决策/TTS 元数据的事后合并通道
// （operations-turn-replay）：WS 路径的 trace 在打点时已落库，只能键级
// 合并——不覆盖 voice 里既有的其他字段，同键后到覆盖。
func TestAttachVoiceStagesMergesKeysWithoutClobbering(t *testing.T) {
	tools := NewStoreMemoryTools(nil)
	agent := &Agent{tools: tools}
	ctx := context.Background()
	base := Trace{ID: "trace-a", MatchID: "m1", UserID: "u1", Input: "你好", Intent: IntentSmalltalk}
	if err := agent.tools.WriteTrace(ctx, base); err != nil {
		t.Fatalf("seed trace: %v", err)
	}

	if err := agent.AttachVoiceStages(ctx, "m1", "trace-a", VoiceObservationPatch{
		Stages: map[string]int{"speech_received": 0, "asr_final": 320, "turn_decided": 980},
	}); err != nil {
		t.Fatalf("first attach: %v", err)
	}
	decision := &VoiceTurnDecision{Source: "model", QueryLatencyMS: 42}
	if err := agent.AttachVoiceStages(ctx, "m1", "trace-a", VoiceObservationPatch{
		Stages:       map[string]int{"audio_delivered": 2100},
		TurnDecision: decision,
		TTSMeta:      &VoiceTraceMetadata{TTSStatus: "ok", TTSMime: "audio/wav", TTSByteCount: 66000},
	}); err != nil {
		t.Fatalf("second attach: %v", err)
	}

	got, err := tools.GetTrace(ctx, "m1", "trace-a")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.Voice == nil {
		t.Fatal("voice meta missing after attach")
	}
	for stage, want := range map[string]int{
		"speech_received": 0, "asr_final": 320, "turn_decided": 980, "audio_delivered": 2100,
	} {
		if have := got.Voice.LatencyStages[stage]; have != want {
			t.Fatalf("stage %s = %d, want %d", stage, have, want)
		}
	}
	if got.Voice.TurnDecision != decision {
		t.Fatalf("turn decision = %+v", got.Voice.TurnDecision)
	}
	if got.Voice.TTSStatus != "ok" || got.Voice.TTSMime != "audio/wav" || got.Voice.TTSByteCount != 66000 {
		t.Fatalf("tts meta = %+v", got.Voice)
	}
	// 同键后到覆盖：重放 turn_decided 以新值生效，兄弟键不丢。
	if err := agent.AttachVoiceStages(ctx, "m1", "trace-a", VoiceObservationPatch{
		Stages: map[string]int{"turn_decided": 999},
	}); err != nil {
		t.Fatalf("overwrite attach: %v", err)
	}
	got, _ = tools.GetTrace(ctx, "m1", "trace-a")
	if got.Voice.LatencyStages["turn_decided"] != 999 {
		t.Fatalf("same-key overwrite failed: %d", got.Voice.LatencyStages["turn_decided"])
	}
	if got.Voice.LatencyStages["asr_final"] != 320 {
		t.Fatalf("sibling key lost on overwrite: %d", got.Voice.LatencyStages["asr_final"])
	}
}

func TestAttachVoiceStagesMissingTraceErrors(t *testing.T) {
	agent := &Agent{tools: NewStoreMemoryTools(nil)}
	if err := agent.AttachVoiceStages(context.Background(), "m1", "trace-none", VoiceObservationPatch{Stages: map[string]int{"asr_final": 1}}); err == nil {
		t.Fatal("missing trace must error so callers can count-and-drop")
	}
}

func TestAttachVoiceStagesNilReceiverAndEmptyPatch(t *testing.T) {
	var agent *Agent
	if err := agent.AttachVoiceStages(context.Background(), "m1", "t", VoiceObservationPatch{Stages: map[string]int{"x": 1}}); err != nil {
		t.Fatalf("nil agent must no-op, got %v", err)
	}
	live := &Agent{tools: NewStoreMemoryTools(nil)}
	if err := live.AttachVoiceStages(context.Background(), "m1", "t", VoiceObservationPatch{}); err != nil {
		t.Fatalf("empty patch must no-op, got %v", err)
	}
}

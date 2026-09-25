package main

import (
	"context"
	"strings"
	"testing"

	"qiuqiu/internal/companion"
	"qiuqiu/internal/relationship"
	"qiuqiu/internal/tts"
)

func TestSynthesizeReplyTurnsPresentationIntoMiMoPerformanceDirection(t *testing.T) {
	synthesizer := &instructionCapturingTTS{}
	presentation := relationship.PresentationPlan{
		VoiceStyle:  "low_disappointed",
		VoiceEnergy: 0.3,
		VoiceSpeed:  0.92,
	}

	if _, err := synthesizeReply(context.Background(), synthesizer, "……还是越了。白喊。", presentation, nil); err != nil {
		t.Fatalf("synthesizeReply: %v", err)
	}
	for _, expected := range []string{"甜美清亮", "失落", "稍慢", "自然停顿"} {
		if !strings.Contains(synthesizer.instruction, expected) {
			t.Fatalf("instruction %q does not contain %q", synthesizer.instruction, expected)
		}
	}
	if synthesizer.text != "……还是越了。白喊。" {
		t.Fatalf("spoken text = %q", synthesizer.text)
	}
	if synthesizer.opts.Voice != "" || synthesizer.opts.Format != "" {
		t.Fatalf("voice/format must fall through to adapter defaults, got %+v", synthesizer.opts)
	}
}

// 情绪状态与回合动作必须一起进指令：兴奋档 × recall 的指令段与既有数值
// 档方向（excited/energy/speed）在同一句指令里并存；紧凑尾巴只落在短反应
// 类动作上——recall 即使短句也走全句；姿态只取首个非 silence 动作（tease
// 不改变 recall 的指令段）。
func TestSynthesizeReplyCarriesAffectAndActInstruction(t *testing.T) {
	synthesizer := &instructionCapturingTTS{}
	presentation := relationship.PresentationPlan{
		Affect:      relationship.AffectState{Valence: 0.8, Arousal: 1, Confidence: 0.9, Engagement: 0.8},
		VoiceStyle:  "excited",
		VoiceEnergy: 0.9,
		VoiceSpeed:  1.05,
	}

	if _, err := synthesizeReply(context.Background(), synthesizer, "进了进了！", presentation, []relationship.CommunicationAct{relationship.ActRecall, relationship.ActTease}); err != nil {
		t.Fatalf("synthesizeReply: %v", err)
	}
	for _, expected := range []string{"像忍不住又提了一遍进球", "兴奋还没退", "兴奋和即时反应感", "能量偏高", "语速稍快"} {
		if !strings.Contains(synthesizer.instruction, expected) {
			t.Fatalf("instruction %q does not contain %q", synthesizer.instruction, expected)
		}
	}
	// recall 是完整语义动作：短句不吃紧凑尾巴。
	if strings.Contains(synthesizer.instruction, "反应一闪而过") {
		t.Fatalf("recall must keep the full sentence on short utterances: %q", synthesizer.instruction)
	}
	if strings.Contains(synthesizer.instruction, "笑意压不住") {
		t.Fatalf("instruction must follow the primary act only: %q", synthesizer.instruction)
	}
	// 短反应类动作（react 类）：短句追加紧凑尾巴。
	if _, err := synthesizeReply(context.Background(), synthesizer, "进了进了！", presentation, []relationship.CommunicationAct{relationship.ActReact}); err != nil {
		t.Fatalf("synthesizeReply react: %v", err)
	}
	if !strings.Contains(synthesizer.instruction, "反应一闪而过") {
		t.Fatalf("react short utterance must append the compact tail: %q", synthesizer.instruction)
	}
}

func TestPrimaryActPicksFirstNonSilence(t *testing.T) {
	cases := []struct {
		acts []relationship.CommunicationAct
		want relationship.CommunicationAct
	}{
		{nil, relationship.ActReact},
		{[]relationship.CommunicationAct{relationship.ActSilence}, relationship.ActReact},
		{[]relationship.CommunicationAct{relationship.ActSilence, relationship.ActRecall, relationship.ActTease}, relationship.ActRecall},
		{[]relationship.CommunicationAct{relationship.ActTease, relationship.ActRecall}, relationship.ActTease},
	}
	for _, testCase := range cases {
		if got := primaryAct(testCase.acts); got != testCase.want {
			t.Fatalf("primaryAct(%v) = %v, want %v", testCase.acts, got, testCase.want)
		}
	}
}

func TestTurnActsWithoutDecisionIsEmpty(t *testing.T) {
	if acts := turnActs(companion.Trace{}); acts != nil {
		t.Fatalf("turnActs without decision = %v, want nil", acts)
	}
	decision := &relationship.Decision{Actions: []relationship.CommunicationAct{relationship.ActReact}}
	if acts := turnActs(companion.Trace{RelationshipDecision: decision}); len(acts) != 1 {
		t.Fatalf("turnActs with decision = %v, want the decision actions", acts)
	}
}

type instructionCapturingTTS struct {
	text        string
	instruction string
	opts        tts.VoiceOpts
}

func (s *instructionCapturingTTS) Synthesize(_ context.Context, text string, opts tts.VoiceOpts) (*tts.SynthesizeResult, error) {
	s.text = text
	s.instruction = opts.Instruction
	s.opts = opts
	return &tts.SynthesizeResult{AudioData: []byte("audio"), MimeType: "audio/wav"}, nil
}

func (s *instructionCapturingTTS) SynthesizeStream(context.Context, string, tts.VoiceOpts) (<-chan []byte, error) {
	return nil, tts.ErrNotSupported
}

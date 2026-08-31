package main

import (
	"context"
	"strings"
	"testing"

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

	if _, err := synthesizeReply(context.Background(), synthesizer, "……还是越了。白喊。", "", presentation); err != nil {
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
}

type instructionCapturingTTS struct {
	text        string
	instruction string
}

func (s *instructionCapturingTTS) Synthesize(context.Context, string, string) (*tts.SynthesizeResult, error) {
	return nil, nil
}

func (s *instructionCapturingTTS) SynthesizeWithInstruction(_ context.Context, text, voiceID, instruction string) (*tts.SynthesizeResult, error) {
	s.text = text
	s.instruction = instruction
	return &tts.SynthesizeResult{AudioData: []byte("audio"), MimeType: "audio/wav"}, nil
}

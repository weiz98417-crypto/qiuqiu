package pipeline

import (
	"context"
	"testing"
	"time"

	"qiuqiu/internal/event"
)

func TestEvalAIPipelineFallsBackWithoutTextLLM(t *testing.T) {
	pipe := NewAIPipeline(nil, nil, NewPromptManager())
	inst := &AIGenerationInstruction{
		Event: &event.StandardEvent{
			Type:      "goal",
			Team:      "home",
			Minute:    24,
			Timestamp: time.Date(2026, 6, 18, 20, 0, 0, 0, time.UTC),
		},
		Expression: "celebrate",
	}

	result := pipe.Process(context.Background(), inst)
	if result.Text == "" || !result.FallbackUsed {
		t.Fatalf("expected deterministic fallback without LLM, got %+v", result)
	}
}

package pipeline

import (
	"context"
	"fmt"
	"log"
	"qiuqiu/internal/event"
	"qiuqiu/internal/llm"
	"qiuqiu/internal/tts"
	"time"
)

// AIPipeline orchestrates LLM generation + TTS synthesis with fallback chain.
type AIPipeline struct {
	llmClient   *llm.Client
	ttsClient   *tts.Client
	promptMgr   *PromptManager
	recent      *RecentPhrases
	fallbackLLM *llm.Client
}

func NewAIPipeline(llmClient *llm.Client, ttsClient *tts.Client, promptMgr *PromptManager) *AIPipeline {
	return &AIPipeline{
		llmClient: llmClient,
		ttsClient: ttsClient,
		promptMgr: promptMgr,
		recent:    NewRecentPhrases(10),
	}
}

func (p *AIPipeline) SetFallbackLLM(client *llm.Client) {
	p.fallbackLLM = client
}

type AIPipelineOutput struct {
	Text         string        `json:"text"`
	AudioData    []byte        `json:"audio,omitempty"`
	Expression   string        `json:"expression"`
	LLMDuration  time.Duration `json:"llm_latency_ms"`
	TTSDuration  time.Duration `json:"tts_latency_ms"`
	FallbackUsed bool          `json:"fallback"`
	Error        error         `json:"-"`
}

// Process handles a single instruction through the full pipeline.
func (p *AIPipeline) Process(ctx context.Context, inst *AIGenerationInstruction) *AIPipelineOutput {
	output := &AIPipelineOutput{Expression: inst.Expression}

	// 1. Build messages from the event
	messages, err := p.promptMgr.BuildPrompt(inst, UserContext{})
	if err != nil {
		output.Text = templateFallback(inst.Event)
		output.FallbackUsed = true
		return output
	}

	// Convert to LLM client messages
	llmMsgs := make([]llm.Message, len(messages))
	for i, m := range messages {
		llmMsgs[i] = llm.Message{Role: m.Role, Content: m.Content}
	}

	// 2. LLM call with retries for similarity
	var text string
	temp := 0.7
	for retry := 0; retry < 3; retry++ {
		result, err := p.primaryLLM(ctx, llmMsgs, temp)
		if err != nil {
			log.Printf("ai-pipeline: LLM error: %v", err)
			text = templateFallback(inst.Event)
			output.FallbackUsed = true
			break
		}
		output.LLMDuration = result.Duration

		// 3. Validate
		valid, reason := ValidateLLMOutput(result.Text)
		if !valid {
			if reason == "safety" {
				text = templateFallback(inst.Event)
				output.FallbackUsed = true
				break
			}
			temp += 0.15
			continue
		}

		// 4. Similarity check
		if p.recent.IsSimilar(result.Text) {
			temp += 0.15
			continue
		}

		// 5. Sanitize
		text = Sanitize(result.Text)
		break
	}

	if text == "" {
		text = templateFallback(inst.Event)
		output.FallbackUsed = true
	}

	output.Text = text

	// 6. TTS
	if p.ttsClient != nil {
		ttsResult, err := p.ttsClient.Synthesize(ctx, text, "EXAVITQu4vr4xnSDxMaL")
		if err != nil {
			output.Error = fmt.Errorf("tts: %w", err)
		} else {
			output.AudioData = ttsResult.AudioData
			output.TTSDuration = ttsResult.Duration
		}
	}

	return output
}

func (p *AIPipeline) primaryLLM(ctx context.Context, messages []llm.Message, temp float64) (*llm.GenerateResult, error) {
	result, err := p.llmClient.GenerateWithMessages(ctx, messages, temp)
	if err != nil && p.fallbackLLM != nil {
		log.Printf("ai-pipeline: Deepseek failed, falling back to Ollama")
		return p.fallbackLLM.GenerateWithMessages(ctx, messages, temp)
	}
	return result, err
}

func templateFallback(ev *event.StandardEvent) string {
	switch ev.Type {
	case "goal", "penalty":
		return "球进了！！"
	case "shot":
		return "好球！差一点！"
	case "yellow_card":
		return "吃牌了。"
	case "red_card":
		return "红牌！这下麻烦了。"
	case "match_start":
		return "比赛开始了！一起看吧～"
	case "match_end":
		return "比赛结束了。"
	default:
		return "嗯！"
	}
}

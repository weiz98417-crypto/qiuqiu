package pipeline

import (
	"context"
	"fmt"
	"log"
	"qiuqiu/internal/event"
	"qiuqiu/internal/llm"
	"qiuqiu/internal/tts"
	"strings"
	"sync"
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

type AIGenerationInstruction struct {
	InstructionType string
	Event           *event.StandardEvent
	MatchContext    *EnrichedContext
	Expression      string
	Priority        int
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
	AudioMIME    string        `json:"audio_mime,omitempty"`
	Expression   string        `json:"expression"`
	LLMDuration  time.Duration `json:"llm_latency_ms"`
	TTSDuration  time.Duration `json:"tts_latency_ms"`
	FirstByteAt  time.Duration `json:"first_byte_ms"` // streaming: first sentence→TTS
	FallbackUsed bool          `json:"fallback"`
	Error        error         `json:"-"`
}

// ProcessStreaming uses streaming LLM + sentence-split TTS for lower perceived latency.
func (p *AIPipeline) ProcessStreaming(ctx context.Context, inst *AIGenerationInstruction) *AIPipelineOutput {
	output := &AIPipelineOutput{Expression: inst.Expression}
	if p.llmClient == nil {
		output.Text = templateFallback(inst.Event)
		output.FallbackUsed = true
		return output
	}
	messages, err := p.promptMgr.BuildPrompt(inst, UserContext{})
	if err != nil {
		output.Text = templateFallback(inst.Event)
		output.FallbackUsed = true
		return output
	}
	llmMsgs := make([]llm.Message, len(messages))
	for i, m := range messages {
		llmMsgs[i] = llm.Message{Role: m.Role, Content: m.Content}
	}

	splitter := NewSentenceSplitter()
	var fullText strings.Builder
	start := time.Now()
	firstSent := false

	for chunk := range p.llmClient.StreamWithMessages(ctx, llmMsgs, 0.7) {
		if chunk.Done {
			break
		}
		fullText.WriteString(chunk.Text)
		if sentence := splitter.Feed(chunk.Text); sentence != "" {
			if valid, _ := ValidateLLMOutput(sentence); valid {
				sentence = Sanitize(sentence)
				if !firstSent {
					firstSent = true
					output.FirstByteAt = time.Since(start)
					output.Text = sentence
				}
				if p.ttsClient != nil {
					ttsResult, err := p.ttsClient.Synthesize(ctx, sentence, "cgSgspJ2msm6clMCkdW9")
					if err == nil {
						output.AudioData = ttsResult.AudioData
						output.AudioMIME = ttsResult.MimeType
						output.TTSDuration = ttsResult.Duration
					}
				}
				output.LLMDuration = time.Since(start)
				return output // Return after first sentence for streaming
			}
		}
	}
	// Flush remaining
	if remaining := splitter.FlushAll(); remaining != "" && output.Text == "" {
		output.Text = remaining
		output.FirstByteAt = time.Since(start)
		if p.ttsClient != nil {
			ttsResult, err := p.ttsClient.Synthesize(ctx, remaining, "cgSgspJ2msm6clMCkdW9")
			if err == nil {
				output.AudioData = ttsResult.AudioData
				output.AudioMIME = ttsResult.MimeType
				output.TTSDuration = ttsResult.Duration
			}
		}
	}
	if output.Text == "" {
		output.Text = fullText.String()
	}
	return output
}

// Process handles a single instruction through the full pipeline.
func (p *AIPipeline) Process(ctx context.Context, inst *AIGenerationInstruction) *AIPipelineOutput {
	output := &AIPipelineOutput{Expression: inst.Expression}
	if p.llmClient == nil {
		output.Text = templateFallback(inst.Event)
		output.FallbackUsed = true
		return output
	}

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
		ttsResult, err := p.ttsClient.Synthesize(ctx, text, "cgSgspJ2msm6clMCkdW9")
		if err != nil {
			output.Error = fmt.Errorf("tts: %w", err)
		} else {
			output.AudioData = ttsResult.AudioData
			output.AudioMIME = ttsResult.MimeType
			output.TTSDuration = ttsResult.Duration
		}
	}

	return output
}

func (p *AIPipeline) primaryLLM(ctx context.Context, messages []llm.Message, temp float64) (*llm.GenerateResult, error) {
	result, err := p.llmClient.GenerateWithMessages(ctx, messages, temp)
	if err != nil && p.fallbackLLM != nil {
		log.Printf("ai-pipeline: MiMo failed, falling back to Ollama")
		return p.fallbackLLM.GenerateWithMessages(ctx, messages, temp)
	}
	return result, err
}

var fallbackPool = map[string][]string{
	"goal":         {"球进了！！", "进了！漂亮！", "这球太关键了！", "门将毫无办法！", "关键时刻站出来了！"},
	"penalty":      {"点球！球进了！", "稳稳罚进！"},
	"shot":         {"好球！差一点！", "这脚有威胁！", "射门！", "可惜了！", "差之毫厘！"},
	"yellow_card":  {"吃牌了。", "黄牌，这动作没必要。", "领到黄牌了。"},
	"red_card":     {"红牌！！这下麻烦了！", "直接红牌！", "被罚下了！"},
	"match_start":  {"比赛开始了！一起看吧～", "开球了！今天这场比赛有看头！"},
	"match_end":    {"比赛结束了！", "全场比赛结束！"},
	"foul":         {"犯规了。", "这动作有点大。"},
	"corner":       {"角球！", "角球机会！"},
	"offside":      {"越位了。", "边裁举旗了。"},
	"substitution": {"换人了。", "换人调整。"},
	"var_check":    {"VAR在检查...", "裁判去看回放了。"},
}

var fallbackIdx = make(map[string]int)
var fallbackMu sync.Mutex

func templateFallback(ev *event.StandardEvent) string {
	pool, ok := fallbackPool[ev.Type]
	if !ok {
		return "嗯！"
	}
	fallbackMu.Lock()
	idx := fallbackIdx[ev.Type]
	fallbackIdx[ev.Type] = (idx + 1) % len(pool)
	fallbackMu.Unlock()
	return pool[idx]
}

package journal

// LLM 织写（memory-scoring 的 structured seam 之外的自由文本路：与
// companion realizer 同形走 llm.Client）。约束进提示（第一人称、只许引
// 用所给事实、比分原样、≤3 句），确定性后校验（validateBody）在
// Generate——LLM 幻觉比分必被降级底稿，零编造不靠模型自觉。

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"qiuqiu/internal/llm"
)

// LLMGenerator 是 TextGenerator 的生产实现。
type LLMGenerator struct {
	client *llm.Client
}

// NewLLMGenerator 装配；client 为 nil 时 Generate 返回错误（调用方降级
// 确定性底稿）。
func NewLLMGenerator(client *llm.Client) *LLMGenerator {
	return &LLMGenerator{client: client}
}

const journalSystemPrompt = `你是陪看足球助手"球球"，在每场看完的比赛后写一篇第一人称手记（「我」=球球，写给这位一起看球的用户）。纪律：
- 只允许使用给出的事实（队名、比分、进球者与时间、用户画像条目），一个字都不许编造——尤其是比分，必须与给出的一致，没有给出的事实不许提。
- 短文 1-3 句，像熟悉的朋友的赛后留言，可以有情绪但不播音、不客服。
- 若给出画像条目，可以自然地呼应一次（例：用户支持的队赢了/输了）。
- 不许提问，不许邀约下一场，不许提及未给出的任何比赛或球员。`

// GenerateJournal 织写手记正文。
func (g *LLMGenerator) GenerateJournal(ctx context.Context, facts MatchFacts, portrait []PortraitLine) (string, error) {
	if g == nil || g.client == nil {
		return "", ErrNotSupportedGenerator
	}
	var factsBuilder strings.Builder
	fmt.Fprintf(&factsBuilder, "比赛：%s 对 %s\n终场比分：%s\n", facts.HomeTeam, facts.AwayTeam, facts.Score)
	if len(facts.Goals) > 0 {
		factsBuilder.WriteString("进球：" + strings.Join(facts.Goals, "、") + "\n")
	}
	if len(portrait) > 0 {
		var lines strings.Builder
		for _, line := range portrait {
			fmt.Fprintf(&lines, "- %s：%s\n", line.SubTopic, line.Content)
		}
		factsBuilder.WriteString("用户画像（可自然呼应，不许复述为比赛事实）：\n" + lines.String())
	}
	result, err := g.client.GenerateWithMessagesLimit(ctx, []llm.Message{
		{Role: "system", Content: journalSystemPrompt},
		{Role: "user", Content: factsBuilder.String()},
	}, 0.6, 220)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(result.Text), nil
}

// ErrNotSupportedGenerator 表示生成器缺席（nil client/测试桩）。
var ErrNotSupportedGenerator = errors.New("journal generator is not configured")

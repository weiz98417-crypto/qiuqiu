package companion

import (
	"context"
	"fmt"
	"strings"

	"qiuqiu/internal/llm"
)

type LLMReplyPolisher struct {
	client *llm.Client
}

func NewLLMReplyPolisher(client *llm.Client) *LLMReplyPolisher {
	if client == nil {
		return nil
	}
	return &LLMReplyPolisher{client: client}
}

func (p *LLMReplyPolisher) Polish(ctx context.Context, req PolishRequest) (string, error) {
	if p == nil || p.client == nil {
		return "", fmt.Errorf("llm polisher unavailable")
	}
	messages := []llm.Message{
		{
			Role: "system",
			Content: strings.TrimSpace(`你是球球的回复润色层，只能把给定回复改得更自然。
不能新增、删除或改写任何比赛事实。
必须保留所有事实锚点，包括比分、时间、球员名、事件描述和参与人。
输出一句中文口语化短回复，不要解释规则。`),
		},
		{
			Role: "user",
			Content: fmt.Sprintf("用户问题：%s\n意图：%s\n必须保留的事实锚点：%s\n原始可靠回复：%s",
				req.Input,
				req.Intent,
				strings.Join(req.RequiredAnchors, "；"),
				req.DeterministicReply,
			),
		},
	}
	result, err := p.client.GenerateWithMessages(ctx, messages, 0.2)
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(result.Text)
	if text == "" {
		return "", fmt.Errorf("llm polish returned empty output")
	}
	return text, nil
}

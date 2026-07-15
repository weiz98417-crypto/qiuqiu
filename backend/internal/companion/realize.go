package companion

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"qiuqiu/internal/llm"
	"qiuqiu/internal/relationship"
)

type LLMReplyRealizer struct {
	client *llm.Client
}

func NewLLMReplyRealizer(client *llm.Client) *LLMReplyRealizer {
	if client == nil {
		return nil
	}
	return &LLMReplyRealizer{client: client}
}

func (realizer *LLMReplyRealizer) Realize(ctx context.Context, req RealizationRequest) (RealizedTurn, error) {
	if realizer == nil || realizer.client == nil {
		return RealizedTurn{}, fmt.Errorf("reply realizer unavailable")
	}
	if req.Decision.Speech == nil {
		return RealizedTurn{}, fmt.Errorf("reply realizer received a silent decision")
	}
	content := req.Decision.Speech.Content
	messages := []llm.Message{
		{
			Role: "system",
			Content: strings.TrimSpace(`你是球球的语言实现层，只负责把已经决定好的沟通动作说成自然中文。
球球是一起看足球的数字球友，不是客服、解说员、治疗师或恋爱伴侣。
不能改变沟通动作、关系阶段、事实、不确定性、边界、调侃许可或是否追问。
不要使用服务腔，不复述用户原话，不固定采用“接情绪+分析+反问”，不要每轮都提问。
禁止恋爱化、排他化、依赖性表达，禁止编造人类生活经历，禁止提及模型、后台、导播台或内部记录。
只在当前话题确实相关时自然带过给出的共同上下文，不炫耀记忆能力，不解释来源。
沟通动作包含 recall 且给出未完话题时，必须在台词里明确说出该话题。
只输出最终台词，不解释规则。`),
		},
		{
			Role: "user",
			Content: fmt.Sprintf(`用户原话：%s
意图：%s
沟通动作：%s
关系阶段：%s
修复类别：%s
可自然引用：%s
表达目标：%s
可靠底稿：%s
必须保留：%s
禁止新增：%s
禁止触碰的话题：%s
最多句数：%d
最多字数：%d
允许问句：%t
分析深度：%s
调侃范围：%s
口语强度：%s
称呼：%s`,
				req.UserInput,
				req.Intent,
				joinActions(req.Decision.Actions),
				req.Decision.Relationship.Stage,
				req.Decision.Relationship.RepairCategory,
				formatRelationshipMemories(req.Decision.Memories),
				content.Goal,
				req.ReliableText,
				strings.Join(content.RequiredAnchors, "；"),
				strings.Join(content.ForbiddenClaims, "；"),
				strings.Join(content.ForbiddenTopics, "；"),
				content.MaxSentences,
				content.MaxCharacters,
				content.QuestionAllowed,
				content.AnalysisDepth,
				content.BanterScope,
				content.ProfanityLevel,
				content.Addressing,
			),
		},
	}
	result, err := realizer.client.GenerateWithMessages(ctx, messages, 0.55)
	if err != nil {
		return RealizedTurn{}, err
	}
	text := strings.TrimSpace(result.Text)
	if text == "" {
		return RealizedTurn{}, fmt.Errorf("reply realizer returned empty output")
	}
	return RealizedTurn{Text: text}, nil
}

func formatRelationshipMemories(memories []relationship.RelationshipMemory) string {
	formatted := make([]string, 0, 2)
	for _, memory := range memories {
		if len(formatted) == 2 {
			break
		}
		switch memory.Kind {
		case relationship.MemoryKindTasteEvidence:
			var payload relationship.TasteMemoryPayload
			if json.Unmarshal(memory.Payload, &payload) == nil && payload.Subject != "" {
				prefix := "偏好"
				if payload.Direction == "dislike" {
					prefix = "不喜欢"
				}
				formatted = append(formatted, prefix+payload.Subject)
			}
		case relationship.MemoryKindOpenThread:
			var payload relationship.OpenThreadMemoryPayload
			if json.Unmarshal(memory.Payload, &payload) == nil && payload.Topic != "" {
				formatted = append(formatted, "未完话题"+payload.Topic)
			}
		case relationship.MemoryKindSharedMoment:
			var payload relationship.SharedMomentMemoryPayload
			if json.Unmarshal(memory.Payload, &payload) == nil && payload.Summary != "" {
				formatted = append(formatted, "共同瞬间"+payload.Summary)
			}
		}
	}
	if len(formatted) == 0 {
		return "无"
	}
	return strings.Join(formatted, "；")
}

func joinActions(actions []relationship.CommunicationAct) string {
	values := make([]string, 0, len(actions))
	for _, action := range actions {
		values = append(values, string(action))
	}
	return strings.Join(values, "+")
}

package companion

// 记忆进措辞层（openspec/changes/memory-into-turns）：Recall/Portrait 的
// 注入面从闲聊 realization 一个窄口扩大到主动回合、开线程回访与事实应答
// 补充语。共同的保守门（措辞层边界，决策层不动）：仅在 realizer 可用且
// recall 记忆材料非空时才尝试 realizer 化——无记忆材料时确定性/运营文本
// 原样出去。这同时是零漂移保障：evals 不接记忆种子，全部走原文路径。

import (
	"context"
	"strings"

	"qiuqiu/internal/relationship"
)

// realizeWithMemory 在记忆材料非空时把 reliable 文本交 realizer 带记忆
// 重新措辞：guard 通过才采用（true），超时/失败/拒绝一律原样返回（false）。
// input 是措辞与 guard 的原文；focus 是 recall 检索词——检索是双向
// contains 语义，必须传单个实体词（球员/球队名），传整段描述永远落空。
// 运营 ProactiveText 在这里是锚点（Q3 落定），不是被替换对象——reliable
// 喂给措辞目标，也进 guard 的锚源。
func (a *Agent) realizeWithMemory(ctx context.Context, intent Intent, userID, input, focus, reliable string, anchors []string, decision relationship.Decision, trace *Trace) (string, bool) {
	if a == nil || a.realizer == nil || decision.Speech == nil {
		return reliable, false
	}
	memoryContext := a.recallMemoryBlock(ctx, userID, focus, trace)
	if strings.TrimSpace(memoryContext) == "" {
		return reliable, false
	}
	portraitContext := a.portraitMemoryBlock(ctx, userID, trace)
	realizeCtx, cancel := context.WithTimeout(ctx, a.realizeTimeout)
	defer cancel()
	realized, err := a.realizer.Realize(realizeCtx, RealizationRequest{
		UserInput: input,
		Intent:    intent,
		Grounding: relationship.GroundedContent{
			Intent:          string(intent),
			ReliableText:    reliable,
			RequiredAnchors: append([]string(nil), anchors...),
			FactMode:        relationship.FactModeDeterministic,
		},
		Decision:        decision,
		MemoryContext:   memoryContext,
		PortraitContext: portraitContext,
		ReliableText:    reliable,
	})
	if err != nil {
		if trace != nil {
			trace.Error = strings.TrimSpace(err.Error())
		}
		return reliable, false
	}
	if strings.TrimSpace(realized.Text) == "" {
		return reliable, false
	}
	validated, _ := guardValidateReply(input, intent, realized.Text, anchors, reliable, decision)
	if validated == "" {
		return reliable, false
	}
	return validated, true
}

// factCallbackDecision 为事实应答的记忆衔接语合成最小 realize 决策载体：
// 沟通动作是 recall，一句为限、允许一个问句；不进 director 存储。
func factCallbackDecision() relationship.Decision {
	return relationship.Decision{
		Actions: []relationship.CommunicationAct{relationship.ActRecall},
		Speech: &relationship.SpeechPlan{
			Content: relationship.ContentPolicy{
				Goal:            "顺着长期记忆自然补一句衔接，不重复事实本身，不引入新赛况",
				MaxSentences:    1,
				QuestionAllowed: true,
				BanterScope:     "none",
			},
		},
	}
}

// appendFactMemoryCallback 在事实应答后追加一句记忆衔接。事实本体的措辞
// 仍归确定性路径所有（design decision 4）：衔接语是追加句，单独过 guard
// （一句为限、事实语言不得超出 锚点+原文+记忆材料 的来源集），拒绝即整句
// 丢弃、事实应答原样返回。
func (a *Agent) appendFactMemoryCallback(ctx context.Context, req AgentBoundaryRequest, intent Intent, reply string, anchors []string, trace *Trace) string {
	if a == nil || a.realizer == nil || trace == nil || !isFactIntent(intent) || strings.TrimSpace(reply) == "" {
		return reply
	}
	memoryContext := a.recallMemoryBlock(ctx, req.UserID, req.Text, trace)
	if strings.TrimSpace(memoryContext) == "" {
		return reply
	}
	portraitContext := a.portraitMemoryBlock(ctx, req.UserID, trace)
	realizeCtx, cancel := context.WithTimeout(ctx, a.realizeTimeout)
	defer cancel()
	decision := factCallbackDecision()
	realized, err := a.realizer.Realize(realizeCtx, RealizationRequest{
		UserInput: req.Text,
		Intent:    intent,
		Grounding: relationship.GroundedContent{
			Intent:          string(intent),
			ReliableText:    reply,
			RequiredAnchors: append([]string(nil), anchors...),
			FactMode:        relationship.FactModeAnchored,
		},
		Decision:        decision,
		MemoryContext:   memoryContext,
		PortraitContext: portraitContext,
		ReliableText:    reply,
	})
	if err != nil || strings.TrimSpace(realized.Text) == "" {
		return reply
	}
	// 锚源放宽到记忆材料：衔接语里的球员/球队来自 recall 是合法来源。
	tail, _ := guardValidateReply(req.Text, intent, realized.Text, anchors, reply+" "+memoryContext, decision)
	if tail == "" {
		return reply
	}
	trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "response.emit_companion_reply", Args: map[string]string{"mode": "fact_memory_callback"}})
	return reply + tail
}

// threadRecoveryDecision 为开线程回访措辞合成最小 realize 决策载体：回访
// 的沟通动作（recalling）本就是回访逻辑的确定性决定，不进 director 存储。
func threadRecoveryDecision() relationship.Decision {
	return relationship.Decision{
		Actions: []relationship.CommunicationAct{relationship.ActRecall},
		Speech: &relationship.SpeechPlan{
			Content: relationship.ContentPolicy{
				Goal:            "把之前没答完的话题自然接回来，保留底稿里的事实与答案",
				MaxSentences:    3,
				QuestionAllowed: true,
				BanterScope:     "none",
			},
		},
	}
}

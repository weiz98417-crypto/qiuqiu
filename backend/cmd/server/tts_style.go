package main

import (
	"context"
	"strings"

	"qiuqiu/internal/companion"
	"qiuqiu/internal/relationship"
	"qiuqiu/internal/tts"
)

// synthesizeReply 是两条语音回复链路（HTTP 语音会话与 WS 投递服务）共用
// 的合成入口：把表演计划与回合沟通动作折算成一条自然语言表演指令，经
// seam 的 VoiceOpts.Instruction 下发；没有风格通道的 adapter 自行忽略。
func synthesizeReply(ctx context.Context, synthesizer speechSynthesizer, text string, presentation relationship.PresentationPlan, acts []relationship.CommunicationAct) (*tts.SynthesizeResult, error) {
	return synthesizer.Synthesize(ctx, text, tts.VoiceOpts{
		Instruction: mimoPerformanceInstruction(presentation, acts, len([]rune(text))),
	})
}

// mimoPerformanceInstruction 拼装 Miimo 表演指令：基础人设恒定；情绪×动
// 作段由 tts.InstructionFor 给出（Affect State 来源是表演计划内嵌的该回
// 合情绪）；数值档（风格/能量/语速）继续由表演计划驱动——两段互补，前
// 者命名情绪与姿态，后者做数值微调。
func mimoPerformanceInstruction(presentation relationship.PresentationPlan, acts []relationship.CommunicationAct, utterLen int) string {
	parts := []string{
		"使用当前固定音色，以20岁左右中文女生的感觉表演。声线甜美清亮但不过度夹，像熟悉的朋友陪着看球。普通话口语自然，不要播音腔。句内要有自然快慢变化，陈述句句尾自然回落，保留轻微呼吸和自然停顿；激动时有爆发力但不要尖叫或破音。",
		tts.InstructionFor(presentation.Affect, primaryAct(acts), utterLen),
		voiceStyleDirection(presentation.VoiceStyle),
		voiceEnergyDirection(presentation.VoiceEnergy),
		voiceSpeedDirection(presentation.VoiceSpeed),
	}
	return strings.Join(parts, "")
}

// primaryAct 归一回合动作为语音姿态：取首个非 silence 的动作；无动作的
// 回合（如赛程查询）回落 react 的中性姿态。
func primaryAct(acts []relationship.CommunicationAct) relationship.CommunicationAct {
	for _, act := range acts {
		if act != "" && act != relationship.ActSilence {
			return act
		}
	}
	return relationship.ActReact
}

// turnActs 取该回合决策的沟通动作序列，供语音指令映射使用；无决策回合
// 返回 nil。
func turnActs(trace companion.Trace) []relationship.CommunicationAct {
	if trace.RelationshipDecision == nil {
		return nil
	}
	return trace.RelationshipDecision.Actions
}

func voiceStyleDirection(style string) string {
	switch strings.ToLower(strings.TrimSpace(style)) {
	case "excited":
		return "这一句带明显兴奋和即时反应感，重点词更鲜活，语调可以上扬但不要尖叫。"
	case "tense":
		return "这一句紧张而专注，语气略收紧，重点词清楚，但不要压迫或僵硬。"
	case "low_disappointed":
		return "这一句有失落感但保持克制，音量略低，句尾自然回落，不要刻意哭腔。"
	case "quiet", "soft":
		return "这一句轻柔、亲近，声音略收但不要耳语，保持真实交流感。"
	case "warm":
		return "这一句温暖亲切，像带着浅浅笑意和熟人说话。"
	default:
		return "这一句放松自然，像朋友之间随口交流。"
	}
}

func voiceEnergyDirection(energy float64) string {
	switch {
	case energy >= 0.75:
		return "整体能量偏高，重音更鲜明，但控制音量和音高，避免破音。"
	case energy > 0 && energy <= 0.35:
		return "整体能量偏低，减少用力感，但咬字仍然清楚。"
	default:
		return "整体保持中等能量，不要每个字都同样用力。"
	}
}

func voiceSpeedDirection(speed float64) string {
	switch {
	case speed > 0 && speed <= 0.96:
		return "整体语速稍慢，不拖字，保留自然停顿和句内节奏变化。"
	case speed >= 1.04:
		return "整体语速稍快，但不要赶字，仍要保留自然停顿和句内节奏变化。"
	default:
		return "整体使用自然语速，按语义形成快慢变化，不要匀速念稿。"
	}
}

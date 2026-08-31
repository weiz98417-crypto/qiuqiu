package main

import (
	"context"
	"strings"

	"qiuqiu/internal/relationship"
	"qiuqiu/internal/tts"
)

type instructedSpeechSynthesizer interface {
	SynthesizeWithInstruction(ctx context.Context, text, voiceID, instruction string) (*tts.SynthesizeResult, error)
}

func synthesizeReply(ctx context.Context, synthesizer speechSynthesizer, text, voiceID string, presentation relationship.PresentationPlan) (*tts.SynthesizeResult, error) {
	if instructed, ok := synthesizer.(instructedSpeechSynthesizer); ok {
		return instructed.SynthesizeWithInstruction(ctx, text, voiceID, mimoPerformanceInstruction(presentation))
	}
	return synthesizer.Synthesize(ctx, text, voiceID)
}

func mimoPerformanceInstruction(presentation relationship.PresentationPlan) string {
	parts := []string{
		"使用当前固定音色，以20岁左右中文女生的感觉表演。声线甜美清亮但不过度夹，像熟悉的朋友陪着看球。普通话口语自然，不要播音腔。句内要有自然快慢变化，陈述句句尾自然回落，保留轻微呼吸和自然停顿；激动时有爆发力但不要尖叫或破音。",
		voiceStyleDirection(presentation.VoiceStyle),
		voiceEnergyDirection(presentation.VoiceEnergy),
		voiceSpeedDirection(presentation.VoiceSpeed),
	}
	return strings.Join(parts, "")
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

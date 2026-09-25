package tts

import (
	"context"
	"errors"
)

// ErrNotSupported 表示 adapter 不具备该能力。流式位预留期间所有现役
// adapter 都返回它——调用方据此后退到整段 Synthesize。
var ErrNotSupported = errors.New("tts adapter does not support this capability")

// VoiceOpts 是一次合成的表现选项。全字段可零值：空值走 adapter 内的
// 供应商默认，调用方不需要知道任何供应商知识。
type VoiceOpts struct {
	// Instruction 是自然语言表演指令（情绪、语速、姿态），经供应商的
	// 风格通道下发；没有风格通道的 adapter 忽略它。
	Instruction string
	// Format 是音频容器格式；空 = adapter 默认（Miimo 为 wav）。
	Format string
	// Voice 是音色；空 = adapter 默认（Miimo 为冰糖）。
	Voice string
}

// Synthesizer 是 TTS Provider seam（ADR-0012 修订）：调用方只见此接口，
// 供应商知识（URL/model/voice/WAV 组包/熔断器）全部收敛在 adapter 内。
type Synthesizer interface {
	// Synthesize 整段合成：返回完整音频字节（Miimo 为解码后的 WAV）。
	Synthesize(ctx context.Context, text string, opts VoiceOpts) (*SynthesizeResult, error)
	// SynthesizeStream 为流式位预留（voice-transport-upgrade 决策后由真
	// 流式 adapter 实现）：现役 Miimo 是整段合成，不做假流式，现役
	// adapter 一律返回 ErrNotSupported；ch 只在错误为 nil 时有效。
	SynthesizeStream(ctx context.Context, text string, opts VoiceOpts) (ch <-chan []byte, err error)
}

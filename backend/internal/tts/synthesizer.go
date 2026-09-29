package tts

import (
	"context"
	"errors"
)

// ErrNotSupported 表示 adapter 不具备该能力。流式位已变现役
// （voice-streaming-delivery），现役 Client 不再返回它；测试替身仍以它
// 声明「流式能力缺席」，调用方据此后退到整段 Synthesize。
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
	// SynthesizeStream 流式合成（voice-streaming-delivery 起变现役）：
	// Miimo stream:true SSE 吃 pcm16@24kHz 裸分片（无 WAV 封装），按到达
	// 顺序投到 ch，合成结束（含回退）后关闭；SSE 断流/超时/非 200 时
	// adapter 内回退整段 Synthesize，全量音频作为单个分片投出，熔断沿用。
	// 需要分片元数据与降级标记的调用方（投递层 task 3.4/3.5）改用
	// StreamingSynthesizer 能力接口（stream.go），不要改本签名——cmd/server
	// 的测试替身以旧签名实现它，形状变更即全量波及。
	// ch 只在错误为 nil 时有效；起始性失败（未配置）同步返回 error。
	SynthesizeStream(ctx context.Context, text string, opts VoiceOpts) (ch <-chan []byte, err error)
}

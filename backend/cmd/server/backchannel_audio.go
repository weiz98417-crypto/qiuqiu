package main

import (
	"context"
	"strings"
	"time"

	"qiuqiu/internal/backchannel"
	"qiuqiu/internal/deliverykey"
	"qiuqiu/internal/relationship"
	"qiuqiu/internal/tts"
)

// backchannelTTSTimeout 是微反应短 TTS 的单次合成预算：≤10 字 one-shot，
// 超时即弃（ADR-0016：失败即弃，文字气泡已在）。
const backchannelTTSTimeout = 5 * time.Second

// backchannelAudioSink 是微反应音频下发需要的最小发送面（wsWriter 满足，
// 测试用假件替换）。SendAudio 在单锁内连发 metadata+binary，客户端 FIFO
// 队列按到达序配对，与主回合音频交错也安全。
type backchannelAudioSink interface {
	SendAudio(meta interface{}, data []byte) error
}

// backchannelInstruction 拼装微反应的表演指令：基础人设 + 情绪×react 段。
// 短语 ≤10 字必咬合 InstructionFor 的紧凑尾巴（一闪而过）；style/energy/
// speed 数值段不参与——微反应没有表演计划，情绪已由 affectByEvent 命名。
func backchannelInstruction(verdict backchannel.Verdict) string {
	return mimoBasePersona + tts.InstructionFor(verdict.Affect, relationship.ActReact, len([]rune(verdict.Phrase)))
}

// deliverBackchannelAudio 是微反应短 TTS 的同步内核（ADR-0016 v1.1）：
// one-shot 合成 + voice_audio 直发（deliveryKey=backchannel-<事件ID> 供
// 客户端按 deliveryKey 配对去重、source 标记通道；不带 traceId/eventId，
// 语音通道不进回合送达台账）。调用方以 goroutine 包装，不阻塞事件泵；
// nil 合成器（无 key 且未开 mock）是合法形态：v1 纯文字，静默跳过。
func deliverBackchannelAudio(ctx context.Context, synthesizer speechSynthesizer, sink backchannelAudioSink, verdict backchannel.Verdict, eventID string) error {
	if synthesizer == nil || sink == nil {
		return nil
	}
	result, err := synthesizer.Synthesize(ctx, verdict.Phrase, tts.VoiceOpts{Instruction: backchannelInstruction(verdict)})
	if err != nil {
		return err
	}
	mime := strings.TrimSpace(result.MimeType)
	if mime == "" {
		mime = "audio/wav"
	}
	return sink.SendAudio(map[string]interface{}{
		"type":        "voice_audio",
		"mime":        mime,
		"byteLength":  len(result.AudioData),
		"deliveryKey": deliverykey.ForBackchannel(eventID),
		"source":      "backchannel",
		// 短语只作字幕微标注(memory-surfacing 1.8):纯展示,不进对话流,
		// 无 traceId/eventId——不落回合送达台账(ADR-0016 纪律)。
		"text": verdict.Phrase,
	}, result.AudioData)
}

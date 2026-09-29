package main

import (
	"sync"

	"qiuqiu/internal/companion"
)

// voiceStageBuffers 是语音观测的 utterance 级缓冲（operations-turn-replay）：
// asr_final 与 turn_query 结论都诞生在 trace 之前，先按 utteranceID 记，
// 转写完成时 rekey 到 signalID，turn_decided 时取走（取后即清，缓冲不跨
// 话轮）。单锁有界（满 64 整表重置，与锚点表同纪律）；watchConnection 的
// 锚点表保持独立——它的生命周期（重置时机）与话轮缓冲不同。
type voiceStageBuffers struct {
	mu       sync.Mutex
	asr      map[string]int
	verdicts map[string]*companion.VoiceTurnDecision
	// turnDecided 按 signalID 记 turn_decided 锚点耗时（attachVoiceStages
	// 的 nil-audio 半写入，audio 半读出——tts_first_audio 以它为基准段）。
	turnDecided map[string]int
}

const voiceStageBufferCapacity = 64

func newVoiceStageBuffers() *voiceStageBuffers {
	return &voiceStageBuffers{
		asr:         make(map[string]int),
		verdicts:    make(map[string]*companion.VoiceTurnDecision),
		turnDecided: make(map[string]int),
	}
}

func (b *voiceStageBuffers) recordAsrElapsed(utteranceID string, elapsedMS int) {
	if utteranceID == "" || elapsedMS < 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.asr) >= voiceStageBufferCapacity {
		b.asr = make(map[string]int)
	}
	b.asr[utteranceID] = elapsedMS
}

func (b *voiceStageBuffers) recordTurnVerdict(utteranceID string, decision *companion.VoiceTurnDecision) {
	if utteranceID == "" || decision == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.verdicts) >= voiceStageBufferCapacity {
		b.verdicts = make(map[string]*companion.VoiceTurnDecision)
	}
	b.verdicts[utteranceID] = decision
}

// rekey 把 utterance 级缓冲搬到 signalID 名下（转写完成回调同时持有两种
// ID，是唯一同时知道映射的时刻）。
func (b *voiceStageBuffers) rekey(utteranceID, signalID string) {
	if utteranceID == "" || signalID == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if elapsed, ok := b.asr[utteranceID]; ok {
		delete(b.asr, utteranceID)
		b.asr[signalID] = elapsed
	}
	if verdict, ok := b.verdicts[utteranceID]; ok {
		delete(b.verdicts, utteranceID)
		b.verdicts[signalID] = verdict
	}
}

// take 取走该话轮的全部缓冲。
func (b *voiceStageBuffers) take(signalID string) (int, bool, *companion.VoiceTurnDecision) {
	b.mu.Lock()
	defer b.mu.Unlock()
	asr, hasAsr := b.asr[signalID]
	delete(b.asr, signalID)
	verdict := b.verdicts[signalID]
	delete(b.verdicts, signalID)
	return asr, hasAsr, verdict
}

// recordTurnDecided 记 turn_decided 锚点耗时（audio 半算 tts_first_audio
// 的基准段；容量纪律与 asr 同）。
func (b *voiceStageBuffers) recordTurnDecided(signalID string, elapsedMS int) {
	if signalID == "" || elapsedMS < 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.turnDecided) >= voiceStageBufferCapacity {
		b.turnDecided = make(map[string]int)
	}
	b.turnDecided[signalID] = elapsedMS
}

// takeTurnDecided 取走 turn_decided 耗时。
func (b *voiceStageBuffers) takeTurnDecided(signalID string) (int, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	elapsed, ok := b.turnDecided[signalID]
	delete(b.turnDecided, signalID)
	return elapsed, ok
}

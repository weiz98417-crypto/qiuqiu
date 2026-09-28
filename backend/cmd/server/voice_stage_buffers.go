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
}

const voiceStageBufferCapacity = 64

func newVoiceStageBuffers() *voiceStageBuffers {
	return &voiceStageBuffers{
		asr:      make(map[string]int),
		verdicts: make(map[string]*companion.VoiceTurnDecision),
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

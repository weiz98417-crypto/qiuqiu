package companion

// 每用户知识生命周期状态（openspec/changes/knowledge-worldinfo）：sticky/
// cooldown 状态机的轮次记忆。归属裁决：knowledge.Library 全局跨用户、
// 永不感知用户——状态住 companion 层有界 FIFO map，进程内存（与
// knowledgeTriggerStates 同纪律：256 用户上限，重启清零=状态机归零）；
// 管道本体（knowledge.Select）纯函数：状态进、状态出，跨用户不串味。

import (
	"hash/fnv"
	"sync"

	"qiuqiu/internal/knowledge"
)

// knowledgeLifecycleUsers 每用户状态的有界 FIFO 上限。
const knowledgeLifecycleUsers = 256

// knowledgeCandidateDepth 是知识问答管道的候选深度：四段后处理要有几名
// 候选才打得开（互斥/冷却/骰下都有次优顶上）——8 足够 121 条目的库。
const knowledgeCandidateDepth = 8

type knowledgeLifecycleStates struct {
	mu     sync.Mutex
	order  []string
	states map[string]*knowledge.Lifecycle
}

func newKnowledgeLifecycleStates() *knowledgeLifecycleStates {
	return &knowledgeLifecycleStates{states: map[string]*knowledge.Lifecycle{}}
}

func (k *knowledgeLifecycleStates) state(userID string) *knowledge.Lifecycle {
	k.mu.Lock()
	defer k.mu.Unlock()
	state, ok := k.states[userID]
	if !ok {
		state = knowledge.NewLifecycle()
		k.states[userID] = state
		k.order = append(k.order, userID)
		for len(k.states) > knowledgeLifecycleUsers {
			oldest := k.order[0]
			k.order = k.order[1:]
			delete(k.states, oldest)
		}
	}
	return state
}

// knowledgeLifecycleSeed 是概率掷骰的每用户盐：fnv64a(userID)——同一用户
// 同轮同条目恒同果（重放一致），跨用户独立（不串味）。
func knowledgeLifecycleSeed(userID string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(userID))
	return h.Sum64()
}

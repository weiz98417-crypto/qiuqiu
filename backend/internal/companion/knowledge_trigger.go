package companion

// 判罚时刻的知识附句（openspec/changes/knowledge-event-triggers，ADR-0017
// 修订）：判罚类事件过限频门后命中策展条目——quote 作为织写锚进 realizer
// 锚点（硬指令引号原样携带），织写失败降级为确定性附句 verbatim answer。
// 附句是搭 ActReact 便车的追加句，不是独立 Proactive Turn——不走 ADR-0015
// 引用码门机器，独立于 backchannel 计数。

import (
	"strings"
	"sync"
	"time"
)

// judgmentTriggerEvents 判罚类事件白名单：只有「等用户问就晚了」的规则
// 时刻才触发，进球报球员简历这类语用不做。
var judgmentTriggerEvents = map[string]bool{
	"var_check":        true,
	"var_result":       true,
	"goal_cancelled":   true,
	"red_card":         true,
	"penalty":          true,
	"penalty_awarded":  true,
}

// 每场限频（Q28 初值）：同条目 1 次、附句总量 2。
const (
	knowledgeTriggerPerEntry   = 1
	knowledgeTriggerPerMatch   = 2
	knowledgeTriggerStateUsers = 256
)

type knowledgeTriggerState struct {
	perEntry map[string]int
	total    int
	seenAt   time.Time
}

// knowledgeTriggerStates 每用户每场的附句计数（随连接生命周期之外的进程
// 内存走，有界 FIFO 淘汰——与 memory 队列 citations 同纪律）。
type knowledgeTriggerStates struct {
	mu    sync.Mutex
	order []string
	states map[string]*knowledgeTriggerState
}

func newKnowledgeTriggerStates() *knowledgeTriggerStates {
	return &knowledgeTriggerStates{states: map[string]*knowledgeTriggerState{}}
}

func (k *knowledgeTriggerStates) state(userID, matchID string) *knowledgeTriggerState {
	k.mu.Lock()
	defer k.mu.Unlock()
	key := userID + "|" + matchID
	state, ok := k.states[key]
	if !ok {
		state = &knowledgeTriggerState{perEntry: map[string]int{}, seenAt: time.Now().UTC()}
		k.states[key] = state
		k.order = append(k.order, key)
		for len(k.states) > knowledgeTriggerStateUsers {
			oldest := k.order[0]
			k.order = k.order[1:]
			delete(k.states, oldest)
		}
	}
	return state
}

// knowledgeTrigger 命中判罚事件的知识条目：过 quiet/限频门后返回条目。
// 未命中（nil）表示本拍不带知识附句。
func (a *Agent) knowledgeTrigger(req MatchEventRequest, trace *Trace) *knowledgeEntry {
	if a == nil || a.knowledge == nil || a.triggerStates == nil {
		return nil
	}
	if !judgmentTriggerEvents[req.Event.EventType] {
		return nil
	}
	if req.Talkativeness == "quiet" || req.UserSpeaking {
		return nil
	}
	entry, ok := a.knowledge.TriggerLookup(req.Event.EventType)
	if !ok {
		return nil
	}
	state := a.triggerStates.state(req.UserID, req.Event.MatchID)
	if state.total >= knowledgeTriggerPerMatch || state.perEntry[entry.ID] >= knowledgeTriggerPerEntry {
		return nil
	}
	state.total++
	state.perEntry[entry.ID]++
	if trace != nil {
		trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "knowledge.trigger", Args: map[string]string{
			"id": entry.ID, "eventType": req.Event.EventType,
		}})
	}
	return &knowledgeEntry{ID: entry.ID, Quote: entry.Quote, Answer: entry.Answer}
}

type knowledgeEntry struct {
	ID     string
	Quote  string
	Answer string
}

// knowledgeDeterministicAppendix 确定性附句模板（织写失败降级 / 无 realizer
// 路径）：verbatim answer，不碰措辞。
func knowledgeDeterministicAppendix(entry *knowledgeEntry) string {
	if entry == nil || strings.TrimSpace(entry.Answer) == "" {
		return ""
	}
	return "补一句规则：" + entry.Answer
}

// knowledgeQuoteCarried 校验织写产物是否原样携带引语锚。
func knowledgeQuoteCarried(reply string, entry *knowledgeEntry) bool {
	if entry == nil || strings.TrimSpace(entry.Quote) == "" {
		return true
	}
	return strings.Contains(reply, entry.Quote)
}

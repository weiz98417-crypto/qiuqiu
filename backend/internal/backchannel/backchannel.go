// Package backchannel 实现伴随反应通道（openspec/changes/backchannel，
// ADR-0016）：微反应不是发言——不占回合槽、不过 C2 引用码门、不进回合
// 调度器；信任约束换形为独立限频 + quiet 档禁用 + 全量审计（trace 与
// Interaction Ledger 由调用方落）。纯规则决策，零 LLM、零延迟。
package backchannel

import (
	"time"
)

// 限频与白名单（Q7 策略初值，运营可调点集中于此）。
const (
	MaxPerHalf  = 3
	MaxPerMatch = 6
	// QuietTier 用户的安静档：微反应也是打扰，直接禁用。
	QuietTier = "quiet"
)

// whitelistedEvents 是可触发微反应的事件类型（高情绪、低争议）。
var whitelistedEvents = map[string]bool{
	"big_chance": true,
	"miss":       true,
	"save":       true,
	"var_check":  true,
}

// phrasePools 按事件类型分桶的短语池（≤10 字，球球口吻）。
var phrasePools = map[string][]string{
	"big_chance": {"哇，这球太险了！", "就差一点点！"},
	"miss":       {"哎呀，太可惜了！", "这都没进！"},
	"save":       {"神扑！稳住了！", "这扑救没话说！"},
	"var_check":  {"VAR 回放，心悬住了。", "等 VAR，先别喊。"},
}

// 表演槽位取自 presentation-map.json 既有库存（ADR-0007，不新增资产）。
var presentations = map[string][2]string{ // [expression, motion]
	"big_chance": {"excited", "celebrate_02"},
	"miss":       {"sad", "miss"},
	"save":       {"surprised", "tense"},
	"var_check":  {"thinking", "analysis"},
}

// State 是一条连接内的微反应计数（随连接生命周期走）。
type State struct {
	period      string
	halfCount   int
	fullCount   int
	rotation    map[string]int
	lastEmitted time.Time
}

// Verdict 是一次微反应的产出。
type Verdict struct {
	Phrase     string `json:"text"`
	EventType  string `json:"eventType"`
	Expression string `json:"expression"`
	Motion     string `json:"motion"`
}

// Decide 决定是否发一条微反应：白名单 + 半场/全场限频 + quiet 档 + 用户
// 正在说话时让路 + 10 秒风暴去重。命中则更新计数并轮转短语，避免连发
// 重复。
func Decide(state *State, eventType, period, talkativeness string, userSpeaking bool, now time.Time) (Verdict, bool) {
	if state == nil || !whitelistedEvents[eventType] {
		return Verdict{}, false
	}
	if talkativeness == QuietTier || userSpeaking {
		return Verdict{}, false
	}
	if state.period != period {
		state.period = period
		state.halfCount = 0
	}
	if state.halfCount >= MaxPerHalf || state.fullCount >= MaxPerMatch {
		return Verdict{}, false
	}
	pool := phrasePools[eventType]
	if len(pool) == 0 {
		return Verdict{}, false
	}
	// 距上一条过近（<10s）直接让路——同一波事件风暴只出一条。
	if !state.lastEmitted.IsZero() && now.Sub(state.lastEmitted) < 10*time.Second {
		return Verdict{}, false
	}
	if state.rotation == nil {
		state.rotation = map[string]int{}
	}
	rotation := state.rotation[eventType]
	state.rotation[eventType] = rotation + 1
	phrase := pool[rotation%len(pool)]
	state.halfCount++
	state.fullCount++
	state.lastEmitted = now
	presentation := presentations[eventType]
	return Verdict{Phrase: phrase, EventType: eventType, Expression: presentation[0], Motion: presentation[1]}, true
}

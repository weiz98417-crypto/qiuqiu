package observation

import (
	"sync"
	"time"
)

// ClientHealthLedger 收客户端语音健康遥测（快修 P1 观测面）：播放期抢话
// 误打断/降级/恢复、嘴型退化。聚合口径按 Operations Observation 纪律——
// 面板只见事件种类、计数与比赛归属，不见用户身份与任何正文。
// 进程内环形缓冲：单实例部署的事实观测面，重启清零（遥测非账本）。
type ClientHealthLedger struct {
	mu       sync.Mutex
	counters map[string]int64
	recent   []ClientHealthEvent
	capacity int
}

// ClientHealthEvent 是一条已裁剪的健康事件：种类 + 时间 + 比赛归属。
type ClientHealthEvent struct {
	Kind    string    `json:"kind"`
	At      time.Time `json:"at"`
	MatchID string    `json:"matchId,omitempty"`
}

const clientHealthRecentCapacity = 200

func NewClientHealthLedger() *ClientHealthLedger {
	return &ClientHealthLedger{
		counters: map[string]int64{},
		capacity: clientHealthRecentCapacity,
	}
}

// Record 记一条事件：计数 +1，近环追加（满则淘汰最旧）。kind 空则忽略。
func (l *ClientHealthLedger) Record(kind, matchID string, now time.Time) {
	if l == nil || kind == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.counters[kind]++
	l.recent = append(l.recent, ClientHealthEvent{Kind: kind, At: now.UTC(), MatchID: matchID})
	if excess := len(l.recent) - l.capacity; excess > 0 {
		l.recent = append([]ClientHealthEvent(nil), l.recent[excess:]...)
	}
}

// Snapshot 返回计数与最近事件（新在前）的只读拷贝。
func (l *ClientHealthLedger) Snapshot() (map[string]int64, []ClientHealthEvent) {
	if l == nil {
		return map[string]int64{}, nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	counters := make(map[string]int64, len(l.counters))
	for kind, count := range l.counters {
		counters[kind] = count
	}
	recent := make([]ClientHealthEvent, len(l.recent))
	for i, event := range l.recent {
		recent[len(l.recent)-1-i] = event
	}
	return counters, recent
}

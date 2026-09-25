package observation

// 气氛旁证挂点（openspec/changes/ambient-audio-observation 6.2）。
//
// ADR-0002 宪法线：气氛信号（欢呼/嘘声/音量骤变）永不作 Match Fact、
// 永不进事实账本——只经既有 Record 入口落 observation store 作外部主张
// 旁证（corroborating evidence，最低权重档）。coordinator 没有独立的
// 「证据/来源」扩展点，故此处只做最小增量：一个 kind=ambient 的旁证输入
// 构造器，不触碰既有确认/矛盾逻辑。
//
// 为什么旁证行永远不会改判事实：AmbientEventType 产出 ambient_* 事件类型，
// 而 matchstate 事实事件不可能携带该前缀，eventTypeCompatible 的精确匹配
// 分支永不命中——旁证行不会确认/矛盾任何事实，只随窗口静默过期（过期
// Resolution 的 ReliableText 为空，companion 侧直接过滤，不产生话轮）。

import (
	"strings"
	"time"
)

const (
	// KindAmbient 标记气氛旁证观察行：与用户主张（score/event 家族）在
	// kind 域隔离，从形状上防止旁证被当作主张参与确认/矛盾。
	KindAmbient = "ambient"
	// CertaintyAmbient 是旁证的最低权重档标注：气氛事件只有声学置信度、
	// 无主张语义，置信档位单独命名，不与用户主张的 certainty 档混用。
	CertaintyAmbient = "ambient_corroboration"
)

// AmbientCorroboration 把一个 sidecar 气氛事件折算成 observation store 的
// 旁证输入。构造器刻意不收主张字段（无 claimed score/team/player）——
// 旁证在源头就不携带比分语义，这是负例测试（气氛永不进事实账本）的
// 第一道闸。
func AmbientCorroboration(signalID, userID, matchID, eventKind string, at time.Time) Input {
	return Input{
		SignalID:   signalID,
		UserID:     userID,
		MatchID:    matchID,
		Kind:       KindAmbient,
		EventType:  AmbientEventType(eventKind),
		Certainty:  CertaintyAmbient,
		ReceivedAt: at,
	}
}

// AmbientEventType 把 sidecar 的气氛种类映射到旁证专属事件类型命名空间
// （ambient_cheer / ambient_boo / ambient_volume_spike）。该前缀保证与
// matchstate 事实事件类型永不相交。
func AmbientEventType(eventKind string) string {
	return "ambient_" + strings.TrimSpace(eventKind)
}

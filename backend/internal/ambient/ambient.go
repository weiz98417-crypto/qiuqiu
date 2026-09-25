package ambient

// 气氛旁路的事件形状与解析（openspec/changes/ambient-audio-observation）。
// SenseVoice AED sidecar 输入观赛会话已有的音频分片，输出气氛信号事件；
// 事件只作 observation store 的外部主张旁证（最低权重档），永不作
// Match Fact、永不进事实账本（ADR-0002 宪法线，负例测试锁死）。

import (
	"fmt"
	"strings"
	"time"
)

// 气氛信号三类：欢呼 / 嘘声 / 音量骤变（design.md 事件形状的 kind 域）。
const (
	KindCheer       = "cheer"
	KindBoo         = "boo"
	KindVolumeSpike = "volume_spike"
)

// Event 是 sidecar 对一个音频分片的气氛判定，与 design.md 形状一致：
// {kind, confidence, ts}；session_scope 由服务端接线层补（sidecar 无状态、
// 会话外不接收），原始音频不落盘、事件即弃。
type Event struct {
	Kind       string    `json:"kind"`
	Confidence float64   `json:"confidence"`
	TS         time.Time `json:"ts"`
}

// NormalizeKind 返回合法化后的气氛种类；未知种类返回空串（调用方丢弃）。
// sidecar 版本漂移或多出的实验种类在此收口，旁路形状保持稳定。
func NormalizeKind(kind string) string {
	switch strings.TrimSpace(kind) {
	case KindCheer:
		return KindCheer
	case KindBoo:
		return KindBoo
	case KindVolumeSpike:
		return KindVolumeSpike
	default:
		return ""
	}
}

// sanitize 校正单条事件：未知种类丢弃、置信度夹到 [0,1]、缺 ts 补 now。
// 返回 false 表示该事件不可用（解析容错的一环，坏事件静默丢弃）。
func (e Event) sanitize(now time.Time) (Event, bool) {
	kind := NormalizeKind(e.Kind)
	if kind == "" {
		return Event{}, false
	}
	if e.Confidence < 0 {
		e.Confidence = 0
	}
	if e.Confidence > 1 {
		e.Confidence = 1
	}
	if e.TS.IsZero() {
		e.TS = now
	}
	e.Kind = kind
	return e, true
}

// String 供日志/计数使用，一行一个事件不换行。
func (e Event) String() string {
	return fmt.Sprintf("{kind:%s confidence:%.2f ts:%s}", e.Kind, e.Confidence, e.TS.Format(time.RFC3339))
}

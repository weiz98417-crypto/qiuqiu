package conversation

// 赛点分类（openspec/changes/policy-bits B2）：matchstate 事件 → 赛点权重
// 类。proactive gate 的白名单无权重——点球/红牌/决胜球与普通进球同级；
// 本分类器给「这球可能定胜负」的时刻单独抬档（UrgencyPivotal）并固定引用
// 码 pivotal（下游 reason code = proactive_citation:pivotal）。
//
// 分类只读事件自带事实（类型/时钟/事件后比分），不读用户画像——赛点是比
// 赛本身的属性，与用户支持谁无关。

import (
	"fmt"
	"strings"

	"qiuqiu/internal/matchstate"
)

// pivotalGoalWindowMinutes 是决胜时段窗口：第 75 分钟起（常规时间的最后
// 15 分钟）。此窗内打成/保持一球差的进球是赛点——「领先一球进入最后 15
// 分钟」与「决胜球」同窗同判。
const pivotalGoalWindowMinutes = 75

// IsPivotalMatchEvent 报告事件是否赛点：
//   - 点球判罚（penalty_awarded/penalty，判罚中或主罚）；
//   - 红牌（red_card，少一人改写均势）；
//   - 决胜时段（时钟 ≥75'）内打成或保持一球差的进球。
func IsPivotalMatchEvent(event matchstate.MatchEvent) bool {
	switch event.EventType {
	case "penalty_awarded", "penalty", "red_card":
		return true
	case "goal":
		minute, ok := pivotalClockMinute(event.Clock)
		if !ok || minute < pivotalGoalWindowMinutes {
			return false
		}
		home, away, ok := pivotalScoreAfter(event)
		if !ok {
			return false
		}
		return home-away == 1 || away-home == 1
	default:
		return false
	}
}

// pivotalClockMinute 解析事件时钟（"MM:SS"）为分钟数；解析不了（空/异形）
// 视为不命中——分类器只对事实确凿的事件抬档。
func pivotalClockMinute(clock string) (int, bool) {
	var minutes, seconds int
	if _, err := fmt.Sscanf(strings.TrimSpace(clock), "%d:%d", &minutes, &seconds); err != nil || minutes < 0 || seconds < 0 || seconds > 59 {
		return 0, false
	}
	return minutes, true
}

// pivotalScoreAfter 取事件后比分：账本投影优先（EffectiveScoreAfter），
// 回落上报比分（Score——matchstate 纪律：事件行携带的就是事件后比分）。
func pivotalScoreAfter(event matchstate.MatchEvent) (home, away int, ok bool) {
	if event.EffectiveScoreAfter != nil {
		return event.EffectiveScoreAfter.Home, event.EffectiveScoreAfter.Away, true
	}
	return event.Score.Home, event.Score.Away, true
}

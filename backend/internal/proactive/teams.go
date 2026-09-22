package proactive

// 球队名对齐（season-subscription 还债项）：别名 → 全名映射与预备队排除
// 下沉到叶子包 internal/teamalign（relationship 也要用，且 relationship ←
// companion ← proactive 会成环，proactive 只能委托叶子）。companion 的
// 意图侧经本包导出的包装函数使用。

import (
	"qiuqiu/internal/teamalign"
)

// AlignKey 把口语别名展开成赛程全名。
func AlignKey(team string) string {
	return teamalign.Align(team)
}

// IsReserveOrYouthTeam 报告该队是否为预备队/青年队/女足。
func IsReserveOrYouthTeam(team string) bool {
	return teamalign.IsReserveOrYouth(team)
}

// TeamNameAligns 单队对齐：别名展开后全等或互为包含；预备队/青年队/女足
// 直接排除。
func TeamNameAligns(a, b string) bool {
	if IsReserveOrYouthTeam(a) || IsReserveOrYouthTeam(b) {
		return false
	}
	return teamalign.Aligns(a, b)
}

// teamNameMatches 是订阅/展开内部的队名匹配（别名感知）。
func teamNameMatches(fixtureTeam, want string) bool {
	if IsReserveOrYouthTeam(fixtureTeam) {
		return false
	}
	return TeamNameAligns(fixtureTeam, want)
}

package proactive

// 球队名对齐（season-subscription 还债项）：口语别名 → 赛程全名的单一
// 映射点，加上预备队/青年队/女足排除。companion 的意图侧委托本包——
// 展开器在 proactive，别名逻辑必须与订阅/提醒同一处定义。

import (
	"regexp"
	"strings"
)

// TeamAliases 是常用球队别名：口语短名 → 赛程全名（"皇马"不是
// "皇家马德里"的子串，纯 contains 永远对不上）。
var TeamAliases = map[string]string{
	"皇马":  "皇家马德里",
	"巴萨":  "巴塞罗那",
	"曼联":  "曼彻斯特联",
	"曼城":  "曼彻斯特城",
	"拜仁":  "拜仁慕尼黑",
	"国米":  "国际米兰",
	"大巴黎": "巴黎圣日耳曼",
	"马竞":  "马德里竞技",
}

// ReserveTeamPattern 识别预备队/青年队/女足——订阅与提醒默认排除。
var ReserveTeamPattern = regexp.MustCompile(`(?i)(u2[0-9]|u1[0-9]|b队|二队|预备队|青年队|女足)`)

// IsReserveOrYouthTeam 报告该队是否为预备队/青年队/女足。
func IsReserveOrYouthTeam(team string) bool {
	return ReserveTeamPattern.MatchString(team)
}

// AlignKey 把口语别名展开成赛程全名；未知名原样返回。
func AlignKey(team string) string {
	team = strings.TrimSpace(team)
	if full, ok := TeamAliases[team]; ok {
		return full
	}
	return team
}

// TeamNameAligns 单队对齐：别名展开后全等或互为包含。
func TeamNameAligns(a, b string) bool {
	a, b = AlignKey(a), AlignKey(b)
	if a == "" || b == "" {
		return false
	}
	return a == b || strings.Contains(a, b) || strings.Contains(b, a)
}

// teamNameMatches 是订阅/展开内部的队名匹配（别名感知）。
func teamNameMatches(fixtureTeam, want string) bool {
	if IsReserveOrYouthTeam(fixtureTeam) {
		return false
	}
	return TeamNameAligns(fixtureTeam, want)
}

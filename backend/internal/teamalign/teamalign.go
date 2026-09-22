// Package teamalign 是球队名对齐的叶子工具包：口语别名 → 赛程全名的
// 单一映射点，加预备队/青年队/女足排除。proactive 与 relationship 共用
//（"皇马"不是"皇家马德里"的子串——纯 contains 永远对不上）。
package teamalign

import (
	"regexp"
	"strings"
)

// Aliases 是常用球队别名：口语短名 → 赛程全名。
var Aliases = map[string]string{
	"皇马":  "皇家马德里",
	"巴萨":  "巴塞罗那",
	"曼联":  "曼彻斯特联",
	"曼城":  "曼彻斯特城",
	"拜仁":  "拜仁慕尼黑",
	"国米":  "国际米兰",
	"大巴黎": "巴黎圣日耳曼",
	"马竞":  "马德里竞技",
}

// ReserveOrYouth 识别预备队/青年队/女足——订阅与提醒默认排除。
var ReserveOrYouth = regexp.MustCompile(`(?i)(u2[0-9]|u1[0-9]|b队|二队|预备队|青年队|女足)`)

// IsReserveOrYouth 报告该队是否为预备队/青年队/女足。
func IsReserveOrYouth(team string) bool {
	return ReserveOrYouth.MatchString(team)
}

// Align 把口语别名展开成赛程全名；未知名原样返回。
func Align(team string) string {
	team = strings.TrimSpace(team)
	if full, ok := Aliases[team]; ok {
		return full
	}
	return team
}

// Aligns 单队对齐：别名展开后全等或互为包含。
func Aligns(a, b string) bool {
	a, b = Align(a), Align(b)
	if a == "" || b == "" {
		return false
	}
	return a == b || strings.Contains(a, b) || strings.Contains(b, a)
}

package directordraft

// 转写启发式分类（openspec/changes/deep-water-polish 1.3）：从 service.go
// 分离出的「这段话像不像比赛口述 / 说了什么」纯判定层——isPlausibleMatch
// Transcript、matchHints、事件类型推断与球员提及扫描。同包分离，函数签名
// 与词面零变化；草稿装配仍在 service.go。

import (
	"strings"
	"unicode"

	"qiuqiu/internal/matchstate"
)

func isNonEventDescription(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return false
	}
	for _, marker := range []string{
		"unless there is a specific request",
		"citations should",
		"author, year",
		"page number",
		"citation",
		"bibliography",
		"除非有明确要求",
		"引用均按常规方式列出",
		"作者、年份和页码",
		"作者、年份、页码",
	} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func isPlausibleMatchTranscript(value string, config matchstate.MatchConfig) bool {
	value = strings.TrimSpace(value)
	meaningfulRunes := 0
	for _, character := range value {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			meaningfulRunes++
		}
	}
	if meaningfulRunes < 2 {
		return false
	}

	normalized := strings.ToLower(value)
	for _, hint := range matchHints(config) {
		hint = strings.TrimSpace(hint)
		if utf8RuneCount(hint) >= 2 && strings.Contains(normalized, strings.ToLower(hint)) {
			return true
		}
	}
	for _, keyword := range []string{
		"进球", "进了", "射门", "防守", "扑救", "换人", "犯规", "黄牌", "红牌", "点球", "助攻",
		"越位", "传球", "角球", "任意球", "门柱", "开球", "绝杀", "补时", "goal", "shoot",
		"shot", "save", "substitution", "foul", "yellow card", "red card", "penalty", "offside",
	} {
		if strings.Contains(normalized, keyword) {
			return true
		}
	}
	return false
}

func utf8RuneCount(value string) int {
	return len([]rune(value))
}

func matchHints(config matchstate.MatchConfig) []string {
	hints := []string{config.HomeTeam, config.AwayTeam}
	for _, player := range append(append([]matchstate.Player{}, config.HomePlayers...), config.AwayPlayers...) {
		hints = append(hints, player.Name)
	}
	return uniqueStrings(hints)
}

func inferEventType(transcript string) string {
	normalized := strings.ToLower(strings.TrimSpace(transcript))
	for _, match := range []struct {
		eventType string
		keywords  []string
	}{
		{"goal", []string{"进球", "球进了", "破门", "得分", "领先", "goal"}},
		{"substitution", []string{"换人", "上场", "下场", "substitution"}},
		{"red_card", []string{"红牌", "red card"}},
		{"yellow_card", []string{"黄牌", "yellow card"}},
		{"save", []string{"扑救", "save"}},
		{"foul", []string{"犯规", "foul"}},
		{"big_chance", []string{"绝佳机会", "单刀", "big chance"}},
		{"shot", []string{"射门", "起脚", "shot", "shoot"}},
	} {
		for _, keyword := range match.keywords {
			if strings.Contains(normalized, keyword) {
				return match.eventType
			}
		}
	}
	return ""
}

func mentionedPlayers(transcript string, config matchstate.MatchConfig) []string {
	normalizedTranscript := normalizeLookup(transcript)
	type mention struct {
		name  string
		index int
	}
	mentions := make([]mention, 0)
	consider := func(player matchstate.Player) {
		name := strings.TrimSpace(player.Name)
		lookup := normalizeLookup(name)
		if lookup == "" {
			return
		}
		index := strings.Index(normalizedTranscript, lookup)
		if index < 0 {
			for _, part := range strings.FieldsFunc(name, func(character rune) bool { return !unicode.IsLetter(character) && !unicode.IsDigit(character) }) {
				partLookup := normalizeLookup(part)
				if len([]rune(partLookup)) < 2 {
					continue
				}
				if candidateIndex := strings.Index(normalizedTranscript, partLookup); candidateIndex >= 0 && (index < 0 || candidateIndex < index) {
					index = candidateIndex
				}
			}
		}
		if index >= 0 {
			mentions = append(mentions, mention{name: name, index: index})
		}
	}
	for _, player := range config.HomePlayers {
		consider(player)
	}
	for _, player := range config.AwayPlayers {
		consider(player)
	}
	for left := 0; left < len(mentions); left++ {
		for right := left + 1; right < len(mentions); right++ {
			if mentions[right].index < mentions[left].index {
				mentions[left], mentions[right] = mentions[right], mentions[left]
			}
		}
	}
	result := make([]string, 0, len(mentions))
	seen := make(map[string]bool)
	for _, item := range mentions {
		lookup := normalizeLookup(item.name)
		if seen[lookup] {
			continue
		}
		seen[lookup] = true
		result = append(result, item.name)
	}
	return result
}

package companion

import (
	"fmt"
	"strings"

	"qiuqiu/internal/matchstate"
)

// FallbackProactiveText 是主动回合的唯一兜底罐头库：事件未带人工话术
// （ProactiveText 为空）时，按事件类型给出一句中文短反应。运营台写路径
// 的主动装饰与 agent 的比赛反应路径共用这一份，避免双罐头库漂移。
func FallbackProactiveText(ev matchstate.MatchEvent) string {
	switch ev.EventType {
	case "goal":
		if scorer := goalScorerName(ev); scorer != "" {
			if ev.Score.Home > 0 || ev.Score.Away > 0 {
				return fmt.Sprintf("%s进了！比分来到%d比%d。", scorer, ev.Score.Home, ev.Score.Away)
			}
			return scorer + "进了！"
		}
		return "进了！这一下气氛直接被点起来了。"
	case "red_card":
		return "红牌来了，比赛走势一下子变得很微妙。"
	case "penalty":
		return "点球时刻来了，先深呼吸，这球太关键了。"
	case "var_check":
		return "VAR 介入了，这几秒真的很折磨人。"
	case "big_chance", "pressure":
		return "这波很危险，我们盯紧一点。"
	case "miss":
		return "哎呀，就差一点点，这球太可惜了。"
	case "substitution":
		subOn, subOff := substitutionNames(ev)
		teamName := strings.TrimSpace(ev.TeamName)
		if teamName == "" {
			teamName = "这边"
		}
		switch {
		case subOn != "" && subOff != "":
			return fmt.Sprintf("%s换人，%s上场，%s下场。", teamName, subOn, subOff)
		case subOn != "":
			return fmt.Sprintf("%s换人，%s上场。", teamName, subOn)
		case subOff != "":
			return fmt.Sprintf("%s换人，%s下场。", teamName, subOff)
		}
		return teamName + "正在调整人员。"
	default:
		if ev.Description != "" {
			return ev.Description
		}
		return "场上有新情况，我们一起看下去。"
	}
}

func substitutionNames(event matchstate.MatchEvent) (subOn, subOff string) {
	for _, participant := range event.Participants {
		switch participant.Role {
		case "sub_on":
			subOn = strings.TrimSpace(participant.Name)
		case "sub_off":
			subOff = strings.TrimSpace(participant.Name)
		}
	}
	return subOn, subOff
}

func goalScorerName(event matchstate.MatchEvent) string {
	if player := strings.TrimSpace(event.PlayerName); player != "" {
		return player
	}
	for _, participant := range event.Participants {
		if participant.Role == "scorer" && strings.TrimSpace(participant.Name) != "" {
			return strings.TrimSpace(participant.Name)
		}
	}
	return ""
}

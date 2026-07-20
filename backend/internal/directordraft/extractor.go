package directordraft

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"qiuqiu/internal/llm"
	"qiuqiu/internal/matchstate"
)

type LLMExtractor struct {
	client *llm.Client
}

func NewLLMExtractor(client *llm.Client) *LLMExtractor {
	return &LLMExtractor{client: client}
}

func (extractor *LLMExtractor) Extract(ctx context.Context, transcript string, match MatchContext) (Extraction, error) {
	if extractor == nil || extractor.client == nil {
		return Extraction{}, ErrNotConfigured
	}
	prompt := fmt.Sprintf(`你是足球赛事导演台的事件结构化器。只抽取输入中明确出现的信息，不猜测球员、比分或事实状态。
只输出一个 JSON 对象，不要 Markdown：
{"team":"球队名称或home/away","eventType":"事件类型","participants":[{"role":"角色","name":"球员姓名"}],"description":"简洁事实描述","occurredClock":"MM:SS或空字符串","confidence":0到1}
允许的事件类型：goal, shot, big_chance, save, miss, foul, yellow_card, red_card, var_check, var_result, goal_cancelled, penalty, penalty_awarded, substitution, injury, tactical_shift, pressure, kickoff, halftime, fulltime, match_end, operator_note。
角色必须与行为匹配，例如换人为 sub_on/sub_off，进球为 scorer/assist，射门为 shooter/assist，犯规为 offender/fouled。
比赛：%s 对 %s。
主队名单：%s。
客队名单：%s。`, match.Config.HomeTeam, match.Config.AwayTeam, rosterNames(match.Config.HomePlayers), rosterNames(match.Config.AwayPlayers))
	result, err := extractor.client.GenerateWithMessagesLimit(ctx, []llm.Message{
		{Role: "system", Content: prompt},
		{Role: "user", Content: transcript},
	}, 0.1, 320)
	if err != nil {
		return Extraction{}, err
	}
	var extraction Extraction
	if err := json.Unmarshal(extractJSONObject(result.Text), &extraction); err != nil {
		return Extraction{}, fmt.Errorf("director draft decode: %w", err)
	}
	return extraction, nil
}

func extractJSONObject(value string) []byte {
	value = strings.TrimSpace(value)
	start := strings.IndexByte(value, '{')
	end := strings.LastIndexByte(value, '}')
	if start >= 0 && end >= start {
		value = value[start : end+1]
	}
	return []byte(value)
}

func rosterNames(players []matchstate.Player) string {
	names := make([]string, 0, len(players))
	for _, player := range players {
		if name := strings.TrimSpace(player.Name); name != "" {
			names = append(names, name)
		}
	}
	return strings.Join(names, "、")
}

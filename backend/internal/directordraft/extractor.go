package directordraft

import (
	"context"
	"fmt"
	"strings"

	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/structured"
)

type LLMExtractor struct {
	client *structured.Client
}

func NewLLMExtractor(client *structured.Client) *LLMExtractor {
	return &LLMExtractor{client: client}
}

const extractionToolName = "extract_match_event"

func (extractor *LLMExtractor) Extract(ctx context.Context, transcript string, match MatchContext) (Extraction, error) {
	if extractor == nil || extractor.client == nil {
		return Extraction{}, ErrNotConfigured
	}
	prompt := fmt.Sprintf(`你是足球赛事导演台的事件结构化器。只抽取输入中明确出现的信息，不猜测球员、比分或事实状态。
把抽取结果通过 %s 工具调用返回，字段含义以工具 schema 为准。
允许的事件类型：goal, shot, big_chance, save, miss, foul, yellow_card, red_card, var_check, var_result, goal_cancelled, penalty, penalty_awarded, substitution, injury, tactical_shift, pressure, kickoff, halftime, fulltime, match_end, operator_note。
角色必须与行为匹配，例如换人为 sub_on/sub_off，进球为 scorer/assist/defender，射门为 shooter/assist/blocker/keeper，绝佳机会为 attacker/passer/defender/keeper，犯规为 offender/fouled。同一角色可以出现多次（例如多个助攻者或防守者），每名球员单独输出一项，不要把多个姓名合并成一个 name。description 只能是一句来自口述的比赛事实；不得输出引用格式、作者年份页码、任务说明、解释或其他元文本。无法确定时 description 留空。
比赛：%s 对 %s。
主队名单：%s。
客队名单：%s。`, extractionToolName, match.Config.HomeTeam, match.Config.AwayTeam, rosterNames(match.Config.HomePlayers), rosterNames(match.Config.AwayPlayers))
	return structured.Extract[Extraction](ctx, extractor.client, structured.CallOptions{
		SystemPrompt:    prompt,
		UserContent:     transcript,
		ToolName:        extractionToolName,
		ToolDescription: "从导演口述稿中抽取单个比赛事件",
		Temperature:     0.1,
		MaxTokens:       320,
	})
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

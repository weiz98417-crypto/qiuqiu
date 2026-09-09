package pipeline

import (
	"fmt"
	"qiuqiu/internal/llm"
	"qiuqiu/internal/matchstate"
	"strings"
	"sync"
)

// PromptManager loads and caches prompt templates.
type PromptManager struct {
	mu     sync.RWMutex
	system string
}

func NewPromptManager() *PromptManager {
	return &PromptManager{}
}

// LoadSystem sets the system persona prompt.
func (pm *PromptManager) LoadSystem(text string) {
	pm.mu.Lock()
	pm.system = text
	pm.mu.Unlock()
}

// System returns the system persona prompt.
func (pm *PromptManager) System() string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.system
}

// BuildReplyMessages creates chat messages for user speech → LLM reply.
func BuildReplyMessages(systemPrompt, text, intent string) []llm.Message {
	return []llm.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: fmt.Sprintf("用户%s说：%s\n请根据语境简短回复", intentLabel(intent), text)},
	}
}

// BuildReplyMessagesWithMatch adds the current match snapshot before the user's chat.
func BuildReplyMessagesWithMatch(systemPrompt, text, intent string, snapshot matchstate.Snapshot) []llm.Message {
	return []llm.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "system", Content: formatMatchSnapshot(snapshot)},
		{Role: "user", Content: fmt.Sprintf("用户%s说：%s\n请结合当前比赛上下文，用普通球迷能听懂的中文简短回复。不要假装看到未提供的比赛画面。", intentLabel(intent), text)},
	}
}

func BuildProactiveEventMessages(systemPrompt string, ev matchstate.MatchEvent, snapshot matchstate.Snapshot) []llm.Message {
	eventLine := formatEventPackage(ev)
	return []llm.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "system", Content: formatMatchSnapshot(snapshot)},
		{Role: "user", Content: fmt.Sprintf("运营刚刚发布了一个比赛事件包：\n%s\n请你作为球球主动对独自看球的用户说一句中文短反应。要求：1句，口语化，有陪伴感；不要编造画面外信息；如果是重大事件可以更有情绪。", eventLine)},
	}
}

func formatEventPackage(ev matchstate.MatchEvent) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[比赛状态]\n")
	fmt.Fprintf(&b, "时间：%s %s\n", ev.Period, ev.Clock)
	fmt.Fprintf(&b, "比分：%d-%d\n", ev.Score.Home, ev.Score.Away)
	if ev.TeamName != "" {
		fmt.Fprintf(&b, "当前方：%s\n", ev.TeamName)
	}
	fmt.Fprintf(&b, "\n[实时事件]\n")
	fmt.Fprintf(&b, "事件：%s\n", ev.EventType)
	if ev.PlayerName != "" {
		fmt.Fprintf(&b, "主参与人：%s\n", ev.PlayerName)
	}
	if p := formatParticipantsInline(ev.Participants); p != "" {
		fmt.Fprintf(&b, "参与人：%s\n", p)
	}
	fmt.Fprintf(&b, "强度：%d/5\n", ev.Intensity)
	if ev.RecommendedAction != "" {
		fmt.Fprintf(&b, "推荐动作：%s\n", ev.RecommendedAction)
	}
	fmt.Fprintf(&b, "导演描述：%s", trimRepeatedTeam(ev.TeamName, ev.Description))
	return b.String()
}

func formatMatchSnapshot(snapshot matchstate.Snapshot) string {
	if snapshot.MatchID == "" || len(snapshot.RecentEvents) == 0 {
		return "当前比赛上下文：暂无人工录入的赛事事件。你可以正常陪用户聊天，也可以请用户告诉你刚才发生了什么。"
	}

	var recent []string
	for _, ev := range snapshot.RecentEvents {
		line := fmt.Sprintf("- %s %s：%s%s", ev.Clock, ev.EventType, ev.Description, formatParticipants(ev.Participants))
		if ev.TeamName != "" {
			line = fmt.Sprintf("- %s %s · %s：%s%s", ev.Clock, ev.TeamName, ev.EventType, trimRepeatedTeam(ev.TeamName, ev.Description), formatParticipants(ev.Participants))
		}
		recent = append(recent, line)
	}

	return fmt.Sprintf(
		"当前比赛上下文：\n- 比赛：%s vs %s\n- 比分：%d-%d\n- 时间：%s %s\n- 情绪强度：%d/5\n- 最近事件：\n%s",
		snapshot.HomeTeam,
		snapshot.AwayTeam,
		snapshot.Score.Home,
		snapshot.Score.Away,
		snapshot.Period,
		snapshot.Clock,
		snapshot.EmotionalTemperature,
		strings.Join(recent, "\n"),
	)
}

func trimRepeatedTeam(teamName, description string) string {
	prefix := teamName + "："
	return strings.TrimPrefix(description, prefix)
}

func formatParticipants(participants []matchstate.Participant) string {
	inline := formatParticipantsInline(participants)
	if inline == "" {
		return ""
	}
	return "\n  参与人：" + inline
}

func formatParticipantsInline(participants []matchstate.Participant) string {
	if len(participants) == 0 {
		return ""
	}
	parts := make([]string, 0, len(participants))
	for _, participant := range participants {
		if participant.Name == "" {
			continue
		}
		role := participant.Role
		if role == "" {
			role = "player"
		}
		parts = append(parts, fmt.Sprintf("%s=%s", role, participant.Name))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "；")
}

func intentLabel(intent string) string {
	switch intent {
	case "question":
		return "在问"
	case "command":
		return "命令"
	case "praise":
		return "在夸"
	case "complain":
		return "在吐槽"
	default:
		return ""
	}
}

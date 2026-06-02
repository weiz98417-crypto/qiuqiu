package companion

import (
	"context"
	"fmt"
	"strings"
	"time"

	"qiuqiu/internal/matchstate"
)

type Intent string

const (
	IntentSmalltalk       Intent = "smalltalk"
	IntentMatchStatus     Intent = "match_status_question"
	IntentRecentEvent     Intent = "recent_event_question"
	IntentPlayerQuestion  Intent = "player_question"
	IntentEmotionReaction Intent = "emotion_reaction"
	IntentControlCommand  Intent = "control_command"
	IntentUnknown         Intent = "unknown"
)

type MessageRequest struct {
	MatchID string
	UserID  string
	Text    string
	Now     time.Time
}

type Response struct {
	Intent Intent
	Reply  string
	Trace  Trace
}

type ProactiveResponse struct {
	Reply string
	Trace Trace
}

type Trace struct {
	ID             string     `json:"id"`
	MatchID        string     `json:"matchId"`
	UserID         string     `json:"userId"`
	Input          string     `json:"input"`
	Intent         Intent     `json:"intent"`
	ToolCalls      []ToolCall `json:"toolCalls"`
	RetrievedEvent []string   `json:"retrievedEventIds"`
	Output         string     `json:"output"`
	Reason         string     `json:"reason"`
	LatencyMS      int        `json:"latencyMs"`
	Error          string     `json:"error"`
	CreatedAt      time.Time  `json:"createdAt"`
}

type ToolCall struct {
	Name string            `json:"name"`
	Args map[string]string `json:"args"`
}

type MemoryTools interface {
	Snapshot(ctx context.Context, matchID string) (matchstate.Snapshot, error)
	RecentEvents(ctx context.Context, matchID string, limit int) ([]matchstate.MatchEvent, error)
	EventsByPlayer(ctx context.Context, matchID, playerName string, limit int) ([]matchstate.MatchEvent, error)
	WriteTrace(ctx context.Context, trace Trace) error
}

type Agent struct {
	tools MemoryTools
}

func NewAgent(tools MemoryTools) *Agent {
	return &Agent{tools: tools}
}

func (a *Agent) HandleMessage(ctx context.Context, req MessageRequest) (Response, error) {
	start := time.Now()
	intent := Classify(req.Text)
	trace := Trace{
		ID:        traceID(req.Now),
		MatchID:   req.MatchID,
		UserID:    req.UserID,
		Input:     req.Text,
		Intent:    intent,
		CreatedAt: req.Now,
	}

	var reply string
	switch intent {
	case IntentMatchStatus:
		snapshot, err := a.tools.Snapshot(ctx, req.MatchID)
		if err != nil {
			return Response{}, err
		}
		trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "match.read_snapshot", Args: map[string]string{"matchId": req.MatchID}})
		reply = fmt.Sprintf("现在是%s %d-%d %s，时间在%s %s。", snapshot.HomeTeam, snapshot.Score.Home, snapshot.Score.Away, snapshot.AwayTeam, displayPeriod(snapshot.Period), snapshot.Clock)
	case IntentRecentEvent:
		events, err := a.tools.RecentEvents(ctx, req.MatchID, 8)
		if err != nil {
			return Response{}, err
		}
		trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "match.search_events", Args: map[string]string{"matchId": req.MatchID, "limit": "8"}})
		reply, trace.RetrievedEvent = answerRecentEvent(req.Text, events)
	case IntentPlayerQuestion:
		player := inferPlayer(req.Text)
		events, err := a.tools.EventsByPlayer(ctx, req.MatchID, player, 8)
		if err != nil {
			return Response{}, err
		}
		trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "match.get_player_timeline", Args: map[string]string{"matchId": req.MatchID, "playerName": player, "limit": "8"}})
		reply, trace.RetrievedEvent = answerPlayerQuestion(req.Text, player, events)
	case IntentControlCommand:
		reply = "收到，我会少说一点，关键变化再提醒你。"
	case IntentEmotionReaction:
		reply = "哈哈我也有点上头，但我会盯住导演台给到的事实，不乱编。"
	case IntentSmalltalk:
		reply = "我在，陪你看。你想聊比赛我就跟着场上节奏走，想闲聊也行。"
	default:
		reply = "这个我先按陪看来理解。要是你问当场比赛，我会只根据导演台已经记录的事件回答。"
	}

	trace.Output = reply
	trace.LatencyMS = int(time.Since(start).Milliseconds())
	if trace.Reason == "" {
		trace.Reason = "deterministic_companion_policy"
	}
	if err := a.tools.WriteTrace(ctx, trace); err != nil {
		return Response{}, err
	}
	return Response{Intent: intent, Reply: reply, Trace: trace}, nil
}

func (a *Agent) HandleProactiveEvent(ctx context.Context, userID string, ev matchstate.MatchEvent, snapshot matchstate.Snapshot) (ProactiveResponse, error) {
	start := time.Now()
	trace := Trace{
		ID:        traceID(time.Now()),
		MatchID:   ev.MatchID,
		UserID:    userID,
		Input:     ev.Description,
		Intent:    IntentRecentEvent,
		CreatedAt: time.Now(),
		ToolCalls: []ToolCall{{Name: "response.emit_companion_reply", Args: map[string]string{
			"eventId":   ev.ID,
			"eventType": ev.EventType,
			"clock":     ev.Clock,
		}}},
		RetrievedEvent: []string{ev.ID},
		Reason:         "operator_event_proactive_line",
	}
	reply := ev.ProactiveText
	if strings.TrimSpace(reply) == "" {
		reply = fallbackProactive(ev, snapshot)
	}
	trace.Output = reply
	trace.LatencyMS = int(time.Since(start).Milliseconds())
	if err := a.tools.WriteTrace(ctx, trace); err != nil {
		return ProactiveResponse{}, err
	}
	return ProactiveResponse{Reply: reply, Trace: trace}, nil
}

func Classify(text string) Intent {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return IntentUnknown
	}
	lower := strings.ToLower(trimmed)
	if containsAny(lower, "别说", "少说", "闭嘴", "安静", "别播报") {
		return IntentControlCommand
	}
	if containsAny(lower, "哈哈", "太激动", "紧张", "舒服", "漂亮", "牛") {
		return IntentEmotionReaction
	}
	if containsAny(lower, "几比几", "比分", "现在多少", "现在几") {
		return IntentMatchStatus
	}
	if containsAny(lower, "谁助攻", "助攻", "刚才谁", "上一个", "刚刚") {
		return IntentRecentEvent
	}
	if containsAny(lower, "进球了吗", "表现", "有没有进球", "有进球") {
		return IntentPlayerQuestion
	}
	if len([]rune(trimmed)) <= 12 {
		return IntentSmalltalk
	}
	return IntentUnknown
}

func answerRecentEvent(text string, events []matchstate.MatchEvent) (string, []string) {
	if containsAny(text, "助攻") {
		for _, ev := range events {
			if ev.EventType != "goal" {
				continue
			}
			assist := participantNames(ev.Participants, "assist")
			preAssist := participantNames(ev.Participants, "pre_assist")
			var parts []string
			if len(assist) > 0 {
				parts = append(parts, strings.Join(assist, "、")+"助攻")
			}
			if len(preAssist) > 0 {
				parts = append(parts, strings.Join(preAssist, "、")+"参与策动")
			}
			if len(parts) == 0 {
				return "我这边只看到刚才有进球记录，但没有看到明确助攻人。", []string{ev.ID}
			}
			return "刚才这球是" + strings.Join(parts, "，") + "。", []string{ev.ID}
		}
		return "我这边目前还没看到进球助攻记录。", nil
	}
	if len(events) == 0 {
		return "我这边目前还没有导演台录入的实时事件。", nil
	}
	ev := events[0]
	return fmt.Sprintf("刚才是%s %s：%s", ev.Clock, eventLabel(ev.EventType), ev.Description), []string{ev.ID}
}

func answerPlayerQuestion(text, player string, events []matchstate.MatchEvent) (string, []string) {
	if player == "" {
		return "你说的是哪位球员？我可以按导演台记录帮你翻一下。", nil
	}
	var ids []string
	for _, ev := range events {
		ids = append(ids, ev.ID)
		if ev.EventType == "goal" && containsAny(text, "进球") {
			return fmt.Sprintf("有，我这边看到%s在%s有进球记录：%s。", player, ev.Clock, ev.Description), ids
		}
	}
	if containsAny(text, "进球") {
		return fmt.Sprintf("我这边目前没有看到%s的进球记录。", player), ids
	}
	if len(events) == 0 {
		return fmt.Sprintf("我这边目前还没有%s的实时事件记录。", player), nil
	}
	return fmt.Sprintf("我这边看到%s最近参与了%d条事件，最新一条是：%s。", player, len(events), events[0].Description), ids
}

func fallbackProactive(ev matchstate.MatchEvent, snapshot matchstate.Snapshot) string {
	switch ev.EventType {
	case "goal":
		if ev.PlayerName != "" {
			return fmt.Sprintf("%s进了！现在%s %d-%d %s。", ev.PlayerName, snapshot.HomeTeam, snapshot.Score.Home, snapshot.Score.Away, snapshot.AwayTeam)
		}
		return fmt.Sprintf("进球了！现在%s %d-%d %s。", snapshot.HomeTeam, snapshot.Score.Home, snapshot.Score.Away, snapshot.AwayTeam)
	case "penalty", "var_check", "big_chance":
		return "这一下很关键，我们先看裁判和双方球员怎么反应。"
	default:
		if ev.Description != "" {
			return ev.Description
		}
		return "场上有新变化，我先陪你盯着。"
	}
}

func participantNames(participants []matchstate.Participant, role string) []string {
	var names []string
	for _, p := range participants {
		if p.Role == role && p.Name != "" {
			names = append(names, p.Name)
		}
	}
	return names
}

func inferPlayer(text string) string {
	known := []string{"佩德里", "法比安", "亚马尔", "穆西亚拉", "莫拉塔", "哈弗茨", "菲尔克鲁格", "Pedri", "Musiala"}
	for _, name := range known {
		if strings.Contains(text, name) {
			return name
		}
	}
	return ""
}

func eventLabel(eventType string) string {
	switch eventType {
	case "goal":
		return "进球"
	case "shot":
		return "射门"
	case "save":
		return "扑救"
	case "penalty":
		return "点球"
	case "var_check":
		return "VAR"
	default:
		return eventType
	}
}

func displayPeriod(period string) string {
	switch period {
	case "first_half":
		return "上半场"
	case "second_half":
		return "下半场"
	case "halftime":
		return "中场"
	case "fulltime":
		return "完场"
	default:
		return period
	}
}

func containsAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func traceID(now time.Time) string {
	if now.IsZero() {
		now = time.Now()
	}
	return fmt.Sprintf("trace_%d", now.UnixNano())
}

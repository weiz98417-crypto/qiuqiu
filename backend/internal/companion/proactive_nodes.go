package companion

// 赛事节奏节点（openspec/changes/proactive-match-nodes）：中场闲聊与失球
// 安慰。复盘邀约的簿子侧在 internal/proactive（review.go）+ main.go 观察者。
// 两节点都搭事件反应拍的既有路径：中场换确定性摘要为回复底稿，失球安慰是
// 进球话轮的前置前缀——不立独立话轮、不动 ADR-0015 门机器。

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/teamalign"
)

// HalftimeBreakReply 是中场节点的确定性底稿：比分事实不 LLM 化，realizer
// 在其上织写闲聊语气（Guard 的来源集含 ReliableText，比分数字安全）。
func HalftimeBreakReply(snapshot matchstate.Snapshot) string {
	home := strings.TrimSpace(snapshot.HomeTeam)
	away := strings.TrimSpace(snapshot.AwayTeam)
	if home == "" || away == "" {
		return "上半场结束，中场休息。下半场回来我接着陪你看。"
	}
	return fmt.Sprintf("上半场结束，%s %d:%d %s。中场歇会儿，下半场回来我接着陪你唠。",
		home, snapshot.Score.Home, snapshot.Score.Away, away)
}

// goalComfortOnce 记录每用户每场已发过的失球安慰（proactive-match-nodes：
// 每场 ≤1）。进程内有界 FIFO，与 knowledgeTriggerStates 同纪律。
type goalComfortOnce struct {
	mu    sync.Mutex
	order []string
	seen  map[string]bool
}

func newGoalComfortOnce() *goalComfortOnce {
	return &goalComfortOnce{seen: map[string]bool{}}
}

func (g *goalComfortOnce) mark(userID, matchID string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	key := userID + "|" + matchID
	if g.seen[key] {
		return false
	}
	g.seen[key] = true
	g.order = append(g.order, key)
	for len(g.seen) > 512 {
		oldest := g.order[0]
		g.order = g.order[1:]
		delete(g.seen, oldest)
	}
	return true
}

// concededTeam 从进球事件推导丢球一方：事件 TeamName 是进球方，对阵另一方
// 即丢球方。队名缺失返回空。
func concededTeam(ev matchstate.MatchEvent, snapshot matchstate.Snapshot) string {
	scorer := strings.TrimSpace(ev.TeamName)
	home := strings.TrimSpace(snapshot.HomeTeam)
	away := strings.TrimSpace(snapshot.AwayTeam)
	switch {
	case scorer != "" && scorer == home:
		return away
	case scorer != "" && scorer == away:
		return home
	default:
		return ""
	}
}

// goalComfortPrefix 判定失球安慰（ADR-0019 留尾清偿）：丢球方命中用户订阅
// 球队（teamalign 对齐）→ 返回安慰前缀并记账；否则空串。安静档不安慰——
// 与微反应同一「安静管发言」边界。
func (a *Agent) goalComfortPrefix(userID string, ev matchstate.MatchEvent, snapshot matchstate.Snapshot, talkativeness string, trace *Trace) string {
	if a == nil || a.subscriptions == nil || a.comfortSent == nil {
		return ""
	}
	if talkativeness == "quiet" || ev.EventType != "goal" {
		return ""
	}
	conceded := concededTeam(ev, snapshot)
	if conceded == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	subs, err := a.subscriptions.ActiveForUser(ctx, userID)
	if err != nil || len(subs) == 0 {
		return ""
	}
	for _, sub := range subs {
		if teamalign.Aligns(sub.TeamName, conceded) {
			if !a.comfortSent.mark(userID, ev.MatchID) {
				return ""
			}
			if trace != nil {
				trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "relationship.goal_comfort", Args: map[string]string{"team": conceded}})
			}
			clock := strings.TrimSpace(ev.Clock)
			if clock != "" {
				return fmt.Sprintf("%s丢球了，别急着上头，才到 %s。", conceded, clock)
			}
			return conceded + "丢球了，别急着上头，比赛还长。"
		}
	}
	return ""
}

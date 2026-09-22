package companion

// 赛事节奏节点（proactive-match-nodes）的单元锁：中场确定性摘要、失球安慰
// （订阅命中才触发、每场一次、quiet 禁用）。

import (
	"context"
	"strings"
	"testing"
	"time"

	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/proactive"
	"qiuqiu/internal/relationship"
)

func snapshotForComfort() matchstate.Snapshot {
	return matchstate.Snapshot{
		MatchID:  "match-1",
		HomeTeam: "利物浦",
		AwayTeam: "切尔西",
		Score:    matchstate.Score{Home: 1, Away: 0},
		Period:   "halftime",
	}
}

func TestHalftimeBreakReplyCarriesScore(t *testing.T) {
	reply := HalftimeBreakReply(snapshotForComfort())
	if !strings.Contains(reply, "利物浦 1:0 切尔西") || !strings.Contains(reply, "中场") {
		t.Fatalf("reply = %q, want halftime summary with score", reply)
	}
}

func TestHandleMatchEventHalftimeUsesSummary(t *testing.T) {
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore())).WithDirector(relationship.NewDirector(relationship.NewMemoryRepository()))
	request := MatchEventRequest{
		UserID: "user-1",
		Event: matchstate.MatchEvent{
			ID: "half-1", MatchID: "match-1", EventType: "halftime",
			Description: "半场结束", Confirmed: true,
		},
		Snapshot:      snapshotForComfort(),
		OutputAllowed: true,
		Talkativeness: "normal",
		Now:           time.Date(2026, 9, 23, 20, 45, 0, 0, time.UTC),
	}
	response, err := agent.HandleMatchEvent(context.Background(), request)
	if err != nil {
		t.Fatalf("HandleMatchEvent: %v", err)
	}
	if !strings.Contains(response.Reply, "利物浦 1:0 切尔西") {
		t.Fatalf("halftime reply = %q, want summary", response.Reply)
	}
}

func TestGoalComfortRequiresSubscribedTeam(t *testing.T) {
	subs := proactive.NewMemorySubscriptionStore()
	if _, err := subs.Append(context.Background(), proactive.Subscription{
		UserID: "user-1", TeamName: "切尔西", Status: "active",
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore())).WithDirector(relationship.NewDirector(relationship.NewMemoryRepository())).WithSubscriptions(subs)

	request := MatchEventRequest{
		UserID: "user-1",
		Event: matchstate.MatchEvent{
			ID: "goal-1", MatchID: "match-1", EventType: "goal",
			TeamName: "利物浦", Clock: "33'", Description: "利物浦进球", Confirmed: true,
		},
		Snapshot:      snapshotForComfort(),
		OutputAllowed: true,
		Talkativeness: "normal",
		Now:           time.Date(2026, 9, 23, 20, 33, 0, 0, time.UTC),
	}
	response, err := agent.HandleMatchEvent(context.Background(), request)
	if err != nil {
		t.Fatalf("goal for subscribed conceded team: %v", err)
	}
	// 利物浦进球 → 切尔西（用户订阅队）丢球 → 安慰前缀。
	if !strings.Contains(response.Reply, "切尔西丢球了") {
		t.Fatalf("reply = %q, want comfort prefix", response.Reply)
	}

	// 同一场第二次丢球不重复安慰。
	request.Event.ID = "goal-2"
	request.Event.Clock = "40'"
	second, err := agent.HandleMatchEvent(context.Background(), request)
	if err != nil {
		t.Fatalf("second goal: %v", err)
	}
	if strings.Contains(second.Reply, "切尔西丢球了") {
		t.Fatalf("second reply = %q, want comfort sent once per match", second.Reply)
	}

	// 对方（非订阅队）丢球不安慰。
	request.Event.ID = "goal-3"
	request.Event.TeamName = "切尔西"
	request.Event.Clock = "44'"
	third, err := agent.HandleMatchEvent(context.Background(), request)
	if err != nil {
		t.Fatalf("opponent goal: %v", err)
	}
	if strings.Contains(third.Reply, "丢球了，别急着上头") {
		t.Fatalf("opponent-goal reply = %q, want no comfort", third.Reply)
	}
}

func TestGoalComfortQuietSilent(t *testing.T) {
	subs := proactive.NewMemorySubscriptionStore()
	if _, err := subs.Append(context.Background(), proactive.Subscription{
		UserID: "user-1", TeamName: "切尔西", Status: "active",
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore())).WithDirector(relationship.NewDirector(relationship.NewMemoryRepository())).WithSubscriptions(subs)
	request := MatchEventRequest{
		UserID: "user-1",
		Event: matchstate.MatchEvent{
			ID: "goal-1", MatchID: "match-1", EventType: "goal",
			TeamName: "利物浦", Description: "利物浦进球", Confirmed: true,
		},
		Snapshot:      snapshotForComfort(),
		OutputAllowed: true,
		Talkativeness: "quiet",
		Now:           time.Date(2026, 9, 23, 20, 33, 0, 0, time.UTC),
	}
	response, err := agent.HandleMatchEvent(context.Background(), request)
	if err != nil {
		t.Fatalf("HandleMatchEvent: %v", err)
	}
	if strings.Contains(response.Reply, "丢球了，别急着上头") {
		t.Fatalf("quiet reply = %q, want no comfort", response.Reply)
	}
}

func TestFulltimeReviewReminderTiming(t *testing.T) {
	ft := time.Date(2026, 9, 23, 22, 0, 0, 0, time.UTC)
	reminder, err := proactive.NewReminder(proactive.Reminder{
		UserID: "user-1", MatchID: "match-1",
		HomeTeam: "利物浦", AwayTeam: "切尔西",
		KickoffAt: ft, Kind: proactive.KindFulltimeReview,
	})
	if err != nil {
		t.Fatalf("NewReminder: %v", err)
	}
	if !reminder.DeliverAt.Equal(ft.Add(15*time.Minute)) {
		t.Fatalf("deliver at %v, want FT+15min", reminder.DeliverAt)
	}
	if !reminder.ExpireAt.Equal(ft.Add(2*time.Hour)) {
		t.Fatalf("expire at %v, want FT+2h", reminder.ExpireAt)
	}
	if !reminder.Due(ft.Add(16 * time.Minute)) {
		t.Fatalf("reminder should be due at FT+16min")
	}
	if reminder.Due(ft.Add(3 * time.Hour)) {
		t.Fatalf("reminder should be expired at FT+3h")
	}
	if !strings.Contains(proactive.FulltimeReviewReply(reminder), "利物浦 对 切尔西") {
		t.Fatalf("review reply = %q", proactive.FulltimeReviewReply(reminder))
	}
}

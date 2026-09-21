package companion

// subscription_manage 意图与队名对齐的单元锁（openspec/changes/
// season-subscription）：订阅/列表/取消/上限，以及同名前缀不误配。

import (
	"context"
	"strings"
	"testing"
	"time"

	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/proactive"
)

func TestTeamNameAlignsRejectsReserveTeams(t *testing.T) {
	if proactive.TeamNameAligns("曼联", "曼联U21") {
		t.Fatal("reserve team must not align with the senior team")
	}
	if !proactive.TeamNameAligns("皇马", "皇家马德里") {
		t.Fatal("short name must align with the full name within 2 runes")
	}
	if proactive.TeamNameAligns("", "皇家马德里") || proactive.TeamNameAligns("皇马", "") {
		t.Fatal("empty names must never align")
	}
}

func subscriptionFixtureTools(home, away, period string) *snapshotOverrideTools {
	return &snapshotOverrideTools{
		StoreMemoryTools: NewStoreMemoryTools(matchstate.NewStore()),
		snapshot: matchstate.Snapshot{
			MatchID: "m1", HomeTeam: home, AwayTeam: away, Period: period, Clock: "00:00",
		},
	}
}

func TestSubscriptionManageSubscribeListCancel(t *testing.T) {
	kickoff := time.Date(2026, 10, 1, 20, 0, 0, 0, time.UTC)
	book := proactive.NewMemorySubscriptionStore()
	agent := NewAgent(subscriptionFixtureTools("西班牙", "德国", "pre_match")).
		WithScheduleReader(scheduleReaderStub{fixtures: []ScheduleMatch{{
			HomeTeam: "皇家马德里", AwayTeam: "巴塞罗那", KickoffAt: kickoff,
		}}}).
		WithSubscriptions(book)

	// 订阅：用户口语里的短名对齐赛程全名。
	handling, err := agent.handleSubscriptionManage(&userTurn{
		ctx:   context.Background(),
		req:   AgentBoundaryRequest{MatchID: "m1", UserID: "user-1", Text: "以后皇马的比赛都叫我", Now: kickoff.Add(-72 * time.Hour)},
		trace: &Trace{ID: "t-sub"},
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if !strings.Contains(handling.reply, "皇家马德里") {
		t.Fatalf("reply = %q, want the subscription confirmation", handling.reply)
	}
	subs, _ := book.ActiveForUser(context.Background(), "user-1")
	if len(subs) != 1 || !strings.Contains(subs[0].TeamName, "马德里") {
		t.Fatalf("subs = %+v", subs)
	}

	// 列表：确定性拼装。
	handling, err = agent.handleSubscriptionManage(&userTurn{
		ctx:   context.Background(),
		req:   AgentBoundaryRequest{MatchID: "m1", UserID: "user-1", Text: "列出我的订阅"},
		trace: &Trace{ID: "t-sub-list"},
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(handling.reply, "皇家马德里") {
		t.Fatalf("list reply = %q", handling.reply)
	}

	// 取消：翻 cancelled，再列表为空。
	if _, err = agent.handleSubscriptionManage(&userTurn{
		ctx:   context.Background(),
		req:   AgentBoundaryRequest{MatchID: "m1", UserID: "user-1", Text: "别叫我皇马的了"},
		trace: &Trace{ID: "t-sub-cancel"},
	}); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if subs, _ = book.ActiveForUser(context.Background(), "user-1"); len(subs) != 0 {
		t.Fatalf("subs after cancel = %+v", subs)
	}
}

func TestSubscriptionManageEnforcesLimit(t *testing.T) {
	book := proactive.NewMemorySubscriptionStore()
	agent := NewAgent(subscriptionFixtureTools("西班牙", "德国", "pre_match")).
		WithScheduleReader(scheduleReaderStub{fixtures: []ScheduleMatch{{
			HomeTeam: "皇家马德里", AwayTeam: "巴塞罗那", KickoffAt: time.Now().Add(48 * time.Hour),
		}}}).
		WithSubscriptions(book)
	ctx := context.Background()
	for _, team := range []string{"A队", "B队", "C队"} {
		if _, err := book.Append(ctx, proactive.Subscription{UserID: "user-1", TeamName: team}); err != nil {
			t.Fatalf("seed sub: %v", err)
		}
	}
	handling, err := agent.handleSubscriptionManage(&userTurn{
		ctx:   ctx,
		req:   AgentBoundaryRequest{MatchID: "m1", UserID: "user-1", Text: "以后皇马的比赛都叫我"},
		trace: &Trace{ID: "t-sub-limit"},
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if !strings.Contains(handling.reply, "三支") {
		t.Fatalf("reply = %q, want the limit notice", handling.reply)
	}
	if subs, _ := book.ActiveForUser(ctx, "user-1"); len(subs) != 3 {
		t.Fatalf("subs = %d, want the cap respected", len(subs))
	}
}

func TestSubscriptionManageDegradesWithoutStore(t *testing.T) {
	agent := NewAgent(subscriptionFixtureTools("西班牙", "德国", "pre_match"))
	handling, err := agent.handleSubscriptionManage(&userTurn{
		ctx:   context.Background(),
		req:   AgentBoundaryRequest{MatchID: "m1", UserID: "user-1", Text: "以后皇马的比赛都叫我"},
		trace: &Trace{ID: "t-sub-nil"},
	})
	if err != nil {
		t.Fatalf("handleSubscriptionManage: %v", err)
	}
	if !strings.Contains(handling.reply, "还没接上") {
		t.Fatalf("reply = %q", handling.reply)
	}
}

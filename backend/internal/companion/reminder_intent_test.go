package companion

// reminder_request 意图的单元锁（openspec/changes/proactive-scheduler）：
// pre_match + 赛程源有 kickoff → 落簿并确定性回复；非 pre_match / 无
// kickoff / 未接提醒簿 → 如实回话不假装记上了。

import (
	"context"
	"strings"
	"testing"
	"time"

	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/proactive"
)

type scheduleReaderStub struct {
	fixtures []ScheduleMatch
}

func (s scheduleReaderStub) TodayFixtures(context.Context) ([]ScheduleMatch, error) {
	return s.fixtures, nil
}

func TestReminderRequestSchedulesPreMatchNudge(t *testing.T) {
	kickoff := time.Date(2026, 9, 22, 20, 0, 0, 0, time.UTC)
	tools := &snapshotOverrideTools{
		StoreMemoryTools: NewStoreMemoryTools(matchstate.NewStore()),
		snapshot: matchstate.Snapshot{
			MatchID: "m1", HomeTeam: "西班牙", AwayTeam: "德国",
			Period: "pre_match", Clock: "00:00",
		},
	}
	book := proactive.NewMemoryStore()
	agent := NewAgent(tools).
		WithScheduleReader(scheduleReaderStub{fixtures: []ScheduleMatch{{
			HomeTeam: "西班牙", AwayTeam: "德国", KickoffAt: kickoff,
		}}}).
		WithReminders(book)

	handling, err := agent.handleReminderRequest(&userTurn{
		ctx:   context.Background(),
		req:   AgentBoundaryRequest{MatchID: "m1", UserID: "user-1", Now: kickoff.Add(-2 * time.Hour)},
		trace: &Trace{ID: "t-reminder"},
	})
	if err != nil {
		t.Fatalf("handleReminderRequest: %v", err)
	}
	if !strings.Contains(handling.reply, "30分钟") {
		t.Fatalf("reply = %q, want the lead time promised", handling.reply)
	}
	pendings, err := book.PendingForUser(context.Background(), "user-1")
	if err != nil || len(pendings) != 1 {
		t.Fatalf("pending reminders = %v, %v", pendings, err)
	}
	if !pendings[0].KickoffAt.Equal(kickoff) {
		t.Fatalf("kickoff = %v, want %v", pendings[0].KickoffAt, kickoff)
	}
}

func TestReminderRequestRefusesAfterKickoff(t *testing.T) {
	tools := &snapshotOverrideTools{
		StoreMemoryTools: NewStoreMemoryTools(matchstate.NewStore()),
		snapshot: matchstate.Snapshot{
			MatchID: "m1", HomeTeam: "西班牙", AwayTeam: "德国",
			Period: "first_half", Clock: "12:00",
		},
	}
	agent := NewAgent(tools).WithReminders(proactive.NewMemoryStore())
	handling, err := agent.handleReminderRequest(&userTurn{
		ctx:   context.Background(),
		req:   AgentBoundaryRequest{MatchID: "m1", UserID: "user-1"},
		trace: &Trace{ID: "t-reminder-late"},
	})
	if err != nil {
		t.Fatalf("handleReminderRequest: %v", err)
	}
	if handling.deterministicReason != "reminder_policy" || strings.Contains(handling.reply, "好，") {
		t.Fatalf("reply = %q, want the honest refusal", handling.reply)
	}
}

func TestReminderRequestDegradesWithoutBookOrKickoff(t *testing.T) {
	// 未接提醒簿：如实说没接上。
	agent := NewAgent(&snapshotOverrideTools{
		StoreMemoryTools: NewStoreMemoryTools(matchstate.NewStore()),
		snapshot:         matchstate.Snapshot{MatchID: "m1", Period: "pre_match"},
	})
	handling, err := agent.handleReminderRequest(&userTurn{
		ctx: context.Background(), req: AgentBoundaryRequest{MatchID: "m1"}, trace: &Trace{ID: "t1"},
	})
	if err != nil {
		t.Fatalf("handleReminderRequest: %v", err)
	}
	if !strings.Contains(handling.reply, "还没接上") {
		t.Fatalf("reply = %q", handling.reply)
	}

	// 接了簿但赛程源没给 kickoff：如实说不知道时间，不落簿。
	book := proactive.NewMemoryStore()
	agent2 := NewAgent(&snapshotOverrideTools{
		StoreMemoryTools: NewStoreMemoryTools(matchstate.NewStore()),
		snapshot:         matchstate.Snapshot{MatchID: "m1", HomeTeam: "西班牙", AwayTeam: "德国", Period: "pre_match"},
	}).WithScheduleReader(scheduleReaderStub{}).WithReminders(book)
	handling, err = agent2.handleReminderRequest(&userTurn{
		ctx: context.Background(), req: AgentBoundaryRequest{MatchID: "m1", UserID: "user-1"}, trace: &Trace{ID: "t2"},
	})
	if err != nil {
		t.Fatalf("handleReminderRequest: %v", err)
	}
	if !strings.Contains(handling.reply, "几点开球") {
		t.Fatalf("reply = %q", handling.reply)
	}
	if pendings, _ := book.PendingForUser(context.Background(), "user-1"); len(pendings) != 0 {
		t.Fatalf("kickoff-less reminder must not be scheduled: %v", pendings)
	}
}

// registry 声明与 flag 的回归：reminder_request 走置信门、非闲聊回复资格。
func TestReminderRequestRegistryFlags(t *testing.T) {
	spec := intentRegistry.specFor(IntentReminderRequest)
	if !spec.ConfidenceGated || spec.ReplyEligible || spec.IsFact {
		t.Fatalf("spec flags = %+v", spec)
	}
	if confidenceGatedIntent(IntentReminderRequest) != true || routerReplyEligibleIntent(IntentReminderRequest) != false {
		t.Fatal("registry lookups drifted from spec flags")
	}
}

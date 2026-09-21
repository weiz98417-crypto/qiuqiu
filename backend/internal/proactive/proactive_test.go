package proactive

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestNewReminderDerivesDeliveryWindow(t *testing.T) {
	kickoff := time.Date(2026, 9, 22, 20, 0, 0, 0, time.UTC)
	reminder, err := NewReminder(Reminder{UserID: "user-1", MatchID: "m1", KickoffAt: kickoff})
	if err != nil {
		t.Fatalf("NewReminder: %v", err)
	}
	if reminder.LeadMinutes != DefaultLeadMinutes {
		t.Fatalf("lead = %d, want default %d", reminder.LeadMinutes, DefaultLeadMinutes)
	}
	if want := kickoff.Add(-DefaultLeadMinutes * time.Minute); !reminder.DeliverAt.Equal(want) {
		t.Fatalf("deliverAt = %v, want %v", reminder.DeliverAt, want)
	}
	if want := kickoff.Add(ExpireAfterKickoff); !reminder.ExpireAt.Equal(want) {
		t.Fatalf("expireAt = %v, want %v", reminder.ExpireAt, want)
	}
	if reminder.CitationCode() != "reminder:"+reminder.ID || !strings.HasPrefix(reminder.CitationCode(), CitationPrefix+":") {
		t.Fatalf("citation = %q", reminder.CitationCode())
	}
	if _, err := NewReminder(Reminder{UserID: "user-1"}); err == nil {
		t.Fatal("reminder without a kickoff must be rejected")
	}
}

func TestReminderDueWindow(t *testing.T) {
	kickoff := time.Date(2026, 9, 22, 20, 0, 0, 0, time.UTC)
	reminder, _ := NewReminder(Reminder{UserID: "user-1", KickoffAt: kickoff})
	before := kickoff.Add(-31 * time.Minute)
	during := kickoff.Add(-15 * time.Minute)
	after := kickoff.Add(31 * time.Minute)
	if reminder.Due(before) || !reminder.Due(during) || reminder.Due(after) {
		t.Fatalf("due window wrong: before=%v during=%v after=%v", reminder.Due(before), reminder.Due(during), reminder.Due(after))
	}
}

func TestMemoryStoreAppendDueSweep(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	now := time.Date(2026, 9, 22, 19, 40, 0, 0, time.UTC)
	reminder, err := store.Append(ctx, Reminder{
		UserID: "user-1", MatchID: "m1", HomeTeam: "西班牙", AwayTeam: "德国",
		KickoffAt: now.Add(20 * time.Minute), CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}

	// 到点即 due；未到点不 due。
	due, err := store.DuePending(ctx, now)
	if err != nil || len(due) != 1 {
		t.Fatalf("DuePending at deliver time = %v, %v", due, err)
	}
	if due[0].ID != reminder.ID {
		t.Fatalf("due id = %q, want %q", due[0].ID, reminder.ID)
	}
	early, err := store.DuePending(ctx, now.Add(-11*time.Minute))
	if err != nil || len(early) != 0 {
		t.Fatalf("DuePending before deliver time = %v, %v", early, err)
	}

	// 送达翻状态，之后不再 due。
	if err := store.MarkDelivered(ctx, reminder.ID); err != nil {
		t.Fatalf("MarkDelivered: %v", err)
	}
	if due, _ = store.DuePending(ctx, now); len(due) != 0 {
		t.Fatalf("delivered reminder still due: %v", due)
	}

	// 过期清扫：pending + 越过 expire_at → suppressed 并返回（转记忆素材）。
	missed, _ := store.Append(ctx, Reminder{UserID: "user-2", KickoffAt: now.Add(-time.Hour), CreatedAt: now})
	suppressed, err := store.SweepSuppressed(ctx, now)
	if err != nil || len(suppressed) != 1 || suppressed[0].ID != missed.ID {
		t.Fatalf("SweepSuppressed = %v, %v", suppressed, err)
	}
	pendings, _ := store.PendingForUser(ctx, "user-2")
	if len(pendings) != 0 {
		t.Fatalf("suppressed reminder still pending for user: %v", pendings)
	}
}

func TestPreMatchReminderReplyMentionsTeamsAndKickoff(t *testing.T) {
	kickoff := time.Date(2026, 9, 22, 20, 0, 0, 0, time.UTC)
	reminder, _ := NewReminder(Reminder{UserID: "u", HomeTeam: "西班牙", AwayTeam: "德国", KickoffAt: kickoff, Timezone: "UTC"})
	reply := PreMatchReminderReply(reminder)
	if !strings.Contains(reply, "西班牙") || !strings.Contains(reply, "德国") || !strings.Contains(reply, "20:00") {
		t.Fatalf("reply = %q", reply)
	}
}

func TestMemoryStoreMarkDeliveredRespectsPendingGuard(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	now := time.Date(2026, 9, 22, 21, 0, 0, 0, time.UTC)
	reminder, _ := store.Append(ctx, Reminder{UserID: "u", KickoffAt: now.Add(-30 * time.Minute), CreatedAt: now})
	// 竞态窗口：清扫先把它翻 suppressed，迟到的送达不得改回 delivered
	//（ADR-0015 错过即静默）。
	if _, err := store.SweepSuppressed(ctx, now); err != nil {
		t.Fatalf("SweepSuppressed: %v", err)
	}
	if err := store.MarkDelivered(ctx, reminder.ID); err == nil {
		t.Fatal("MarkDelivered on a suppressed reminder must fail")
	}
}

func TestSweepLoopMaterializesAndStops(t *testing.T) {
	store := NewMemoryStore()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Now().UTC()
	if _, err := store.Append(ctx, Reminder{
		UserID: "user-1", MatchID: "m1", HomeTeam: "西班牙", AwayTeam: "德国",
		KickoffAt: now.Add(-time.Hour), CreatedAt: now,
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	materialized := make(chan Reminder, 1)
	go SweepLoop(ctx, store, func(_ context.Context, reminder Reminder) {
		materialized <- reminder
		cancel()
	}, 10*time.Millisecond)
	select {
	case reminder := <-materialized:
		if reminder.HomeTeam != "西班牙" || reminder.Status != StatusSuppressed {
			t.Fatalf("materialized = %+v", reminder)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("SweepLoop never materialized the expired reminder")
	}
}

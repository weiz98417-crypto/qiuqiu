package proactive

import (
	"context"
	"testing"
	"time"
)

func TestExpandSubscriptionsCreatesDedupedReminders(t *testing.T) {
	store := NewMemoryStore()
	subs := NewMemorySubscriptionStore()
	ctx := context.Background()
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	sub, err := subs.Append(ctx, Subscription{UserID: "user-1", TeamName: "皇马"})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	fixtures := []Fixture{
		{FixtureID: "f1", HomeTeam: "巴塞罗那", AwayTeam: "皇家马德里", KickoffAt: now.Add(48 * time.Hour)},
		{FixtureID: "f2", HomeTeam: "皇家马德里", AwayTeam: "曼联", KickoffAt: now.Add(96 * time.Hour)},
		{FixtureID: "f3", HomeTeam: "曼联U21", AwayTeam: "切尔西", KickoffAt: now.Add(120 * time.Hour)},
		{FixtureID: "f0", HomeTeam: "皇家马德里", AwayTeam: "马德里竞技", KickoffAt: now.Add(-24 * time.Hour)},
	}
	created := ExpandSubscriptions(ctx, []Subscription{sub}, store, fixtures,
		func(ctx context.Context, userID string) ([]Reminder, error) { return store.PendingForUser(ctx, userID) }, now)
	if created != 2 {
		t.Fatalf("created = %d, want 2 (future team matches only, reserve excluded)", created)
	}
	// 再展开一轮：去重，不重复落簿。
	if again := ExpandSubscriptions(ctx, []Subscription{sub}, store, fixtures,
		func(ctx context.Context, userID string) ([]Reminder, error) { return store.PendingForUser(ctx, userID) }, now); again != 0 {
		t.Fatalf("second expansion created %d, want 0", again)
	}
	pendings, _ := store.PendingForUser(ctx, "user-1")
	for _, reminder := range pendings {
		if reminder.SubscriptionID != sub.ID {
			t.Fatalf("reminder %q missing subscription link", reminder.ID)
		}
		if got := reminder.CitationCode(); got[:12] != "subscription" {
			t.Fatalf("citation = %q", got)
		}
	}
}

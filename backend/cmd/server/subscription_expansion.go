package main

// 订阅展开节拍（openspec/changes/season-subscription）：每日合并扫描
// 未来 14 天赛程，把活跃订阅展开成提醒簿条目（去重键 subscription+fixture，
// Q17/Q9）。赛程源缺席时静默——展开是尽力而为，订阅记录不受影响。

import (
	"context"
	"log"
	"time"

	"qiuqiu/internal/companion"
	"qiuqiu/internal/proactive"
)

func runSubscriptionExpansion(ctx context.Context, subs proactive.SubscriptionStore, reminders proactive.Store, reader companion.ScheduleReader) {
	if subs == nil || reminders == nil {
		return
	}
	ticker := time.NewTicker(12 * time.Hour)
	defer ticker.Stop()
	expand := func() {
		now := time.Now().UTC()
		active, err := subs.Active(ctx)
		if err != nil {
			log.Printf("subscription expansion: %v", err)
			return
		}
		if len(active) == 0 || reader == nil {
			return
		}
		var fixtures []companion.ScheduleMatch
		if searchReader, ok := reader.(companion.ScheduleSearchReader); ok {
			if result, err := searchReader.Search(ctx, companion.ScheduleSearchRequest{
				From: now,
				To:   now.Add(14 * 24 * time.Hour),
			}); err == nil {
				fixtures = result.Fixtures
			}
		} else {
			if today, err := reader.TodayFixtures(ctx); err == nil {
				fixtures = today
			}
		}
		converted := make([]proactive.Fixture, 0, len(fixtures))
		for _, fixture := range fixtures {
			converted = append(converted, proactive.Fixture{
				FixtureID: fixture.FixtureID, HomeTeam: fixture.HomeTeam,
				AwayTeam: fixture.AwayTeam, KickoffAt: fixture.KickoffAt,
			})
		}
		if created := proactive.ExpandSubscriptions(ctx, active, reminders, converted,
			func(ctx context.Context, userID string) ([]proactive.Reminder, error) {
				return reminders.PendingForUser(ctx, userID)
			}, now); created > 0 {
			log.Printf("subscription expansion created %d reminders", created)
		}
	}
	expand()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			expand()
		}
	}
}

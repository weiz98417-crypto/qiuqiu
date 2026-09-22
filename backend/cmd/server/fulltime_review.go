package main

// 赛后复盘邀约（openspec/changes/proactive-match-nodes）：终场事件触发——
// 只为归因窗口里真看过这场比赛的用户建簿（memory.Queue.UsersForEndedMatch），
// due=终场+15min，+2h 过期转 suppressed（补递与过期语义复用 045 outbox）。
// 进程内去重：同一终场事件的 fact revision 重放不再重复建簿。

import (
	"context"
	"log"
	"sync"
	"time"

	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/memory"
	"qiuqiu/internal/proactive"
)

var fulltimeReviewDedupe = struct {
	sync.Mutex
	seen map[string]bool
}{seen: map[string]bool{}}

func scheduleFulltimeReviewReminders(ctx context.Context, store proactive.Store, queue *memory.Queue, matchStore matchstate.Repository, event matchstate.MatchEvent) {
	if store == nil || queue == nil {
		return
	}
	fulltimeReviewDedupe.Lock()
	if fulltimeReviewDedupe.seen[event.MatchID] {
		fulltimeReviewDedupe.Unlock()
		return
	}
	fulltimeReviewDedupe.seen[event.MatchID] = true
	fulltimeReviewDedupe.Unlock()

	users := queue.UsersForEndedMatch(event.MatchID)
	if len(users) == 0 {
		return
	}
	snapshot := matchStore.PublicSnapshot(event.MatchID)
	now := time.Now().UTC()
	for _, userID := range users {
		reminder, err := proactive.NewReminder(proactive.Reminder{
			UserID:   userID,
			MatchID:  event.MatchID,
			HomeTeam: snapshot.HomeTeam,
			AwayTeam: snapshot.AwayTeam,
			// KickoffAt 承载终场时刻：复盘类的时序由 NewReminder 的 Kind
			// 分支正推（+15min 投递 / +2h 过期）。
			KickoffAt: now,
			Kind:      proactive.KindFulltimeReview,
			Status:    proactive.StatusPending,
		})
		if err != nil {
			log.Printf("fulltime review reminder: %v", err)
			continue
		}
		if _, err := store.Append(ctx, reminder); err != nil {
			log.Printf("fulltime review append: %v", err)
		}
	}
}

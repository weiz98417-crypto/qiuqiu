package proactive

// 订阅簿（openspec/changes/season-subscription）：球队级提醒订阅，由每日
// 展开器落成 Reminder（引用码 subscription:<id>，ADR-0015 的第三钥匙延伸
// ——用户显式订阅）。管理走对话即接口；每用户上限 3 支（Q18）。

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// CitationSubscriptionPrefix 是订阅提醒的 C2 引用码前缀。
	CitationSubscriptionPrefix = "subscription"
	// MaxSubscriptionsPerUser 防滥用（Q18）。
	MaxSubscriptionsPerUser = 3
	// SubscriptionStatus 两种终态之一。
	SubscriptionActive    = "active"
	SubscriptionCancelled = "cancelled"
)

type Subscription struct {
	ID        string
	UserID    string
	TeamName  string
	Status    string
	CreatedAt time.Time
}

func (s Subscription) CitationCode() string {
	return CitationSubscriptionPrefix + ":" + s.ID
}

// SubscriptionStore 是订阅簿的接缝（Postgres/内存双实现）。
type SubscriptionStore interface {
	Append(ctx context.Context, sub Subscription) (Subscription, error)
	ActiveForUser(ctx context.Context, userID string) ([]Subscription, error)
	// Active 返回全部用户的活跃订阅（展开器用）。
	Active(ctx context.Context) ([]Subscription, error)
	// CancelTeam 取消该用户某队的活跃订阅（队名对齐口径由实现保证与
	// intent 侧一致），返回被取消的订阅。
	CancelTeam(ctx context.Context, userID, teamName string) (Subscription, bool, error)
}

// MemorySubscriptionStore 测试/无库降级实现。
type MemorySubscriptionStore struct {
	mu    sync.Mutex
	subs  []Subscription
	next  int
}

func NewMemorySubscriptionStore() *MemorySubscriptionStore {
	return &MemorySubscriptionStore{}
}

func (s *MemorySubscriptionStore) Append(_ context.Context, sub Subscription) (Subscription, error) {
	if s == nil {
		return Subscription{}, fmt.Errorf("subscription store unavailable")
	}
	if strings.TrimSpace(sub.UserID) == "" || strings.TrimSpace(sub.TeamName) == "" {
		return Subscription{}, fmt.Errorf("subscription needs user and team")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.subs {
		if existing.UserID == sub.UserID && existing.Status == SubscriptionActive &&
			teamNameMatches(existing.TeamName, sub.TeamName) {
			return existing, nil
		}
	}
	s.next++
	sub.ID = fmt.Sprintf("sub-%d", s.next)
	sub.Status = SubscriptionActive
	if sub.CreatedAt.IsZero() {
		sub.CreatedAt = time.Now().UTC()
	}
	s.subs = append(s.subs, sub)
	return sub, nil
}

func (s *MemorySubscriptionStore) ActiveForUser(_ context.Context, userID string) ([]Subscription, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Subscription, 0)
	for _, sub := range s.subs {
		if sub.UserID == userID && sub.Status == SubscriptionActive {
			out = append(out, sub)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s *MemorySubscriptionStore) CancelTeam(_ context.Context, userID, teamName string) (Subscription, bool, error) {
	if s == nil {
		return Subscription{}, false, fmt.Errorf("subscription store unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.subs {
		sub := &s.subs[i]
		if sub.UserID == userID && sub.Status == SubscriptionActive && teamNameMatches(sub.TeamName, teamName) {
			sub.Status = SubscriptionCancelled
			return *sub, true, nil
		}
	}
	return Subscription{}, false, nil
}

// Fixture 是展开器的最小赛程投影（companion.ScheduleMatch 折算进来）。
type Fixture struct {
	FixtureID string
	HomeTeam  string
	AwayTeam  string
	KickoffAt time.Time
}

// ExpandSubscriptions 把活跃订阅展开成未来赛程的 Reminder（Q9：调用方带
// 已扫描窗口的赛程与该用户在途提醒；去重键 = subscription+fixture）。
// 纯函数、可测；投递仍走提醒簿的两腿。返回新落簿数。
func ExpandSubscriptions(ctx context.Context, subs []Subscription, reminders Store, fixtures []Fixture, allForUser func(ctx context.Context, userID string) ([]Reminder, error), now time.Time) int {
	created := 0
	for _, sub := range subs {
		sub := sub
		seenReminders, err := allForUser(ctx, sub.UserID)
		if err != nil {
			continue
		}
		for _, fixture := range fixtures {
			if fixture.KickoffAt.IsZero() || !fixture.KickoffAt.After(now) {
				continue
			}
			if !teamNameMatches(fixture.HomeTeam, sub.TeamName) && !teamNameMatches(fixture.AwayTeam, sub.TeamName) {
				continue
			}
			duplicate := false
			for _, pending := range seenReminders {
				if pending.SubscriptionID == sub.ID && pending.MatchID == fixtureKey(fixture) {
					duplicate = true
					break
				}
			}
			if duplicate {
				continue
			}
			if _, err := reminders.Append(ctx, Reminder{
				UserID:         sub.UserID,
				MatchID:        fixtureKey(fixture),
				HomeTeam:       fixture.HomeTeam,
				AwayTeam:       fixture.AwayTeam,
				KickoffAt:      fixture.KickoffAt,
				SubscriptionID: sub.ID,
				CreatedAt:      now,
			}); err == nil {
				created++
			}
		}
	}
	return created
}

func fixtureKey(fixture Fixture) string {
	if fixture.FixtureID != "" {
		return "fixture:" + fixture.FixtureID
	}
	return "fixture:" + fixture.HomeTeam + "-" + fixture.AwayTeam + "-" + fixture.KickoffAt.UTC().Format("200601021504")
}

// Active 返回全部用户的活跃订阅（每日展开器用）。
func (s *MemorySubscriptionStore) Active(_ context.Context) ([]Subscription, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Subscription, 0)
	for _, sub := range s.subs {
		if sub.Status == SubscriptionActive {
			out = append(out, sub)
		}
	}
	return out, nil
}

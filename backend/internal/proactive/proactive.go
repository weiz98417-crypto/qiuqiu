// Package proactive 是主动调度的时间维度（openspec/changes/proactive-
// scheduler）：与 WS 连接无关的用户级提醒簿 + 服务端节拍。此前「没人连接
// 就没有任何主动回合」——门控与调度是 watchConnection 的私有字段；本包把
// 「谁触发」从传输细节里解放出来：时钟/赛程产出回合候选，投递仍走同一个
// 规划 seam 与 C2 引用码门（第三把钥匙：reminder:<id>，用户显式请求，
// ADR-0015）。投递积压照抄 observation_resolution_outbox 模式（migration
// 020）：持久待递 → 连接/在线节拍补递 → 送达翻状态；错过（开球+30 分钟）
// 过期翻 suppressed 并转为记忆素材。
package proactive

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// 提醒时序：开球前 DefaultLeadMinutes 叫人；开球后 ExpireAfterKickoff 仍
// 未送达即静默过期（Q13：不迟到的球友），过期事实转为记忆素材。
const (
	DefaultLeadMinutes = 30
	ExpireAfterKickoff = 30 * time.Minute

	// CitationPrefix 是 C2 的第三把钥匙（ADR-0015）：用户显式请求的提醒，
	// 正当性强于画像口味——它是用户自己要的。定义在本包而非 conversation
	//（conversation ← companion ← proactive 会成环）；gate 只要求引用码
	// 非空，前缀语义归来源包所有。
	CitationPrefix = "reminder"
)

type ReminderStatus string

const (
	StatusPending    ReminderStatus = "pending"
	StatusDelivered  ReminderStatus = "delivered"
	StatusSuppressed ReminderStatus = "suppressed"
)

type Reminder struct {
	ID          string
	UserID      string
	MatchID     string
	HomeTeam    string
	AwayTeam    string
	KickoffAt   time.Time
	LeadMinutes int
	// Timezone 是请求方的 IANA 时区（user_speech 附带）；开球时间展示用，
	// 空则退服务器本地时区。
	Timezone string
	// SubscriptionID 非空 = 订阅展开产物（引用码走 subscription:<id>）。
	SubscriptionID string
	Status         ReminderStatus
	DeliverAt      time.Time
	ExpireAt       time.Time
	CreatedAt      time.Time
}

// CitationCode 是这次主动回合的 C2 引用码：单次提醒 = reminder:<id>，
// 订阅展开产物 = subscription:<subID>（Q8：用户显式订阅同属第三钥匙）。
func (r Reminder) CitationCode() string {
	if r.SubscriptionID != "" {
		return CitationSubscriptionPrefix + ":" + r.SubscriptionID
	}
	return CitationPrefix + ":" + r.ID
}

// NewReminder 校验并补全时序字段（DeliverAt/ExpireAt 由 kickoff 推导）。
func NewReminder(r Reminder) (Reminder, error) {
	if strings.TrimSpace(r.UserID) == "" {
		return Reminder{}, fmt.Errorf("reminder needs a user")
	}
	if r.KickoffAt.IsZero() {
		return Reminder{}, fmt.Errorf("reminder needs a kickoff time")
	}
	if r.LeadMinutes <= 0 {
		r.LeadMinutes = DefaultLeadMinutes
	}
	if r.Status == "" {
		r.Status = StatusPending
	}
	r.DeliverAt = r.KickoffAt.Add(-time.Duration(r.LeadMinutes) * time.Minute)
	r.ExpireAt = r.KickoffAt.Add(ExpireAfterKickoff)
	return r, nil
}

// Due reports whether the reminder should fire at now（已到点、未过期、未送出）。
func (r Reminder) Due(now time.Time) bool {
	return r.Status == StatusPending && !now.Before(r.DeliverAt) && now.Before(r.ExpireAt)
}

// PreMatchReminderReply 是提醒回合的确定性文本：事实（对阵/开球时间）由
// 提醒簿拥有，不LLM 化——这是「快、准、不打扰」的一类回合。开球时间按
// 请求方时区展示，空/非法时区退服务器本地。
func PreMatchReminderReply(r Reminder) string {
	location := time.Local
	if r.Timezone != "" {
		if loaded, err := time.LoadLocation(r.Timezone); err == nil {
			location = loaded
		}
	}
	when := r.KickoffAt.In(location)
	return fmt.Sprintf("快开球了，%s 对 %s，%s开球。我在这儿陪你。", r.HomeTeam, r.AwayTeam, when.Format("15:04"))
}

// Store 是提醒簿的接缝：MemoryStore 是测试/无库降级实现，PostgresStore 是
// 生产实现（migration 045）。
type Store interface {
	Append(ctx context.Context, reminder Reminder) (Reminder, error)
	PendingForUser(ctx context.Context, userID string) ([]Reminder, error)
	// AllForUser 返回该用户全部提醒（含已投递/已静默）——订阅展开去重
	// 必须看到历史键，否则早场已投递的提醒会被重复展开。
	AllForUser(ctx context.Context, userID string) ([]Reminder, error)
	DuePending(ctx context.Context, now time.Time) ([]Reminder, error)
	MarkDelivered(ctx context.Context, id string) error
	// SweepSuppressed 把过期待递翻 suppressed 并返回它们（调用方转为记忆
	// 素材，Q13）；未过期的 pending 原样留在簿里。
	SweepSuppressed(ctx context.Context, now time.Time) ([]Reminder, error)
	// SuppressPendingSubscription 取消订阅时把该订阅的未投递提醒翻
	// suppressed（不再打扰）。
	SuppressPendingSubscription(ctx context.Context, userID, subscriptionID string) error
}

// MemoryStore 是第二个 adapter（ADR-0006 同款双实现纪律）。
type MemoryStore struct {
	mu        sync.Mutex
	reminders []Reminder
	nextID    int
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{}
}

func (s *MemoryStore) Append(_ context.Context, reminder Reminder) (Reminder, error) {
	if s == nil {
		return Reminder{}, fmt.Errorf("reminder store unavailable")
	}
	reminder, err := NewReminder(reminder)
	if err != nil {
		return Reminder{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	reminder.ID = fmt.Sprintf("rem-%d", s.nextID)
	s.reminders = append(s.reminders, reminder)
	return reminder, nil
}

func (s *MemoryStore) PendingForUser(_ context.Context, userID string) ([]Reminder, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Reminder, 0)
	for _, reminder := range s.reminders {
		if reminder.UserID == userID && reminder.Status == StatusPending {
			out = append(out, reminder)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DeliverAt.Before(out[j].DeliverAt) })
	return out, nil
}

// AllForUser 返回该用户全部提醒（订阅展开去重用）。
func (s *MemoryStore) AllForUser(_ context.Context, userID string) ([]Reminder, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Reminder, 0)
	for _, reminder := range s.reminders {
		if reminder.UserID == userID {
			out = append(out, reminder)
		}
	}
	return out, nil
}

func (s *MemoryStore) DuePending(_ context.Context, now time.Time) ([]Reminder, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Reminder, 0)
	for _, reminder := range s.reminders {
		if reminder.Due(now) {
			out = append(out, reminder)
		}
	}
	return out, nil
}

func (s *MemoryStore) MarkDelivered(_ context.Context, id string) error {
	if s == nil {
		return fmt.Errorf("reminder store unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.reminders {
		// pending 守卫：已被清扫翻 suppressed 的提醒不得改回 delivered
		//（ADR-0015 错过即静默）。
		if s.reminders[i].ID == id && s.reminders[i].Status == StatusPending {
			s.reminders[i].Status = StatusDelivered
			return nil
		}
	}
	return fmt.Errorf("reminder %q not found", id)
}

func (s *MemoryStore) SweepSuppressed(_ context.Context, now time.Time) ([]Reminder, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	suppressed := make([]Reminder, 0)
	for i := range s.reminders {
		reminder := &s.reminders[i]
		if reminder.Status == StatusPending && !now.Before(reminder.ExpireAt) {
			reminder.Status = StatusSuppressed
			suppressed = append(suppressed, *reminder)
		}
	}
	return suppressed, nil
}

// SuppressPendingSubscription 取消订阅时静默该订阅的在途提醒。
func (s *MemoryStore) SuppressPendingSubscription(_ context.Context, userID, subscriptionID string) error {
	if s == nil {
		return fmt.Errorf("reminder store unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.reminders {
		r := &s.reminders[i]
		if r.UserID == userID && r.SubscriptionID == subscriptionID && r.Status == StatusPending {
			r.Status = StatusSuppressed
		}
	}
	return nil
}

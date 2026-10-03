package datasource

// auto-hosting 2.1:上游改判盲区——VAR 结果细分、事件消失/内容变化的 diff
// 检测、goal_cancelled 的 RevisionOf 引用与无引用降级链。红测试先行:
// 本文件先于实现存在,实现落地后全部转绿。

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"qiuqiu/internal/matchstate"
)

func TestVarDetailMappingProducesCorrectionTypes(t *testing.T) {
	cases := []struct {
		detail string
		want   string
	}{
		{"Goal cancelled", "goal_cancelled"},
		{"Goal confirmed", "var_result"},
		{"Penalty confirmed", "var_check"},
		{"Penalty cancelled", "var_check"},
		{"Card upgraded", "var_check"},
		{"Card downgraded", "var_check"},
	}
	for _, tc := range cases {
		source := Event{Time: EventTime{Elapsed: 20}, Type: "Var", Detail: tc.detail}
		got := source.ToStandardEvent(42, 1, 0)
		if got.Type != tc.want {
			t.Fatalf("Var detail %q → type %q, want %q", tc.detail, got.Type, tc.want)
		}
		if got.Detail != tc.detail {
			t.Fatalf("Var detail %q not carried onto StandardEvent (got %q)", tc.detail, got.Detail)
		}
	}
}

// sequenceEventsClient 按调用序返回脚本化快照,末尾驻留最后一份。
type sequenceEventsClient struct {
	mu        sync.Mutex
	responses [][]Event
	calls     int
}

func (c *sequenceEventsClient) GetEvents(int) ([]Event, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	index := c.calls
	if index >= len(c.responses) {
		index = len(c.responses) - 1
	}
	c.calls++
	return c.responses[index], nil
}

// collectDeliveries 抽取 n 条投递(及时 ack,超时失败)。
func collectDeliveries(t *testing.T, ch <-chan PollDelivery, n int, within time.Duration) []PollDelivery {
	t.Helper()
	var got []PollDelivery
	deadline := time.After(within)
	for len(got) < n {
		select {
		case delivery := <-ch:
			delivery.Acknowledge(true)
			got = append(got, delivery)
		case <-deadline:
			t.Fatalf("collected %d/%d deliveries within %v", len(got), n, within)
		}
	}
	return got
}

func goalEvent(minute int, player string) Event {
	return Event{
		Time:   EventTime{Elapsed: minute},
		Team:   TeamRef{ID: 1, Name: "Arsenal"},
		Player: PlayerRef{ID: 7, Name: player},
		Type:   "Goal",
		Detail: "Normal Goal",
	}
}

func TestPollerEmitsRetractedWhenEventDisappears(t *testing.T) {
	client := &sequenceEventsClient{responses: [][]Event{
		{goalEvent(12, "Saka")},
		{}, // 第二轮:上游事件消失(撤回)
	}}
	events := make(chan PollDelivery, 8)
	poller := NewPoller(client, 42, events).WithInterval(5 * time.Millisecond).WithInitialEvents(true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		poller.Run(ctx)
		close(done)
	}()

	deliveries := collectDeliveries(t, events, 2, 3*time.Second)
	cancel()
	<-done

	if deliveries[0].Kind != PollEventKind {
		t.Fatalf("first delivery kind = %q, want event", deliveries[0].Kind)
	}
	if deliveries[1].Kind != PollRetractedKind {
		t.Fatalf("second delivery kind = %q, want retracted", deliveries[1].Kind)
	}
	if deliveries[1].Event == nil || deliveries[1].Event.Type != "goal" {
		t.Fatalf("retracted payload = %+v, want last-known goal event", deliveries[1].Event)
	}
}

func TestPollerEmitsChangedWhenContentChanges(t *testing.T) {
	client := &sequenceEventsClient{responses: [][]Event{
		{goalEvent(12, "Saka")},
		{goalEvent(12, "Odegaard")}, // 同一事件内容变化(球员归属修正)
	}}
	events := make(chan PollDelivery, 8)
	poller := NewPoller(client, 42, events).WithInterval(5 * time.Millisecond).WithInitialEvents(true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		poller.Run(ctx)
		close(done)
	}()

	deliveries := collectDeliveries(t, events, 2, 3*time.Second)
	cancel()
	<-done

	if deliveries[0].Kind != PollEventKind {
		t.Fatalf("first delivery kind = %q, want event", deliveries[0].Kind)
	}
	if deliveries[1].Kind != PollChangedKind {
		t.Fatalf("second delivery kind = %q, want changed", deliveries[1].Kind)
	}
	if deliveries[1].Event == nil || deliveries[1].Event.Player.Name != "Odegaard" {
		t.Fatalf("changed payload = %+v, want corrected player", deliveries[1].Event)
	}
}

func TestPollerStableSnapshotEmitsNothing(t *testing.T) {
	client := &sequenceEventsClient{responses: [][]Event{
		{goalEvent(12, "Saka")},
		{goalEvent(12, "Saka")},
		{goalEvent(12, "Saka")},
	}}
	events := make(chan PollDelivery, 8)
	poller := NewPoller(client, 42, events).WithInterval(5 * time.Millisecond).WithInitialEvents(true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		poller.Run(ctx)
		close(done)
	}()

	collectDeliveries(t, events, 1, 3*time.Second) // 首轮事件
	// 再等三轮(15ms),稳态快照不应再有任何投递。
	time.Sleep(60 * time.Millisecond)
	select {
	case delivery := <-events:
		delivery.Acknowledge(true)
		t.Fatalf("stable snapshot emitted extra delivery kind=%q event=%+v", delivery.Kind, delivery.Event)
	case <-time.After(50 * time.Millisecond):
	}
	cancel()
	<-done
}

// gatedEventsClient 驻留当前相,由测试显式推进——控制「goal 先入账并被确认,
// VAR 结论后到」的真实时序。
type gatedEventsClient struct {
	mu     sync.Mutex
	phases [][]Event
	phase  int
}

func (c *gatedEventsClient) GetEvents(int) ([]Event, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	index := c.phase
	if index >= len(c.phases) {
		index = len(c.phases) - 1
	}
	return c.phases[index], nil
}

func (c *gatedEventsClient) advance() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.phase < len(c.phases)-1 {
		c.phase++
	}
}

func waitForFact(t *testing.T, store *matchstate.Store, matchID string, eventType string, within time.Duration) matchstate.MatchEvent {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		for _, candidate := range store.Events(matchID) {
			if candidate.EventType == eventType {
				return candidate
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("fact %s not ingested within %v", eventType, within)
	return matchstate.MatchEvent{}
}

func TestAPISportsVarCancelReferencesConfirmedGoal(t *testing.T) {
	store := matchstate.NewStore()
	if _, _, err := store.SetConfig("match-var", matchstate.MatchConfig{HomeTeam: "Arsenal", AwayTeam: "Liverpool"}); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	client := &gatedEventsClient{phases: [][]Event{
		{goalEvent(12, "Saka")},
		{goalEvent(12, "Saka"), {Time: EventTime{Elapsed: 16}, Team: TeamRef{ID: 1, Name: "Arsenal"}, Type: "Var", Detail: "Goal cancelled"}},
	}}
	manager := NewManager(context.Background(), store, client, ManagerConfig{PollInterval: 5 * time.Millisecond})
	t.Cleanup(manager.Close)
	if _, err := manager.Start("match-var", SourceConfig{Type: SourceAPISports, FixtureID: 42, ImportHistory: true}); err != nil {
		t.Fatalf("start: %v", err)
	}

	goal := waitForFact(t, store, "match-var", "goal", 3*time.Second)
	if _, _, err := store.ConfirmFact("match-var", goal.FactID, "operator-test"); err != nil {
		t.Fatalf("confirm goal: %v", err)
	}
	client.advance()

	cancelled := waitForFact(t, store, "match-var", "goal_cancelled", 3*time.Second)
	if cancelled.FactStatus != matchstate.FactStatusProvisional {
		t.Fatalf("goal_cancelled status = %q, want provisional", cancelled.FactStatus)
	}
	if cancelled.RevisionOf != goal.FactID && cancelled.RevisionOf != goal.ID {
		t.Fatalf("goal_cancelled revisionOf = %q, want goal fact %q/%q", cancelled.RevisionOf, goal.FactID, goal.ID)
	}
	if cancelled.TeamID != goal.TeamID {
		t.Fatalf("goal_cancelled team = %q, want referenced goal team %q", cancelled.TeamID, goal.TeamID)
	}
}

func TestAPISportsVarCancelDegradesToVarCheckWithoutConfirmedGoal(t *testing.T) {
	store := matchstate.NewStore()
	if _, _, err := store.SetConfig("match-var2", matchstate.MatchConfig{HomeTeam: "Arsenal", AwayTeam: "Liverpool"}); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	// 进球从未入账(或未确认):VAR 取消结论没有可引用的事实——降级 var_check 留运营。
	client := &gatedEventsClient{phases: [][]Event{
		{},
		{{Time: EventTime{Elapsed: 16}, Team: TeamRef{ID: 1, Name: "Arsenal"}, Type: "Var", Detail: "Goal cancelled"}},
	}}
	manager := NewManager(context.Background(), store, client, ManagerConfig{PollInterval: 5 * time.Millisecond})
	t.Cleanup(manager.Close)
	if _, err := manager.Start("match-var2", SourceConfig{Type: SourceAPISports, FixtureID: 43, ImportHistory: true}); err != nil {
		t.Fatalf("start: %v", err)
	}

	client.advance()
	degraded := waitForFact(t, store, "match-var2", "var_check", 3*time.Second)
	if !strings.Contains(strings.ToLower(degraded.Description), "取消") && !strings.Contains(strings.ToLower(degraded.Description), "cancel") {
		t.Fatalf("degraded var_check description = %q, want cancellation context preserved", degraded.Description)
	}
	for _, candidate := range store.Events("match-var2") {
		if candidate.EventType == "goal_cancelled" {
			t.Fatalf("unexpected goal_cancelled without confirmed reference: %+v", candidate)
		}
	}
}

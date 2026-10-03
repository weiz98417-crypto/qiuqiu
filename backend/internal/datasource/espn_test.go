package datasource

// auto-hosting 2.4/2.5:ESPN 快照 diff 源 + 双源仲裁行为。

import (
	"context"
	"sync"
	"testing"
	"time"

	"qiuqiu/internal/matchstate"
)

type scriptedEspnClient struct {
	mu        sync.Mutex
	responses []*EspnSummary
	calls     int
}

func (c *scriptedEspnClient) GetSummary(league, eventID string) (*EspnSummary, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	index := c.calls
	if index >= len(c.responses) {
		index = len(c.responses) - 1
	}
	c.calls++
	if c.responses[index] == nil {
		time.Sleep(50 * time.Millisecond)
		return c.responses[index], nil
	}
	return c.responses[index], nil
}

func espnSummary(state string, period int, completed bool, keyEvents []EspnKeyEvent) *EspnSummary {
	summary := &EspnSummary{KeyEvents: keyEvents}
	summary.Header.Competitions = []struct {
		Date        string           `json:"date"`
		Status      EspnStatus       `json:"status"`
		Competitors []EspnCompetitor `json:"competitors"`
	}{{
		Status: EspnStatus{Type: struct {
			State       string `json:"state"`
			Completed   bool   `json:"completed"`
			Description string `json:"description"`
		}{State: state, Completed: completed}, DisplayClock: "45:00", Period: period},
		Competitors: []EspnCompetitor{
			{HomeAway: "home", Team: struct {
				DisplayName string `json:"displayName"`
			}{DisplayName: "Arsenal"}, Score: "1"},
			{HomeAway: "away", Team: struct {
				DisplayName string `json:"displayName"`
			}{DisplayName: "Liverpool"}, Score: "0"},
		},
	}}
	summary.Rosters = []struct {
		HomeAway string `json:"homeAway"`
		Team     struct {
			ID string `json:"id"`
		} `json:"team"`
	}{
		{HomeAway: "home", Team: struct {
			ID string `json:"id"`
		}{ID: "1"}},
		{HomeAway: "away", Team: struct {
			ID string `json:"id"`
		}{ID: "2"}},
	}
	return summary
}

func espnGoal(id string, teamID, player string) EspnKeyEvent {
	return EspnKeyEvent{
		ID: id,
		Type: struct {
			ID   string `json:"id"`
			Text string `json:"text"`
		}{ID: "goal", Text: "Goal"},
		Clock: struct {
			DisplayValue string `json:"displayValue"`
		}{DisplayValue: "23'"},
		Team: struct {
			ID          string `json:"id"`
			DisplayName string `json:"displayName"`
		}{ID: teamID, DisplayName: "Arsenal"},
		ScoringPlay: true,
		Participants: []struct {
			Athlete struct {
				DisplayName string `json:"displayName"`
			} `json:"athlete"`
		}{{Athlete: struct {
			DisplayName string `json:"displayName"`
		}{DisplayName: player}}},
	}
}

func newEspnManager(t *testing.T, matchID string, responses []*EspnSummary) (*Manager, *matchstate.Store) {
	t.Helper()
	store := matchstate.NewStore()
	if _, _, err := store.SetConfig(matchID, matchstate.MatchConfig{HomeTeam: "Arsenal", AwayTeam: "Liverpool"}); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	manager := NewManager(context.Background(), store, nil, ManagerConfig{PollInterval: 5 * time.Millisecond, EspnPollInterval: 5 * time.Millisecond})
	manager.espnClient = &scriptedEspnClient{responses: responses}
	t.Cleanup(manager.Close)
	if _, err := manager.Start(matchID, SourceConfig{Type: SourceESPN, League: "eng.1", EspnEventID: "701234"}); err != nil {
		t.Fatalf("start espn: %v", err)
	}
	return manager, store
}

func TestEspnEventTypeMapping(t *testing.T) {
	cases := map[string]string{
		"goal":             "goal",
		"goal---own":       "goal",
		"penalty---scored": "goal",
		"yellow-card":      "yellow_card",
		"red-card":         "red_card",
		"second-yellow":    "red_card",
		"substitution":     "substitution",
		"penalty---missed": "",
		"var-review":       "",
	}
	for typeID, want := range cases {
		if got := espnEventType(typeID); got != want {
			t.Fatalf("espnEventType(%q) = %q, want %q", typeID, got, want)
		}
	}
}

func TestEspnSourceIngestsGoalAndLifecycle(t *testing.T) {
	_, store := newEspnManager(t, "match-espn", []*EspnSummary{
		espnSummary("pre", 1, false, nil),
		espnSummary("in", 1, false, []EspnKeyEvent{espnGoal("901", "1", "Saka")}),
		espnSummary("in", 2, false, []EspnKeyEvent{espnGoal("901", "1", "Saka")}),
		espnSummary("post", 2, true, []EspnKeyEvent{espnGoal("901", "1", "Saka")}),
	})

	goal := waitForFact(t, store, "match-espn", "goal", 3*time.Second)
	if goal.Source != string(SourceESPN) || goal.ProviderEventID != "espn:901" {
		t.Fatalf("goal = source=%q providerEventID=%q, want espn/espn:901", goal.Source, goal.ProviderEventID)
	}
	if goal.FactStatus != matchstate.FactStatusProvisional {
		t.Fatalf("goal status = %q, want provisional", goal.FactStatus)
	}
	if goal.TeamID != "home" || goal.PlayerName != "Saka" {
		t.Fatalf("goal = %+v, want home/Saka", goal)
	}
	kickoff := waitForFact(t, store, "match-espn", "kickoff", 3*time.Second)
	if kickoff.ProviderEventID != "espn:status:kickoff" {
		t.Fatalf("kickoff providerEventID = %q", kickoff.ProviderEventID)
	}
	fulltime := waitForFact(t, store, "match-espn", "fulltime", 3*time.Second)
	// 生命周期事件携带账本投影分(goal 仍 provisional 未进投影)=0-0,设计使然。
	if fulltime.Score.Home != 0 || fulltime.Score.Away != 0 {
		t.Fatalf("fulltime score = %+v, want 0-0 (provisional goal not in projection)", fulltime.Score)
	}
	if goal.Score.Home != 1 || goal.Score.Away != 0 {
		t.Fatalf("goal score = %+v, want 1-0 (ledger+1 derivation)", goal.Score)
	}

	// 快照稳态:同一 goal 重复出现不再入账。
	time.Sleep(60 * time.Millisecond)
	count := 0
	for _, candidate := range store.Events("match-espn") {
		if candidate.ProviderEventID == "espn:901" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("espn:901 landed %d times, want 1", count)
	}
}

func TestEspnRequiresConfiguredClient(t *testing.T) {
	manager := NewManager(context.Background(), matchstate.NewStore(), nil, ManagerConfig{})
	t.Cleanup(manager.Close)
	if _, err := manager.Start("match-x", SourceConfig{Type: SourceESPN, League: "eng.1", EspnEventID: "1"}); err == nil {
		t.Fatalf("espn source without client should fail")
	}
}

// 2.5 双源仲裁:ESPN 与 api-sports 的兼容同事实——先到者入账,后到者
// ErrDuplicate 静默去重(互为印证,无双计);归属矛盾(异队)→ conflict 挂起。
func TestDualSourceCompatibleDuplicateDedupedIncompatibleConflicts(t *testing.T) {
	store := matchstate.NewStore()
	if _, _, err := store.SetConfig("match-dual", matchstate.MatchConfig{HomeTeam: "Arsenal", AwayTeam: "Liverpool"}); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	first := matchstate.MatchEvent{
		Source: string(SourceESPN), ProviderName: "espn", ProviderEventID: "espn:901",
		EventType: "goal", Period: "first_half", Clock: "23:00", TeamID: "home",
		Score: matchstate.Score{Home: 1}, FactStatus: matchstate.FactStatusProvisional,
		Description: "进球:Saka。", Visibility: "public", Status: "active",
	}
	if _, _, err := store.Create("match-dual", first); err != nil {
		t.Fatalf("espn goal create: %v", err)
	}
	// api-sports 兼容同事实(同队同时钟):ErrDuplicate,无双计。
	second := first
	second.Source = string(SourceAPISports)
	second.ProviderEventID = "42:1204"
	if _, _, err := store.Create("match-dual", second); err == nil {
		t.Fatalf("compatible duplicate should be rejected")
	} else if err != matchstate.ErrDuplicate {
		t.Fatalf("compatible duplicate error = %v, want ErrDuplicate", err)
	}
	// api-sports 归属矛盾(异队):conflict 挂起。
	third := second
	third.TeamID = "away"
	if _, _, err := store.Create("match-dual", third); err != matchstate.ErrConflict {
		t.Fatalf("incompatible candidate error = %v, want ErrConflict", err)
	}
	conflicts := store.FactConflicts("match-dual")
	if len(conflicts) != 1 || conflicts[0].Status != matchstate.ConflictStatusOpen {
		t.Fatalf("conflicts = %+v, want one open", conflicts)
	}
	// 双计哨兵:public goal 只有 ESPN 一条。
	publicGoals := 0
	for _, candidate := range store.Events("match-dual") {
		if candidate.EventType == "goal" && candidate.FactStatus == matchstate.FactStatusConfirmed {
			publicGoals++
		}
	}
	if publicGoals > 1 {
		t.Fatalf("double-counted goals: %d", publicGoals)
	}
}

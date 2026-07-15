package datasource

import (
	"context"
	"sync"
	"testing"
	"time"

	"qiuqiu/internal/matchstate"
)

func TestManagerDefaultsToManualSource(t *testing.T) {
	manager := NewManager(context.Background(), matchstate.NewStore(), nil, ManagerConfig{})
	t.Cleanup(manager.Close)

	status := manager.Status("match-1")
	if status.ActiveSource != SourceManual {
		t.Fatalf("active source = %q, want manual", status.ActiveSource)
	}
	if status.Sources[SourceManual].State != StateReady {
		t.Fatalf("manual state = %q, want ready", status.Sources[SourceManual].State)
	}
	if status.Sources[SourceAPISports].State != StateUnconfigured {
		t.Fatalf("api-sports state = %q, want unconfigured", status.Sources[SourceAPISports].State)
	}
}

func TestAPISportsSourceIngestsRepeatedProviderEventOnce(t *testing.T) {
	store := matchstate.NewStore()
	if _, _, err := store.SetConfig("match-1", matchstate.MatchConfig{HomeTeam: "Arsenal", AwayTeam: "Liverpool"}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}
	client := &repeatingEventsClient{events: []Event{{
		Time:   EventTime{Elapsed: 12},
		Team:   TeamRef{ID: 1, Name: "Arsenal"},
		Player: PlayerRef{ID: 7, Name: "Saka"},
		Type:   "Goal",
		Detail: "Normal Goal",
	}}}
	manager := NewManager(context.Background(), store, client, ManagerConfig{PollInterval: 5 * time.Millisecond})
	t.Cleanup(manager.Close)

	if _, err := manager.Start("match-1", SourceConfig{Type: SourceAPISports, FixtureID: 42, ImportHistory: true}); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	waitFor(t, time.Second, func() bool { return len(store.Events("match-1")) == 1 })
	waitFor(t, time.Second, func() bool { return client.Calls() >= 3 })

	events := store.Events("match-1")
	if len(events) != 1 {
		t.Fatalf("stored events = %d, want 1", len(events))
	}
	if events[0].ProviderEventID == "" || events[0].Source != string(SourceAPISports) {
		t.Fatalf("provider metadata missing: %+v", events[0])
	}
	if events[0].Score != (matchstate.Score{Home: 1, Away: 0}) {
		t.Fatalf("score = %+v, want 1-0", events[0].Score)
	}
	if events[0].Confirmed {
		t.Fatalf("provider goal = %+v, want pending until a terminal confirmation", events[0])
	}
}

func TestAPISportsSourceSkipsHistoryByDefault(t *testing.T) {
	store := matchstate.NewStore()
	if _, _, err := store.SetConfig("match-1", matchstate.MatchConfig{HomeTeam: "Arsenal", AwayTeam: "Liverpool"}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}
	historical := Event{Time: EventTime{Elapsed: 12}, Team: TeamRef{ID: 1, Name: "Arsenal"}, Player: PlayerRef{ID: 7, Name: "Saka"}, Type: "Goal", Detail: "Normal Goal"}
	newEvent := Event{Time: EventTime{Elapsed: 30}, Team: TeamRef{ID: 2, Name: "Liverpool"}, Player: PlayerRef{ID: 9, Name: "Nunez"}, Type: "Goal", Detail: "Normal Goal"}
	client := &scriptedEventsClient{responses: [][]Event{{historical}, {historical, newEvent}}}
	manager := NewManager(context.Background(), store, client, ManagerConfig{PollInterval: 5 * time.Millisecond})
	t.Cleanup(manager.Close)

	if _, err := manager.Start("match-1", SourceConfig{Type: SourceAPISports, FixtureID: 42}); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	waitFor(t, time.Second, func() bool { return client.Calls() >= 2 && len(store.Events("match-1")) == 1 })

	events := store.Events("match-1")
	if events[0].PlayerName != "Nunez" {
		t.Fatalf("stored player = %q, want only new event", events[0].PlayerName)
	}
}

func TestNormalizeAPISportsEventTypes(t *testing.T) {
	cases := []struct {
		typeName string
		detail   string
		want     string
	}{
		{typeName: "Goal", detail: "Normal Goal", want: "goal"},
		{typeName: "Card", detail: "Second Yellow card", want: "red_card"},
		{typeName: "Card", detail: "Yellow Card", want: "yellow_card"},
		{typeName: "subst", want: "substitution"},
		{typeName: "Var", detail: "Goal cancelled", want: "var_check"},
	}
	for _, testCase := range cases {
		if got := normalizeType(testCase.typeName, testCase.detail); got != testCase.want {
			t.Fatalf("normalizeType(%q, %q) = %q, want %q", testCase.typeName, testCase.detail, got, testCase.want)
		}
	}
}

func TestStopWaitsForExternalSourceWorkersToExit(t *testing.T) {
	client := &blockingEventsClient{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	manager := NewManager(context.Background(), matchstate.NewStore(), client, ManagerConfig{PollInterval: time.Millisecond})
	t.Cleanup(manager.Close)

	if _, err := manager.Start("match-stop", SourceConfig{Type: SourceAPISports, FixtureID: 42, ImportHistory: true}); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	waitForSignal(t, client.started, "source poll to start")

	stopped := make(chan struct{})
	go func() {
		manager.Stop("match-stop")
		close(stopped)
	}()
	select {
	case <-stopped:
		t.Fatal("Stop returned before the active poll exited")
	case <-time.After(20 * time.Millisecond):
	}

	close(client.release)
	waitForSignal(t, stopped, "source stop to finish")
	if got := manager.Status("match-stop").ActiveSource; got != SourceManual {
		t.Fatalf("active source = %q, want manual", got)
	}
}

type repeatingEventsClient struct {
	mu     sync.Mutex
	calls  int
	events []Event
}

type scriptedEventsClient struct {
	mu        sync.Mutex
	calls     int
	responses [][]Event
}

type blockingEventsClient struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (c *blockingEventsClient) GetEvents(fixtureID int) ([]Event, error) {
	c.once.Do(func() { close(c.started) })
	<-c.release
	return nil, nil
}

func (c *scriptedEventsClient) GetEvents(fixtureID int) ([]Event, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	index := c.calls
	if index >= len(c.responses) {
		index = len(c.responses) - 1
	}
	c.calls++
	return append([]Event(nil), c.responses[index]...), nil
}

func (c *scriptedEventsClient) Calls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func (c *repeatingEventsClient) GetEvents(fixtureID int) ([]Event, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	return append([]Event(nil), c.events...), nil
}

func (c *repeatingEventsClient) Calls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for condition")
}

func waitForSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

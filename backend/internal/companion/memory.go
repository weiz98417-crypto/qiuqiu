package companion

import (
	"context"
	"strings"
	"sync"

	"qiuqiu/internal/matchstate"
)

type StoreMemoryTools struct {
	store  matchstate.Repository
	mu     sync.Mutex
	traces []Trace
}

func NewStoreMemoryTools(store *matchstate.Store) *StoreMemoryTools {
	return &StoreMemoryTools{store: store}
}

func NewRepositoryMemoryTools(store matchstate.Repository) *StoreMemoryTools {
	return &StoreMemoryTools{store: store}
}

func (m *StoreMemoryTools) Snapshot(ctx context.Context, matchID string) (matchstate.Snapshot, error) {
	_ = ctx
	return m.store.Snapshot(matchID), nil
}

func (m *StoreMemoryTools) RecentEvents(ctx context.Context, matchID string, limit int) ([]matchstate.MatchEvent, error) {
	_ = ctx
	events := activeOnly(m.store.Events(matchID))
	if limit > 0 && len(events) > limit {
		events = events[:limit]
	}
	return events, nil
}

func (m *StoreMemoryTools) EventsByPlayer(ctx context.Context, matchID, playerName string, limit int) ([]matchstate.MatchEvent, error) {
	_ = ctx
	playerName = strings.TrimSpace(playerName)
	var out []matchstate.MatchEvent
	for _, ev := range activeOnly(m.store.Events(matchID)) {
		if eventHasPlayer(ev, playerName) {
			out = append(out, ev)
		}
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (m *StoreMemoryTools) WriteTrace(ctx context.Context, trace Trace) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	m.traces = append(m.traces, trace)
	return nil
}

func (m *StoreMemoryTools) Traces() []Trace {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Trace(nil), m.traces...)
}

func activeOnly(events []matchstate.MatchEvent) []matchstate.MatchEvent {
	out := make([]matchstate.MatchEvent, 0, len(events))
	for _, ev := range events {
		if ev.Status == "active" {
			out = append(out, ev)
		}
	}
	return out
}

func eventHasPlayer(ev matchstate.MatchEvent, playerName string) bool {
	if playerName == "" {
		return false
	}
	if strings.EqualFold(ev.PlayerName, playerName) {
		return true
	}
	for _, participant := range ev.Participants {
		if strings.EqualFold(participant.Name, playerName) {
			return true
		}
	}
	return false
}

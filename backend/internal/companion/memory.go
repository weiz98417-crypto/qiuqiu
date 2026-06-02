package companion

import (
	"context"
	"sort"
	"strings"
	"sync"

	"qiuqiu/internal/matchstate"
)

type StoreMemoryTools struct {
	store       matchstate.Repository
	traceWriter TraceWriter
	mu          sync.Mutex
	traces      []Trace
}

func NewStoreMemoryTools(store *matchstate.Store) *StoreMemoryTools {
	return &StoreMemoryTools{store: store}
}

func NewRepositoryMemoryTools(store matchstate.Repository) *StoreMemoryTools {
	return &StoreMemoryTools{store: store}
}

func (m *StoreMemoryTools) WithTraceWriter(writer TraceWriter) *StoreMemoryTools {
	m.traceWriter = writer
	return m
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
	m.mu.Lock()
	m.traces = append(m.traces, trace)
	m.mu.Unlock()
	if m.traceWriter != nil {
		return m.traceWriter.WriteTrace(ctx, trace)
	}
	return nil
}

func (m *StoreMemoryTools) Traces() []Trace {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Trace(nil), m.traces...)
}

func (m *StoreMemoryTools) ListTraces(ctx context.Context, matchID string, limit int) ([]Trace, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	var traces []Trace
	for _, trace := range m.traces {
		if trace.MatchID == matchID {
			traces = append(traces, trace)
		}
	}
	sort.SliceStable(traces, func(i, j int) bool {
		return traces[i].CreatedAt.After(traces[j].CreatedAt)
	})
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if len(traces) > limit {
		traces = traces[:limit]
	}
	return append([]Trace(nil), traces...), nil
}

func (m *StoreMemoryTools) GetTrace(ctx context.Context, matchID, traceID string) (Trace, error) {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, trace := range m.traces {
		if trace.MatchID == matchID && trace.ID == traceID {
			return trace, nil
		}
	}
	return Trace{}, ErrTraceNotFound
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

package companion

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"qiuqiu/internal/matchstate"
)

type StoreMemoryTools struct {
	store       matchstate.Repository
	traceWriter TraceWriter
	turnReader  ConversationTurnReader
	mu          sync.Mutex
	traces      []Trace
	turns       []ConversationTurn
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

func (m *StoreMemoryTools) WithTurnReader(reader ConversationTurnReader) *StoreMemoryTools {
	m.turnReader = reader
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
	m.appendTraceTurnsLocked(trace)
	m.mu.Unlock()
	if m.traceWriter != nil {
		return m.traceWriter.WriteTrace(ctx, trace)
	}
	return nil
}

func (m *StoreMemoryTools) UpdateTrace(ctx context.Context, trace Trace) error {
	m.mu.Lock()
	for i := range m.traces {
		if m.traces[i].MatchID == trace.MatchID && m.traces[i].ID == trace.ID {
			m.traces[i] = trace
			break
		}
	}
	m.mu.Unlock()
	if updater, ok := m.traceWriter.(TraceUpdater); ok {
		return updater.UpdateTrace(ctx, trace)
	}
	return nil
}

func (m *StoreMemoryTools) Reset(matchID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.trimTracesAndTurnsLocked(matchID)
	return nil
}

func (m *StoreMemoryTools) RecentTurns(ctx context.Context, matchID, userID string, limit int) ([]ConversationTurn, error) {
	m.mu.Lock()
	local := m.recentTurnsLocked(matchID, userID, limit)
	m.mu.Unlock()
	if m.turnReader == nil {
		return local, nil
	}
	remote, err := m.turnReader.RecentTurns(ctx, matchID, userID, limit)
	if err != nil {
		if len(local) > 0 {
			return local, nil
		}
		return nil, err
	}
	return mergeTurns(local, remote, limit), nil
}

func (m *StoreMemoryTools) recentTurnsLocked(matchID, userID string, limit int) []ConversationTurn {
	var turns []ConversationTurn
	for i := len(m.turns) - 1; i >= 0; i-- {
		turn := m.turns[i]
		if turn.MatchID != matchID || turn.UserID != userID {
			continue
		}
		turns = append(turns, turn)
		if limit > 0 && len(turns) >= limit {
			break
		}
	}
	for i, j := 0, len(turns)-1; i < j; i, j = i+1, j-1 {
		turns[i], turns[j] = turns[j], turns[i]
	}
	return append([]ConversationTurn(nil), turns...)
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

func (m *StoreMemoryTools) appendTraceTurnsLocked(trace Trace) {
	eventID := ""
	if len(trace.RetrievedEvent) > 0 {
		eventID = trace.RetrievedEvent[0]
	}
	if strings.TrimSpace(trace.Input) != "" {
		m.turns = append(m.turns, ConversationTurn{
			MatchID:   trace.MatchID,
			UserID:    trace.UserID,
			Role:      "user",
			Text:      trace.Input,
			CreatedAt: trace.CreatedAt,
		})
	}
	if strings.TrimSpace(trace.Output) != "" {
		m.turns = append(m.turns, ConversationTurn{
			MatchID:   trace.MatchID,
			UserID:    trace.UserID,
			Role:      "qiuqiu",
			Text:      trace.Output,
			EventID:   eventID,
			CreatedAt: trace.CreatedAt,
		})
	}
	const maxTurns = 400
	if len(m.turns) > maxTurns {
		m.turns = m.turns[len(m.turns)-maxTurns:]
	}
}

func (m *StoreMemoryTools) trimTracesAndTurnsLocked(matchID string) {
	if matchID == "" {
		m.traces = nil
		m.turns = nil
		return
	}
	traces := m.traces[:0]
	for _, trace := range m.traces {
		if trace.MatchID != matchID {
			traces = append(traces, trace)
		}
	}
	m.traces = traces
	turns := m.turns[:0]
	for _, turn := range m.turns {
		if turn.MatchID != matchID {
			turns = append(turns, turn)
		}
	}
	m.turns = turns
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

func mergeTurns(a, b []ConversationTurn, limit int) []ConversationTurn {
	if limit <= 0 {
		limit = 10
	}
	seen := make(map[string]bool, len(a)+len(b))
	merged := make([]ConversationTurn, 0, len(a)+len(b))
	for _, turns := range [][]ConversationTurn{a, b} {
		for _, turn := range turns {
			key := turn.MatchID + "\x00" + turn.UserID + "\x00" + turn.Role + "\x00" + turn.Text + "\x00" + turn.EventID + "\x00" + turn.CreatedAt.Format(time.RFC3339Nano)
			if seen[key] {
				continue
			}
			seen[key] = true
			merged = append(merged, turn)
		}
	}
	sort.SliceStable(merged, func(i, j int) bool {
		return merged[i].CreatedAt.Before(merged[j].CreatedAt)
	})
	if len(merged) > limit {
		merged = merged[len(merged)-limit:]
	}
	return merged
}

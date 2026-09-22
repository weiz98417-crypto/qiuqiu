package relationship

import (
	"strings"
	"testing"
	"time"
)

// 决策层记忆信号锁（openspec/changes/memory-in-policy，ADR-0019）：画像
// 口味命中本场进球时放大 affect 并留 reason code；无信号时与现状一致。

func TestMemoryBiasAmplifiesFavoriteTeamGoal(t *testing.T) {
	state := &StateBundle{}
	normalizeStateBundle(state, "user-1", "match-1")
	signal := Signal{
		Kind: SignalMatchEvent,
		ID:   "sig-mem-1",
		Match: &MatchSignal{
			EventType: "goal",
			OutputAllowed: true,
			TeamName:  "皇家马德里",
			Memory:    MemorySignals{FavoriteTeam: "皇马"},
		},
	}
	acts, codes := applyPolicy(state, signal, time.Now())
	joined := strings.Join(codes, ",")
	if !strings.Contains(joined, "policy_memory:favorite_team_goal") {
		t.Fatalf("codes = %q, want the memory reason code", joined)
	}
	if state.Match.Affect.Valence <= 0 {
		t.Fatalf("valence = %f, want positive amplification for favorite team goal", state.Match.Affect.Valence)
	}
	if len(acts) == 0 || acts[0] != ActReact {
		t.Fatalf("acts = %+v, want ActReact", acts)
	}
}

func TestNoMemorySignalKeepsBaseline(t *testing.T) {
	state := &StateBundle{}
	normalizeStateBundle(state, "user-1", "match-1")
	signal := Signal{
		Kind: SignalMatchEvent,
		ID:   "sig-mem-2",
		Match: &MatchSignal{
			EventType: "goal",
			TeamName:  "皇家马德里",
		},
	}
	_, codes := applyPolicy(state, signal, time.Now())
	if strings.Contains(strings.Join(codes, ","), "policy_memory") {
		t.Fatalf("codes = %q, want no memory codes without memory signals", codes)
	}
}

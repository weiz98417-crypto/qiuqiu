package pipeline

import (
	"qiuqiu/internal/event"
	"sync"
)

// ContextEnricher adds match context to events: significance, game state, etc.
type ContextEnricher struct {
	mu     sync.Mutex
	states map[int64]*MatchState // match_id -> state
}

// MatchState tracks per-match state for context enrichment.
type MatchState struct {
	MatchID      string
	HomeTeam     string
	AwayTeam     string
	HomeScore    int
	AwayScore    int
	PreviousHome int
	PreviousAway int
	Minute       int
	Half         int
	Status       string // 'live'|'ht'|'ft'
	PlayerGoals  map[string]int
}

// EnrichedContext is the enhanced context for LLM generation.
type EnrichedContext struct {
	ScoreBefore  string
	ScoreAfter   string
	IsEqualizer  bool
	IsWinner     bool
	IsHatTrick   bool
	TimeContext  string
	HalfContext  string
	Significance string
}

func NewContextEnricher() *ContextEnricher {
	return &ContextEnricher{
		states: make(map[int64]*MatchState),
	}
}

// UpdateState updates match state from an event.
func (e *ContextEnricher) UpdateState(ev *event.StandardEvent) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Use team names as composite key since we don't have match_id in StandardEvent yet
	key := ev.MatchID
	state, ok := e.states[key]
	if !ok {
		state = &MatchState{
			PlayerGoals: make(map[string]int),
		}
		e.states[key] = state
	}

	// Track score changes
	state.PreviousHome = state.HomeScore
	state.PreviousAway = state.AwayScore
	if ev.Score.Home > 0 || ev.Score.Away > 0 {
		state.HomeScore = ev.Score.Home
		state.AwayScore = ev.Score.Away
	}
	state.Minute = ev.Minute
	state.Half = (ev.Minute / 45) + 1

	// Track player goals
	if ev.Type == "goal" && ev.Player.Name != "" {
		state.PlayerGoals[ev.Player.Name]++
	}
}

// Enrich computes enriched context for an event.
func (e *ContextEnricher) Enrich(ev *event.StandardEvent) *EnrichedContext {
	e.mu.Lock()
	state, ok := e.states[ev.MatchID]
	if !ok {
		state = &MatchState{PlayerGoals: make(map[string]int)}
		e.states[ev.MatchID] = state
	}
	e.mu.Unlock()

	ctx := &EnrichedContext{
		ScoreBefore:  formatScore(state.PreviousHome, state.PreviousAway),
		ScoreAfter:   formatScore(state.HomeScore, state.AwayScore),
		IsEqualizer:  ev.Type == "goal" && state.HomeScore == state.AwayScore && state.HomeScore > 0,
		IsWinner:     ev.Type == "goal" && ev.Minute > 85,
		IsHatTrick:   ev.Type == "goal" && state.PlayerGoals[ev.Player.Name] >= 3,
		TimeContext:  getTimeContext(ev.Minute),
		HalfContext:  getHalfContext(state.Half),
		Significance: calcSignificance(ev, state),
	}

	return ctx
}

func formatScore(home, away int) string {
	if home == 0 && away == 0 {
		return "0-0"
	}
	return itoa(home) + "-" + itoa(away)
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}

func getTimeContext(minute int) string {
	switch {
	case minute < 10:
		return "开局"
	case minute < 30:
		return "上半段"
	case minute < 45:
		return "上半场尾声"
	case minute < 55:
		return "下半场开局"
	case minute < 75:
		return "中段"
	case minute < 85:
		return "冲刺阶段"
	default:
		return "尾声"
	}
}

func getHalfContext(half int) string {
	if half <= 1 {
		return "上半场"
	}
	return "下半场"
}

func calcSignificance(ev *event.StandardEvent, state *MatchState) string {
	if ev.Type != "goal" {
		return ""
	}
	if ev.Minute > 85 && state.PreviousHome == state.PreviousAway && state.HomeScore != state.AwayScore {
		return "绝杀"
	}
	if state.HomeScore == state.AwayScore {
		return "扳平球"
	}
	if state.PreviousHome == 0 && state.PreviousAway == 0 && (state.HomeScore+state.AwayScore) == 1 {
		return "首开纪录"
	}
	if state.HomeScore-state.PreviousHome > 1 || state.AwayScore-state.PreviousAway > 1 {
		return "反超球"
	}
	return ""
}

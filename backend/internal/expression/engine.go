package expression

import (
	"sync"
	"time"
)

// State is a Live2D expression state.
type State int

const (
	Idle State = iota
	Listening
	Speaking
	Excited
	Nervous
	Regret
	Tease
	Surprised
	Happy
	Confused
)

func (s State) String() string {
	return [...]string{"idle", "listening", "speaking", "excited", "nervous", "regret", "tease", "surprised", "happy", "confused"}[s]
}

func (s State) Priority() int {
	return [...]int{0, 6, 10, 8, 7, 6, 5, 9, 4, 3}[s]
}

// Config maps event types to expressions.
type Config struct {
	State     State
	Intensity float64
	Duration  time.Duration
}

// EventMap returns the expression config for a given event type.
func EventMap(eventType string, minute int) Config {
	switch eventType {
	case "goal":
		if minute > 85 {
			return Config{Excited, 1.0, 8 * time.Second}
		}
		return Config{Excited, 1.0, 5 * time.Second}
	case "own_goal":
		return Config{Surprised, 0.9, 5 * time.Second}
	case "red_card":
		return Config{Surprised, 0.9, 5 * time.Second}
	case "yellow_card":
		return Config{Tease, 0.4, 3 * time.Second}
	case "penalty":
		return Config{Nervous, 0.8, 5 * time.Second}
	case "shot":
		return Config{Nervous, 0.6, 3 * time.Second}
	case "match_start":
		return Config{Happy, 0.6, 3 * time.Second}
	case "match_end":
		return Config{Idle, 0.5, 5 * time.Second}
	case "var_check":
		return Config{Confused, 0.5, 3 * time.Second}
	case "offside":
		return Config{Tease, 0.3, 2 * time.Second}
	default:
		return Config{Idle, 0.2, 2 * time.Second}
	}
}

// Engine manages expression state with anti-flapping.
type Engine struct {
	mu                sync.Mutex
	current           State
	lastChange        time.Time
	changesThisMinute int
	minuteStart       time.Time
	decayTimer        *time.Timer
}

func New() *Engine {
	return &Engine{current: Idle}
}

// Set requests a new expression. Anti-flap rules apply.
func (e *Engine) Set(cfg Config) State {
	e.mu.Lock()
	defer e.mu.Unlock()

	now := time.Now()

	// Reset minute counter
	if now.Sub(e.minuteStart) > time.Minute {
		e.changesThisMinute = 0
		e.minuteStart = now
	}

	// Anti-flap: 1s minimum between changes
	if now.Sub(e.lastChange) < time.Second {
		if cfg.State.Priority() <= e.current.Priority() {
			return e.current
		}
	}

	// Max 3 changes per minute
	if e.changesThisMinute >= 3 {
		return e.current
	}

	e.current = cfg.State
	e.lastChange = now
	e.changesThisMinute++

	// Auto-decay after duration
	if e.decayTimer != nil {
		e.decayTimer.Stop()
	}
	e.decayTimer = time.AfterFunc(cfg.Duration, func() {
		e.mu.Lock()
		if e.current == cfg.State {
			e.current = Idle
		}
		e.mu.Unlock()
	})

	return e.current
}

// Current returns the active expression.
func (e *Engine) Current() State {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.current
}

// TransitionDuration returns the blend time between two expressions (ms).
func TransitionDuration(from, to State) int {
	// Simplified matrix: most transitions 200-300ms
	if from == to {
		return 0
	}
	if to == Excited || to == Surprised {
		return 200
	}
	return 300
}

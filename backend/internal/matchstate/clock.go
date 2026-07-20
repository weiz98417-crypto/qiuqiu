package matchstate

import (
	"fmt"
	"strings"
	"time"
)

const (
	ClockActionStart  = "start"
	ClockActionPause  = "pause"
	ClockActionSet    = "set"
	ClockActionAdjust = "adjust"
)

type MatchClock struct {
	MatchID        string     `json:"matchId"`
	Period         string     `json:"period"`
	ElapsedSeconds int        `json:"elapsedSeconds"`
	Running        bool       `json:"running"`
	AnchorAt       *time.Time `json:"anchorAt,omitempty"`
	Source         string     `json:"source"`
	Version        int64      `json:"version"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

type ClockCommand struct {
	Action          string `json:"action"`
	Period          string `json:"period,omitempty"`
	ElapsedSeconds  *int   `json:"elapsedSeconds,omitempty"`
	DeltaSeconds    *int   `json:"deltaSeconds,omitempty"`
	ExpectedVersion int64  `json:"expectedVersion"`
	Source          string `json:"source,omitempty"`
}

type ClockRepository interface {
	Clock(matchID string) MatchClock
	SetClock(matchID string, command ClockCommand) (MatchClock, error)
	SubscribeClock(matchID string) (<-chan MatchClock, func())
}

func defaultMatchClock(matchID string) MatchClock {
	return MatchClock{
		MatchID: strings.TrimSpace(matchID),
		Period:  "pre_match",
		Source:  "system",
	}
}

func normalizeMatchClock(matchID string, clock MatchClock) MatchClock {
	if strings.TrimSpace(clock.MatchID) == "" {
		clock.MatchID = strings.TrimSpace(matchID)
	}
	if strings.TrimSpace(clock.Period) == "" {
		clock.Period = "pre_match"
	}
	clock.Period = normalizePeriod(clock.Period)
	if strings.TrimSpace(clock.Source) == "" {
		clock.Source = "system"
	}
	if clock.ElapsedSeconds < 0 {
		clock.ElapsedSeconds = 0
	}
	return clock
}

func applyClockCommand(current MatchClock, command ClockCommand, now time.Time) (MatchClock, error) {
	current = normalizeMatchClock(current.MatchID, current)
	if command.ExpectedVersion != current.Version {
		return MatchClock{}, fmt.Errorf("%w: expected version %d, current version %d", ErrClockVersionConflict, command.ExpectedVersion, current.Version)
	}
	now = now.UTC()
	next := current
	next.ElapsedSeconds = current.elapsedAt(now)
	if command.Period != "" {
		next.Period = normalizePeriod(command.Period)
		if !validClockPeriod(next.Period) {
			return MatchClock{}, fmt.Errorf("%w: unsupported match period %q", ErrInvalid, command.Period)
		}
	}
	if command.ElapsedSeconds != nil {
		if *command.ElapsedSeconds < 0 || *command.ElapsedSeconds > 6*60*60 {
			return MatchClock{}, fmt.Errorf("%w: elapsedSeconds must be between 0 and 21600", ErrInvalid)
		}
		next.ElapsedSeconds = *command.ElapsedSeconds
	}

	switch strings.ToLower(strings.TrimSpace(command.Action)) {
	case ClockActionStart:
		if current.Running && command.Period == "" && command.ElapsedSeconds == nil {
			return current, nil
		}
		next.Running = true
		next.AnchorAt = timePointer(now)
	case ClockActionPause:
		if !current.Running && command.Period == "" && command.ElapsedSeconds == nil {
			return current, nil
		}
		next.Running = false
		next.AnchorAt = nil
	case ClockActionSet:
		if command.Period == "" && command.ElapsedSeconds == nil {
			return MatchClock{}, fmt.Errorf("%w: set clock requires period or elapsedSeconds", ErrInvalid)
		}
		if next.Running {
			next.AnchorAt = timePointer(now)
		} else {
			next.AnchorAt = nil
		}
	case ClockActionAdjust:
		if command.DeltaSeconds == nil {
			return MatchClock{}, fmt.Errorf("%w: adjust clock requires deltaSeconds", ErrInvalid)
		}
		next.ElapsedSeconds += *command.DeltaSeconds
		if next.ElapsedSeconds < 0 || next.ElapsedSeconds > 6*60*60 {
			return MatchClock{}, fmt.Errorf("%w: adjusted clock must be between 0 and 21600", ErrInvalid)
		}
		if next.Running {
			next.AnchorAt = timePointer(now)
		} else {
			next.AnchorAt = nil
		}
	default:
		return MatchClock{}, fmt.Errorf("%w: unsupported clock action %q", ErrInvalid, command.Action)
	}

	next.Source = defaultString(strings.TrimSpace(command.Source), "operator")
	next.Version = current.Version + 1
	next.UpdatedAt = now
	return next, nil
}

func (clock MatchClock) elapsedAt(now time.Time) int {
	elapsed := clock.ElapsedSeconds
	if clock.Running && clock.AnchorAt != nil {
		delta := int(now.UTC().Sub(clock.AnchorAt.UTC()).Seconds())
		if delta > 0 {
			elapsed += delta
		}
	}
	if elapsed < 0 {
		return 0
	}
	return elapsed
}

func (clock MatchClock) CurrentElapsed(now time.Time) int {
	return clock.elapsedAt(now)
}

func (clock MatchClock) displayAt(now time.Time) string {
	elapsed := clock.elapsedAt(now)
	return fmt.Sprintf("%02d:%02d", elapsed/60, elapsed%60)
}

func validClockPeriod(period string) bool {
	switch period {
	case "pre_match", "first_half", "halftime", "second_half", "extra_time", "fulltime":
		return true
	default:
		return false
	}
}

func timePointer(value time.Time) *time.Time {
	value = value.UTC()
	return &value
}

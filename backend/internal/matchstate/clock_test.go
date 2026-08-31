package matchstate

import (
	"errors"
	"testing"
	"time"
)

func TestSnapshotKeepsMatchClockIndependentFromEventTime(t *testing.T) {
	store := NewStore()
	matchID := "independent-clock"

	clock, err := store.SetClock(matchID, ClockCommand{
		Action:          ClockActionSet,
		Period:          "first_half",
		ElapsedSeconds:  intPointer(10 * 60),
		ExpectedVersion: 0,
		Source:          "operator",
	})
	if err != nil {
		t.Fatalf("SetClock: %v", err)
	}
	if clock.Version != 1 {
		t.Fatalf("clock version = %d, want 1", clock.Version)
	}

	if _, _, err := store.Create(matchID, MatchEvent{
		EventType:   "shot",
		Period:      "first_half",
		Clock:       "09:30",
		Description: "a delayed shot report",
	}); err != nil {
		t.Fatalf("Create event: %v", err)
	}

	snapshot := store.PublicSnapshot(matchID)
	if snapshot.Clock != "10:00" || snapshot.Period != "first_half" {
		t.Fatalf("snapshot clock = %s %s, want first_half 10:00", snapshot.Period, snapshot.Clock)
	}
	if snapshot.MatchClock.Version != 1 || snapshot.MatchClock.ElapsedSeconds != 10*60 {
		t.Fatalf("snapshot match clock = %+v", snapshot.MatchClock)
	}
	if len(snapshot.RecentEvents) != 1 || snapshot.RecentEvents[0].Clock != "09:30" {
		t.Fatalf("event occurrence time changed: %+v", snapshot.RecentEvents)
	}
}

func TestRunningClockAdvancesAndPauseMaterializesElapsedTime(t *testing.T) {
	store := NewStore()
	matchID := "running-clock"
	now := time.Date(2026, 7, 17, 20, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }

	clock, err := store.SetClock(matchID, ClockCommand{
		Action: ClockActionSet, Period: "first_half", ElapsedSeconds: intPointer(10 * 60), ExpectedVersion: 0,
	})
	if err != nil {
		t.Fatalf("set clock: %v", err)
	}
	clock, err = store.SetClock(matchID, ClockCommand{Action: ClockActionStart, ExpectedVersion: clock.Version})
	if err != nil {
		t.Fatalf("start clock: %v", err)
	}
	now = now.Add(7 * time.Second)
	if got := store.PublicSnapshot(matchID).Clock; got != "10:07" {
		t.Fatalf("running snapshot clock = %s, want 10:07", got)
	}
	clock, err = store.SetClock(matchID, ClockCommand{Action: ClockActionPause, ExpectedVersion: clock.Version})
	if err != nil {
		t.Fatalf("pause clock: %v", err)
	}
	if clock.Running || clock.ElapsedSeconds != 607 || clock.AnchorAt != nil {
		t.Fatalf("paused clock = %+v", clock)
	}
}

func TestRunningClockCapsElapsedTimeAtDatabaseMaximumWhenPaused(t *testing.T) {
	store := NewStore()
	matchID := "running-clock-at-maximum"
	now := time.Date(2026, 7, 17, 20, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }

	clock, err := store.SetClock(matchID, ClockCommand{
		Action: ClockActionSet, Period: "first_half", ElapsedSeconds: intPointer(maxClockElapsedSeconds), ExpectedVersion: 0,
	})
	if err != nil {
		t.Fatalf("set clock: %v", err)
	}
	clock, err = store.SetClock(matchID, ClockCommand{Action: ClockActionStart, ExpectedVersion: clock.Version})
	if err != nil {
		t.Fatalf("start clock: %v", err)
	}
	now = now.Add(7 * time.Second)
	clock, err = store.SetClock(matchID, ClockCommand{Action: ClockActionPause, ExpectedVersion: clock.Version})
	if err != nil {
		t.Fatalf("pause clock at maximum: %v", err)
	}
	if clock.Running || clock.ElapsedSeconds != maxClockElapsedSeconds || clock.AnchorAt != nil {
		t.Fatalf("paused clock = %+v, want capped at %d seconds", clock, maxClockElapsedSeconds)
	}
}

func TestStartingPreMatchClockBeginsFirstHalf(t *testing.T) {
	store := NewStore()
	now := time.Date(2026, 7, 22, 14, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }

	clock, err := store.SetClock("kickoff-clock", ClockCommand{Action: ClockActionStart, ExpectedVersion: 0})
	if err != nil {
		t.Fatalf("start clock: %v", err)
	}
	if !clock.Running || clock.Period != "first_half" || clock.ElapsedSeconds != 0 {
		t.Fatalf("started clock = %+v, want running first_half at 00:00", clock)
	}

	// Repair the state produced by the old start behavior without resetting elapsed time.
	legacy := MatchClock{MatchID: "legacy-kickoff-clock", Period: "pre_match", ElapsedSeconds: 75, Running: true, AnchorAt: timePointer(now), Version: 4}
	store.clocks[legacy.MatchID] = legacy
	clock, err = store.SetClock(legacy.MatchID, ClockCommand{Action: ClockActionStart, ExpectedVersion: legacy.Version})
	if err != nil {
		t.Fatalf("repair legacy start: %v", err)
	}
	if !clock.Running || clock.Period != "first_half" || clock.ElapsedSeconds != 75 || clock.Version != 5 {
		t.Fatalf("repaired clock = %+v, want running first_half preserving elapsed time", clock)
	}
}

func TestClockRejectsStaleVersion(t *testing.T) {
	store := NewStore()
	clock, err := store.SetClock("versioned-clock", ClockCommand{
		Action: ClockActionSet, Period: "first_half", ElapsedSeconds: intPointer(60), ExpectedVersion: 0,
	})
	if err != nil {
		t.Fatalf("set clock: %v", err)
	}
	if _, err := store.SetClock("versioned-clock", ClockCommand{
		Action: ClockActionAdjust, DeltaSeconds: intPointer(10), ExpectedVersion: clock.Version - 1,
	}); !errors.Is(err, ErrClockVersionConflict) {
		t.Fatalf("stale update error = %v, want ErrClockVersionConflict", err)
	}
}

func intPointer(value int) *int {
	return &value
}

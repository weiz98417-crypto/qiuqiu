package conversation

import (
	"context"
	"testing"
	"time"
)

func TestProactiveTurnWaitsForActiveUserTurn(t *testing.T) {
	scheduler := NewScheduler(context.Background(), Config{})
	t.Cleanup(scheduler.Close)

	userStarted := make(chan struct{})
	releaseUser := make(chan struct{})
	userCanceled := make(chan struct{}, 1)
	proactiveStarted := make(chan struct{})

	scheduler.SubmitUser(func(ctx context.Context, playback Playback) {
		close(userStarted)
		select {
		case <-releaseUser:
		case <-ctx.Done():
			userCanceled <- struct{}{}
		}
	})

	waitForSignal(t, userStarted, "user turn to start")
	scheduler.SubmitProactive("goal-1", UrgencyCritical, time.Minute, func(context.Context, Playback) {
		close(proactiveStarted)
	})

	select {
	case <-userCanceled:
		t.Fatal("proactive turn canceled the active user turn")
	case <-proactiveStarted:
		t.Fatal("proactive turn started before the user turn finished")
	case <-time.After(50 * time.Millisecond):
	}

	close(releaseUser)
	waitForSignal(t, proactiveStarted, "queued proactive turn to start")
}

func TestUserTurnPreemptsActiveProactiveTurn(t *testing.T) {
	scheduler := NewScheduler(context.Background(), Config{})
	t.Cleanup(scheduler.Close)

	proactiveStarted := make(chan struct{})
	proactiveCanceled := make(chan struct{})
	userStarted := make(chan struct{})

	scheduler.SubmitProactive("goal-1", UrgencyCritical, time.Minute, func(ctx context.Context, playback Playback) {
		close(proactiveStarted)
		<-ctx.Done()
		close(proactiveCanceled)
	})
	waitForSignal(t, proactiveStarted, "proactive turn to start")

	scheduler.SubmitUser(func(context.Context, Playback) {
		close(userStarted)
	})

	waitForSignal(t, proactiveCanceled, "proactive turn to be canceled")
	waitForSignal(t, userStarted, "user turn to start after preemption")
}

func TestQueuedProactiveTurnWaitsForPlaybackToEnd(t *testing.T) {
	scheduler := NewScheduler(context.Background(), Config{})
	t.Cleanup(scheduler.Close)

	userGenerated := make(chan struct{})
	proactiveStarted := make(chan struct{})
	scheduler.SubmitUser(func(ctx context.Context, playback Playback) {
		playback("trace-user-1")
		close(userGenerated)
	})
	waitForSignal(t, userGenerated, "user audio to be generated")

	scheduler.SubmitProactive("goal-1", UrgencyCritical, time.Minute, func(context.Context, Playback) {
		close(proactiveStarted)
	})
	select {
	case <-proactiveStarted:
		t.Fatal("proactive turn started before user playback ended")
	case <-time.After(50 * time.Millisecond):
	}

	scheduler.PlaybackChanged("trace-user-1", "ended")
	waitForSignal(t, proactiveStarted, "proactive turn after playback ended")
}

func TestInterruptCancelsActiveTurn(t *testing.T) {
	scheduler := NewScheduler(context.Background(), Config{})
	t.Cleanup(scheduler.Close)

	started := make(chan struct{})
	canceled := make(chan struct{})
	scheduler.SubmitProactive("goal-1", UrgencyCritical, time.Minute, func(ctx context.Context, playback Playback) {
		playback("trace-goal-1")
		close(started)
		<-ctx.Done()
		close(canceled)
	})
	waitForSignal(t, started, "active turn to start")

	scheduler.Interrupt()
	waitForSignal(t, canceled, "active turn to be canceled")
}

func TestDuplicateProactiveTurnIsIgnored(t *testing.T) {
	scheduler := NewScheduler(context.Background(), Config{})
	t.Cleanup(scheduler.Close)

	releaseUser := make(chan struct{})
	userStarted := make(chan struct{})
	firstStarted := make(chan struct{})
	duplicateStarted := make(chan struct{})
	scheduler.SubmitUser(func(context.Context, Playback) {
		close(userStarted)
		<-releaseUser
	})
	waitForSignal(t, userStarted, "user turn to start")

	scheduler.SubmitProactive("goal-1", UrgencyCritical, time.Minute, func(context.Context, Playback) {
		close(firstStarted)
	})
	scheduler.SubmitProactive("goal-1", UrgencyCritical, time.Minute, func(context.Context, Playback) {
		close(duplicateStarted)
	})
	close(releaseUser)
	waitForSignal(t, firstStarted, "first proactive turn to start")

	select {
	case <-duplicateStarted:
		t.Fatal("duplicate proactive turn started")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestExpiredProactiveTurnIsDropped(t *testing.T) {
	scheduler := NewScheduler(context.Background(), Config{})
	t.Cleanup(scheduler.Close)

	releaseUser := make(chan struct{})
	userStarted := make(chan struct{})
	expiredStarted := make(chan struct{})
	scheduler.SubmitUser(func(context.Context, Playback) {
		close(userStarted)
		<-releaseUser
	})
	waitForSignal(t, userStarted, "user turn to start")

	scheduler.SubmitProactive("commentary-1", UrgencyNormal, 20*time.Millisecond, func(context.Context, Playback) {
		close(expiredStarted)
	})
	time.Sleep(40 * time.Millisecond)
	close(releaseUser)

	select {
	case <-expiredStarted:
		t.Fatal("expired proactive turn started")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestCriticalProactiveTurnRunsBeforeNormalTurn(t *testing.T) {
	scheduler := NewScheduler(context.Background(), Config{})
	t.Cleanup(scheduler.Close)

	releaseUser := make(chan struct{})
	userStarted := make(chan struct{})
	order := make(chan string, 2)
	scheduler.SubmitUser(func(context.Context, Playback) {
		close(userStarted)
		<-releaseUser
	})
	waitForSignal(t, userStarted, "user turn to start")

	scheduler.SubmitProactive("commentary-1", UrgencyNormal, time.Minute, func(context.Context, Playback) {
		order <- "normal"
	})
	scheduler.SubmitProactive("goal-1", UrgencyCritical, time.Minute, func(context.Context, Playback) {
		order <- "critical"
	})
	close(releaseUser)

	select {
	case got := <-order:
		if got != "critical" {
			t.Fatalf("first proactive turn = %q, want critical", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for queued proactive turn")
	}
}

func TestPlaybackTimeoutReleasesQueuedTurn(t *testing.T) {
	scheduler := NewScheduler(context.Background(), Config{PlaybackTimeout: 20 * time.Millisecond})
	t.Cleanup(scheduler.Close)

	userGenerated := make(chan struct{})
	proactiveStarted := make(chan struct{})
	scheduler.SubmitUser(func(ctx context.Context, playback Playback) {
		playback("trace-user-timeout")
		close(userGenerated)
	})
	waitForSignal(t, userGenerated, "user audio to be generated")
	scheduler.SubmitProactive("goal-1", UrgencyCritical, time.Minute, func(context.Context, Playback) {
		close(proactiveStarted)
	})

	waitForSignal(t, proactiveStarted, "queued turn after playback timeout")
}

func TestUserInputReleasesCompletedTurnWaitingForPlayback(t *testing.T) {
	scheduler := NewScheduler(context.Background(), Config{})
	t.Cleanup(scheduler.Close)

	firstReturned := make(chan struct{})
	secondStarted := make(chan struct{})
	scheduler.SubmitUser(func(ctx context.Context, playback Playback) {
		playback("trace-first")
		close(firstReturned)
	})
	waitForSignal(t, firstReturned, "first user turn to finish generation")
	time.Sleep(20 * time.Millisecond)

	scheduler.SubmitUser(func(context.Context, Playback) {
		close(secondStarted)
	})
	waitForSignal(t, secondStarted, "second user turn after interrupting playback")
}

func TestUserInputDoesNotWaitForCanceledTurnToReturn(t *testing.T) {
	scheduler := NewScheduler(context.Background(), Config{})
	t.Cleanup(scheduler.Close)

	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondStarted := make(chan struct{})
	scheduler.SubmitUser(func(context.Context, Playback) {
		close(firstStarted)
		<-releaseFirst
	})
	waitForSignal(t, firstStarted, "first user turn to start")

	scheduler.SubmitUser(func(context.Context, Playback) {
		close(secondStarted)
	})
	waitForSignal(t, secondStarted, "second user turn without waiting for canceled provider")
	close(releaseFirst)
}

func waitForSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

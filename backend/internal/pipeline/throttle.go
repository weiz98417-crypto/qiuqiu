package pipeline

import (
	"context"
	"fmt"
	"qiuqiu/internal/event"
	"time"

	"github.com/redis/go-redis/v9"
)

// Throttler limits event frequency by type.
type Throttler struct {
	rdb     *redis.Client
	config  ThrottleConfig
}

type ThrottleConfig struct {
	Goal       time.Duration
	Shot       time.Duration
	Foul       time.Duration
	YellowCard time.Duration
	Corner     time.Duration
	Offside    time.Duration
}

func DefaultThrottleConfig() ThrottleConfig {
	return ThrottleConfig{
		Goal:       0,
		Shot:       15 * time.Second,
		Foul:       30 * time.Second,
		YellowCard: 0,
		Corner:     60 * time.Second,
		Offside:    60 * time.Second,
	}
}

// IntervalFor returns the throttle interval for a given event type.
func (c ThrottleConfig) IntervalFor(eventType string) time.Duration {
	switch eventType {
	case "goal", "red_card", "penalty", "match_start", "match_end":
		return 0 // never throttle
	case "shot":
		return c.Shot
	case "foul", "substitution", "var_check":
		return c.Foul
	case "yellow_card":
		return c.YellowCard
	case "corner":
		return c.Corner
	case "offside":
		return c.Offside
	default:
		return 30 * time.Second
	}
}

var throttleMultiplier float64 = 1.0

func SetThrottleMultiplier(m float64) {
	throttleMultiplier = m
}

func NewThrottler(rdb *redis.Client) *Throttler {
	return &Throttler{
		rdb:    rdb,
		config: DefaultThrottleConfig(),
	}
}

// ShouldThrottle checks if the event should be suppressed by throttle.
func (t *Throttler) ShouldThrottle(ev *event.StandardEvent) bool {
	interval := t.config.IntervalFor(ev.Type)
	if interval == 0 {
		return false
	}
	if t.rdb == nil {
		return false
	}

	key := fmt.Sprintf("throttle:%d:%s", ev.ID/1000000, ev.Type)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	s := interval.Nanoseconds()
	s = int64(float64(s) * throttleMultiplier)
	if s < 0 { s = 0 }
	ok, err := t.rdb.SetNX(ctx, key, "1", time.Duration(s)).Result()
	if err != nil {
		return false
	}
	return !ok
}

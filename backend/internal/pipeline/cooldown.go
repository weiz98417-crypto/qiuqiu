package pipeline

import (
	"time"
)

// Cooldown manages the dynamic cooldown period between AI utterances.
// Base interval: 8s, shortened to 5s during dense periods, extended to 12s during quiet periods.
type Cooldown struct {
	until       time.Time
	baseDur     time.Duration
	denseDur    time.Duration
	quietDur    time.Duration
	lastEventAt time.Time
	eventCount  int
}

func NewCooldown() *Cooldown {
	return &Cooldown{
		baseDur:  8 * time.Second,
		denseDur: 5 * time.Second,
		quietDur: 12 * time.Second,
	}
}

// SetBase updates the base cooldown duration dynamically (for talkativeness control).
func (c *Cooldown) SetBase(d time.Duration) {
	c.baseDur = d
}

// IsActive returns true if cooldown is still in effect.
func (c *Cooldown) IsActive() bool {
	return time.Now().Before(c.until)
}

// Record marks a new utterance and calculates the next cooldown duration.
func (c *Cooldown) Record() {
	now := time.Now()

	// Track event density: if last event was within 30s, we're in dense mode
	if now.Sub(c.lastEventAt) < 30*time.Second {
		c.eventCount++
	} else {
		c.eventCount = 0
	}
	c.lastEventAt = now

	dur := c.baseDur
	if c.eventCount > 3 {
		dur = c.denseDur // dense: shorten cooldown
	} else if c.eventCount == 0 {
		dur = c.quietDur // quiet: extend cooldown
	}

	c.until = now.Add(dur)
}

// ForceBreak immediately ends the cooldown (used for P0 events).
func (c *Cooldown) ForceBreak() {
	c.until = time.Now()
}

// Until returns when the cooldown ends.
func (c *Cooldown) Until() time.Time {
	return c.until
}

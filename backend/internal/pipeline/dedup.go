package pipeline

import (
	"context"
	"fmt"
	"qiuqiu/internal/event"
	"time"

	"github.com/redis/go-redis/v9"
)

// Deduper prevents duplicate events using Redis sliding window.
type Deduper struct {
	rdb *redis.Client
}

func NewDeduper(rdb *redis.Client) *Deduper {
	return &Deduper{rdb: rdb}
}

// IsDuplicate checks if the event has been seen within the TTL window.
// Key: dedup:{match_id}:{type}:{team}:{minute}, TTL 30s.
func (d *Deduper) IsDuplicate(ev *event.StandardEvent) bool {
	if d.rdb == nil {
		return false // no Redis, skip dedup
	}

	key := fmt.Sprintf("dedup:%d:%s:%s:%d", ev.ID/1000000, ev.Type, ev.Team, ev.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	ok, err := d.rdb.SetNX(ctx, key, "1", 30*time.Second).Result()
	if err != nil {
		return false // on Redis error, don't block
	}
	return !ok // SetNX returns true if key didn't exist
}

// DedupKey returns the dedup key for testing.
func (d *Deduper) DedupKey(ev *event.StandardEvent) string {
	return fmt.Sprintf("dedup:%d:%s:%s:%d", ev.ID/1000000, ev.Type, ev.Team, ev.Minute)
}

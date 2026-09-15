package main

import (
	"strings"
	"sync"
	"time"
)

const (
	userSignalDedupeTTL        = 15 * time.Minute
	userSignalDedupeMaxEntries = 4096
)

type signalDeduper struct {
	mu      sync.Mutex
	entries map[string]time.Time
	ttl     time.Duration
	maxSize int
}

func newSignalDeduper(ttl time.Duration, maxSize int) *signalDeduper {
	if ttl <= 0 {
		ttl = userSignalDedupeTTL
	}
	if maxSize <= 0 {
		maxSize = userSignalDedupeMaxEntries
	}
	return &signalDeduper{entries: make(map[string]time.Time), ttl: ttl, maxSize: maxSize}
}

func (d *signalDeduper) Claim(key string, now time.Time) bool {
	key = strings.TrimSpace(key)
	if d == nil || key == "" {
		return false
	}
	if now.IsZero() {
		now = time.Now()
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	for existing, claimedAt := range d.entries {
		if now.Sub(claimedAt) >= d.ttl {
			delete(d.entries, existing)
		}
	}
	if claimedAt, exists := d.entries[key]; exists && now.Sub(claimedAt) < d.ttl {
		return false
	}
	if len(d.entries) >= d.maxSize {
		var oldestKey string
		var oldestAt time.Time
		for existing, claimedAt := range d.entries {
			if oldestKey == "" || claimedAt.Before(oldestAt) {
				oldestKey = existing
				oldestAt = claimedAt
			}
		}
		if oldestKey != "" {
			delete(d.entries, oldestKey)
		}
	}
	d.entries[key] = now
	return true
}

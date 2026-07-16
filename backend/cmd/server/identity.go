package main

import (
	"context"
	"strings"
	"sync"

	"qiuqiu/internal/config"
)

type connectionIdentity struct {
	mu     sync.RWMutex
	userID string
	ready  chan struct{}
	once   sync.Once
}

func connectionUserID(identity *connectionIdentity, cfg *config.Config, requested string) (string, bool) {
	if current := identity.Get(); current != "" {
		requested = stableUserID(requested, "")
		if cfg != nil && cfg.SessionAuthRequired() && requested != "" && requested != current {
			return "", false
		}
		return current, true
	}
	if cfg != nil && cfg.LegacyAuthAllowed() {
		return identity.Set(requested), true
	}
	return "", true
}

func newConnectionIdentity(fallback string) *connectionIdentity {
	identity := &connectionIdentity{userID: stableUserID(fallback, ""), ready: make(chan struct{})}
	if identity.userID != "" {
		identity.once.Do(func() { close(identity.ready) })
	}
	return identity
}

func (identity *connectionIdentity) Set(value string) string {
	identity.mu.Lock()
	defer identity.mu.Unlock()
	if identity.userID != "" {
		return identity.userID
	}
	identity.userID = stableUserID(value, "")
	if identity.userID != "" {
		identity.once.Do(func() { close(identity.ready) })
	}
	return identity.userID
}

func (identity *connectionIdentity) Get() string {
	identity.mu.RLock()
	defer identity.mu.RUnlock()
	return identity.userID
}

func (identity *connectionIdentity) Wait(ctx context.Context) string {
	if userID := identity.Get(); userID != "" {
		return userID
	}
	select {
	case <-identity.ready:
		return identity.Get()
	case <-ctx.Done():
		return ""
	}
}

func stableUserID(value, fallback string) string {
	return stableIdentifier(value, fallback, 128, false)
}

func stableSignalID(value, fallback string) string {
	return stableIdentifier(value, fallback, 256, true)
}

func stableIdentifier(value, fallback string, maximumLength int, allowColon bool) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maximumLength {
		return fallback
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' {
			continue
		}
		switch character {
		case '_', '-', '.':
			continue
		case ':':
			if allowColon {
				continue
			}
			return fallback
		default:
			return fallback
		}
	}
	return value
}

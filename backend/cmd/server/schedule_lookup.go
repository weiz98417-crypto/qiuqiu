package main

import (
	"context"
	"sync"
	"time"
)

type scheduleLookupLifecycle struct {
	mu         sync.Mutex
	generation uint64
	lookupID   string
	cancel     context.CancelFunc
}

func (lifecycle *scheduleLookupLifecycle) BeginTurn() uint64 {
	lifecycle.mu.Lock()
	lifecycle.generation++
	generation := lifecycle.generation
	cancel := lifecycle.cancel
	lifecycle.lookupID = ""
	lifecycle.cancel = nil
	lifecycle.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return generation
}

func (lifecycle *scheduleLookupLifecycle) Start(parent context.Context, generation uint64, lookupID string, expiresAt time.Time) (context.Context, bool) {
	if parent == nil {
		parent = context.Background()
	}
	var lookupCtx context.Context
	var cancel context.CancelFunc
	if expiresAt.IsZero() {
		lookupCtx, cancel = context.WithCancel(parent)
	} else {
		lookupCtx, cancel = context.WithDeadline(parent, expiresAt)
	}

	lifecycle.mu.Lock()
	if generation != lifecycle.generation || lookupID == "" {
		lifecycle.mu.Unlock()
		cancel()
		return lookupCtx, false
	}
	previousCancel := lifecycle.cancel
	lifecycle.lookupID = lookupID
	lifecycle.cancel = cancel
	lifecycle.mu.Unlock()
	if previousCancel != nil {
		previousCancel()
	}
	return lookupCtx, true
}

func (lifecycle *scheduleLookupLifecycle) IsCurrent(lookupID string) bool {
	lifecycle.mu.Lock()
	defer lifecycle.mu.Unlock()
	return lookupID != "" && lifecycle.lookupID == lookupID
}

func (lifecycle *scheduleLookupLifecycle) Complete(lookupID string) bool {
	lifecycle.mu.Lock()
	if lookupID == "" || lifecycle.lookupID != lookupID {
		lifecycle.mu.Unlock()
		return false
	}
	cancel := lifecycle.cancel
	lifecycle.lookupID = ""
	lifecycle.cancel = nil
	lifecycle.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return true
}

func (lifecycle *scheduleLookupLifecycle) Cancel() {
	lifecycle.BeginTurn()
}

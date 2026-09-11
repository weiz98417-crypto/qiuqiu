package matchstate

import "strings"

const (
	LifecycleDraft     = "draft"
	LifecycleScheduled = "scheduled"
	LifecycleLive      = "live"
	LifecycleFinished  = "finished"
	LifecycleCancelled = "cancelled"
	LifecycleArchived  = "archived"
)

func normalizeLifecycle(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case LifecycleDraft, LifecycleScheduled, LifecycleLive, LifecycleFinished, LifecycleCancelled, LifecycleArchived:
		return value
	default:
		return ""
	}
}

func validLifecycleTransition(from, to string) bool {
	from, to = normalizeLifecycle(from), normalizeLifecycle(to)
	if to == "" {
		return false
	}
	if from == "" {
		// Legacy matches have no explicit lifecycle; allow an operator to adopt
		// any sensible state, while preserving clock-derived public behaviour.
		return true
	}
	if from == to {
		return true
	}
	switch from {
	case LifecycleDraft:
		return to == LifecycleScheduled || to == LifecycleCancelled
	case LifecycleScheduled:
		return to == LifecycleLive || to == LifecycleCancelled || to == LifecycleDraft
	case LifecycleLive:
		return to == LifecycleFinished || to == LifecycleCancelled
	case LifecycleFinished, LifecycleCancelled:
		return to == LifecycleArchived
	case LifecycleArchived:
		return false
	default:
		return false
	}
}

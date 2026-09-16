package conversation

import (
	"strings"
	"time"

	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/relationship"
)

// Proactive citation reason codes (C2): every proactive turn must cite what
// justifies it — an open thread from the ledger or a shared/recalled match
// moment. The emitted request carries the code verbatim, e.g.
// "open_thread:123" or "shared_moment:<eventId>".
const (
	CitationOpenThread   = "open_thread"
	CitationSharedMoment = "shared_moment"
)

// ThreadCitation builds the reason code referencing an open thread id.
func ThreadCitation(threadID string) string {
	return CitationOpenThread + ":" + strings.TrimSpace(threadID)
}

// EventCitation builds the reason code citing the match event as the shared
// moment (goal/card/VAR drama on the watched match).
func EventCitation(eventID string) string {
	return CitationSharedMoment + ":" + strings.TrimSpace(eventID)
}

// ProactiveGate keeps the operator whitelist and mode as preconditions and
// adds the two C2 requirements: a non-empty citation (no reflex without a
// reason) and the user's talkativeness tier (quiet restricts, never enables).
type ProactiveGate struct{}

func NewProactiveGate() *ProactiveGate {
	return &ProactiveGate{}
}

// Allow decides one in-match proactive turn. citation is the reason code from
// ThreadCitation/EventCitation; talkativeness is the raw client tier
// ("", "quiet", "normal", "active"); critical marks critical match events.
func (g *ProactiveGate) Allow(policy matchstate.AutomationPolicy, eventType, citation, talkativeness string, critical bool, _ time.Time) bool {
	if policy.Mode != matchstate.AutomationModeActive {
		return false
	}
	if !containsEventType(policy.EventTypes, eventType) {
		return false
	}
	if strings.TrimSpace(citation) == "" {
		return false
	}
	// Quiet tier: never initiative except critical match events already
	// allowed by policy. L0-safe — the tier can only suppress.
	if relationship.IsQuiet(talkativeness) && !critical {
		return false
	}
	return true
}

func (g *ProactiveGate) AllowManual(_ time.Time) bool {
	return true
}

func containsEventType(eventTypes []string, eventType string) bool {
	for _, allowed := range eventTypes {
		if allowed == eventType {
			return true
		}
	}
	return false
}

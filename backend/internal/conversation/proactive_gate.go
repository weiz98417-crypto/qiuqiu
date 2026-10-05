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
	// CitationPivotal 是赛点事件的固定引用码（policy-bits B2）：下游拼上
	// proactive_citation: 前缀后，理由码 = proactive_citation:pivotal——
	// 赛点的抬档理由就是赛点本身，压过 open_thread/shared_moment 的常规
	// 引用序。
	CitationPivotal = "pivotal"
	// C2 的第三把钥匙（ADR-0015）"reminder:<id>"（用户显式请求的提醒）
	// 定义在 internal/proactive——本包不能反向依赖它（conversation ←
	// companion ← proactive 会成环），gate 只要求引用码非空，前缀语义归
	// 各来源包所有。
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
// ThreadCitation/EventCitation (or CitationPivotal for pivotal moments);
// talkativeness is the raw client tier ("", "quiet", "normal", "active");
// critical marks critical match events; pivotal marks 赛点 (policy-bits B2).
func (g *ProactiveGate) Allow(policy matchstate.AutomationPolicy, eventType, citation, talkativeness string, critical, pivotal bool, _ time.Time) bool {
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
	// allowed by policy — and pivotal moments (B2 默认裁决 4.2: quiet 档
	// 赛点仍放行，单句短播的预算在 relationship contentPolicyFor，gate 只
	// 管放行)。L0-safe — the tier can only suppress.
	if relationship.IsQuiet(talkativeness) && !critical && !pivotal {
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

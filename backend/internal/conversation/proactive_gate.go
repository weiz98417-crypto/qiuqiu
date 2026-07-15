package conversation

import (
	"time"

	"qiuqiu/internal/matchstate"
)

type ProactiveGate struct{}

func NewProactiveGate() *ProactiveGate {
	return &ProactiveGate{}
}

func (g *ProactiveGate) Allow(policy matchstate.AutomationPolicy, eventType string, _ bool, _ time.Time) bool {
	return policy.Mode == matchstate.AutomationModeActive && containsEventType(policy.EventTypes, eventType)
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

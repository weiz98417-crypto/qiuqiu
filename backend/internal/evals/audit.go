package evals

import (
	"fmt"
	"strings"
	"time"

	"qiuqiu/internal/companion"
)

type AuditIssue struct {
	TraceID  string `json:"traceId"`
	Severity string `json:"severity"`
	Category string `json:"category"`
	Message  string `json:"message"`
}

type TraceAuditReport struct {
	SchemaVersion string       `json:"schemaVersion"`
	AuditedAt     time.Time    `json:"auditedAt"`
	TraceCount    int          `json:"traceCount"`
	Blockers      int          `json:"blockers"`
	Warnings      int          `json:"warnings"`
	Issues        []AuditIssue `json:"issues"`
}

func AuditTraces(traces []companion.Trace) TraceAuditReport {
	report := TraceAuditReport{SchemaVersion: SchemaVersion, AuditedAt: time.Now().UTC(), TraceCount: len(traces)}
	for _, trace := range traces {
		if strings.TrimSpace(trace.Output) == "" || strings.TrimSpace(trace.Reason) == "" {
			report.add(trace.ID, "blocker", "trace", "missing output or reason")
		}
		tools := toolsIn(trace)
		if !tools["trace.write_decision"] && trace.Reason != "operator_event_proactive_line" {
			report.add(trace.ID, "warning", "trace", "missing trace.write_decision tool marker")
		}
		for _, call := range trace.ToolCalls {
			if !companion.IsUserAgentToolAllowed(call.Name) {
				report.add(trace.ID, "blocker", "tool_boundary", fmt.Sprintf("disallowed tool %q", call.Name))
			}
		}
		if isFactIntent(trace.Intent) && len(trace.RetrievedEvent) == 0 && trace.Intent != companion.IntentMatchStatus {
			report.add(trace.ID, "warning", "grounding", "fact-oriented reply has no retrieved event")
		}
		if trace.Intent == companion.IntentMatchStatus && !tools["match.read_snapshot"] {
			report.add(trace.ID, "blocker", "trajectory", "status reply skipped match.read_snapshot")
		}
		if trace.Intent == companion.IntentRecentEvent && !tools["match.search_events"] {
			report.add(trace.ID, "blocker", "trajectory", "recent-event reply skipped match.search_events")
		}
		if trace.Intent == companion.IntentMatchClaim {
			if trace.Claim == nil {
				report.add(trace.ID, "blocker", "claim_safety", "match claim has no structured assessment")
			}
			if !tools["match.read_snapshot"] {
				report.add(trace.ID, "blocker", "trajectory", "match claim skipped match.read_snapshot")
			}
			if !tools["match.search_events"] {
				report.add(trace.ID, "blocker", "trajectory", "match claim skipped match.search_events")
			}
			if !tools["match.verify_user_claim"] {
				report.add(trace.ID, "blocker", "trajectory", "match claim skipped match.verify_user_claim")
			}
			if !usesDeterministicClaimPolicy(trace.ToolCalls) {
				report.add(trace.ID, "blocker", "claim_safety", "match claim did not use deterministic claim response policy")
			}
			if trace.Claim != nil && trace.Claim.Kind == "event" && trace.Claim.Status != companion.ClaimStatusUnverified && len(trace.RetrievedEvent) == 0 {
				report.add(trace.ID, "blocker", "grounding", "verified event claim has no retrieved event")
			}
		}
		if trace.Claim != nil && !usesDeterministicFactPolicy(trace.ToolCalls) {
			report.add(trace.ID, "blocker", "claim_safety", "structured fact assessment did not use a deterministic response policy")
		}
		if trace.Voice != nil {
			if trace.Voice.TTSStatus == "ok" && trace.Voice.TTSByteCount <= 0 {
				report.add(trace.ID, "blocker", "voice", "TTS marked ok without audio bytes")
			}
			if strings.HasPrefix(trace.Voice.PlaybackStatus, "failed") && strings.TrimSpace(trace.Output) == "" {
				report.add(trace.ID, "blocker", "voice", "playback failed without text fallback")
			}
		}
	}
	return report
}

func usesDeterministicClaimPolicy(calls []companion.ToolCall) bool {
	for _, call := range calls {
		if call.Name == "response.emit_companion_reply" && call.Args["mode"] == "deterministic" && call.Args["reason"] == "claim_policy" {
			return true
		}
	}
	return false
}

func usesDeterministicFactPolicy(calls []companion.ToolCall) bool {
	for _, call := range calls {
		if call.Name == "response.emit_companion_reply" && call.Args["mode"] == "deterministic" {
			return true
		}
	}
	return false
}

func (report *TraceAuditReport) add(traceID, severity, category, message string) {
	report.Issues = append(report.Issues, AuditIssue{TraceID: traceID, Severity: severity, Category: category, Message: message})
	if severity == "blocker" {
		report.Blockers++
	} else {
		report.Warnings++
	}
}

func isFactIntent(intent companion.Intent) bool {
	return intent == companion.IntentRecentEvent || intent == companion.IntentFollowUp || intent == companion.IntentPlayerQuestion
}

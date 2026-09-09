package evals

import (
	"strings"
	"time"

	"qiuqiu/internal/interaction"
)

type InteractionAuditReport struct {
	AuditedAt time.Time    `json:"auditedAt"`
	Blockers  int          `json:"blockers"`
	Issues    []AuditIssue `json:"issues"`
}

func AuditInteractions(events []interaction.Event) InteractionAuditReport {
	report := InteractionAuditReport{AuditedAt: time.Now().UTC()}
	turns := make(map[string]interaction.Event)
	deliveryStates := make(map[string]string)
	playbackCompleted := make(map[string]int)
	mediaFailed := make(map[string]bool)
	textDelivered := make(map[string]bool)
	staleTurns := make(map[string]bool)
	factRevisions := make(map[string]string)
	for _, event := range events {
		if event.Kind != interaction.KindFactRevision {
			continue
		}
		for _, factID := range event.FactIDs {
			factRevisions[factID] = event.FactRevision
		}
	}
	for _, event := range events {
		switch event.Kind {
		case interaction.KindTurnPlanned:
			turns[event.TraceID] = event
			if len(event.FactIDs) > 0 && strings.TrimSpace(event.FactRevision) == "" {
				grounded := true
				for _, factID := range event.FactIDs {
					if strings.TrimSpace(factRevisions[factID]) == "" {
						grounded = false
					}
				}
				if !grounded {
					report.add(event.TraceID, "grounding", "fact-backed turn has no fact revision")
				}
			}
		case interaction.KindDelivery:
			deliveryStates[event.TraceID] = event.DeliveryState
			if event.DeliveryState == "text_delivered" || event.DeliveryState == "completed" {
				textDelivered[event.TraceID] = true
			}
		case interaction.KindMediaDelivery:
			if event.DeliveryState == "failed" {
				mediaFailed[event.TraceID] = true
			}
		case interaction.KindPlaybackResult:
			if event.PlaybackState == "completed" || event.PlaybackState == "ended" {
				deliveryStates[event.TraceID] = "completed"
				playbackCompleted[event.DeliveryKey]++
			}
		case interaction.KindTurnStale:
			staleTurns[event.TraceID] = true
		}
	}
	for traceID := range turns {
		if traceID != "" && !staleTurns[traceID] && deliveryStates[traceID] == "" {
			report.add(traceID, "delivery", "planned turn has no delivery outcome")
		}
		if mediaFailed[traceID] && !textDelivered[traceID] {
			report.add(traceID, "fallback", "media failed without a delivered text fallback")
		}
	}
	for deliveryKey, count := range playbackCompleted {
		if strings.TrimSpace(deliveryKey) != "" && count > 1 {
			report.add(deliveryKey, "delivery", "delivery completed audible playback more than once")
		}
	}
	return report
}

func (report *InteractionAuditReport) add(traceID, category, message string) {
	report.Blockers++
	report.Issues = append(report.Issues, AuditIssue{TraceID: traceID, Severity: "blocker", Category: category, Message: message})
}

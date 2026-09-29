// Package deliverykey owns the deliveryKey string protocol. A deliveryKey
// identifies one deliverable unit across the delivery ledger (dedupe),
// interaction ledger (playback accounting), client FIFO pairing, and
// console replay. Before this package the formats lived as ad-hoc string
// concatenations across nine packages with four distinct semantics; every
// new proactive path added another bare key. The constructors here are the
// single source of those formats (voice-streaming-delivery task 3.1).
//
// Formats (all ":"-joined; components are ID-shaped, never user free text):
//   - event:      <eventID>:<factRevision>:<factStatus>   (match fact deliveries)
//   - trace:      <traceID>                               (conversation replies, plan follow-ups, lookups, decisions)
//   - reminder:   reminder:<reminderID>
//   - backchannel: backchannel-<eventID>                  (dash: predates this package, kept for ledger continuity)
//   - observation: <pendingID>:<resolvedRevision>:<status>
//
// Free-text components must go through playbackEventID-style escaping (see
// cmd/server/delivery.go) — never through these constructors.
package deliverykey

import (
	"fmt"

	"qiuqiu/internal/matchstate"
)

// ForEvent extrapolates the key of a match-fact delivery from its ledger
// event. Re-exports matchstate.DeliveryKey so callers reach one package for
// every key format.
func ForEvent(event matchstate.MatchEvent) string {
	return matchstate.DeliveryKey(event)
}

// ForTrace keys a delivery by its trace ID: conversation replies, open-thread
// recoveries, schedule lookups, proactive decisions, media-delivery records.
func ForTrace(traceID string) string {
	return traceID
}

// ForReminder keys a user-requested reminder delivery (ADR-0015 citation).
func ForReminder(reminderID string) string {
	return "reminder:" + reminderID
}

// ForBackchannel keys a micro-reaction audio delivery. Dash separator for
// continuity with keys already in ledgers.
func ForBackchannel(eventID string) string {
	return "backchannel-" + eventID
}

// ForObservation keys a resolved-observation delivery; the revision and
// status in the key let the same pending observation re-deliver after a
// re-resolution without colliding.
func ForObservation(pendingID string, resolvedRevision int, status string) string {
	return fmt.Sprintf("%s:%d:%s", pendingID, resolvedRevision, status)
}

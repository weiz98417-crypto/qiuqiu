package relationship

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
)

func applyRelationshipMemory(state *StateBundle, signal Signal, now time.Time) []string {
	if signal.Kind == SignalDeliveryResult && signal.Delivery != nil {
		return applyRelationshipMemoryDelivery(state, *signal.Delivery)
	}
	if signal.Kind == SignalMatchEvent && signal.Match != nil {
		return applySharedMomentMemory(state, signal, now)
	}
	if signal.Kind != SignalUserTurn || signal.User == nil {
		return nil
	}
	reasons := make([]string, 0, 3)
	if subject, direction, ok := explicitTastePreference(signal.User.Text); ok {
		payload, err := json.Marshal(TasteMemoryPayload{Subject: subject, Direction: direction})
		if err == nil {
			memory := newRelationshipMemory(signal, MemoryKindTasteEvidence, subject, payload, now)
			upsertRelationshipMemory(&state.Memories, memory)
			upsertTastePreference(&state.Relationship.Taste.StylePreferences, subject, direction, fallbackMemorySource(signal), now)
			reasons = append(reasons, "explicit_taste_memory_recorded")
		}
	}
	if topic, ok := explicitOpenThreadTopic(signal.User.Text); ok {
		payload, err := json.Marshal(OpenThreadMemoryPayload{Topic: topic})
		if err == nil {
			memory := newRelationshipMemory(signal, MemoryKindOpenThread, topic, payload, now)
			expiresAt := now.Add(14 * 24 * time.Hour)
			memory.ExpiresAt = &expiresAt
			upsertRelationshipMemory(&state.Memories, memory)
			reasons = append(reasons, "open_thread_memory_recorded")
		}
	}
	cues := mergeUserCues(signal.User.Cues, inferUserCues(strings.TrimSpace(signal.User.Text)))
	if hasCue(cues, CueBanterDenied) && state.Relationship.Repair.TriggerSignalID == signal.ID {
		scope := firstCueScope(cues, CueBanterDenied)
		if scope == "" {
			scope = "all"
		}
		boundaryScope := "banter:" + scope
		if !hasBoundary(state.Relationship.Boundaries, boundaryScope, "denied") {
			state.Relationship.Boundaries = append(state.Relationship.Boundaries, UserBoundary{
				ID:    relationshipMemoryID(signal.UserID, MemoryKindBoundary, boundaryScope),
				Scope: boundaryScope, Rule: "denied", Explicit: true,
				SourceSignalID: signal.ID, SourceTraceID: fallbackMemorySource(signal), CreatedAt: now,
			})
		}
	}
	for index := range state.Relationship.Boundaries {
		boundary := &state.Relationship.Boundaries[index]
		if boundary.SourceSignalID != signal.ID {
			continue
		}
		if boundary.SourceTraceID == "" {
			boundary.SourceTraceID = fallbackMemorySource(signal)
		}
		payload, err := json.Marshal(BoundaryMemoryPayload{Scope: boundary.Scope, Rule: boundary.Rule})
		if err != nil {
			continue
		}
		memory := newRelationshipMemory(signal, MemoryKindBoundary, boundary.Scope+"="+boundary.Rule, payload, now)
		if !hasRelationshipMemory(state.Memories, memory.ID) {
			state.Relationship.Evidence.ExplicitBoundaries++
		}
		upsertRelationshipMemory(&state.Memories, memory)
		reasons = append(reasons, "boundary_memory_recorded")
	}
	if state.Relationship.Repair.Active && state.Relationship.Repair.TriggerSignalID == signal.ID {
		payload, err := json.Marshal(ProcedureMemoryPayload{
			Category: state.Relationship.Repair.Category, BehaviorChanges: append([]string(nil), state.Relationship.Repair.BehaviorChanges...),
		})
		if err == nil {
			upsertRelationshipMemory(&state.Memories, newRelationshipMemory(signal, MemoryKindProcedure, state.Relationship.Repair.Category, payload, now))
			reasons = append(reasons, "repair_procedure_memory_recorded")
		}
	}
	return reasons
}

func applyRelationshipMemoryDelivery(state *StateBundle, delivery DeliverySignal) []string {
	decisionID := strings.TrimSpace(delivery.DecisionID)
	if decisionID == "" {
		return nil
	}
	reasons := make([]string, 0, 1)
	for index := range state.Memories {
		memory := &state.Memories[index]
		if memory.Kind != MemoryKindOpenThread || memory.Status != "active" || !containsString(memory.PendingDecisionIDs, decisionID) {
			continue
		}
		memory.PendingDecisionIDs = removeString(memory.PendingDecisionIDs, decisionID)
		if (delivery.State == "text_delivered" || delivery.State == "ok") && containsString(delivery.UsedMemoryIDs, memory.ID) {
			memory.Status = "resolved"
			memory.PendingDecisionIDs = nil
			reasons = append(reasons, "delivered_open_thread_resolved")
		} else {
			reasons = append(reasons, "open_thread_delivery_pending_cleared")
		}
	}
	return reasons
}

func selectRelationshipMemories(state *StateBundle, signal Signal, now time.Time, decisionID string, limit int) []RelationshipMemory {
	if limit <= 0 || signal.Kind != SignalUserTurn || signal.User == nil {
		return nil
	}
	text := strings.TrimSpace(signal.User.Text)
	cues := mergeUserCues(signal.User.Cues, inferUserCues(text))
	selected := make([]RelationshipMemory, 0, limit)
	for index := range state.Memories {
		memory := &state.Memories[index]
		if memory.Status != "active" || (memory.ExpiresAt != nil && !memory.ExpiresAt.After(now)) {
			continue
		}
		matched := false
		switch memory.Kind {
		case MemoryKindTasteEvidence:
			var payload TasteMemoryPayload
			matched = json.Unmarshal(memory.Payload, &payload) == nil && payload.Subject != "" && strings.Contains(text, payload.Subject)
		case MemoryKindOpenThread:
			matched = hasCue(cues, CueOpenThreadReady)
		case MemoryKindSharedMoment:
			matched = hasCue(cues, CueSharedMomentRecalled)
		}
		if !matched {
			continue
		}
		usedAt := now
		memory.LastUsedAt = &usedAt
		if memory.Kind == MemoryKindOpenThread {
			memory.PendingDecisionIDs = []string{decisionID}
		}
		selected = append(selected, cloneRelationshipMemory(*memory))
		if len(selected) == limit {
			break
		}
	}
	return selected
}

func applySharedMomentMemory(state *StateBundle, signal Signal, now time.Time) []string {
	match := signal.Match
	if match == nil || !match.Confirmed || !match.Critical || match.Intensity < 4 || strings.TrimSpace(match.EventID) == "" {
		return nil
	}
	reasons := make([]string, 0, 2)
	if match.RevisionOf != "" {
		for index := range state.Memories {
			memory := &state.Memories[index]
			if memory.Kind != MemoryKindSharedMoment || memory.Status != "active" {
				continue
			}
			var payload SharedMomentMemoryPayload
			if json.Unmarshal(memory.Payload, &payload) == nil && payload.EventID == match.RevisionOf {
				memory.Status = "revoked"
				reasons = append(reasons, "superseded_shared_moment_revoked")
			}
		}
	}
	summary := strings.TrimSpace(match.Description)
	if summary == "" {
		summary = match.EventType
	}
	payload, err := json.Marshal(SharedMomentMemoryPayload{
		EventID: match.EventID, EventType: match.EventType, Summary: summary,
		TeamName: match.TeamName, PlayerName: match.PlayerName, RevisionOf: match.RevisionOf,
	})
	if err != nil {
		return reasons
	}
	memory := newRelationshipMemory(signal, MemoryKindSharedMoment, match.EventID, payload, now)
	upsertRelationshipMemory(&state.Memories, memory)
	return append(reasons, "shared_moment_memory_recorded")
}

func explicitTastePreference(text string) (string, string, bool) {
	text = strings.TrimSpace(text)
	for _, candidate := range []struct {
		prefix    string
		direction string
	}{
		{prefix: "我不喜欢", direction: "dislike"},
		{prefix: "我更吃", direction: "like"},
		{prefix: "我喜欢", direction: "like"},
		{prefix: "我支持", direction: "like"},
	} {
		position := strings.Index(text, candidate.prefix)
		if position < 0 {
			continue
		}
		subject := normalizeTasteSubject(text[position+len(candidate.prefix):])
		if subject != "" {
			return subject, candidate.direction, true
		}
	}
	return "", "", false
}

func normalizeTasteSubject(subject string) string {
	subject = strings.Trim(strings.TrimSpace(subject), "，。！？、,.!?;；：: ")
	for _, suffix := range []string{"这一套", "这套", "这种踢法", "的踢法", "一点"} {
		subject = strings.TrimSuffix(subject, suffix)
	}
	return strings.TrimSpace(subject)
}

func explicitOpenThreadTopic(text string) (string, bool) {
	text = strings.TrimSpace(text)
	for _, marker := range []string{"下场接着聊", "下次接着聊", "回头再说", "下半场再聊", "赛后再聊"} {
		position := strings.Index(text, marker)
		if position < 0 {
			continue
		}
		topic := strings.Trim(strings.TrimSpace(text[:position]), "，。！？、,.!?;；：: ")
		if topic != "" {
			return topic, true
		}
	}
	return "", false
}

func upsertRelationshipMemory(memories *[]RelationshipMemory, candidate RelationshipMemory) {
	for index := range *memories {
		if (*memories)[index].ID != candidate.ID {
			continue
		}
		candidate.CreatedAt = (*memories)[index].CreatedAt
		candidate.LastUsedAt = (*memories)[index].LastUsedAt
		*memories = append((*memories)[:index], (*memories)[index+1:]...)
		break
	}
	*memories = append([]RelationshipMemory{candidate}, *memories...)
}

func hasRelationshipMemory(memories []RelationshipMemory, id string) bool {
	for _, memory := range memories {
		if memory.ID == id {
			return true
		}
	}
	return false
}

func upsertTastePreference(preferences *[]TastePreference, subject, direction, evidenceRef string, now time.Time) {
	for index := range *preferences {
		if (*preferences)[index].Subject != subject {
			continue
		}
		preference := &(*preferences)[index]
		preference.Direction = direction
		preference.Confidence = 1
		preference.LastUpdatedAt = now
		if !containsString(preference.EvidenceRefs, evidenceRef) {
			preference.EvidenceRefs = append(preference.EvidenceRefs, evidenceRef)
		}
		return
	}
	*preferences = append(*preferences, TastePreference{
		Subject: subject, Direction: direction, Confidence: 1,
		EvidenceRefs: []string{evidenceRef}, LastUpdatedAt: now,
	})
}

func newRelationshipMemory(signal Signal, kind, subject string, payload []byte, now time.Time) RelationshipMemory {
	return RelationshipMemory{
		ID: relationshipMemoryID(signal.UserID, kind, subject), UserID: signal.UserID, MatchID: signal.MatchID,
		Kind: kind, Payload: payload, Confidence: 1, SourceTraceID: fallbackMemorySource(signal), Status: "active", CreatedAt: now,
	}
}

func firstCueScope(cues []UserCue, kind UserCueKind) string {
	for _, cue := range cues {
		if cue.Kind == kind {
			return strings.TrimSpace(cue.Scope)
		}
	}
	return ""
}

func relationshipMemoryID(userID, kind, subject string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(userID) + "\x00" + kind + "\x00" + strings.TrimSpace(subject)))
	return "memory_" + hex.EncodeToString(digest[:12])
}

func fallbackMemorySource(signal Signal) string {
	if strings.TrimSpace(signal.TraceID) != "" {
		return strings.TrimSpace(signal.TraceID)
	}
	return signal.ID
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func removeString(values []string, unwanted string) []string {
	kept := values[:0]
	for _, value := range values {
		if value != unwanted {
			kept = append(kept, value)
		}
	}
	return kept
}

package matchstate

import (
	"fmt"
	"strings"
	"time"
)

type FactCommandResult struct {
	Events     []MatchEvent
	Changed    MatchEvent
	Retracted  []MatchEvent
	Integrity  *MatchIntegrity
	Projection FactLedgerProjection
}

type FactConflictCommandResult struct {
	AppliedAt         string
	Events            []MatchEvent
	Conflict          FactConflict
	Changed           []MatchEvent
	Retracted         []MatchEvent
	ReconciledFactIDs []string
	SelectedFactIDs   []string
	ConflictResolved  bool
}

func (FactLedgerEngine) ResolveConflict(input FactLedgerProjectInput, conflict FactConflict, selectedFactIDs []string, operatorID, reason string) (FactConflictCommandResult, error) {
	matchID := strings.TrimSpace(input.MatchID)
	operatorID = strings.TrimSpace(operatorID)
	reason = strings.TrimSpace(reason)
	if matchID == "" || strings.TrimSpace(conflict.ID) == "" || len(uniqueFactIDs(selectedFactIDs)) == 0 || operatorID == "" || reason == "" {
		return FactConflictCommandResult{}, fmt.Errorf("%w: matchId, conflictId, selectedFactIds, operatorId and reason are required", ErrInvalid)
	}
	if conflict.MatchID != "" && strings.TrimSpace(conflict.MatchID) != matchID {
		return FactConflictCommandResult{}, fmt.Errorf("%w: conflict does not belong to match", ErrInvalid)
	}
	events := append([]MatchEvent(nil), input.Events...)
	transition, err := resolveConflictSelection(events, conflict, selectedFactIDs)
	if err != nil {
		return FactConflictCommandResult{}, err
	}
	timestamp := commandTime(input.Now).Format(time.RFC3339Nano)
	reconcile := factIDSet(transition.ReconcileFactIDs)
	revoke := factIDSet(transition.RevokeFactIDs)
	release := factIDSet(transition.ReleaseFactIDs)
	result := FactConflictCommandResult{
		AppliedAt:         timestamp,
		Events:            events,
		ReconciledFactIDs: append([]string(nil), transition.ReconcileFactIDs...),
		SelectedFactIDs:   append([]string(nil), transition.SelectedFactIDs...),
		ConflictResolved:  transition.Resolved,
	}
	for index := range result.Events {
		event := &result.Events[index]
		if event.Status != "active" {
			continue
		}
		changed := false
		if _, selected := reconcile[event.FactID]; selected {
			event.FactStatus = FactStatusReconciled
			event.Confirmed = true
			event.ConfirmedBy = operatorID
			event.PublicAt = timestamp
			changed = true
		} else if _, rejected := revoke[event.FactID]; rejected && event.FactStatus != FactStatusRevoked {
			event.FactStatus = FactStatusRevoked
			event.Confirmed = false
			event.ConfirmedBy = operatorID
			event.PublicAt = ""
			changed = true
		} else if _, released := release[event.FactID]; released && event.FactStatus == FactStatusConflict {
			event.FactStatus = FactStatusProvisional
			event.Confirmed = false
			event.ConfirmedBy = operatorID
			event.PublicAt = ""
			changed = true
		}
		if !changed {
			continue
		}
		event.FactRevision++
		event.UpdatedAt = timestamp
		result.Changed = append(result.Changed, *event)
		if event.FactStatus == FactStatusRevoked {
			result.Retracted = append(result.Retracted, *event)
		}
	}
	resolved := cloneFactConflict(conflict)
	resolved.MatchID = matchID
	resolved.SelectedFactIDs = uniqueFactIDs(append(resolved.SelectedFactIDs, transition.SelectedFactIDs...))
	resolved.Reason = reason
	resolved.ResolvedBy = operatorID
	if transition.Resolved {
		resolved.Status = ConflictStatusResolved
		resolved.ResolvedAt = timestamp
		if len(resolved.SelectedFactIDs) == 1 {
			resolved.ChosenFactID = resolved.SelectedFactIDs[0]
		} else {
			resolved.ChosenFactID = ""
		}
	} else {
		resolved.Members = transition.RemainingMembers
		resolved.Edges = transition.RemainingEdges
	}
	result.Conflict = resolved
	return result, nil
}

func (FactLedgerEngine) Correct(input FactLedgerProjectInput, eventID string, replacement MatchEvent, replacementID string, recordedSequence int64) (FactCommandResult, error) {
	matchID := strings.TrimSpace(input.MatchID)
	eventID = strings.TrimSpace(eventID)
	if matchID == "" || eventID == "" {
		return FactCommandResult{}, fmt.Errorf("%w: matchId and event id are required", ErrInvalid)
	}
	events := append([]MatchEvent(nil), input.Events...)
	found := -1
	for index := range events {
		if events[index].ID == eventID && events[index].Status == "active" {
			found = index
			break
		}
	}
	if found < 0 {
		return FactCommandResult{}, ErrNotFound
	}
	replacement.MatchID = matchID
	replacement.RevisionOf = eventID
	requestedStatus := replacement.FactStatus
	normalize(&replacement)
	if requestedStatus == "" {
		replacement.FactStatus = events[found].FactStatus
		replacement.Confirmed = replacement.FactStatus == FactStatusConfirmed || replacement.FactStatus == FactStatusReconciled
	}
	if err := validate(replacement); err != nil {
		return FactCommandResult{}, err
	}
	projection, err := (FactLedgerEngine{}).Project(input)
	if err != nil {
		return FactCommandResult{}, err
	}
	if err := validateCorrectionTimeline(events, events[found], replacement); err != nil {
		return FactCommandResult{}, err
	}
	config := normalizeConfig(matchID, input.Config)
	if err := validateCorrection(events[found], replacement, projection.Snapshot, config); err != nil {
		return FactCommandResult{}, err
	}
	now := commandTime(input.Now)
	timestamp := now.Format(time.RFC3339Nano)
	events[found].Status = "corrected"
	events[found].UpdatedAt = timestamp
	replacement.ID = strings.TrimSpace(replacementID)
	replacement.RecordedSequence = recordedSequence
	if replacement.FactID == "" {
		replacement.FactID = events[found].FactID
	}
	replacement.FactRevision = events[found].FactRevision + 1
	replacement.CreatedAt = timestamp
	replacement.UpdatedAt = timestamp
	events = append(events, replacement)
	return projectFactCommand(input, events, replacement, nil, nil)
}

func (FactLedgerEngine) Confirm(input FactLedgerProjectInput, factID, operatorID string) (FactCommandResult, error) {
	matchID := strings.TrimSpace(input.MatchID)
	factID = strings.TrimSpace(factID)
	operatorID = strings.TrimSpace(operatorID)
	if matchID == "" || factID == "" || operatorID == "" {
		return FactCommandResult{}, fmt.Errorf("%w: matchId, factId and operatorId are required", ErrInvalid)
	}
	events := append([]MatchEvent(nil), input.Events...)
	found := activeFactIndex(events, factID)
	if found < 0 {
		return FactCommandResult{}, ErrNotFound
	}
	if events[found].FactStatus != FactStatusProvisional {
		return FactCommandResult{}, fmt.Errorf("%w: fact is not provisional", ErrInvalid)
	}
	projection, err := (FactLedgerEngine{}).Project(input)
	if err != nil {
		return FactCommandResult{}, err
	}
	config := normalizeConfig(matchID, input.Config)
	if err := validateAgainstSnapshot(events[found], projection.Snapshot, config); err != nil {
		return FactCommandResult{}, err
	}
	if err := validateSubstitutionLineup(events[found], config, events); err != nil {
		return FactCommandResult{}, err
	}
	timestamp := commandTime(input.Now).Format(time.RFC3339Nano)
	events[found].FactStatus = FactStatusConfirmed
	events[found].Confirmed = true
	events[found].ConfirmedBy = operatorID
	events[found].PublicAt = timestamp
	events[found].FactRevision++
	events[found].UpdatedAt = timestamp
	return projectFactCommand(input, events, events[found], nil, nil)
}

func (FactLedgerEngine) Transition(input FactLedgerProjectInput, factID, operatorID string, status FactStatus) (FactCommandResult, error) {
	matchID := strings.TrimSpace(input.MatchID)
	factID = strings.TrimSpace(factID)
	operatorID = strings.TrimSpace(operatorID)
	if matchID == "" || factID == "" || operatorID == "" {
		return FactCommandResult{}, fmt.Errorf("%w: matchId, factId and operatorId are required", ErrInvalid)
	}
	events := append([]MatchEvent(nil), input.Events...)
	found := activeFactIndex(events, factID)
	if found < 0 {
		return FactCommandResult{}, ErrNotFound
	}
	if err := validateFactTransition(events[found].FactStatus, status); err != nil {
		return FactCommandResult{}, err
	}
	previousStatus := events[found].FactStatus
	timestamp := commandTime(input.Now).Format(time.RFC3339Nano)
	events[found].FactStatus = status
	events[found].Confirmed = status == FactStatusReconciled
	events[found].ConfirmedBy = operatorID
	events[found].PublicAt = ""
	if events[found].Confirmed {
		events[found].PublicAt = timestamp
	}
	events[found].FactRevision++
	events[found].UpdatedAt = timestamp
	var retracted []MatchEvent
	if status == FactStatusReconciled {
		for index := range reconciliationConflictIndices(events, found) {
			events[index].FactStatus = FactStatusRevoked
			events[index].Confirmed = false
			events[index].ConfirmedBy = operatorID
			events[index].PublicAt = ""
			events[index].FactRevision++
			events[index].UpdatedAt = timestamp
			retracted = append(retracted, events[index])
		}
	}
	var integrity *MatchIntegrity
	if (status == FactStatusReconciled || previousStatus == FactStatusConflict) && !hasActiveFactConflict(events) {
		value := MatchIntegrity{Status: "ok"}
		integrity = &value
	}
	return projectFactCommand(input, events, events[found], retracted, integrity)
}

func projectFactCommand(input FactLedgerProjectInput, events []MatchEvent, changed MatchEvent, retracted []MatchEvent, integrity *MatchIntegrity) (FactCommandResult, error) {
	input.Events = events
	if integrity != nil {
		input.Config.Integrity = *integrity
	}
	projection, err := (FactLedgerEngine{}).Project(input)
	if err != nil {
		return FactCommandResult{}, err
	}
	return FactCommandResult{Events: events, Changed: changed, Retracted: retracted, Integrity: integrity, Projection: projection}, nil
}

func activeFactIndex(events []MatchEvent, factID string) int {
	for index := range events {
		if events[index].FactID == factID && events[index].Status == "active" {
			return index
		}
	}
	return -1
}

func commandTime(now time.Time) time.Time {
	if now.IsZero() {
		return time.Now().UTC()
	}
	return now.UTC()
}

func factIDSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

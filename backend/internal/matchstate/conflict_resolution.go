package matchstate

import (
	"fmt"
	"sort"
	"strings"
)

type conflictResolutionTransition struct {
	SelectedFactIDs  []string
	ReconcileFactIDs []string
	RevokeFactIDs    []string
	ReleaseFactIDs   []string
	RemainingMembers []FactConflictMember
	RemainingEdges   []FactConflictEdge
	Resolved         bool
}

func CompatibleSelectionForLegacyChoice(conflict FactConflict, chosenFactID string) ([]string, error) {
	chosenFactID = strings.TrimSpace(chosenFactID)
	members := make(map[string]ConflictMemberRole, len(conflict.Members))
	for _, member := range conflict.Members {
		members[member.FactID] = member.Role
	}
	chosenRole := members[chosenFactID]
	if chosenRole == "" {
		return nil, fmt.Errorf("%w: chosen fact is not a member of the conflict", ErrInvalid)
	}
	selection := []string{chosenFactID}
	edges := normalizedConflictEdges(conflict.Edges, members)
	conflictsWithChosen := make(map[string]struct{})
	for _, edge := range edges {
		if edge.LeftFactID == chosenFactID {
			conflictsWithChosen[edge.RightFactID] = struct{}{}
		}
		if edge.RightFactID == chosenFactID {
			conflictsWithChosen[edge.LeftFactID] = struct{}{}
		}
	}
	for factID, role := range members {
		if role != ConflictMemberAccepted || factID == chosenFactID {
			continue
		}
		if _, incompatible := conflictsWithChosen[factID]; !incompatible {
			selection = append(selection, factID)
		}
	}
	return uniqueFactIDs(selection), nil
}

func resolveConflictSelection(events []MatchEvent, conflict FactConflict, selectedFactIDs []string) (conflictResolutionTransition, error) {
	if conflict.Status != ConflictStatusOpen {
		return conflictResolutionTransition{}, fmt.Errorf("%w: conflict is already resolved", ErrInvalid)
	}
	members := make(map[string]ConflictMemberRole, len(conflict.Members))
	for _, member := range conflict.Members {
		factID := strings.TrimSpace(member.FactID)
		if factID != "" {
			members[factID] = member.Role
		}
	}
	active := make(map[string]MatchEvent, len(events))
	for _, event := range events {
		if event.Status == "active" && members[event.FactID] != "" {
			active[event.FactID] = event
		}
	}
	selected := uniqueFactIDs(selectedFactIDs)
	if len(selected) == 0 {
		return conflictResolutionTransition{}, fmt.Errorf("%w: at least one selected fact is required", ErrInvalid)
	}
	for _, factID := range selected {
		if members[factID] == "" || active[factID].FactID == "" {
			return conflictResolutionTransition{}, fmt.Errorf("%w: selected fact %q is not an active member of the conflict", ErrInvalid, factID)
		}
	}
	edges := normalizedConflictEdges(conflict.Edges, members)
	selectedSet := make(map[string]struct{}, len(selected))
	for _, factID := range selected {
		selectedSet[factID] = struct{}{}
	}
	for _, edge := range edges {
		_, leftSelected := selectedSet[edge.LeftFactID]
		_, rightSelected := selectedSet[edge.RightFactID]
		if leftSelected && rightSelected {
			return conflictResolutionTransition{}, fmt.Errorf("%w: selected facts %q and %q are mutually exclusive", ErrInvalid, edge.LeftFactID, edge.RightFactID)
		}
	}

	revoke := make(map[string]struct{})
	for _, edge := range edges {
		if _, selectedLeft := selectedSet[edge.LeftFactID]; selectedLeft {
			revoke[edge.RightFactID] = struct{}{}
		}
		if _, selectedRight := selectedSet[edge.RightFactID]; selectedRight {
			revoke[edge.LeftFactID] = struct{}{}
		}
	}
	for factID := range revoke {
		delete(selectedSet, factID)
	}
	transition := conflictResolutionTransition{SelectedFactIDs: append([]string(nil), selected...)}
	for _, factID := range selected {
		event := active[factID]
		if event.FactStatus == FactStatusConflict || event.FactStatus == FactStatusProvisional {
			transition.ReconcileFactIDs = append(transition.ReconcileFactIDs, factID)
		}
	}
	for factID := range revoke {
		if members[factID] != ConflictMemberAccepted || !containsFactID(selected, factID) {
			transition.RevokeFactIDs = append(transition.RevokeFactIDs, factID)
		}
	}
	sort.Strings(transition.ReconcileFactIDs)
	sort.Strings(transition.RevokeFactIDs)

	activeAfter := make(map[string]bool, len(active))
	for factID := range active {
		activeAfter[factID] = true
	}
	for _, factID := range transition.RevokeFactIDs {
		activeAfter[factID] = false
	}
	remaining := make([]FactConflictEdge, 0, len(edges))
	for _, edge := range edges {
		if activeAfter[edge.LeftFactID] && activeAfter[edge.RightFactID] {
			remaining = append(remaining, edge)
		}
	}
	transition.RemainingEdges = remaining
	remainingFacts := make(map[string]struct{}, len(remaining)*2)
	for _, edge := range remaining {
		remainingFacts[edge.LeftFactID] = struct{}{}
		remainingFacts[edge.RightFactID] = struct{}{}
	}
	for factID, event := range active {
		if event.FactStatus != FactStatusConflict {
			continue
		}
		if _, selected := selectedSet[factID]; selected {
			continue
		}
		if _, rejected := revoke[factID]; rejected {
			continue
		}
		if _, stillConflicting := remainingFacts[factID]; !stillConflicting {
			transition.ReleaseFactIDs = append(transition.ReleaseFactIDs, factID)
		}
	}
	sort.Strings(transition.ReleaseFactIDs)
	if len(remaining) == 0 {
		transition.Resolved = true
		return transition, nil
	}
	for factID := range remainingFacts {
		role := members[factID]
		if IsPublicFact(active[factID]) {
			role = ConflictMemberAccepted
		} else {
			role = ConflictMemberCandidate
		}
		transition.RemainingMembers = append(transition.RemainingMembers, FactConflictMember{FactID: factID, Role: role})
	}
	sort.Slice(transition.RemainingMembers, func(i, j int) bool {
		return transition.RemainingMembers[i].FactID < transition.RemainingMembers[j].FactID
	})
	return transition, nil
}

func normalizedConflictEdges(edges []FactConflictEdge, members map[string]ConflictMemberRole) []FactConflictEdge {
	seen := make(map[string]struct{}, len(edges))
	normalized := make([]FactConflictEdge, 0, len(edges))
	appendEdge := func(left, right string, reason string) {
		left = strings.TrimSpace(left)
		right = strings.TrimSpace(right)
		if left == "" || right == "" || left == right || members[left] == "" || members[right] == "" {
			return
		}
		if left > right {
			left, right = right, left
		}
		key := left + "\x00" + right
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		normalized = append(normalized, FactConflictEdge{LeftFactID: left, RightFactID: right, Reason: reason})
	}
	for _, edge := range edges {
		appendEdge(edge.LeftFactID, edge.RightFactID, edge.Reason)
	}
	if len(normalized) == 0 {
		var accepted, candidates []string
		for factID, role := range members {
			if role == ConflictMemberAccepted {
				accepted = append(accepted, factID)
			} else {
				candidates = append(candidates, factID)
			}
		}
		for _, acceptedID := range accepted {
			for _, candidateID := range candidates {
				appendEdge(acceptedID, candidateID, "legacy_accepted_candidate")
			}
		}
	}
	sort.Slice(normalized, func(i, j int) bool {
		if normalized[i].LeftFactID == normalized[j].LeftFactID {
			return normalized[i].RightFactID < normalized[j].RightFactID
		}
		return normalized[i].LeftFactID < normalized[j].LeftFactID
	})
	return normalized
}

func uniqueFactIDs(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func containsFactID(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

package evals

import (
	"context"
	"fmt"

	"qiuqiu/internal/relationship"
)

func RunRelationshipJourney(ctx context.Context, director relationship.CompanionDirector, journey RelationshipJourney) RelationshipJourneyResult {
	result := RelationshipJourneyResult{ID: journey.ID}
	for _, step := range journey.Steps {
		signal := step.Signal
		if signal.OccurredAt.IsZero() {
			signal.OccurredAt = step.At
		}
		if signal.ReceivedAt.IsZero() {
			signal.ReceivedAt = step.At
		}
		decision, err := director.Apply(ctx, signal)
		if err != nil {
			result.Failures = append(result.Failures, fmt.Sprintf("%s: apply: %v", step.ID, err))
			continue
		}
		result.Decisions = append(result.Decisions, decision)
		gradeRelationshipJourneyStep(&result, step, decision)
	}
	result.Passed = len(result.Failures) == 0
	return result
}

func gradeRelationshipJourneyStep(result *RelationshipJourneyResult, step RelationshipJourneyStep, decision relationship.Decision) {
	expect := step.Expect
	if expect.Stage != "" && decision.Relationship.Stage != expect.Stage {
		result.Failures = append(result.Failures, fmt.Sprintf("%s: stage got %q, want %q", step.ID, decision.Relationship.Stage, expect.Stage))
	}
	if expect.RepairActive != nil && decision.Relationship.RepairActive != *expect.RepairActive {
		result.Failures = append(result.Failures, fmt.Sprintf("%s: repair active got %t, want %t", step.ID, decision.Relationship.RepairActive, *expect.RepairActive))
	}
	if expect.BoundaryCount != nil && decision.Relationship.BoundaryCount != *expect.BoundaryCount {
		result.Failures = append(result.Failures, fmt.Sprintf("%s: boundary count got %d, want %d", step.ID, decision.Relationship.BoundaryCount, *expect.BoundaryCount))
	}
	for _, action := range expect.RequiredActions {
		if !journeyHasAction(decision.Actions, action) {
			result.Failures = append(result.Failures, fmt.Sprintf("%s: missing action %q", step.ID, action))
		}
	}
	for _, action := range expect.ForbiddenActions {
		if journeyHasAction(decision.Actions, action) {
			result.Failures = append(result.Failures, fmt.Sprintf("%s: forbidden action %q", step.ID, action))
		}
	}
	for _, kind := range expect.MemoryKinds {
		if !journeyHasMemoryKind(decision.Memories, kind) {
			result.Failures = append(result.Failures, fmt.Sprintf("%s: missing memory kind %q", step.ID, kind))
		}
	}
}

func journeyHasAction(actions []relationship.CommunicationAct, wanted relationship.CommunicationAct) bool {
	for _, action := range actions {
		if action == wanted {
			return true
		}
	}
	return false
}

func journeyHasMemoryKind(memories []relationship.RelationshipMemory, wanted string) bool {
	for _, memory := range memories {
		if memory.Kind == wanted {
			return true
		}
	}
	return false
}

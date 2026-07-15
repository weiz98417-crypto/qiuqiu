package relationship

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

type CompanionDirector interface {
	Apply(ctx context.Context, signal Signal) (Decision, error)
}

type Director struct {
	repository StateRepository
}

func NewDirector(repository StateRepository) *Director {
	return &Director{repository: repository}
}

func (d *Director) Apply(ctx context.Context, signal Signal) (Decision, error) {
	if d == nil || d.repository == nil {
		return Decision{}, errors.New("relationship director is not configured")
	}
	if err := validateSignal(signal); err != nil {
		return Decision{}, err
	}
	if decision, ok, err := d.repository.DecisionBySignal(ctx, signal.UserID, signal.MatchID, signal.ID); err != nil {
		return Decision{}, err
	} else if ok {
		return decision, nil
	}

	for attempt := 0; attempt < 3; attempt++ {
		state, err := d.repository.Load(ctx, signal.UserID, signal.MatchID)
		if err != nil {
			return Decision{}, err
		}
		normalizeStateBundle(&state, signal.UserID, signal.MatchID)
		updatedAt := signal.ReceivedAt.UTC()
		if updatedAt.IsZero() {
			updatedAt = signal.OccurredAt.UTC()
		}
		if updatedAt.IsZero() {
			updatedAt = time.Now().UTC()
		}
		actions, reasons := applyPolicy(&state, signal, updatedAt)
		reasons = append(reasons, applyRelationshipMemory(&state, signal, updatedAt)...)
		decisionID := "decision:" + signal.UserID + ":" + signal.MatchID + ":" + signal.ID
		selectedMemories := selectRelationshipMemories(&state, signal, updatedAt, decisionID, 2)
		if signal.FactRevision != "" {
			state.Match.LastFactRevision = signal.FactRevision
		}
		recordActions(&state.Match, signal.ID, actions, updatedAt)
		advanceStage(&state.Relationship)
		state.Relationship.UpdatedAt = updatedAt
		state.Match.UpdatedAt = updatedAt
		state.Match.LastSignalID = signal.ID
		state.Relationship.Version++
		state.Match.Version++
		decision := Decision{
			ID:           decisionID,
			SignalID:     signal.ID,
			StateVersion: state.Match.Version,
			Actions:      actions,
			Relationship: RelationshipView{
				Stage:               state.Relationship.Stage,
				RepairActive:        state.Relationship.Repair.Active,
				RepairCategory:      state.Relationship.Repair.Category,
				BoundaryCount:       len(state.Relationship.Boundaries),
				AllowedBanterScopes: allowedBanterScopes(state.Relationship.Banter),
				GreetingDelivered:   state.Relationship.GreetingDeliveredAt != nil,
				InitiativeMode:      state.Relationship.Preferences.InitiativeMode,
				AnalysisAppetite:    state.Relationship.Preferences.AnalysisAppetite,
			},
			Presentation:  presentationFor(state.Match.Affect, signal, actions),
			Speech:        speechFor(signal, actions, state.Relationship, state.Match),
			Memories:      selectedMemories,
			PlaybackState: state.Match.PlaybackState,
			ReasonCodes:   append([]string{"relationship_decision"}, reasons...),
			CreatedAt:     updatedAt,
		}
		err = d.repository.CompareAndSwap(ctx, ExpectedVersions{
			Relationship: state.Relationship.Version - 1,
			Match:        state.Match.Version - 1,
		}, StateUpdate{Relationship: state.Relationship, Match: state.Match, Memories: state.Memories, Decision: decision})
		if err == nil {
			return decision, nil
		}
		if !errors.Is(err, ErrConcurrentUpdate) {
			return Decision{}, err
		}
		if decision, ok, lookupErr := d.repository.DecisionBySignal(ctx, signal.UserID, signal.MatchID, signal.ID); lookupErr != nil {
			return Decision{}, lookupErr
		} else if ok {
			return decision, nil
		}
	}
	return Decision{}, ErrConcurrentUpdate
}

func validateSignal(signal Signal) error {
	if strings.TrimSpace(signal.ID) == "" {
		return fmt.Errorf("signal id is required")
	}
	if strings.TrimSpace(signal.UserID) == "" {
		return fmt.Errorf("user id is required")
	}
	if strings.TrimSpace(signal.MatchID) == "" {
		return fmt.Errorf("match id is required")
	}
	switch signal.Kind {
	case SignalSessionOpened, SignalUserTurn, SignalMatchEvent, SignalDeliveryResult:
		return nil
	default:
		return fmt.Errorf("unsupported signal kind %q", signal.Kind)
	}
}

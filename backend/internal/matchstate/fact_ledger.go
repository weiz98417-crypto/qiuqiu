package matchstate

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
)

type FactLedgerEngine struct{}

type FactLedgerProjectInput struct {
	MatchID string
	Events  []MatchEvent
	Config  MatchConfig
	Clock   MatchClock
	Now     time.Time
}

type FactLedgerProjection struct {
	Snapshot     Snapshot
	PublicEvents []MatchEvent
}

type FactProjectionDifference struct {
	Field     string
	Legacy    any
	Projected any
}

type FactProjectionAudit struct {
	MatchID        string
	Mismatches     []string
	Differences    []FactProjectionDifference
	LegacyScore    Score
	ProjectedScore Score
	Error          string
}

type projectedGoalState string

const (
	projectedGoalPending      projectedGoalState = "pending"
	projectedGoalActive       projectedGoalState = "active"
	projectedGoalCancelled    projectedGoalState = "cancelled"
	projectedGoalCheckpointed projectedGoalState = "checkpointed"
	projectedGoalRevoked      projectedGoalState = "revoked"
	projectedGoalUnavailable  projectedGoalState = "unavailable"
)

type projectedGoal struct {
	teamID string
	state  projectedGoalState
}

func (FactLedgerEngine) Project(input FactLedgerProjectInput) (FactLedgerProjection, error) {
	matchID := strings.TrimSpace(input.MatchID)
	if matchID == "" {
		return FactLedgerProjection{}, fmt.Errorf("%w: matchId is required", ErrInvalid)
	}
	events, err := orderFactEvents(input.Events)
	if err != nil {
		return FactLedgerProjection{}, err
	}
	now := input.Now
	config := normalizeConfig(matchID, input.Config)
	clock := normalizeMatchClock(matchID, input.Clock)
	snapshot := Snapshot{
		MatchID: matchID, HomeTeam: config.HomeTeam, AwayTeam: config.AwayTeam,
		Period: clock.Period, Clock: clock.displayAt(now), MatchClock: clock,
		Momentum: "neutral", EmotionalTemperature: 1,
		LastUpdatedAt: now.UTC().Format(time.RFC3339Nano), Integrity: config.Integrity,
	}
	goals := make(map[string]projectedGoal)
	goalAliases := make(map[string]string)
	for _, event := range events {
		if event.EventType != "goal" {
			continue
		}
		canonicalID := strings.TrimSpace(event.FactID)
		if canonicalID == "" {
			canonicalID = strings.TrimSpace(event.ID)
		}
		if canonicalID == "" {
			return FactLedgerProjection{}, fmt.Errorf("%w: goal requires an id or factId", ErrInvalid)
		}
		goal := goals[canonicalID]
		if goal.state == "" {
			goal.state = projectedGoalUnavailable
		}
		if event.FactStatus == FactStatusRevoked {
			goal.state = projectedGoalRevoked
		}
		if IsPublicFact(event) {
			if goal.state == projectedGoalPending {
				return FactLedgerProjection{}, fmt.Errorf("%w: goal fact %q has multiple active public revisions", ErrInvalid, canonicalID)
			}
			goal.teamID = event.TeamID
			goal.state = projectedGoalPending
		}
		goals[canonicalID] = goal
		for _, alias := range []string{event.ID, event.FactID} {
			alias = strings.TrimSpace(alias)
			if alias == "" {
				continue
			}
			if existing, ok := goalAliases[alias]; ok && existing != canonicalID {
				return FactLedgerProjection{}, fmt.Errorf("%w: goal reference %q is ambiguous", ErrInvalid, alias)
			}
			goalAliases[alias] = canonicalID
		}
	}

	projection := FactLedgerProjection{}
	cancelledGoals := make(map[string]struct{})
	for _, event := range events {
		if !IsPublicFact(event) {
			continue
		}
		projected := event
		switch event.EventType {
		case "goal":
			canonicalID := goalAliases[event.ID]
			if canonicalID == "" {
				canonicalID = goalAliases[event.FactID]
			}
			goal := goals[canonicalID]
			if goal.state != projectedGoalPending {
				return FactLedgerProjection{}, fmt.Errorf("%w: goal %q appears more than once in the replay order", ErrInvalid, canonicalID)
			}
			if err := changeTeamScore(&snapshot.Score, event.TeamID, 1); err != nil {
				return FactLedgerProjection{}, err
			}
			goal.teamID = event.TeamID
			goal.state = projectedGoalActive
			goals[canonicalID] = goal
		case "goal_cancelled":
			reference := strings.TrimSpace(event.RevisionOf)
			canonicalID, ok := goalAliases[reference]
			if !ok {
				return FactLedgerProjection{}, fmt.Errorf("%w: goal cancellation references an unknown public goal", ErrInvalid)
			}
			if _, duplicate := cancelledGoals[canonicalID]; duplicate {
				return FactLedgerProjection{}, fmt.Errorf("%w: goal %q is cancelled more than once", ErrInvalid, reference)
			}
			goal := goals[canonicalID]
			switch goal.state {
			case projectedGoalActive:
				if err := changeTeamScore(&snapshot.Score, goal.teamID, -1); err != nil {
					return FactLedgerProjection{}, err
				}
				goal.state = projectedGoalCancelled
				goals[canonicalID] = goal
			case projectedGoalRevoked:
			case projectedGoalUnavailable:
				return FactLedgerProjection{}, fmt.Errorf("%w: goal cancellation references a goal that was never public", ErrInvalid)
			case projectedGoalCheckpointed:
				return FactLedgerProjection{}, fmt.Errorf("%w: goal %q is earlier than the latest score correction", ErrInvalid, reference)
			case projectedGoalPending:
				return FactLedgerProjection{}, fmt.Errorf("%w: goal cancellation appears before goal %q", ErrInvalid, reference)
			default:
				return FactLedgerProjection{}, fmt.Errorf("%w: goal %q cannot be cancelled from state %q", ErrInvalid, reference, goal.state)
			}
			cancelledGoals[canonicalID] = struct{}{}
		case "score_correction":
			if event.Score.Home < 0 || event.Score.Away < 0 {
				return FactLedgerProjection{}, fmt.Errorf("%w: score correction cannot produce a negative score", ErrInvalid)
			}
			snapshot.Score = event.Score
			for canonicalID, goal := range goals {
				if goal.state == projectedGoalActive {
					goal.state = projectedGoalCheckpointed
					goals[canonicalID] = goal
				}
			}
		}
		if snapshot.Score.Home < 0 || snapshot.Score.Away < 0 {
			return FactLedgerProjection{}, fmt.Errorf("%w: fact projection produced a negative score", ErrInvalid)
		}
		projected.Score = snapshot.Score
		if projected.TeamName != "" {
			switch projected.TeamID {
			case "home":
				snapshot.HomeTeam = projected.TeamName
			case "away":
				snapshot.AwayTeam = projected.TeamName
			}
		}
		snapshot.EmotionalTemperature = projected.Intensity
		snapshot.LastRecommendedAction = projected.RecommendedAction
		snapshot.LastPublicDescription = projected.Description
		if projected.UpdatedAt != "" {
			snapshot.LastUpdatedAt = projected.UpdatedAt
		}
		projection.PublicEvents = append([]MatchEvent{projected}, projection.PublicEvents...)
		snapshot.RecentEvents = append([]MatchEvent{projected}, snapshot.RecentEvents...)
		if isKeyEvent(projected.EventType) {
			snapshot.KeyEvents = append([]MatchEvent{projected}, snapshot.KeyEvents...)
		}
		snapshot.Momentum = momentumFor(projected)
	}
	if !clock.UpdatedAt.IsZero() && clock.UpdatedAt.Format(time.RFC3339Nano) > snapshot.LastUpdatedAt {
		snapshot.LastUpdatedAt = clock.UpdatedAt.Format(time.RFC3339Nano)
	}
	if len(snapshot.RecentEvents) > 5 {
		snapshot.RecentEvents = snapshot.RecentEvents[:5]
	}
	if len(snapshot.KeyEvents) > 8 {
		snapshot.KeyEvents = snapshot.KeyEvents[:8]
	}
	projection.Snapshot = snapshot
	return projection, nil
}

func orderFactEvents(events []MatchEvent) ([]MatchEvent, error) {
	ordered := append([]MatchEvent(nil), events...)
	sequenced := 0
	seen := make(map[int64]struct{}, len(events))
	for _, event := range events {
		if event.RecordedSequence <= 0 {
			continue
		}
		sequenced++
		if _, duplicate := seen[event.RecordedSequence]; duplicate {
			return nil, fmt.Errorf("%w: recordedSequence %d is duplicated", ErrInvalid, event.RecordedSequence)
		}
		seen[event.RecordedSequence] = struct{}{}
	}
	if sequenced != 0 && sequenced != len(events) {
		return nil, fmt.Errorf("%w: recordedSequence must be present on the complete history", ErrInvalid)
	}
	if sequenced == len(events) {
		sort.SliceStable(ordered, func(left, right int) bool {
			return ordered[left].RecordedSequence < ordered[right].RecordedSequence
		})
	}
	return ordered, nil
}

func auditFactProjection(input FactLedgerProjectInput, legacy Snapshot, legacyPublicEvents []MatchEvent, observer func(FactProjectionAudit)) {
	if observer == nil {
		return
	}
	projection, err := (FactLedgerEngine{}).Project(input)
	audit := FactProjectionAudit{
		MatchID: input.MatchID, LegacyScore: legacy.Score,
	}
	if err != nil {
		audit.Error = err.Error()
		addProjectionDifference(&audit, "projection_error", nil, audit.Error)
		observer(audit)
		return
	}
	audit.ProjectedScore = projection.Snapshot.Score
	if legacy.Score != projection.Snapshot.Score {
		addProjectionDifference(&audit, "score", legacy.Score, projection.Snapshot.Score)
	}
	if !reflect.DeepEqual(legacy.RecentEvents, projection.Snapshot.RecentEvents) {
		addProjectionDifference(&audit, "recent_events", legacy.RecentEvents, projection.Snapshot.RecentEvents)
	}
	if !reflect.DeepEqual(legacy.KeyEvents, projection.Snapshot.KeyEvents) {
		addProjectionDifference(&audit, "key_events", legacy.KeyEvents, projection.Snapshot.KeyEvents)
	}
	if legacy.HomeTeam != projection.Snapshot.HomeTeam || legacy.AwayTeam != projection.Snapshot.AwayTeam {
		addProjectionDifference(
			&audit,
			"teams",
			map[string]string{"home": legacy.HomeTeam, "away": legacy.AwayTeam},
			map[string]string{"home": projection.Snapshot.HomeTeam, "away": projection.Snapshot.AwayTeam},
		)
	}
	if !reflect.DeepEqual(legacyPublicEvents, projection.PublicEvents) {
		addProjectionDifference(&audit, "public_events", legacyPublicEvents, projection.PublicEvents)
	}
	if len(audit.Mismatches) > 0 {
		observer(audit)
	}
}

func addProjectionDifference(audit *FactProjectionAudit, field string, legacy, projected any) {
	audit.Mismatches = append(audit.Mismatches, field)
	audit.Differences = append(audit.Differences, FactProjectionDifference{
		Field: field, Legacy: legacy, Projected: projected,
	})
}

func newestFirstPublicFacts(events []MatchEvent) []MatchEvent {
	public := filterPublicFacts(events)
	for left, right := 0, len(public)-1; left < right; left, right = left+1, right-1 {
		public[left], public[right] = public[right], public[left]
	}
	return public
}

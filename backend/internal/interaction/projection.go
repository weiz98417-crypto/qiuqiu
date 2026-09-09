package interaction

import (
	"sort"

	"qiuqiu/internal/relationship"
)

type Journey struct {
	UserID          string         `json:"userId"`
	MatchID         string         `json:"matchId"`
	MatchIDs        []string       `json:"matchIds,omitempty"`
	TurnCount       int            `json:"turnCount"`
	DeliveryCount   int            `json:"deliveryCount"`
	FactIDs         []string       `json:"factIds,omitempty"`
	DeliveryByState map[string]int `json:"deliveryByState"`
	TraceIDs        []string       `json:"traceIds,omitempty"`
	DecisionIDs     []string       `json:"decisionIds,omitempty"`
}

type TurnView struct {
	TraceID         string                         `json:"traceId"`
	SignalID        string                         `json:"signalId,omitempty"`
	InputText       string                         `json:"inputText,omitempty"`
	OutputText      string                         `json:"outputText,omitempty"`
	DecisionID      string                         `json:"decisionId,omitempty"`
	Decision        *relationship.Decision         `json:"decision,omitempty"`
	Presentation    *relationship.PresentationPlan `json:"presentation,omitempty"`
	FactIDs         []string                       `json:"factIds,omitempty"`
	FactRevision    string                         `json:"factRevision,omitempty"`
	DeliveryKey     string                         `json:"deliveryKey,omitempty"`
	MediaType       string                         `json:"mediaType,omitempty"`
	DeliveryStates  []string                       `json:"deliveryStates,omitempty"`
	DeliveryReasons []string                       `json:"deliveryReasons,omitempty"`
	PlaybackStates  []string                       `json:"playbackStates,omitempty"`
	Stale           bool                           `json:"stale,omitempty"`
}

func ProjectTurns(events []Event) []TurnView {
	inputs := make(map[string]string)
	for _, event := range events {
		if event.Kind == KindSignal && event.SignalID != "" && event.InputText != "" {
			inputs[event.SignalID] = event.InputText
		}
	}
	byTrace := make(map[string]*TurnView)
	order := make([]string, 0)
	for _, event := range events {
		if event.TraceID == "" {
			continue
		}
		turn := byTrace[event.TraceID]
		if turn == nil {
			turn = &TurnView{TraceID: event.TraceID}
			byTrace[event.TraceID] = turn
			order = append(order, event.TraceID)
		}
		if event.SignalID != "" {
			if input, ok := inputs[event.SignalID]; ok {
				turn.SignalID = event.SignalID
				turn.InputText = input
			} else if turn.SignalID == "" && event.Kind == KindTurnPlanned {
				turn.SignalID = event.SignalID
			}
		}
		if event.OutputText != "" {
			turn.OutputText = event.OutputText
		}
		if event.DecisionID != "" {
			turn.DecisionID = event.DecisionID
		}
		if event.Decision != nil {
			decision := *event.Decision
			turn.Decision = &decision
		}
		if event.Presentation != nil {
			presentation := *event.Presentation
			turn.Presentation = &presentation
		}
		if len(event.FactIDs) > 0 {
			turn.FactIDs = append([]string(nil), event.FactIDs...)
		}
		if event.FactRevision != "" {
			turn.FactRevision = event.FactRevision
		}
		if event.DeliveryState != "" {
			turn.DeliveryStates = append(turn.DeliveryStates, event.DeliveryState)
		}
		if event.DeliveryReason != "" {
			turn.DeliveryReasons = append(turn.DeliveryReasons, event.DeliveryReason)
		}
		if event.DeliveryKey != "" {
			turn.DeliveryKey = event.DeliveryKey
		}
		if event.MediaType != "" {
			turn.MediaType = event.MediaType
		}
		if event.PlaybackState != "" {
			turn.PlaybackStates = append(turn.PlaybackStates, event.PlaybackState)
		}
		if event.Kind == KindTurnStale || event.Stale {
			turn.Stale = true
		}
	}
	result := make([]TurnView, 0, len(order))
	for _, traceID := range order {
		turn := *byTrace[traceID]
		sort.SliceStable(turn.PlaybackStates, func(left, right int) bool {
			return playbackStateRank(turn.PlaybackStates[left]) < playbackStateRank(turn.PlaybackStates[right])
		})
		result = append(result, turn)
	}
	return result
}

func playbackStateRank(state string) int {
	switch state {
	case "started":
		return 1
	case "ended", "completed", "interrupted", "skipped", "blocked":
		return 2
	default:
		return 3
	}
}

// ProjectJourney rebuilds the longitudinal evaluation view from immutable events.
func ProjectJourney(events []Event) Journey {
	journey := Journey{DeliveryByState: make(map[string]int)}
	facts := make(map[string]struct{})
	traces := make(map[string]struct{})
	decisions := make(map[string]struct{})
	matches := make(map[string]struct{})
	for _, event := range events {
		if journey.UserID == "" {
			journey.UserID = event.UserID
		}
		if event.MatchID != "" {
			matches[event.MatchID] = struct{}{}
		}
		switch event.Kind {
		case KindTurnPlanned:
			journey.TurnCount++
		case KindDelivery, KindMediaDelivery, KindPlaybackResult:
			journey.DeliveryCount++
			state := event.DeliveryState
			if state == "" {
				state = event.PlaybackState
			}
			if state != "" {
				journey.DeliveryByState[state]++
			}
		}
		for _, factID := range event.FactIDs {
			facts[factID] = struct{}{}
		}
		if event.TraceID != "" {
			traces[event.TraceID] = struct{}{}
		}
		if event.DecisionID != "" {
			decisions[event.DecisionID] = struct{}{}
		}
	}
	for id := range facts {
		journey.FactIDs = append(journey.FactIDs, id)
	}
	for id := range traces {
		journey.TraceIDs = append(journey.TraceIDs, id)
	}
	for id := range decisions {
		journey.DecisionIDs = append(journey.DecisionIDs, id)
	}
	for id := range matches {
		journey.MatchIDs = append(journey.MatchIDs, id)
	}
	if len(journey.MatchIDs) == 1 {
		journey.MatchID = journey.MatchIDs[0]
	}
	sort.Strings(journey.FactIDs)
	sort.Strings(journey.TraceIDs)
	sort.Strings(journey.DecisionIDs)
	sort.Strings(journey.MatchIDs)
	return journey
}

package relationship

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

var ErrConcurrentUpdate = errors.New("relationship state changed concurrently")

type StateRepository interface {
	Load(ctx context.Context, userID, matchID string) (StateBundle, error)
	CompareAndSwap(ctx context.Context, expected ExpectedVersions, update StateUpdate) error
	DecisionBySignal(ctx context.Context, userID, matchID, signalID string) (Decision, bool, error)
	RefreshDecision(ctx context.Context, userID, matchID, signalID string, update Decision) (Decision, bool, error)
}

type MatchResetter interface {
	ResetMatch(matchID string) error
}

type MemoryRepository struct {
	mu            sync.Mutex
	relationships map[string]RelationshipState
	matches       map[string]MatchCompanionState
	decisions     map[string]Decision
	decisionMatch map[string]string
	memories      map[string]RelationshipMemory
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		relationships: make(map[string]RelationshipState),
		matches:       make(map[string]MatchCompanionState),
		decisions:     make(map[string]Decision),
		decisionMatch: make(map[string]string),
		memories:      make(map[string]RelationshipMemory),
	}
}

func (r *MemoryRepository) Load(_ context.Context, userID, matchID string) (StateBundle, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.loadLocked(userID, matchID), nil
}

func (r *MemoryRepository) CompareAndSwap(_ context.Context, expected ExpectedVersions, update StateUpdate) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	current := r.loadLocked(update.Relationship.UserID, update.Match.MatchID)
	if current.Relationship.Version != expected.Relationship || current.Match.Version != expected.Match {
		return ErrConcurrentUpdate
	}
	key := decisionKey(update.Relationship.UserID, update.Match.MatchID, update.Decision.SignalID)
	if existing, ok := r.decisions[key]; ok {
		if existing.ID == update.Decision.ID {
			return nil
		}
		return ErrConcurrentUpdate
	}
	r.relationships[update.Relationship.UserID] = update.Relationship
	r.matches[matchKey(update.Match.UserID, update.Match.MatchID)] = update.Match
	for _, memory := range update.Memories {
		r.memories[memory.ID] = cloneRelationshipMemory(memory)
	}
	r.decisions[key] = cloneDecision(update.Decision)
	r.decisionMatch[key] = update.Match.MatchID
	return nil
}

func (r *MemoryRepository) DecisionBySignal(_ context.Context, userID, matchID, signalID string) (Decision, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	decision, ok := r.decisions[decisionKey(userID, matchID, signalID)]
	return cloneDecision(decision), ok, nil
}

func (r *MemoryRepository) RefreshDecision(_ context.Context, userID, matchID, signalID string, update Decision) (Decision, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := decisionKey(userID, matchID, signalID)
	decision, ok := r.decisions[key]
	if !ok {
		return Decision{}, false, nil
	}
	if decision.RefreshCount >= 1 {
		return cloneDecision(decision), true, nil
	}
	update.ID, update.SignalID = decision.ID, decision.SignalID
	r.decisions[key] = cloneDecision(update)
	return cloneDecision(update), true, nil
}

func (r *MemoryRepository) ResetMatch(matchID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, state := range r.matches {
		if state.MatchID == matchID {
			delete(r.matches, key)
		}
	}
	for key, storedMatchID := range r.decisionMatch {
		if storedMatchID == matchID {
			delete(r.decisionMatch, key)
			delete(r.decisions, key)
		}
	}
	return nil
}

func (r *MemoryRepository) loadLocked(userID, matchID string) StateBundle {
	relationshipState, ok := r.relationships[userID]
	if !ok {
		relationshipState = RelationshipState{UserID: userID, Stage: StageFirstMeeting}
	}
	matchState, ok := r.matches[matchKey(userID, matchID)]
	if !ok {
		matchState = MatchCompanionState{UserID: userID, MatchID: matchID}
	}
	relationshipState.Boundaries = append([]UserBoundary(nil), relationshipState.Boundaries...)
	relationshipState.Repair.BehaviorChanges = append([]string(nil), relationshipState.Repair.BehaviorChanges...)
	relationshipState.Evidence.SharedMatches = cloneTimes(relationshipState.Evidence.SharedMatches)
	relationshipState.Banter = clonePermissions(relationshipState.Banter)
	relationshipState.Preferences.AllowedAddressing = append([]string(nil), relationshipState.Preferences.AllowedAddressing...)
	relationshipState.Taste = cloneTaste(relationshipState.Taste)
	matchState.RecentActions = cloneActionRecords(matchState.RecentActions)
	matchState.RecentPhraseHashes = append([]uint64(nil), matchState.RecentPhraseHashes...)
	matchState.OpenThreads = append([]OpenThread(nil), matchState.OpenThreads...)
	memories := make([]RelationshipMemory, 0)
	for _, memory := range r.memories {
		if memory.UserID == userID {
			memories = append(memories, cloneRelationshipMemory(memory))
		}
	}
	sort.Slice(memories, func(left, right int) bool {
		return memories[left].CreatedAt.After(memories[right].CreatedAt)
	})
	return StateBundle{Relationship: relationshipState, Match: matchState, Memories: memories}
}

func matchKey(userID, matchID string) string {
	return userID + "\x00" + matchID
}

func decisionKey(userID, matchID, signalID string) string {
	return userID + "\x00" + matchID + "\x00" + signalID
}

func cloneTimes(values map[string]time.Time) map[string]time.Time {
	if values == nil {
		return nil
	}
	cloned := make(map[string]time.Time, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func clonePermissions(values map[string]Permission) map[string]Permission {
	if values == nil {
		return nil
	}
	cloned := make(map[string]Permission, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func cloneTaste(value TasteState) TasteState {
	value.StylePreferences = cloneTastePreferences(value.StylePreferences)
	value.PlayerArchetypes = cloneTastePreferences(value.PlayerArchetypes)
	return value
}

func cloneTastePreferences(values []TastePreference) []TastePreference {
	cloned := append([]TastePreference(nil), values...)
	for index := range cloned {
		cloned[index].EvidenceRefs = append([]string(nil), cloned[index].EvidenceRefs...)
	}
	return cloned
}

func cloneActionRecords(values []ActionRecord) []ActionRecord {
	cloned := append([]ActionRecord(nil), values...)
	for index := range cloned {
		cloned[index].Actions = append([]CommunicationAct(nil), cloned[index].Actions...)
	}
	return cloned
}

func cloneDecision(value Decision) Decision {
	value.Actions = append([]CommunicationAct(nil), value.Actions...)
	value.ReasonCodes = append([]string(nil), value.ReasonCodes...)
	value.Memories = cloneRelationshipMemories(value.Memories)
	value.UsedMemoryIDs = append([]string(nil), value.UsedMemoryIDs...)
	if value.Speech != nil {
		speech := *value.Speech
		speech.Actions = append([]CommunicationAct(nil), value.Speech.Actions...)
		speech.Content.RequiredAnchors = append([]string(nil), value.Speech.Content.RequiredAnchors...)
		speech.Content.ForbiddenClaims = append([]string(nil), value.Speech.Content.ForbiddenClaims...)
		speech.Content.ForbiddenTopics = append([]string(nil), value.Speech.Content.ForbiddenTopics...)
		speech.Content.RecentPhraseHashes = append([]uint64(nil), value.Speech.Content.RecentPhraseHashes...)
		value.Speech = &speech
	}
	return value
}

func cloneRelationshipMemories(values []RelationshipMemory) []RelationshipMemory {
	cloned := make([]RelationshipMemory, len(values))
	for index, value := range values {
		cloned[index] = cloneRelationshipMemory(value)
	}
	return cloned
}

func cloneRelationshipMemory(value RelationshipMemory) RelationshipMemory {
	value.Payload = append([]byte(nil), value.Payload...)
	value.PendingDecisionIDs = append([]string(nil), value.PendingDecisionIDs...)
	return value
}

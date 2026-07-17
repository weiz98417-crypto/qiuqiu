package observation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"qiuqiu/internal/matchstate"
)

type Status string

const (
	StatusPendingSync   Status = "pending_sync"
	StatusCorroborating Status = "corroborating"
	StatusConfirmed     Status = "confirmed"
	StatusContradicted  Status = "contradicted"
	StatusConflict      Status = "conflict"
	StatusExpired       Status = "expired"
	StatusSuperseded    Status = "superseded"
)

type Input struct {
	SignalID        string
	TraceID         string
	UserID          string
	MatchID         string
	Kind            string
	EventType       string
	ClaimedTeam     string
	ClaimedPlayer   string
	ClaimedScore    *matchstate.Score
	Certainty       string
	ReceivedAt      time.Time
	ReconcileWindow time.Duration
}

type PendingObservation struct {
	ID               string            `json:"id"`
	SignalID         string            `json:"signalId"`
	TraceID          string            `json:"traceId"`
	UserID           string            `json:"userId"`
	MatchID          string            `json:"matchId"`
	Kind             string            `json:"kind"`
	EventType        string            `json:"eventType,omitempty"`
	ClaimedTeam      string            `json:"claimedTeam,omitempty"`
	ClaimedPlayer    string            `json:"claimedPlayer,omitempty"`
	ClaimedScore     *matchstate.Score `json:"claimedScore,omitempty"`
	Certainty        string            `json:"certainty,omitempty"`
	Status           Status            `json:"status"`
	CandidateFactID  string            `json:"candidateFactId,omitempty"`
	ResolvedFactID   string            `json:"resolvedFactId,omitempty"`
	ResolvedRevision int               `json:"resolvedRevision,omitempty"`
	ReceivedAt       time.Time         `json:"receivedAt"`
	FollowUpDeadline time.Time         `json:"followUpDeadline"`
	ReconcileUntil   time.Time         `json:"reconcileUntil"`
	ResolvedAt       *time.Time        `json:"resolvedAt,omitempty"`
	ResolutionReason string            `json:"resolutionReason,omitempty"`
}

type Resolution struct {
	ObservationID    string    `json:"observationId"`
	UserID           string    `json:"userId"`
	MatchID          string    `json:"matchId"`
	Status           Status    `json:"status"`
	FactID           string    `json:"factId,omitempty"`
	FactRevision     int       `json:"factRevision,omitempty"`
	ReliableText     string    `json:"reliableText,omitempty"`
	DeliveryKey      string    `json:"deliveryKey"`
	FollowUpDeadline time.Time `json:"followUpDeadline"`
}

type Coordinator interface {
	Record(context.Context, Input) (PendingObservation, error)
	OnFactChanged(context.Context, matchstate.MatchEvent) ([]Resolution, error)
	Expire(context.Context, time.Time) ([]Resolution, error)
}

type ResolutionRecovery interface {
	PendingResolutions(context.Context, string, string, time.Time) ([]Resolution, error)
	MarkResolutionDelivered(context.Context, string, time.Time) error
}

type ResolutionSuppressor interface {
	SuppressFollowUp(context.Context, string, time.Time) error
}

type MatchResetter interface {
	ResetMatch(context.Context, string) error
}

type MemoryCoordinator struct {
	mu         sync.Mutex
	byScope    map[string]PendingObservation
	deliveries map[string]Resolution
}

func NewMemoryCoordinator() *MemoryCoordinator {
	return &MemoryCoordinator{byScope: make(map[string]PendingObservation), deliveries: make(map[string]Resolution)}
}

func (c *MemoryCoordinator) Record(ctx context.Context, input Input) (PendingObservation, error) {
	if err := ctx.Err(); err != nil {
		return PendingObservation{}, err
	}
	input.SignalID = strings.TrimSpace(input.SignalID)
	input.UserID = strings.TrimSpace(input.UserID)
	input.MatchID = strings.TrimSpace(input.MatchID)
	if input.SignalID == "" || input.UserID == "" || input.MatchID == "" {
		return PendingObservation{}, fmt.Errorf("signalId, userId and matchId are required")
	}
	if input.ReceivedAt.IsZero() {
		input.ReceivedAt = time.Now().UTC()
	}
	scope := input.UserID + "\x00" + input.MatchID + "\x00" + input.SignalID
	c.mu.Lock()
	defer c.mu.Unlock()
	if existing, ok := c.byScope[scope]; ok {
		return cloneObservation(existing), nil
	}
	for _, existing := range c.byScope {
		if sameActiveObservation(existing, input) {
			return cloneObservation(existing), nil
		}
	}
	activeCount := 0
	oldestScope := ""
	var oldest PendingObservation
	for candidateScope, existing := range c.byScope {
		if existing.UserID != input.UserID || existing.MatchID != input.MatchID || !isActiveStatus(existing.Status) {
			continue
		}
		activeCount++
		if oldestScope == "" || existing.ReceivedAt.Before(oldest.ReceivedAt) {
			oldestScope = candidateScope
			oldest = existing
		}
	}
	if activeCount >= 5 && oldestScope != "" {
		resolvedAt := input.ReceivedAt.UTC()
		oldest.Status = StatusSuperseded
		oldest.ResolvedAt = &resolvedAt
		oldest.ResolutionReason = "pending observation limit exceeded"
		c.byScope[oldestScope] = oldest
	}
	followUpWindow, reconcileWindow := windowsForInput(input)
	observation := PendingObservation{
		ID:               observationID(scope),
		SignalID:         input.SignalID,
		TraceID:          strings.TrimSpace(input.TraceID),
		UserID:           input.UserID,
		MatchID:          input.MatchID,
		Kind:             strings.TrimSpace(input.Kind),
		EventType:        strings.TrimSpace(input.EventType),
		ClaimedTeam:      strings.TrimSpace(input.ClaimedTeam),
		ClaimedPlayer:    strings.TrimSpace(input.ClaimedPlayer),
		ClaimedScore:     cloneScore(input.ClaimedScore),
		Certainty:        strings.TrimSpace(input.Certainty),
		Status:           StatusPendingSync,
		ReceivedAt:       input.ReceivedAt.UTC(),
		FollowUpDeadline: input.ReceivedAt.UTC().Add(followUpWindow),
		ReconcileUntil:   input.ReceivedAt.UTC().Add(reconcileWindow),
	}
	c.byScope[scope] = observation
	return cloneObservation(observation), nil
}

func (c *MemoryCoordinator) OnFactChanged(ctx context.Context, event matchstate.MatchEvent) ([]Resolution, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if event.FactStatus != matchstate.FactStatusProvisional &&
		event.FactStatus != matchstate.FactStatusConfirmed &&
		event.FactStatus != matchstate.FactStatusReconciled &&
		event.FactStatus != matchstate.FactStatusRevoked {
		return nil, nil
	}
	factID := strings.TrimSpace(event.FactID)
	if factID == "" {
		factID = strings.TrimSpace(event.ID)
	}
	eventAt := matchEventTime(event)
	c.mu.Lock()
	defer c.mu.Unlock()
	var resolutions []Resolution
	for scope, pending := range c.byScope {
		updated, changed, resolution, resolved := applyFact(pending, event, eventAt, factID)
		if changed {
			c.byScope[scope] = updated
		}
		if resolved {
			if strings.TrimSpace(resolution.ReliableText) != "" {
				c.deliveries[resolution.DeliveryKey] = resolution
			}
			resolutions = append(resolutions, resolution)
		}
	}
	return resolutions, nil
}

func (c *MemoryCoordinator) PendingResolutions(ctx context.Context, userID, matchID string, now time.Time) ([]Resolution, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var resolutions []Resolution
	for key, resolution := range c.deliveries {
		if now.After(resolution.FollowUpDeadline) {
			delete(c.deliveries, key)
			continue
		}
		if resolution.UserID == strings.TrimSpace(userID) && resolution.MatchID == strings.TrimSpace(matchID) {
			resolutions = append(resolutions, resolution)
		}
	}
	return resolutions, nil
}

func (c *MemoryCoordinator) MarkResolutionDelivered(ctx context.Context, deliveryKey string, deliveredAt time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	delete(c.deliveries, strings.TrimSpace(deliveryKey))
	c.mu.Unlock()
	return nil
}

func (c *MemoryCoordinator) SuppressFollowUp(ctx context.Context, observationID string, suppressedAt time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	observationID = strings.TrimSpace(observationID)
	if observationID == "" {
		return nil
	}
	if suppressedAt.IsZero() {
		suppressedAt = time.Now().UTC()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for scope, pending := range c.byScope {
		if pending.ID != observationID {
			continue
		}
		if pending.FollowUpDeadline.After(suppressedAt) {
			pending.FollowUpDeadline = suppressedAt.UTC()
			c.byScope[scope] = pending
		}
		break
	}
	for key, resolution := range c.deliveries {
		if resolution.ObservationID == observationID {
			delete(c.deliveries, key)
		}
	}
	return nil
}

func (c *MemoryCoordinator) ResetMatch(ctx context.Context, matchID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	matchID = strings.TrimSpace(matchID)
	c.mu.Lock()
	defer c.mu.Unlock()
	for scope, pending := range c.byScope {
		if pending.MatchID == matchID {
			delete(c.byScope, scope)
		}
	}
	for deliveryKey, resolution := range c.deliveries {
		if resolution.MatchID == matchID {
			delete(c.deliveries, deliveryKey)
		}
	}
	return nil
}

func (c *MemoryCoordinator) Get(id string) (PendingObservation, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, pending := range c.byScope {
		if pending.ID == id {
			return cloneObservation(pending), true
		}
	}
	return PendingObservation{}, false
}

func (c *MemoryCoordinator) Expire(ctx context.Context, now time.Time) ([]Resolution, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var resolutions []Resolution
	for scope, pending := range c.byScope {
		if pending.Status != StatusPendingSync && pending.Status != StatusCorroborating && pending.Status != StatusConflict {
			continue
		}
		if now.Before(pending.ReconcileUntil) {
			continue
		}
		resolvedAt := now.UTC()
		pending.Status = StatusExpired
		pending.ResolvedAt = &resolvedAt
		pending.ResolutionReason = "reconciliation window elapsed"
		c.byScope[scope] = pending
		resolutions = append(resolutions, resolutionFor(pending, matchstate.MatchEvent{}))
	}
	return resolutions, nil
}

func windowsFor(eventType string) (time.Duration, time.Duration) {
	switch strings.TrimSpace(eventType) {
	case "var_result", "goal_cancelled":
		return 45 * time.Second, 2 * time.Minute
	case "shot", "save", "yellow_card", "substitution":
		return 10 * time.Second, 45 * time.Second
	default:
		return 15 * time.Second, time.Minute
	}
}

func windowsForInput(input Input) (time.Duration, time.Duration) {
	followUpWindow, reconcileWindow := windowsFor(input.EventType)
	if input.ReconcileWindow > reconcileWindow {
		reconcileWindow = input.ReconcileWindow
	}
	return followUpWindow, reconcileWindow
}

func DefaultReconcileWindow(eventType string) time.Duration {
	_, reconcileWindow := windowsFor(eventType)
	return reconcileWindow
}

func matches(pending PendingObservation, event matchstate.MatchEvent, eventAt time.Time) bool {
	if !matchesScopeAndIdentity(pending, event, eventAt) || !eventTypeCompatible(pending, event) {
		return false
	}
	if pending.ClaimedScore != nil && *pending.ClaimedScore != event.Score {
		return false
	}
	return true
}

func matchesScopeAndIdentity(pending PendingObservation, event matchstate.MatchEvent, eventAt time.Time) bool {
	if pending.MatchID != strings.TrimSpace(event.MatchID) {
		return false
	}
	if pending.Status != StatusPendingSync && pending.Status != StatusCorroborating {
		return false
	}
	if !eventAt.IsZero() && eventAt.After(pending.ReconcileUntil) {
		return false
	}
	if pending.ClaimedPlayer != "" && !strings.EqualFold(pending.ClaimedPlayer, strings.TrimSpace(event.PlayerName)) {
		return false
	}
	if pending.ClaimedTeam != "" && !strings.EqualFold(pending.ClaimedTeam, strings.TrimSpace(event.TeamName)) {
		return false
	}
	return true
}

func eventTypeCompatible(pending PendingObservation, event matchstate.MatchEvent) bool {
	pendingType := strings.TrimSpace(pending.EventType)
	eventType := strings.TrimSpace(event.EventType)
	switch pendingType {
	case "":
		return pending.Kind == "score" && pending.ClaimedScore != nil
	case "play":
		return isOnBallPlay(eventType)
	default:
		return pendingType == eventType
	}
}

func isOnBallPlay(eventType string) bool {
	switch strings.TrimSpace(eventType) {
	case "goal", "big_chance", "shot", "save", "miss":
		return true
	default:
		return false
	}
}

func matchEventTime(event matchstate.MatchEvent) time.Time {
	for _, value := range []string{event.UpdatedAt, event.CreatedAt} {
		if parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value)); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func repeatedResolution(pending PendingObservation, event matchstate.MatchEvent, factID string) (Resolution, bool) {
	if pending.MatchID != strings.TrimSpace(event.MatchID) {
		return Resolution{}, false
	}
	expected := StatusConfirmed
	if event.FactStatus == matchstate.FactStatusRevoked {
		expected = StatusContradicted
	}
	if pending.Status != expected {
		return Resolution{}, false
	}
	if pending.ResolvedFactID != factID || pending.ResolvedRevision != event.FactRevision {
		return Resolution{}, false
	}
	return resolutionFor(pending, event), true
}

func resolutionFor(pending PendingObservation, event matchstate.MatchEvent) Resolution {
	return Resolution{
		ObservationID:    pending.ID,
		UserID:           pending.UserID,
		MatchID:          pending.MatchID,
		Status:           pending.Status,
		FactID:           pending.ResolvedFactID,
		FactRevision:     pending.ResolvedRevision,
		ReliableText:     reliableText(pending, event),
		DeliveryKey:      fmt.Sprintf("%s:%d:%s", pending.ID, pending.ResolvedRevision, pending.Status),
		FollowUpDeadline: pending.FollowUpDeadline,
	}
}

func reliableText(pending PendingObservation, event matchstate.MatchEvent) string {
	if pending.Status == StatusConfirmed {
		switch strings.TrimSpace(event.EventType) {
		case "shot":
			return "跟上了，你说的是刚才那脚射门。那一下确实有东西。"
		case "save":
			return "跟上了，刚才那次扑救确实漂亮。"
		case "miss":
			return "跟上了，你说的是刚才那次机会。可惜没进。"
		case "big_chance":
			return "跟上了，刚才那次机会确实够精彩。"
		}
		if player := strings.TrimSpace(event.PlayerName); player != "" {
			return "跟上了，确实是" + player + "进的。"
		}
		if team := strings.TrimSpace(event.TeamName); team != "" {
			return "跟上了，确实是" + team + "进了。"
		}
		return "跟上了，这球确实算进了。"
	}
	if pending.Status == StatusContradicted {
		return "结果出来了，这球没算。刚才那一下是真把人骗到了。"
	}
	return ""
}

func matchesGoalContradiction(pending PendingObservation, event matchstate.MatchEvent, eventAt time.Time) bool {
	if strings.TrimSpace(pending.EventType) != "goal" ||
		(event.FactStatus != matchstate.FactStatusConfirmed && event.FactStatus != matchstate.FactStatusReconciled) {
		return false
	}
	if pending.ClaimedPlayer == "" && pending.ClaimedTeam == "" && pending.ClaimedScore == nil {
		return false
	}
	switch strings.TrimSpace(event.EventType) {
	case "shot", "save", "miss", "goal_cancelled":
		return matchesScopeAndIdentity(pending, event, eventAt)
	default:
		return false
	}
}

func matchesRevocation(pending PendingObservation, event matchstate.MatchEvent, eventAt time.Time, factID string) bool {
	if pending.MatchID != strings.TrimSpace(event.MatchID) || (!eventAt.IsZero() && eventAt.After(pending.ReconcileUntil)) {
		return false
	}
	if pending.ResolvedFactID == factID || pending.CandidateFactID == factID {
		return pending.Status == StatusConfirmed || pending.Status == StatusCorroborating
	}
	return matches(pending, event, eventAt)
}

func applyFact(pending PendingObservation, event matchstate.MatchEvent, eventAt time.Time, factID string) (PendingObservation, bool, Resolution, bool) {
	if resolution, ok := repeatedResolution(pending, event, factID); ok {
		return pending, false, resolution, true
	}
	if event.FactStatus == matchstate.FactStatusRevoked {
		if !matchesRevocation(pending, event, eventAt, factID) {
			return pending, false, Resolution{}, false
		}
		resolvedAt := eventAt
		if resolvedAt.IsZero() {
			resolvedAt = time.Now().UTC()
		}
		pending.Status = StatusContradicted
		pending.ResolvedFactID = factID
		pending.ResolvedRevision = event.FactRevision
		pending.ResolvedAt = &resolvedAt
		pending.ResolutionReason = "matched revoked fact"
		correctionDeadline := resolvedAt.Add(45 * time.Second)
		if pending.FollowUpDeadline.Before(correctionDeadline) {
			pending.FollowUpDeadline = correctionDeadline
		}
		return pending, true, resolutionFor(pending, event), true
	}
	if matchesGoalContradiction(pending, event, eventAt) {
		resolvedAt := eventAt
		if resolvedAt.IsZero() {
			resolvedAt = time.Now().UTC()
		}
		pending.Status = StatusContradicted
		pending.ResolvedFactID = factID
		pending.ResolvedRevision = event.FactRevision
		pending.ResolvedAt = &resolvedAt
		pending.ResolutionReason = "matched confirmed non-goal fact"
		return pending, true, resolutionFor(pending, event), true
	}
	if !matches(pending, event, eventAt) {
		return pending, false, Resolution{}, false
	}
	if event.FactStatus == matchstate.FactStatusProvisional {
		if pending.Status == StatusCorroborating && pending.CandidateFactID != "" && pending.CandidateFactID != factID {
			pending.Status = StatusConflict
			pending.ResolutionReason = "multiple matching candidate facts"
			return pending, true, Resolution{}, false
		}
		pending.Status = StatusCorroborating
		pending.CandidateFactID = factID
		return pending, true, Resolution{}, false
	}
	resolvedAt := eventAt
	if resolvedAt.IsZero() {
		resolvedAt = time.Now().UTC()
	}
	pending.Status = StatusConfirmed
	pending.CandidateFactID = factID
	pending.ResolvedFactID = factID
	pending.ResolvedRevision = event.FactRevision
	pending.ResolvedAt = &resolvedAt
	pending.ResolutionReason = "matched public fact"
	return pending, true, resolutionFor(pending, event), true
}

func observationID(scope string) string {
	sum := sha256.Sum256([]byte(scope))
	return "obs_" + hex.EncodeToString(sum[:12])
}

func cloneObservation(value PendingObservation) PendingObservation {
	value.ClaimedScore = cloneScore(value.ClaimedScore)
	return value
}

func cloneScore(value *matchstate.Score) *matchstate.Score {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func sameActiveObservation(existing PendingObservation, input Input) bool {
	if existing.UserID != input.UserID || existing.MatchID != input.MatchID {
		return false
	}
	if existing.Status != StatusPendingSync && existing.Status != StatusCorroborating && existing.Status != StatusConflict {
		return false
	}
	if !input.ReceivedAt.Before(existing.ReconcileUntil) {
		return false
	}
	if existing.Kind != strings.TrimSpace(input.Kind) || existing.EventType != strings.TrimSpace(input.EventType) ||
		existing.ClaimedTeam != strings.TrimSpace(input.ClaimedTeam) || existing.ClaimedPlayer != strings.TrimSpace(input.ClaimedPlayer) {
		return false
	}
	return scoresEqual(existing.ClaimedScore, input.ClaimedScore)
}

func isActiveStatus(status Status) bool {
	return status == StatusPendingSync || status == StatusCorroborating || status == StatusConflict
}

func scoresEqual(left, right *matchstate.Score) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

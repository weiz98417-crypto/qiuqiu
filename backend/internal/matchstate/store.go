package matchstate

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrNotFound = errors.New("not found")
	ErrInvalid  = errors.New("invalid match event")
)

type Score struct {
	Home int `json:"home"`
	Away int `json:"away"`
}

type MatchConfig struct {
	MatchID     string   `json:"matchId"`
	HomeTeam    string   `json:"homeTeam"`
	AwayTeam    string   `json:"awayTeam"`
	Competition string   `json:"competition,omitempty"`
	Kickoff     string   `json:"kickoff,omitempty"`
	HomePlayers []Player `json:"homePlayers,omitempty"`
	AwayPlayers []Player `json:"awayPlayers,omitempty"`
	UpdatedAt   string   `json:"updatedAt"`
}

type Player struct {
	Name     string `json:"name"`
	Number   string `json:"number,omitempty"`
	Position string `json:"position,omitempty"`
}

type Participant struct {
	Role     string `json:"role"`
	Name     string `json:"name"`
	TeamID   string `json:"teamId,omitempty"`
	TeamName string `json:"teamName,omitempty"`
}

type MatchEvent struct {
	ID                string        `json:"id"`
	MatchID           string        `json:"matchId"`
	Source            string        `json:"source"`
	ProviderName      string        `json:"providerName,omitempty"`
	OperatorID        string        `json:"operatorId,omitempty"`
	Period            string        `json:"period"`
	Clock             string        `json:"clock"`
	EventType         string        `json:"eventType"`
	TeamID            string        `json:"teamId,omitempty"`
	TeamName          string        `json:"teamName,omitempty"`
	PlayerName        string        `json:"playerName,omitempty"`
	Participants      []Participant `json:"participants,omitempty"`
	Score             Score         `json:"score"`
	Intensity         int           `json:"intensity"`
	Sentiment         string        `json:"sentiment,omitempty"`
	Description       string        `json:"description"`
	ProactiveText     string        `json:"proactiveText,omitempty"`
	Tags              []string      `json:"tags,omitempty"`
	RecommendedAction string        `json:"recommendedAction,omitempty"`
	Visibility        string        `json:"visibility"`
	CreatedAt         string        `json:"createdAt"`
	UpdatedAt         string        `json:"updatedAt"`
	RevisionOf        string        `json:"revisionOf,omitempty"`
	Status            string        `json:"status"`
}

type Snapshot struct {
	MatchID               string       `json:"matchId"`
	HomeTeam              string       `json:"homeTeam"`
	AwayTeam              string       `json:"awayTeam"`
	Score                 Score        `json:"score"`
	Period                string       `json:"period"`
	Clock                 string       `json:"clock"`
	Momentum              string       `json:"momentum"`
	EmotionalTemperature  int          `json:"emotionalTemperature"`
	RecentEvents          []MatchEvent `json:"recentEvents"`
	KeyEvents             []MatchEvent `json:"keyEvents"`
	LastUpdatedAt         string       `json:"lastUpdatedAt"`
	LastRecommendedAction string       `json:"lastRecommendedAction,omitempty"`
	LastPublicDescription string       `json:"lastPublicDescription,omitempty"`
}

type Repository interface {
	SetConfig(matchID string, config MatchConfig) (MatchConfig, Snapshot, error)
	Config(matchID string) MatchConfig
	Reset(matchID string) error
	Create(matchID string, ev MatchEvent) (MatchEvent, Snapshot, error)
	Correct(matchID, eventID string, replacement MatchEvent) (MatchEvent, Snapshot, error)
	Events(matchID string) []MatchEvent
	Snapshot(matchID string) Snapshot
	Subscribe(matchID string) (<-chan MatchEvent, func())
}

type Store struct {
	mu          sync.RWMutex
	events      map[string][]MatchEvent
	configs     map[string]MatchConfig
	subscribers map[string]map[chan MatchEvent]struct{}
	nextID      int64
}

func NewStore() *Store {
	return &Store{
		events:      make(map[string][]MatchEvent),
		configs:     make(map[string]MatchConfig),
		subscribers: make(map[string]map[chan MatchEvent]struct{}),
	}
}

func (s *Store) SetConfig(matchID string, config MatchConfig) (MatchConfig, Snapshot, error) {
	matchID = strings.TrimSpace(matchID)
	if matchID == "" {
		return MatchConfig{}, Snapshot{}, fmt.Errorf("%w: matchId is required", ErrInvalid)
	}
	config.MatchID = matchID
	config.HomeTeam = defaultString(config.HomeTeam, "主队")
	config.AwayTeam = defaultString(config.AwayTeam, "客队")
	config.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)

	s.mu.Lock()
	s.configs[matchID] = config
	snapshot := buildSnapshot(matchID, s.events[matchID], config)
	s.mu.Unlock()

	return config, snapshot, nil
}

func (s *Store) Config(matchID string) MatchConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return normalizeConfig(matchID, s.configs[matchID])
}

func (s *Store) Reset(matchID string) error {
	matchID = strings.TrimSpace(matchID)
	if matchID == "" {
		return fmt.Errorf("%w: matchId is required", ErrInvalid)
	}
	s.mu.Lock()
	delete(s.events, matchID)
	delete(s.configs, matchID)
	s.mu.Unlock()
	return nil
}

func (s *Store) Create(matchID string, ev MatchEvent) (MatchEvent, Snapshot, error) {
	matchID = strings.TrimSpace(matchID)
	if matchID == "" {
		return MatchEvent{}, Snapshot{}, fmt.Errorf("%w: matchId is required", ErrInvalid)
	}
	ev.MatchID = matchID
	normalize(&ev)
	if err := validate(ev); err != nil {
		return MatchEvent{}, Snapshot{}, err
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	s.mu.Lock()
	s.nextID++
	ev.ID = fmt.Sprintf("evt_%d", s.nextID)
	ev.CreatedAt = now
	ev.UpdatedAt = now
	s.events[matchID] = append(s.events[matchID], ev)
	snapshot := buildSnapshot(matchID, s.events[matchID], s.configs[matchID])
	subs := s.subscriberListLocked(matchID)
	s.mu.Unlock()

	s.publish(subs, ev)
	return ev, snapshot, nil
}

func (s *Store) Correct(matchID, eventID string, replacement MatchEvent) (MatchEvent, Snapshot, error) {
	matchID = strings.TrimSpace(matchID)
	eventID = strings.TrimSpace(eventID)
	if matchID == "" || eventID == "" {
		return MatchEvent{}, Snapshot{}, fmt.Errorf("%w: matchId and event id are required", ErrInvalid)
	}

	s.mu.Lock()
	events := s.events[matchID]
	found := -1
	for i := range events {
		if events[i].ID == eventID && events[i].Status == "active" {
			found = i
			break
		}
	}
	if found == -1 {
		s.mu.Unlock()
		return MatchEvent{}, Snapshot{}, ErrNotFound
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	events[found].Status = "corrected"
	events[found].UpdatedAt = now
	s.nextID++
	replacement.ID = fmt.Sprintf("evt_%d", s.nextID)
	replacement.MatchID = matchID
	replacement.RevisionOf = eventID
	replacement.CreatedAt = now
	replacement.UpdatedAt = now
	normalize(&replacement)
	if err := validate(replacement); err != nil {
		s.mu.Unlock()
		return MatchEvent{}, Snapshot{}, err
	}
	events = append(events, replacement)
	s.events[matchID] = events
	snapshot := buildSnapshot(matchID, events, s.configs[matchID])
	subs := s.subscriberListLocked(matchID)
	s.mu.Unlock()

	s.publish(subs, replacement)
	return replacement, snapshot, nil
}

func (s *Store) Events(matchID string) []MatchEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	events := append([]MatchEvent(nil), s.events[matchID]...)
	sort.SliceStable(events, func(i, j int) bool {
		return events[i].CreatedAt > events[j].CreatedAt
	})
	return events
}

func (s *Store) Snapshot(matchID string) Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return buildSnapshot(matchID, s.events[matchID], s.configs[matchID])
}

func (s *Store) Subscribe(matchID string) (<-chan MatchEvent, func()) {
	ch := make(chan MatchEvent, 16)
	s.mu.Lock()
	if s.subscribers[matchID] == nil {
		s.subscribers[matchID] = make(map[chan MatchEvent]struct{})
	}
	s.subscribers[matchID][ch] = struct{}{}
	s.mu.Unlock()

	unsubscribe := func() {
		s.mu.Lock()
		if subs := s.subscribers[matchID]; subs != nil {
			delete(subs, ch)
			if len(subs) == 0 {
				delete(s.subscribers, matchID)
			}
		}
		close(ch)
		s.mu.Unlock()
	}
	return ch, unsubscribe
}

func (s *Store) subscriberListLocked(matchID string) []chan MatchEvent {
	var subs []chan MatchEvent
	for ch := range s.subscribers[matchID] {
		subs = append(subs, ch)
	}
	return subs
}

func (s *Store) publish(subs []chan MatchEvent, ev MatchEvent) {
	for _, ch := range subs {
		select {
		case ch <- ev:
		default:
		}
	}
}

func normalize(ev *MatchEvent) {
	ev.Source = defaultString(ev.Source, "operator")
	ev.Period = defaultString(ev.Period, "first_half")
	ev.Visibility = defaultString(ev.Visibility, "public")
	ev.Status = defaultString(ev.Status, "active")
	ev.EventType = strings.TrimSpace(ev.EventType)
	ev.Clock = strings.TrimSpace(ev.Clock)
	ev.Description = strings.TrimSpace(ev.Description)
	ev.PlayerName = strings.TrimSpace(ev.PlayerName)
	ev.Participants = normalizeParticipants(ev.Participants)
	if len(ev.Participants) == 0 && ev.PlayerName != "" {
		ev.Participants = []Participant{{
			Role:     DefaultParticipantRole(ev.EventType),
			Name:     ev.PlayerName,
			TeamID:   ev.TeamID,
			TeamName: ev.TeamName,
		}}
	}
	if ev.PlayerName == "" && len(ev.Participants) > 0 {
		ev.PlayerName = ev.Participants[0].Name
	}
	if ev.Intensity < 1 {
		ev.Intensity = 3
	}
	if ev.Intensity > 5 {
		ev.Intensity = 5
	}
	if ev.RecommendedAction == "" {
		ev.RecommendedAction = DefaultAction(ev.EventType)
	}
	if ev.Sentiment == "" {
		ev.Sentiment = DefaultSentiment(ev.EventType)
	}
}

func normalizeParticipants(participants []Participant) []Participant {
	out := make([]Participant, 0, len(participants))
	for _, participant := range participants {
		participant.Role = strings.TrimSpace(participant.Role)
		participant.Name = strings.TrimSpace(participant.Name)
		participant.TeamID = strings.TrimSpace(participant.TeamID)
		participant.TeamName = strings.TrimSpace(participant.TeamName)
		if participant.Name == "" {
			continue
		}
		if participant.Role == "" {
			participant.Role = "player"
		}
		out = append(out, participant)
	}
	return out
}

func validate(ev MatchEvent) error {
	if ev.EventType == "" {
		return fmt.Errorf("%w: eventType is required", ErrInvalid)
	}
	if ev.Clock == "" {
		return fmt.Errorf("%w: clock is required", ErrInvalid)
	}
	if ev.Description == "" {
		return fmt.Errorf("%w: description is required", ErrInvalid)
	}
	if !allowedEventTypes[ev.EventType] {
		return fmt.Errorf("%w: unsupported eventType %q", ErrInvalid, ev.EventType)
	}
	return nil
}

func buildSnapshot(matchID string, events []MatchEvent, config MatchConfig) Snapshot {
	config = normalizeConfig(matchID, config)
	snap := Snapshot{
		MatchID:              matchID,
		HomeTeam:             config.HomeTeam,
		AwayTeam:             config.AwayTeam,
		Score:                Score{},
		Period:               "pre_match",
		Clock:                "00:00",
		Momentum:             "neutral",
		EmotionalTemperature: 1,
		LastUpdatedAt:        time.Now().UTC().Format(time.RFC3339Nano),
	}

	for _, ev := range events {
		if ev.Status != "active" {
			continue
		}
		if ev.TeamName != "" {
			switch ev.TeamID {
			case "home":
				snap.HomeTeam = ev.TeamName
			case "away":
				snap.AwayTeam = ev.TeamName
			}
		}
		snap.Score = ev.Score
		snap.Period = ev.Period
		snap.Clock = ev.Clock
		snap.EmotionalTemperature = ev.Intensity
		snap.LastRecommendedAction = ev.RecommendedAction
		snap.LastPublicDescription = ev.Description
		snap.LastUpdatedAt = ev.UpdatedAt
		snap.RecentEvents = append([]MatchEvent{ev}, snap.RecentEvents...)
		if isKeyEvent(ev.EventType) {
			snap.KeyEvents = append([]MatchEvent{ev}, snap.KeyEvents...)
		}
		snap.Momentum = momentumFor(ev)
	}

	if len(snap.RecentEvents) > 5 {
		snap.RecentEvents = snap.RecentEvents[:5]
	}
	if len(snap.KeyEvents) > 8 {
		snap.KeyEvents = snap.KeyEvents[:8]
	}
	return snap
}

func normalizeConfig(matchID string, config MatchConfig) MatchConfig {
	config.MatchID = defaultString(config.MatchID, matchID)
	config.HomeTeam = defaultString(config.HomeTeam, "主队")
	config.AwayTeam = defaultString(config.AwayTeam, "客队")
	config.HomePlayers = normalizePlayers(config.HomePlayers)
	config.AwayPlayers = normalizePlayers(config.AwayPlayers)
	return config
}

func normalizePlayers(players []Player) []Player {
	out := make([]Player, 0, len(players))
	for _, player := range players {
		player.Name = strings.TrimSpace(player.Name)
		player.Number = strings.TrimSpace(player.Number)
		player.Position = strings.TrimSpace(player.Position)
		if player.Name == "" {
			continue
		}
		out = append(out, player)
	}
	return out
}

func isKeyEvent(t string) bool {
	switch t {
	case "goal", "red_card", "penalty", "var_check", "halftime", "fulltime":
		return true
	default:
		return false
	}
}

func momentumFor(ev MatchEvent) string {
	switch ev.EventType {
	case "goal", "big_chance", "shot", "pressure":
		if ev.TeamID != "" {
			return ev.TeamID + "_pressure"
		}
		return "pressure"
	case "halftime", "fulltime":
		return "settled"
	default:
		return "neutral"
	}
}

func DefaultAction(eventType string) string {
	switch eventType {
	case "goal":
		return "celebrate"
	case "big_chance", "penalty", "var_check":
		return "tense"
	case "save":
		return "surprise"
	case "miss":
		return "miss"
	case "foul", "yellow_card":
		return "complain"
	case "red_card":
		return "angry"
	case "tactical_shift", "halftime":
		return "analysis"
	case "pressure":
		return "focus"
	case "fulltime":
		return "comfort"
	case "kickoff":
		return "wave"
	default:
		return "listen"
	}
}

func DefaultParticipantRole(eventType string) string {
	switch eventType {
	case "goal":
		return "scorer"
	case "shot", "miss":
		return "shooter"
	case "save":
		return "keeper"
	case "foul", "yellow_card", "red_card":
		return "offender"
	case "penalty":
		return "taker"
	case "substitution":
		return "sub_on"
	case "injury":
		return "injured"
	default:
		return "player"
	}
}

func DefaultSentiment(eventType string) string {
	switch eventType {
	case "goal":
		return "celebratory"
	case "big_chance", "penalty", "var_check", "pressure":
		return "tense"
	case "miss":
		return "regret"
	case "foul", "yellow_card", "red_card":
		return "complaint"
	case "tactical_shift", "halftime":
		return "analytical"
	default:
		return "neutral"
	}
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

var allowedEventTypes = map[string]bool{
	"kickoff":        true,
	"goal":           true,
	"shot":           true,
	"big_chance":     true,
	"save":           true,
	"miss":           true,
	"foul":           true,
	"yellow_card":    true,
	"red_card":       true,
	"var_check":      true,
	"penalty":        true,
	"substitution":   true,
	"injury":         true,
	"tactical_shift": true,
	"pressure":       true,
	"halftime":       true,
	"fulltime":       true,
	"operator_note":  true,
}

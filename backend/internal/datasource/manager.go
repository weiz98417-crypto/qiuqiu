package datasource

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"qiuqiu/internal/event"
	"qiuqiu/internal/matchstate"
)

type SourceType string

const (
	SourceManual    SourceType = "manual"
	SourceReplay    SourceType = "replay"
	SourceAPISports SourceType = "api-sports"
)

type SourceState string

const (
	StateReady        SourceState = "ready"
	StateStandby      SourceState = "standby"
	StateRunning      SourceState = "running"
	StateStopped      SourceState = "stopped"
	StateError        SourceState = "error"
	StateUnconfigured SourceState = "unconfigured"
)

type EventsClient interface {
	GetEvents(fixtureID int) ([]Event, error)
}

type sourceCursorStore interface {
	SourceCursor(matchID, sourceType, sourceKey string) (int64, error)
	SetSourceCursor(matchID, sourceType, sourceKey string, cursor int64) error
}

type ManagerConfig struct {
	PollInterval time.Duration
}

type SourceConfig struct {
	Type          SourceType `json:"type"`
	FixtureID     int        `json:"fixtureId,omitempty"`
	ImportHistory bool       `json:"importHistory,omitempty"`
}

type SourceStatus struct {
	Type        SourceType  `json:"type"`
	State       SourceState `json:"state"`
	FixtureID   int         `json:"fixtureId,omitempty"`
	StartedAt   string      `json:"startedAt,omitempty"`
	LastPollAt  string      `json:"lastPollAt,omitempty"`
	LastEventAt string      `json:"lastEventAt,omitempty"`
	LatencyMS   int64       `json:"latencyMs,omitempty"`
	Error       string      `json:"error,omitempty"`
	Reconnects  int         `json:"reconnects,omitempty"`
}

type MatchSourceStatus struct {
	MatchID      string                      `json:"matchId"`
	ActiveSource SourceType                  `json:"activeSource"`
	Sources      map[SourceType]SourceStatus `json:"sources"`
}

type Manager struct {
	ctx       context.Context
	cancel    context.CancelFunc
	store     matchstate.Repository
	client    EventsClient
	config    ManagerConfig
	controlMu sync.Mutex
	mu        sync.RWMutex
	active    map[string]SourceType
	runs      map[string]*sourceRun
	wg        sync.WaitGroup
}

type sourceRun struct {
	cancel context.CancelFunc
	status SourceStatus
	wait   sync.WaitGroup
}

func NewManager(parent context.Context, store matchstate.Repository, client EventsClient, config ManagerConfig) *Manager {
	ctx, cancel := context.WithCancel(parent)
	if config.PollInterval <= 0 {
		config.PollInterval = 3 * time.Second
	}
	return &Manager{
		ctx:    ctx,
		cancel: cancel,
		store:  store,
		client: client,
		config: config,
		active: make(map[string]SourceType),
		runs:   make(map[string]*sourceRun),
	}
}

func (m *Manager) Status(matchID string) MatchSourceStatus {
	m.mu.RLock()
	active := m.active[matchID]
	m.mu.RUnlock()
	if active == "" {
		active = SourceManual
	}
	apiState := StateStandby
	if m.client == nil {
		apiState = StateUnconfigured
	}
	status := MatchSourceStatus{
		MatchID:      matchID,
		ActiveSource: active,
		Sources: map[SourceType]SourceStatus{
			SourceManual:    {Type: SourceManual, State: StateReady},
			SourceReplay:    {Type: SourceReplay, State: StateReady},
			SourceAPISports: {Type: SourceAPISports, State: apiState},
		},
	}
	m.mu.RLock()
	if run := m.runs[matchID]; run != nil {
		status.Sources[SourceAPISports] = run.status
	}
	m.mu.RUnlock()
	return status
}

func (m *Manager) Start(matchID string, config SourceConfig) (MatchSourceStatus, error) {
	matchID = strings.TrimSpace(matchID)
	if matchID == "" {
		return MatchSourceStatus{}, fmt.Errorf("matchId is required")
	}
	if config.Type == "" {
		config.Type = SourceManual
	}
	if config.Type != SourceManual && config.Type != SourceReplay && config.Type != SourceAPISports {
		return MatchSourceStatus{}, fmt.Errorf("unsupported source %q", config.Type)
	}

	if config.Type == SourceAPISports {
		if m.client == nil {
			return MatchSourceStatus{}, fmt.Errorf("api-sports is not configured")
		}
		if config.FixtureID <= 0 {
			return MatchSourceStatus{}, fmt.Errorf("fixtureId is required for api-sports")
		}
	}
	sourceKey := strconv.Itoa(config.FixtureID)
	sourceCursor := int64(0)
	var commitCursor func(int64) error
	if config.Type == SourceAPISports {
		if cursorStore, ok := m.store.(sourceCursorStore); ok {
			cursor, err := cursorStore.SourceCursor(matchID, string(SourceAPISports), sourceKey)
			if err != nil {
				return MatchSourceStatus{}, fmt.Errorf("load source cursor: %w", err)
			}
			sourceCursor = cursor
			commitCursor = func(cursor int64) error {
				return cursorStore.SetSourceCursor(matchID, string(SourceAPISports), sourceKey, cursor)
			}
		}
	}

	m.controlMu.Lock()
	defer m.controlMu.Unlock()
	m.stopAndWait(matchID)
	m.mu.Lock()
	if config.Type == SourceManual || config.Type == SourceReplay {
		m.active[matchID] = config.Type
		m.mu.Unlock()
		return m.Status(matchID), nil
	}
	runCtx, runCancel := context.WithCancel(m.ctx)
	run := &sourceRun{
		cancel: runCancel,
		status: SourceStatus{
			Type:      SourceAPISports,
			State:     StateRunning,
			FixtureID: config.FixtureID,
			StartedAt: time.Now().UTC().Format(time.RFC3339Nano),
		},
	}
	m.runs[matchID] = run
	m.active[matchID] = SourceAPISports
	m.mu.Unlock()

	events := make(chan PollDelivery, 32)
	reports := make(chan PollReport, 8)
	snapshot := m.store.Snapshot(matchID)
	poller := NewPoller(m.client, int64(config.FixtureID), events).
		WithInterval(m.config.PollInterval).
		WithInitialEvents(config.ImportHistory).
		WithReports(reports).
		WithCursor(sourceCursor).
		WithCursorCommit(commitCursor)
	poller.SetScore(snapshot.Score.Home, snapshot.Score.Away)
	m.wg.Add(2)
	run.wait.Add(2)
	go func() {
		defer m.wg.Done()
		defer run.wait.Done()
		poller.Run(runCtx)
	}()
	go func() {
		defer m.wg.Done()
		defer run.wait.Done()
		m.consumeAPISports(runCtx, matchID, config.FixtureID, events, reports)
	}()

	return m.Status(matchID), nil
}

func (m *Manager) Stop(matchID string) MatchSourceStatus {
	m.controlMu.Lock()
	m.stopAndWait(matchID)
	m.controlMu.Unlock()
	return m.Status(matchID)
}

func (m *Manager) Ingest(ctx context.Context, matchID string, matchEvent matchstate.MatchEvent) (matchstate.MatchEvent, matchstate.Snapshot, error) {
	select {
	case <-ctx.Done():
		return matchstate.MatchEvent{}, matchstate.Snapshot{}, ctx.Err()
	default:
	}
	if repository, ok := m.store.(matchstate.OperatorTransactionRepository); ok {
		return repository.CreateOperator(ctx, matchID, matchEvent)
	}
	return m.store.Create(matchID, matchEvent)
}

func (m *Manager) stopAndWait(matchID string) {
	m.mu.Lock()
	run := m.runs[matchID]
	delete(m.runs, matchID)
	m.active[matchID] = SourceManual
	if run != nil {
		run.cancel()
	}
	m.mu.Unlock()
	if run != nil {
		run.wait.Wait()
	}
}

func (m *Manager) consumeAPISports(ctx context.Context, matchID string, fixtureID int, events <-chan PollDelivery, reports <-chan PollReport) {
	for {
		select {
		case <-ctx.Done():
			return
		case report := <-reports:
			m.updatePollStatus(matchID, report)
		case delivery := <-events:
			standardEvent := delivery.Event
			if standardEvent == nil {
				delivery.Acknowledge(false)
				continue
			}
			matchEvent := apiSportsMatchEvent(matchID, fixtureID, standardEvent, m.store.Snapshot(matchID))
			_, _, err := m.Ingest(ctx, matchID, matchEvent)
			persisted := err == nil || errors.Is(err, matchstate.ErrDuplicate) || errors.Is(err, matchstate.ErrConflict)
			delivery.Acknowledge(persisted)
			if !persisted {
				m.updateSourceError(matchID, err)
				continue
			}
			m.mu.Lock()
			if run := m.runs[matchID]; run != nil {
				run.status.LastEventAt = time.Now().UTC().Format(time.RFC3339Nano)
				run.status.State = StateRunning
				run.status.Error = ""
			}
			m.mu.Unlock()
		}
	}
}

func (m *Manager) updatePollStatus(matchID string, report PollReport) {
	m.mu.Lock()
	defer m.mu.Unlock()
	run := m.runs[matchID]
	if run == nil {
		return
	}
	run.status.LastPollAt = report.At.UTC().Format(time.RFC3339Nano)
	run.status.LatencyMS = report.Latency.Milliseconds()
	if report.Err != nil {
		run.status.State = StateError
		run.status.Error = report.Err.Error()
		run.status.Reconnects++
		return
	}
	run.status.State = StateRunning
	run.status.Error = ""
}

func (m *Manager) updateSourceError(matchID string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if run := m.runs[matchID]; run != nil {
		run.status.State = StateError
		run.status.Error = err.Error()
	}
}

func apiSportsMatchEvent(matchID string, fixtureID int, source *event.StandardEvent, snapshot matchstate.Snapshot) matchstate.MatchEvent {
	eventType := source.Type
	switch eventType {
	case "match_start":
		eventType = "kickoff"
	case "match_end":
		eventType = "fulltime"
	}
	teamID := ""
	if strings.EqualFold(source.Team, snapshot.HomeTeam) || source.Team == "home" {
		teamID = "home"
	} else if strings.EqualFold(source.Team, snapshot.AwayTeam) || source.Team == "away" {
		teamID = "away"
	}
	score := snapshot.Score
	if eventType == "goal" {
		if teamID == "home" {
			score.Home++
		} else if teamID == "away" {
			score.Away++
		}
	}
	period := "first_half"
	if source.Minute > 45 {
		period = "second_half"
	}
	playerName := strings.TrimSpace(source.Player.Name)
	description := strings.TrimSpace(playerName + " " + eventType)
	if description == "" {
		description = eventType
	}
	matchEvent := matchstate.MatchEvent{
		MatchID:         matchID,
		Source:          string(SourceAPISports),
		ProviderName:    "api-sports",
		ProviderEventID: strconv.Itoa(fixtureID) + ":" + strconv.FormatInt(source.ID, 10),
		Period:          period,
		Clock:           fmt.Sprintf("%02d:00", source.Minute),
		EventType:       eventType,
		TeamID:          teamID,
		TeamName:        source.Team,
		PlayerName:      playerName,
		Score:           score,
		Intensity:       3,
		FactStatus:      matchstate.FactStatusProvisional,
		Description:     description,
		Tags:            []string{"provider=api-sports", "fixture=" + strconv.Itoa(fixtureID)},
		Visibility:      "public",
		Status:          "active",
	}
	if playerName != "" {
		matchEvent.Participants = []matchstate.Participant{{
			Role:     matchstate.DefaultParticipantRole(eventType),
			Name:     playerName,
			TeamID:   teamID,
			TeamName: source.Team,
		}}
	}
	switch eventType {
	case "goal":
		matchEvent.Intensity = 5
		matchEvent.ProactiveText = fmt.Sprintf("%s进球了，比分来到%d比%d。", playerName, score.Home, score.Away)
	case "red_card":
		matchEvent.Intensity = 5
		matchEvent.ProactiveText = fmt.Sprintf("%s被红牌罚下。", playerName)
	}
	return matchEvent
}

func (m *Manager) Close() {
	m.cancel()
	m.wg.Wait()
}

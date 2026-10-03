package datasource

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
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

type Freshness string

const (
	FreshnessFresh    Freshness = "fresh"
	FreshnessDegraded Freshness = "degraded"
	FreshnessUnknown  Freshness = "unknown"
	FreshnessOffline  Freshness = "offline"
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
	// AutoConfirmWindow 是稳定窗(ADR-0024):>0 启用信任分级自动确认,
	// <=0 禁用(生产建议 30s)。
	AutoConfirmWindow time.Duration
	// EspnPollInterval 是 ESPN 快照轮询节拍:<=0 用生产默认 5s(ESPN 无 SLA,
	// 不快于 5s);测试注入小值。
	EspnPollInterval time.Duration
}

type SourceConfig struct {
	Type          SourceType `json:"type"`
	FixtureID     int        `json:"fixtureId,omitempty"`
	ImportHistory bool       `json:"importHistory,omitempty"`
	ExpectedDelay string     `json:"expectedDelay,omitempty"`
	// ESPN 快照源(auto-hosting 2.4):联赛 slug + ESPN event ID。
	League      string `json:"league,omitempty"`
	EspnEventID string `json:"espnEventId,omitempty"`
}

type SourceStatus struct {
	Type                 SourceType  `json:"type"`
	State                SourceState `json:"state"`
	FixtureID            int         `json:"fixtureId,omitempty"`
	StartedAt            string      `json:"startedAt,omitempty"`
	LastPollAt           string      `json:"lastPollAt,omitempty"`
	LastEventAt          string      `json:"lastEventAt,omitempty"`
	LatencyMS            int64       `json:"latencyMs,omitempty"`
	Error                string      `json:"error,omitempty"`
	Reconnects           int         `json:"reconnects,omitempty"`
	Freshness            Freshness   `json:"freshness"`
	LatencyP95MS         int64       `json:"latencyP95Ms,omitempty"`
	FreshnessThresholdMS int64       `json:"freshnessThresholdMs,omitempty"`
	UserMayLead          bool        `json:"userMayLead"`
	ExpectedDelay        string      `json:"expectedDelay,omitempty"`
	// 上游漂移计数(auto-hosting 2.1):changed/retracted 投递不产事实,
	// 只在此显形——运营台可见,稳定窗确认(ADR-0024)以此为否决信号。
	Drifts          int    `json:"drifts,omitempty"`
	Retractions     int    `json:"retractions,omitempty"`
	LastDriftAt     string `json:"lastDriftAt,omitempty"`
	LastDriftDetail string `json:"lastDriftDetail,omitempty"`
}

type MatchSourceStatus struct {
	MatchID      string                      `json:"matchId"`
	ActiveSource SourceType                  `json:"activeSource"`
	Sources      map[SourceType]SourceStatus `json:"sources"`
}

type Manager struct {
	ctx         context.Context
	cancel      context.CancelFunc
	store       matchstate.Repository
	client      EventsClient
	espnClient  EspnClient
	config      ManagerConfig
	controlMu   sync.Mutex
	mu          sync.RWMutex
	active      map[string]SourceType
	runs        map[string]*sourceRun
	manualDelay map[string]string
	wg          sync.WaitGroup
	stability   *stabilityWindowState
}

type sourceRun struct {
	cancel    context.CancelFunc
	status    SourceStatus
	latencies []latencySample
	wait      sync.WaitGroup
	// lastDriftAtTime 保留漂移时间的单调钟形态(veto 比较用):wire 的
	// LastDriftAt 字符串经 RFC3339 往返丢单调读数,Windows 时钟粒度下与窗
	// 起点同刻度时 After() 会误判。
	lastDriftAtTime time.Time
	// driftSeq 是本场漂移的递增序号(stability 窗口否决的比较基准,无时钟)。
	driftSeq int64
}

type latencySample struct {
	at      time.Time
	latency time.Duration
}

// WithEspnClient 装配 ESPN 快照源客户端(nil 即 ESPN 源 unconfigured)。
func (m *Manager) WithEspnClient(client EspnClient) *Manager {
	m.espnClient = client
	return m
}

// EspnMatchPreview 拉一份 ESPN summary 供托管编排填充比赛配置
// (auto-hosting 2.6;不做缓存,一次编排一次拉取)。
func (m *Manager) EspnMatchPreview(league, eventID string) (*EspnSummary, error) {
	if m.espnClient == nil {
		return nil, fmt.Errorf("espn is not configured")
	}
	return m.espnClient.GetSummary(league, eventID)
}

// EspnConfigured 报告 ESPN 源是否可用(运营台选项门控)。
func (m *Manager) EspnConfigured() bool {
	return m.espnClient != nil
}

func NewManager(parent context.Context, store matchstate.Repository, client EventsClient, config ManagerConfig) *Manager {
	ctx, cancel := context.WithCancel(parent)
	if config.PollInterval <= 0 {
		config.PollInterval = 3 * time.Second
	}
	manager := &Manager{
		ctx:         ctx,
		cancel:      cancel,
		store:       store,
		client:      client,
		config:      config,
		active:      make(map[string]SourceType),
		runs:        make(map[string]*sourceRun),
		manualDelay: make(map[string]string),
		stability:   newStabilityWindowState(),
	}
	if config.AutoConfirmWindow > 0 {
		manager.wg.Add(1)
		go func() {
			defer manager.wg.Done()
			manager.runStabilitySweeper(ctx)
		}()
	}
	return manager
}

func (m *Manager) Status(matchID string) MatchSourceStatus {
	m.mu.RLock()
	active := m.active[matchID]
	manualDelay := m.manualDelay[matchID]
	m.mu.RUnlock()
	if active == "" {
		active = SourceManual
	}
	apiState := StateStandby
	if m.client == nil {
		apiState = StateUnconfigured
	}
	espnState := StateStandby
	if m.espnClient == nil {
		espnState = StateUnconfigured
	}
	status := MatchSourceStatus{
		MatchID:      matchID,
		ActiveSource: active,
		Sources: map[SourceType]SourceStatus{
			SourceManual:    {Type: SourceManual, State: StateReady, Freshness: FreshnessUnknown, UserMayLead: true, ExpectedDelay: defaultExpectedDelay(manualDelay)},
			SourceReplay:    {Type: SourceReplay, State: StateReady, Freshness: FreshnessUnknown, UserMayLead: true},
			SourceAPISports: {Type: SourceAPISports, State: apiState, Freshness: FreshnessOffline, UserMayLead: true},
			SourceESPN:      {Type: SourceESPN, State: espnState, Freshness: FreshnessOffline, UserMayLead: true},
		},
	}
	m.mu.RLock()
	if run := m.runs[matchID]; run != nil {
		// 活源状态落自己的类型槽(多外源形态:按 run.Type 落,不再硬编码 api-sports)。
		entry := m.deriveSourceStatus(run.status, time.Now().UTC())
		status.Sources[entry.Type] = entry
	}
	m.mu.RUnlock()
	return status
}

func (m *Manager) ObservationReconcileWindow(matchID string, base time.Duration) time.Duration {
	if base <= 0 {
		base = time.Minute
	}
	status := m.Status(strings.TrimSpace(matchID))
	active := status.Sources[status.ActiveSource]
	switch status.ActiveSource {
	case SourceManual:
		switch defaultExpectedDelay(active.ExpectedDelay) {
		case "conservative":
			return maxDuration(base, 2*time.Minute)
		case "normal":
			return maxDuration(base, 90*time.Second)
		default:
			return base
		}
	case SourceAPISports:
		switch active.Freshness {
		case FreshnessOffline:
			return maxDuration(base, 2*time.Minute)
		case FreshnessDegraded, FreshnessUnknown:
			return maxDuration(base, 90*time.Second)
		default:
			return base
		}
	default:
		return base
	}
}

func (m *Manager) Start(matchID string, config SourceConfig) (MatchSourceStatus, error) {
	matchID = strings.TrimSpace(matchID)
	if matchID == "" {
		return MatchSourceStatus{}, fmt.Errorf("matchId is required")
	}
	if config.Type == "" {
		config.Type = SourceManual
	}
	if config.Type != SourceManual && config.Type != SourceReplay && config.Type != SourceAPISports && config.Type != SourceESPN {
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
	if config.Type == SourceESPN {
		if m.espnClient == nil {
			return MatchSourceStatus{}, fmt.Errorf("espn is not configured")
		}
		if strings.TrimSpace(config.League) == "" || strings.TrimSpace(config.EspnEventID) == "" {
			return MatchSourceStatus{}, fmt.Errorf("league and espnEventId are required for espn source")
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
		if config.Type == SourceManual {
			m.manualDelay[matchID] = defaultExpectedDelay(config.ExpectedDelay)
		}
		m.mu.Unlock()
		return m.Status(matchID), nil
	}
	sourceType := config.Type
	runCtx, runCancel := context.WithCancel(m.ctx)
	run := &sourceRun{
		cancel: runCancel,
		status: SourceStatus{
			Type:      sourceType,
			State:     StateRunning,
			FixtureID: config.FixtureID,
			StartedAt: time.Now().UTC().Format(time.RFC3339Nano),
		},
	}
	m.runs[matchID] = run
	m.active[matchID] = sourceType
	m.mu.Unlock()

	if config.Type == SourceESPN {
		interval := m.config.EspnPollInterval
		if interval <= 0 {
			interval = 5 * time.Second // ESPN 无 SLA:生产默认不快于 5s
		}
		espnRunInstance := &espnRun{
			manager:  m,
			matchID:  matchID,
			league:   config.League,
			eventID:  config.EspnEventID,
			interval: interval,
			seen:     make(map[string]bool),
		}
		m.wg.Add(1)
		run.wait.Add(1)
		go func() {
			defer m.wg.Done()
			defer run.wait.Done()
			espnRunInstance.loop(runCtx)
		}()
		return m.Status(matchID), nil
	}

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
			if delivery.Kind != PollEventKind {
				// 上游漂移(auto-hosting 2.1):消失/内容变化不产事实——登记
				// 计数与描述供稳定窗否决与运营关注,确认投递不阻塞 poller。
				m.recordSourceDrift(matchID, delivery.Kind, standardEvent)
				delivery.Acknowledge(true)
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
	run.latencies = append(run.latencies, latencySample{at: report.At.UTC(), latency: report.Latency})
	cutoff := report.At.UTC().Add(-5 * time.Minute)
	kept := run.latencies[:0]
	for _, sample := range run.latencies {
		if !sample.at.Before(cutoff) {
			kept = append(kept, sample)
		}
	}
	run.latencies = kept
	run.status.LatencyP95MS = latencyP95(run.latencies).Milliseconds()
	if report.Err != nil {
		run.status.State = StateError
		run.status.Error = report.Err.Error()
		run.status.Reconnects++
		return
	}
	run.status.State = StateRunning
	run.status.Error = ""
}

func (m *Manager) deriveSourceStatus(status SourceStatus, now time.Time) SourceStatus {
	threshold := 2*m.config.PollInterval + time.Duration(status.LatencyP95MS)*time.Millisecond
	if threshold < 6*time.Second {
		threshold = 6 * time.Second
	}
	if threshold > 20*time.Second {
		threshold = 20 * time.Second
	}
	status.FreshnessThresholdMS = threshold.Milliseconds()
	switch status.State {
	case StateUnconfigured, StateStopped:
		status.Freshness = FreshnessOffline
	case StateRunning, StateError:
		lastPoll, err := time.Parse(time.RFC3339Nano, status.LastPollAt)
		if err != nil {
			status.Freshness = FreshnessUnknown
		} else {
			age := now.Sub(lastPoll)
			switch {
			case status.State == StateRunning && age <= threshold:
				status.Freshness = FreshnessFresh
			case age <= 2*threshold:
				status.Freshness = FreshnessDegraded
			default:
				status.Freshness = FreshnessOffline
			}
		}
	default:
		status.Freshness = FreshnessUnknown
	}
	status.UserMayLead = status.Freshness != FreshnessFresh
	return status
}

func latencyP95(samples []latencySample) time.Duration {
	if len(samples) == 0 {
		return 0
	}
	values := make([]time.Duration, len(samples))
	for index, sample := range samples {
		values[index] = sample.latency
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	index := (95*len(values) + 99) / 100
	if index < 1 {
		index = 1
	}
	return values[index-1]
}

func defaultExpectedDelay(value string) string {
	switch strings.TrimSpace(value) {
	case "fast", "normal", "conservative":
		return strings.TrimSpace(value)
	default:
		return "normal"
	}
}

func maxDuration(left, right time.Duration) time.Duration {
	if left > right {
		return left
	}
	return right
}

func (m *Manager) updateSourceError(matchID string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if run := m.runs[matchID]; run != nil {
		run.status.State = StateError
		run.status.Error = err.Error()
	}
}

// recordSourceDrift 登记上游漂移(auto-hosting 2.1):正文只进日志,状态面
// 只留计数与最近一条描述——与 duplex_event 同纪律。
func (m *Manager) recordSourceDrift(matchID string, kind PollKind, standardEvent *event.StandardEvent) {
	if standardEvent == nil {
		return
	}
	log.Printf("datasource drift: match=%q kind=%q type=%q minute=%d player=%q detail=%q",
		matchID, kind, standardEvent.Type, standardEvent.Minute, standardEvent.Player.Name, standardEvent.Detail)
	m.mu.Lock()
	defer m.mu.Unlock()
	run := m.runs[matchID]
	if run == nil {
		return
	}
	if kind == PollRetractedKind {
		run.status.Retractions++
	} else {
		run.status.Drifts++
	}
	driftAt := time.Now()
	run.lastDriftAtTime = driftAt
	run.driftSeq++
	run.status.LastDriftAt = driftAt.UTC().Format(time.RFC3339Nano)
	run.status.LastDriftDetail = fmt.Sprintf("%s %d' %s %s", standardEvent.Type, standardEvent.Minute, standardEvent.Player.Name, standardEvent.Detail)
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
	// VAR 结论类事件必须引用一条已确认事实(auto-hosting 2.1;matchstate
	// validateEventRelations 硬约束):goal_cancelled 引用同队最近已确认进球,
	// var_result 同理。找不到引用则降级 var_check 留运营——取消语境保留在
	// 描述里,绝不产一条无引用的 correction(会被账本拒绝)。
	revisionOf := ""
	if eventType == "goal_cancelled" || eventType == "var_result" {
		revisionOf = varReferenceFactID(snapshot, teamID)
		if revisionOf == "" {
			description = strings.TrimSpace("VAR 判定进球取消待复核 " + playerName)
			eventType = "var_check"
		} else if eventType == "goal_cancelled" {
			// 账本校验要求取消事件的比分 = 当前比分扣掉被取消进球
			// (validateAgainstSnapshot):确认后投影回放据此回退。
			switch teamID {
			case "home":
				if score.Home > 0 {
					score.Home--
				}
			case "away":
				if score.Away > 0 {
					score.Away--
				}
			}
			description = fmt.Sprintf("VAR 判定:%s 的进球无效,比分待更正。", playerName)
		} else {
			description = fmt.Sprintf("VAR 确认:%s 的进球有效。", playerName)
		}
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
		RevisionOf:      revisionOf,
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

// varReferenceFactID 在快照里为 VAR 结论类事件找可引用的已确认事实:同队
// 最近一条已确认进球(goal_cancelled/var_result 同源)。已被取消过的进球
// 不再引用(防重复取消被账本拒绝)。找不到返回空串——调用方降级 var_check。
func varReferenceFactID(snapshot matchstate.Snapshot, teamID string) string {
	for index := len(snapshot.RecentEvents) - 1; index >= 0; index-- {
		candidate := snapshot.RecentEvents[index]
		if candidate.EventType != "goal" || candidate.FactID == "" {
			continue
		}
		if candidate.FactStatus != matchstate.FactStatusConfirmed && candidate.FactStatus != matchstate.FactStatusReconciled {
			continue
		}
		if teamID != "" && candidate.TeamID != teamID {
			continue
		}
		if goalAlreadyCancelled(snapshot, candidate.FactID) {
			continue
		}
		return candidate.FactID
	}
	return ""
}

func goalAlreadyCancelled(snapshot matchstate.Snapshot, goalFactID string) bool {
	for _, event := range snapshot.RecentEvents {
		if event.EventType == "goal_cancelled" && (event.RevisionOf == goalFactID) {
			return true
		}
	}
	return false
}

func (m *Manager) Close() {
	m.cancel()
	m.wg.Wait()
}

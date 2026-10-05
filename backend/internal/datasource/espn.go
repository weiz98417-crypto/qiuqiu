package datasource

// ESPN hidden API adapter(auto-hosting 2.4):免费无 key 的快照 diff 源。
// 端点 site.web.api.espn.com(与 scripts/demo-seed.mjs 同源同字段,非官方、
// 无 SLA——风险对冲见 ADR-0024/design D6:健康探测显形 + api-sports 备源切换)。
// 快照 diff 形态:每轮拉 summary 全量,与上次快照 diff 产事件;ProviderEventID
// 用 ESPN 稳定事件 ID(espn:<id>),冷启动重放被账本按 Source+ProviderEventID
// 幂等吸收。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"qiuqiu/internal/matchstate"
)

const SourceESPN SourceType = "espn"

const espnBaseURL = "https://site.web.api.espn.com"

type EspnClient interface {
	GetSummary(league, eventID string) (*EspnSummary, error)
}

type EspnClientHTTP struct {
	baseURL    string
	httpClient *http.Client
}

func NewEspnClient() *EspnClientHTTP {
	return &EspnClientHTTP{baseURL: espnBaseURL, httpClient: &http.Client{Timeout: 10 * time.Second}}
}

func (c *EspnClientHTTP) WithBaseURL(url string) *EspnClientHTTP {
	if trimmed := strings.TrimRight(strings.TrimSpace(url), "/"); trimmed != "" {
		c.baseURL = trimmed
	}
	return c
}

type EspnSummary struct {
	Header struct {
		League struct {
			Name string `json:"name"`
		} `json:"league"`
		Competitions []struct {
			Date        string           `json:"date"`
			Status      EspnStatus       `json:"status"`
			Competitors []EspnCompetitor `json:"competitors"`
		} `json:"competitions"`
	} `json:"header"`
	KeyEvents []EspnKeyEvent `json:"keyEvents"`
	Rosters   []struct {
		HomeAway string `json:"homeAway"`
		Team     struct {
			ID string `json:"id"`
		} `json:"team"`
	} `json:"rosters"`
}

type EspnStatus struct {
	Type struct {
		State       string `json:"state"` // pre | in | post
		Completed   bool   `json:"completed"`
		Description string `json:"description"`
	} `json:"type"`
	DisplayClock string `json:"displayClock"`
	Period       int    `json:"period"`
}

type EspnCompetitor struct {
	HomeAway string `json:"homeAway"`
	Team     struct {
		DisplayName string `json:"displayName"`
	} `json:"team"`
	Score string `json:"score"`
}

type EspnKeyEvent struct {
	ID   string `json:"id"`
	Type struct {
		ID   string `json:"id"` // goal | goal---* | penalty---scored | yellow-card | red-card | second-yellow | substitution | ...
		Text string `json:"text"`
	} `json:"type"`
	Clock struct {
		DisplayValue string `json:"displayValue"`
	} `json:"clock"`
	Team struct {
		ID          string `json:"id"`
		DisplayName string `json:"displayName"`
	} `json:"team"`
	ScoringPlay      bool   `json:"scoringPlay"`
	Text             string `json:"text"`
	AthletesInvolved []struct {
		DisplayName string `json:"displayName"`
	} `json:"athletesInvolved"`
	Participants []struct {
		Athlete struct {
			DisplayName string `json:"displayName"`
		} `json:"athlete"`
	} `json:"participants"`
}

func (c *EspnClientHTTP) GetSummary(league, eventID string) (*EspnSummary, error) {
	url := fmt.Sprintf("%s/apis/site/v2/sports/soccer/%s/summary?event=%s", c.baseURL, league, eventID)
	req, err := http.NewRequestWithContext(context.Background(), "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("espn request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("espn request failed with status %d: %s", resp.StatusCode, string(body))
	}
	var summary EspnSummary
	if err := json.NewDecoder(resp.Body).Decode(&summary); err != nil {
		return nil, fmt.Errorf("decode espn summary: %w", err)
	}
	return &summary, nil
}

// espnEventType 是 demo-seed.mjs espnEventType 的 Go 移植:映射不出的类型
// 返回空串(跳过,不入账)。
func espnEventType(typeID string) string {
	switch {
	case typeID == "goal" || strings.HasPrefix(typeID, "goal---") || typeID == "penalty---scored":
		return "goal"
	case typeID == "yellow-card":
		return "yellow_card"
	case typeID == "red-card" || typeID == "second-yellow":
		return "red_card"
	case typeID == "substitution":
		return "substitution"
	default:
		return ""
	}
}

// espnSide 把事件球队 ID 映射成 home/away(经 summary rosters,同 demo-seed)。
func (s *EspnSummary) espnSide(teamID string) string {
	if teamID == "" {
		return ""
	}
	for _, roster := range s.Rosters {
		if roster.Team.ID == teamID {
			return roster.HomeAway
		}
	}
	return ""
}

func (s *EspnSummary) status() (EspnStatus, bool) {
	if len(s.Header.Competitions) == 0 {
		return EspnStatus{}, false
	}
	return s.Header.Competitions[0].Status, true
}

// espnSnapshot 是一轮 summary 的归一化产物:可入账事件 + 生命周期状态。
type espnSnapshot struct {
	events    []EspnKeyEvent
	state     string // pre | in | post
	period    int
	completed bool
}

// espnRun 是 ESPN 快照 diff 源的运行态:每轮拉 summary,与上次快照 diff 产
// 新事件 + 生命周期迁移(kickoff/halftime/fulltime),经 Manager.Ingest 落
// provisional(自动确认由 ADR-0024 稳定窗统一处理)。
type espnRun struct {
	manager       *Manager
	matchID       string
	league        string
	eventID       string
	interval      time.Duration
	seen          map[string]bool
	lastState     string
	lastPeriod    int
	lastCompleted bool
}

func (r *espnRun) loop(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	backoff := time.Second
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			startedAt := time.Now()
			summary, err := r.manager.espnClient.GetSummary(r.league, r.eventID)
			r.manager.updatePollStatus(r.matchID, PollReport{At: time.Now().UTC(), Latency: time.Since(startedAt), Err: err})
			if err != nil {
				backoff = min(backoff*2, 30*time.Second)
				log.Printf("espn poller: error fetching summary (retry in %v): %v", backoff, err)
				r.manager.mu.Lock()
				if run := r.manager.runs[r.matchID]; run != nil {
					run.status.State = StateError
					run.status.Error = err.Error()
				}
				r.manager.mu.Unlock()
				ticker.Reset(backoff)
				continue
			}
			backoff = time.Second
			ticker.Reset(r.interval)
			if !r.ingest(summary) {
				return
			}
		}
	}
}

// ingest diff 一份快照;返回 false 表示 ctx 取消。
func (r *espnRun) ingest(summary *EspnSummary) bool {
	// 账本校验要求事件比分=当前公开投影分:取 store 快照而非 ESPN 记分牌
	// (provisional 未确认不进投影,与 api-sports 的派生纪律一致)。
	ledgerScore := r.manager.store.Snapshot(r.matchID).Score
	state, ok := summary.status()
	if !ok {
		return true
	}
	// 生命周期迁移:pre→in = kickoff,period 1→2 = halftime,completed = fulltime。
	if r.lastState == "pre" && state.Type.State == "in" {
		r.emitStatusEvent("kickoff", state, ledgerScore)
	}
	if r.lastPeriod == 1 && state.Period >= 2 {
		r.emitStatusEvent("halftime", state, ledgerScore)
	}
	if !r.lastCompleted && state.Type.Completed {
		r.emitStatusEvent("fulltime", state, ledgerScore)
		// 终场即收源(auto-hosting 2.6 design D5):不留悬挂轮询;Stop 会
		// 停掉本 goroutine(ctx 取消),下一轮循环自然退出。
		go r.manager.Stop(r.matchID)
		return true
	}
	r.lastState = state.Type.State
	r.lastPeriod = state.Period
	r.lastCompleted = state.Type.Completed

	for _, keyEvent := range summary.KeyEvents {
		eventType := espnEventType(keyEvent.Type.ID)
		if eventType == "" || r.seen[keyEvent.ID] {
			continue
		}
		matchEvent := espnMatchEvent(r.matchID, r.eventID, summary, keyEvent, eventType, ledgerScore)
		if matchEvent == nil {
			continue
		}
		r.deliver(matchEvent)
		r.seen[keyEvent.ID] = true
		r.manager.mu.Lock()
		if run := r.manager.runs[r.matchID]; run != nil {
			run.status.LastEventAt = time.Now().UTC().Format(time.RFC3339Nano)
			run.status.State = StateRunning
			run.status.Error = ""
		}
		r.manager.mu.Unlock()
	}
	return true
}

// deliver 落账一条事件;ErrDuplicate/ErrConflict(跨源仲裁)同为已消化,
// 失败只记日志与源状态——seen 标记由调用方在成功后补,下轮快照 diff 重试。
func (r *espnRun) deliver(matchEvent *matchstate.MatchEvent) {
	_, _, err := r.manager.Ingest(r.manager.ctx, r.matchID, *matchEvent)
	persisted := err == nil || errors.Is(err, matchstate.ErrDuplicate) || errors.Is(err, matchstate.ErrConflict)
	if !persisted {
		log.Printf("espn ingest: match=%q event=%q: %v", r.matchID, matchEvent.ProviderEventID, err)
		r.manager.updateSourceError(r.matchID, err)
	}
}

// emitStatusEvent 落一条生命周期事件(ProviderEventID 稳定:espn:status:<code>;
// clock 取 ESPN 状态时钟,账本要求 clock 必填)。
func (r *espnRun) emitStatusEvent(eventType string, state EspnStatus, ledgerScore matchstate.Score) {
	providerEventID := "espn:status:" + eventType
	clock := strings.TrimSpace(state.DisplayClock)
	if clock == "" {
		switch eventType {
		case "kickoff":
			clock = "00:00"
		case "halftime":
			clock = "45:00"
		default:
			clock = "90:00"
		}
	}
	r.deliver(&matchstate.MatchEvent{
		MatchID:         r.matchID,
		Source:          string(SourceESPN),
		ProviderName:    "espn",
		ProviderEventID: providerEventID,
		EventType:       eventType,
		Clock:           clock,
		Score:           ledgerScore,
		Intensity:       3,
		FactStatus:      matchstate.FactStatusProvisional,
		Description:     espnStatusDescription(eventType),
		Tags:            []string{"provider=espn", "fixture=" + r.eventID},
		Visibility:      "public",
		Status:          "active",
	})
}

func espnStatusDescription(eventType string) string {
	switch eventType {
	case "kickoff":
		return "比赛开始了。"
	case "halftime":
		return "上半场结束,进入中场休息。"
	case "fulltime":
		return "比赛结束了。"
	default:
		return eventType
	}
}

// espnMatchEvent 把一条 ESPN keyEvent 映射成 provisional 账本事件。
func espnMatchEvent(matchID, eventID string, summary *EspnSummary, keyEvent EspnKeyEvent, eventType string, ledgerScore matchstate.Score) *matchstate.MatchEvent {
	side := summary.espnSide(keyEvent.Team.ID)
	period := "first_half"
	clock := strings.TrimSpace(keyEvent.Clock.DisplayValue)
	minute := clockMinutes(clock)
	if minute > 45 {
		period = "second_half"
	}
	if state, ok := summary.status(); ok && state.Period >= 4 {
		period = "second_half"
	}
	playerName := ""
	for _, participant := range keyEvent.Participants {
		if name := strings.TrimSpace(participant.Athlete.DisplayName); name != "" {
			playerName = name
			break
		}
	}
	if playerName == "" && len(keyEvent.AthletesInvolved) > 0 {
		playerName = strings.TrimSpace(keyEvent.AthletesInvolved[0].DisplayName)
	}
	description := espnEventDescription(eventType, playerName, keyEvent.Text)
	matchEvent := matchstate.MatchEvent{
		MatchID:         matchID,
		Source:          string(SourceESPN),
		ProviderName:    "espn",
		ProviderEventID: "espn:" + keyEvent.ID,
		Period:          period,
		Clock:           clock,
		EventType:       eventType,
		TeamID:          side,
		TeamName:        keyEvent.Team.DisplayName,
		PlayerName:      playerName,
		Score:           eventScore(ledgerScore, side, eventType),
		Intensity:       3,
		FactStatus:      matchstate.FactStatusProvisional,
		Description:     description,
		Tags:            []string{"provider=espn", "fixture=" + eventID, "event=" + keyEvent.ID},
		Visibility:      "public",
		Status:          "active",
	}
	if eventType == "goal" {
		matchEvent.Intensity = 5
	}
	if playerName != "" {
		matchEvent.Participants = []matchstate.Participant{{
			Role:     matchstate.DefaultParticipantRole(eventType),
			Name:     playerName,
			TeamID:   side,
			TeamName: keyEvent.Team.DisplayName,
		}}
	}
	return &matchEvent
}

func espnEventDescription(eventType, playerName, text string) string {
	who := playerName
	if who == "" {
		who = "球员"
	}
	switch eventType {
	case "goal":
		return fmt.Sprintf("进球:%s。", who)
	case "yellow_card":
		return fmt.Sprintf("黄牌:%s。", who)
	case "red_card":
		return fmt.Sprintf("红牌:%s。", who)
	case "substitution":
		return fmt.Sprintf("换人:%s。", who)
	default:
		if strings.TrimSpace(text) != "" {
			return text
		}
		return eventType
	}
}

// clockMinutes 从 ESPN 时钟展示值解析分钟("45'+2'" → 45;"90'+3'" → 90)。
func clockMinutes(displayValue string) int {
	value := strings.TrimSpace(displayValue)
	if index := strings.Index(value, "'"); index >= 0 {
		value = value[:index]
	}
	minutes, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0
	}
	return minutes
}

// eventScore 派生事件的比分:goal 按队 +1(与 api-sports 派生纪律一致),
// 其余事件保持当前投影分——账本校验(validateAgainstSnapshot)据此放行。
func eventScore(current matchstate.Score, teamID, eventType string) matchstate.Score {
	score := current
	if eventType == "goal" {
		switch teamID {
		case "home":
			score.Home++
		case "away":
			score.Away++
		}
	}
	return score
}

package matchstate

import (
	"sort"
	"strings"
	"time"
)

type MatchSummary struct {
	MatchID     string `json:"matchId"`
	HomeTeam    string `json:"homeTeam"`
	AwayTeam    string `json:"awayTeam"`
	Competition string `json:"competition,omitempty"`
	Kickoff     string `json:"kickoff,omitempty"`
	Period      string `json:"period,omitempty"`
	Clock       string `json:"clock,omitempty"`
	HomeScore   int    `json:"homeScore"`
	AwayScore   int    `json:"awayScore"`
	LiveLabel   string `json:"liveLabel"`
	Status      string `json:"status"`
	Lifecycle   string `json:"lifecycle,omitempty"`
	UpdatedAt   string `json:"updatedAt,omitempty"`
}

type CatalogRepository interface {
	PublicMatchCatalog() ([]MatchSummary, error)
}

func (s *Store) PublicMatchCatalog() ([]MatchSummary, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]MatchSummary, 0, len(s.configs))
	for matchID, config := range s.configs {
		item := summaryFromState(matchID, config, s.clocks[matchID], s.events[matchID])
		if item.Status != "scheduled" && item.Status != "live" && item.Status != "finished" {
			continue
		}
		items = append(items, item)
	}
	sort.Slice(items, func(left, right int) bool {
		if statusRank(items[left].Status) != statusRank(items[right].Status) {
			return statusRank(items[left].Status) < statusRank(items[right].Status)
		}
		if items[left].Kickoff == items[right].Kickoff {
			return items[left].MatchID < items[right].MatchID
		}
		return items[left].Kickoff < items[right].Kickoff
	})
	return items, nil
}

func statusRank(status string) int {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "live":
		return 0
	case "scheduled":
		return 1
	case "finished":
		return 2
	default:
		return 3
	}
}

func summaryFromState(matchID string, config MatchConfig, clock MatchClock, events []MatchEvent) MatchSummary {
	config = normalizeConfig(matchID, config)
	now := time.Now().UTC()
	projection, err := (FactLedgerEngine{}).Project(FactLedgerProjectInput{MatchID: matchID, Events: events, Config: config, Clock: clock, Now: now})
	if err != nil {
		projection.Snapshot = buildLegacySnapshot(matchID, filterPublicFacts(events), config, clock, now)
	}
	period := strings.TrimSpace(projection.Snapshot.Period)
	liveLabel := "等待开赛"
	status := "scheduled"
	lifecycle := normalizeLifecycle(config.Lifecycle)
	if lifecycle != "" {
		status = lifecycle
	}
	switch strings.ToLower(period) {
	case "fulltime", "full_time", "finished":
		liveLabel = "已结束"
		if lifecycle == "" {
			status = "finished"
		}
	case "first_half", "second_half", "extra_time", "halftime":
		liveLabel = "直播中"
		if lifecycle == "" {
			status = "live"
		}
	}
	return MatchSummary{
		MatchID: matchID, HomeTeam: projection.Snapshot.HomeTeam, AwayTeam: projection.Snapshot.AwayTeam,
		Competition: projection.Snapshot.Competition, Kickoff: config.Kickoff, Period: period,
		Clock: projection.Snapshot.Clock, HomeScore: projection.Snapshot.Score.Home,
		AwayScore: projection.Snapshot.Score.Away, LiveLabel: liveLabel, Status: status,
		UpdatedAt: config.UpdatedAt, Lifecycle: lifecycle,
	}
}

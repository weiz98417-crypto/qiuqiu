package datasource

import (
	"context"
	"log"
	"time"

	"qiuqiu/internal/event"
)

type PollReport struct {
	At      time.Time
	Latency time.Duration
	Err     error
}

type Poller struct {
	client         EventsClient
	matchID        int64
	lastEventID    int64
	eventChan      chan<- *event.StandardEvent
	reportChan     chan<- PollReport
	interval       time.Duration
	homeScore      int
	awayScore      int
	includeInitial bool
	initialized    bool
}

func NewPoller(client EventsClient, matchID int64, eventChan chan<- *event.StandardEvent) *Poller {
	return &Poller{client: client, matchID: matchID, eventChan: eventChan, interval: 3 * time.Second}
}

func (p *Poller) SetScore(home, away int) {
	p.homeScore = home
	p.awayScore = away
}

func (p *Poller) WithInterval(interval time.Duration) *Poller {
	if interval > 0 {
		p.interval = interval
	}
	return p
}

func (p *Poller) WithInitialEvents(include bool) *Poller {
	p.includeInitial = include
	return p
}

func (p *Poller) WithReports(reports chan<- PollReport) *Poller {
	p.reportChan = reports
	return p
}

func (p *Poller) Run(ctx context.Context) {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	backoff := time.Second
	maxBackoff := 30 * time.Second
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			startedAt := time.Now()
			events, err := p.client.GetEvents(int(p.matchID))
			p.report(PollReport{At: time.Now().UTC(), Latency: time.Since(startedAt), Err: err})
			if err != nil {
				backoff = min(backoff*2, maxBackoff)
				log.Printf("poller: error fetching events (retry in %v): %v", backoff, err)
				ticker.Reset(backoff)
				continue
			}
			backoff = time.Second
			ticker.Reset(p.interval)
			if !p.initialized && !p.includeInitial {
				for _, raw := range events {
					if raw.ID() > p.lastEventID {
						p.lastEventID = raw.ID()
					}
				}
				p.initialized = true
				continue
			}
			p.initialized = true
			for _, raw := range events {
				if raw.ID() <= p.lastEventID {
					continue
				}
				p.lastEventID = raw.ID()
				standardEvent := raw.ToStandardEvent(p.matchID, p.homeScore, p.awayScore)
				select {
				case p.eventChan <- standardEvent:
				default:
					log.Printf("poller: event channel full, dropping event %d", standardEvent.ID)
				}
			}
		}
	}
}

func (p *Poller) report(report PollReport) {
	if p.reportChan == nil {
		return
	}
	select {
	case p.reportChan <- report:
	default:
	}
}

func min(left, right time.Duration) time.Duration {
	if left < right {
		return left
	}
	return right
}

func (e *Event) ID() int64 {
	return int64(e.Time.Elapsed)*100 + int64(len(e.Type))
}

func (e *Event) ToStandardEvent(matchID int64, homeScore, awayScore int) *event.StandardEvent {
	return &event.StandardEvent{
		MatchID:   matchID,
		ID:        e.ID(),
		Type:      normalizeType(e.Type, e.Detail),
		Team:      e.Team.Name,
		Minute:    e.Time.Elapsed,
		Player:    event.PlayerInfo{ID: e.Player.ID, Name: e.Player.Name},
		Score:     event.ScoreInfo{Home: homeScore, Away: awayScore},
		Timestamp: time.Now(),
	}
}

func normalizeType(eventType, detail string) string {
	switch eventType {
	case "Goal":
		return "goal"
	case "Card":
		if detail == "Red Card" || detail == "Second Yellow card" {
			return "red_card"
		}
		return "yellow_card"
	case "Var":
		return "var_check"
	case "subst":
		return "substitution"
	default:
		return eventType
	}
}

package datasource

import (
	"context"
	"log"
	"sort"
	"sync"
	"time"

	"qiuqiu/internal/event"
)

type PollReport struct {
	At      time.Time
	Latency time.Duration
	Err     error
}

type PollDelivery struct {
	Event        *event.StandardEvent
	acknowledged chan bool
}

func newPollDelivery(standardEvent *event.StandardEvent) PollDelivery {
	return PollDelivery{Event: standardEvent, acknowledged: make(chan bool, 1)}
}

func (d PollDelivery) Acknowledge(persisted bool) {
	d.acknowledged <- persisted
}

type Poller struct {
	client         EventsClient
	matchID        int64
	lastEventID    int64
	lastEventMu    sync.Mutex
	eventChan      chan<- PollDelivery
	reportChan     chan<- PollReport
	commitCursor   func(int64) error
	interval       time.Duration
	homeScore      int
	awayScore      int
	includeInitial bool
	initialized    bool
}

func NewPoller(client EventsClient, matchID int64, eventChan chan<- PollDelivery) *Poller {
	return &Poller{client: client, matchID: matchID, eventChan: eventChan, interval: 3 * time.Second}
}

func (p *Poller) WithCursor(cursor int64) *Poller {
	if cursor > 0 {
		p.lastEventID = cursor
		p.initialized = true
	}
	return p
}

func (p *Poller) WithCursorCommit(commit func(int64) error) *Poller {
	p.commitCursor = commit
	return p
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
			sort.SliceStable(events, func(left, right int) bool {
				return events[left].ID() < events[right].ID()
			})
			if !p.initialized && !p.includeInitial {
				latestEventID := int64(0)
				for _, raw := range events {
					if raw.ID() > latestEventID {
						latestEventID = raw.ID()
					}
				}
				if latestEventID > 0 {
					if err := p.advanceCursor(latestEventID); err != nil {
						p.report(PollReport{At: time.Now().UTC(), Err: err})
						continue
					}
				}
				p.initialized = true
				continue
			}
			p.initialized = true
		eventLoop:
			for _, raw := range events {
				p.lastEventMu.Lock()
				lastEventID := p.lastEventID
				p.lastEventMu.Unlock()
				if raw.ID() <= lastEventID {
					continue
				}
				standardEvent := raw.ToStandardEvent(p.matchID, p.homeScore, p.awayScore)
				delivery := newPollDelivery(standardEvent)
				select {
				case p.eventChan <- delivery:
				case <-ctx.Done():
					return
				}
				select {
				case persisted := <-delivery.acknowledged:
					if !persisted {
						break eventLoop
					}
					if err := p.advanceCursor(standardEvent.ID); err != nil {
						p.report(PollReport{At: time.Now().UTC(), Err: err})
						break eventLoop
					}
				case <-ctx.Done():
					return
				}
			}
		}
	}
}

func (p *Poller) advanceCursor(eventID int64) error {
	if eventID <= 0 {
		return nil
	}
	if p.commitCursor != nil {
		if err := p.commitCursor(eventID); err != nil {
			return err
		}
	}
	p.lastEventMu.Lock()
	if eventID > p.lastEventID {
		p.lastEventID = eventID
	}
	p.lastEventMu.Unlock()
	return nil
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

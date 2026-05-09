package datasource

import (
	"context"
	"log"
	"qiuqiu/internal/event"
	"time"
)

// Poller polls api-sports.io for new match events.
type Poller struct {
	client      *Client
	matchID     int64
	lastEventID int64
	eventChan   chan<- *event.StandardEvent
	interval    time.Duration
	homeScore   int
	awayScore   int
}

func (p *Poller) SetScore(home, away int) {
	p.homeScore = home
	p.awayScore = away
}

func NewPoller(client *Client, matchID int64, eventChan chan<- *event.StandardEvent) *Poller {
	return &Poller{
		client:    client,
		matchID:   matchID,
		eventChan: eventChan,
		interval:  3 * time.Second,
	}
}

// Run starts the polling loop. Blocks until ctx is cancelled.
func (p *Poller) Run(ctx context.Context) {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	backoff := 1 * time.Second
	maxBackoff := 30 * time.Second
	consecutiveErrors := 0

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			events, err := p.client.GetEvents(int(p.matchID))
			if err != nil {
				consecutiveErrors++
				backoff = min(backoff*2, maxBackoff)
				log.Printf("poller: error fetching events (retry in %v): %v", backoff, err)
				ticker.Reset(backoff)
				continue
			}
			backoff = 1 * time.Second
			consecutiveErrors = 0
			ticker.Reset(p.interval)

			for _, raw := range events {
				if raw.ID() <= p.lastEventID {
					continue
				}
				p.lastEventID = raw.ID()
				ev := raw.ToStandardEvent(p.matchID, p.homeScore, p.awayScore)
				select {
				case p.eventChan <- ev:
				default:
					log.Printf("poller: event channel full, dropping event %d", ev.ID)
				}
			}
		}
	}
}

// min returns the smaller of two durations.
func min(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

// ID returns the event ID from api-sports format.
func (e *Event) ID() int64 {
	// api-sports events don't have a numeric ID; use elapsed time + type hash
	return int64(e.Time.Elapsed)*100 + int64(len(e.Type))
}

// ToStandardEvent converts api-sports Event to StandardEvent.
func (e *Event) ToStandardEvent(matchID int64, homeScore, awayScore int) *event.StandardEvent {
	evType := normalizeType(e.Type, e.Detail)
	return &event.StandardEvent{
		MatchID:   matchID,
		ID:        e.ID(),
		Type:      evType,
		Team:      e.Team.Name,
		Minute:    e.Time.Elapsed,
		Player:    event.PlayerInfo{ID: e.Player.ID, Name: e.Player.Name},
		Score:     event.ScoreInfo{Home: homeScore, Away: awayScore},
		Timestamp: time.Now(),
	}
}

func normalizeType(t, detail string) string {
	switch t {
	case "Goal":
		return "goal"
	case "Card":
		if detail == "Red Card" {
			return "red_card"
		}
		return "yellow_card"
	case "subst":
		return "substitution"
	default:
		return t
	}
}

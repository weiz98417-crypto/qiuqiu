package datasource

import (
	"context"
	"log"
	"sort"
	"strconv"
	"strings"
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
	Kind         PollKind
	Event        *event.StandardEvent
	acknowledged chan bool
}

// PollKind 区分新增事件与上游漂移(auto-hosting 2.1):changed/retracted 不产
// 新事实,由 Manager 登记供稳定窗否决与运营关注。
type PollKind string

const (
	PollEventKind     PollKind = "event"
	PollChangedKind   PollKind = "changed"
	PollRetractedKind PollKind = "retracted"
)

func newPollDelivery(kind PollKind, standardEvent *event.StandardEvent) PollDelivery {
	return PollDelivery{Kind: kind, Event: standardEvent, acknowledged: make(chan bool, 1)}
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
	// seen 是已见事件的最后形态(按合成 ID),供上游漂移 diff——消失=
	// retracted,内容变化=changed。进程内状态,重启即重建。
	seen map[int64]*event.StandardEvent
}

func NewPoller(client EventsClient, matchID int64, eventChan chan<- PollDelivery) *Poller {
	return &Poller{client: client, matchID: matchID, eventChan: eventChan, interval: 3 * time.Second, seen: make(map[int64]*event.StandardEvent)}
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
				p.recordSeen(events)
				continue
			}
			p.initialized = true
			ingestHealthy := true
		eventLoop:
			for _, raw := range events {
				p.lastEventMu.Lock()
				lastEventID := p.lastEventID
				p.lastEventMu.Unlock()
				if raw.ID() <= lastEventID {
					continue
				}
				standardEvent := raw.ToStandardEvent(p.matchID, p.homeScore, p.awayScore)
				delivery := newPollDelivery(PollEventKind, standardEvent)
				select {
				case p.eventChan <- delivery:
				case <-ctx.Done():
					return
				}
				select {
				case persisted := <-delivery.acknowledged:
					if !persisted {
						ingestHealthy = false
						break eventLoop
					}
					if err := p.advanceCursor(standardEvent.ID); err != nil {
						p.report(PollReport{At: time.Now().UTC(), Err: err})
						ingestHealthy = false
						break eventLoop
					}
				case <-ctx.Done():
					return
				}
			}
			if !ingestHealthy {
				continue
			}
			if !p.emitDriftDiffs(events, ctx) {
				return
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

// recordSeen 把整份快照登记为已见(首轮快进或健康落账后)。
func (p *Poller) recordSeen(events []Event) {
	p.lastEventMu.Lock()
	defer p.lastEventMu.Unlock()
	for _, raw := range events {
		p.seen[raw.ID()] = raw.ToStandardEvent(p.matchID, p.homeScore, p.awayScore)
	}
}

// emitDriftDiffs 对已见事件做上游漂移检测(auto-hosting 2.1):上一轮见过、
// 本轮消失 → retracted;同 ID 内容变化 → changed。漂移投递不产新事实、不推进
// 游标;返回 false 表示 ctx 取消。新增事件(未见 ID)在此登记为已见——它们
// 已由 eventLoop 作为新事件投递。
func (p *Poller) emitDriftDiffs(events []Event, ctx context.Context) bool {
	current := make(map[int64]*event.StandardEvent, len(events))
	for _, raw := range events {
		standard := raw.ToStandardEvent(p.matchID, p.homeScore, p.awayScore)
		current[standard.ID] = standard
	}
	p.lastEventMu.Lock()
	defer p.lastEventMu.Unlock()
	for id, prior := range p.seen {
		if _, ok := current[id]; ok {
			continue
		}
		if !p.emitDrift(ctx, newPollDelivery(PollRetractedKind, prior)) {
			return false
		}
		delete(p.seen, id)
	}
	for id, standard := range current {
		prior, ok := p.seen[id]
		if !ok {
			p.seen[id] = standard
			continue
		}
		if eventFingerprint(prior) == eventFingerprint(standard) {
			continue
		}
		if !p.emitDrift(ctx, newPollDelivery(PollChangedKind, standard)) {
			return false
		}
		p.seen[id] = standard
	}
	return true
}

func (p *Poller) emitDrift(ctx context.Context, delivery PollDelivery) bool {
	select {
	case p.eventChan <- delivery:
	case <-ctx.Done():
		return false
	}
	select {
	case persisted := <-delivery.acknowledged:
		return persisted
	case <-ctx.Done():
		return false
	}
}

// eventFingerprint 是漂移检测的内容指纹:类型/细目/球队/分钟/球员任一变化
// 都算漂移(信息性信号,运营与稳定窗自行判读)。
func eventFingerprint(e *event.StandardEvent) string {
	return strings.Join([]string{e.Type, e.Detail, e.Team, strconv.Itoa(e.Minute), e.Player.Name}, "|")
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
		Detail:    e.Detail,
		Team:      e.Team.Name,
		Minute:    e.Time.Elapsed,
		Player:    event.PlayerInfo{ID: e.Player.ID, Name: e.Player.Name},
		Score:     event.ScoreInfo{Home: homeScore, Away: awayScore},
		Timestamp: time.Now(),
	}
}

func normalizeType(eventType, detail string) string {
	detailLower := strings.ToLower(detail)
	switch eventType {
	case "Goal":
		return "goal"
	case "Card":
		if detail == "Red Card" || detail == "Second Yellow card" {
			return "red_card"
		}
		return "yellow_card"
	case "Var":
		// VAR 结果细分(auto-hosting 2.1):取消结论映射 correction 语义类,
		// 确认结论映射 var_result;其余(判罚 VAR/加牌)保持 var_check。
		// goal_cancelled/var_result 的账本引用(RevisionOf)由 Manager 映射。
		if strings.Contains(detailLower, "goal") {
			if strings.Contains(detailLower, "cancel") {
				return "goal_cancelled"
			}
			if strings.Contains(detailLower, "confirm") {
				return "var_result"
			}
		}
		return "var_check"
	case "subst":
		return "substitution"
	default:
		return eventType
	}
}

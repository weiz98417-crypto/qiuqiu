package conversation

import (
	"context"
	"sync"
	"time"
)

type Urgency int

const (
	UrgencyNormal Urgency = iota
	UrgencyCritical
)

type Config struct {
	PlaybackTimeout time.Duration
}

type Playback func(traceID string)

type Job func(ctx context.Context, playback Playback)

type Scheduler struct {
	ctx      context.Context
	cancel   context.CancelFunc
	config   Config
	commands chan request
	events   chan schedulerEvent
	done     chan struct{}
	close    sync.Once
}

type request struct {
	user     bool
	key      string
	urgency  Urgency
	expires  time.Time
	job      Job
	accepted chan struct{}
}

type activeTurn struct {
	id      uint64
	request request
	cancel  context.CancelFunc
	traceID string
	done    bool
	timer   *time.Timer
}

type eventKind int

const (
	eventTurnFinished eventKind = iota
	eventPlaybackRegistered
	eventPlaybackChanged
	eventInterrupt
	eventPlaybackTimedOut
)

type schedulerEvent struct {
	kind    eventKind
	turnID  uint64
	traceID string
	state   string
}

func NewScheduler(parent context.Context, config Config) *Scheduler {
	if config.PlaybackTimeout <= 0 {
		config.PlaybackTimeout = 90 * time.Second
	}
	ctx, cancel := context.WithCancel(parent)
	scheduler := &Scheduler{
		ctx:      ctx,
		cancel:   cancel,
		config:   config,
		commands: make(chan request, 16),
		events:   make(chan schedulerEvent, 32),
		done:     make(chan struct{}),
	}
	go scheduler.run()
	return scheduler
}

func (s *Scheduler) SubmitUser(job Job) {
	s.submit(request{user: true, job: job})
}

func (s *Scheduler) SubmitProactive(key string, urgency Urgency, ttl time.Duration, job Job) {
	s.submit(request{key: key, urgency: urgency, expires: time.Now().Add(ttl), job: job})
}

func (s *Scheduler) PlaybackChanged(traceID, state string) {
	s.emit(schedulerEvent{kind: eventPlaybackChanged, traceID: traceID, state: state})
}

func (s *Scheduler) Interrupt() {
	s.emit(schedulerEvent{kind: eventInterrupt})
}

func (s *Scheduler) Close() {
	s.close.Do(s.cancel)
	<-s.done
}

func (s *Scheduler) submit(req request) {
	req.accepted = make(chan struct{})
	select {
	case s.commands <- req:
	case <-s.ctx.Done():
		return
	}
	select {
	case <-req.accepted:
	case <-s.ctx.Done():
	}
}

func (s *Scheduler) emit(event schedulerEvent) {
	select {
	case s.events <- event:
	case <-s.ctx.Done():
	}
}

func (s *Scheduler) run() {
	defer close(s.done)
	var pending []request
	var active *activeTurn
	var nextTurnID uint64
	seenKeys := make(map[string]time.Time)

	startNext := func() {
		if active != nil {
			return
		}
		var next request
		for len(pending) > 0 {
			next = pending[0]
			pending = pending[1:]
			if next.user || next.expires.IsZero() || next.expires.After(time.Now()) {
				break
			}
			delete(seenKeys, next.key)
			next = request{}
		}
		if next.job == nil {
			return
		}
		turnCtx, turnCancel := context.WithCancel(s.ctx)
		nextTurnID++
		turnID := nextTurnID
		active = &activeTurn{id: turnID, request: next, cancel: turnCancel}
		go func() {
			next.job(turnCtx, func(traceID string) {
				if traceID != "" {
					s.emit(schedulerEvent{kind: eventPlaybackRegistered, turnID: turnID, traceID: traceID})
				}
			})
			s.emit(schedulerEvent{kind: eventTurnFinished, turnID: turnID})
		}()
	}
	finishActive := func() {
		if active == nil || !active.done || active.traceID != "" {
			return
		}
		active.cancel()
		if active.timer != nil {
			active.timer.Stop()
		}
		active = nil
		startNext()
	}

	for {
		select {
		case <-s.ctx.Done():
			return
		case req := <-s.commands:
			if req.job == nil {
				close(req.accepted)
				continue
			}
			now := time.Now()
			for key, expires := range seenKeys {
				if !expires.After(now) {
					delete(seenKeys, key)
				}
			}
			if !req.user && req.key != "" {
				if expires, exists := seenKeys[req.key]; exists && expires.After(now) {
					close(req.accepted)
					continue
				}
				seenKeys[req.key] = req.expires
			}
			if req.user {
				if active != nil {
					active.cancel()
					if active.timer != nil {
						active.timer.Stop()
					}
					active = nil
				}
				pending = append([]request{req}, pending...)
			} else {
				insertAt := len(pending)
				for index, queued := range pending {
					if !queued.user && queued.urgency < req.urgency {
						insertAt = index
						break
					}
				}
				pending = append(pending, request{})
				copy(pending[insertAt+1:], pending[insertAt:])
				pending[insertAt] = req
			}
			close(req.accepted)
			startNext()
		case event := <-s.events:
			switch event.kind {
			case eventPlaybackRegistered:
				if active != nil && active.id == event.turnID {
					active.traceID = event.traceID
					turnID := active.id
					traceID := event.traceID
					active.timer = time.AfterFunc(s.config.PlaybackTimeout, func() {
						s.emit(schedulerEvent{kind: eventPlaybackTimedOut, turnID: turnID, traceID: traceID})
					})
				}
			case eventTurnFinished:
				if active != nil && active.id == event.turnID {
					active.done = true
					finishActive()
				}
			case eventPlaybackChanged:
				if active != nil && active.traceID == event.traceID && event.state != "started" {
					active.traceID = ""
					if active.timer != nil {
						active.timer.Stop()
						active.timer = nil
					}
					finishActive()
				}
			case eventInterrupt:
				if active != nil {
					active.cancel()
					active.traceID = ""
					if active.timer != nil {
						active.timer.Stop()
						active.timer = nil
					}
					finishActive()
				}
			case eventPlaybackTimedOut:
				if active != nil && active.id == event.turnID && active.traceID == event.traceID {
					active.traceID = ""
					active.timer = nil
					finishActive()
				}
			}
		}
	}
}

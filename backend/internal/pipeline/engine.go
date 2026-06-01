package pipeline

import (
	"context"
	"log"
	"qiuqiu/internal/event"
	"time"
)

// Engine is the event processing pipeline.
type Engine struct {
	deduper   *Deduper
	throttler *Throttler
	queue     *PriorityQueue
	cooldown  *Cooldown
	enricher  *ContextEnricher

	eventChan chan *event.StandardEvent // buffer=1024
	outputCh  chan *AIGenerationInstruction
}

// AIGenerationInstruction is the output of the pipeline.
type AIGenerationInstruction struct {
	InstructionType string
	Event           *event.StandardEvent
	MatchContext    *EnrichedContext
	Expression      string
	Priority        int
}

func NewEngine(deduper *Deduper, throttler *Throttler, enricher *ContextEnricher) *Engine {
	return &Engine{
		deduper:   deduper,
		throttler: throttler,
		queue:     NewPriorityQueue(),
		cooldown:  NewCooldown(),
		enricher:  enricher,
		eventChan: make(chan *event.StandardEvent, 1024),
		outputCh:  make(chan *AIGenerationInstruction, 64),
	}
}

// Output returns the output channel for downstream consumers.
func (e *Engine) Output() <-chan *AIGenerationInstruction {
	return e.outputCh
}

// EventChan returns the input channel for pushing events from external sources.
func (e *Engine) EventChan() chan<- *event.StandardEvent {
	return e.eventChan
}

// Push adds an event to the pipeline.
func (e *Engine) Push(ev *event.StandardEvent) {
	select {
	case e.eventChan <- ev:
	default:
		log.Printf("pipeline: event channel full, dropping P%d event", ev.Priority())
	}
}

// Run starts the pipeline processing loop.
func (e *Engine) Run(ctx context.Context) {
	for {
		select {
		case ev := <-e.eventChan:
			e.processEvent(ev)
		case <-ctx.Done():
			return
		default:
			// Check for pending events in queue after cooldown
			if e.queue.Len() > 0 && !e.cooldown.IsActive() {
				ev := e.queue.PopEvent()
				if ev != nil {
					e.emitInstruction(ev)
				}
			}
			time.Sleep(50 * time.Millisecond) // prevent busy loop
		}
	}
}

func (e *Engine) processEvent(ev *event.StandardEvent) {
	// 1. Dedup
	if e.deduper.IsDuplicate(ev) {
		return
	}

	// 2. Throttle
	if e.throttler.ShouldThrottle(ev) {
		return
	}

	// 3. Priority: P0 immediately, others queue
	if ev.Priority() > 0 {
		e.queue.PushEvent(ev)
		return
	}

	// 4. Cooldown check (P0 skips)
	if ev.Priority() == 0 && e.cooldown.IsActive() {
		e.cooldown.ForceBreak()
	}

	e.emitInstruction(ev)
}

func (e *Engine) emitInstruction(ev *event.StandardEvent) {
	ctx := e.enricher.Enrich(ev)

	inst := &AIGenerationInstruction{
		InstructionType: "event_reaction",
		Event:           ev,
		MatchContext:    ctx,
		Expression:      mapExpression(ev.Type),
		Priority:        ev.Priority(),
	}

	e.cooldown.Record()
	select {
	case e.outputCh <- inst:
	default:
		log.Printf("pipeline: output channel full, dropping instruction")
	}
}

func (e *Engine) SetCooldown(d time.Duration) {
	e.cooldown.SetBase(d)
}

func mapExpression(eventType string) string {
	switch eventType {
	case "goal", "penalty":
		return "excited"
	case "red_card":
		return "nervous"
	case "yellow_card", "foul":
		return "tease"
	case "shot":
		return "nervous"
	case "match_end":
		return "normal"
	case "match_start":
		return "normal"
	default:
		return "normal"
	}
}

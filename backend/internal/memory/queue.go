package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"qiuqiu/internal/privacy"
)

// Audit reason codes persisted to memory_extraction_audit for every extraction
// decision (ADR-0006: the fact-first culture applies to memory too).
const (
	ReasonAccepted            = "accepted"
	ReasonRejectedInvalid     = "rejected_invalid"
	ReasonRejectedLowImport   = "rejected_low_importance"
	ReasonBacklogged          = "backlogged"
	ReasonBacklogPutFailed    = "backlog_put_failed"
	ReasonReplayedFromBacklog = "replayed_from_backlog"
	ReasonPayloadMarshal      = "payload_marshal_failed"
)

// ExtractionAudit is one auditable extraction decision. The importance and
// ledger sequence are the enqueue-time values; Memobase never rewrites them.
type ExtractionAudit struct {
	MomentID       string
	UserID         string
	Kind           MomentKind
	Importance     float64
	LedgerSequence int64
	ReasonCode     string
	Detail         string
	CreatedAt      time.Time
}

// ReflectionAudit records one reflection beat (post-match or idle): what the
// portrait refresh observed and which ledger sequences it cites.
type ReflectionAudit struct {
	UserID         string
	MatchID        string
	Trigger        string
	Insight        string
	CitedSequences []int64
	Status         string
	Detail         string
	CreatedAt      time.Time
}

// AuditSink persists extraction decisions locally.
type AuditSink interface {
	RecordExtraction(ctx context.Context, entry ExtractionAudit) error
}

// ReflectionSink persists reflection beats locally.
type ReflectionSink interface {
	RecordReflection(ctx context.Context, entry ReflectionAudit) error
}

// BacklogEntry is a serialized chat blob waiting for Memobase to come back.
type BacklogEntry struct {
	ID            int64
	UserID        string
	Payload       []byte
	Attempts      int
	NextAttemptAt time.Time
}

// BacklogStore is the local table Memobase outages drain into.
// The method is PutBacklog (not Put) so one concrete type can implement both
// BacklogStore and PortraitOverlayStore, whose Put has a different signature.
type BacklogStore interface {
	PutBacklog(ctx context.Context, entry BacklogEntry) (int64, error)
	Due(ctx context.Context, limit int) ([]BacklogEntry, error)
	MarkReplayed(ctx context.Context, id int64) error
	// MarkFailed stores the incremented attempt count; the store flips the
	// row to a terminal status after MaxBacklogAttempts.
	MarkFailed(ctx context.Context, id int64, attempts int, nextAttemptAt time.Time, lastError string) error
}

// MaxBacklogAttempts bounds backlog retries before a row is parked as failed.
const MaxBacklogAttempts = 10

// Queue fronts a Memobase adapter with the async write path from ADR-0006:
// Observe never blocks a watch turn (bounded channel, drainer goroutine,
// exponential backoff) and outages drain into the local backlog. The
// open-thread ledger (C2) stays local: Threads/AppendThread delegate to the
// ThreadStore behind WithThreads instead of Memobase.
type Queue struct {
	adapter     *Memobase
	audit       AuditSink
	reflections ReflectionSink
	backlog     BacklogStore
	threads     ThreadStore
	portraits   PortraitOverlayStore

	items     chan enqueueItem
	dropped   atomic.Int64
	backoff   func(attempt int) time.Duration
	maxAttempts int
	backlogBatch int

	pendingMu      sync.Mutex
	citations      map[string][]int64
	recentMatchEnds map[string]time.Time

	// Console health (ADR-0008 overview memory cell): a bounded tail of the
	// extraction decisions recorded by this process plus a live count of the
	// moments currently waiting in the Memobase outage backlog.
	healthMu       sync.Mutex
	auditTail      []AuditRow
	backlogPending atomic.Int64
}

type enqueueItem struct {
	moment     Moment
	reasonCode string // empty means proceed to extraction
}

// QueueOption tunes the drainer (tests shrink the backoff).
type QueueOption func(*Queue)

// WithBackoff overrides the retry delay function (attempt is 1-based).
func WithBackoff(delay func(attempt int) time.Duration) QueueOption {
	return func(q *Queue) {
		if delay != nil {
			q.backoff = delay
		}
	}
}

// WithMaxAttempts bounds per-moment retries before backlogging (minimum 1).
func WithMaxAttempts(attempts int) QueueOption {
	return func(q *Queue) {
		if attempts >= 1 {
			q.maxAttempts = attempts
		}
	}
}

// WithBacklogBatch sets how many due backlog rows one drain cycle replays.
func WithBacklogBatch(size int) QueueOption {
	return func(q *Queue) {
		if size >= 1 {
			q.backlogBatch = size
		}
	}
}

// WithReflections attaches the reflection audit sink.
func WithReflections(sink ReflectionSink) QueueOption {
	return func(q *Queue) {
		if sink != nil {
			q.reflections = sink
		}
	}
}

// WithThreads attaches the local open-thread store (C2). When absent,
// Threads degrades to the adapter (ErrNotSupported on Memobase).
func WithThreads(store ThreadStore) QueueOption {
	return func(q *Queue) {
		if store != nil {
			q.threads = store
		}
	}
}

// WithPortraitOverlays attaches the local portrait override store (C3, the
// 球球懂我 page). When absent, Portrait still works but user edits and
// deletion tombstones have nowhere to live, so the C3 mutations report
// ErrNotSupported.
func WithPortraitOverlays(store PortraitOverlayStore) QueueOption {
	return func(q *Queue) {
		if store != nil {
			q.portraits = store
		}
	}
}

// NewQueue wires the async pipeline. audit and backlog may be nil (dev mode
// without Postgres): observations still flow but decisions are only logged.
func NewQueue(adapter *Memobase, audit AuditSink, backlog BacklogStore, options ...QueueOption) *Queue {
	queue := &Queue{
		adapter:         adapter,
		audit:           audit,
		backlog:         backlog,
		items:           make(chan enqueueItem, queueCapacity),
		backoff:         DefaultBackoff,
		maxAttempts:     3,
		backlogBatch:    20,
		citations:       make(map[string][]int64),
		recentMatchEnds: make(map[string]time.Time),
	}
	for _, option := range options {
		if option != nil {
			option(queue)
		}
	}
	return queue
}

// queueCapacity bounds the in-flight observation buffer; overflow is counted
// and logged, never allowed to block a turn.
const queueCapacity = 256

// DefaultBackoff: immediate first try, then 500ms, 1s, 2s, ... capped at 30s.
func DefaultBackoff(attempt int) time.Duration {
	if attempt <= 1 {
		return 0
	}
	delay := 500 * time.Millisecond
	for attempt > 2 && delay < 30*time.Second {
		delay *= 2
		attempt--
	}
	if delay > 30*time.Second {
		delay = 30 * time.Second
	}
	return delay
}

// BacklogRetryDelay paces backlog replay: 30s, 1m, 2m, ... capped at 10m.
func BacklogRetryDelay(attempts int) time.Duration {
	if attempts <= 0 {
		return 30 * time.Second
	}
	delay := 30 * time.Second
	for attempts > 1 && delay < 10*time.Minute {
		delay *= 2
		attempts--
	}
	if delay > 10*time.Minute {
		delay = 10 * time.Minute
	}
	return delay
}

// Observe enqueues without ever touching the network or the database on the
// turn path; validation decisions are audited by the drainer instead.
func (q *Queue) Observe(_ context.Context, moment Moment) error {
	if q == nil || !q.adapter.Configured() {
		return nil
	}
	if strings.TrimSpace(moment.UserID) == "" || strings.TrimSpace(moment.Content) == "" {
		q.enqueue(enqueueItem{moment: moment, reasonCode: ReasonRejectedInvalid})
		return nil
	}
	moment.Importance = clamp01(moment.Importance)
	if moment.OccurredAt.IsZero() {
		moment.OccurredAt = time.Now().UTC()
	}
	if moment.Importance < MinExtractionImportance {
		q.enqueue(enqueueItem{moment: moment, reasonCode: ReasonRejectedLowImport})
		return nil
	}
	if moment.LedgerSequence > 0 {
		q.trackCitation(moment.UserID, moment.LedgerSequence)
	}
	select {
	case q.items <- enqueueItem{moment: moment}:
	default:
		q.dropped.Add(1)
		log.Printf("memory: observation queue full, dropped moment for user %q (total dropped %d)", moment.UserID, q.dropped.Load())
	}
	return nil
}

func (q *Queue) enqueue(item enqueueItem) {
	select {
	case q.items <- item:
	default:
		q.dropped.Add(1)
	}
}

// Recall degrades transparently: the adapter returns nil while unreachable,
// and callers keep read_recent.
func (q *Queue) Recall(ctx context.Context, query Query) []Recall {
	if q == nil || !q.adapter.Configured() {
		return nil
	}
	return q.adapter.Recall(ctx, query)
}

// Portrait is the single read path behind prompt injection: synthesis layered
// with the user's local edits, tombstones applied. It keeps the ErrUnavailable
// signal when neither Memobase nor any local overlay can contribute, so
// callers skip the block.
func (q *Queue) Portrait(ctx context.Context, userID string) (Portrait, error) {
	if q == nil {
		return Portrait{}, ErrUnavailable
	}
	portrait := q.assemblePortrait(ctx, userID)
	if portrait.Block == "" && !q.adapter.Configured() {
		return Portrait{}, ErrUnavailable
	}
	return portrait, nil
}

// PortraitEntries is the user-page read of the same assembled portrait. It
// never fails: whatever survives degradation is shown.
func (q *Queue) PortraitEntries(ctx context.Context, userID string) ([]PortraitEntry, time.Time) {
	portrait := q.assemblePortrait(ctx, userID)
	return portrait.Entries, portrait.UpdatedAt
}

// assemblePortrait merges the Memobase synthesis with the local overlay
// layer. Every failure degrades to "less portrait", never to an error: the
// privacy tombstone hides everything, an unreachable adapter falls back to
// overlay-only entries, an unreachable overlay store falls back to synthesis.
func (q *Queue) assemblePortrait(ctx context.Context, userID string) Portrait {
	if q == nil || strings.TrimSpace(userID) == "" {
		return Portrait{}
	}
	if q.portraits != nil {
		switch err := q.portraits.Check(ctx, userID); {
		case errors.Is(err, privacy.ErrDataDeleted), errors.Is(err, privacy.ErrDeletionInProgress):
			// Privacy lifecycle: the portrait vanishes from the very next
			// turn, regardless of what Memobase still holds.
			return Portrait{}
		case err != nil:
			log.Printf("memory: portrait privacy check for %q: %v", userID, err)
		}
	}
	var entries []PortraitEntry
	var updatedAt time.Time
	if portrait, err := q.adapter.Portrait(ctx, userID); err == nil {
		entries = portrait.Entries
		updatedAt = portrait.UpdatedAt
	}
	if q.portraits != nil {
		overlays, err := q.portraits.List(ctx, userID)
		if err != nil {
			log.Printf("memory: portrait overlays for %q: %v", userID, err)
		} else {
			for _, overlay := range overlays {
				if !overlay.Deleted && overlay.UpdatedAt.After(updatedAt) {
					updatedAt = overlay.UpdatedAt
				}
			}
			entries = ResolvePortrait(entries, overlays)
		}
	}
	if len(entries) == 0 {
		return Portrait{}
	}
	return Portrait{
		Block:     RenderPortraitBlock(entries, updatedAt),
		Entries:   entries,
		UpdatedAt: updatedAt,
	}
}

// SetPortraitEntry records the user's edit for one profile slot and
// best-effort forwards it to Memobase when the synthesis entry id is known.
// The local overlay decides what the next turn sees; the remote sync only
// helps the synthesis layer converge.
func (q *Queue) SetPortraitEntry(ctx context.Context, userID, topic, subTopic, content, entryID string) (PortraitEntry, error) {
	if q == nil || q.portraits == nil {
		return PortraitEntry{}, ErrNotSupported
	}
	overlay, err := q.portraits.Put(ctx, userID, topic, subTopic, content)
	if err != nil {
		return PortraitEntry{}, err
	}
	q.syncProfileEntry(ctx, userID, entryID, func(ctx context.Context, mutator ProfileEntryMutator) error {
		return mutator.UpdateProfileEntry(ctx, userID, entryID, topic, subTopic, content)
	})
	return PortraitEntry{
		ID:        entryID,
		Topic:     overlay.Topic,
		SubTopic:  overlay.SubTopic,
		Content:   overlay.Content,
		UpdatedAt: overlay.UpdatedAt,
		Source:    PortraitSourceUser,
	}, nil
}

// ForgetPortraitEntry tombstones one profile slot. The tombstone is local and
// permanent (re-extraction cannot resurrect it into a prompt); the Memobase
// slot is deleted best-effort when its id is known.
func (q *Queue) ForgetPortraitEntry(ctx context.Context, userID, topic, subTopic, entryID string) error {
	if q == nil || q.portraits == nil {
		return ErrNotSupported
	}
	if entryID == "" {
		if portrait, err := q.adapter.Portrait(ctx, userID); err == nil {
			for _, entry := range portrait.Entries {
				if entry.Topic == topic && entry.SubTopic == subTopic {
					entryID = entry.ID
					break
				}
			}
		}
	}
	if err := q.portraits.Delete(ctx, userID, topic, subTopic); err != nil {
		return err
	}
	q.syncProfileEntry(ctx, userID, entryID, func(ctx context.Context, mutator ProfileEntryMutator) error {
		return mutator.DeleteProfileEntry(ctx, userID, entryID)
	})
	return nil
}

// ForgetPortrait forgets every slot at once (whole-portrait delete). It
// tombstones synthesis slots and user-created overlay slots alike.
func (q *Queue) ForgetPortrait(ctx context.Context, userID string) error {
	if q == nil || q.portraits == nil {
		return ErrNotSupported
	}
	portrait, _ := q.adapter.Portrait(ctx, userID)
	seen := make(map[string]bool, len(portrait.Entries))
	for _, entry := range portrait.Entries {
		key := portraitKey(entry.Topic, entry.SubTopic)
		seen[key] = true
		if err := q.portraits.Delete(ctx, userID, entry.Topic, entry.SubTopic); err != nil {
			return err
		}
		if entry.ID != "" {
			q.syncProfileEntry(ctx, userID, entry.ID, func(ctx context.Context, mutator ProfileEntryMutator) error {
				return mutator.DeleteProfileEntry(ctx, userID, entry.ID)
			})
		}
	}
	overlays, err := q.portraits.List(ctx, userID)
	if err != nil {
		return nil
	}
	for _, overlay := range overlays {
		if overlay.Deleted {
			continue
		}
		if seen[portraitKey(overlay.Topic, overlay.SubTopic)] {
			continue
		}
		if err := q.portraits.Delete(ctx, userID, overlay.Topic, overlay.SubTopic); err != nil {
			return err
		}
	}
	return nil
}

// syncProfileEntry forwards a user mutation to Memobase through the
// ProfileEntryMutator seam; failures are logged, never surfaced — the overlay
// write above already fixed what the next turn sees.
func (q *Queue) syncProfileEntry(ctx context.Context, userID, entryID string, mutate func(context.Context, ProfileEntryMutator) error) {
	if entryID == "" || q.adapter == nil || !q.adapter.Configured() {
		return
	}
	syncCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	// The Memobase adapter is the concrete ProfileEntryMutator implementor;
	// a type assertion here would not even compile against the concrete type.
	if err := mutate(syncCtx, q.adapter); err != nil {
		log.Printf("memory: sync portrait entry %q for user %q: %v", entryID, userID, err)
	}
}

func (q *Queue) Threads(ctx context.Context, userID string) ([]Thread, error) {
	if q == nil {
		return nil, ErrNotSupported
	}
	if q.threads != nil {
		return q.threads.OpenThreads(ctx, userID)
	}
	return q.adapter.Threads(ctx, userID)
}

// OpenThreads satisfies ThreadStore alongside Threads: without a local store
// the queue has nowhere to list threads from, so it degrades to
// ErrNotSupported (the Memobase adapter cannot list threads either).
func (q *Queue) OpenThreads(ctx context.Context, userID string) ([]Thread, error) {
	if q == nil || q.threads == nil {
		return nil, ErrNotSupported
	}
	return q.threads.OpenThreads(ctx, userID)
}

// AppendThread inserts one open-thread candidate via the local store; the
// write is best-effort on the caller side and never blocks a turn.
func (q *Queue) AppendThread(ctx context.Context, thread Thread) (Thread, error) {
	if q == nil || q.threads == nil {
		return Thread{}, ErrNotSupported
	}
	return q.threads.AppendThread(ctx, thread)
}

// MarkThreadAddressed closes one open thread as answered.
func (q *Queue) MarkThreadAddressed(ctx context.Context, threadID string) error {
	if q == nil || q.threads == nil {
		return ErrNotSupported
	}
	return q.threads.MarkThreadAddressed(ctx, threadID)
}

// ExpireStaleThreads ages out open threads past the TTL and returns the
// expired rows for the audit trail.
func (q *Queue) ExpireStaleThreads(ctx context.Context, now time.Time, ttl time.Duration) ([]Thread, error) {
	if q == nil || q.threads == nil {
		return nil, ErrNotSupported
	}
	if ttl <= 0 {
		ttl = DefaultThreadTTL
	}
	return q.threads.ExpireStaleThreads(ctx, now, ttl)
}

// Run drains the queue until the context is canceled (single goroutine).
func (q *Queue) Run(ctx context.Context) {
	if q == nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case item := <-q.items:
			q.process(ctx, item)
		}
	}
}

func (q *Queue) process(ctx context.Context, item enqueueItem) {
	if item.reasonCode != "" {
		q.recordAudit(ctx, rejectionAudit(item.moment, item.reasonCode))
		return
	}
	moment := item.moment
	var lastErr error
	for attempt := 1; attempt <= q.maxAttempts; attempt++ {
		if delay := q.backoff(attempt); delay > 0 && !q.sleep(ctx, delay) {
			return
		}
		if ctx.Err() != nil {
			return
		}
		if err := q.adapter.Observe(ctx, moment); err != nil {
			lastErr = err
			continue
		}
		if err := q.adapter.Flush(ctx, moment.UserID); err != nil {
			lastErr = err
			continue
		}
		q.recordAudit(ctx, ExtractionAudit{
			MomentID:       momentID(moment),
			UserID:         moment.UserID,
			Kind:           moment.Kind,
			Importance:     moment.Importance,
			LedgerSequence: moment.LedgerSequence,
			ReasonCode:     ReasonAccepted,
			CreatedAt:      time.Now().UTC(),
		})
		q.drainBacklog(ctx)
		return
	}
	q.backlogMoment(ctx, moment, lastErr)
}

func (q *Queue) backlogMoment(ctx context.Context, moment Moment, cause error) {
	audit := ExtractionAudit{
		MomentID:       momentID(moment),
		UserID:         moment.UserID,
		Kind:           moment.Kind,
		Importance:     moment.Importance,
		LedgerSequence: moment.LedgerSequence,
		ReasonCode:     ReasonBacklogged,
		Detail:         errorText(cause),
		CreatedAt:      time.Now().UTC(),
	}
	if q.backlog == nil {
		q.recordAudit(ctx, audit)
		return
	}
	payload, err := q.adapter.chatBlobPayload(moment)
	if err != nil {
		audit.ReasonCode = ReasonPayloadMarshal
		audit.Detail = err.Error()
		q.recordAudit(ctx, audit)
		return
	}
	if _, err := q.backlog.PutBacklog(ctx, BacklogEntry{UserID: moment.UserID, Payload: payload}); err != nil {
		audit.ReasonCode = ReasonBacklogPutFailed
		audit.Detail = err.Error()
	} else {
		// Live backlog depth for Health(): the row now waits for Memobase.
		q.backlogPending.Add(1)
	}
	q.recordAudit(ctx, audit)
}

// drainBacklog replays due rows after a successful flush; replay failures
// re-schedule with BacklogRetryDelay until MaxBacklogAttempts.
func (q *Queue) drainBacklog(ctx context.Context) {
	if q.backlog == nil {
		return
	}
	entries, err := q.backlog.Due(ctx, q.backlogBatch)
	if err != nil {
		log.Printf("memory: read backlog: %v", err)
		return
	}
	for _, entry := range entries {
		if err := q.adapter.insertPayload(ctx, entry.UserID, entry.Payload); err != nil {
			q.failBacklog(ctx, entry, err)
			continue
		}
		if err := q.adapter.Flush(ctx, entry.UserID); err != nil {
			q.failBacklog(ctx, entry, err)
			continue
		}
		if err := q.backlog.MarkReplayed(ctx, entry.ID); err != nil {
			log.Printf("memory: mark backlog %d replayed: %v", entry.ID, err)
		} else {
			q.backlogPending.Add(-1)
		}
		kind, importance, sequence := decodeMomentFields(entry.Payload)
		q.recordAudit(ctx, ExtractionAudit{
			MomentID:       fmt.Sprintf("backlog:%d", entry.ID),
			UserID:         entry.UserID,
			Kind:           kind,
			Importance:     importance,
			LedgerSequence: sequence,
			ReasonCode:     ReasonReplayedFromBacklog,
			CreatedAt:      time.Now().UTC(),
		})
	}
}

func (q *Queue) failBacklog(ctx context.Context, entry BacklogEntry, cause error) {
	attempts := entry.Attempts + 1
	if err := q.backlog.MarkFailed(ctx, entry.ID, attempts, time.Now().UTC().Add(BacklogRetryDelay(attempts)), errorText(cause)); err != nil {
		log.Printf("memory: mark backlog %d failed: %v", entry.ID, err)
	}
}

func (q *Queue) recordAudit(ctx context.Context, entry ExtractionAudit) {
	if q == nil {
		return
	}
	q.rememberAuditTail(entry)
	if q.audit == nil {
		return
	}
	auditCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := q.audit.RecordExtraction(auditCtx, entry); err != nil {
		log.Printf("memory: record extraction audit: %v", err)
	}
}

// rememberAuditTail keeps the last healthAuditTail decisions in-process so
// Health() can expose the audit tail read-only (ADR-0008), with or without a
// database-backed audit sink.
func (q *Queue) rememberAuditTail(entry ExtractionAudit) {
	q.healthMu.Lock()
	defer q.healthMu.Unlock()
	q.auditTail = append(q.auditTail, AuditRow{
		MomentID:   entry.MomentID,
		UserID:     entry.UserID,
		Kind:       entry.Kind,
		ReasonCode: entry.ReasonCode,
		Detail:     entry.Detail,
		CreatedAt:  entry.CreatedAt,
	})
	if len(q.auditTail) > healthAuditTail {
		q.auditTail = q.auditTail[len(q.auditTail)-healthAuditTail:]
	}
}

func rejectionAudit(moment Moment, reasonCode string) ExtractionAudit {
	return ExtractionAudit{
		MomentID:       momentID(moment),
		UserID:         moment.UserID,
		Kind:           moment.Kind,
		Importance:     moment.Importance,
		LedgerSequence: moment.LedgerSequence,
		ReasonCode:     reasonCode,
		CreatedAt:      time.Now().UTC(),
	}
}

// ReflectNow runs one reflection beat for a user: flush pending extractions,
// refresh the portrait, and persist an audit record citing the ledger
// sequences observed since the previous beat.
func (q *Queue) ReflectNow(ctx context.Context, userID, matchID, trigger string) (Portrait, error) {
	if q == nil || !q.adapter.Configured() {
		return Portrait{}, ErrUnavailable
	}
	cited := q.takeCitations(userID)
	status := "refreshed"
	detail := ""
	if err := q.adapter.Flush(ctx, userID); err != nil {
		status = "flush_failed"
		detail = errorText(err)
	}
	portrait, portraitErr := q.adapter.Portrait(ctx, userID)
	if portraitErr != nil {
		if status == "refreshed" {
			status = "profile_unavailable"
		}
		detail = strings.TrimSpace(detail + " " + errorText(portraitErr))
	}
	if q.reflections != nil {
		reflectionCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		err := q.reflections.RecordReflection(reflectionCtx, ReflectionAudit{
			UserID:         userID,
			MatchID:        matchID,
			Trigger:        trigger,
			Insight:        firstPortraitLines(portrait.Block),
			CitedSequences: cited,
			Status:         status,
			Detail:         detail,
			CreatedAt:      time.Now().UTC(),
		})
		cancel()
		if err != nil {
			log.Printf("memory: record reflection audit: %v", err)
		}
	}
	return portrait, portraitErr
}

// NotifyMatchEnded records a finished match so the next reflection beat can
// run the post-match pass; safe to call from event callbacks.
func (q *Queue) NotifyMatchEnded(matchID string) {
	if q == nil || strings.TrimSpace(matchID) == "" {
		return
	}
	q.pendingMu.Lock()
	q.recentMatchEnds[matchID] = time.Now().UTC()
	q.pendingMu.Unlock()
}

// TakeMatchEnds returns and clears recently ended match IDs.
func (q *Queue) TakeMatchEnds() []string {
	if q == nil {
		return nil
	}
	q.pendingMu.Lock()
	defer q.pendingMu.Unlock()
	ended := make([]string, 0, len(q.recentMatchEnds))
	for matchID := range q.recentMatchEnds {
		ended = append(ended, matchID)
	}
	q.recentMatchEnds = make(map[string]time.Time)
	return ended
}

// ActiveUsers lists users with observations since their last reflection.
func (q *Queue) ActiveUsers() []string {
	if q == nil {
		return nil
	}
	q.pendingMu.Lock()
	defer q.pendingMu.Unlock()
	users := make([]string, 0, len(q.citations))
	for userID := range q.citations {
		users = append(users, userID)
	}
	return users
}

func (q *Queue) trackCitation(userID string, sequence int64) {
	q.pendingMu.Lock()
	defer q.pendingMu.Unlock()
	q.citations[userID] = append(q.citations[userID], sequence)
	const maxCitations = 64
	if len(q.citations[userID]) > maxCitations {
		q.citations[userID] = q.citations[userID][len(q.citations[userID])-maxCitations:]
	}
}

func (q *Queue) takeCitations(userID string) []int64 {
	q.pendingMu.Lock()
	defer q.pendingMu.Unlock()
	cited := q.citations[userID]
	delete(q.citations, userID)
	if len(cited) > 0 {
		return append([]int64(nil), cited...)
	}
	return nil
}

func (q *Queue) sleep(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// momentID derives a stable audit identifier from the moment content.
func momentID(moment Moment) string {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(moment.UserID + "\x00" + string(moment.Kind) + "\x00" + moment.Content))
	return fmt.Sprintf("%s:%016x", moment.Kind, hash.Sum64())
}

func decodeMomentFields(payload []byte) (MomentKind, float64, int64) {
	var decoded struct {
		Fields struct {
			Kind           string  `json:"kind"`
			Importance     float64 `json:"importance"`
			LedgerSequence int64   `json:"ledger_sequence"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return "", 0, 0
	}
	return MomentKind(decoded.Fields.Kind), decoded.Fields.Importance, decoded.Fields.LedgerSequence
}

func firstPortraitLines(block string) string {
	if block == "" {
		return ""
	}
	lines := strings.Split(block, "\n")
	if len(lines) > 4 {
		lines = lines[:4]
	}
	return strings.Join(lines, "\n")
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

var (
	_ Memories    = (*Queue)(nil)
	_ Memories    = (*Memobase)(nil)
	_ Memories    = (*Fake)(nil)
	_ ThreadStore = (*Queue)(nil)
	_ ThreadStore = (*Fake)(nil)
)

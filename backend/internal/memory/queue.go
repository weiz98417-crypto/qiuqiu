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
// Observe never blocks a watch turn (bounded channel, drainer goroutine) and
// outages drain into the local backlog. The write itself gets exactly one
// inline attempt — the first failure hands the moment to the persistent
// backlog (backlog-first) instead of sleeping the drainer shared by every
// user. The open-thread ledger (C2) stays local: Threads/AppendThread
// delegate to the ThreadStore behind WithThreads instead of Memobase.
type Queue struct {
	adapter      *Memobase
	audit        AuditSink
	reflections  ReflectionSink
	backlog      BacklogStore
	threads      ThreadStore
	portraits    PortraitOverlayStore

	// 向量召回路（openspec/changes/semantic-memory）：双路之一，任何故障
	// 弃权即现状行为。
	vectorStore VectorMomentStore
	embedder    Embedder

	items        chan enqueueItem
	dropped      atomic.Int64
	backlogBatch int

	pendingMu      sync.Mutex
	citations      map[string][]int64
	citationOrder  []string
	recentMatchEnds map[string]time.Time
	// userMatches 是反思归因（reflection-attribution）：每个用户最近互动
	// 过的比赛，插入序、最新的在尾部。只服务 post_match 审计标签，是
	// best-effort 提示而非正确性数据，与 citations 同样的有界纪律。
	userMatches    map[string][]string
	matchOrder     []string

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

// QueueOption tunes the drainer.
type QueueOption func(*Queue)

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

// WithVectorRecall 启用 pgvector 召回路（openspec/changes/semantic-memory）：
// Observe 异步嵌 Moment，Recall 与 contains 路双路合并。store 或 embedder
// 为 nil 即不启用（行为=现状）。
func WithVectorRecall(store VectorMomentStore, embedder Embedder) QueueOption {
	return func(q *Queue) {
		if store != nil && embedder != nil {
			q.vectorStore = store
			q.embedder = embedder
		}
	}
}

// Embedder 是向量召回路的嵌入能力接缝；internal/embedding.Client 结构性
// 满足，测试用桩。
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

// 向量路的两个独立预算：嵌入/检索本地 Ollama 毫秒级，200ms 覆盖抖动；
// 超时即弃权（降级矩阵 Q12）。
const (
	vectorQueryTimeout  = 200 * time.Millisecond
	vectorEmbedTimeout  = 3 * time.Second
)

// NewQueue wires the async pipeline. audit and backlog may be nil (dev mode
// without Postgres): observations still flow but decisions are only logged.
func NewQueue(adapter *Memobase, audit AuditSink, backlog BacklogStore, options ...QueueOption) *Queue {
	queue := &Queue{
		adapter:         adapter,
		audit:           audit,
		backlog:         backlog,
		items:           make(chan enqueueItem, queueCapacity),
		backlogBatch:    20,
		citations:       make(map[string][]int64),
		recentMatchEnds: make(map[string]time.Time),
		userMatches:     make(map[string][]string),
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

// Bounds keeping the drainer's in-process ledgers memory-flat no matter how
// many users or matches a long-running process sees:
const (
	// maxCitationUsers caps how many users may hold untaken citation
	// ledgers at once; the longest-unreflected users are evicted first
	// (citations are best-effort reflection hints, never correctness data).
	maxCitationUsers = 256
	// maxRecentMatchEnds caps the pending match-end ledger the same way;
	// the earliest-ended matches are dropped first.
	maxRecentMatchEnds = 256
	// maxUserMatches bounds the per-user match attribution window (most
	// recent kept).
	maxUserMatches = 8
)

// BacklogRetryDelay paces backlog replay: 30s, 1m, 2m, ... capped at 10m.
// It is the only retry backoff left: the drainer itself writes once and
// hands failures to the backlog, whose replays this curve spaces out.
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
// turn path; validation decisions are audited by the drainer instead. 向量路
// 独立于 Memobase：有效时刻在入队旁路异步嵌写 pgvector（fail-soft）。
func (q *Queue) Observe(_ context.Context, moment Moment) error {
	if q == nil {
		return nil
	}
	empty := strings.TrimSpace(moment.UserID) == "" || strings.TrimSpace(moment.Content) == ""
	moment.Importance = clamp01(moment.Importance)
	if moment.OccurredAt.IsZero() {
		moment.OccurredAt = time.Now().UTC()
	}
	if matchID := strings.TrimSpace(moment.MatchID); !empty && matchID != "" {
		q.rememberUserMatch(moment.UserID, matchID)
	}
	if !empty && moment.Importance >= MinExtractionImportance {
		q.observeVector(moment)
	}
	if !q.adapter.Configured() {
		return nil
	}
	if empty {
		q.enqueue(enqueueItem{moment: moment, reasonCode: ReasonRejectedInvalid})
		return nil
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

// observeVector 异步嵌 Moment 落向量库：失败重试一次后放弃（contains 路
// 已覆盖该内容），绝不阻塞回合路径。
func (q *Queue) observeVector(moment Moment) {
	if q.vectorStore == nil || q.embedder == nil {
		return
	}
	go func() {
		for attempt := 0; attempt < 2; attempt++ {
			embedCtx, cancel := context.WithTimeout(context.Background(), vectorEmbedTimeout)
			vector, err := q.embedder.Embed(embedCtx, moment.Content)
			cancel()
			if err == nil {
				storeCtx, storeCancel := context.WithTimeout(context.Background(), vectorEmbedTimeout)
				err = q.vectorStore.Store(storeCtx, moment, vector)
				storeCancel()
				if err == nil {
					return
				}
			}
			if attempt == 0 {
				time.Sleep(500 * time.Millisecond)
			}
		}
		log.Printf("memory: vector path dropped moment for user %q (contains path still covers it)", moment.UserID)
	}()
}

func (q *Queue) enqueue(item enqueueItem) {
	select {
	case q.items <- item:
	default:
		q.dropped.Add(1)
	}
}

// Recall 双路合并（openspec/changes/semantic-memory）：contains 路（adapter，
// 原样打分排序）+ 向量路（pgvector 余弦），各取一半配额、按 content 去重、
// adapter 路保序在前。embedding 故障时向量路弃权，行为=现状。
func (q *Queue) Recall(ctx context.Context, query Query) []Recall {
	if q == nil {
		return nil
	}
	adapterConfigured := q.adapter.Configured()
	vectorEnabled := q.vectorStore != nil && q.embedder != nil
	if !adapterConfigured && !vectorEnabled {
		return nil
	}
	limit := query.Limit
	if limit <= 0 {
		limit = defaultRecallLimit
	}
	var adapterPath []Recall
	if adapterConfigured {
		adapterPath = q.adapter.Recall(ctx, query)
	}
	var vectorPath []Recall
	if vectorEnabled && strings.TrimSpace(query.Focus) != "" {
		vecCtx, cancel := context.WithTimeout(ctx, vectorQueryTimeout)
		defer cancel()
		if vector, err := q.embedder.Embed(vecCtx, query.Focus); err == nil {
			if vectors, err := q.vectorStore.Search(vecCtx, query.UserID, vector, limit); err != nil {
				log.Printf("memory: vector recall search degraded: %v", err)
			} else {
				vectorPath = vectors
			}
		} else {
			log.Printf("memory: vector recall embed degraded: %v", err)
		}
	}
	// 双路各取一半配额（adapter 保序在前）、按 content 去重；向量路弃权
	// 或空手时 adapter 独享全量配额——弃权=现状，配额不得回退。
	seen := make(map[string]bool, limit)
	merged := make([]Recall, 0, limit)
	adapterQuota := limit
	if len(vectorPath) > 0 {
		adapterQuota = limit - limit/2
	}
	for _, recall := range adapterPath {
		if len(merged) >= adapterQuota {
			break
		}
		if seen[recall.Content] {
			continue
		}
		seen[recall.Content] = true
		merged = append(merged, recall)
	}
	for _, recall := range vectorPath {
		if len(merged) >= limit {
			break
		}
		if seen[recall.Content] {
			continue
		}
		seen[recall.Content] = true
		merged = append(merged, recall)
	}
	return merged
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
// tombstones synthesis slots and user-created overlay slots alike. Failures
// surface honestly: an unreachable adapter means the remote synthesis slots
// cannot be deleted, and reporting that as success would hide a half-done
// privacy deletion — callers map the error to a 5xx. Without a configured
// Memobase (dev) there is no remote half, so the local delete still runs.
func (q *Queue) ForgetPortrait(ctx context.Context, userID string) error {
	if q == nil || q.portraits == nil {
		return ErrNotSupported
	}
	var portrait Portrait
	if q.adapter.Configured() {
		var err error
		if portrait, err = q.adapter.Portrait(ctx, userID); err != nil {
			return err
		}
	}
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
		return err
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

// process writes the moment with exactly one inline attempt (backlog-first):
// the first Observe/Flush failure hands the moment to the persistent backlog
// instead of sleeping this drainer — one user's Memobase jitter must not
// queue everyone else's moments behind a backoff. The backlog channel paces
// its own replays with BacklogRetryDelay; without a backlog store (dev mode
// without Postgres) the failure is audited and dropped after one attempt.
func (q *Queue) process(ctx context.Context, item enqueueItem) {
	if item.reasonCode != "" {
		q.recordAudit(ctx, rejectionAudit(item.moment, item.reasonCode))
		return
	}
	if ctx.Err() != nil {
		return
	}
	moment := item.moment
	if err := q.adapter.Observe(ctx, moment); err != nil {
		q.backlogMoment(ctx, moment, err)
		return
	}
	if err := q.adapter.Flush(ctx, moment.UserID); err != nil {
		q.backlogMoment(ctx, moment, err)
		return
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
// run the post-match pass; safe to call from event callbacks. The ledger is
// bounded: beyond maxRecentMatchEnds the earliest-ended matches are dropped.
func (q *Queue) NotifyMatchEnded(matchID string) {
	if q == nil || strings.TrimSpace(matchID) == "" {
		return
	}
	q.pendingMu.Lock()
	defer q.pendingMu.Unlock()
	q.recentMatchEnds[matchID] = time.Now().UTC()
	for len(q.recentMatchEnds) > maxRecentMatchEnds {
		oldestID := ""
		var oldest time.Time
		for id, endedAt := range q.recentMatchEnds {
			if oldestID == "" || endedAt.Before(oldest) {
				oldestID, oldest = id, endedAt
			}
		}
		delete(q.recentMatchEnds, oldestID)
	}
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

// rememberUserMatch records one match the user interacted under, most recent
// last, bounded per user and across users (same best-effort discipline as
// citations). Repeated observations of the current match are collapsed.
func (q *Queue) rememberUserMatch(userID, matchID string) {
	q.pendingMu.Lock()
	defer q.pendingMu.Unlock()
	matches := q.userMatches[userID]
	if len(matches) > 0 && matches[len(matches)-1] == matchID {
		return
	}
	matches = append(matches, matchID)
	if len(matches) > maxUserMatches {
		matches = matches[len(matches)-maxUserMatches:]
	}
	if _, tracked := q.userMatches[userID]; !tracked {
		q.matchOrder = append(q.matchOrder, userID)
	}
	q.userMatches[userID] = matches
	for len(q.userMatches) > maxCitationUsers {
		oldest := q.matchOrder[0]
		q.matchOrder = q.matchOrder[1:]
		delete(q.userMatches, oldest)
	}
}

// RecentEndedMatchFor returns the most recently interacted match among the
// ones that just ended, or "" when the user watched none of them. The
// reflection beat uses this to label the post-match audit with the match the
// user actually watched instead of an arbitrary ended match.
func (q *Queue) RecentEndedMatchFor(userID string, ended []string) string {
	if q == nil || len(ended) == 0 {
		return ""
	}
	q.pendingMu.Lock()
	defer q.pendingMu.Unlock()
	matches := q.userMatches[userID]
	if len(matches) == 0 {
		return ""
	}
	endedSet := make(map[string]bool, len(ended))
	for _, matchID := range ended {
		endedSet[matchID] = true
	}
	for index := len(matches) - 1; index >= 0; index-- {
		if endedSet[matches[index]] {
			return matches[index]
		}
	}
	return ""
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

// forgetCitationUser drops a user from the insertion-order index once its
// ledger was taken (or evicted); keeps citationOrder as long as the map.
func (q *Queue) forgetCitationUser(userID string) {
	for index, pending := range q.citationOrder {
		if pending == userID {
			q.citationOrder = append(q.citationOrder[:index], q.citationOrder[index+1:]...)
			return
		}
	}
}

func (q *Queue) trackCitation(userID string, sequence int64) {
	q.pendingMu.Lock()
	defer q.pendingMu.Unlock()
	if _, tracked := q.citations[userID]; !tracked {
		// citationOrder preserves insertion order so the bound below evicts
		// the longest-unreflected users instead of a random map entry.
		q.citationOrder = append(q.citationOrder, userID)
	}
	q.citations[userID] = append(q.citations[userID], sequence)
	const maxCitations = 64
	if len(q.citations[userID]) > maxCitations {
		q.citations[userID] = q.citations[userID][len(q.citations[userID])-maxCitations:]
	}
	for len(q.citations) > maxCitationUsers {
		oldest := q.citationOrder[0]
		q.citationOrder = q.citationOrder[1:]
		delete(q.citations, oldest) // no-op if takeCitations already removed it
	}
}

func (q *Queue) takeCitations(userID string) []int64 {
	q.pendingMu.Lock()
	defer q.pendingMu.Unlock()
	cited := q.citations[userID]
	delete(q.citations, userID)
	q.forgetCitationUser(userID)
	if len(cited) > 0 {
		return append([]int64(nil), cited...)
	}
	return nil
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

package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type auditLog struct {
	mu      sync.Mutex
	entries []ExtractionAudit
}

func (l *auditLog) RecordExtraction(_ context.Context, entry ExtractionAudit) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, entry)
	return nil
}

func (l *auditLog) count(reasonCode string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	total := 0
	for _, entry := range l.entries {
		if entry.ReasonCode == reasonCode {
			total++
		}
	}
	return total
}

func (l *auditLog) last() ExtractionAudit {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.entries) == 0 {
		return ExtractionAudit{}
	}
	return l.entries[len(l.entries)-1]
}

type backlogTable struct {
	mu       sync.Mutex
	entries  []BacklogEntry
	nextID   int64
	replayed []int64
	failed   map[int64]int
}

func newBacklogTable() *backlogTable {
	return &backlogTable{failed: make(map[int64]int)}
}

func (b *backlogTable) PutBacklog(_ context.Context, entry BacklogEntry) (int64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextID++
	entry.ID = b.nextID
	b.entries = append(b.entries, entry)
	return entry.ID, nil
}

func (b *backlogTable) Due(_ context.Context, limit int) ([]BacklogEntry, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	due := make([]BacklogEntry, 0, limit)
	for _, entry := range b.entries {
		if len(due) >= limit {
			break
		}
		replayed := false
		for _, id := range b.replayed {
			if id == entry.ID {
				replayed = true
				break
			}
		}
		if !replayed {
			due = append(due, entry)
		}
	}
	return due, nil
}

func (b *backlogTable) MarkReplayed(_ context.Context, id int64) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.replayed = append(b.replayed, id)
	return nil
}

func (b *backlogTable) MarkFailed(_ context.Context, id int64, attempts int, _ time.Time, _ string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failed[id] = attempts
	return nil
}

func (b *backlogTable) size() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.entries)
}

func (b *backlogTable) replayCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.replayed)
}

// stubHandler answers adapter calls; insertFailures counts down remaining
// forced 500s for /blobs/insert paths.
func stubQueueServer(insertFailures *atomic.Int64) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/users/"):
			_, _ = w.Write(memobaseOK(`{}`))
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/v1/blobs/insert/"):
			if insertFailures.Add(-1) >= 0 {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("insert unavailable"))
				return
			}
			_, _ = w.Write(memobaseOK(`"blob-1"`))
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/v1/users/buffer/"):
			_, _ = w.Write(memobaseOK(`null`))
		default:
			_, _ = w.Write(memobaseOK(`null`))
		}
	}))
}

func waitFor(t *testing.T, timeout time.Duration, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}

// TestQueueSingleFailedWriteHandsOffToBacklog pins the backlog-first
// contract: the first Observe failure goes straight to the persistent
// backlog with exactly one adapter attempt — no inline retry, no drainer
// sleep while every other user's moments queue up behind the backoff.
func TestQueueSingleFailedWriteHandsOffToBacklog(t *testing.T) {
	var insertCalls atomic.Int64
	server, _ := stubMemobaseServer(func(r recordedRequest) (int, []byte) {
		switch {
		case r.method == http.MethodPost && strings.HasPrefix(r.path, "/api/v1/blobs/insert/"):
			if insertCalls.Add(1) == 1 {
				return http.StatusInternalServerError, []byte("memobase down")
			}
			return http.StatusOK, memobaseOK(`"blob-1"`)
		case r.method == http.MethodGet && strings.HasPrefix(r.path, "/api/v1/users/"):
			return http.StatusOK, memobaseOK(`{}`)
		default:
			return http.StatusOK, memobaseOK(`null`)
		}
	})
	defer server.Close()
	audit := &auditLog{}
	backlog := newBacklogTable()
	queue := NewQueue(NewMemobase(MemobaseConfig{BaseURL: server.URL, Token: "test-token"}), audit, backlog)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go queue.Run(ctx)
	if err := queue.Observe(ctx, Moment{UserID: "user-1", Kind: MomentUserFact, Content: "我喜欢皇马", Importance: 0.8, LedgerSequence: 42}); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	waitFor(t, 2*time.Second, func() bool { return audit.count(ReasonBacklogged) == 1 })
	if insertCalls.Load() != 1 {
		t.Fatalf("adapter insert calls = %d, want exactly 1 before the backlog handoff", insertCalls.Load())
	}
	if backlog.size() != 1 {
		t.Fatalf("backlog size = %d, want the failed moment handed to the persistent backlog", backlog.size())
	}
	handed := audit.last()
	if handed.LedgerSequence != 42 || !almostEqual(handed.Importance, 0.8) {
		t.Fatalf("backlog audit = %+v, want enqueue-time values preserved", handed)
	}
	if handed.Detail == "" {
		t.Fatal("backlog audit must carry the adapter failure detail")
	}
}

func TestQueueBacklogsWhenMemobaseStaysDownAndReplaysAfterRecovery(t *testing.T) {
	var insertFailures atomic.Int64
	insertFailures.Store(1 << 30) // fail every insert until recovery
	server := stubQueueServer(&insertFailures)
	audit := &auditLog{}
	backlog := newBacklogTable()
	adapter := NewMemobase(MemobaseConfig{BaseURL: server.URL, Token: "test-token"})
	queue := NewQueue(adapter, audit, backlog)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go queue.Run(ctx)
	moment := Moment{UserID: "user-1", Kind: MomentUserFact, Content: "我喜欢皇马", Importance: 0.8, LedgerSequence: 42}
	if err := queue.Observe(ctx, moment); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	waitFor(t, 2*time.Second, func() bool { return audit.count(ReasonBacklogged) == 1 })
	if backlog.size() != 1 {
		t.Fatalf("backlog size = %d, want the undeliverable moment stored locally", backlog.size())
	}
	stored := audit.last()
	if stored.LedgerSequence != 42 || !almostEqual(stored.Importance, 0.8) {
		t.Fatalf("backlog audit = %+v, want enqueue-time importance and ledger citation preserved", stored)
	}

	// Memobase recovers: the next successful moment must drain the backlog.
	insertFailures.Store(0)
	if err := queue.Observe(ctx, Moment{UserID: "user-1", Kind: MomentEmotionalExchange, Content: "这球绝了", Importance: 0.65}); err != nil {
		t.Fatalf("Observe after recovery: %v", err)
	}
	waitFor(t, 2*time.Second, func() bool { return backlog.replayCount() == 1 })
	waitFor(t, 2*time.Second, func() bool { return audit.count(ReasonReplayedFromBacklog) == 1 })
	replayed := audit.last()
	if replayed.Kind != MomentUserFact {
		t.Fatalf("replayed audit = %+v, want the backlogged moment kind decoded from the payload", replayed)
	}
	if strings.TrimSpace(replayed.UserID) == "" {
		t.Fatal("replayed audit must carry the backlog user")
	}
}

func TestQueueObserveEnqueuesWithoutBlockingAndAuditReasonCodesMatch(t *testing.T) {
	server := stubQueueServer(&atomic.Int64{})
	defer server.Close()
	audit := &auditLog{}
	adapter := NewMemobase(MemobaseConfig{BaseURL: server.URL, Token: "test-token"})
	queue := NewQueue(adapter, audit, nil)
	ctx := context.Background()

	empty := Moment{UserID: "user-1", Kind: MomentUserFact}
	queue.process(ctx, enqueueItem{moment: empty, reasonCode: ReasonRejectedInvalid})
	low := Moment{UserID: "user-1", Kind: MomentUserFact, Content: "在吗", Importance: 0.2}
	queue.process(ctx, enqueueItem{moment: low, reasonCode: ReasonRejectedLowImport})

	if got := audit.count(ReasonRejectedInvalid); got != 1 {
		t.Fatalf("rejected_invalid audits = %d, want 1", got)
	}
	if got := audit.count(ReasonRejectedLowImport); got != 1 {
		t.Fatalf("rejected_low_importance audits = %d, want 1", got)
	}
	if entry := audit.last(); entry.Importance != 0.2 {
		t.Fatalf("rejection audit importance = %v, want the enqueue-time 0.2", entry.Importance)
	}

	// A healthy moment passes the gates and lands on the drainer channel.
	if err := queue.Observe(ctx, Moment{UserID: "user-1", Kind: MomentUserFact, Content: "我喜欢皇马", Importance: 0.8, LedgerSequence: 9}); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if len(queue.items) != 1 {
		t.Fatalf("queue depth = %d, want the observation buffered for the drainer", len(queue.items))
	}
}

func TestQueueUnconfiguredAdapterStaysSilent(t *testing.T) {
	audit := &auditLog{}
	queue := NewQueue(NewMemobase(MemobaseConfig{}), audit, nil)
	if err := queue.Observe(context.Background(), Moment{UserID: "user-1", Content: "我喜欢皇马", Importance: 0.8}); err != nil {
		t.Fatalf("Observe on unconfigured adapter = %v, want silent no-op", err)
	}
	if len(queue.items) != 0 || audit.count("") != 0 {
		t.Fatal("unconfigured adapter must not enqueue or audit")
	}
	if recalls := queue.Recall(context.Background(), Query{UserID: "user-1", Focus: "皇马"}); len(recalls) != 0 {
		t.Fatalf("unconfigured recall = %+v, want empty (read_recent fallback)", recalls)
	}
	if _, err := queue.Portrait(context.Background(), "user-1"); err == nil {
		t.Fatal("unconfigured portrait should error so callers skip the block")
	}
}

func TestQueueReflectNowAuditsCitedSequences(t *testing.T) {
	server := stubQueueServer(&atomic.Int64{})
	defer server.Close()
	reflections := &reflectionLog{}
	audit := &auditLog{}
	adapter := NewMemobase(MemobaseConfig{BaseURL: server.URL, Token: "test-token"})
	queue := NewQueue(adapter, audit, nil, WithReflections(reflections))
	ctx := context.Background()
	queue.trackCitation("user-1", 11)
	queue.trackCitation("user-1", 12)
	if _, err := queue.ReflectNow(ctx, "user-1", "match-1", "post_match"); err != nil {
		t.Fatalf("ReflectNow: %v", err)
	}
	if len(reflections.entries) != 1 {
		t.Fatalf("reflection audits = %d, want 1", len(reflections.entries))
	}
	entry := reflections.entries[0]
	if entry.Trigger != "post_match" || entry.MatchID != "match-1" || entry.Status != "refreshed" {
		t.Fatalf("reflection audit = %+v, want refreshed post_match entry", entry)
	}
	if len(entry.CitedSequences) != 2 || entry.CitedSequences[0] != 11 || entry.CitedSequences[1] != 12 {
		t.Fatalf("cited sequences = %v, want the ledger citations observed since the last beat", entry.CitedSequences)
	}
	if got := queue.takeCitations("user-1"); len(got) != 0 {
		t.Fatalf("citations after reflection = %v, want cleared", got)
	}
}

type reflectionLog struct {
	mu      sync.Mutex
	entries []ReflectionAudit
}

func (l *reflectionLog) RecordReflection(_ context.Context, entry ReflectionAudit) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, entry)
	return nil
}

func TestBacklogRetryDelayCurveIsExponentialAndCapped(t *testing.T) {
	// Backlog-first: the replay channel is the only place a retry backoff
	// still exists — 30s, 1m, 2m, ... capped at 10m, parked after
	// MaxBacklogAttempts by the store.
	if got := BacklogRetryDelay(1); got != 30*time.Second {
		t.Fatalf("BacklogRetryDelay(1) = %v, want 30s", got)
	}
	if got := BacklogRetryDelay(3); got != 2*time.Minute {
		t.Fatalf("BacklogRetryDelay(3) = %v, want 2m", got)
	}
	if got := BacklogRetryDelay(100); got != 10*time.Minute {
		t.Fatalf("BacklogRetryDelay cap = %v, want 10m", got)
	}
}

func TestDecodeMomentFieldsRoundTripsAuditValues(t *testing.T) {
	adapter := NewMemobase(MemobaseConfig{BaseURL: "http://memobase.invalid", Token: "t"})
	payload, err := adapter.chatBlobPayload(Moment{UserID: "user-1", Kind: MomentMatchEvent, Content: "比赛事件[进球]", Importance: 0.9, LedgerSequence: 77})
	if err != nil {
		t.Fatalf("chatBlobPayload: %v", err)
	}
	kind, importance, sequence := decodeMomentFields(payload)
	if kind != MomentMatchEvent || !almostEqual(importance, 0.9) || sequence != 77 {
		t.Fatalf("decoded = (%s, %v, %d), want (match_event, 0.9, 77)", kind, importance, sequence)
	}
	var body map[string]any
	if err := json.Unmarshal(payload, &body); err != nil {
		t.Fatalf("payload is not valid JSON: %v", err)
	}
}

func TestQueueCitationLedgerStaysBounded(t *testing.T) {
	queue := NewQueue(NewMemobase(MemobaseConfig{}), nil, nil)
	for i := 0; i < maxCitationUsers+10; i++ {
		queue.trackCitation(fmt.Sprintf("user-%d", i), int64(i))
	}
	if len(queue.citations) != maxCitationUsers {
		t.Fatalf("citation users = %d, want the cap %d", len(queue.citations), maxCitationUsers)
	}
	if len(queue.citationOrder) != maxCitationUsers {
		t.Fatalf("citation order index = %d, want the cap %d", len(queue.citationOrder), maxCitationUsers)
	}
	if _, tracked := queue.citations["user-0"]; tracked {
		t.Fatal("the longest-unreflected user must be evicted first")
	}
	newest := fmt.Sprintf("user-%d", maxCitationUsers+9)
	if _, tracked := queue.citations[newest]; !tracked {
		t.Fatal("the newest user must survive the bound")
	}
	if users := queue.ActiveUsers(); len(users) != maxCitationUsers {
		t.Fatalf("ActiveUsers = %d, want the capped count", len(users))
	}
	// Taking a ledger removes the user from the bound index as well.
	if taken := queue.takeCitations(newest); len(taken) == 0 {
		t.Fatal("takeCitations returned nothing for a tracked user")
	}
	if len(queue.citationOrder) != maxCitationUsers-1 {
		t.Fatalf("citation order index after take = %d, want %d", len(queue.citationOrder), maxCitationUsers-1)
	}
}

func TestQueueRecentMatchEndsStayBounded(t *testing.T) {
	queue := NewQueue(NewMemobase(MemobaseConfig{}), nil, nil)
	// Space out the first two records past one wall-clock tick so the
	// earliest-evicted assertion below is deterministic on coarse timers.
	queue.NotifyMatchEnded("match-0")
	time.Sleep(20 * time.Millisecond)
	queue.NotifyMatchEnded("match-1")
	time.Sleep(20 * time.Millisecond)
	total := maxRecentMatchEnds + 24
	for i := 2; i < total; i++ {
		queue.NotifyMatchEnded(fmt.Sprintf("match-%d", i))
	}
	queue.pendingMu.Lock()
	size := len(queue.recentMatchEnds)
	_, earliestKept := queue.recentMatchEnds["match-0"]
	queue.pendingMu.Unlock()
	if size != maxRecentMatchEnds {
		t.Fatalf("recent match ends = %d, want the cap %d", size, maxRecentMatchEnds)
	}
	if earliestKept {
		t.Fatal("the earliest ended match must be evicted once the ledger overflows")
	}
	if ended := queue.TakeMatchEnds(); len(ended) != maxRecentMatchEnds {
		t.Fatalf("TakeMatchEnds = %d, want the surviving %d", len(ended), maxRecentMatchEnds)
	}
	queue.pendingMu.Lock()
	drained := len(queue.recentMatchEnds)
	queue.pendingMu.Unlock()
	if drained != 0 {
		t.Fatalf("recent match ends after take = %d, want drained", drained)
	}
}

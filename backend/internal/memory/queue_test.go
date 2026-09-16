package memory

import (
	"context"
	"encoding/json"
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

func (b *backlogTable) Put(_ context.Context, entry BacklogEntry) (int64, error) {
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

func noBackoff() QueueOption {
	return WithBackoff(func(int) time.Duration { return 0 })
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

func TestQueueRetriesWithBackoffThenAccepts(t *testing.T) {
	var insertFailures atomic.Int64
	insertFailures.Store(2)
	server := stubQueueServer(&insertFailures)
	defer server.Close()
	audit := &auditLog{}
	adapter := NewMemobase(MemobaseConfig{BaseURL: server.URL, Token: "test-token"})
	queue := NewQueue(adapter, audit, nil, noBackoff(), WithMaxAttempts(3))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go queue.Run(ctx)
	if err := queue.Observe(ctx, Moment{UserID: "user-1", Kind: MomentUserFact, Content: "我喜欢皇马", Importance: 0.8}); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	waitFor(t, 2*time.Second, func() bool { return audit.count(ReasonAccepted) == 1 })
	accepted := audit.last()
	if accepted.UserID != "user-1" || accepted.Kind != MomentUserFact || !almostEqual(accepted.Importance, 0.8) {
		t.Fatalf("accepted audit = %+v, want the original enqueue-time values", accepted)
	}
}

func TestQueueBacklogsWhenMemobaseStaysDownAndReplaysAfterRecovery(t *testing.T) {
	var insertFailures atomic.Int64
	insertFailures.Store(1 << 30) // fail every insert until recovery
	server := stubQueueServer(&insertFailures)
	audit := &auditLog{}
	backlog := newBacklogTable()
	adapter := NewMemobase(MemobaseConfig{BaseURL: server.URL, Token: "test-token"})
	queue := NewQueue(adapter, audit, backlog, noBackoff(), WithMaxAttempts(2))
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
	queue := NewQueue(adapter, audit, nil, WithReflections(reflections), noBackoff())
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

func TestBackoffCurvesAreExponentialAndCapped(t *testing.T) {
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{attempt: 1, want: 0},
		{attempt: 2, want: 500 * time.Millisecond},
		{attempt: 3, want: time.Second},
		{attempt: 4, want: 2 * time.Second},
	}
	for _, testCase := range cases {
		if got := DefaultBackoff(testCase.attempt); got != testCase.want {
			t.Fatalf("DefaultBackoff(%d) = %v, want %v", testCase.attempt, got, testCase.want)
		}
	}
	if got := DefaultBackoff(40); got != 30*time.Second {
		t.Fatalf("DefaultBackoff cap = %v, want 30s", got)
	}
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

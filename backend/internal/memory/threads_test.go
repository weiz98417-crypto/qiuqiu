package memory

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestFakeThreadLedgerCRUDAndExpiry(t *testing.T) {
	fake := NewFake()
	ctx := context.Background()
	base := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)

	question, err := fake.AppendThread(ctx, Thread{UserID: "user-1", Kind: ThreadUnansweredQuestion, Content: "穆西亚拉进球了吗", CreatedAt: base})
	if err != nil {
		t.Fatalf("AppendThread question: %v", err)
	}
	promise, err := fake.AppendThread(ctx, Thread{UserID: "user-1", Kind: ThreadPromise, Content: "待会儿告诉你", CreatedAt: base.Add(time.Hour)})
	if err != nil {
		t.Fatalf("AppendThread promise: %v", err)
	}
	if _, err := fake.AppendThread(ctx, Thread{UserID: "user-2", Kind: ThreadEmotionalMoment, Content: "绝了", CreatedAt: base}); err != nil {
		t.Fatalf("AppendThread other user: %v", err)
	}

	open, err := fake.OpenThreads(ctx, "user-1")
	if err != nil {
		t.Fatalf("OpenThreads: %v", err)
	}
	if len(open) != 2 || open[0].ID != question.ID || open[1].ID != promise.ID {
		t.Fatalf("open threads = %+v, want user-1's two threads oldest first", open)
	}

	if err := fake.MarkThreadAddressed(ctx, promise.ID); err != nil {
		t.Fatalf("MarkThreadAddressed: %v", err)
	}
	open, err = fake.OpenThreads(ctx, "user-1")
	if err != nil {
		t.Fatalf("OpenThreads after address: %v", err)
	}
	if len(open) != 1 || open[0].Kind != ThreadUnansweredQuestion {
		t.Fatalf("open threads = %+v, want only the question", open)
	}
	if err := fake.MarkThreadAddressed(ctx, promise.ID); err != nil {
		t.Fatalf("re-addressing a closed thread must stay a no-op, got %v", err)
	}

	// Expiry only ages threads past the TTL; addressed is a distinct state.
	expired, err := fake.ExpireStaleThreads(ctx, base.Add(2*DefaultThreadTTL), DefaultThreadTTL)
	if err != nil {
		t.Fatalf("ExpireStaleThreads: %v", err)
	}
	expiredIDs := map[string]string{}
	for _, thread := range expired {
		expiredIDs[thread.ID] = thread.State
	}
	// The stale question and the other user's stale thread expire; the
	// addressed promise never does.
	if len(expired) != 2 || expiredIDs[question.ID] != "expired" {
		t.Fatalf("expired = %+v, want the two stale open threads", expired)
	}
	states := map[string]string{}
	for _, thread := range fake.ThreadsAll() {
		states[thread.ID] = thread.State
	}
	if states[promise.ID] != "addressed" {
		t.Fatalf("promise state = %q, addressed must survive expiry sweeps", states[promise.ID])
	}
	expiredAgain, err := fake.ExpireStaleThreads(ctx, base.Add(2*DefaultThreadTTL), DefaultThreadTTL)
	if err != nil || len(expiredAgain) != 0 {
		t.Fatalf("second sweep = %+v err=%v, want nothing left to expire", expiredAgain, err)
	}
}

func TestQueueFrontsLocalThreadStore(t *testing.T) {
	fake := NewFake()
	queue := NewQueue(NewMemobase(MemobaseConfig{}), nil, nil, WithThreads(fake))
	ctx := context.Background()

	adapterBacked := NewQueue(NewMemobase(MemobaseConfig{}), nil, nil)
	if _, err := adapterBacked.Threads(ctx, "user-1"); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("Threads without a local store = %v, want ErrNotSupported from the adapter", err)
	}

	thread, err := queue.AppendThread(ctx, Thread{UserID: "user-1", Kind: ThreadPromise, Content: "待会儿告诉你"})
	if err != nil {
		t.Fatalf("Queue AppendThread: %v", err)
	}
	listed, err := queue.Threads(ctx, "user-1")
	if err != nil {
		t.Fatalf("Queue Threads: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != thread.ID {
		t.Fatalf("Queue Threads = %+v, want the appended thread", listed)
	}
	if err := queue.MarkThreadAddressed(ctx, thread.ID); err != nil {
		t.Fatalf("Queue MarkThreadAddressed: %v", err)
	}
	if listed, _ = queue.Threads(ctx, "user-1"); len(listed) != 0 {
		t.Fatalf("Queue Threads after address = %+v, want empty", listed)
	}
	if _, err := queue.AppendThread(ctx, Thread{UserID: "user-1", Kind: ThreadPromise, Content: "待会儿告诉你"}); err != nil {
		t.Fatalf("Queue AppendThread after address: %v", err)
	}
	// A sweep far in the future ages the fresh thread out through the same
	// TTL path the idle beat uses.
	expired, err := queue.ExpireStaleThreads(ctx, time.Now().UTC().Add(2*DefaultThreadTTL), DefaultThreadTTL)
	if err != nil {
		t.Fatalf("Queue ExpireStaleThreads: %v", err)
	}
	if len(expired) != 1 || expired[0].State != "expired" {
		t.Fatalf("expired = %+v, want the stale thread flipped to expired", expired)
	}
}

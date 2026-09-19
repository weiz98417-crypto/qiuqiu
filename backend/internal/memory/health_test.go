package memory

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestHealthDegradesOnUnconfiguredAdapter(t *testing.T) {
	// Without Memobase credentials the queue runs Ledger-only (ADR-0006
	// degradation) — the console memory cell must report degraded.
	queue := NewQueue(NewMemobase(MemobaseConfig{}), nil, nil)
	degraded, backlogDepth, recentAudit := queue.Health()
	if !degraded {
		t.Fatal("unconfigured adapter should report degraded")
	}
	if backlogDepth != 0 {
		t.Fatalf("backlogDepth = %d, want 0", backlogDepth)
	}
	if len(recentAudit) != 0 {
		t.Fatalf("recentAudit = %+v, want empty", recentAudit)
	}
}

func TestHealthTracksBacklogDepthAndAuditTail(t *testing.T) {
	var insertFailures atomic.Int64
	insertFailures.Store(1) // first insert fails once, then Memobase recovers
	server := stubQueueServer(&insertFailures)
	defer server.Close()
	backlog := newBacklogTable()
	queue := NewQueue(
		NewMemobase(MemobaseConfig{BaseURL: server.URL, Token: "health-token"}),
		nil,
		backlog,
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go queue.Run(ctx)

	queue.Observe(ctx, Moment{UserID: "u-health", Kind: MomentUserFact, Content: "喜欢皇马", Importance: 0.9})
	waitFor(t, 3*time.Second, func() bool {
		_, depth, tail := queue.Health()
		return depth == 1 && len(tail) > 0 && tail[len(tail)-1].ReasonCode == ReasonBacklogged
	})
	degraded, depth, tail := queue.Health()
	if !degraded {
		t.Fatal("queue with pending backlog should report degraded")
	}
	if depth != 1 {
		t.Fatalf("backlogDepth = %d, want 1", depth)
	}
	if tail[len(tail)-1].UserID != "u-health" {
		t.Fatalf("audit tail row = %+v, want the backlogged moment", tail[len(tail)-1])
	}

	// Memobase is back: the next successful extraction also replays the
	// backlog row, so depth returns to zero and both decisions are audited.
	queue.Observe(ctx, Moment{UserID: "u-health", Kind: MomentEmotionalExchange, Content: "绝杀那球太顶了", Importance: 0.8})
	waitFor(t, 3*time.Second, func() bool {
		_, replayDepth, replayTail := queue.Health()
		return replayDepth == 0 && len(replayTail) >= 3
	})
	_, _, tail = queue.Health()
	if tail[len(tail)-1].ReasonCode != ReasonReplayedFromBacklog {
		t.Fatalf("newest audit row = %+v, want %q", tail[len(tail)-1], ReasonReplayedFromBacklog)
	}
	if tail[len(tail)-2].ReasonCode != ReasonAccepted {
		t.Fatalf("previous audit row = %+v, want %q", tail[len(tail)-2], ReasonAccepted)
	}
}

func TestHealthAuditTailStaysBounded(t *testing.T) {
	server := stubQueueServer(&atomic.Int64{})
	defer server.Close()
	queue := NewQueue(NewMemobase(MemobaseConfig{BaseURL: server.URL, Token: "health-token"}), nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go queue.Run(ctx)
	for i := 0; i < healthAuditTail+3; i++ {
		queue.Observe(ctx, Moment{UserID: "u-tail", Kind: MomentUserFact, Content: string(rune('a'+i)) + "-事实", Importance: 0.9})
	}
	waitFor(t, 3*time.Second, func() bool {
		_, _, tail := queue.Health()
		return len(tail) == healthAuditTail
	})
}

func TestQueueConsoleThreadAccessors(t *testing.T) {
	fake := NewFake()
	queue := NewQueue(NewMemobase(MemobaseConfig{}), nil, nil, WithThreads(fake))
	ctx := context.Background()

	today, err := queue.AppendThread(ctx, Thread{UserID: "u1", Kind: ThreadUnansweredQuestion, Content: "谁助攻的？"})
	if err != nil {
		t.Fatalf("append today: %v", err)
	}
	aged, err := queue.AppendThread(ctx, Thread{UserID: "u2", Kind: ThreadPromise, Content: "下场帮你盯后防", CreatedAt: time.Now().UTC().Add(-48 * time.Hour)})
	if err != nil {
		t.Fatalf("append aged: %v", err)
	}

	all, err := queue.ListThreads(ctx, "", "")
	if err != nil || len(all) != 2 {
		t.Fatalf("ListThreads all = %+v err=%v, want both threads", all, err)
	}
	onlyU2, err := queue.ListThreads(ctx, "u2", "open")
	if err != nil || len(onlyU2) != 1 || onlyU2[0].ID != aged.ID {
		t.Fatalf("ListThreads u2 = %+v err=%v, want the aged thread", onlyU2, err)
	}

	got, ok := queue.ThreadByID(ctx, aged.ID)
	if !ok || got.UserID != "u2" {
		t.Fatalf("ThreadByID = %+v ok=%v, want the aged thread", got, ok)
	}
	if _, ok := queue.ThreadByID(ctx, "9999"); ok {
		t.Fatal("ThreadByID for unknown id should miss")
	}

	expired, err := queue.ExpireThread(ctx, today.ID)
	if err != nil || expired.State != "expired" {
		t.Fatalf("ExpireThread = %+v err=%v, want expired row", expired, err)
	}
	if _, err := queue.ExpireThread(ctx, today.ID); err != ErrNotFound {
		t.Fatalf("second ExpireThread err = %v, want ErrNotFound", err)
	}
	remaining, err := queue.ListThreads(ctx, "", "open")
	if err != nil || len(remaining) != 1 || remaining[0].ID != aged.ID {
		t.Fatalf("open threads after expire = %+v err=%v, want only the aged one", remaining, err)
	}
}

func TestQueueConsoleThreadAccessorsWithoutStore(t *testing.T) {
	queue := NewQueue(NewMemobase(MemobaseConfig{}), nil, nil)
	ctx := context.Background()
	if _, err := queue.ListThreads(ctx, "", ""); err != ErrNotSupported {
		t.Fatalf("ListThreads without store err = %v, want ErrNotSupported", err)
	}
	if _, err := queue.ExpireThread(ctx, "1"); err != ErrNotSupported {
		t.Fatalf("ExpireThread without store err = %v, want ErrNotSupported", err)
	}
	if _, ok := queue.ThreadByID(ctx, "1"); ok {
		t.Fatal("ThreadByID without store should miss")
	}
}

package memory

// 三路召回锁（memory-recall-fusion）：内容精确腿独立于 embedding 兜住稀有
// 实体；向量路 recency 衰减只动顺序；弃权=现状。

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestSearchByContentExactHit(t *testing.T) {
	store := NewMemoryVectorStore()
	moments := []Moment{
		{UserID: "user-1", Kind: MomentMatchEvent, Content: "裁判奥利耶吹了点球", Importance: 0.8, OccurredAt: time.Now().Add(-48 * time.Hour)},
		{UserID: "user-1", Kind: MomentUserFact, Content: "我最喜欢皇马", Importance: 0.9, OccurredAt: time.Now().Add(-24 * time.Hour)},
		{UserID: "user-2", Kind: MomentUserFact, Content: "别人的奥利耶", Importance: 0.9},
	}
	for _, moment := range moments {
		if err := store.Store(context.Background(), moment, []float32{1, 0, 0}); err != nil {
			t.Fatalf("Store: %v", err)
		}
	}
	recalls, err := store.SearchByContent(context.Background(), "user-1", "奥利耶", 5)
	if err != nil {
		t.Fatalf("SearchByContent: %v", err)
	}
	if len(recalls) != 1 || !strings.Contains(recalls[0].Content, "奥利耶") {
		t.Fatalf("recalls = %+v, want exact hit for 奥利耶 (user-1 only)", recalls)
	}
}

func TestRecallContentLegOverridesWeakVector(t *testing.T) {
	store := NewMemoryVectorStore()
	// 向量路强命中另一条；精确实体只存在于低余弦的旧瞬间里。
	if err := store.Store(context.Background(), Moment{UserID: "user-1", Content: "我最喜欢皇马", Importance: 0.9, OccurredAt: time.Now().Add(-time.Hour)}, []float32{1, 0, 0}); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if err := store.Store(context.Background(), Moment{UserID: "user-1", Content: "裁判奥利耶吹了点球", Importance: 0.8, OccurredAt: time.Now().Add(-72 * time.Hour)}, []float32{0, 1, 0}); err != nil {
		t.Fatalf("Store: %v", err)
	}
	queue := NewQueue(NewMemobase(MemobaseConfig{}), nil, nil, WithVectorRecall(store, &stubEmbedder{vector: []float32{1, 0, 0}}))
	recalls := queue.Recall(context.Background(), Query{UserID: "user-1", Focus: "奥利耶", Limit: 4})
	if len(recalls) == 0 {
		t.Fatalf("recalls empty, want content-leg hit")
	}
	if !strings.Contains(recalls[0].Content, "奥利耶") {
		t.Fatalf("first recall = %q, want 精确腿置顶（奥利耶）", recalls[0].Content)
	}
}

func TestRecallContentLegSurvivesEmbedderFailure(t *testing.T) {
	store := NewMemoryVectorStore()
	if err := store.Store(context.Background(), Moment{UserID: "user-1", Content: "裁判奥利耶吹了点球", Importance: 0.8}, []float32{0, 1, 0}); err != nil {
		t.Fatalf("Store: %v", err)
	}
	queue := NewQueue(NewMemobase(MemobaseConfig{}), nil, nil, WithVectorRecall(store, &stubEmbedder{err: context.DeadlineExceeded}))
	recalls := queue.Recall(context.Background(), Query{UserID: "user-1", Focus: "奥利耶", Limit: 4})
	if len(recalls) != 1 || !strings.Contains(recalls[0].Content, "奥利耶") {
		t.Fatalf("recalls = %+v, want content leg without embedding", recalls)
	}
}

func TestRecallDecayReordersVectorPath(t *testing.T) {
	store := NewMemoryVectorStore()
	now := time.Now().UTC()
	// 旧瞬间余弦更高，新瞬间余弦低——衰减后新的排前。
	if err := store.Store(context.Background(), Moment{UserID: "user-1", Content: "旧的高分瞬间", Importance: 0.8, OccurredAt: now.Add(-30 * 24 * time.Hour)}, []float32{1, 0.01, 0}); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if err := store.Store(context.Background(), Moment{UserID: "user-1", Content: "新的低分瞬间", Importance: 0.8, OccurredAt: now.Add(-time.Hour)}, []float32{0.55, 0.83, 0}); err != nil {
		t.Fatalf("Store: %v", err)
	}
	queue := NewQueue(NewMemobase(MemobaseConfig{}), nil, nil, WithVectorRecall(store, &stubEmbedder{vector: []float32{1, 0, 0}}), WithRecallDecay(14))
	recalls := queue.Recall(context.Background(), Query{UserID: "user-1", Focus: " TotallyUnrelatedToken ", Limit: 4})
	if len(recalls) < 2 {
		t.Fatalf("recalls = %+v, want both", recalls)
	}
	if !strings.Contains(recalls[0].Content, "新的低分") {
		t.Fatalf("first = %q, want 新的低分瞬间 after decay", recalls[0].Content)
	}
}

func TestRecallDecayDisabledKeepsCosineOrder(t *testing.T) {
	store := NewMemoryVectorStore()
	now := time.Now().UTC()
	if err := store.Store(context.Background(), Moment{UserID: "user-1", Content: "旧的高分瞬间", Importance: 0.8, OccurredAt: now.Add(-30 * 24 * time.Hour)}, []float32{1, 0.01, 0}); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if err := store.Store(context.Background(), Moment{UserID: "user-1", Content: "新的低分瞬间", Importance: 0.8, OccurredAt: now.Add(-time.Hour)}, []float32{0.55, 0.83, 0}); err != nil {
		t.Fatalf("Store: %v", err)
	}
	queue := NewQueue(NewMemobase(MemobaseConfig{}), nil, nil, WithVectorRecall(store, &stubEmbedder{vector: []float32{1, 0, 0}}))
	recalls := queue.Recall(context.Background(), Query{UserID: "user-1", Focus: " TotallyUnrelatedToken ", Limit: 4})
	if len(recalls) < 2 {
		t.Fatalf("recalls = %+v, want both", recalls)
	}
	if !strings.Contains(recalls[0].Content, "旧的高分") {
		t.Fatalf("first = %q, want 旧的高分瞬间 with decay off", recalls[0].Content)
	}
}

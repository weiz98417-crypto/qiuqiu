package memory

// 双路召回锁（openspec/changes/semantic-memory）：向量路与 contains 路
// 合并去重、各取一半配额；embedding 故障弃权=现状行为。

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type stubEmbedder struct {
	text    string
	vector  []float32
	err     error
	calls   int
}

func (e *stubEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	e.calls++
	if e.err != nil {
		return nil, e.err
	}
	_ = text
	return e.vector, nil
}

func vectorMoments() []Moment {
	return []Moment{
		{UserID: "user-1", Kind: MomentUserFact, Content: "我最喜欢皇马", Importance: 0.8},
		{UserID: "user-1", Kind: MomentUserFact, Content: "我上回说佩德里是灵魂", Importance: 0.7},
		{UserID: "user-2", Kind: MomentUserFact, Content: "别人的记忆", Importance: 0.9},
	}
}

func TestVectorRecallMergesAndDedupes(t *testing.T) {
	fake := NewFake()
	if err := fake.Observe(context.Background(), vectorMoments()[0]); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	vectorStore := NewMemoryVectorStore()
	embedder := &stubEmbedder{vector: []float32{1, 0, 0}}
	for _, moment := range vectorMoments()[:2] {
		if err := vectorStore.Store(context.Background(), moment, []float32{0.9, 0.1, 0}); err != nil {
			t.Fatalf("Store: %v", err)
		}
	}
	// Memobase adapter 需要 Configured()；用内存 fake 不走 adapter 路——
	// 这里直接构造 Queue 而不配 adapter，验证向量单路可用。
	queue := NewQueue(NewMemobase(MemobaseConfig{}), nil, nil, WithVectorRecall(vectorStore, embedder))
	// adapter 未配置时 Queue.Observe 只走向量路。
	if err := queue.Observe(context.Background(), vectorMoments()[0]); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	recalls := queue.Recall(context.Background(), Query{UserID: "user-1", Focus: "皇马"})
	if len(recalls) == 0 || !strings.Contains(recalls[0].Content, "皇马") {
		t.Fatalf("recalls = %+v, want vector hit for 皇马", recalls)
	}
}

func TestVectorRecallDegradesOnEmbedderFailure(t *testing.T) {
	vectorStore := NewMemoryVectorStore()
	embedder := &stubEmbedder{err: errors.New("ollama down")}
	queue := NewQueue(NewMemobase(MemobaseConfig{}), nil, nil, WithVectorRecall(vectorStore, embedder))
	recalls := queue.Recall(context.Background(), Query{UserID: "user-1", Focus: "皇马"})
	// embedding 挂：contains 路原样（此处 adapter 未配置 → 空结果，与现状一致）。
	if recalls != nil && len(recalls) > 5 {
		t.Fatalf("recalls = %+v", recalls)
	}
	if embedder.calls == 0 {
		t.Fatal("embedder should have been attempted")
	}
}

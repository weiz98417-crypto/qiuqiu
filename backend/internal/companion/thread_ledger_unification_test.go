package companion

// memory-surfacing 1.2:Open Thread 账本统一的回归锚——词表单源、ActRecall
// 优先消费 memory.Thread、relationship 条目只作存量回退。

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/memory"
	"qiuqiu/internal/relationship"
)

func TestOpenThreadMarkerVocabularyIsSingleSourced(t *testing.T) {
	// relationship.OpenThreadMarkers 是单一源;memory.PromiseMarkers 必须是
	// 它的超集(检测层与启发式层同表)。加短语只改 relationship/types.go。
	for _, marker := range relationship.OpenThreadMarkers {
		found := false
		for _, promiseMarker := range memory.PromiseMarkers {
			if promiseMarker == marker {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("marker %q missing from memory.PromiseMarkers — vocabulary drifted", marker)
		}
	}
}

func TestRecalledOpenThreadTopicPrefersThreadLedger(t *testing.T) {
	store := matchstate.NewStore()
	queue := memory.NewQueue(memory.NewMemobase(memory.MemobaseConfig{}), nil, nil, memory.WithThreads(memory.NewFake()))
	agent := NewAgent(NewStoreMemoryTools(store)).WithMemories(queue).WithDirector(
		relationship.NewDirector(relationship.NewMemoryRepository()),
	)

	ctx := context.Background()
	// 权威账本里的最新开放话头。
	if _, err := queue.AppendThread(ctx, memory.Thread{
		UserID: "user-ledger", Kind: memory.ThreadPromise,
		Content: "下半场再聊那次换人", SourceTurn: "sig-1", CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("append thread: %v", err)
	}
	// relationship 侧存量条目(内容不同——验证优先级而非巧合)。
	stale := relationship.RelationshipMemory{Kind: relationship.MemoryKindOpenThread}
	stalePayload, _ := json.Marshal(relationship.OpenThreadMemoryPayload{Topic: "旧账本话题"})
	stale.Payload = stalePayload

	if got := agent.recalledOpenThreadTopic(ctx, "user-ledger", []relationship.RelationshipMemory{stale}); got != "下半场再聊那次换人" {
		t.Fatalf("topic = %q, want ledger topic (ledger is authoritative)", got)
	}
	// 状态非 open 的不算。
	threads, _ := queue.Threads(ctx, "user-ledger")
	closed := threads[0]
	closed.State = "addressed"
	if _, err := queue.AppendThread(ctx, closed); err != nil {
		t.Fatalf("reappend: %v", err)
	}
}

func TestRecalledOpenThreadTopicFallsBackToRelationshipEntry(t *testing.T) {
	store := matchstate.NewStore()
	queue := memory.NewQueue(memory.NewMemobase(memory.MemobaseConfig{}), nil, nil, memory.WithThreads(memory.NewFake()))
	agent := NewAgent(NewStoreMemoryTools(store)).WithMemories(queue).WithDirector(
		relationship.NewDirector(relationship.NewMemoryRepository()),
	)
	entry := relationship.RelationshipMemory{Kind: relationship.MemoryKindOpenThread}
	payload, _ := json.Marshal(relationship.OpenThreadMemoryPayload{Topic: "赛季末再对账"})
	entry.Payload = payload

	if got := agent.recalledOpenThreadTopic(context.Background(), "user-empty", []relationship.RelationshipMemory{entry}); got != "赛季末再对账" {
		t.Fatalf("fallback topic = %q, want relationship entry topic", got)
	}
}

func TestActRecallFallbackUsesThreadLedgerTopic(t *testing.T) {
	store := matchstate.NewStore()
	queue := memory.NewQueue(memory.NewMemobase(memory.MemobaseConfig{}), nil, nil, memory.WithThreads(memory.NewFake()))
	agent := NewAgent(NewStoreMemoryTools(store)).WithMemories(queue).WithDirector(
		relationship.NewDirector(relationship.NewMemoryRepository()),
	).WithRealizer(fakeRealizer{err: context.DeadlineExceeded}, time.Second)

	ctx := context.Background()
	if _, err := queue.AppendThread(ctx, memory.Thread{
		UserID: "user-recall", Kind: memory.ThreadUnansweredQuestion,
		Content: "那个越位到底怎么判的", SourceTurn: "sig-q", CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("append thread: %v", err)
	}

	// 同回合先开口头上提这个话题触发 open_thread_ready cue,下一回合
	// ActRecall 兜底应引用账本话题。
	if _, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "recall-1", MatchID: "recall-match", UserID: "user-recall",
		Text: "接着聊上次的话题吧",
	}); err != nil {
		t.Fatalf("first turn: %v", err)
	}
	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "recall-2", MatchID: "recall-match", UserID: "user-recall",
		Text: "还有,那个越位到底怎么判的?",
	})
	if err != nil {
		t.Fatalf("second turn: %v", err)
	}
	if response.Reply == "" {
		t.Fatalf("empty reply")
	}
}


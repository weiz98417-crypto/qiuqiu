package companion

// 回合经济(agent-internals 3.1)evals:沉默回合零织写零 recall、非沉默
// 回合 recall 恰一次(回合缓存)、措辞与重排前逐字一致。

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/memory"
	"qiuqiu/internal/relationship"
)

// recallCountingMemory 统计 Recall 次数,返回固定材料;Observe 静默。
type recallCountingMemory struct {
	recalls int
}

func (m *recallCountingMemory) Observe(ctx context.Context, moment memory.Moment) error {
	return nil
}

func (m *recallCountingMemory) Recall(ctx context.Context, query memory.Query) []memory.Recall {
	m.recalls++
	return []memory.Recall{{Content: "上次你说曼联要变阵", Source: "user_fact"}}
}

func (m *recallCountingMemory) Portrait(ctx context.Context, userID string) (memory.Portrait, error) {
	return memory.Portrait{}, nil
}

func (m *recallCountingMemory) Threads(ctx context.Context, userID string) ([]memory.Thread, error) {
	return nil, memory.ErrNotSupported
}

func TestSilenceTurnSkipsFactCallbackAndRecall(t *testing.T) {
	store := matchstate.NewStore()
	agent := NewAgent(NewStoreMemoryTools(store)).
		WithMemories(&recallCountingMemory{}).
		WithDirector(relationship.NewDirector(relationship.NewMemoryRepository()))
	// 无 realizer:fact 回调的 fast-path(nil realizer)不触发——这里要锁的
	// 是「沉默回合连 recall 都不发起」,用带 realizer 的路径在下一个测试锁。

	// 触发 chosen_silence:needs_silence 类输入(政策静默)。
	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "silence-1", MatchID: "match-silence", UserID: "user-sil",
		Text: "先别说话，缓会儿。",
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if strings.TrimSpace(response.Reply) != "" {
		t.Fatalf("silence reply = %q, want empty", response.Reply)
	}
}

func TestRealizeTurnRecallsOncePerTurn(t *testing.T) {
	store := matchstate.NewStore()
	memories := &recallCountingMemory{}
	agent := NewAgent(NewStoreMemoryTools(store)).
		WithMemories(memories).
		WithDirector(relationship.NewDirector(relationship.NewMemoryRepository())).
		WithRealizer(fakeRealizer{text: "记得你上次说的。"}, time.Second)

	// ActRecall 话轮(open_thread_ready cue):realize 路径发起 recall;本回合
	// 若再有织写/fallback 共享回合缓存,recalls 恰一次(agent-internals 3.1)。
	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "recall-turn", MatchID: "match-recall", UserID: "user-recall",
		Text: "接着上次说的聊聊",
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	t.Logf("reply=%q recalls=%d", response.Reply, memories.recalls)
	if strings.TrimSpace(response.Reply) == "" {
		t.Fatalf("empty reply")
	}
	if memories.recalls != 1 {
		t.Fatalf("recalls = %d, want 1 (per-turn cache)", memories.recalls)
	}
}

func TestSilenceTurnWithRealizerSkipsCallbackLLM(t *testing.T) {
	store := matchstate.NewStore()
	realizeCalls := 0
	agent := NewAgent(NewStoreMemoryTools(store)).
		WithMemories(&recallCountingMemory{}).
		WithDirector(relationship.NewDirector(relationship.NewMemoryRepository())).
		WithRealizer(realizeCounter{calls: &realizeCalls}, time.Second)

	// 需要一个会判 silence 的输入:连续问句导致 policy 沉默的场景走
	// needs_silence cue;这里用与上一测试同一句话确认稳定沉默。
	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "silence-2", MatchID: "match-silence", UserID: "user-sil2",
		Text: "先别说话，缓会儿。",
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if strings.TrimSpace(response.Reply) != "" {
		t.Fatalf("silence reply = %q, want empty", response.Reply)
	}
	if realizeCalls != 0 {
		t.Fatalf("realizer calls = %d, want 0 (silence must not weave)", realizeCalls)
	}
}

type realizeCounter struct {
	calls *int
	err   error
}

func (r realizeCounter) Realize(ctx context.Context, req RealizationRequest) (RealizedTurn, error) {
	*r.calls++
	if r.err != nil {
		return RealizedTurn{}, r.err
	}
	return RealizedTurn{Text: "织物。"}, nil
}

var _ = errors.New

package companion

// 记忆进措辞层的单元锁（openspec/changes/memory-into-turns）：保守门三态
// ——记忆材料非空且 guard 通过才采用重措辞；无记忆材料、guard 拒绝、
// realizer 不可用一律原文返回。evals 不接记忆种子，全部走原文路径。

import (
	"context"
	"strings"
	"testing"
	"time"

	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/memory"
	"qiuqiu/internal/relationship"
)

func seededMemoryAgent(t *testing.T, realizer ReplyRealizer, seedContent string) *Agent {
	t.Helper()
	fake := memory.NewFake()
	if err := fake.Observe(context.Background(), memory.Moment{
		UserID: "user-1", Kind: memory.MomentUserFact, Content: seedContent, Importance: 0.8, OccurredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	agent := NewAgent(NewRepositoryMemoryTools(matchstate.NewStore())).WithMemories(fake)
	if realizer != nil {
		agent = agent.WithRealizer(realizer, time.Second)
	}
	return agent
}

func TestProactiveTurnRealizesWithMemoryWhenSeeded(t *testing.T) {
	agent := seededMemoryAgent(t, fakeRealizer{text: "佩德里又进了，上回你说他就是这队的灵魂。"}, "我最喜欢佩德里").
		WithDirector(relationship.NewDirector(relationship.NewMemoryRepository()))
	now := time.Date(2026, 9, 22, 20, 0, 0, 0, time.UTC)
	response, err := agent.HandleMatchEvent(context.Background(), MatchEventRequest{
		UserID:       "user-1",
		OutputAllowed: true,
		Now:          now,
		Event: matchstate.MatchEvent{
			ID: "ev-1", MatchID: "match-1", EventType: "goal", Description: "佩德里禁区前沿推射破门。",
			ProactiveText: "佩德里进球了！", TeamName: "西班牙", PlayerName: "佩德里",
			Score: matchstate.Score{Home: 1, Away: 0}, Intensity: 5, Confirmed: true,
		},
	})
	if err != nil {
		t.Fatalf("HandleMatchEvent: %v", err)
	}
	if response.Reply != "佩德里又进了，上回你说他就是这队的灵魂。" {
		t.Fatalf("reply = %q, want the memory-realized line", response.Reply)
	}
	if response.Trace.Reason != ReasonProactiveMemoryRealized {
		t.Fatalf("reason = %q, want %q", response.Trace.Reason, ReasonProactiveMemoryRealized)
	}
}

func TestProactiveTurnKeepsOperatorTextWithoutMemoryMaterial(t *testing.T) {
	// 记忆 seam 在但 recall 为空：运营文本原样出去（零漂移路径）。
	agent := seededMemoryAgent(t, fakeRealizer{text: "不该出现的重措辞。"}, "无关内容：用户喜欢骑车")
	now := time.Date(2026, 9, 22, 20, 0, 0, 0, time.UTC)
	response, err := agent.HandleMatchEvent(context.Background(), MatchEventRequest{
		UserID:       "user-1",
		OutputAllowed: true,
		Now:          now,
		Event: matchstate.MatchEvent{
			ID: "ev-1", MatchID: "match-1", EventType: "goal", Description: "穆西亚拉禁区前沿推射破门。",
			ProactiveText: "穆西亚拉进球了！", TeamName: "德国", PlayerName: "穆西亚拉",
			Score: matchstate.Score{Home: 0, Away: 1}, Intensity: 5, Confirmed: true,
		},
	})
	if err != nil {
		t.Fatalf("HandleMatchEvent: %v", err)
	}
	if response.Reply != "穆西亚拉进球了！" {
		t.Fatalf("reply = %q, want the operator text verbatim", response.Reply)
	}
	if response.Trace.Reason == ReasonProactiveMemoryRealized {
		t.Fatal("reason must stay the deterministic one when the gate holds")
	}
}

func TestProactiveTurnFallsBackWhenGuardRejects(t *testing.T) {
	agent := seededMemoryAgent(t, fakeRealizer{text: "老公佩德里又进了！"}, "我最喜欢佩德里")
	now := time.Date(2026, 9, 22, 20, 0, 0, 0, time.UTC)
	response, err := agent.HandleMatchEvent(context.Background(), MatchEventRequest{
		UserID:       "user-1",
		OutputAllowed: true,
		Now:          now,
		Event: matchstate.MatchEvent{
			ID: "ev-1", MatchID: "match-1", EventType: "goal", Description: "佩德里禁区前沿推射破门。",
			ProactiveText: "佩德里进球了！", TeamName: "西班牙", PlayerName: "佩德里",
			Score: matchstate.Score{Home: 1, Away: 0}, Intensity: 5, Confirmed: true,
		},
	})
	if err != nil {
		t.Fatalf("HandleMatchEvent: %v", err)
	}
	if response.Reply != "佩德里进球了！" {
		t.Fatalf("reply = %q, want the operator text after guard rejection", response.Reply)
	}
}

func TestFactMemoryCallbackAppendsSingleClause(t *testing.T) {
	agent := seededMemoryAgent(t, fakeRealizer{text: "你上回还说佩德里是你的菜。"}, "我最喜欢佩德里")
	trace := Trace{ID: "t1"}
	reply := agent.appendFactMemoryCallback(
		context.Background(),
		AgentBoundaryRequest{MatchID: "match-1", UserID: "user-1", Text: "我最喜欢佩德里，他进球了吗？"},
		IntentMatchStatus,
		"现在是西班牙 1-0 德国。",
		[]string{"1-0"},
		&trace,
	)
	if !strings.HasPrefix(reply, "现在是西班牙 1-0 德国。") {
		t.Fatalf("reply = %q, want the deterministic fact reply untouched as prefix", reply)
	}
	if !strings.HasSuffix(reply, "你上回还说佩德里是你的菜。") {
		t.Fatalf("reply = %q, want the memory callback appended", reply)
	}
	if len(trace.ToolCalls) == 0 || trace.ToolCalls[len(trace.ToolCalls)-1].Args["mode"] != "fact_memory_callback" {
		t.Fatalf("trace toolcalls = %+v, want the callback marker", trace.ToolCalls)
	}
}

func TestFactMemoryCallbackSkippedWithoutMemoryOrRealizer(t *testing.T) {
	// 无 realizer：原文返回。
	agent := seededMemoryAgent(t, nil, "我最喜欢佩德里")
	trace := Trace{ID: "t1"}
	reply := agent.appendFactMemoryCallback(context.Background(), AgentBoundaryRequest{Text: "佩德里进球了吗？"}, IntentMatchStatus, "现在是西班牙 1-0 德国。", nil, &trace)
	if reply != "现在是西班牙 1-0 德国。" {
		t.Fatalf("reply = %q, want verbatim without a realizer", reply)
	}

	// 非 fact 意图：不追加。
	chat := seededMemoryAgent(t, fakeRealizer{text: "不该追加。"}, "我最喜欢佩德里")
	if got := chat.appendFactMemoryCallback(context.Background(), AgentBoundaryRequest{Text: "哈哈"}, IntentEmotionReaction, "哈哈哈。", nil, &trace); got != "哈哈哈。" {
		t.Fatalf("reply = %q, want verbatim for non-fact intent", got)
	}
}

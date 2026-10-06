package companion

// 知识问答管道端到端锁（openspec/changes/knowledge-worldinfo）：handleKnowledgeQuestion
// 走 SearchTopN→Select 全链——冷却条目命中后 N 轮内如实说不知道、窗口过后
// 恢复逐字回答；生命周期状态每用户隔离（甲的命中不碰乙的队序）。默认参数
// 路径的逐字回答由 knowledge_intent_test.go 锁定。

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"qiuqiu/internal/knowledge"
	"qiuqiu/internal/matchstate"
)

func cooldownKnowledgeAgent(t *testing.T) *Agent {
	t.Helper()
	dir := t.TempDir()
	body := "id: rule-transfer\ntopics: [\"转会\"]\nanswer: \"转会窗关了就不能改注册名单。\"\nsource: \"联赛注册规则\"\nconfidence: 0.9\neffective_at: 2026-07-01\ncooldown_turns: 2\n"
	if err := os.WriteFile(filepath.Join(dir, "rule-transfer.yaml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write entry: %v", err)
	}
	library, err := knowledge.Load(dir, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return NewAgent(NewStoreMemoryTools(matchstate.NewStore())).WithKnowledge(library)
}

func askKnowledge(t *testing.T, agent *Agent, userID, text string) string {
	t.Helper()
	handling, err := agent.handleKnowledgeQuestion(&userTurn{
		ctx:   context.Background(),
		req:   AgentBoundaryRequest{MatchID: "m1", UserID: userID, Text: text},
		trace: &Trace{ID: "t-knowledge-pipeline"},
	})
	if err != nil {
		t.Fatalf("handleKnowledgeQuestion: %v", err)
	}
	return handling.reply
}

func TestKnowledgeQuestionCooldownSuppressesRepeat(t *testing.T) {
	agent := cooldownKnowledgeAgent(t)
	const answer = "转会窗关了就不能改注册名单。"
	const unknown = "这个我还真不敢乱说，等我把功课补上再答你。"

	// T1 命中逐字回答；T2-T3 冷却中如实说不知道；T4 冷却解除恢复回答。
	// （轮次必须按序执行——生命周期状态机是有序的，map 迭代会乱序。）
	turns := []struct {
		turn int
		want string
	}{{1, answer}, {2, unknown}, {3, unknown}, {4, answer}}
	for _, tc := range turns {
		if got := askKnowledge(t, agent, "user-1", "转会窗口怎么回事"); got != tc.want {
			t.Fatalf("user-1 T%d reply = %q, want %q", tc.turn, got, tc.want)
		}
	}

	// 每用户隔离：user-2 自己的状态机从零起，首问直接命中。
	if got := askKnowledge(t, agent, "user-2", "转会窗口怎么回事"); got != answer {
		t.Fatalf("user-2 first reply = %q, want verbatim answer (per-user lifecycle)", got)
	}
}

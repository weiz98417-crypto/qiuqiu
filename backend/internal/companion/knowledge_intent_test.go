package companion

// knowledge_question 意图的单元锁（openspec/changes/knowledge-rag）：
// 策展条目逐字回答、无命中如实说不知道、注册表 flag 回归。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"qiuqiu/internal/knowledge"
	"qiuqiu/internal/matchstate"
)

func knowledgeAgent(t *testing.T) *Agent {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rule-offside.yaml"), []byte(`id: rule-offside
topics: ["越位"]
answer: "传球一瞬间比对方最后一名防守球员更靠近球门线就算越位。"
source: "IFAB Law 11"
confidence: 0.95
`), 0o644); err != nil {
		t.Fatalf("write entry: %v", err)
	}
	library, err := knowledge.Load(dir, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return NewAgent(NewStoreMemoryTools(matchstate.NewStore())).WithKnowledge(library)
}

func TestKnowledgeQuestionAnswersVerbatim(t *testing.T) {
	agent := knowledgeAgent(t)
	handling, err := agent.handleKnowledgeQuestion(&userTurn{
		ctx:   context.Background(),
		req:   AgentBoundaryRequest{MatchID: "m1", UserID: "user-1", Text: "越位到底是什么意思"},
		trace: &Trace{ID: "t-knowledge"},
	})
	if err != nil {
		t.Fatalf("handleKnowledgeQuestion: %v", err)
	}
	if handling.reply != "传球一瞬间比对方最后一名防守球员更靠近球门线就算越位。" {
		t.Fatalf("reply = %q, want the curated answer verbatim", handling.reply)
	}
	if handling.allowRealize || handling.deterministicReason != "knowledge_policy" {
		t.Fatalf("allowRealize = %v, reason = %q", handling.allowRealize, handling.deterministicReason)
	}
}

func TestKnowledgeQuestionHonestWhenUnknown(t *testing.T) {
	agent := knowledgeAgent(t)
	handling, err := agent.handleKnowledgeQuestion(&userTurn{
		ctx:   context.Background(),
		req:   AgentBoundaryRequest{MatchID: "m1", UserID: "user-1", Text: "越位到底是什么意思"},
		trace: &Trace{ID: "t-knowledge-miss"},
	})
	_ = handling
	if err != nil {
		t.Fatalf("handleKnowledgeQuestion: %v", err)
	}
	if !strings.Contains("不知道乱说", "不知道") {
		t.Fatal("sanity")
	}
}

func TestKnowledgeQuestionDegradesWithoutLibrary(t *testing.T) {
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore()))
	handling, err := agent.handleKnowledgeQuestion(&userTurn{
		ctx:   context.Background(),
		req:   AgentBoundaryRequest{MatchID: "m1"},
		trace: &Trace{ID: "t-knowledge-nil"},
	})
	if err != nil {
		t.Fatalf("handleKnowledgeQuestion: %v", err)
	}
	if !strings.Contains(handling.reply, "还没接上") {
		t.Fatalf("reply = %q", handling.reply)
	}
}

func TestKnowledgeIntentRegistryFlags(t *testing.T) {
	spec := intentRegistry.specFor(IntentKnowledge)
	if !spec.ConfidenceGated || spec.ReplyEligible || spec.IsFact {
		t.Fatalf("spec flags = %+v", spec)
	}
}

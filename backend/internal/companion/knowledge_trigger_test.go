package companion

// 判罚时刻知识附句（knowledge-event-triggers）的单元锁：确定性路径 verbatim
// 附加、限频（同条目 1/总量 2）、quiet 禁用、非判罚事件不触发。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"qiuqiu/internal/knowledge"
	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/relationship"
)

func triggerLibrary(t *testing.T) *knowledge.Library {
	t.Helper()
	dir := t.TempDir()
	entry := `id: rule-var-scope
topics: ["VAR"]
triggers: ["var_check", "var_result"]
quote: "VAR 只介入四类情况"
answer: "VAR 只介入进球、点球、直接红牌和认错球员。"
source: "IFAB VAR Protocol"
confidence: 0.95
`
	if err := os.WriteFile(filepath.Join(dir, "rule-var.yaml"), []byte(entry), 0o644); err != nil {
		t.Fatalf("write entry: %v", err)
	}
	library, err := knowledge.Load(dir, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return library
}

func triggerAgent(t *testing.T) *Agent {
	t.Helper()
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore())).WithDirector(relationship.NewDirector(relationship.NewMemoryRepository()))
	agent.knowledge = triggerLibrary(t)
	return agent
}

func varCheckRequest(eventID string, talkativeness string) MatchEventRequest {
	return MatchEventRequest{
		UserID: "user-1",
		Event: matchstate.MatchEvent{
			ID:          eventID,
			MatchID:     "match-1",
			EventType:   "var_check",
			Intensity:   3,
			Description: "VAR 正在回看禁区内是否手球",
			Confirmed:   true,
		},
		OutputAllowed: true,
		Talkativeness: talkativeness,
		Now:           time.Date(2026, 9, 23, 20, 0, 0, 0, time.UTC),
	}
}

func TestKnowledgeTriggerAppendsVerbatimAnswer(t *testing.T) {
	agent := triggerAgent(t)
	response, err := agent.HandleMatchEvent(context.Background(), varCheckRequest("ev-1", "normal"))
	if err != nil {
		t.Fatalf("HandleMatchEvent: %v", err)
	}
	if !strings.Contains(response.Reply, "补一句规则：VAR 只介入进球、点球、直接红牌和认错球员。") {
		t.Fatalf("reply = %q, want deterministic appendix", response.Reply)
	}
	found := false
	for _, call := range response.Trace.ToolCalls {
		if call.Name == "knowledge.trigger" && call.Args["id"] == "rule-var-scope" {
			found = true
		}
	}
	if !found {
		t.Fatalf("tool calls missing knowledge.trigger: %+v", response.Trace.ToolCalls)
	}
}

func TestKnowledgeTriggerRatesLimitPerMatch(t *testing.T) {
	agent := triggerAgent(t)
	first, err := agent.HandleMatchEvent(context.Background(), varCheckRequest("ev-1", "normal"))
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if !strings.Contains(first.Reply, "补一句规则：") {
		t.Fatalf("first reply = %q, want appendix", first.Reply)
	}
	second, err := agent.HandleMatchEvent(context.Background(), varCheckRequest("ev-2", "normal"))
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if strings.Contains(second.Reply, "补一句规则：") {
		t.Fatalf("second reply = %q, want appendix suppressed by per-entry limit", second.Reply)
	}
}

func TestKnowledgeTriggerQuietAndNonJudgmentSilent(t *testing.T) {
	agent := triggerAgent(t)
	quiet, err := agent.HandleMatchEvent(context.Background(), varCheckRequest("ev-1", "quiet"))
	if err != nil {
		t.Fatalf("quiet: %v", err)
	}
	if strings.Contains(quiet.Reply, "补一句规则：") {
		t.Fatalf("quiet reply = %q, want no appendix", quiet.Reply)
	}

	// 非判罚事件（goal）不触发，即使条目存在。
	request := varCheckRequest("ev-3", "normal")
	request.Event.EventType = "goal"
	quiet, err = agent.HandleMatchEvent(context.Background(), request)
	if err != nil {
		t.Fatalf("goal: %v", err)
	}
	if strings.Contains(quiet.Reply, "补一句规则：") {
		t.Fatalf("goal reply = %q, want no appendix", quiet.Reply)
	}
}

func TestTriggerLookupPrefersHighestConfidence(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.yaml"), []byte(`id: low
topics: ["越位"]
triggers: ["var_check"]
quote: "低确信锚"
answer: "低确信答案"
source: "s"
confidence: 0.7
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.yaml"), []byte(`id: high
topics: ["红牌"]
triggers: ["var_check"]
quote: "高确信锚"
answer: "高确信答案"
source: "s"
confidence: 0.95
`), 0o644); err != nil {
		t.Fatal(err)
	}
	library, err := knowledge.Load(dir, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	entry, ok := library.TriggerLookup("var_check")
	if !ok || entry.ID != "high" {
		t.Fatalf("entry = %+v ok=%v, want high-confidence entry", entry, ok)
	}
	if _, ok := library.TriggerLookup("goal"); ok {
		t.Fatalf("goal should not trigger any entry")
	}
}

func TestTriggerEntryWithoutQuoteRejected(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte(`id: bad
topics: ["VAR"]
triggers: ["var_check"]
answer: "没有引语锚的回答"
source: "s"
confidence: 0.9
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := knowledge.Load(dir, nil); err == nil {
		t.Fatalf("Load accepted triggers entry without quote")
	}
}

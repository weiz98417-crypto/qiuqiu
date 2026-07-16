package companion

import (
	"context"
	"strings"
	"testing"
	"time"

	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/relationship"
)

func TestDirectorGoalQuestionsUseRecentMatchFacts(t *testing.T) {
	questions := []string{
		"刚才谁进的球？",
		"刚刚那个球谁破门的？",
		"最新这个进球是谁？",
	}
	for _, question := range questions {
		if got := Classify(question); got != IntentRecentEvent {
			t.Errorf("Classify(%q) = %q, want %q", question, got, IntentRecentEvent)
		}
	}

	events := []matchstate.MatchEvent{
		{ID: "evt-pressure", EventType: "pressure", Clock: "78:30", Description: "利物浦持续压迫。"},
		{ID: "evt-goal", EventType: "goal", Clock: "77:10", PlayerName: "萨拉赫", Description: "萨拉赫禁区内推射破门。"},
	}
	reply, ids := answerRecentEvent(questions[0], events)
	if !strings.Contains(reply, "萨拉赫") {
		t.Fatalf("goal reply = %q, want scorer", reply)
	}
	if len(ids) != 1 || ids[0] != "evt-goal" {
		t.Fatalf("retrieved ids = %v, want latest goal", ids)
	}
}

func TestDisbeliefReactionGetsARealConversationTurn(t *testing.T) {
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore())).WithDirector(
		relationship.NewDirector(relationship.NewMemoryRepository()),
	).WithRealizer(fakeRealizer{text: "嗯，我在呢。"}, time.Second)
	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "disbelief-turn-1",
		MatchID:  "match-disbelief",
		UserID:   "user-1",
		Text:     "真的假的",
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if response.Intent != IntentEmotionReaction {
		t.Fatalf("intent = %q, want %q", response.Intent, IntentEmotionReaction)
	}
	if response.Reply == "嗯，我在。" {
		t.Fatal("disbelief reaction fell back to presence acknowledgement")
	}
	if !strings.Contains(response.Reply, "真的假的") || !strings.Contains(response.Reply, "刚刚") {
		t.Fatalf("reply = %q, want disbelief acknowledgement with a contextual follow-up", response.Reply)
	}
}

func TestUserRepliesDoNotExposeEnterpriseFactSources(t *testing.T) {
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore()))
	inputs := []string{
		"哈哈太激动了",
		"刚才比赛发生了什么？",
		"有人进球了吗？",
	}
	for _, input := range inputs {
		response, err := agent.HandleMessage(context.Background(), MessageRequest{
			MatchID: "user-surface-language",
			UserID:  "user-1",
			Text:    input,
		})
		if err != nil {
			t.Fatalf("HandleMessage(%q) error: %v", input, err)
		}
		if strings.Contains(response.Reply, "导演台") || strings.Contains(response.Reply, "导播台") {
			t.Errorf("user reply leaked enterprise terminology: %q", response.Reply)
		}
	}
}

func TestDisplayPeriodNeverLeaksInternalPrematchCode(t *testing.T) {
	if got := displayPeriod("pre_match"); got != "赛前" {
		t.Fatalf("displayPeriod(pre_match) = %q, want 赛前", got)
	}
}

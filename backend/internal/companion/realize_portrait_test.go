package companion

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"qiuqiu/internal/llm"
	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/memory"
	"qiuqiu/internal/relationship"
)

// capturingRealizer records the realization request so tests can assert what
// the real prompt would have carried (the Replika law: the portrait must be
// wired into what QiuQiu actually says, not decorative).
type capturingRealizer struct {
	reply   string
	request RealizationRequest
}

func (realizer *capturingRealizer) Realize(_ context.Context, req RealizationRequest) (RealizedTurn, error) {
	realizer.request = req
	return RealizedTurn{Text: realizer.reply}, nil
}

func seedPortraitAgent(t *testing.T) (*Agent, *capturingRealizer, *memory.Fake) {
	t.Helper()
	fake := memory.NewFake()
	updatedAt := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	entries := []memory.PortraitEntry{{
		ID:        "prof-1",
		Topic:     "basic_info",
		SubTopic:  "favorite_player",
		Content:   "佩德里",
		UpdatedAt: updatedAt,
		Source:    memory.PortraitSourceSynthesis,
	}}
	fake.SetPortrait("user-1", memory.Portrait{
		Block:     memory.RenderPortraitBlock(entries, updatedAt),
		Entries:   entries,
		UpdatedAt: updatedAt,
	})
	realizer := &capturingRealizer{reply: "嗯，看着呢。"}
	agent := NewAgent(NewRepositoryMemoryTools(matchstate.NewStore())).
		WithMemories(fake).
		WithRealizer(realizer, time.Second)
	return agent, realizer, fake
}

func TestAgentInjectsPortraitBlockIntoRealization(t *testing.T) {
	agent, realizer, _ := seedPortraitAgent(t)
	ctx := context.Background()

	response, err := agent.HandleMessage(ctx, MessageRequest{
		SignalID: "portrait-inject", MatchID: "match-1", UserID: "user-1",
		Text: "最近挺累的，今晚就想轻松看场球", Now: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if !strings.Contains(realizer.request.PortraitContext, "【用户画像】") {
		t.Fatalf("portrait context = %q, want the bounded portrait block", realizer.request.PortraitContext)
	}
	if !strings.Contains(realizer.request.PortraitContext, "佩德里") || !strings.Contains(realizer.request.PortraitContext, "画像更新：2026-09-10") {
		t.Fatalf("portrait context = %q, want the seeded fact and the UpdatedAt citation", realizer.request.PortraitContext)
	}
	if !strings.Contains(realizer.request.PortraitContext, "不得据此新增赛况事实") {
		t.Fatalf("portrait context = %q, want the fact-discipline header", realizer.request.PortraitContext)
	}
	calledPortrait := false
	for _, call := range response.Trace.ToolCalls {
		if call.Name == "memory.portrait" {
			calledPortrait = true
			if call.Args["entries"] != "1" {
				t.Fatalf("memory.portrait args = %+v, want the entry count", call.Args)
			}
		}
	}
	if !calledPortrait {
		t.Fatalf("trace tool calls = %+v, want memory.portrait", response.Trace.ToolCalls)
	}
}

// TestPortraitForgetDegradesToNoneOnNextTurn is the companion-level delete
// eval: once the portrait slot is tombstoned (the seam applies the C3 page's
// delete), the very next realization request renders 无 instead of the fact.
func TestPortraitForgetDegradesToNoneOnNextTurn(t *testing.T) {
	agent, realizer, fake := seedPortraitAgent(t)
	ctx := context.Background()

	if _, err := agent.HandleMessage(ctx, MessageRequest{
		SignalID: "portrait-before", MatchID: "match-1", UserID: "user-1",
		Text: "最近挺累的，今晚就想轻松看场球", Now: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if !strings.Contains(realizer.request.PortraitContext, "佩德里") {
		t.Fatalf("portrait context = %q, want the fact before deletion", realizer.request.PortraitContext)
	}

	fake.ForgetPortraitEntries("user-1", "favorite_player")

	if _, err := agent.HandleMessage(ctx, MessageRequest{
		SignalID: "portrait-after", MatchID: "match-1", UserID: "user-1",
		Text: "今晚还有什么可聊的", Now: time.Now().Add(time.Second).UTC(),
	}); err != nil {
		t.Fatalf("HandleMessage after forget: %v", err)
	}
	if strings.TrimSpace(realizer.request.PortraitContext) != "" {
		t.Fatalf("portrait context after forget = %q, want empty so the prompt renders 无", realizer.request.PortraitContext)
	}
}

func TestAgentPortraitStaysEmptyWithoutMemorySeam(t *testing.T) {
	realizer := &capturingRealizer{reply: "嗯。"}
	agent := NewAgent(NewRepositoryMemoryTools(matchstate.NewStore())).WithRealizer(realizer, time.Second)
	if _, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "portrait-none", MatchID: "match-1", UserID: "user-1",
		Text: "最近挺累的", Now: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if realizer.request.PortraitContext != "" {
		t.Fatalf("portrait context without a memory seam = %q, want empty", realizer.request.PortraitContext)
	}
}

func TestLLMReplyRealizerRendersPortraitOrNone(t *testing.T) {
	var prompt string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request llm.ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(request.Messages) > 1 {
			prompt = request.Messages[1].Content
		}
		_ = json.NewEncoder(w).Encode(llm.ChatResponse{
			Choices: []llm.Choice{{Message: llm.Message{Role: "assistant", Content: "嗯。"}}},
		})
	}))
	defer server.Close()

	realizer := NewLLMReplyRealizer(llm.NewClient(server.URL, "test-key", "test-model"))
	decision := relationship.Decision{
		Actions:      []relationship.CommunicationAct{relationship.ActReact},
		Relationship: relationship.RelationshipView{Stage: relationship.StageFamiliar},
		Speech:       &relationship.SpeechPlan{Content: relationship.ContentPolicy{Goal: "chat", MaxSentences: 2, MaxCharacters: 80}},
	}

	if _, err := realizer.Realize(context.Background(), RealizationRequest{
		UserInput:       "今晚看什么",
		Intent:          IntentSmalltalk,
		Decision:        decision,
		PortraitContext: "【用户画像】长期综合记忆，仅供自然带过，不得据此新增赛况事实：\n- 基本信息/favorite_player：佩德里",
	}); err != nil {
		t.Fatalf("Realize: %v", err)
	}
	if !strings.Contains(prompt, "用户画像参考：【用户画像】") || !strings.Contains(prompt, "佩德里") {
		t.Fatalf("prompt = %q, want the portrait block injected", prompt)
	}

	if _, err := realizer.Realize(context.Background(), RealizationRequest{
		UserInput: "今晚看什么",
		Intent:    IntentSmalltalk,
		Decision:  decision,
	}); err != nil {
		t.Fatalf("Realize without portrait: %v", err)
	}
	if !strings.Contains(prompt, "用户画像参考：无") {
		t.Fatalf("prompt = %q, want 无 for the unconfigured portrait", prompt)
	}
}

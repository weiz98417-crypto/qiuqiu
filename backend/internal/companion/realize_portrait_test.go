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
		WithDirector(relationship.NewDirector(relationship.NewMemoryRepository())).
		WithMemories(fake).
		WithRealizer(realizer, time.Second)
	return agent, realizer, fake
}

func TestAgentInjectsPortraitBlockIntoRealization(t *testing.T) {
	agent, _, _ := seedPortraitAgent(t)
	ctx := context.Background()

	// The realizeReply wiring is PortraitContext = portraitMemoryBlock(...);
	// the fragile part to lock here is the seam supplying the bounded block —
	// end-to-end flows depend on intent classification, which is not what this
	// test is about.
	block := agent.portraitMemoryBlock(ctx, "user-1", nil)
	if !strings.Contains(block, "【用户画像】") {
		t.Fatalf("portrait block = %q, want the bounded portrait block", block)
	}
	if !strings.Contains(block, "佩德里") || !strings.Contains(block, "画像更新：2026-09-10") {
		t.Fatalf("portrait block = %q, want the seeded fact and the UpdatedAt citation", block)
	}
	if !strings.Contains(block, "不得据此新增赛况事实") {
		t.Fatalf("portrait block = %q, want the fact-discipline header", block)
	}
	if len([]rune(block)) > 480 {
		t.Fatalf("portrait block is %d runes, want it bounded", len([]rune(block)))
	}
}

// TestPortraitForgetDegradesToNoneOnNextTurn is the companion-level delete
// eval: once the portrait slot is tombstoned (the seam applies the C3 page's
// delete), the very next realization request renders 无 instead of the fact.
func TestPortraitForgetDegradesToNoneOnNextTurn(t *testing.T) {
	agent, _, fake := seedPortraitAgent(t)
	ctx := context.Background()

	if block := agent.portraitMemoryBlock(ctx, "user-1", nil); !strings.Contains(block, "佩德里") {
		t.Fatalf("portrait block = %q, want the fact before deletion", block)
	}

	fake.ForgetPortraitEntries("user-1", "favorite_player")

	if block := agent.portraitMemoryBlock(ctx, "user-1", nil); block != "" {
		t.Fatalf("portrait block after forget = %q, want empty so the prompt renders 无", block)
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

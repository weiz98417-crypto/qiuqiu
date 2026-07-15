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
	"qiuqiu/internal/relationship"
)

func TestLLMReplyRealizerReceivesNaturalRelationshipMemoryContext(t *testing.T) {
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
			Choices: []llm.Choice{{Message: llm.Message{Role: "assistant", Content: "这场压得更凶。"}}},
		})
	}))
	defer server.Close()

	payload, err := json.Marshal(relationship.TasteMemoryPayload{Subject: "高位压迫", Direction: "like"})
	if err != nil {
		t.Fatalf("marshal memory: %v", err)
	}
	realizer := NewLLMReplyRealizer(llm.NewClient(server.URL, "test-key", "test-model"))
	_, err = realizer.Realize(context.Background(), RealizationRequest{
		UserInput: "这场的高位压迫你怎么看？",
		Intent:    IntentSmalltalk,
		Decision: relationship.Decision{
			Actions:      []relationship.CommunicationAct{relationship.ActOpinion},
			Relationship: relationship.RelationshipView{Stage: relationship.StageFamiliar},
			Speech:       &relationship.SpeechPlan{Content: relationship.ContentPolicy{Goal: "opinion", MaxSentences: 2, MaxCharacters: 80}},
			Memories:     []relationship.RelationshipMemory{{Kind: relationship.MemoryKindTasteEvidence, Payload: payload}},
		},
	})
	if err != nil {
		t.Fatalf("Realize: %v", err)
	}
	if !strings.Contains(prompt, "可自然引用：偏好高位压迫") {
		t.Fatalf("prompt = %q", prompt)
	}
	if strings.Contains(prompt, "历史记录") || strings.Contains(prompt, "后台") {
		t.Fatalf("prompt exposes internal memory language: %q", prompt)
	}
}

func TestAgentRelationshipMemoryPointsToOriginatingTrace(t *testing.T) {
	director := relationship.NewDirector(relationship.NewMemoryRepository())
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore())).
		WithDirector(director).
		WithRealizer(fakeRealizer{text: "嗯，这点挺鲜明。"}, time.Second)
	ctx := context.Background()
	now := time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC)

	origin, err := agent.HandleMessage(ctx, MessageRequest{
		SignalID: "taste-origin", MatchID: "match-1", UserID: "user-1", Text: "我更吃高位压迫这一套", Now: now,
	})
	if err != nil {
		t.Fatalf("record preference: %v", err)
	}
	recalled, err := agent.HandleMessage(ctx, MessageRequest{
		SignalID: "taste-recall", MatchID: "match-2", UserID: "user-1", Text: "这场高位压迫又来了", Now: now.Add(7 * 24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("recall preference: %v", err)
	}
	decision := recalled.Trace.RelationshipDecision
	if decision == nil || len(decision.Memories) != 1 {
		t.Fatalf("decision memories = %+v", decision)
	}
	if decision.Memories[0].SourceTraceID != origin.Trace.ID {
		t.Fatalf("source trace = %q, want %q", decision.Memories[0].SourceTraceID, origin.Trace.ID)
	}
}

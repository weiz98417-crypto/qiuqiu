package main

import (
	"strings"
	"testing"

	"qiuqiu/internal/config"
)

func TestEvalMiMoKeyConfiguresTextLLM(t *testing.T) {
	cfg := &config.Config{
		MiMoAPIKey:  "mimo-key",
		MiMoBaseURL: "https://api.xiaomimimo.com/v1",
		MiMoModel:   "mimo-v2.5-pro",
	}

	llmClient := newTextLLMClient(cfg)
	if llmClient == nil {
		t.Fatalf("expected MiMo key to configure text LLM client")
	}
	if !strings.Contains(llmClient.DebugString(), "xiaomimimo.com") {
		t.Fatalf("expected text LLM client to use MiMo base URL, got %s", llmClient.DebugString())
	}

	cfg.MiMoAPIKey = ""
	if newTextLLMClient(cfg) != nil {
		t.Fatal("empty MiMo key should disable the text LLM client")
	}
}

package main

import (
	"strings"
	"testing"

	"qiuqiu/internal/config"
)

func TestEvalMiMoKeyDoesNotConfigureTextLLM(t *testing.T) {
	cfg := &config.Config{
		MiMoAPIKey:      "mimo-voice-key",
		MiMoBaseURL:     "https://api.xiaomimimo.com/v1",
		MiMoModel:       "mimo-v2.5",
		DeepseekBaseURL: "https://api.deepseek.com/v1",
		DeepseekModel:   "deepseek-v4-flash",
	}

	llmClient := newTextLLMClient(cfg)
	if llmClient != nil {
		t.Fatalf("MiMo voice key should not configure text LLM polish client")
	}

	cfg.DeepseekAPIKey = "deepseek-key"
	llmClient = newTextLLMClient(cfg)
	if llmClient == nil {
		t.Fatalf("expected DeepSeek key to configure text LLM client")
	}
	if !strings.Contains(llmClient.DebugString(), "deepseek") {
		t.Fatalf("expected text LLM client to use DeepSeek base URL, got %s", llmClient.DebugString())
	}
}

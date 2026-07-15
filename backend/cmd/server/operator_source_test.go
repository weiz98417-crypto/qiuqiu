package main

import (
	"os"
	"strings"
	"testing"
)

func TestExternalSourceIsConfiguredButNeverAutoStarted(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	text := string(source)
	for _, required := range []string{
		"cfg.APISportsAPIKey",
		"datasource.NewClient",
		"datasource.NewManager",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("server should wire controllable sports data provider %q", required)
		}
	}
	if strings.Contains(text, "sourceManager.Start(") {
		t.Fatal("server must not auto-start an external sports data source")
	}
}

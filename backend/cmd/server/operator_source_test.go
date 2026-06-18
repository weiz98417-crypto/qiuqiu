package main

import (
	"os"
	"strings"
	"testing"
)

func TestEvalDirectorConsoleIsTheMatchFactSource(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	text := string(source)
	for _, forbidden := range []string{
		"APISPORTS_API_KEY",
		"datasource.NewClient",
		"datasource.NewPoller",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("server should not auto-wire sports data provider %q; director console is the match fact source", forbidden)
		}
	}
}

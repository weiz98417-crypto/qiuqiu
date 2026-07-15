package evals

import (
	"context"
	"path/filepath"
	"testing"

	"qiuqiu/internal/matchstate"
)

func TestVersionedEvalCasesPass(t *testing.T) {
	cases, err := LoadCases(filepath.Join("..", "..", "..", "evals", "cases"))
	if err != nil {
		t.Fatalf("LoadCases: %v", err)
	}
	if len(cases) < 8 {
		t.Fatalf("expected a meaningful eval corpus, got %d cases", len(cases))
	}
	report := Run(context.Background(), cases)
	if report.Scorecard.FailedCases != 0 {
		t.Fatalf("eval report has failures: %+v", report)
	}
	if report.Scorecard.FactSafetyRate != 1 || report.Scorecard.TrajectoryRate != 1 || report.Scorecard.TraceCompleteRate != 1 {
		t.Fatalf("expected deterministic graders to pass: %+v", report.Scorecard)
	}
}

func TestLoadCasesRejectsUnknownSuite(t *testing.T) {
	err := (Case{Version: SchemaVersion, ID: "invalid", Suite: "exploratory", Config: matchstate.MatchConfig{HomeTeam: "A", AwayTeam: "B"}}).Validate()
	if err == nil {
		t.Fatal("expected invalid suite to fail validation")
	}
}

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
	// router_net 真网档需要真 key 且有费用/抖动，与 CLI -suite all 同语义：
	// 默认门禁不跑，显式点名才执行。
	var gated []Case
	for _, evalCase := range cases {
		if evalCase.Suite == "router_net" {
			continue
		}
		gated = append(gated, evalCase)
	}
	report := Run(context.Background(), gated)
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

func TestPortraitSeedReachesMemoryContextAndForgetDropsIt(t *testing.T) {
	seed := &PortraitSeed{UserID: "fan-9", Entries: []PortraitSeedEntry{{Topic: "basic_info", SubTopic: "favorite_player", Content: "佩德里"}}}
	passing := Case{
		Version: SchemaVersion, ID: "regression.portrait-grading-pass", Suite: "regression",
		Config: matchstate.MatchConfig{HomeTeam: "西班牙", AwayTeam: "德国"},
		Realizer: &RealizerFixture{Reply: "嗯，一起看着呢。"},
		Portrait: seed,
		Turns: []TurnStep{{
			ID: "tangential", UserID: "fan-9", Text: "最近工作好累，今晚就想轻松看场球",
			Expect: TurnExpectation{RequiredTools: []string{"memory.portrait"}, MemoryMustMention: []string{"佩德里"}},
		}},
	}
	leaked := Case{
		Version: SchemaVersion, ID: "regression.portrait-grading-leak", Suite: "regression",
		Config: matchstate.MatchConfig{HomeTeam: "西班牙", AwayTeam: "德国"},
		Realizer: &RealizerFixture{Reply: "嗯，一起看着呢。"},
		Portrait: seed,
		Turns: []TurnStep{
			{
				// No expectations here; the forget applies after this turn.
				ID: "seed", UserID: "fan-9", Text: "最近工作好累",
				ForgetPortrait: []string{"favorite_player"},
			},
			{
				// The fact was forgotten, so expecting it must FAIL: this
				// proves the grader detects a fact leaving the context.
				ID: "after-delete", UserID: "fan-9", Text: "最近工作好累，今晚就想轻松看场球",
				Expect: TurnExpectation{MemoryMustMention: []string{"佩德里"}},
			},
		},
	}
	report := Run(context.Background(), []Case{passing, leaked})
	if len(report.Cases) != 2 {
		t.Fatalf("cases = %d, want 2", len(report.Cases))
	}
	if !report.Cases[0].Passed {
		t.Fatalf("seeded portrait should satisfy memoryMustMention: %+v", report.Cases[0].Failures)
	}
	if report.Cases[1].Passed {
		t.Fatal("after forgetPortrait, a turn expecting the fact must fail (the grader must see it leave the context)")
	}
}

package evals

import (
	"testing"

	"qiuqiu/internal/companion"
	"qiuqiu/internal/matchstate"
)

func TestAuditTracesFindsTruthAndToolBoundaryViolations(t *testing.T) {
	report := AuditTraces([]companion.Trace{
		{
			ID:             "valid",
			Intent:         companion.IntentRecentEvent,
			Input:          "刚才谁助攻？",
			Output:         "法比安助攻。",
			Reason:         "deterministic_companion_policy",
			RetrievedEvent: []string{"evt_1"},
			ToolCalls: []companion.ToolCall{
				{Name: "match.search_events"},
				{Name: "conversation.append_turn"},
				{Name: "trace.write_decision"},
				{Name: "response.emit_companion_reply"},
			},
		},
		{
			ID:     "invalid",
			Intent: companion.IntentMatchStatus,
			Output: "1-0",
			ToolCalls: []companion.ToolCall{
				{Name: "operator.create_event"},
			},
		},
	})
	if report.Blockers < 3 {
		t.Fatalf("expected missing reason, mutation tool, and snapshot violations: %+v", report)
	}
	if report.TraceCount != 2 {
		t.Fatalf("wrong trace count: %+v", report)
	}
}

func TestAuditTracesRequiresUserClaimVerificationTrajectory(t *testing.T) {
	report := AuditTraces([]companion.Trace{{
		ID:     "unsafe-claim",
		Intent: companion.IntentMatchClaim,
		Input:  "德国已经3比0领先了",
		Output: "德国3比0了。",
		Reason: "deterministic_companion_policy",
		ToolCalls: []companion.ToolCall{
			{Name: "conversation.append_turn"},
			{Name: "trace.write_decision"},
			{Name: "response.emit_companion_reply"},
		},
	}})
	if report.Blockers < 3 {
		t.Fatalf("expected missing claim, snapshot, and verification blockers: %+v", report)
	}
}

func TestAuditTracesRejectsContradictedScoreInOutput(t *testing.T) {
	claimed := matchstate.Score{Home: 0, Away: 3}
	actual := matchstate.Score{Home: 0, Away: 0}
	report := AuditTraces([]companion.Trace{{
		ID:     "unsafe-score-output",
		Intent: companion.IntentMatchClaim,
		Input:  "德国已经3比0领先了",
		Output: "西班牙 0-0 德国，不过德国确实3比0领先了。",
		Reason: "user_match_claim_contradicted",
		Claim: &companion.FactClaim{
			Kind:         "score",
			Status:       companion.ClaimStatusContradicted,
			ClaimedScore: &claimed,
			ActualScore:  &actual,
		},
		ToolCalls: []companion.ToolCall{
			{Name: "match.read_snapshot"},
			{Name: "match.search_events"},
			{Name: "match.verify_user_claim"},
			{Name: "conversation.append_turn"},
			{Name: "trace.write_decision"},
			{Name: "response.emit_companion_reply"},
		},
	}})
	if report.Blockers == 0 {
		t.Fatalf("expected contradicted score output blocker: %+v", report)
	}
}

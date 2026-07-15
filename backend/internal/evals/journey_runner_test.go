package evals

import (
	"context"
	"testing"
	"time"

	"qiuqiu/internal/relationship"
)

func TestTwoWeekRelationshipJourney(t *testing.T) {
	start := time.Date(2026, 7, 1, 20, 0, 0, 0, time.UTC)
	active := true
	inactive := false
	oneBoundary := 1
	journey := RelationshipJourney{
		ID: "two-week-ballmate",
		Steps: []RelationshipJourneyStep{
			{ID: "open-1", At: start, Signal: relationship.Signal{ID: "open-1", Kind: relationship.SignalSessionOpened, UserID: "user-1", MatchID: "match-1"}},
			{ID: "taste", At: start.Add(time.Minute), Signal: relationship.Signal{ID: "taste", Kind: relationship.SignalUserTurn, UserID: "user-1", MatchID: "match-1", User: &relationship.UserSignal{Text: "我更吃高位压迫这一套"}}},
			{ID: "thread", At: start.Add(2 * time.Minute), Signal: relationship.Signal{ID: "thread", Kind: relationship.SignalUserTurn, UserID: "user-1", MatchID: "match-1", User: &relationship.UserSignal{Text: "高位压迫这个问题，下场接着聊"}}},
			{ID: "open-2", At: start.Add(7 * 24 * time.Hour), Signal: relationship.Signal{ID: "open-2", Kind: relationship.SignalSessionOpened, UserID: "user-1", MatchID: "match-2"}},
			{ID: "recall", At: start.Add(7*24*time.Hour + time.Minute), Signal: relationship.Signal{ID: "recall", Kind: relationship.SignalUserTurn, UserID: "user-1", MatchID: "match-2", User: &relationship.UserSignal{Text: "接着上次说高位压迫"}}, Expect: RelationshipJourneyExpectation{Stage: relationship.StageFamiliar, RequiredActions: []relationship.CommunicationAct{relationship.ActRecall}, MemoryKinds: []string{relationship.MemoryKindOpenThread}}},
			{ID: "rupture", At: start.Add(7*24*time.Hour + 2*time.Minute), Signal: relationship.Signal{ID: "rupture", Kind: relationship.SignalUserTurn, UserID: "user-1", MatchID: "match-2", User: &relationship.UserSignal{Text: "别拿这个开我玩笑了，烦", Cues: []relationship.UserCue{{Kind: relationship.CueBanterDenied, Scope: "prediction"}}}}, Expect: RelationshipJourneyExpectation{RepairActive: &active, BoundaryCount: &oneBoundary, RequiredActions: []relationship.CommunicationAct{relationship.ActRepair}}},
			{ID: "repair-1", At: start.Add(7*24*time.Hour + 3*time.Minute), Signal: relationship.Signal{ID: "repair-1", Kind: relationship.SignalUserTurn, UserID: "user-1", MatchID: "match-2", User: &relationship.UserSignal{Text: "继续看"}}, Expect: RelationshipJourneyExpectation{RepairActive: &active, ForbiddenActions: []relationship.CommunicationAct{relationship.ActAsk, relationship.ActAnalyze, relationship.ActTease}}},
			{ID: "repair-2", At: start.Add(14 * 24 * time.Hour), Signal: relationship.Signal{ID: "repair-2", Kind: relationship.SignalUserTurn, UserID: "user-1", MatchID: "match-3", User: &relationship.UserSignal{Text: "新一场，继续"}}, Expect: RelationshipJourneyExpectation{RepairActive: &active, ForbiddenActions: []relationship.CommunicationAct{relationship.ActAsk, relationship.ActAnalyze, relationship.ActTease}}},
			{ID: "repair-3", At: start.Add(14*24*time.Hour + time.Minute), Signal: relationship.Signal{ID: "repair-3", Kind: relationship.SignalUserTurn, UserID: "user-1", MatchID: "match-3", User: &relationship.UserSignal{Text: "这次短点就挺好"}}, Expect: RelationshipJourneyExpectation{RepairActive: &inactive}},
			{ID: "open-3", At: start.Add(14*24*time.Hour + 2*time.Minute), Signal: relationship.Signal{ID: "open-3", Kind: relationship.SignalSessionOpened, UserID: "user-1", MatchID: "match-3"}},
			{ID: "trust", At: start.Add(14*24*time.Hour + 3*time.Minute), Signal: relationship.Signal{ID: "trust", Kind: relationship.SignalUserTurn, UserID: "user-1", MatchID: "match-3", User: &relationship.UserSignal{Text: "你上次那个判断挺准，接着来", Cues: []relationship.UserCue{{Kind: relationship.CueAcceptedJudgment}, {Kind: relationship.CueAcceptedInitiative}, {Kind: relationship.CueBanterAllowed, Scope: "match_judgment"}}}}, Expect: RelationshipJourneyExpectation{Stage: relationship.StageWatchBuddy}},
			{ID: "taste-recall", At: start.Add(14*24*time.Hour + 4*time.Minute), Signal: relationship.Signal{ID: "taste-recall", Kind: relationship.SignalUserTurn, UserID: "user-1", MatchID: "match-3", User: &relationship.UserSignal{Text: "这场高位压迫又来了"}}, Expect: RelationshipJourneyExpectation{Stage: relationship.StageWatchBuddy, MemoryKinds: []string{relationship.MemoryKindTasteEvidence}}},
		},
	}

	result := RunRelationshipJourney(context.Background(), relationship.NewDirector(relationship.NewMemoryRepository()), journey)
	if !result.Passed {
		t.Fatalf("journey failures: %v", result.Failures)
	}
	if len(result.Decisions) != len(journey.Steps) {
		t.Fatalf("decisions = %d, want %d", len(result.Decisions), len(journey.Steps))
	}
}

package relationship

import (
	"context"
	"testing"
	"time"
)

func TestDirectorApplyIsIdempotent(t *testing.T) {
	director := NewDirector(NewMemoryRepository())
	now := time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC)
	signal := Signal{
		ID:         "signal-1",
		Kind:       SignalUserTurn,
		UserID:     "user-1",
		MatchID:    "match-1",
		OccurredAt: now,
		ReceivedAt: now,
		User:       &UserSignal{Text: "先看看比赛"},
		Grounding:  GroundedContent{Intent: "smalltalk", FactMode: FactModeNone},
	}

	first, err := director.Apply(context.Background(), signal)
	if err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	retried, err := director.Apply(context.Background(), signal)
	if err != nil {
		t.Fatalf("retried Apply: %v", err)
	}
	if retried.ID != first.ID {
		t.Fatalf("retried decision id = %q, want %q", retried.ID, first.ID)
	}
	if retried.StateVersion != first.StateVersion {
		t.Fatalf("retried state version = %d, want %d", retried.StateVersion, first.StateVersion)
	}

	next := signal
	next.ID = "signal-2"
	next.OccurredAt = now.Add(time.Second)
	next.ReceivedAt = next.OccurredAt
	second, err := director.Apply(context.Background(), next)
	if err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if second.StateVersion != first.StateVersion+1 {
		t.Fatalf("second state version = %d, want %d", second.StateVersion, first.StateVersion+1)
	}
}

func TestSignalIdempotencyIsScopedPerUser(t *testing.T) {
	director := NewDirector(NewMemoryRepository())
	now := time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC)
	first, err := director.Apply(context.Background(), Signal{
		ID: "shared-client-signal", Kind: SignalUserTurn, UserID: "user-1", MatchID: "match-1", OccurredAt: now, User: &UserSignal{Text: "继续"},
	})
	if err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	second, err := director.Apply(context.Background(), Signal{
		ID: "shared-client-signal", Kind: SignalUserTurn, UserID: "user-2", MatchID: "match-1", OccurredAt: now, User: &UserSignal{Text: "继续"},
	})
	if err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if first.ID == second.ID {
		t.Fatalf("cross-user decisions share id %q", first.ID)
	}
	if second.SignalID != "shared-client-signal" || second.StateVersion != 1 {
		t.Fatalf("second decision reused another user's state: %+v", second)
	}
	third, err := director.Apply(context.Background(), Signal{
		ID: "shared-client-signal", Kind: SignalUserTurn, UserID: "user-1", MatchID: "match-2", OccurredAt: now, User: &UserSignal{Text: "继续"},
	})
	if err != nil {
		t.Fatalf("third Apply: %v", err)
	}
	if third.ID == first.ID || third.StateVersion != 1 {
		t.Fatalf("cross-match decision reused old match state: %+v", third)
	}
}

func TestExplicitBoundaryDoesNotStartRepair(t *testing.T) {
	director := NewDirector(NewMemoryRepository())
	decision, err := director.Apply(context.Background(), Signal{
		ID:         "boundary-1",
		Kind:       SignalUserTurn,
		UserID:     "user-1",
		MatchID:    "match-1",
		OccurredAt: time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC),
		User:       &UserSignal{Text: "你别问工作细节，我不想说工作"},
		Grounding:  GroundedContent{Intent: "smalltalk", FactMode: FactModeNone},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(decision.Actions) != 1 || decision.Actions[0] != ActAcknowledge {
		t.Fatalf("actions = %v, want [%s]", decision.Actions, ActAcknowledge)
	}
	if decision.Relationship.RepairActive {
		t.Fatal("first explicit boundary must not start repair")
	}
	if decision.Relationship.BoundaryCount != 1 {
		t.Fatalf("boundary count = %d, want 1", decision.Relationship.BoundaryCount)
	}
}

func TestRepairPersistsUntilFollowThrough(t *testing.T) {
	director := NewDirector(NewMemoryRepository())
	now := time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC)
	feedback, err := director.Apply(context.Background(), Signal{
		ID:         "repair-1",
		Kind:       SignalUserTurn,
		UserID:     "user-1",
		MatchID:    "match-1",
		OccurredAt: now,
		User:       &UserSignal{Text: "你又开始讲大道理了"},
		Grounding:  GroundedContent{Intent: "smalltalk", FactMode: FactModeNone},
	})
	if err != nil {
		t.Fatalf("feedback Apply: %v", err)
	}
	if len(feedback.Actions) != 1 || feedback.Actions[0] != ActRepair {
		t.Fatalf("feedback actions = %v, want [%s]", feedback.Actions, ActRepair)
	}
	if !feedback.Relationship.RepairActive {
		t.Fatal("feedback must start repair")
	}

	for index := 0; index < 3; index++ {
		decision, applyErr := director.Apply(context.Background(), Signal{
			ID:         "repair-follow-" + string(rune('1'+index)),
			Kind:       SignalUserTurn,
			UserID:     "user-1",
			MatchID:    "match-1",
			OccurredAt: now.Add(time.Duration(index+1) * time.Minute),
			User:       &UserSignal{Text: "继续看球"},
			Grounding:  GroundedContent{Intent: "smalltalk", FactMode: FactModeNone},
		})
		if applyErr != nil {
			t.Fatalf("follow-up %d Apply: %v", index+1, applyErr)
		}
		if index < 2 && !decision.Relationship.RepairActive {
			t.Fatalf("repair cleared after only %d follow-up turns", index+1)
		}
		if index == 2 && decision.Relationship.RepairActive {
			t.Fatal("repair did not clear after three compliant follow-up turns")
		}
	}
}

func TestInteractionFeedbackUsesSpecificRepairCategories(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		category string
	}{
		{name: "over analysis", text: "你又开始讲大道理了", category: "over_analysis"},
		{name: "repetition", text: "你怎么老说我在陪你看？", category: "repetition"},
		{name: "banter boundary", text: "别拿这个开我玩笑了，烦", category: "banter_boundary"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := NewMemoryRepository()
			director := NewDirector(repository)
			_, err := director.Apply(context.Background(), Signal{
				ID:         "feedback-" + test.name,
				Kind:       SignalUserTurn,
				UserID:     "user-1",
				MatchID:    "match-1",
				OccurredAt: time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC),
				User:       &UserSignal{Text: test.text},
			})
			if err != nil {
				t.Fatalf("Apply: %v", err)
			}
			state, err := repository.Load(context.Background(), "user-1", "match-1")
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if state.Relationship.Repair.Category != test.category {
				t.Fatalf("repair category = %q, want %q", state.Relationship.Repair.Category, test.category)
			}
		})
	}
}

func TestGenericBanterDenialRevokesEveryActiveScope(t *testing.T) {
	repository := NewMemoryRepository()
	director := NewDirector(repository)
	now := time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC)
	_, err := director.Apply(context.Background(), Signal{
		ID:         "allow-banter",
		Kind:       SignalUserTurn,
		UserID:     "user-1",
		MatchID:    "match-1",
		OccurredAt: now,
		User: &UserSignal{Text: "可以吐槽", Cues: []UserCue{
			{Kind: CueBanterAllowed, Scope: "prediction"},
			{Kind: CueBanterAllowed, Scope: "match_judgment"},
		}},
	})
	if err != nil {
		t.Fatalf("allow Apply: %v", err)
	}
	revoked, err := director.Apply(context.Background(), Signal{
		ID:         "deny-banter",
		Kind:       SignalUserTurn,
		UserID:     "user-1",
		MatchID:    "match-1",
		OccurredAt: now.Add(time.Minute),
		User:       &UserSignal{Text: "别拿这个开我玩笑了，烦"},
	})
	if err != nil {
		t.Fatalf("deny Apply: %v", err)
	}
	if revoked.Relationship.AllowedBanterScopes != 0 {
		t.Fatalf("allowed scopes = %d, want 0", revoked.Relationship.AllowedBanterScopes)
	}
}

func TestMemoryRepositoryReturnsIndependentDecisionCopies(t *testing.T) {
	repository := NewMemoryRepository()
	director := NewDirector(repository)
	signal := Signal{
		ID:         "copy-safety",
		Kind:       SignalUserTurn,
		UserID:     "user-1",
		MatchID:    "match-1",
		OccurredAt: time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC),
		User:       &UserSignal{Text: "继续"},
	}
	first, err := director.Apply(context.Background(), signal)
	if err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	first.Actions[0] = ActSilence
	first.ReasonCodes[0] = "mutated"
	first.Speech.Actions[0] = ActSilence
	retried, err := director.Apply(context.Background(), signal)
	if err != nil {
		t.Fatalf("retry Apply: %v", err)
	}
	if retried.Actions[0] == ActSilence || retried.ReasonCodes[0] == "mutated" || retried.Speech.Actions[0] == ActSilence {
		t.Fatalf("stored decision was mutated through returned slices: %+v", retried)
	}
}

func TestMemoryRepositoryResetMatchKeepsCrossMatchRelationship(t *testing.T) {
	repository := NewMemoryRepository()
	director := NewDirector(repository)
	now := time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC)
	for _, signal := range []Signal{
		{ID: "match-1-signal", Kind: SignalUserTurn, UserID: "user-1", MatchID: "match-1", OccurredAt: now, User: &UserSignal{Text: "继续"}},
		{ID: "match-2-signal", Kind: SignalUserTurn, UserID: "user-1", MatchID: "match-2", OccurredAt: now, User: &UserSignal{Text: "继续"}},
	} {
		if _, err := director.Apply(context.Background(), signal); err != nil {
			t.Fatalf("Apply %s: %v", signal.ID, err)
		}
	}
	if err := repository.ResetMatch("match-1"); err != nil {
		t.Fatalf("ResetMatch: %v", err)
	}
	matchOne, err := repository.Load(context.Background(), "user-1", "match-1")
	if err != nil {
		t.Fatalf("Load match-1: %v", err)
	}
	matchTwo, err := repository.Load(context.Background(), "user-1", "match-2")
	if err != nil {
		t.Fatalf("Load match-2: %v", err)
	}
	if matchOne.Match.Version != 0 || matchTwo.Match.Version != 1 || matchOne.Relationship.Version != 2 {
		t.Fatalf("unexpected states after reset: matchOne=%+v matchTwo=%+v", matchOne, matchTwo)
	}
	if _, ok, err := repository.DecisionBySignal(context.Background(), "user-1", "match-1", "match-1-signal"); err != nil || ok {
		t.Fatalf("reset match decision still exists: ok=%v err=%v", ok, err)
	}
	if _, ok, err := repository.DecisionBySignal(context.Background(), "user-1", "match-2", "match-2-signal"); err != nil || !ok {
		t.Fatalf("other match decision missing: ok=%v err=%v", ok, err)
	}
}

func TestAffectCarriesAcrossGoalVARAndCancellation(t *testing.T) {
	director := NewDirector(NewMemoryRepository())
	ctx := context.Background()
	now := time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC)
	goal, err := director.Apply(ctx, Signal{
		ID:         "event-goal",
		Kind:       SignalMatchEvent,
		UserID:     "user-1",
		MatchID:    "match-1",
		OccurredAt: now,
		Match:      &MatchSignal{EventID: "goal-1", EventType: "goal", Intensity: 5},
	})
	if err != nil {
		t.Fatalf("goal Apply: %v", err)
	}
	if goal.Presentation.Affect.Valence <= 0.5 || goal.Presentation.Affect.Arousal <= 0.7 {
		t.Fatalf("goal affect = %+v", goal.Presentation.Affect)
	}
	if goal.Presentation.Expression != "excited" {
		t.Fatalf("goal expression = %q, want excited", goal.Presentation.Expression)
	}

	checking, err := director.Apply(ctx, Signal{
		ID:         "event-var",
		Kind:       SignalMatchEvent,
		UserID:     "user-1",
		MatchID:    "match-1",
		OccurredAt: now.Add(time.Second),
		Match:      &MatchSignal{EventID: "var-1", EventType: "var_check", Intensity: 5},
	})
	if err != nil {
		t.Fatalf("VAR Apply: %v", err)
	}
	if checking.Presentation.Affect.Valence <= 0 {
		t.Fatalf("VAR reset positive momentum: %+v", checking.Presentation.Affect)
	}
	if checking.Presentation.Affect.Tension <= goal.Presentation.Affect.Tension {
		t.Fatalf("VAR tension = %.2f, want > %.2f", checking.Presentation.Affect.Tension, goal.Presentation.Affect.Tension)
	}
	if checking.Presentation.Affect.Confidence >= goal.Presentation.Affect.Confidence {
		t.Fatalf("VAR confidence = %.2f, want < %.2f", checking.Presentation.Affect.Confidence, goal.Presentation.Affect.Confidence)
	}

	cancelled, err := director.Apply(ctx, Signal{
		ID:         "event-cancelled",
		Kind:       SignalMatchEvent,
		UserID:     "user-1",
		MatchID:    "match-1",
		OccurredAt: now.Add(3 * time.Second),
		Match:      &MatchSignal{EventID: "cancel-1", EventType: "goal_cancelled", Intensity: 5},
	})
	if err != nil {
		t.Fatalf("cancellation Apply: %v", err)
	}
	if cancelled.Presentation.Affect.Valence >= 0 {
		t.Fatalf("cancelled goal valence = %.2f, want negative", cancelled.Presentation.Affect.Valence)
	}
	if cancelled.Presentation.Expression != "deflated" {
		t.Fatalf("cancelled expression = %q, want deflated", cancelled.Presentation.Expression)
	}
}

func TestRelationshipStagesRequireDiverseCrossMatchEvidence(t *testing.T) {
	director := NewDirector(NewMemoryRepository())
	ctx := context.Background()
	now := time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC)
	apply := func(id, matchID string, kind SignalKind, cues ...UserCue) Decision {
		t.Helper()
		signal := Signal{ID: id, Kind: kind, UserID: "user-1", MatchID: matchID, OccurredAt: now}
		if kind == SignalUserTurn {
			signal.User = &UserSignal{Text: "继续", Cues: cues}
		}
		decision, err := director.Apply(ctx, signal)
		if err != nil {
			t.Fatalf("Apply %s: %v", id, err)
		}
		return decision
	}

	apply("open-1", "match-1", SignalSessionOpened)
	apply("preference", "match-1", SignalUserTurn, UserCue{Kind: CueStablePreference})
	apply("open-2", "match-2", SignalSessionOpened)
	familiar := apply("thread", "match-2", SignalUserTurn, UserCue{Kind: CueContinuedThread})
	if familiar.Relationship.Stage != StageFamiliar {
		t.Fatalf("stage = %q, want %q", familiar.Relationship.Stage, StageFamiliar)
	}

	apply("open-3", "match-3", SignalSessionOpened)
	watchBuddy := apply("trust", "match-3", SignalUserTurn,
		UserCue{Kind: CueAcceptedJudgment},
		UserCue{Kind: CueAcceptedInitiative},
		UserCue{Kind: CueBanterAllowed, Scope: "prediction"},
	)
	if watchBuddy.Relationship.Stage != StageWatchBuddy {
		t.Fatalf("stage = %q, want %q", watchBuddy.Relationship.Stage, StageWatchBuddy)
	}

	apply("open-4", "match-4", SignalSessionOpened)
	apply("open-5", "match-5", SignalSessionOpened)
	apply("open-6", "match-6", SignalSessionOpened)
	oldBallmate := apply("old-evidence", "match-6", SignalUserTurn,
		UserCue{Kind: CueSharedMomentRecalled},
		UserCue{Kind: CueContinuedDisagreement},
	)
	if oldBallmate.Relationship.Stage != StageOldBallmate {
		t.Fatalf("stage = %q, want %q", oldBallmate.Relationship.Stage, StageOldBallmate)
	}
}

func TestMutedMatchEventStillUpdatesAffect(t *testing.T) {
	director := NewDirector(NewMemoryRepository())
	decision, err := director.Apply(context.Background(), Signal{
		ID:         "muted-goal",
		Kind:       SignalMatchEvent,
		UserID:     "user-1",
		MatchID:    "match-1",
		OccurredAt: time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC),
		Match: &MatchSignal{
			EventID:       "goal-1",
			EventType:     "goal",
			Intensity:     5,
			OutputAllowed: false,
		},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if decision.Speech != nil {
		t.Fatalf("speech = %+v, want nil when output is disabled", decision.Speech)
	}
	if len(decision.Actions) != 1 || decision.Actions[0] != ActSilence {
		t.Fatalf("actions = %v, want [%s]", decision.Actions, ActSilence)
	}
	if decision.Presentation.Affect.Valence <= 0.5 {
		t.Fatalf("muted goal did not update affect: %+v", decision.Presentation.Affect)
	}
	if decision.Presentation.Expression != "excited" {
		t.Fatalf("expression = %q, want excited", decision.Presentation.Expression)
	}
}

func TestBanterPermissionCanBeGrantedAndRevoked(t *testing.T) {
	director := NewDirector(NewMemoryRepository())
	ctx := context.Background()
	now := time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC)
	setup := []Signal{
		{ID: "open-1", Kind: SignalSessionOpened, UserID: "user-1", MatchID: "match-1", OccurredAt: now},
		{ID: "preference", Kind: SignalUserTurn, UserID: "user-1", MatchID: "match-1", OccurredAt: now, User: &UserSignal{Text: "我喜欢快攻", Cues: []UserCue{{Kind: CueStablePreference}}}},
		{ID: "open-2", Kind: SignalSessionOpened, UserID: "user-1", MatchID: "match-2", OccurredAt: now},
		{ID: "thread", Kind: SignalUserTurn, UserID: "user-1", MatchID: "match-2", OccurredAt: now, User: &UserSignal{Text: "接着上次说", Cues: []UserCue{{Kind: CueContinuedThread}}}},
	}
	for _, signal := range setup {
		if _, err := director.Apply(ctx, signal); err != nil {
			t.Fatalf("setup %s: %v", signal.ID, err)
		}
	}

	invited, err := director.Apply(ctx, Signal{
		ID:         "banter-invite",
		Kind:       SignalUserTurn,
		UserID:     "user-1",
		MatchID:    "match-2",
		OccurredAt: now.Add(time.Minute),
		User: &UserSignal{
			Text: "我赛前说他必进，是不是又毒奶了？",
			Cues: []UserCue{{Kind: CueBanterAllowed, Scope: "prediction"}},
		},
	})
	if err != nil {
		t.Fatalf("invite Apply: %v", err)
	}
	if len(invited.Actions) != 1 || invited.Actions[0] != ActTease {
		t.Fatalf("invite actions = %v, want [%s]", invited.Actions, ActTease)
	}
	if invited.Relationship.AllowedBanterScopes != 1 {
		t.Fatalf("allowed scopes = %d, want 1", invited.Relationship.AllowedBanterScopes)
	}

	revoked, err := director.Apply(ctx, Signal{
		ID:         "banter-revoke",
		Kind:       SignalUserTurn,
		UserID:     "user-1",
		MatchID:    "match-2",
		OccurredAt: now.Add(2 * time.Minute),
		User: &UserSignal{
			Text: "别拿这个开我玩笑了，烦",
			Cues: []UserCue{{Kind: CueBanterDenied, Scope: "prediction"}},
		},
	})
	if err != nil {
		t.Fatalf("revoke Apply: %v", err)
	}
	if len(revoked.Actions) != 1 || revoked.Actions[0] != ActRepair {
		t.Fatalf("revoke actions = %v, want [%s]", revoked.Actions, ActRepair)
	}
	if revoked.Relationship.AllowedBanterScopes != 0 {
		t.Fatalf("allowed scopes = %d, want 0", revoked.Relationship.AllowedBanterScopes)
	}
	if !revoked.Relationship.RepairActive {
		t.Fatal("rejected banter must start repair")
	}
}

func TestPlayfulWrongFactUsesTeaseAndDisagree(t *testing.T) {
	director := NewDirector(NewMemoryRepository())
	ctx := context.Background()
	now := time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC)
	setup := []Signal{
		{ID: "open-1", Kind: SignalSessionOpened, UserID: "user-1", MatchID: "match-1", OccurredAt: now},
		{ID: "preference", Kind: SignalUserTurn, UserID: "user-1", MatchID: "match-1", OccurredAt: now, User: &UserSignal{Text: "我喜欢快攻", Cues: []UserCue{{Kind: CueStablePreference}}}},
		{ID: "open-2", Kind: SignalSessionOpened, UserID: "user-1", MatchID: "match-2", OccurredAt: now},
		{ID: "thread", Kind: SignalUserTurn, UserID: "user-1", MatchID: "match-2", OccurredAt: now, User: &UserSignal{Text: "接着上次说", Cues: []UserCue{{Kind: CueContinuedThread}}}},
	}
	for _, signal := range setup {
		if _, err := director.Apply(ctx, signal); err != nil {
			t.Fatalf("setup %s: %v", signal.ID, err)
		}
	}

	decision, err := director.Apply(ctx, Signal{
		ID:         "wrong-player-joke",
		Kind:       SignalUserTurn,
		UserID:     "user-1",
		MatchID:    "match-2",
		OccurredAt: now.Add(time.Minute),
		User: &UserSignal{
			Text: "哈兰德这球真漂亮",
			Cues: []UserCue{{Kind: CueBanterAllowed, Scope: "match_judgment"}},
		},
		Grounding: GroundedContent{Intent: "match_fact_claim", FactMode: FactModeDeterministic},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	want := []CommunicationAct{ActTease, ActDisagree}
	if len(decision.Actions) != len(want) || decision.Actions[0] != want[0] || decision.Actions[1] != want[1] {
		t.Fatalf("actions = %v, want %v", decision.Actions, want)
	}
}

func TestExplicitTacticalQuestionSelectsAnalyze(t *testing.T) {
	decision, err := NewDirector(NewMemoryRepository()).Apply(context.Background(), Signal{
		ID:         "analysis-1",
		Kind:       SignalUserTurn,
		UserID:     "user-1",
		MatchID:    "match-1",
		OccurredAt: time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC),
		User:       &UserSignal{Text: "他们为什么右路一直被打穿？"},
		Grounding:  GroundedContent{Intent: "smalltalk", FactMode: FactModeNone},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(decision.Actions) != 1 || decision.Actions[0] != ActAnalyze {
		t.Fatalf("actions = %v, want [%s]", decision.Actions, ActAnalyze)
	}
}

func TestPersonalInsultSelectsDisagreeAndOpinion(t *testing.T) {
	decision, err := NewDirector(NewMemoryRepository()).Apply(context.Background(), Signal{
		ID:         "insult-1",
		Kind:       SignalUserTurn,
		UserID:     "user-1",
		MatchID:    "match-1",
		OccurredAt: time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC),
		User:       &UserSignal{Text: "这人就是个废物"},
		Grounding:  GroundedContent{Intent: "emotion_reaction", FactMode: FactModeNone},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	want := []CommunicationAct{ActDisagree, ActOpinion}
	if len(decision.Actions) != len(want) || decision.Actions[0] != want[0] || decision.Actions[1] != want[1] {
		t.Fatalf("actions = %v, want %v", decision.Actions, want)
	}
}

func TestDirectorPlansPresentationForGreetingAndUserReply(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC)
	director := NewDirector(NewMemoryRepository())

	greeting, err := director.Apply(ctx, Signal{
		ID: "presentation-greeting", Kind: SignalSessionOpened, UserID: "user-1", MatchID: "match-1", OccurredAt: now,
	})
	if err != nil {
		t.Fatalf("greeting Apply: %v", err)
	}
	if greeting.Presentation.Expression != "happy" || greeting.Presentation.Motion != "hello" {
		t.Fatalf("greeting presentation = %+v", greeting.Presentation)
	}

	reply, err := director.Apply(ctx, Signal{
		ID: "presentation-reply", Kind: SignalUserTurn, UserID: "user-1", MatchID: "match-1", OccurredAt: now.Add(time.Minute),
		User: &UserSignal{Text: "在吗"}, Grounding: GroundedContent{Intent: "smalltalk"},
	})
	if err != nil {
		t.Fatalf("reply Apply: %v", err)
	}
	if reply.Presentation.Expression != "chat" || reply.Presentation.Motion != "speak" {
		t.Fatalf("reply presentation = %+v", reply.Presentation)
	}
}

func TestReadyOpenThreadSelectsRecallAndOpinion(t *testing.T) {
	decision, err := NewDirector(NewMemoryRepository()).Apply(context.Background(), Signal{
		ID:         "thread-ready",
		Kind:       SignalUserTurn,
		UserID:     "user-1",
		MatchID:    "match-1",
		OccurredAt: time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC),
		User: &UserSignal{
			Text: "刚才说的中场问题果然来了",
			Cues: []UserCue{{Kind: CueOpenThreadReady}},
		},
		Grounding: GroundedContent{Intent: "smalltalk", FactMode: FactModeNone},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	want := []CommunicationAct{ActRecall, ActOpinion}
	if len(decision.Actions) != len(want) || decision.Actions[0] != want[0] || decision.Actions[1] != want[1] {
		t.Fatalf("actions = %v, want %v", decision.Actions, want)
	}
}

func TestUserRequestForQuietSelectsSilence(t *testing.T) {
	decision, err := NewDirector(NewMemoryRepository()).Apply(context.Background(), Signal{
		ID:         "quiet-1",
		Kind:       SignalUserTurn,
		UserID:     "user-1",
		MatchID:    "match-1",
		OccurredAt: time.Date(2026, 7, 15, 22, 0, 0, 0, time.UTC),
		User: &UserSignal{
			Text: "不想分析，陪我缓会儿",
			Cues: []UserCue{{Kind: CueNeedsSilence}},
		},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(decision.Actions) != 1 || decision.Actions[0] != ActSilence {
		t.Fatalf("actions = %v, want [%s]", decision.Actions, ActSilence)
	}
	if decision.Speech != nil {
		t.Fatalf("speech = %+v, want nil", decision.Speech)
	}
}

func TestNaturalInitiativeBudgetThrottlesNormalButKeepsCritical(t *testing.T) {
	director := NewDirector(NewMemoryRepository())
	ctx := context.Background()
	now := time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC)
	first, err := director.Apply(ctx, Signal{
		ID:         "normal-1",
		Kind:       SignalMatchEvent,
		UserID:     "user-1",
		MatchID:    "match-1",
		OccurredAt: now,
		Match:      &MatchSignal{EventID: "shot-1", EventType: "shot_missed", Intensity: 3, OutputAllowed: true},
	})
	if err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	if first.Speech == nil {
		t.Fatal("first normal event should be allowed")
	}

	second, err := director.Apply(ctx, Signal{
		ID:         "normal-2",
		Kind:       SignalMatchEvent,
		UserID:     "user-1",
		MatchID:    "match-1",
		OccurredAt: now.Add(30 * time.Second),
		Match:      &MatchSignal{EventID: "shot-2", EventType: "shot_missed", Intensity: 3, OutputAllowed: true},
	})
	if err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if second.Speech != nil || len(second.Actions) != 1 || second.Actions[0] != ActSilence {
		t.Fatalf("second decision = %+v, want throttled silence", second)
	}

	critical, err := director.Apply(ctx, Signal{
		ID:         "critical-1",
		Kind:       SignalMatchEvent,
		UserID:     "user-1",
		MatchID:    "match-1",
		OccurredAt: now.Add(31 * time.Second),
		Match: &MatchSignal{
			EventID:       "goal-1",
			EventType:     "goal",
			Intensity:     5,
			OutputAllowed: true,
			Critical:      true,
			UserSpeaking:  true,
		},
	})
	if err != nil {
		t.Fatalf("critical Apply: %v", err)
	}
	if critical.Speech == nil {
		t.Fatal("critical event must bypass normal cooldown")
	}
	if critical.Speech.Delivery.InterruptMode != "after_user" {
		t.Fatalf("interrupt mode = %q, want after_user", critical.Speech.Delivery.InterruptMode)
	}
}

func TestDirectorOwnsConfiguredNormalInitiativeCooldown(t *testing.T) {
	director := NewDirector(NewMemoryRepository())
	ctx := context.Background()
	now := time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC)
	apply := func(id string, offset time.Duration) Decision {
		t.Helper()
		decision, err := director.Apply(ctx, Signal{
			ID: id, Kind: SignalMatchEvent, UserID: "user-1", MatchID: "match-1", OccurredAt: now.Add(offset),
			Match: &MatchSignal{EventID: id, EventType: "shot", Intensity: 3, OutputAllowed: true, NormalCooldownSeconds: 10},
		})
		if err != nil {
			t.Fatalf("Apply %s: %v", id, err)
		}
		return decision
	}
	if first := apply("shot-1", 0); first.Speech == nil {
		t.Fatal("first event should speak")
	}
	if cooling := apply("shot-2", 9*time.Second); cooling.Speech != nil {
		t.Fatalf("cooling decision = %+v", cooling)
	}
	if ready := apply("shot-3", 11*time.Second); ready.Speech == nil {
		t.Fatalf("configured cooldown was not honored: %+v", ready)
	}
}

func TestUnverifiedDecisionUsesReactAndDisagree(t *testing.T) {
	decision, err := NewDirector(NewMemoryRepository()).Apply(context.Background(), Signal{
		ID:         "unverified-penalty",
		Kind:       SignalUserTurn,
		UserID:     "user-1",
		MatchID:    "match-1",
		OccurredAt: time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC),
		User:       &UserSignal{Text: "百分百点球，裁判瞎了"},
		Grounding:  GroundedContent{Intent: "match_fact_claim", FactMode: FactModeUnverified},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	want := []CommunicationAct{ActReact, ActDisagree}
	if len(decision.Actions) != len(want) || decision.Actions[0] != want[0] || decision.Actions[1] != want[1] {
		t.Fatalf("actions = %v, want %v", decision.Actions, want)
	}
}

func TestOpinionConflictUsesDisagreeAndOpinion(t *testing.T) {
	decision, err := NewDirector(NewMemoryRepository()).Apply(context.Background(), Signal{
		ID:         "opinion-conflict",
		Kind:       SignalUserTurn,
		UserID:     "user-1",
		MatchID:    "match-1",
		OccurredAt: time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC),
		User: &UserSignal{
			Text: "他今天踢得明明挺好",
			Cues: []UserCue{{Kind: CueOpinionConflict}},
		},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	want := []CommunicationAct{ActDisagree, ActOpinion}
	if len(decision.Actions) != len(want) || decision.Actions[0] != want[0] || decision.Actions[1] != want[1] {
		t.Fatalf("actions = %v, want %v", decision.Actions, want)
	}
}

func TestSharedMomentAtBallmateStageUsesRecallAndTease(t *testing.T) {
	director := NewDirector(NewMemoryRepository())
	ctx := context.Background()
	now := time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC)
	signals := []Signal{
		{ID: "open-1", Kind: SignalSessionOpened, UserID: "user-1", MatchID: "match-1", OccurredAt: now},
		{ID: "preference", Kind: SignalUserTurn, UserID: "user-1", MatchID: "match-1", OccurredAt: now, User: &UserSignal{Text: "我喜欢快攻", Cues: []UserCue{{Kind: CueStablePreference}}}},
		{ID: "open-2", Kind: SignalSessionOpened, UserID: "user-1", MatchID: "match-2", OccurredAt: now},
		{ID: "thread", Kind: SignalUserTurn, UserID: "user-1", MatchID: "match-2", OccurredAt: now, User: &UserSignal{Text: "接着", Cues: []UserCue{{Kind: CueContinuedThread}}}},
		{ID: "open-3", Kind: SignalSessionOpened, UserID: "user-1", MatchID: "match-3", OccurredAt: now},
		{ID: "trust", Kind: SignalUserTurn, UserID: "user-1", MatchID: "match-3", OccurredAt: now, User: &UserSignal{Text: "继续", Cues: []UserCue{{Kind: CueAcceptedJudgment}, {Kind: CueAcceptedInitiative}, {Kind: CueBanterAllowed, Scope: "match_judgment"}}}},
	}
	for _, signal := range signals {
		if _, err := director.Apply(ctx, signal); err != nil {
			t.Fatalf("setup %s: %v", signal.ID, err)
		}
	}

	decision, err := director.Apply(ctx, Signal{
		ID:         "shared-moment",
		Kind:       SignalUserTurn,
		UserID:     "user-1",
		MatchID:    "match-3",
		OccurredAt: now.Add(time.Minute),
		User:       &UserSignal{Text: "这个时间这个比分，又想起那场了", Cues: []UserCue{{Kind: CueSharedMomentRecalled}}},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	want := []CommunicationAct{ActRecall, ActTease}
	if len(decision.Actions) != len(want) || decision.Actions[0] != want[0] || decision.Actions[1] != want[1] {
		t.Fatalf("actions = %v, want %v", decision.Actions, want)
	}
}

func TestDeliverySignalRecordsFirstGreetingAndPlaybackState(t *testing.T) {
	director := NewDirector(NewMemoryRepository())
	ctx := context.Background()
	now := time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC)
	applyScenario(t, director, Signal{ID: "session-1", Kind: SignalSessionOpened, UserID: "user-1", MatchID: "match-1", OccurredAt: now})
	decision, err := director.Apply(ctx, Signal{
		ID:         "delivery-1",
		Kind:       SignalDeliveryResult,
		UserID:     "user-1",
		MatchID:    "match-1",
		OccurredAt: now.Add(time.Second),
		Delivery: &DeliverySignal{
			DecisionID: "decision:session-1",
			State:      "text_delivered",
			Purpose:    "first_meeting",
		},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !decision.Relationship.GreetingDelivered {
		t.Fatal("first greeting delivery was not recorded")
	}
	if decision.PlaybackState != "text_delivered" {
		t.Fatalf("playback state = %q", decision.PlaybackState)
	}
	if decision.Speech != nil {
		t.Fatalf("delivery result produced speech: %+v", decision.Speech)
	}
}

func TestNewRelationshipUsesConfirmedProductDefaults(t *testing.T) {
	decision, err := NewDirector(NewMemoryRepository()).Apply(context.Background(), Signal{
		ID:         "defaults-1",
		Kind:       SignalSessionOpened,
		UserID:     "user-1",
		MatchID:    "match-1",
		OccurredAt: time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if decision.Relationship.InitiativeMode != "natural" {
		t.Fatalf("initiative mode = %q, want natural", decision.Relationship.InitiativeMode)
	}
	if decision.Relationship.AnalysisAppetite != "brief" {
		t.Fatalf("analysis appetite = %q, want brief", decision.Relationship.AnalysisAppetite)
	}
}

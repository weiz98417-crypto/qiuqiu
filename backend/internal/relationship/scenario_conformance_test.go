package relationship

import (
	"context"
	"testing"
	"time"
)

func TestHumanityScenarioConformance(t *testing.T) {
	now := time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC)
	tests := []struct {
		name           string
		run            func(*testing.T, *Director) Decision
		wantActions    []CommunicationAct
		wantStage      RelationshipStage
		wantExpression string
		wantRepair     bool
		wantBoundaries int
	}{
		{
			name: "01_first_meeting",
			run: func(t *testing.T, director *Director) Decision {
				return applyScenario(t, director, Signal{ID: "s01", Kind: SignalSessionOpened, UserID: "user", MatchID: "match-1", OccurredAt: now})
			},
			wantActions: []CommunicationAct{ActAcknowledge},
			wantStage:   StageFirstMeeting,
		},
		{
			name: "02_familiar_user_returns",
			run: func(t *testing.T, director *Director) Decision {
				prepareStage(t, director, StageFamiliar, now)
				return applyScenario(t, director, Signal{ID: "s02", Kind: SignalSessionOpened, UserID: "user", MatchID: "match-return", OccurredAt: now.Add(21 * 24 * time.Hour)})
			},
			wantStage: StageFamiliar,
		},
		{
			name: "03_playful_wrong_scorer",
			run: func(t *testing.T, director *Director) Decision {
				prepareStage(t, director, StageFamiliar, now)
				return applyScenario(t, director, Signal{ID: "s03", Kind: SignalUserTurn, UserID: "user", MatchID: "match-2", OccurredAt: now, User: &UserSignal{Text: "哈兰德这球真漂亮", Cues: []UserCue{{Kind: CueBanterAllowed, Scope: "match_judgment"}}}, Grounding: GroundedContent{Intent: "match_fact_claim", FactMode: FactModeDeterministic}})
			},
			wantActions: []CommunicationAct{ActTease, ActDisagree},
		},
		{
			name: "04_opinion_conflict",
			run: func(t *testing.T, director *Director) Decision {
				prepareStage(t, director, StageWatchBuddy, now)
				return applyScenario(t, director, Signal{ID: "s04", Kind: SignalUserTurn, UserID: "user", MatchID: "match-3", OccurredAt: now, User: &UserSignal{Text: "他今天踢得明明挺好", Cues: []UserCue{{Kind: CueOpinionConflict}}}})
			},
			wantActions: []CommunicationAct{ActDisagree, ActOpinion},
		},
		{
			name: "05_unverified_penalty",
			run: func(t *testing.T, director *Director) Decision {
				return applyScenario(t, director, Signal{ID: "s05", Kind: SignalUserTurn, UserID: "user", MatchID: "match-1", OccurredAt: now, User: &UserSignal{Text: "百分百点球，裁判瞎了"}, Grounding: GroundedContent{Intent: "match_fact_claim", FactMode: FactModeUnverified}})
			},
			wantActions: []CommunicationAct{ActReact, ActDisagree},
		},
		{
			name: "06_goal_cancelled_by_var",
			run: func(t *testing.T, director *Director) Decision {
				applyScenario(t, director, matchScenarioSignal("s06-goal", "goal", now, true, true))
				applyScenario(t, director, matchScenarioSignal("s06-var", "var_check", now.Add(time.Second), true, true))
				return applyScenario(t, director, matchScenarioSignal("s06-cancel", "goal_cancelled", now.Add(3*time.Second), true, true))
			},
			wantActions:    []CommunicationAct{ActReact},
			wantExpression: "deflated",
		},
		{
			name: "07_repeated_missed_chances",
			run: func(t *testing.T, director *Director) Decision {
				applyScenario(t, director, matchScenarioSignal("s07-1", "shot_missed", now, true, false))
				applyScenario(t, director, matchScenarioSignal("s07-2", "shot_missed", now.Add(91*time.Second), true, false))
				return applyScenario(t, director, matchScenarioSignal("s07-3", "shot_missed", now.Add(182*time.Second), true, false))
			},
			wantActions: []CommunicationAct{ActReact},
		},
		{
			name: "08_explicit_analysis_request",
			run: func(t *testing.T, director *Director) Decision {
				prepareStage(t, director, StageFamiliar, now)
				return applyScenario(t, director, Signal{ID: "s08", Kind: SignalUserTurn, UserID: "user", MatchID: "match-2", OccurredAt: now, User: &UserSignal{Text: "他们为什么右路一直被打穿？"}})
			},
			wantActions: []CommunicationAct{ActAnalyze},
		},
		{
			name: "09_over_analysis_feedback",
			run: func(t *testing.T, director *Director) Decision {
				prepareStage(t, director, StageWatchBuddy, now)
				return applyScenario(t, director, Signal{ID: "s09", Kind: SignalUserTurn, UserID: "user", MatchID: "match-3", OccurredAt: now, User: &UserSignal{Text: "你又开始讲大道理了"}})
			},
			wantActions: []CommunicationAct{ActRepair},
			wantRepair:  true,
		},
		{
			name: "10_repetitive_reply_feedback",
			run: func(t *testing.T, director *Director) Decision {
				return applyScenario(t, director, Signal{ID: "s10", Kind: SignalUserTurn, UserID: "user", MatchID: "match-1", OccurredAt: now, User: &UserSignal{Text: "你怎么老说我在陪你看？"}})
			},
			wantActions: []CommunicationAct{ActRepair},
			wantRepair:  true,
		},
		{
			name: "11_user_invites_banter",
			run: func(t *testing.T, director *Director) Decision {
				prepareStage(t, director, StageFamiliar, now)
				return applyScenario(t, director, Signal{ID: "s11", Kind: SignalUserTurn, UserID: "user", MatchID: "match-2", OccurredAt: now, User: &UserSignal{Text: "是不是又毒奶了？"}})
			},
			wantActions: []CommunicationAct{ActTease},
		},
		{
			name: "12_user_rejects_banter",
			run: func(t *testing.T, director *Director) Decision {
				prepareStage(t, director, StageWatchBuddy, now)
				return applyScenario(t, director, Signal{ID: "s12", Kind: SignalUserTurn, UserID: "user", MatchID: "match-3", OccurredAt: now, User: &UserSignal{Text: "别拿这个开我玩笑了，烦"}})
			},
			wantActions: []CommunicationAct{ActRepair},
			wantRepair:  true,
		},
		{
			name: "13_critical_stage_silence",
			run: func(t *testing.T, director *Director) Decision {
				prepareStage(t, director, StageOldBallmate, now)
				return applyScenario(t, director, matchScenarioSignal("s13", "goal", now, false, true))
			},
			wantActions:    []CommunicationAct{ActSilence},
			wantExpression: "excited",
		},
		{
			name: "14_natural_open_thread_recall",
			run: func(t *testing.T, director *Director) Decision {
				prepareStage(t, director, StageWatchBuddy, now)
				return applyScenario(t, director, Signal{ID: "s14", Kind: SignalUserTurn, UserID: "user", MatchID: "match-3", OccurredAt: now, User: &UserSignal{Text: "刚才说的中场问题果然来了", Cues: []UserCue{{Kind: CueOpenThreadReady}}}})
			},
			wantActions: []CommunicationAct{ActRecall, ActOpinion},
		},
		{
			name: "15_return_after_long_absence",
			run: func(t *testing.T, director *Director) Decision {
				prepareStage(t, director, StageWatchBuddy, now)
				return applyScenario(t, director, Signal{ID: "s15", Kind: SignalSessionOpened, UserID: "user", MatchID: "match-return", OccurredAt: now.Add(60 * 24 * time.Hour)})
			},
			wantStage: StageWatchBuddy,
		},
		{
			name: "16_user_needs_quiet_after_loss",
			run: func(t *testing.T, director *Director) Decision {
				prepareStage(t, director, StageOldBallmate, now)
				return applyScenario(t, director, Signal{ID: "s16", Kind: SignalUserTurn, UserID: "user", MatchID: "match-6", OccurredAt: now, User: &UserSignal{Text: "不想分析，陪我缓会儿"}})
			},
			wantActions:    []CommunicationAct{ActSilence},
			wantExpression: "low",
		},
		{
			name: "17_user_insults_player",
			run: func(t *testing.T, director *Director) Decision {
				return applyScenario(t, director, Signal{ID: "s17", Kind: SignalUserTurn, UserID: "user", MatchID: "match-1", OccurredAt: now, User: &UserSignal{Text: "这人就是个废物"}})
			},
			wantActions: []CommunicationAct{ActDisagree, ActOpinion},
		},
		{
			name: "18_work_boundary",
			run: func(t *testing.T, director *Director) Decision {
				prepareStage(t, director, StageWatchBuddy, now)
				return applyScenario(t, director, Signal{ID: "s18", Kind: SignalUserTurn, UserID: "user", MatchID: "match-3", OccurredAt: now, User: &UserSignal{Text: "你别问细节，我不想说工作"}})
			},
			wantActions:    []CommunicationAct{ActAcknowledge},
			wantBoundaries: 1,
		},
		{
			name: "19_analysis_preference_changes",
			run: func(t *testing.T, director *Director) Decision {
				prepareStage(t, director, StageFamiliar, now)
				return applyScenario(t, director, Signal{ID: "s19", Kind: SignalUserTurn, UserID: "user", MatchID: "match-2", OccurredAt: now, User: &UserSignal{Text: "最近可以多说点他们怎么站位"}})
			},
			wantActions: []CommunicationAct{ActAnalyze},
		},
		{
			name: "20_old_ballmate_shared_moment",
			run: func(t *testing.T, director *Director) Decision {
				prepareStage(t, director, StageOldBallmate, now)
				return applyScenario(t, director, Signal{ID: "s20", Kind: SignalUserTurn, UserID: "user", MatchID: "match-6", OccurredAt: now, User: &UserSignal{Text: "这个时间这个比分，又想起那场了", Cues: []UserCue{{Kind: CueSharedMomentRecalled}}}})
			},
			wantActions: []CommunicationAct{ActRecall, ActTease},
			wantStage:   StageOldBallmate,
		},
	}

	if len(tests) != 20 {
		t.Fatalf("scenario count = %d, want 20", len(tests))
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := test.run(t, NewDirector(NewMemoryRepository()))
			if !sameActions(decision.Actions, test.wantActions) {
				t.Fatalf("actions = %v, want %v", decision.Actions, test.wantActions)
			}
			if test.wantStage != "" && decision.Relationship.Stage != test.wantStage {
				t.Fatalf("stage = %q, want %q", decision.Relationship.Stage, test.wantStage)
			}
			if test.wantExpression != "" && decision.Presentation.Expression != test.wantExpression {
				t.Fatalf("expression = %q, want %q", decision.Presentation.Expression, test.wantExpression)
			}
			if decision.Relationship.RepairActive != test.wantRepair {
				t.Fatalf("repair active = %v, want %v", decision.Relationship.RepairActive, test.wantRepair)
			}
			if test.wantBoundaries > 0 && decision.Relationship.BoundaryCount != test.wantBoundaries {
				t.Fatalf("boundary count = %d, want %d", decision.Relationship.BoundaryCount, test.wantBoundaries)
			}
		})
	}
}

func applyScenario(t *testing.T, director *Director, signal Signal) Decision {
	t.Helper()
	decision, err := director.Apply(context.Background(), signal)
	if err != nil {
		t.Fatalf("Apply %s: %v", signal.ID, err)
	}
	return decision
}

func matchScenarioSignal(id, eventType string, now time.Time, outputAllowed, critical bool) Signal {
	return Signal{
		ID:         id,
		Kind:       SignalMatchEvent,
		UserID:     "user",
		MatchID:    "match-1",
		OccurredAt: now,
		Match: &MatchSignal{
			EventID:       id + "-event",
			EventType:     eventType,
			Intensity:     5,
			OutputAllowed: outputAllowed,
			Critical:      critical,
		},
	}
}

func prepareStage(t *testing.T, director *Director, wanted RelationshipStage, now time.Time) {
	t.Helper()
	signals := []Signal{
		{ID: "prepare-open-1", Kind: SignalSessionOpened, UserID: "user", MatchID: "match-1", OccurredAt: now},
		{ID: "prepare-preference", Kind: SignalUserTurn, UserID: "user", MatchID: "match-1", OccurredAt: now, User: &UserSignal{Text: "我喜欢快攻", Cues: []UserCue{{Kind: CueStablePreference}}}},
		{ID: "prepare-open-2", Kind: SignalSessionOpened, UserID: "user", MatchID: "match-2", OccurredAt: now},
		{ID: "prepare-thread", Kind: SignalUserTurn, UserID: "user", MatchID: "match-2", OccurredAt: now, User: &UserSignal{Text: "接着上次", Cues: []UserCue{{Kind: CueContinuedThread}}}},
	}
	if wanted == StageWatchBuddy || wanted == StageOldBallmate {
		signals = append(signals,
			Signal{ID: "prepare-open-3", Kind: SignalSessionOpened, UserID: "user", MatchID: "match-3", OccurredAt: now},
			Signal{ID: "prepare-trust", Kind: SignalUserTurn, UserID: "user", MatchID: "match-3", OccurredAt: now, User: &UserSignal{Text: "继续", Cues: []UserCue{{Kind: CueAcceptedJudgment}, {Kind: CueAcceptedInitiative}, {Kind: CueBanterAllowed, Scope: "match_judgment"}}}},
		)
	}
	if wanted == StageOldBallmate {
		signals = append(signals,
			Signal{ID: "prepare-open-4", Kind: SignalSessionOpened, UserID: "user", MatchID: "match-4", OccurredAt: now},
			Signal{ID: "prepare-open-5", Kind: SignalSessionOpened, UserID: "user", MatchID: "match-5", OccurredAt: now},
			Signal{ID: "prepare-open-6", Kind: SignalSessionOpened, UserID: "user", MatchID: "match-6", OccurredAt: now},
			Signal{ID: "prepare-old", Kind: SignalUserTurn, UserID: "user", MatchID: "match-6", OccurredAt: now, User: &UserSignal{Text: "那场还记得", Cues: []UserCue{{Kind: CueSharedMomentRecalled}, {Kind: CueContinuedDisagreement}}}},
		)
	}
	var decision Decision
	for _, signal := range signals {
		decision = applyScenario(t, director, signal)
	}
	if decision.Relationship.Stage != wanted {
		t.Fatalf("prepared stage = %q, want %q", decision.Relationship.Stage, wanted)
	}
}

func sameActions(actual, expected []CommunicationAct) bool {
	if len(actual) != len(expected) {
		return false
	}
	for index := range actual {
		if actual[index] != expected[index] {
			return false
		}
	}
	return true
}

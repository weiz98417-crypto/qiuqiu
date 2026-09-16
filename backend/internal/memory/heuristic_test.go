package memory

import (
	"math"
	"testing"
)

func TestScoreImportanceHeuristicTable(t *testing.T) {
	cases := []struct {
		name    string
		kind    MomentKind
		content string
		stage   Stage
		want    float64
	}{
		{name: "goal match event", kind: MomentMatchEvent, content: "比赛事件[进球]：主队破门", stage: StageNone, want: 0.90},
		{name: "red card match event", kind: MomentMatchEvent, content: "比赛事件[红牌]：后卫两黄变一红", stage: StageNone, want: 0.80},
		{name: "var match event", kind: MomentMatchEvent, content: "比赛事件[VAR]：裁判正在回看", stage: StageNone, want: 0.80},
		{name: "yellow card match event", kind: MomentMatchEvent, content: "比赛事件[黄牌]：中场战术犯规", stage: StageNone, want: 0.55},
		{name: "explicit user preference", kind: MomentUserFact, content: "我喜欢皇马", stage: StageNone, want: 0.80},
		{name: "banter boundary counts as user fact", kind: MomentUserFact, content: "别拿毒奶开我玩笑", stage: StageNone, want: 0.80},
		{name: "plain user fact", kind: MomentUserFact, content: "随便聊聊吧", stage: StageNone, want: 0.70},
		{name: "promise wording", kind: MomentPromise, content: "待会儿告诉你谁助攻", stage: StageNone, want: 0.90},
		{name: "emotional exchange", kind: MomentEmotionalExchange, content: "这球绝了", stage: StageNone, want: 0.65},
		{name: "routine content", kind: MomentKind("other"), content: "嗯嗯", stage: StageNone, want: 0.30},
		{name: "old ballmate stage weights up", kind: MomentUserFact, content: "随便聊聊吧", stage: StageOldBallmate, want: 0.77},
		{name: "first meeting stage weights down", kind: MomentUserFact, content: "我喜欢皇马", stage: StageFirstMeeting, want: 0.72},
		{name: "watch buddy stage", kind: MomentUserFact, content: "随便聊聊吧", stage: StageWatchBuddy, want: 0.735},
		{name: "stacked markers clamp to one", kind: MomentUserFact, content: "我喜欢皇马，待会儿告诉你，这球绝了", stage: StageOldBallmate, want: 1.0},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := ScoreImportance(testCase.kind, testCase.content, testCase.stage); !almostEqual(got, testCase.want) {
				t.Fatalf("ScoreImportance(%s, %q, %s) = %v, want %v", testCase.kind, testCase.content, testCase.stage, got, testCase.want)
			}
		})
	}
}

func TestRoutineMomentsFallBelowExtractionFloor(t *testing.T) {
	if got := ScoreImportance(MomentKind("other"), "在吗", StageNone); got >= MinExtractionImportance {
		t.Fatalf("routine moment importance %v should stay below the extraction floor %v", got, MinExtractionImportance)
	}
	if ScoreImportance(MomentMatchEvent, "比赛事件[进球]", StageNone) < MinExtractionImportance {
		t.Fatal("goal moments must always clear the extraction floor")
	}
}

func TestScoreImportanceStaysInUnitRange(t *testing.T) {
	for _, stage := range []Stage{StageNone, StageFirstMeeting, StageFamiliar, StageWatchBuddy, StageOldBallmate} {
		for _, kind := range []MomentKind{MomentUserFact, MomentEmotionalExchange, MomentPromise, MomentMatchEvent, MomentKind("other")} {
			if got := ScoreImportance(kind, "我喜欢皇马，待会儿告诉你，这球绝了，VAR 红牌 进球", stage); got < 0 || got > 1 {
				t.Fatalf("importance %v out of range for kind %s stage %s", got, kind, stage)
			}
		}
	}
}

func almostEqual(left, right float64) bool {
	return math.Abs(left-right) < 1e-9
}

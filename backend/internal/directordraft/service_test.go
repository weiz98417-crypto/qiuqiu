package directordraft

import (
	"context"
	"errors"
	"testing"

	"qiuqiu/internal/matchstate"
)

func intPointer(value int) *int       { return &value }
func int64Pointer(value int64) *int64 { return &value }

type stubExtractor struct {
	extraction Extraction
}

func TestVoiceDraftPreservesRecordingStartClock(t *testing.T) {
	service := NewService(nil, stubExtractor{extraction: Extraction{
		Team: "西班牙", EventType: "shot", Description: "佩德里完成射门。",
		Participants: []ExtractedParticipant{{Role: "shooter", Name: "佩德里"}},
	}})
	result, err := service.Build(context.Background(), Request{
		Text:                 "佩德里射门",
		OccurredPeriod:       "first_half",
		OccurredSeconds:      intPointer(23*60 + 41),
		CapturedClockVersion: int64Pointer(8),
	}, MatchContext{
		MatchID: "match-clock-capture",
		Config: matchstate.MatchConfig{
			HomeTeam: "西班牙", AwayTeam: "德国",
			HomePlayers: []matchstate.Player{{Name: "佩德里"}},
		},
		Clock: matchstate.MatchClock{MatchID: "match-clock-capture", Period: "first_half", ElapsedSeconds: 24 * 60, Version: 9},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if result.Draft.OccurredPeriod != "first_half" || result.Draft.OccurredSeconds != 23*60+41 || result.Draft.CapturedClockVersion != 8 {
		t.Fatalf("recording start clock = %+v", result.Draft)
	}
}

func (stub stubExtractor) Extract(context.Context, string, MatchContext) (Extraction, error) {
	return stub.extraction, nil
}

func TestTextInputProducesRosterCalibratedDraftWithoutPublishing(t *testing.T) {
	service := NewService(nil, stubExtractor{extraction: Extraction{
		Team:        "德国",
		EventType:   "substitution",
		Description: "德国换人，菲尔克鲁格换下哈弗茨。",
		Participants: []ExtractedParticipant{
			{Role: "sub_on", Name: "菲尔克鲁格"},
			{Role: "sub_off", Name: "哈弗茨"},
		},
	}})

	result, err := service.Build(context.Background(), Request{
		Text: "德国换人，菲尔克鲁格换下哈弗茨",
	}, MatchContext{
		MatchID: "match-1",
		Config: matchstate.MatchConfig{
			HomeTeam: "西班牙",
			AwayTeam: "德国",
			AwayPlayers: []matchstate.Player{
				{Name: "菲尔克鲁格", Number: "9"},
				{Name: "哈弗茨", Number: "7"},
			},
		},
		Clock: matchstate.MatchClock{MatchID: "match-1", Period: "second_half", ElapsedSeconds: 67 * 60, Version: 3},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if result.Transcript != "德国换人，菲尔克鲁格换下哈弗茨" {
		t.Fatalf("transcript = %q", result.Transcript)
	}
	if result.Draft.Source != "operator_voice" || result.Draft.TeamID != "away" || result.Draft.EventType != "substitution" {
		t.Fatalf("draft identity = %+v", result.Draft)
	}
	if result.Draft.OccurredPeriod != "second_half" || result.Draft.OccurredSeconds != 67*60 || result.Draft.CapturedClockVersion != 3 {
		t.Fatalf("draft occurrence time = %+v", result.Draft)
	}
	if len(result.Draft.Participants) != 2 || !result.Draft.Participants[0].Resolved || !result.Draft.Participants[1].Resolved {
		t.Fatalf("participants = %+v", result.Draft.Participants)
	}
	if result.Ready != true || len(result.Warnings) != 0 {
		t.Fatalf("result = %+v", result)
	}
}

func TestGoalVoiceDraftRecoversTeamAndRosterNamesFromNaturalChinese(t *testing.T) {
	service := NewService(nil, stubExtractor{extraction: Extraction{
		EventType:   "goal",
		Description: "法比安鲁伊斯接亚马尔传球射门得分，西班牙一比零领先。",
		Participants: []ExtractedParticipant{
			{Role: "scorer", Name: "法比安鲁伊斯"},
			{Role: "assist", Name: "亚马尔"},
		},
	}})
	result, err := service.Build(context.Background(), Request{
		Text: "法比安鲁伊斯拿到亚马尔的传球起脚射门球进了，西班牙队1-0领先",
	}, MatchContext{
		MatchID: "natural-goal-voice",
		Config: matchstate.MatchConfig{
			HomeTeam: "西班牙",
			AwayTeam: "德国",
			HomePlayers: []matchstate.Player{
				{Name: "法比安·鲁伊斯"},
				{Name: "拉明·亚马尔"},
			},
		},
		Clock: matchstate.MatchClock{MatchID: "natural-goal-voice", Period: "first_half", ElapsedSeconds: 18 * 60, Version: 2},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !result.Ready || result.Draft.EventType != "goal" || result.Draft.TeamID != "home" {
		t.Fatalf("voice goal draft = %+v", result)
	}
	if len(result.Draft.Participants) != 2 {
		t.Fatalf("participants = %+v", result.Draft.Participants)
	}
	if result.Draft.Participants[0].Name != "法比安·鲁伊斯" || result.Draft.Participants[1].Name != "拉明·亚马尔" {
		t.Fatalf("canonical participants = %+v", result.Draft.Participants)
	}
}

func TestGoalVoiceDraftFillsEmptyExtractorFieldsFromRosterMentions(t *testing.T) {
	service := NewService(nil, stubExtractor{})
	result, err := service.Build(context.Background(), Request{
		Text: "法比安鲁伊斯拿到亚马尔的传球起脚射门球进了，西班牙队1-0领先",
	}, MatchContext{
		MatchID: "fallback-goal-voice",
		Config: matchstate.MatchConfig{
			HomeTeam: "西班牙",
			AwayTeam: "德国",
			HomePlayers: []matchstate.Player{
				{Name: "法比安·鲁伊斯"},
				{Name: "拉明·亚马尔"},
			},
		},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !result.Ready || result.Draft.EventType != "goal" || result.Draft.TeamID != "home" || len(result.Draft.Participants) != 2 {
		t.Fatalf("fallback voice goal draft = %+v", result)
	}
}

func TestGoalVoiceDraftNormalizesChineseEventTypeBeforeInferringParticipants(t *testing.T) {
	service := NewService(nil, stubExtractor{extraction: Extraction{
		Team:        "西班牙队",
		EventType:   "进球",
		Description: "法比安鲁伊斯拿到亚马尔的传球起脚射门球进了，西班牙队1-0领先。",
	}})
	result, err := service.Build(context.Background(), Request{
		Text: "法比安鲁伊斯拿到亚马尔的传球起脚射门球进了，西班牙队1-0领先",
	}, MatchContext{
		MatchID: "chinese-event-type-goal-voice",
		Config: matchstate.MatchConfig{
			HomeTeam: "西班牙",
			AwayTeam: "德国",
			HomePlayers: []matchstate.Player{
				{Name: "法比安·鲁伊斯"},
				{Name: "拉明·亚马尔"},
			},
		},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !result.Ready || result.Draft.EventType != "goal" || result.Draft.TeamID != "home" || len(result.Draft.Participants) != 2 {
		t.Fatalf("normalized Chinese event draft = %+v", result)
	}
}

func TestGoalVoiceDraftAllowsMultipleAssistsAndOppositionDefenders(t *testing.T) {
	service := NewService(nil, stubExtractor{extraction: Extraction{
		Team:        "西班牙",
		EventType:   "goal",
		Description: "西班牙进球。",
		Participants: []ExtractedParticipant{
			{Role: "scorer", Name: "莫拉塔"},
			{Role: "assist", Name: "亚马尔"},
			{Role: "assist", Name: "佩德里"},
			{Role: "defender", Name: "吕迪格"},
			{Role: "defender", Name: "基米希"},
		},
	}})
	result, err := service.Build(context.Background(), Request{Text: "莫拉塔进球，亚马尔和佩德里助攻，吕迪格和基米希防守。"}, MatchContext{
		MatchID: "multiple-goal-participants",
		Config: matchstate.MatchConfig{
			HomeTeam: "西班牙", AwayTeam: "德国",
			HomePlayers: []matchstate.Player{{Name: "莫拉塔"}, {Name: "亚马尔"}, {Name: "佩德里"}},
			AwayPlayers: []matchstate.Player{{Name: "吕迪格"}, {Name: "基米希"}},
		},
		Clock: matchstate.MatchClock{MatchID: "multiple-goal-participants", Period: "first_half"},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !result.Ready || len(result.Warnings) != 0 {
		t.Fatalf("multiple participant goal = %+v", result)
	}
	if len(result.Draft.Participants) != 5 {
		t.Fatalf("participants = %+v", result.Draft.Participants)
	}
}

func TestVoiceDraftRejectsCitationInstructionAsEventDescription(t *testing.T) {
	service := NewService(nil, stubExtractor{extraction: Extraction{
		Team:        "Spain",
		EventType:   "goal",
		Description: "Unless there is a specific request, citations should list author, year, and page number.",
		Participants: []ExtractedParticipant{
			{Role: "scorer", Name: "Fabian Ruiz"},
		},
	}})
	result, err := service.Build(context.Background(), Request{
		Text: "Fabian Ruiz shoots, Musiala defends, but the ball goes in.",
	}, MatchContext{
		MatchID: "citation-leak",
		Config: matchstate.MatchConfig{
			HomeTeam:    "Spain",
			AwayTeam:    "Germany",
			HomePlayers: []matchstate.Player{{Name: "Fabian Ruiz"}},
			AwayPlayers: []matchstate.Player{{Name: "Musiala"}},
		},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if result.Draft.Description != "" || result.Ready {
		t.Fatalf("citation instruction must not become a publishable description: %+v", result)
	}
	if len(result.Warnings) == 0 {
		t.Fatalf("expected an operator review warning: %+v", result)
	}
}

func TestVoiceDraftRejectsTeamNamePolicyAsEventDescription(t *testing.T) {
	service := NewService(nil, stubExtractor{extraction: Extraction{
		Team:        "Spain",
		EventType:   "goal",
		Description: "除非有明确要求，否则所有球队和球员的名字都保持原样，包括他们的国家/地区。",
		Participants: []ExtractedParticipant{
			{Role: "scorer", Name: "Fabian Ruiz"},
		},
	}})
	result, err := service.Build(context.Background(), Request{Text: "Fabian Ruiz shoots, Musiala defends, but the ball goes in."}, MatchContext{
		MatchID: "team-name-policy-leak",
		Config: matchstate.MatchConfig{
			HomeTeam:    "Spain",
			AwayTeam:    "Germany",
			HomePlayers: []matchstate.Player{{Name: "Fabian Ruiz"}},
			AwayPlayers: []matchstate.Player{{Name: "Musiala"}},
		},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if result.Draft.Description != "" || result.Ready || len(result.Warnings) == 0 {
		t.Fatalf("team-name policy must not become a publishable description: %+v", result)
	}
}

func TestVoiceDraftRejectsTeamNamePolicyAsTranscript(t *testing.T) {
	service := NewService(nil, stubExtractor{})
	_, err := service.Build(context.Background(), Request{
		Text: "除非有明确要求，否则所有球队和球员的名字都保持原样，包括他们的国家/地区。",
	}, MatchContext{})
	if !errors.Is(err, ErrInvalidTranscript) {
		t.Fatalf("expected invalid transcript error, got %v", err)
	}
}

func TestVoiceDraftRejectsNonMatchASRTranscript(t *testing.T) {
	service := NewService(nil, stubExtractor{extraction: Extraction{
		Team: "Spain", EventType: "goal", Description: "Fabian Ruiz scores.",
		Participants: []ExtractedParticipant{{Role: "scorer", Name: "Fabian Ruiz"}},
	}})
	_, err := service.Build(context.Background(), Request{Text: "0."}, MatchContext{
		MatchID: "invalid-asr-transcript",
		Config: matchstate.MatchConfig{
			HomeTeam:    "Spain",
			AwayTeam:    "Germany",
			HomePlayers: []matchstate.Player{{Name: "Fabian Ruiz"}},
		},
	})
	if !errors.Is(err, ErrInvalidTranscript) {
		t.Fatalf("expected invalid ASR transcript to be rejected, got %v", err)
	}
}

func TestVoiceTranscriptionReturnsRawTextWithoutEventExtraction(t *testing.T) {
	service := NewService(nil, nil)
	result, err := service.Transcribe(context.Background(), Request{Text: "法比安鲁伊斯射门，穆西亚拉防守，但是球进了"}, matchstate.MatchConfig{})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if result.Transcript != "法比安鲁伊斯射门，穆西亚拉防守，但是球进了" {
		t.Fatalf("transcript = %q", result.Transcript)
	}
}

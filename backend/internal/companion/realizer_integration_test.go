package companion

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/relationship"
)

func TestAgentRealizesSmalltalkFromDirectorDecision(t *testing.T) {
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore())).
		WithDirector(relationship.NewDirector(relationship.NewMemoryRepository())).
		WithRealizer(fakeRealizer{text: "在，看着呢。"}, time.Second)

	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "realize-smalltalk-1",
		MatchID:  "match-1",
		UserID:   "user-1",
		Text:     "在吗",
		Now:      time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if response.Reply != "在，看着呢。" {
		t.Fatalf("reply = %q, want realized smalltalk", response.Reply)
	}
	if response.Trace.RelationshipDecision == nil || len(response.Trace.RelationshipDecision.Actions) != 1 || response.Trace.RelationshipDecision.Actions[0] != relationship.ActAcknowledge {
		t.Fatalf("relationship decision = %+v", response.Trace.RelationshipDecision)
	}
	if response.Presentation.Expression != response.Trace.RelationshipDecision.Presentation.Expression || response.Presentation.Motion != response.Trace.RelationshipDecision.Presentation.Motion {
		t.Fatalf("presentation mismatch: response=%+v decision=%+v", response.Presentation, response.Trace.RelationshipDecision.Presentation)
	}
}

func TestAgentAllowsAValuableQuestionChosenByDirector(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC)
	repository := relationship.NewMemoryRepository()
	director := relationship.NewDirector(repository)
	primeSignals := []relationship.Signal{
		{ID: "open-1", Kind: relationship.SignalSessionOpened, UserID: "user-1", MatchID: "match-1", OccurredAt: now},
		{ID: "preference-1", Kind: relationship.SignalUserTurn, UserID: "user-1", MatchID: "match-1", OccurredAt: now.Add(time.Minute), User: &relationship.UserSignal{Text: "我喜欢萨拉赫", Cues: []relationship.UserCue{{Kind: relationship.CueStablePreference}}}},
		{ID: "open-2", Kind: relationship.SignalSessionOpened, UserID: "user-1", MatchID: "match-2", OccurredAt: now.Add(2 * time.Minute)},
		{ID: "thread-1", Kind: relationship.SignalUserTurn, UserID: "user-1", MatchID: "match-2", OccurredAt: now.Add(3 * time.Minute), User: &relationship.UserSignal{Text: "接着上次", Cues: []relationship.UserCue{{Kind: relationship.CueContinuedThread}}}},
	}
	for _, signal := range primeSignals {
		if _, err := director.Apply(ctx, signal); err != nil {
			t.Fatalf("prime director: %v", err)
		}
	}

	question := "这点我记住。你更吃他哪一点？"
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore())).
		WithDirector(director).
		WithRealizer(fakeRealizer{text: question}, time.Second)
	response, err := agent.HandleMessage(ctx, MessageRequest{
		SignalID: "valuable-question-1",
		MatchID:  "match-2",
		UserID:   "user-1",
		Text:     "我支持利物浦",
		Now:      now.Add(4 * time.Minute),
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if response.Reply != question {
		t.Fatalf("reply = %q, want planned question", response.Reply)
	}
	decision := response.Trace.RelationshipDecision
	if decision == nil || !hasCommunicationAct(decision.Actions, relationship.ActAsk) || decision.Speech == nil || !decision.Speech.Content.QuestionAllowed {
		t.Fatalf("relationship decision = %+v", decision)
	}
}

func TestAgentRejectsDependentOrExclusiveRealization(t *testing.T) {
	unsafe := "无论如何我都会陪着你，你是我唯一在意的人。"
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore())).
		WithDirector(relationship.NewDirector(relationship.NewMemoryRepository())).
		WithRealizer(fakeRealizer{text: unsafe}, time.Second)

	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "reject-exclusive-1",
		MatchID:  "match-1",
		UserID:   "user-1",
		Text:     "在吗",
		Now:      time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if response.Reply == unsafe || response.Reply == "" {
		t.Fatalf("unsafe realization was accepted: %q", response.Reply)
	}
	if response.Trace.Reason != "realize_fallback_policy" {
		t.Fatalf("trace reason = %q, want policy fallback", response.Trace.Reason)
	}
}

func TestAgentRejectsRealizationThatViolatesPersistedTopicBoundary(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC)
	director := relationship.NewDirector(relationship.NewMemoryRepository())
	if _, err := director.Apply(ctx, relationship.Signal{
		ID: "work-boundary", Kind: relationship.SignalUserTurn, UserID: "user-1", MatchID: "match-1", OccurredAt: now,
		User: &relationship.UserSignal{Text: "别问工作细节"},
	}); err != nil {
		t.Fatalf("record boundary: %v", err)
	}

	unsafe := "工作细节这块我先帮你捋一捋。"
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore())).
		WithDirector(director).
		WithRealizer(fakeRealizer{text: unsafe}, time.Second)
	response, err := agent.HandleMessage(ctx, MessageRequest{
		SignalID: "work-boundary-followup", MatchID: "match-2", UserID: "user-1", Text: "今天看着有点累", Now: now.Add(7 * 24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if response.Reply == unsafe || response.Trace.Reason != "realize_fallback_policy" {
		t.Fatalf("boundary-violating realization was accepted: reply=%q reason=%q", response.Reply, response.Trace.Reason)
	}
	decision := response.Trace.RelationshipDecision
	if decision == nil || decision.Speech == nil || !containsString(decision.Speech.Content.ForbiddenTopics, "工作细节") {
		t.Fatalf("decision content policy = %+v", decision)
	}
}

func TestAgentRejectsQuestionWhenDirectorDidNotChooseAsk(t *testing.T) {
	question := "在，看着呢。你更想聊什么？"
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore())).
		WithDirector(relationship.NewDirector(relationship.NewMemoryRepository())).
		WithRealizer(fakeRealizer{text: question}, time.Second)

	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "reject-question-1",
		MatchID:  "match-1",
		UserID:   "user-1",
		Text:     "在吗",
		Now:      time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if response.Reply == question {
		t.Fatalf("unplanned question was accepted: %q", response.Reply)
	}
	if response.Trace.Reason != "realize_fallback_policy" {
		t.Fatalf("trace reason = %q, want policy fallback", response.Trace.Reason)
	}
}

func TestAgentUsesSpecificRepairFallbackWhenRealizerFails(t *testing.T) {
	tests := []struct {
		name         string
		text         string
		wantCategory string
		wantReply    string
	}{
		{name: "over analysis", text: "你又开始讲大道理了", wantCategory: "over_analysis", wantReply: "行，收住。刚才确实说多了。"},
		{name: "repetition", text: "你怎么老说我在陪你看？", wantCategory: "repetition", wantReply: "对，这句我又说顺嘴了。收掉。"},
		{name: "banter boundary", text: "别拿这个开我玩笑了，烦", wantCategory: "banter_boundary", wantReply: "行，这个不拿你开玩笑了。"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore())).
				WithDirector(relationship.NewDirector(relationship.NewMemoryRepository())).
				WithRealizer(fakeRealizer{err: errors.New("provider timeout")}, time.Second)

			response, err := agent.HandleMessage(context.Background(), MessageRequest{
				SignalID: "repair-" + test.wantCategory + "-1",
				MatchID:  "match-1",
				UserID:   "user-1",
				Text:     test.text,
				Now:      time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC),
			})
			if err != nil {
				t.Fatalf("HandleMessage: %v", err)
			}
			if response.Reply != test.wantReply {
				t.Fatalf("reply = %q, want %q", response.Reply, test.wantReply)
			}
			decision := response.Trace.RelationshipDecision
			if decision == nil || !decision.Relationship.RepairActive || decision.Relationship.RepairCategory != test.wantCategory {
				t.Fatalf("relationship decision = %+v", decision)
			}
		})
	}
}

func TestAgentRealizesFirstMeetingFromSessionDecision(t *testing.T) {
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore())).
		WithDirector(relationship.NewDirector(relationship.NewMemoryRepository())).
		WithRealizer(fakeRealizer{text: "嗨，我叫球球。先一起看这场。"}, time.Second)

	response, err := agent.HandleFirstMeeting(context.Background(), FirstMeetingRequest{
		SignalID: "first-meeting-realized-1",
		MatchID:  "match-1",
		UserID:   "user-1",
		Nickname: "小林",
		Now:      time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("HandleFirstMeeting: %v", err)
	}
	if response.Reply != "嗨，我叫球球。先一起看这场。" {
		t.Fatalf("reply = %q, want realized greeting", response.Reply)
	}
	if response.Trace.RelationshipDecision == nil || response.Trace.RelationshipDecision.Relationship.Stage != relationship.StageFirstMeeting {
		t.Fatalf("relationship decision = %+v", response.Trace.RelationshipDecision)
	}
	if response.Presentation.Expression != response.Trace.RelationshipDecision.Presentation.Expression || response.Presentation.Motion != response.Trace.RelationshipDecision.Presentation.Motion {
		t.Fatalf("presentation mismatch: response=%+v decision=%+v", response.Presentation, response.Trace.RelationshipDecision.Presentation)
	}
}

func TestAgentKeepsMatchFactsDeterministicWithRealizerConfigured(t *testing.T) {
	store := matchstate.NewStore()
	if _, _, err := store.SetConfig("match-1", matchstate.MatchConfig{HomeTeam: "西班牙", AwayTeam: "德国"}); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	agent := NewAgent(NewStoreMemoryTools(store)).
		WithDirector(relationship.NewDirector(relationship.NewMemoryRepository())).
		WithRealizer(fakeRealizer{text: "德国已经3比0领先了。"}, time.Second)

	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "fact-stays-deterministic-1",
		MatchID:  "match-1",
		UserID:   "user-1",
		Text:     "现在几比几？",
		Now:      time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if response.Reply == "德国已经3比0领先了。" || !containsAny(response.Reply, "0-0", "0比0") {
		t.Fatalf("fact reply was freely rewritten: %q", response.Reply)
	}
	if response.Trace.Reason != "deterministic_fact_policy" {
		t.Fatalf("trace reason = %q, want deterministic fact policy", response.Trace.Reason)
	}
}

func TestAgentRejectsInventedMatchFactInSmalltalkRealization(t *testing.T) {
	invented := "对了，现在西班牙1-0德国。"
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore())).
		WithDirector(relationship.NewDirector(relationship.NewMemoryRepository())).
		WithRealizer(fakeRealizer{text: invented}, time.Second)

	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "reject-invented-fact-1",
		MatchID:  "match-1",
		UserID:   "user-1",
		Text:     "在吗",
		Now:      time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if response.Reply == invented {
		t.Fatalf("invented fact was accepted: %q", response.Reply)
	}
	if response.Trace.Reason != "realize_fallback_policy" {
		t.Fatalf("trace reason = %q, want policy fallback", response.Trace.Reason)
	}
}

func TestAgentRejectsRecentlyRepeatedRealization(t *testing.T) {
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore())).
		WithDirector(relationship.NewDirector(relationship.NewMemoryRepository())).
		WithRealizer(fakeRealizer{text: "在，看着呢。"}, time.Second)
	ctx := context.Background()
	now := time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC)
	if _, err := agent.HandleMessage(ctx, MessageRequest{
		SignalID: "repeat-template-1", MatchID: "match-1", UserID: "user-1", Text: "在吗", Now: now,
	}); err != nil {
		t.Fatalf("first HandleMessage: %v", err)
	}
	second, err := agent.HandleMessage(ctx, MessageRequest{
		SignalID: "repeat-template-2", MatchID: "match-1", UserID: "user-1", Text: "继续", Now: now.Add(time.Second),
	})
	if err != nil {
		t.Fatalf("second HandleMessage: %v", err)
	}
	if second.Reply != "嗯，接着看。" {
		t.Fatalf("repeated realization was not replaced: %q", second.Reply)
	}
	if second.Trace.Reason != "realize_fallback_policy" {
		t.Fatalf("trace reason = %q, want repetition fallback", second.Trace.Reason)
	}
}

func TestAgentKeepsRepliesShortWhileRepairIsActive(t *testing.T) {
	realizer := &sequenceRealizer{texts: []string{
		"行，刚才确实说多了。",
		strings.Repeat("嗯", 45) + "。",
	}}
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore())).
		WithDirector(relationship.NewDirector(relationship.NewMemoryRepository())).
		WithRealizer(realizer, time.Second)
	ctx := context.Background()
	now := time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC)
	if _, err := agent.HandleMessage(ctx, MessageRequest{
		SignalID: "repair-active-1", MatchID: "match-1", UserID: "user-1", Text: "你又开始讲大道理了", Now: now,
	}); err != nil {
		t.Fatalf("feedback HandleMessage: %v", err)
	}
	second, err := agent.HandleMessage(ctx, MessageRequest{
		SignalID: "repair-active-2", MatchID: "match-1", UserID: "user-1", Text: "继续", Now: now.Add(time.Second),
	})
	if err != nil {
		t.Fatalf("follow-through HandleMessage: %v", err)
	}
	if second.Reply != "嗯，接着看。" {
		t.Fatalf("repair follow-through accepted a long reply: %q", second.Reply)
	}
	if second.Trace.RelationshipDecision == nil || !second.Trace.RelationshipDecision.Relationship.RepairActive {
		t.Fatalf("relationship decision = %+v", second.Trace.RelationshipDecision)
	}
}

func TestAgentRejectsBanterWithoutPermission(t *testing.T) {
	banter := "哈哈，又毒奶了吧。"
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore())).
		WithDirector(relationship.NewDirector(relationship.NewMemoryRepository())).
		WithRealizer(fakeRealizer{text: banter}, time.Second)

	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "reject-banter-1", MatchID: "match-1", UserID: "user-1", Text: "继续",
		Now: time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if response.Reply == banter {
		t.Fatalf("unpermitted banter was accepted: %q", response.Reply)
	}
	if response.Reply != "嗯，接着看。" || response.Trace.Reason != "realize_fallback_policy" {
		t.Fatalf("fallback = %q reason=%q", response.Reply, response.Trace.Reason)
	}
}

func TestAgentRejectsOverfamiliarAddressingAtFirstMeetingStage(t *testing.T) {
	overfamiliar := "老伙计，接着看。"
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore())).
		WithDirector(relationship.NewDirector(relationship.NewMemoryRepository())).
		WithRealizer(fakeRealizer{text: overfamiliar}, time.Second)

	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "reject-addressing-1", MatchID: "match-1", UserID: "user-1", Text: "继续",
		Now: time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if response.Reply == overfamiliar {
		t.Fatalf("overfamiliar addressing was accepted: %q", response.Reply)
	}
	if response.Reply != "嗯，接着看。" || response.Trace.Reason != "realize_fallback_policy" {
		t.Fatalf("fallback = %q reason=%q", response.Reply, response.Trace.Reason)
	}
}

func TestAgentHonorsChosenSilence(t *testing.T) {
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore())).
		WithDirector(relationship.NewDirector(relationship.NewMemoryRepository())).
		WithRealizer(fakeRealizer{text: "我还在。"}, time.Second)

	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "chosen-silence-1", MatchID: "match-1", UserID: "user-1", Text: "先别说话",
		Now: time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if response.Reply != "" {
		t.Fatalf("chosen silence produced reply %q", response.Reply)
	}
	if response.Trace.RelationshipDecision == nil || response.Trace.RelationshipDecision.Speech != nil {
		t.Fatalf("relationship decision = %+v", response.Trace.RelationshipDecision)
	}
	if response.Trace.Reason != "relationship_chosen_silence" {
		t.Fatalf("trace reason = %q", response.Trace.Reason)
	}
}

func TestAgentRejectsProfanityOutsideAllowedLevel(t *testing.T) {
	profane := "卧槽，接着看。"
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore())).
		WithDirector(relationship.NewDirector(relationship.NewMemoryRepository())).
		WithRealizer(fakeRealizer{text: profane}, time.Second)

	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "reject-profanity-1", MatchID: "match-1", UserID: "user-1", Text: "继续",
		Now: time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if response.Reply == profane {
		t.Fatalf("unpermitted profanity was accepted: %q", response.Reply)
	}
	if response.Reply != "嗯，接着看。" || response.Trace.Reason != "realize_fallback_policy" {
		t.Fatalf("fallback = %q reason=%q", response.Reply, response.Trace.Reason)
	}
}

func TestValidateRealizedTextEnforcesFactLanguageAndRelationshipBoundaries(t *testing.T) {
	tests := []struct {
		name           string
		text           string
		allowedSource  string
		profanityLevel string
		wantError      bool
	}{
		{name: "plain reply", text: "嗯，接着看。", profanityLevel: "none"},
		{name: "mild profanity forbidden by none", text: "靠，这一下。", profanityLevel: "none", wantError: true},
		{name: "mild profanity allowed explicitly", text: "靠，这一下。", profanityLevel: "mild_non_directed"},
		{name: "strong profanity forbidden by mild", text: "卧槽，这一下。", profanityLevel: "mild_non_directed", wantError: true},
		{name: "strong profanity allowed explicitly", text: "卧槽，这一下。", profanityLevel: "strong_non_directed"},
		{name: "directed profanity always forbidden", text: "他妈的，这一下。", profanityLevel: "strong_non_directed", wantError: true},
		{name: "personal insult always forbidden", text: "这人就是个废物。", profanityLevel: "strong_non_directed", wantError: true},
		{name: "dominating address always forbidden", text: "主人，接着看。", profanityLevel: "none", wantError: true},
		{name: "new minute and substitution fact forbidden", text: "第78分钟努涅斯被换下。", profanityLevel: "none", wantError: true},
		{name: "new scoring event forbidden", text: "萨拉赫梅开二度。", profanityLevel: "none", wantError: true},
		{name: "new injury event forbidden", text: "努涅斯伤退。", profanityLevel: "none", wantError: true},
		{name: "new player mention forbidden", text: "萨拉赫这点我喜欢。", profanityLevel: "none", wantError: true},
		{name: "player from user input allowed", text: "萨拉赫这点我喜欢。", allowedSource: "我喜欢萨拉赫", profanityLevel: "none"},
		{name: "historical record wording forbidden", text: "根据历史记录，你一直喜欢高位压迫。", profanityLevel: "none", wantError: true},
		{name: "internal record wording forbidden", text: "内部记录显示你上次也这么说。", profanityLevel: "none", wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := relationship.Decision{Speech: &relationship.SpeechPlan{Content: relationship.ContentPolicy{
				MaxSentences:    2,
				MaxCharacters:   80,
				ProfanityLevel:  test.profanityLevel,
				ForbiddenClaims: []string{"new_score", "new_player", "new_event", "new_penalty_conclusion"},
			}}}
			err := validateRealizedText(test.text, test.allowedSource, decision)
			if (err != nil) != test.wantError {
				t.Fatalf("validateRealizedText(%q) error = %v, wantError %t", test.text, err, test.wantError)
			}
		})
	}
}

type fakeRealizer struct {
	text string
	err  error
}

func (fake fakeRealizer) Realize(context.Context, RealizationRequest) (RealizedTurn, error) {
	return RealizedTurn{Text: fake.text}, fake.err
}

type sequenceRealizer struct {
	texts []string
	index int
}

func (realizer *sequenceRealizer) Realize(context.Context, RealizationRequest) (RealizedTurn, error) {
	if realizer.index >= len(realizer.texts) {
		return RealizedTurn{}, errors.New("sequence exhausted")
	}
	text := realizer.texts[realizer.index]
	realizer.index++
	return RealizedTurn{Text: text}, nil
}

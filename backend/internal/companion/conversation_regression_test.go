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

func TestDirectorGoalQuestionsUseRecentMatchFacts(t *testing.T) {
	questions := []string{
		"刚才谁进的球？",
		"刚刚那个球谁破门的？",
		"最新这个进球是谁？",
		"谁进了？",
		"谁进球了？",
		"刚才谁进了？",
		"哪个球员进的？",
	}
	for _, question := range questions {
		if got := Classify(question); got != IntentRecentEvent {
			t.Errorf("Classify(%q) = %q, want %q", question, got, IntentRecentEvent)
		}
	}

	events := []matchstate.MatchEvent{
		{ID: "evt-pressure", EventType: "pressure", Clock: "78:30", Description: "利物浦持续压迫。"},
		{ID: "evt-goal", EventType: "goal", Clock: "77:10", PlayerName: "萨拉赫", Description: "萨拉赫禁区内推射破门。"},
	}
	reply, ids := answerRecentEvent(questions[0], events)
	if !strings.Contains(reply, "萨拉赫") {
		t.Fatalf("goal reply = %q, want scorer", reply)
	}
	if len(ids) != 1 || ids[0] != "evt-goal" {
		t.Fatalf("retrieved ids = %v, want latest goal", ids)
	}

	for _, question := range questions {
		reply, ids = answerRecentEvent(question, events)
		if !strings.Contains(reply, "萨拉赫") {
			t.Errorf("answerRecentEvent(%q) = %q, want scorer", question, reply)
		}
		if len(ids) != 1 || ids[0] != "evt-goal" {
			t.Errorf("answerRecentEvent(%q) ids = %v, want latest goal", question, ids)
		}
	}
}

func TestNaturalGoalScorerFollowUpUsesConfirmedEventInsteadOfPresenceFallback(t *testing.T) {
	store := matchstate.NewStore()
	if _, _, err := store.SetConfig("goal-follow-up", matchstate.MatchConfig{
		HomeTeam: "西班牙",
		AwayTeam: "德国",
	}); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	if _, _, err := store.Create("goal-follow-up", matchstate.MatchEvent{
		EventType:    "goal",
		Period:       "first_half",
		Clock:        "45:18",
		TeamID:       "home",
		TeamName:     "西班牙",
		PlayerName:   "佩德里",
		Participants: []matchstate.Participant{{Role: "scorer", Name: "佩德里", TeamID: "home", TeamName: "西班牙"}},
		Score:        matchstate.Score{Home: 1, Away: 0},
		Description:  "佩德里进球了。",
		Confirmed:    true,
		FactStatus:   matchstate.FactStatusConfirmed,
		Visibility:   "public",
	}); err != nil {
		t.Fatalf("Create goal: %v", err)
	}

	agent := NewAgent(NewStoreMemoryTools(store))
	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "goal-follow-up-turn",
		MatchID:  "goal-follow-up",
		UserID:   "user-1",
		Text:     "谁进了？",
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if response.Intent != IntentRecentEvent {
		t.Fatalf("intent = %q, want %q", response.Intent, IntentRecentEvent)
	}
	if !strings.Contains(response.Reply, "佩德里") || isPresenceOnlyReply(response.Reply) {
		t.Fatalf("reply = %q, want confirmed scorer instead of presence fallback", response.Reply)
	}
}

func TestCommonMatchStatusQuestionsDoNotFallBackToSmalltalk(t *testing.T) {
	questions := []string{
		"比赛什么情况了",
		"比赛时间是多少了？",
		"踢到第几分钟了",
	}
	for _, question := range questions {
		if got := Classify(question); got != IntentMatchStatus {
			t.Errorf("Classify(%q) = %q, want %q", question, got, IntentMatchStatus)
		}
	}
}

func TestTodayScheduleQuestionDoesNotFallBackToSmalltalk(t *testing.T) {
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore()))
	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "today-schedule-question",
		MatchID:  "today-schedule-question",
		UserID:   "user-1",
		Text:     "有什么比赛吗？",
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if response.Intent != IntentSchedule {
		t.Fatalf("intent = %q, want %q", response.Intent, IntentSchedule)
	}
	if response.Reply != "今天的赛程我还没拿到，你想查哪个联赛？" {
		t.Fatalf("reply = %q, want an honest schedule boundary", response.Reply)
	}
}

func TestTodayScheduleQuestionUsesAvailableFixtures(t *testing.T) {
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore())).WithScheduleReader(staticScheduleReader{
		fixtures: []ScheduleMatch{
			{HomeTeam: "曼城", AwayTeam: "利物浦"},
			{HomeTeam: "皇马", AwayTeam: "巴萨"},
		},
	})
	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "today-schedule-with-data",
		MatchID:  "today-schedule-with-data",
		UserID:   "user-1",
		Text:     "今晚有啥球？",
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if response.Intent != IntentSchedule {
		t.Fatalf("intent = %q, want %q", response.Intent, IntentSchedule)
	}
	if response.Reply != "今天有：曼城对利物浦、皇马对巴萨。" {
		t.Fatalf("reply = %q, want formatted schedule", response.Reply)
	}
	assertToolCalled(t, response.Trace, "schedule.read_today")
}

func TestTodayScheduleQuestionPrefersTheActiveMatchSnapshot(t *testing.T) {
	store := matchstate.NewStore()
	matchID := "active-friendly"
	if _, _, err := store.SetConfig(matchID, matchstate.MatchConfig{
		HomeTeam:    "西班牙",
		AwayTeam:    "德国",
		Competition: "友谊赛",
	}); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	elapsedSeconds := 23*60 + 41
	if _, err := store.SetClock(matchID, matchstate.ClockCommand{
		Action:          matchstate.ClockActionSet,
		Period:          "first_half",
		ElapsedSeconds:  &elapsedSeconds,
		ExpectedVersion: 0,
	}); err != nil {
		t.Fatalf("SetClock: %v", err)
	}
	if _, _, err := store.Create(matchID, matchstate.MatchEvent{
		EventType:   "goal",
		Period:      "first_half",
		Clock:       "23:41",
		TeamID:      "home",
		TeamName:    "西班牙",
		Score:       matchstate.Score{Home: 1, Away: 0},
		Description: "西班牙进球。",
		Confirmed:   true,
		FactStatus:  matchstate.FactStatusConfirmed,
		Visibility:  "public",
	}); err != nil {
		t.Fatalf("Create goal: %v", err)
	}

	agent := NewAgent(NewStoreMemoryTools(store)).WithScheduleReader(staticScheduleReader{
		fixtures: []ScheduleMatch{{HomeTeam: "曼城", AwayTeam: "利物浦"}},
	})
	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "active-match-schedule-question",
		MatchID:  matchID,
		UserID:   "user-1",
		Text:     "有什么比赛吗？",
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if response.Reply != "现在正在看西班牙对德国的友谊赛，西班牙 1-0 德国，时间在上半场 23:41。" {
		t.Fatalf("reply = %q, want active match score", response.Reply)
	}
	assertToolCalled(t, response.Trace, "match.read_snapshot")
	assertToolNotCalled(t, response.Trace, "schedule.read_today")
}

func TestMixedLanguageGreetingsUseTheSocialIntentFamily(t *testing.T) {
	testCases := []struct {
		input string
		want  Intent
		reply string
	}{
		{input: "好，Hello，球球，下午好啊。", want: IntentSmalltalk, reply: "下午好，来了。"},
		{input: "hello球球下午好", want: IntentSmalltalk, reply: "下午好，来了。"},
		{input: "喂，球球，早上好！", want: IntentSmalltalk, reply: "早上好，来了。"},
		{input: "你好，晚上好", want: IntentSmalltalk, reply: "晚上好，来了。"},
	}

	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore()))
	for _, testCase := range testCases {
		if got := Classify(testCase.input); got != testCase.want {
			t.Errorf("Classify(%q) = %q, want %q", testCase.input, got, testCase.want)
		}
		response, err := agent.HandleMessage(context.Background(), MessageRequest{
			SignalID: "mixed-greeting-" + testCase.input,
			MatchID:  "mixed-greeting",
			UserID:   "user-1",
			Text:     testCase.input,
		})
		if err != nil {
			t.Fatalf("HandleMessage(%q): %v", testCase.input, err)
		}
		if response.Reply != testCase.reply {
			t.Errorf("reply for %q = %q, want %q", testCase.input, response.Reply, testCase.reply)
		}
	}
}

func TestGreetingDoesNotOverrideAConcreteMatchQuestion(t *testing.T) {
	if got := Classify("你好，现在几比几？"); got != IntentMatchStatus {
		t.Fatalf("Classify(greeting plus score question) = %q, want %q", got, IntentMatchStatus)
	}
}

func TestGenericShortSmalltalkUsesAConversationBackchannel(t *testing.T) {
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore()))
	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "generic-smalltalk-backchannel",
		MatchID:  "generic-smalltalk-backchannel",
		UserID:   "user-1",
		Text:     "你说",
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if response.Intent != IntentSmalltalk {
		t.Fatalf("intent = %q, want %q", response.Intent, IntentSmalltalk)
	}
	if response.Reply != "嗯，我听着呢。" {
		t.Fatalf("reply = %q, want a conversational backchannel", response.Reply)
	}
}

func TestSocialClassifierUsesExplicitConversationActs(t *testing.T) {
	testCases := []struct {
		input string
		want  Intent
	}{
		{input: "陪我看会儿吧", want: IntentSmalltalk},
		{input: "开玩笑，德国3比0了", want: IntentSmalltalk},
		{input: "要是德国3比0就好了", want: IntentSmalltalk},
		{input: "谢谢你", want: IntentSmalltalk},
		{input: "好不好", want: IntentUnknown},
		{input: "这不是好问题", want: IntentUnknown},
		{input: "今天有什么比赛吗？", want: IntentSchedule},
		{input: "有什么比赛吗？", want: IntentSchedule},
	}
	for _, testCase := range testCases {
		if got := Classify(testCase.input); got != testCase.want {
			t.Errorf("Classify(%q) = %q, want %q", testCase.input, got, testCase.want)
		}
	}
}

func TestConnectionCheckGetsNaturalSmalltalkReply(t *testing.T) {
	testCases := []struct {
		input string
		want  string
	}{
		{input: "喂，球球，能听到我说话吗？", want: "听得到。"},
		{input: "球球，你还听得到吗？", want: "听得到。"},
		{input: "在吗？", want: "在，听着呢。"},
	}

	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore()))
	for _, testCase := range testCases {
		response, err := agent.HandleMessage(context.Background(), MessageRequest{
			SignalID: "connection-check-" + testCase.input,
			MatchID:  "connection-check",
			UserID:   "user-1",
			Text:     testCase.input,
		})
		if err != nil {
			t.Fatalf("HandleMessage(%q): %v", testCase.input, err)
		}
		if response.Intent != IntentSmalltalk {
			t.Errorf("intent for %q = %q, want %q", testCase.input, response.Intent, IntentSmalltalk)
		}
		if response.Reply != testCase.want {
			t.Errorf("reply for %q = %q, want %q", testCase.input, response.Reply, testCase.want)
		}
		if containsAny(response.Reply, "按陪看", "已经确认的比赛信息") {
			t.Errorf("reply leaked internal routing policy: %q", response.Reply)
		}
	}
}

func TestUnknownReplyDoesNotExposeInternalRoutingPolicy(t *testing.T) {
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore()))
	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "unknown-natural-fallback",
		MatchID:  "unknown-natural-fallback",
		UserID:   "user-1",
		Text:     "请你分析一下今天球场草皮对传控节奏的隐藏影响",
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if response.Intent != IntentUnknown {
		t.Fatalf("intent = %q, want %q", response.Intent, IntentUnknown)
	}
	if containsAny(response.Reply, "按陪看", "已经确认的比赛信息", "理解") {
		t.Fatalf("unknown reply leaked internal routing policy: %q", response.Reply)
	}
	if !strings.Contains(response.Reply, "没接明白") {
		t.Fatalf("unknown reply = %q, want a natural clarification", response.Reply)
	}
}

func TestComplimentFollowUpKeepsTheConversationThread(t *testing.T) {
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore())).WithDirector(
		relationship.NewDirector(relationship.NewMemoryRepository()),
	).WithRealizer(fakeRealizer{err: errors.New("provider timeout")}, time.Second)

	first, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "compliment-thread-1",
		MatchID:  "compliment-thread",
		UserID:   "user-1",
		Text:     "你今天看起来精神不错呀！",
	})
	if err != nil {
		t.Fatalf("first HandleMessage: %v", err)
	}
	if first.Intent != IntentSmalltalk {
		t.Errorf("first intent = %q, want %q", first.Intent, IntentSmalltalk)
	}
	if first.Reply != "被你看出来了，今天状态确实不错。" {
		t.Errorf("first reply = %q, want a natural compliment response", first.Reply)
	}

	second, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "compliment-thread-2",
		MatchID:  "compliment-thread",
		UserID:   "user-1",
		Text:     "有点意思吗？我觉得你是有点意思。",
	})
	if err != nil {
		t.Fatalf("second HandleMessage: %v", err)
	}
	if second.Intent != IntentSmalltalk {
		t.Errorf("second intent = %q, want %q", second.Intent, IntentSmalltalk)
	}
	if second.Reply != "那我就当你是在夸我了。" {
		t.Fatalf("second reply = %q, want a natural follow-up", second.Reply)
	}
	if strings.Contains(second.Reply, "没接明白") {
		t.Fatalf("compliment follow-up collapsed to unknown fallback: %q", second.Reply)
	}
}

func TestDisbeliefReactionGetsARealConversationTurn(t *testing.T) {
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore())).WithDirector(
		relationship.NewDirector(relationship.NewMemoryRepository()),
	).WithRealizer(fakeRealizer{text: "嗯，我在呢。"}, time.Second)
	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "disbelief-turn-1",
		MatchID:  "match-disbelief",
		UserID:   "user-1",
		Text:     "真的假的",
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if response.Intent != IntentEmotionReaction {
		t.Fatalf("intent = %q, want %q", response.Intent, IntentEmotionReaction)
	}
	if response.Reply == "嗯，我在。" {
		t.Fatal("disbelief reaction fell back to presence acknowledgement")
	}
	if !strings.Contains(response.Reply, "真的假的") || !strings.Contains(response.Reply, "刚刚") {
		t.Fatalf("reply = %q, want disbelief acknowledgement with a contextual follow-up", response.Reply)
	}
}

func TestShortMatchPraiseSurvivesRealizerFailureWithoutInventingAGoal(t *testing.T) {
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore())).WithDirector(
		relationship.NewDirector(relationship.NewMemoryRepository()),
	).WithRealizer(fakeRealizer{err: errors.New("provider timeout")}, time.Second)
	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "short-match-praise-1",
		MatchID:  "match-short-praise",
		UserID:   "user-1",
		Text:     "好球！",
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if response.Intent != IntentEmotionReaction {
		t.Fatalf("intent = %q, want %q", response.Intent, IntentEmotionReaction)
	}
	if response.Reply == "嗯，我在。" || isPresenceOnlyReply(response.Reply) {
		t.Fatalf("short match praise fell back to presence acknowledgement: %q", response.Reply)
	}
	if containsAny(response.Reply, "进球", "破门", "领先") {
		t.Fatalf("short match praise invented a match fact: %q", response.Reply)
	}
}

func TestNaturalSelfReportDoesNotCollapseToPresenceFallback(t *testing.T) {
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore())).WithDirector(
		relationship.NewDirector(relationship.NewMemoryRepository()),
	).WithRealizer(fakeRealizer{err: errors.New("provider timeout")}, time.Second)
	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "natural-self-report-1",
		MatchID:  "match-natural-self-report",
		UserID:   "user-1",
		Text:     "我打进啦",
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if isPresenceOnlyReply(response.Reply) {
		t.Fatalf("natural self-report collapsed to presence acknowledgement: intent=%q reply=%q", response.Intent, response.Reply)
	}
	if response.Intent != IntentPersonalShare {
		t.Fatalf("intent = %q, want %q", response.Intent, IntentPersonalShare)
	}
	if response.Trace.RelationshipDecision == nil || !hasCommunicationAct(response.Trace.RelationshipDecision.Actions, relationship.ActReact) {
		t.Fatalf("relationship decision = %+v, want react action", response.Trace.RelationshipDecision)
	}
}

func TestColloquialGoalReportUsesFactVerificationInsteadOfPresenceFallback(t *testing.T) {
	store := matchstate.NewStore()
	matchID := "colloquial-goal-report"
	if _, _, err := store.SetConfig(matchID, matchstate.MatchConfig{HomeTeam: "西班牙", AwayTeam: "德国"}); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	elapsedSeconds := 10 * 60
	if _, err := store.SetClock(matchID, matchstate.ClockCommand{
		Action:          matchstate.ClockActionSet,
		Period:          "first_half",
		ElapsedSeconds:  &elapsedSeconds,
		ExpectedVersion: 0,
	}); err != nil {
		t.Fatalf("SetClock: %v", err)
	}
	if _, _, err := store.Create(matchID, matchstate.MatchEvent{
		EventType:   "kickoff",
		Period:      "first_half",
		Clock:       "10:00",
		Score:       matchstate.Score{},
		Description: "比赛开始。",
		Confirmed:   true,
		FactStatus:  matchstate.FactStatusConfirmed,
		Visibility:  "public",
	}); err != nil {
		t.Fatalf("Create kickoff: %v", err)
	}

	agent := NewAgent(NewStoreMemoryTools(store)).WithDirector(
		relationship.NewDirector(relationship.NewMemoryRepository()),
	)
	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "colloquial-goal-report-1",
		MatchID:  matchID,
		UserID:   "user-1",
		Text:     "进啦",
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if response.Intent != IntentMatchClaim {
		t.Fatalf("intent = %q, want %q", response.Intent, IntentMatchClaim)
	}
	if response.Trace.Claim == nil || response.Trace.Claim.Status != ClaimStatusUnverified {
		t.Fatalf("claim = %+v, want unverified event claim", response.Trace.Claim)
	}
	if isPresenceOnlyReply(response.Reply) {
		t.Fatalf("colloquial goal report collapsed to presence acknowledgement: %q", response.Reply)
	}
}

func TestPresenceOnlyRealizationIsRejectedForNonPresenceInput(t *testing.T) {
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore())).WithDirector(
		relationship.NewDirector(relationship.NewMemoryRepository()),
	).WithRealizer(fakeRealizer{text: "嗯，我在。"}, time.Second)
	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "reject-presence-only-1",
		MatchID:  "match-reject-presence-only",
		UserID:   "user-1",
		Text:     "今天真累",
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if isPresenceOnlyReply(response.Reply) {
		t.Fatalf("presence-only realization was accepted for a natural statement: %q", response.Reply)
	}
	if response.Trace.Reason != "realize_fallback_policy" {
		t.Fatalf("trace reason = %q, want realize_fallback_policy", response.Trace.Reason)
	}
}

func TestUserRepliesDoNotExposeEnterpriseFactSources(t *testing.T) {
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore()))
	inputs := []string{
		"哈哈太激动了",
		"刚才比赛发生了什么？",
		"有人进球了吗？",
	}
	for _, input := range inputs {
		response, err := agent.HandleMessage(context.Background(), MessageRequest{
			MatchID: "user-surface-language",
			UserID:  "user-1",
			Text:    input,
		})
		if err != nil {
			t.Fatalf("HandleMessage(%q) error: %v", input, err)
		}
		if strings.Contains(response.Reply, "导演台") || strings.Contains(response.Reply, "导播台") {
			t.Errorf("user reply leaked enterprise terminology: %q", response.Reply)
		}
	}
}

type staticScheduleReader struct {
	fixtures []ScheduleMatch
	err      error
}

func (reader staticScheduleReader) TodayFixtures(context.Context) ([]ScheduleMatch, error) {
	return reader.fixtures, reader.err
}

func TestDisplayPeriodNeverLeaksInternalPrematchCode(t *testing.T) {
	if got := displayPeriod("pre_match"); got != "赛前" {
		t.Fatalf("displayPeriod(pre_match) = %q, want 赛前", got)
	}
}

func TestCompanionReadsOnlyPublicMatchFacts(t *testing.T) {
	store := matchstate.NewStore()
	if _, _, err := store.Create("public-only", matchstate.MatchEvent{
		Source:      "api-sports",
		Period:      "first_half",
		Clock:       "12:00",
		EventType:   "goal",
		TeamID:      "home",
		PlayerName:  "Saka",
		Score:       matchstate.Score{Home: 1},
		Description: "Saka scored",
		Visibility:  "public",
	}); err != nil {
		t.Fatalf("Create provisional event: %v", err)
	}
	tools := NewStoreMemoryTools(store)
	snapshot, err := tools.Snapshot(context.Background(), "public-only")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if snapshot.Score != (matchstate.Score{}) {
		t.Fatalf("companion snapshot exposed provisional score: %+v", snapshot)
	}
	events, err := tools.RecentEvents(context.Background(), "public-only", 8)
	if err != nil {
		t.Fatalf("RecentEvents: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("companion events exposed provisional facts: %+v", events)
	}
}

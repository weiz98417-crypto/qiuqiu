package companion

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"qiuqiu/internal/matchstate"
)

func TestClassifyScheduleIntentUsesIndependentScopeFields(t *testing.T) {
	testCases := []struct {
		input string
		want  ScheduleScope
	}{
		{input: "有什么比赛吗？", want: ScheduleScopeNearby},
		{input: "现在有什么比赛？", want: ScheduleScopeCurrent},
		{input: "今天有啥球？", want: ScheduleScopeToday},
		{input: "明天足球赛程呢？", want: ScheduleScopeTomorrow},
	}
	for _, testCase := range testCases {
		intent := ClassifyScheduleIntent(testCase.input)
		if intent.Topic != "football_schedule" || intent.Action != "query" {
			t.Errorf("ClassifyScheduleIntent(%q) = %#v, want football query", testCase.input, intent)
		}
		if intent.Scope != testCase.want {
			t.Errorf("ClassifyScheduleIntent(%q).Scope = %q, want %q", testCase.input, intent.Scope, testCase.want)
		}
	}
	if got := Classify("比赛现在什么情况？"); got != IntentMatchStatus {
		t.Fatalf("fact question was classified as %q, want %q", got, IntentMatchStatus)
	}
}

func TestBuildScheduleSearchRequestUsesUserTimezone(t *testing.T) {
	now := time.Date(2026, 7, 23, 4, 45, 0, 0, time.UTC)
	testCases := []struct {
		intent ScheduleIntent
		from   time.Time
		to     time.Time
	}{
		{
			intent: ScheduleIntent{Scope: ScheduleScopeToday},
			from:   time.Date(2026, 7, 23, 0, 0, 0, 0, time.FixedZone("CST", 8*60*60)),
			to:     time.Date(2026, 7, 24, 0, 0, 0, 0, time.FixedZone("CST", 8*60*60)),
		},
		{
			intent: ScheduleIntent{Scope: ScheduleScopeTomorrow},
			from:   time.Date(2026, 7, 24, 0, 0, 0, 0, time.FixedZone("CST", 8*60*60)),
			to:     time.Date(2026, 7, 25, 0, 0, 0, 0, time.FixedZone("CST", 8*60*60)),
		},
		{
			intent: ScheduleIntent{Scope: ScheduleScopeNearby},
			from:   time.Date(2026, 7, 23, 0, 0, 0, 0, time.FixedZone("CST", 8*60*60)),
			to:     time.Date(2026, 7, 25, 0, 0, 0, 0, time.FixedZone("CST", 8*60*60)),
		},
	}
	for _, testCase := range testCases {
		request := BuildScheduleSearchRequest(testCase.intent, now, "Asia/Shanghai")
		if !request.From.Equal(testCase.from) || !request.To.Equal(testCase.to) {
			t.Errorf("BuildScheduleSearchRequest(%q) = %s to %s, want %s to %s", testCase.intent.Scope, request.From, request.To, testCase.from, testCase.to)
		}
		if request.Timezone != "Asia/Shanghai" {
			t.Errorf("timezone = %q, want Asia/Shanghai", request.Timezone)
		}
	}
}

func TestBuildScheduleSearchRequestAcceptsUTCOffsetTimezone(t *testing.T) {
	now := time.Date(2026, 7, 23, 18, 0, 0, 0, time.UTC)
	request := BuildScheduleSearchRequest(
		ScheduleIntent{Scope: ScheduleScopeTomorrow},
		now,
		"UTC+08:00",
	)
	wantFrom := time.Date(2026, 7, 25, 0, 0, 0, 0, time.FixedZone("UTC+08:00", 8*60*60))
	if request.Timezone != "UTC+08:00" || !request.From.Equal(wantFrom) {
		t.Fatalf("offset schedule request = %#v, want timezone UTC+08:00 from %s", request, wantFrom)
	}
}

func TestScheduleSearchReaderReturnsTypedFixtureMetadata(t *testing.T) {
	homeScore, awayScore := 1, 0
	reader := rangeScheduleReader{result: ScheduleSearchResult{
		Fixtures: []ScheduleMatch{{
			FixtureID:   "fixture-1",
			HomeTeam:    "西班牙",
			AwayTeam:    "德国",
			Competition: "友谊赛",
			KickoffAt:   time.Date(2026, 7, 25, 20, 0, 0, 0, time.FixedZone("CST", 8*60*60)),
			Status:      "1H",
			HomeScore:   &homeScore,
			AwayScore:   &awayScore,
		}},
		Source:    "api-sports",
		Freshness: "fresh",
	}}
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore())).WithScheduleReader(&reader)
	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "schedule-search-metadata",
		MatchID:  "schedule-search-metadata",
		UserID:   "user-1",
		Text:     "明天有什么比赛？",
		Timezone: "Asia/Shanghai",
		Now:      time.Date(2026, 7, 23, 18, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if response.Trace.Schedule == nil || response.Trace.Schedule.Scope != ScheduleScopeTomorrow {
		t.Fatalf("trace schedule = %#v, want tomorrow intent", response.Trace.Schedule)
	}
	assertToolCalled(t, response.Trace, "schedule.search")
	if response.Reply != "明天有：西班牙对德国（友谊赛，20:00开球，进行中） 1-0。来源：api-sports，数据刚刚更新。" {
		t.Fatalf("reply = %q, want normalized fixture details", response.Reply)
	}
	if reader.lastRequest.Timezone != "Asia/Shanghai" ||
		!reader.lastRequest.From.Equal(time.Date(2026, 7, 25, 0, 0, 0, 0, time.FixedZone("CST", 8*60*60))) {
		t.Fatalf("search request = %#v, want Asia/Shanghai tomorrow window", reader.lastRequest)
	}
}

func TestScheduleSearchRejectsExplicitlyStaleResults(t *testing.T) {
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore())).WithScheduleReader(&rangeScheduleReader{
		result: ScheduleSearchResult{
			Fixtures:  []ScheduleMatch{{HomeTeam: "西班牙", AwayTeam: "德国"}},
			Freshness: "stale",
		},
	})
	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "schedule-search-stale",
		MatchID:  "schedule-search-stale",
		UserID:   "user-1",
		Text:     "今天有什么比赛？",
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if !strings.Contains(response.Reply, "不拿它当准确信息") {
		t.Fatalf("stale reply = %q", response.Reply)
	}
}

func TestActiveMatchScheduleReplyRejectsUnreliableIntegrity(t *testing.T) {
	for _, status := range []string{"stale", "conflict"} {
		_, active := activeMatchScheduleReply(matchstate.Snapshot{
			HomeTeam:  "西班牙",
			AwayTeam:  "德国",
			Period:    "first_half",
			Integrity: matchstate.MatchIntegrity{Status: status},
		})
		if active {
			t.Fatalf("integrity %q was treated as an active match", status)
		}
	}
}

func TestScheduleSearchRejectsIncompleteFixtures(t *testing.T) {
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore())).WithScheduleReader(&rangeScheduleReader{
		result: ScheduleSearchResult{Fixtures: []ScheduleMatch{{HomeTeam: "西班牙"}}},
	})
	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "schedule-search-incomplete",
		MatchID:  "schedule-search-incomplete",
		UserID:   "user-1",
		Text:     "今天有什么比赛？",
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if response.Reply != "我查到这个时间段暂时没有可靠的赛程。" {
		t.Fatalf("reply = %q, want an honest empty result", response.Reply)
	}
}

func TestProgressiveScheduleLookupAcknowledgesBeforeSearching(t *testing.T) {
	now := time.Now().UTC()
	reader := &rangeScheduleReader{result: ScheduleSearchResult{
		Fixtures: []ScheduleMatch{{HomeTeam: "Spain", AwayTeam: "Germany"}},
		Source:   "api-sports",
	}}
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore())).WithScheduleReader(reader)

	acknowledgement, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID:            "progressive-schedule",
		MatchID:             "progressive-schedule",
		UserID:              "user-1",
		Text:                "明天有什么比赛？",
		Timezone:            "Asia/Shanghai",
		ProgressiveSchedule: true,
		Now:                 now,
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if acknowledgement.ScheduleLookup == nil {
		t.Fatal("progressive schedule response did not include a pending lookup")
	}
	if reader.searchCalls != 0 {
		t.Fatalf("schedule search ran before acknowledgement, calls = %d", reader.searchCalls)
	}
	lookup := *acknowledgement.ScheduleLookup
	if lookup.ParentTraceID != acknowledgement.Trace.ID || lookup.ID == "" || acknowledgement.Trace.LookupID != lookup.ID {
		t.Fatalf("lookup trace linkage = %+v, acknowledgement trace = %q", lookup, acknowledgement.Trace.ID)
	}
	if acknowledgement.Reply == "" || !lookup.ExpiresAt.After(now) {
		t.Fatalf("invalid acknowledgement response: %+v", acknowledgement)
	}

	result, err := agent.ResolveScheduleLookup(context.Background(), lookup)
	if err != nil {
		t.Fatalf("ResolveScheduleLookup: %v", err)
	}
	if reader.searchCalls != 1 {
		t.Fatalf("schedule search calls = %d, want 1", reader.searchCalls)
	}
	if result.Trace.ParentTraceID != acknowledgement.Trace.ID || result.Trace.LookupID != lookup.ID {
		t.Fatalf("result parent trace = %q, want %q", result.Trace.ParentTraceID, acknowledgement.Trace.ID)
	}
	ledgerEvents, err := agent.interactions.List(context.Background(), lookup.UserID, lookup.MatchID, 20)
	if err != nil {
		t.Fatalf("list interaction ledger: %v", err)
	}
	var projectedResult Trace
	for _, event := range ledgerEvents {
		if event.TraceID == result.Trace.ID {
			if err := json.Unmarshal(event.TracePayload, &projectedResult); err != nil {
				t.Fatalf("decode projected schedule result: %v", err)
			}
			break
		}
	}
	if projectedResult.ID != result.Trace.ID || projectedResult.LookupID != lookup.ID || projectedResult.ParentTraceID != acknowledgement.Trace.ID {
		t.Fatalf("schedule result was not fully recorded in interaction ledger: %+v", projectedResult)
	}
	if toolCallNamed(projectedResult, "schedule.search") == nil {
		t.Fatal("projected schedule result omitted schedule.search tool call")
	}
	if !strings.Contains(result.Reply, "Spain") || !strings.Contains(result.Reply, "Germany") {
		t.Fatalf("schedule result = %q, want fixture teams", result.Reply)
	}
	searchCall := toolCallNamed(result.Trace, "schedule.search")
	if searchCall == nil || searchCall.Args["state"] != "completed" || searchCall.Args["source"] != "api-sports" {
		t.Fatalf("schedule result trace call = %+v", searchCall)
	}
}

func TestProgressiveScheduleLookupReturnsSafeProviderFailure(t *testing.T) {
	reader := &rangeScheduleReader{err: errors.New("secret provider credential rejected")}
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore())).WithScheduleReader(reader)
	acknowledgement, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID:            "progressive-schedule-failure",
		MatchID:             "progressive-schedule-failure",
		UserID:              "user-1",
		Text:                "今天有什么比赛？",
		ProgressiveSchedule: true,
		Now:                 time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}

	result, err := agent.ResolveScheduleLookup(context.Background(), *acknowledgement.ScheduleLookup)
	if err != nil {
		t.Fatalf("ResolveScheduleLookup: %v", err)
	}
	if strings.Contains(result.Reply, "secret") || strings.Contains(result.Reply, "credential") {
		t.Fatalf("provider details leaked into reply: %q", result.Reply)
	}
	if result.Trace.Error == "" || result.Trace.Reason != "schedule_lookup_unavailable" {
		t.Fatalf("provider failure trace = %+v", result.Trace)
	}
}

func TestProgressiveScheduleLookupExpiresWithoutSearching(t *testing.T) {
	reader := &rangeScheduleReader{}
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore())).WithScheduleReader(reader)
	_, err := agent.ResolveScheduleLookup(context.Background(), ScheduleLookup{
		ID:        "expired-lookup",
		ExpiresAt: time.Now().Add(-time.Second),
	})
	if !errors.Is(err, ErrScheduleLookupExpired) {
		t.Fatalf("ResolveScheduleLookup error = %v, want ErrScheduleLookupExpired", err)
	}
	if reader.searchCalls != 0 {
		t.Fatalf("expired lookup searched provider %d times", reader.searchCalls)
	}
}

func TestProgressiveScheduleLookupPrefersMatchThatBecameActive(t *testing.T) {
	store := matchstate.NewStore()
	matchID := "schedule-context-update"
	if _, _, err := store.SetConfig(matchID, matchstate.MatchConfig{
		HomeTeam:    "Spain",
		AwayTeam:    "Germany",
		Competition: "Friendly",
	}); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	reader := &rangeScheduleReader{result: ScheduleSearchResult{
		Fixtures: []ScheduleMatch{{HomeTeam: "France", AwayTeam: "Brazil"}},
	}}
	agent := NewAgent(NewStoreMemoryTools(store)).WithScheduleReader(reader)
	acknowledgement, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID:            "progressive-schedule-context-update",
		MatchID:             matchID,
		UserID:              "user-1",
		Text:                "现在有什么比赛？",
		ProgressiveSchedule: true,
		Now:                 time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	elapsedSeconds := 12 * 60
	if _, err := store.SetClock(matchID, matchstate.ClockCommand{
		Action:         matchstate.ClockActionSet,
		Period:         "first_half",
		ElapsedSeconds: &elapsedSeconds,
	}); err != nil {
		t.Fatalf("SetClock: %v", err)
	}

	result, err := agent.ResolveScheduleLookup(context.Background(), *acknowledgement.ScheduleLookup)
	if err != nil {
		t.Fatalf("ResolveScheduleLookup: %v", err)
	}
	if !strings.Contains(result.Reply, "Spain") || !strings.Contains(result.Reply, "Germany") || strings.Contains(result.Reply, "France") {
		t.Fatalf("updated context result = %q", result.Reply)
	}
	if result.Trace.Reason != "schedule_lookup_context_updated" || reader.searchCalls != 0 {
		t.Fatalf("updated context trace = %+v, search calls = %d", result.Trace, reader.searchCalls)
	}
}

func TestProgressiveScheduleLookupSuppressesProviderResultWhenMatchBecomesActiveDuringSearch(t *testing.T) {
	store := matchstate.NewStore()
	matchID := "schedule-context-update-during-search"
	if _, _, err := store.SetConfig(matchID, matchstate.MatchConfig{
		HomeTeam:    "Spain",
		AwayTeam:    "Germany",
		Competition: "Friendly",
	}); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	reader := &blockingScheduleReader{
		started: make(chan struct{}),
		release: make(chan struct{}),
		result: ScheduleSearchResult{
			Fixtures: []ScheduleMatch{{HomeTeam: "France", AwayTeam: "Brazil"}},
			Source:   "api-sports",
		},
	}
	agent := NewAgent(NewStoreMemoryTools(store)).WithScheduleReader(reader)
	acknowledgement, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID:            "progressive-schedule-context-during-search",
		MatchID:             matchID,
		UserID:              "user-1",
		Text:                "现在有什么比赛？",
		ProgressiveSchedule: true,
		Now:                 time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}

	type lookupResult struct {
		response Response
		err      error
	}
	resultChannel := make(chan lookupResult, 1)
	go func() {
		response, resolveErr := agent.ResolveScheduleLookup(context.Background(), *acknowledgement.ScheduleLookup)
		resultChannel <- lookupResult{response: response, err: resolveErr}
	}()
	select {
	case <-reader.started:
	case <-time.After(time.Second):
		t.Fatal("schedule search did not start")
	}
	elapsedSeconds := 12 * 60
	if _, err := store.SetClock(matchID, matchstate.ClockCommand{
		Action:         matchstate.ClockActionSet,
		Period:         "first_half",
		ElapsedSeconds: &elapsedSeconds,
	}); err != nil {
		t.Fatalf("SetClock: %v", err)
	}
	close(reader.release)

	var resolved lookupResult
	select {
	case resolved = <-resultChannel:
	case <-time.After(time.Second):
		t.Fatal("schedule lookup did not finish")
	}
	if resolved.err != nil {
		t.Fatalf("ResolveScheduleLookup: %v", resolved.err)
	}
	if !strings.Contains(resolved.response.Reply, "Spain") ||
		!strings.Contains(resolved.response.Reply, "Germany") ||
		strings.Contains(resolved.response.Reply, "France") {
		t.Fatalf("updated context result = %q", resolved.response.Reply)
	}
	if resolved.response.Trace.Reason != "schedule_lookup_context_updated" || reader.searchCalls != 1 {
		t.Fatalf("updated context trace = %+v, search calls = %d", resolved.response.Trace, reader.searchCalls)
	}
}

func toolCallNamed(trace Trace, name string) *ToolCall {
	for index := range trace.ToolCalls {
		if trace.ToolCalls[index].Name == name {
			return &trace.ToolCalls[index]
		}
	}
	return nil
}

type rangeScheduleReader struct {
	result      ScheduleSearchResult
	err         error
	lastRequest ScheduleSearchRequest
	searchCalls int
}

type blockingScheduleReader struct {
	started     chan struct{}
	release     chan struct{}
	result      ScheduleSearchResult
	lastRequest ScheduleSearchRequest
	searchCalls int
}

func (reader *blockingScheduleReader) Search(ctx context.Context, request ScheduleSearchRequest) (ScheduleSearchResult, error) {
	reader.searchCalls++
	reader.lastRequest = request
	close(reader.started)
	select {
	case <-reader.release:
		return reader.result, nil
	case <-ctx.Done():
		return ScheduleSearchResult{}, ctx.Err()
	}
}

func (reader *blockingScheduleReader) TodayFixtures(context.Context) ([]ScheduleMatch, error) {
	return reader.result.Fixtures, nil
}

func (reader *rangeScheduleReader) Search(_ context.Context, request ScheduleSearchRequest) (ScheduleSearchResult, error) {
	reader.searchCalls++
	reader.lastRequest = request
	return reader.result, reader.err
}

func (reader *rangeScheduleReader) TodayFixtures(context.Context) ([]ScheduleMatch, error) {
	return reader.result.Fixtures, nil
}

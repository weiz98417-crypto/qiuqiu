package evals

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"qiuqiu/internal/companion"
	"qiuqiu/internal/knowledge"
	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/memory"
	"qiuqiu/internal/observation"
	"qiuqiu/internal/relationship"
	qiuqiuRouter "qiuqiu/internal/router"
)

func Run(ctx context.Context, cases []Case) Report {
	startedAt := time.Now().UTC()
	report := Report{SchemaVersion: SchemaVersion, StartedAt: startedAt}
	for _, evalCase := range cases {
		report.Cases = append(report.Cases, runCase(ctx, evalCase))
	}
	report.FinishedAt = time.Now().UTC()
	report.Scorecard = summarize(report.Cases)
	return report
}

// materializeKnowledge 把夹具条目写进临时 YAML 目录再走正式 Load（与
// 生产同一解析与校验路径），返回库与清理函数。
func materializeKnowledge(entries []KnowledgeEntryFixture) (*knowledge.Library, func(), error) {
	dir, err := os.MkdirTemp("", "evals-knowledge-")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	for _, entry := range entries {
		body, err := yaml.Marshal(entry)
		if err != nil {
			cleanup()
			return nil, nil, err
		}
		path := filepath.Join(dir, strings.ReplaceAll(entry.ID, "/", "_")+".yaml")
		if err := os.WriteFile(path, body, 0o644); err != nil {
			cleanup()
			return nil, nil, err
		}
	}
	library, err := knowledge.Load(dir, nil)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	return library, cleanup, nil
}

func runCase(ctx context.Context, evalCase Case) CaseResult {
	startedAt := time.Now()
	result := CaseResult{ID: evalCase.ID, Suite: evalCase.Suite, Tags: evalCase.Tags, Scores: map[string]float64{}}
	store := matchstate.NewStore()
	tools := companion.NewStoreMemoryTools(store)
	memorySeam := memory.NewFake()
	agent := companion.NewAgent(tools).WithDirector(relationship.NewDirector(relationship.NewMemoryRepository())).WithMemories(memorySeam)
	// Production attaches the pending-observation coordinator by default;
	// the evals mirror that so the C2 persisted-claim journeys run in-suite.
	agent.WithObservationCoordinator(observation.NewMemoryCoordinator())
	var realizer *scriptedRealizer
	if evalCase.Realizer != nil {
		realizer = &scriptedRealizer{fixture: *evalCase.Realizer}
		agent.WithRealizer(realizer, time.Second)
	}
	if evalCase.Router != nil {
		agent.WithRouter(scriptedRouter{fixture: *evalCase.Router})
	}
	if len(evalCase.Knowledge) > 0 {
		library, cleanup, err := materializeKnowledge(evalCase.Knowledge)
		if err != nil {
			result.addFailure("setup", fmt.Sprintf("knowledge fixture: %v", err))
			return result.finish(startedAt)
		}
		defer cleanup()
		agent.WithKnowledge(library)
	}
	matchID := "eval-" + evalCase.ID
	if _, _, err := store.SetConfig(matchID, evalCase.Config); err != nil {
		result.addFailure("truth", fmt.Sprintf("set config: %v", err))
		return result.finish(startedAt)
	}
	if evalCase.Portrait != nil {
		seededAt := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
		entries := make([]memory.PortraitEntry, 0, len(evalCase.Portrait.Entries))
		for _, entry := range evalCase.Portrait.Entries {
			entries = append(entries, memory.PortraitEntry{
				Topic:     entry.Topic,
				SubTopic:  entry.SubTopic,
				Content:   entry.Content,
				UpdatedAt: seededAt,
				Source:    memory.PortraitSourceSynthesis,
			})
		}
		memorySeam.SetPortrait(evalCase.Portrait.UserID, memory.Portrait{
			Block:     memory.RenderPortraitBlock(entries, seededAt),
			Entries:   entries,
			UpdatedAt: seededAt,
		})
	}

	eventIDs := map[string]string{}
	applyEventStep := func(step EventStep) {
		if err := syncEvalClock(store, matchID, step.Event); err != nil {
			result.addFailure("truth", fmt.Sprintf("event %s clock: %v", step.Key, err))
			return
		}
		var created matchstate.MatchEvent
		var snapshot matchstate.Snapshot
		var err error
		if step.Corrects == "" {
			created, snapshot, err = store.Create(matchID, step.Event)
		} else {
			created, snapshot, err = store.Correct(matchID, eventIDs[step.Corrects], step.Event)
		}
		if step.ExpectError {
			if err == nil {
				result.addFailure("truth", fmt.Sprintf("event %s expected validation error", step.Key))
			}
			return
		}
		if err != nil {
			result.addFailure("truth", fmt.Sprintf("event %s: %v", step.Key, err))
			return
		}
		eventIDs[step.Key] = created.ID
		if step.Proactive != nil {
			result.Steps = append(result.Steps, gradeProactive(ctx, agent, created, snapshot, eventIDs, step.Key, *step.Proactive))
		}
	}
	deferredByTurn := map[string][]EventStep{}
	for _, step := range evalCase.Events {
		if key := strings.TrimSpace(step.AfterTurn); key != "" {
			deferredByTurn[key] = append(deferredByTurn[key], step)
			continue
		}
		applyEventStep(step)
	}

	baseTime := time.Date(2026, 7, 10, 20, 0, 0, 0, time.UTC)
	for index, turn := range evalCase.Turns {
		turnStartedAt := time.Now()
		if realizer != nil {
			// Capture is per-turn: a stale request from an earlier turn must
			// never satisfy (or fail) this turn's memory expectations.
			realizer.captured = nil
		}
		response, err := agent.HandleMessage(ctx, companion.MessageRequest{
			MatchID: matchID,
			UserID:  turn.UserID,
			Text:    turn.Text,
			Voice:   turn.Voice,
			Now:     baseTime.Add(time.Duration(index) * time.Second),
		})
		stepResult := StepResult{ID: turn.ID, Kind: "turn", LatencyMS: int(time.Since(turnStartedAt).Milliseconds())}
		if err != nil {
			stepResult.Failures = append(stepResult.Failures, Failure{Category: "trajectory", Message: err.Error()})
		} else {
			stepResult.Reply = response.Reply
			stepResult.TraceID = response.Trace.ID
			stepResult.Trace = &response.Trace
			var realized *companion.RealizationRequest
			if realizer != nil {
				realized = realizer.captured
			}
			gradeTurn(&stepResult, response, turn.Expect, eventIDs, realized, memorySeam)
			for _, subTopic := range turn.ForgetPortrait {
				memorySeam.ForgetPortraitEntries(turn.UserID, subTopic)
			}
		}
		result.Steps = append(result.Steps, stepResult)
		for _, step := range deferredByTurn[turn.ID] {
			applyEventStep(step)
		}
	}

	if evalCase.Final.Score != nil {
		snapshot := store.Snapshot(matchID)
		if snapshot.Score != *evalCase.Final.Score {
			result.addFailure("truth", fmt.Sprintf("final score got %d-%d, want %d-%d", snapshot.Score.Home, snapshot.Score.Away, evalCase.Final.Score.Home, evalCase.Final.Score.Away))
		}
	}
	traces := tools.Traces()
	if len(traces) < evalCase.Final.MinimumTraces {
		result.addFailure("trace", fmt.Sprintf("stored traces got %d, want at least %d", len(traces), evalCase.Final.MinimumTraces))
	}
	for _, trace := range traces {
		gradeTraceContract(&result, trace)
	}
	for _, step := range result.Steps {
		for _, failure := range step.Failures {
			result.Failures = append(result.Failures, failure)
		}
	}
	return result.finish(startedAt)
}

func syncEvalClock(store *matchstate.Store, matchID string, event matchstate.MatchEvent) error {
	var minutes, seconds int
	if _, err := fmt.Sscanf(strings.TrimSpace(event.Clock), "%d:%d", &minutes, &seconds); err != nil || minutes < 0 || seconds < 0 || seconds > 59 {
		return nil
	}
	elapsed := minutes*60 + seconds
	current := store.Clock(matchID)
	if current.Version > 0 && current.Period == event.Period && elapsed < current.ElapsedSeconds {
		return nil
	}
	_, err := store.SetClock(matchID, matchstate.ClockCommand{
		Action: matchstate.ClockActionSet, Period: event.Period, ElapsedSeconds: &elapsed, ExpectedVersion: current.Version, Source: "eval-fixture",
	})
	return err
}

func gradeProactive(ctx context.Context, agent *companion.Agent, event matchstate.MatchEvent, snapshot matchstate.Snapshot, eventIDs map[string]string, key string, expect ProactiveExpectation) StepResult {
	startedAt := time.Now()
	response, err := agent.HandleMatchEvent(ctx, companion.MatchEventRequest{
		UserID:        fallbackUserID(expect.UserID),
		Event:         event,
		Snapshot:      snapshot,
		OutputAllowed: true,
		Critical:      evalCriticalEvent(event.EventType),
	})
	result := StepResult{ID: "proactive:" + key, Kind: "proactive", LatencyMS: int(time.Since(startedAt).Milliseconds())}
	if err != nil {
		result.Failures = append(result.Failures, Failure{Category: "trajectory", Message: err.Error()})
		return result
	}
	result.Reply = response.Reply
	result.TraceID = response.Trace.ID
	result.Trace = &response.Trace
	if expect.Exact != "" && response.Reply != expect.Exact {
		result.Failures = append(result.Failures, Failure{Category: "truth", Message: fmt.Sprintf("proactive reply got %q, want exact %q", response.Reply, expect.Exact)})
	}
	gradeTextAndTrace(&result, response.Reply, response.Trace, expect.MustMention, expect.MustNotMention, expect.Reason, expect.RequiredTools, expect.ForbiddenTools, expect.RetrievedEventKeys, eventIDs)
	return result
}

func evalCriticalEvent(eventType string) bool {
	switch eventType {
	case "goal", "penalty", "penalty_awarded", "red_card", "var_result", "goal_cancelled", "match_end":
		return true
	default:
		return false
	}
}

func gradeTurn(result *StepResult, response companion.Response, expect TurnExpectation, eventIDs map[string]string, realized *companion.RealizationRequest, memorySeam *memory.Fake) {
	if expect.Intent != "" && response.Intent != expect.Intent {
		result.Failures = append(result.Failures, Failure{Category: "trajectory", Message: fmt.Sprintf("intent got %q, want %q", response.Intent, expect.Intent)})
	}
	if expect.Exact != "" && response.Reply != expect.Exact {
		result.Failures = append(result.Failures, Failure{Category: "truth", Message: fmt.Sprintf("reply got %q, want exact %q", response.Reply, expect.Exact)})
	}
	gradeTextAndTrace(result, response.Reply, response.Trace, expect.MustMention, expect.MustNotMention, expect.Reason, expect.RequiredTools, expect.ForbiddenTools, expect.RetrievedEventKeys, eventIDs)
	gradeMemoryContext(result, expect, realized)
	gradeRouterExpectation(result, response.Trace, expect, memorySeam)
	if expect.ForbidClaim && response.Trace.Claim != nil {
		result.Failures = append(result.Failures, Failure{Category: "claim_safety", Message: "trace must not contain a fact claim"})
	}
	if expect.ClaimKind != "" || expect.ClaimStatus != "" || expect.ClaimCertainty != "" {
		if response.Trace.Claim == nil {
			result.Failures = append(result.Failures, Failure{Category: "claim_safety", Message: "trace is missing expected fact claim"})
		} else {
			if expect.ClaimKind != "" && response.Trace.Claim.Kind != expect.ClaimKind {
				result.Failures = append(result.Failures, Failure{Category: "claim_safety", Message: fmt.Sprintf("claim kind got %q, want %q", response.Trace.Claim.Kind, expect.ClaimKind)})
			}
			if expect.ClaimStatus != "" && response.Trace.Claim.Status != expect.ClaimStatus {
				result.Failures = append(result.Failures, Failure{Category: "claim_safety", Message: fmt.Sprintf("claim status got %q, want %q", response.Trace.Claim.Status, expect.ClaimStatus)})
			}
			if expect.ClaimCertainty != "" && response.Trace.Claim.Certainty != expect.ClaimCertainty {
				result.Failures = append(result.Failures, Failure{Category: "claim_safety", Message: fmt.Sprintf("claim certainty got %q, want %q", response.Trace.Claim.Certainty, expect.ClaimCertainty)})
			}
		}
	}
	if expect.Voice != nil {
		gradeVoice(result, response.Trace.Voice, expect.Voice)
	}
}

func gradeTextAndTrace(result *StepResult, reply string, trace companion.Trace, mustMention, mustNotMention []string, reason string, requiredTools, forbiddenTools, eventKeys []string, eventIDs map[string]string) {
	for _, value := range mustMention {
		if !strings.Contains(reply, value) {
			result.Failures = append(result.Failures, Failure{Category: "truth", Message: fmt.Sprintf("reply must mention %q", value)})
		}
	}
	for _, value := range mustNotMention {
		if strings.Contains(reply, value) {
			result.Failures = append(result.Failures, Failure{Category: "truth", Message: fmt.Sprintf("reply must not mention %q", value)})
		}
	}
	if reason != "" && trace.Reason != reason {
		result.Failures = append(result.Failures, Failure{Category: "trace", Message: fmt.Sprintf("reason got %q, want %q", trace.Reason, reason)})
	}
	toolSet := toolsIn(trace)
	for _, name := range requiredTools {
		if !toolSet[name] {
			result.Failures = append(result.Failures, Failure{Category: "trajectory", Message: fmt.Sprintf("missing required tool %q", name)})
		}
	}
	for _, name := range forbiddenTools {
		if toolSet[name] {
			result.Failures = append(result.Failures, Failure{Category: "trajectory", Message: fmt.Sprintf("called forbidden tool %q", name)})
		}
	}
	for _, key := range eventKeys {
		id := eventIDs[key]
		if id == "" || !contains(trace.RetrievedEvent, id) {
			result.Failures = append(result.Failures, Failure{Category: "trajectory", Message: fmt.Sprintf("trace did not retrieve event key %q", key)})
		}
	}
	if trace.Output == "" || trace.Reason == "" {
		result.Failures = append(result.Failures, Failure{Category: "trace", Message: "trace output and reason are required"})
	}
}

// gradeMemoryContext checks the memory context the realization request
// actually carried — the seam where recall (ADR-0006) and the portrait (C3)
// are injected. Portrait facts reaching here is what makes the portrait real
// rather than a Replika-style unwired diary.
func gradeMemoryContext(result *StepResult, expect TurnExpectation, realized *companion.RealizationRequest) {
	if len(expect.MemoryMustMention) == 0 && len(expect.MemoryMustNotMention) == 0 {
		return
	}
	if realized == nil {
		result.Failures = append(result.Failures, Failure{Category: "trajectory", Message: "memory context expectations need a realizer fixture to capture the realization request"})
		return
	}
	memoryContext := realized.MemoryContext + "\n" + realized.PortraitContext
	for _, value := range expect.MemoryMustMention {
		if !strings.Contains(memoryContext, value) {
			result.Failures = append(result.Failures, Failure{Category: "trajectory", Message: fmt.Sprintf("realization memory context must mention %q", value)})
		}
	}
	for _, value := range expect.MemoryMustNotMention {
		if strings.Contains(memoryContext, value) {
			result.Failures = append(result.Failures, Failure{Category: "trajectory", Message: fmt.Sprintf("realization memory context must not mention %q", value)})
		}
	}
}

// gradeRouterExpectation locks the ADR-0009 routing observability: the raw
// verdict on the trace, the reply-adoption flag, and the C3 funnel thread.
func gradeRouterExpectation(result *StepResult, trace companion.Trace, expect TurnExpectation, memorySeam *memory.Fake) {
	if expect.RouterIntent != "" {
		if trace.Router == nil {
			result.Failures = append(result.Failures, Failure{Category: "trajectory", Message: "turn expected a router verdict, trace.router is empty"})
		} else if trace.Router.Intent != expect.RouterIntent {
			result.Failures = append(result.Failures, Failure{Category: "trajectory", Message: fmt.Sprintf("router intent got %q, want %q", trace.Router.Intent, expect.RouterIntent)})
		}
	}
	if expect.RouterMinConfidence > 0 && (trace.Router == nil || trace.Router.Confidence < expect.RouterMinConfidence) {
		result.Failures = append(result.Failures, Failure{Category: "trajectory", Message: fmt.Sprintf("router confidence %.2f is below the required %.2f", routerConfidenceOf(trace), expect.RouterMinConfidence)})
	}
	if expect.RouterReplyUsed != nil && (trace.Router == nil || trace.Router.ReplyUsed != *expect.RouterReplyUsed) {
		result.Failures = append(result.Failures, Failure{Category: "trajectory", Message: fmt.Sprintf("router replyUsed got %v, want %v", routerReplyUsedOf(trace), *expect.RouterReplyUsed)})
	}
	if expect.UnroutableThread {
		found := false
		if memorySeam != nil {
			for _, thread := range memorySeam.ThreadsAll() {
				if thread.Kind == memory.ThreadUnroutable && thread.UserID == trace.UserID && thread.Content == strings.TrimSpace(trace.Input) {
					found = true
					break
				}
			}
		}
		if !found {
			result.Failures = append(result.Failures, Failure{Category: "trajectory", Message: "expected an unroutable funnel thread for this turn"})
		}
	}
}

func routerConfidenceOf(trace companion.Trace) float64 {
	if trace.Router == nil {
		return 0
	}
	return trace.Router.Confidence
}

func routerReplyUsedOf(trace companion.Trace) bool {
	return trace.Router != nil && trace.Router.ReplyUsed
}

func gradeVoice(result *StepResult, actual, expected *companion.VoiceTraceMetadata) {
	if actual == nil {
		result.Failures = append(result.Failures, Failure{Category: "voice", Message: "expected voice metadata"})
		return
	}
	for _, check := range []struct{ name, actual, expected string }{
		{"asrStatus", actual.ASRStatus, expected.ASRStatus},
		{"asrText", actual.ASRText, expected.ASRText},
		{"ttsStatus", actual.TTSStatus, expected.TTSStatus},
		{"playbackStatus", actual.PlaybackStatus, expected.PlaybackStatus},
	} {
		if check.expected != "" && check.actual != check.expected {
			result.Failures = append(result.Failures, Failure{Category: "voice", Message: fmt.Sprintf("voice %s got %q, want %q", check.name, check.actual, check.expected)})
		}
	}
}

func gradeTraceContract(result *CaseResult, trace companion.Trace) {
	if strings.TrimSpace(trace.Output) == "" || strings.TrimSpace(trace.Reason) == "" {
		result.addFailure("trace", fmt.Sprintf("trace %s has incomplete output or reason", trace.ID))
	}
	for _, call := range trace.ToolCalls {
		if !companion.IsUserAgentToolAllowed(call.Name) {
			result.addFailure("trajectory", fmt.Sprintf("trace %s used disallowed agent tool %q", trace.ID, call.Name))
		}
	}
}

func toolsIn(trace companion.Trace) map[string]bool {
	set := make(map[string]bool, len(trace.ToolCalls))
	for _, call := range trace.ToolCalls {
		set[call.Name] = true
	}
	return set
}

func fallbackUserID(userID string) string {
	if strings.TrimSpace(userID) == "" {
		return "eval-user"
	}
	return userID
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func (result *CaseResult) addFailure(category, message string) {
	result.Failures = append(result.Failures, Failure{Category: category, Message: message})
}

func (result CaseResult) finish(startedAt time.Time) CaseResult {
	result.LatencyMS = int(time.Since(startedAt).Milliseconds())
	failed := map[string]bool{}
	for _, failure := range result.Failures {
		failed[failure.Category] = true
	}
	result.Passed = len(result.Failures) == 0
	for _, category := range []string{"truth", "trajectory", "trace", "voice"} {
		if failed[category] {
			result.Scores[category] = 0
		} else {
			result.Scores[category] = 1
		}
	}
	return result
}

func summarize(cases []CaseResult) Scorecard {
	scorecard := Scorecard{TotalCases: len(cases), Suites: map[string]float64{}}
	if len(cases) == 0 {
		return scorecard
	}
	suiteTotals := map[string]int{}
	suitePassed := map[string]int{}
	truth, trajectory, trace := 0, 0, 0
	for _, result := range cases {
		suiteTotals[result.Suite]++
		if result.Passed {
			scorecard.PassedCases++
			suitePassed[result.Suite]++
		}
		if result.Scores["truth"] == 1 {
			truth++
		}
		if result.Scores["trajectory"] == 1 {
			trajectory++
		}
		if result.Scores["trace"] == 1 {
			trace++
		}
	}
	scorecard.FailedCases = scorecard.TotalCases - scorecard.PassedCases
	scorecard.PassRate = float64(scorecard.PassedCases) / float64(scorecard.TotalCases)
	scorecard.FactSafetyRate = float64(truth) / float64(scorecard.TotalCases)
	scorecard.TrajectoryRate = float64(trajectory) / float64(scorecard.TotalCases)
	scorecard.TraceCompleteRate = float64(trace) / float64(scorecard.TotalCases)
	for suite, total := range suiteTotals {
		scorecard.Suites[suite] = float64(suitePassed[suite]) / float64(total)
	}
	return scorecard
}

// scriptedRealizer stands in for the LLM realizer and captures every
// realization request so memory-context expectations can grade the injection
// point. It is used as *scriptedRealizer (pointer) so captures survive.
type scriptedRealizer struct {
	fixture  RealizerFixture
	captured *companion.RealizationRequest
}

func (realizer *scriptedRealizer) Realize(_ context.Context, req companion.RealizationRequest) (companion.RealizedTurn, error) {
	captured := req
	realizer.captured = &captured
	if realizer.fixture.Error != "" {
		return companion.RealizedTurn{}, errors.New(realizer.fixture.Error)
	}
	return companion.RealizedTurn{Text: realizer.fixture.Reply}, nil
}

// scriptedRouter stands in for the ADR-0009 intent router: the route whose
// matchText equals the user text answers the turn; texts without a route
// fall through to unknown/1.0 (the casual gate never opens). A route with
// error=true simulates a transport failure so the degradation path is
// exercised in-suite.
type scriptedRouter struct {
	fixture RouterFixture
}

func (router scriptedRouter) Enabled() bool { return true }

func (router scriptedRouter) Route(_ context.Context, req qiuqiuRouter.Request) (qiuqiuRouter.Result, error) {
	for _, route := range router.fixture.Routes {
		if route.MatchText != req.Text {
			continue
		}
		if route.Error {
			return qiuqiuRouter.Result{}, errors.New("scripted router failure")
		}
		return qiuqiuRouter.Result{
			Intent:     route.Intent,
			Player:     route.Player,
			Team:       route.Team,
			Score:      route.Score,
			Confidence: route.Confidence,
			Reply:      route.Reply,
		}, nil
	}
	return qiuqiuRouter.Result{Intent: "unknown", Confidence: 1}, nil
}

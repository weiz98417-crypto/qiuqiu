package evals

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"qiuqiu/internal/companion"
	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/relationship"
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

func runCase(ctx context.Context, evalCase Case) CaseResult {
	startedAt := time.Now()
	result := CaseResult{ID: evalCase.ID, Suite: evalCase.Suite, Tags: evalCase.Tags, Scores: map[string]float64{}}
	store := matchstate.NewStore()
	tools := companion.NewStoreMemoryTools(store)
	agent := companion.NewAgent(tools).WithDirector(relationship.NewDirector(relationship.NewMemoryRepository()))
	if evalCase.Realizer != nil {
		agent.WithRealizer(scriptedRealizer{fixture: *evalCase.Realizer}, time.Second)
	}
	matchID := "eval-" + evalCase.ID
	if _, _, err := store.SetConfig(matchID, evalCase.Config); err != nil {
		result.addFailure("truth", fmt.Sprintf("set config: %v", err))
		return result.finish(startedAt)
	}

	eventIDs := map[string]string{}
	for _, step := range evalCase.Events {
		if err := syncEvalClock(store, matchID, step.Event); err != nil {
			result.addFailure("truth", fmt.Sprintf("event %s clock: %v", step.Key, err))
			continue
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
			continue
		}
		if err != nil {
			result.addFailure("truth", fmt.Sprintf("event %s: %v", step.Key, err))
			continue
		}
		eventIDs[step.Key] = created.ID
		if step.Proactive != nil {
			result.Steps = append(result.Steps, gradeProactive(ctx, agent, created, snapshot, eventIDs, step.Key, *step.Proactive))
		}
	}

	baseTime := time.Date(2026, 7, 10, 20, 0, 0, 0, time.UTC)
	for index, turn := range evalCase.Turns {
		turnStartedAt := time.Now()
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
			gradeTurn(&stepResult, response, turn.Expect, eventIDs)
		}
		result.Steps = append(result.Steps, stepResult)
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

func gradeTurn(result *StepResult, response companion.Response, expect TurnExpectation, eventIDs map[string]string) {
	if expect.Intent != "" && response.Intent != expect.Intent {
		result.Failures = append(result.Failures, Failure{Category: "trajectory", Message: fmt.Sprintf("intent got %q, want %q", response.Intent, expect.Intent)})
	}
	if expect.Exact != "" && response.Reply != expect.Exact {
		result.Failures = append(result.Failures, Failure{Category: "truth", Message: fmt.Sprintf("reply got %q, want exact %q", response.Reply, expect.Exact)})
	}
	gradeTextAndTrace(result, response.Reply, response.Trace, expect.MustMention, expect.MustNotMention, expect.Reason, expect.RequiredTools, expect.ForbiddenTools, expect.RetrievedEventKeys, eventIDs)
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

type scriptedRealizer struct{ fixture RealizerFixture }

func (realizer scriptedRealizer) Realize(context.Context, companion.RealizationRequest) (companion.RealizedTurn, error) {
	if realizer.fixture.Error != "" {
		return companion.RealizedTurn{}, errors.New(realizer.fixture.Error)
	}
	return companion.RealizedTurn{Text: realizer.fixture.Reply}, nil
}

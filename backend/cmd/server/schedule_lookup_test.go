package main

import (
	"context"
	"testing"
	"time"

	"qiuqiu/internal/companion"
	"qiuqiu/internal/datasource"
)

type fixedScheduleFixturesClient struct {
	fixtures []datasource.Fixture
}

func (client fixedScheduleFixturesClient) GetTodayFixturesContext(context.Context) ([]datasource.Fixture, error) {
	return client.fixtures, nil
}

func (client fixedScheduleFixturesClient) GetFixturesContext(context.Context, time.Time, time.Time, string) ([]datasource.Fixture, error) {
	return client.fixtures, nil
}

func TestScheduleLookupLifecycleRejectsAStaleTurn(t *testing.T) {
	var lifecycle scheduleLookupLifecycle
	staleGeneration := lifecycle.BeginTurn()
	lifecycle.BeginTurn()

	lookupCtx, started := lifecycle.Start(context.Background(), staleGeneration, "lookup-old", time.Now().Add(time.Minute))
	if started {
		t.Fatal("lookup from an older user turn was accepted")
	}
	select {
	case <-lookupCtx.Done():
	default:
		t.Fatal("rejected lookup context was not canceled")
	}
}

func TestAPISportsScheduleReaderKeepsUnknownScoreUnset(t *testing.T) {
	reader := apiSportsScheduleReader{client: fixedScheduleFixturesClient{fixtures: []datasource.Fixture{
		{ID: 1, HomeTeam: "Spain", AwayTeam: "Germany", Competition: "Friendly", Status: "NS"},
		{ID: 2, HomeTeam: "France", AwayTeam: "Brazil", Competition: "Friendly", Status: "FT", HomeGoalKnown: true, AwayGoalKnown: true},
		{ID: 3, HomeTeam: "Club A", AwayTeam: "Club B", Competition: "Other", Status: "NS"},
	}}}
	result, err := reader.Search(context.Background(), companion.ScheduleSearchRequest{Competition: "Friendly"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Fixtures) != 2 {
		t.Fatalf("filtered fixtures = %+v, want two Friendly matches", result.Fixtures)
	}
	if result.Fixtures[0].HomeScore != nil || result.Fixtures[0].AwayScore != nil {
		t.Fatalf("unknown score became known: %+v", result.Fixtures[0])
	}
	if result.Fixtures[1].HomeScore == nil || result.Fixtures[1].AwayScore == nil ||
		*result.Fixtures[1].HomeScore != 0 || *result.Fixtures[1].AwayScore != 0 {
		t.Fatalf("known goalless score was lost: %+v", result.Fixtures[1])
	}
}

func TestScheduleLookupLifecycleCancelsAndCompletesOnce(t *testing.T) {
	var lifecycle scheduleLookupLifecycle
	generation := lifecycle.BeginTurn()
	lookupCtx, started := lifecycle.Start(context.Background(), generation, "lookup-1", time.Now().Add(time.Minute))
	if !started || !lifecycle.IsCurrent("lookup-1") {
		t.Fatal("current lookup was not registered")
	}
	if !lifecycle.Complete("lookup-1") || lifecycle.Complete("lookup-1") {
		t.Fatal("lookup completion was not idempotent")
	}
	select {
	case <-lookupCtx.Done():
	default:
		t.Fatal("completed lookup context was not released")
	}

	generation = lifecycle.BeginTurn()
	lookupCtx, started = lifecycle.Start(context.Background(), generation, "lookup-2", time.Now().Add(time.Minute))
	if !started {
		t.Fatal("second lookup was not registered")
	}
	lifecycle.Cancel()
	select {
	case <-lookupCtx.Done():
	default:
		t.Fatal("canceled lookup context remained active")
	}
}

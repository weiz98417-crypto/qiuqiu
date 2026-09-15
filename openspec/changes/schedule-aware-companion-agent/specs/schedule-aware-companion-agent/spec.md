# Schedule-Aware Companion Agent

## ADDED Requirements

### Requirement: Schedule questions use structured intent fields

The companion MUST classify a football schedule request into the `football_schedule` topic, the `query` action, and one of the `current`, `today`, `tomorrow`, or `nearby` scopes without requiring an exact sentence match.

#### Scenario: Natural schedule wording

- **WHEN** the user asks `有什么比赛吗？`
- **THEN** the request is classified as a football schedule query with the `nearby` scope
- **AND** the trace records the structured intent and confidence

#### Scenario: Explicit date scope

- **WHEN** the user asks `明天有什么比赛？`
- **THEN** the request is classified with the `tomorrow` scope
- **AND** the search window uses the user's timezone

### Requirement: Active match context takes precedence over schedule search

The companion MUST read the attached public match snapshot before external schedule search for `current` and `nearby` requests, and MUST include the configured competition when it is available.

#### Scenario: Live attached match

- **WHEN** Spain is leading Germany 1-0 in an in-progress friendly match and the user asks `有什么比赛吗？`
- **THEN** the companion answers with the attached teams, competition, score, period, and clock
- **AND** no external schedule search is called

#### Scenario: No usable live context

- **WHEN** the attached snapshot is absent, finished, stale, or conflicted
- **THEN** the companion does not present it as a live answer
- **AND** the request continues to the schedule search path

### Requirement: Date-range schedule search returns verified fixture metadata

The schedule search contract MUST accept a timezone-aware date range and MUST normalize fixture teams, competition, kickoff, lifecycle status, score, source, and freshness before formatting a response.

#### Scenario: Complete provider result

- **WHEN** the provider returns a complete fixture with competition, status, and score metadata
- **THEN** the companion formats those fields in the schedule answer
- **AND** the trace records a `schedule.search` tool call with the requested date range

#### Scenario: Incomplete provider result

- **WHEN** a provider result has no home team or away team
- **THEN** the fixture is discarded
- **AND** the companion reports that no reliable schedule result is available instead of inventing a fixture

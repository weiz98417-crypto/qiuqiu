# Design

## Provider Boundary

Provider adapters should expose a small interface:

- `listFixtures(date)`
- `getMatch(matchId)`
- `getTeams(competitionId)`
- `getLineups(matchId)` optional
- `getScore(matchId)` optional

Live event feeds are intentionally excluded from the first bridge.

## Provider Priority

Manual operator state remains the product source of truth for live events. Provider data may:

- Prefill team names and kickoff times.
- Suggest clock/score corrections.
- Provide halftime/fulltime validation.
- Enrich AI context with non-live metadata.

## Failure Behavior

If provider calls fail:

- Operator panel remains usable.
- Frontend companion remains usable.
- UI shows provider status only to operators, not casual users.

## Candidate Sources

Candidate providers should be evaluated on:

- Coverage for World Cup 2026.
- Free or low monthly cost.
- Terms of use.
- Rate limits.
- Latency and reliability.
- Whether commercial use is allowed.

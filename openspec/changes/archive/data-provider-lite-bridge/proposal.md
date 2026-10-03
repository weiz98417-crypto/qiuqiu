# Data Provider Lite Bridge

## Why

Manual operation solves the MVP blocker, but lightweight data can still improve trust and reduce operator work. The product should support schedules, teams, lineups, and score checks from cheap/free providers without becoming dependent on them.

## What Changes

- Define a provider adapter boundary for low-cost football data.
- Start with non-critical data: fixtures, teams, match metadata, and optional score validation.
- Keep provider data subordinate to operator-authored live events during the MVP.

## Non-goals

- Do not buy or integrate official live event streams in this change.
- Do not scrape copyrighted broadcast data.
- Do not make provider availability required for the app to run.

## Success Criteria

- The app can run a manual match with no provider configured.
- Provider data can prefill match metadata when available.
- Provider failures degrade gracefully.

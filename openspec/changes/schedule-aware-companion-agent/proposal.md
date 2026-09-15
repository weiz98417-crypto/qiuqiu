# Schedule-Aware Companion Agent

## Status

In progress — foundation slice landed

## Why

QiuQiu currently treats schedule questions as a collection of hand-written phrases. This creates two problems:

1. Natural variants such as `有什么比赛吗？` can fall through to `unknown` even when a live match is already attached to the session.
2. A schedule lookup is handled as one synchronous reply instead of an agent task that can acknowledge the request, search reliable sources, and proactively report the result.

The user-facing behavior should be context-aware: the current match is the first source of truth, and external schedule search is only needed when the current session does not contain the requested match context.

## What Changes

- Replace phrase enumeration with structured schedule intent fields: topic, action, and time scope.
- Prefer the active match snapshot before querying external schedule sources.
- Add a typed `schedule.search` tool with today/tomorrow/nearby scopes and explicit source metadata.
- Split schedule interaction into an immediate acknowledgement and an asynchronous result announcement.
- Offer reminders only after the user explicitly confirms; do not create reminders from a suggestion.
- Add cancellation, deduplication, trace linkage, timeout, and stale-result protection for schedule lookups.
- Keep structured match facts authoritative over search results and model-generated text.

## Goals

- Understand natural schedule questions without maintaining a sentence-by-sentence phrase list.
- Answer an active match question with the current teams, competition, score, clock, and freshness.
- Search today and tomorrow when there is no active match context.
- Tell the user when a search starts and proactively announce a reliable result.
- Preserve the existing fact-safety boundary: no score, event, or competition may be invented.

## Non-goals

- Do not let the user-facing agent create, correct, or revoke match facts.
- Do not treat web search snippets as authoritative live score data.
- Do not create a reminder before explicit user confirmation.
- Do not replace structured match-event retrieval with vector search.
- Do not introduce a separate agent runtime before the Go tool and delivery contracts are stable.

## Acceptance Criteria

- `有什么比赛吗？` while Spain–Germany is live returns the current match context instead of an unknown fallback.
- `现在有什么比赛？` first checks the attached live match before external search.
- With no active match, `今天有球吗？` or `明天有什么比赛？` produces an acknowledgement followed by a result announcement when a source responds.
- A source failure produces a transparent failure message and never fabricates fixtures.
- A user interruption cancels or invalidates the pending lookup result.
- Every lookup has a trace containing intent fields, scope, sources, result freshness, delivery state, and error information.

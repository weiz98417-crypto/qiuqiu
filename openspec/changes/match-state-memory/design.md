# Design

## Match State Snapshot

```json
{
  "matchId": "match_...",
  "homeTeam": "Argentina",
  "awayTeam": "France",
  "score": { "home": 1, "away": 0 },
  "period": "first_half",
  "clock": "23:15",
  "momentum": "home_pressure",
  "emotionalTemperature": 4,
  "recentEvents": [],
  "keyEvents": [],
  "lastUpdatedAt": "2026-05-22T00:00:00.000Z"
}
```

## Prompt Injection

DeepSeek should receive a compact match context block before user-visible chat history. The context should be factual and short.

Example:

```text
Current match context:
- Argentina vs France, Argentina leads 1-0 at 23:15.
- Recent event: Argentina scored from a back-post header.
- User is watching alone and wants casual explanation, emotional companionship, and football context.
```

## Memory Rules

- Score and clock are always preserved.
- Recent events are summarized, not pasted endlessly.
- User mood is tracked lightly for tone, not used as a medical or psychological profile.
- If operator data conflicts with user chat, ask for clarification or state uncertainty.

## Failure Behavior

If match state is missing, QiuQiu should gracefully fall back to normal companion chat and invite the user to tell it what happened.

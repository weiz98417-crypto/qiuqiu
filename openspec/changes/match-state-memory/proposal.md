# Match State Memory

## Why

QiuQiu should not respond like a generic chatbot during a match. Once the operator enters events, the assistant needs persistent match context: score, time, recent events, emotional arc, and user reactions.

## What Changes

- Maintain a normalized match state derived from the event timeline.
- Inject relevant match state into DeepSeek prompts.
- Track recent user sentiment and companion tone.
- Provide a compact context snapshot for frontend and backend use.

## Non-goals

- Do not create a long-term user profile in this change.
- Do not build advanced tactical analytics.
- Do not require paid provider data.

## Success Criteria

- AI replies can reference current score and recent match events.
- The companion can react proactively to operator events.
- Long chat history does not erase core match context.

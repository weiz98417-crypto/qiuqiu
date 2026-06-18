# Memory Trace Console

## Why

The product's trust comes from showing that QiuQiu answers from director facts and remembered conversation context. Operators and customers need a console that explains what QiuQiu knew, what it retrieved, why it answered, and where failures occurred.

## What Changes

- Add or enhance a match memory panel for active facts and recent conversation context.
- Expand trace visualization for agent decisions, ASR, TTS, and fallback reasons.
- Show retrieved event IDs and referenced facts for each QiuQiu response.
- Make correction state visible so inactive facts are auditable but not treated as current truth.
- Add evals for trace completeness and active-fact-only answers.

## Non-goals

- Do not expose secrets, raw API keys, or private environment values.
- Do not let the trace panel mutate match facts.
- Do not replace the director console's editing flow.
- Do not build long-term vector memory in this change.

## Success Criteria

- An operator can inspect the latest active match facts in one place.
- An operator can open a QiuQiu response and see input, intent, tool calls, retrieved event IDs, output, and fallback reason.
- Voice turns show ASR/TTS status without exposing secrets.
- Corrected facts are visible as history but not used by current answers.
- Trace and memory views support the demo script without developer tools.

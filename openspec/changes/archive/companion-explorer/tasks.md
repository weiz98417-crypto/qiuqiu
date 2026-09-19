## 1. Intent And Policy

- [x] 1.1 Define explorer intent categories in code and OpenSpec.
- [x] 1.2 Route match status, recent event, player, smalltalk, and control commands deterministically.
- [x] 1.3 Make unknown input fall back to a safe companion response.

## 2. Match Memory Tools

- [x] 2.1 Expose snapshot lookup for current score, clock, period, and recent events.
- [x] 2.2 Expose recent event lookup for assistant-style questions like "刚才谁助攻？".
- [x] 2.3 Expose player timeline lookup for player-specific questions.

## 3. Reply Generation

- [x] 3.1 Build structured reply templates for each intent.
- [x] 3.2 Keep LLM output as optional polish only.
- [x] 3.3 Ensure missing facts produce a safe "not recorded" answer.

## 4. Trace And Persistence

- [x] 4.1 Persist user turns, intent, tool calls, retrieved event IDs, and output.
- [x] 4.2 Persist enough metadata to reconstruct the turn later.
- [x] 4.3 Keep trace writing non-blocking for the main chat flow.

## 5. Evals

- [x] 5.1 Add baseline eval for goal -> assist -> score -> missing-player question.
- [x] 5.2 Add boundary eval for quiet mode, correction-aware memory, and unknown facts.
- [x] 5.3 Add a benchmark for the recent-event query path.

## 6. Product Integration

- [x] 6.1 Route user speech through the explorer path in the backend.
- [x] 6.2 Preserve existing TTS / Live2D output handling.
- [x] 6.3 Keep the director console unchanged for this change.

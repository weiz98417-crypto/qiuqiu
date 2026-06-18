## 1. Memory Panel

- [x] 1.1 Add or enhance a panel showing current score, clock, period, and active facts.
- [x] 1.2 Show recent active events and key events separately.
- [x] 1.3 Show participant roles for the selected event.
- [x] 1.4 Show recent user/QiuQiu turns relevant to the current match.
- [x] 1.5 Visually separate corrected/inactive facts from active facts.

## 2. Trace Panel

- [x] 2.1 Show trace list with intent, output summary, created time, and fallback state.
- [x] 2.2 Show trace detail with input, intent, tool calls, retrieved event IDs, output, reason, latency, and errors.
- [x] 2.3 Show referenced match events from trace detail.
- [x] 2.4 Add ASR/TTS status fields to trace detail when available.
- [x] 2.5 Ensure trace detail never displays API keys, tokens, or raw environment values.

## 3. Voice Metadata

- [x] 3.1 Record ASR success/failure status without storing raw audio.
- [x] 3.2 Record TTS success/failure status, mime type, and byte count without storing raw audio.
- [x] 3.3 Record browser playback fallback state when reported.
- [x] 3.4 Add tests for voice metadata in traces.

## 4. Active Fact Authority

- [x] 4.1 Verify active facts shown in memory panel match snapshot state.
- [x] 4.2 Verify corrected events are visible as history but excluded from current answers.
- [x] 4.3 Verify follow-up traces reference the correct active event ID.
- [x] 4.4 Add evals for corrected event visibility and active-only answers.

## 5. QA

- [x] 5.1 Run trace API tests.
- [x] 5.2 Run browser QA for memory and trace panels.
- [x] 5.3 Verify the demo script can be presented without opening developer tools.

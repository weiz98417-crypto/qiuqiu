# Voice Browser E2E Acceptance Specification

## ADDED Requirements

### Requirement: Browser voice happy path

The user app MUST support a real browser voice loop from microphone input to QiuQiu text and audio output.

#### Scenario: User asks about the latest assist by voice

- **GIVEN** the canonical Spain vs Germany demo is seeded with Pedri scoring, Fabian assisting, and Yamal participating in the build-up
- **AND** `MIMO_API_KEY` is configured through the process environment
- **WHEN** the user grants microphone permission and asks "刚才谁助攻？"
- **THEN** MiMo ASR transcribes the user speech into text
- **AND** QiuQiu answers from active director facts
- **AND** the answer mentions Fabian
- **AND** MiMo TTS returns playable audio to the browser

#### Scenario: Live2D state transitions during voice

- **GIVEN** the user starts a voice interaction
- **WHEN** recording, ASR, companion reply, and TTS playback proceed
- **THEN** the browser shows listening, processing, replying, and speaking states in order
- **AND** Live2D returns to idle after playback or fallback completion

### Requirement: Browser voice fallback behavior

Voice failures MUST degrade to usable text chat without disconnecting the match session.

#### Scenario: Microphone permission denied

- **GIVEN** the user denies microphone permission
- **WHEN** the user app receives the denial
- **THEN** the app shows a recoverable voice fallback state
- **AND** text chat remains usable
- **AND** the WebSocket session remains connected when it was already connected

#### Scenario: ASR failure

- **GIVEN** MiMo ASR returns an error or empty transcript
- **WHEN** the browser voice request completes
- **THEN** QiuQiu does not publish an ungrounded answer
- **AND** the UI explains that speech recognition failed
- **AND** text input remains usable for the same question

#### Scenario: TTS or playback failure

- **GIVEN** QiuQiu produced a grounded text answer
- **WHEN** MiMo TTS fails or the browser blocks audio playback
- **THEN** the grounded text answer remains visible
- **AND** Live2D performs a non-audio speaking or fallback state
- **AND** the user can continue the conversation

### Requirement: Voice acceptance evidence

The project MUST provide repeatable evidence for browser voice acceptance without storing secrets or raw user audio.

#### Scenario: Successful voice run evidence

- **GIVEN** a successful browser voice run completed
- **WHEN** the tester reviews acceptance evidence
- **THEN** the evidence includes ASR text, QiuQiu reply text, TTS status or byte count, Live2D state evidence, and a trace ID
- **AND** the evidence does not include API keys, authorization headers, or raw audio blobs

#### Scenario: Repeatable acceptance instructions

- **GIVEN** a presenter needs to verify voice before a demo
- **WHEN** they open the voice acceptance runbook or script instructions
- **THEN** they can seed the canonical match, configure MiMo environment variables, open the correct URL, grant browser permissions, and verify expected output without reading source code

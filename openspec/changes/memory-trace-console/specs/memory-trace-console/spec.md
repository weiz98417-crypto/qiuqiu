# Memory Trace Console Specification

## ADDED Requirements

### Requirement: Match memory panel

The operator console MUST provide a read-only match memory panel that explains the active facts QiuQiu can use.

#### Scenario: Active match facts are visible

- **GIVEN** a match has current score, period, clock, and recent active events
- **WHEN** the operator opens the memory panel
- **THEN** the panel shows score, period, clock, recent active events, and key event summaries
- **AND** selected events show participant roles such as scorer, assist, and pre-assist

#### Scenario: Corrected facts are separated

- **GIVEN** a match contains corrected events and active replacement events
- **WHEN** the operator opens the memory panel
- **THEN** corrected facts are visually separated from active facts
- **AND** corrected facts are not presented as current truth

### Requirement: Trace detail for companion decisions

The operator console MUST expose enough trace detail to explain why QiuQiu produced a response.

#### Scenario: Trace detail for a grounded answer

- **GIVEN** QiuQiu answered "刚才谁助攻？" from a director goal event
- **WHEN** the operator opens that trace
- **THEN** the detail shows input text, intent, tool calls, retrieved event IDs, output text, reason, latency, and created timestamp
- **AND** the referenced goal event is visible from the trace detail

#### Scenario: Trace detail for missing facts

- **GIVEN** QiuQiu answered that a requested fact is not currently recorded
- **WHEN** the operator opens that trace
- **THEN** the detail shows the attempted lookup
- **AND** the detail records the missing-fact or fallback reason

### Requirement: Voice metadata in traces

Voice turns MUST record non-secret ASR and TTS metadata that can be inspected from the trace console.

#### Scenario: Successful voice trace

- **GIVEN** a user voice question is processed successfully
- **WHEN** the trace detail is opened
- **THEN** it shows ASR status, ASR text, TTS status, mime type, and audio byte count when available
- **AND** it does not store or display raw audio

#### Scenario: Voice fallback trace

- **GIVEN** ASR, TTS, or browser playback failed during a turn
- **WHEN** the trace detail is opened
- **THEN** it shows the failure stage and fallback reason
- **AND** it does not expose API keys, tokens, or raw environment values

### Requirement: Demo-readable trace UX

The trace and memory console MUST be usable in a customer demo without opening developer tools.

#### Scenario: Presenter explains a grounded answer

- **GIVEN** the canonical demo has a proactive goal and a user follow-up
- **WHEN** the presenter opens the memory and trace panels
- **THEN** they can show that the director event was recorded, QiuQiu retrieved that exact event, and the answer was grounded in active facts
- **AND** all visible data is safe for customer-facing presentation
